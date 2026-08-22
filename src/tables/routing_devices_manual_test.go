package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func manualGateCfg(enabled bool, devices ...config.Device) *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.Devices.Enabled = enabled
	cfg.Queue.Devices.Devices = devices
	return &cfg
}

func setBoundTo(macs ...string) *config.SetConfig {
	s := &config.SetConfig{Id: "s", Name: "S"}
	s.Routing.Mode = config.RoutingModeMTProtoWS
	s.Targets.SourceDevices = macs
	return s
}

func captureRules(t *testing.T, fn func()) [][]string {
	t.Helper()
	var captured [][]string
	orig := runLogged
	runLogged = func(op string, args ...string) {
		captured = append(captured, append([]string{}, args...))
	}
	defer func() { runLogged = orig }()
	fn()
	return captured
}

func TestRouteSetDeviceGateManualDevices(t *testing.T) {
	const manualMAC = "02:B4:0A:4D:01:2C"
	manual := config.Device{MAC: manualMAC, IP: "10.77.1.44", Name: "host1", Selected: false, IsManual: true}
	real1 := config.Device{MAC: "AA:BB:CC:DD:EE:FF", IP: "10.77.1.10", Selected: false}

	t.Run("manual source device becomes an ip matcher", func(t *testing.T) {
		g := routeSetDeviceGate(manualGateCfg(false, manual, real1), setBoundTo(manualMAC))
		if !g.isWhitelist() || len(g.matches) != 1 {
			t.Fatalf("expected a single-matcher whitelist, got %+v", g)
		}
		if !g.matches[0].IsIP() || g.matches[0].IP != "10.77.1.44" {
			t.Errorf("expected ip matcher 10.77.1.44, got %+v", g.matches[0])
		}
		if len(macSet(g)) != 0 {
			t.Errorf("manual device must not yield a mac matcher, got %v", macSet(g))
		}
	})

	t.Run("no matcher value may carry a synthetic mac", func(t *testing.T) {
		g := routeSetDeviceGate(manualGateCfg(true, manual, real1), setBoundTo(manualMAC, real1.MAC))
		for _, m := range g.matches {
			if strings.HasPrefix(strings.ToUpper(m.MAC), "02:B4") {
				t.Errorf("synthetic mac reached a kernel matcher: %+v", m)
			}
		}
	})

	t.Run("mixed binding yields one matcher of each kind", func(t *testing.T) {
		g := routeSetDeviceGate(manualGateCfg(false, manual, real1), setBoundTo(manualMAC, real1.MAC))
		if len(g.matches) != 2 {
			t.Fatalf("expected two matchers, got %+v", g.matches)
		}
		if !ipSet(g)["10.77.1.44"] || !macSet(g)["AA:BB:CC:DD:EE:FF"] {
			t.Errorf("expected one ip and one mac matcher, got %+v", g.matches)
		}
	})

	t.Run("manual device without an ip fails closed and loud", func(t *testing.T) {
		broken := config.Device{MAC: "02:B4:00:00:00:01", IP: "", Selected: false, IsManual: true}
		g := routeSetDeviceGate(manualGateCfg(false, broken), setBoundTo(broken.MAC))
		if !g.isWhitelist() || len(g.matches) != 0 {
			t.Fatalf("expected an enabled whitelist with no matcher, got %+v", g)
		}
		if g.degraded == "" {
			t.Error("an unusable manual device must set a degraded reason")
		}
		if g.key() == "" {
			t.Error("deny-all gate must have a key distinct from the disabled gate")
		}
	})

	t.Run("global whitelist with the same manual device no longer collapses", func(t *testing.T) {
		selected := manual
		selected.Selected = true
		g := routeSetDeviceGate(manualGateCfg(true, selected, real1), setBoundTo(manualMAC))
		if !g.isWhitelist() || len(g.matches) != 1 || !ipSet(g)["10.77.1.44"] {
			t.Errorf("expected intersection to keep the manual device, got %+v", g)
		}
		if g.degraded != "" {
			t.Errorf("healthy gate must not be degraded, got %q", g.degraded)
		}
	})

	t.Run("genuinely empty intersection still denies all and says why", func(t *testing.T) {
		selected := real1
		selected.Selected = true
		g := routeSetDeviceGate(manualGateCfg(true, selected, manual), setBoundTo(manualMAC))
		if !g.enabled || len(g.matches) != 0 {
			t.Fatalf("expected an active deny-all gate, got %+v", g)
		}
		if g.degraded != gateDegradedCombined {
			t.Errorf("expected the combined-filter reason, got %q", g.degraded)
		}
	})

	t.Run("exclude mode yields a blacklist ip matcher", func(t *testing.T) {
		set := setBoundTo(manualMAC)
		set.Targets.SourceDevicesExclude = true
		g := routeSetDeviceGate(manualGateCfg(false, manual), set)
		if !g.isBlacklist() || len(g.matches) != 1 || !ipSet(g)["10.77.1.44"] {
			t.Errorf("expected blacklist ip matcher, got %+v", g)
		}
	})

	t.Run("ipv6 manual device yields a v6 matcher", func(t *testing.T) {
		v6 := config.Device{MAC: "02:B4:00:00:00:02", IP: "2001:db8::5", Selected: false, IsManual: true}
		g := routeSetDeviceGate(manualGateCfg(false, v6), setBoundTo(v6.MAC))
		if len(g.matches) != 1 || !g.matches[0].V6 {
			t.Fatalf("expected a v6 matcher, got %+v", g.matches)
		}
	})

	t.Run("key changes when a manual device ip changes", func(t *testing.T) {
		moved := manual
		moved.IP = "10.77.1.45"
		before := routeSetDeviceGate(manualGateCfg(false, manual), setBoundTo(manualMAC)).key()
		after := routeSetDeviceGate(manualGateCfg(false, moved), setBoundTo(manualMAC)).key()
		if before == after {
			t.Errorf("editing a manual device ip must invalidate the gate key, both %q", before)
		}
	})

	t.Run("a mac left behind by a deleted device stays a mac matcher", func(t *testing.T) {
		g := routeSetDeviceGate(manualGateCfg(false), setBoundTo("AA:BB:CC:DD:EE:FF"))
		if len(g.matches) != 1 || g.matches[0].IsIP() {
			t.Errorf("an unknown mac must stay a mac matcher, got %+v", g.matches)
		}
	})
}

