package metrics

import (
	"testing"
	"time"
)

func TestObserveFlowCountsAFlowOnceAcrossGenerations(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	busy, idle := flowKey(1), flowKey(2)

	m.ObserveFlow(busy, "")
	m.ObserveFlow(busy, "")
	m.ObserveFlow(idle, "")
	if got := m.Totals().Conns; got != 2 {
		t.Fatalf("two flows seen with three packets must count 2, got %d", got)
	}

	for i := 0; i < 5; i++ {
		r.step(61 * time.Second)
		m.ObserveFlow(busy, "")
	}
	if got := m.Totals().Conns; got != 2 {
		t.Fatalf("a flow that keeps sending is found in the old generation and carried forward, got %d", got)
	}

	m.ObserveFlow(idle, "")
	if got := m.Totals().Conns; got != 3 {
		t.Fatalf("a flow idle for two rotations is new again, got %d", got)
	}
}

func TestObserveFlowFoundInTheOldGenerationOnlyIsStillKnown(t *testing.T) {
	r := newRig(t, rigStart, 0)
	k := flowKey(7)
	r.m.ObserveFlow(k, "")
	r.step(61 * time.Second)
	r.m.ObserveFlow(k, "")
	r.m.ObserveFlow(k, "")
	if got := r.m.Totals().Conns; got != 1 {
		t.Fatalf("one rotation later the flow must still be known, got %d", got)
	}
}

func tableSizes(m *MetricsCollector) (cur, old int) {
	m.fl.mu.Lock()
	defer m.fl.mu.Unlock()
	return len(m.fl.cur), len(m.fl.old)
}

func TestAFullTableRotatesEarlyAndKeepsCountingEachFlowOnce(t *testing.T) {
	r := newRig(t, rigStart, 4)
	m := r.m
	for i := 0; i < 4; i++ {
		m.ObserveFlow(flowKey(i), "")
	}
	m.ObserveFlow(flowKey(4), "")
	if cur, old := tableSizes(m); cur != 1 || old != 4 {
		t.Fatalf("a new flow on a full table starts a new generation: cur %d old %d, want 1 and 4", cur, old)
	}

	for pass := 0; pass < 3; pass++ {
		for i := 0; i < 5; i++ {
			m.ObserveFlow(flowKey(i), "")
		}
	}
	if got := m.Totals().Conns; got != 5 {
		t.Fatalf("five flows that keep sending are five connections however often the table turns over, got %d", got)
	}
	if cur, old := tableSizes(m); cur > 4 || old > 4 {
		t.Fatalf("a generation never holds more than the cap: cur %d old %d", cur, old)
	}

	for i := 10; i < 20; i++ {
		m.ObserveFlow(flowKey(i), "")
	}
	m.ObserveFlow(flowKey(0), "")
	if got := m.Totals().Conns; got != 16 {
		t.Fatalf("a flow silent for a whole generation is forgotten and counts again, want 16, got %d", got)
	}
}

func TestAFloodBeyondTheCapCountsEveryFlowOnce(t *testing.T) {
	const capacity, flows, revisitGap = 256, 10000, 100
	r := newRig(t, rigStart, capacity)
	m := r.m
	for i := 0; i < flows; i++ {
		m.ObserveFlow(flowKey(i), "")
		m.ObserveFlow(flowKey(i), "")
		if i >= revisitGap {
			m.ObserveFlow(flowKey(i-revisitGap), "video")
		}
	}

	tot := m.Totals()
	if tot.Conns != flows {
		t.Fatalf("%d flows, each seen again while still in one of the two generations, must count %d, got %d", flows, flows, tot.Conns)
	}
	if tot.ConnsInSets != flows-revisitGap {
		t.Fatalf("every revisited flow is classified once, want %d, got %d", flows-revisitGap, tot.ConnsInSets)
	}
	in, not := sumBuckets(m.Hello().Activity.Minute)
	if in+not != flows || in != flows-revisitGap {
		t.Fatalf("buckets hold %d in sets and %d not in a set, want %d and %d", in, not, flows-revisitGap, revisitGap)
	}
	if cur, old := tableSizes(m); cur > capacity || old > capacity {
		t.Fatalf("a generation never holds more than the cap: cur %d old %d", cur, old)
	}
}

func TestEarlyRotationRestartsTheTimedOne(t *testing.T) {
	r := newRig(t, rigStart, 4)
	m := r.m
	r.step(50 * time.Second)
	for i := 0; i < 5; i++ {
		m.ObserveFlow(flowKey(i), "")
	}
	r.step(20 * time.Second)
	m.ObserveFlow(flowKey(0), "")
	if got := m.Totals().Conns; got != 5 {
		t.Fatalf("the generation that filled 20 s ago must still be the old one, got %d connections", got)
	}
}

