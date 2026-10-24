package tables

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
)

const (
	nftTableName    = "b4_mangle"
	nftChainName    = "b4_chain"
	nftNatTableName = "b4_nat"
	nftNatChainName = "b4_masq"

	nftBaseChainPriority = -150

	nftIntervalSetFlags = "flags interval ; auto-merge ;"
)

type NFTablesManager struct {
	cfg                  *config.Config
	ipVersionFilter      string
	ctPacketsSupported   bool
	ctDirectionSupported bool
	socketKey            string
	sysctlsApplied       bool
	dropSetV4            bool
	dropSetV6            bool
}

func NewNFTablesManager(cfg *config.Config) *NFTablesManager {
	return &NFTablesManager{cfg: cfg}
}

func (n *NFTablesManager) runNft(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), iptCommandTimeout)
	defer cancel()

	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "nft", args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return out.String(), fmt.Errorf("command [nft %s] gave up after %v: %w", strings.Join(args, " "), iptCommandTimeout, ctx.Err())
	}
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

func (n *NFTablesManager) runNftScript(script string) (string, error) {
	return runNftStdin(script)
}

var runNftStdin = runNftStdinExec

func runNftStdinExec(script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), iptCommandTimeout)
	defer cancel()

	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return out.String(), fmt.Errorf("command [nft -f -] gave up after %v: %w", iptCommandTimeout, ctx.Err())
	}
	if err != nil {
		output := strings.TrimSpace(out.String())
		if output != "" {
			return output, fmt.Errorf("command [nft -f -] failed: %w (%s)", err, output)
		}
		return output, fmt.Errorf("command [nft -f -] failed: %w", err)
	}
	return out.String(), nil
}

func (n *NFTablesManager) addSetElements(name string, elements []string) error {
	elements = expandZeroPrefix(elements)
	if len(elements) == 0 {
		return nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "add element inet %s %s { ", nftTableName, name)
	sb.WriteString(strings.Join(elements, ", "))
	sb.WriteString(" }\n")
	if _, err := n.runNftScript(sb.String()); err != nil {
		return fmt.Errorf("failed to add %d elements to set %s: %w", len(elements), name, err)
	}
	log.Tracef("Added %d elements to nftables set: %s", len(elements), name)
	return nil
}

