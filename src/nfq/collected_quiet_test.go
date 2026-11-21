package nfq

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
)

func quietTickRig(t *testing.T) (*silenceRig, *atomic.Int32) {
	t.Helper()
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)
	state := newRuntimeState()
	state.bans, state.destState, state.tlsCache = r.w.bans, r.w.destState, r.w.tlsCache
	r.pool.state = state
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.pool.collectedCtx, r.pool.collectedCancel = ctx, cancel
	r.pool.collectedBase = r.w.getConfig()
	paused := &atomic.Int32{}
	r.pool.collectedPaused = func() bool {
		paused.Add(1)
		return false
	}
	return r, paused
}

func tickWhileASaveHoldsTheConfig(t *testing.T, r *silenceRig, now time.Time) bool {
	t.Helper()
	r.pool.configMu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.pool.escalationTick(now)
	}()
	finished := false
	select {
	case <-done:
		finished = true
	case <-time.After(2 * time.Second):
	}
	r.pool.configMu.Unlock()
	<-done
	r.w.bans.wg.Wait()
	return finished
}

func TestABlockSweepDoesNotWaitForASaveWhenNothingIsCollected(t *testing.T) {
	r, paused := quietTickRig(t)
	r.pool.escalationTick(time.Now())
	paused.Store(0)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	if !tickWhileASaveHoldsTheConfig(t, r, time.Now().Add(time.Minute)) {
		t.Error("with no set collecting or using collected IPs, the 2 s tick waited for a save that held the pool's config lock, so the silent-hello sweep and the ban list waited with it")
	}
	if !r.w.bans.reg.Banned(net.ParseIP("1.2.3.4"), 443) {
		t.Error("the tick did not sweep the silent ClientHello into a ban")
	}
	if got := len(r.pool.GetBans()); got != 1 {
		t.Errorf("the ban list holds %d entries, want the new ban", got)
	}
	if n := paused.Load(); n != 0 {
		t.Errorf("the tick asked %d times whether Discovery runs, with nothing collected to hold back", n)
	}
}

func TestACollectedPoolStillGetsItsPass(t *testing.T) {
	r, paused := quietTickRig(t)
	r.pool.escalationTick(time.Now())
	paused.Store(0)
	consumer := collectedConsumerSet()
	cfg := *r.w.getConfig()
	cfg.Sets = append(append(cfg.Sets[:0:0], cfg.Sets...), &consumer)
	if err := r.pool.UpdateConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r.pool.escalationTick(time.Now())
	if paused.Load() == 0 {
		t.Fatal("a save that adds a set using collected IPs left the collected pass idle")
	}
}

func TestAQuietPoolStillEndsALeftoverCollectWatch(t *testing.T) {
	r, paused := quietTickRig(t)
	now := time.Now()
	r.pool.escalationTick(now)
	if !r.pool.collectedQuiet.Load() {
		t.Fatal("setup: with nothing collected or in use, the first tick must leave the pool quiet")
	}
	paused.Store(0)

	leftover := historyWatch("leftover", 81)
	leftover.setID = r.w.getConfig().Sets[0].Id
	leftover.opened, leftover.deadline = now.Add(-2*time.Second), now.Add(-time.Second)
	r.w.destState.WatchHello(leftover)
	if !r.w.destState.CollectWatching() {
		t.Fatal("setup: the leftover Collect watch is not counted, so this test proves nothing")
	}

	r.pool.escalationTick(now)
	if r.w.destState.CollectWatching() {
		t.Error("a quiet pool skipped the pass with a Collect watch open, so a watch left from a set that no longer collects was never ended and every packet to its server kept paying for it")
	}
	if deadline, pending := r.w.destState.NextCollectedDeadline(); pending {
		t.Errorf("the leftover watch's deadline %v stays queued, so the collected timer fires at once on every loop", deadline)
	}
	if paused.Load() == 0 {
		t.Error("the tick never reached the collected pass")
	}
}

func TestTheCollectedHistoryCleanupSkipsTheWatchLockWhenNothingIsRemembered(t *testing.T) {
	tracker := &destStateTracker{}
	tracker.watchMu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		tracker.cleanupCollectedHistory()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		tracker.watchMu.Unlock()
		<-done
		t.Fatal("with no connection of a Collect set remembered, the history cleanup still waited for the watch lock")
	}
	tracker.watchMu.Unlock()

	tracker.WatchHello(historyWatch("flow", 71))
	tracker.Unwatch("flow")
	old := lookupFlow
	t.Cleanup(func() { lookupFlow = old })
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return ctnl.Flow{}, false, nil }
	tracker.cleanupCollectedHistory()
	if tracker.collectHistory.order.Len() != 0 {
		t.Fatal("a remembered connection that closed was not cleaned up")
	}
}
