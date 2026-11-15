package tables

import (
	"slices"
	"strings"
	"testing"
)

const (
	iptLocalDeliveryReturnRule = "-o lo -m mark ! --mark 0x0/0x27fff -j RETURN"
	nftLocalDeliveryReturnRule = `oifname "lo" meta mark & 0x27fff != 0x0 return`
)

func requireLocalDeliveryReturnAboveTheQueue(t *testing.T, chain string, rules []string, want string, queues func(string) bool) {
	t.Helper()
	at, firstQueue := slices.Index(rules, want), slices.IndexFunc(rules, queues)
	switch {
	case firstQueue < 0:
		t.Fatalf("%s holds no queue rule, so this check proves nothing: %q", chain, rules)
	case at < 0:
		t.Errorf("%s lacks %q: a connection the router opens to a proxy set's address is delivered to the set's listener over the loopback interface, and without the return its packets are queued on the way, where a server name another set lists gets that set's strategy: %q", chain, want, rules)
	case at > firstQueue:
		t.Errorf("%s holds %q at %d, below the queue rule at %d, which takes the packet first: %q", chain, want, at, firstQueue, rules)
	}
}

func TestIPTablesKeepsALocalDeliveryToASetsListenerOutOfTheQueue(t *testing.T) {
	for _, l := range captureLayouts {
		t.Run(l.name, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			cfg := captureLayoutConfig(backendIPTables, l)
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			requireLocalDeliveryReturnAboveTheQueue(t, iptChainName, mangleRules(h, iptChainName), iptLocalDeliveryReturnRule,
				func(r string) bool { return goldenIptTarget(r) == "NFQUEUE" })
			if !(&Monitor{}).checkRules(cfg) {
				t.Errorf("the monitor finds rules missing right after they were installed")
			}
		})
	}
}

func TestARestoredLocalDeliveryReturnLandsAboveTheQueueRules(t *testing.T) {
	queues := func(r string) bool { return goldenIptTarget(r) == "NFQUEUE" }
	for _, c := range []struct {
		name  string
		gone  func(rule, own string) bool
		exact bool
	}{
		{"the return alone", func(r, own string) bool { return r == iptLocalDeliveryReturnRule }, true},
		{"the return and the one for b4's own packets", func(r, own string) bool { return r == iptLocalDeliveryReturnRule || r == own }, true},
		{"every return ahead of the queue rules", func(r, own string) bool {
			return goldenIptTarget(r) == "RETURN" && !strings.Contains(r, "--dport 53")
		}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			cfg := sendIntoTestConfig(backendIPTables, false)
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			chain := func() *goldenIptChain { return h.iptTable(backendIPTables, "mangle").chain(iptChainName) }
			fresh := slices.Clone(chain().rules)
			own := fresh[0]
			requireLocalDeliveryReturnAboveTheQueue(t, iptChainName, fresh, iptLocalDeliveryReturnRule, queues)

			chain().rules = slices.DeleteFunc(chain().rules, func(r string) bool { return c.gone(r, own) })
			if slices.Contains(chain().rules, iptLocalDeliveryReturnRule) || !slices.ContainsFunc(chain().rules, queues) {
				t.Fatalf("the deletion left %q, want the return gone and the queue rules in place", chain().rules)
			}
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules after the deletion: %v", err)
			}

			got := chain().rules
			requireLocalDeliveryReturnAboveTheQueue(t, iptChainName, got, iptLocalDeliveryReturnRule, queues)
			if got[0] != own {
				t.Errorf("after the restore %s starts with %q, want the return for b4's own packets %q", iptChainName, got[0], own)
			}
			if c.exact && !slices.Equal(got, fresh) {
				t.Errorf("after the restore %s = %q, want the chain of a fresh start %q", iptChainName, got, fresh)
			}
			if !(&Monitor{}).checkRules(cfg) {
				t.Errorf("the monitor still finds rules missing after they were put back, so it restores them on every check")
			}
		})
	}
}

func TestNFTablesKeepsALocalDeliveryToASetsListenerOutOfTheQueue(t *testing.T) {
	for _, l := range captureLayouts {
		t.Run(l.name, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			cfg := captureLayoutConfig(backendNFTables, l)
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			table := nftEmittedTable(h.take())
			requireLocalDeliveryReturnAboveTheQueue(t, nftChainName, table.chains[nftChainName], nftLocalDeliveryReturnRule,
				func(r string) bool { return strings.Contains(r, " queue ") })
			serveNftListings(t, table)
			if !(&Monitor{}).checkRules(cfg) {
				t.Errorf("the monitor finds rules missing right after they were installed")
			}
		})
	}
}
