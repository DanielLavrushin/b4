package tables

import (
	"net"
	"strconv"
	"testing"
)

func TestNetnsASetWithNoFreeTableLeavesTheRouterAlone(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()
	RoutingClearAll()

	for table := 100; table <= 249; table++ {
		netnsRun(t, "ip", "route", "replace", "default", "via", netnsPrimaryGW, "dev", netnsPrimary, "table", strconv.Itoa(table))
	}
	defer func() {
		for table := 100; table <= 249; table++ {
			_, _ = run("ip", "route", "flush", "table", strconv.Itoa(table))
		}
	}()

	rulesBefore := netnsRuleLines(t)
	mainBefore := netnsRun(t, "ip", "route", "show", "table", "main")

	cfg := netnsConfig(backendIPTables)
	RoutingSyncConfig(cfg)
	defer RoutingClearAll()
	RoutingHandleDNS(cfg, cfg.Sets[0], []net.IP{net.ParseIP(netnsTarget)})

	if _, cached := routeRuleCache[cfg.Sets[0].Id]; cached {
		t.Errorf("every table from 100 to 249 belongs to another program, yet a DNS answer installed the set")
	}
	if got := netnsRuleLines(t); got != rulesBefore {
		t.Errorf("the router's routing rules changed; with no table of its own the set must leave them alone.\nbefore:\n%s\nafter:\n%s", rulesBefore, got)
	}
	if got := netnsRun(t, "ip", "route", "show", "table", "main"); got != mainBefore {
		t.Errorf("the main routing table changed.\nbefore:\n%s\nafter:\n%s", mainBefore, got)
	}
}
