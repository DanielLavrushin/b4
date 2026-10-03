package metrics

import "math"

type cpuSample struct {
	mono  int64
	ticks uint64
	ok    bool
}

type minuteBucket struct {
	seq      uint32
	start    int64
	end      int64
	inSets   uint64
	notInSet uint64
	cpuStart cpuSample
	cpuPct   float64
	ovl      [3]uint64
}

type tenBucket struct {
	seq      uint32
	start    int64
	end      int64
	inSets   uint64
	notInSet uint64
	rssMax   uint64
}

func cpuPercent(a, b cpuSample, clk uint64, cores int) float64 {
	if !a.ok || !b.ok || b.mono <= a.mono || b.ticks < a.ticks || clk == 0 || cores <= 0 {
		return 0
	}
	secs := float64(b.mono-a.mono) / 1e9
	return round2(float64(b.ticks-a.ticks) / (secs * float64(clk) * float64(cores)) * 100)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func (f *flowState) openMinute() *minuteBucket {
	return &f.minute[f.minSeq%MinuteBuckets]
}

func (f *flowState) openTen() *tenBucket {
	return &f.ten[f.tenSeq%TenMinuteBuckets]
}

func (f *flowState) roll(mono, off int64, cpu cpuSample, ovl [3]uint64, clk uint64, cores int) {
	for {
		b := f.openMinute()
		if mono < b.end {
			break
		}
		b.cpuPct = cpuPercent(b.cpuStart, cpu, clk, cores)
		start := b.end
		f.minSeq++
		*f.openMinute() = minuteBucket{
			seq:      f.minSeq,
			start:    start,
			end:      nextBoundary(start, off, minuteNs),
			cpuStart: cpu,
			ovl:      ovl,
		}
	}
	for {
		b := f.openTen()
		if mono < b.end {
			break
		}
		start := b.end
		f.tenSeq++
		*f.openTen() = tenBucket{
			seq:   f.tenSeq,
			start: start,
			end:   nextBoundary(start, off, tenMinuteNs),
		}
	}
	if cpu.ok {
		f.cpuLast = cpu
	}
}

func firstSeq(open uint32, ring int) uint32 {
	if open < uint32(ring-1) {
		return 0
	}
	return open - uint32(ring-1)
}

func (f *flowState) minuteBuckets(from uint32, off int64) []ActivityBucket {
	out := make([]ActivityBucket, 0, f.minSeq-from+1)
	for s := from; s <= f.minSeq; s++ {
		b := &f.minute[s%MinuteBuckets]
		out = append(out, ActivityBucket{T: wallMs(b.start, off), InSets: b.inSets, NotInSet: b.notInSet})
	}
	return out
}

func (f *flowState) tenBuckets(from uint32, off int64) ([]ActivityBucket, []RSSBucket) {
	act := make([]ActivityBucket, 0, f.tenSeq-from+1)
	rss := make([]RSSBucket, 0, f.tenSeq-from+1)
	for s := from; s <= f.tenSeq; s++ {
		b := &f.ten[s%TenMinuteBuckets]
		t := wallMs(b.start, off)
		act = append(act, ActivityBucket{T: t, InSets: b.inSets, NotInSet: b.notInSet})
		rss = append(rss, RSSBucket{T: t, Max: b.rssMax})
	}
	return act, rss
}

func (f *flowState) cpuPeak(clk uint64, cores int) (float64, int64, bool) {
	if f.minSeq == 0 {
		b := f.openMinute()
		if !b.cpuStart.ok || !f.cpuLast.ok {
			return 0, 0, false
		}
		return cpuPercent(b.cpuStart, f.cpuLast, clk, cores), b.start, true
	}
	lo := firstSeq(f.minSeq, MinuteBuckets)
	peak, at := -1.0, int64(0)
	for s := lo; s < f.minSeq; s++ {
		b := &f.minute[s%MinuteBuckets]
		if b.cpuPct >= peak {
			peak, at = b.cpuPct, b.start
		}
	}
	return peak, at, true
}

func (f *flowState) overloadBase() [3]uint64 {
	seq := uint32(0)
	if f.minSeq > 10 {
		seq = f.minSeq - 10
	}
	return f.minute[seq%MinuteBuckets].ovl
}

func deltaSince(now, base uint64) uint64 {
	if now < base {
		return now
	}
	return now - base
}
