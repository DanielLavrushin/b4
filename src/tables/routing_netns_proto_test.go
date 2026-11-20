package tables

import (
	"strings"
	"sync"
	"testing"
)

func netnsProbeRouteProtocolsAfresh(t *testing.T) {
	t.Helper()
	prev := routeIPSupportsProto
	routeIPSupportsProto = routeIPSupportsProtoExec
	routeProtoOnce = sync.Once{}
	routeProtoOK = false
	t.Cleanup(func() {
		routeIPSupportsProto = prev
		routeProtoOnce = sync.Once{}
		routeProtoOK = false
	})
}

func netnsPretendRouteProtocolsUnknown(t *testing.T) {
	t.Helper()
	prev := routeIPSupportsProto
	routeIPSupportsProto = routeIPSupportsProtoExec
	routeProtoOnce = sync.Once{}
	routeProtoOnce.Do(func() {})
	routeProtoOK = false
	t.Cleanup(func() {
		routeIPSupportsProto = prev
		routeProtoOnce = sync.Once{}
		routeProtoOK = false
	})
}

func netnsRouteLine(routes, prefix string) string {
	for _, line := range strings.Split(routes, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

func TestNetnsAKillSwitchSetTagsItsDefaultRouteAndBlackholeWithB4sProtocol(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		netnsProbeRouteProtocolsAfresh(t)
		set := netnsGatewaySet("netns-proto-set", netnsPrimary, "")
		set.Routing.KillSwitch = true
		netnsGatewayInstall(t, netnsGatewayConfig(engine, set))

		routes := netnsGatewayRoutes(t, netnsGatewayState(t, set.Id), false)
		def := netnsRouteLine(routes, "default ")
		if def == "" || !strings.Contains(def, "dev "+netnsPrimary) {
			t.Fatalf("the set's table has no default route through %s:\n%s", netnsPrimary, routes)
		}
		if routeRuleField(def, "proto") != routeProtoID {
			t.Errorf("the set's default route carries no \"proto %s\", so b4 cannot tell it from a route another service put in the table, and a crash leaves it behind for good; iproute2 here understands route protocols:\n%s", routeProtoID, routes)
		}
		hole := netnsRouteLine(routes, "blackhole default")
		if hole == "" || routeRuleField(hole, "metric") != routeKillSwitchMetric {
			t.Fatalf("the kill switch armed no blackhole at metric %s:\n%s", routeKillSwitchMetric, routes)
		}
		if routeRuleField(hole, "proto") != routeProtoID {
			t.Errorf("the kill switch blackhole carries no \"proto %s\":\n%s", routeProtoID, routes)
		}
	})
}

func TestNetnsTheFirstStartAfterAnOlderBuildCrashedTakesBackTheTableItLeftUntagged(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		set := netnsGatewaySet("netns-untagged-set", netnsPrimary, "")
		set.Routing.KillSwitch = true
		cfg := netnsGatewayConfig(engine, set)
		if err := AddRules(cfg); err != nil {
			t.Fatalf("AddRules: %v", err)
		}
		t.Cleanup(func() { _ = ClearRules(cfg) })
		t.Cleanup(RoutingClearAll)

		netnsPretendRouteProtocolsUnknown(t)
		RoutingSyncConfig(cfg)
		older := netnsGatewayState(t, set.Id)
		left := netnsGatewayRoutes(t, older, false)
		if netnsRouteLine(left, "default ") == "" || netnsRouteLine(left, "blackhole default") == "" || strings.Contains(left, " proto ") {
			t.Fatalf("the older build should have left an untagged default route and blackhole in table %d:\n%s", older.table, left)
		}

		routeMu.Lock()
		routeEngine = nil
		routeMu.Unlock()
		netnsProbeRouteProtocolsAfresh(t)
		RoutingClearAll()
		RoutingSyncConfig(cfg)

		now := netnsGatewayState(t, set.Id)
		if now.table != older.table {
			t.Fatalf("the first start after the crash judged table %d foreign because of the untagged routes the older build left there and moved the set to table %d; table %d still holds:\n%s",
				older.table, now.table, older.table, netnsGatewayRoutes(t, older, false))
		}
		routes := netnsGatewayRoutes(t, now, false)
		for _, line := range strings.Split(strings.TrimSpace(routes), "\n") {
			if routeRuleField(line, "proto") != routeProtoID {
				t.Errorf("table %d still holds a route without \"proto %s\" after the first start: %q\n%s", now.table, routeProtoID, line, routes)
			}
		}
	})
}
