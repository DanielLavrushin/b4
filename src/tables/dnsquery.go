package tables

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/utils"
)

const (
	iptRawChainName     = "B4_RAW"
	iptDNSCopyChainName = "B4_DNSCOPY"

	nftRawPriority = -300
	nftRawPreChain = "raw_prerouting"
	nftRawOutChain = "raw_output"

	dnsCopyModule    = "xt_NFLOG"
	dnsCopyLogModule = "nfnetlink_log"
)

type dnsQueryPlacement uint8

const (
	dnsHeldMangle dnsQueryPlacement = iota
	dnsHeldRaw
	dnsCopied
)

func (p dnsQueryPlacement) String() string {
	switch p {
	case dnsHeldRaw:
		return "raw"
	case dnsCopied:
		return "copy"
	}
	return "mangle"
}

var (
	DNSQueryCopyOpenFunc   func() (uint16, error)
	DNSQueryCopyAliveFunc  func() bool
	DNSQueryCopyPlacedFunc func(v4, v6 bool)
)

var (
	dnsQueryPlacementMu sync.Mutex
	dnsQueryPlacements  map[string]dnsQueryPlacement
	dnsCopyGroup        atomic.Uint32
	rawFallbackWarned   sync.Map
	copyFallbackWarned  sync.Map
	dnsCopyNoted        sync.Map
	dnsNatPortWarned    sync.Map
	rawQueueProven      sync.Map
	rawQueueUnusable    sync.Map
	dnsCopyUnusable     sync.Map
	dnsCopyErrors       sync.Map
)

func noteDNSQueryPlacement(placements map[string]dnsQueryPlacement) {
	var v4, v6 bool
	for bin, placement := range placements {
		if placement != dnsCopied {
			continue
		}
		if strings.HasPrefix(bin, "ip6") {
			v6 = true
		} else {
			v4 = true
		}
	}
	dnsQueryPlacementMu.Lock()
	dnsQueryPlacements = placements
	dnsQueryPlacementMu.Unlock()
	if DNSQueryCopyPlacedFunc != nil {
		DNSQueryCopyPlacedFunc(v4, v6)
	}
}

func (p dnsQueryPlacement) describe() string {
	switch p {
	case dnsHeldRaw:
		return "held in the raw table"
	case dnsCopied:
		return "copied with NFLOG"
	}
	return "held in the mangle table"
}

func dnsQueryPlacementMoves(placements map[string]dnsQueryPlacement) []string {
	dnsQueryPlacementMu.Lock()
	defer dnsQueryPlacementMu.Unlock()
	var moves []string
	for bin, placement := range placements {
		if was, ok := dnsQueryPlacements[bin]; ok && was != placement {
			moves = append(moves, fmt.Sprintf("IPTABLES[%s]: DNS queries were %s and will be %s, rebuilding the rules", bin, was.describe(), placement.describe()))
		}
	}
	sort.Strings(moves)
	return moves
}

func dnsQueryPlacementOf(ipt string) dnsQueryPlacement {
	dnsQueryPlacementMu.Lock()
	defer dnsQueryPlacementMu.Unlock()
	return dnsQueryPlacements[ipt]
}

type DNSQueryState struct {
	Applied      bool
	Release      string
	KeeneticOS   bool
	Affected     bool
	Group        int
	Placements   map[string]string
	RawMissing   []string
	RawPackages  []string
	CopyMissing  []string
	CopyPackages []string
	CopyError    string
}

