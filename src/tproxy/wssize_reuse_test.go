package tproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/warmtuple"
	"golang.org/x/sys/unix"
)

type reuseTestDialer struct {
	dial func(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error)
}

func (d reuseTestDialer) Dial(ctx context.Context, s *config.SetConfig, owner, host string, target netip.AddrPort) (net.Conn, error) {
	return d.dial(ctx, s, owner, host, target)
}
func (d reuseTestDialer) AcquireClient() (func(), bool) { return func() {}, true }

type reuseTestClient struct{ net.Conn }

func (c reuseTestClient) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 50000}
}

func TestWSReuseReplaysOriginalFragmentedClientHello(t *testing.T) {
	hello := realClientHello(t, "video.example.org")
	body := []byte("original encrypted application bytes")
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	server, backend := net.Pipe()
	defer server.Close()
	defer backend.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	set := config.NewSetConfig()
	set.Enabled = true
	set.TCP.WSSizeReuse.Enabled = true
	l := &Listener{ctx: ctx, Warm: reuseTestDialer{dial: func(_ context.Context, _ *config.SetConfig, owner, host string, target netip.AddrPort) (net.Conn, error) {
		if owner != "192.0.2.10" || host != "video.example.org" || target.String() != "203.0.113.2:443" {
			return nil, errors.New("selector changed")
		}
		return backend, nil
	}}}
	l.set.Store(&set)
	done := make(chan struct{})
	go func() { defer close(done); l.serveWSReuse(reuseTestClient{b}, net.ParseIP("203.0.113.2"), 443, nil) }()
	received := make(chan []byte, 1)
	go func() {
		buf := make([]byte, len(hello)+len(body))
		_, err := io.ReadFull(server, buf)
		if err != nil {
			received <- nil
			return
		}
		received <- buf
		server.Write([]byte("reply"))
		server.Close()
	}()
	a.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := a.Write(hello[:11]); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write(hello[11:]); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write(body); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 5)
	if _, err := io.ReadFull(a, reply); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(<-received, append(hello, body...)) || string(reply) != "reply" {
		t.Fatal("client stream changed")
	}
	a.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay failed to clean up")
	}
}

func TestWSReuseWithoutSNIOrWithWarmFailureNeverForwards(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(map[bool]string{true: "missing SNI", false: "warm failure"}[missing], func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			set := config.NewSetConfig()
			set.Enabled = true
			set.TCP.WSSizeReuse.Enabled = true
			l := &Listener{ctx: ctx, Warm: reuseTestDialer{dial: func(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error) {
				calls++
				return nil, errors.New("warm failed")
			}}}
			l.set.Store(&set)
			result := sniffResult{prefix: []byte("must not reach server")}
			if !missing {
				result.host = "video.example.org"
				result.tlsVersion = 0x0304
			}
			l.serveWSReuse(reuseTestClient{b}, net.ParseIP("203.0.113.2"), 443, &result)
			if (calls == 0) != missing {
				t.Fatal("wrong warm decision")
			}
		})
	}
}

func TestWSReuseClientCloseCancelsPendingWarm(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	client, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	set := config.NewSetConfig()
	set.Enabled = true
	set.TCP.WSSizeReuse.Enabled = true
	l := &Listener{ctx: ctx, Warm: reuseTestDialer{dial: func(ctx context.Context, _ *config.SetConfig, _, _ string, _ netip.AddrPort) (net.Conn, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}}}
	l.set.Store(&set)
	done := make(chan struct{})
	go func() { defer close(done); l.serveWSReuse(server, net.ParseIP("203.0.113.2"), 443, nil) }()
	if _, err := client.Write(realClientHello(t, "video.example.org")); err != nil {
		t.Fatal(err)
	}
	<-entered
	client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closed client left a warm running")
	}
}

func TestWarmClientPollErrorsDoNotCancel(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	a, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	old := warmClientPoll
	defer func() { warmClientPoll = old }()
	var calls atomic.Int32
	var closed atomic.Bool
	polled := make(chan struct{})
	warmClientPoll = func(fds []unix.PollFd, _ int) (int, error) {
		n := calls.Add(1)
		if n == 1 {
			return 0, unix.EINTR
		}
		if n == 2 {
			fds[0].Revents = unix.POLLERR
			return 1, nil
		}
		if n == 3 {
			close(polled)
		}
		if closed.Load() {
			fds[0].Revents = unix.POLLRDHUP
			return 1, nil
		}
		return 0, nil
	}
	ctx, stop := watchWarmClient(context.Background(), b)
	defer stop()
	select {
	case <-polled:
	case <-time.After(time.Second):
		t.Fatal("transient poll error cancelled warm")
	}
	if ctx.Err() != nil {
		t.Fatal("live client cancelled")
	}
	closed.Store(true)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("closed client not cancelled")
	}
}

func TestWSReuseRelaySurvivesListenerStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend, server := net.Pipe()
	defer server.Close()
	set := config.NewSetConfig()
	set.Enabled = true
	set.TCP.WSSizeReuse.Enabled = true
	l := &Listener{ctx: ctx, Warm: reuseTestDialer{dial: func(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error) {
		return backend, nil
	}}}
	l.set.Store(&set)
	a, b := net.Pipe()
	defer a.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.serveWSReuse(reuseTestClient{b}, net.ParseIP("203.0.113.2"), 443, &sniffResult{host: "video.example.org", tlsVersion: 0x0304, prefix: []byte("hello")})
	}()
	buf := make([]byte, 5)
	server.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(server, buf); err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(100 * time.Millisecond)
	a.SetDeadline(time.Now().Add(time.Second))
	go server.Write([]byte("reply"))
	if _, err := io.ReadFull(a, buf); err != nil {
		t.Fatalf("the listener stop cut a relayed connection: %v", err)
	}
	if string(buf) != "reply" {
		t.Fatalf("client read %q, want the server's reply", buf)
	}
	a.Close()
	server.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the relay did not end with the client")
	}
}

func TestWSReuseListenerStopAbortsPendingWarm(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, cancelled := make(chan struct{}), make(chan struct{})
	set := config.NewSetConfig()
	set.Enabled = true
	set.TCP.WSSizeReuse.Enabled = true
	l := &Listener{ctx: ctx, Warm: reuseTestDialer{dial: func(ctx context.Context, _ *config.SetConfig, _, _ string, _ netip.AddrPort) (net.Conn, error) {
		close(entered)
		<-ctx.Done()
		<-cancelled
		return nil, ctx.Err()
	}}}
	l.set.Store(&set)
	a, b := net.Pipe()
	defer a.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.serveWSReuse(reuseTestClient{b}, net.ParseIP("203.0.113.2"), 443, &sniffResult{host: "video.example.org", tlsVersion: 0x0304, prefix: []byte("hello")})
	}()
	<-entered
	cancel()
	close(cancelled)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a listener stop left the warm running")
	}
	a.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := a.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the client was left open after the listener stop aborted its warm: %v", err)
	}
}

type slotTestDialer struct {
	slots    chan struct{}
	err      error
	acquired atomic.Int32
	released atomic.Int32
	dials    atomic.Int32
	mu       sync.Mutex
	servers  []net.Conn
}

func (d *slotTestDialer) Dial(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error) {
	d.dials.Add(1)
	if d.err != nil {
		return nil, d.err
	}
	c, s := net.Pipe()
	d.mu.Lock()
	d.servers = append(d.servers, s)
	d.mu.Unlock()
	go func() { _, _ = io.Copy(io.Discard, s) }()
	return c, nil
}

func (d *slotTestDialer) AcquireClient() (func(), bool) {
	select {
	case d.slots <- struct{}{}:
		d.acquired.Add(1)
		var once sync.Once
		return func() { once.Do(func() { <-d.slots; d.released.Add(1) }) }, true
	default:
		return nil, false
	}
}

func (d *slotTestDialer) closeServers() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, s := range d.servers {
		_ = s.Close()
	}
}

type addressedConn struct {
	net.Conn
	local, remote net.Addr
}

func (c addressedConn) LocalAddr() net.Addr  { return c.local }
func (c addressedConn) RemoteAddr() net.Addr { return c.remote }

type queuedListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func (q *queuedListener) Accept() (net.Conn, error) {
	select {
	case c := <-q.conns:
		return c, nil
	case <-q.done:
		return nil, net.ErrClosed
	}
}

func (q *queuedListener) Close() error {
	q.once.Do(func() { close(q.done) })
	return nil
}

func (q *queuedListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }

type reuseAcceptRig struct {
	l       *Listener
	ln      *queuedListener
	hello   []byte
	clients []net.Conn
}

func startReuseAcceptRig(t *testing.T, warm *slotTestDialer) *reuseAcceptRig {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	set := config.NewSetConfig()
	set.Enabled = true
	set.TCP.WSSizeReuse.Enabled = true
	l := &Listener{ctx: ctx, cancel: cancel, WSReuse: true, Warm: warm, SetName: "reuse"}
	l.set.Store(&set)
	r := &reuseAcceptRig{l: l, ln: &queuedListener{conns: make(chan net.Conn), done: make(chan struct{})}, hello: realClientHello(t, "video.example.org")}
	loopDone := make(chan struct{})
	go func() { defer close(loopDone); l.acceptLoop(r.ln, "v4") }()
	t.Cleanup(func() {
		_ = r.ln.Close()
		<-loopDone
		for _, c := range r.clients {
			_ = c.Close()
		}
		warm.closeServers()
		cancel()
		if !waitUntil(2*time.Second, func() bool { return l.Active() == 0 }) {
			t.Errorf("%d reuse connections still served after the clients and servers closed", l.Active())
		}
	})
	return r
}

