package tables

import (
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func echOnlyRig(t *testing.T, engine string, strip bool) (*goldenHost, *config.Config) {
	t.Helper()
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	noteDNSQueryPlacement(nil)
	resetECHOnlyState(t)
	prevApplied := rulesAppliedCfg
	t.Cleanup(func() {
		noteDNSQueryPlacement(nil)
		rulesAppliedCfg = prevApplied
	})

	c := config.NewConfig()
	cfg := &c
	cfg.System.Tables.Engine = engine
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	if strip {
		stripECHOnly(t, cfg)
	}
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	return h, cfg
}

func goldenIptRules(h *goldenHost, bin, table, chain string) []string {
	tb := h.ipt[bin][table]
	if tb == nil {
		return nil
	}
	c := tb.chain(chain)
	if c == nil {
		return nil
	}
	return c.rules
}

func goldenB4Leftovers(h *goldenHost, bin string) []string {
	var left []string
	for name, tb := range h.ipt[bin] {
		for _, c := range tb.chains {
			if strings.HasPrefix(c.name, "B4") {
				left = append(left, name+" chain "+c.name)
			}
			for _, r := range c.rules {
				if strings.Contains(r, "NFQUEUE") || strings.Contains(r, "B4") {
					left = append(left, name+" "+c.name+": "+r)
				}
			}
		}
	}
	return left
}

func indexOfRule(rules []string, parts ...string) int {
	return slices.IndexFunc(rules, func(r string) bool {
		for _, p := range parts {
			if !strings.Contains(r, p) {
				return false
			}
		}
		return true
	})
}

func TestIPTablesQueuesIPv6AnswersOnlyForECHKeysWhileIPv6IsOff(t *testing.T) {
	h, cfg := echOnlyRig(t, backendIPTables, true)
	v6 := backendIP6Tables

	pre := goldenIptRules(h, v6, "mangle", "B4_PREROUTING")
	if indexOfRule(pre, "--sport 53", "NFQUEUE") < 0 {
		t.Fatalf("ip6tables B4_PREROUTING = %q, want it to queue DNS answers: a validating resolver that reaches a zone's servers over IPv6 otherwise gets the ECH key b4 removes over IPv4", pre)
	}
	if indexOfRule(goldenIptRules(h, v6, "mangle", "PREROUTING"), "-j B4_PREROUTING") < 0 {
		t.Fatal("ip6tables PREROUTING does not jump to B4_PREROUTING")
	}
	out := goldenIptRules(h, v6, "mangle", "OUTPUT")
	accept, answers := indexOfRule(out, "--mark", "-j ACCEPT"), indexOfRule(out, "--sport 53", "NFQUEUE")
	if answers < 0 || accept < 0 || accept > answers {
		t.Fatalf("ip6tables OUTPUT = %q, want b4's own packets let through ahead of the DNS answer queue rule", out)
	}
	for _, chain := range []string{"B4_PREROUTING", "OUTPUT"} {
		if i := indexOfRule(goldenIptRules(h, v6, "mangle", chain), "--dport 53"); i >= 0 {
			t.Errorf("ip6tables %s queues DNS queries, but with IPv6 support off only answers are queued", chain)
		}
	}
	if pre := goldenIptRules(h, v6, "mangle", "B4_PREROUTING"); indexOfRule(pre, "-p tcp") >= 0 {
		t.Errorf("ip6tables B4_PREROUTING = %q, it must not queue TCP while IPv6 support is off", pre)
	}
	if tb := h.ipt[v6]["mangle"]; tb != nil && tb.chain(iptChainName) != nil {
		t.Errorf("ip6tables has the %s capture chain although IPv6 support is off", iptChainName)
	}
	if tb := h.ipt[v6]["nat"]; tb == nil || tb.chain(dnsTCPChainName) == nil {
		t.Error("DNS over TCP over IPv6 is not redirected to b4, so a resolver that asks the zone over TCP gets the key")
	} else if indexOfRule(tb.chain(dnsTCPChainName).rules, "fe80::/10", "RETURN") < 0 {
		t.Errorf("%s = %q: the relay cannot dial a link-local resolver without its zone, so TCP to fe80::/10 must stay out of the redirect", dnsTCPChainName, tb.chain(dnsTCPChainName).rules)
	}
	if tb := h.ipt[backendIPTables]["nat"]; tb != nil && tb.chain(dnsTCPChainName) != nil && indexOfRule(tb.chain(dnsTCPChainName).rules, "fe80") >= 0 {
		t.Error("the IPv4 DNS-over-TCP chain got an IPv6 link-local rule")
	}

	m := &Monitor{}
	if !m.checkRules(cfg) {
		t.Fatal("the monitor finds rules missing right after they were installed")
	}
	tb := h.ipt[v6]["mangle"].chain("OUTPUT")
	tb.rules = slices.Delete(tb.rules, answers, answers+1)
	if m.checkRules(cfg) {
		t.Error("the monitor did not notice that the IPv6 DNS answer rule left OUTPUT")
	}

	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	if left := goldenB4Leftovers(h, v6); len(left) > 0 {
		t.Errorf("ip6tables keeps b4 rules after the teardown: %q", left)
	}
}

func TestIPTablesDropsTheIPv6AnswerRulesWhenNoSetRemovesKeysAnyMore(t *testing.T) {
	h, _ := echOnlyRig(t, backendIPTables, true)
	if len(goldenB4Leftovers(h, backendIP6Tables)) == 0 {
		t.Fatal("the rig installed no IPv6 rules to begin with")
	}

	c := config.NewConfig()
	next := &c
	next.System.Tables.Engine = backendIPTables
	next.Queue.IPv6Enabled = false
	if err := RefreshRules(next); err != nil {
		t.Fatalf("RefreshRules: %v", err)
	}
	if left := goldenB4Leftovers(h, backendIP6Tables); len(left) > 0 {
		t.Errorf("ip6tables keeps b4 rules after the last set stopped removing ECH keys: %q", left)
	}
	if !(&Monitor{}).checkRules(next) {
		t.Error("the monitor still expects IPv6 answer rules after the last set stopped removing ECH keys")
	}
}

func resetECHOnlyState(t *testing.T) {
	t.Helper()
	reset := func() {
		echOnlyDNSUnusable.Clear()
		noteNftDNSTCPFailed(false, false)
		noteNftDNSTCPFailed(true, false)
	}
	reset()
	t.Cleanup(reset)
}

func TestIPTablesMonitorNoticesIPv6AnswerRulesAMissedRefreshSkipped(t *testing.T) {
	_, applied := echOnlyRig(t, backendIPTables, false)

	c := *applied
	wanted := &c
	stripECHOnly(t, wanted)
	if !wanted.ECHOnlyFamily(true) {
		t.Fatal("the wanted config must ask for ECH-only IPv6 answers")
	}
	if (&Monitor{}).checkRules(wanted) {
		t.Error("a set that started removing ECH keys while the refresh was missed left ip6tables without its answer rules, and the monitor did not notice")
	}
}

func TestIPTablesMonitorLeavesAnIP6TablesThatCannotQueueAlone(t *testing.T) {
	h, cfg := echOnlyRig(t, backendIPTables, true)
	for _, chain := range []string{"B4_PREROUTING", "OUTPUT"} {
		tb := h.ipt[backendIP6Tables]["mangle"].chain(chain)
		tb.rules = slices.DeleteFunc(tb.rules, func(r string) bool { return strings.Contains(r, "--sport 53") })
	}
	echOnlyDNSUnusable.Store(backendIP6Tables, true)
	if !(&Monitor{}).checkRules(cfg) {
		t.Error("ip6tables cannot queue, so the monitor must not ask for its answer rules and rebuild the firewall on every tick")
	}
}

func ech6Config(t *testing.T) *config.Config {
	t.Helper()
	c := config.NewConfig()
	cfg := &c
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	cfg.Queue.StartNum = 537
	cfg.Queue.Threads = 1
	stripECHOnly(t, cfg)
	return cfg
}

func TestNftStopsExpectingAnIPv6DNSOverTCPTableItCouldNotBuild(t *testing.T) {
	resetECHOnlyState(t)
	nftStubFailing(t, "add chain ip6 "+nftDNSTCPTableName6)
	cfg := ech6Config(t)

	if err := NewNFTablesManager(cfg).ApplyDNSTCP(); err != nil {
		t.Fatalf("ApplyDNSTCP: %v", err)
	}
	if nftDNSTCPExpected(cfg, true) {
		t.Error("the monitor still expects the IPv6 DNS-over-TCP table the kernel refused, so it rebuilds the firewall on every tick")
	}
	if !nftDNSTCPExpected(cfg, false) {
		t.Error("the IPv4 DNS-over-TCP table was built and must still be checked")
	}
}

func TestNftKeepsLinkLocalDNSOverTCPOutOfTheRelay(t *testing.T) {
	argvLog := nftFailingListStub(t)
	n := NewNFTablesManager(ech6Config(t))
	for _, v6 := range []bool{false, true} {
		if err := n.applyDNSTCPFamily(v6); err != nil {
			t.Fatalf("applyDNSTCPFamily(%v): %v", v6, err)
		}
	}
	lines := strings.Split(readLog(t, argvLog), "\n")
	for _, chain := range []string{nftDNSTCPPreChain, nftDNSTCPOutChain} {
		prefix := "add rule ip6 " + nftDNSTCPTableName6 + " " + chain + " "
		ret := slices.Index(lines, prefix+"ip6 daddr fe80::/10 return")
		redirect := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, prefix+"tcp dport 53 redirect") })
		if ret < 0 || redirect < 0 || ret > redirect {
			t.Errorf("%s: link-local destinations must return ahead of the redirect:\n%s", chain, strings.Join(lines, "\n"))
		}
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "add rule ip "+nftDNSTCPTableName+" ") && strings.Contains(l, "fe80") {
			t.Errorf("the IPv4 DNS-over-TCP table got an IPv6 rule: %s", l)
		}
	}
}