func DNSQueryPlacement() DNSQueryState {
	dnsQueryPlacementMu.Lock()
	placements := make(map[string]dnsQueryPlacement, len(dnsQueryPlacements))
	for bin, placement := range dnsQueryPlacements {
		placements[bin] = placement
	}
	dnsQueryPlacementMu.Unlock()
	if len(placements) == 0 {
		return DNSQueryState{}
	}
	facts := dnsKernelFacts()
	_, nft := placements[backendNFTables]
	st := DNSQueryState{
		Applied:    true,
		Release:    facts.Release,
		KeeneticOS: facts.KeeneticOS && !nft,
		Affected:   facts.TwinDropped && !nft,
		Group:      int(dnsCopyGroup.Load()),
		Placements: make(map[string]string, len(placements)),
	}
	held := false
	var copyErrs []string
	for bin, placement := range placements {
		st.Placements[bin] = placement.String()
		if placement != dnsHeldMangle || bin == backendNFTables {
			continue
		}
		held = true
		if !st.KeeneticOS {
			st.RawMissing = append(st.RawMissing, iptRawTableModule(bin))
		}
		if v, ok := dnsCopyErrors.Load(bin); ok {
			copyErrs = append(copyErrs, v.(string))
		}
	}
	sort.Strings(st.RawMissing)
	st.RawPackages = kmodPkgsFor(st.RawMissing)
	if st.Affected && held {
		st.CopyMissing = dnsCopyMissingModules()
		st.CopyPackages = kmodPkgsFor(st.CopyMissing)
		sort.Strings(copyErrs)
		st.CopyError = strings.Join(utils.FilterUniqueStrings(copyErrs), "; ")
	}
	return st
}

