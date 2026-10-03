package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/handler"
	"github.com/daniellavrushin/b4/http/ws"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/tables"
	"github.com/daniellavrushin/b4/tproxy"
	"github.com/gorilla/websocket"
)

func dashboardTestSet(id, name string, enabled bool) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Name = name
	set.Enabled = enabled
	return &set
}

func TestSetKindFollowsTheRoutingMode(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		mode    string
		want    string
	}{
		{"routing off", false, "", metrics.SetKindBypass},
		{"routing off keeps a block mode inert", false, config.RoutingModeBlock, metrics.SetKindBypass},
		{"block", true, config.RoutingModeBlock, metrics.SetKindBlock},
		{"upstream proxy", true, config.RoutingModeProxy, metrics.SetKindProxy},
		{"telegram over websocket", true, config.RoutingModeMTProtoWS, metrics.SetKindProxy},
		{"interface", true, config.RoutingModeInterface, metrics.SetKindRoute},
		{"default mode is an interface route", true, "", metrics.SetKindRoute},
	}
	for _, c := range cases {
		set := dashboardTestSet("s", "S", true)
		set.Routing.Enabled = c.enabled
		set.Routing.Mode = c.mode
		if got := setKind(set); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSetMetasListsEnabledSetsInConfigOrder(t *testing.T) {
	cfg := config.NewConfig()
	watched := dashboardTestSet("w", "Watched", true)
	watched.Discovery.Watchdog = true
	block := dashboardTestSet("b", "Blocked", true)
	block.Routing.Enabled = true
	block.Routing.Mode = config.RoutingModeBlock
	cfg.Sets = []*config.SetConfig{
		dashboardTestSet("off", "Off", false),
		watched,
		nil,
		block,
		dashboardTestSet("off2", "Off too", false),
	}

	metas, disabled := setMetas(&cfg)
	if disabled != 2 {
		t.Fatalf("two sets are disabled, got %d", disabled)
	}
	want := []metrics.SetMeta{
		{ID: "w", Name: "Watched", Kind: metrics.SetKindBypass, Watched: true},
		{ID: "b", Name: "Blocked", Kind: metrics.SetKindBlock},
	}
	if len(metas) != len(want) {
		t.Fatalf("got %+v, want %+v", metas, want)
	}
	for i := range want {
		if metas[i] != want[i] {
			t.Fatalf("set %d: got %+v, want %+v", i, metas[i], want[i])
		}
	}
	if metas, disabled := setMetas(nil); metas != nil || disabled != 0 {
		t.Fatalf("no config lists no sets, got %+v %d", metas, disabled)
	}
}

func TestUpstreamAttentionKeepsOnlyFailingUpstreams(t *testing.T) {
	mc := &metrics.MetricsCollector{}
	failedAt := time.Now()
	got := upstreamAttention(mc, []tproxy.UpstreamHealth{
		{SetID: "ok", SetName: "Fine", Upstream: "10.0.0.2:1080"},
		{SetID: "down", SetName: "Down", Upstream: "10.0.0.3:1080", FailOpen: true, ConsecutiveFailures: 4, LastError: "connection refused", LastFailure: failedAt},
	})
	if len(got) != 1 {
		t.Fatalf("only the failing upstream is attention, got %+v", got)
	}
	u := got[0]
	if u.SetID != "down" || u.SetName != "Down" || u.Upstream != "10.0.0.3:1080" || !u.FailOpen || u.Failures != 4 || u.LastError != "connection refused" {
		t.Fatalf("unexpected attention entry: %+v", u)
	}
	if d := u.LastFailure - failedAt.UnixMilli(); d < -1000 || d > 1000 {
		t.Fatalf("last_failure must be the failure time in wall ms, got %d for %d", u.LastFailure, failedAt.UnixMilli())
	}
}

func TestRulesInfoReportsTheMonitor(t *testing.T) {
	mc := &metrics.MetricsCollector{}
	checked := time.Now()
	info := rulesInfo(mc, tables.RulesStatus{Monitored: true, Interval: 30 * time.Second, LastCheck: checked, Restores: 3, LastRestore: checked.Add(-time.Minute)})
	if !info.Monitored || info.IntervalS != 30 || info.Restores != 3 || info.LastCheck == 0 || info.LastRestore >= info.LastCheck {
		t.Fatalf("unexpected rules info: %+v", info)
	}
	off := rulesInfo(mc, tables.RulesStatus{Interval: 30 * time.Second})
	if off.Monitored || off.IntervalS != 0 || off.LastCheck != 0 || off.LastRestore != 0 {
		t.Fatalf("a monitor that is off reports nothing, got %+v", off)
	}
}

func TestStartedEventCarriesVersionEngineAndThreads(t *testing.T) {
	mc := &metrics.MetricsCollector{}
	noteStarted(mc, "1.85.0", metrics.EngineInfo{Mode: metrics.EngineModeTUN, Threads: 4})
	items := mc.Hello().Events.Items
	if len(items) != 1 {
		t.Fatalf("one started event, got %+v", items)
	}
	ev := items[0]
	if ev.Code != metrics.EventStarted || ev.Level != metrics.LevelInfo || ev.Args["version"] != "1.85.0" || ev.Args["engine"] != "tun" || ev.Args["threads"] != "4" {
		t.Fatalf("unexpected started event: %+v", ev)
	}
	if !strings.Contains(ev.Message, "TUN") || !strings.Contains(ev.Message, "4 threads") {
		t.Fatalf("the fallback text names the engine and threads: %q", ev.Message)
	}
}

var dashboardFlowSeq atomic.Uint32

func dashboardFlow(n byte, proto uint8, seq uint32) metrics.FlowKey {
	var k metrics.FlowKey
	copy(k.Addr[0:4], []byte{192, 168, 1, n})
	copy(k.Addr[16:20], []byte{203, 0, 113, n})
	k.SPort = uint16(10000 + seq%50000)
	k.DPort = 443
	k.Proto = proto
	return k
}

type dashboardIDs struct {
	bypass, block, proxy string
}

func checkDashboardFrame(t *testing.T, label string, ids dashboardIDs, fr metrics.Frame) {
	t.Helper()
	if fr.Type != metrics.FrameHello {
		t.Fatalf("%s: want a hello frame, got %q", label, fr.Type)
	}
	if fr.Engine.State != metrics.EngineRunning || fr.Engine.Mode != metrics.EngineModeNFQueue || fr.Engine.Threads != 2 || fr.Engine.Firewall != "nftables" {
		t.Fatalf("%s: engine %+v", label, fr.Engine)
	}
	if !fr.Rules.Monitored || fr.Rules.IntervalS != 10 || fr.Rules.Restores != 2 || fr.Rules.LastCheck == 0 || fr.Rules.LastRestore == 0 {
		t.Fatalf("%s: rules %+v", label, fr.Rules)
	}
	if fr.SetsDisabled != 1 || len(fr.Sets) != 3 {
		t.Fatalf("%s: sets %+v disabled %d", label, fr.Sets, fr.SetsDisabled)
	}
	want := []struct {
		id, kind string
		watched  bool
	}{
		{ids.bypass, metrics.SetKindBypass, true},
		{ids.block, metrics.SetKindBlock, false},
		{ids.proxy, metrics.SetKindProxy, false},
	}
	for i, w := range want {
		s := fr.Sets[i]
		if s.ID != w.id || s.Kind != w.kind || s.Watched != w.watched {
			t.Fatalf("%s: set %d is %+v, want %+v", label, i, s, w)
		}
	}
	if s := fr.Sets[0]; s.Conns60m != 1 || s.LastMatch == 0 || s.Open != nil {
		t.Fatalf("%s: the bypass set saw one flow: %+v", label, s)
	}
	if s := fr.Sets[1]; s.Conns60m != 1 || s.DNSBlocked != 1 {
		t.Fatalf("%s: the block set saw one blocked flow and one blocked lookup: %+v", label, s)
	}
	if s := fr.Sets[2]; s.Open == nil || *s.Open != 3 {
		t.Fatalf("%s: the proxy set has 3 open connections: %+v", label, s)
	}
	inSets, notInSet := uint64(0), uint64(0)
	for _, b := range fr.Activity.Minute {
		inSets += b.InSets
		notInSet += b.NotInSet
	}
	if len(fr.Activity.Minute) == 0 || len(fr.Activity.TenMinute) == 0 || inSets < 2 || notInSet < 1 {
		t.Fatalf("%s: activity %+v", label, fr.Activity)
	}
	if fr.Totals.Conns != 3 || fr.Totals.ConnsInSets != 2 || fr.Totals.BlockedConns != 1 || fr.Totals.BlockedDNS != 1 {
		t.Fatalf("%s: totals %+v", label, fr.Totals)
	}
	if fr.Blocked == nil || len(fr.Blocked.Domains) != 2 || len(fr.Blocked.Devices) != 1 || fr.Blocked.Devices[0].Key != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("%s: blocked %+v", label, fr.Blocked)
	}
	if len(fr.Attention.Upstreams) != 1 || fr.Attention.Upstreams[0].SetID != ids.proxy || fr.Attention.Upstreams[0].LastFailure == 0 {
		t.Fatalf("%s: upstreams %+v", label, fr.Attention.Upstreams)
	}
	if fr.Attention.Overload.WindowS != metrics.OverloadWindowSeconds {
		t.Fatalf("%s: overload %+v", label, fr.Attention.Overload)
	}
	if fr.LastPacketAt == 0 {
		t.Fatalf("%s: a packet was processed, last_packet_at must be set", label)
	}
}