func TestGatedJumpEmission(t *testing.T) {
	ipv4 := routeDeviceGate{enabled: true, matches: []config.DeviceMatch{{IP: "10.77.1.44"}}}
	ipv6 := routeDeviceGate{enabled: true, matches: []config.DeviceMatch{{IP: "2001:db8::5", V6: true}}}
	mac := routeDeviceGate{enabled: true, matches: []config.DeviceMatch{{MAC: "AA:BB:CC:DD:EE:FF"}}}
	denyAll := routeDeviceGate{enabled: true}

	t.Run("nft emits ip saddr for a manual device", func(t *testing.T) {
		rules := captureRules(t, func() { nftEmitGatedJump(routeNftPrerouting, "b4r_x_pre", true, ipv4) })
		if len(rules) != 1 {
			t.Fatalf("expected one rule, got %v", rules)
		}
		got := strings.Join(rules[0], " ")
		want := "nft insert rule inet " + routeNftTable + " " + routeNftPrerouting + " ip saddr 10.77.1.44 jump b4r_x_pre"
		if got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})

	t.Run("nft emits ip6 saddr for a v6 manual device", func(t *testing.T) {
		rules := captureRules(t, func() { nftEmitGatedJump(routeNftPrerouting, "b4r_x_pre", false, ipv6) })
		if len(rules) != 1 || !strings.Contains(strings.Join(rules[0], " "), "ip6 saddr 2001:db8::5") {
			t.Errorf("expected an ip6 saddr rule, got %v", rules)
		}
	})

	t.Run("nft still emits ether saddr for a discovered device", func(t *testing.T) {
		rules := captureRules(t, func() { nftEmitGatedJump(routeNftPrerouting, "b4r_x_pre", false, mac) })
		if len(rules) != 1 || !strings.Contains(strings.Join(rules[0], " "), "ether saddr aa:bb:cc:dd:ee:ff") {
			t.Errorf("expected an ether saddr rule, got %v", rules)
		}
	})

	t.Run("iptables emits -s for a manual device", func(t *testing.T) {
		rules := captureRules(t, func() {
			iptEmitGatedJump(backendIPTables, "mangle", "PREROUTING", "b4r_x_pre", true, ipv4)
		})
		if len(rules) != 1 {
			t.Fatalf("expected one rule, got %v", rules)
		}
		got := strings.Join(rules[0], " ")
		want := backendIPTables + " -w -t mangle -I PREROUTING 1 -s 10.77.1.44 -j b4r_x_pre"
		if got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})

	t.Run("iptables skips an ip matcher of the other family", func(t *testing.T) {
		rules := captureRules(t, func() {
			iptEmitGatedJump(backendIPTables, "mangle", "PREROUTING", "b4r_x_pre", true, ipv6)
		})
		if len(rules) != 0 {
			t.Errorf("a v6 matcher must emit nothing into iptables, got %v", rules)
		}
		rules = captureRules(t, func() {
			iptEmitGatedJump(backendIP6Tables, "mangle", "PREROUTING", "b4r_x_pre", true, ipv6)
		})
		if len(rules) != 1 || !strings.Contains(strings.Join(rules[0], " "), "-s 2001:db8::5") {
			t.Errorf("expected the v6 matcher in ip6tables, got %v", rules)
		}
	})

	t.Run("a mac matcher still reaches both families", func(t *testing.T) {
		for _, cmd := range []string{backendIPTables, backendIP6Tables} {
			rules := captureRules(t, func() {
				iptEmitGatedJump(cmd, "mangle", "PREROUTING", "b4r_x_pre", false, mac)
			})
			if len(rules) != 1 || !strings.Contains(strings.Join(rules[0], " "), "--mac-source AA:BB:CC:DD:EE:FF") {
				t.Errorf("%s: expected a mac-source rule, got %v", cmd, rules)
			}
		}
	})

	t.Run("a whitelist with no matcher emits nothing at all", func(t *testing.T) {
		nftRules := captureRules(t, func() { nftEmitGatedJump(routeNftPrerouting, "b4r_x_pre", true, denyAll) })
		if len(nftRules) != 0 {
			t.Errorf("deny-all must never fall through to an ungated jump, got %v", nftRules)
		}
		iptRules := captureRules(t, func() {
			iptEmitGatedJump(backendIPTables, "mangle", "PREROUTING", "b4r_x_pre", true, denyAll)
		})
		if len(iptRules) != 0 {
			t.Errorf("deny-all must never fall through to an ungated jump, got %v", iptRules)
		}
	})

	t.Run("a disabled gate still emits one ungated jump", func(t *testing.T) {
		rules := captureRules(t, func() { nftEmitGatedJump(routeNftPrerouting, "b4r_x_pre", true, routeDeviceGate{}) })
		if len(rules) != 1 || strings.Contains(strings.Join(rules[0], " "), "saddr") {
			t.Errorf("expected a single ungated jump, got %v", rules)
		}
	})
}

