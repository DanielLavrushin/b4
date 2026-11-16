package tables

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tproxy"
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

func requireRouterOwnProxyReturnAboveTheQueue(t *testing.T, chain string, rules []string, want string, queues func(string) bool) {
	t.Helper()
	at, firstQueue := slices.Index(rules, want), slices.IndexFunc(rules, queues)
	switch {
	case firstQueue < 0:
		t.Fatalf("%s holds no queue rule, so this check proves nothing: %q", chain, rules)
	case at < 0:
		t.Errorf("%s lacks %q: with a device filter the router's own packets reach it from the output hook, where a packet a proxy set has just marked still has the uplink as its output interface, and a proxy set enabled after the capture rules were written has no return of its own there: %q", chain, want, rules)
	case at > firstQueue:
		t.Errorf("%s holds %q at %d, below the queue rule at %d, which takes the packet first: %q", chain, want, at, firstQueue, rules)
	}
}

func iptMarkSpecMatches(t *testing.T, spec []string, mark uint32) bool {
	t.Helper()
	matched, seen := true, false
	for i := 0; i+1 < len(spec); i++ {
		if spec[i] != "--mark" {
			continue
		}
		seen = true
		value, mask, _ := strings.Cut(spec[i+1], "/")
		v, err1 := strconv.ParseUint(value, 0, 32)
		m, err2 := strconv.ParseUint(mask, 0, 32)
		if err1 != nil || err2 != nil {
			t.Fatalf("mark match %q does not parse", spec[i+1])
		}
		hit := mark&uint32(m) == uint32(v)
		if i > 0 && spec[i-1] == "!" {
			hit = !hit
		}
		matched = matched && hit
	}
	if !seen {
		t.Fatalf("no mark match in %q", spec)
	}
	return matched
}

func nftMarkRuleMatches(t *testing.T, rule []string, mark uint32) bool {
	t.Helper()
	matched, seen := true, false
	for i := 0; i+5 < len(rule); i++ {
		if rule[i] != "meta" || rule[i+1] != "mark" || rule[i+2] != "&" {
			continue
		}
		seen = true
		m, err1 := strconv.ParseUint(rule[i+3], 0, 32)
		v, err2 := strconv.ParseUint(rule[i+5], 0, 32)
		if err1 != nil || err2 != nil {
			t.Fatalf("mark test %q does not parse", rule[i:i+6])
		}
		hit := mark&uint32(m) == uint32(v)
		switch rule[i+4] {
		case "==":
		case "!=":
			hit = !hit
		default:
			t.Fatalf("mark test %q uses an operator this check does not know", rule[i:i+6])
		}
		matched = matched && hit
	}
	if !seen {
		t.Fatalf("no mark test in %q", rule)
	}
	return matched
}

func TestTheRouterOwnProxyReturnTakesOnlyThePacketsAProxySetMarkedOnTheRouter(t *testing.T) {
	hashed := tproxy.MarkForSet("p", 0)
	for _, c := range []struct {
		name string
		mark uint32
		want bool
	}{
		{"a router's own packet a proxy set marked", hashed | config.RouterOwnProxyMarkBit, true},
		{"a router's own packet a proxy set with a pinned mark marked", 0x123 | config.RouterOwnProxyMarkBit, true},
		{"a KeeneticOS hotspot mark", 0x0ffffaaa, false},
		{"a MagiTrickle mark", 0x4d616769, false},
		{"bit 0x1000000 with no set mark", config.RouterOwnProxyMarkBit, false},
		{"a device's packet a proxy set diverted", hashed, false},
		{"a router's own packet an interface set marked", 0x2bd8, false},
		{"the queue mark", 0x8000, false},
		{"a proxy set's mark with bit 0x1000000 and another service's bit 0x80000", hashed | config.RouterOwnProxyMarkBit | 0x80000, false},
		{"no mark", 0, false},
	} {
		if got := iptMarkSpecMatches(t, iptRouterOwnProxyReturn(), c.mark); got != c.want {
			t.Errorf("iptables, %s (0x%x): returned = %v, want %v", c.name, c.mark, got, c.want)
		}
		if got := nftMarkRuleMatches(t, nftRouterOwnProxyReturn(), c.mark); got != c.want {
			t.Errorf("nftables, %s (0x%x): returned = %v, want %v", c.name, c.mark, got, c.want)
		}
	}
}

