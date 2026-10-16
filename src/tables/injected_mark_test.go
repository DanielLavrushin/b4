package tables

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const (
	iptOwnSaveRule       = "-m mark --mark 0x8000/0x22008000 -j CONNMARK --save-mark --nfmask 0x8000 --ctmask 0x8000"
	iptInjectedSaveRule  = "-m mark --mark 0x2008000/0x22008000 -j CONNMARK --save-mark --nfmask 0x2000000 --ctmask 0x2000000"
	iptOwnReturnRule     = "-m connmark --mark 0x8000/0x8000 -j RETURN"
	iptInjectedReturn    = "-p tcp -m connmark --mark 0x2000000/0x2000000 --tcp-flags SYN,RST NONE -j RETURN"
	iptWiderTCPWindow    = "-p tcp -m connbytes --connbytes-dir original --connbytes-mode packets --connbytes 20:38 -m connmark --mark 0x2000000/0x2000000 -m conntrack --ctdir ORIGINAL -j NFQUEUE --queue-balance 537:540 --queue-bypass"
	iptWiderUDPWindow    = "-p udp -m connbytes --connbytes-dir original --connbytes-mode packets --connbytes 9:16 -m connmark --mark 0x2000000/0x2000000 -m conntrack --ctdir ORIGINAL -j NFQUEUE --queue-balance 537:540 --queue-bypass"
	nftOwnSaveRule       = "meta mark & 0x22008000 == 0x8000 ct mark set ct mark | 0x8000"
	nftInjectedSaveRule  = "meta mark & 0x22008000 == 0x2008000 ct mark set ct mark | 0x2000000"
	nftOwnReturnRule     = "ct mark & 0x8000 == 0x8000 return"
	nftInjectedReturn    = "ct mark & 0x2000000 == 0x2000000 tcp flags & (syn|rst) == 0 return"
	nftWiderTCPWindow    = "meta l4proto tcp ct original packets 20-38 ct mark & 0x2000000 == 0x2000000 ct direction original counter queue num 537-540 bypass"
	nftWiderUDPWindow    = "meta l4proto udp ct original packets 9-16 ct mark & 0x2000000 == 0x2000000 ct direction original counter queue num 537-540 bypass"
	nftMarkAcceptRule    = "meta mark & 0x8000 == 0x8000 accept"
	iptMarkAcceptRuleStr = "-m mark --mark 0x8000/0x8000 -j ACCEPT"
)

func sendIntoTestConfig(backend string, incoming bool) *config.Config {
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backend
	cfg.Queue.IPv4Enabled = true
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "frag", "frag", true
	if incoming {
		set.TCP.Incoming.Mode = "fake"
	}
	cfg.Sets = []*config.SetConfig{&set}
	return &cfg
}

func mangleRules(h *goldenHost, chain string) []string {
	c := h.iptTable(backendIPTables, "mangle").chain(chain)
	if c == nil {
		return nil
	}
	return c.rules
}

func countRule(rules []string, rule string) int {
	n := 0
	for _, r := range rules {
		if r == rule {
			n++
		}
	}
	return n
}

func lastIndexFunc(rules []string, match func(string) bool) int {
	for i := len(rules) - 1; i >= 0; i-- {
		if match(rules[i]) {
			return i
		}
	}
	return -1
}

func requireRightAfter(t *testing.T, where string, rules []string, window func(string) bool, wider func(string) bool) {
	t.Helper()
	last := lastIndexFunc(rules, window)
	at := slices.IndexFunc(rules, wider)
	switch {
	case last < 0:
		t.Errorf("%s holds no window rule, so this check proves nothing: %q", where, rules)
	case at < 0:
		t.Errorf("%s lacks the wider window for the connections b4 sends into: %q", where, rules)
	case at != last+1:
		t.Errorf("%s: the wider window sits at %d, want it right after the last window rule at %d: %q", where, at, last, rules)
	}
}

