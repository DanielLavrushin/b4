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

func localDeliveryEnsure(t *testing.T, ipv4, ipv6 bool) ([]localDeliveryRuleDel, []string) {
	t.Helper()
	logged, delRule, sysctl := runLogged, routeDelRuleLoop, writeSysctl
	addCheck, delCheck := routeAddSourceCheckRule, routeDelSourceCheckRule
	t.Cleanup(func() {
		runLogged = logged
		routeDelRuleLoop = delRule
		writeSysctl = sysctl
		routeAddSourceCheckRule = addCheck
		routeDelSourceCheckRule = delCheck
	})

	var dels []localDeliveryRuleDel
	var cmds []string
	writeSysctl = func(path, value string) {}
	routeDelRuleLoop = func(v6 bool, mark, table string) {
		dels = append(dels, localDeliveryRuleDel{ipv6: v6, mark: mark, table: table})
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
		added, cleared := localDeliveryCmdIndex(cmds, addCheck), localDeliveryCmdIndex(cmds, delCheck)
		if added < 0 {
			t.Fatalf("ipv4=%v ipv6=%v: the source-check rule was never added, so with net.ipv4.conf.all.src_valid_mark=1 every LAN SYN the set diverts is checked against the local-delivery table and dropped as a martian: %v", tc.ipv4, tc.ipv6, cmds)
		}
		if cleared < 0 || cleared > added {
			t.Errorf("ipv4=%v ipv6=%v: the source-check rule is added without clearing the previous one first, so every re-install stacks another copy: %v", tc.ipv4, tc.ipv6, cmds)
		}
		if old := localDeliveryCmdIndex(cmds, delOldCheck); old < 0 || old > added {
			t.Errorf("ipv4=%v ipv6=%v: a source-check rule an earlier build added with the per-set mask is never removed, and it sends the router's own connections past the set: %v", tc.ipv4, tc.ipv6, cmds)
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

func TestRouteEnsureLocalDelivery_ClearsBothFamiliesWhicheverIsEnabled(t *testing.T) {
	const tableStr = "252"

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
				ipv6 bool
				name string
			}{{false, "v4"}, {true, "v6"}} {
				marks := localDeliveryDeletedFamily(dels, fam.ipv6)
				if len(marks) != 3 {
					t.Fatalf("%s stale rules were cleared %d time(s), want the legacy bare, the legacy self-masked and the shared-mask form; a rule left behind keeps sending traffic to table %s after the family is turned off", fam.name, len(marks), tableStr)
				}
				if marks[0] != "0x20fa/0xffffffff" || marks[1] != "0x20fa/0x20fa" || marks[2] != "0x20fa/0x27fff" {
					t.Errorf("%s stale rule deletion used marks %v, want the legacy bare, the legacy self-masked and the shared-mask form", fam.name, marks)
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
