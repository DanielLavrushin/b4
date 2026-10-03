package metrics

import (
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu   sync.Mutex
	mono int64
	wall int64
}

func newFakeClock(wall time.Time) *fakeClock { return &fakeClock{wall: wall.UnixNano()} }

func (c *fakeClock) now() (int64, int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mono, c.wall
}

func (c *fakeClock) wallNs() int64 {
	_, w := c.now()
	return w
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.mono += int64(d)
	c.wall += int64(d)
	c.mu.Unlock()
}

func (c *fakeClock) jump(d time.Duration) {
	c.mu.Lock()
	c.wall += int64(d)
	c.mu.Unlock()
}

type fakeSampler struct {
	ticks   uint64
	rssV    uint64
	thr     int
	ct      Conntrack
	ctOK    bool
	mem     uint64
	noCPU   bool
	ctReads int
}

func (s *fakeSampler) cpuTicks() (uint64, bool) { return s.ticks, !s.noCPU }
func (s *fakeSampler) rss() uint64              { return s.rssV }
func (s *fakeSampler) threads() int             { return s.thr }
func (s *fakeSampler) conntrack() (Conntrack, bool) {
	s.ctReads++
	return s.ct, s.ctOK
}
func (s *fakeSampler) memTotal() uint64 { return s.mem }

type rig struct {
	t   testing.TB
	m   *MetricsCollector
	clk *fakeClock
	smp *fakeSampler
}

var rigStart = time.Date(2026, 10, 3, 12, 0, 35, 0, time.UTC)

func newRig(t testing.TB, start time.Time, capacity int) *rig {
	t.Helper()
	clk := newFakeClock(start)
	smp := &fakeSampler{rssV: 30 << 20, thr: 12, mem: 512 << 20}
	m := &MetricsCollector{nowFn: clk.now, sampler: smp, clkTck: 100, cores: 2, flowCap: capacity}
	m.initOnce.Do(m.init)
	return &rig{t: t, m: m, clk: clk, smp: smp}
}

func (r *rig) step(d time.Duration) {
	r.clk.advance(d)
	r.m.tick()
}

func (r *rig) stepEach(total, every time.Duration) {
	for done := time.Duration(0); done < total; done += every {
		r.step(every)
	}
}

func (r *rig) at(wall time.Time) {
	d := time.Duration(wall.UnixNano() - r.clk.wallNs())
	if d < 0 {
		r.t.Fatalf("rig.at cannot move backwards: %v", d)
	}
	r.step(d)
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func flowKey(n int) FlowKey {
	var k FlowKey
	binary.BigEndian.PutUint32(k.Addr[0:4], 0x0a000000|uint32(n&0xffffff))
	binary.BigEndian.PutUint32(k.Addr[16:20], 0x5db8d822)
	k.SPort = uint16(1024 + n%60000)
	k.DPort = 443
	k.Proto = 6
	return k
}

func bucketTimes(bs []ActivityBucket) []int64 {
	out := make([]int64, len(bs))
	for i, b := range bs {
		out[i] = b.T
	}
	return out
}

func sumBuckets(bs []ActivityBucket) (inSets, notInSet uint64) {
	for _, b := range bs {
		inSets += b.InSets
		notInSet += b.NotInSet
	}
	return
}

func withSetsProvider(t testing.TB, metas []SetMeta, disabled int) {
	t.Helper()
	SetSetsProvider(func() ([]SetMeta, int) { return metas, disabled })
	t.Cleanup(func() { SetSetsProvider(nil) })
}

func setActivity(t testing.TB, fr Frame, id string) SetActivity {
	t.Helper()
	for _, s := range fr.Sets {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("set %q missing from the frame: %+v", id, fr.Sets)
	return SetActivity{}
}
