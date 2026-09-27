package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func loopJumpAllowGate() routeDeviceGate {
	return routeDeviceGate{enabled: true, matches: []config.DeviceMatch{{MAC: "AA:BB:CC:DD:EE:FF"}}}
}

func loopJumpAllowConfig() config.Config {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	cfg.Queue.Devices.Enabled = true
	cfg.Queue.Devices.Devices = []config.Device{{MAC: "AA:BB:CC:DD:EE:FF", Selected: true}}
	return cfg
}

func TestPreLoopMarkOnlyForAProxySetCarryingRouterTrafficBehindAnAllowList(t *testing.T) {
	allow := loopJumpAllowGate()
	deny := routeDeviceGate{enabled: true, blacklist: true, matches: allow.matches}
	emptied := routeDeviceGate{enabled: true, degraded: gateDegradedCombined}
	cases := []struct {
		name string
		st   routeState
		gate routeDeviceGate
		want uint32
	}{
		{"proxy set behind an allow list", routeState{mode: config.RoutingModeProxy, mark: 0x239c9}, allow, 0x239c9},
		{"mtproto-ws set behind an allow list", routeState{mode: config.RoutingModeMTProtoWS, mark: config.TelegramBridgeMark}, allow, config.TelegramBridgeMark},
		{"allow list emptied by an exclude list", routeState{mode: config.RoutingModeProxy, mark: 0x239c9}, emptied, 0x239c9},
		{"no device filter", routeState{mode: config.RoutingModeProxy, mark: 0x239c9}, routeDeviceGate{}, 0},
		{"deny list", routeState{mode: config.RoutingModeProxy, mark: 0x239c9}, deny, 0},
		{"source-scoped proxy set", routeState{mode: config.RoutingModeProxy, mark: 0x239c9, srcScoped: true}, allow, 0},
		{"interface set", routeState{mode: config.RoutingModeInterface, mark: 0x1b1d}, allow, 0},
		{"no mark yet", routeState{mode: config.RoutingModeProxy}, allow, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := routePreLoopMark(tc.st, tc.gate); got != tc.want {
				t.Errorf("routePreLoopMark = 0x%x, want 0x%x", got, tc.want)
			}
		})
	}
}

func TestAllowListedPreJumpAdmitsTheRoutersLoopbackTripIpt(t *testing.T) {
	stubBinaries(t, backendIPTables, backendIP6Tables)

	emitted := stubIptPrerouting(t, preroutingWithCapture)
	routeEnsureGatedPreJump(&routeIptBackend{}, "b4r_x_pre", loopJumpAllowGate(), 0x239c9)
	for _, cmd := range []string{backendIPTables, backendIP6Tables} {
		want := cmd + " -w -t mangle -I PREROUTING 2 -i lo -m mark --mark 0x239c9/0x27fff -j b4r_x_pre"
		found := false
		for _, e := range *emitted {
			if e == want {
				found = true
			}
		}
		if !found {
			t.Errorf("no loopback jump %q above the capture chain: %v", want, *emitted)
		}
	}

	for name, gate := range map[string]routeDeviceGate{
		"no filter": {},
		"deny list": {enabled: true, blacklist: true, matches: loopJumpAllowGate().matches},
	} {
		emitted := stubIptPrerouting(t, preroutingWithCapture)
		routeEnsureGatedPreJump(&routeIptBackend{}, "b4r_x_pre", gate, 0x239c9)
		for _, e := range *emitted {
			if strings.Contains(e, "-i lo") {
				t.Errorf("%s: the unconditioned jump already takes the loopback trip, a second one is noise: %q", name, e)
			}
		}
	}
}

func TestAllowListedPreJumpAdmitsTheRoutersLoopbackTripNft(t *testing.T) {
	origRun := run
	t.Cleanup(func() { run = origRun })
	run = func(args ...string) (string, error) { return "", nil }

	rules := captureRules(t, func() {
		routeEnsureGatedPreJump(&routeNftBackend{}, "b4r_x_pre", loopJumpAllowGate(), 0x239c9)
	})
	want := `nft add rule inet b4_route prerouting iifname "lo" meta mark & 0x27fff == 0x239c9 jump b4r_x_pre`
	for _, r := range rules {
		if strings.Join(r, " ") == want {
			return
		}
	}
	t.Errorf("no loopback jump %q: %v", want, rules)
}

