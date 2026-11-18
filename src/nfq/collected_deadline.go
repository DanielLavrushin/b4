package nfq

import (
	"container/heap"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type collectedDeadlineHeap []*helloWatch

func (h collectedDeadlineHeap) Len() int           { return len(h) }
func (h collectedDeadlineHeap) Less(i, j int) bool { return h[i].deadline.Before(h[j].deadline) }
func (h collectedDeadlineHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].collectIndex, h[j].collectIndex = i, j
}
func (h *collectedDeadlineHeap) Push(value any) {
	watch := value.(*helloWatch)
	watch.collectIndex = len(*h)
	*h = append(*h, watch)
}
func (h *collectedDeadlineHeap) Pop() any {
	old := *h
	watch := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	watch.collectIndex = -1
	return watch
}

func (t *destStateTracker) collectedWakeLocked() chan struct{} {
	if t.collectWake == nil {
		t.collectWake = make(chan struct{}, 1)
	}
	return t.collectWake
}

func (t *destStateTracker) CollectedWake() <-chan struct{} {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	return t.collectedWakeLocked()
}

func (t *destStateTracker) signalCollectedLocked() {
	select {
	case t.collectedWakeLocked() <- struct{}{}:
	default:
	}
}

func (t *destStateTracker) addCollectedDeadlineLocked(h *helloWatch) {
	if !h.collect || h.collectIndex >= 0 {
		return
	}
	heap.Push(&t.collectDeadlines, h)
	t.signalCollectedLocked()
}

func (t *destStateTracker) removeCollectedDeadlineLocked(h *helloWatch) {
	if !h.collect || h.collectIndex < 0 {
		return
	}
	heap.Remove(&t.collectDeadlines, h.collectIndex)
	t.signalCollectedLocked()
}

func (t *destStateTracker) NextCollectedDeadline() (time.Time, bool) {
	t.watchMu.RLock()
	defer t.watchMu.RUnlock()
	if len(t.collectDeadlines) == 0 {
		return time.Time{}, false
	}
	return t.collectDeadlines[0].deadline, true
}

func (t *destStateTracker) DueCollected(now time.Time, limit int) []helloWatch {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	var due []helloWatch
	for len(t.collectDeadlines) > 0 && len(due) < limit {
		h := t.collectDeadlines[0]
		if now.Before(h.deadline) {
			break
		}
		heap.Pop(&t.collectDeadlines)
		if now.Sub(h.deadline) > helloWatchTTL || h.closed {
			t.unwatchLocked(h.key)
			continue
		}
		h.probing = true
		due = append(due, *h)
	}
	return due
}

func (w *Worker) sweepCollectedHellos(cfg *config.Config, now time.Time) {
	if w == nil || w.destState == nil {
		return
	}
	for _, h := range w.destState.DueCollected(now, maxSilenceLookups) {
		var set *config.SetConfig
		if cfg != nil {
			set = cfg.GetSetById(h.setID)
		}
		if cfg == nil || cfg.Queue.IsDiscovery || set == nil || !set.Enabled || !set.TCP.IPBlockDetect.Collects() {
			w.destState.EndWatch(h)
			continue
		}
		w.collectSilentHello(h)
	}
}

func (p *Pool) sweepCollectedHellos(now time.Time) {
	if len(p.Workers) > 0 {
		p.Workers[0].sweepCollectedHellos(p.GetFirstWorkerConfig(), now)
	}
}

func (t *destStateTracker) resetCollectedTimer(timer *time.Timer) <-chan time.Time {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	deadline, ok := t.NextCollectedDeadline()
	if !ok {
		return nil
	}
	timer.Reset(max(time.Until(deadline), time.Duration(0)))
	return timer.C
}
