package tables

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type IPTablesManager struct {
	cfg              *config.Config
	multiportSupport map[string]bool // per-binary cache (iptables vs ip6tables may differ)
}

func NewIPTablesManager(cfg *config.Config) *IPTablesManager {
	return &IPTablesManager{cfg: cfg, multiportSupport: make(map[string]bool)}
}

// hasMultiportSupport checks if iptables multiport module is available
func (im *IPTablesManager) hasMultiportSupport(ipt string) bool {
	if result, ok := im.multiportSupport[ipt]; ok {
		return result
	}

	// Try to add and immediately remove a test rule using multiport
	testSpec := []string{"-p", "tcp", "-m", "multiport", "--dports", "80,443", "-j", "ACCEPT"}
	_, err := run(append([]string{ipt, "-w", "-t", "filter", "-C", "INPUT"}, testSpec...)...)
	if err == nil {
		// Rule exists (unlikely), multiport works
		im.multiportSupport[ipt] = true
		return true
	}

	// Try to add the rule
	_, err = run(append([]string{ipt, "-w", "-t", "filter", "-A", "INPUT"}, testSpec...)...)
	if err == nil {
		// Success - remove it immediately
		_, _ = run(append([]string{ipt, "-w", "-t", "filter", "-D", "INPUT"}, testSpec...)...)
		im.multiportSupport[ipt] = true
		log.Tracef("IPTABLES[%s]: multiport module is available", ipt)
		return true
	}

	log.Warnf("IPTABLES[%s]: multiport module not available, using individual port rules", ipt)
	im.multiportSupport[ipt] = false
	return false
}

func (im *IPTablesManager) existsChain(ipt, table, chain string) bool {
	_, err := run(ipt, "-w", "-t", table, "-S", chain)
	return err == nil
}

func (im *IPTablesManager) ensureChain(ipt, table, chain string) {
	if !im.existsChain(ipt, table, chain) {
		_, _ = run(ipt, "-w", "-t", table, "-N", chain)
	}
}

func (im *IPTablesManager) existsRule(ipt, table, chain string, spec []string) bool {
	_, err := run(append([]string{ipt, "-w", "-t", table, "-C", chain}, spec...)...)
	return err == nil
}

func (im *IPTablesManager) delAll(ipt, table, chain string, spec []string) {
	for {
		_, err := run(append([]string{ipt, "-w", "-t", table, "-D", chain}, spec...)...)
		if err != nil {
			break
		}
	}
}

type Rule struct {
	manager *IPTablesManager
	IPT     string
	Table   string
	Chain   string
	Spec    []string
	Action  string
}

func (r Rule) Apply() error {
	if r.manager.existsRule(r.IPT, r.Table, r.Chain, r.Spec) {
		return nil
	}
	op := "-A"
	if strings.ToUpper(r.Action) == "I" {
		op = "-I"
	}
	_, err := run(append([]string{r.IPT, "-w", "-t", r.Table, op, r.Chain}, r.Spec...)...)
	return err
}

func (r Rule) Remove() {
	r.manager.delAll(r.IPT, r.Table, r.Chain, r.Spec)
}

type Chain struct {
	manager *IPTablesManager
	IPT     string
	Table   string
	Name    string
}

func (c Chain) Ensure() {
	c.manager.ensureChain(c.IPT, c.Table, c.Name)
}

func (c Chain) Remove() {
	if c.manager.existsChain(c.IPT, c.Table, c.Name) {
		_, _ = run(c.IPT, "-w", "-t", c.Table, "-F", c.Name)
		_, _ = run(c.IPT, "-w", "-t", c.Table, "-X", c.Name)
	}
}

type SysctlSetting struct {
	Name    string
	Desired string
	Revert  string
}

var sysctlSnapPath = "/tmp/b4_sysctl_snapshot.json"

func loadSysctlSnapshot() map[string]string {
	b, err := os.ReadFile(sysctlSnapPath)
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return map[string]string{}
	}
	return m
}

func saveSysctlSnapshot(m map[string]string) {
	b, _ := json.Marshal(m)
	_ = os.WriteFile(sysctlSnapPath, b, 0600)
}

