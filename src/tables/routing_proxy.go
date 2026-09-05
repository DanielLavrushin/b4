package tables

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/tproxy"
)

var nftMarkRuleRe = regexp.MustCompile(`meta mark & 0x([0-9a-fA-F]+) == 0x([0-9a-fA-F]+) (accept|return)`)

func nftParseMarkRule(line string) (uint32, string, bool) {
	m := nftMarkRuleRe.FindStringSubmatch(line)
	if m == nil {
		return 0, "", false
	}
	a, errA := strconv.ParseUint(m[1], 16, 32)
	b, errB := strconv.ParseUint(m[2], 16, 32)
	if errA != nil || errB != nil || a != b {
		return 0, "", false
	}
	return uint32(a), m[3], true
}

func nftHandleFromLine(line string) string {
	idx := strings.LastIndex(line, "# handle ")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(line[idx+len("# handle "):])
}

const proxyRulePriority = 3

const proxyLocalDeliveryTable = 252

var (
	proxyNftPreflightOnce sync.Once
	proxyIptPreflightOnce [2]sync.Once // [0]=iptables, [1]=iptables-legacy
	tproxyProbeMu         sync.Mutex
)

func tproxyMissingNft() (missing []string, probed bool) {
	loadKernelModuleList("nft_tproxy", "nft_socket")

	tproxyProbeMu.Lock()
	defer tproxyProbeMu.Unlock()

	const probeTable = "_b4_proxy_probe"
	_, _ = run("nft", "delete", "table", "inet", probeTable)
	if _, err := run("nft", "add", "table", "inet", probeTable); err != nil {
		return nil, false
	}
	defer func() { _, _ = run("nft", "delete", "table", "inet", probeTable) }()
	if _, err := run("nft", "add", "chain", "inet", probeTable, "test"); err != nil {
		return nil, false
	}

	if out, err := run("nft", "add", "rule", "inet", probeTable, "test",
		"socket", "transparent", "1", "drop"); err != nil {
		missing = append(missing, "nft_socket")
		kmodNoteRejected("nft_socket", "nft", out)
	}
	if out, err := run("nft", "add", "rule", "inet", probeTable, "test",
		"ip", "protocol", "tcp", "tproxy", "ip", "to", ":1", "drop"); err != nil {
		missing = append(missing, "nft_tproxy")
		kmodNoteRejected("nft_tproxy", "nft", out)
	}
	return missing, true
}

func tproxyMissingIpt(legacy bool) (missing []string, probed bool) {
	loadKernelModuleList("nf_tproxy_ipv4", "nf_tproxy_ipv6", "xt_TPROXY", "xt_socket")

	ipt := backendIPTables
	if legacy {
		ipt = backendIPTablesLegacy
	}
	if !hasBinary(ipt) {
		return nil, false
	}

	tproxyProbeMu.Lock()
	defer tproxyProbeMu.Unlock()

	const probeChain = "B4_PROXY_PROBE"
	_, _ = run(ipt, "-w", "-t", "mangle", "-F", probeChain)
	_, _ = run(ipt, "-w", "-t", "mangle", "-X", probeChain)
	if _, err := run(ipt, "-w", "-t", "mangle", "-N", probeChain); err != nil {
		return nil, false
	}
	defer func() {
		_, _ = run(ipt, "-w", "-t", "mangle", "-F", probeChain)
		_, _ = run(ipt, "-w", "-t", "mangle", "-X", probeChain)
	}()

	if out, err := run(ipt, "-w", "-t", "mangle", "-A", probeChain,
		"-p", "tcp", "-m", "socket", "--transparent", "-j", "ACCEPT"); err != nil {
		missing = append(missing, "xt_socket")
		kmodNoteRejected("xt_socket", ipt, out)
	}
	if out, err := run(ipt, "-w", "-t", "mangle", "-A", probeChain,
		"-p", "tcp", "-j", "TPROXY", "--on-port", "1", "--tproxy-mark", "0x1/0x1"); err != nil {
		missing = append(missing, "xt_TPROXY")
		kmodNoteRejected("xt_TPROXY", ipt, out)
	}
	return missing, true
}

// ProbeTProxyCapability reports whether transparent proxy / TPROXY redirection
// (used by proxy and mtproto-ws routing modes) is usable on the active firewall
// backend, along with any missing kernel modules and the packages providing them.
func ProbeTProxyCapability(cfg *config.Config) (available bool, missing []string, packages []string) {
	var miss []string
	var probed bool
	backend := detectFirewallBackend(cfg)
	if backend == backendNFTables {
		miss, probed = tproxyMissingNft()
	} else {
		miss, probed = tproxyMissingIpt(backend == backendIPTablesLegacy)
	}
	return probed && len(miss) == 0, miss, kmodPkgsFor(miss)
}

