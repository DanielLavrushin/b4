package tables

import (
	"fmt"
	"testing"
)

const netnsJumpSwapTarget = "b4r_swaptest"

func netnsAJumpSwapLeavesForeignRulesAlone(t *testing.T, engine string, mangle, atTop bool) {
	netnsRequire(t)
	netnsRequireEngine(t, engine)
	b := &routeIptBackend{legacy: engine == backendIPTablesLegacy}
	table := iptTable(mangle)
	base := "POSTROUTING"
	if mangle {
		base = "OUTPUT"
	}
	ipt := func(args ...string) error {
		_, err := runExec(append([]string{engine, "-w", "-t", table}, args...)...)
		return err
	}
	intruder := func(n int) []string {
		return []string{base, "-s", fmt.Sprintf("198.51.100.%d", n), "-j", "RETURN"}
	}
	firmware := []string{base, "-s", "192.0.2.1", "-j", "RETURN"}
	held := []string{base, "-m", "mark", "--mark", "0x0/0x0", "-j", netnsJumpSwapTarget}

	for _, cmd := range b.iptBoth() {
		if hasBinary(cmd) {
			runExec(cmd, "-w", "-t", table, "-N", netnsJumpSwapTarget)
		}
	}
	intruders := 0
	orig := run
	t.Cleanup(func() {
		run = orig
		for n := 1; n <= intruders; n++ {
			ipt(append([]string{"-D"}, intruder(n)...)...)
		}
		ipt(append([]string{"-D"}, firmware...)...)
		for _, cmd := range b.iptBoth() {
			if !hasBinary(cmd) {
				continue
			}
			iptDeleteJumpsTo(cmd, table, base, netnsJumpSwapTarget)
			runExec(cmd, "-w", "-t", table, "-X", netnsJumpSwapTarget)
		}
	})

	if err := ipt(append([]string{"-A"}, firmware...)...); err != nil {
		t.Fatalf("cannot add the rule that stands in for the firmware's: %v", err)
	}
	if err := ipt("-A", base, "-j", netnsJumpSwapTarget); err != nil {
		t.Fatalf("cannot add the jump the swap is to replace: %v", err)
	}

	run = func(args ...string) (string, error) {
		out, err := orig(args...)
		if len(args) > 0 && args[0] == engine && intruders < 200 {
			intruders++
			ipt(append([]string{"-I"}, append([]string{base, "1"}, intruder(intruders)[1:]...)...)...)
		}
		return out, err
	}
	b.ensureJumpRule(base, netnsJumpSwapTarget, mangle, atTop)
	run = orig

	if intruders < 3 {
		t.Fatalf("only %d rule(s) went into %s %s between b4's commands, too few to move a line number b4 had read", intruders, table, base)
	}
	if err := ipt(append([]string{"-C"}, firmware...)...); err != nil {
		t.Errorf("the rule that stood above the set's jump in %s %s is gone after the swap. Another program put a rule at the head of the chain after each of b4's %d commands, the way a firmware does while it restarts its firewall, and that moves every line number b4 read before: a delete by line number then removes the rule that sits there now, which was never b4's, and nothing is logged",
			table, base, intruders)
	}
	gone := 0
	for n := 1; n <= intruders; n++ {
		if err := ipt(append([]string{"-C"}, intruder(n)...)...); err != nil {
			gone++
		}
	}
	if gone > 0 {
		t.Errorf("%d of the %d rules another program inserted during the swap are gone from %s %s", gone, intruders, table, base)
	}
	if jumps := iptJumpLineNumbers(engine, table, base, func(target string) bool { return target == netnsJumpSwapTarget }); len(jumps) != 1 {
		t.Errorf("%s %s has %d jumps to the set's chain after the swap, want one", table, base, len(jumps))
	}
	if err := ipt("-C", base, "-j", netnsJumpSwapTarget); err != nil {
		t.Errorf("%s %s has no plain jump to the set's chain after the swap: %v", table, base, err)
	}
	if err := ipt(append([]string{"-C"}, held...)...); err == nil {
		t.Errorf("the jump that holds the set's place during the swap is still in %s %s", table, base)
	}
}

func TestNetnsAJumpSwapLeavesForeignRulesAlone(t *testing.T) {
	netnsAJumpSwapLeavesForeignRulesAlone(t, backendIPTables, true, true)
}

func TestNetnsAJumpSwapLeavesForeignRulesAloneAppended(t *testing.T) {
	netnsAJumpSwapLeavesForeignRulesAlone(t, backendIPTables, false, false)
}

func TestNetnsAJumpSwapLeavesForeignRulesAloneLegacy(t *testing.T) {
	netnsAJumpSwapLeavesForeignRulesAlone(t, backendIPTablesLegacy, true, true)
}