func TestDashboardFramesCarryTheWiredState(t *testing.T) {
	seq := dashboardFlowSeq.Add(1)
	ids := dashboardIDs{
		bypass: fmt.Sprintf("dash-bypass-%d", seq),
		block:  fmt.Sprintf("dash-block-%d", seq),
		proxy:  fmt.Sprintf("dash-proxy-%d", seq),
	}
	cfg := config.NewConfig()
	bypass := dashboardTestSet(ids.bypass, "Video", true)
	bypass.Discovery.Watchdog = true
	block := dashboardTestSet(ids.block, "Ads", true)
	block.Routing.Enabled = true
	block.Routing.Mode = config.RoutingModeBlock
	proxy := dashboardTestSet(ids.proxy, "Work", true)
	proxy.Routing.Enabled = true
	proxy.Routing.Mode = config.RoutingModeProxy
	cfg.Sets = []*config.SetConfig{bypass, block, proxy, dashboardTestSet(fmt.Sprintf("dash-off-%d", seq), "Off", false)}
	var cfgPtr atomic.Pointer[config.Config]
	cfgPtr.Store(&cfg)

	var packets atomic.Uint64
	checked := time.Now()
	mc := metrics.GetMetricsCollector()
	wireDashboard(mc, dashboardSources{
		config:  cfgPtr.Load,
		packets: packets.Load,
		upstreams: func() []tproxy.UpstreamHealth {
			return []tproxy.UpstreamHealth{{SetID: ids.proxy, SetName: "Work", Upstream: "10.0.0.9:1080", ConsecutiveFailures: 2, LastError: "refused", LastFailure: checked}}
		},
		open: func() map[string]int64 { return map[string]int64{ids.proxy: 3} },
		rules: func() tables.RulesStatus {
			return tables.RulesStatus{Monitored: true, Interval: 10 * time.Second, LastCheck: checked, Restores: 2, LastRestore: checked}
		},
		replaced: func() bool { return false },
		overload: func() (uint64, uint64, uint64) { return 0, 0, 0 },
	})
	t.Cleanup(func() {
		wireDashboard(mc, dashboardSources{})
		mc.ResetCounters()
	})
	mc.ResetCounters()
	mc.SetEngine(metrics.EngineInfo{State: metrics.EngineRunning, Mode: metrics.EngineModeNFQueue, Threads: 2, Firewall: "nftables"})

	mc.ObserveFlow(dashboardFlow(1, 6, seq), "")
	mc.ObserveFlow(dashboardFlow(1, 6, seq), ids.bypass)
	mc.ObserveFlow(dashboardFlow(2, 17, seq), "")
	mc.ObserveFlow(dashboardFlow(3, 6, seq), ids.block)
	mc.RecordBlockedFlow(dashboardFlow(3, 6, seq), ids.block, "ads.example", "aa:bb:cc:dd:ee:ff")
	mc.RecordBlockedDNS(ids.block, "tracker.example", "")
	packets.Add(5)

	deadline := time.Now().Add(5 * time.Second)
	for mc.Hello().LastPacketAt == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the collector never noticed the processed packets")
		}
		time.Sleep(50 * time.Millisecond)
	}

	api := handler.NewAPIHandler(&cfgPtr)
	mux := http.NewServeMux()
	api.RegisterEndpoints(mux, &cfgPtr)
	mux.HandleFunc("/api/ws/metrics", ws.HandleMetricsWebSocket)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/metrics")
	if err != nil {
		t.Fatal(err)
	}
	var rest metrics.Frame
	err = json.NewDecoder(resp.Body).Decode(&rest)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	checkDashboardFrame(t, "GET /api/metrics", ids, rest)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/api/ws/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var hello metrics.Frame
	if err := json.Unmarshal(data, &hello); err != nil {
		t.Fatal(err)
	}
	checkDashboardFrame(t, "/api/ws/metrics hello", ids, hello)

	_, data, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("no tick after the hello: %v", err)
	}
	var tick metrics.Frame
	if err := json.Unmarshal(data, &tick); err != nil {
		t.Fatal(err)
	}
	if tick.Type != metrics.FrameTick || len(tick.Sets) != 3 || tick.Engine.State != metrics.EngineRunning || !tick.Rules.Monitored || len(tick.Activity.Minute) == 0 {
		t.Fatalf("the tick after the hello carries the same live state: %+v", tick)
	}
}
