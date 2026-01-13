package tables

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	nftTableName = "b4_mangle"
	nftChainName = "b4_chain"
)

type NFTablesManager struct {
	cfg             *config.Config
	ipVersionFilter string
}

func NewNFTablesManager(cfg *config.Config) *NFTablesManager {
	return &NFTablesManager{cfg: cfg}
}

func (n *NFTablesManager) runNft(args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command("nft", args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func (n *NFTablesManager) tableExists() bool {
	out, err := n.runNft("list", "tables")
	if err != nil {
		return false
	}
	return strings.Contains(out, nftTableName)
}

func (n *NFTablesManager) chainExists(chain string) bool {
	_, err := n.runNft("list", "chain", "inet", nftTableName, chain)
	return err == nil
}

func (n *NFTablesManager) createTable() error {
	if n.tableExists() {
		return nil
	}
	_, err := n.runNft("add", "table", "inet", nftTableName)
	if err != nil {
		return fmt.Errorf("failed to create nftables table: %w", err)
	}
	log.Tracef("Created nftables table: %s", nftTableName)
	return nil
}

func (n *NFTablesManager) createChain(chain, hook string, priority int, policy string) error {
	if n.chainExists(chain) {
		return nil
	}

	var cmd []string
	if hook != "" {
		cmd = []string{"add", "chain", "inet", nftTableName, chain,
			fmt.Sprintf("{ type filter hook %s priority %d ; policy %s ; }", hook, priority, policy)}
	} else {
		cmd = []string{"add", "chain", "inet", nftTableName, chain}
	}

	_, err := n.runNft(cmd...)
	if err != nil {
		return fmt.Errorf("failed to create chain %s: %w", chain, err)
	}
	log.Tracef("Created nftables chain: %s", chain)
	return nil
}

func (n *NFTablesManager) buildNFQueueAction() string {
	if n.cfg.Queue.Threads > 1 {
		return fmt.Sprintf("queue num %d-%d bypass", n.cfg.Queue.StartNum, n.cfg.Queue.StartNum+n.cfg.Queue.Threads-1)
	}
	return fmt.Sprintf("queue num %d bypass", n.cfg.Queue.StartNum)
}

func (n *NFTablesManager) addRule(chain string, args ...string) error {
	cmd := append([]string{"add", "rule", "inet", nftTableName, chain}, args...)
	log.Tracef("NFTABLES: adding rule to %s: %v", chain, args)
	_, err := n.runNft(cmd...)
	if err != nil {
		return fmt.Errorf("failed to add rule to %s: %w", chain, err)
	}
	return nil
}

func (n *NFTablesManager) addFilteredRule(chain string, args ...string) error {
	if n.ipVersionFilter != "" {
		args = append(strings.Fields(n.ipVersionFilter), args...)
	}
	return n.addRule(chain, args...)
}

func (n *NFTablesManager) addQueueRule(chain string, args ...string) error {
	args = append(args, strings.Fields(n.buildNFQueueAction())...)
	return n.addFilteredRule(chain, args...)
}

func (n *NFTablesManager) Apply() error {
	cfg := n.cfg
	if !hasBinary("nft") {
		return fmt.Errorf("nft binary not found")
	}

	log.Tracef("NFTABLES: adding rules")
	loadKernelModules()

	// Clear existing table to prevent rule duplication
	if n.tableExists() {
		n.Clear()
	}

	// Set IP version filter
	switch {
	case cfg.Queue.IPv4Enabled && cfg.Queue.IPv6Enabled:
		n.ipVersionFilter = ""
	case cfg.Queue.IPv4Enabled:
		n.ipVersionFilter = "meta nfproto ipv4"
	case cfg.Queue.IPv6Enabled:
		n.ipVersionFilter = "meta nfproto ipv6"
	}

	if err := n.createTable(); err != nil {
		return err
	}

	if err := n.createChain(nftChainName, "", 0, ""); err != nil {
		return err
	}

	if err := n.createChain("prerouting", "prerouting", -150, "accept"); err != nil {
		return err
	}

	if err := n.createChain("output", "output", -150, "accept"); err != nil {
		return err
	}

	markAccept := fmt.Sprintf("0x%x", cfg.Queue.Mark)

	if cfg.Queue.Devices.Enabled && len(cfg.Queue.Devices.Mac) > 0 {
		if err := n.createChain("forward", "forward", -150, "accept"); err != nil {
			return err
		}

		if cfg.Queue.Devices.WhiteIsBlack {
			for _, mac := range cfg.Queue.Devices.Mac {
				if mac = strings.ToUpper(strings.TrimSpace(mac)); mac != "" {
					if err := n.addRule("forward", "ether", "saddr", mac, "return"); err != nil {
						return err
					}
				}
			}
			if err := n.addRule("forward", "jump", nftChainName); err != nil {
				return err
			}
		} else {
			for _, mac := range cfg.Queue.Devices.Mac {
				if mac = strings.ToUpper(strings.TrimSpace(mac)); mac != "" {
					if err := n.addRule("forward", "ether", "saddr", mac, "jump", nftChainName); err != nil {
						return err
					}
				}
			}
		}
	} else {

		if err := n.createChain("postrouting", "postrouting", -150, "accept"); err != nil {
			return err
		}
		if err := n.addRule("postrouting", "jump", nftChainName); err != nil {
			return err
		}
	}

	if err := n.addRule("output", "oifname", `"lo"`, "return"); err != nil {
		return err
	}
	if err := n.addRule("output", "meta", "mark", markAccept, "accept"); err != nil {
		return err
	}
	if err := n.addRule("output", "jump", nftChainName); err != nil {
		return err
	}

	if err := n.addRule(nftChainName, "meta", "mark", markAccept, "return"); err != nil {
		return err
	}

	tcpLimit := fmt.Sprintf("%d", cfg.MainSet.TCP.ConnBytesLimit+1)
	udpLimit := fmt.Sprintf("%d", cfg.MainSet.UDP.ConnBytesLimit+1)

	if err := n.addQueueRule(nftChainName, "tcp", "dport", "443", "ct", "original", "packets", "<", tcpLimit, "counter"); err != nil {
		return err
	}

	if err := n.addQueueRule(nftChainName, "udp", "dport", "53", "counter"); err != nil {
		return err
	}

	if err := n.addQueueRule("prerouting", "udp", "sport", "53", "counter"); err != nil {
		return err
	}

	if err := n.addQueueRule("prerouting", "tcp", "sport", "443", "ct", "original", "packets", "<", tcpLimit, "counter"); err != nil {
		return err
	}

	if err := n.addQueueRule("prerouting", "tcp", "sport", "443", "tcp", "flags", "&", "(syn|ack)", "==", "(syn|ack)", "counter"); err != nil {
		return err
	}

	udpPorts := cfg.CollectUDPPorts()
	var udpPortExpr string
	if len(udpPorts) == 1 {
		udpPortExpr = udpPorts[0]
	} else {
		udpPortExpr = "{ " + strings.Join(udpPorts, ", ") + " }"
	}
	if err := n.addQueueRule(nftChainName, "udp", "dport", udpPortExpr, "ct", "original", "packets", "<", udpLimit, "counter"); err != nil {
		return err
	}

	setSysctlOrProc("net.netfilter.nf_conntrack_checksum", "0")
	setSysctlOrProc("net.netfilter.nf_conntrack_tcp_be_liberal", "1")

	if log.Level(log.CurLevel.Load()) >= log.LevelTrace {
		out, _ := n.runNft("list", "table", "inet", nftTableName)
		log.Tracef("Current nftables rules:\n%s", out)
	}

	return nil
}

func (n *NFTablesManager) Clear() error {
	if !hasBinary("nft") {
		return nil
	}

	log.Tracef("NFTABLES: clearing rules")

	if n.tableExists() {
		if _, err := n.runNft("flush", "table", "inet", nftTableName); err != nil {
			log.Errorf("Failed to flush nftables table: %v", err)
		}
		time.Sleep(30 * time.Millisecond)
		if _, err := n.runNft("delete", "table", "inet", nftTableName); err != nil {
			log.Errorf("Failed to delete nftables table: %v", err)
		}
	}

	return nil
}
