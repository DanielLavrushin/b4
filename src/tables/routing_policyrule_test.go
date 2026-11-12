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

func proxyRuleReconcile(t *testing.T, v4, v6 []string, ipv6 bool) []string {
	t.Helper()

	prevLines, prevLogged, prevRun, prevCache, prevGone :=
		routeRuleLines, runLogged, run, routeRuleCache, routePolicyRuleGone
	prevAdd, prevDel := routeAddSourceCheckRule, routeDelSourceCheckRule
	t.Cleanup(func() {
		routeRuleLines = prevLines
		runLogged = prevLogged
		run = prevRun
		routeRuleCache = prevCache
		routePolicyRuleGone = prevGone
		routeAddSourceCheckRule = prevAdd
		routeDelSourceCheckRule = prevDel
	})

	hasBinaryCache.Store("ip", true)

	routeRuleLines = func(fam routeFamily) ([]string, bool) {
		if len(fam.flag) > 0 {
			return v6, true
		}
		return v4, true
	}
	var cmds []string
	runLogged = func(op string, args ...string) bool { cmds = append(cmds, strings.Join(args, " ")); return true }
	run = func(args ...string) (string, error) { cmds = append(cmds, strings.Join(args, " ")); return "", nil }
	routeAddSourceCheckRule = func(mark uint32) {
		cmds = append(cmds, "add source-check "+routeSourceCheckMarkRule(mark))
	}
	routeDelSourceCheckRule = func(markStrMask string) {
		cmds = append(cmds, "del source-check "+markStrMask)
	}

	routeRuleCache = map[string]routeState{
		"set-a": {mode: config.RoutingModeProxy, mark: 0x239c9, table: proxyLocalDeliveryTable},
	}
	routePolicyRuleGone = make(map[string]bool)

	routeReconcilePolicyRules(true, ipv6)
	return cmds
}

func TestAProxySetIsNotGivenAnInterfacePolicyRule(t *testing.T) {
	cmds := proxyRuleReconcile(t, []string{"0:\tfrom all lookup local"}, nil, false)

	for _, cmd := range cmds {
		if strings.Contains(cmd, "priority 10252") {
			t.Fatalf("a proxy set carries its traffic to a local listener and installs its rule on its own path at "+
				"its own priority, so writing an interface rule for it points its mark at a table it does not "+
				"use: %v", cmds)
		}
	}
}

func TestAProxySetsRuleThatIsNotInTheKernelIsPutBack(t *testing.T) {
	bare := []string{"0:\tfrom all lookup local", "32766:\tfrom all lookup main"}
	cmds := proxyRuleReconcile(t, bare, bare, true)

	const (
		addV4    = "ip rule add fwmark 0x239c9/0x27fff lookup 252 priority 3"
		addV6    = "ip -6 rule add fwmark 0x239c9/0x27fff lookup 252 priority 3"
		addCheck = "add source-check 0x239c9/0x1027fff"
	)
	for _, want := range []string{addV4, addV6} {
		if localDeliveryCmdIndex(cmds, want) < 0 {
			t.Fatalf("a proxy set's chains are watched and restored every monitor pass while the rule that delivers "+
				"its mark to b4 was never read back. With that rule gone the chains still stand, so the set reads "+
				"as healthy while a device's connection is dropped and the router's own leaves by the ordinary "+
				"uplink, until the set is edited or b4 restarts. %q was not run: %v", want, cmds)
		}
	}
	check := localDeliveryCmdIndex(cmds, addCheck)
	if check < 0 || check > localDeliveryCmdIndex(cmds, addV4) {
		t.Errorf("whatever cleared the set's rule cleared its source-check rule too, and with "+
			"net.ipv4.conf.all.src_valid_mark=1 the set drops every device's connection without it; it has to be "+
			"back before the rule that needs it: %v", cmds)
	}
	checks := 0
	for _, cmd := range cmds {
		if cmd == addCheck {
			checks++
		}
	}
	if checks != 1 {
		t.Errorf("the source-check rule was added %d time(s), want once; IPv6 input has no source validation: %v", checks, cmds)
	}
}

func TestAProxySetsRuleAlreadyInTheKernelIsLeftAlone(t *testing.T) {
	standing := []string{
		"0:\tfrom all lookup local",
		"2:\tfrom all fwmark 0x239c9/0x1027fff iif lo lookup main",
		"3:\tfrom all fwmark 0x239c9/0x27fff lookup 252",
		"32766:\tfrom all lookup main",
	}
	cmds := proxyRuleReconcile(t, standing, standing, true)

	if len(cmds) != 0 {
		t.Fatalf("the rule was already there, and a monitor pass that touches it every ten seconds either stacks "+
			"duplicates or opens the very gap it is there to close: %v", cmds)
	}
}

