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
	ownMark := fmt.Sprintf("0x%x/0x%x", 0x1234, routeSetMarkMask)
	listing := strings.Join([]string{
		"0:\tfrom all lookup local",
		fmt.Sprintf("%d:\tfrom all fwmark %s lookup %d", routePolicyRuleBase+table, ownMark, table),
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

	if len(deleted) != 1 || deleted[0] != fmt.Sprintf("v6=false %s %d", ownMark, table) {
		t.Fatalf("expected exactly the orphaned b4 rule to be deleted, got %v", deleted)
	}
	if len(flushed) != 1 || !strings.HasSuffix(flushed[0], fmt.Sprintf("table %d", table)) {
		t.Fatalf("expected the orphaned table to be flushed once, got %v", flushed)
	}
	for _, cmd := range executed {
		if strings.Contains(cmd, "lookup 200") || strings.Contains(cmd, "table 200") {
			t.Fatalf("a rule b4 did not add was touched: %s", cmd)
		}
	}
}
