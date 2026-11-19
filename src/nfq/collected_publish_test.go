package nfq

import (
	"context"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/iphealth"
)

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