func TestIPTablesLeavesIPv6AloneWithoutASetThatRemovesKeys(t *testing.T) {
	h, _ := echOnlyRig(t, backendIPTables, false)
	if left := goldenB4Leftovers(h, backendIP6Tables); len(left) > 0 {
		t.Fatalf("ip6tables got b4 rules although IPv6 support is off and no set removes ECH keys: %q", left)
	}
}

type goldenNftCommands struct {
	rules  map[string][]string
	tables []string
}

func goldenNftEmitted(h *goldenHost) goldenNftCommands {
	got := goldenNftCommands{rules: map[string][]string{}}
	for _, line := range h.take() {
		f := strings.Fields(line)
		if len(f) < 6 || f[0] != "exec" || f[1] != "nft" || f[2] != "add" {
			continue
		}
		switch {
		case f[3] == "table":
			got.tables = append(got.tables, f[4]+" "+f[5])
		case f[3] == "rule" && len(f) > 7 && f[4] == "inet" && f[5] == nftTableName:
			got.rules[f[6]] = append(got.rules[f[6]], strings.Join(f[7:], " "))
		}
	}
	return got
}

func TestNftQueuesIPv6AnswersOnlyForECHKeysWhileIPv6IsOff(t *testing.T) {
	h, _ := echOnlyRig(t, backendNFTables, true)
	got := goldenNftEmitted(h)

	for _, chain := range []string{"prerouting", "output"} {
		rules := got.rules[chain]
		if indexOfRule(rules, "meta nfproto ipv6 udp sport 53 counter", "queue") < 0 {
			t.Errorf("%s = %q, want IPv6 DNS answers queued", chain, rules)
		}
		if indexOfRule(rules, "meta nfproto ipv4 udp sport 53", "queue") < 0 {
			t.Errorf("%s = %q, the IPv4 answer rule must stay", chain, rules)
		}
		if i := indexOfRule(rules, "meta nfproto ipv6", "dport 53"); i >= 0 {
			t.Errorf("%s queues IPv6 DNS queries: %q", chain, rules[i])
		}
	}
	for _, chain := range []string{nftRawPreChain, nftRawOutChain, nftChainName} {
		if i := indexOfRule(got.rules[chain], "nfproto ipv6"); i >= 0 {
			t.Errorf("%s handles IPv6 although only DNS answers are queued for ECH keys", chain)
		}
	}
	if !slices.Contains(got.tables, "ip6 "+nftDNSTCPTableName6) {
		t.Errorf("tables %q: DNS over TCP over IPv6 is not redirected to b4", got.tables)
	}
}

