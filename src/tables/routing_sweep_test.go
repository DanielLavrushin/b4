package tables

import (
	"fmt"
	"strings"
	"testing"
)

func TestSweepRemovesOrphanedOwnRulesAndLeavesForeignOnes(t *testing.T) {
	stubBinaries(t, "ip")
	origRun := run
	origDel := routeDelRuleLoop
	t.Cleanup(func() {
		run = origRun
		routeDelRuleLoop = origDel
	})

	table := 10420
	sharedProxyTable := 10500
	ownMark := fmt.Sprintf("0x%x/0x%x", 0x1234, routeSetMarkMask)
	proxyMarkA := fmt.Sprintf("0x%x/0x%x", 0x1111, routeSetMarkMask)
	proxyMarkB := fmt.Sprintf("0x%x/0x%x", 0x2222, routeSetMarkMask)
	ownRule := fmt.Sprintf("%d:\tfrom all fwmark %s lookup %d", routePolicyRuleBase+table, ownMark, table)
	listing := strings.Join([]string{
		"0:\tfrom all lookup local",
		fmt.Sprintf("%d:\tfrom all fwmark %s lookup %d", proxyRulePriority, proxyMarkA, sharedProxyTable),
		fmt.Sprintf("%d:\tfrom all fwmark %s lookup %d", proxyRulePriority, proxyMarkB, sharedProxyTable),
		ownRule,
		ownRule,
		"20000:\tfrom all fwmark 0x8000/0x8000 lookup 200",
		"32766:\tfrom all lookup main",
	}, "\n")

	var flushed, executed []string
	run = func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		executed = append(executed, joined)
		if strings.HasSuffix(joined, "rule show") && !strings.Contains(joined, "-6") {
			return listing, nil
		}
		if strings.Contains(joined, "route flush table") {
			flushed = append(flushed, joined)
		}
		return "", nil
	}
	var deleted []string
	routeDelRuleLoop = func(ipv6 bool, mark, tbl string) {
		deleted = append(deleted, fmt.Sprintf("v6=%v %s %s", ipv6, mark, tbl))
	}

	routeSweepOwnRules()

	wantDeleted := strings.Join([]string{
		fmt.Sprintf("v6=false %s %d", proxyMarkA, sharedProxyTable),
		fmt.Sprintf("v6=false %s %d", proxyMarkB, sharedProxyTable),
		fmt.Sprintf("v6=false %s %d", ownMark, table),
	}, "\n")
	if got := strings.Join(deleted, "\n"); got != wantDeleted {
		t.Fatalf("every orphaned b4 rule must be deleted once, proxy sets share one table under distinct marks:\nwant\n%s\ngot\n%s", wantDeleted, got)
	}
	wantFlushed := strings.Join([]string{
		fmt.Sprintf("ip route flush table %d", sharedProxyTable),
		fmt.Sprintf("ip route flush table %d", table),
	}, "\n")
	if got := strings.Join(flushed, "\n"); got != wantFlushed {
		t.Fatalf("each orphaned table must be flushed exactly once:\nwant\n%s\ngot\n%s", wantFlushed, got)
	}
	for _, cmd := range executed {
		if strings.Contains(cmd, "lookup 200") || strings.Contains(cmd, "table 200") {
			t.Fatalf("a rule b4 did not add was touched: %s", cmd)
		}
	}
}
