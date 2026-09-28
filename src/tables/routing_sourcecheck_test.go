package tables

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
)

func TestProxySourceCheckRuleArgs(t *testing.T) {
	if got, want := routeSourceCheckMarkRule(config.TelegramBridgeMark), "0x24bab/0x1027fff"; got != want {
		t.Fatalf("source-check mark = %q, want %q", got, want)
	}
	add := strings.Join(proxySourceCheckRuleAddArgs("0x24bab/0x1027fff"), " ")
	if want := "ip rule add fwmark 0x24bab/0x1027fff iif lo lookup main priority 2"; add != want {
		t.Fatalf("add = %q, want %q", add, want)
	}
	del := strings.Join(proxySourceCheckRuleDelArgs("0x24bab/0x1027fff"), " ")
	if want := "ip rule del fwmark 0x24bab/0x1027fff iif lo lookup main"; del != want {
		t.Fatalf("del = %q, want %q", del, want)
	}
	if proxySourceCheckRulePriority >= proxyRulePriority {
		t.Fatalf("the source-check rule sits at priority %d, not above the local-delivery rule at %d, so the check still lands in the local-delivery table", proxySourceCheckRulePriority, proxyRulePriority)
	}
}

func TestRouterOwnProxyMarkBitIsFree(t *testing.T) {
	f := config.RouterOwnProxyMarkBit
	if f&(f-1) != 0 {
		t.Fatalf("RouterOwnProxyMarkBit 0x%x is not a single bit", f)
	}
	for _, used := range []struct {
		name string
		bits uint32
	}{
		{"the per-set marks", routeSetMarkMask},
		{"the default queue mark", 0x8000},
		{"XrayUI's mark", 0x10000},
		{"SelfDialRelayMark", config.SelfDialRelayMark},
		{"the TUN engine marks", uint32(engine.TunSteerMark | engine.ClientMark | engine.ReinjectMarkBit)},
		{"the legacy TUN rule marks", 0x80000 | 0x100000 | 0x800000},
		{"the Tailscale and OpenWrt pbr mask", 0xff0000},
	} {
		if f&used.bits != 0 {
			t.Errorf("RouterOwnProxyMarkBit 0x%x overlaps %s (0x%x)", f, used.name, used.bits)
		}
	}
}

func sourceCheckMarkArg(t *testing.T, lines []string, needle, flag string) (value, mask uint32) {
	t.Helper()
	idx := firstLineWith(lines, needle)
	if idx < 0 {
		t.Fatalf("no rule with %q in:\n%s", needle, strings.Join(lines, "\n"))
	}
	fw := routeRuleField(lines[idx], flag)
	slash := strings.IndexByte(fw, '/')
	if slash < 0 {
		t.Fatalf("%s %q has no mask: %s", flag, fw, lines[idx])
	}
	v, err1 := strconv.ParseUint(strings.TrimPrefix(fw[:slash], "0x"), 16, 32)
	m, err2 := strconv.ParseUint(strings.TrimPrefix(fw[slash+1:], "0x"), 16, 32)
	if err1 != nil || err2 != nil {
		t.Fatalf("%s %q does not parse: %s", flag, fw, lines[idx])
	}
	return uint32(v), uint32(m)
}

func TestProxySourceCheckTakesForwardedConnectionsButNotTheRoutersOwn(t *testing.T) {
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)
	stubBinaries(t, backendIPTables)

	st := proxyGuardState(0x239c9)
	if err := routeEnsureProxyRule(&routeIptBackend{}, v4OnlyConfig(), orderTestSet("guard", config.RoutingModeProxy, nil), st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}
	outValue, outMask := sourceCheckMarkArg(t, chainRuleLines(*emitted, st.chainOut), "-j MARK", "--set-mark")
	tproxyValue, tproxyMask := sourceCheckMarkArg(t, chainRuleLines(*emitted, st.chainPre), "-j TPROXY", "--tproxy-mark")
	divertValue, divertMask := sourceCheckMarkArg(t, chainRuleLines(*emitted, st.chainPre), "--transparent -m set --match-set "+st.setV4+" dst -j MARK", "--set-mark")
	checkValue, checkMask := sourceCheckMarkArg(t, []string{strings.Join(proxySourceCheckRuleAddArgs(routeSourceCheckMarkRule(st.mark)), " ")}, "fwmark", "fwmark")
	setMark := func(mark, value, mask uint32) uint32 { return mark&^mask ^ value }
	sourceCheck := func(mark uint32) bool { return (mark^checkValue)&checkMask == 0 }
	localDelivery := func(mark uint32) bool { return (mark^st.mark)&routeSetMarkMask == 0 }

	own := setMark(0, outValue, outMask)
	if sourceCheck(own) {
		t.Errorf("the router's own connection carries mark 0x%x, which the source-check rule takes to main, so a main route to a set address of /8 or longer, even a private one, sends the connection past the set", own)
	}
	if !localDelivery(own) {
		t.Errorf("the router's own connection carries mark 0x%x, which misses the local-delivery rule, so it never reaches the set's listener", own)
	}
	for _, before := range []uint32{0, config.RouterOwnProxyMarkBit} {
		if forwarded := setMark(before, tproxyValue, tproxyMask); !sourceCheck(forwarded) {
			t.Errorf("a forwarded connection arriving with mark 0x%x leaves TPROXY with 0x%x, which misses the source-check rule, so with net.ipv4.conf.all.src_valid_mark=1 it is dropped as a martian", before, forwarded)
		}
		if diverted := setMark(before, divertValue, divertMask); !sourceCheck(diverted) {
			t.Errorf("a packet of an established forwarded connection arriving with mark 0x%x leaves the divert rule with 0x%x, which misses the source-check rule, so with net.ipv4.conf.all.src_valid_mark=1 it is dropped as a martian", before, diverted)
		}
	}
}

