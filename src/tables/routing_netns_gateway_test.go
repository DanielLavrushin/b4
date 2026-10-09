package tables

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func netnsGatewayConfig(engine, gw string) *config.Config {
	cfg := netnsConfig(engine)
	set := cfg.Sets[0]
	set.Id = "netns-gateway-set"
	set.Name = "netnsgateway"
	set.Routing.EgressInterface = netnsPrimary
	set.Routing.EgressGateway = gw
	cfg.Sets = []*config.SetConfig{set}
	return cfg
}

func netnsGatewaySync(t *testing.T, engine, gw string) routeState {
	t.Helper()
	routeEngine = nil
	t.Cleanup(func() { routeEngine = nil })
	cfg := netnsGatewayConfig(engine, gw)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() { _ = ClearRules(cfg) })
	RoutingSyncConfig(cfg)
	t.Cleanup(RoutingClearAll)
	st, ok := routeRuleCache["netns-gateway-set"]
	if !ok {
		t.Fatal("the gateway set built no rules")
	}
	return st
}

func netnsGatewayTable(t *testing.T, st routeState) string {
	t.Helper()
	return fmt.Sprintf("%d", st.table)
}

func TestNetnsGatewaySetKeepsItsViaRoute(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			if engine == backendNFTables && !hasBinary("nft") {
				t.Skip("nft is not installed")
			}
			st := netnsGatewaySync(t, engine, netnsPrimaryGW)
			routes := netnsRun(t, "ip", "route", "show", "table", netnsGatewayTable(t, st))
			want := fmt.Sprintf("default via %s dev %s", netnsPrimaryGW, netnsPrimary)
			if !strings.Contains(routes, want) {
				t.Errorf("the set table carries no via route, want %q in:\n%s", want, routes)
			}
			if strings.Contains(routes, "scope link") {
				t.Errorf("no on-link next-hop route belongs in the set table:\n%s", routes)
			}
			chain := netnsPreChain(t, engine, st.chainPre)
			mac := netnsLinkMAC(t, netnsPrimary+"p")
			guards := []string{fmt.Sprintf("ip saddr %s", netnsPrimaryGW), fmt.Sprintf("ether saddr %s", mac)}
			if engine != backendNFTables {
				guards = []string{fmt.Sprintf("-s %s", netnsPrimaryGW), fmt.Sprintf("--mac-source %s", mac)}
			}
			matched := false
			for _, guard := range guards {
				if strings.Contains(chain, guard) {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("the pre chain carries no narrow guard for the gateway:\n%s", chain)
			}
		})
	}
}

func TestNetnsGatewaySetFallsBackToTheInterfaceRoute(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			if engine == backendNFTables && !hasBinary("nft") {
				t.Skip("nft is not installed")
			}
			st := netnsGatewaySync(t, engine, "198.51.100.250")
			routes := netnsRun(t, "ip", "route", "show", "table", netnsGatewayTable(t, st))
			if strings.Contains(routes, "via 198.51.100.250") {
				t.Errorf("an unreachable gateway must not stay the next hop:\n%s", routes)
			}
			if !strings.Contains(routes, "default") {
				t.Errorf("a refused gateway must leave the interface route behind, got:\n%s", routes)
			}
		})
	}
}

