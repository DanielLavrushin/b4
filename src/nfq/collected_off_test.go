package nfq

import (
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

func saveCollectedFeaturesOff(t *testing.T, r *lifetimeRig) {
	t.Helper()
	next := *r.p.collectedBase
	next.Sets = make([]*config.SetConfig, len(r.p.collectedBase.Sets))
	for i, set := range r.p.collectedBase.Sets {
		off := *set
		off.Targets.UseCollectedIPs = false
		if off.TCP.IPBlockDetect.Collects() {
			off.TCP.IPBlockDetect.Action = "block"
		}
		next.Sets[i] = &off
	}
	if err := r.p.UpdateConfig(&next); err != nil {
		t.Fatal(err)
	}
}

func TestTurningCollectedIPsOffLeavesTheFirewallAndMatcherToTheSave(t *testing.T) {
	r := newLifetimeRig(t, nil)
	r.collect(t)
	changed := 0
	r.p.collectedChanged = func(*sni.SuffixSet) { changed++ }

	saveCollectedFeaturesOff(t, r)
	installs, matcher := r.installs, r.w.getMatcher()
	r.sweep(6 * time.Second)
	r.expireAt(7 * time.Second)
	r.sweep(7 * time.Second)

	if r.installs != installs || r.w.getMatcher() != matcher || changed != 0 {
		t.Errorf("with no set using collected IPs any more, the pool's leftovers still ran the collected install and rebuilt the matcher: installs %d -> %d, matcher kept=%v, resolver updates=%d", installs, r.installs, r.w.getMatcher() == matcher, changed)
	}
	if r.collected() != 0 || len(r.p.publishedCollected()) != 0 || len(r.p.GetCollectedIPs()) != 0 {
		t.Errorf("the expired address was not cleared: pooled=%d published=%v listed=%d", r.collected(), r.p.publishedCollected(), len(r.p.GetCollectedIPs()))
	}
	r.p.configMu.Lock()
	dirty := r.p.collectedDirty
	r.p.configMu.Unlock()
	if dirty || !r.p.collectedQuiet.Load() {
		t.Errorf("the drained pool did not settle: dirty=%v quiet=%v", dirty, r.p.collectedQuiet.Load())
	}
}

func TestTurningCollectedIPsBackOnInstallsThePool(t *testing.T) {
	r := newLifetimeRig(t, nil)
	r.collect(t)
	on := r.p.collectedBase
	saveCollectedFeaturesOff(t, r)
	r.sweep(6 * time.Second)
	installs := r.installs
	n := *on
	if err := r.p.UpdateConfig(&n); err != nil {
		t.Fatal(err)
	}
	r.sweep(7 * time.Second)
	if r.installs != installs+1 || len(r.lastView(t)) != 1 || !r.consumerMatches("1.2.3.4") {
		t.Fatalf("turning Use collected IPs back on did not install the address still pooled: installs %d -> %d, view=%v", installs, r.installs, r.lastView(t))
	}
}
