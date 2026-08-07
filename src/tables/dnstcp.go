package tables

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const dnsTCPChainName = "B4_DNSTCP"

var dnsTCPListenerReady atomic.Bool

func SetDNSTCPListenerReady(ready bool) {
	dnsTCPListenerReady.Store(ready)
}

func dnsTCPEnabled(cfg *config.Config) bool {
	return cfg.HasDNSRedirect() && !cfg.Queue.IsDiscovery && dnsTCPListenerReady.Load()
}

func (manager *IPTablesManager) dnsTCPMarkAccept() string {
	mark := manager.cfg.MainInjectedMark()
	return fmt.Sprintf("0x%x/0x%x", mark, mark)
}

func (manager *IPTablesManager) buildDNSTCPManifest(ipt string) ([]Chain, []Rule) {
	port := fmt.Sprintf("%d", config.DNSTCPPort)
	chains := []Chain{{manager: manager, IPT: ipt, Table: "nat", Name: dnsTCPChainName}}
	rules := []Rule{
		{manager: manager, IPT: ipt, Table: "nat", Chain: dnsTCPChainName, Action: "A",
			Spec: []string{"-m", "mark", "--mark", manager.dnsTCPMarkAccept(), "-j", "RETURN"}},
		{manager: manager, IPT: ipt, Table: "nat", Chain: dnsTCPChainName, Action: "A",
			Spec: []string{"-p", "tcp", "--dport", "53", "-j", "REDIRECT", "--to-ports", port}},
		{manager: manager, IPT: ipt, Table: "nat", Chain: "PREROUTING", Action: "I",
			Spec: []string{"-p", "tcp", "--dport", "53", "-j", dnsTCPChainName}},
		{manager: manager, IPT: ipt, Table: "nat", Chain: "OUTPUT", Action: "I",
			Spec: []string{"-p", "tcp", "--dport", "53", "-j", dnsTCPChainName}},
	}
	return chains, rules
}

func (im *IPTablesManager) teardownDNSTCPChain(ipt string) {
	im.delAll(ipt, "nat", "PREROUTING", []string{"-p", "tcp", "--dport", "53", "-j", dnsTCPChainName})
	im.delAll(ipt, "nat", "OUTPUT", []string{"-p", "tcp", "--dport", "53", "-j", dnsTCPChainName})
	if im.existsChain(ipt, "nat", dnsTCPChainName) {
		_, _ = run(ipt, "-w", "-t", "nat", "-F", dnsTCPChainName)
		_, _ = run(ipt, "-w", "-t", "nat", "-X", dnsTCPChainName)
	}
}

const nftDNSTCPTableName = "b4_dnsnat"

func (n *NFTablesManager) dnsTCPTableExists() bool {
	out, err := n.runNft("list", "tables")
	if err != nil {
		return false
	}
	return strings.Contains(out, nftDNSTCPTableName)
}

func (n *NFTablesManager) ApplyDNSTCP() error {
	if !dnsTCPEnabled(n.cfg) {
		return nil
	}

	log.Tracef("NFTABLES: adding DNS-over-TCP redirect rules")

	if _, err := n.runNft("add", "table", "ip", nftDNSTCPTableName); err != nil {
		return fmt.Errorf("failed to create nftables dns nat table: %w", err)
	}

	mark := fmt.Sprintf("0x%x", n.cfg.MainInjectedMark())
	port := fmt.Sprintf("%d", config.DNSTCPPort)

	for chain, hook := range map[string]string{"dnstcp_pre": "prerouting", "dnstcp_out": "output"} {
		if _, err := n.runNft("add", "chain", "ip", nftDNSTCPTableName, chain,
			fmt.Sprintf("{ type nat hook %s priority dstnat ; policy accept ; }", hook)); err != nil {
			return fmt.Errorf("failed to create nat %s chain: %w", hook, err)
		}
		if _, err := n.runNft("flush", "chain", "ip", nftDNSTCPTableName, chain); err != nil {
			return fmt.Errorf("failed to flush nat %s chain: %w", hook, err)
		}
		if _, err := n.runNft("add", "rule", "ip", nftDNSTCPTableName, chain,
			"meta", "mark", "&", mark, "==", mark, "return"); err != nil {
			return fmt.Errorf("failed to add dns tcp mark-bypass rule: %w", err)
		}
		if _, err := n.runNft("add", "rule", "ip", nftDNSTCPTableName, chain,
			"tcp", "dport", "53", "redirect", "to", ":"+port); err != nil {
			return fmt.Errorf("failed to add dns tcp redirect rule: %w", err)
		}
	}

	log.Infof("NFTABLES: DNS over TCP redirected to b4 on port %s", port)
	return nil
}

func (n *NFTablesManager) ClearDNSTCP() {
	if !n.dnsTCPTableExists() {
		return
	}
	log.Tracef("NFTABLES: clearing DNS-over-TCP redirect rules")
	if _, err := n.runNft("flush", "table", "ip", nftDNSTCPTableName); err != nil {
		log.Errorf("Failed to flush nftables dns nat table: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := n.runNft("delete", "table", "ip", nftDNSTCPTableName); err != nil {
		log.Errorf("Failed to delete nftables dns nat table: %v", err)
	}
}
