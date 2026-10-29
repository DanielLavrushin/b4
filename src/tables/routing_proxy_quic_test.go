package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func quicTestSet(udp, blockQUIC bool) *config.SetConfig {
	set := orderTestSet("quic", config.RoutingModeProxy, nil)
	set.Routing.Upstream.UDP = udp
	if blockQUIC {
		set.UDP.Mode = config.UDPModeReject
		set.UDP.FilterQUIC = config.QUICFilterAll
	}
	return set
}

func quicTestState(set *config.SetConfig, quicReject bool) routeState {
	st := proxyGuardState(0x239c9)
	st.setID, st.ipv6 = set.Id, true
	st.setV4, st.setV6 = routeBuildSetNames(set.Id)
	st.chainPre, st.chainOut, st.chainSNAT = routeBuildChainNames(set.Id)
	st.chainQUIC = routeBuildQUICChainName(set.Id)
	st.chainQOut = routeBuildQUICOutChainName(set.Id)
	st.quicReject = quicReject
	return st
}

func linesNaming(lines []string, chain string) []string {
	var out []string
	for _, line := range lines {
		if strings.Contains(" "+line+" ", " "+chain+" ") {
			out = append(out, line)
		}
	}
	return out
}

func jumpLines(lines []string, parent, chain string) []string {
	var out []string
	for _, line := range lines {
		if strings.HasSuffix(line, parent+chain) {
			out = append(out, line)
		}
	}
	return out
}

func quicTestBackend(engine string) routeBackend {
	if engine == backendIPTables {
		return &routeIptBackend{}
	}
	return &routeNftBackend{}
}

func quicTProxyRules(lines []string, proto string) []string {
	var out []string
	for _, line := range lines {
		if !strings.Contains(line, "-j TPROXY") && !strings.Contains(line, " tproxy ") {
			continue
		}
		if strings.Contains(line, "-p "+proto+" ") || strings.Contains(line, "ip protocol "+proto+" ") || strings.Contains(line, "meta l4proto "+proto+" ") {
			out = append(out, line)
		}
	}
	return out
}

func TestAUDPProxySetRefusesQUICOnlyWhenItBlocksQUIC(t *testing.T) {
	cfg := config.NewConfig()

	plain := buildRouteState(&cfg, quicTestSet(true, false))
	blocking := buildRouteState(&cfg, quicTestSet(true, true))
	if plain.quicReject {
		t.Error("a set that routes UDP through the upstream with Block QUIC off must send its QUIC to the upstream as well")
	}
	if !blocking.quicReject {
		t.Error("Block QUIC on a set that routes UDP through the upstream must refuse the set's QUIC; the packet queue never sees that UDP, so the firewall is the only place left to refuse it")
	}
	if routeStateEqual(plain, blocking) {
		t.Error("turning Block QUIC on or off must change the rule state, or the set's rules are never rebuilt")
	}
	parents := map[string]string{}
	for _, r := range routeStateChains(blocking) {
		if r.table == "filter" {
			parents[r.chain] = r.parent
		}
	}
	if parents[blocking.chainQUIC] != "FORWARD" || parents[blocking.chainQOut] != "OUTPUT" {
		t.Errorf("the monitor must watch %s from FORWARD and %s from OUTPUT for a set that blocks QUIC, got %+v",
			blocking.chainQUIC, blocking.chainQOut, routeStateChains(blocking))
	}

	scoped := quicTestSet(true, true)
	scoped.Routing.SourceInterfaces = []string{"br0"}
	for _, r := range routeStateChains(buildRouteState(&cfg, scoped)) {
		if r.table == "filter" && r.parent == "OUTPUT" {
			t.Errorf("a set limited to source interfaces leaves the router's own traffic alone and builds no %s, so the monitor must not wait for it", r.chain)
		}
	}

	tcpOnly := buildRouteState(&cfg, quicTestSet(false, false))
	tcpOnlyBlocking := buildRouteState(&cfg, quicTestSet(false, true))
	if !tcpOnly.quicReject || !tcpOnlyBlocking.quicReject {
		t.Error("a set whose UDP stays off the upstream refuses its QUIC whatever Block QUIC says")
	}
	if !routeStateEqual(tcpOnly, tcpOnlyBlocking) {
		t.Error("with UDP off the upstream, Block QUIC changes nothing in the routing rules, so toggling it must not rebuild the set")
	}

	mtws := quicTestSet(false, true)
	mtws.Routing.Mode = config.RoutingModeMTProtoWS
	if buildRouteState(&cfg, mtws).quicReject {
		t.Error("an mtproto-ws set never diverts UDP, so its Block QUIC acts in the packet queue and needs no QUIC chain")
	}
}

