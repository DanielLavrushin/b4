package nfq

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/iphealth"
)

type blockedInstall struct {
	mu      sync.Mutex
	views   []*config.Config
	current func() bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockedInstall(t *testing.T) *blockedInstall {
	b := &blockedInstall{entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(b.free)
	return b
}

func (b *blockedInstall) free() { b.once.Do(func() { close(b.release) }) }

func (b *blockedInstall) install(_ context.Context, view *config.Config, current func() bool) error {
	b.mu.Lock()
	b.views = append(b.views, view)
	first := len(b.views) == 1
	if first {
		b.current = current
	}
	b.mu.Unlock()
	if !current() {
		return errors.New("superseded")
	}
	if first {
		close(b.entered)
		<-b.release
	}
	return nil
}

func (b *blockedInstall) installed() []*config.Config {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.views)
}

func (b *blockedInstall) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-b.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the pass never reached the collected installer, so this test proves nothing")
	}
}

func TestCollectInstallDoesNotHoldConfigWrites(t *testing.T) {
	w, p, sent := collectedRig(t, func(context.Context, *config.Config) error { return nil })
	b := newBlockedInstall(t)
	p.collectedInstall = b.install
	w.ProcessPacket(silentHelloFrom(42000))
	if !w.destState.Watching() {
		t.Fatal("missing TLS watch, so this test proves nothing")
	}
	start := time.Now()
	passed := make(chan struct{})
	go func() {
		defer close(passed)
		sweepAcknowledgedCollectedHellos(w, start.Add(4*time.Second))
	}()
	b.waitEntered(t)

	if !returnsWithin(100*time.Millisecond, func() { config.LockWrites()() }) {
		t.Fatal("a config write waited for the collected install")
	}
	if !returnsWithin(100*time.Millisecond, func() { p.GetCollectedIPs() }) {
		t.Fatal("reading the collected pool waited for the collected install")
	}
	p.configMu.Lock()
	n := *p.collectedBase
	p.configMu.Unlock()
	next := &n
	next.System.Logging.Instaflush = !next.System.Logging.Instaflush
	if !returnsWithin(100*time.Millisecond, func() { _ = p.UpdateConfig(next) }) {
		t.Fatal("a save waited for the collected install")
	}
	b.mu.Lock()
	current, stale := b.current, b.views[0]
	b.mu.Unlock()
	if current() {
		t.Fatal("the install in flight still reports its view current after a save")
	}
	b.free()
	<-passed

	if got := w.getConfig(); got == stale || got.System.Logging.Instaflush != next.System.Logging.Instaflush {
		t.Fatal("the workers hold the view the save superseded")
	}
	if ports := resetsTo(sent()); len(ports) != 0 {
		t.Fatalf("a superseded install reset clients %v", ports)
	}
	p.configMu.Lock()
	dirty := p.collectedDirty
	_, pooled := p.collected[netip.MustParseAddr("1.2.3.4")]
	p.configMu.Unlock()
	if !dirty || !pooled || len(p.publishedCollected()) != 0 {
		t.Fatalf("a superseded install must keep the address for the next pass: dirty=%v pooled=%v published=%v", dirty, pooled, p.publishedCollected())
	}

	sweepAcknowledgedCollectedHellos(w, start.Add(6*time.Second))
	views := b.installed()
	if len(views) != 2 || views[1].System.Logging.Instaflush != next.System.Logging.Instaflush || !slices.Equal(views[1].Sets[0].Targets.CollectedIPs, []string{"1.2.3.4"}) {
		t.Fatalf("the next pass must install the saved config with the address: installs=%d", len(views))
	}
	if !slices.Equal(p.publishedCollected(), []string{"1.2.3.4"}) {
		t.Fatalf("the next pass did not publish the address: %v", p.publishedCollected())
	}
	if ports := resetsTo(sent()); !slices.Equal(ports, []uint16{42000}) {
		t.Fatalf("the parked connection must be reset by the pass that publishes its address, got resets to %v", ports)
	}
}

