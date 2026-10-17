package tables

import (
	"fmt"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

type markReturnChain struct {
	name         string
	facts        dnsKernel
	placement    dnsQueryPlacement
	table, chain string
	tune         func(t *testing.T, cfg *config.Config)
	own          func(mark uint) string
}

var (
	markReturnChains = []markReturnChain{
		{"the capture chain", unaffectedDNSKernel, dnsHeldRaw, "mangle", iptChainName, nil, iptOwnPacketMark},
		{"DNS queries copied with NFLOG", keeneticDNSKernel, dnsCopied, "mangle", iptDNSCopyChainName, nil, iptOwnPacketMark},
		{"DNS queries queued from the raw table", unaffectedDNSKernel, dnsHeldRaw, "raw", iptRawChainName, nil, iptOwnPacketMark},
		{"DNS over TCP for a set that only removes ECH keys", keeneticDNSKernel, dnsCopied, "nat", dnsTCPChainName, stripECHOnly, iptMarkAccept},
	}
	markReturnMarks = []uint{config.NewConfig().Queue.Mark, 0x400000}
)

func stripECHOnly(t *testing.T, cfg *config.Config) {
	t.Helper()
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "ech", "ech", true
	set.Targets.SNIDomains = []string{"cloudflare-ech.com"}
	set.DNS.StripECH = true
	cfg.Sets = append(cfg.Sets, &set)
	if !cfg.DNSTCPInterceptEnabled() || cfg.HasDNSRedirect() {
		t.Fatalf("the set must turn DNS over TCP interception on with no resolver of its own")
	}
	prev4, prev6 := dnsTCPListenerReadyV4.Load(), dnsTCPListenerReadyV6.Load()
	SetDNSTCPListenerReady(true, true)
	t.Cleanup(func() { SetDNSTCPListenerReady(prev4, prev6) })
	natRedirectMu.Lock()
	prevNat := natRedirectCache
	natRedirectCache = map[string]bool{}
	natRedirectMu.Unlock()
	t.Cleanup(func() {
		natRedirectMu.Lock()
		natRedirectCache = prevNat
		natRedirectMu.Unlock()
	})
}

func markReturnRig(t *testing.T, tc markReturnChain, mark uint) (*config.Config, func() *goldenIptChain) {
	t.Helper()
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	stubDNSKernel(t, tc.facts)
	stubDNSCopyListener(t, func() (uint16, error) { return 537, nil })
	stubDNSCopyAlive(t, func() bool { return true })
	noteDNSQueryPlacement(nil)
	t.Cleanup(func() { noteDNSQueryPlacement(nil) })

	c := config.NewConfig()
	cfg := &c
	cfg.System.Tables.Engine = backendIPTables
	cfg.Queue.Mark = mark
	if tc.tune != nil {
		tc.tune(t, cfg)
	}
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	if got := dnsQueryPlacementOf(backendIPTables); got != tc.placement {
		t.Fatalf("DNS queries placed in %s, want %s", got, tc.placement)
	}
	chain := func() *goldenIptChain { return h.ipt[backendIPTables][tc.table].chain(tc.chain) }
	if rules := chain().rules; len(rules) < 2 || rules[0] != goldenIptCanon(iptMarkReturn(tc.own(mark))) {
		t.Fatalf("%s = %q, want it to start with the return for b4's own packets", tc.chain, rules)
	}
	if !(&Monitor{}).checkRules(cfg) {
		t.Fatalf("the monitor finds rules missing right after they were installed")
	}
	return cfg, chain
}

func TestARestoredMarkReturnLandsAboveTheRuleItGuards(t *testing.T) {
	for _, tc := range markReturnChains {
		for _, mark := range markReturnMarks {
			t.Run(fmt.Sprintf("%s, queue mark %#x", tc.name, mark), func(t *testing.T) {
				cfg, chain := markReturnRig(t, tc, mark)
				fresh := slices.Clone(chain().rules)

				m := &Monitor{}
				chain().rules = slices.Delete(chain().rules, 0, 1)
				if m.checkRules(cfg) {
					t.Errorf("the monitor did not notice that the return for b4's own packets was deleted from %s", tc.chain)
				}
				if err := AddRules(cfg); err != nil {
					t.Fatalf("AddRules after the deletion: %v", err)
				}
				if got := chain().rules; !slices.Equal(got, fresh) {
					t.Errorf("after the restore %s = %q, want %q: below %q the return lets b4's own packets reach it", tc.chain, got, fresh, fresh[1])
				}
				if !m.checkRules(cfg) {
					t.Errorf("the monitor still finds rules missing after they were put back, so it restores them on every check")
				}
			})
		}
	}
}

func TestTheMonitorWantsTheMarkReturnForTheConfiguredQueueMark(t *testing.T) {
	for _, tc := range markReturnChains {
		for _, mark := range markReturnMarks {
			t.Run(fmt.Sprintf("%s, queue mark %#x", tc.name, mark), func(t *testing.T) {
				cfg, chain := markReturnRig(t, tc, mark)
				other := uint(0x400000)
				if mark == other {
					other = 0x8000
				}
				want := goldenIptCanon(iptMarkReturn(tc.own(mark)))
				chain().rules[0] = goldenIptCanon(iptMarkReturn(tc.own(other)))

				m := &Monitor{}
				if m.checkRules(cfg) {
					t.Errorf("the monitor accepted %s returning mark %#x in place of the queue mark %#x, so b4's own packets reach the rules below it", tc.chain, other, mark)
				}
				if err := AddRules(cfg); err != nil {
					t.Fatalf("AddRules: %v", err)
				}
				if got := chain().rules; got[0] != want {
					t.Errorf("after the restore %s = %q, want %q first", tc.chain, got, want)
				}
				if !m.checkRules(cfg) {
					t.Errorf("the monitor still finds rules missing after the restore, so it restores them on every check")
				}
			})
		}
	}
}
