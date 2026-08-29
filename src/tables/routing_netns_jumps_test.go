package tables

import (
	"os/exec"
	"testing"
)

func TestNetnsAFlushedBaseChainIsNoticed(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsConfig(backendIPTables)
	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	if !RoutingRulesPresent(cfg) {
		t.Fatalf("b4 has just installed its routing, so it must read as present")
	}

	if out, err := exec.Command("iptables", "-t", "mangle", "-F", "PREROUTING").CombinedOutput(); err != nil {
		t.Fatalf("flush PREROUTING: %v: %s", err, out)
	}

	if RoutingRulesPresent(cfg) {
		t.Fatalf("the firmware rebuilt its own firewall and took b4's jump out of mangle PREROUTING with it. " +
			"b4's own chains survive that, so checking only that they exist reads as healthy while no packet " +
			"reaches them: the set silently stops routing and nothing puts it back")
	}

	RoutingForceResync(cfg)
	if !RoutingRulesPresent(cfg) {
		t.Fatalf("a resync must put the jump back")
	}
}
