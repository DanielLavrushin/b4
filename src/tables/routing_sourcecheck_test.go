package tables

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestProxySourceCheckRuleArgs(t *testing.T) {
	add := strings.Join(proxySourceCheckRuleArgs("add", "0x24bab/0x27fff"), " ")
	if want := "ip rule add fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7 priority 2"; add != want {
		t.Fatalf("add = %q, want %q", add, want)
	}
	del := strings.Join(proxySourceCheckRuleArgs("del", "0x24bab/0x27fff"), " ")
	if want := "ip rule del fwmark 0x24bab/0x27fff iif lo lookup main"; del != want {
		t.Fatalf("del = %q, want %q", del, want)
	}
	if proxySourceCheckRulePriority >= proxyRulePriority {
		t.Fatalf("the source-check rule sits at priority %d, not above the local-delivery rule at %d, so the check still lands in the local-delivery table", proxySourceCheckRulePriority, proxyRulePriority)
	}
}

func TestProxySourceCheckKeepsNetworksAndSkipsDefaultRoutes(t *testing.T) {
	args := proxySourceCheckRuleArgs("add", "0x24bab/0x27fff")
	suppress := -1
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "suppress_prefixlength" {
			suppress, _ = strconv.Atoi(args[i+1])
		}
	}
	if suppress < 0 {
		t.Fatalf("the source-check rule has no suppress_prefixlength, so the router's own marked dials would follow the main default route and never reach the set: %v", args)
	}
	for _, tc := range []struct {
		route string
		bits  int
		used  bool
	}{
		{"default", 0, false},
		{"0.0.0.0/1 from a VPN client that splits the default route", 1, false},
		{"10.0.0.0/8", 8, true},
		{"192.168.50.0/24 on br0", 24, true},
		{"10.8.0.2/32", 32, true},
	} {
		if used := tc.bits > suppress; used != tc.used {
			t.Errorf("%s: used by the source check = %v, want %v (suppress_prefixlength %d)", tc.route, used, tc.used, suppress)
		}
	}
}

