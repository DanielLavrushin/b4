package tables

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const dnsTCPChainName = "B4_DNSTCP"

const nftDstnatPriority = -100

var (
	dnsTCPListenerReadyV4 atomic.Bool
	dnsTCPListenerReadyV6 atomic.Bool
)

func SetDNSTCPListenerReady(v4, v6 bool) {
	dnsTCPListenerReadyV4.Store(v4)
	dnsTCPListenerReadyV6.Store(v6)
}

func dnsTCPWanted(cfg *config.Config) bool {
	return cfg.DNSTCPInterceptEnabled() && !cfg.Queue.IsDiscovery
}

func dnsTCPEnabledFamily(cfg *config.Config, v6 bool) bool {
	if !dnsTCPWanted(cfg) {
		return false
	}
	if v6 {
		return dnsTCPListenerReadyV6.Load()
	}
	return dnsTCPListenerReadyV4.Load()
}

var (
	natRedirectMu    sync.Mutex
	natRedirectCache = map[string]bool{}
)

func hasNATRedirectSupport(ipt string, port int) bool {
	natRedirectMu.Lock()
	if result, ok := natRedirectCache[ipt]; ok {
		natRedirectMu.Unlock()
		return result
	}
	natRedirectMu.Unlock()

	im := &IPTablesManager{}
	supported, err := im.probeModuleInTempChain(ipt, "nat",
		[]string{"-p", "tcp", "--dport", "53", "-j", "REDIRECT", "--to-ports", fmt.Sprintf("%d", port)})

	natRedirectMu.Lock()
	natRedirectCache[ipt] = supported
	natRedirectMu.Unlock()

	if supported {
		log.Tracef("IPTABLES[%s]: nat REDIRECT is available", ipt)
	} else {
		log.Warnf("IPTABLES[%s]: nat REDIRECT unavailable (%v), DNS over TCP stays with the upstream resolver for this family", ipt, err)
	}
	return supported
}

func (manager *IPTablesManager) dnsTCPMarkAccept() string {
	mark := manager.cfg.MainInjectedMark()
	return fmt.Sprintf("0x%x/0x%x", mark, mark)
}

func dnsTCPJumpSpec() []string {
	return []string{"-p", "tcp", "--dport", "53", "-j", dnsTCPChainName}
}

func dnsTCPRedirectSpec(port int) []string {
	return []string{"-p", "tcp", "--dport", "53", "-j", "REDIRECT", "--to-ports", fmt.Sprintf("%d", port)}
}

func dnsTCPRulesPresent(ipt string, port int) bool {
	im := &IPTablesManager{}
	return im.existsRule(ipt, "nat", "PREROUTING", dnsTCPJumpSpec()) &&
		im.existsRule(ipt, "nat", "OUTPUT", dnsTCPJumpSpec()) &&
		im.existsRule(ipt, "nat", dnsTCPChainName, dnsTCPRedirectSpec(port))
}

func (manager *IPTablesManager) buildDNSTCPManifest(ipt string) ([]Chain, []Rule) {
	chains := []Chain{{manager: manager, IPT: ipt, Table: "nat", Name: dnsTCPChainName}}
	rules := []Rule{
		{manager: manager, IPT: ipt, Table: "nat", Chain: dnsTCPChainName, Action: "A",
			Spec: []string{"-m", "mark", "--mark", manager.dnsTCPMarkAccept(), "-j", "RETURN"}},
		{manager: manager, IPT: ipt, Table: "nat", Chain: dnsTCPChainName, Action: "A",
			Spec: dnsTCPRedirectSpec(manager.cfg.DNSTCPListenPort())},
		{manager: manager, IPT: ipt, Table: "nat", Chain: "PREROUTING", Action: "I", Spec: dnsTCPJumpSpec()},
		{manager: manager, IPT: ipt, Table: "nat", Chain: "OUTPUT", Action: "I", Spec: dnsTCPJumpSpec()},
	}
	return chains, rules
}

func (im *IPTablesManager) teardownDNSTCPChain(ipt string) {
	im.delAll(ipt, "nat", "PREROUTING", dnsTCPJumpSpec())
	im.delAll(ipt, "nat", "OUTPUT", dnsTCPJumpSpec())
	if im.existsChain(ipt, "nat", dnsTCPChainName) {
		_, _ = run(ipt, "-w", "-t", "nat", "-F", dnsTCPChainName)
		_, _ = run(ipt, "-w", "-t", "nat", "-X", dnsTCPChainName)
	}
}

const (
	nftDNSTCPTableName  = "b4_dnsnat"
	nftDNSTCPTableName6 = "b4_dnsnat6"
)

func nftDNSTCPTable(v6 bool) (family, table string) {
	if v6 {
		return "ip6", nftDNSTCPTableName6
	}
	return "ip", nftDNSTCPTableName
}

func (n *NFTablesManager) dnsTCPTableExists() bool {
	out, err := n.runNft("list", "tables")
	if err != nil {
		return false
	}
	return strings.Contains(out, nftDNSTCPTableName) || strings.Contains(out, nftDNSTCPTableName6)
}

func (n *NFTablesManager) dnsTCPFamilyExists(v6 bool) bool {
	out, err := n.runNft("list", "tables")
	if err != nil {
		return false
	}
	_, table := nftDNSTCPTable(v6)
	return strings.Contains(out, table)
}