func connmarkMissingNft() (missing []string, probed bool) {
	loadKernelModuleList("nft_ct")

	tproxyProbeMu.Lock()
	defer tproxyProbeMu.Unlock()

	const probeTable = "_b4_connmark_probe"
	_, _ = run("nft", "delete", "table", "inet", probeTable)
	if _, err := run("nft", "add", "table", "inet", probeTable); err != nil {
		return nil, false
	}
	defer func() { _, _ = run("nft", "delete", "table", "inet", probeTable) }()
	if _, err := run("nft", "add", "chain", "inet", probeTable, "test"); err != nil {
		return nil, false
	}

	if out, err := run("nft", "add", "rule", "inet", probeTable, "test",
		"ct", "mark", "set", "ct", "mark", "|", "0x8000"); err != nil {
		missing = append(missing, "nft_ct")
		kmodNoteRejected("nft_ct", "nft", out)
	}
	return missing, true
}

func connmarkMissingIpt(legacy bool) (missing []string, probed bool) {
	loadKernelModuleList("xt_connmark", "xt_CONNMARK")

	ipt := backendIPTables
	if legacy {
		ipt = backendIPTablesLegacy
	}
	if !hasBinary(ipt) {
		return nil, false
	}

	tproxyProbeMu.Lock()
	defer tproxyProbeMu.Unlock()

	const probeChain = "B4_CONNMARK_PROBE"
	_, _ = run(ipt, "-w", "-t", "mangle", "-F", probeChain)
	_, _ = run(ipt, "-w", "-t", "mangle", "-X", probeChain)
	if _, err := run(ipt, "-w", "-t", "mangle", "-N", probeChain); err != nil {
		return nil, false
	}
	defer func() {
		_, _ = run(ipt, "-w", "-t", "mangle", "-F", probeChain)
		_, _ = run(ipt, "-w", "-t", "mangle", "-X", probeChain)
	}()

	if out, err := run(ipt, "-w", "-t", "mangle", "-A", probeChain,
		"-m", "connmark", "--mark", "0x8000/0x8000", "-j", "RETURN"); err != nil {
		missing = append(missing, "xt_connmark")
		kmodNoteRejected("xt_connmark", ipt, out)
	}
	if out, err := run(ipt, "-w", "-t", "mangle", "-A", probeChain,
		"-j", "CONNMARK", "--save-mark", "--nfmask", "0x8000", "--ctmask", "0x8000"); err != nil {
		missing = append(missing, "xt_CONNMARK")
		kmodNoteRejected("xt_CONNMARK", ipt, out)
	}
	return missing, true
}

// ProbeConnmarkCapability reports whether the conntrack-mark save/restore used
// by the reply-side self-bypass (so b4 doesn't intercept its own marked
// connections, e.g. the MTProto WS bridge upstream) is usable on the active
// firewall backend.
func ProbeConnmarkCapability(cfg *config.Config) (available bool, missing []string, packages []string) {
	var miss []string
	var probed bool
	backend := detectFirewallBackend(cfg)
	if backend == backendNFTables {
		miss, probed = connmarkMissingNft()
	} else {
		miss, probed = connmarkMissingIpt(backend == backendIPTablesLegacy)
	}
	return probed && len(miss) == 0, miss, kmodPkgsFor(miss)
}

func proxyNftPreflight() {
	proxyNftPreflightOnce.Do(func() {
		missing, probed := tproxyMissingNft()
		if !probed || len(missing) == 0 {
			return
		}
		log.Errorf("Routing (proxy mode): the firewall does not support %s - proxy diversion inactive. %s",
			strings.Join(missing, ", "), kmodMissingHint(missing))
	})
}

func proxyIptPreflight(legacy bool) {
	idx := 0
	if legacy {
		idx = 1
	}
	proxyIptPreflightOnce[idx].Do(func() {
		missing, probed := tproxyMissingIpt(legacy)
		if !probed || len(missing) == 0 {
			return
		}
		log.Errorf("Routing (proxy/mtproto-ws mode): the firewall does not support %s - transparent diversion inactive; traffic for affected sets will NOT be redirected (e.g. the Telegram WS bridge will hang at \"Connecting…\"). %s",
			strings.Join(missing, ", "), kmodMissingHint(missing))
	})
}

// proxyInputChain returns the (table, chain) tuple that holds the system's
// input filter chain, so the proxy mark-accept rule can be inserted there.
// OpenWrt 22.03+ firewall4 uses `inet fw4 input`; bespoke / non-fw4 systems
// typically have `inet filter input`. Returns ok=false if neither exists.
func proxyInputChain() (table, chain string, ok bool) {
	candidates := [][2]string{
		{"fw4", "input"},
		{"filter", "input"},
	}
	for _, c := range candidates {
		if _, err := run("nft", "list", "chain", "inet", c[0], c[1]); err == nil {
			return c[0], c[1], true
		}
	}
	return "", "", false
}

func proxyMarkAndPort(set *config.SetConfig) (uint32, int) {
	mark := tproxy.MarkForSet(set.Id, set.Routing.FWMark)
	if set.Routing.FWMark > 0 && mark != set.Routing.FWMark {
		log.Warnf("Routing: set '%s' asks for fwmark 0x%x, which has bits outside the routing mark mask 0x%x that b4 cannot carry through its firewall rules, so a mark is assigned instead",
			set.Name, set.Routing.FWMark, routeSetMarkMask)
	}
	port := tproxy.PortFor(mark)
	return mark, port
}

