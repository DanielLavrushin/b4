package tables

import (
	"fmt"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type routeIptBackend struct {
	legacy bool
}

func (b *routeIptBackend) name() string { return b.ipt4() }

func (b *routeIptBackend) ipt4() string {
	if b.legacy {
		return backendIPTablesLegacy
	}
	return backendIPTables
}

func (b *routeIptBackend) ipt6() string {
	if b.legacy {
		return backendIP6TablesLegacy
	}
	return backendIP6Tables
}

func (b *routeIptBackend) iptBoth() []string {
	return []string{b.ipt4(), b.ipt6()}
}

func (b *routeIptBackend) iptFor(v6 bool) string {
	if v6 {
		return b.ipt6()
	}
	return b.ipt4()
}

func (b *routeIptBackend) available() bool {
	return hasBinary(b.ipt4()) && hasBinary("ipset")
}

func iptTable(isMangle bool) string {
	if isMangle {
		return "mangle"
	}
	return "nat"
}

func (b *routeIptBackend) ensureBase() error { return nil }

func (b *routeIptBackend) ensureIPSet(name string, v6 bool) error {
	family := "inet"
	if v6 {
		family = "inet6"
	}
	out, err := run("ipset", "create", name, "hash:net", "family", family, "timeout", "3600", "-exist")
	if err != nil {
		return fmt.Errorf("ipset create %s: %v: %s", name, err, strings.TrimSpace(out))
	}
	return nil
}

func (b *routeIptBackend) addElements(setName string, ips []string, ttlSec int) {
	if len(ips) == 0 {
		return
	}
	entries := expandZeroPrefix(ips)
	if ttlSec < 0 {
		ttlSec = 0
	}

	var sb strings.Builder
	sb.Grow(len(entries) * (len(setName) + 32))
	for _, ip := range entries {
		fmt.Fprintf(&sb, "add %s %s timeout %d\n", setName, ip, ttlSec)
	}

	if err := runStdin(sb.String(), "ipset", "restore", "-exist"); err == nil {
		return
	}

	for _, ip := range entries {
		runLogged("routing: ipset add "+ip,
			"ipset", "add", setName, ip, "timeout", fmt.Sprintf("%d", ttlSec), "-exist")
	}
}

func (b *routeIptBackend) ensureChain(chain string, isMangle bool) error {
	table := iptTable(isMangle)
	ipt4 := b.ipt4()
	for _, cmd := range b.iptBoth() {
		if !hasBinary(cmd) {
			continue
		}
		out, err := run(cmd, "-w", "-t", table, "-N", chain)
		if err != nil && !strings.Contains(strings.TrimSpace(out), "already exists") {
			if cmd == ipt4 {
				return fmt.Errorf("%s -N %s in %s: %v: %s", cmd, chain, table, err, strings.TrimSpace(out))
			}
			log.Tracef("routing: %s -N %s in %s failed (non-fatal): %s", cmd, chain, table, strings.TrimSpace(out))
		}
	}
	return nil
}

func (b *routeIptBackend) flushChain(chain string, isMangle bool) {
	table := iptTable(isMangle)
	for _, cmd := range b.iptBoth() {
		if !hasBinary(cmd) {
			continue
		}
		runLogged("routing: flush chain "+chain, cmd, "-w", "-t", table, "-F", chain)
	}
}

func (b *routeIptBackend) deleteChain(chain string, isMangle bool) {
	table := iptTable(isMangle)
	for _, cmd := range b.iptBoth() {
		if !hasBinary(cmd) {
			continue
		}
		runLogged("routing: flush chain "+chain, cmd, "-w", "-t", table, "-F", chain)
		runLogged("routing: delete chain "+chain, cmd, "-w", "-t", table, "-X", chain)
	}
}

func (b *routeIptBackend) addBypassRule(chain string, mark uint32) {
	markHex := fmt.Sprintf("0x%x/0x%x", mark, mark)
	for _, cmd := range b.iptBoth() {
		if !hasBinary(cmd) {
			continue
		}
		runLogged("routing: add bypass rule "+chain,
			cmd, "-w", "-t", "mangle", "-A", chain,
			"-m", "mark", "--mark", markHex, "-j", "RETURN")
	}
}

func (b *routeIptBackend) addClaimedBypassRule(chain string) {
	maskHex := fmt.Sprintf("0x0/0x%x", routeSetMarkMask)
	for _, cmd := range b.iptBoth() {
		if !hasBinary(cmd) {
			continue
		}
		runLogged("routing: add claimed bypass rule "+chain,
			cmd, "-w", "-t", "mangle", "-A", chain,
			"-m", "mark", "!", "--mark", maskHex, "-j", "RETURN")
	}
}

func (b *routeIptBackend) addRouterTrafficGuard(chain string, v6 bool, setName string, mark uint32) bool {
	cmd := b.iptFor(v6)
	if !hasBinary(cmd) {
		return false
	}
	name := routeHashlimitName(chain, v6)
	args := []string{cmd, "-w", "-t", "mangle", "-A", chain,
		"-m", "set", "--match-set", setName, "dst",
		"-m", "conntrack", "--ctstate", "NEW",
		"-m", "hashlimit",
		"--hashlimit-above", fmt.Sprintf("%d/sec", routeRouterTrafficRate),
		"--hashlimit-burst", fmt.Sprintf("%d", routeRouterTrafficRate*2),
		"--hashlimit-name", name,
		"-j", "RETURN"}

	if _, err := run(args...); err == nil {
		return true
	}

	loadHashlimitModule()

	if _, err := run(args...); err != nil {
		log.Tracef("routing: %s takes no router-traffic rate guard on %s (%v); a routing loop through this set would not be capped", chain, cmd, err)
		return false
	}
	return true
}

func routeIptSetMarkArgs(mark uint32) []string {
	return []string{"-j", "MARK", "--set-xmark", fmt.Sprintf("0x%x/0x%x", mark, routeSetMarkMask)}
}

func (b *routeIptBackend) addMarkRule(chain string, v6 bool, setName string, mark uint32, sourceIface string, tagHostConntrack bool) {
	cmd := b.iptFor(v6)
	if !hasBinary(cmd) {
		return
	}
	args := []string{"-w", "-t", "mangle", "-A", chain}
	if sourceIface != "" {
		args = append(args, "-i", sourceIface)
	}
	args = append(args, "-m", "set", "--match-set", setName, "dst")

	markArgs := append(append([]string{}, args...), routeIptSetMarkArgs(mark)...)
	runLogged("routing: add mark rule "+chain, append([]string{cmd}, markArgs...)...)

	if tagHostConntrack {
		ctArgs := append(append([]string{}, args...),
			"-j", "CONNMARK", "--set-xmark",
			fmt.Sprintf("0x%x/0x%x", hostRouteCTMark, hostRouteCTMark))
		runLogged("routing: add ct mark rule "+chain, append([]string{cmd}, ctArgs...)...)
	}
}

func routeIptInjectedMarkArgs(chain, setName string, mark, queueMark uint32, source []string) []string {
	args := []string{
		"-w", "-t", "mangle", "-A", chain,
		"-m", "mark", "--mark", fmt.Sprintf("0x%x/0x%x", queueMark, queueMark),
	}
	args = append(args, source...)
	args = append(args, "-m", "set", "--match-set", setName, "dst")
	return append(args, routeIptSetMarkArgs(mark)...)
}

func (b *routeIptBackend) addInjectedMarkRule(chain string, v6 bool, setName string, mark, queueMark uint32, sources []config.DeviceMatch) {
	cmd := b.iptFor(v6)
	if !hasBinary(cmd) {
		return
	}
	usable := routeInjectedSourcesForFamily(sources, v6)
	if len(usable) == 0 {
		runLogged("routing: add injected mark rule "+chain,
			append([]string{cmd}, routeIptInjectedMarkArgs(chain, setName, mark, queueMark, nil)...)...)
		return
	}
	for _, m := range usable {
		args, ok := iptMatchArgs(m, v6)
		if !ok {
			continue
		}
		runLogged("routing: add injected mark rule "+chain,
			append([]string{cmd}, routeIptInjectedMarkArgs(chain, setName, mark, queueMark, args)...)...)
	}
}

func routeIptJumpArgs(table, baseChain, targetChain string, atTop bool) []string {
	if atTop {
		return []string{"-w", "-t", table, "-I", baseChain, "1", "-j", targetChain}
	}
	return []string{"-w", "-t", table, "-A", baseChain, "-j", targetChain}
}

func (b *routeIptBackend) ensureJumpRule(baseChain, targetChain string, isMangle bool, atTop bool) {
	table := iptTable(isMangle)
	b.deleteJumpRules(baseChain, targetChain, isMangle)
	args := routeIptJumpArgs(table, baseChain, targetChain, atTop)
	for _, cmd := range b.iptBoth() {
		if !hasBinary(cmd) {
			continue
		}
		runLogged("routing: add jump "+baseChain+"->"+targetChain,
			append([]string{cmd}, args...)...)
	}
}

func (b *routeIptBackend) jumpPrepends(atTop bool) bool { return atTop }

func (b *routeIptBackend) deleteJumpRules(baseChain, targetChain string, isMangle bool) {
	table := iptTable(isMangle)
	for _, cmd := range b.iptBoth() {
		if !hasBinary(cmd) {
			continue
		}
		iptDeleteJumpsTo(cmd, table, baseChain, targetChain)
	}
}

func (b *routeIptBackend) addMasqueradeRule(chain string, mark uint32, iface string, v6 bool) {
	cmd := b.iptFor(v6)
	if !hasBinary(cmd) {
		return
	}
	markHex := fmt.Sprintf("0x%x/0x%x", mark, routeSetMarkMask)
	ctMask := fmt.Sprintf("0x%x/0x%x", hostRouteCTMark, hostRouteCTMark)

	runLogged("routing: add masquerade rule",
		cmd, "-w", "-t", "nat", "-A", chain,
		"-m", "mark", "--mark", markHex,
		"-m", "connmark", "--mark", ctMask,
		"-o", iface,
		"-j", "MASQUERADE",
	)
}

func (b *routeIptBackend) addSNATRule(chain, setName, iface, srcIP string, mark uint32, v6 bool) {
	cmd := b.iptFor(v6)
	if !hasBinary(cmd) {
		return
	}
	ctMask := fmt.Sprintf("0x%x/0x%x", hostRouteCTMark, hostRouteCTMark)
	markHex := fmt.Sprintf("0x%x/0x%x", mark, routeSetMarkMask)

	runLogged("routing: add snat rule",
		cmd, "-w", "-t", "nat", "-A", chain,
		"-m", "mark", "--mark", markHex,
		"-m", "set", "--match-set", setName, "dst",
		"-m", "connmark", "--mark", ctMask,
		"-o", iface,
		"-j", "SNAT", "--to-source", srcIP,
	)
}

func (b *routeIptBackend) flushIPSet(name string) {
	if !hasBinary("ipset") {
		return
	}
	runLogged("routing: flush ipset "+name, "ipset", "flush", name)
}

func (b *routeIptBackend) destroyIPSet(name string) {
	if !hasBinary("ipset") {
		return
	}
	runLogged("routing: destroy ipset "+name, "ipset", "destroy", name)
}

func (b *routeIptBackend) clearAll() {
	for _, table := range []string{"mangle", "nat", "filter"} {
		for _, cmd := range b.iptBoth() {
			if !hasBinary(cmd) {
				continue
			}
			for _, parent := range iptBuiltinParents(table) {
				nums := iptJumpLineNumbers(cmd, table, parent, func(t string) bool {
					return strings.HasPrefix(t, "b4r_")
				})
				iptDeleteJumpLines(cmd, table, parent, "routing: cleanup leftover rule", nums)
			}
			out2, _ := run(cmd, "-w", "-t", table, "-L", "-n")
			for _, line := range strings.Split(out2, "\n") {
				if !strings.HasPrefix(line, "Chain b4r_") {
					continue
				}
				chainName := strings.Fields(line)[1]
				runLogged("routing: flush leftover chain", cmd, "-w", "-t", table, "-F", chainName)
				runLogged("routing: delete leftover chain", cmd, "-w", "-t", table, "-X", chainName)
			}
		}
	}

	for _, cmd := range b.iptBoth() {
		if !hasBinary(cmd) {
			continue
		}
		sweepProxyInputAcceptsIpt(cmd)
	}

	// Clean up stale b4r_* ipsets
	if hasBinary("ipset") {
		out, _ := run("ipset", "list", "-n")
		for _, name := range strings.Split(strings.TrimSpace(out), "\n") {
			name = strings.TrimSpace(name)
			if strings.HasPrefix(name, "b4r_") {
				runLogged("routing: flush leftover ipset", "ipset", "flush", name)
				runLogged("routing: destroy leftover ipset", "ipset", "destroy", name)
			}
		}
	}
}