func TestAProxySetsRuleThatCannotBeAddedIsRetriedQuietly(t *testing.T) {
	prevLines, prevLogged, prevRun, prevCache, prevGone :=
		routeRuleLines, runLogged, run, routeRuleCache, routePolicyRuleGone
	prevAdd, prevDel := routeAddSourceCheckRule, routeDelSourceCheckRule
	t.Cleanup(func() {
		routeRuleLines = prevLines
		runLogged = prevLogged
		run = prevRun
		routeRuleCache = prevCache
		routePolicyRuleGone = prevGone
		routeAddSourceCheckRule = prevAdd
		routeDelSourceCheckRule = prevDel
	})

	hasBinaryCache.Store("ip", true)

	routeRuleLines = func(fam routeFamily) ([]string, bool) { return []string{"0:\tfrom all lookup local"}, true }
	loud, quiet, checks := 0, 0, 0
	runLogged = func(op string, args ...string) bool { loud++; return false }
	run = func(args ...string) (string, error) { quiet++; return "", nil }
	routeAddSourceCheckRule = func(mark uint32) { checks++ }
	routeDelSourceCheckRule = func(markStrMask string) {}

	routeRuleCache = map[string]routeState{
		"set-a": {mode: config.RoutingModeProxy, mark: 0x239c9, table: proxyLocalDeliveryTable},
	}
	routePolicyRuleGone = make(map[string]bool)

	for i := 0; i < 4; i++ {
		routeReconcilePolicyRules(true, false)
	}

	if loud != 1 || quiet != 3 {
		t.Errorf("four monitor passes over a rule this ip command refuses logged %d failure(s) and retried quietly "+
			"%d time(s), want 1 and 3; a busybox without masked fwmark rules refuses it on every pass, and a "+
			"warning every ten seconds buries the log", loud, quiet)
	}
	if checks != 4 {
		t.Errorf("the source-check rule was asked for %d time(s) over four passes that never saw it, want one per pass", checks)
	}
}

func TestAProxySetsSourceCheckRuleIsWatchedOnItsOwn(t *testing.T) {
	const (
		addV4    = "ip rule add fwmark 0x239c9/0x27fff lookup 252 priority 3"
		addCheck = "add source-check 0x239c9/0x1027fff"
		live     = "3:\tfrom all fwmark 0x239c9/0x27fff lookup 252"
	)

	alone := proxyRuleReconcile(t, []string{"0:\tfrom all lookup local", live}, nil, false)
	if len(alone) != 1 || alone[0] != addCheck {
		t.Errorf("the set's rule stands and only its source-check rule is gone, so the monitor has to put back that "+
			"one rule and nothing else; it ran %v", alone)
	}

	prevLines, prevLogged, prevRun, prevCache, prevGone :=
		routeRuleLines, runLogged, run, routeRuleCache, routePolicyRuleGone
	prevAdd, prevDel := routeAddSourceCheckRule, routeDelSourceCheckRule
	t.Cleanup(func() {
		routeRuleLines = prevLines
		runLogged = prevLogged
		run = prevRun
		routeRuleCache = prevCache
		routePolicyRuleGone = prevGone
		routeAddSourceCheckRule = prevAdd
		routeDelSourceCheckRule = prevDel
	})

	hasBinaryCache.Store("ip", true)
	routeRuleLines = func(fam routeFamily) ([]string, bool) { return []string{"0:\tfrom all lookup local"}, true }
	var pass []string
	runLogged = func(op string, args ...string) bool { pass = append(pass, strings.Join(args, " ")); return true }
	run = func(args ...string) (string, error) { pass = append(pass, strings.Join(args, " ")); return "", nil }
	routeAddSourceCheckRule = func(mark uint32) { pass = append(pass, "add source-check "+routeSourceCheckMarkRule(mark)) }
	routeDelSourceCheckRule = func(markStrMask string) {}
	routeRuleCache = map[string]routeState{
		"set-a": {mode: config.RoutingModeProxy, mark: 0x239c9, table: proxyLocalDeliveryTable},
	}
	routePolicyRuleGone = make(map[string]bool)

	for n := 1; n <= 2; n++ {
		pass = nil
		routeReconcilePolicyRules(true, false)
		check, rule := localDeliveryCmdIndex(pass, addCheck), localDeliveryCmdIndex(pass, addV4)
		if check < 0 || rule < 0 || check > rule {
			t.Errorf("pass %d found both rules gone and ran %v, want the source-check rule and then the set's rule. "+
				"On a router both were taken out a second time before a pass had seen them back, and only the "+
				"set's rule returned: with net.ipv4.conf.all.src_valid_mark=1 that leaves the set dropping every "+
				"device's connection while it reads as healthy", n, pass)
		}
	}
}

func stubRtTableNames(t *testing.T, names map[int]string) {
	t.Helper()
	prev := routeRtTableNames
	routeRtTableNames = func() map[int]string { return names }
	routeForgetRtTableNames()
	t.Cleanup(func() {
		routeRtTableNames = prev
		routeForgetRtTableNames()
	})
}

func TestPolicyRuleIsFoundWhenTheTableHasAName(t *testing.T) {
	stubRtTableNames(t, map[int]string{252: "b4proxy"})

	line := "10252:\tfrom all fwmark 0x239c9/0x27fff lookup b4proxy"
	if !routePolicyRuleInstalled([]string{line}, 0x239c9, 252) {
		t.Errorf("ip rule show prints the name from /etc/iproute2/rt_tables, not the number, so comparing only against %d reads a live rule as missing and b4 keeps trying to put back a rule that is already there: %q", 252, line)
	}
}

func TestPolicyRuleStillMatchesANumericLookup(t *testing.T) {
	stubRtTableNames(t, map[int]string{252: "b4proxy"})

	line := "10252:\tfrom all fwmark 0x239c9/0x27fff lookup 252"
	if !routePolicyRuleInstalled([]string{line}, 0x239c9, 252) {
		t.Errorf("an unnamed table is still printed as a number: %q", line)
	}
}

func TestPolicyRuleDoesNotMatchAnotherTablesName(t *testing.T) {
	stubRtTableNames(t, map[int]string{252: "b4proxy", 199: "other"})

	line := "10252:\tfrom all fwmark 0x239c9/0x27fff lookup other"
	if routePolicyRuleInstalled([]string{line}, 0x239c9, 252) {
		t.Errorf("a rule pointing at a different table must not count as this table's rule: %q", line)
	}
}