func quicPassRules(lines []string) []string {
	var out []string
	for _, line := range lines {
		if strings.Contains(line, "udp dport 443 accept") || (strings.Contains(line, "--dport 443") && strings.HasSuffix(line, "-j ACCEPT")) {
			out = append(out, line)
		}
	}
	return out
}

func indexOfLine(lines []string, match func(string) bool) int {
	for i, line := range lines {
		if match(line) {
			return i
		}
	}
	return -1
}

func TestABlockQUICProxySetStopsItsQUICAheadOfTheTProxyRules(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			localGuardReset(t)
			emitted := captureProxyRuleArgv(t)
			stubBinaries(t, backendIPTables, backendIP6Tables)

			cfg := v4OnlyConfig()
			cfg.Queue.IPv6Enabled = true
			set := quicTestSet(true, true)
			st := quicTestState(set, true)
			if err := routeEnsureProxyRule(quicTestBackend(engine), cfg, set, st, nil); err != nil {
				t.Fatalf("routeEnsureProxyRule: %v", err)
			}

			perFamily := 2
			fwd, out := routeNftBlockFwd+" jump ", routeNftBlockOut+" jump "
			if engine == backendIPTables {
				perFamily = 1
				fwd, out = "-I FORWARD 1 -j ", "-A OUTPUT -j "
			}

			pre := chainRuleLines(*emitted, st.chainPre)
			pass := quicPassRules(pre)
			udp := quicTProxyRules(pre, "udp")
			for _, sn := range []string{st.setV4, st.setV6} {
				if n := countLinesWith(pass, sn); n != perFamily {
					t.Errorf("expected %d rule(s) ending %s for UDP 443 to %s, got %d; without them the UDP TPROXY rule or a later proxy set takes the QUIC this set refuses:\n%s",
						perFamily, st.chainPre, sn, n, strings.Join(pre, "\n"))
				}
				passAt := indexOfLine(pre, func(l string) bool { return strings.Contains(l, sn) && len(quicPassRules([]string{l})) == 1 })
				udpAt := indexOfLine(pre, func(l string) bool { return strings.Contains(l, sn) && len(quicTProxyRules([]string{l}, "udp")) == 1 })
				if udpAt < 0 || passAt > udpAt {
					t.Errorf("for %s the UDP 443 rule must precede the UDP TPROXY rule (443 at %d, TPROXY at %d):\n%s", sn, passAt, udpAt, strings.Join(pre, "\n"))
				}
			}
			for _, line := range udp {
				if strings.Contains(line, "dport") {
					t.Errorf("the UDP TPROXY rule needs no port match, the rule before it already took UDP 443: %s", line)
				}
			}
			tcp := quicTProxyRules(pre, "tcp")
			if len(tcp) == 0 {
				t.Fatalf("no TCP TPROXY rule in %s:\n%s", st.chainPre, strings.Join(pre, "\n"))
			}
			for _, line := range tcp {
				if strings.Contains(line, "dport") {
					t.Errorf("the set's TCP to port 443 must still go to the upstream: %s", line)
				}
			}

			for _, chain := range []string{st.chainQUIC, st.chainQOut} {
				quic := chainRuleLines(*emitted, chain)
				for _, sn := range []string{st.setV4, st.setV6} {
					refused := false
					for _, line := range quic {
						if strings.Contains(line, sn) && strings.Contains(line, "443") &&
							(strings.Contains(line, "reject with icmpx type port-unreachable") || strings.Contains(line, "-j REJECT")) {
							refused = true
						}
					}
					if !refused {
						t.Errorf("no rule in %s refuses UDP 443 to %s:\n%s", chain, sn, strings.Join(quic, "\n"))
					}
				}
			}
			if len(jumpLines(*emitted, fwd, st.chainQUIC)) == 0 {
				t.Errorf("no %q%s; on iptables the FORWARD jump goes to the top, because a router firewall accepts LAN traffic above an appended one and the set's QUIC then leaves directly:\n%s", fwd, st.chainQUIC, strings.Join(*emitted, "\n"))
			}
			if len(jumpLines(*emitted, out, st.chainQOut)) == 0 {
				t.Errorf("no %q%s, so the router's own QUIC to the set's addresses is never refused:\n%s", out, st.chainQOut, strings.Join(*emitted, "\n"))
			}
			if wrong := append(jumpLines(*emitted, out, st.chainQUIC), jumpLines(*emitted, fwd, st.chainQOut)...); len(wrong) != 0 {
				t.Errorf("%s serves forwarded traffic and %s the router's own; a jump to either from the other hook: %v", st.chainQUIC, st.chainQOut, wrong)
			}

			for _, line := range chainRuleLines(*emitted, st.chainOut) {
				if strings.Contains(line, "udp") {
					t.Errorf("%s marks only the router's own TCP for the upstream; a UDP rule here needs the same port 443 stop as %s: %s", st.chainOut, st.chainPre, line)
				}
			}
		})
	}
}