func TestObserveFlowDoesNotAllocateAcrossEarlyRotations(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	next := 0
	flood := func() {
		for end := next + 4*flowTableCap; next < end; next++ {
			m.ObserveFlow(flowKey(next), "")
		}
	}
	flood()
	if allocs := testing.AllocsPerRun(1, flood); allocs != 0 {
		t.Fatalf("%d new flows, four early rotations, %.0f allocations: once both generations have grown, rotating must reuse them", 4*flowTableCap, allocs)
	}
	if got := m.Totals().Conns; got != uint64(next) {
		t.Fatalf("every new flow counts once, want %d, got %d", next, got)
	}
}

func TestAFlowCountedBeforeAResetAddsNothingToTheNewTotals(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	withSetsProvider(t, []SetMeta{{ID: "video", Kind: SetKindBypass}, {ID: "ads", Kind: SetKindBlock}}, 0)
	early, blocked, late := flowKey(1), flowKey(2), flowKey(3)
	m.ObserveFlow(early, "")
	m.ObserveFlow(blocked, "")

	m.ResetCounters()
	m.ObserveFlow(early, "video")
	m.RecordBlockedFlow(blocked, "ads", "ads.example", "aa:bb:cc:dd:ee:ff")
	m.RecordBlockedFlow(blocked, "ads", "ads.example", "aa:bb:cc:dd:ee:ff")

	fr := m.Hello()
	if fr.Totals.Conns != 0 || fr.Totals.ConnsInSets != 0 || fr.Totals.BlockedConns != 0 {
		t.Fatalf("flows counted before the reset belong to the old totals: %+v", fr.Totals)
	}
	if len(fr.Blocked.Domains) != 0 || len(fr.Blocked.Devices) != 0 {
		t.Fatalf("the blocked lists count what the totals count: %+v", fr.Blocked)
	}
	if b := fr.Activity.Minute[len(fr.Activity.Minute)-1]; b.InSets != 2 || b.NotInSet != 0 {
		t.Fatalf("the activity history is not a counter, both flows move into sets in the open bucket: %+v", b)
	}
	if s := setActivity(t, fr, "video"); s.Conns60m != 1 || s.LastMatch == 0 {
		t.Fatalf("per-set activity still sees the match: %+v", s)
	}

	m.ObserveFlow(late, "")
	m.ObserveFlow(late, "video")
	m.RecordBlockedFlow(late, "video", "late.example", "")
	if tot := m.Totals(); tot.Conns != 1 || tot.ConnsInSets != 1 || tot.BlockedConns != 1 {
		t.Fatalf("a flow first seen after the reset counts in full: %+v", tot)
	}
}

func TestResetGenerationsNeverWrapIntoALiveFlow(t *testing.T) {
	for _, resets := range []int{254, 255, 256, 510, 1000} {
		r := newRig(t, rigStart, 0)
		m := r.m
		old := flowKey(1)
		m.ObserveFlow(old, "")
		for i := 0; i < resets; i++ {
			m.ResetCounters()
		}
		fresh := flowKey(2)
		m.ObserveFlow(fresh, "")
		m.ObserveFlow(old, "video")
		m.ObserveFlow(fresh, "video")
		if tot := m.Totals(); tot.Conns != 1 || tot.ConnsInSets != 1 {
			t.Fatalf("after %d resets: only the flow seen since the last reset counts, got %+v", resets, tot)
		}
	}
}

func TestClassificationMovesOnlyInsideOpenBuckets(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)
	m := r.m
	k := flowKey(1)
	m.ObserveFlow(k, "")
	r.step(time.Minute)
	m.ObserveFlow(k, "video")

	hello := m.Hello()
	minute := hello.Activity.Minute
	if len(minute) != 2 {
		t.Fatalf("want 2 minute buckets, got %d", len(minute))
	}
	if minute[0].NotInSet != 1 || minute[0].InSets != 0 {
		t.Fatalf("the closed minute must not change: %+v", minute[0])
	}
	if minute[1].NotInSet != 0 || minute[1].InSets != 0 {
		t.Fatalf("the flow was not counted in the open minute, nothing may move there: %+v", minute[1])
	}
	ten := hello.Activity.TenMinute
	if len(ten) != 1 || ten[0].InSets != 1 || ten[0].NotInSet != 0 {
		t.Fatalf("the ten-minute bucket is still open, the flow moves to in_sets: %+v", ten)
	}
	if hello.Totals.Conns != 1 || hello.Totals.ConnsInSets != 1 {
		t.Fatalf("totals %+v", hello.Totals)
	}

	m.ObserveFlow(k, "other")
	if got := m.Totals().ConnsInSets; got != 1 {
		t.Fatalf("the first set wins, a later match does not count again, got %d", got)
	}
}

