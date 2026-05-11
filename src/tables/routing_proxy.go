package tables

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/tproxy"
)

const proxyRulePriority = 5

const proxyLocalDeliveryTable = 252

var proxyNftPreflightOnce sync.Once

func proxyNftPreflight() {
	proxyNftPreflightOnce.Do(func() {
		_, _ = run("sh", "-c", "modprobe -q nft_tproxy 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nft_socket 2>/dev/null || true")

		const probeTable = "_b4_proxy_probe"
		_, _ = run("nft", "delete", "table", "inet", probeTable)
		if _, err := run("nft", "add", "table", "inet", probeTable); err != nil {
			return
		}
		defer func() { _, _ = run("nft", "delete", "table", "inet", probeTable) }()
		if _, err := run("nft", "add", "chain", "inet", probeTable, "test"); err != nil {
			return
		}

		var missing []string
		if _, err := run("nft", "add", "rule", "inet", probeTable, "test",
			"socket", "transparent", "1", "drop"); err != nil {
			missing = append(missing, "nft_socket")
		}
		if _, err := run("nft", "add", "rule", "inet", probeTable, "test",
			"ip", "protocol", "tcp", "tproxy", "ip", "to", ":1", "drop"); err != nil {
			missing = append(missing, "nft_tproxy")
		}
		if len(missing) == 0 {
			return
		}

		pkgs := make([]string, 0, len(missing))
		for _, m := range missing {
			switch m {
			case "nft_socket":
				pkgs = append(pkgs, "kmod-nft-socket")
			case "nft_tproxy":
				pkgs = append(pkgs, "kmod-nft-tproxy")
			}
		}
		log.Errorf("Routing (proxy mode): missing kernel module(s) %s — proxy diversion inactive. Required package(s): %s",
			strings.Join(missing, ", "), strings.Join(pkgs, " "))
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
	port := tproxy.PortFor(mark)
	return mark, port
}

func proxyTable() int {
	return proxyLocalDeliveryTable
}

func proxyActiveCount() int {
	n := 0
	for _, st := range routeRuleCache {
		if st.mode == config.RoutingModeProxy {
			n++
		}
	}
	return n
}

func routeEnsureProxyRule(be routeBackend, cfg *config.Config, set *config.SetConfig, st routeState, sources []string) error {
	if be.name() == backendNFTables {
		proxyNftPreflight()
	}
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
	if err := be.ensureChain(st.chainPre, true); err != nil {
		return err
	}
	be.flushChain(st.chainPre, true)

	queueMark := routeQueueBypassMark(cfg)
	be.addBypassRule(st.chainPre, queueMark)

	port, _ := portFromState(st)
	legacy := isLegacyIptBackend(be)

	switch be.name() {
	case backendNFTables:
		if cfg.Queue.IPv4Enabled {
			addProxyDivertRuleNft(st.chainPre, false, st.setV4, st.mark)
			addProxyTProxyRuleNft(st.chainPre, false, st.setV4, st.mark, port, sources)
		}
		if cfg.Queue.IPv6Enabled {
			addProxyDivertRuleNft(st.chainPre, true, st.setV6, st.mark)
			addProxyTProxyRuleNft(st.chainPre, true, st.setV6, st.mark, port, sources)
		}
		ensureProxyOutputBaseRulesNft(cfg, st, queueMark)
	default:
		if err := be.ensureChain(st.chainOut, true); err != nil {
			return err
		}
		be.flushChain(st.chainOut, true)
		be.addBypassRule(st.chainOut, queueMark)
		if cfg.Queue.IPv4Enabled {
			addProxyDivertRuleIpt(false, st.chainPre, st.setV4, st.mark, legacy)
			addProxyTProxyRuleIpt(false, st.chainPre, st.setV4, st.mark, port, sources, legacy)
			addProxyOutputMarkRuleIpt(false, st.chainOut, st.setV4, st.mark, legacy)
		}
		if cfg.Queue.IPv6Enabled {
			addProxyDivertRuleIpt(true, st.chainPre, st.setV6, st.mark, legacy)
			addProxyTProxyRuleIpt(true, st.chainPre, st.setV6, st.mark, port, sources, legacy)
			addProxyOutputMarkRuleIpt(true, st.chainOut, st.setV6, st.mark, legacy)
		}
		insertProxyOutputJump(be, st.chainOut)
	}

	insertProxyJumpAtTop(be, st.chainPre)
	addProxyInputAccept(be, st.mark)

	routeEnsureLocalDelivery(st.mark, st.table, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled)
	return nil
}

func routeCleanupProxyRule(be routeBackend, st routeState) {
	markStr := fmt.Sprintf("0x%x", st.mark)
	markStrMask := fmt.Sprintf("0x%x/0x%x", st.mark, st.mark)
	tableStr := fmt.Sprintf("%d", st.table)

	if hasBinary("ip") {
		routeDelRuleLoop(false, markStr, tableStr)
		routeDelRuleLoop(false, markStrMask, tableStr)
		routeDelRuleLoop(true, markStr, tableStr)
		routeDelRuleLoop(true, markStrMask, tableStr)
		if proxyActiveCount() <= 1 {
			runLogged("routing: delete proxy local route v4", "ip", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", tableStr)
			runLogged("routing: delete proxy local route v6", "ip", "-6", "route", "del", "local", "::/0", "dev", "lo", "table", tableStr)
		}
	}

	removeProxyInputAccept(be, st.mark)
	be.deleteJumpRules("PREROUTING", st.chainPre, true)
	be.flushChain(st.chainPre, true)
	be.deleteChain(st.chainPre, true)

	if be.name() == backendNFTables {
		deleteNftRulesContaining(routeNftOutput, "@"+st.setV4)
		deleteNftRulesContaining(routeNftOutput, "@"+st.setV6)
	} else {
		be.deleteJumpRules("OUTPUT", st.chainOut, true)
		be.flushChain(st.chainOut, true)
		be.deleteChain(st.chainOut, true)
	}

	be.flushIPSet(st.setV4)
	be.destroyIPSet(st.setV4)
	be.flushIPSet(st.setV6)
	be.destroyIPSet(st.setV6)
}

func routeEnsureLocalDelivery(mark uint32, table int, ipv4, ipv6 bool) {
	markStrMask := fmt.Sprintf("0x%x/0x%x", mark, mark)
	tableStr := fmt.Sprintf("%d", table)
	prioStr := fmt.Sprintf("%d", proxyRulePriority)

	writeSysctl("/proc/sys/net/ipv4/conf/lo/rp_filter", "0")
	writeSysctl("/proc/sys/net/ipv4/conf/all/rp_filter", "2")

	if ipv4 {
		routeDelRuleLoop(false, fmt.Sprintf("0x%x", mark), tableStr)
		routeDelRuleLoop(false, markStrMask, tableStr)
		runLogged("routing: add ip rule v4 (proxy)", "ip", "rule", "add", "fwmark", markStrMask, "lookup", tableStr, "priority", prioStr)
		runLogged("routing: add local route v4 (proxy)", "ip", "route", "replace", "local", "0.0.0.0/0", "dev", "lo", "table", tableStr)
	}
	if ipv6 {
		routeDelRuleLoop(true, fmt.Sprintf("0x%x", mark), tableStr)
		routeDelRuleLoop(true, markStrMask, tableStr)
		runLogged("routing: add ip rule v6 (proxy)", "ip", "-6", "rule", "add", "fwmark", markStrMask, "lookup", tableStr, "priority", prioStr)
		runLogged("routing: add local route v6 (proxy)", "ip", "-6", "route", "replace", "local", "::/0", "dev", "lo", "table", tableStr)
	}
}

func writeSysctl(path, value string) {
	cur, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(cur)) == value {
		return
	}
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		log.Tracef("routing: sysctl %s=%s failed: %v", path, value, err)
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

func insertProxyJumpAtTop(be routeBackend, chain string) {
	if be.name() == backendNFTables {
		deleteNftJumpRules(routeNftTable, routeNftPrerouting, chain)
		runLogged("routing: insert prerouting jump (proxy)", "nft", "insert", "rule", "inet", routeNftTable, routeNftPrerouting, "jump", chain)
		return
	}
	for _, fam := range []string{backendIPTables, backendIP6Tables, backendIPTablesLegacy, backendIP6TablesLegacy} {
		if !hasBinary(fam) {
			continue
		}
		for i := 0; i < 100; i++ {
			if _, err := run(fam, "-w", "-t", "mangle", "-D", "PREROUTING", "-j", chain); err != nil {
				break
			}
		}
		runLogged("routing: insert prerouting jump (proxy) "+fam,
			fam, "-w", "-t", "mangle", "-I", "PREROUTING", "1", "-j", chain)
	}
}

func insertProxyOutputJump(be routeBackend, chain string) {
	if be.name() == backendNFTables {
		return
	}
	for _, fam := range []string{backendIPTables, backendIP6Tables, backendIPTablesLegacy, backendIP6TablesLegacy} {
		if !hasBinary(fam) {
			continue
		}
		for i := 0; i < 100; i++ {
			if _, err := run(fam, "-w", "-t", "mangle", "-D", "OUTPUT", "-j", chain); err != nil {
				break
			}
		}
		runLogged("routing: insert output jump (proxy) "+fam,
			fam, "-w", "-t", "mangle", "-I", "OUTPUT", "1", "-j", chain)
	}
}

func ensureProxyOutputBaseRulesNft(cfg *config.Config, st routeState, queueMark uint32) {
	bypassSig := fmt.Sprintf("meta mark & 0x%x == 0x%x return", queueMark, queueMark)
	out, err := run("nft", "list", "chain", "inet", routeNftTable, routeNftOutput)
	if err == nil && !strings.Contains(out, bypassSig) {
		runLogged("routing: insert output bypass (proxy)",
			"nft", "insert", "rule", "inet", routeNftTable, routeNftOutput,
			"meta", "mark", "&", fmt.Sprintf("0x%x", queueMark), "==", fmt.Sprintf("0x%x", queueMark), "return")
	}

	deleteNftRulesContaining(routeNftOutput, "@"+st.setV4)
	deleteNftRulesContaining(routeNftOutput, "@"+st.setV6)

	markHex := fmt.Sprintf("0x%x", st.mark)
	if cfg.Queue.IPv4Enabled {
		runLogged("routing: add output mark rule (base)",
			"nft", "add", "rule", "inet", routeNftTable, routeNftOutput,
			"ip", "protocol", "tcp",
			"ip", "daddr", "@"+st.setV4,
			"meta", "mark", "set", markHex)
	}
	if cfg.Queue.IPv6Enabled {
		runLogged("routing: add output mark rule (base)",
			"nft", "add", "rule", "inet", routeNftTable, routeNftOutput,
			"meta", "l4proto", "tcp",
			"ip6", "daddr", "@"+st.setV6,
			"meta", "mark", "set", markHex)
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
	runLogged("routing: add output mark rule "+chain,
		cmd, "-w", "-t", "mangle", "-A", chain, "-p", "tcp",
		"-m", "set", "--match-set", setName, "dst",
		"-j", "MARK", "--set-mark", markHex)
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
	args := []string{"add", "rule", "inet", routeNftTable, chain}
	if v6 {
		args = append(args, "ip6", "daddr", "@"+setName)
	} else {
		args = append(args, "ip", "daddr", "@"+setName)
	}
	args = append(args, "socket", "transparent", "1", "meta", "mark", "set", markHex, "accept")
	runLogged("routing: add divert "+chain, append([]string{"nft"}, args...)...)
}

func addProxyInputAccept(be routeBackend, mark uint32) {
	markHex := fmt.Sprintf("0x%x/0x%x", mark, mark)
	if be.name() == backendNFTables {
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

func removeProxyInputAccept(be routeBackend, mark uint32) {
	markHex := fmt.Sprintf("0x%x/0x%x", mark, mark)
	if be.name() == backendNFTables {
		sig := fmt.Sprintf("meta mark & 0x%x == 0x%x accept", mark, mark)
		for _, c := range [][2]string{{"filter", "input"}, {"fw4", "input"}} {
			table, chain := c[0], c[1]
			out, err := run("nft", "-a", "list", "chain", "inet", table, chain)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(out, "\n") {
				handleIdx := strings.LastIndex(line, "# handle ")
				if handleIdx == -1 {
					continue
				}
				rule := strings.TrimSpace(line[:handleIdx])
				if !strings.Contains(rule, sig) {
					continue
				}
				handle := strings.TrimSpace(line[handleIdx+len("# handle "):])
				if handle == "" {
					continue
				}
				runLogged("routing: delete input accept (proxy)",
					"nft", "delete", "rule", "inet", table, chain, "handle", handle)
			}
		}
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

func addProxyTProxyRuleNft(chain string, v6 bool, setName string, mark uint32, port int, sources []string) {
	markHex := fmt.Sprintf("0x%x", mark)
	portStr := fmt.Sprintf(":%d", port)

	emit := func(src string) {
		args := []string{"add", "rule", "inet", routeNftTable, chain}
		if src != "" {
			args = append(args, "iifname", fmt.Sprintf("%q", src))
		}
		if v6 {
			args = append(args,
				"meta", "l4proto", "tcp",
				"ip6", "daddr", "@"+setName,
				"meta", "mark", "set", markHex,
				"tproxy", "ip6", "to", portStr,
				"accept",
			)
		} else {
			args = append(args,
				"ip", "protocol", "tcp",
				"ip", "daddr", "@"+setName,
				"meta", "mark", "set", markHex,
				"tproxy", "ip", "to", portStr,
				"accept",
			)
		}
		runLogged("routing: add tproxy rule "+chain, append([]string{"nft"}, args...)...)
	}

	if len(sources) == 0 {
		emit("")
		return
	}
	for _, src := range sources {
		emit(src)
	}
}

func addProxyTProxyRuleIpt(v6 bool, chain, setName string, mark uint32, port int, sources []string, legacy bool) {
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
		args := []string{cmd, "-w", "-t", "mangle", "-A", chain, "-p", "tcp"}
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