func TestAFailedCollectedInstallRollsBackOnlyAfterAChange(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changed bool
		want    int
	}{
		{name: "failed before any change", want: 1},
		{name: "failed after a change", changed: true, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, p, sent := collectedRig(t, func(context.Context, *config.Config) error { return nil })
			var views []*config.Config
			p.collectedInstall = func(_ context.Context, view *config.Config, current func() bool) error {
				views = append(views, view)
				if len(views) == 1 {
					if tc.changed && !current() {
						return errors.New("superseded")
					}
					return errors.New("kernel refused")
				}
				if !current() {
					return errors.New("superseded")
				}
				return nil
			}
			w.ProcessPacket(silentHelloFrom(42000))
			sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))
			if len(views) != tc.want {
				t.Fatalf("installer calls=%d want %d", len(views), tc.want)
			}
			if tc.changed && len(views[1].Sets[0].Targets.CollectedIPs) != 0 {
				t.Fatalf("the rollback must install the published view, got %v", views[1].Sets[0].Targets.CollectedIPs)
			}
			if len(p.collected) != 0 || len(p.publishedCollected()) != 0 || len(resetsTo(sent())) != 0 {
				t.Fatalf("a failed install must leave no candidate, publication or reset: candidates=%d published=%v resets=%v", len(p.collected), p.publishedCollected(), resetsTo(sent()))
			}
		})
	}
}

func TestClearingCollectedAddressesWaitsForThePassInFlight(t *testing.T) {
	w, p, sent := collectedRig(t, func(context.Context, *config.Config) error { return nil })
	b := newBlockedInstall(t)
	p.collectedInstall = b.install
	w.ProcessPacket(silentHelloFrom(42000))
	passed := make(chan struct{})
	go func() {
		defer close(passed)
		sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))
	}()
	b.waitEntered(t)
	cleared := make(chan int, 1)
	go func() {
		n, _ := p.ClearCollectedIPs("")
		cleared <- n
	}()
	select {
	case <-cleared:
		b.free()
		t.Fatal("clearing the pool ran while a collected pass was installing")
	case <-time.After(100 * time.Millisecond):
	}
	b.free()
	<-passed
	if n := <-cleared; n != 1 {
		t.Fatalf("clearing after the pass removed %d addresses, want the one it published", n)
	}
	if len(b.installed()) != 2 || len(p.publishedCollected()) != 0 || !slices.Equal(resetsTo(sent()), []uint16{42000}) {
		t.Fatalf("installs=%d published=%v resets=%v", len(b.installed()), p.publishedCollected(), resetsTo(sent()))
	}
}

func silentHelloTo(dst net.IP, sport uint16) []byte {
	pkt := silentHelloFrom(sport)
	copy(pkt[16:20], dst.To4())
	return pkt
}

func TestCollectPublishesOncePerPass(t *testing.T) {
	installs := 0
	var installed []string
	w, p, sent := unseededCollectedRig(t, func(_ context.Context, cfg *config.Config) error {
		installs++
		installed = cfg.Sets[0].Targets.CollectedIPs
		return nil
	})
	first, second := net.ParseIP("1.2.3.4"), net.ParseIP("1.2.3.5")
	seedEarlierSilentConnection(w, first, 443)
	seedEarlierSilentConnection(w, second, 443)
	w.ProcessPacket(silentHelloTo(first, 42000))
	w.ProcessPacket(silentHelloTo(second, 42001))
	w.destState.watchMu.RLock()
	watched := len(w.destState.watches)
	w.destState.watchMu.RUnlock()
	if watched != 2 {
		t.Fatalf("both ClientHellos must be watched, got %d watches, so this test proves nothing", watched)
	}

	sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))

	if installs != 1 || !slices.Equal(installed, []string{"1.2.3.4", "1.2.3.5"}) {
		t.Fatalf("two addresses that complete their count in one pass must be installed together: installs=%d last view %v", installs, installed)
	}
	if !slices.Equal(p.collectedPublished, []string{"1.2.3.4", "1.2.3.5"}) {
		t.Fatalf("both addresses must be published, got %v", p.collectedPublished)
	}
	if ports := resetsTo(sent()); !slices.Equal(ports, []uint16{42000, 42001}) {
		t.Fatalf("both silent connections must be reset after the one install, got resets to %v", ports)
	}
}