var (
	proxyTableMu       sync.Mutex
	proxyTableChosen   int
	proxyTableResolved bool
)

func proxyTable() int {
	proxyTableMu.Lock()
	defer proxyTableMu.Unlock()
	if !proxyTableResolved {
		if picked := routePickProxyTable(); picked > 0 {
			proxyTableChosen = picked
			proxyTableResolved = true
		}
	}
	return proxyTableChosen
}

func proxyTableForget() {
	proxyTableMu.Lock()
	proxyTableChosen = 0
	proxyTableResolved = false
	proxyTableMu.Unlock()
}

func proxyActiveCount() int {
	n := 0
	for _, st := range routeRuleCache {
		if config.RoutingUsesTProxy(st.mode) {
			n++
		}
	}
	return n
}

func routeAddProxyRouterTrafficGuard(be routeBackend, cfg *config.Config, st routeState) {
	guarded := true
	if cfg.Queue.IPv4Enabled && !be.addRouterTrafficGuard(st.chainOut, false, st.setV4, st.mark) {
		guarded = false
	}
	if cfg.Queue.IPv6Enabled && !be.addRouterTrafficGuard(st.chainOut, true, st.setV6, st.mark) {
		guarded = false
	}
	if !guarded {
		log.Warnf("Routing: %s took no rate guard on the router's own traffic, so an upstream proxy whose own connections leave from this router and land in the set would open them without limit; on nftables this needs a kernel with rule limits, on iptables the xt_hashlimit module", st.chainOut)
	}
}

func routeEnsureProxyRule(be routeBackend, cfg *config.Config, set *config.SetConfig, st routeState, sources []string) error {
	if st.table <= 0 {
		return fmt.Errorf("no free routing table for transparent proxying")
	}
	if be.name() == backendNFTables {
		proxyNftPreflight()
		deleteNftRulesContaining(routeNftOutput, "@"+st.setV4)
		deleteNftRulesContaining(routeNftOutput, "@"+st.setV6)
	}
	if err := be.ensureChain(st.chainPre, true); err != nil {
		return err
	}
	if err := be.ensureChain(st.chainOut, true); err != nil {
		return err
	}
	superseded := []routeChainSnapshot{
		be.snapshotChainRules(st.chainPre, true),
		be.snapshotChainRules(st.chainOut, true),
	}
	rebuilt := false
	defer func() {
		if !rebuilt {
			return
		}
		for _, snap := range superseded {
			be.dropChainRules(snap)
		}
	}()
	if cfg.Queue.IPv4Enabled {
		if err := be.ensureIPSet(st.setV4, false); err != nil {
			return err
		}
	}
	if cfg.Queue.IPv6Enabled {
		if err := be.ensureIPSet(st.setV6, true); err != nil {
			return err
		}
	}

	gate := routeSetDeviceGate(cfg, set)
	routeWarnDeviceGate(set.Name, gate)
	sourceScoped := routeSetIsSourceScoped(set)
	routeSelfDialBypass(be, cfg, st.chainPre)
	be.addClaimedBypassRule(st.chainPre, st.mark)
	routeAddBlacklistGate(be, "mangle", st.chainPre, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled, gate)
	routeAddLocalDestinationGuard(be, st.chainPre, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled)
	if !sourceScoped {
		routeSelfDialBypass(be, cfg, st.chainOut)
		be.addClaimedBypassRule(st.chainOut, 0)
		routeAddProxyRouterTrafficGuard(be, cfg, st)
	}

	port, _ := portFromState(st)
	legacy := isLegacyIptBackend(be)

	udp := set.Routing.Upstream.UDP

	switch be.name() {
	case backendNFTables:
		if cfg.Queue.IPv4Enabled {
			addProxyDivertRuleNft(st.chainPre, false, st.setV4, st.mark)
			addProxyTProxyRuleNft(st.chainPre, false, st.setV4, st.mark, port, sources, "tcp")
			if udp {
				addProxyTProxyRuleNft(st.chainPre, false, st.setV4, st.mark, port, sources, "udp")
			}
		}
		if cfg.Queue.IPv6Enabled {
			addProxyDivertRuleNft(st.chainPre, true, st.setV6, st.mark)
			addProxyTProxyRuleNft(st.chainPre, true, st.setV6, st.mark, port, sources, "tcp")
			if udp {
				addProxyTProxyRuleNft(st.chainPre, true, st.setV6, st.mark, port, sources, "udp")
			}
		}
		if !sourceScoped {
			addProxyOutputMarkRulesNft(cfg, st)
		}
	default:
		proxyIptPreflight(legacy)
		if cfg.Queue.IPv4Enabled {
			addProxyDivertRuleIpt(false, st.chainPre, st.setV4, st.mark, legacy)
			addProxyTProxyRuleIpt(false, st.chainPre, st.setV4, st.mark, port, sources, legacy, "tcp")
			if udp {
				addProxyTProxyRuleIpt(false, st.chainPre, st.setV4, st.mark, port, sources, legacy, "udp")
			}
			if !sourceScoped {
				addProxyOutputMarkRuleIpt(false, st.chainOut, st.setV4, st.mark, legacy)
			}
		}
		if cfg.Queue.IPv6Enabled {
			addProxyDivertRuleIpt(true, st.chainPre, st.setV6, st.mark, legacy)
			addProxyTProxyRuleIpt(true, st.chainPre, st.setV6, st.mark, port, sources, legacy, "tcp")
			if udp {
				addProxyTProxyRuleIpt(true, st.chainPre, st.setV6, st.mark, port, sources, legacy, "udp")
			}
			if !sourceScoped {
				addProxyOutputMarkRuleIpt(true, st.chainOut, st.setV6, st.mark, legacy)
			}
		}
	}

	if routeWantsOutputJump(st) {
		be.ensureJumpRule("OUTPUT", st.chainOut, true, true)
	} else {
		be.deleteJumpRules("OUTPUT", st.chainOut, true)
	}
	routeEnsureGatedPreJump(be, st.chainPre, gate)
	addProxyInputAccept(be, st.mark)

	if sourceScoped {
		log.Infof("Routing [%s]: set '%s' is limited to source devices or interfaces, so traffic the router itself originates keeps using the normal route", be.name(), set.Name)
	}

	if st.quicReject {
		if err := routeEnsureQUICReject(be, cfg, st, gate, sourceScoped, sources); err != nil {
			return err
		}
		log.Infof("Routing [%s]: set '%s' refuses QUIC (UDP/%d) to matched addresses so clients fall back to TCP through the upstream; enable 'Route UDP through upstream' if the proxy supports UDP ASSOCIATE",
			be.name(), set.Name, quicRejectPort)
	}

	routeEnsureLocalDelivery(st.mark, st.table, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled)
	rebuilt = true
	return nil
}

