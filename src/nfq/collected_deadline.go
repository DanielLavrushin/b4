package nfq

import (
	"container/heap"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/log"
)

type collectedReading struct {
	watch helloWatch
	flow  ctnl.Flow
	state serverSilence
}

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
		if now.Sub(h.deadline) > helloWatchTTL || h.closed || h.parked {
			t.unwatchLocked(h.key)
			continue
		}
		h.probing = true
		due = append(due, *h)
	}
	return due
}

func (t *destStateTracker) countCollected(readings []collectedReading, now time.Time, window time.Duration) []helloWatch {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	answered := make(map[string]bool)
	for _, r := range readings {
		if r.state != silenceAnswered {
			continue
		}
		answered[iphealth.BanKey(r.watch.dst, int(r.watch.port))] = true
		t.forgetCollectRunLocked(&r.watch)
		if cur := t.watches[r.watch.key]; cur != nil && cur.gen == r.watch.gen {
			t.unwatchLocked(r.watch.key)
		}
	}
	var completed []helloWatch
	for _, r := range readings {
		cur := t.watches[r.watch.key]
		if r.state == silenceAnswered || cur == nil || cur.gen != r.watch.gen {
			continue
		}
		key := iphealth.BanKey(cur.dst, int(cur.port))
		if cur.closed || r.state != silenceOpen || !collectedFlowSilent(cur, cur, r.flow) {
			log.Tracef("Collected IPs: no reliable silent-flow evidence for %s", cur.key)
			t.unwatchLocked(cur.key)
			continue
		}
		if answered[key] {
			log.Tracef("Collected IPs: %s took the ClientHello and sent nothing back, but answered another connection that came due with it, so this one is not counted (host: %s, set: %s)", key, cur.host, cur.set)
			t.unwatchLocked(cur.key)
			continue
		}
		count, ok := t.countCollectRunLocked(key, cur.counted, now, window)
		if !ok {
			t.unwatchLocked(cur.key)
			continue
		}
		cur.counted = true
		cur.replyBytes, cur.replyPackets, cur.replyChecked = r.flow.ReplyBytes, r.flow.ReplyPackets, true
		if count < silentHellosToCollect {
			log.Tracef("Collected IPs: %s took the ClientHello and sent nothing back, unanswered connection %d of the %d in a row that collect the address (host: %s, set: %s)", key, count, silentHellosToCollect, cur.host, cur.set)
			cur.probing, cur.parked = false, true
			cur.deadline = cur.deadline.Add(helloWatchTTL)
			t.addCollectedDeadlineLocked(cur)
			continue
		}
		log.Tracef("Collected IPs: %s took the ClientHello and sent nothing back on %d connections in a row (host: %s, set: %s)", key, count, cur.host, cur.set)
		completed = append(completed, *cur)
	}
	return completed
}

func (w *Worker) sweepCollectedHellos(cfg *config.Config, now time.Time) {
	if w == nil || w.destState == nil {
		return
	}
	var readings []collectedReading
	for _, h := range w.destState.DueCollected(now, maxSilenceLookups) {
		var set *config.SetConfig
		if cfg != nil {
			set = cfg.GetSetById(h.setID)
		}
		if cfg == nil || cfg.Queue.IsDiscovery || set == nil || !set.Enabled || !set.TCP.IPBlockDetect.Collects() {
			w.destState.EndWatch(h)
			continue
		}
		flow, state := readCollectedFlow(h)
		readings = append(readings, collectedReading{watch: h, flow: flow, state: state})
	}
	if len(readings) == 0 {
		return
	}
	for _, h := range w.destState.countCollected(readings, now, cfg.System.IPHealth.RetestInterval()) {
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
