package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tproxy"
)

func captureEmitted(t *testing.T, fn func()) []string {
	t.Helper()
	orig := runLogged
	t.Cleanup(func() { runLogged = orig })

	var out []string
	runLogged = func(op string, args ...string) bool {
		out = append(out, strings.Join(args, " "))
		return true
	}
	fn()
	return out
}

func TestMarkMaskExcludesTheQueueAndSelfDialMarks(t *testing.T) {
	cfg := config.NewConfig()
	queue := routeQueueBypassMark(&cfg)

	if routeSetMarkMask&queue != 0 {
		t.Errorf("the routing mark mask 0x%x overlaps the queue bypass mark 0x%x, so claiming a set mark would disturb packets b4 injected itself", routeSetMarkMask, queue)
	}
	if routeSetMarkMask&SelfDialMark != 0 {
		t.Errorf("the routing mark mask 0x%x overlaps the self-dial mark 0x%x", routeSetMarkMask, SelfDialMark)
	}
}

func TestMarkMaskCoversEveryMarkASetCanBeGiven(t *testing.T) {
	for _, m := range []uint32{0x100, 0x7EFF, tproxy.MarkBase, tproxy.MarkBase + tproxy.MarkRange - 1} {
		if m&^routeSetMarkMask != 0 {
			t.Errorf("mark 0x%x has bits outside the mask 0x%x, so its rules would never match", m, routeSetMarkMask)
		}
	}
}

func TestMarkIsReplacedNotAccumulated(t *testing.T) {
	nft := strings.Join(routeNftSetMarkArgs(0x1b1d), " ")
	if !strings.Contains(nft, "meta mark set meta mark &") {
		t.Errorf("the nft mark must be applied through the mask so a second set replaces the first instead of OR-ing into a union: %q", nft)
	}
	if strings.HasSuffix(nft, "meta mark or 0x1b1d") && !strings.Contains(nft, "&") {
		t.Errorf("a bare OR lets two sets' marks combine into a third set's mark: %q", nft)
	}

	ipt := strings.Join(routeIptSetMarkArgs(0x1b1d), " ")
	if !strings.Contains(ipt, "--set-xmark") {
		t.Errorf("iptables --set-mark value/mask ORs the value in, so it must be --set-xmark to clear the other sets' bits: %q", ipt)
	}
	if !strings.Contains(ipt, "0x27fff") {
		t.Errorf("the xmark must clear the whole routing mark field, got %q", ipt)
	}
}

func TestChainGuardStopsAnySetMarkNotJustItsOwn(t *testing.T) {
	stubBinaries(t, backendIPTables, backendIP6Tables)

	nft := captureEmitted(t, func() { (&routeNftBackend{}).addClaimedBypassRule("b4r_x_pre", 0) })
	if len(nft) != 1 || !strings.Contains(nft[0], "meta mark & 0x27fff != 0x0 return") {
		t.Errorf("the nft chain guard must return for a packet any routing set already claimed, got %v", nft)
	}

	ipt := captureEmitted(t, func() { (&routeIptBackend{}).addClaimedBypassRule("b4r_x_pre", 0) })
	found := false
	for _, c := range ipt {
		if strings.Contains(c, "-m mark ! --mark 0x0/0x27fff -j RETURN") {
			found = true
		}
	}
	if !found {
		t.Errorf("the iptables chain guard must return for a packet any routing set already claimed, got %v", ipt)
	}
}

func TestChainGuardLetsTheChainsOwnMarkBackIn(t *testing.T) {
	stubBinaries(t, backendIPTables, backendIP6Tables)

	nft := captureEmitted(t, func() { (&routeNftBackend{}).addClaimedBypassRule("b4r_x_pre", 0x239c9) })
	if len(nft) != 1 || !strings.Contains(nft[0], "meta mark & 0x27fff != 0x0 meta mark & 0x27fff != 0x239c9 return") {
		t.Errorf("a proxy set marks the router's own connection in OUTPUT and the local route sends it back through prerouting, so the guard has to let that one mark past or the set's tproxy rule is never reached: %v", nft)
	}

	ipt := captureEmitted(t, func() { (&routeIptBackend{}).addClaimedBypassRule("b4r_x_pre", 0x239c9) })
	found := false
	for _, c := range ipt {
		if strings.Contains(c, "-m mark ! --mark 0x0/0x27fff -m mark ! --mark 0x239c9/0x27fff -j RETURN") {
			found = true
		}
	}
	if !found {
		t.Errorf("the iptables guard needs the same exemption for the chain's own mark, got %v", ipt)
	}
}

func TestChainGuardIgnoresAnOwnMarkOutsideTheMask(t *testing.T) {
	stubBinaries(t, backendIPTables, backendIP6Tables)

	nft := captureEmitted(t, func() { (&routeNftBackend{}).addClaimedBypassRule("b4r_x_pre", 0x100000) })
	if len(nft) != 1 || !strings.Contains(nft[0], "meta mark & 0x27fff != 0x0 return") {
		t.Errorf("a mark with bits outside the mask can never equal the masked field, so the second clause would always hold and the guard would stop returning at all: %v", nft)
	}

	ipt := captureEmitted(t, func() { (&routeIptBackend{}).addClaimedBypassRule("b4r_x_pre", 0x100000) })
	for _, c := range ipt {
		if strings.Contains(c, "0x100000") {
			t.Errorf("the exemption must be masked before it is emitted, got %v", ipt)
		}
	}
}

func TestPolicyRuleMatchesUnderTheSharedMask(t *testing.T) {
	if got := routeSetMarkRule(0x1b1d); got != "0x1b1d/0x27fff" {
		t.Errorf("ip rule fwmark form is %q; a self-masked form matches any superset mark and steals another set's traffic", got)
	}
}

func TestStaleRuleCleanupCoversTheLegacyForms(t *testing.T) {
	forms := routeStaleMarkRules(0x1b1d)
	want := []string{"0x1b1d", "0x1b1d/0x1b1d", "0x1b1d/0x27fff"}
	if len(forms) != len(want) {
		t.Fatalf("got %v, want the bare, legacy self-masked and shared-mask forms %v", forms, want)
	}
	for i := range want {
		if forms[i] != want[i] {
			t.Errorf("form %d is %q, want %q; an upgrade leaves the old rule behind otherwise", i, forms[i], want[i])
		}
	}
}

func TestPinnedMarkOutsideTheMaskIsRefused(t *testing.T) {
	origCache := routeRuleCache
	origAuto := routeIfaceAuto
	t.Cleanup(func() { routeRuleCache = origCache; routeIfaceAuto = origAuto })
	routeRuleCache = make(map[string]routeState)
	routeIfaceAuto = make(map[string]routeState)

	cfg := config.NewConfig()
	set := config.NewSetConfig()
	set.Id, set.Name = "pinned", "pinned"
	set.Routing.EgressInterface = "eth0"
	set.Routing.FWMark = 0x100000
	set.Routing.Table = 199

	mark, table := routeResolveIDs(&cfg, &set)
	if mark == 0x100000 || table == 199 {
		t.Errorf("a pinned mark with bits outside the mask was accepted (0x%x/%d); its rules would never match", mark, table)
	}
	if mark&^routeSetMarkMask != 0 {
		t.Errorf("the assigned mark 0x%x still has bits outside the mask 0x%x", mark, routeSetMarkMask)
	}
}
