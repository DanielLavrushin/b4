package tables

import (
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const (
	netnsGatewayID      = "netns-gateway-set"
	netnsGatewayHop     = "10.201.1.3"
	netnsGatewayHopPeer = "10.201.1.4"
	netnsGatewayTun     = "b4gt0"
	netnsGatewayTunIP   = "10.201.7.1"
	netnsGatewayTunHop  = "10.201.7.2"
)

func netnsGatewayEngines(t *testing.T, test func(t *testing.T, engine string)) {
	t.Helper()
	netnsRequire(t)
	netnsSetupLinks(t)
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			if engine == backendNFTables && !hasBinary("nft") {
				t.Skip("nft is not installed")
			}
			routeEngine = nil
			t.Cleanup(func() { routeEngine = nil })
			test(t, engine)
		})
	}
}

func netnsGatewaySet(id, iface, gw string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Name = strings.ReplaceAll(id, "-", "")
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeInterface
	set.Routing.EgressInterface = iface
	set.Routing.EgressGateway = gw
	set.Targets.IPs = []string{netnsTarget}
	set.Targets.IpsToMatch = []string{netnsTarget}
	return &set
}

func netnsGatewayConfig(engine string, sets ...*config.SetConfig) *config.Config {
	cfg := netnsConfig(engine)
	cfg.Sets = sets
	return cfg
}

func netnsGatewayInstall(t *testing.T, cfg *config.Config) {
	t.Helper()
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() { _ = ClearRules(cfg) })
	RoutingSyncConfig(cfg)
	t.Cleanup(RoutingClearAll)
}

func netnsGatewayState(t *testing.T, id string) routeState {
	t.Helper()
	routeMu.Lock()
	defer routeMu.Unlock()
	st, ok := routeRuleCache[id]
	if !ok {
		t.Fatalf("set %s built no rules", id)
	}
	return st
}

func netnsGatewayRules(t *testing.T, engine, chain string, v6 bool) []string {
	t.Helper()
	var out string
	switch {
	case engine == backendNFTables:
		out = netnsRun(t, "nft", "list", "chain", "inet", routeNftTable, chain)
	case v6:
		out = netnsRun(t, backendIP6Tables, "-w", "-t", "mangle", "-S", chain)
	default:
		out = netnsRun(t, backendIPTables, "-w", "-t", "mangle", "-S", chain)
	}
	var rules []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "", line == "}", strings.HasPrefix(line, "table "), strings.HasPrefix(line, "chain "), strings.HasPrefix(line, "-N "):
			continue
		}
		rules = append(rules, strings.TrimPrefix(line, "-A "+chain+" "))
	}
	return rules
}

func netnsGatewayRoutes(t *testing.T, st routeState, v6 bool) string {
	t.Helper()
	args := []string{"ip"}
	if v6 {
		args = append(args, "-6")
	}
	return netnsRun(t, append(args, "route", "show", "table", fmt.Sprint(st.table))...)
}

func netnsGatewayExemptions(engine, iface string, gws ...string) []string {
	if engine == backendNFTables {
		if len(gws) == 1 {
			return []string{fmt.Sprintf(`iifname "%s" ip saddr %s return`, iface, gws[0])}
		}
		return []string{fmt.Sprintf(`iifname "%s" ip saddr { %s } return`, iface, strings.Join(gws, ", "))}
	}
	var rules []string
	for _, gw := range gws {
		rules = append(rules, fmt.Sprintf("-s %s/32 -i %s -j RETURN", gw, iface))
	}
	return rules
}

func netnsGatewayFullGuard(engine, iface string) string {
	if engine == backendNFTables {
		return fmt.Sprintf(`iifname "%s" return`, iface)
	}
	return fmt.Sprintf("-i %s -j RETURN", iface)
}

func netnsGatewayIPv6Guard(engine, iface string) string {
	if engine == backendNFTables {
		return fmt.Sprintf(`iifname "%s" meta nfproto ipv6 return`, iface)
	}
	return fmt.Sprintf("-i %s -j RETURN", iface)
}