func TestClassificationInTheSameMinuteMovesTheCount(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	k := flowKey(3)
	m.ObserveFlow(k, "")
	m.ObserveFlow(k, "video")
	b := m.Hello().Activity.Minute[0]
	if b.InSets != 1 || b.NotInSet != 0 {
		t.Fatalf("an open bucket moves the flow from not_in_set to in_sets: %+v", b)
	}
}

func TestPerSetRingSumsTheLastSixtyMinutes(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)
	m := r.m
	withSetsProvider(t, []SetMeta{
		{ID: "a", Name: "Video", Kind: SetKindBypass},
		{ID: "b", Name: "Games", Kind: SetKindRoute, Watched: true},
	}, 2)

	m.ObserveFlow(flowKey(5000), "b")
	n := 0
	for minute := 0; minute < 70; minute++ {
		m.ObserveFlow(flowKey(n), "a")
		n++
		r.step(time.Minute)
	}
	m.ObserveFlow(flowKey(n), "a")
	matchedAt := r.clk.wallNs()

	fr := m.Hello()
	a := setActivity(t, fr, "a")
	if a.Conns60m != 60 {
		t.Fatalf("one flow per minute: the last 60 minute buckets hold 60, got %d", a.Conns60m)
	}
	if a.LastMatch != matchedAt/int64(time.Millisecond) {
		t.Fatalf("last_match %d, want %d", a.LastMatch, matchedAt/int64(time.Millisecond))
	}
	if a.Name != "Video" || a.Kind != SetKindBypass {
		t.Fatalf("meta not carried: %+v", a)
	}
	b := setActivity(t, fr, "b")
	if b.Conns60m != 0 || !b.Watched || b.LastMatch == 0 {
		t.Fatalf("set b matched 70 minutes ago only: %+v", b)
	}
	if fr.SetsDisabled != 2 {
		t.Fatalf("sets_disabled %d", fr.SetsDisabled)
	}
	var inSets uint64
	for _, s := range fr.Sets {
		inSets += s.Conns60m
	}
	minuteIn, _ := sumBuckets(fr.Activity.Minute[1:])
	if minuteIn != inSets {
		t.Fatalf("per-set sums %d must match the 60 most recent minute buckets %d", inSets, minuteIn)
	}
}

func TestSetWithoutStateReportsZeros(t *testing.T) {
	r := newRig(t, rigStart, 0)
	withSetsProvider(t, []SetMeta{{ID: "quiet", Name: "Quiet", Kind: SetKindBlock}}, 0)
	s := setActivity(t, r.m.Hello(), "quiet")
	if s.Conns60m != 0 || s.LastMatch != 0 || s.DNSBlocked != 0 || s.Open != nil {
		t.Fatalf("a set that never matched reports zeros: %+v", s)
	}
}

func TestProxySetsReportOpenConnections(t *testing.T) {
	r := newRig(t, rigStart, 0)
	withSetsProvider(t, []SetMeta{
		{ID: "p", Name: "Proxy", Kind: SetKindProxy},
		{ID: "q", Name: "Idle proxy", Kind: SetKindProxy},
		{ID: "b", Name: "Bypass", Kind: SetKindBypass},
	}, 0)
	if s := setActivity(t, r.m.Hello(), "p"); s.Open != nil {
		t.Fatalf("without a provider open is omitted, got %d", *s.Open)
	}
	SetProxyOpenProvider(func() map[string]int64 { return map[string]int64{"p": 3, "b": 9} })
	t.Cleanup(func() { SetProxyOpenProvider(nil) })
	fr := r.m.Hello()
	if s := setActivity(t, fr, "p"); s.Open == nil || *s.Open != 3 {
		t.Fatalf("proxy set p: %+v", s)
	}
	if s := setActivity(t, fr, "q"); s.Open == nil || *s.Open != 0 {
		t.Fatalf("a proxy set without open connections reports 0: %+v", s)
	}
	if s := setActivity(t, fr, "b"); s.Open != nil {
		t.Fatalf("open is for proxy sets only: %+v", s)
	}
}

