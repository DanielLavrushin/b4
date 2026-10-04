package metrics

import (
	"slices"
	"testing"
	"time"
)

func TestFirstBucketStartsAtProcessStartAndLaterOnesOnMinuteBoundaries(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m

	fr := m.Hello()
	if got := bucketTimes(fr.Activity.Minute); !slices.Equal(got, []int64{ms(rigStart)}) {
		t.Fatalf("first minute bucket %v, want process start %d", got, ms(rigStart))
	}
	if fr.Activity.TenMinute[0].T != ms(rigStart) || fr.RSSHistory[0].T != ms(rigStart) {
		t.Fatalf("first ten-minute bucket %+v / rss %+v", fr.Activity.TenMinute, fr.RSSHistory)
	}

	r.step(24 * time.Second)
	if n := len(m.Hello().Activity.Minute); n != 1 {
		t.Fatalf("12:00:59 is still the first minute, got %d buckets", n)
	}

	r.step(1700 * time.Millisecond)
	got := bucketTimes(m.Hello().Activity.Minute)
	want := []int64{ms(rigStart), ms(time.Date(2026, 10, 3, 12, 1, 0, 0, time.UTC))}
	if !slices.Equal(got, want) {
		t.Fatalf("a late tick still opens the bucket at the boundary: got %v want %v", got, want)
	}

	r.at(time.Date(2026, 10, 3, 13, 5, 0, 0, time.UTC))
	fr = m.Hello()
	if len(fr.Activity.Minute) != MinuteBuckets {
		t.Fatalf("hello holds %d minute buckets, want %d", len(fr.Activity.Minute), MinuteBuckets)
	}
	if first, last := fr.Activity.Minute[0].T, fr.Activity.Minute[MinuteBuckets-1].T; first != ms(time.Date(2026, 10, 3, 12, 5, 0, 0, time.UTC)) || last != ms(time.Date(2026, 10, 3, 13, 5, 0, 0, time.UTC)) {
		t.Fatalf("minute window %d..%d", first, last)
	}
	for i := 1; i < len(fr.Activity.Minute); i++ {
		if d := fr.Activity.Minute[i].T - fr.Activity.Minute[i-1].T; d != 60_000 {
			t.Fatalf("minute buckets must be contiguous, gap %d at %d", d, i)
		}
	}
	wantTen := []int64{ms(rigStart)}
	for min := 10; min <= 60; min += 10 {
		wantTen = append(wantTen, ms(time.Date(2026, 10, 3, 12, min, 0, 0, time.UTC)))
	}
	if got := bucketTimes(fr.Activity.TenMinute); !slices.Equal(got, wantTen) {
		t.Fatalf("ten-minute buckets %v want %v", got, wantTen)
	}
	if len(fr.RSSHistory) != len(fr.Activity.TenMinute) || fr.RSSHistory[len(fr.RSSHistory)-1].T != wantTen[len(wantTen)-1] {
		t.Fatalf("rss history must follow the ten-minute buckets: %+v", fr.RSSHistory)
	}
}

func TestTenMinuteRingHolds145Buckets(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), 0)
	r.stepEach(30*time.Hour, 10*time.Minute)
	fr := r.m.Hello()
	if len(fr.Activity.TenMinute) != TenMinuteBuckets || len(fr.RSSHistory) != RSSBuckets {
		t.Fatalf("got %d ten-minute and %d rss buckets", len(fr.Activity.TenMinute), len(fr.RSSHistory))
	}
	if last := fr.Activity.TenMinute[TenMinuteBuckets-1].T; last != ms(time.Date(2026, 10, 4, 6, 0, 0, 0, time.UTC)) {
		t.Fatalf("the open ten-minute bucket must be the last element, got %d", last)
	}
}