func netnsGatewayLocalGuard(engine string) string {
	if engine == backendNFTables {
		return "fib daddr type { local, broadcast, multicast } return"
	}
	return "-m addrtype --dst-type LOCAL,BROADCAST,MULTICAST -j RETURN"
}

func netnsGatewayRedirectDrop(engine, iface string) string {
	if engine == backendNFTables {
		return fmt.Sprintf(`oifname "%s" icmp type redirect drop`, iface)
	}
	return fmt.Sprintf("-o %s -p icmp -m icmp --icmp-type 5 -j DROP", iface)
}

func netnsAssertNoMACGuard(t *testing.T, rules []string) {
	t.Helper()
	for _, rule := range rules {
		if strings.Contains(rule, "ether saddr") || strings.Contains(rule, "--mac-source") {
			t.Errorf("the guard keys on the gateway's address, never on a MAC, got %q", rule)
		}
	}
}

func netnsAssertNarrowedGuard(t *testing.T, engine string, st routeState, gws ...string) {
	t.Helper()
	pre := netnsGatewayRules(t, engine, st.chainPre, false)
	for _, want := range append(netnsGatewayExemptions(engine, st.iface, gws...), netnsGatewayLocalGuard(engine)) {
		if !slices.Contains(pre, want) {
			t.Errorf("a usable IPv4 gateway on an Ethernet egress narrows the guard, but %s lacks %q:\n%s", st.chainPre, want, strings.Join(pre, "\n"))
		}
	}
	if engine == backendNFTables && !slices.Contains(pre, netnsGatewayIPv6Guard(engine, st.iface)) {
		t.Errorf("IPv6 arriving on %s keeps the full guard next to an IPv4 gateway:\n%s", st.iface, strings.Join(pre, "\n"))
	}
	if slices.Contains(pre, netnsGatewayFullGuard(engine, st.iface)) {
		t.Errorf("the IPv4 guard of a set with a usable gateway is narrowed, yet %s returns everything from %s:\n%s", st.chainPre, st.iface, strings.Join(pre, "\n"))
	}
	netnsAssertNoMACGuard(t, pre)
	out := netnsGatewayRules(t, engine, st.chainOut, false)
	if !slices.Contains(out, netnsGatewayRedirectDrop(engine, st.iface)) {
		t.Errorf("a narrowed guard sends packets back out %s, so the router must not tell the sender to skip it; %s has no redirect drop:\n%s", st.iface, st.chainOut, strings.Join(out, "\n"))
	}
}

func netnsAssertFullGuard(t *testing.T, engine string, st routeState) {
	t.Helper()
	pre := netnsGatewayRules(t, engine, st.chainPre, false)
	if !slices.Contains(pre, netnsGatewayFullGuard(engine, st.iface)) {
		t.Errorf("%s must return everything arriving on %s, as a set without a gateway does:\n%s", st.chainPre, st.iface, strings.Join(pre, "\n"))
	}
	for _, rule := range pre {
		if strings.Contains(rule, "saddr") || strings.HasPrefix(rule, "-s ") || rule == netnsGatewayLocalGuard(engine) {
			t.Errorf("the full guard takes no gateway exemption and no local-destination return, got %q", rule)
		}
	}
	netnsAssertNoMACGuard(t, pre)
	for _, rule := range netnsGatewayRules(t, engine, st.chainOut, false) {
		if strings.Contains(rule, "redirect") || strings.Contains(rule, "--icmp-type 5") {
			t.Errorf("a set on the full guard drops no redirects, got %q in %s", rule, st.chainOut)
		}
	}
}

func netnsGatewayNeighbor(t *testing.T, ip string) {
	t.Helper()
	netnsRun(t, "ip", "neigh", "replace", ip, "lladdr", netnsLinkMAC(t, netnsPrimary+"p"), "dev", netnsPrimary, "nud", "permanent")
	t.Cleanup(func() { _, _ = run("ip", "neigh", "del", ip, "dev", netnsPrimary) })
}

