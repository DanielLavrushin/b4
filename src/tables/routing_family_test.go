package tables

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func familyTestConfig(ipv4, ipv6 bool) *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = ipv4
	cfg.Queue.IPv6Enabled = ipv6
	return &cfg
}

func familyTestSet() *config.SetConfig {
	s := config.NewSetConfig()
	s.Id = "famtest"
	s.Name = "famtest"
	s.Enabled = true
	s.Routing.Enabled = true
	s.Routing.Mode = config.RoutingModeInterface
	s.Routing.EgressInterface = "b4fam0"
	s.Routing.FWMark = 0x7e10
	s.Routing.Table = 232
	s.Targets.IpsToMatch = []string{"198.51.100.7", "2001:db8::7"}
	return &s
}

func familyResetGlobals(t *testing.T) {
	t.Helper()
	engine, cache, auto := routeEngine, routeRuleCache, routeIfaceAuto
	logged, delRule := runLogged, routeDelRuleLoop
	t.Cleanup(func() {
		routeEngine = engine
		routeRuleCache = cache
		routeIfaceAuto = auto
		runLogged = logged
		routeDelRuleLoop = delRule
	})
	routeEngine = nil
	routeRuleCache = make(map[string]routeState)
	routeIfaceAuto = make(map[string]routeState)
	runLogged = func(op string, args ...string) {}
	routeDelRuleLoop = func(ipv6 bool, mark, table string) {}
}

func routeFamilyCountOps(ops []string, want string) int {
	n := 0
	for _, op := range ops {
		if op == want {
			n++
		}
	}
	return n
}

func TestRouteStateEqual_AddressFamilyIsPartOfTheState(t *testing.T) {
	familyResetGlobals(t)
	base := buildRouteState(familyTestConfig(true, true), familyTestSet())

	cases := []struct {
		name   string
		mutate func(*routeState)
		why    string
	}{
		{"ipv6", func(st *routeState) { st.ipv6 = false }, "turning IPv6 off leaves every v6 set, mark rule, masquerade rule and ip rule installed"},
		{"ipv4", func(st *routeState) { st.ipv4 = false }, "turning IPv4 off leaves every v4 set, mark rule, masquerade rule and ip rule installed"},
		{"srcScoped", func(st *routeState) { st.srcScoped = !st.srcScoped }, "scoping a set to a source device leaves the OUTPUT jump the set no longer wants"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			other := base
			tc.mutate(&other)
			if routeStateEqual(base, other) {
				t.Errorf("routeStateEqual ignores %s, so the sync cache-hits and %s", tc.name, tc.why)
			}
		})
	}
}

func TestBuildRouteState_RecordsTheEnabledFamilies(t *testing.T) {
	familyResetGlobals(t)
	set := familyTestSet()

	both := buildRouteState(familyTestConfig(true, true), set)
	if !both.ipv4 || !both.ipv6 {
		t.Errorf("state built with both families enabled = ipv4:%v ipv6:%v", both.ipv4, both.ipv6)
	}

	v4only := buildRouteState(familyTestConfig(true, false), set)
	if !v4only.ipv4 || v4only.ipv6 {
		t.Errorf("state built with IPv6 disabled = ipv4:%v ipv6:%v", v4only.ipv4, v4only.ipv6)
	}

	v6only := buildRouteState(familyTestConfig(false, true), set)
	if v6only.ipv4 || !v6only.ipv6 {
		t.Errorf("state built with IPv4 disabled = ipv4:%v ipv6:%v", v6only.ipv4, v6only.ipv6)
	}
}

func TestRoutingSyncConfig_FlippingIPv6RebuildsTheSetRules(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("RoutingSyncConfig gives up before it touches a set when the ip binary is missing")
	}
	familyResetGlobals(t)

	set := familyTestSet()
	setV4, setV6 := routeBuildSetNames(set.Id)
	chainPre, chainOut, chainSNAT := routeBuildChainNames(set.Id)
	markOp := "mark 0x7e10"

	sync := func(ipv6 bool) (*mockRouteBackend, map[string][]string) {
		pushed := map[string][]string{}
		be := &mockRouteBackend{addElementsFn: func(name string, ips []string, ttlSec int) {
			pushed[name] = append(pushed[name], ips...)
		}}
		routeEngine = be
		cfg := familyTestConfig(true, ipv6)
		cfg.Sets = []*config.SetConfig{set}
		RoutingSyncConfig(cfg)
		return be, pushed
	}

	on, pushedOn := sync(true)
	if len(on.deletedJumps) != 0 {
		t.Fatalf("the first sync installs the set for the first time and must tear nothing down: %+v", on.deletedJumps)
	}
	if n := routeFamilyCountOps(on.chainOps[chainPre], markOp); n != 2 {
		t.Fatalf("expected a mark rule per family in %s, got %d (%v)", chainPre, n, on.chainOps[chainPre])
	}
	if len(pushedOn[setV6]) == 0 {
		t.Fatalf("with IPv6 enabled the v6 members must reach %s, got %v", setV6, pushedOn)
	}
	if !routeRuleCache[set.Id].ipv6 {
		t.Fatal("the cached state does not record that it was built with IPv6 enabled")
	}

	off, pushedOff := sync(false)
	for _, want := range []struct{ base, chain string }{
		{"PREROUTING", chainPre},
		{"OUTPUT", chainOut},
		{"POSTROUTING", chainSNAT},
	} {
		if !off.hasDeletedJump(want.base, want.chain) {
			t.Errorf("disabling IPv6 never removed the %s jump to %s, so the v6 rules under it keep marking traffic: %+v", want.base, want.chain, off.deletedJumps)
		}
	}
	if !off.hasJump("PREROUTING", chainPre) {
		t.Errorf("the set was torn down but never rebuilt, so it stopped routing altogether: %+v", off.jumps)
	}
	if n := routeFamilyCountOps(off.chainOps[chainPre], markOp); n != 1 {
		t.Errorf("expected only the IPv4 mark rule in %s after disabling IPv6, got %d (%v)", chainPre, n, off.chainOps[chainPre])
	}
	if len(pushedOff[setV6]) != 0 {
		t.Errorf("with IPv6 disabled %s is never created, so pushing %v into it can only fail", setV6, pushedOff[setV6])
	}
	if len(pushedOff[setV4]) == 0 {
		t.Errorf("the IPv4 members must still be pushed into %s after the rebuild", setV4)
	}
	if routeRuleCache[set.Id].ipv6 {
		t.Error("the cached state still claims IPv6, so the next sync would cache-hit and never rebuild")
	}

	back, pushedBack := sync(true)
	if !back.hasDeletedJump("PREROUTING", chainPre) {
		t.Errorf("re-enabling IPv6 never rebuilt the set, so no v6 rule would ever appear without a restart: %+v", back.deletedJumps)
	}
	if len(pushedBack[setV6]) == 0 {
		t.Errorf("re-enabling IPv6 left %s without its members, got %v", setV6, pushedBack)
	}
}