func (ipt *IPTablesManager) buildNFQSpec(queueStart, threads int) []string {
	if threads > 1 {
		start := strconv.Itoa(queueStart)
		end := strconv.Itoa(queueStart + threads - 1)
		return []string{"-j", "NFQUEUE", "--queue-balance", start + ":" + end, "--queue-bypass"}
	}
	return []string{"-j", "NFQUEUE", "--queue-num", strconv.Itoa(queueStart), "--queue-bypass"}
}

func (s SysctlSetting) Apply() {
	snap := loadSysctlSnapshot()
	if _, ok := snap[s.Name]; !ok {
		snap[s.Name] = getSysctlOrProc(s.Name)
		saveSysctlSnapshot(snap)
	}
	setSysctlOrProc(s.Name, s.Desired)
}

func (s SysctlSetting) RevertBack() {
	snap := loadSysctlSnapshot()
	if v, ok := snap[s.Name]; ok && v != "" {
		setSysctlOrProc(s.Name, v)
		delete(snap, s.Name)
		saveSysctlSnapshot(snap)
		return
	}
	setSysctlOrProc(s.Name, s.Revert)
}

type Manifest struct {
	Chains  []Chain
	Rules   []Rule
	Sysctls []SysctlSetting
}

func (m Manifest) Apply() error {
	for _, c := range m.Chains {
		c.Ensure()
	}
	for _, r := range m.Rules {
		if err := r.Apply(); err != nil {
			return err
		}
	}
	for _, s := range m.Sysctls {
		s.Apply()
	}
	return nil
}

func (m Manifest) RemoveRules() {
	for i := len(m.Rules) - 1; i >= 0; i-- {
		m.Rules[i].Remove()
	}
}

func (m Manifest) RemoveChains() {
	for i := len(m.Chains) - 1; i >= 0; i-- {
		m.Chains[i].Remove()
	}
}

func (m Manifest) RevertSysctls() {
	for _, s := range m.Sysctls {
		s.RevertBack()
	}
}

