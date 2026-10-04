package handler

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/nfq"
)

func newMetricsTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	cfg := config.NewConfig()
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterMetricsApi()
	return mux
}

func serveMetrics(mux *http.ServeMux, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestMetricsEndpointsRejectOtherMethods(t *testing.T) {
	mux := newMetricsTestMux(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/metrics"},
		{http.MethodGet, "/api/metrics/reset"},
		{http.MethodGet, "/api/escalations/clear"},
		{http.MethodPost, "/api/metrics/summary"},
	} {
		if rec := serveMetrics(mux, c.method, c.path); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: got %d, want 405", c.method, c.path, rec.Code)
		}
	}
}

func TestGetMetricsReturnsAHelloFrame(t *testing.T) {
	mux := newMetricsTestMux(t)
	rec := serveMetrics(mux, http.MethodGet, "/api/metrics")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	var fr metrics.Frame
	if err := json.NewDecoder(rec.Body).Decode(&fr); err != nil {
		t.Fatal(err)
	}
	if fr.Type != metrics.FrameHello || fr.Now == 0 || len(fr.Activity.Minute) == 0 || len(fr.RSSHistory) == 0 {
		t.Fatalf("not a hello frame: %+v", fr)
	}
	if fr.Blocked == nil || fr.Escalations == nil || fr.Events == nil {
		t.Fatal("the hello frame carries every list")
	}
	if fr.Process.ThreadLimit == 0 || fr.Process.CPUCores == 0 {
		t.Fatalf("process info %+v", fr.Process)
	}
}

func TestResetMetricsResetsCountersOnly(t *testing.T) {
	mux := newMetricsTestMux(t)
	mc := GetMetricsCollector()
	mc.RecordRSTDrop()
	mc.RecordBlockedDNS("ads", "ads.example", "")
	mc.UpdateEscalations([]metrics.EscalationEntry{{Host: "kept.example", ToSet: "Fallback", SetAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}})
	t.Cleanup(func() { mc.UpdateEscalations([]metrics.EscalationEntry{}) })
	mc.Event(metrics.LevelError, metrics.EventSOCKS5Failed, map[string]string{"error": "boom"}, "Failed to start SOCKS5 server: boom")
	before := mc.Hello()

	time.Sleep(2 * time.Millisecond)
	rec := serveMetrics(mux, http.MethodPost, "/api/metrics/reset")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	var resp MetricsResetResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.StatsSince <= before.StatsSince {
		t.Fatalf("response %+v, stats_since was %d", resp, before.StatsSince)
	}

	after := mc.Hello()
	if after.Totals.RSTDropped != 0 || after.Totals.BlockedDNS != 0 || len(after.Blocked.Domains) != 0 {
		t.Fatalf("counters must be zero after reset: %+v", after.Totals)
	}
	if after.StatsSince != resp.StatsSince {
		t.Fatalf("stats_since %d, response said %d", after.StatsSince, resp.StatsSince)
	}
	if len(after.Escalations.Items) != 1 || after.Escalations.Items[0].Host != "kept.example" {
		t.Fatalf("reset must not clear live escalations: %+v", after.Escalations.Items)
	}
	if !slices.ContainsFunc(after.Events.Errors, func(e metrics.Event) bool { return e.Code == metrics.EventSOCKS5Failed }) {
		t.Fatal("reset must not clear events")
	}
	if after.UptimeS < before.UptimeS {
		t.Fatalf("reset must not restart the uptime: %d -> %d", before.UptimeS, after.UptimeS)
	}
}

func TestClearEscalationsEmptiesTheList(t *testing.T) {
	mux := newMetricsTestMux(t)
	mc := GetMetricsCollector()
	saved := globalPool
	t.Cleanup(func() { globalPool = saved })

	for _, pool := range []*nfq.Pool{nil, {}} {
		globalPool = pool
		mc.UpdateEscalations([]metrics.EscalationEntry{{Host: "a.example", ToSet: "Fallback", SetAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}})
		rev := mc.Hello().Escalations.Rev

		rec := serveMetrics(mux, http.MethodPost, "/api/escalations/clear")
		if rec.Code != http.StatusOK {
			t.Fatalf("pool %v: got %d", pool != nil, rec.Code)
		}
		var resp EscalationsClearResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if !resp.Success || resp.Cleared != 0 {
			t.Fatalf("pool %v: response %+v", pool != nil, resp)
		}
		list := mc.Hello().Escalations
		if len(list.Items) != 0 || list.Rev <= rev {
			t.Fatalf("pool %v: the collector's list must be empty with a new rev: %+v (rev was %d)", pool != nil, list, rev)
		}
	}
}

func TestMetricsSummaryUsesTheNewAccessors(t *testing.T) {
	mux := newMetricsTestMux(t)
	mc := GetMetricsCollector()
	mc.ResetCounters()
	mc.CountConnection("socks")
	mc.CountConnection("")
	mc.RecordBlockedDNS("ads", "ads.example", "")
	key := metrics.FlowKey{Proto: 6, DPort: 443, SPort: 50123}
	binary.BigEndian.PutUint64(key.Addr[:8], uint64(time.Now().UnixNano()))
	mc.RecordBlockedFlow(key, "ads", "ads.example", "")
	mc.RecordRSTDrop()

	rec := serveMetrics(mux, http.MethodGet, "/api/metrics/summary")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	want := []string{"connections", "connections_in_sets", "connections_last_minute", "rst_dropped", "blocked_total", "uptime", "uptime_s", "cpu_percent", "rss_bytes", "mem_total_bytes"}
	for _, k := range want {
		if _, ok := raw[k]; !ok {
			t.Errorf("summary lacks %q: %s", k, rec.Body.String())
		}
	}
	if len(raw) != len(want) {
		t.Errorf("summary has extra keys: %s", rec.Body.String())
	}

	var s MetricsSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Connections != 3 || s.ConnectionsInSets != 2 || s.BlockedTotal != 2 || s.RSTDropped != 1 {
		t.Fatalf("summary %+v", s)
	}
	if s.Uptime == "" || s.UptimeS != mc.UptimeSeconds() && s.UptimeS != mc.UptimeSeconds()-1 {
		t.Fatalf("uptime %q %d", s.Uptime, s.UptimeS)
	}
}
