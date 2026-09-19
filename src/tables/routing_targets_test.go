package tables

import (
	"net"
	"testing"
	"time"

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
	routeRuleCache[set.Id] = routeState{setV4: "b4r_famtest_v4", ipv4: true, targetsKey: routeTargetsKey(set)}
	routeMu.Unlock()

	if !routeAddResolvedIPs(cfg, set, ips) {
		t.Fatalf("addresses for the set's current targets were refused")
	}
	if got := len(pushed["b4r_famtest_v4"]); got != 1 {
		t.Fatalf("addresses for the current targets were not written, got %d entries", got)
	}

	edited := *set
	edited.Targets.SNIDomains = []string{"b.example"}
	routeMu.Lock()
	st := routeRuleCache[set.Id]
	st.targetsKey = routeTargetsKey(&edited)
	routeRuleCache[set.Id] = st
	routeLastReResolve[set.Id] = time.Now()
	routeMu.Unlock()

	if routeAddResolvedIPs(cfg, set, ips) {
		t.Fatalf("addresses resolved for the old targets were accepted after the targets changed")
	}
	if got := len(pushed["b4r_famtest_v4"]); got != 1 {
		t.Fatalf("addresses resolved for the old targets were written, got %d entries", got)
	}
	routeMu.Lock()
	_, stamped := routeLastReResolve[set.Id]
	routeMu.Unlock()
	if stamped {
		t.Fatalf("a refused write kept the re-resolve stamp, so the new targets would wait a full interval")
	}

	if !routeAddResolvedIPs(cfg, &edited, ips) {
		t.Fatalf("addresses for the new targets were refused")
	}
	if got := len(pushed["b4r_famtest_v4"]); got != 2 {
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

func TestSyncRefreshesTheTargetsKeyOfAnUnchangedSet(t *testing.T) {
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
	if first.targetsKey != routeTargetsKey(set) {
		t.Fatalf("installed state does not carry the set's targets key")
	}

	edited := *set
	edited.Targets.GeoSiteCategories = []string{"category-b"}
	cfg2 := familyTestConfig(true, false)
	cfg2.Sets = []*config.SetConfig{&edited}
	RoutingSyncConfig(cfg2)

	routeMu.Lock()
	second := routeRuleCache[set.Id]
	routeMu.Unlock()
	if second.targetsKey == first.targetsKey {
		t.Fatalf("a targets-only edit left the old targets key on the kept routing state")
	}
	if !routeStateEqual(first, second) {
		t.Fatalf("a targets-only edit rebuilt the routing state instead of keeping it")
	}
	if second.targetsKey != routeTargetsKey(&edited) {
		t.Fatalf("kept routing state carries a key other than the edited set's")
	}
}
