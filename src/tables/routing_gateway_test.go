package tables

import (
	"errors"
	"strings"
	"testing"
)

// captureRoutes installs the stubs the route helpers run through and returns
// where their commands land. ifaceRoutes is what `ip route show default dev
// <iface>` answers, so the fallback path can be given a gateway of its own or
// none at all, and refuseVia makes the "via" route come back failed the way the
// kernel does when the next hop does not resolve.
func captureRoutes(t *testing.T, ifaceRoutes string, refuseVia bool) *[]string {
	t.Helper()

	prevRun := run
	t.Cleanup(func() { run = prevRun })
	run = func(args ...string) (string, error) {
		if contains(args, "main") {
			return "10.99.0.0/24 dev mihomo proto kernel scope link\n192.0.2.0/24 dev eth1 proto kernel scope link\n2001:db8::/64 dev mihomo proto kernel scope link\n", nil
		}
		return ifaceRoutes, nil
	}
	prevProto := routeIPSupportsProto
	t.Cleanup(func() { routeIPSupportsProto = prevProto })
	routeIPSupportsProto = func() bool { return false }
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

	return &cmds
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

// last is the command written last, which is the one a refusal has to leave
// behind: the route before it is not what the set ends up with.
func last(cmds *[]string) string { return (*cmds)[len(*cmds)-1] }

func TestASetGatewayBecomesTheNextHopOfItsDefaultRoute(t *testing.T) {
	cmds := captureRoutes(t, "", false)
	routeReplaceDefaultRoute("mihomo", "10.99.0.2", "10.99.0.1", "132", false)
	got := joined(*cmds)
	if strings.Contains(got, "scope link") {
		t.Errorf("the gateway needs no on-link route of its own, got:\n%s", got)
	}
	if !strings.Contains(got, "route replace default via 10.99.0.1 dev mihomo") || !strings.Contains(got, "src 10.99.0.2") {
		t.Errorf("the set's default route must go through the gateway it asked for, got:\n%s", got)
	}
	if got := last(cmds); !strings.Contains(got, "via 10.99.0.1") {
		t.Errorf("the gateway route must be the last one written, so a refusal falls through to the fallback: %q", got)
	}
}
func TestOwnRoutesTakeBackTheDefaultAndTheKillSwitch(t *testing.T) {
	cmds := captureRoutes(t, "", false)
	routeDeleteOwnRoutes("mihomo", "132")
	got := joined(*cmds)
	if !strings.Contains(got, "ip route del default dev mihomo table 132") {
		t.Errorf("the IPv4 default route must be removed, got:\n%s", got)
	}
	if !strings.Contains(got, "blackhole default metric 4096 table 132") {
		t.Errorf("the kill switch must be removed with the default route, got:\n%s", got)
	}
	if strings.Contains(got, "scope link") || strings.Contains(got, "10.99.0.1") {
		t.Errorf("no next-hop route exists anymore, got:\n%s", got)
	}
}

func TestASetGatewayWinsOverTheOneFoundOnTheInterface(t *testing.T) {
	cmds := captureRoutes(t, "default via 192.0.2.1 dev eth1 proto static\n", false)
	routeReplaceDefaultRoute("eth1", "", "192.0.2.254", "140", false)
	got := joined(*cmds)

	if strings.Contains(got, "via 192.0.2.1") {
		t.Errorf("the gateway read off the interface must not be used once the set names one, got:\n%s", got)
	}
	if !strings.Contains(got, "default via 192.0.2.254 dev eth1") {
		t.Errorf("the set's own gateway must win, got:\n%s", got)
	}
}

func TestAnIPv6GatewayReachesOnlyTheIPv6Route(t *testing.T) {
	cmds := captureRoutes(t, "", false)
	routeReplaceDefaultRoute("mihomo", "2001:db8::2", "2001:db8::1", "132", true)
	got := joined(*cmds)
	if strings.Contains(got, "scope link") {
		t.Errorf("the gateway needs no on-link route of its own, got:\n%s", got)
	}
	if !strings.Contains(got, "ip -6 route replace default via 2001:db8::1 dev mihomo") || !strings.Contains(got, "src 2001:db8::2") {
		t.Errorf("the set's IPv6 default route must go through the gateway it asked for, got:\n%s", got)
	}
	if strings.Contains(got, "ip route") {
		t.Errorf("an IPv6 gateway must not reach the IPv4 route table, got:\n%s", got)
	}
}

func TestARefusedGatewayLeavesTheSetWithARoute(t *testing.T) {
	cmds := captureRoutes(t, "", true)
	routeReplaceDefaultRoute("mihomo", "10.99.0.2", "10.99.0.9", "132", false)
	got := joined(*cmds)
	fallback := last(cmds)
	if !strings.Contains(fallback, "route replace default dev mihomo") || contains(strings.Fields(fallback), "via") {
		t.Errorf("a refused gateway must fall back to the interface route, got %q", fallback)
	}
	if strings.Contains(got, "scope link") {
		t.Errorf("the refused gateway leaves no on-link route behind, got:\n%s", got)
	}
}

func TestAnInterfaceWithoutAGatewayKeepsTheRouteItHadBefore(t *testing.T) {
	cmds := captureRoutes(t, "", false)
	routeReplaceDefaultRoute("mihomo", "10.99.0.2", "", "132", false)
	got := joined(*cmds)

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
func TestAGatewayIsReachableThroughItsOwnSubnetOrMainTable(t *testing.T) {
	prev := run
	t.Cleanup(func() { run = prev })
	run = func(args ...string) (string, error) {
		return "192.0.2.0/24 dev b4test0 proto kernel scope link\n203.0.113.0/24 dev other0 proto kernel scope link\n", nil
	}
	for _, c := range []struct {
		iface, gw string
		ok        bool
	}{
		{"b4test0", "192.0.2.5", true},
		{"lo", "127.0.0.2", true},
		{"b4test0", "198.51.100.5", false},
		{"other0", "192.0.2.5", false},
		{"b4test0", "192.0.2.0", false},
		{"b4test0", "192.0.2.255", false},
		{"b4test0", "127.0.0.1", false},
		{"", "192.0.2.5", false},
		{"b4test0", "not-an-ip", false},
	} {
		if got := routeGatewayReachable(c.iface, c.gw); got != c.ok {
			t.Errorf("routeGatewayReachable(%q, %q) = %v, want %v", c.iface, c.gw, got, c.ok)
		}
	}
}
func TestAGatewayNarrowsTheEgressGuardToTheNextHop(t *testing.T) {
	loopTestSysfs(t)
	localGuardReset(t)
	prevProto := routeIPSupportsProto
	t.Cleanup(func() { routeIPSupportsProto = prevProto })
	routeIPSupportsProto = func() bool { return false }
	prevRun, prevLogged := run, runLogged
	run = func(args ...string) (string, error) { return "", errors.New("no commands in unit test") }
	runLogged = func(op string, args ...string) bool { return true }
	t.Cleanup(func() { run, runLogged = prevRun, prevLogged })
	rpFilterHarness(t, map[string]string{"eth1": "1"})
	prevSeen := routeIfaceSeen
	routeIfaceSeen = make(map[string]bool)
	t.Cleanup(func() { routeIfaceSeen = prevSeen })
	cfg := familyTestConfig(true, false)
	be := &mockRouteBackend{}
	set := familyTestSet()
	set.Routing.EgressInterface = "eth1"
	set.Routing.EgressGateway = "192.0.2.1"
	st := buildRouteState(cfg, set)
	if err := routeEnsureRule(be, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureRule: %v", err)
	}
	ops := be.chainOps[st.chainPre]
	if indexOfPrefix(ops, "loop-guard") >= 0 {
		t.Errorf("a gateway set narrows the guard to the next hop instead of the full guard: %v", ops)
	}
	if indexOfOp(ops, "narrow-guard eth1 192.0.2.1 ") < 0 {
		t.Errorf("the v4 next hop must be guarded by its own address: %v", ops)
	}
}
func TestNarrowGuardKeepsAFamilyGuardWhereTheGatewayDoesNotReach(t *testing.T) {
	var cmds []string
	prev := runLogged
	runLogged = func(op string, args ...string) bool { cmds = append(cmds, strings.Join(args, " ")); return true }
	t.Cleanup(func() { runLogged = prev })
	if !(&routeNftBackend{}).addNarrowEgressGuard("b4r_x_pre", "eth1", "192.0.2.1", "", true, true) {
		t.Fatal("both families installable must report success")
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, `iifname "eth1" ip saddr 192.0.2.1 return`) {
		t.Errorf("the v4 gateway must be guarded by its address:\n%s", joined)
	}
	if !strings.Contains(joined, `iifname "eth1" meta nfproto ipv6 return`) {
		t.Errorf("the family without a gateway keeps the full guard:\n%s", joined)
	}
}
func TestNarrowGuardFallsBackWhenTheMACMatchIsRejected(t *testing.T) {
	stubBinaries(t, backendIPTables, backendIP6Tables)
	var cmds []string
	prev := runLogged
	runLogged = func(op string, args ...string) bool {
		cmds = append(cmds, strings.Join(args, " "))
		return !strings.Contains(strings.Join(args, " "), "--mac-source")
	}
	t.Cleanup(func() { runLogged = prev })
	if !(&routeIptBackend{}).addNarrowEgressGuard("b4r_x_pre", "eth1", "", "aa:bb:cc:dd:ee:ff", false, true) {
		t.Fatal("the full-guard fallback must still report success")
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "-i eth1 -j RETURN") {
		t.Errorf("a rejected MAC match must fall back to the full guard:\n%s", joined)
	}
}
func TestGatewayMACChangeRebuildsTheChain(t *testing.T) {
	a := routeState{iface: "eth1", egressGW: "192.0.2.1"}
	b := a
	b.gwMAC = "aa:bb:cc:dd:ee:ff"
	if routeStateEqual(a, b) {
		t.Error("a new neighbor MAC must rebuild the chain, or the narrow guard keeps matching the old one")
	}
}
func TestGatewayMACComesFromAUsableNeighborEntry(t *testing.T) {
	prev := run
	t.Cleanup(func() { run = prev })
	for _, c := range []struct {
		line string
		want string
	}{
		{"192.0.2.1 dev eth1 lladdr aa:bb:cc:dd:ee:ff REACHABLE", "aa:bb:cc:dd:ee:ff"},
		{"192.0.2.1 dev eth1 lladdr aa:bb:cc:dd:ee:ff STALE", "aa:bb:cc:dd:ee:ff"},
		{"192.0.2.1 dev eth1 lladdr aa:bb:cc:dd:ee:ff DELAY", "aa:bb:cc:dd:ee:ff"},
		{"192.0.2.1 dev eth1 lladdr aa:bb:cc:dd:ee:ff PROBE", "aa:bb:cc:dd:ee:ff"},
		{"192.0.2.1 dev eth1 lladdr aa:bb:cc:dd:ee:ff PERMANENT", "aa:bb:cc:dd:ee:ff"},
		{"192.0.2.1 dev eth1 lladdr aa:bb:cc:dd:ee:ff FAILED", ""},
		{"192.0.2.1 dev eth1 INCOMPLETE", ""},
		{"", ""},
	} {
		run = func(args ...string) (string, error) { return c.line, nil }
		if got := routeGatewayMAC("eth1", "192.0.2.1"); got != c.want {
			t.Errorf("routeGatewayMAC(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}