func TestNetnsGatewaySetKeepsItsViaRoute(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		netnsGatewayInstall(t, netnsGatewayConfig(engine, netnsGatewaySet(netnsGatewayID, netnsPrimary, netnsGatewayHop)))
		st := netnsGatewayState(t, netnsGatewayID)
		routes := netnsGatewayRoutes(t, st, false)
		want := fmt.Sprintf("default via %s dev %s", netnsGatewayHop, netnsPrimary)
		if !strings.Contains(routes, want) {
			t.Errorf("the set table must route through the set's gateway, want %q in:\n%s", want, routes)
		}
		if strings.Contains(routes, "via "+netnsPrimaryGW) {
			t.Errorf("%s is main's default next hop, which a set without a gateway gets; the set's own gateway must replace it:\n%s", netnsPrimaryGW, routes)
		}
		if strings.Contains(routes, "scope link") {
			t.Errorf("no on-link next-hop route belongs in the set table:\n%s", routes)
		}
		netnsAssertNarrowedGuard(t, engine, st, netnsGatewayHop)
	})
}

func TestNetnsGatewayGuardKeepsTheAddressWhenTheNeighborMACChanges(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		netnsGatewayNeighbor(t, netnsGatewayHop)
		cfg := netnsGatewayConfig(engine, netnsGatewaySet(netnsGatewayID, netnsPrimary, netnsGatewayHop))
		netnsGatewayInstall(t, cfg)
		st := netnsGatewayState(t, netnsGatewayID)
		before := netnsGatewayRules(t, engine, st.chainPre, false)

		netnsRun(t, "ip", "neigh", "change", netnsGatewayHop, "lladdr", "02:00:00:00:00:99", "dev", netnsPrimary, "nud", "permanent")
		RoutingSyncConfig(cfg)
		RoutingReinstallForInterface(cfg, netnsPrimary)

		after := netnsGatewayState(t, netnsGatewayID)
		if got := netnsGatewayRules(t, engine, after.chainPre, false); !slices.Equal(got, before) {
			t.Errorf("the guard follows the gateway's address, so a new MAC behind it changes nothing:\nbefore:\n%s\nafter:\n%s", strings.Join(before, "\n"), strings.Join(got, "\n"))
		}
		netnsAssertNarrowedGuard(t, engine, after, netnsGatewayHop)
	})
}

func TestNetnsGatewayGuardKeepsTheAddressWhenTheNeighborDies(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		netnsGatewayNeighbor(t, netnsGatewayHop)
		cfg := netnsGatewayConfig(engine, netnsGatewaySet(netnsGatewayID, netnsPrimary, netnsGatewayHop))
		netnsGatewayInstall(t, cfg)
		before := netnsGatewayRules(t, engine, netnsGatewayState(t, netnsGatewayID).chainPre, false)

		for _, step := range []struct {
			what string
			args []string
		}{
			{"failed", []string{"ip", "neigh", "change", netnsGatewayHop, "lladdr", netnsLinkMAC(t, netnsPrimary+"p"), "dev", netnsPrimary, "nud", "failed"}},
			{"deleted", []string{"ip", "neigh", "del", netnsGatewayHop, "dev", netnsPrimary}},
		} {
			netnsRun(t, step.args...)
			RoutingSyncConfig(cfg)
			RoutingReinstallForInterface(cfg, netnsPrimary)
			st := netnsGatewayState(t, netnsGatewayID)
			if got := netnsGatewayRules(t, engine, st.chainPre, false); !slices.Equal(got, before) {
				t.Errorf("with the neighbor entry %s the guard must stay on the gateway's address:\nbefore:\n%s\nafter:\n%s", step.what, strings.Join(before, "\n"), strings.Join(got, "\n"))
			}
			netnsAssertNarrowedGuard(t, engine, st, netnsGatewayHop)
		}
	})
}

