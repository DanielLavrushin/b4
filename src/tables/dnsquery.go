package tables

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	iptRawChainName = "B4_RAW"

	nftRawPriority = -300
	nftRawPreChain = "raw_prerouting"
	nftRawOutChain = "raw_output"
)

var (
	dnsQueryPlacementMu sync.Mutex
	dnsQueryFromRaw     map[string]bool
	rawFallbackWarned   sync.Map
	rawQueueProven      sync.Map
)

func noteDNSQueryPlacement(fromRaw map[string]bool) {
	dnsQueryPlacementMu.Lock()
	dnsQueryFromRaw = fromRaw
	dnsQueryPlacementMu.Unlock()
}

func dnsQueryPlacementChanged(fromRaw map[string]bool) bool {
	dnsQueryPlacementMu.Lock()
	defer dnsQueryPlacementMu.Unlock()
	for bin, raw := range fromRaw {
		if was, ok := dnsQueryFromRaw[bin]; ok && was != raw {
			return true
		}
	}
	return false
}

func dnsQueriesQueuedFromRaw(ipt string) bool {
	dnsQueryPlacementMu.Lock()
	defer dnsQueryPlacementMu.Unlock()
	return dnsQueryFromRaw[ipt]
}

func DNSQueryPlacement() (applied bool, afterConntrack, missing, packages []string) {
	dnsQueryPlacementMu.Lock()
	placement := make(map[string]bool, len(dnsQueryFromRaw))
	for bin, raw := range dnsQueryFromRaw {
		placement[bin] = raw
	}
	dnsQueryPlacementMu.Unlock()
	if len(placement) == 0 {
		return false, nil, nil, nil
	}
	for bin, raw := range placement {
		if raw {
			continue
		}
		afterConntrack = append(afterConntrack, bin)
		if bin != backendNFTables {
			missing = append(missing, iptRawTableModule(bin))
		}
	}
	sort.Strings(afterConntrack)
	sort.Strings(missing)
	return true, afterConntrack, missing, kmodPkgsFor(missing)
}

func captureSkipsDNSQueries(cfg *config.Config, fromRaw bool) bool {
	return fromRaw && len(cfg.Queue.Interfaces) == 0
}

func iptDNSQueryCaptureReturn() []string {
	return []string{"-p", "udp", "--dport", "53", "-j", "RETURN"}
}

func iptQueueTokens(queueStart, threads int) []string {
	if threads > 1 {
		return []string{"balance", strconv.Itoa(queueStart) + ":" + strconv.Itoa(queueStart+threads-1)}
	}
	return []string{"num", strconv.Itoa(queueStart)}
}

