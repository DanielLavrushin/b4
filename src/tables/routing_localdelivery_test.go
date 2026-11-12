package tables

import (
	"strings"
	"testing"
)

type localDeliveryRuleDel struct {
	ipv6  bool
	mark  string
	table string
}

type localDeliveryKernel struct {
	v4, v6     []string
	unreadable bool
}

const (
	localDeliveryLiveRule    = "3:\tfrom all fwmark 0x20fa/0x27fff lookup 252"
	localDeliverySourceCheck = "2:\tfrom all fwmark 0x20fa/0x1027fff iif lo lookup main"
)

func localDeliveryEnsure(t *testing.T, ipv4, ipv6 bool) ([]localDeliveryRuleDel, []string) {
	t.Helper()
	return localDeliveryEnsureOver(t, localDeliveryKernel{}, ipv4, ipv6)
}

func localDeliveryEnsureOver(t *testing.T, kernel localDeliveryKernel, ipv4, ipv6 bool) ([]localDeliveryRuleDel, []string) {
	t.Helper()
	logged, delRule, sysctl, lines := runLogged, routeDelRuleLoop, writeSysctl, routeRuleLines
	addCheck, delCheck := routeAddSourceCheckRule, routeDelSourceCheckRule
	t.Cleanup(func() {
		runLogged = logged
		routeDelRuleLoop = delRule
		writeSysctl = sysctl
		routeRuleLines = lines
		routeAddSourceCheckRule = addCheck
		routeDelSourceCheckRule = delCheck
	})

	var dels []localDeliveryRuleDel
	var cmds []string
	writeSysctl = func(path, value string) {}
	routeRuleLines = func(fam routeFamily) ([]string, bool) {
		if kernel.unreadable {
			return nil, false
		}
		if len(fam.flag) > 0 {
			return kernel.v6, true
		}
		return kernel.v4, true
	}
	routeDelRuleLoop = func(v6 bool, mark, table string) {
		dels = append(dels, localDeliveryRuleDel{ipv6: v6, mark: mark, table: table})
		fam := "v4"
		if v6 {
			fam = "v6"
		}
		cmds = append(cmds, "del rule "+fam+" "+mark+" "+table)
	}
	runLogged = func(op string, args ...string) bool {
		cmds = append(cmds, strings.Join(args, " "))
		return true
	}
	routeAddSourceCheckRule = func(mark uint32) {
		cmds = append(cmds, "add source-check "+routeSourceCheckMarkRule(mark))
	}
	routeDelSourceCheckRule = func(markStrMask string) {
		cmds = append(cmds, "del source-check "+markStrMask)
	}

	routeEnsureLocalDelivery(0x20fa, proxyLocalDeliveryTable, ipv4, ipv6)
	return dels, cmds
}

func localDeliveryCmdIndex(cmds []string, want string) int {
	for i, c := range cmds {
		if c == want {
			return i
		}
	}
	return -1
}

func TestRouteEnsureLocalDelivery_PointsTheSourceCheckAtMainForIPv4(t *testing.T) {
	const (
		addCheck    = "add source-check 0x20fa/0x1027fff"
		delCheck    = "del source-check 0x20fa/0x1027fff"
		delOldCheck = "del source-check 0x20fa/0x27fff"
	)

	for _, tc := range []struct{ ipv4, ipv6 bool }{{true, true}, {true, false}} {
		_, cmds := localDeliveryEnsure(t, tc.ipv4, tc.ipv6)
		added := localDeliveryCmdIndex(cmds, addCheck)
		if added < 0 {
			t.Fatalf("ipv4=%v ipv6=%v: the source-check rule was never added, so with net.ipv4.conf.all.src_valid_mark=1 every LAN SYN the set diverts is checked against the local-delivery table and dropped as a martian: %v", tc.ipv4, tc.ipv6, cmds)
		}
		if localDeliveryCmdIndex(cmds, delCheck) >= 0 {
			t.Errorf("ipv4=%v ipv6=%v: the source-check rule in use was deleted on the way to installing it; until it is back, src_valid_mark=1 drops every connection the set diverts as a martian: %v", tc.ipv4, tc.ipv6, cmds)
		}
		if old := localDeliveryCmdIndex(cmds, delOldCheck); old < 0 || old < added {
			t.Errorf("ipv4=%v ipv6=%v: a source-check rule an earlier build added with the per-set mask must go, and only once the current one stands; it sends the router's own connections past the set: %v", tc.ipv4, tc.ipv6, cmds)
		}

		_, again := localDeliveryEnsureOver(t, localDeliveryKernel{v4: []string{localDeliverySourceCheck}}, tc.ipv4, tc.ipv6)
		if localDeliveryCmdIndex(again, addCheck) >= 0 {
			t.Errorf("ipv4=%v ipv6=%v: the source-check rule was already in the kernel and was added again; an old kernel takes the duplicate (3.16 does, 4.9 refuses it), so every re-install stacks another copy: %v", tc.ipv4, tc.ipv6, again)
		}
		if localDeliveryCmdIndex(again, delCheck) >= 0 {
			t.Errorf("ipv4=%v ipv6=%v: the source-check rule already in the kernel was deleted during a re-install: %v", tc.ipv4, tc.ipv6, again)
		}
	}

	_, v6only := localDeliveryEnsure(t, false, true)
	if localDeliveryCmdIndex(v6only, addCheck) >= 0 {
		t.Errorf("the source-check rule was added while IPv4 is disabled: %v", v6only)
	}
	if localDeliveryCmdIndex(v6only, delCheck) < 0 {
		t.Errorf("the source-check rule from an earlier ensure is never removed when IPv4 is turned off: %v", v6only)
	}
	for _, c := range v6only {
		if strings.HasPrefix(c, "ip -6 ") && strings.Contains(c, "suppress_prefixlength") {
			t.Errorf("an IPv6 source-check rule was added; IPv6 input has no source validation, so it would only change routing: %q", c)
		}
	}
}