func TestCountConnectionNeverDedups(t *testing.T) {
	r := newRig(t, rigStart, 0)
	r.m.CountConnection("socks")
	r.m.CountConnection("socks")
	r.m.CountConnection("")
	tot := r.m.Totals()
	if tot.Conns != 3 || tot.ConnsInSets != 2 {
		t.Fatalf("totals %+v", tot)
	}
	b := r.m.Hello().Activity.Minute[0]
	if b.InSets != 2 || b.NotInSet != 1 {
		t.Fatalf("bucket %+v", b)
	}
}

func TestBlockedFlowCountsOncePerFlow(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	k := flowKey(9)
	m.ObserveFlow(k, "ads")
	m.RecordBlockedFlow(k, "ads", "ads.example", "aa:bb:cc:dd:ee:ff")
	m.RecordBlockedFlow(k, "ads", "ads.example", "aa:bb:cc:dd:ee:ff")
	m.RecordBlockedFlow(flowKey(10), "ads", "ads.example", "")

	tot := m.Totals()
	if tot.BlockedConns != 2 {
		t.Fatalf("two blocked flows, got %d", tot.BlockedConns)
	}
	if tot.Conns != 2 || tot.ConnsInSets != 2 {
		t.Fatalf("a blocked flow not seen before is counted as a connection too: %+v", tot)
	}
	bl := m.Hello().Blocked
	if len(bl.Domains) != 1 || bl.Domains[0].Count != 2 || bl.Domains[0].Key != "ads.example" {
		t.Fatalf("domains %+v", bl.Domains)
	}
	if len(bl.Devices) != 1 || bl.Devices[0].Count != 1 {
		t.Fatalf("devices %+v", bl.Devices)
	}
}

func TestBlockedDNSCountsEveryLookup(t *testing.T) {
	r := newRig(t, rigStart, 0)
	withSetsProvider(t, []SetMeta{{ID: "ads", Name: "Ads", Kind: SetKindBlock}}, 0)
	r.m.RecordBlockedDNS("ads", "ads.example", "aa:bb:cc:dd:ee:ff")
	r.m.RecordBlockedDNS("ads", "ads.example", "aa:bb:cc:dd:ee:ff")
	fr := r.m.Hello()
	if fr.Totals.BlockedDNS != 2 || fr.Totals.Conns != 0 {
		t.Fatalf("totals %+v", fr.Totals)
	}
	if s := setActivity(t, fr, "ads"); s.DNSBlocked != 2 || s.LastMatch != 0 {
		t.Fatalf("a DNS block is not a flow match: %+v", s)
	}
}

func TestSetStateIsForgottenTwoHoursAfterItLeavesTheList(t *testing.T) {
	r := newRig(t, rigStart, 0)
	metas := []SetMeta{{ID: "keep", Kind: SetKindBypass}}
	withSetsProvider(t, metas, 0)
	r.m.ObserveFlow(flowKey(1), "keep")
	r.m.ObserveFlow(flowKey(2), "gone")

	r.stepEach(119*time.Minute, time.Minute)
	r.m.fl.mu.Lock()
	gone := r.m.fl.set("gone")
	r.m.fl.mu.Unlock()
	if gone == nil {
		t.Fatal("a set is kept for 2 h after its last activity")
	}
	r.stepEach(3*time.Minute, time.Minute)
	r.m.fl.mu.Lock()
	gone, keep := r.m.fl.set("gone"), r.m.fl.set("keep")
	free := len(r.m.fl.free)
	r.m.fl.mu.Unlock()
	if gone != nil || free != 1 {
		t.Fatalf("set state for an id missing from the provider list must go after 2 h (free slots %d)", free)
	}
	if keep == nil {
		t.Fatal("a set still in the provider list is kept")
	}

	r.m.ObserveFlow(flowKey(3), "new")
	r.m.fl.mu.Lock()
	free = len(r.m.fl.free)
	r.m.fl.mu.Unlock()
	if free != 0 {
		t.Fatal("a freed slot is reused")
	}
}

func TestZeroValueCollectorWorks(t *testing.T) {
	m := &MetricsCollector{nowFn: newFakeClock(rigStart).now, sampler: &fakeSampler{}}
	m.ObserveFlow(flowKey(1), "x")
	m.Event(LevelInfo, EventSettingsApplied, map[string]string{"sets": "1"}, "Settings applied")
	fr := m.Hello()
	if fr.Totals.Conns != 1 || len(fr.Events.Items) != 1 || fr.Events.Items[0].Code != EventSettingsApplied {
		t.Fatalf("frame %+v", fr)
	}
}