func (manager *IPTablesManager) buildManifest() (Manifest, error) {
	cfg := manager.cfg
	var ipts []string
	if cfg.Queue.IPv4Enabled && hasBinary("iptables") {
		ipts = append(ipts, "iptables")
	}
	if cfg.Queue.IPv6Enabled && hasBinary("ip6tables") {
		ipts = append(ipts, "ip6tables")
	}
	if len(ipts) == 0 {
		return Manifest{}, errors.New("no valid iptables binaries found")
	}
	queueNum := cfg.Queue.StartNum
	threads := cfg.Queue.Threads
	chainName := "B4"
	markAccept := fmt.Sprintf("0x%x/0x%x", cfg.Queue.Mark, cfg.Queue.Mark)
	if cfg.Queue.Mark == 0 {
		markAccept = "0x8000/0x8000"
	}

	var chains []Chain
	var rules []Rule

	for _, ipt := range ipts {
		ch := Chain{manager: manager, IPT: ipt, Table: "mangle", Name: chainName}
		chains = append(chains, ch)

		tcpConnbytesRange := fmt.Sprintf("0:%d", cfg.MainSet.TCP.ConnBytesLimit)
		udpConnbytesRange := fmt.Sprintf("0:%d", cfg.MainSet.UDP.ConnBytesLimit)

		tcpSpec := append(
			[]string{"-p", "tcp", "--dport", "443",
				"-m", "connbytes", "--connbytes-dir", "original",
				"--connbytes-mode", "packets", "--connbytes", tcpConnbytesRange},
			manager.buildNFQSpec(queueNum, threads)...,
		)

		dnsSpec := append(
			[]string{"-p", "udp", "--dport", "53"},
			manager.buildNFQSpec(queueNum, threads)...,
		)

		dnsResponseSpec := append(
			[]string{"-p", "udp", "--sport", "53"},
			manager.buildNFQSpec(queueNum, threads)...,
		)

		tcpResponseSpec := append(
			[]string{"-p", "tcp", "--sport", "443",
				"-m", "connbytes", "--connbytes-dir", "reply",
				"--connbytes-mode", "packets", "--connbytes", tcpConnbytesRange},
			manager.buildNFQSpec(queueNum, threads)...,
		)

		synackSpec := append(
			[]string{"-p", "tcp", "--sport", "443", "--tcp-flags", "SYN,ACK", "SYN,ACK"},
			manager.buildNFQSpec(queueNum, threads)...,
		)

		rules = append(rules,
			Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: "PREROUTING", Action: "I", Spec: dnsResponseSpec},
			Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: "PREROUTING", Action: "I", Spec: tcpResponseSpec},
			Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: "PREROUTING", Action: "I", Spec: synackSpec},

			Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: chainName, Action: "A", Spec: tcpSpec},
			Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: chainName, Action: "A", Spec: dnsSpec},
		)

		udpPorts := cfg.CollectUDPPorts()
		for i, p := range udpPorts {
			udpPorts[i] = strings.ReplaceAll(p, "-", ":")
		}

		if manager.hasMultiportSupport(ipt) {
			// Use multiport for efficiency (batches up to 15 ports per rule)
			udpPortChunks := chunkPorts(udpPorts, 15)
			for _, chunk := range udpPortChunks {
				udpPortSpec := []string{"-p", "udp", "-m", "multiport", "--dports", strings.Join(chunk, ",")}
				udpSpec := append(
					append(udpPortSpec,
						"-m", "connbytes", "--connbytes-dir", "original",
						"--connbytes-mode", "packets", "--connbytes", udpConnbytesRange),
					manager.buildNFQSpec(queueNum, threads)...,
				)
				rules = append(rules, Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: chainName, Action: "A", Spec: udpSpec})
			}
		} else {
			// Fallback: create individual rules for each port/range
			for _, port := range udpPorts {
				udpPortSpec := []string{"-p", "udp", "--dport", port}
				udpSpec := append(
					append(udpPortSpec,
						"-m", "connbytes", "--connbytes-dir", "original",
						"--connbytes-mode", "packets", "--connbytes", udpConnbytesRange),
					manager.buildNFQSpec(queueNum, threads)...,
				)
				rules = append(rules, Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: chainName, Action: "A", Spec: udpSpec})
			}
		}

		if cfg.Queue.Devices.Enabled && len(cfg.Queue.Devices.Mac) > 0 {
			if cfg.Queue.Devices.WhiteIsBlack {
				rules = append(rules,
					Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: "FORWARD", Action: "I",
						Spec: []string{"-j", chainName}},
				)
				for _, mac := range cfg.Queue.Devices.Mac {
					mac = strings.ToUpper(strings.TrimSpace(mac))
					if mac == "" {
						continue
					}
					rules = append(rules,
						Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: "FORWARD", Action: "I",
							Spec: []string{"-m", "mac", "--mac-source", mac, "-j", "RETURN"}},
					)
				}
			} else {
				for _, mac := range cfg.Queue.Devices.Mac {
					mac = strings.ToUpper(strings.TrimSpace(mac))
					if mac == "" {
						continue
					}
					rules = append(rules,
						Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: "FORWARD", Action: "I",
							Spec: []string{"-m", "mac", "--mac-source", mac, "-j", chainName}},
					)
				}
			}
		} else {
			rules = append(rules,
				Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: "POSTROUTING", Action: "I",
					Spec: []string{"-j", chainName}},
			)
		}

		rules = append(rules,
			Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: "PREROUTING", Action: "I", Spec: dnsResponseSpec},
			Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: "OUTPUT", Action: "I",
				Spec: []string{"-m", "mark", "--mark", markAccept, "-j", "ACCEPT"}},
			Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: "OUTPUT", Action: "A",
				Spec: []string{"-j", chainName}},
		)
	}

	sysctls := []SysctlSetting{
		{Name: "net.netfilter.nf_conntrack_checksum", Desired: "0", Revert: "1"},
		{Name: "net.netfilter.nf_conntrack_tcp_be_liberal", Desired: "1", Revert: "0"},
	}

	return Manifest{Chains: chains, Rules: rules, Sysctls: sysctls}, nil
}

func (ipt *IPTablesManager) Apply() error {
	log.Infof("IPTABLES: adding rules")
	loadKernelModules()
	m, err := ipt.buildManifest()
	if err != nil {
		return err
	}
	result := m.Apply()

	if log.Level(log.CurLevel.Load()) >= log.LevelTrace {
		iptables_trace, _ := run("sh", "-c", "cat /proc/net/netfilter/nfnetlink_queue && iptables -t mangle -vnL --line-numbers")
		log.Tracef("Current iptables mangle table:\n%s", iptables_trace)
	}
	return result
}