func TestNoCollectionWhileDiscoveryRuns(t *testing.T) {
	r := newCollectCountRig(t)
	paused := true
	r.p.SetCollectedPause(func() bool { return paused })
	n := *r.w.getConfig()
	if err := r.p.UpdateConfig(&n); err != nil {
		t.Fatal(err)
	}
	if !r.p.collectedDirty {
		t.Fatal("setup: a save with a consumer must leave the pool to reconcile")
	}

	r.silent(t, 42000)
	r.sweep(4 * time.Second)
	r.silent(t, 42001)
	r.sweep(5 * time.Second)

	if r.installs != 0 || r.collected() != 0 {
		t.Fatalf("nothing may be collected or installed while Discovery runs: candidates=%d installs=%d", r.collected(), r.installs)
	}
	if ports := resetsTo(r.sent()); len(ports) != 0 {
		t.Fatalf("no connection may be reset while Discovery runs, got resets to %v", ports)
	}
	if r.watched(42000) || r.watched(42001) {
		t.Fatal("a due connection must end while Discovery runs, or the deadline timer fires again at once")
	}
	r.w.destState.watchMu.RLock()
	run := r.w.destState.collectRuns[iphealth.BanKey(net.ParseIP("1.2.3.4"), 443)]
	r.w.destState.watchMu.RUnlock()
	if run.count != 0 {
		t.Fatalf("a connection that ended while Discovery runs must not be counted, count=%d", run.count)
	}
	if !r.p.collectedDirty {
		t.Fatal("the save's reconcile was dropped while Discovery ran")
	}

	paused = false
	r.sweep(6 * time.Second)
	if r.installs != 1 || r.p.collectedDirty {
		t.Fatalf("the first pass after Discovery must reconcile the save: installs=%d dirty=%v", r.installs, r.p.collectedDirty)
	}
	r.silent(t, 42002)
	r.sweep(10 * time.Second)
	r.silent(t, 42003)
	r.sweep(11 * time.Second)
	if r.collected() != 1 || r.installs != 2 {
		t.Fatalf("collection must resume once Discovery ends: candidates=%d installs=%d", r.collected(), r.installs)
	}
	if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{42002, 42003}) {
		t.Fatalf("both silent connections after Discovery must be reset, got resets to %v", ports)
	}
}

func TestACollectedCommitSettlesThePendingReconcileUnderItsOwnLock(t *testing.T) {
	set := collectedConsumerSet()
	w := newInjectingWorker(t, &set)
	p := collectedTestPool(t, w, func(context.Context, *config.Config) error { return nil })
	n := *w.getConfig()
	if err := p.UpdateConfig(&n); err != nil {
		t.Fatal(err)
	}
	p.configMu.Lock()
	p.collectedRetryWait, p.collectedRetryAt = collectedRetryFirst, time.Now().Add(time.Hour)
	dirty := p.collectedDirty
	p.configMu.Unlock()
	if !dirty {
		t.Fatal("setup: a save with a consumer must leave the pool to reconcile")
	}
	if r := p.publishCollected(false); r.outcome != collectedApplied {
		t.Fatalf("setup: the publish failed with outcome %d", r.outcome)
	}
	p.configMu.Lock()
	dirty, wait, at := p.collectedDirty, p.collectedRetryWait, p.collectedRetryAt
	p.configMu.Unlock()
	if dirty || wait != 0 || !at.IsZero() {
		t.Fatalf("the commit left the reconcile pending (dirty=%v wait=%v): a caller that clears it after the lock is released also clears the reconcile of a save that landed in between", dirty, wait)
	}
}
