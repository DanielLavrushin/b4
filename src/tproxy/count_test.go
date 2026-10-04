package tproxy

import (
	"errors"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
)

func TestListenerCountsEachAcceptedConnectionForItsSet(t *testing.T) {
	mc := metrics.GetMetricsCollector()
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.SetID = "tproxy-count"
	l.UseDomain = false

	before := mc.Totals()
	proxyThrough(t, l, nil)
	up.next(t)
	proxyThrough(t, l, nil)
	up.next(t)
	after := mc.Totals()
	if conns, inSets := after.Conns-before.Conns, after.ConnsInSets-before.ConnsInSets; conns != 2 || inSets != 2 {
		t.Fatalf("two accepted connections of a proxy set count twice, in the set: conns %d, in sets %d", conns, inSets)
	}
}

func TestTelegramBridgeConnectionsAreNotCountedAsASet(t *testing.T) {
	mc := metrics.GetMetricsCollector()
	before := mc.Totals()
	(&Listener{SetID: config.TelegramBridgeSetID, MTProtoWS: true}).countConnection()
	if after := mc.Totals(); after.Conns != before.Conns {
		t.Fatalf("the Telegram bridge is not a set and keeps its own counters, conns moved by %d", after.Conns-before.Conns)
	}
	(&Listener{SetID: "user-mtproto-ws", MTProtoWS: true}).countConnection()
	if after := mc.Totals(); after.Conns != before.Conns+1 || after.ConnsInSets != before.ConnsInSets+1 {
		t.Fatalf("a user set in Telegram WebSocket mode is counted like any proxy set: %+v -> %+v", before, after)
	}
}

func TestManagerReportsOpenConnectionsAndHealthWithoutItsLock(t *testing.T) {
	m := NewManager(nil)
	defer m.Stop()
	work := &Listener{SetID: "work", SetName: "Work"}
	work.Upstream.Host, work.Upstream.Port = "10.0.0.9", 1080
	bridge := &Listener{SetID: config.TelegramBridgeSetID, MTProtoWS: true}
	work.activeConns.Add(3)
	bridge.activeConns.Add(1)

	m.mu.Lock()
	m.listeners["work"] = work
	m.listeners[config.TelegramBridgeSetID] = bridge
	m.publishLocked()

	open := m.OpenConnections()
	health := m.UpstreamHealth()
	m.mu.Unlock()

	if open["work"] != 3 || open[config.TelegramBridgeSetID] != 1 || len(open) != 2 {
		t.Fatalf("open connections per set: %+v", open)
	}
	if len(health) != 1 || health[0].SetID != "work" || health[0].Upstream != "10.0.0.9:1080" {
		t.Fatalf("upstream health lists proxy sets only: %+v", health)
	}

	m.Stop()
	if open := m.OpenConnections(); len(open) != 0 {
		t.Fatalf("a stopped manager has no listeners left: %+v", open)
	}
}

func TestUpstreamFailureTimeKeepsItsMonotonicReading(t *testing.T) {
	l := &Listener{SetID: "work", SetName: "Work"}
	l.noteUpstreamFailure("example.com", 443, errors.New("connection refused"))
	h := l.Health()
	if h.ConsecutiveFailures != 1 || h.LastError != "connection refused" || h.LastFailure.IsZero() {
		t.Fatalf("unexpected health: %+v", h)
	}
	if h.LastFailure == h.LastFailure.Round(0) {
		t.Fatal("the failure time must keep its monotonic reading so the dashboard can relabel it after a clock step")
	}
}