func TestTickCarriesTheLastClosedAndTheOpenBucket(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)
	var sent SentRevs
	fr := r.m.Tick(&sent)
	if len(fr.Activity.Minute) != 1 || len(fr.Activity.TenMinute) != 1 || len(fr.RSSHistory) != 1 {
		t.Fatalf("before the first rollover a tick has only the open bucket: %+v", fr.Activity)
	}
	r.stepEach(25*time.Minute, time.Minute)
	fr = r.m.Tick(&sent)
	if got := bucketTimes(fr.Activity.Minute); !slices.Equal(got, []int64{ms(time.Date(2026, 10, 3, 12, 24, 0, 0, time.UTC)), ms(time.Date(2026, 10, 3, 12, 25, 0, 0, time.UTC))}) {
		t.Fatalf("tick minute buckets %v", got)
	}
	if got := bucketTimes(fr.Activity.TenMinute); !slices.Equal(got, []int64{ms(time.Date(2026, 10, 3, 12, 10, 0, 0, time.UTC)), ms(time.Date(2026, 10, 3, 12, 20, 0, 0, time.UTC))}) {
		t.Fatalf("tick ten-minute buckets %v", got)
	}
	if len(fr.RSSHistory) != 2 {
		t.Fatalf("tick rss %+v", fr.RSSHistory)
	}
}

func TestWallClockJumpRelabelsHistory(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	m.ObserveFlow(flowKey(1), "")
	r.step(30 * time.Second)
	m.ObserveFlow(flowKey(2), "")
	before := m.Hello()

	jump := 365*24*time.Hour + 17500*time.Millisecond
	r.clk.jump(jump)
	r.step(time.Second)
	after := m.Hello()

	if len(after.Activity.Minute) != len(before.Activity.Minute) {
		t.Fatalf("a clock jump must not open or drop buckets: %d -> %d", len(before.Activity.Minute), len(after.Activity.Minute))
	}
	shift := jump.Milliseconds()
	for i := range before.Activity.Minute {
		if d := after.Activity.Minute[i].T - before.Activity.Minute[i].T; d != shift {
			t.Fatalf("bucket %d moved by %d ms, want %d", i, d, shift)
		}
		if after.Activity.Minute[i].NotInSet != before.Activity.Minute[i].NotInSet {
			t.Fatal("a clock jump must not change counts")
		}
	}
	if d := after.Activity.TenMinute[0].T - before.Activity.TenMinute[0].T; d != shift {
		t.Fatalf("ten-minute bucket moved by %d", d)
	}
	if d := after.Now - before.Now; d != shift+1000 {
		t.Fatalf("now moved by %d, want %d", d, shift+1000)
	}
	if after.UptimeS != 31 {
		t.Fatalf("uptime comes from the monotonic clock, got %d", after.UptimeS)
	}
	if d := after.StatsSince - before.StatsSince; d != shift {
		t.Fatalf("stats_since moved by %d", d)
	}

	r.step(54 * time.Second)
	newWall := time.Date(2027, 10, 3, 12, 2, 17, 500e6, time.UTC)
	got := bucketTimes(m.Hello().Activity.Minute)
	if got[len(got)-1] != ms(newWall) {
		t.Fatalf("the bucket open during the jump closes on the monotonic clock, next starts at %d want %d", got[len(got)-1], ms(newWall))
	}
	r.step(42500 * time.Millisecond)
	got = bucketTimes(m.Hello().Activity.Minute)
	if got[len(got)-1] != ms(time.Date(2027, 10, 3, 12, 3, 0, 0, time.UTC)) {
		t.Fatalf("buckets realign to the new wall-clock minute, got %d", got[len(got)-1])
	}
}

func TestSmallClockNoiseKeepsBucketTimesStable(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)
	r.step(90 * time.Second)
	before := bucketTimes(r.m.Hello().Activity.Minute)
	r.clk.jump(3 * time.Millisecond)
	r.step(time.Second)
	r.clk.jump(-5 * time.Millisecond)
	r.step(time.Second)
	if got := bucketTimes(r.m.Hello().Activity.Minute); !slices.Equal(got, before) {
		t.Fatalf("millisecond noise between the two clocks must not move bucket keys: %v -> %v", before, got)
	}
}