func TestIPTablesTellsTheConnectionsB4SendsIntoFromItsOwn(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	cfg := sendIntoTestConfig(backendIPTables, false)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}

	out := mangleRules(h, "OUTPUT")
	accept := slices.Index(out, iptMarkAcceptRuleStr)
	if accept < 0 {
		t.Fatalf("mangle OUTPUT lost the ACCEPT for b4's own packets: %q", out)
	}
	for _, save := range []string{iptOwnSaveRule, iptInjectedSaveRule} {
		switch at := slices.Index(out, save); {
		case at < 0:
			t.Errorf("mangle OUTPUT lacks %q: %q", save, out)
		case at > accept:
			t.Errorf("%q sits below the ACCEPT for b4's own packets, which never reach it: %q", save, out)
		}
	}
	if slices.ContainsFunc(out, func(r string) bool { return strings.Contains(r, "0x8000/0x20008000") }) {
		t.Errorf("mangle OUTPUT still holds the save of earlier versions, which gives the connections b4 sends into the queue mark: %q", out)
	}

	pre := mangleRules(h, "B4_PREROUTING")
	own, injected := slices.Index(pre, iptOwnReturnRule), slices.Index(pre, iptInjectedReturn)
	firstQueue := slices.IndexFunc(pre, func(r string) bool { return goldenIptTarget(r) == "NFQUEUE" })
	switch {
	case own < 0 || injected < 0 || firstQueue < 0:
		t.Fatalf("B4_PREROUTING misses a rule (return for b4's own connections %d, for the ones it sends into %d, first queue rule %d): %q", own, injected, firstQueue, pre)
	case injected != own+1:
		t.Errorf("the return for the connections b4 sends into sits at %d, want it right below the one for b4's own at %d: %q", injected, own, pre)
	case injected > firstQueue:
		t.Errorf("the return for the connections b4 sends into sits below a queue rule, so their server data is queued: %q", pre)
	}

	b4 := mangleRules(h, iptChainName)
	requireRightAfter(t, "B4 (TCP)", b4,
		func(r string) bool {
			return strings.HasPrefix(r, "-p tcp -m multiport --dports ") && strings.Contains(r, "--connbytes 0:19 ")
		},
		func(r string) bool { return r == iptWiderTCPWindow })
	requireRightAfter(t, "B4 (UDP)", b4,
		func(r string) bool {
			return strings.HasPrefix(r, "-p udp -m multiport --dports ") && strings.Contains(r, "--connbytes 0:8 ")
		},
		func(r string) bool { return r == iptWiderUDPWindow })
	if b4[0] != "-m mark --mark 0x8000/0x8000 -j RETURN" {
		t.Errorf("B4 must start with the return for b4's own packets, got %q", b4)
	}

	m := &Monitor{}
	if !m.checkRules(cfg) {
		t.Fatalf("the monitor finds rules missing right after they were installed")
	}

	preChain := h.iptTable(backendIPTables, "mangle").chain("B4_PREROUTING")
	preChain.rules = slices.DeleteFunc(preChain.rules, func(r string) bool { return r == iptInjectedReturn })
	b4Chain := h.iptTable(backendIPTables, "mangle").chain(iptChainName)
	b4Chain.rules = slices.DeleteFunc(b4Chain.rules, func(r string) bool { return r == iptWiderTCPWindow })
	if err := AddRules(cfg); err != nil {
		t.Fatalf("restore: %v", err)
	}
	pre = mangleRules(h, "B4_PREROUTING")
	firstQueue = slices.IndexFunc(pre, func(r string) bool { return goldenIptTarget(r) == "NFQUEUE" })
	if at := slices.Index(pre, iptInjectedReturn); at < 0 || at > firstQueue || countRule(pre, iptInjectedReturn) != 1 {
		t.Errorf("a restore put the return for the connections b4 sends into back at %d, first queue rule at %d: %q", at, firstQueue, pre)
	}
	if got := countRule(mangleRules(h, iptChainName), iptWiderTCPWindow); got != 1 {
		t.Errorf("after a restore B4 holds the wider TCP window %d times, want once", got)
	}
}