func TestProxySetBehindAnAllowListStillMarksTheRoutersOwnConnections(t *testing.T) {
	stubBinaries(t, backendIPTables)

	cfg := loopJumpAllowConfig()
	set := orderTestSet("guard", config.RoutingModeProxy, nil)
	st := proxyGuardState(0x239c9)

	emitted := stubProxyRuleSideEffects(t)
	run = func(args ...string) (string, error) {
		*emitted = append(*emitted, strings.Join(args, " "))
		return "", nil
	}
	if err := routeEnsureProxyRule(&routeIptBackend{}, &cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}
	joined := strings.Join(*emitted, "\n")
	for _, want := range []string{
		"-I OUTPUT 1 -j b4r_guard_out",
		"-A b4r_guard_out -p tcp -m conntrack --ctdir ORIGINAL -m set --match-set b4r_guard_v4 dst -j MARK --set-mark 0x239c9/0x239c9",
		"-A PREROUTING -i lo -m mark --mark 0x239c9/0x27fff -j b4r_guard_pre",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q; b4's SOCKS5 server dials from the router, and without it the dial leaves by the normal route:\n%s", want, joined)
		}
	}

	scopedSet := orderTestSet("guard", config.RoutingModeProxy, []string{"AA:BB:CC:DD:EE:FF"})
	scoped := st
	scoped.srcScoped = true
	emitted = stubProxyRuleSideEffects(t)
	run = func(args ...string) (string, error) {
		*emitted = append(*emitted, strings.Join(args, " "))
		return "", nil
	}
	if err := routeEnsureProxyRule(&routeIptBackend{}, &cfg, scopedSet, scoped, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}
	for _, e := range *emitted {
		if strings.Contains(e, "-i lo") || strings.Contains(e, "-A b4r_guard_out") || strings.Contains(e, "-I OUTPUT 1 -j b4r_guard_out") {
			t.Errorf("a set limited to source devices leaves the router's own traffic alone, got %q", e)
		}
	}
}

func TestPrecedenceLiftKeepsTheLoopbackJump(t *testing.T) {
	stubBinaries(t, backendIPTables)
	cfg := withOrderedCache(t, orderTestSet("first", config.RoutingModeProxy, nil))
	cfg.Queue.Devices = loopJumpAllowConfig().Queue.Devices
	st := routeRuleCache["first"]
	st.mark = 0x239c9
	routeRuleCache["first"] = st

	chain := simulatedPrerouting(t, []string{"DIVERT", "b4r_first_pre", "b4r_first_pre", captureChainPre})
	inner := runLogged
	var lifted []string
	runLogged = func(op string, args ...string) bool {
		lifted = append(lifted, strings.Join(args, " "))
		return inner(op, args...)
	}

	routeEnsurePreJumpPrecedence(&routeIptBackend{}, cfg)

	if got := strings.Join(*chain, ","); got != "b4r_first_pre,b4r_first_pre,DIVERT,"+captureChainPre {
		t.Errorf("mangle PREROUTING is %s after the lift", got)
	}
	found := false
	for _, e := range lifted {
		if strings.Contains(e, "-i lo -m mark --mark 0x239c9/0x27fff -j b4r_first_pre") {
			found = true
		}
	}
	if !found {
		t.Errorf("the lift re-emitted the set's jumps without the loopback one and then dropped the old one: %v", lifted)
	}
}