func (ipt *IPTablesManager) Clear() error {
	m, err := ipt.buildManifest()
	if err != nil {
		return err
	}

	ipt.clearB4JumpRules()

	m.RemoveRules()
	time.Sleep(30 * time.Millisecond)
	m.RemoveChains()
	return nil
}

func (ipt *IPTablesManager) clearB4JumpRules() {
	ipts := []string{}
	if ipt.cfg.Queue.IPv4Enabled && hasBinary("iptables") {
		ipts = append(ipts, "iptables")
	}
	if ipt.cfg.Queue.IPv6Enabled && hasBinary("ip6tables") {
		ipts = append(ipts, "ip6tables")
	}

	for _, iptBin := range ipts {
		// Clean POSTROUTING
		for {
			_, err := run(iptBin, "-w", "-t", "mangle", "-D", "POSTROUTING", "-j", "B4")
			if err != nil {
				break
			}
		}

		// Clean FORWARD
		for {
			out, _ := run(iptBin, "-w", "-t", "mangle", "-S", "FORWARD")
			if !strings.Contains(out, "B4") && !strings.Contains(out, "--mac-source") {
				break
			}

			_, err1 := run(iptBin, "-w", "-t", "mangle", "-D", "FORWARD", "-j", "B4")

			lines := strings.Split(out, "\n")
			removedAny := false
			for _, line := range lines {
				if strings.Contains(line, "--mac-source") {
					parts := strings.Fields(line)
					for i, p := range parts {
						if p == "--mac-source" && i+1 < len(parts) {
							mac := strings.ToUpper(parts[i+1])
							if _, err := run(iptBin, "-w", "-t", "mangle", "-D", "FORWARD",
								"-m", "mac", "--mac-source", mac, "-j", "RETURN"); err == nil {
								removedAny = true
							}
							if _, err := run(iptBin, "-w", "-t", "mangle", "-D", "FORWARD",
								"-m", "mac", "--mac-source", mac, "-j", "B4"); err == nil {
								removedAny = true
							}
							break
						}
					}
				}
			}

			if err1 != nil && !removedAny {
				break
			}
		}

		// Clean PREROUTING - parse and remove any NFQUEUE rules for DNS
		for {
			out, _ := run(iptBin, "-w", "-t", "mangle", "--line-numbers", "-nL", "PREROUTING")
			lines := strings.Split(out, "\n")
			removed := false
			for _, line := range lines {
				if strings.Contains(line, "spt:53") && strings.Contains(line, "NFQUEUE") {
					parts := strings.Fields(line)
					if len(parts) > 0 {
						lineNum := parts[0]
						if _, err := run(iptBin, "-w", "-t", "mangle", "-D", "PREROUTING", lineNum); err == nil {
							removed = true
							break // Re-parse after deletion since line numbers shift
						}
					}
				}
			}
			if !removed {
				break
			}
		}

		// Clean OUTPUT - parse and remove any B4 mark rules
		for {
			out, _ := run(iptBin, "-w", "-t", "mangle", "-S", "OUTPUT")
			if !strings.Contains(out, "ACCEPT") || !strings.Contains(out, "mark") {
				break
			}

			removed := false
			lines := strings.Split(out, "\n")
			for _, line := range lines {
				if strings.Contains(line, "-j ACCEPT") && strings.Contains(line, "--mark") {
					parts := strings.Fields(line)
					for i, p := range parts {
						if p == "--mark" && i+1 < len(parts) {
							mark := parts[i+1]
							_, err := run(iptBin, "-w", "-t", "mangle", "-D", "OUTPUT",
								"-m", "mark", "--mark", mark, "-j", "ACCEPT")
							if err == nil {
								removed = true
							}
							break
						}
					}
					if removed {
						break
					}
				}
			}
			if !removed {
				break
			}
		}

		for {
			_, err := run(iptBin, "-w", "-t", "mangle", "-D", "OUTPUT", "-j", "B4")
			if err != nil {
				break
			}
		}
	}
}

func chunkPorts(ports []string, maxSize int) [][]string {
	if len(ports) <= maxSize {
		return [][]string{ports}
	}
	var chunks [][]string
	for i := 0; i < len(ports); i += maxSize {
		end := i + maxSize
		if end > len(ports) {
			end = len(ports)
		}
		chunks = append(chunks, ports[i:end])
	}
	return chunks
}
