package tables

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

const sweepSharedTable = 137

func sweepSharedTableDeletes(t *testing.T, foreignV4, foreignV6 bool) []string {
	t.Helper()
	stubBinaries(t, "ip")
	own := fmt.Sprintf("%d:\tfrom all fwmark 0x%x/0x%x lookup %d", routePolicyRuleBase+sweepSharedTable, 0x1234, routeSetMarkMask, sweepSharedTable)
	foreign := fmt.Sprintf("5000:\tfrom all fwmark 0x500/0xf00 lookup %d", sweepSharedTable)
	rules := func(withForeign bool) string {
		if withForeign {
			return foreign + "\n" + own
		}
		return own
	}
	routes := map[string]string{
		fmt.Sprintf("ip route show table %d", sweepSharedTable): strings.Join([]string{
			"default via 192.168.9.1 dev ovpn9",
			"blackhole default metric " + routeKillSwitchMetric,
		}, "\n"),
		fmt.Sprintf("ip -6 route show table %d", sweepSharedTable): "default dev ovpn9 metric 1024 pref medium",
	}

	prevRun, prevDel, prevProto := run, routeDelRuleLoop, routeIPSupportsProto
	t.Cleanup(func() { run, routeDelRuleLoop, routeIPSupportsProto = prevRun, prevDel, prevProto })
	routeIPSupportsProto = func() bool { return true }
	routeDelRuleLoop = func(bool, string, string) {}
	var deleted []string
	run = func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case joined == "ip rule show":
			return rules(foreignV4), nil
		case joined == "ip -6 rule show":
			return rules(foreignV6), nil
		case strings.Contains(joined, " rule del "), strings.Contains(joined, " -D "):
			return "", errors.New("stub: no such rule")
		case strings.Contains(joined, " route del "):
			deleted = append(deleted, joined)
		}
		return routes[joined], nil
	}

	routeSweepOwnRules()
	return deleted
}

func TestTheSweepLeavesAnUntaggedDefaultInATableAnotherServicesRuleAlsoPointsAt(t *testing.T) {
	deleted := sweepSharedTableDeletes(t, true, true)

	for _, cmd := range deleted {
		if strings.Contains(cmd, " del default ") {
			t.Errorf("table %d is also the target of a rule b4 did not add, so its untagged default may belong to that service, and nothing puts it back if b4 does not install there; the sweep deleted it: %s", sweepSharedTable, cmd)
		}
	}
	blackhole := fmt.Sprintf("ip route del blackhole default metric %s table %d", routeKillSwitchMetric, sweepSharedTable)
	if !slices.Contains(deleted, blackhole) {
		t.Errorf("the metric-%s blackhole is b4's kill switch shape and must still be swept from a shared table; deletes run:\n%s", routeKillSwitchMetric, strings.Join(deleted, "\n"))
	}
}

func TestTheSweepJudgesATableSharedInEachAddressFamilyOnItsOwn(t *testing.T) {
	deleted := sweepSharedTableDeletes(t, true, false)

	if slices.Contains(deleted, fmt.Sprintf("ip route del default dev ovpn9 table %d", sweepSharedTable)) {
		t.Errorf("another service's IPv4 rule points at table %d, so the untagged IPv4 default there must stay; deletes run:\n%s", sweepSharedTable, strings.Join(deleted, "\n"))
	}
	v6 := fmt.Sprintf("ip -6 route del default dev ovpn9 metric 1024 table %d", sweepSharedTable)
	if !slices.Contains(deleted, v6) {
		t.Errorf("only b4's leftover IPv6 rule points at table %d, and an IPv6 lookup never reaches a table through an IPv4 rule, so in IPv6 the table is not shared and its untagged default is swept like any other a leftover b4 rule alone reaches; %q never ran. Deletes run:\n%s", sweepSharedTable, v6, strings.Join(deleted, "\n"))
	}
}
