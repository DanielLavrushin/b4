package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func stickyIptRules(t *testing.T, fn func(be *routeIptBackend)) []string {
	t.Helper()
	prev := runLogged
	var cmds []string
	runLogged = func(op string, args ...string) bool {
		cmds = append(cmds, strings.Join(args, " "))
		return true
	}
	t.Cleanup(func() { runLogged = prev })

	be := &routeIptBackend{}
	if !hasBinary(be.ipt4()) {
		t.Skip("iptables not present")
	}
	fn(be)
	return cmds
}

func TestMarkRuleOnlyClaimsANewConnection(t *testing.T) {
	cmds := stickyIptRules(t, func(be *routeIptBackend) {
		be.addMarkRule("b4r_test_pre", false, "b4r_test_v4", 0x5179, "", true)
	})

	if len(cmds) != 3 {
		t.Fatalf("want a mark, a host-conntrack tag and a save, got %v", cmds)
	}
	for _, c := range cmds {
		if !strings.Contains(c, "-m conntrack --ctstate NEW") {
			t.Fatalf("every rule must be scoped to a new connection, or a live flow gets re-routed halfway: %q", c)
		}
		if !strings.Contains(c, "--match-set b4r_test_v4 dst") {
			t.Fatalf("rule lost its destination match: %q", c)
		}
	}
	if !strings.Contains(cmds[0], "MARK") {
		t.Fatalf("first rule should mark the packet: %q", cmds[0])
	}
	if !strings.Contains(cmds[2], "CONNMARK --save-mark") {
		t.Fatalf("the decision must be saved onto the connection: %q", cmds[2])
	}
}

func TestMarkRestoreRuleAppliesToEstablishedTrafficOnly(t *testing.T) {
	cmds := stickyIptRules(t, func(be *routeIptBackend) {
		be.addMarkRestoreRule("b4r_test_pre", false, "br0", 0x5179)
	})

	if len(cmds) != 1 {
		t.Fatalf("want exactly one restore rule, got %v", cmds)
	}
	c := cmds[0]
	if !strings.Contains(c, "! --ctstate NEW") {
		t.Fatalf("the restore must not touch a new connection, that is where the decision is made: %q", c)
	}
	if !strings.Contains(c, "--ctdir ORIGINAL") {
		t.Fatalf("the restore MUST be limited to the original direction. Without it a reply is marked too, "+
			"the set's ip rule sends it back out the egress interface instead of to the client, and the router "+
			"loops until it falls over: %q", c)
	}
	if !strings.Contains(c, "CONNMARK --restore-mark") {
		t.Fatalf("want a restore-mark: %q", c)
	}
	if !strings.Contains(c, "--nfmask 0x27fff --ctmask 0x27fff") {
		t.Fatalf("the restore must be masked to the routing bits so it leaves other marks alone: %q", c)
	}
}

func TestSaveAndRestoreUseTheSameMask(t *testing.T) {
	save := strings.Join(routeIptSaveMarkArgs(), " ")
	if !strings.Contains(save, "--nfmask 0x27fff --ctmask 0x27fff") {
		t.Fatalf("save mask must match the routing mark mask: %q", save)
	}

	cmds := stickyIptRules(t, func(be *routeIptBackend) {
		be.addMarkRestoreRule("b4r_test_pre", false, "br0", 0x5179)
	})
	if !strings.Contains(cmds[0], "--nfmask 0x27fff --ctmask 0x27fff") {
		t.Fatalf("restore mask must match the save mask, or a decision is read back wrong: %q", cmds[0])
	}
}

func TestRestoreIsEmittedBeforeTheMarkRules(t *testing.T) {
	be := &mockRouteBackend{}
	cfg := familyTestConfig(true, false)
	set := familyTestSet()

	st := buildRouteState(cfg, set)
	if err := routeEnsureRule(be, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureRule: %v", err)
	}

	ops := be.chainOps[st.chainPre]
	restore, mark := -1, -1
	for i, op := range ops {
		if op == "restore" && restore < 0 {
			restore = i
		}
		if strings.HasPrefix(op, "mark 0x") && mark < 0 {
			mark = i
		}
	}
	if restore < 0 || mark < 0 {
		t.Fatalf("want both a restore and a mark rule in %s, got %v", st.chainPre, ops)
	}
	if restore > mark {
		t.Fatalf("the restore must come first, otherwise an established flow is re-evaluated against the set: %v", ops)
	}
}