func TestAlreadyOrderedCountsASetsJumpsOnce(t *testing.T) {
	stubBinaries(t, backendIPTables)
	cfg := withOrderedCache(t,
		orderTestSet("first", config.RoutingModeProxy, nil),
		orderTestSet("second", config.RoutingModeProxy, nil),
	)
	ordered := routeOrderedRoutingSets(cfg)

	simulatedPrerouting(t, []string{"b4r_first_pre", "b4r_first_pre", "b4r_second_pre", "b4r_second_pre", captureChainPre})
	if !routePreJumpsAlreadyOrdered(&routeIptBackend{}, ordered) {
		t.Error("a device jump and a loopback jump for the same set, in config order above the capture chain, are already right")
	}

	simulatedPrerouting(t, []string{"b4r_first_pre", "b4r_second_pre", "b4r_first_pre", captureChainPre})
	if routePreJumpsAlreadyOrdered(&routeIptBackend{}, ordered) {
		t.Error("jumps of one set split by another set's are out of order")
	}

	simulatedPrerouting(t, []string{"b4r_first_pre", "b4r_second_pre", captureChainPre, "b4r_second_pre"})
	if routePreJumpsAlreadyOrdered(&routeIptBackend{}, ordered) {
		t.Error("a jump below the capture chain is not in effect")
	}
}

func TestAGateThatAdmitsNoDeviceInAFamilyTakesTheOldJumpAway(t *testing.T) {
	stubBinaries(t, backendIPTables)
	chain := simulatedPrerouting(t, []string{"b4r_x_pre", captureChainPre})

	v6Only := routeDeviceGate{enabled: true, matches: []config.DeviceMatch{{IP: "fd00::5", V6: true}}}
	routeEnsureGatedPreJump(&routeIptBackend{}, "b4r_x_pre", v6Only, 0)

	if got := strings.Join(*chain, ","); got != captureChainPre {
		t.Errorf("iptables mangle PREROUTING is %s; the unconditioned jump from before the allow list kept routing every IPv4 device", got)
	}
}

func TestAGateThatAdmitsNoDeviceStillGetsTheLoopbackJump(t *testing.T) {
	stubBinaries(t, backendIPTables)
	chain := simulatedPrerouting(t, []string{"b4r_x_pre", captureChainPre})

	v6Only := routeDeviceGate{enabled: true, matches: []config.DeviceMatch{{IP: "fd00::5", V6: true}}}
	routeEnsureGatedPreJump(&routeIptBackend{}, "b4r_x_pre", v6Only, 0x239c9)

	if got := strings.Join(*chain, ","); got != "b4r_x_pre,"+captureChainPre {
		t.Errorf("iptables mangle PREROUTING is %s, want the loopback jump alone above the capture chain", got)
	}
}

func TestJumplessGatedChainsFollowTheFamilyOfTheAllowedDevices(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.Devices.Enabled = true
	cfg.Queue.Devices.Devices = []config.Device{{IP: "192.168.1.50", IsManual: true, Selected: true}}

	set := orderTestSet("plain", config.RoutingModeInterface, nil)
	iface := routeState{set: set, mode: config.RoutingModeInterface, mark: 0x1b1d, chainPre: "b4r_plain_pre"}
	proxy := routeState{set: set, mode: config.RoutingModeProxy, mark: 0x239c9, chainPre: "b4r_plain_pre", chainQUIC: "b4r_plain_quic", quicReject: true}

	if got := routeJumplessGatedChains(&cfg, iface, false); len(got) != 0 {
		t.Errorf("IPv4 has the device's jump, got %v", got)
	}
	if got := strings.Join(routeJumplessGatedChains(&cfg, iface, true), ","); got != "b4r_plain_pre" {
		t.Errorf("IPv6 has no jump for an IPv4-only allow list, so the presence check must not demand one; got %q", got)
	}
	if got := strings.Join(routeJumplessGatedChains(&cfg, proxy, true), ","); got != "b4r_plain_quic" {
		t.Errorf("the proxy set's loopback jump exists in IPv6 too, only its QUIC chain has no jump there; got %q", got)
	}

	cfg.Queue.Devices.Enabled = false
	if got := routeJumplessGatedChains(&cfg, iface, true); len(got) != 0 {
		t.Errorf("with the filter off every chain has its unconditioned jump, got %v", got)
	}
}