func TestBothCaptureChainsReturnTheRoutersOwnProxiedPacketsAboveTheQueue(t *testing.T) {
	for _, l := range captureLayouts {
		t.Run(backendIPTables+"/"+l.name, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			cfg := captureLayoutConfig(backendIPTables, l)
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			requireRouterOwnProxyReturnAboveTheQueue(t, iptChainName, mangleRules(h, iptChainName), strings.Join(iptRouterOwnProxyReturn(), " "),
				func(r string) bool { return goldenIptTarget(r) == "NFQUEUE" })
			if !(&Monitor{}).checkRules(cfg) {
				t.Errorf("the monitor finds rules missing right after they were installed")
			}
		})
		t.Run(backendNFTables+"/"+l.name, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			cfg := captureLayoutConfig(backendNFTables, l)
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			table := nftEmittedTable(h.take())
			requireRouterOwnProxyReturnAboveTheQueue(t, nftChainName, table.chains[nftChainName], strings.Join(nftRouterOwnProxyReturn(), " "),
				func(r string) bool { return strings.Contains(r, " queue ") })
			serveNftListings(t, table)
			if !(&Monitor{}).checkRules(cfg) {
				t.Errorf("the monitor finds rules missing right after they were installed")
			}
		})
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
	routerOwn := strings.Join(iptRouterOwnProxyReturn(), " ")
	for _, c := range []struct {
		name  string
		gone  func(rule, own string) bool
		exact bool
	}{
		{"the return alone", func(r, own string) bool { return r == iptLocalDeliveryReturnRule }, true},
		{"the return and the one for b4's own packets", func(r, own string) bool { return r == iptLocalDeliveryReturnRule || r == own }, true},
		{"the return for the router's own proxied packets alone", func(r, own string) bool { return r == routerOwn }, true},
		{"both returns for proxied packets", func(r, own string) bool { return r == routerOwn || r == iptLocalDeliveryReturnRule }, true},
		{"the return for the router's own proxied packets and the one for b4's own packets", func(r, own string) bool { return r == routerOwn || r == own }, true},
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

			requireRouterOwnProxyReturnAboveTheQueue(t, iptChainName, fresh, routerOwn, queues)

			chain().rules = slices.DeleteFunc(chain().rules, func(r string) bool { return c.gone(r, own) })
			if !slices.ContainsFunc(chain().rules, queues) || slices.ContainsFunc(chain().rules, func(r string) bool { return c.gone(r, own) }) {
				t.Fatalf("the deletion left %q, want the chosen returns gone and the queue rules in place", chain().rules)
			}
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules after the deletion: %v", err)
			}

			got := chain().rules
			requireLocalDeliveryReturnAboveTheQueue(t, iptChainName, got, iptLocalDeliveryReturnRule, queues)
			requireRouterOwnProxyReturnAboveTheQueue(t, iptChainName, got, routerOwn, queues)
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

func TestNFTablesSkipsEveryProxySetsMarkInTheCaptureChain(t *testing.T) {
	for _, l := range captureLayouts {
		t.Run(l.name, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			cfg := captureLayoutConfig(backendNFTables, l)
			proxy := config.NewSetConfig()
			proxy.Id, proxy.Name, proxy.Enabled = "p", "p", true
			proxy.Routing.Enabled, proxy.Routing.Mode = true, config.RoutingModeProxy
			pinned := config.NewSetConfig()
			pinned.Id, pinned.Name, pinned.Enabled = "q", "q", true
			pinned.Routing.Enabled, pinned.Routing.Mode, pinned.Routing.FWMark = true, config.RoutingModeProxy, 0x123
			cfg.Sets = append(cfg.Sets, &proxy, &pinned)
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			table := nftEmittedTable(h.take())
			rules := table.chains[nftChainName]
			firstQueue := slices.IndexFunc(rules, func(r string) bool { return strings.Contains(r, " queue ") })
			if firstQueue < 0 {
				t.Fatalf("%s holds no queue rule, so this check proves nothing: %q", nftChainName, rules)
			}
			for _, mark := range []uint32{tproxy.MarkForConfig(cfg, &proxy), 0x123} {
				want := fmt.Sprintf("meta mark & 0x27fff == 0x%x return", mark)
				switch at := slices.Index(rules, want); {
				case at < 0:
					t.Errorf("%s lacks %q: with a device filter the router's own packets reach it from the output hook, where a packet a proxy set marked still has the uplink as its output interface, so only the set's mark keeps it out of the queue: %q", nftChainName, want, rules)
				case at > firstQueue:
					t.Errorf("%s holds %q at %d, below the queue rule at %d: %q", nftChainName, want, at, firstQueue, rules)
				}
			}
			serveNftListings(t, table)
			if !(&Monitor{}).checkRules(cfg) {
				t.Errorf("the monitor finds rules missing right after they were installed")
			}
		})
	}
}
