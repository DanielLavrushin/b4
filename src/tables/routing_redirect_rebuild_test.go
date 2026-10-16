package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func outChainWithoutHandles(engine string, rules []string) []string {
	if engine != backendNFTables {
		return rules
	}
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		if i := strings.Index(r, " "); i >= 0 {
			r = r[i+1:]
		}
		out = append(out, r)
	}
	return out
}

func outChainIPv4Rules(engine string, rules []string) []string {
	if engine == backendNFTables {
		return rules
	}
	var out []string
	for _, r := range rules {
		if strings.HasPrefix(r, backendIPTables+" ") {
			out = append(out, r)
		}
	}
	return out
}

func outChainIPv6Rules(rules []string) []string {
	var out []string
	for _, r := range rules {
		if strings.HasPrefix(r, backendIP6Tables+" ") {
			out = append(out, r)
		}
	}
	return out
}

func TestTheRedirectDropStaysFirstAcrossRebuilds(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			familyResetGlobals(t)
			gwIsolate(t)
			fw := stubRevertFirewall(t)
			stubBinaries(t, "ip", backendIPTables, backendIP6Tables, "ipset")
			gwStubHost(t, map[string][]string{"eth1": {"192.0.2.10/24", "2001:db8:1::10/64"}})
			cfg := familyTestConfig(true, true)
			set := gwTestSet("vr", "eth1", "")
			cfg.Sets = []*config.SetConfig{set}
			var be routeBackend = &routeNftBackend{}
			drop := `oifname "eth1" icmp type redirect drop`
			refuse := "ip saddr"
			if engine == backendIPTables {
				be = &routeIptBackend{}
				drop = "iptables -o eth1 -p icmp --icmp-type redirect -j DROP"
				refuse = "-s 192.0.2.1"
			}
			out := func(st routeState) []string {
				return outChainWithoutHandles(engine, fw.chainRules(be, st.chainOut, true))
			}
			countDrop := func(rules []string) int {
				n := 0
				for _, r := range rules {
					if strings.Contains(r, "redirect") {
						n++
					}
				}
				return n
			}

			st0 := gwResolvedState(cfg, set)
			if routeGuardNarrowed(st0) {
				t.Fatalf("no gateway must not narrow")
			}
			if err := routeEnsureRule(be, cfg, set, st0, nil); err != nil {
				t.Fatalf("install without gateway: %v", err)
			}
			plain := out(st0)
			if countDrop(plain) != 0 {
				t.Fatalf("no gateway, yet a drop:\n%s", strings.Join(plain, "\n"))
			}

			set.Routing.EgressGateway = "192.0.2.1"
			st1 := gwResolvedState(cfg, set)
			if !routeGuardNarrowed(st1) {
				t.Fatalf("gateway must narrow, got %+v", st1.gwPeersV4)
			}
			if err := routeEnsureRule(be, cfg, set, st1, nil); err != nil {
				t.Fatalf("rebuild with gateway: %v", err)
			}
			gw := out(st1)
			v4 := outChainIPv4Rules(engine, gw)
			if countDrop(gw) != 1 || len(v4) == 0 || v4[0] != drop {
				t.Fatalf("after the rebuild to a gateway the drop must be the only one and first:\n%s", strings.Join(gw, "\n"))
			}
			if engine == backendIPTables && countDrop(outChainIPv6Rules(gw)) != 0 {
				t.Fatalf("ip6tables out chain got a drop:\n%s", strings.Join(gw, "\n"))
			}

			if err := routeEnsureRule(be, cfg, set, st1, nil); err != nil {
				t.Fatalf("second rebuild: %v", err)
			}
			again := out(st1)
			if strings.Join(again, "\n") != strings.Join(gw, "\n") {
				t.Fatalf("a same-state rebuild changed the chain:\nbefore\n%s\nafter\n%s", strings.Join(gw, "\n"), strings.Join(again, "\n"))
			}

			fw.refuseRules(refuse)
			if err := routeEnsureRule(be, cfg, set, st1, nil); err == nil {
				t.Fatalf("the narrowed guard was refused, yet the rebuild succeeded")
			}
			fw.refuseRules("")
			failed := out(st1)
			if strings.Join(failed, "\n") != strings.Join(gw, "\n") {
				t.Fatalf("a failed rebuild did not restore the out chain:\nwant\n%s\ngot\n%s", strings.Join(gw, "\n"), strings.Join(failed, "\n"))
			}

			set.Routing.EgressGateway = "192.0.2.2"
			st2 := gwResolvedState(cfg, set)
			if err := routeEnsureRule(be, cfg, set, st2, nil); err != nil {
				t.Fatalf("rebuild with another gateway: %v", err)
			}
			moved := out(st2)
			mv4 := outChainIPv4Rules(engine, moved)
			if countDrop(moved) != 1 || len(mv4) == 0 || mv4[0] != drop {
				t.Fatalf("after a gateway change the drop must be the only one and first:\n%s", strings.Join(moved, "\n"))
			}

			set.Routing.EgressGateway = ""
			st3 := gwResolvedState(cfg, set)
			if err := routeEnsureRule(be, cfg, set, st3, nil); err != nil {
				t.Fatalf("rebuild without gateway: %v", err)
			}
			back := out(st3)
			if countDrop(back) != 0 {
				t.Fatalf("a set back to no gateway kept a drop:\n%s", strings.Join(back, "\n"))
			}
			if strings.Join(back, "\n") != strings.Join(plain, "\n") {
				t.Fatalf("back to no gateway, the out chain differs from the first gateway-less install:\nfirst\n%s\nnow\n%s", strings.Join(plain, "\n"), strings.Join(back, "\n"))
			}
		})
	}
}