func TestRouteRuleIsSourceCheck(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"2:\tfrom all fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7", true},
		{"2:      from all fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7", true},
		{"2:\tfrom all fwmark 0x2a1b3/0x27fff iif lo lookup main suppress_prefixlength 7", true},
		{"3:\tfrom all fwmark 0x24bab/0x27fff lookup 252", false},
		{"2:\tfrom all fwmark 0x10000/0x10000 lookup 77", false},
		{"2:\tfrom all fwmark 0x24bab/0x27fff iif br0 lookup main suppress_prefixlength 7", false},
		{"5:\tfrom all fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7", false},
		{"2:\tfrom all fwmark 0x24bab/0xffffffff iif lo lookup main", false},
		{"32766:\tfrom all lookup main", false},
	} {
		if got := routeRuleIsSourceCheck(tc.line); got != tc.want {
			t.Errorf("routeRuleIsSourceCheck(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func sourceCheckStubRun(t *testing.T, srcValidMark, reject bool) *[]string {
	t.Helper()
	origRun, origOn := run, routeSrcValidMarkOn
	warned := routeSourceCheckWarned.Load()
	t.Cleanup(func() {
		run = origRun
		routeSrcValidMarkOn = origOn
		routeSourceCheckWarned.Store(warned)
	})
	routeSourceCheckWarned.Store(false)
	routeSrcValidMarkOn = func() bool { return srcValidMark }
	var cmds []string
	run = func(args ...string) (string, error) {
		cmds = append(cmds, strings.Join(args, " "))
		if reject {
			return `Error: argument "suppress_prefixlength" is wrong: Failed to parse rule type`, errors.New("exit status 1")
		}
		return "", nil
	}
	return &cmds
}

func TestRouteAddSourceCheckRule_AddsTheRule(t *testing.T) {
	cmds := sourceCheckStubRun(t, true, false)
	routeAddSourceCheckRuleExec(config.TelegramBridgeMark)
	want := "ip rule add fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7 priority 2"
	if len(*cmds) != 1 || (*cmds)[0] != want {
		t.Fatalf("commands = %v, want exactly %q", *cmds, want)
	}
}

func TestRouteAddSourceCheckRule_WarnsOnceAndOnlyWhenSrcValidMarkIsOn(t *testing.T) {
	buf := captureTablesLog(t)

	sourceCheckStubRun(t, false, true)
	routeAddSourceCheckRuleExec(config.TelegramBridgeMark)
	if strings.Contains(buf.String(), "src_valid_mark") {
		t.Fatalf("a rejected source-check rule was reported while src_valid_mark is 0, where the rule changes nothing: %s", buf.String())
	}

	sourceCheckStubRun(t, true, true)
	routeAddSourceCheckRuleExec(config.TelegramBridgeMark)
	routeAddSourceCheckRuleExec(0x2a1b3)
	if n := strings.Count(buf.String(), "src_valid_mark is 1"); n != 1 {
		t.Fatalf("a rejected source-check rule with src_valid_mark=1 was reported %d time(s), want once: %s", n, buf.String())
	}
}

func TestRouteDelSourceCheckRule_RemovesEveryCopy(t *testing.T) {
	origRun := run
	t.Cleanup(func() { run = origRun })
	calls := 0
	run = func(args ...string) (string, error) {
		calls++
		if got := strings.Join(args, " "); got != "ip rule del fwmark 0x24bab/0x27fff iif lo lookup main" {
			t.Fatalf("unexpected command %q", got)
		}
		if calls > 2 {
			return "RTNETLINK answers: No such file or directory", errors.New("exit status 2")
		}
		return "", nil
	}
	routeDelSourceCheckRuleExec("0x24bab/0x27fff")
	if calls != 3 {
		t.Fatalf("the delete ran %d time(s), want 3: two copies, then the miss that ends the loop", calls)
	}
}

func TestRouteSweepOwnRules_RemovesTheSourceCheckRuleWithoutTouchingMain(t *testing.T) {
	f := &sweepFixture{
		rules: "0:      from all lookup local\n" +
			"2:      from all fwmark 0x10000/0x10000 lookup 77\n" +
			"2:      from all fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7\n" +
			"3:      from all fwmark 0x24bab/0x27fff lookup 252\n" +
			"20:     from all lookup 8437\n" +
			"32766:  from all lookup main\n",
		tables: map[string]string{"252": "local default dev lo scope host\n"},
	}
	f.install(t, true)
	origDel := routeDelSourceCheckRule
	t.Cleanup(func() { routeDelSourceCheckRule = origDel })
	var removed []string
	routeDelSourceCheckRule = func(markStrMask string) { removed = append(removed, markStrMask) }

	routeSweepOwnRules()

	if len(removed) != 1 || removed[0] != "0x24bab/0x27fff" {
		t.Fatalf("source-check rules removed = %v, want exactly the leftover for mark 0x24bab", removed)
	}
	for _, d := range f.deleted {
		if strings.HasSuffix(d, " main") {
			t.Errorf("the sweep deleted a rule that looks up the main table as if it were b4's own table: %s", d)
		}
		if strings.HasSuffix(d, " 77") || strings.HasSuffix(d, " 8437") {
			t.Errorf("the sweep deleted a rule another service owns: %s", d)
		}
	}
	if len(f.deleted) != 1 || f.deleted[0] != "v6=false 0x24bab/0x27fff 252" {
		t.Errorf("rules deleted = %v, want only b4's own local-delivery rule", f.deleted)
	}
	for _, cmd := range f.executed {
		if strings.Contains(cmd, "table main") {
			t.Errorf("the sweep touched routes in the main table: %s", cmd)
		}
	}
	f.mustRunOnce(t, "ip route del local 0.0.0.0/0 dev lo table 252")
}

func sourceCheckCleanupFixture(t *testing.T) *[]string {
	t.Helper()
	familyResetGlobals(t)
	stubBinaries(t, "ip")
	origRun := run
	t.Cleanup(func() {
		run = origRun
		proxyTableForget()
	})
	run = func(args ...string) (string, error) { return "", errors.New("stubbed") }
	var removed []string
	routeDelSourceCheckRule = func(markStrMask string) { removed = append(removed, markStrMask) }
	return &removed
}

func sourceCheckProxyState(mode string, mark uint32, table int) routeState {
	return routeState{
		setID:    "sourcecheck",
		mode:     mode,
		mark:     mark,
		table:    table,
		ipv4:     true,
		setV4:    "b4r_sourcecheck_v4",
		setV6:    "b4r_sourcecheck_v6",
		chainPre: "b4r_sourcecheck_pre",
		chainOut: "b4r_sourcecheck_out",
	}
}

func TestRouteCleanupForRebuild_SourceCheckRuleFollowsTheMark(t *testing.T) {
	removed := sourceCheckCleanupFixture(t)
	old := sourceCheckProxyState(config.RoutingModeProxy, 0x2a1b3, 252)
	moved := sourceCheckProxyState(config.RoutingModeProxy, 0x2b0c4, 252)
	routeCleanupForRebuild(&mockRouteBackend{}, old, moved)()
	if len(*removed) != 1 || (*removed)[0] != "0x2a1b3/0x27fff" {
		t.Fatalf("a set that moved from mark 0x2a1b3 to 0x2b0c4 removed source-check rules %v, want the old mark's alone", *removed)
	}

	removed = sourceCheckCleanupFixture(t)
	retabled := sourceCheckProxyState(config.RoutingModeProxy, 0x2a1b3, 251)
	routeCleanupForRebuild(&mockRouteBackend{}, old, retabled)()
	if len(*removed) != 0 {
		t.Fatalf("a set that kept its mark and only changed table removed source-check rules %v; the rebuilt set needs that same rule", *removed)
	}
}

func TestRouteCleanupProxyRule_RemovesTheSourceCheckRule(t *testing.T) {
	removed := sourceCheckCleanupFixture(t)
	st := sourceCheckProxyState(config.RoutingModeMTProtoWS, config.TelegramBridgeMark, proxyLocalDeliveryTable)
	routeCleanupProxyRule(&mockRouteBackend{}, st, false)
	if len(*removed) != 1 || (*removed)[0] != "0x24bab/0x27fff" {
		t.Fatalf("removing the Telegram bridge left its source-check rule behind: removed %v", *removed)
	}
}
