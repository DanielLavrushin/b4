package nfq

import (
	"context"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func collectedConsumerSet() config.SetConfig {
	consumer := newPassiveSet()
	consumer.Id, consumer.Name = "consumer", "consumer"
	consumer.Targets.DomainOnly = false
	consumer.Targets.DomainsToMatch = nil
	consumer.Targets.UseCollectedIPs = true
	return consumer
}

func collectedTestPool(t *testing.T, w *Worker, install func(context.Context, *config.Config) error) *Pool {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p := &Pool{Workers: []*Worker{w}, state: &runtimeState{tlsCache: w.tlsCache, destState: w.destState}, collectedBase: w.getConfig(), collectedCtx: ctx, collectedCancel: cancel, collectedInstall: install}
	w.pool = p
	return p
}

func TestASaveWithoutCollectedSetsRunsNoCollectedInstall(t *testing.T) {
	for _, tc := range []struct {
		name      string
		consumer  bool
		mode      string
		skip      bool
		discovery bool
		want      int
	}{
		{name: "nfqueue-plain", want: 0},
		{name: "tun-plain", mode: "tun", want: 0},
		{name: "saved-tun-consumer", consumer: true, mode: "tun", want: 0},
		{name: "skip_setup-consumer", consumer: true, skip: true, want: 0},
		{name: "discovery-consumer", consumer: true, discovery: true, want: 0},
		{name: "nfqueue-consumer", consumer: true, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := newPassiveSet()
			if tc.consumer {
				set = collectedConsumerSet()
			}
			w := newInjectingWorker(t, &set)
			calls := 0
			p := collectedTestPool(t, w, func(context.Context, *config.Config) error { calls++; return nil })
			n := *w.getConfig()
			next := &n
			next.Queue.Mode = tc.mode
			next.System.Tables.SkipSetup = tc.skip
			next.Queue.IsDiscovery = tc.discovery
			if err := p.UpdateConfig(next); err != nil {
				t.Fatal(err)
			}
			before := w.getMatcher()
			p.reconcileCollected()
			if calls != tc.want {
				t.Fatalf("installer calls=%d want %d, matcher kept=%v", calls, tc.want, w.getMatcher() == before)
			}
			if tc.want == 0 && w.getMatcher() != before {
				t.Fatal("the reconcile rebuilt the matcher for a save that touches no collected IPs")
			}
		})
	}
}

func TestATUNEngineNeverReconcilesCollected(t *testing.T) {
	set := collectedConsumerSet()
	w := newInjectingWorker(t, &set)
	calls := 0
	p := collectedTestPool(t, w, func(context.Context, *config.Config) error { calls++; return nil })
	p.collectedEngineTUN = true
	n := *w.getConfig()
	next := &n
	next.Queue.Mode = ""
	if err := p.UpdateConfig(next); err != nil {
		t.Fatal(err)
	}
	before := w.getMatcher()
	p.reconcileCollected()
	if calls != 0 || w.getMatcher() != before {
		t.Fatalf("a TUN engine reconciled collected IPs after a save to nfqueue: installer calls=%d, matcher kept=%v", calls, w.getMatcher() == before)
	}
}