func localDeliveryDeletedFamily(dels []localDeliveryRuleDel, ipv6 bool) []string {
	var marks []string
	for _, d := range dels {
		if d.ipv6 == ipv6 {
			marks = append(marks, d.mark)
		}
	}
	return marks
}

func localDeliveryHasCmd(cmds []string, want string) bool {
	for _, c := range cmds {
		if c == want {
			return true
		}
	}
	return false
}

func TestRouteEnsureLocalDelivery_ClearsTheFamilyThatIsTurnedOff(t *testing.T) {
	const (
		tableStr = "252"
		live     = "0x20fa/0x27fff"
	)

	cases := []struct{ ipv4, ipv6 bool }{
		{true, true},
		{true, false},
		{false, true},
		{false, false},
	}
	for _, tc := range cases {
		t.Run(strings.Join([]string{boolLabel(tc.ipv4), boolLabel(tc.ipv6)}, "/"), func(t *testing.T) {
			dels, _ := localDeliveryEnsure(t, tc.ipv4, tc.ipv6)

			for _, fam := range []struct {
				ipv6, enabled bool
				name          string
			}{{false, tc.ipv4, "v4"}, {true, tc.ipv6, "v6"}} {
				marks := localDeliveryDeletedFamily(dels, fam.ipv6)
				seen := map[string]int{}
				for _, m := range marks {
					seen[m]++
				}
				if seen["0x20fa/0xffffffff"] != 1 || seen["0x20fa/0x20fa"] != 1 {
					t.Errorf("%s stale rule deletion used marks %v, want the legacy bare and the legacy self-masked shape once each; a rule an older build left behind keeps sending traffic to table %s", fam.name, marks, tableStr)
				}
				switch {
				case fam.enabled && seen[live] != 0:
					t.Errorf("%s is enabled and the rule in use (%s) was deleted, so until it is added back a packet the set marks finds nothing pointing at table %s: %v", fam.name, live, tableStr, marks)
				case !fam.enabled && seen[live] != 1:
					t.Errorf("%s is turned off and its rule (%s) was deleted %d time(s), want once; left behind it keeps sending traffic to table %s after the family is turned off: %v", fam.name, live, seen[live], tableStr, marks)
				}
			}
			for _, d := range dels {
				if d.table != tableStr {
					t.Errorf("stale rule deletion targeted table %q, want %q", d.table, tableStr)
				}
			}
		})
	}
}

