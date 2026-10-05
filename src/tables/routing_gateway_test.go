package tables

import (
	"strings"
	"testing"
)

// gatewayRouteCommands runs routeReplaceDefaultRoute with every command
// captured. ifaceRoutes is what `ip route show default dev <iface>` answers,
// so the fallback path can be given a gateway of its own or none at all, and
// refuseVia makes the "via" route come back failed the way the kernel does
// when the next hop does not resolve.
func gatewayRouteCommands(t *testing.T, iface, src, gw, table, ifaceRoutes string, ipv6, refuseVia bool) []string {
	t.Helper()

	prevRun := run
	t.Cleanup(func() { run = prevRun })
	run = func(args ...string) (string, error) { return ifaceRoutes, nil }

	prev := runLogged
	var cmds []string
	runLogged = func(op string, args ...string) bool {
		if refuseVia && contains(args, "via") {
			return false
		}
		cmds = append(cmds, strings.Join(args, " "))
		return true
	}
	t.Cleanup(func() { runLogged = prev })

	routeReplaceDefaultRoute(iface, src, gw, table, ipv6)
	return cmds
}

func contains(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func joined(cmds []string) string { return strings.Join(cmds, "\n") }

func TestASetGatewayBecomesTheNextHopOfItsDefaultRoute(t *testing.T) {
	cmds := gatewayRouteCommands(t, "mihomo", "10.99.0.2", "10.99.0.1", "132", "", false, false)
	got := joined(cmds)

	// A TUN carries a host address, so nothing resolves the next hop for the
	// kernel and `via` is refused unless the route to it is written first.
	if !strings.Contains(got, "route replace 10.99.0.1 dev mihomo scope link") || !strings.Contains(got, "table 132") {
		t.Errorf("the next hop itself must be routed into the set's table, got:\n%s", got)
	}
	if !strings.Contains(got, "route replace default via 10.99.0.1 dev mihomo") || !strings.Contains(got, "src 10.99.0.2") {
		t.Errorf("the set's default route must go through the gateway it asked for, got:\n%s", got)
	}
	if got := cmds[len(cmds)-1]; !strings.Contains(got, "via 10.99.0.1") {
		t.Errorf("the gateway route must be the last one written, so a refusal falls through to the fallback: %q", got)
	}
}

func TestTheNextHopRouteIsTakenBackWithTheDefaultOne(t *testing.T) {
	prev := runLogged
	var cmds []string
	runLogged = func(op string, args ...string) bool { cmds = append(cmds, strings.Join(args, " ")); return true }
	t.Cleanup(func() { runLogged = prev })

	routeDeleteOwnRoutes("mihomo", "10.99.0.1", "132")

	got := joined(cmds)
	// Left behind, this one sits in the table until the number is handed to
	// another set, and a set that changed its gateway would collect them.
	if !strings.Contains(got, "ip route del 10.99.0.1 dev mihomo table 132") {
		t.Errorf("the IPv4 next hop must be removed with the default route, got:\n%s", got)
	}
	if strings.Contains(got, "ip -6 route del 10.99.0.1") {
		t.Errorf("an IPv4 next hop has no place in the IPv6 route table, got:\n%s", got)
	}
}

func TestASetGatewayWinsOverTheOneFoundOnTheInterface(t *testing.T) {
	cmds := gatewayRouteCommands(t, "eth1", "", "192.0.2.254", "140",
		"default via 192.0.2.1 dev eth1 proto static\n", false, false)
	got := joined(cmds)

	if strings.Contains(got, "via 192.0.2.1") {
		t.Errorf("the gateway read off the interface must not be used once the set names one, got:\n%s", got)
	}
	if !strings.Contains(got, "default via 192.0.2.254 dev eth1") {
		t.Errorf("the set's own gateway must win, got:\n%s", got)
	}
}

func TestARefusedGatewayLeavesTheSetWithARoute(t *testing.T) {
	cmds := gatewayRouteCommands(t, "mihomo", "10.99.0.2", "192.0.2.254", "132", "", false, true)
	got := joined(cmds)

	// The table must never be left without a default route: an empty table
	// falls through to the main one and the set's traffic leaves by the
	// ordinary uplink, which is what routing exists to prevent.
	last := cmds[len(cmds)-1]
	if !strings.Contains(last, "route replace default dev mihomo") || contains(strings.Fields(last), "via") {
		t.Errorf("a refused gateway must fall back to the interface route, got %q", last)
	}
	if !strings.Contains(got, "route replace 192.0.2.254 dev mihomo scope link") {
		t.Errorf("the next hop route is still worth keeping in place, got:\n%s", got)
	}
}

func TestAnInterfaceWithoutAGatewayKeepsTheRouteItHadBefore(t *testing.T) {
	cmds := gatewayRouteCommands(t, "mihomo", "10.99.0.2", "", "132", "", false, false)
	got := joined(cmds)

	if contains(strings.Fields(got), "via") {
		t.Errorf("with no gateway b4 must behave exactly as before, got:\n%s", got)
	}
	if !strings.Contains(got, "route replace default dev mihomo") {
		t.Errorf("expected the plain interface route, got:\n%s", got)
	}
}

func TestAGatewayOfOneFamilyOnlyReachesThatFamily(t *testing.T) {
	if got := routeAddrForFamily("192.0.2.254", true); got != "" {
		t.Errorf("an IPv4 gateway must not be offered to the IPv6 route, got %q", got)
	}
	if got := routeAddrForFamily("2001:DB8::254", false); got != "" {
		t.Errorf("an IPv6 gateway must not be offered to the IPv4 route, got %q", got)
	}
	if got := routeAddrForFamily("2001:DB8::254", true); got != "2001:db8::254" {
		t.Errorf("the IPv6 gateway must be canonicalized for the IPv6 route, got %q", got)
	}
}

func TestSetsWithDifferentGatewaysStopSharingATable(t *testing.T) {
	base := routeIfaceAutoKey("eth1", "192.0.2.10", "", false)
	one := routeIfaceAutoKey("eth1", "192.0.2.10", "192.0.2.1", false)
	two := routeIfaceAutoKey("eth1", "192.0.2.10", "192.0.2.2", false)

	// One table cannot hold two default routes, so the gateway has to keep
	// sets apart, not just mark them.
	if one == two {
		t.Fatalf("two gateways share the key %q, so the second route replace would overwrite the first", one)
	}
	if one == base {
		t.Errorf("a set with a gateway must not join the group of sets without one: %q", one)
	}
}

func TestASetWithoutAGatewayKeepsTheMarkAndTableItAlreadyHad(t *testing.T) {
	// The key feeds the hash the mark and table come from. Adding the gateway
	// to every key would move every installed set onto a new mark and table
	// on upgrade, so an empty gateway must leave the key exactly as it was.
	if got := routeIfaceAutoKey("eth1", "192.0.2.10", "", false); got != "eth1|192.0.2.10" {
		t.Errorf("the key of a set without a gateway changed: got %q", got)
	}
	if got := routeIfaceAutoKey("eth1", "192.0.2.10", "", true); got != "eth1|192.0.2.10|ks" {
		t.Errorf("the kill switch must still land last: got %q", got)
	}
	if got := routeIfaceAutoKey("eth1", "192.0.2.10", "192.0.2.1", true); got != "eth1|192.0.2.10|gw=192.0.2.1|ks" {
		t.Errorf("the gateway must sit before the kill switch: got %q", got)
	}
}
