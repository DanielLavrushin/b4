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
	nftTableName    = "b4_mangle"
	nftChainName    = "b4_chain"
	nftNatTableName = "b4_nat"
	nftNatChainName = "b4_masq"
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
	if err != nil {
		output := strings.TrimSpace(out.String())
		cmdStr := "nft " + strings.Join(args, " ")
		if output != "" {
			return output, fmt.Errorf("command [%s] failed: %w (%s)", cmdStr, err, output)
		}
		return output, fmt.Errorf("command [%s] failed: %w", cmdStr, err)
	}
	return out.String(), nil
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

func (n *NFTablesManager) createSet(name, addrType, extraFlags string) error {
	setDef := fmt.Sprintf("{ type %s ; %s }", addrType, extraFlags)
	_, err := n.runNft("add", "set", "inet", nftTableName, name, setDef)
	if err != nil {
		return fmt.Errorf("failed to create set %s: %w", name, err)
	}
	log.Tracef("Created nftables set: %s", name)
	return nil
}

func (n *NFTablesManager) addSetElements(name string, elements []string) error {
	const batchSize = 10000
	for i := 0; i < len(elements); i += batchSize {
		end := i + batchSize
		if end > len(elements) {
			end = len(elements)
		}
		chunk := elements[i:end]
		elemExpr := "{ " + strings.Join(chunk, ", ") + " }"
		_, err := n.runNft("add", "element", "inet", nftTableName, name, elemExpr)
		if err != nil {
			return fmt.Errorf("failed to add elements to set %s (batch %d-%d): %w", name, i, end, err)
		}
	}
	log.Tracef("Added %d elements to nftables set: %s", len(elements), name)
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

	markValue := cfg.Queue.Mark
	if markValue == 0 {
		markValue = 0x8000
	}
	markAccept := fmt.Sprintf("0x%x", markValue)

	selectedMACs := cfg.Queue.Devices.SelectedMACs()
	if cfg.Queue.Devices.Enabled && len(selectedMACs) > 0 {
		if err := n.createChain("forward", "forward", -150, "accept"); err != nil {
			return err
		}

		if cfg.Queue.Devices.WhiteIsBlack {
			for _, mac := range selectedMACs {
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
			for _, mac := range selectedMACs {
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
	if err := n.addRule("output", "meta", "mark", "&", markAccept, "==", markAccept, "accept"); err != nil {
		return err
	}
	if err := n.addRule("output", "jump", nftChainName); err != nil {
		return err
	}

	if err := n.addRule(nftChainName, "meta", "mark", "&", markAccept, "==", markAccept, "return"); err != nil {
		return err
	}

	// Collect TCP and UDP ports
	tcpPorts := cfg.CollectTCPPorts()
	var tcpPortExpr string
	if len(tcpPorts) == 1 {
		tcpPortExpr = tcpPorts[0]
	} else {
		tcpPortExpr = "{ " + strings.Join(tcpPorts, ", ") + " }"
	}

	// Duplication rules: queue ALL TCP packets on configured ports to specific IPs (no connbytes limit).
	// Must come before the generic connbytes-limited rules.
	dupIPv4, dupIPv6 := cfg.CollectDuplicateIPs()
	queueAction := strings.Fields(n.buildNFQueueAction())
	if len(dupIPv4) > 0 && cfg.Queue.IPv4Enabled {
		if err := n.createSet("b4_dup_v4", "ipv4_addr", "flags interval ;"); err != nil {
			return err
		}
		if err := n.addSetElements("b4_dup_v4", dupIPv4); err != nil {
			return err
		}
		args := append([]string{"meta", "nfproto", "ipv4", "ip", "daddr", "@b4_dup_v4", "tcp", "dport", tcpPortExpr, "counter"}, queueAction...)
		if err := n.addRule(nftChainName, args...); err != nil {
			return err
		}
	}
	if len(dupIPv6) > 0 && cfg.Queue.IPv6Enabled {
		if err := n.createSet("b4_dup_v6", "ipv6_addr", "flags interval ;"); err != nil {
			return err
		}
		if err := n.addSetElements("b4_dup_v6", dupIPv6); err != nil {
			return err
		}
		args := append([]string{"meta", "nfproto", "ipv6", "ip6", "daddr", "@b4_dup_v6", "tcp", "dport", tcpPortExpr, "counter"}, queueAction...)
		if err := n.addRule(nftChainName, args...); err != nil {
			return err
		}
	}

	tcpLimit := fmt.Sprintf("%d", cfg.Queue.TCPConnBytesLimit+1)
	udpLimit := fmt.Sprintf("%d", cfg.Queue.UDPConnBytesLimit+1)

	if err := n.addQueueRule(nftChainName, "tcp", "dport", tcpPortExpr, "ct", "original", "packets", "<", tcpLimit, "counter"); err != nil {
		return err
	}

	if err := n.addQueueRule(nftChainName, "udp", "dport", "53", "counter"); err != nil {
		return err
	}

	if err := n.addQueueRule("prerouting", "udp", "dport", "53", "counter"); err != nil {
		return err
	}

	if err := n.addQueueRule("prerouting", "udp", "sport", "53", "counter"); err != nil {
		return err
	}
	if err := n.addQueueRule("output", "udp", "sport", "53", "counter"); err != nil {
		return err
	}

	if err := n.addQueueRule("prerouting", "tcp", "sport", tcpPortExpr, "ct", "original", "packets", "<", tcpLimit, "counter"); err != nil {
		return err
	}

	if err := n.addQueueRule("prerouting", "tcp", "sport", tcpPortExpr, "tcp", "flags", "&", "(syn|ack)", "==", "(syn|ack)", "counter"); err != nil {
		return err
	}

	if err := n.addQueueRule("prerouting", "tcp", "sport", tcpPortExpr, "tcp", "flags", "&", "rst", "==", "rst", "counter"); err != nil {
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

	if err := n.ApplyMasquerade(); err != nil {
		return err
	}

	if err := n.ApplyMSSClamp(); err != nil {
		return err
	}

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

	n.ClearMasquerade()

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

func (n *NFTablesManager) natTableExists() bool {
	out, err := n.runNft("list", "tables")
	if err != nil {
		return false
	}
	return strings.Contains(out, nftNatTableName)
}

func (n *NFTablesManager) ApplyMasquerade() error {
	if !n.cfg.System.Tables.Masquerade {
		return nil
	}

	log.Tracef("NFTABLES: adding masquerade rules")

	if !n.natTableExists() {
		if _, err := n.runNft("add", "table", "ip", nftNatTableName); err != nil {
			return fmt.Errorf("failed to create nftables nat table: %w", err)
		}
	}

	chainCmd := []string{"add", "chain", "ip", nftNatTableName, nftNatChainName,
		"{ type nat hook postrouting priority srcnat ; policy accept ; }"}
	if _, err := n.runNft(chainCmd...); err != nil {
		return fmt.Errorf("failed to create nat postrouting chain: %w", err)
	}

	ruleArgs := []string{"add", "rule", "ip", nftNatTableName, nftNatChainName}
	if iface := n.cfg.System.Tables.MasqueradeInterface; iface != "" {
		ruleArgs = append(ruleArgs, "oifname", fmt.Sprintf("%q", iface))
	}
	ruleArgs = append(ruleArgs, "masquerade")

	if _, err := n.runNft(ruleArgs...); err != nil {
		return fmt.Errorf("failed to add masquerade rule: %w", err)
	}

	iface := n.cfg.System.Tables.MasqueradeInterface
	if iface == "" {
		iface = "all"
	}
	log.Infof("NFTABLES: masquerade enabled (interface: %s)", iface)
	return nil
}

func (n *NFTablesManager) ClearMasquerade() {
	if !n.natTableExists() {
		return
	}

	log.Tracef("NFTABLES: clearing masquerade rules")
	if _, err := n.runNft("flush", "table", "ip", nftNatTableName); err != nil {
		log.Errorf("Failed to flush nftables nat table: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := n.runNft("delete", "table", "ip", nftNatTableName); err != nil {
		log.Errorf("Failed to delete nftables nat table: %v", err)
	}
}

func (n *NFTablesManager) ApplyMSSClamp() error {
	cfg := n.cfg
	global, globalSize := cfg.HasGlobalMSSClamp()
	deviceClamps := cfg.CollectDeviceMSSClamps()
	setClamps := cfg.CollectSetMSSClamps()

	if !global && len(deviceClamps) == 0 && len(setClamps) == 0 {
		return nil
	}

	log.Infof("NFTABLES: adding MSS clamp rules")

	synMatch := []string{"tcp", "flags", "syn", "/", "syn,rst"}
	mssSet := func(size int) []string {
		return []string{"tcp", "option", "maxseg", "size", "set", fmt.Sprintf("%d", size)}
	}

	needsForward := global || len(deviceClamps) > 0 || len(setClamps) > 0
	if needsForward && !n.chainExists("forward") {
		if err := n.createChain("forward", "forward", -150, "accept"); err != nil {
			return fmt.Errorf("failed to create forward chain for MSS clamp: %w", err)
		}
	}

	// Global MSS clamp - applies to all TCP port 443 traffic
	if global {
		// Outgoing SYN (dport 443)
		args := append([]string{"tcp", "dport", "443"}, synMatch...)
		args = append(args, mssSet(globalSize)...)
		if err := n.addFilteredRule("output", args...); err != nil {
			return fmt.Errorf("failed to add global MSS clamp output rule: %w", err)
		}

		// Forward SYN (dport 443)
		args = append([]string{"tcp", "dport", "443"}, synMatch...)
		args = append(args, mssSet(globalSize)...)
		if err := n.addFilteredRule("forward", args...); err != nil {
			return fmt.Errorf("failed to add global MSS clamp forward rule: %w", err)
		}

		// Incoming SYN-ACK (sport 443)
		args = append([]string{"tcp", "sport", "443"}, synMatch...)
		args = append(args, mssSet(globalSize)...)
		if err := n.addFilteredRule("prerouting", args...); err != nil {
			return fmt.Errorf("failed to add global MSS clamp prerouting rule: %w", err)
		}

		log.Infof("NFTABLES: global MSS clamp enabled (size: %d)", globalSize)
	}

	// Per-device MSS clamp rules (FORWARD chain with MAC matching)
	if len(deviceClamps) > 0 {
		for size, macs := range deviceClamps {
			for _, mac := range macs {
				// Outgoing SYN from device (ether saddr MAC, dport 443)
				args := append([]string{"ether", "saddr", mac, "tcp", "dport", "443"}, synMatch...)
				args = append(args, mssSet(size)...)
				if err := n.addFilteredRule("forward", args...); err != nil {
					return fmt.Errorf("failed to add per-device MSS clamp forward outgoing rule for %s: %w", mac, err)
				}

				// Incoming SYN-ACK to device (ether daddr MAC, sport 443)
				args = append([]string{"ether", "daddr", mac, "tcp", "sport", "443"}, synMatch...)
				args = append(args, mssSet(size)...)
				if err := n.addFilteredRule("forward", args...); err != nil {
					return fmt.Errorf("failed to add per-device MSS clamp forward incoming rule for %s: %w", mac, err)
				}
			}
			log.Infof("NFTABLES: per-device MSS clamp for %d devices (size: %d)", len(macs), size)
		}
	}

	for _, e := range setClamps {
		setName4 := fmt.Sprintf("b4_mss_%d_v4", e.SetIdx)
		setName6 := fmt.Sprintf("b4_mss_%d_v6", e.SetIdx)
		hasV4 := len(e.IPv4) > 0 && cfg.Queue.IPv4Enabled
		hasV6 := len(e.IPv6) > 0 && cfg.Queue.IPv6Enabled

		if hasV4 {
			if err := n.createSet(setName4, "ipv4_addr", "flags interval ;"); err != nil {
				return fmt.Errorf("failed to create set MSS ipv4 set: %w", err)
			}
			if err := n.addSetElements(setName4, e.IPv4); err != nil {
				return fmt.Errorf("failed to populate set MSS ipv4 set: %w", err)
			}
		}
		if hasV6 {
			if err := n.createSet(setName6, "ipv6_addr", "flags interval ;"); err != nil {
				return fmt.Errorf("failed to create set MSS ipv6 set: %w", err)
			}
			if err := n.addSetElements(setName6, e.IPv6); err != nil {
				return fmt.Errorf("failed to populate set MSS ipv6 set: %w", err)
			}
		}

		emitOut := func(chain, family, addrFamily, setName, macSaddr string) error {
			args := []string{"meta", "nfproto", family}
			if macSaddr != "" {
				args = append(args, "ether", "saddr", macSaddr)
			}
			if setName != "" {
				args = append(args, addrFamily, "daddr", "@"+setName)
			}
			args = append(args, "tcp", "dport", "443")
			args = append(args, synMatch...)
			args = append(args, mssSet(e.Size)...)
			if err := n.addRule(chain, args...); err != nil {
				return fmt.Errorf("failed to add per-set MSS outgoing rule (chain=%s, set=%s): %w", chain, e.SetID, err)
			}
			return nil
		}
		emitIn := func(chain, family, addrFamily, setName, macDaddr string) error {
			args := []string{"meta", "nfproto", family}
			if macDaddr != "" {
				args = append(args, "ether", "daddr", macDaddr)
			}
			if setName != "" {
				args = append(args, addrFamily, "saddr", "@"+setName)
			}
			args = append(args, "tcp", "sport", "443")
			args = append(args, synMatch...)
			args = append(args, mssSet(e.Size)...)
			if err := n.addRule(chain, args...); err != nil {
				return fmt.Errorf("failed to add per-set MSS incoming rule (chain=%s, set=%s): %w", chain, e.SetID, err)
			}
			return nil
		}

		applyFamily := func(family, addrFamily, setName string, enabled bool) error {
			if !enabled && setName != "" {
				return nil
			}
			if len(e.MACs) > 0 {
				for _, mac := range e.MACs {
					if err := emitOut("forward", family, addrFamily, setName, mac); err != nil {
						return err
					}
					if err := emitIn("forward", family, addrFamily, setName, mac); err != nil {
						return err
					}
				}
			} else {
				if err := emitOut("output", family, addrFamily, setName, ""); err != nil {
					return err
				}
				if err := emitOut("forward", family, addrFamily, setName, ""); err != nil {
					return err
				}
				if err := emitIn("prerouting", family, addrFamily, setName, ""); err != nil {
					return err
				}
			}
			return nil
		}

		if hasV4 {
			if err := applyFamily("ipv4", "ip", setName4, true); err != nil {
				return err
			}
		}
		if hasV6 {
			if err := applyFamily("ipv6", "ip6", setName6, true); err != nil {
				return err
			}
		}
		if !hasV4 && !hasV6 && len(e.MACs) > 0 {
			if cfg.Queue.IPv4Enabled {
				if err := applyFamily("ipv4", "", "", false); err != nil {
					return err
				}
			}
			if cfg.Queue.IPv6Enabled {
				if err := applyFamily("ipv6", "", "", false); err != nil {
					return err
				}
			}
		}
		log.Infof("NFTABLES: per-set MSS clamp for set %q (size: %d, v4=%d v6=%d macs=%d)",
			e.SetID, e.Size, len(e.IPv4), len(e.IPv6), len(e.MACs))
	}

	return nil
}
