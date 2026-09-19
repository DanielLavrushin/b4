package tables

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestResolvedIPsAreDroppedWhenTheSetTargetsChanged(t *testing.T) {
	familyResetGlobals(t)
	pushed := map[string][]string{}
	var ttls []int
	routeEngine = &mockRouteBackend{addElementsFn: func(name string, ips []string, ttlSec int) {
		pushed[name] = append(pushed[name], ips...)
		ttls = append(ttls, ttlSec)
	}}
	set := familyTestSet()
	set.Targets.SNIDomains = []string{"a.example"}
	set.Routing.IPTTLSeconds = 600
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
	republished.Routing.IPTTLSeconds = 60
	if !routeAddResolvedIPs(cfg, &republished, ips) {
		t.Fatalf("addresses from a republished copy of the same targets were refused")
	}
	if got := len(pushed["b4r_famtest_v4"]); got != 2 {
		t.Fatalf("addresses from an unchanged republished set were not written, got %d entries", got)
	}
	if ttls[len(ttls)-1] != set.Routing.IPTTLSeconds {
		t.Fatalf("addresses from a republished set were written with the snapshot's TTL %d instead of the installed set's %d", ttls[len(ttls)-1], set.Routing.IPTTLSeconds)
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

func stubRetryState(t *testing.T) {
	t.Helper()
	routeMu.Lock()
	prevSynced, prevRetry, prevBase := routeSyncedCfg, routeSyncRetry, routeSyncRetryBase
	routeClearSyncRetry()
	routeSyncedCfg = nil
	routeSyncRetryBase = 20 * time.Millisecond
	routeMu.Unlock()
	t.Cleanup(func() {
		routeMu.Lock()
		routeClearSyncRetry()
		routeSyncedCfg, routeSyncRetry, routeSyncRetryBase = prevSynced, prevRetry, prevBase
		routeMu.Unlock()
	})
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestFailedRoutingSyncRetriesOnItsOwnWithBackoff(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	var fail atomic.Bool
	fail.Store(true)
	routeEngine = &mockRouteBackend{ensureBaseFn: func() error {
		if fail.Load() {
			return errTestClear
		}
		return nil
	}}
	set := familyTestSet()
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}
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

	waitUntil(t, "a second failed attempt to back off", func() bool {
		routeMu.Lock()
		defer routeMu.Unlock()
		return routeSyncRetryDelay > routeSyncRetryBase
	})
	if routingSyncRetryConfig() != cfg || routingSyncedConfig() != earlier {
		t.Fatalf("a failed retry lost the pending config or moved the last completed sync")
	}

	fail.Store(false)
	waitUntil(t, "the retry to succeed", func() bool {
		return routingSyncRetryConfig() == nil && routingSyncedConfig() == cfg
	})
	routeMu.Lock()
	_, installed := routeRuleCache[set.Id]
	routeMu.Unlock()
	if !installed {
		t.Fatalf("the retried sync did not install the set")
	}
}

func TestSetThatFailsToInstallKeepsTheSyncQueued(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	var fail atomic.Bool
	fail.Store(true)
	routeEngine = &mockRouteBackend{ensureChainFn: func(string, bool) error {
		if fail.Load() {
			return errTestClear
		}
		return nil
	}}
	set := familyTestSet()
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}

	RoutingSyncConfig(cfg)
	if routingSyncedConfig() != cfg {
		t.Fatalf("a partially applied sync was not recorded as the state the cache is built from")
	}
	if routingSyncRetryConfig() != cfg {
		t.Fatalf("a set that failed to install did not keep the sync queued for retry")
	}

	fail.Store(false)
	waitUntil(t, "the retry to install the set", func() bool {
		routeMu.Lock()
		defer routeMu.Unlock()
		_, installed := routeRuleCache[set.Id]
		return installed && routeSyncRetry == nil
	})
}

func TestMonitorLeavesRoutingAloneWhileARetryIsQueued(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	m, ptr := newLockTestMonitor(t)
	published := routedTestConfig("b4test0")
	ptr.Store(published)
	routeMu.Lock()
	routeSyncRetry = published
	routeMu.Unlock()

	if m.reconcileRouting(false) {
		t.Fatalf("routing phase reported work while a newer config was queued for retry")
	}
	if _, tracked := m.ifaceState["b4test0"]; tracked {
		t.Fatalf("routing phase reconciled a config whose sync is still queued for retry")
	}

	routeMu.Lock()
	routeSyncedCfg = published
	routeMu.Unlock()
	m.reconcileRouting(false)
	if _, tracked := m.ifaceState["b4test0"]; !tracked {
		t.Fatalf("routing phase stayed switched off although the queued retry is for the config that is already installed")
	}
}

func TestRetryBackoffOnlyGrowsOnTheTimersOwnAttempts(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	routeEngine = &mockRouteBackend{ensureBaseFn: func() error { return errTestClear }}
	set := familyTestSet()
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}

	RoutingSyncConfig(cfg)
	RoutingSyncConfig(cfg)
	routeMu.Lock()
	delay := routeSyncRetryDelay
	routeMu.Unlock()
	if delay != routeSyncRetryBase {
		t.Fatalf("a sync driven by a save or link event doubled the retry delay to %v", delay)
	}
	waitUntil(t, "the timer's own retry to back off", func() bool {
		routeMu.Lock()
		defer routeMu.Unlock()
		return routeSyncRetryDelay > routeSyncRetryBase
	})
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