func iptLineHasTokens(line string, want []string) bool {
	fields := strings.Fields(line)
	for i := 0; i+len(want) <= len(fields); i++ {
		match := true
		for j, w := range want {
			if fields[i+j] != w {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func iptRawTableModule(ipt string) string {
	if strings.HasPrefix(ipt, "ip6") {
		return "ip6table_raw"
	}
	return "iptable_raw"
}

func iptRawDNSJump() []string {
	return []string{"-p", "udp", "--dport", "53", "-j", iptRawChainName}
}

func (im *IPTablesManager) checkRawQueueSupport(ipt string) error {
	if err, ok := im.rawQueueSupport[ipt]; ok {
		return err
	}
	spec := []string{"-j", "NFQUEUE", "--queue-num", "0", "--queue-bypass"}
	supported, probeErr := im.probeModuleInTempChain(ipt, "raw", spec)
	if !supported {
		loadKernelModuleList(iptRawTableModule(ipt))
		supported, probeErr = im.probeModuleInTempChain(ipt, "raw", spec)
	}
	var err error
	switch {
	case supported:
		rawQueueProven.Store(ipt, true)
	case rawQueueProvenFor(ipt):
		log.Debugf("IPTABLES[%s]: the raw table probe failed (%v) after it had worked, keeping DNS queries in the raw table", ipt, probeErr)
	default:
		err = fmt.Errorf("%s cannot queue packets from the raw table (%v)", ipt, probeErr)
	}
	im.rawQueueSupport[ipt] = err
	return err
}

func rawQueueProvenFor(ipt string) bool {
	_, ok := rawQueueProven.Load(ipt)
	return ok
}

func (im *IPTablesManager) queueDNSQueriesFromRaw(ipt string) bool {
	err := im.checkRawQueueSupport(ipt)
	if err == nil {
		return true
	}
	if _, warned := rawFallbackWarned.LoadOrStore(ipt, true); warned {
		log.Debugf("IPTABLES[%s]: DNS queries stay in the mangle table: %v", ipt, err)
		return false
	}
	log.Warnf("IPTABLES[%s]: %v, so DNS queries are queued in the mangle table, after conntrack has seen them. Kernels before 4.18 (4.14 before 4.14.173), such as the 4.9 of Keenetic routers, then drop the second of two queries a device sends at once from one socket (an A and an AAAA lookup). Load the %s kernel module (OpenWrt: opkg install %s) to queue them from the raw table", ipt, err, iptRawTableModule(ipt), kmodRawPackage(ipt))
	return false
}

func kmodRawPackage(ipt string) string {
	if pkgs := kmodPackageTable[iptRawTableModule(ipt)].kernel; len(pkgs) > 0 {
		return pkgs[0]
	}
	return "kmod-ipt-raw"
}

func iptRawDNSQueueRules(manager *IPTablesManager, ipt, markAccept string, dnsSpec []string) []Rule {
	return []Rule{
		{manager: manager, IPT: ipt, Table: "raw", Chain: iptRawChainName, Action: "A",
			Spec: []string{"-m", "mark", "--mark", markAccept, "-j", "RETURN"}},
		{manager: manager, IPT: ipt, Table: "raw", Chain: iptRawChainName, Action: "A", Spec: dnsSpec},
		{manager: manager, IPT: ipt, Table: "raw", Chain: "PREROUTING", Action: "A", Spec: iptRawDNSJump()},
		{manager: manager, IPT: ipt, Table: "raw", Chain: "OUTPUT", Action: "A", Spec: iptRawDNSJump()},
	}
}

func dnsAnswerMatch(fromRaw bool) []string {
	if fromRaw {
		return []string{"-p", "udp", "--sport", "53", "!", "--dport", "53"}
	}
	return []string{"-p", "udp", "--sport", "53"}
}

func iptIsDNSQueryQueueLine(line string) bool {
	if iptListLineTarget(line) != "NFQUEUE" {
		return false
	}
	for _, f := range strings.Fields(line) {
		if f == "dpt:53" {
			return true
		}
	}
	return false
}

func iptIsRawDNSJumpLine(line string) bool {
	return iptListLineTarget(line) == iptRawChainName
}

func iptRemoveRawDNSJumps(ipt string) {
	for _, chain := range []string{"PREROUTING", "OUTPUT"} {
		iptDeleteListedLines(ipt, "raw", chain, iptIsRawDNSJumpLine)
	}
}

func (im *IPTablesManager) dropUnusedDNSQueryPlacement(fromRaw map[string]bool) {
	queue := iptQueueTokens(im.cfg.Queue.StartNum, im.cfg.Queue.Threads)
	ownQueueLine := func(line string) bool {
		return iptIsDNSQueryQueueLine(line) && iptLineHasTokens(line, queue)
	}
	for ipt, raw := range fromRaw {
		if raw {
			iptDeleteListedLines(ipt, "mangle", "B4_PREROUTING", iptIsDNSQueryQueueLine)
			iptDeleteListedLines(ipt, "mangle", "OUTPUT", ownQueueLine)
			continue
		}
		iptRemoveRawDNSJumps(ipt)
		Chain{manager: im, IPT: ipt, Table: "raw", Name: iptRawChainName}.Remove()
	}
}

func iptRawDNSQueuePresent(ipt string) bool {
	out, err := run(ipt, "-w", "-t", "raw", "-S", iptRawChainName)
	if err != nil || !strings.Contains(out, dnsRequestPortMatch) || !strings.Contains(out, "NFQUEUE") {
		return false
	}
	for _, chain := range []string{"PREROUTING", "OUTPUT"} {
		if _, err := run(append([]string{ipt, "-w", "-t", "raw", "-C", chain}, iptRawDNSJump()...)...); err != nil {
			return false
		}
	}
	return true
}

func (n *NFTablesManager) addDNSQueryQueueChains(markAccept string) error {
	if err := n.createChain(nftRawPreChain, "prerouting", nftRawPriority, "accept"); err != nil {
		return err
	}
	if err := n.createChain(nftRawOutChain, "output", nftRawPriority, "accept"); err != nil {
		return err
	}
	if err := n.addRule(nftRawOutChain, "oifname", `"lo"`, "return"); err != nil {
		return err
	}
	for _, chain := range []string{nftRawPreChain, nftRawOutChain} {
		if err := n.addRule(chain, "meta", "mark", "&", markAccept, "==", markAccept, "return"); err != nil {
			return err
		}
		if err := n.addQueueRule(chain, "udp", "dport", "53", "counter"); err != nil {
			return err
		}
	}
	return nil
}

func (n *NFTablesManager) addDNSQueryRules(markAccept string) (bool, error) {
	err := n.addDNSQueryQueueChains(markAccept)
	if err == nil {
		return true, nil
	}
	n.dropDNSQueryQueueChains()
	log.Warnf("nftables: DNS queries cannot be queued ahead of conntrack (%v), so they are queued in prerouting and output, where a second query sent at the same moment from the same socket can clash with the first in conntrack", err)
	for _, chain := range []string{"prerouting", "output"} {
		if err := n.addQueueRule(chain, "udp", "dport", "53", "counter"); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (n *NFTablesManager) dropDNSQueryQueueChains() {
	for _, chain := range []string{nftRawPreChain, nftRawOutChain} {
		_, _ = n.runNft("flush", "chain", "inet", nftTableName, chain)
		_, _ = n.runNft("delete", "chain", "inet", nftTableName, chain)
	}
}

func (n *NFTablesManager) dnsQueryQueueChainsPresent() bool {
	for _, chain := range []string{nftRawPreChain, nftRawOutChain} {
		out, err := n.runNft("list", "chain", "inet", nftTableName, chain)
		if err != nil || !strings.Contains(out, dnsRequestPortMatch) || !strings.Contains(out, "queue") {
			return false
		}
	}
	return true
}
