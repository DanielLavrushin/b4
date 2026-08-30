package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestNoStaleRuleShapeIsDeletedWithoutAMask(t *testing.T) {
	const mark = uint32(0x6c53)

	for _, shape := range routeStaleMarkRules(mark) {
		if strings.Contains(shape, "/") {
			continue
		}
		t.Fatalf("b4 asks the kernel to delete the rule shape %q, which carries no mask, and iproute2 leaves "+
			"FRA_FWMASK out of a request written that way. Before Linux 4.18 a delete never compared the mask "+
			"unless that attribute was there, so the request matches the rule b4 has just added as %q and takes "+
			"it out again. On a Keenetic, an older OpenWrt or a Padavan the set is then marked with nothing "+
			"pointing at its table and everything it matches leaves by the ordinary uplink", shape, routeSetMarkRule(mark))
	}
}

func TestTheLegacyRuleShapeIsStillAskedForByItsRealMask(t *testing.T) {
	const mark = uint32(0x6c53)

	want := "0x6c53/0xffffffff"
	found := false
	for _, shape := range routeStaleMarkRules(mark) {
		if shape == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("an older b4 wrote the rule as a bare fwmark, which the kernel stores with a mask of all ones, "+
			"so %q is the shape that matches it exactly and nothing else. Without it that leftover rule stays and "+
			"keeps steering traffic into the set's table: %v", want, routeStaleMarkRules(mark))
	}
}

func policyRuleReconcile(t *testing.T, present []string) []string {
	t.Helper()

	prevLines, prevLogged, prevCache, prevGone, prevSeen :=
		routeRuleLines, runLogged, routeRuleCache, routePolicyRuleGone, routeIfaceSeen
	t.Cleanup(func() {
		routeRuleLines = prevLines
		runLogged = prevLogged
		routeRuleCache = prevCache
		routePolicyRuleGone = prevGone
		routeIfaceSeen = prevSeen
	})

	hasBinaryCache.Store("ip", true)

	routeRuleLines = func(fam routeFamily) ([]string, bool) { return present, true }
	var cmds []string
	runLogged = func(op string, args ...string) bool { cmds = append(cmds, strings.Join(args, " ")); return true }

	routeRuleCache = map[string]routeState{
		"set-a": {mode: config.RoutingModeInterface, mark: 0x6c53, table: 190, iface: "nwg1"},
	}
	routePolicyRuleGone = make(map[string]bool)
	routeIfaceSeen = map[string]bool{"nwg1": true}

	routeReconcilePolicyRules(true, false)
	return cmds
}

func TestAPolicyRuleThatIsNotInTheKernelIsPutBack(t *testing.T) {
	cmds := policyRuleReconcile(t, []string{
		"0:\tfrom all lookup local",
		"32766:\tfrom all lookup main",
	})

	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "ip rule add fwmark 0x6c53/0x27fff lookup 190 priority 10190") {
		t.Fatalf("a set's firewall rules are watched and restored every monitor pass while the policy rule they "+
			"depend on was never read back, so a set whose rule never went in or was taken out by the firmware "+
			"reads as healthy while every address it matches leaves by the ordinary uplink:\n%s", joined)
	}
}

func TestAPolicyRuleAlreadyInTheKernelIsLeftAlone(t *testing.T) {
	cmds := policyRuleReconcile(t, []string{
		"0:\tfrom all lookup local",
		"10190:\tfrom all fwmark 0x6c53/0x27fff lookup 190",
		"32766:\tfrom all lookup main",
	})

	if len(cmds) != 0 {
		t.Fatalf("the rule was already there, and adding it a second time stacks a duplicate at the same "+
			"priority on every monitor pass: %v", cmds)
	}
}

func TestARuleAtSomebodyElsesPriorityDoesNotCountAsTheSetsOwn(t *testing.T) {
	cmds := policyRuleReconcile(t, []string{
		"0:\tfrom all lookup local",
		"1000:\tfrom all fwmark 0x6c53/0x27fff lookup 190",
		"32766:\tfrom all lookup main",
	})

	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "priority 10190") {
		t.Fatalf("a rule sending the set's mark to its table sits at a priority b4 did not choose, which is what a "+
			"user adds by hand when the set stops working. Table allocation reads that rule as another program's "+
			"and moves the set off the table on the next full resync, so b4 has to hold its own rule at its own "+
			"priority rather than take the stray one as proof it is covered:\n%s", joined)
	}
}

func TestAnInterfaceThatHasNeverAppearedGetsNoPolicyRule(t *testing.T) {
	prevLines, prevLogged, prevCache, prevGone, prevSeen :=
		routeRuleLines, runLogged, routeRuleCache, routePolicyRuleGone, routeIfaceSeen
	t.Cleanup(func() {
		routeRuleLines = prevLines
		runLogged = prevLogged
		routeRuleCache = prevCache
		routePolicyRuleGone = prevGone
		routeIfaceSeen = prevSeen
	})

	hasBinaryCache.Store("ip", true)

	routeRuleLines = func(fam routeFamily) ([]string, bool) { return []string{"0:\tfrom all lookup local"}, true }
	var cmds []string
	runLogged = func(op string, args ...string) bool { cmds = append(cmds, strings.Join(args, " ")); return true }

	routeRuleCache = map[string]routeState{
		"set-a": {mode: config.RoutingModeInterface, mark: 0x6c53, table: 190, iface: "b4-no-such-iface0"},
	}
	routePolicyRuleGone = make(map[string]bool)
	routeIfaceSeen = make(map[string]bool)

	routeReconcilePolicyRules(true, false)

	if len(cmds) != 0 {
		t.Fatalf("the sync path installs no rule for an interface that has never been on this router, because "+
			"the table behind it holds nothing and the lookup would end there. The monitor must not put back "+
			"what the sync deliberately left out: %v", cmds)
	}
}

func TestAProxySetIsNotGivenAnInterfacePolicyRule(t *testing.T) {
	prevLines, prevLogged, prevCache, prevGone :=
		routeRuleLines, runLogged, routeRuleCache, routePolicyRuleGone
	t.Cleanup(func() {
		routeRuleLines = prevLines
		runLogged = prevLogged
		routeRuleCache = prevCache
		routePolicyRuleGone = prevGone
	})

	hasBinaryCache.Store("ip", true)

	routeRuleLines = func(fam routeFamily) ([]string, bool) { return []string{"0:\tfrom all lookup local"}, true }
	var cmds []string
	runLogged = func(op string, args ...string) bool { cmds = append(cmds, strings.Join(args, " ")); return true }

	routeRuleCache = map[string]routeState{
		"set-a": {mode: config.RoutingModeProxy, mark: 0x6c53, table: 190, iface: ""},
	}
	routePolicyRuleGone = make(map[string]bool)

	routeReconcilePolicyRules(true, false)

	if len(cmds) != 0 {
		t.Fatalf("a proxy set carries its traffic to a local listener and installs its rule on its own path at "+
			"its own priority, so writing an interface rule for it points its mark at a table it does not "+
			"use: %v", cmds)
	}
}