const quicRejectPort = 443

func routeEnsureQUICReject(be routeBackend, cfg *config.Config, st routeState, gate routeDeviceGate, sourceScoped bool, sources []string) error {
	switch be.name() {
	case backendNFTables:
		if err := ensureBlockBaseNft(); err != nil {
			return err
		}
		if err := be.ensureChain(st.chainQUIC, true); err != nil {
			return err
		}
		be.flushChain(st.chainQUIC, true)
		routeSelfDialBypass(be, cfg, st.chainQUIC)
		if cfg.Queue.IPv4Enabled {
			addQUICRejectRuleNft(st.chainQUIC, false, st.setV4, sources)
		}
		if cfg.Queue.IPv6Enabled {
			addQUICRejectRuleNft(st.chainQUIC, true, st.setV6, sources)
		}
		ensureBlockJumpNft(routeNftBlockFwd, st.chainQUIC, gate)
		if sourceScoped {
			deleteNftJumpRules(routeNftTable, routeNftBlockOut, st.chainQUIC)
		} else {
			ensureBlockJumpNft(routeNftBlockOut, st.chainQUIC, routeDeviceGate{})
		}
	default:
		legacy := isLegacyIptBackend(be)
		if err := ensureBlockChainIpt(st.chainQUIC, legacy); err != nil {
			return err
		}
		for _, m := range quicBypassMarks(cfg) {
			addQUICBypassRuleIpt(st.chainQUIC, m, legacy)
		}
		if cfg.Queue.IPv4Enabled {
			addQUICRejectRuleIpt(false, st.chainQUIC, st.setV4, sources, legacy)
		}
		if cfg.Queue.IPv6Enabled {
			addQUICRejectRuleIpt(true, st.chainQUIC, st.setV6, sources, legacy)
		}
		ensureBlockJumpIpt("FORWARD", st.chainQUIC, legacy, gate)
		if sourceScoped {
			deleteBlockJumpIpt("OUTPUT", st.chainQUIC, legacy)
		} else {
			ensureBlockJumpIpt("OUTPUT", st.chainQUIC, legacy, routeDeviceGate{})
		}
	}
	return nil
}

func routeCleanupQUICReject(be routeBackend, st routeState) {
	if st.chainQUIC == "" {
		return
	}
	switch be.name() {
	case backendNFTables:
		deleteNftJumpRules(routeNftTable, routeNftBlockFwd, st.chainQUIC)
		deleteNftJumpRules(routeNftTable, routeNftBlockOut, st.chainQUIC)
		be.flushChain(st.chainQUIC, true)
		be.deleteChain(st.chainQUIC, true)
	default:
		legacy := isLegacyIptBackend(be)
		deleteBlockJumpIpt("FORWARD", st.chainQUIC, legacy)
		deleteBlockJumpIpt("OUTPUT", st.chainQUIC, legacy)
		flushDeleteBlockChainIpt(st.chainQUIC, legacy)
	}
}

