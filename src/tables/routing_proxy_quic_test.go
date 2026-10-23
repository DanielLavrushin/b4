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
	st.quicReject = quicReject
	return st
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
	watched := false
	for _, r := range routeStateChains(blocking) {
		if r.chain == blocking.chainQUIC && r.table == "filter" {
			watched = true
		}
	}
	if !watched {
		t.Errorf("the monitor must watch the QUIC chain of a set that blocks QUIC, got %+v", routeStateChains(blocking))
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
			parents := []string{routeNftBlockFwd + " jump ", routeNftBlockOut + " jump "}
			if engine == backendIPTables {
				perFamily = 1
				parents = []string{"-I FORWARD 1 -j ", "-A OUTPUT -j "}
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

			quic := chainRuleLines(*emitted, st.chainQUIC)
			for _, sn := range []string{st.setV4, st.setV6} {
				refused := false
				for _, line := range quic {
					if strings.Contains(line, sn) && strings.Contains(line, "443") &&
						(strings.Contains(line, "reject with icmpx type port-unreachable") || strings.Contains(line, "-j REJECT")) {
						refused = true
					}
				}
				if !refused {
					t.Errorf("no rule in %s refuses UDP 443 to %s:\n%s", st.chainQUIC, sn, strings.Join(quic, "\n"))
				}
			}
			for _, parent := range parents {
				if firstLineWith(*emitted, parent+st.chainQUIC) < 0 {
					t.Errorf("no %q%s; on iptables the FORWARD jump goes to the top, because a router firewall accepts LAN traffic above an appended one and the set's QUIC then leaves directly:\n%s", parent, st.chainQUIC, strings.Join(*emitted, "\n"))
				}
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
			if n := countLinesWith(*emitted, st.chainQUIC); n != 0 {
				t.Errorf("a set that refuses no QUIC built or touched %s %d time(s)", st.chainQUIC, n)
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

func TestTurningTheQUICRefusalOffKeepsItUntilTheNewRulesAreIn(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			emitted := captureProxyRuleArgv(t)
			stubBinaries(t, backendIPTables, backendIP6Tables)

			old := quicTestState(quicTestSet(true, true), true)
			cur := old
			cur.quicReject = false
			retire := routeCleanupForRebuild(quicTestBackend(engine), old, cur)
			if n := countLinesWith(*emitted, old.chainQUIC); n != 0 {
				t.Fatalf("the QUIC chain came down before the set's new rules went in, so its QUIC left directly in between:\n%s", strings.Join(*emitted, "\n"))
			}
			retire()
			if n := countLinesWith(*emitted, old.chainQUIC); n == 0 {
				t.Error("the QUIC chain of a set that no longer refuses QUIC was never taken down")
			}
		})
	}
}