func TestAnIncomingModeSetKeepsTheServersDataOfConnectionsB4SendsInto(t *testing.T) {
	t.Run(backendIPTables, func(t *testing.T) {
		h := newGoldenHost(t)
		goldenIsolate(t, h)
		incoming, plain := sendIntoTestConfig(backendIPTables, true), sendIntoTestConfig(backendIPTables, false)
		for i, step := range []struct {
			cfg  *config.Config
			want int
		}{{incoming, 0}, {plain, 1}, {incoming, 0}} {
			var err error
			if i == 0 {
				err = AddRules(step.cfg)
			} else {
				err = RefreshRules(step.cfg)
			}
			if err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
			pre, out := mangleRules(h, "B4_PREROUTING"), mangleRules(h, "OUTPUT")
			if got := countRule(pre, iptInjectedReturn); got != step.want {
				t.Errorf("step %d (incoming mode %v): B4_PREROUTING holds the return for the connections b4 sends into %d times, want %d: %q", i, step.cfg.AnySetTCPIncoming(), got, step.want, pre)
			}
			for _, save := range []string{iptOwnSaveRule, iptInjectedSaveRule} {
				if countRule(out, save) != 1 {
					t.Errorf("step %d: mangle OUTPUT holds %q %d times, want once", i, save, countRule(out, save))
				}
			}
			if countRule(mangleRules(h, iptChainName), iptWiderTCPWindow) != 1 {
				t.Errorf("step %d: the wider TCP window is not in B4 exactly once", i)
			}
		}
	})

	t.Run(backendNFTables, func(t *testing.T) {
		h := newGoldenHost(t)
		goldenIsolate(t, h)
		incoming, plain := sendIntoTestConfig(backendNFTables, true), sendIntoTestConfig(backendNFTables, false)
		var lines []string
		for i, step := range []struct {
			cfg  *config.Config
			want int
		}{{incoming, 0}, {plain, 1}, {incoming, 0}} {
			var err error
			if i == 0 {
				err = AddRules(step.cfg)
			} else {
				err = RefreshRules(step.cfg)
			}
			if err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
			lines = append(lines, h.take()...)
			table := nftEmittedTable(lines)
			if got := countRule(table.chains["prerouting"], nftInjectedReturn); got != step.want {
				t.Errorf("step %d (incoming mode %v): prerouting holds the return for the connections b4 sends into %d times, want %d: %q", i, step.cfg.AnySetTCPIncoming(), got, step.want, table.chains["prerouting"])
			}
			if countRule(table.chains["output"], nftInjectedSaveRule) != 1 {
				t.Errorf("step %d: output lacks %q", i, nftInjectedSaveRule)
			}
		}
	})
}

func TestTheWiderWindowAndTheMarksNeedTheirMatches(t *testing.T) {
	injectedRules := func(m Manifest, ipt string) (saves, returns, windows int) {
		for _, r := range m.Rules {
			spec := strings.Join(r.Spec, " ")
			if r.IPT != ipt || !strings.Contains(spec, "0x2000000") {
				continue
			}
			switch {
			case r.Chain == "OUTPUT" && strings.Contains(spec, "CONNMARK"):
				saves++
			case r.Chain == "B4_PREROUTING" && strings.HasSuffix(spec, "-j RETURN"):
				returns++
			case r.Chain == iptChainName && strings.Contains(spec, "NFQUEUE"):
				windows++
			default:
				t.Errorf("%s: unexpected rule with the bit for the connections b4 sends into, %s: %q", ipt, r.Chain, spec)
			}
		}
		return saves, returns, windows
	}
	for _, tc := range []struct {
		name                    string
		connmark, ctdir         bool
		saves, returns, windows int
	}{
		{"both matches", true, true, 1, 1, 2},
		{"no conntrack direction match", true, false, 1, 1, 0},
		{"no connmark match", false, true, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := dnsQueryTestManager(t, nil)
			manager.connmarkSupport[backendIP6Tables] = tc.connmark
			manager.ctdirSupport[backendIP6Tables] = tc.ctdir
			m, err := manager.buildManifest()
			if err != nil {
				t.Fatalf("buildManifest: %v", err)
			}
			if s, r, w := injectedRules(m, backendIP6Tables); s != tc.saves || r != tc.returns || w != tc.windows {
				t.Errorf("ip6tables: %d save, %d return, %d wider window rules; want %d, %d, %d", s, r, w, tc.saves, tc.returns, tc.windows)
			}
			if s, r, w := injectedRules(m, backendIPTables); s != 1 || r != 1 || w != 2 {
				t.Errorf("iptables, whose matches all load: %d save, %d return, %d wider window rules; want 1, 1, 2", s, r, w)
			}
			if !tc.connmark && slices.ContainsFunc(m.Rules, func(r Rule) bool {
				return r.IPT == backendIP6Tables && strings.Contains(strings.Join(r.Spec, " "), "connmark")
			}) {
				t.Errorf("ip6tables got a connmark rule its probe refused")
			}
		})
	}
}