func addQUICRejectRuleNft(chain string, v6 bool, setName string, sources []string) {
	emit := func(sn, src string) {
		args := []string{"nft", "add", "rule", "inet", routeNftTable, chain}
		if src != "" {
			args = append(args, "iifname", fmt.Sprintf("%q", src))
		}
		if v6 {
			args = append(args, "meta", "l4proto", "udp", "ip6", "daddr", "@"+sn)
		} else {
			args = append(args, "ip", "protocol", "udp", "ip", "daddr", "@"+sn)
		}
		args = append(args, "udp", "dport", fmt.Sprintf("%d", quicRejectPort),
			"reject", "with", "icmpx", "type", "port-unreachable")
		runLogged("routing: add quic reject "+chain, args...)
	}

	for _, sn := range []string{setName, routeNftDynSet(setName)} {
		if len(sources) == 0 {
			emit(sn, "")
			continue
		}
		for _, src := range sources {
			emit(sn, src)
		}
	}
}

// quicBypassMarks must stay equal to routeBypassMarks: the QUIC chain sits in
// the filter table and so has its own emitter, and the monitor checks it against
// the same list every other diverting chain is checked against.
func quicBypassMarks(cfg *config.Config) []uint32 {
	return routeBypassMarks(cfg)
}

func addQUICBypassRuleIpt(chain string, mark uint32, legacy bool) {
	markHex := fmt.Sprintf("0x%x/0x%x", mark, mark)
	for _, v6 := range []bool{false, true} {
		cmd := iptCmdFor(v6, legacy)
		if !hasBinary(cmd) {
			continue
		}
		runLogged("routing: add quic bypass "+chain,
			cmd, "-w", "-t", "filter", "-A", chain, "-m", "mark", "--mark", markHex, "-j", "RETURN")
	}
}

func addQUICRejectRuleIpt(v6 bool, chain, setName string, sources []string, legacy bool) {
	cmd := iptCmdFor(v6, legacy)
	if !hasBinary(cmd) {
		return
	}
	icmpReject := "icmp-port-unreachable"
	if v6 {
		icmpReject = "icmp6-port-unreachable"
	}

	emit := func(src string) {
		args := []string{cmd, "-w", "-t", "filter", "-A", chain}
		if src != "" {
			args = append(args, "-i", src)
		}
		args = append(args, "-p", "udp", "--dport", fmt.Sprintf("%d", quicRejectPort),
			"-m", "set", "--match-set", setName, "dst",
			"-j", "REJECT", "--reject-with", icmpReject)
		runLogged("routing: add quic reject "+chain, args...)
	}

	if len(sources) == 0 {
		emit("")
		return
	}
	for _, src := range sources {
		emit(src)
	}
}

func routeCleanupProxyRule(be routeBackend, st routeState, keepSets bool) {
	tableStr := fmt.Sprintf("%d", st.table)

	if hasBinary("ip") && st.table > 0 {
		routeDelRuleAllForms(st.mark, tableStr)
		if proxyActiveCount() <= 1 {
			runLogged("routing: delete proxy local route v4", "ip", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", tableStr)
			runLogged("routing: delete proxy local route v6", "ip", "-6", "route", "del", "local", "::/0", "dev", "lo", "table", tableStr)
			proxyTableForget()
		}
	}

	removeProxyInputAccept(be, st.mark)
	routeCleanupQUICReject(be, st)
	be.deleteJumpRules("PREROUTING", st.chainPre, true)
	be.flushChain(st.chainPre, true)
	be.deleteChain(st.chainPre, true)

	if be.name() == backendNFTables {
		deleteNftRulesContaining(routeNftOutput, "@"+st.setV4)
		deleteNftRulesContaining(routeNftOutput, "@"+st.setV6)
	}
	be.deleteJumpRules("OUTPUT", st.chainOut, true)
	be.flushChain(st.chainOut, true)
	be.deleteChain(st.chainOut, true)

	routeDropSets(be, st, keepSets)
}

func routeEnsureLocalDelivery(mark uint32, table int, ipv4, ipv6 bool) {
	markStrMask := routeSetMarkRule(mark)
	tableStr := fmt.Sprintf("%d", table)
	prioStr := fmt.Sprintf("%d", proxyRulePriority)

	writeSysctl("/proc/sys/net/ipv4/conf/lo/rp_filter", "0")
	writeSysctl("/proc/sys/net/ipv4/conf/all/rp_filter", "2")

	routeDelRuleAllForms(mark, tableStr)

	if ipv4 {
		runLogged("routing: add ip rule v4 (proxy)", "ip", "rule", "add", "fwmark", markStrMask, "lookup", tableStr, "priority", prioStr)
		runLogged("routing: add local route v4 (proxy)", "ip", "route", "replace", "local", "0.0.0.0/0", "dev", "lo", "table", tableStr)
	} else {
		runLogged("routing: delete local route v4 (proxy)", "ip", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", tableStr)
	}
	if ipv6 {
		runLogged("routing: add ip rule v6 (proxy)", "ip", "-6", "rule", "add", "fwmark", markStrMask, "lookup", tableStr, "priority", prioStr)
		runLogged("routing: add local route v6 (proxy)", "ip", "-6", "route", "replace", "local", "::/0", "dev", "lo", "table", tableStr)
	} else {
		runLogged("routing: delete local route v6 (proxy)", "ip", "-6", "route", "del", "local", "::/0", "dev", "lo", "table", tableStr)
	}
}

var writeSysctl = writeSysctlExec

func writeSysctlExec(path, value string) {
	cur, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(cur)) == value {
		return
	}
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		log.Tracef("routing: sysctl %s=%s failed: %v", path, value, err)
	}
}