func TestNetnsGatewayReinstallKeepsTheViaRoute(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	engine := backendIPTables
	st := netnsGatewaySync(t, engine, netnsPrimaryGW)
	table := netnsGatewayTable(t, st)
	before := netnsRun(t, "ip", "route", "show", "table", table)
	routeEngine = nil
	t.Cleanup(func() { routeEngine = nil })
	RoutingReinstallForInterface(netnsGatewayConfig(engine, netnsPrimaryGW), netnsPrimary)
	after := netnsRun(t, "ip", "route", "show", "table", table)
	want := fmt.Sprintf("default via %s dev %s", netnsPrimaryGW, netnsPrimary)
	if !strings.Contains(after, want) {
		t.Errorf("the reinstall lost the via route, want %q in:\n%s\nwas:\n%s", want, after, before)
	}
}
func TestNetnsGatewaySetSurvivesARedialingProxy(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	dev := netnsStartDevice(t)
	ln, err := net.Listen("tcp4", net.JoinHostPort("", "18080"))
	if err != nil {
		t.Skipf("cannot listen for the redialing proxy: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan int, 1024)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- 1
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				buf := make([]byte, 256)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				if _, err := c.Write(buf[:n]); err != nil {
					return
				}
				back, err := net.DialTimeout("tcp", net.JoinHostPort(netnsTarget, "80"), time.Second)
				if err != nil {
					return
				}
				_ = back.Close()
			}(conn)
		}
	}()
	_ = dev
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			if engine == backendNFTables && !hasBinary("nft") {
				t.Skip("nft is not installed")
			}
			routeEngine = nil
			t.Cleanup(func() { routeEngine = nil })
			cfg := netnsConfig(engine)
			set := cfg.Sets[0]
			set.Id = "netns-gateway-redial"
			set.Name = "netnsgatewayredial"
			set.Routing.EgressInterface = netnsDevLink
			set.Routing.EgressGateway = netnsDevIP
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			t.Cleanup(func() { _ = ClearRules(cfg) })
			RoutingSyncConfig(cfg)
			t.Cleanup(RoutingClearAll)
			st, ok := routeRuleCache["netns-gateway-redial"]
			if !ok {
				t.Fatal("the gateway set built no rules")
			}
			chain := netnsPreChain(t, engine, st.chainPre)
			mac := strings.ToLower(dev.mac)
			guards := []string{fmt.Sprintf("ip saddr %s", netnsDevIP), fmt.Sprintf("ether saddr %s", mac)}
			if engine == backendIPTables {
				guards = []string{fmt.Sprintf("-s %s", netnsDevIP), fmt.Sprintf("--mac-source %s", strings.ToUpper(mac))}
			}
			matched := false
			for _, guard := range guards {
				if strings.Contains(chain, guard) {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("the pre chain carries no narrow guard for the next hop:\n%s", chain)
			}
			if got := dev.probe(t, "tcp", netnsDevRouter, 18080, "client"); !strings.HasPrefix(got, "reply=") {
				t.Fatalf("device -> redialing proxy: %s", got)
			}
			n := 0
		drain:
			for {
				select {
				case <-accepted:
					n++
				default:
					break drain
				}
			}
			if n != 1 {
				t.Errorf("the redialing proxy accepted %d connection(s), want exactly 1: the narrow guard must return the proxy's own redial unmarked", n)
			}
		})
	}
}
func TestNetnsGatewaySetKeepsRouterDestinationsUnmarked(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			if engine == backendNFTables && !hasBinary("nft") {
				t.Skip("nft is not installed")
			}
			routeEngine = nil
			t.Cleanup(func() { routeEngine = nil })
			cfg := netnsConfig(engine)
			set := cfg.Sets[0]
			set.Id = "netns-gateway-localdst"
			set.Name = "netnsgatewaylocaldst"
			set.Routing.EgressInterface = netnsPrimary
			set.Routing.EgressGateway = netnsPrimaryGW
			set.Targets.IPs = []string{"0.0.0.0/0"}
			set.Targets.IpsToMatch = []string{"0.0.0.0/0"}
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			t.Cleanup(func() { _ = ClearRules(cfg) })
			RoutingSyncConfig(cfg)
			t.Cleanup(RoutingClearAll)
			st, ok := routeRuleCache["netns-gateway-localdst"]
			if !ok {
				t.Fatal("the gateway set built no rules")
			}
			chain := netnsPreChain(t, engine, st.chainPre)
			guard := "fib daddr type { local, broadcast, multicast } return"
			if engine == backendIPTables {
				guard = "-m addrtype --dst-type LOCAL"
			}
			if !strings.Contains(chain, guard) {
				t.Errorf("a catch-all gateway set must return router-addressed packets before marking:\n%s", chain)
			}
		})
	}
}
func TestNetnsGatewayOfOneFamilyLeavesTheOtherOnMain(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	engine := backendIPTables
	routeEngine = nil
	t.Cleanup(func() { routeEngine = nil })
	cfg := netnsConfig(engine)
	cfg.Queue.IPv6Enabled = true
	set := cfg.Sets[0]
	set.Id = "netns-gateway-dual"
	set.Name = "netnsgatewaydual"
	set.Routing.EgressInterface = netnsPrimary
	set.Routing.EgressGateway = netnsPrimaryGW
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() { _ = ClearRules(cfg) })
	RoutingSyncConfig(cfg)
	t.Cleanup(RoutingClearAll)
	st, ok := routeRuleCache["netns-gateway-dual"]
	if !ok {
		t.Fatal("the gateway set built no rules")
	}
	chain := netnsPreChain(t, engine, st.chainPre)
	mac := netnsLinkMAC(t, netnsPrimary+"p")
	if !strings.Contains(chain, "--mac-source "+mac) && !strings.Contains(chain, fmt.Sprintf("-s %s", netnsPrimaryGW)) {
		t.Errorf("the next hop must be guarded by its MAC or, without a neighbor entry, its address:\n%s", chain)
	}
}
func TestNetnsIPv6GatewaySurvivesAForcedResync(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	engine := backendIPTables
	routeEngine = nil
	t.Cleanup(func() { routeEngine = nil })
	cfg := netnsConfig(engine)
	cfg.Queue.IPv6Enabled = true
	set := cfg.Sets[0]
	set.Id = "netns-gateway-v6"
	set.Name = "netnsgatewayv6"
	set.Routing.EgressInterface = netnsPrimary
	set.Routing.EgressGateway = "2001:db8::1"
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() { _ = ClearRules(cfg) })
	RoutingSyncConfig(cfg)
	t.Cleanup(RoutingClearAll)
	st, ok := routeRuleCache["netns-gateway-v6"]
	if !ok {
		t.Fatal("the gateway set built no rules")
	}
	table, mark := fmt.Sprintf("%d", st.table), fmt.Sprintf("0x%x/0x%x", st.mark, routeSetMarkMask)
	before, _ := run("ip", "-6", "rule", "show")
	RoutingForceResync(cfg)
	after, _ := run("ip", "-6", "rule", "show")
	still, ok := routeRuleCache["netns-gateway-v6"]
	if !ok || still.table != st.table || still.mark != st.mark {
		t.Fatalf("the resync moved the set off table %s mark %s", table, mark)
	}
	if !strings.Contains(after, mark) || !strings.Contains(after, "lookup "+table) {
		t.Errorf("the v6 policy rule changed across the resync:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