func TestNetnsGatewaySetFallsBackToTheInterfaceRoute(t *testing.T) {
	for _, c := range []struct {
		name, gw string
	}{
		{"not on a network the egress reaches", "198.51.100.250"},
		{"one of the router's own addresses", netnsPrimaryIP},
	} {
		t.Run(c.name, func(t *testing.T) {
			netnsGatewayEngines(t, func(t *testing.T, engine string) {
				netnsGatewayInstall(t, netnsGatewayConfig(engine, netnsGatewaySet(netnsGatewayID, netnsPrimary, c.gw)))
				st := netnsGatewayState(t, netnsGatewayID)
				if st.gwV4 != "" || st.egressGW != c.gw {
					t.Fatalf("a refused gateway counts as none but stays in the state, got gwV4=%q egressGW=%q", st.gwV4, st.egressGW)
				}
				routes := netnsGatewayRoutes(t, st, false)
				if strings.Contains(routes, "via "+c.gw) {
					t.Errorf("a refused gateway must not become the next hop:\n%s", routes)
				}
				want := fmt.Sprintf("default via %s dev %s", netnsPrimaryGW, netnsPrimary)
				if !strings.Contains(routes, want) {
					t.Errorf("a refused gateway leaves the route a set without a gateway gets, want %q in:\n%s", want, routes)
				}
				netnsAssertFullGuard(t, engine, st)
			})
		})
	}
}

func TestNetnsGatewayReinstallKeepsTheViaRoute(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		cfg := netnsGatewayConfig(engine, netnsGatewaySet(netnsGatewayID, netnsPrimary, netnsGatewayHop))
		netnsGatewayInstall(t, cfg)
		st := netnsGatewayState(t, netnsGatewayID)
		netnsRun(t, "ip", "route", "flush", "table", fmt.Sprint(st.table))
		if routes := netnsGatewayRoutes(t, st, false); strings.TrimSpace(routes) != "" {
			t.Fatalf("the flush stands in for the kernel dropping the route on link down, yet the table still holds:\n%s", routes)
		}
		RoutingReinstallForInterface(cfg, netnsPrimary)
		want := fmt.Sprintf("default via %s dev %s", netnsGatewayHop, netnsPrimary)
		if routes := netnsGatewayRoutes(t, st, false); !strings.Contains(routes, want) {
			t.Errorf("the link-up reinstall must put the gateway route back, want %q in:\n%s", want, routes)
		}
	})
}

func TestNetnsGatewaySetKeepsRouterDestinationsUnmarked(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		set := netnsGatewaySet(netnsGatewayID, netnsPrimary, netnsGatewayHop)
		set.Targets.IPs = []string{"0.0.0.0/0"}
		set.Targets.IpsToMatch = []string{"0.0.0.0/0"}
		netnsGatewayInstall(t, netnsGatewayConfig(engine, set))
		st := netnsGatewayState(t, netnsGatewayID)
		pre := netnsGatewayRules(t, engine, st.chainPre, false)
		local := slices.Index(pre, netnsGatewayLocalGuard(engine))
		if local < 0 {
			t.Fatalf("a catch-all gateway set must return router-addressed packets before marking:\n%s", strings.Join(pre, "\n"))
		}
		for i, rule := range pre {
			if strings.Contains(rule, "MARK --set-xmark") || strings.Contains(rule, "meta mark set") {
				if i < local {
					t.Errorf("a mark rule sits above the local-destination return:\n%s", strings.Join(pre, "\n"))
				}
				break
			}
		}
	})
}