func TestNFTablesTellsTheConnectionsB4SendsIntoFromItsOwn(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	cfg := sendIntoTestConfig(backendNFTables, false)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	table := nftEmittedTable(h.take())

	out := table.chains["output"]
	accept := slices.Index(out, nftMarkAcceptRule)
	if accept < 0 {
		t.Fatalf("output lost the accept for b4's own packets: %q", out)
	}
	for _, save := range []string{nftOwnSaveRule, nftInjectedSaveRule} {
		switch at := slices.Index(out, save); {
		case at < 0:
			t.Errorf("output lacks %q: %q", save, out)
		case at > accept:
			t.Errorf("%q sits below the accept for b4's own packets, which never reach it: %q", save, out)
		}
	}

	pre := table.chains["prerouting"]
	own, injected := slices.Index(pre, nftOwnReturnRule), slices.Index(pre, nftInjectedReturn)
	firstQueue := slices.IndexFunc(pre, func(r string) bool { return strings.Contains(r, " queue ") })
	switch {
	case own < 0 || injected < 0 || firstQueue < 0:
		t.Fatalf("prerouting misses a rule (return for b4's own connections %d, for the ones it sends into %d, first queue rule %d): %q", own, injected, firstQueue, pre)
	case injected != own+1:
		t.Errorf("the return for the connections b4 sends into sits at %d, want it right below the one for b4's own at %d: %q", injected, own, pre)
	case injected > firstQueue:
		t.Errorf("the return for the connections b4 sends into sits below a queue rule: %q", pre)
	}

	b4 := table.chains[nftChainName]
	requireRightAfter(t, "b4_chain (TCP)", b4,
		func(r string) bool { return strings.Contains(r, "tcp dport 443 ct original packets < 20 ") },
		func(r string) bool { return strings.HasSuffix(r, nftWiderTCPWindow) })
	requireRightAfter(t, "b4_chain (UDP)", b4,
		func(r string) bool { return strings.Contains(r, "udp dport 443 ct original packets < 9 ") },
		func(r string) bool { return strings.HasSuffix(r, nftWiderUDPWindow) })
	for _, r := range b4 {
		if strings.HasSuffix(r, nftWiderTCPWindow) && !strings.HasPrefix(r, "meta nfproto ipv4 ") {
			t.Errorf("the wider window skips the family filter of the other queue rules: %q", r)
		}
	}

	serveNftListings(t, table)
	if !(&Monitor{}).checkRules(cfg) {
		t.Errorf("the monitor finds rules missing right after they were installed")
	}
}

func TestNFTablesWithoutCtDirectionKeepsTheWindowItHad(t *testing.T) {
	r := newDirectionRig(t)
	r.refuse = refuseNftCtDirectionProbe
	if err := AddRules(sendIntoTestConfig(backendNFTables, false)); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	table := nftEmittedTable(r.h.take())
	for chain, rules := range table.chains {
		for _, rule := range rules {
			if strings.Contains(rule, "ct original packets 20-38") || strings.Contains(rule, "ct original packets 9-16") {
				t.Errorf("%s holds a wider window although its direction match is unavailable: %q", chain, rule)
			}
		}
	}
	if countRule(table.chains["prerouting"], nftInjectedReturn) != 1 || countRule(table.chains["output"], nftInjectedSaveRule) != 1 {
		t.Errorf("the marks for the connections b4 sends into need no direction match, yet they are missing: prerouting %q, output %q", table.chains["prerouting"], table.chains["output"])
	}
}

func TestNFTablesGoesOnWhenTheWiderWindowIsRefused(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	wrap := t.TempDir()
	stub := "#!/bin/sh\ncase \"$*\" in\n*\"ct original packets 20-38\"*) printf 'Error: Could not process rule: Operation not supported\\n' >&2; exit 1 ;;\nesac\nexec '" + filepath.Join(h.dir, "bin", "nft") + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(wrap, "nft"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrap+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := AddRules(sendIntoTestConfig(backendNFTables, false)); err != nil {
		t.Fatalf("a refused wider window failed the whole apply: %v", err)
	}
	table := nftEmittedTable(h.take())
	b4 := table.chains[nftChainName]
	if slices.ContainsFunc(b4, func(r string) bool { return strings.HasSuffix(r, nftWiderTCPWindow) }) {
		t.Fatalf("the refused rule is listed, so this test proves nothing: %q", b4)
	}
	if !slices.ContainsFunc(b4, func(r string) bool { return strings.HasSuffix(r, nftWiderUDPWindow) }) {
		t.Errorf("the rules after the refused one did not go in: %q", b4)
	}
	if !slices.ContainsFunc(table.chains["prerouting"], func(r string) bool { return strings.Contains(r, "tcp sport 443") && strings.Contains(r, " queue ") }) {
		t.Errorf("prerouting lost its queue rules: %q", table.chains["prerouting"])
	}
}
