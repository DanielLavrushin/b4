package ws

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/leaktest"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/gorilla/websocket"
	"go.uber.org/goleak"
)

type fakeFrames struct {
	hellos atomic.Int64
	ticks  atomic.Int64
	pad    int
}

func (f *fakeFrames) frame(kind string, n int64) metrics.Frame {
	fr := metrics.Frame{Type: kind, Now: n}
	for i := 0; i < f.pad; i++ {
		fr.Sets = append(fr.Sets, metrics.SetActivity{ID: strings.Repeat("x", 64), Name: strings.Repeat("y", 64)})
	}
	return fr
}

func (f *fakeFrames) Hello() metrics.Frame {
	fr := f.frame(metrics.FrameHello, f.hellos.Add(1))
	fr.Blocked = &metrics.BlockedLists{Rev: 7}
	fr.Escalations = &metrics.EscalationList{Rev: 3}
	fr.Events = &metrics.EventLog{Rev: 5}
	return fr
}

func (f *fakeFrames) Tick(sent *metrics.SentRevs) metrics.Frame {
	return f.frame(metrics.FrameTick, f.ticks.Add(1))
}

func metricsLeakOptions() []goleak.Option {
	return leaktest.Options(goleak.IgnoreTopFunction("github.com/daniellavrushin/b4/http/ws.(*LogHub).run"))
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func hubSent(h *metricsHub) metrics.SentRevs {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sent
}

func dialMetrics(t *testing.T, url string, rcvbuf int) *websocket.Conn {
	t.Helper()
	d := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	if rcvbuf > 0 {
		d.NetDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			if err == nil {
				err = c.(*net.TCPConn).SetReadBuffer(rcvbuf)
			}
			return c, err
		}
	}
	c, _, err := d.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

func readFrame(t *testing.T, c *websocket.Conn) metrics.Frame {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var fr metrics.Frame
	if err := json.Unmarshal(data, &fr); err != nil {
		t.Fatalf("decode %q: %v", data, err)
	}
	return fr
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMetricsWebSocketSendsHelloThenSharedTicks(t *testing.T) {
	defer goleak.VerifyNone(t, metricsLeakOptions()...)
	src := &fakeFrames{}
	h := newMetricsHub(src)
	srv := httptest.NewServer(http.HandlerFunc(h.serve))
	defer srv.Close()
	url := wsURL(srv)

	h.broadcast()
	if h.encodes.Load() != 0 || src.ticks.Load() != 0 {
		t.Fatal("with no subscribers a tick must not build or encode anything")
	}

	a := dialMetrics(t, url, 0)
	b := dialMetrics(t, url, 0)
	if fr := readFrame(t, a); fr.Type != metrics.FrameHello {
		t.Fatalf("first frame must be hello, got %q", fr.Type)
	}
	if fr := readFrame(t, b); fr.Type != metrics.FrameHello {
		t.Fatalf("first frame must be hello, got %q", fr.Type)
	}
	waitFor(t, "two subscribers", func() bool { return h.subscribers() == 2 })
	if got := hubSent(h); got != (metrics.SentRevs{Blocked: 7, Escalations: 3, Events: 5}) {
		t.Fatalf("the first subscriber's hello sets the sent revs: %+v", got)
	}

	for i := 1; i <= 3; i++ {
		h.broadcast()
		for _, c := range []*websocket.Conn{a, b} {
			fr := readFrame(t, c)
			if fr.Type != metrics.FrameTick || fr.Now != int64(i) {
				t.Fatalf("tick %d: got %q now=%d", i, fr.Type, fr.Now)
			}
		}
	}
	if got := h.encodes.Load(); got != 3 {
		t.Fatalf("two clients must share one encode per tick: %d encodes for 3 ticks", got)
	}
	if got := src.ticks.Load(); got != 3 {
		t.Fatalf("one Tick frame per broadcast, got %d", got)
	}

	a.Close()
	b.Close()
	waitFor(t, "subscribers to leave", func() bool { return h.subscribers() == 0 })
}

func TestMetricsWebSocketDropsASlowClient(t *testing.T) {
	src := &fakeFrames{pad: 400}
	h := newMetricsHub(src)
	srv := httptest.NewServer(http.HandlerFunc(h.serve))
	defer srv.Close()
	url := wsURL(srv)

	fast := dialMetrics(t, url, 0)
	readFrame(t, fast)
	slow := dialMetrics(t, url, 4096)
	defer slow.Close()
	waitFor(t, "two subscribers", func() bool { return h.subscribers() == 2 })

	got := make(chan int64, 1)
	go func() {
		defer close(got)
		for {
			fast.SetReadDeadline(time.Now().Add(10 * time.Second))
			_, data, err := fast.ReadMessage()
			if err != nil {
				return
			}
			var fr metrics.Frame
			if json.Unmarshal(data, &fr) == nil {
				got <- fr.Now
			}
		}
	}()

	dropped := false
	last := int64(0)
	for i := int64(1); i <= 5000 && !dropped; i++ {
		h.broadcast()
		select {
		case n, ok := <-got:
			if !ok || n != i {
				t.Fatalf("the fast client must keep receiving every tick: got %d ok=%v want %d", n, ok, i)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("the fast client stalled at tick %d", i)
		}
		last = i
		dropped = h.subscribers() == 1
	}
	if !dropped {
		t.Fatal("a client that does not read must be dropped once its buffer is full")
	}

	h.broadcast()
	select {
	case n, ok := <-got:
		if !ok || n != last+1 {
			t.Fatalf("the fast client is still served after the slow one was dropped: got %d ok=%v", n, ok)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the fast client stopped receiving after the slow one was dropped")
	}
	if got := h.encodes.Load(); got != uint64(last+1) {
		t.Fatalf("one encode per broadcast, got %d for %d", got, last+1)
	}

	fast.Close()
	for range got {
	}
	waitFor(t, "the fast subscriber to leave", func() bool { return h.subscribers() == 0 })
	srv.Close()
	goleak.VerifyNone(t, metricsLeakOptions()...)
}

func TestMetricsWebSocketUnregistersWhenTheClientLeaves(t *testing.T) {
	defer goleak.VerifyNone(t, metricsLeakOptions()...)
	h := newMetricsHub(&fakeFrames{})
	srv := httptest.NewServer(http.HandlerFunc(h.serve))
	defer srv.Close()
	url := wsURL(srv)
	c := dialMetrics(t, url, 0)
	readFrame(t, c)
	waitFor(t, "a subscriber", func() bool { return h.subscribers() == 1 })
	c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	c.Close()
	waitFor(t, "the subscriber to leave", func() bool { return h.subscribers() == 0 })
	h.broadcast()
	if h.encodes.Load() != 0 {
		t.Fatal("no subscriber left, nothing to encode")
	}
}

func TestMetricsWebSocketServesTheRealCollector(t *testing.T) {
	defer goleak.VerifyNone(t, metricsLeakOptions()...)
	srv := httptest.NewServer(http.HandlerFunc(HandleMetricsWebSocket))
	defer srv.Close()
	c := dialMetrics(t, wsURL(srv), 0)
	defer c.Close()

	hello := readFrame(t, c)
	if hello.Type != metrics.FrameHello || len(hello.Activity.Minute) == 0 || hello.Blocked == nil || hello.Events == nil || hello.Escalations == nil || hello.Process.ThreadLimit == 0 {
		t.Fatalf("hello from the collector: %+v", hello)
	}
	tick := readFrame(t, c)
	if tick.Type != metrics.FrameTick || tick.Now < hello.Now {
		t.Fatalf("the collector's own tick must reach the client: %+v", tick)
	}
	if n := len(tick.Activity.Minute); n < 1 || n > 2 {
		t.Fatalf("a tick carries the open and the last closed bucket, got %d", n)
	}
}
