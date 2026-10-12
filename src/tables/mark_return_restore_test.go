package tables

import (
	"fmt"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestARestoredMarkReturnLandsAboveTheRuleItGuards(t *testing.T) {
	for _, tc := range []struct {
		name         string
		facts        dnsKernel
		placement    dnsQueryPlacement
		table, chain string
	}{
		{"DNS queries copied with NFLOG", keeneticDNSKernel, dnsCopied, "mangle", iptDNSCopyChainName},
		{"DNS queries queued from the raw table", unaffectedDNSKernel, dnsHeldRaw, "raw", iptRawChainName},
	} {
		for _, mark := range []uint{config.NewConfig().Queue.Mark, 0x400000} {
			t.Run(fmt.Sprintf("%s, queue mark %#x", tc.name, mark), func(t *testing.T) {
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
				if err := AddRules(cfg); err != nil {
					t.Fatalf("AddRules: %v", err)
				}
				if got := dnsQueryPlacementOf(backendIPTables); got != tc.placement {
					t.Fatalf("DNS queries placed in %s, want %s", got, tc.placement)
				}
				chain := func() *goldenIptChain { return h.ipt[backendIPTables][tc.table].chain(tc.chain) }
				fresh := slices.Clone(chain().rules)
				ret := goldenIptCanon(iptMarkReturn(iptMarkAccept(mark)))
				if len(fresh) < 2 || fresh[0] != ret {
					t.Fatalf("%s = %q, want it to start with %q", tc.chain, fresh, ret)
				}

				m := &Monitor{}
				if !m.checkRules(cfg) {
					t.Fatalf("the monitor finds rules missing right after they were installed")
				}
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