func TestNetnsAnIPv4GatewayLeavesTheIPv6GuardFull(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		cfg := netnsGatewayConfig(engine, netnsGatewaySet(netnsGatewayID, netnsPrimary, netnsGatewayHop))
		cfg.Queue.IPv6Enabled = true
		netnsGatewayInstall(t, cfg)
		st := netnsGatewayState(t, netnsGatewayID)
		if st.gwV4 != netnsGatewayHop || st.gwV6 != "" {
			t.Fatalf("an IPv4 gateway is the IPv4 next hop only, got gwV4=%q gwV6=%q", st.gwV4, st.gwV6)
		}
		netnsAssertNarrowedGuard(t, engine, st, netnsGatewayHop)
		if engine == backendNFTables {
			return
		}
		v6 := netnsGatewayRules(t, engine, st.chainPre, true)
		if !slices.Contains(v6, netnsGatewayIPv6Guard(engine, netnsPrimary)) {
			t.Errorf("IPv6 arriving on %s keeps the full guard:\n%s", netnsPrimary, strings.Join(v6, "\n"))
		}
		for _, rule := range v6 {
			if strings.HasPrefix(rule, "-s ") || strings.Contains(rule, "--mac-source") {
				t.Errorf("the IPv4 gateway must not narrow the IPv6 guard, got %q", rule)
			}
		}
	})
}

func TestNetnsTwoGatewaysOnOneEgressExemptEachOther(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		a := netnsGatewaySet("netns-gateway-pair-a", netnsPrimary, netnsGatewayHop)
		b := netnsGatewaySet("netns-gateway-pair-b", netnsPrimary, netnsGatewayHopPeer)
		netnsGatewayInstall(t, netnsGatewayConfig(engine, a, b))
		for _, c := range []struct {
			id, own string
		}{
			{a.Id, netnsGatewayHop},
			{b.Id, netnsGatewayHopPeer},
		} {
			st := netnsGatewayState(t, c.id)
			netnsAssertNarrowedGuard(t, engine, st, netnsGatewayHop, netnsGatewayHopPeer)
			routes := netnsGatewayRoutes(t, st, false)
			if want := fmt.Sprintf("default via %s dev %s", c.own, netnsPrimary); !strings.Contains(routes, want) {
				t.Errorf("set %s must route through its own gateway, want %q in:\n%s", c.id, want, routes)
			}
		}
		if netnsGatewayState(t, a.Id).table == netnsGatewayState(t, b.Id).table {
			t.Error("two gateways on one egress need two tables, or one default route overwrites the other")
		}
	})
}

func netnsGatewayTunnel(t *testing.T) {
	t.Helper()
	_, _ = run("ip", "link", "del", netnsGatewayTun)
	if out, err := run("ip", "tuntap", "add", "mode", "tun", "name", netnsGatewayTun); err != nil {
		t.Skipf("cannot create a tun device here: %v (%s)", err, strings.TrimSpace(out))
	}
	t.Cleanup(func() { _, _ = run("ip", "link", "del", netnsGatewayTun) })
	netnsRun(t, "ip", "link", "set", netnsGatewayTun, "up")
	netnsRun(t, "ip", "addr", "add", netnsGatewayTunIP+"/24", "dev", netnsGatewayTun)
}

func TestNetnsGatewayOnATunnelOnlySetsTheRoute(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		netnsGatewayTunnel(t)
		set := netnsGatewaySet(netnsGatewayID, netnsGatewayTun, netnsGatewayTunHop)
		cfg := netnsGatewayConfig(engine, set)
		netnsGatewayInstall(t, cfg)
		st := netnsGatewayState(t, netnsGatewayID)
		if st.gwV4 != netnsGatewayTunHop || routeGuardNarrowed(st) {
			t.Fatalf("a usable gateway on a tun egress routes but does not narrow, got gwV4=%q peers=%v", st.gwV4, st.gwPeersV4)
		}
		want := fmt.Sprintf("default via %s dev %s", netnsGatewayTunHop, netnsGatewayTun)
		if routes := netnsGatewayRoutes(t, st, false); !strings.Contains(routes, want) {
			t.Errorf("the gateway is still the next hop on a tun egress, want %q in:\n%s", want, routes)
		}
		netnsAssertFullGuard(t, engine, st)

		set.Routing.EgressInterface = netnsPrimary
		set.Routing.EgressGateway = netnsGatewayHop
		RoutingSyncConfig(cfg)
		veth := netnsGatewayState(t, netnsGatewayID)
		want = fmt.Sprintf("default via %s dev %s", netnsGatewayHop, netnsPrimary)
		if routes := netnsGatewayRoutes(t, veth, false); !strings.Contains(routes, want) {
			t.Errorf("moved to a veth the set routes through its gateway there, want %q in:\n%s", want, routes)
		}
		netnsAssertNarrowedGuard(t, engine, veth, netnsGatewayHop)
	})
}

