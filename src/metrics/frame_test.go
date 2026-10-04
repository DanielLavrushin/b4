package metrics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTickIncludesListsOnlyWhenTheirRevMoved(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	var sent SentRevs

	fr := m.Tick(&sent)
	if fr.Type != FrameTick || fr.Blocked != nil || fr.Escalations != nil || fr.Events != nil {
		t.Fatalf("nothing changed since rev 0: blocked=%v escalations=%v events=%v", fr.Blocked, fr.Escalations, fr.Events)
	}

	m.Event(LevelInfo, EventSettingsApplied, map[string]string{"sets": "2"}, "Settings applied")
	fr = m.Tick(&sent)
	if fr.Events == nil || fr.Blocked != nil || fr.Escalations != nil {
		t.Fatalf("only the events moved: %+v", fr)
	}
	if sent.Events != fr.Events.Rev || sent.Events == 0 {
		t.Fatalf("sent must record the rev that went out: %+v", sent)
	}
	if fr = m.Tick(&sent); fr.Events != nil {
		t.Fatal("a rev already sent is not repeated")
	}

	m.RecordBlockedDNS("ads", "ads.example", "")
	m.UpdateEscalations([]EscalationEntry{{Host: "a.example", SetAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}})
	fr = m.Tick(&sent)
	if fr.Blocked == nil || fr.Escalations == nil || fr.Events != nil {
		t.Fatalf("blocked and escalations moved: %+v", fr)
	}
	if fr = m.Tick(&sent); fr.Blocked != nil || fr.Escalations != nil {
		t.Fatal("nothing moved since the last tick")
	}

	hello := m.Hello()
	if hello.Type != FrameHello || hello.Blocked == nil || hello.Escalations == nil || hello.Events == nil {
		t.Fatalf("hello always carries every list: %+v", hello)
	}
	other := SentRevs{}
	if fr = m.Tick(&other); fr.Blocked == nil || fr.Events == nil || fr.Escalations == nil {
		t.Fatal("a fresh SentRevs gets every list that ever changed")
	}
}

func TestResetCountersTouchesCountersOnly(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)
	m := r.m
	withSetsProvider(t, []SetMeta{{ID: "ads", Kind: SetKindBlock}, {ID: "video", Kind: SetKindBypass}}, 0)

	m.ObserveFlow(flowKey(1), "video")
	m.ObserveFlow(flowKey(2), "")
	m.RecordBlockedFlow(flowKey(3), "ads", "ads.example", "aa:bb:cc:dd:ee:ff")
	m.RecordBlockedDNS("ads", "ads.example", "aa:bb:cc:dd:ee:ff")
	m.RecordRSTDrop()
	m.RecordEscalation()
	m.UpdateEscalations([]EscalationEntry{{Host: "a.example", SetAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}})
	m.Event(LevelError, EventSOCKS5Failed, nil, "socks5")
	r.step(2 * time.Minute)
	r.step(5 * time.Second)
	m.ObserveFlow(flowKey(50), "")
	before := m.Hello()

	m.ResetCounters()
	after := m.Hello()

	if after.Totals != (Totals{}) {
		t.Fatalf("every total is zero after a reset: %+v", after.Totals)
	}
	if after.StatsSince != ms(time.Date(2026, 10, 3, 12, 2, 5, 0, time.UTC)) || before.StatsSince != ms(time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("stats_since %d -> %d", before.StatsSince, after.StatsSince)
	}
	if len(after.Blocked.Domains) != 0 || len(after.Blocked.Devices) != 0 || after.Blocked.Rev <= before.Blocked.Rev {
		t.Fatalf("blocked lists cleared with a new rev: %+v (was rev %d)", after.Blocked, before.Blocked.Rev)
	}
	if setActivity(t, after, "ads").DNSBlocked != 0 {
		t.Fatal("per-set DNS blocks count since stats_since")
	}

	if after.UptimeS != 125 {
		t.Fatalf("uptime is untouched, got %d", after.UptimeS)
	}
	if after.Events.Rev != before.Events.Rev || len(after.Events.Items) != 1 || len(after.Events.Errors) != 1 {
		t.Fatalf("events are untouched: %+v", after.Events)
	}
	if after.Escalations.Rev != before.Escalations.Rev || len(after.Escalations.Items) != 1 {
		t.Fatalf("live escalations are untouched: %+v", after.Escalations)
	}
	for i := range before.Activity.Minute {
		if before.Activity.Minute[i] != after.Activity.Minute[i] {
			t.Fatalf("activity history is untouched: %+v -> %+v", before.Activity.Minute, after.Activity.Minute)
		}
	}
	if v := setActivity(t, after, "video"); v.Conns60m != 1 || v.LastMatch == 0 {
		t.Fatalf("per-set activity is history, not a counter: %+v", v)
	}

	m.ObserveFlow(flowKey(50), "")
	if got := m.Totals().Conns; got != 0 {
		t.Fatalf("a reset does not forget live flows, they are not counted again: %d", got)
	}
}

func TestHelloJSONHasNoNullArrays(t *testing.T) {
	r := newRig(t, rigStart, 0)
	data, err := json.Marshal(r.m.Hello())
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "null") {
		t.Fatalf("hello must not carry null: %s", s)
	}
	for _, want := range []string{`"type":"hello"`, `"upstreams":[]`, `"sets":[]`, `"domains":[]`, `"devices":[]`, `"items":[]`, `"errors":[]`, `"rss_history":[{`, `"minute":[{`, `"ten_minute":[{`, `"state":"starting"`, `"thread_warn":2000`, `"thread_limit":4000`, `"window_s":600`} {
		if !strings.Contains(s, want) {
			t.Errorf("hello JSON lacks %s: %s", want, s)
		}
	}
	for _, absent := range []string{`"conntrack"`, `"mtproto"`, `"engine_failure"`, `"open"`} {
		if strings.Contains(s, absent) {
			t.Errorf("hello JSON must omit %s when unset: %s", absent, s)
		}
	}

	var sent SentRevs
	tick, _ := json.Marshal(r.m.Tick(&sent))
	for _, absent := range []string{`"blocked":{`, `"escalations":{`, `"events":{`} {
		if strings.Contains(string(tick), absent) {
			t.Errorf("an unchanged tick omits %s: %s", absent, tick)
		}
	}
}