func TestRouteRuleIsSourceCheck(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"2:\tfrom all fwmark 0x24bab/0x1027fff iif lo lookup main", true},
		{"2:      from all fwmark 0x2a1b3/0x1027fff iif lo lookup main", true},
		{"2:\tfrom all to 10.0.0.0/8 fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7", true},
		{"2:      from all to 192.168.0.0/16 fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7", true},
		{"2:\tfrom all fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7", true},
		{"2:      from all fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7", true},
		{"2:\tfrom all fwmark 0x2a1b3/0x27fff iif lo lookup main suppress_prefixlength 7", true},
		{"3:\tfrom all fwmark 0x24bab/0x27fff lookup 252", false},
		{"2:\tfrom all fwmark 0x10000/0x10000 lookup 77", false},
		{"2:\tfrom all fwmark 0x24bab/0x27fff iif br0 lookup main suppress_prefixlength 7", false},
		{"5:\tfrom all fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7", false},
		{"2:\tfrom all fwmark 0x24bab/0xffffffff iif lo lookup main", false},
		{"2:\tfrom all fwmark 0x24bab/0x1027fff iif br0 lookup main", false},
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
			return `Error: argument "0x24bab/0x1027fff" is wrong: fwmark value is invalid`, errors.New("exit status 1")
		}
		return "", nil
	}
	return &cmds
}

func TestRouteAddSourceCheckRule_AddsTheRule(t *testing.T) {
	cmds := sourceCheckStubRun(t, true, false)
	routeAddSourceCheckRuleExec(config.TelegramBridgeMark)
	want := "ip rule add fwmark 0x24bab/0x1027fff iif lo lookup main priority 2"
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
			"2:      from all fwmark 0x24bab/0x1027fff iif lo lookup main\n" +
			"2:      from all to 10.0.0.0/8 fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7\n" +
			"2:      from all to 172.16.0.0/12 fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7\n" +
			"2:      from all to 192.168.0.0/16 fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7\n" +
			"2:      from all to 100.64.0.0/10 fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7\n" +
			"2:      from all fwmark 0x24bab/0x27fff iif lo lookup main suppress_prefixlength 7\n" +
			"2:      from all to 10.0.0.0/8 fwmark 0x2a1b3/0x27fff iif lo lookup main suppress_prefixlength 7\n" +
			"2:      from all to 192.168.0.0/16 fwmark 0x2a1b3/0x27fff iif lo lookup main suppress_prefixlength 7\n" +
			"2:      from all fwmark 0x2a1b3/0x1027fff iif lo lookup main\n" +
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

	if strings.Join(removed, " ") != "0x24bab/0x1027fff 0x24bab/0x27fff 0x2a1b3/0x27fff 0x2a1b3/0x1027fff" {
		t.Fatalf("source-check rules removed = %v, want one delete per mark and mask, which clears every rule of that shape; any other count leaves a set's leftover rules behind or runs the delete loop again for nothing", removed)
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
	if strings.Join(*removed, " ") != "0x2a1b3/0x1027fff 0x2a1b3/0x27fff" {
		t.Fatalf("a set that moved from mark 0x2a1b3 to 0x2b0c4 removed source-check rules %v, want both shapes of the old mark's alone", *removed)
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
	if strings.Join(*removed, " ") != "0x24bab/0x1027fff 0x24bab/0x27fff" {
		t.Fatalf("removing the Telegram bridge left its source-check rule, or one an earlier build added, behind: removed %v", *removed)
	}
}
