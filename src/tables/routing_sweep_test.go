package tables

import (
	"fmt"
	"strings"
	"testing"
)

type sweepFixture struct {
	rules    string
	tables   map[string]string
	executed []string
	deleted  []string
}

func (f *sweepFixture) install(t *testing.T, protoSupported bool) {
	t.Helper()
	stubBinaries(t, "ip")
	origRun, origDel, origProto := run, routeDelRuleLoop, routeIPSupportsProto
	t.Cleanup(func() {
		run = origRun
		routeDelRuleLoop = origDel
		routeIPSupportsProto = origProto
	})
	routeIPSupportsProto = func() bool { return protoSupported }
	run = func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		f.executed = append(f.executed, joined)
		if joined == "ip rule show" {
			return f.rules, nil
		}
		if strings.HasPrefix(joined, "ip route show table ") {
			return f.tables[strings.TrimPrefix(joined, "ip route show table ")], nil
		}
		return "", nil
	}
	routeDelRuleLoop = func(ipv6 bool, mark, tbl string) {
		f.deleted = append(f.deleted, fmt.Sprintf("v6=%v %s %s", ipv6, mark, tbl))
	}
}

func (f *sweepFixture) count(cmd string) int {
	n := 0
	for _, executed := range f.executed {
		if executed == cmd {
			n++
		}
	}
	return n
}

func (f *sweepFixture) mustRunOnce(t *testing.T, cmds ...string) {
	t.Helper()
	for _, cmd := range cmds {
		if n := f.count(cmd); n != 1 {
			t.Fatalf("expected %q exactly once, ran %d times; everything executed:\n%s", cmd, n, strings.Join(f.executed, "\n"))
		}
	}
}

func (f *sweepFixture) mustNotTouch(t *testing.T, markers ...string) {
	t.Helper()
	for _, cmd := range f.executed {
		for _, marker := range markers {
			if strings.Contains(cmd, marker) {
				t.Fatalf("a route or rule b4 did not add was touched (%q): %s", marker, cmd)
			}
		}
	}
}

func TestSweepRemovesOrphanedOwnRulesAndLeavesForeignOnes(t *testing.T) {
	table := 10420
	sharedProxyTable := 10500
	ownMark := fmt.Sprintf("0x%x/0x%x", 0x1234, routeSetMarkMask)
	proxyMarkA := fmt.Sprintf("0x%x/0x%x", 0x1111, routeSetMarkMask)
	proxyMarkB := fmt.Sprintf("0x%x/0x%x", 0x2222, routeSetMarkMask)
	ownRule := fmt.Sprintf("%d:\tfrom all fwmark %s lookup %d", routePolicyRuleBase+table, ownMark, table)
	f := &sweepFixture{
		rules: strings.Join([]string{
			"0:\tfrom all lookup local",
			fmt.Sprintf("%d:\tfrom all fwmark %s lookup %d", proxyRulePriority, proxyMarkA, sharedProxyTable),
			fmt.Sprintf("%d:\tfrom all fwmark %s lookup %d", proxyRulePriority, proxyMarkB, sharedProxyTable),
			ownRule,
			ownRule,
			"20000:\tfrom all fwmark 0x8000/0x8000 lookup 200",
			"32766:\tfrom all lookup main",
		}, "\n"),
		tables: map[string]string{
			fmt.Sprintf("%d", table): strings.Join([]string{
				"default dev wg0 proto 155 scope link",
				"blackhole default proto 155 metric " + routeKillSwitchMetric,
				"10.0.0.0/8 via 10.0.0.1 dev tun0",
				"default via 192.168.2.1 dev eth1 metric 50",
			}, "\n"),
		},
	}
	f.install(t, true)

	routeSweepOwnRules()

	wantDeleted := strings.Join([]string{
		fmt.Sprintf("v6=false %s %d", proxyMarkA, sharedProxyTable),
		fmt.Sprintf("v6=false %s %d", proxyMarkB, sharedProxyTable),
		fmt.Sprintf("v6=false %s %d", ownMark, table),
	}, "\n")
	if got := strings.Join(f.deleted, "\n"); got != wantDeleted {
		t.Fatalf("every orphaned b4 rule must be deleted once, proxy sets share one table under distinct marks:\nwant\n%s\ngot\n%s", wantDeleted, got)
	}
	f.mustRunOnce(t,
		fmt.Sprintf("ip route del local 0.0.0.0/0 dev lo table %d", sharedProxyTable),
		fmt.Sprintf("ip route del default dev wg0 proto 155 table %d", table),
		fmt.Sprintf("ip route del blackhole default metric %s proto 155 table %d", routeKillSwitchMetric, table),
	)
	f.mustNotTouch(t, "flush", "tun0", "eth1", "lookup 200", "table 200")
}

func TestSweepWithoutRouteProtocolsLeavesDefaultRoutesAlone(t *testing.T) {
	table := 137
	ownMark := fmt.Sprintf("0x%x/0x%x", 0x1234, routeSetMarkMask)
	f := &sweepFixture{
		rules: strings.Join([]string{
			"0:\tfrom all lookup local",
			fmt.Sprintf("%d:\tfrom all fwmark %s lookup %d", routePolicyRuleBase+table, ownMark, table),
			"32766:\tfrom all lookup main",
		}, "\n"),
		tables: map[string]string{
			fmt.Sprintf("%d", table): strings.Join([]string{
				"default dev wg0 scope link",
				"default via 192.168.2.1 dev eth1 metric 50",
				"blackhole default metric " + routeKillSwitchMetric,
				"10.0.0.0/8 via 10.0.0.1 dev tun0",
				"blackhole 10.99.0.0/16 metric 5",
			}, "\n"),
		},
	}
	f.install(t, false)

	routeSweepOwnRules()

	if want := fmt.Sprintf("v6=false %s %d", ownMark, table); len(f.deleted) != 1 || f.deleted[0] != want {
		t.Fatalf("the orphaned rule must still be deleted, got %v", f.deleted)
	}
	f.mustRunOnce(t, fmt.Sprintf("ip route del blackhole default metric %s table %d", routeKillSwitchMetric, table))
	f.mustNotTouch(t, "flush", "route del default", "tun0", "eth1", "10.99")
}
