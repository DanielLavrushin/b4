package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func scopedSet(devices []string, exclude bool, ifaces []string) *config.SetConfig {
	s := config.NewSetConfig()
	s.Id = "s1"
	s.Name = "S1"
	s.Targets.SourceDevices = devices
	s.Targets.SourceDevicesExclude = exclude
	s.Routing.SourceInterfaces = ifaces
	return &s
}

func TestRouteSetIsSourceScoped(t *testing.T) {
	cases := []struct {
		name string
		set  *config.SetConfig
		want bool
	}{
		{"nil set", nil, false},
		{"no scope at all", scopedSet(nil, false, nil), false},
		{"source device", scopedSet([]string{"AA:BB:CC:DD:EE:FF"}, false, nil), true},
		{"blank source device", scopedSet([]string{"  "}, false, nil), false},
		{"excluded devices are not a scope", scopedSet([]string{"AA:BB:CC:DD:EE:FF"}, true, nil), false},
		{"source interface", scopedSet(nil, false, []string{"br0"}), true},
		{"blank source interface", scopedSet(nil, false, []string{" "}), false},
		{"device and interface", scopedSet([]string{"AA:BB:CC:DD:EE:FF"}, false, []string{"br0"}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := routeSetIsSourceScoped(tc.set); got != tc.want {
				t.Errorf("routeSetIsSourceScoped = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRouteEnsureChainJumpsAlwaysKeepsTheOutputJump(t *testing.T) {
	st := routeState{chainPre: "b4r_x_pre", chainOut: "b4r_x_out", chainSNAT: "b4r_x_snat"}

	be := &mockRouteBackend{}
	routeEnsureChainJumps(be, st, routeDeviceGate{})
	if !be.hasJump("OUTPUT", st.chainOut) {
		t.Errorf("the packets b4 injects are locally generated, so without an OUTPUT jump they never regain the set's routing mark: %+v", be.jumps)
	}
}

func TestScopedSetKeepsInjectedRulesButNotRouterOriginatedOnes(t *testing.T) {
	cfg := config.NewConfig()
	st := routeState{mark: 0x1b1d, chainOut: "b4r_x_out", setV4: "b4r_x_v4", setV6: "b4r_x_v6"}

	scopedState := st
	scopedState.srcScoped = true
	scoped := &mockRouteBackend{}
	routeAddOutChainRules(scoped, &cfg, scopedState, routeDeviceGate{})

	if len(scoped.injected) == 0 {
		t.Error("a source-scoped set must still re-mark the packets b4 injects for it, or its fakes leave by the router's normal uplink")
	}
	for _, op := range scoped.chainOps[scopedState.chainOut] {
		if strings.HasPrefix(op, "mark ") {
			t.Errorf("a source-scoped set must not divert traffic the router itself originates, got %q", op)
		}
	}

	openState := st
	openState.routerOut = true
	open := &mockRouteBackend{}
	routeAddOutChainRules(open, &cfg, openState, routeDeviceGate{})

	found := false
	for _, op := range open.chainOps[st.chainOut] {
		if strings.HasPrefix(op, "mark ") {
			found = true
		}
	}
	if !found {
		t.Error("an unscoped set still routes the traffic the router originates to its own destinations")
	}
}

func TestRouteStateChainsCoverEverySetsChains(t *testing.T) {
	base := routeState{
		mode:      config.RoutingModeInterface,
		chainPre:  "b4r_x_pre",
		chainOut:  "b4r_x_out",
		chainSNAT: "b4r_x_snat",
	}

	chainsOf := func(st routeState) map[string]bool {
		out := map[string]bool{}
		for _, c := range routeStateChains(st) {
			out[c.chain] = true
		}
		return out
	}

	t.Run("an unscoped interface set still verifies its out chain", func(t *testing.T) {
		got := chainsOf(base)
		if !got["b4r_x_out"] || !got["b4r_x_pre"] || !got["b4r_x_snat"] {
			t.Errorf("expected all three chains, got %v", got)
		}
	})

	t.Run("a source-scoped interface set verifies its out chain too", func(t *testing.T) {
		st := base
		st.srcScoped = true
		got := chainsOf(st)
		if !got["b4r_x_out"] {
			t.Error("a scoped set's out chain carries the rules that re-mark the packets b4 injects, so a flushed chain must be noticed")
		}
		if !got["b4r_x_pre"] || !got["b4r_x_snat"] {
			t.Errorf("the other chains must still be verified, got %v", got)
		}
	})

	t.Run("buildRouteState records the scope", func(t *testing.T) {
		cfg := config.NewConfig()
		set := config.NewSetConfig()
		set.Id, set.Name = "s1", "S1"
		set.Routing.Mode = config.RoutingModeInterface
		if buildRouteState(&cfg, &set).srcScoped {
			t.Error("a set with no source scope must not be marked scoped")
		}
		set.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
		if !buildRouteState(&cfg, &set).srcScoped {
			t.Error("a set bound to a source device must be marked scoped")
		}
	})
}

func TestRouteWantsOutputJumpSplitsByMode(t *testing.T) {
	cases := []struct {
		name string
		st   routeState
		want bool
		why  string
	}{
		{
			name: "an unscoped interface set",
			st:   routeState{mode: config.RoutingModeInterface},
			want: true,
			why:  "it routes the traffic the router originates to its own destinations",
		},
		{
			name: "a source-scoped interface set",
			st:   routeState{mode: config.RoutingModeInterface, srcScoped: true},
			want: true,
			why:  "b4 injects its fakes from the router itself, and only the OUTPUT hook can give them the set's mark",
		},
		{
			name: "an unscoped proxy set",
			st:   routeState{mode: config.RoutingModeProxy},
			want: true,
			why:  "the router's own connections to the set's addresses still have to reach the tproxy listener",
		},
		{
			name: "a source-scoped proxy set",
			st:   routeState{mode: config.RoutingModeProxy, srcScoped: true},
			want: false,
			why:  "b4's listener handles its traffic, so the out chain stays empty and jumping to it would only flap against the next apply",
		},
		{
			name: "a source-scoped mtproto-ws set",
			st:   routeState{mode: config.RoutingModeMTProtoWS, srcScoped: true},
			want: false,
			why:  "it diverts through the same listener as a proxy set",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := routeWantsOutputJump(c.st); got != c.want {
				t.Errorf("routeWantsOutputJump = %v, want %v: %s", got, c.want, c.why)
			}
		})
	}
}

func TestKillSwitchSetsDoNotShareATableWithSetsThatWantNone(t *testing.T) {
	origAuto := routeIfaceAuto
	origCache := routeRuleCache
	t.Cleanup(func() { routeIfaceAuto = origAuto; routeRuleCache = origCache })
	routeIfaceAuto = make(map[string]routeState)
	routeRuleCache = make(map[string]routeState)

	prev := routeTableForeignRoutes
	routeTableForeignRoutes = func(int, string) bool { return false }
	t.Cleanup(func() { routeTableForeignRoutes = prev })

	cfg := config.NewConfig()

	held := &config.SetConfig{Id: "a", Name: "held"}
	held.Routing.Enabled = true
	held.Routing.Mode = config.RoutingModeInterface
	held.Routing.EgressInterface = "wg0"
	held.Routing.KillSwitch = true

	leaky := &config.SetConfig{Id: "b", Name: "leaky"}
	leaky.Routing.Enabled = true
	leaky.Routing.Mode = config.RoutingModeInterface
	leaky.Routing.EgressInterface = "wg0"

	heldMark, heldTable := routeResolveIDs(&cfg, held)
	leakyMark, leakyTable := routeResolveIDs(&cfg, leaky)

	if heldTable == leakyTable || heldMark == leakyMark {
		t.Errorf("both sets landed on mark 0x%x table %d; the blackhole lives in the table, so the last set applied would decide the kill switch for both", heldMark, heldTable)
	}
}

func TestRouteTableWantsKillSwitchCoversEverySetOnTheTable(t *testing.T) {
	origCache := routeRuleCache
	t.Cleanup(func() { routeRuleCache = origCache })

	shared := routeState{mode: config.RoutingModeInterface, mark: 0x1234, table: 120, iface: "wg0"}
	holder := shared
	holder.killSwitch = true

	routeRuleCache = map[string]routeState{"holder": holder}
	if !routeTableWantsKillSwitch(shared) {
		t.Error("a set pinned onto a table another set holds shut must not reopen it")
	}

	routeRuleCache = map[string]routeState{"other": shared}
	if routeTableWantsKillSwitch(shared) {
		t.Error("with nobody asking for it the blackhole has to come out, or the table keeps dropping traffic after the flag is off")
	}
}

func TestInjectedRuleCarriesAnIPSourceWhenEveryDeviceHasOne(t *testing.T) {
	ipGate := routeDeviceGate{
		enabled: true,
		matches: []config.DeviceMatch{{IP: "192.168.1.50"}, {IP: "192.168.1.51"}},
	}
	got := routeInjectedSourceMatches(ipGate)
	if len(got) != 2 {
		t.Errorf("an IP-matched device can be named on the reinjected packet, which keeps an out-of-scope client's fakes off this set's route: %v", got)
	}

	mixed := routeDeviceGate{
		enabled: true,
		matches: []config.DeviceMatch{{IP: "192.168.1.50"}, {MAC: "aa:bb:cc:dd:ee:ff"}},
	}
	if routeInjectedSourceMatches(mixed) != nil {
		t.Error("a reinjected packet carries no MAC, so narrowing the rule to the IP half would drop the other clients' fakes onto the normal uplink")
	}

	if routeInjectedSourceMatches(routeDeviceGate{enabled: true, blacklist: true, matches: ipGate.matches}) != nil {
		t.Error("a blacklist names who is excluded, not who to match")
	}
}

func TestInjectedRuleFallsBackWhenNoSourceMatchesTheFamily(t *testing.T) {
	v4Only := []config.DeviceMatch{{IP: "192.168.1.50"}, {IP: "192.168.1.51"}}

	if got := routeInjectedSourcesForFamily(v4Only, false); len(got) != 2 {
		t.Errorf("an IPv4 device belongs on the IPv4 rule, got %v", got)
	}
	if got := routeInjectedSourcesForFamily(v4Only, true); len(got) != 0 {
		t.Errorf("an IPv4 address cannot be written into an IPv6 rule, got %v", got)
	}

	mixed := []config.DeviceMatch{{IP: "192.168.1.50"}, {IP: "2001:db8::1", V6: true}}
	if got := routeInjectedSourcesForFamily(mixed, true); len(got) != 1 || got[0].IP != "2001:db8::1" {
		t.Errorf("each family takes only its own addresses, got %v", got)
	}
}

func TestOutChainRefusesAPacketAnotherSetAlreadyClaimed(t *testing.T) {
	cfg := config.NewConfig()
	st := routeState{mark: 0x1b1d, chainOut: "b4r_x_out", setV4: "b4r_x_v4", setV6: "b4r_x_v6", routerOut: true}

	be := &mockRouteBackend{}
	routeAddOutChainRules(be, &cfg, st, routeDeviceGate{})

	ops := be.chainOps[st.chainOut]
	claimed := indexOfOp(ops, "claimed-bypass")
	injected := indexOfPrefix(ops, "injected ")
	if claimed < 0 || injected < 0 {
		t.Fatalf("chain is missing a rule it needs: %v", ops)
	}
	if claimed > injected {
		t.Errorf("setting a routing mark leaves the queue mark on the packet, so a second set's chain would re-mark an injection the first set already claimed and send its fakes out the wrong interface: %v", ops)
	}
}

func TestRouteLineBelongsToIfaceOnlyClaimsItsOwnBlackhole(t *testing.T) {
	for _, c := range []struct {
		line string
		ours bool
	}{
		{"blackhole default metric " + routeKillSwitchMetric, true},
		{"blackhole default metric 1", false},
		{"blackhole default", false},
		{"blackhole 10.0.0.0/8 metric " + routeKillSwitchMetric, false},
	} {
		if got := routeLineBelongsToIface(c.line, "tun0"); got != c.ours {
			t.Errorf("routeLineBelongsToIface(%q) = %v, want %v; b4 flushes a table it decides is its own, and another client's kill switch lives in one of these", c.line, got, c.ours)
		}
	}
}

func TestCleanupFlushesATableOnlyWhenNobodyElseUsesIt(t *testing.T) {
	origCache := routeRuleCache
	t.Cleanup(func() { routeRuleCache = origCache })

	leaving := routeState{mode: config.RoutingModeInterface, mark: 0x1111, table: 120, iface: "wg0"}
	pinned := routeState{mode: config.RoutingModeInterface, mark: 0x2222, table: 120, iface: "wg0"}

	routeRuleCache = map[string]routeState{"a": leaving, "b": pinned}
	if routeMarkShareCount(leaving.mark) != 1 {
		t.Error("the ip rule is keyed on the mark, so only a set with the same mark keeps it alive")
	}
	if routeTableShareCount(leaving.table) != 2 {
		t.Error("two sets pinned to one table both live in it, whatever marks they carry")
	}

	routeRuleCache = map[string]routeState{"a": leaving}
	if routeTableShareCount(leaving.table) != 1 {
		t.Error("with the last user gone the table can be flushed")
	}
}
