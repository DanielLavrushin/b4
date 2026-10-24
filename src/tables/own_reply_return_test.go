package tables

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	iptOwnReplyReturnNarrow = "-m connmark --mark 0x8000/0x8000 -m conntrack --ctdir REPLY -m socket -j RETURN"
	iptOwnReplyReturnBroad  = "-m connmark --mark 0x8000/0x8000 -m conntrack --ctdir REPLY -j RETURN"
)

func refuseSocketProbe(bin string) func(args []string) bool {
	return func(args []string) bool {
		return args[0] == bin && slices.Contains(args, "B4_MODULE_TEST") && slices.Contains(args, "socket")
	}
}

func refuseNftSocketProbe(keys ...string) func(args []string) bool {
	return func(args []string) bool {
		if args[0] != "nft" || !slices.Contains(args, "_b4_queue_probe") || !slices.Contains(args, "socket") {
			return false
		}
		return slices.ContainsFunc(keys, func(key string) bool { return slices.Contains(args, key) })
	}
}

func iptOwnReplyReturnsIn(rules []string) []string {
	var out []string
	for _, r := range rules {
		if strings.HasPrefix(r, "-m connmark --mark 0x8000/0x8000 ") && goldenIptTarget(r) == "RETURN" {
			out = append(out, r)
		}
	}
	return out
}

func socketMatchProbes(lines []string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, "-A B4_MODULE_TEST -m socket") {
			n++
		}
	}
	return n
}

func TestIPTablesSkipsOnlyTheRepliesThatReachASocketOnTheRouter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		refuse func(args []string) bool
		want   string
	}{
		{"every match loads", nil, iptOwnReplyReturnNarrow},
		{"no socket match", refuseSocketProbe(backendIPTables), iptOwnReplyReturnBroad},
		{"no conntrack direction match", refuseCtdirProbe(backendIPTables), "-m connmark --mark 0x8000/0x8000 -m socket -j RETURN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDirectionRig(t)
			r.refuse = tc.refuse
			if err := AddRules(sendIntoTestConfig(backendIPTables, false)); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			pre := mangleRules(r.h, "B4_PREROUTING")
			if got := iptOwnReplyReturnsIn(pre); !slices.Equal(got, []string{tc.want}) {
				t.Fatalf("B4_PREROUTING skips the replies to b4's own connections with %q, want only %q", got, tc.want)
			}
			own, injected := slices.Index(pre, tc.want), slices.Index(pre, iptInjectedReturn)
			queue := slices.IndexFunc(pre, func(r string) bool { return goldenIptTarget(r) == "NFQUEUE" })
			if injected != own+1 || queue < injected {
				t.Errorf("B4_PREROUTING order: return for b4's own replies %d, for the connections b4 sends into %d, first queue rule %d: %q", own, injected, queue, pre)
			}
		})
	}
}

func TestNFTablesSkipsOnlyTheRepliesThatReachASocketOnTheRouter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		refuse func(args []string) bool
		want   string
	}{
		{"every expression loads", nil, "ct mark & 0x8000 == 0x8000 ct direction reply socket wildcard 0 return"},
		{"a kernel without socket wildcard", refuseNftSocketProbe("wildcard"), "ct mark & 0x8000 == 0x8000 ct direction reply socket transparent 0 return"},
		{"no socket expression", refuseNftSocketProbe("wildcard", "transparent"), "ct mark & 0x8000 == 0x8000 ct direction reply return"},
		{"no conntrack direction", refuseNftCtDirectionProbe, "ct mark & 0x8000 == 0x8000 socket wildcard 0 return"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDirectionRig(t)
			r.refuse = tc.refuse
			if err := AddRules(sendIntoTestConfig(backendNFTables, false)); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			pre := nftEmittedTable(r.h.take()).chains["prerouting"]
			var got []string
			for _, rule := range pre {
				if strings.HasPrefix(rule, "ct mark & 0x8000 == 0x8000 ") {
					got = append(got, rule)
				}
			}
			if !slices.Equal(got, []string{tc.want}) {
				t.Fatalf("prerouting skips the replies to b4's own connections with %q, want only %q", got, tc.want)
			}
			if own, queue := slices.Index(pre, tc.want), slices.IndexFunc(pre, func(r string) bool { return strings.Contains(r, " queue ") }); queue < own {
				t.Errorf("a queue rule sits above the return for b4's own replies: %q", pre)
			}
		})
	}
}

func TestTheSocketMatchReplacesTheBroaderReturnOnceItLoads(t *testing.T) {
	r := newDirectionRig(t)
	cfg := sendIntoTestConfig(backendIPTables, false)
	ownReturns := func() []string { return iptOwnReplyReturnsIn(mangleRules(r.h, "B4_PREROUTING")) }

	r.refuse = refuseSocketProbe(backendIPTables)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules without the socket match: %v", err)
	}
	if socketMatchProbes(r.h.take()) == 0 {
		t.Fatal("the first apply never probed the socket match, so this test proves nothing")
	}
	if got := ownReturns(); !slices.Equal(got, []string{iptOwnReplyReturnBroad}) {
		t.Fatalf("without the socket match B4_PREROUTING holds %q, want %q", got, iptOwnReplyReturnBroad)
	}

	if err := AddRules(cfg); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if n := socketMatchProbes(r.h.take()); n != 0 {
		t.Errorf("a socket match probe that failed with xt_socket absent ran again %d time(s); it must wait for the module, not run on every apply and monitor restore", n)
	}

	if err := os.MkdirAll(filepath.Join(kmodSysRoot, kmodCanonical("xt_socket")), 0o755); err != nil {
		t.Fatal(err)
	}
	r.refuse = nil
	if err := AddRules(cfg); err != nil {
		t.Fatalf("restore once xt_socket has loaded: %v", err)
	}
	if socketMatchProbes(r.h.take()) == 0 {
		t.Error("once xt_socket has loaded, the next apply must probe the socket match again")
	}
	if got := ownReturns(); !slices.Equal(got, []string{iptOwnReplyReturnNarrow}) {
		t.Errorf("after the socket match loaded B4_PREROUTING holds %q, want only %q: a broader return left in place still hides the replies of device connections", got, iptOwnReplyReturnNarrow)
	}

	r.refuse = refuseSocketProbe(backendIPTables)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("restore with a disturbed probe: %v", err)
	}
	if got := ownReturns(); !slices.Equal(got, []string{iptOwnReplyReturnNarrow}) {
		t.Errorf("a socket match that worked once gave way to %q after one failed probe, as when another program rewrites the table mid-probe", got)
	}
}

func TestTheIPv6AnswerRulesForECHKeysSkipOnlyTheRepliesThatReachASocket(t *testing.T) {
	h, _ := echOnlyRig(t, backendIPTables, true)
	pre := goldenIptRules(h, backendIP6Tables, "mangle", "B4_PREROUTING")
	want := "-m connmark --mark 0x8000/0x8000 -m socket -j RETURN"
	if got := iptOwnReplyReturnsIn(pre); !slices.Equal(got, []string{want}) {
		t.Fatalf("ip6tables B4_PREROUTING skips the replies to b4's own connections with %q, want only %q", got, want)
	}
	if own, answers := slices.Index(pre, want), indexOfRule(pre, "--sport 53", "NFQUEUE"); answers < own {
		t.Errorf("the DNS answer queue rule sits above the return for b4's own replies: %q", pre)
	}
}