func nftJumpHandles(table, parentChain, targetChain string) []string {
	out, err := run("nft", "-a", "list", "chain", "inet", table, parentChain)
	if err != nil {
		log.Tracef("routing: list nft chain inet %s %s failed: %v", table, parentChain, err)
		return nil
	}
	var handles []string
	for _, line := range strings.Split(out, "\n") {
		handleIdx := strings.LastIndex(line, "# handle ")
		if handleIdx == -1 {
			continue
		}
		if !strings.Contains(strings.TrimSpace(line[:handleIdx]), "jump "+targetChain) {
			continue
		}
		handle := strings.TrimSpace(line[handleIdx+len("# handle "):])
		if handle != "" {
			handles = append(handles, handle)
		}
	}
	return handles
}

func nftDropJumpHandles(table, parentChain string, handles []string) {
	for _, handle := range handles {
		runLogged("routing: delete superseded jump "+parentChain,
			"nft", "delete", "rule", "inet", table, parentChain, "handle", handle)
	}
}

func deleteNftJumpRules(table, parentChain, targetChain string) {
	out, err := run("nft", "-a", "list", "chain", "inet", table, parentChain)
	if err != nil {
		log.Tracef("routing: list nft chain inet %s %s failed: %v", table, parentChain, err)
		return
	}
	for _, line := range strings.Split(out, "\n") {
		handleIdx := strings.LastIndex(line, "# handle ")
		if handleIdx == -1 {
			continue
		}
		rule := strings.TrimSpace(line[:handleIdx])
		if !strings.Contains(rule, "jump "+targetChain) {
			continue
		}
		handle := strings.TrimSpace(line[handleIdx+len("# handle "):])
		if handle == "" {
			continue
		}
		runLogged("routing: delete leftover prerouting jump (proxy)",
			"nft", "delete", "rule", "inet", table, parentChain, "handle", handle)
	}
}

var (
	proxyOutMarkMu          sync.Mutex
	proxyOutMarkUnqualified = map[string]bool{}
)

func proxyOutMarkFallsBack(tool, out string) {
	proxyOutMarkMu.Lock()
	warned := proxyOutMarkUnqualified[tool]
	proxyOutMarkUnqualified[tool] = true
	proxyOutMarkMu.Unlock()
	if warned {
		return
	}
	log.Warnf("Routing: %s rejected the connection-direction match (%s), so replies the router's own services send to an address a proxy set matches are marked for the upstream as well",
		tool, strings.TrimSpace(out))
}

func proxyOutMarkForget() {
	proxyOutMarkMu.Lock()
	proxyOutMarkUnqualified = map[string]bool{}
	proxyOutMarkMu.Unlock()
}

func addProxyOutputMarkRulesNft(cfg *config.Config, st routeState) {
	markHex := fmt.Sprintf("0x%x", st.mark)
	emit := func(proto []string, field, sn string) {
		head := append([]string{"nft", "add", "rule", "inet", routeNftTable, st.chainOut}, proto...)
		tail := []string{field, "daddr", "@" + sn, "meta", "mark", "set", markHex}
		proxyOutMarkMu.Lock()
		unqualified := proxyOutMarkUnqualified["nft"]
		proxyOutMarkMu.Unlock()
		if !unqualified {
			args := append(append(append([]string{}, head...), "ct", "direction", "original"), tail...)
			out, err := run(args...)
			if err == nil {
				return
			}
			proxyOutMarkFallsBack("nft", out)
		}
		runLogged("routing: add output mark rule (proxy)", append(head, tail...)...)
	}
	if cfg.Queue.IPv4Enabled {
		for _, sn := range []string{st.setV4, routeNftDynSet(st.setV4)} {
			emit([]string{"ip", "protocol", "tcp"}, "ip", sn)
		}
	}
	if cfg.Queue.IPv6Enabled {
		for _, sn := range []string{st.setV6, routeNftDynSet(st.setV6)} {
			emit([]string{"meta", "l4proto", "tcp"}, "ip6", sn)
		}
	}
}

func deleteNftRulesContaining(chain, substr string) {
	out, err := run("nft", "-a", "list", "chain", "inet", routeNftTable, chain)
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, substr) {
			continue
		}
		idx := strings.LastIndex(line, "# handle ")
		if idx < 0 {
			continue
		}
		handle := strings.TrimSpace(line[idx+len("# handle "):])
		if handle == "" {
			continue
		}
		runLogged("routing: delete nft rule by handle",
			"nft", "delete", "rule", "inet", routeNftTable, chain, "handle", handle)
	}
}