func (r *reuseAcceptRig) connect(port int) net.Conn {
	a, b := net.Pipe()
	r.clients = append(r.clients, a)
	r.ln.conns <- addressedConn{Conn: b, local: &net.TCPAddr{IP: net.ParseIP("203.0.113.2"), Port: 443}, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: port}}
	go func() { _, _ = a.Write(r.hello) }()
	return a
}

func waitUntil(limit time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return true
}

func TestWSReuseLongRelayDoesNotHoldAWarmSlot(t *testing.T) {
	d := &slotTestDialer{slots: make(chan struct{}, 1)}
	r := startReuseAcceptRig(t, d)
	r.connect(50001)
	if !waitUntil(2*time.Second, func() bool { return d.dials.Load() == 1 && d.released.Load() >= 1 }) {
		t.Fatalf("the first connection's relay still holds the only warm slot after its warm: dials=%d acquired=%d released=%d", d.dials.Load(), d.acquired.Load(), d.released.Load())
	}
	r.connect(50002)
	if !waitUntil(2*time.Second, func() bool { return d.dials.Load() == 2 && d.released.Load() == 2 }) {
		t.Fatalf("a second connection was not warmed while the first one relays: dials=%d acquired=%d released=%d", d.dials.Load(), d.acquired.Load(), d.released.Load())
	}
	if got := r.l.Active(); got != 2 {
		t.Fatalf("%d connections relayed, want both", got)
	}
}

type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLog) warnings(substr string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, line := range strings.Split(b.buf.String(), "\n") {
		if strings.Contains(line, "[WARN]") && strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

func captureWarnings(t *testing.T) *lockedLog {
	t.Helper()
	previous := log.Level(log.CurLevel.Load())
	out := &lockedLog{}
	log.Init(out, log.LevelInfo, true)
	t.Cleanup(func() { log.Init(os.Stderr, previous, true) })
	return out
}

func TestWSReuseWarmSlotsFullClosesAndCounts(t *testing.T) {
	logged := captureWarnings(t)
	d := &slotTestDialer{slots: make(chan struct{})}
	r := startReuseAcceptRig(t, d)
	for i := 0; i < 2; i++ {
		client := r.connect(50001 + i)
		client.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := client.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("connection %d was left open with every warm slot busy: %v", i+1, err)
		}
	}
	if d.dials.Load() != 0 {
		t.Fatalf("%d warms dialed without a warm slot", d.dials.Load())
	}
	if got := r.l.warmSlotsFull.count.Load(); got != 2 {
		t.Fatalf("%d connections counted as refused for busy warm slots, want 2", got)
	}
	if n := logged.warnings("every warm slot is busy"); n != 1 {
		t.Fatalf("%d warnings for 2 connections refused within a minute, want 1", n)
	}
}

func serveReuseOnce(l *Listener) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	l.serveWSReuse(reuseTestClient{b}, net.ParseIP("203.0.113.2"), 443, &sniffResult{host: "video.example.org", tlsVersion: 0x0304, prefix: []byte("hello")})
}

func TestWSReuseLeasedPortsWarnOncePerMinute(t *testing.T) {
	logged := captureWarnings(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	set := config.NewSetConfig()
	set.Enabled = true
	set.TCP.WSSizeReuse.Enabled = true
	d := &slotTestDialer{slots: make(chan struct{}, 1), err: warmtuple.ErrCapacity}
	l := &Listener{ctx: ctx, Warm: d, SetName: "reuse"}
	l.set.Store(&set)
	for i := 0; i < 3; i++ {
		serveReuseOnce(l)
	}
	if d.dials.Load() != 3 {
		t.Fatalf("%d warms tried, want 3", d.dials.Load())
	}
	if n := logged.warnings(warmtuple.ErrCapacity.Error()); n != 1 {
		t.Fatalf("%d warnings for 3 connections refused within a minute because every warm port is leased, want 1", n)
	}
	d.err = errors.New("tls: handshake failure")
	serveReuseOnce(l)
	serveReuseOnce(l)
	if n := logged.warnings("handshake failure"); n != 2 {
		t.Fatalf("%d warnings for 2 failed warms, want 2: only the leased-port refusal is rate-limited", n)
	}
}
