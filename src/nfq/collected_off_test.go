package nfq

import (
	"context"
	"errors"
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

func expectTheSavedConfigInstalledOnce(t *testing.T, r *lifetimeRig, installs int, at time.Duration) {
	t.Helper()
	changed := 0
	r.p.collectedChanged = func(*sni.SuffixSet) { changed++ }
	matcher := r.w.getMatcher()
	r.sweep(at)
	if r.installs != installs+1 {
		t.Fatalf("an install that a save superseded can write the old view after the save's own rules, but the next pass did not install the saved config again: installs %d -> %d", installs, r.installs)
	}
	view := r.views[len(r.views)-1]
	if view != r.p.collectedBase {
		t.Error("the pass installed another config than the saved one")
	}
	for _, set := range view.Sets {
		if len(set.Targets.CollectedIPs) > 0 {
			t.Errorf("the installed config still gives set %q the collected addresses %v", set.Id, set.Targets.CollectedIPs)
		}
	}
	if r.w.getMatcher() != matcher || changed != 0 {
		t.Errorf("installing the saved config again rebuilt the matcher the save had already built: matcher kept=%v, resolver updates=%d", r.w.getMatcher() == matcher, changed)
	}
	r.sweep(at + time.Second)
	if r.installs != installs+1 {
		t.Errorf("the saved config was installed again on the pass after: installs %d -> %d", installs+1, r.installs)
	}
}

func TestAnInstallASaveSupersedesIsRepairedOnTheNextPass(t *testing.T) {
	r := newLifetimeRig(t, nil)
	r.collect(t)
	saved := false
	r.p.collectedInstall = func(_ context.Context, view *config.Config, current func() bool) error {
		r.installs++
		r.views = append(r.views, view)
		if !current() {
			return errors.New("superseded")
		}
		if !saved {
			saved = true
			saveCollectedFeaturesOff(t, r)
		}
		return nil
	}
	installs := r.installs
	r.expireAt(6 * time.Second)
	r.sweep(6 * time.Second)
	r.p.configMu.Lock()
	dirty := r.p.collectedDirty
	r.p.configMu.Unlock()
	if !saved || r.installs != installs+1 || !dirty || r.collected() != 0 {
		t.Fatalf("setup: the expiry's install must run and a save must supersede it after its first step: saved=%v installs %d -> %d dirty=%v pooled=%d", saved, installs, r.installs, dirty, r.collected())
	}

	expectTheSavedConfigInstalledOnce(t, r, installs+1, 7*time.Second)
}

func TestARollbackASaveSupersedesIsRepairedOnTheNextPass(t *testing.T) {
	for name, rollback := range map[string]error{
		"the rollback failed":   errors.New("kernel refused the rollback"),
		"the rollback finished": nil,
	} {
		t.Run(name, func(t *testing.T) {
			r := newLifetimeRig(t, nil)
			r.collect(t)
			calls := 0
			r.p.collectedInstall = func(_ context.Context, view *config.Config, current func() bool) error {
				r.installs++
				r.views = append(r.views, view)
				calls++
				if !current() {
					return errors.New("superseded")
				}
				switch calls {
				case 1:
					return errors.New("kernel refused the update")
				case 2:
					saveCollectedFeaturesOff(t, r)
					return rollback
				}
				return nil
			}
			installs := r.installs
			r.expireAt(6 * time.Second)
			r.sweep(6 * time.Second)
			if calls != 2 || r.installs != installs+2 {
				t.Fatalf("setup: the expiry's install must fail after its first step and its rollback must run, with a save during the rollback: calls=%d installs %d -> %d", calls, installs, r.installs)
			}

			expectTheSavedConfigInstalledOnce(t, r, installs+2, 7*time.Second)
		})
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