func TestEngineFailureForcesFailedState(t *testing.T) {
	r := newRig(t, rigStart, 0)
	r.m.SetEngine(EngineInfo{State: EngineRunning, Mode: EngineModeTUN, Threads: 4, Firewall: "nftables"})
	if e := r.m.Hello().Engine; e != (EngineInfo{State: EngineRunning, Mode: EngineModeTUN, Threads: 4, Firewall: "nftables"}) {
		t.Fatalf("engine %+v", e)
	}
	r.m.SetEngineFailure(&EngineFailure{Mode: "tun", Error: "no route"})
	fr := r.m.Hello()
	if fr.Engine.State != EngineFailed || fr.EngineFailure == nil || r.m.Engine().State != EngineFailed {
		t.Fatalf("a set engine_failure forces failed: %+v %+v", fr.Engine, fr.EngineFailure)
	}
	r.m.SetEngineFailure(nil)
	r.m.SetEngineState(EngineStopping)
	if e := r.m.Engine(); e.State != EngineStopping || e.Mode != EngineModeTUN {
		t.Fatalf("SetEngineState changes the state only: %+v", e)
	}
}

func TestProvidersFeedTheFrame(t *testing.T) {
	SetRulesProvider(func() RulesInfo {
		return RulesInfo{Monitored: true, IntervalS: 30, LastCheck: 1, Restores: 2, LastRestore: 3}
	})
	SetUpstreamProvider(func() []UpstreamAttention {
		return []UpstreamAttention{{SetID: "ok", Failures: 0}, {SetID: "down", Upstream: "10.0.0.1:1080", Failures: 4, LastError: "refused"}}
	})
	SetMTProtoStatsProvider(func() *MTProtoStats { return &MTProtoStats{Enabled: true, Port: 443} })
	t.Cleanup(func() {
		SetRulesProvider(nil)
		SetUpstreamProvider(nil)
		SetMTProtoStatsProvider(nil)
	})
	r := newRig(t, rigStart, 0)
	fr := r.m.Hello()
	if fr.Rules != (RulesInfo{Monitored: true, IntervalS: 30, LastCheck: 1, Restores: 2, LastRestore: 3}) {
		t.Fatalf("rules %+v", fr.Rules)
	}
	if len(fr.Attention.Upstreams) != 1 || fr.Attention.Upstreams[0].SetID != "down" {
		t.Fatalf("only failing upstreams are attention items: %+v", fr.Attention.Upstreams)
	}
	if fr.MTProto == nil || fr.MTProto.Port != 443 {
		t.Fatalf("mtproto %+v", fr.MTProto)
	}
}

func TestAccessorsMatchTheFrame(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)
	m := r.m
	m.ObserveFlow(flowKey(1), "a")
	m.ObserveFlow(flowKey(2), "")
	m.ObserveFlow(flowKey(3), "")
	if in, not := m.LastMinute(); in != 0 || not != 0 {
		t.Fatal("no minute has closed yet")
	}
	r.step(time.Minute)
	m.ObserveFlow(flowKey(4), "")
	if in, not := m.LastMinute(); in != 1 || not != 2 {
		t.Fatalf("last closed minute %d/%d", in, not)
	}
	if tot := m.Totals(); tot.Conns != 4 || tot.ConnsInSets != 1 {
		t.Fatalf("totals %+v", tot)
	}
	if m.UptimeSeconds() != 60 || m.UptimeString() != "1m 0s" {
		t.Fatalf("uptime %d %q", m.UptimeSeconds(), m.UptimeString())
	}
	if m.StatsSince() != ms(time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("stats_since %d", m.StatsSince())
	}
	if p := m.Process(); p.RSSBytes != 30<<20 || p.OSThreads != 12 {
		t.Fatalf("process %+v", p)
	}
}

func TestTickListenerRunsAfterEveryTick(t *testing.T) {
	r := newRig(t, rigStart, 0)
	calls := 0
	var seen int64
	r.m.SetTickListener(func() {
		calls++
		seen = r.m.Hello().Now
	})
	r.step(time.Second)
	r.step(time.Second)
	if calls != 2 || seen != ms(rigStart.Add(2*time.Second)) {
		t.Fatalf("listener calls %d, saw now %d", calls, seen)
	}
	r.m.SetTickListener(nil)
	r.step(time.Second)
	if calls != 2 {
		t.Fatal("a cleared listener is not called")
	}
}

func TestWallMillisRelabelsMonotonicTimes(t *testing.T) {
	r := newRig(t, rigStart, 0)
	at := procStart.Add(1500 * time.Millisecond)
	if got := r.m.WallMillis(at); got != ms(rigStart)+1500 {
		t.Fatalf("WallMillis %d, want %d", got, ms(rigStart)+1500)
	}
	plain := time.UnixMilli(1700000000000)
	if got := r.m.WallMillis(plain); got != 1700000000000 {
		t.Fatalf("a time without a monotonic reading passes through, got %d", got)
	}
	if r.m.WallMillis(time.Time{}) != 0 {
		t.Fatal("zero time is 0")
	}
}