func TestRouteEnsureLocalDelivery_NeverTakesTheRuleInUseOutOfTheKernel(t *testing.T) {
	const (
		addRuleV4 = "ip rule add fwmark 0x20fa/0x27fff lookup 252 priority 3"
		addRuleV6 = "ip -6 rule add fwmark 0x20fa/0x27fff lookup 252 priority 3"
		delLiveV4 = "del rule v4 0x20fa/0x27fff 252"
		delLiveV6 = "del rule v6 0x20fa/0x27fff 252"
	)

	standing := localDeliveryKernel{
		v4: []string{"0:\tfrom all lookup local", localDeliverySourceCheck, localDeliveryLiveRule, "32766:\tfrom all lookup main"},
		v6: []string{"0:\tfrom all lookup local", localDeliveryLiveRule, "32766:\tfrom all lookup main"},
	}
	_, cmds := localDeliveryEnsureOver(t, standing, true, true)
	for _, banned := range []string{delLiveV4, delLiveV6} {
		if localDeliveryHasCmd(cmds, banned) {
			t.Errorf("a rebuild ran %q on a set whose rule was standing. Deleting it and adding it back leaves a moment with the packet marked and nothing delivering it to b4: a device's connection is dropped and the router's own leaves by the ordinary uplink: %v", banned, cmds)
		}
	}
	for _, banned := range []string{addRuleV4, addRuleV6} {
		if localDeliveryHasCmd(cmds, banned) {
			t.Errorf("a rebuild ran %q although the rule was standing; an old kernel takes the duplicate (3.16 does, 4.9 refuses it), so every rebuild stacks another copy: %v", banned, cmds)
		}
	}

	_, fresh := localDeliveryEnsure(t, true, true)
	for _, fam := range []struct{ add, stale string }{
		{addRuleV4, "del rule v4 0x20fa/0xffffffff 252"},
		{addRuleV6, "del rule v6 0x20fa/0xffffffff 252"},
	} {
		added, swept := localDeliveryCmdIndex(fresh, fam.add), localDeliveryCmdIndex(fresh, fam.stale)
		if added < 0 {
			t.Fatalf("the rule is not in the kernel and %q was never run: %v", fam.add, fresh)
		}
		if swept < 0 || swept < added {
			t.Errorf("the shapes older builds used must go only after %q has run, so an upgrade from one of them never leaves the set without a rule: %v", fam.add, fresh)
		}
	}

	_, blind := localDeliveryEnsureOver(t, localDeliveryKernel{unreadable: true}, true, true)
	for _, want := range []string{addRuleV4, addRuleV6, "add source-check 0x20fa/0x1027fff"} {
		if !localDeliveryHasCmd(blind, want) {
			t.Errorf("the rules could not be read back and %q was skipped; without a listing b4 cannot know the rule is there, so it has to ask for it: %v", want, blind)
		}
	}
}

func TestRouteEnsureLocalDelivery_ARuleAtAnotherPriorityIsNotTheSetsOwn(t *testing.T) {
	const addRuleV4 = "ip rule add fwmark 0x20fa/0x27fff lookup 252 priority 3"

	_, cmds := localDeliveryEnsureOver(t, localDeliveryKernel{
		v4: []string{"100:\tfrom all fwmark 0x20fa/0x27fff lookup 252"},
	}, true, false)
	if !localDeliveryHasCmd(cmds, addRuleV4) {
		t.Errorf("a rule for the set's mark sits at a priority b4 did not choose and b4 took it as its own instead of holding one at priority %d, ahead of every other program's rules: %v", proxyRulePriority, cmds)
	}
}

func TestRouteEnsureLocalDelivery_AddsOnlyForEnabledFamilies(t *testing.T) {
	const (
		addRuleV4  = "ip rule add fwmark 0x20fa/0x27fff lookup 252 priority 3"
		addRuleV6  = "ip -6 rule add fwmark 0x20fa/0x27fff lookup 252 priority 3"
		addRouteV4 = "ip route replace local 0.0.0.0/0 dev lo table 252"
		addRouteV6 = "ip -6 route replace local ::/0 dev lo table 252"
		delRouteV4 = "ip route del local 0.0.0.0/0 dev lo table 252"
		delRouteV6 = "ip -6 route del local ::/0 dev lo table 252"
	)

	_, both := localDeliveryEnsure(t, true, true)
	for _, want := range []string{addRuleV4, addRuleV6, addRouteV4, addRouteV6} {
		if !localDeliveryHasCmd(both, want) {
			t.Errorf("with both families enabled %q was never emitted: %v", want, both)
		}
	}

	_, v4only := localDeliveryEnsure(t, true, false)
	if localDeliveryHasCmd(v4only, addRuleV6) {
		t.Errorf("an ip -6 rule was added while IPv6 is disabled: %v", v4only)
	}
	if localDeliveryHasCmd(v4only, addRouteV6) {
		t.Errorf("the ::/0 local route was added while IPv6 is disabled: %v", v4only)
	}
	if !localDeliveryHasCmd(v4only, delRouteV6) {
		t.Errorf("the ::/0 local route from an earlier ensure is never removed, so it outlives the setting: %v", v4only)
	}
	if !localDeliveryHasCmd(v4only, addRuleV4) || !localDeliveryHasCmd(v4only, addRouteV4) {
		t.Errorf("the IPv4 side must still be installed: %v", v4only)
	}

	_, v6only := localDeliveryEnsure(t, false, true)
	if localDeliveryHasCmd(v6only, addRuleV4) {
		t.Errorf("an ip rule was added while IPv4 is disabled: %v", v6only)
	}
	if localDeliveryHasCmd(v6only, addRouteV4) {
		t.Errorf("the 0.0.0.0/0 local route was added while IPv4 is disabled: %v", v6only)
	}
	if !localDeliveryHasCmd(v6only, delRouteV4) {
		t.Errorf("the 0.0.0.0/0 local route from an earlier ensure is never removed, so it outlives the setting: %v", v6only)
	}
	if !localDeliveryHasCmd(v6only, addRuleV6) || !localDeliveryHasCmd(v6only, addRouteV6) {
		t.Errorf("the IPv6 side must still be installed: %v", v6only)
	}
}

func boolLabel(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
