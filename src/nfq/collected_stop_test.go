package nfq

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestStopCollectedWaitsForTheInstallInFlight(t *testing.T) {
	set := collectedConsumerSet()
	w := newInjectingWorker(t, &set)
	entered := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	p := collectedTestPool(t, w, func(context.Context, *config.Config) error {
		calls++
		if calls == 1 {
			close(entered)
			<-release
		}
		return nil
	})
	n := *w.getConfig()
	if err := p.UpdateConfig(&n); err != nil {
		t.Fatal(err)
	}
	p.configMu.Lock()
	p.collected = map[netip.Addr]collectedCandidate{netip.MustParseAddr("203.0.113.7"): {setID: "producer", at: time.Now()}}
	p.configMu.Unlock()
	reconciled := make(chan struct{})
	go func() {
		p.reconcileCollected(time.Now())
		close(reconciled)
	}()
	<-entered
	stopped := make(chan struct{})
	go func() {
		p.StopCollected()
		close(stopped)
	}()
	select {
	case <-stopped:
		close(release)
		<-reconciled
		t.Fatal("StopCollected returned while an install was running")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("StopCollected never returned")
	}
	<-reconciled
	p.configMu.Lock()
	p.collectedDirty = true
	p.configMu.Unlock()
	p.reconcileCollected(time.Now().Add(time.Hour))
	if calls != 1 {
		t.Fatalf("the collected installer ran after StopCollected: calls=%d", calls)
	}
}
