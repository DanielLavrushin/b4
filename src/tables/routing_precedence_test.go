package tables

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func precedenceSet(id, iface string, devices []string, ifaces []string, exclude bool) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Name = id
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeInterface
	set.Routing.EgressInterface = iface
	set.Routing.SourceInterfaces = ifaces
	set.Targets.SourceDevices = devices
	set.Targets.SourceDevicesExclude = exclude
	return &set
}

func withCachedSets(t *testing.T, sets ...*config.SetConfig) *config.Config {
	t.Helper()

	origCache := routeRuleCache
	t.Cleanup(func() { routeRuleCache = origCache })
	routeRuleCache = make(map[string]routeState)

	cfg := config.NewConfig()
	cfg.Sets = sets
	for _, set := range sets {
		routeRuleCache[set.Id] = routeState{
			mode:      config.RoutingModeInterface,
			iface:     set.Routing.EgressInterface,
			chainPre:  "b4r_" + set.Id + "_pre",
			chainOut:  "b4r_" + set.Id + "_out",
			chainSNAT: "b4r_" + set.Id + "_snat",
		}
	}
	return &cfg
}

func preJumpTargets(be *mockRouteBackend) []string {
	var out []string
	for _, j := range be.jumps {
		if j.baseChain == "PREROUTING" {
			out = append(out, j.targetChain)
		}
	}
	return out
}

func orderedIDs(cfg *config.Config) []string {
	var ids []string
	for _, set := range routeOrderedRoutingSets(cfg) {
		ids = append(ids, set.Id)
	}
	return ids
}

func TestNarrowerScopedSetsClaimAPacketFirst(t *testing.T) {
	everyone := precedenceSet("everyone", "eth0", nil, nil, false)
	phone := precedenceSet("phone", "eth0", []string{"AA:BB:CC:DD:EE:01"}, nil, false)

	cfg := withCachedSets(t, everyone, phone)

	got := orderedIDs(cfg)
	if len(got) != 2 || got[0] != "phone" {
		t.Errorf("order is %v; a set naming one device must claim the packet before a set that matches everything, whatever order they sit in the config", got)
	}
}

func TestScopeRankOrdersFromNarrowestToWidest(t *testing.T) {
	cfg := config.NewConfig()

	device := precedenceSet("device", "eth0", []string{"AA:BB:CC:DD:EE:01"}, nil, false)
	iface := precedenceSet("iface", "eth0", nil, []string{"br-guest"}, false)
	exclude := precedenceSet("exclude", "eth0", []string{"AA:BB:CC:DD:EE:02"}, nil, true)
	all := precedenceSet("all", "eth0", nil, nil, false)

	ranks := map[string]int{
		"device":  routeSetScopeRank(&cfg, device),
		"iface":   routeSetScopeRank(&cfg, iface),
		"exclude": routeSetScopeRank(&cfg, exclude),
		"all":     routeSetScopeRank(&cfg, all),
	}

	if !(ranks["device"] < ranks["iface"] && ranks["iface"] < ranks["exclude"] && ranks["exclude"] < ranks["all"]) {
		t.Errorf("scope ranks are %v, want device < interface < exclude-list < everything", ranks)
	}
}

func TestConfigOrderBreaksTiesWithinTheSameScope(t *testing.T) {
	first := precedenceSet("first", "eth0", nil, nil, false)
	second := precedenceSet("second", "eth0", nil, nil, false)

	cfg := withCachedSets(t, first, second)

	got := orderedIDs(cfg)
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Errorf("order is %v; sets of equal scope must keep the order the operator arranged them in", got)
	}
}

func TestJumpOrderDoesNotDependOnWhichSetWasEditedLast(t *testing.T) {
	phone := precedenceSet("phone", "eth0", []string{"AA:BB:CC:DD:EE:01"}, nil, false)
	laptop := precedenceSet("laptop", "eth0", []string{"AA:BB:CC:DD:EE:02"}, nil, false)
	everyone := precedenceSet("everyone", "eth0", nil, nil, false)

	cfg := withCachedSets(t, phone, laptop, everyone)
	before := orderedIDs(cfg)

	be := &mockRouteBackend{}
	routeReestablishJumpOrder(be, cfg, true)
	firstPass := preJumpTargets(be)

	be2 := &mockRouteBackend{}
	routeReestablishJumpOrder(be2, cfg, true)
	secondPass := preJumpTargets(be2)

	after := orderedIDs(cfg)
	if len(before) != len(after) {
		t.Fatalf("set count changed between passes: %v vs %v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("order changed between identical syncs: %v then %v", before, after)
		}
	}
	want := []string{"b4r_phone_pre", "b4r_laptop_pre", "b4r_everyone_pre"}
	if len(firstPass) != len(want) {
		t.Fatalf("prerouting jumps were %v, want one per set in precedence order %v", firstPass, want)
	}
	for i := range want {
		if firstPass[i] != want[i] {
			t.Fatalf("prerouting jump order is %v, want %v; the catch-all set must be jumped to last", firstPass, want)
		}
	}
	for i := range firstPass {
		if firstPass[i] != secondPass[i] {
			t.Fatalf("an identical second sync produced a different jump order: %v then %v", firstPass, secondPass)
		}
	}
	if before[len(before)-1] != "everyone" {
		t.Errorf("order is %v; the catch-all set must be evaluated last", before)
	}
}

func TestSingleRoutingSetNeedsNoReordering(t *testing.T) {
	only := precedenceSet("only", "eth0", nil, nil, false)
	cfg := withCachedSets(t, only)

	be := &mockRouteBackend{}
	routeReestablishJumpOrder(be, cfg, true)

	if len(be.jumps) != 0 {
		t.Errorf("a lone routing set had its jumps rewritten for nothing: %v", be.jumps)
	}
}
