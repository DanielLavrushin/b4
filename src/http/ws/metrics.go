package ws

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/gorilla/websocket"
)

const (
	metricsSendBuffer   = 4
	metricsWriteTimeout = 10 * time.Second
	metricsPingEvery    = 30 * time.Second
	metricsReadTimeout  = 60 * time.Second
	metricsReadLimit    = 4096
)

type frameSource interface {
	Hello() metrics.Frame
	Tick(sent *metrics.SentRevs) metrics.Frame
}

type metricsSub struct {
	conn *websocket.Conn
	ch   chan *websocket.PreparedMessage
}

type metricsHub struct {
	src     frameSource
	mu      sync.Mutex
	subs    map[*metricsSub]struct{}
	sent    metrics.SentRevs
	encodes atomic.Uint64
}

var (
	defaultMetricsHub     *metricsHub
	defaultMetricsHubOnce sync.Once
)

func newMetricsHub(src frameSource) *metricsHub {
	return &metricsHub{src: src, subs: make(map[*metricsSub]struct{})}
}

func getMetricsHub() *metricsHub {
	defaultMetricsHubOnce.Do(func() {
		mc := metrics.GetMetricsCollector()
		defaultMetricsHub = newMetricsHub(mc)
		mc.SetTickListener(defaultMetricsHub.broadcast)
	})
	return defaultMetricsHub
}

func HandleMetricsWebSocket(w http.ResponseWriter, r *http.Request) {
	getMetricsHub().serve(w, r)
}

func (h *metricsHub) broadcast() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) == 0 {
		return
	}
	data, err := json.Marshal(h.src.Tick(&h.sent))
	if err != nil {
		log.Errorf("Metrics WebSocket: could not encode a tick frame: %v", err)
		return
	}
	pm, err := websocket.NewPreparedMessage(websocket.TextMessage, data)
	if err != nil {
		log.Errorf("Metrics WebSocket: could not prepare a tick frame: %v", err)
		return
	}
	h.encodes.Add(1)
	for s := range h.subs {
		select {
		case s.ch <- pm:
		default:
			delete(h.subs, s)
			close(s.ch)
			s.conn.Close()
			log.Tracef("Metrics WebSocket client dropped: it fell %d frames behind", metricsSendBuffer)
		}
	}
}

func (h *metricsHub) register(s *metricsSub) metrics.Frame {
	h.mu.Lock()
	defer h.mu.Unlock()
	hello := h.src.Hello()
	if len(h.subs) == 0 {
		h.sent = helloRevs(hello)
	}
	h.subs[s] = struct{}{}
	return hello
}

func helloRevs(f metrics.Frame) metrics.SentRevs {
	var revs metrics.SentRevs
	if f.Blocked != nil {
		revs.Blocked = f.Blocked.Rev
	}
	if f.TopDomains != nil {
		revs.TopDomains = f.TopDomains.Rev
	}
	if f.TopAddresses != nil {
		revs.TopAddresses = f.TopAddresses.Rev
	}
	if f.Escalations != nil {
		revs.Escalations = f.Escalations.Rev
	}
	if f.Events != nil {
		revs.Events = f.Events.Rev
	}
	return revs
}

func (h *metricsHub) unregister(s *metricsSub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s]; ok {
		delete(h.subs, s)
		close(s.ch)
	}
}

func (h *metricsHub) subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

func (h *metricsHub) serve(w http.ResponseWriter, r *http.Request) {
	conn, err := Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("Failed to upgrade metrics WebSocket: %v", err)
		return
	}
	log.Tracef("Metrics WebSocket client connected from %s", r.RemoteAddr)

	s := &metricsSub{conn: conn, ch: make(chan *websocket.PreparedMessage, metricsSendBuffer)}
	hello := h.register(s)

	conn.SetReadLimit(metricsReadLimit)
	conn.SetReadDeadline(time.Now().Add(metricsReadTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(metricsReadTimeout))
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	defer func() {
		h.unregister(s)
		conn.Close()
		<-done
	}()

	data, err := json.Marshal(hello)
	if err != nil {
		log.Errorf("Metrics WebSocket: could not encode the hello frame: %v", err)
		return
	}
	conn.SetWriteDeadline(time.Now().Add(metricsWriteTimeout))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return
	}

	ping := time.NewTicker(metricsPingEvery)
	defer ping.Stop()
	for {
		select {
		case pm, ok := <-s.ch:
			if !ok {
				return
			}
			conn.SetWriteDeadline(time.Now().Add(metricsWriteTimeout))
			if err := conn.WritePreparedMessage(pm); err != nil {
				log.Tracef("Metrics WebSocket client disconnected: %v", err)
				return
			}
		case <-ping.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(metricsWriteTimeout)); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}