func TestNetnsANoPrefixRouteAddressPutsNoNetworkOnLink(t *testing.T) {
	netnsRequire(t)
	const dev, addr, gw, table = "b4np0", "10.201.9.1/24", "10.201.9.5", "4209"
	_, _ = run("ip", "link", "del", dev)
	netnsRun(t, "ip", "link", "add", dev, "type", "veth", "peer", "name", dev+"p")
	t.Cleanup(func() {
		_, _ = run("ip", "link", "del", dev)
		_, _ = run("ip", "route", "flush", "table", table)
	})
	netnsRun(t, "ip", "link", "set", dev, "up")
	netnsRun(t, "ip", "link", "set", dev+"p", "up")
	netnsRun(t, "ip", "addr", "add", addr, "dev", dev, "noprefixroute")

	if usable, reason := routeGatewayUsable(dev, gw); usable || reason != "is not on a network "+dev+" reaches directly" {
		t.Errorf("an address added with noprefixroute puts no route on the link, yet %s counts as usable=%v (%q)", gw, usable, reason)
	}
	if _, err := run("ip", "route", "replace", "default", "via", gw, "dev", dev, "table", table); err == nil {
		t.Fatalf("the kernel took %s as a next hop with no route covering it, so this test no longer pins what the kernel refuses", gw)
	}

	netnsRun(t, "ip", "route", "add", "10.201.9.0/24", "dev", dev)
	if usable, reason := routeGatewayUsable(dev, gw); !usable {
		t.Errorf("a route through %s covers %s, yet it was refused: %s", dev, gw, reason)
	}
	netnsRun(t, "ip", "route", "replace", "default", "via", gw, "dev", dev, "table", table)
}

func TestNetnsARefusedGatewayIsReportedOnlyOnceTheLinkIsUp(t *testing.T) {
	netnsRequire(t)
	familyResetGlobals(t)
	gwForgetWarnings(t)
	const dev = "b4nc0"
	_, _ = run("ip", "link", "del", dev)
	netnsRun(t, "ip", "link", "add", dev, "type", "veth", "peer", "name", dev+"p")
	t.Cleanup(func() { _, _ = run("ip", "link", "del", dev) })
	netnsRun(t, "ip", "link", "set", dev, "up")
	netnsRun(t, "ip", "addr", "add", "10.201.10.1/24", "dev", dev)
	logs := captureTablesLog(t)
	cfg := familyTestConfig(true, false)
	set := gwTestSet("nocarrier", dev, "198.51.100.77")
	cfg.Sets = []*config.SetConfig{set}
	reported := func() int {
		return len(linesWith(strings.Split(logs.String(), "\n"), "set 'nocarrier' names 198.51.100.77 as its gateway"))
	}

	if st := gwResolvedState(cfg, set); st.gwV4 != "" {
		t.Fatalf("a gateway %s cannot reach must count as no gateway, got %q", dev, st.gwV4)
	}
	if n := reported(); n != 0 {
		t.Fatalf("%s is up but has no link, so its addresses may still be on their way, yet the gateway was reported:\n%s", dev, logs.String())
	}
	netnsRun(t, "ip", "link", "set", dev+"p", "up")
	gwResolvedState(cfg, set)
	if n := reported(); n != 1 {
		t.Errorf("with the link up the refused gateway must be reported once, got %d:\n%s", n, logs.String())
	}
}

