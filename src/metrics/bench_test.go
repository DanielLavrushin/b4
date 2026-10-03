package metrics

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentRecordersTicksAndFrames(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	withSetsProvider(t, []SetMeta{{ID: "a", Kind: SetKindBypass}, {ID: "ads", Kind: SetKindBlock}}, 1)

	var wg sync.WaitGroup
	var stop atomic.Bool
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; !stop.Load(); i++ {
				k := flowKey(w*100000 + i%5000)
				switch i % 7 {
				case 0:
					m.ObserveFlow(k, "a")
				case 1:
					m.RecordBlockedFlow(k, "ads", "ads.example", "aa:bb:cc:dd:ee:ff")
				case 2:
					m.RecordBlockedDNS("ads", "ads.example", "")
				case 3:
					m.CountConnection("a")
				default:
					m.ObserveFlow(k, "")
				}
			}
		}(w)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		var sent SentRevs
		for !stop.Load() {
			_ = m.Tick(&sent)
			_ = m.Hello()
			m.Event(LevelInfo, EventSettingsApplied, nil, "settings applied")
			m.NoteRulesRestored()
		}
	}()
	for i := 0; i < 200; i++ {
		r.step(time.Second)
		if i == 100 {
			m.ResetCounters()
		}
	}
	stop.Store(true)
	wg.Wait()
	m.ObserveFlow(flowKey(999999), "")

	tot := m.Totals()
	fr := m.Hello()
	var in, not uint64
	for _, b := range fr.Activity.TenMinute {
		in += b.InSets
		not += b.NotInSet
	}
	if tot.Conns == 0 || in+not < tot.Conns {
		t.Fatalf("buckets %d+%d must hold at least the %d connections since the reset", in, not, tot.Conns)
	}
}

func newBenchCollector(b *testing.B) *MetricsCollector {
	r := newRig(b, rigStart, 0)
	return r.m
}

func BenchmarkObserveFlow(b *testing.B) {
	b.Run("known", func(b *testing.B) {
		m := newBenchCollector(b)
		keys := make([]FlowKey, 1024)
		for i := range keys {
			keys[i] = flowKey(i)
			m.ObserveFlow(keys[i], "")
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.ObserveFlow(keys[i&1023], "")
		}
	})
	b.Run("known-in-set", func(b *testing.B) {
		m := newBenchCollector(b)
		keys := make([]FlowKey, 1024)
		for i := range keys {
			keys[i] = flowKey(i)
			m.ObserveFlow(keys[i], "video")
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.ObserveFlow(keys[i&1023], "video")
		}
	})
	b.Run("new", func(b *testing.B) {
		m := newBenchCollector(b)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.ObserveFlow(flowKey(i), "")
		}
	})
	b.Run("new-in-set", func(b *testing.B) {
		m := newBenchCollector(b)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.ObserveFlow(flowKey(i), "video")
		}
	})
	b.Run("new-beyond-cap-warm", func(b *testing.B) {
		m := newBenchCollector(b)
		warm := 3 * flowTableCap
		for i := 0; i < warm; i++ {
			m.ObserveFlow(flowKey(i), "")
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.ObserveFlow(flowKey(warm+i), "")
		}
	})
	b.Run("parallel-known", func(b *testing.B) {
		m := newBenchCollector(b)
		keys := make([]FlowKey, 4096)
		for i := range keys {
			keys[i] = flowKey(i)
			m.ObserveFlow(keys[i], "")
		}
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				m.ObserveFlow(keys[i&4095], "")
				i++
			}
		})
	})
}

func BenchmarkTickFrame(b *testing.B) {
	m := newBenchCollector(b)
	for i := 0; i < 2000; i++ {
		m.ObserveFlow(flowKey(i), "")
	}
	var sent SentRevs
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Tick(&sent)
	}
}