func TestLongStallClosesEveryMissedBucket(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)
	r.m.ObserveFlow(flowKey(1), "")
	r.step(3 * time.Hour)
	fr := r.m.Hello()
	if len(fr.Activity.Minute) != MinuteBuckets {
		t.Fatalf("got %d minute buckets", len(fr.Activity.Minute))
	}
	if in, not := sumBuckets(fr.Activity.Minute); in != 0 || not != 0 {
		t.Fatalf("the flow from three hours ago is outside the window: %d %d", in, not)
	}
	if last := fr.Activity.Minute[MinuteBuckets-1].T; last != ms(time.Date(2026, 10, 3, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("open bucket %d", last)
	}
	if first := fr.Activity.TenMinute[0].T; first != ms(time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("the ten-minute ring still reaches back to the start: %d", first)
	}
}

func TestCPUPercentOverTenSecondsAndPeakMinute(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)
	for i := 0; i < 60; i++ {
		r.smp.ticks += 50
		r.step(time.Second)
	}
	if got := r.m.Process().CPUPercent; got != 25 {
		t.Fatalf("50 ticks/s at CLK_TCK 100 on 2 cores is 25%%, got %v", got)
	}
	for i := 0; i < 60; i++ {
		r.smp.ticks += 100
		r.step(time.Second)
	}
	for i := 0; i < 5; i++ {
		r.smp.ticks += 10
		r.step(time.Second)
	}
	p := r.m.Process()
	if p.CPUPercent != 27.5 {
		t.Fatalf("10 s window: 5 s at 50%% and 5 s at 5%% is 27.5%%, got %v", p.CPUPercent)
	}
	if p.CPUPeakPercent != 50 || p.CPUPeakAt != ms(time.Date(2026, 10, 3, 12, 1, 0, 0, time.UTC)) {
		t.Fatalf("peak %v at %d", p.CPUPeakPercent, p.CPUPeakAt)
	}
	if p.CPUCores != 2 || p.ThreadWarn != threadDumpTrigger || p.ThreadLimit != maxOSThreads || p.OSThreads != 12 {
		t.Fatalf("process %+v", p)
	}
	if p.RSSBytes != 30<<20 || p.MemTotalBytes != 512<<20 {
		t.Fatalf("memory %+v", p)
	}
}

func TestCPUPeakBeforeTheFirstClosedMinute(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)
	for i := 0; i < 20; i++ {
		r.smp.ticks += 20
		r.step(time.Second)
	}
	p := r.m.Process()
	if p.CPUPeakPercent != 10 || p.CPUPeakAt != ms(time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("with no closed minute the open one so far stands in: %v at %d", p.CPUPeakPercent, p.CPUPeakAt)
	}
}

func TestRSSHistoryKeepsTheMaxPerTenMinutes(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)
	r.smp.rssV = 40 << 20
	r.step(time.Minute)
	r.smp.rssV = 35 << 20
	r.step(time.Minute)
	r.smp.rssV = 20 << 20
	r.step(9 * time.Minute)
	fr := r.m.Hello()
	if len(fr.RSSHistory) != 2 || fr.RSSHistory[0].Max != 40<<20 || fr.RSSHistory[1].Max != 20<<20 {
		t.Fatalf("rss history %+v", fr.RSSHistory)
	}
	if fr.Process.RSSBytes != 20<<20 {
		t.Fatalf("current rss %d", fr.Process.RSSBytes)
	}
}