func netnsGatewaySnapshot(t *testing.T, engine string, ids ...string) string {
	t.Helper()
	var b strings.Builder
	for _, id := range ids {
		st := netnsGatewayState(t, id)
		for _, chain := range []string{st.chainPre, st.chainOut} {
			if engine == backendNFTables {
				b.WriteString(netnsRun(t, "nft", "-a", "list", "chain", "inet", routeNftTable, chain))
				continue
			}
			b.WriteString(netnsRun(t, backendIPTables, "-w", "-t", "mangle", "-S", chain))
		}
		b.WriteString(netnsGatewayRoutes(t, st, false))
	}
	b.WriteString(netnsRun(t, "ip", "rule", "show"))
	return b.String()
}

func TestNetnsMonitorLeavesGatewaySetsAlone(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		a := netnsGatewaySet("netns-gateway-quiet-a", netnsPrimary, netnsGatewayHop)
		b := netnsGatewaySet("netns-gateway-quiet-b", netnsPrimary, netnsGatewayHopPeer)
		refused := netnsGatewaySet("netns-gateway-quiet-c", netnsPrimary, "198.51.100.250")
		cfg := netnsGatewayConfig(engine, a, b, refused)
		cfg.System.Tables.MonitorInterval = 10
		netnsGatewayInstall(t, cfg)
		if !RoutingRulesPresent(cfg) {
			t.Fatal("the presence check reads the gateway sets' freshly built rules as missing, so every tick would rebuild them")
		}
		ids := []string{a.Id, b.Id, refused.Id}
		before := netnsGatewaySnapshot(t, engine, ids...)

		var ptr atomic.Pointer[config.Config]
		ptr.Store(cfg)
		m := NewMonitor(&ptr)
		m.snapshotRoutingIfaces(cfg)
		for tick := 1; tick <= 3; tick++ {
			if acted := m.tick(false); acted || m.lost {
				t.Errorf("tick %d acted=%v lost=%v on gateway sets nothing touched", tick, acted, m.lost)
			}
		}
		if after := netnsGatewaySnapshot(t, engine, ids...); after != before {
			t.Errorf("the monitor rebuilt quiet gateway sets:\nbefore:\n%s\nafter:\n%s", before, after)
		}
		netnsAssertNarrowedGuard(t, engine, netnsGatewayState(t, a.Id), netnsGatewayHop, netnsGatewayHopPeer)
		netnsAssertFullGuard(t, engine, netnsGatewayState(t, refused.Id))
	})
}

func TestNetnsFallbackMarkRuleMatchesTheOriginalDirectionOnly(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		held := routeCTMarkIsHeld()
		routeCTMarkHeld.Store(false)
		t.Cleanup(func() { routeCTMarkHeld.Store(held) })
		netnsGatewayInstall(t, netnsGatewayConfig(engine,
			netnsGatewaySet("netns-gateway-fallback-gw", netnsPrimary, netnsGatewayHop),
			netnsGatewaySet("netns-gateway-fallback-plain", netnsSecondary, "")))
		for _, id := range []string{"netns-gateway-fallback-gw", "netns-gateway-fallback-plain"} {
			st := netnsGatewayState(t, id)
			zero, direction := "-m mark --mark 0x0/0x27fff", "-m conntrack --ctdir ORIGINAL"
			if engine == backendNFTables {
				zero, direction = "meta mark & 0x00027fff == 0x00000000", "ct direction original"
			}
			found := 0
			for _, rule := range netnsGatewayRules(t, engine, st.chainPre, false) {
				if !strings.Contains(rule, zero) || !strings.Contains(rule, st.setV4) {
					continue
				}
				found++
				if !strings.Contains(rule, direction) {
					t.Errorf("the fallback mark rule of %s marks replies too, which sends them back out the egress: %q", id, rule)
				}
			}
			if found != 1 {
				t.Errorf("want one fallback mark rule in %s while the connection mark is not kept, found %d", st.chainPre, found)
			}
		}
	})
}