func addProxyOutputMarkRuleIpt(v6 bool, chain, setName string, mark uint32, legacy bool) {
	cmd := backendIPTables
	if v6 {
		cmd = backendIP6Tables
	}
	if legacy {
		if v6 {
			cmd = backendIP6TablesLegacy
		} else {
			cmd = backendIPTablesLegacy
		}
	}
	if !hasBinary(cmd) {
		return
	}
	markHex := fmt.Sprintf("0x%x/0x%x", mark, mark)
	tail := []string{"-m", "set", "--match-set", setName, "dst", "-j", "MARK", "--set-mark", markHex}
	proxyOutMarkMu.Lock()
	unqualified := proxyOutMarkUnqualified[cmd]
	proxyOutMarkMu.Unlock()
	if !unqualified {
		args := append([]string{cmd, "-w", "-t", "mangle", "-A", chain, "-p", "tcp", "-m", "conntrack", "--ctdir", "ORIGINAL"}, tail...)
		out, err := run(args...)
		if err == nil {
			return
		}
		proxyOutMarkFallsBack(cmd, out)
	}
	runLogged("routing: add output mark rule "+chain,
		append([]string{cmd, "-w", "-t", "mangle", "-A", chain, "-p", "tcp"}, tail...)...)
}

func addProxyDivertRuleIpt(v6 bool, chain, setName string, mark uint32, legacy bool) {
	cmd := backendIPTables
	if v6 {
		cmd = backendIP6Tables
	}
	if legacy {
		if v6 {
			cmd = backendIP6TablesLegacy
		} else {
			cmd = backendIPTablesLegacy
		}
	}
	if !hasBinary(cmd) {
		return
	}
	markHex := fmt.Sprintf("0x%x/0x%x", mark, mark)
	runLogged("routing: add divert mark "+chain,
		cmd, "-w", "-t", "mangle", "-A", chain, "-p", "tcp",
		"-m", "socket", "--transparent",
		"-m", "set", "--match-set", setName, "dst",
		"-j", "MARK", "--set-mark", markHex)
	runLogged("routing: add divert accept "+chain,
		cmd, "-w", "-t", "mangle", "-A", chain, "-p", "tcp",
		"-m", "socket", "--transparent",
		"-m", "set", "--match-set", setName, "dst",
		"-j", "ACCEPT")
}

func addProxyDivertRuleNft(chain string, v6 bool, setName string, mark uint32) {
	markHex := fmt.Sprintf("0x%x", mark)
	emit := func(sn string) {
		args := []string{"add", "rule", "inet", routeNftTable, chain}
		if v6 {
			args = append(args, "meta", "l4proto", "tcp", "ip6", "daddr", "@"+sn)
		} else {
			args = append(args, "ip", "protocol", "tcp", "ip", "daddr", "@"+sn)
		}
		args = append(args, "socket", "transparent", "1", "meta", "mark", "set", markHex, "accept")
		runLogged("routing: add divert "+chain, append([]string{"nft"}, args...)...)
	}
	emit(setName)
	emit(routeNftDynSet(setName))
}

func addProxyInputAccept(be routeBackend, mark uint32) {
	markHex := fmt.Sprintf("0x%x/0x%x", mark, mark)
	if be.name() == backendNFTables {
		removeProxyInputAcceptNft(mark)
		table, chain, ok := proxyInputChain()
		if !ok {
			log.Tracef("routing: no nft input filter chain found (tried inet fw4, inet filter); skipping input accept rule")
			return
		}
		runLogged("routing: add input accept (proxy)",
			"nft", "insert", "rule", "inet", table, chain,
			"meta", "mark", "&", fmt.Sprintf("0x%x", mark), "==", fmt.Sprintf("0x%x", mark), "accept")
		return
	}
	for _, fam := range []string{backendIPTables, backendIP6Tables, backendIPTablesLegacy, backendIP6TablesLegacy} {
		if !hasBinary(fam) {
			continue
		}
		for i := 0; i < 100; i++ {
			if _, err := run(fam, "-w", "-D", "INPUT", "-m", "mark", "--mark", markHex, "-j", "ACCEPT"); err != nil {
				break
			}
		}
		runLogged("routing: add input accept (proxy) "+fam,
			fam, "-w", "-I", "INPUT", "1", "-m", "mark", "--mark", markHex, "-j", "ACCEPT")
	}
}

func removeProxyInputAcceptNft(mark uint32) {
	for _, c := range [][2]string{{"filter", "input"}, {"fw4", "input"}} {
		table, chain := c[0], c[1]
		out, err := run("nft", "-a", "list", "chain", "inet", table, chain)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			m, verb, ok := nftParseMarkRule(line)
			if !ok || verb != "accept" || m != mark {
				continue
			}
			handle := nftHandleFromLine(line)
			if handle == "" {
				continue
			}
			runLogged("routing: delete input accept (proxy)",
				"nft", "delete", "rule", "inet", table, chain, "handle", handle)
		}
	}
}

