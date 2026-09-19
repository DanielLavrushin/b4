package tables

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestResolvedIPsAreDroppedWhenTheSetTargetsChanged(t *testing.T) {
	familyResetGlobals(t)
	pushed := map[string][]string{}
	routeEngine = &mockRouteBackend{addElementsFn: func(name string, ips []string, ttlSec int) {
		pushed[name] = append(pushed[name], ips...)
	}}
	set := familyTestSet()
	set.Targets.SNIDomains = []string{"a.example"}
	cfg := familyTestConfig(true, false)
	ips := []net.IP{net.ParseIP("203.0.113.9")}

	routeMu.Lock()
	routeRuleCache[set.Id] = routeState{setV4: "b4r_famtest_v4", ipv4: true, set: set}
	routeMu.Unlock()

	if !routeAddResolvedIPs(cfg, set, ips) {
		t.Fatalf("addresses for the set's current targets were refused")
	}
	if got := len(pushed["b4r_famtest_v4"]); got != 1 {
		t.Fatalf("addresses for the current targets were not written, got %d entries", got)
	}

	republished := *set
	republished.Targets.SNIDomains = []string{"A.example "}
	if !routeAddResolvedIPs(cfg, &republished, ips) {
		t.Fatalf("addresses from a republished copy of the same targets were refused")
	}
	if got := len(pushed["b4r_famtest_v4"]); got != 2 {
		t.Fatalf("addresses from an unchanged republished set were not written, got %d entries", got)
	}

	edited := *set
	edited.Targets.SNIDomains = []string{"b.example"}
	routeMu.Lock()
	st := routeRuleCache[set.Id]
	st.set = &edited
	routeRuleCache[set.Id] = st
	routeMu.Unlock()

	if routeAddResolvedIPs(cfg, set, ips) {
		t.Fatalf("addresses resolved for the old targets were accepted after the targets changed")
	}
	if got := len(pushed["b4r_famtest_v4"]); got != 2 {
		t.Fatalf("addresses resolved for the old targets were written, got %d entries", got)
	}

	if !routeAddResolvedIPs(cfg, &edited, ips) {
		t.Fatalf("addresses for the new targets were refused")
	}
	if got := len(pushed["b4r_famtest_v4"]); got != 3 {
		t.Fatalf("addresses for the new targets were not written, got %d entries", got)
	}
}

func TestResolvedIPsForAnUninstalledSetAreRefused(t *testing.T) {
	familyResetGlobals(t)
	routeEngine = &mockRouteBackend{}
	set := familyTestSet()
	cfg := familyTestConfig(true, false)
	if routeAddResolvedIPs(cfg, set, []net.IP{net.ParseIP("203.0.113.9")}) {
		t.Fatalf("addresses for a set that is no longer installed were accepted")
	}
}

func TestSameResolveTargets(t *testing.T) {
	a := config.NewSetConfig()
	a.Targets.SNIDomains = []string{"a.example", "b.example"}
	b := a
	b.Targets.SNIDomains = []string{"B.example", " a.example"}
	if !routeSameResolveTargets(&a, &b) {
		t.Fatalf("the same domains in another order and case were treated as a change")
	}
	c := a
	c.Targets.SNIDomains = []string{"a.example", "c.example"}
	if routeSameResolveTargets(&a, &c) {
		t.Fatalf("a replaced domain was not treated as a change")
	}
	d := a
	d.Targets.DomainOnly = true
	if routeSameResolveTargets(&a, &d) {
		t.Fatalf("a domain-only flip was not treated as a change")
	}
	e := a
	e.Targets.GeoSiteCategories = []string{"category"}
	if !routeSameResolveTargets(&a, &e) {
		t.Fatalf("a category edit, which changes nothing that gets resolved, was treated as a change")
	}
	f := a
	f.Targets.SNIDomains = []string{"a.example", "a.example"}
	if routeSameResolveTargets(&a, &f) {
		t.Fatalf("a duplicated domain replacing another was not treated as a change")
	}
}

func TestFailedRoutingSyncIsRetriedByTheMonitor(t *testing.T) {
	familyResetGlobals(t)
	fail := true
	routeEngine = &mockRouteBackend{ensureBaseFn: func() error {
		if fail {
			return errTestClear
		}
		return nil
	}}
	set := familyTestSet()
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}
	routeMu.Lock()
	prevSynced, prevRetry := routeSyncedCfg, routeSyncRetry
	routeSyncedCfg, routeSyncRetry = nil, nil
	routeMu.Unlock()
	t.Cleanup(func() {
		routeMu.Lock()
		routeSyncedCfg, routeSyncRetry = prevSynced, prevRetry
		routeMu.Unlock()
	})

	earlier := familyTestConfig(true, false)
	routeMu.Lock()
	routeSyncedCfg = earlier
	routeMu.Unlock()

	RoutingSyncConfig(cfg)
	if routingSyncedConfig() != earlier {
		t.Fatalf("a sync that failed at its base was recorded as the last completed sync")
	}
	if routingSyncRetryConfig() != cfg {
		t.Fatalf("a sync that failed at its base was not queued for retry")
	}

	m, ptr := newLockTestMonitor(t)
	ptr.Store(cfg)
	if !m.reconcileRouting(false) {
		t.Fatalf("a tick with a failed retry read as nothing to do")
	}
	if routingSyncRetryConfig() != cfg {
		t.Fatalf("a failed retry lost the pending config, got %v", routingSyncRetryConfig())
	}
	if routingSyncedConfig() != earlier {
		t.Fatalf("a failed retry moved the last completed sync")
	}

	fail = false
	if !m.reconcileRouting(false) {
		t.Fatalf("the monitor did not report the retried sync")
	}
	if routingSyncRetryConfig() != nil {
		t.Fatalf("the retry was left queued after it succeeded")
	}
	if routingSyncedConfig() != cfg {
		t.Fatalf("the retried sync was not recorded as the last completed sync")
	}
	routeMu.Lock()
	_, installed := routeRuleCache[set.Id]
	routeMu.Unlock()
	if !installed {
		t.Fatalf("the retried sync did not install the set")
	}
}

func TestSyncKeepsTheStateButFollowsTheSetOfAnUnchangedSet(t *testing.T) {
	familyResetGlobals(t)
	routeEngine = &mockRouteBackend{}
	set := familyTestSet()
	set.Targets.GeoSiteCategories = []string{"category-a"}
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}
	RoutingSyncConfig(cfg)

	routeMu.Lock()
	first, ok := routeRuleCache[set.Id]
	routeMu.Unlock()
	if !ok {
		t.Fatalf("set was not installed by the sync")
	}
	if first.set != set {
		t.Fatalf("installed state does not point at the set it was built from")
	}

	edited := *set
	edited.Targets.GeoSiteCategories = []string{"category-b"}
	cfg2 := familyTestConfig(true, false)
	cfg2.Sets = []*config.SetConfig{&edited}
	RoutingSyncConfig(cfg2)

	routeMu.Lock()
	second := routeRuleCache[set.Id]
	routeMu.Unlock()
	if second.set != &edited {
		t.Fatalf("a save that kept the routing state left the state pointing at the previous set")
	}
	if !routeStateEqual(first, second) {
		t.Fatalf("an edit outside the routing settings rebuilt the routing state instead of keeping it")
	}
}