func dnsCopyMissingModules() []string {
	var missing []string
	for _, module := range []string{dnsCopyLogModule, dnsCopyModule} {
		kmodStateMu.Lock()
		rejected := kmodRejected[kmodCanonical(module)] != ""
		kmodStateMu.Unlock()
		if rejected || kmodStateOf(module) != kmodStatePresent {
			missing = append(missing, module)
		}
	}
	return missing
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

func iptListingJumpsTo(listing, chain string) bool {
	for _, line := range strings.Split(listing, "\n") {
		if iptLineHasTokens(line, []string{"-j", chain}) {
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

func cachedProbeFailure(cache *sync.Map, ipt, module string) error {
	v, ok := cache.Load(ipt)
	if !ok {
		return nil
	}
	if kmodLoaded(module) {
		cache.Delete(ipt)
		return nil
	}
	err, _ := v.(error)
	return err
}

func cacheProbeFailure(cache *sync.Map, ipt, module string, err error) {
	if err != nil && !kmodLoaded(module) {
		cache.Store(ipt, err)
	}
}

func (im *IPTablesManager) checkRawQueueSupport(ipt string) error {
	if err, ok := im.rawQueueSupport[ipt]; ok {
		return err
	}
	module := iptRawTableModule(ipt)
	if !rawQueueProvenFor(ipt) {
		if err := cachedProbeFailure(&rawQueueUnusable, ipt, module); err != nil {
			im.rawQueueSupport[ipt] = err
			return err
		}
	}
	spec := []string{"-j", "NFQUEUE", "--queue-num", "0", "--queue-bypass"}
	supported, probeErr := im.probeModuleInTempChain(ipt, "raw", spec)
	if !supported {
		loadKernelModuleList(module)
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
		cacheProbeFailure(&rawQueueUnusable, ipt, module, err)
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
	log.Warnf("IPTABLES[%s]: %v, so DNS queries are queued in the mangle table, after conntrack has seen them. Kernels before 4.18 (4.14 before 4.14.173) then drop the second of two queries a device sends at once from one socket (an A and an AAAA lookup). Load the %s kernel module (OpenWrt: opkg install %s) to queue them from the raw table", ipt, err, iptRawTableModule(ipt), kmodRawPackage(ipt))
	return false
}

func (im *IPTablesManager) checkDNSCopySupport(ipt string) error {
	if err, ok := im.dnsCopySupport[ipt]; ok {
		return err
	}
	err := cachedProbeFailure(&dnsCopyUnusable, ipt, dnsCopyModule)
	if err == nil {
		err = im.probeDNSCopy(ipt)
	}
	if err != nil {
		dnsCopyErrors.Store(ipt, err.Error())
	} else {
		dnsCopyErrors.Delete(ipt)
	}
	im.dnsCopySupport[ipt] = err
	return err
}

func (im *IPTablesManager) probeDNSCopy(ipt string) error {
	if DNSQueryCopyOpenFunc == nil {
		return errors.New("b4 has no DNS query copy listener")
	}
	loadKernelModuleList(dnsCopyLogModule, dnsCopyModule)
	group, err := DNSQueryCopyOpenFunc()
	if err != nil {
		return fmt.Errorf("b4 could not bind its NFLOG listener: %w", err)
	}
	dnsCopyGroup.Store(uint32(group))
	supported, probeErr := im.probeModuleInTempChain(ipt, "mangle", iptDNSCopySpec(group))
	if supported {
		return nil
	}
	kmodNoteRejected(dnsCopyModule, ipt, fmt.Sprint(probeErr))
	err = fmt.Errorf("%s rejected the NFLOG target (%v)", ipt, probeErr)
	cacheProbeFailure(&dnsCopyUnusable, ipt, dnsCopyModule, err)
	return err
}

func (im *IPTablesManager) dnsQueryPlacementFor(ipt string) dnsQueryPlacement {
	facts := dnsKernelFacts()
	switch {
	case !facts.TwinDropped && facts.KeeneticOS:
		return dnsHeldMangle
	case !facts.TwinDropped:
		if im.queueDNSQueriesFromRaw(ipt) {
			return dnsHeldRaw
		}
		return dnsHeldMangle
	case facts.KeeneticOS:
		copyErr := im.checkDNSCopySupport(ipt)
		if copyErr == nil {
			noteDNSQueriesCopied(ipt, "IPTABLES[%s]: KeeneticOS runs its raw table after conntrack (kernel %s), so b4 copies DNS queries with NFLOG (group %d) instead of holding them in its queue", ipt, facts.Release, dnsCopyGroup.Load())
			return dnsCopied
		}
		warnDNSQueriesHeld(ipt, copyErr, "IPTABLES[%s]: b4 cannot copy DNS queries with NFLOG (%v), so it holds them in its queue after conntrack, and on this kernel (%s) a device that asks for a name's IPv4 and IPv6 addresses at once from one socket often loses the second answer. NFLOG comes with the \"Kernel modules for Netfilter\" component of KeeneticOS", ipt, copyErr, facts.Release)
		return dnsHeldMangle
	}
	rawErr := im.checkRawQueueSupport(ipt)
	if rawErr == nil {
		return dnsHeldRaw
	}
	copyErr := im.checkDNSCopySupport(ipt)
	if copyErr == nil {
		noteDNSQueriesCopied(ipt, "IPTABLES[%s]: %v, and kernel %s drops the second of two DNS queries a device sends at once from one socket when they wait in a queue after conntrack, so b4 copies DNS queries with NFLOG (group %d) instead of holding them", ipt, rawErr, facts.Release, dnsCopyGroup.Load())
		return dnsCopied
	}
	warnDNSQueriesHeld(ipt, copyErr, "IPTABLES[%s]: %v, and b4 cannot copy DNS queries with NFLOG either (%v), so it holds them in its queue after conntrack, where this kernel (%s) drops the second of two queries a device sends at once from one socket (an A and an AAAA lookup). Load the %s kernel module (OpenWrt: opkg install %s) to queue them from the raw table, or xt_NFLOG and nfnetlink_log (OpenWrt: opkg install kmod-ipt-nflog iptables-mod-nflog) to copy them", ipt, rawErr, copyErr, facts.Release, iptRawTableModule(ipt), kmodRawPackage(ipt))
	return dnsHeldMangle
}

func noteDNSQueriesCopied(ipt, format string, args ...any) {
	if _, noted := dnsCopyNoted.LoadOrStore(ipt, true); noted {
		return
	}
	log.Infof(format, args...)
}

func warnDNSQueriesHeld(ipt string, copyErr error, format string, args ...any) {
	if _, warned := copyFallbackWarned.LoadOrStore(ipt, true); warned {
		log.Debugf("IPTABLES[%s]: DNS queries stay in the mangle table: %v", ipt, copyErr)
		return
	}
	log.Warnf(format, args...)
}

func iptDNSCopySpec(group uint16) []string {
	return []string{"-p", "udp", "--dport", "53", "-j", "NFLOG",
		"--nflog-group", strconv.Itoa(int(group)), "--nflog-prefix", config.DNSQueryCopyPrefix, "--nflog-threshold", "1"}
}

func iptDNSCopyJumps(manager *IPTablesManager, ipt string) []Rule {
	jump := func(chain string, match ...string) Rule {
		return Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: chain, Action: "I",
			Spec: append(match, "-p", "udp", "--dport", "53", "-j", iptDNSCopyChainName)}
	}
	return []Rule{
		jump("INPUT"),
		jump("FORWARD"),
		jump("OUTPUT", "!", "-o", "lo"),
	}
}

func iptMarkReturn(markAccept string) []string {
	return []string{"-m", "mark", "--mark", markAccept, "-j", "RETURN"}
}

func iptDNSCopyRules(manager *IPTablesManager, ipt, markAccept string, group uint16) []Rule {
	rules := []Rule{
		{manager: manager, IPT: ipt, Table: "mangle", Chain: iptDNSCopyChainName, Action: "I", Spec: iptMarkReturn(markAccept)},
		{manager: manager, IPT: ipt, Table: "mangle", Chain: iptDNSCopyChainName, Action: "A", Spec: iptDNSCopySpec(group)},
	}
	return append(rules, iptDNSCopyJumps(manager, ipt)...)
}

func iptIsDNSCopyReturnLine(line string) bool {
	return iptLineHasTokens(line, []string{"-A", iptDNSCopyChainName}) &&
		iptLineHasTokens(line, []string{"-m", "mark", "--mark"}) &&
		iptLineHasTokens(line, []string{"-j", "RETURN"})
}

func iptIsDNSCopyLine(line string, group uint16) bool {
	for _, want := range [][]string{
		{"-A", iptDNSCopyChainName},
		{"-p", "udp"},
		{"--dport", "53"},
		{"-j", "NFLOG"},
		{"--nflog-prefix", config.DNSQueryCopyPrefix},
	} {
		if !iptLineHasTokens(line, want) {
			return false
		}
	}
	if group == 0 {
		return !slices.Contains(strings.Fields(line), "--nflog-group")
	}
	return iptLineHasTokens(line, []string{"--nflog-group", strconv.Itoa(int(group))})
}

func iptDNSCopyPresent(ipt string, group uint16) bool {
	out, err := run(ipt, "-w", "-t", "mangle", "-S", iptDNSCopyChainName)
	if err != nil {
		return false
	}
	var returns, copies bool
	for _, line := range strings.Split(out, "\n") {
		returns = returns || iptIsDNSCopyReturnLine(line)
		copies = copies || iptIsDNSCopyLine(line, group)
	}
	if !returns || !copies {
		return false
	}
	for _, r := range iptDNSCopyJumps(nil, ipt) {
		if _, err := run(append([]string{ipt, "-w", "-t", "mangle", "-C", r.Chain}, r.Spec...)...); err != nil {
			return false
		}
	}
	return true
}

func iptDNATPort(dest string) string {
	if strings.HasPrefix(dest, "[") {
		if _, port, ok := strings.Cut(dest, "]:"); ok {
			return port
		}
		return ""
	}
	if strings.Count(dest, ":") != 1 {
		return ""
	}
	_, port, _ := strings.Cut(dest, ":")
	return port
}

func iptDNSNatPort(line string) (string, bool) {
	fields := strings.Fields(line)
	var udp, dns bool
	target, port := "", ""
	for i := 0; i+1 < len(fields); i++ {
		negated := i > 0 && fields[i-1] == "!"
		switch fields[i] {
		case "-p":
			udp = fields[i+1] == "udp" && !negated
		case "--dport":
			dns = fields[i+1] == "53" && !negated
		case "-j":
			target = fields[i+1]
		case "--to-ports":
			if target == "REDIRECT" {
				port = fields[i+1]
			}
		case "--to-destination":
			if target == "DNAT" {
				port = iptDNATPort(fields[i+1])
			}
		}
	}
	return port, udp && dns && port != "" && port != "53"
}

func iptNATTableListed(ipt string) bool {
	path := "/proc/net/ip_tables_names"
	if strings.HasPrefix(ipt, "ip6") {
		path = "/proc/net/ip6_tables_names"
	}
	raw, err := readProcNetFile(path)
	return err == nil && slices.Contains(strings.Fields(string(raw)), "nat")
}

func warnPortChangingDNSNat(placements map[string]dnsQueryPlacement) {
	keenetic := dnsKernelFacts().KeeneticOS
	for ipt, placement := range placements {
		if placement != dnsCopied || (keenetic && !iptNATTableListed(ipt)) {
			continue
		}
		out, err := run(ipt, "-w", "-t", "nat", "-S")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			port, moved := iptDNSNatPort(line)
			if !moved {
				continue
			}
			if _, warned := dnsNatPortWarned.LoadOrStore(ipt+"\x00"+line, true); warned {
				continue
			}
			log.Warnf("IPTABLES[%s]: the nat rule \"%s\" moves DNS queries to port %s; b4 copies DNS queries on this router and cannot rewrite answers that come back from another port, so the sets do not answer, block or redirect the names those devices ask for", ipt, line, port)
		}
	}
}

func kmodRawPackage(ipt string) string {
	if pkgs := kmodPackageTable[iptRawTableModule(ipt)].kernel; len(pkgs) > 0 {
		return pkgs[0]
	}
	return "kmod-ipt-raw"
}

func iptRawDNSQueueRules(manager *IPTablesManager, ipt, markAccept string, dnsSpec []string) []Rule {
	return []Rule{
		{manager: manager, IPT: ipt, Table: "raw", Chain: iptRawChainName, Action: "I", Spec: iptMarkReturn(markAccept)},
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

func (im *IPTablesManager) dropUnusedDNSQueryPlacement(placements map[string]dnsQueryPlacement) {
	queue := iptQueueTokens(im.cfg.Queue.StartNum, im.cfg.Queue.Threads)
	ownQueueLine := func(line string) bool {
		return iptIsDNSQueryQueueLine(line) && iptLineHasTokens(line, queue)
	}
	for ipt, placement := range placements {
		if placement != dnsHeldMangle {
			iptDeleteListedLines(ipt, "mangle", "B4_PREROUTING", iptIsDNSQueryQueueLine)
			iptDeleteListedLines(ipt, "mangle", "OUTPUT", ownQueueLine)
		}
		if placement != dnsHeldRaw && RawTablePresent(ipt) {
			iptRemoveRawDNSJumps(ipt)
			Chain{manager: im, IPT: ipt, Table: "raw", Name: iptRawChainName}.Remove()
		}
		if placement != dnsCopied && DNSQueryCopyKernel() {
			for _, r := range iptDNSCopyJumps(im, ipt) {
				r.Remove()
			}
			Chain{manager: im, IPT: ipt, Table: "mangle", Name: iptDNSCopyChainName}.Remove()
		}
	}
}

func iptRawDNSQueuePresent(ipt, markAccept string) bool {
	out, err := run(ipt, "-w", "-t", "raw", "-S", iptRawChainName)
	if err != nil || !strings.Contains(out, dnsRequestPortMatch) || !strings.Contains(out, "NFQUEUE") {
		return false
	}
	if _, err := run(append([]string{ipt, "-w", "-t", "raw", "-C", iptRawChainName}, iptMarkReturn(markAccept)...)...); err != nil {
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