func (n *NFTablesManager) buildNFQueueAction() string {
	return nfqueueActionExpr(n.cfg.Queue.StartNum, n.cfg.Queue.Threads)
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

func queueMatchWithLimit(match []string, dir, limit string, ctPackets, ctDirection bool) []string {
	if ctPackets {
		match = slices.Concat(match, []string{"ct", "original", "packets", "<", limit})
	}
	return queueMatchInDirection(match, dir, ctDirection)
}

func queueMatchInDirection(match []string, dir string, ctDirection bool) []string {
	args := slices.Clip(match)
	if ctDirection {
		args = append(args, "ct", "direction", dir)
	}
	return append(args, "counter")
}

func (n *NFTablesManager) addLimitedQueueRule(chain, dir, limit string, ctDirection bool, match ...string) error {
	return n.addQueueRule(chain, queueMatchWithLimit(match, dir, limit, n.ctPacketsSupported, ctDirection)...)
}

func nftInjectedReplyReturn(mark uint) []string {
	return []string{"ct", "mark", "&", fmt.Sprintf("0x%x", engine.InjectedMarkBit|mark), "==", fmt.Sprintf("0x%x", engine.InjectedMarkBit),
		"tcp", "flags", "&", "(syn|rst)", "==", "0", "return"}
}

func nftOwnReplyReturn(mark string, ctDirection bool, socketKey string) []string {
	rule := []string{"ct", "mark", "&", mark, "==", mark}
	if ctDirection {
		rule = append(rule, "ct", "direction", "reply")
	}
	if socketKey != "" {
		rule = append(rule, "socket", socketKey, "0")
	}
	return append(rule, "return")
}

func (n *NFTablesManager) checkQueueSupport() error {
	caps := probeQueueNftCached(n.buildNFQueueAction())
	if !caps.probed {
		n.ctPacketsSupported = true
		log.Tracef("NFTABLES: queue capability probe inconclusive, assuming queue and conntrack counter support")
		return nil
	}

	if !caps.queue {
		return fmt.Errorf("nftables rejected the packet-queue rule [%s], so b4 cannot intercept packets: %v - the usual cause is a missing nft_queue kernel module: load it with 'modprobe nft_queue' (OpenWrt: opkg install kmod-nft-queue, or apk add kmod-nft-queue; Debian/Ubuntu: apt install linux-modules-extra-$(uname -r)), or switch b4 to TUN mode (queue.mode = \"tun\"), which does not need NFQUEUE", n.buildNFQueueAction(), caps.queueErr)
	}

	n.ctPacketsSupported = caps.ctPackets
	if !caps.ctPackets {
		log.Warnf("nftables: conntrack packet counters (ct original packets) are unavailable, so every packet on the configured ports is queued instead of only the first few - install kmod-nft-core (kmod-nft-conntrack) to restore the per-connection limit")
	}
	n.ctDirectionSupported = caps.ctDirection
	if !caps.ctDirection {
		log.Warnf("nftables: the conntrack direction match (ct direction) is unavailable, so the queue rules select TCP packets by port alone and b4 tells a client's packets from a server's replies by their ports - install kmod-nft-core (kmod-nft-conntrack) to have them selected by conntrack direction")
	}
	n.socketKey = caps.socketKey
	if caps.socketKey == "" && n.cfg.Queue.Mark != 0 {
		warnOwnReplySocketMissing("NFTABLES", "the socket expression", caps.socketErr, kmodMissingHint([]string{"nft_socket"}))
	}
	return nil
}

func (n *NFTablesManager) Apply() error {
	if err := n.apply(); err != nil {
		n.clear(n.sysctlsApplied)
		noteQueueRulesCleared()
		return err
	}
	noteQueueDirectionExact(n.ctDirectionSupported, n.ctDirectionSupported)
	noteUDPDropSets(n.dropSetV4, n.dropSetV6)
	return nil
}

func (n *NFTablesManager) apply() error {
	cfg := n.cfg
	withdrawUDPDropSets()
	if !hasBinary("nft") {
		return fmt.Errorf("nft binary not found")
	}

	log.Tracef("NFTABLES: adding rules")
	loadKernelModules()

	if err := n.checkQueueSupport(); err != nil {
		return err
	}
	narrowQueueDirectionExact(n.ctDirectionSupported, n.ctDirectionSupported)

	// Clear existing table to prevent rule duplication
	if n.tableExists() {
		n.clear(true)
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

	if err := n.createChain("prerouting", "prerouting", nftBaseChainPriority, "accept"); err != nil {
		return err
	}

	if err := n.createChain("output", "output", nftBaseChainPriority, "accept"); err != nil {
		return err
	}

	if err := n.ApplyMSSClamp(); err != nil {
		return err
	}

	markValue := cfg.Queue.Mark
	if markValue == 0 {
		markValue = 0x8000
	}
	markAccept := fmt.Sprintf("0x%x", markValue)

	selectedMACs := cfg.Queue.Devices.SelectedMACs()
	if queueJumpsFromForward(cfg) {
		if err := n.createChain("forward", "forward", nftBaseChainPriority, "accept"); err != nil {
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

		if err := n.createChain("postrouting", "postrouting", nftBaseChainPriority, "accept"); err != nil {
			return err
		}
		if err := n.addRule("postrouting", "jump", nftChainName); err != nil {
			return err
		}
	}

	if err := n.addRule("output", "oifname", `"lo"`, "return"); err != nil {
		return err
	}
	if cfg.Queue.Mark != 0 {
		injected := fmt.Sprintf("0x%x", engine.InjectedMarkBit)
		markMask := fmt.Sprintf("0x%x", markValue|engine.ClientMark|engine.InjectedMarkBit)
		if err := n.addRule("output", "meta", "mark", "&", markMask, "==", markAccept, "ct", "mark", "set", "ct", "mark", "|", markAccept); err != nil {
			log.Warnf("nftables: connmark save unavailable, so the replies to b4's own lookups and probes reach its queue rules like those of a device: %v", err)
		}
		if err := n.addRule("output", "meta", "mark", "&", markMask, "==", fmt.Sprintf("0x%x", markValue|engine.InjectedMarkBit), "ct", "mark", "set", "ct", "mark", "|", injected); err != nil {
			log.Warnf("nftables: the connections b4 sends fakes or split segments into cannot be marked, so the server's packets on them stay queued: %v", err)
		}
	}
	if err := n.addRule("output", "meta", "mark", "&", markAccept, "==", markAccept, "accept"); err != nil {
		return err
	}
	if queueJumpsFromForward(cfg) {
		if err := n.addRule("output", "jump", nftChainName); err != nil {
			return err
		}
	}

	ownMask, ownValue := nftOwnPacketMark(cfg.Queue.Mark)
	if err := n.addRule(nftChainName, "meta", "mark", "&", ownMask, "==", ownValue, "return"); err != nil {
		return err
	}

	if err := n.addRule(nftChainName, "meta", "mark", "&", fmt.Sprintf("0x%x", config.PerSetRouteMarkBits), "==", fmt.Sprintf("0x%x", config.TelegramBridgeMark), "return"); err != nil {
		return err
	}

	noDPIVal := fmt.Sprintf("0x%x", config.SelfDialRelayMark)
	noDPIMask := fmt.Sprintf("0x%x", config.SelfDialSkipMatchMask)
	if err := n.addRule(nftChainName, "meta", "mark", "&", noDPIMask, "==", noDPIVal, "return"); err != nil {
		return err
	}

	if cfg.Queue.Mark != 0 {
		if err := n.addRule("prerouting", nftOwnReplyReturn(markAccept, n.ctDirectionSupported, n.socketKey)...); err != nil {
			log.Warnf("nftables: the return for the replies to b4's own lookups and probes is unavailable, so they reach its queue rules like those of a device: %v", err)
		}
		if !cfg.AnySetTCPIncoming() {
			if err := n.addRule("prerouting", nftInjectedReplyReturn(cfg.Queue.Mark)...); err != nil {
				log.Warnf("nftables: the server's data packets on connections b4 sends fakes or split segments into cannot be kept out of the queue, so b4 is handed them too: %v", err)
			}
		}
	}

	// Collect TCP and UDP ports
	tcpPorts := cfg.CollectTCPPorts()
	var tcpPortExpr string
	if len(tcpPorts) == 1 {
		tcpPortExpr = tcpPorts[0]
	} else {
		tcpPortExpr = "{ " + strings.Join(tcpPorts, ", ") + " }"
	}

	if err := n.addDuplicateQueueRules(tcpPortExpr); err != nil {
		return err
	}

	tcpLimit := fmt.Sprintf("%d", cfg.Queue.TCPConnBytesLimit+1)
	udpLimit := fmt.Sprintf("%d", cfg.Queue.UDPConnBytesLimit+1)

	if err := n.addLimitedQueueRule(nftChainName, "original", tcpLimit, n.ctDirectionSupported, "tcp", "dport", tcpPortExpr); err != nil {
		return err
	}

	queriesFromRaw, err := n.addDNSQueryRules(ownMask, ownValue)
	if err != nil {
		return err
	}
	placement := dnsHeldMangle
	if queriesFromRaw {
		placement = dnsHeldRaw
	}
	noteDNSQueryPlacement(map[string]dnsQueryPlacement{backendNFTables: placement})

	if err := n.addDNSAnswerRules(queriesFromRaw); err != nil {
		return err
	}

	if err := n.addLimitedQueueRule("prerouting", "reply", tcpLimit, n.ctDirectionSupported, "tcp", "sport", tcpPortExpr); err != nil {
		return err
	}

	if err := n.addQueueRule("prerouting", queueMatchInDirection([]string{"tcp", "sport", tcpPortExpr, "tcp", "flags", "&", "(syn|ack)", "==", "(syn|ack)"}, "reply", n.ctDirectionSupported)...); err != nil {
		return err
	}

	if err := n.addQueueRule("prerouting", queueMatchInDirection([]string{"tcp", "sport", tcpPortExpr, "tcp", "flags", "&", "rst", "==", "rst"}, "reply", n.ctDirectionSupported)...); err != nil {
		return err
	}

	udpPorts := cfg.CollectUDPPorts()
	var udpPortExpr string
	if len(udpPorts) == 1 {
		udpPortExpr = udpPorts[0]
	} else {
		udpPortExpr = "{ " + strings.Join(udpPorts, ", ") + " }"
	}
	if queriesFromRaw {
		if err := n.addRule(nftChainName, "udp", "dport", "53", "return"); err != nil {
			return err
		}
	}
	n.dropSetV4, n.dropSetV6 = n.addUDPDropRules()
	if err := n.addLimitedQueueRule(nftChainName, "original", udpLimit, false, "udp", "dport", udpPortExpr); err != nil {
		return err
	}

	for _, s := range b4SysctlSettings() {
		s.Apply()
	}
	n.sysctlsApplied = true

	if err := n.ApplyMasquerade(); err != nil {
		return err
	}

	if err := n.ApplyDNSTCP(); err != nil {
		return err
	}

	if log.Level(log.CurLevel.Load()) >= log.LevelTrace {
		out, _ := n.runNft("list", "table", "inet", nftTableName)
		log.Tracef("Current nftables rules:\n%s", out)
	}

	return nil
}

func (n *NFTablesManager) addDuplicateQueueRules(tcpPortExpr string) error {
	cfg := n.cfg
	dupIPv4, dupIPv6 := cfg.CollectDuplicateIPs()
	queueAction := strings.Fields(n.buildNFQueueAction())
	if len(dupIPv4) > 0 && cfg.Queue.IPv4Enabled {
		if err := n.createSet("b4_dup_v4", "ipv4_addr", nftIntervalSetFlags); err != nil {
			return err
		}
		if err := n.addSetElements("b4_dup_v4", dupIPv4); err != nil {
			return err
		}
		args := append(queueMatchInDirection([]string{"meta", "nfproto", "ipv4", "ip", "daddr", "@b4_dup_v4", "tcp", "dport", tcpPortExpr}, "original", n.ctDirectionSupported), queueAction...)
		if err := n.addRule(nftChainName, args...); err != nil {
			return err
		}
	}
	if len(dupIPv6) > 0 && cfg.Queue.IPv6Enabled {
		if err := n.createSet("b4_dup_v6", "ipv6_addr", nftIntervalSetFlags); err != nil {
			return err
		}
		if err := n.addSetElements("b4_dup_v6", dupIPv6); err != nil {
			return err
		}
		args := append(queueMatchInDirection([]string{"meta", "nfproto", "ipv6", "ip6", "daddr", "@b4_dup_v6", "tcp", "dport", tcpPortExpr}, "original", n.ctDirectionSupported), queueAction...)
		if err := n.addRule(nftChainName, args...); err != nil {
			return err
		}
	}
	return nil
}

func (n *NFTablesManager) Clear() error {
	noteQueueRulesCleared()
	return n.clear(true)
}

func (n *NFTablesManager) clear(revertSysctls bool) error {
	withdrawUDPDropSets()
	if !hasBinary("nft") {
		return nil
	}

	log.Tracef("NFTABLES: clearing rules")
	noteDNSQueryPlacement(nil)

	n.ClearMasquerade()
	n.ClearDNSTCP()

	if n.tableExists() {
		if _, err := n.runNft("flush", "table", "inet", nftTableName); err != nil {
			log.Errorf("Failed to flush nftables table: %v", err)
		}
		time.Sleep(30 * time.Millisecond)
		if _, err := n.runNft("delete", "table", "inet", nftTableName); err != nil {
			log.Errorf("Failed to delete nftables table: %v", err)
		}
	}

	if revertSysctls {
		for _, s := range b4SysctlSettings() {
			s.RevertBack()
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
	if !n.cfg.System.Tables.Masquerade.Enabled {
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

	if _, err := n.runNft("flush", "chain", "ip", nftNatTableName, nftNatChainName); err != nil {
		return fmt.Errorf("failed to flush nat postrouting chain: %w", err)
	}

	markClient := fmt.Sprintf("0x%x", engine.ClientMark)
	if _, err := n.runNft("add", "rule", "ip", nftNatTableName, nftNatChainName,
		"meta", "mark", "&", markClient, "==", markClient, "return"); err != nil {
		return fmt.Errorf("failed to add masquerade mark-bypass rule: %w", err)
	}

	if dev := activeTUNDevice(); dev != "" {
		if _, err := n.runNft("add", "rule", "ip", nftNatTableName, nftNatChainName,
			"oifname", fmt.Sprintf("%q", dev), "return"); err != nil {
			return fmt.Errorf("failed to exempt the TUN device from masquerade: %w", err)
		}
	}

	ifaces := masqueradeInterfaces(n.cfg)
	if len(ifaces) == 0 {
		if _, err := n.runNft("add", "rule", "ip", nftNatTableName, nftNatChainName, "masquerade"); err != nil {
			return fmt.Errorf("failed to add masquerade rule: %w", err)
		}
		log.Infof("NFTABLES: masquerade enabled (interfaces: all)")
		return nil
	}

	for _, iface := range ifaces {
		if _, err := n.runNft("add", "rule", "ip", nftNatTableName, nftNatChainName,
			"oifname", fmt.Sprintf("%q", iface), "masquerade"); err != nil {
			return fmt.Errorf("failed to add masquerade rule for %s: %w", iface, err)
		}
	}
	log.Infof("NFTABLES: masquerade enabled (interfaces: %s)", strings.Join(ifaces, ", "))
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

func (n *NFTablesManager) ClearMSSClamp() {
	if !hasBinary("nft") {
		return
	}
	if n.tableExists() {
		if _, err := n.runNft("delete", "table", "inet", nftTableName); err != nil {
			log.Tracef("NFTABLES: MSS clamp clear (delete table %s): %v", nftTableName, err)
		}
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
		if err := n.createChain("forward", "forward", nftBaseChainPriority, "accept"); err != nil {
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
		for size, matches := range deviceClamps {
			emitted := 0
			for _, m := range matches {
				if !deviceMatchFamilyEnabled(m, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled) {
					continue
				}
				args := append(nftSourceMatchArgs(m), "tcp", "dport", "443")
				args = append(args, synMatch...)
				args = append(args, mssSet(size)...)
				if err := n.addFilteredRule("forward", args...); err != nil {
					return fmt.Errorf("failed to add per-device MSS clamp forward outgoing rule for %s: %w", m.Key(), err)
				}

				args = append(nftReplyMatchArgs(m), "tcp", "sport", "443")
				args = append(args, synMatch...)
				args = append(args, mssSet(size)...)
				if err := n.addFilteredRule("forward", args...); err != nil {
					return fmt.Errorf("failed to add per-device MSS clamp forward incoming rule for %s: %w", m.Key(), err)
				}
				emitted++
			}
			if emitted > 0 {
				log.Infof("NFTABLES: per-device MSS clamp for %d devices (size: %d)", emitted, size)
			}
		}
	}

	for _, e := range setClamps {
		setName4 := fmt.Sprintf("b4_mss_%d_v4", e.SetIdx)
		setName6 := fmt.Sprintf("b4_mss_%d_v6", e.SetIdx)
		hasV4 := len(e.IPv4) > 0 && cfg.Queue.IPv4Enabled
		hasV6 := len(e.IPv6) > 0 && cfg.Queue.IPv6Enabled
		emitV4 := hasV4 && setHasSourceForFamily(e.Sources, false)
		emitV6 := hasV6 && setHasSourceForFamily(e.Sources, true)

		if emitV4 {
			if err := n.createSet(setName4, "ipv4_addr", nftIntervalSetFlags); err != nil {
				return fmt.Errorf("failed to create set MSS ipv4 set: %w", err)
			}
			if err := n.addSetElements(setName4, e.IPv4); err != nil {
				return fmt.Errorf("failed to populate set MSS ipv4 set: %w", err)
			}
		}
		if emitV6 {
			if err := n.createSet(setName6, "ipv6_addr", nftIntervalSetFlags); err != nil {
				return fmt.Errorf("failed to create set MSS ipv6 set: %w", err)
			}
			if err := n.addSetElements(setName6, e.IPv6); err != nil {
				return fmt.Errorf("failed to populate set MSS ipv6 set: %w", err)
			}
		}

		emitOut := func(chain, family, addrFamily, setName string, src *config.DeviceMatch) error {
			args := []string{"meta", "nfproto", family}
			if src != nil {
				args = append(args, nftSourceMatchArgs(*src)...)
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
		emitIn := func(chain, family, addrFamily, setName string, src *config.DeviceMatch) error {
			args := []string{"meta", "nfproto", family}
			if src != nil {
				args = append(args, nftReplyMatchArgs(*src)...)
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
			if len(e.Sources) > 0 {
				for i := range e.Sources {
					src := e.Sources[i]
					if src.IsIP() && ((src.V6 && family != "ipv6") || (!src.V6 && family != "ipv4")) {
						continue
					}
					if err := emitOut("forward", family, addrFamily, setName, &src); err != nil {
						return err
					}
					if err := emitIn("forward", family, addrFamily, setName, &src); err != nil {
						return err
					}
				}
			} else {
				if err := emitOut("output", family, addrFamily, setName, nil); err != nil {
					return err
				}
				if err := emitOut("forward", family, addrFamily, setName, nil); err != nil {
					return err
				}
				if err := emitIn("prerouting", family, addrFamily, setName, nil); err != nil {
					return err
				}
			}
			return nil
		}

		if emitV4 {
			if err := applyFamily("ipv4", "ip", setName4, true); err != nil {
				return err
			}
		}
		if emitV6 {
			if err := applyFamily("ipv6", "ip6", setName6, true); err != nil {
				return err
			}
		}
		if mssClampUnresolved(cfg, e) {
			log.Infof("NFTABLES: per-set MSS clamp for set %q skipped: its IP targets resolve to no addresses yet", e.SetID)
			continue
		}
		if mssClampMACOnly(cfg, e) {
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
		log.Infof("NFTABLES: per-set MSS clamp for set %q (size: %d, v4=%d v6=%d sources=%d)",
			e.SetID, e.Size, len(e.IPv4), len(e.IPv6), len(e.Sources))
	}

	return nil
}