func TestOverloadIsTheIncreaseOverTenMinutes(t *testing.T) {
	var inject, raw, overflow uint64
	SetOverloadProvider(func() (uint64, uint64, uint64) { return inject, raw, overflow })
	t.Cleanup(func() { SetOverloadProvider(nil) })
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)

	inject, raw = 5, 1
	r.step(time.Minute)
	ov := r.m.Hello().Attention.Overload
	if ov.WindowS != OverloadWindowSeconds || ov.InjectSkipped != 5 || ov.RawSendDropped != 1 || ov.QueueOverflow != 0 {
		t.Fatalf("young process: the window reaches back to the start: %+v", ov)
	}
	r.stepEach(12*time.Minute, time.Minute)
	overflow = 7
	ov = r.m.Hello().Attention.Overload
	if ov.InjectSkipped != 0 || ov.RawSendDropped != 0 || ov.QueueOverflow != 7 {
		t.Fatalf("increases older than the window drop out: %+v", ov)
	}
}

func TestLastPacketAtTracksTheCounter(t *testing.T) {
	var packets uint64
	SetPacketCounterProvider(func() uint64 { return packets })
	t.Cleanup(func() { SetPacketCounterProvider(nil) })
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)

	r.step(time.Second)
	if got := r.m.Hello().LastPacketAt; got != 0 {
		t.Fatalf("no packet since start must be 0, got %d", got)
	}
	packets = 10
	r.step(time.Second)
	at := ms(time.Date(2026, 10, 3, 12, 0, 2, 0, time.UTC))
	r.step(time.Second)
	r.step(time.Second)
	if got := r.m.Hello().LastPacketAt; got != at {
		t.Fatalf("last_packet_at %d, want %d", got, at)
	}
	packets = 3
	r.step(time.Second)
	if got := r.m.Hello().LastPacketAt; got != at {
		t.Fatalf("a counter that went down (new pool) is not a packet, got %d", got)
	}
	packets = 4
	r.step(time.Second)
	if got := r.m.Hello().LastPacketAt; got != ms(time.Date(2026, 10, 3, 12, 0, 6, 0, time.UTC)) {
		t.Fatalf("last_packet_at after the new baseline %d", got)
	}
}

func TestConntrackAndBinaryEveryTenSeconds(t *testing.T) {
	replaced := false
	SetBinaryReplacedProvider(func() bool { return replaced })
	t.Cleanup(func() { SetBinaryReplacedProvider(nil) })
	r := newRig(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), 0)

	if r.m.Hello().Attention.Conntrack != nil {
		t.Fatal("conntrack is omitted until it was read")
	}
	r.smp.ct, r.smp.ctOK = Conntrack{Count: 930, Max: 1000}, true
	r.step(time.Second)
	if ct := r.m.Hello().Attention.Conntrack; ct == nil || ct.Count != 930 || ct.Max != 1000 {
		t.Fatalf("conntrack %+v", ct)
	}
	replaced = true
	r.stepEach(9*time.Second, time.Second)
	if r.m.Hello().Attention.BinaryReplaced || r.smp.ctReads != 1 {
		t.Fatalf("re-checked before 10 s passed (reads %d)", r.smp.ctReads)
	}
	r.step(time.Second)
	if !r.m.Hello().Attention.BinaryReplaced || r.smp.ctReads != 2 {
		t.Fatalf("binary_replaced must be re-checked every 10 s (reads %d)", r.smp.ctReads)
	}
	r.smp.ctOK = false
	r.step(10 * time.Second)
	if r.m.Hello().Attention.Conntrack != nil {
		t.Fatal("an unreadable conntrack table is omitted")
	}
}

func TestNextBoundary(t *testing.T) {
	cases := []struct {
		start, off, step, want int64
	}{
		{0, 35e9, minuteNs, 25e9},
		{25e9, 35e9, minuteNs, 85e9},
		{0, 60e9, minuteNs, 60e9},
		{0, -1, minuteNs, 1},
	}
	for _, c := range cases {
		if got := nextBoundary(c.start, c.off, c.step); got != c.want {
			t.Errorf("nextBoundary(%d, %d, %d) = %d, want %d", c.start, c.off, c.step, got, c.want)
		}
	}
}