func (n *NFTablesManager) applyDNSTCPFamily(v6 bool) error {
	family, table := nftDNSTCPTable(v6)
	mark := fmt.Sprintf("0x%x", n.cfg.MainInjectedMark())
	port := fmt.Sprintf("%d", n.cfg.DNSTCPListenPort())

	if _, err := n.runNft("add", "table", family, table); err != nil {
		return fmt.Errorf("failed to create nftables dns nat table (%s): %w", family, err)
	}

	for _, c := range []struct{ chain, hook string }{
		{"dnstcp_pre", "prerouting"},
		{"dnstcp_out", "output"},
	} {
		chain, hook := c.chain, c.hook
		if _, err := n.runNft("add", "chain", family, table, chain,
			fmt.Sprintf("{ type nat hook %s priority %d ; policy accept ; }", hook, nftDstnatPriority)); err != nil {
			return fmt.Errorf("failed to create nat %s chain (%s): %w", hook, family, err)
		}
		if _, err := n.runNft("flush", "chain", family, table, chain); err != nil {
			return fmt.Errorf("failed to flush nat %s chain (%s): %w", hook, family, err)
		}
		if _, err := n.runNft("add", "rule", family, table, chain,
			"meta", "mark", "&", mark, "==", mark, "return"); err != nil {
			return fmt.Errorf("failed to add dns tcp mark-bypass rule (%s): %w", family, err)
		}
		if _, err := n.runNft("add", "rule", family, table, chain,
			"tcp", "dport", "53", "redirect", "to", ":"+port); err != nil {
			return fmt.Errorf("failed to add dns tcp redirect rule (%s): %w", family, err)
		}
	}
	return nil
}

func ApplyDNSTCPOnly(cfg *config.Config) error {
	if !dnsTCPWanted(cfg) {
		return nil
	}
	loadKernelModules()
	backend := detectFirewallBackend(cfg)
	if backend == backendNFTables {
		nft := NewNFTablesManager(cfg)
		if err := nft.createTable(); err != nil {
			return err
		}
		return nft.ApplyDNSTCP()
	}
	return NewIPTablesManager(cfg, backend == backendIPTablesLegacy).applyDNSTCPStandalone()
}

func ClearDNSTCPOnly(cfg *config.Config) {
	backend := detectFirewallBackend(cfg)
	if backend == backendNFTables {
		NewNFTablesManager(cfg).ClearDNSTCP()
		return
	}
	im := NewIPTablesManager(cfg, backend == backendIPTablesLegacy)
	for _, ipt := range im.teardownBinaries() {
		im.teardownDNSTCPChain(ipt)
	}
}

func (im *IPTablesManager) applyDNSTCPStandalone() error {
	for _, ipt := range im.applyBinaries() {
		if !dnsTCPEnabledFamily(im.cfg, ipt == backendIP6Tables || ipt == backendIP6TablesLegacy) {
			continue
		}
		if !hasNATRedirectSupport(ipt, im.cfg.DNSTCPListenPort()) {
			continue
		}
		im.teardownDNSTCPChain(ipt)
		chains, rules := im.buildDNSTCPManifest(ipt)
		for _, c := range chains {
			c.Ensure()
		}
		for _, r := range rules {
			if err := r.Apply(); err != nil {
				im.teardownDNSTCPChain(ipt)
				return fmt.Errorf("DNS-over-TCP redirect (%s): %w", ipt, err)
			}
		}
		log.Infof("IPTABLES[%s]: DNS-over-TCP redirect to port %d installed", ipt, im.cfg.DNSTCPListenPort())
	}
	return nil
}

func (n *NFTablesManager) ApplyDNSTCP() error {
	if !dnsTCPWanted(n.cfg) {
		return nil
	}

	log.Tracef("NFTABLES: adding DNS-over-TCP redirect rules")

	applied := make([]string, 0, 2)
	for _, v6 := range []bool{false, true} {
		if !dnsTCPEnabledFamily(n.cfg, v6) {
			continue
		}
		if v6 && !n.cfg.Queue.IPv6Enabled {
			continue
		}
		if !v6 && !n.cfg.Queue.IPv4Enabled {
			continue
		}
		if err := n.applyDNSTCPFamily(v6); err != nil {
			n.clearDNSTCPFamily(v6)
			log.Warnf("NFTABLES: %v, DNS over TCP stays with the upstream resolver for this family", err)
			continue
		}
		family, _ := nftDNSTCPTable(v6)
		applied = append(applied, family)
	}

	if len(applied) > 0 {
		log.Infof("NFTABLES: DNS over TCP redirected to b4 on port %d (%s)", n.cfg.DNSTCPListenPort(), strings.Join(applied, ", "))
	}
	return nil
}

func (n *NFTablesManager) clearDNSTCPFamily(v6 bool) {
	if !n.dnsTCPFamilyExists(v6) {
		return
	}
	family, table := nftDNSTCPTable(v6)
	if _, err := n.runNft("flush", "table", family, table); err != nil {
		log.Errorf("Failed to flush nftables dns nat table (%s): %v", family, err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := n.runNft("delete", "table", family, table); err != nil {
		log.Errorf("Failed to delete nftables dns nat table (%s): %v", family, err)
	}
}

func (n *NFTablesManager) ClearDNSTCP() {
	if !n.dnsTCPTableExists() {
		return
	}
	log.Tracef("NFTABLES: clearing DNS-over-TCP redirect rules")
	n.clearDNSTCPFamily(false)
	n.clearDNSTCPFamily(true)
}
