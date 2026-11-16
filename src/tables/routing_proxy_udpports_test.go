package tables

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func portRoutedTestSet() *config.SetConfig {
	set := orderTestSet("ports", config.RoutingModeProxy, nil)
	set.Routing.Upstream.UDP = true
	set.Routing.SourceInterfaces = []string{"br0"}
	set.UDP.DPortFilter = "50000-50100,19294-19344"
	return set
}

func portRoutedTestState(cfg *config.Config, set *config.SetConfig) routeState {
	st := quicTestState(set, false)
	st.udpPorts = set.RoutedUDPPortSpec()
	st.ipv6 = cfg.Queue.IPv6Enabled
	return st
}

func udpPortRules(lines []string) []string {
	var out []string
	for _, line := range lines {
		if strings.Contains(line, "udp dport {") || strings.Contains(line, "--dports") {
			out = append(out, line)
		}
	}
	return out
}

func TestAPortRoutedSetCarriesItsPortsInTheRuleState(t *testing.T) {
	cfg := config.NewConfig()
	set := portRoutedTestSet()
	st := buildRouteState(&cfg, set)
	if st.udpPorts != "19294-19344,50000-50100" {
		t.Fatalf("udpPorts = %q, want the set's UDP ports", st.udpPorts)
	}

	other := portRoutedTestSet()
	other.UDP.DPortFilter = "50000-50100"
	if routeStateEqual(st, buildRouteState(&cfg, other)) {
		t.Error("a changed port list must change the rule state, or the set's rules are never rebuilt")
	}

	blocking := portRoutedTestSet()
	blocking.UDP.Mode, blocking.UDP.FilterQUIC = config.UDPModeReject, config.QUICFilterAll
	if buildRouteState(&cfg, blocking).quicReject {
		t.Error("a port-routed set has no addresses, so a QUIC refusal by address would match nothing and must not be built")
	}

	unscoped := portRoutedTestSet()
	unscoped.Routing.SourceInterfaces = nil
	if got := buildRouteState(&cfg, unscoped).udpPorts; got != "" {
		t.Errorf("an unscoped set carries ports %q into its rule state; it routes nothing by port", got)
	}
}

func TestAPortRoutedSetTakesItsUDPByPortInTheOriginalDirection(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			localGuardReset(t)
			emitted := captureProxyRuleArgv(t)
			stubBinaries(t, backendIPTables, backendIP6Tables)

			cfg := v4OnlyConfig()
			cfg.Queue.IPv6Enabled = true
			set := portRoutedTestSet()
			st := portRoutedTestState(cfg, set)
			sources := routeNormalizedSources(set.Routing.SourceInterfaces)
			if err := routeEnsureProxyRule(quicTestBackend(engine), cfg, set, st, sources); err != nil {
				t.Fatalf("routeEnsureProxyRule: %v", err)
			}

			pre := chainRuleLines(*emitted, st.chainPre)
			rules := udpPortRules(pre)
			if len(rules) != 2 {
				t.Fatalf("expected one port rule per address family in %s, got %d:\n%s", st.chainPre, len(rules), strings.Join(pre, "\n"))
			}
			for _, rule := range rules {
				if !strings.Contains(rule, "ct direction original") && !strings.Contains(rule, "--ctdir ORIGINAL") {
					t.Errorf("without the original-direction match the rule takes replies whose port falls in the range: %s", rule)
				}
				if !strings.Contains(rule, `iifname "br0"`) && !strings.Contains(rule, "-i br0") {
					t.Errorf("the rule must be limited to the set's source interface: %s", rule)
				}
				if !strings.Contains(rule, "19294-19344, 50000-50100") && !strings.Contains(rule, "19294:19344,50000:50100") {
					t.Errorf("the rule must match the set's ports: %s", rule)
				}
				if !strings.Contains(rule, "tproxy") && !strings.Contains(rule, "TPROXY") {
					t.Errorf("the rule must hand the datagram to the transparent listener: %s", rule)
				}
			}
			guardAt := indexOfLine(pre, func(l string) bool {
				return strings.Contains(l, "fib daddr type") || strings.Contains(l, "--dst-type")
			})
			portAt := indexOfLine(pre, func(l string) bool { return len(udpPortRules([]string{l})) == 1 })
			if guardAt < 0 || guardAt > portAt {
				t.Errorf("the local-destination guard (at %d) must precede the port rule (at %d), or UDP to the router itself goes to the upstream:\n%s",
					guardAt, portAt, strings.Join(pre, "\n"))
			}
		})
	}
}

func TestARefusedPortRuleFailsTheSetInsteadOfLeavingItSilent(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			localGuardReset(t)
			captureProxyRuleArgv(t)
			stubBinaries(t, backendIPTables, backendIP6Tables)
			run = func(args ...string) (string, error) {
				line := strings.Join(args, " ")
				if strings.Contains(line, "ct direction original") || strings.Contains(line, "--ctdir ORIGINAL") {
					return "No chain/target/match by that name", errors.New("exit status 1")
				}
				return "", nil
			}

			cfg := v4OnlyConfig()
			set := portRoutedTestSet()
			st := portRoutedTestState(cfg, set)
			err := routeEnsureProxyRule(quicTestBackend(engine), cfg, set, st, []string{"br0"})
			if err == nil {
				t.Fatal("a kernel that refuses the direction match leaves the set without its port rule, and the install must report that")
			}
			if !strings.Contains(err.Error(), "routes no UDP") {
				t.Errorf("the error must say what the set lost: %v", err)
			}
		})
	}
}

func TestAPortRuleIsNeverEmittedWithoutAScopeAtTheRule(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			localGuardReset(t)
			emitted := captureProxyRuleArgv(t)
			stubBinaries(t, backendIPTables, backendIP6Tables)

			cfg := v4OnlyConfig()
			set := portRoutedTestSet()
			st := portRoutedTestState(cfg, set)
			err := routeEnsureProxyRule(quicTestBackend(engine), cfg, set, st, nil)
			if err == nil {
				t.Fatal("the set's state names ports to route, yet no source interface reached the rule and no device list gates the chain: the install must refuse")
			}
			if rules := udpPortRules(*emitted); len(rules) != 0 {
				t.Errorf("an unscoped port rule was emitted, and it takes replies and inbound connections from the uplink:\n%s", strings.Join(rules, "\n"))
			}
		})
	}
}

func TestIptablesPortChunksCountARangeAsTwoMultiportSlots(t *testing.T) {
	ranges := "1-2,3-4,5-6,7-8,9-10,11-12,13-14,15-16"
	chunks := proxyUDPPortChunksIpt(ranges)
	if len(chunks) != 2 || len(chunks[0]) != 7 || !slices.Equal(chunks[1], []string{"15:16"}) {
		t.Errorf("chunks = %v: xt_multiport takes 15 ports per rule and a range uses two of them, so eight ranges need two rules", chunks)
	}
	single := proxyUDPPortChunksIpt("1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16")
	if len(single) != 2 || len(single[0]) != 15 {
		t.Errorf("chunks = %v, want 15 single ports in the first rule", single)
	}
	mixed := proxyUDPPortChunksIpt("1-2,3-4,5-6,7-8,9-10,11-12,13-14,20")
	if len(mixed) != 1 {
		t.Errorf("chunks = %v: seven ranges and one port fill exactly 15 slots", mixed)
	}
}