func TestAUDPProxySetWithoutBlockQUICSendsAllItsUDPUpstream(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			localGuardReset(t)
			emitted := captureProxyRuleArgv(t)
			stubBinaries(t, backendIPTables, backendIP6Tables)

			cfg := v4OnlyConfig()
			cfg.Queue.IPv6Enabled = true
			set := quicTestSet(true, false)
			st := quicTestState(set, false)
			if err := routeEnsureProxyRule(quicTestBackend(engine), cfg, set, st, nil); err != nil {
				t.Fatalf("routeEnsureProxyRule: %v", err)
			}

			pre := chainRuleLines(*emitted, st.chainPre)
			udp := quicTProxyRules(pre, "udp")
			if len(udp) == 0 {
				t.Fatal("no UDP TPROXY rule for a set that routes UDP through the upstream")
			}
			for _, line := range udp {
				if strings.Contains(line, "dport") {
					t.Errorf("with Block QUIC off the set's QUIC goes to the upstream with the rest of its UDP: %s", line)
				}
			}
			if pass := quicPassRules(pre); len(pass) != 0 {
				t.Errorf("with Block QUIC off nothing may stop the set's QUIC ahead of the upstream: %v", pass)
			}
			for _, chain := range []string{st.chainQUIC, st.chainQOut} {
				if n := len(linesNaming(*emitted, chain)); n != 0 {
					t.Errorf("a set that refuses no QUIC built or touched %s %d time(s)", chain, n)
				}
			}
		})
	}
}

func TestAProxySetWithUDPOffKeepsItsQUICFromLaterSets(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			localGuardReset(t)
			emitted := captureProxyRuleArgv(t)
			stubBinaries(t, backendIPTables, backendIP6Tables)

			cfg := v4OnlyConfig()
			set := quicTestSet(false, false)
			st := quicTestState(set, true)
			if err := routeEnsureProxyRule(quicTestBackend(engine), cfg, set, st, nil); err != nil {
				t.Fatalf("routeEnsureProxyRule: %v", err)
			}

			pre := chainRuleLines(*emitted, st.chainPre)
			if udp := quicTProxyRules(pre, "udp"); len(udp) != 0 {
				t.Errorf("a set with UDP off the upstream diverts no UDP: %v", udp)
			}
			if pass := quicPassRules(pre); len(pass) == 0 {
				t.Errorf("a set that refuses its QUIC ends %s for UDP 443 to its addresses, or a later set that routes UDP upstream takes it:\n%s", st.chainPre, strings.Join(pre, "\n"))
			}
		})
	}
}

func TestTheQUICRefusalReturnsDevicesTheSetExcludes(t *testing.T) {
	for _, scope := range []string{"set", "global"} {
		for _, engine := range []string{backendNFTables, backendIPTables} {
			t.Run(scope+"/"+engine, func(t *testing.T) {
				quicExcludedDeviceCheck(t, engine, scope == "global")
			})
		}
	}
}

func quicExcludedDeviceCheck(t *testing.T, engine string, global bool) {
	const mac = "aa:bb:cc:dd:ee:01"
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)
	stubBinaries(t, backendIPTables, backendIP6Tables)

	cfg := v4OnlyConfig()
	cfg.Queue.IPv6Enabled = true
	set := quicTestSet(true, true)
	if global {
		cfg.Queue.Devices.Enabled, cfg.Queue.Devices.WhiteIsBlack = true, true
		cfg.Queue.Devices.Devices = []config.Device{{MAC: mac, Selected: true}}
	} else {
		set.Targets.SourceDevices = []string{mac}
		set.Targets.SourceDevicesExclude = true
	}
	st := quicTestState(set, true)
	if err := routeEnsureProxyRule(quicTestBackend(engine), cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}

	quic := chainRuleLines(*emitted, st.chainQUIC)
	isReturn := func(l string) bool {
		return strings.Contains(strings.ToLower(l), mac) && (strings.HasSuffix(l, " return") || strings.HasSuffix(l, "-j RETURN"))
	}
	isReject := func(l string) bool {
		return strings.Contains(l, "reject with icmpx") || strings.Contains(l, "-j REJECT")
	}
	skip, reject := indexOfLine(quic, isReturn), indexOfLine(quic, isReject)
	if skip < 0 || reject < 0 || skip > reject {
		t.Errorf("%s must return the excluded device %s ahead of its reject rule (return at %d, reject at %d), as the set's own chain does:\n%s",
			st.chainQUIC, mac, skip, reject, strings.Join(quic, "\n"))
	}

	own := chainRuleLines(*emitted, st.chainQOut)
	if indexOfLine(own, isReject) < 0 {
		t.Errorf("excluding a device must not stop %s refusing the router's own QUIC:\n%s", st.chainQOut, strings.Join(own, "\n"))
	}
	for _, line := range own {
		if strings.Contains(strings.ToLower(line), mac) {
			t.Errorf("%s is reached from OUTPUT, where iptables refuses a MAC match and no device sends the router's own traffic: %s", st.chainQOut, line)
		}
	}
	out := routeNftBlockOut + " jump "
	if engine == backendIPTables {
		out = "-A OUTPUT -j "
	}
	if wrong := jumpLines(*emitted, out, st.chainQUIC); len(wrong) != 0 {
		t.Errorf("OUTPUT jumps to %s, which matches device MACs; iptables refuses that jump and the set's install fails: %v", st.chainQUIC, wrong)
	}
}