func sweepProxyInputAcceptsNft() {
	for _, c := range [][2]string{{"filter", "input"}, {"fw4", "input"}} {
		table, chain := c[0], c[1]
		out, err := run("nft", "-a", "list", "chain", "inet", table, chain)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			m, verb, ok := nftParseMarkRule(line)
			if !ok || verb != "accept" || !tproxy.InMarkRange(m) {
				continue
			}
			handle := nftHandleFromLine(line)
			if handle == "" {
				continue
			}
			runLogged("routing: sweep input accept (proxy)",
				"nft", "delete", "rule", "inet", table, chain, "handle", handle)
		}
	}
}

func sweepProxyInputAcceptsIpt(cmd string) {
	iptDeleteListedLines(cmd, "filter", "INPUT", func(line string) bool {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != "ACCEPT" || !strings.Contains(line, "mark match") {
			return false
		}
		m, ok := iptMarkFromRule(line)
		return ok && tproxy.InMarkRange(m)
	})
}

func iptMarkFromRule(line string) (uint32, bool) {
	for _, f := range strings.Fields(line) {
		parts := strings.Split(f, "/")
		if len(parts) != 2 {
			continue
		}
		a, errA := strconv.ParseUint(strings.TrimPrefix(parts[0], "0x"), 16, 32)
		b, errB := strconv.ParseUint(strings.TrimPrefix(parts[1], "0x"), 16, 32)
		if errA != nil || errB != nil || a != b {
			continue
		}
		return uint32(a), true
	}
	return 0, false
}

func removeProxyInputAccept(be routeBackend, mark uint32) {
	markHex := fmt.Sprintf("0x%x/0x%x", mark, mark)
	if be.name() == backendNFTables {
		removeProxyInputAcceptNft(mark)
		return
	}
	for _, fam := range []string{backendIPTables, backendIP6Tables, backendIPTablesLegacy, backendIP6TablesLegacy} {
		if !hasBinary(fam) {
			continue
		}
		for i := 0; i < 100; i++ {
			if _, err := run(fam, "-w", "-D", "INPUT", "-m", "mark", "--mark", markHex, "-j", "ACCEPT"); err != nil {
				break
			}
		}
	}
}

func addProxyTProxyRuleNft(chain string, v6 bool, setName string, mark uint32, port int, sources []string, proto string) {
	markHex := fmt.Sprintf("0x%x", mark)
	portStr := fmt.Sprintf(":%d", port)

	emit := func(sn, src string) {
		args := []string{"add", "rule", "inet", routeNftTable, chain}
		if src != "" {
			args = append(args, "iifname", fmt.Sprintf("%q", src))
		}
		if v6 {
			args = append(args,
				"meta", "l4proto", proto,
				"ip6", "daddr", "@"+sn,
				"meta", "mark", "set", markHex,
				"tproxy", "ip6", "to", portStr,
				"accept",
			)
		} else {
			args = append(args,
				"ip", "protocol", proto,
				"ip", "daddr", "@"+sn,
				"meta", "mark", "set", markHex,
				"tproxy", "ip", "to", portStr,
				"accept",
			)
		}
		runLogged("routing: add tproxy rule "+chain, append([]string{"nft"}, args...)...)
	}

	for _, sn := range []string{setName, routeNftDynSet(setName)} {
		if len(sources) == 0 {
			emit(sn, "")
			continue
		}
		for _, src := range sources {
			emit(sn, src)
		}
	}
}

func addProxyTProxyRuleIpt(v6 bool, chain, setName string, mark uint32, port int, sources []string, legacy bool, proto string) {
	cmd := backendIPTables
	if v6 {
		cmd = backendIP6Tables
	}
	if legacy {
		if v6 {
			cmd = backendIP6TablesLegacy
		} else {
			cmd = backendIPTablesLegacy
		}
	}
	if !hasBinary(cmd) {
		return
	}
	markHex := fmt.Sprintf("0x%x/0x%x", mark, mark)

	emit := func(src string) {
		args := []string{cmd, "-w", "-t", "mangle", "-A", chain, "-p", proto}
		if src != "" {
			args = append(args, "-i", src)
		}
		args = append(args,
			"-m", "set", "--match-set", setName, "dst",
			"-j", "TPROXY",
			"--tproxy-mark", markHex,
			"--on-port", fmt.Sprintf("%d", port),
		)
		runLogged("routing: add tproxy rule "+chain, args...)
	}

	if len(sources) == 0 {
		emit("")
		return
	}
	for _, src := range sources {
		emit(src)
	}
}

func portFromState(st routeState) (int, bool) {
	if st.tproxyPort > 0 {
		return st.tproxyPort, true
	}
	return tproxy.PortFor(st.mark), false
}

func isLegacyIptBackend(be routeBackend) bool {
	if ipt, ok := be.(*routeIptBackend); ok {
		return ipt.legacy
	}
	return false
}