func TestNftLeavesIPv6AloneWithoutASetThatRemovesKeys(t *testing.T) {
	h, _ := echOnlyRig(t, backendNFTables, false)
	got := goldenNftEmitted(h)
	if len(got.rules["prerouting"]) == 0 {
		t.Fatal("the rig emitted no prerouting rules at all")
	}
	for chain, rules := range got.rules {
		if i := indexOfRule(rules, "nfproto ipv6"); i >= 0 {
			t.Errorf("%s queues IPv6 although IPv6 support is off and no set removes ECH keys: %q", chain, rules[i])
		}
	}
	if slices.Contains(got.tables, "ip6 "+nftDNSTCPTableName6) {
		t.Error("DNS over TCP over IPv6 is redirected although no set needs it")
	}
}

func TestNftListingOfIPv6AnswerRules(t *testing.T) {
	listing := "table inet b4_mangle {\n\tchain output {\n\t\tmeta nfproto ipv4 udp sport 53 udp dport != 53 counter packets 0 bytes 0 queue flags bypass to 537-540\n\t\tmeta nfproto ipv6 udp sport 53 counter packets 4 bytes 1188 queue flags bypass to 537-540\n\t}\n}\n"
	if !nftListingQueuesECHOnlyAnswers(listing, "ipv6") {
		t.Error("the IPv6 answer rule in a live listing was not found")
	}
	if nftListingQueuesECHOnlyAnswers(strings.Replace(listing, "meta nfproto ipv6 udp sport 53", "meta nfproto ipv6 udp sport 5353", 1), "ipv6") {
		t.Error("a rule for port 5353 was taken for the DNS answer rule")
	}
	if nftListingQueuesECHOnlyAnswers(strings.Replace(listing, "nfproto ipv6", "nfproto ipv4", 1), "ipv6") {
		t.Error("an IPv4 rule was taken for the IPv6 one")
	}
}
