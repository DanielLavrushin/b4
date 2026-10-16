package tables

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
)

const (
	iptOwnSaveRule       = "-m mark --mark 0x8000/0x22008000 -j CONNMARK --save-mark --nfmask 0x8000 --ctmask 0x8000"
	iptInjectedSaveRule  = "-m mark --mark 0x2008000/0x22008000 -j CONNMARK --save-mark --nfmask 0x2000000 --ctmask 0x2000000"
	iptOwnReturnRule     = "-m connmark --mark 0x8000/0x8000 -j RETURN"
	iptInjectedReturn    = "-p tcp -m connmark --mark 0x2000000/0x2008000 --tcp-flags SYN,RST NONE -j RETURN"
	nftOwnSaveRule       = "meta mark & 0x22008000 == 0x8000 ct mark set ct mark | 0x8000"
	nftInjectedSaveRule  = "meta mark & 0x22008000 == 0x2008000 ct mark set ct mark | 0x2000000"
	nftOwnReturnRule     = "ct mark & 0x8000 == 0x8000 return"
	nftInjectedReturn    = "ct mark & 0x2008000 == 0x2000000 tcp flags & (syn|rst) == 0 return"
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

func requireNoQueueRuleOnTheInjectedBit(t *testing.T, where string, rules []string) {
	t.Helper()
	injected := regexp.MustCompile(fmt.Sprintf(`0x%x([^0-9a-fA-F]|$)`, engine.InjectedMarkBit))
	for _, r := range rules {
		if injected.MatchString(r) {
			t.Errorf("%s holds a rule on the bit for the connections b4 sends into, %q: those connections share the queue window of every other connection, since b4 processing their later packets lands its fakes inside the server's window", where, r)
		}
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
	if b4[0] != "-m mark --mark 0x8000/0x8000 -j RETURN" {
		t.Errorf("B4 must start with the return for b4's own packets, got %q", b4)
	}
	requireNoQueueRuleOnTheInjectedBit(t, "B4", b4)

	m := &Monitor{}
	if !m.checkRules(cfg) {
		t.Fatalf("the monitor finds rules missing right after they were installed")
	}

	preChain := h.iptTable(backendIPTables, "mangle").chain("B4_PREROUTING")
	preChain.rules = slices.DeleteFunc(preChain.rules, func(r string) bool { return r == iptInjectedReturn })
	if err := AddRules(cfg); err != nil {
		t.Fatalf("restore: %v", err)
	}
	pre = mangleRules(h, "B4_PREROUTING")
	firstQueue = slices.IndexFunc(pre, func(r string) bool { return goldenIptTarget(r) == "NFQUEUE" })
	if at := slices.Index(pre, iptInjectedReturn); at < 0 || at > firstQueue || countRule(pre, iptInjectedReturn) != 1 {
		t.Errorf("a restore put the return for the connections b4 sends into back at %d, first queue rule at %d: %q", at, firstQueue, pre)
	}
}

func TestTheMonitorRestoresTheTCPQueueRulesBesideTheReturnForConnectionsB4SendsInto(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	cfg := sendIntoTestConfig(backendIPTables, false)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	if !(&Monitor{}).checkRules(cfg) {
		t.Fatalf("the monitor finds rules missing right after they were installed")
	}

	preChain := h.iptTable(backendIPTables, "mangle").chain("B4_PREROUTING")
	preChain.rules = slices.DeleteFunc(preChain.rules, func(r string) bool {
		return strings.HasPrefix(r, "-p tcp ") && goldenIptTarget(r) == "NFQUEUE"
	})
	if countRule(preChain.rules, iptInjectedReturn) != 1 || !slices.ContainsFunc(preChain.rules, func(r string) bool { return goldenIptTarget(r) == "NFQUEUE" }) {
		t.Fatalf("the return for the connections b4 sends into or the DNS answer rule went with the TCP queue rules, so this test proves nothing: %q", preChain.rules)
	}
	if (&Monitor{}).checkRules(cfg) {
		t.Errorf("B4_PREROUTING holds no TCP queue rule, so b4 never sees a SYN-ACK or a reset, yet the monitor wants no restore: %q", preChain.rules)
	}
}

func TestTheReturnForConnectionsB4SendsIntoSkipsForeignMarksWithTheQueueBit(t *testing.T) {
	parseHex := func(t *testing.T, s string) uint32 {
		t.Helper()
		v, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 32)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		return uint32(v)
	}
	for _, q := range []uint{0x8000, 0x10000} {
		t.Run(fmt.Sprintf("queue mark %#x", q), func(t *testing.T) {
			spec := iptInjectedReplyReturn(q)
			at := slices.Index(spec, "--mark")
			if at < 0 || at+1 >= len(spec) {
				t.Fatalf("iptables return %q matches no connection mark", spec)
			}
			value, mask, ok := strings.Cut(spec[at+1], "/")
			if !ok {
				t.Fatalf("iptables return matches %q, want value/mask", spec[at+1])
			}
			iptValue, iptMask := parseHex(t, value), parseHex(t, mask)

			nft := strings.Join(nftInjectedReplyReturn(q), " ")
			fields := strings.Fields(nft)
			if len(fields) < 6 || fields[0] != "ct" || fields[1] != "mark" || fields[2] != "&" || fields[4] != "==" {
				t.Fatalf("nftables return %q does not start with a masked ct mark comparison", nft)
			}
			nftMask, nftValue := parseHex(t, fields[3]), parseHex(t, fields[5])
			if nftMask != iptMask || nftValue != iptValue {
				t.Errorf("the backends disagree: iptables %#x/%#x, nftables & %#x == %#x", iptValue, iptMask, nftMask, nftValue)
			}

			queueBits := uint32(q)
			for _, tc := range []struct {
				name    string
				ctmark  uint32
				returns bool
			}{
				{"a connection b4 sent packets into", engine.InjectedMarkBit, true},
				{"the same with an unrelated bit of another service", engine.InjectedMarkBit | 0x1, true},
				{"a KeeneticOS hotspot mark", 0x0ffff1a3 | queueBits, false},
				{"a foreign mark with the injected and the queue bit", engine.InjectedMarkBit | queueBits, false},
				{"b4's own connection", queueBits, false},
				{"a connection without a mark", 0, false},
			} {
				if got := tc.ctmark&iptMask == iptValue; got != tc.returns {
					t.Errorf("%s (ct mark %#x): returned %v, want %v", tc.name, tc.ctmark, got, tc.returns)
				}
			}
		})
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

func TestTheMarksForConnectionsB4SendsIntoNeedConnmark(t *testing.T) {
	injectedRules := func(m Manifest, ipt string) (saves, returns int) {
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
			default:
				t.Errorf("%s: unexpected rule with the bit for the connections b4 sends into, %s: %q", ipt, r.Chain, spec)
			}
		}
		return saves, returns
	}
	for _, tc := range []struct {
		name            string
		connmark, ctdir bool
		saves, returns  int
	}{
		{"both matches", true, true, 1, 1},
		{"no conntrack direction match", true, false, 1, 1},
		{"no connmark match", false, true, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := dnsQueryTestManager(t, nil)
			manager.connmarkSupport[backendIP6Tables] = tc.connmark
			manager.ctdirSupport[backendIP6Tables] = tc.ctdir
			m, err := manager.buildManifest()
			if err != nil {
				t.Fatalf("buildManifest: %v", err)
			}
			if s, r := injectedRules(m, backendIP6Tables); s != tc.saves || r != tc.returns {
				t.Errorf("ip6tables: %d save, %d return rules; want %d, %d", s, r, tc.saves, tc.returns)
			}
			if s, r := injectedRules(m, backendIPTables); s != 1 || r != 1 {
				t.Errorf("iptables, whose matches all load: %d save, %d return rules; want 1, 1", s, r)
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

	requireNoQueueRuleOnTheInjectedBit(t, nftChainName, table.chains[nftChainName])

	serveNftListings(t, table)
	if !(&Monitor{}).checkRules(cfg) {
		t.Errorf("the monitor finds rules missing right after they were installed")
	}
}

func TestNFTablesMarksTheConnectionsB4SendsIntoWithoutCtDirection(t *testing.T) {
	r := newDirectionRig(t)
	r.refuse = refuseNftCtDirectionProbe
	if err := AddRules(sendIntoTestConfig(backendNFTables, false)); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	table := nftEmittedTable(r.h.take())
	if countRule(table.chains["prerouting"], nftInjectedReturn) != 1 || countRule(table.chains["output"], nftInjectedSaveRule) != 1 {
		t.Errorf("the marks for the connections b4 sends into need no direction match, yet they are missing: prerouting %q, output %q", table.chains["prerouting"], table.chains["output"])
	}
	requireNoQueueRuleOnTheInjectedBit(t, nftChainName, table.chains[nftChainName])
}