func TestTurningTheQUICRefusalOffKeepsItUntilTheNewRulesAreIn(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			emitted := captureProxyRuleArgv(t)
			stubBinaries(t, backendIPTables, backendIP6Tables)

			old := quicTestState(quicTestSet(true, true), true)
			cur := old
			cur.quicReject = false
			retire := routeCleanupForRebuild(quicTestBackend(engine), old, cur)
			for _, chain := range []string{old.chainQUIC, old.chainQOut} {
				if n := len(linesNaming(*emitted, chain)); n != 0 {
					t.Fatalf("%s came down before the set's new rules went in, so its QUIC left directly in between:\n%s", chain, strings.Join(*emitted, "\n"))
				}
			}
			retire()
			for _, chain := range []string{old.chainQUIC, old.chainQOut} {
				if n := len(linesNaming(*emitted, chain)); n == 0 {
					t.Errorf("%s of a set that no longer refuses QUIC was never taken down", chain)
				}
			}
		})
	}
}

func TestASetThatNeverRefusedQUICLeavesTheQUICChainsAlone(t *testing.T) {
	bridge := quicTestState(quicTestSet(true, false), false)
	bridge.mode = config.RoutingModeMTProtoWS
	for _, engine := range []string{backendNFTables, backendIPTables} {
		for name, old := range map[string]routeState{
			"proxy set with UDP through the upstream": quicTestState(quicTestSet(true, false), false),
			"Telegram over WebSocket":                 bridge,
		} {
			t.Run(engine+"/"+name, func(t *testing.T) {
				emitted := captureProxyRuleArgv(t)
				stubBinaries(t, backendIPTables, backendIP6Tables)

				cur := old
				cur.deviceKey = "w:mac=96:AB:60:74:88:DE"
				routeCleanupForRebuild(quicTestBackend(engine), old, cur)()
				routeCleanupProxyRule(quicTestBackend(engine), old, false)
				for _, chain := range []string{old.chainQUIC, old.chainQOut} {
					if lines := linesNaming(*emitted, chain); len(lines) != 0 {
						t.Errorf("the set never refused QUIC, so %s never existed, yet its rebuild and its removal ran commands on it; each one failed and logged a warning:\n%s", chain, strings.Join(lines, "\n"))
					}
				}
			})
		}
		t.Run(engine+"/a set that refuses QUIC", func(t *testing.T) {
			emitted := captureProxyRuleArgv(t)
			stubBinaries(t, backendIPTables, backendIP6Tables)

			st := quicTestState(quicTestSet(false, false), true)
			routeCleanupProxyRule(quicTestBackend(engine), st, false)
			for _, chain := range []string{st.chainQUIC, st.chainQOut} {
				if len(linesNaming(*emitted, chain)) == 0 {
					t.Errorf("removing a set that refuses QUIC left %s behind", chain)
				}
			}
		})
	}
}

func TestLimitingTheSetToSourcesTakesDownOnlyTheRouterQUICChain(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			emitted := captureProxyRuleArgv(t)
			stubBinaries(t, backendIPTables, backendIP6Tables)

			old := quicTestState(quicTestSet(true, true), true)
			cur := old
			cur.srcScoped = true
			retire := routeCleanupForRebuild(quicTestBackend(engine), old, cur)
			if n := len(linesNaming(*emitted, old.chainQOut)); n != 0 {
				t.Fatalf("%s came down before the set's new rules went in:\n%s", old.chainQOut, strings.Join(*emitted, "\n"))
			}
			retire()
			if n := len(linesNaming(*emitted, old.chainQOut)); n == 0 {
				t.Errorf("a set limited to source devices or interfaces leaves the router's own traffic alone, yet %s stayed", old.chainQOut)
			}
			if n := len(linesNaming(*emitted, old.chainQUIC)); n != 0 {
				t.Errorf("%s still refuses the QUIC of the set's devices and must stay:\n%s", old.chainQUIC, strings.Join(*emitted, "\n"))
			}
		})
	}
}