func TestRestoreNeverMarksAReply(t *testing.T) {
	iptCmds := stickyIptRules(t, func(be *routeIptBackend) {
		be.addMarkRestoreRule("b4r_test_pre", false, "br0", 0x5179)
	})
	if !strings.Contains(iptCmds[0], "--ctdir ORIGINAL") {
		t.Fatalf("iptables restore is not direction-scoped: %q", iptCmds[0])
	}

	prev := runLogged
	var nftCmds []string
	runLogged = func(op string, args ...string) bool { nftCmds = append(nftCmds, strings.Join(args, " ")); return true }
	t.Cleanup(func() { runLogged = prev })

	(&routeNftBackend{}).addMarkRestoreRule("b4r_test_pre", false, "br0", 0x5179)
	if len(nftCmds) != 1 {
		t.Fatalf("want one nft restore rule, got %v", nftCmds)
	}
	if !strings.Contains(nftCmds[0], "ct direction original") {
		t.Fatalf("nft restore is not direction-scoped, same loop: %q", nftCmds[0])
	}
	if !strings.Contains(nftCmds[0], "ct state != new") {
		t.Fatalf("nft restore must skip a new connection: %q", nftCmds[0])
	}
}

func TestMarkRuleMatchesTheDestinationOnlySoRepliesStayUnmarked(t *testing.T) {
	cmds := stickyIptRules(t, func(be *routeIptBackend) {
		be.addMarkRule("b4r_test_pre", false, "b4r_test_v4", 0x5179, "", true)
	})
	for _, c := range cmds {
		if strings.Contains(c, "--match-set b4r_test_v4 src") {
			t.Fatalf("matching the source would claim replies as well and route them back out the egress: %q", c)
		}
		if !strings.Contains(c, "--match-set b4r_test_v4 dst") {
			t.Fatalf("the routing mark is a destination decision: %q", c)
		}
	}
}

func TestEveryChainThatMarksAlsoRestores(t *testing.T) {
	be := &mockRouteBackend{}
	cfg := familyTestConfig(true, false)
	set := familyTestSet()
	set.Routing.RouterTraffic = config.RouterTrafficInclude

	st := buildRouteState(cfg, set)
	if err := routeEnsureRule(be, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureRule: %v", err)
	}

	for _, chain := range []string{st.chainPre, st.chainOut} {
		ops := be.chainOps[chain]
		restore, mark := -1, -1
		for i, op := range ops {
			if op == "restore" && restore < 0 {
				restore = i
			}
			if strings.HasPrefix(op, "mark 0x") && mark < 0 {
				mark = i
			}
		}
		if mark < 0 {
			continue
		}
		if restore < 0 {
			t.Fatalf("%s marks only the first packet of a connection but never restores that mark on the rest. "+
				"Packet two onwards falls back to the main table, so the connection is split between the set's "+
				"egress and the normal route and never completes: %v", chain, ops)
		}
		if restore > mark {
			t.Fatalf("%s restores after it marks, so an established flow is re-evaluated against the set: %v", chain, ops)
		}
	}
}

func TestRestoreOnlyTouchesConnectionsB4Tagged(t *testing.T) {
	cmds := stickyIptRules(t, func(be *routeIptBackend) {
		be.addMarkRestoreRule("b4r_test_pre", false, "br0", 0x5179)
	})

	c := cmds[0]
	if !strings.Contains(c, "-m connmark --mark 0x40000000/0x40000000") {
		t.Fatalf("the restore reads a conntrack mark back onto the packet and that decides which table the "+
			"kernel uses. Without the tag b4 puts on the connections it claimed, it promotes any conntrack "+
			"mark on the box into a routing decision, and it re-arms the routing mark on a packet coming back "+
			"out of the set's own egress: %q", c)
	}
	if !strings.Contains(c, "-i br0") {
		t.Fatalf("a set scoped to a source interface marks only traffic arriving there, so the restore has to "+
			"be scoped the same way or it re-arms the mark on an interface the set never claimed: %q", c)
	}
}

func TestRestoreIsScopedToEverySourceOfTheSet(t *testing.T) {
	be := &mockRouteBackend{}
	cfg := familyTestConfig(true, false)
	set := familyTestSet()
	set.Routing.SourceInterfaces = []string{"br0", "br1"}

	st := buildRouteState(cfg, set)
	if err := routeEnsureRule(be, cfg, set, st, []string{"br0", "br1"}); err != nil {
		t.Fatalf("routeEnsureRule: %v", err)
	}

	restores := 0
	for _, op := range be.chainOps[st.chainPre] {
		if op == "restore" {
			restores++
		}
	}
	if restores != 2 {
		t.Fatalf("want one restore per source interface so each is scoped, got %d in %v",
			restores, be.chainOps[st.chainPre])
	}
}
