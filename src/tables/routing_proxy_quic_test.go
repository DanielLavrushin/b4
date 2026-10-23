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

func TestABlockQUICProxySetKeepsUDP443OutOfItsTProxyRules(t *testing.T) {
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

			except, perFamily := "udp dport != 443", 2
			parents := []string{routeNftBlockFwd + " jump ", routeNftBlockOut + " jump "}
			if engine == backendIPTables {
				except, perFamily = "! --dport 443", 1
				parents = []string{"FORWARD -j ", "OUTPUT -j "}
			}

			pre := chainRuleLines(*emitted, st.chainPre)
			udp := quicTProxyRules(pre, "udp")
			for _, sn := range []string{st.setV4, st.setV6} {
				if n := countLinesWith(udp, sn); n != perFamily {
					t.Errorf("expected %d UDP TPROXY rule(s) for %s, got %d:\n%s", perFamily, sn, n, strings.Join(udp, "\n"))
				}
			}
			for _, line := range udp {
				if !strings.Contains(line, except) {
					t.Errorf("the UDP TPROXY rule still diverts QUIC to the upstream, so it never reaches the reject rule: %s", line)
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
					t.Errorf("nothing jumps to %s from %q, so neither forwarded nor the router's own QUIC is refused", st.chainQUIC, strings.TrimSpace(parent))
				}
			}

			for _, line := range chainRuleLines(*emitted, st.chainOut) {
				if strings.Contains(line, "udp") {
					t.Errorf("%s marks only the router's own TCP for the upstream; a UDP rule here needs the same port 443 exception as %s: %s", st.chainOut, st.chainPre, line)
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

			udp := quicTProxyRules(chainRuleLines(*emitted, st.chainPre), "udp")
			if len(udp) == 0 {
				t.Fatal("no UDP TPROXY rule for a set that routes UDP through the upstream")
			}
			for _, line := range udp {
				if strings.Contains(line, "dport") {
					t.Errorf("with Block QUIC off the set's QUIC goes to the upstream with the rest of its UDP: %s", line)
				}
			}
			if n := countLinesWith(*emitted, st.chainQUIC); n != 0 {
				t.Errorf("a set that refuses no QUIC built or touched %s %d time(s)", st.chainQUIC, n)
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
