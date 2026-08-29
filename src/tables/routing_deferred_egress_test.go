package tables

import (
	"strings"
	"testing"
)

func deferredEgressCommands(t *testing.T, iface string, killSwitch bool) string {
	t.Helper()
	prev := runLogged
	var cmds []string
	runLogged = func(op string, args ...string) bool { cmds = append(cmds, strings.Join(args, " ")); return true }
	t.Cleanup(func() { runLogged = prev })

	st := loopTestState(iface, false)
	st.setID = "deferred"
	st.killSwitch = killSwitch

	routeEnsurePolicyRouting(st, true, false)
	return strings.Join(cmds, "\n")
}

func TestAbsentEgressInterfaceInstallsNoBlackhole(t *testing.T) {
	emitted := deferredEgressCommands(t, "b4-no-such-iface0", true)

	if strings.Contains(emitted, "route replace blackhole default") {
		t.Fatalf("a set whose egress interface has never existed installed a blackhole default route. "+
			"A blackhole does not fall through to the main table, it ends the lookup with EINVAL, so every "+
			"address the set matches is dropped inside the kernel with no ICMP and no reset. With a large "+
			"geosite set that is most of what the network browses, and the router stops forwarding:\n%s", emitted)
	}
}

func TestAbsentEgressInterfaceInstallsNoPolicyRule(t *testing.T) {
	emitted := deferredEgressCommands(t, "b4-no-such-iface0", true)

	if strings.Contains(emitted, "rule add fwmark") {
		t.Fatalf("a set whose egress interface has never existed installed its fwmark rule, steering marked "+
			"packets into a table that cannot carry them:\n%s", emitted)
	}
}

func TestAbsentEgressInterfaceClearsAStaleBlackhole(t *testing.T) {
	emitted := deferredEgressCommands(t, "b4-no-such-iface0", true)

	if !strings.Contains(emitted, "route del blackhole default") {
		t.Fatalf("a blackhole left in the table by an earlier sync must be taken out when the interface is "+
			"gone, or it keeps dropping traffic with no rule left to explain it:\n%s", emitted)
	}
}

func TestPresentEgressInterfaceStillGetsItsRuleAndKillSwitch(t *testing.T) {
	emitted := deferredEgressCommands(t, "lo", true)

	if !strings.Contains(emitted, "rule add fwmark") {
		t.Fatalf("an interface that exists must still get its policy rule:\n%s", emitted)
	}
	if !strings.Contains(emitted, "route replace blackhole default") {
		t.Fatalf("the kill switch must still arm for an interface that exists, that is the whole feature:\n%s", emitted)
	}
}

func TestAVanishedInterfaceKeepsItsKillSwitch(t *testing.T) {
	prev := routeIfaceSeen
	routeIfaceSeen = map[string]bool{"b4-gone0": true}
	t.Cleanup(func() { routeIfaceSeen = prev })

	emitted := deferredEgressCommands(t, "b4-gone0", true)

	if !strings.Contains(emitted, "route replace blackhole default") {
		t.Fatalf("an interface that carried the set and then went away must blackhole, not leak. Without the "+
			"blackhole the mark rule still fires, no rule matches the mark, the lookup falls through to the "+
			"main table and the set leaves by the ordinary uplink with the router's real address, which is "+
			"exactly what the kill switch exists to stop:\n%s", emitted)
	}
	if !strings.Contains(emitted, "rule add fwmark") {
		t.Fatalf("the blackhole is only reached through the set's own rule, so the rule has to stay:\n%s", emitted)
	}
}

func TestTheTableIsPopulatedBeforeTrafficIsPointedAtIt(t *testing.T) {
	emitted := deferredEgressCommands(t, "lo", true)

	route := strings.Index(emitted, "route replace default")
	rule := strings.Index(emitted, "rule add fwmark")
	if route < 0 || rule < 0 {
		t.Fatalf("want both a default route and a policy rule:\n%s", emitted)
	}
	if route > rule {
		t.Fatalf("the policy rule went in before the table had a route in it. For the window between the two "+
			"the table holds nothing but the kill switch, so every packet the set matches is dropped, and if "+
			"the route install fails it stays that way:\n%s", emitted)
	}
}

func TestTheLivePolicyRuleIsNeverDeletedBeforeItIsReAdded(t *testing.T) {
	prev := routeDelRuleLoop
	var deleted []string
	routeDelRuleLoop = func(ipv6 bool, mark, table string) { deleted = append(deleted, mark) }
	t.Cleanup(func() { routeDelRuleLoop = prev })

	const mark = uint32(0x4d05)
	routeDelStaleRuleForms(mark, "169")

	live := routeSetMarkRule(mark)
	for _, m := range deleted {
		if m == live {
			t.Fatalf("b4 asked to delete %q, the very rule it is about to add back. Between the two the packet "+
				"carries the set's mark and nothing points it at the set's table, so it takes the main table "+
				"and leaves by the ordinary uplink with the router's own address", live)
		}
	}
}

func TestStaleRuleShapesAreStillCleanedUp(t *testing.T) {
	prev := routeDelRuleLoop
	var deleted []string
	routeDelRuleLoop = func(ipv6 bool, mark, table string) {
		if !ipv6 {
			deleted = append(deleted, mark)
		}
	}
	t.Cleanup(func() { routeDelRuleLoop = prev })

	const mark = uint32(0x4d05)
	routeDelStaleRuleForms(mark, "169")

	want := []string{"0x4d05", "0x4d05/0x4d05"}
	for _, w := range want {
		found := false
		for _, d := range deleted {
			if d == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("an older b4 wrote the rule as %q, and a leftover copy still steers traffic into the "+
				"set's table, so it has to go even though the shape in use stays: deleted %v", w, deleted)
		}
	}
}