func TestBlacklistGateEmission(t *testing.T) {
	be := &routeNftBackend{}
	gate := routeDeviceGate{enabled: true, blacklist: true, matches: []config.DeviceMatch{
		{IP: "10.77.1.44"},
		{IP: "2001:db8::5", V6: true},
		{MAC: "AA:BB:CC:DD:EE:FF"},
	}}

	t.Run("nft returns on ip saddr for manual devices", func(t *testing.T) {
		rules := captureRules(t, func() { routeAddBlacklistGate(be, "mangle", "b4r_x_pre", true, true, gate) })
		joined := make([]string, 0, len(rules))
		for _, r := range rules {
			joined = append(joined, strings.Join(r, " "))
		}
		all := strings.Join(joined, "\n")
		for _, want := range []string{"ip saddr 10.77.1.44 return", "ip6 saddr 2001:db8::5 return", "ether saddr aa:bb:cc:dd:ee:ff return"} {
			if !strings.Contains(all, want) {
				t.Errorf("missing %q in:\n%s", want, all)
			}
		}
	})

	t.Run("iptables returns per family and per matcher kind", func(t *testing.T) {
		stubBinaries(t, backendIPTables, backendIP6Tables)
		rules := captureRules(t, func() {
			routeAddBlacklistGate(&routeIptBackend{}, "mangle", "b4r_x_pre", true, true, gate)
		})
		got := make([]string, 0, len(rules))
		for _, r := range rules {
			got = append(got, strings.Join(r, " "))
		}
		want := []string{
			backendIPTables + " -w -t mangle -A b4r_x_pre -s 10.77.1.44 -j RETURN",
			backendIPTables + " -w -t mangle -A b4r_x_pre -m mac --mac-source AA:BB:CC:DD:EE:FF -j RETURN",
			backendIP6Tables + " -w -t mangle -A b4r_x_pre -s 2001:db8::5 -j RETURN",
			backendIP6Tables + " -w -t mangle -A b4r_x_pre -m mac --mac-source AA:BB:CC:DD:EE:FF -j RETURN",
		}
		if len(got) != len(want) {
			t.Fatalf("expected %d rules, got %d: %v", len(want), len(got), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("rule %d:\n got  %q\n want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("nft honours the disabled family", func(t *testing.T) {
		rules := captureRules(t, func() { routeAddBlacklistGate(be, "mangle", "b4r_x_pre", true, false, gate) })
		for _, r := range rules {
			if strings.Contains(strings.Join(r, " "), "ip6 saddr") {
				t.Errorf("v6 matcher emitted while ipv6 is disabled: %v", r)
			}
		}
	})
}

func TestRouteDeviceGateForManualDevices(t *testing.T) {
	t.Run("a broken manual device leaves the global filter inert but explained", func(t *testing.T) {
		broken := config.Device{MAC: "02:B4:00:00:00:01", Selected: true, IsManual: true}
		g := routeDeviceGateFor(manualGateCfg(true, broken))
		if g.enabled {
			t.Errorf("expected an inactive gate, got %+v", g)
		}
		if g.degraded != gateDegradedManualIP {
			t.Errorf("expected the manual-ip reason, got %q", g.degraded)
		}
		if g.key() != "" {
			t.Errorf("an inactive gate must keep an empty key, got %q", g.key())
		}
	})

	t.Run("a discovered device without a mac is skipped quietly", func(t *testing.T) {
		g := routeDeviceGateFor(manualGateCfg(true, config.Device{MAC: "  ", Selected: true}))
		if g.enabled || g.degraded != "" {
			t.Errorf("expected a silent inactive gate, got %+v", g)
		}
	})

	t.Run("the reason reaches the set gate when nothing is bound", func(t *testing.T) {
		broken := config.Device{MAC: "02:B4:00:00:00:01", Selected: true, IsManual: true}
		g := routeSetDeviceGate(manualGateCfg(true, broken), setBoundTo())
		if g.degraded != gateDegradedManualIP {
			t.Errorf("expected the reason to propagate, got %+v", g)
		}
	})
}
