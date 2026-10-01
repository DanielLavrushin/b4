package tables

import (
	"fmt"
	"strconv"

	"github.com/daniellavrushin/b4/log"
)

const discoveryChainIPT = "B4_DISCOVERY"

type discoveryIptBackend struct {
	legacy bool
}

func (b *discoveryIptBackend) name() string { return b.ipt4() }

func (b *discoveryIptBackend) ipt4() string {
	if b.legacy {
		return backendIPTablesLegacy
	}
	return backendIPTables
}

func (b *discoveryIptBackend) ipt6() string {
	if b.legacy {
		return backendIP6TablesLegacy
	}
	return backendIP6Tables
}

func (b *discoveryIptBackend) available() bool {
	return hasBinary(b.ipt4()) || hasBinary(b.ipt6())
}

func discoveryQueueAction(queueStart int, threads int) []string {
	if threads > 1 {
		return []string{"--queue-balance", fmt.Sprintf("%d:%d", queueStart, queueStart+threads-1), "--queue-bypass"}
	}
	return []string{"--queue-num", strconv.Itoa(queueStart), "--queue-bypass"}
}

func (b *discoveryIptBackend) apply(flowMark uint, injectedMark uint, queueStart int, threads int) error {
	loadKernelModules()

	for _, bin := range []string{b.ipt4(), b.ipt6()} {
		if !hasBinary(bin) {
			continue
		}
		_, _ = run(bin, "-w", "-t", "mangle", "-N", discoveryChainIPT)
		_, _ = run(bin, "-w", "-t", "mangle", "-F", discoveryChainIPT)

		flow := fmt.Sprintf("0x%x/0xffffffff", flowMark)
		injected := fmt.Sprintf("0x%x/0xffffffff", injectedMark)
		queueSpec := append([]string{bin, "-w", "-t", "mangle", "-A", discoveryChainIPT, "-m", "mark", "--mark", flow, "-j", "NFQUEUE"}, discoveryQueueAction(queueStart, threads)...)

		if _, err := run(bin, "-w", "-t", "mangle", "-A", discoveryChainIPT, "-m", "mark", "--mark", injected, "-j", "ACCEPT"); err != nil {
			return err
		}
		if _, err := run(bin, "-w", "-t", "mangle", "-A", discoveryChainIPT, "-m", "connmark", "--mark", flow, "-j", "MARK", "--set-mark", strconv.FormatUint(uint64(flowMark), 10)); err != nil {
			return err
		}
		if _, err := run(bin, "-w", "-t", "mangle", "-A", discoveryChainIPT, "-m", "mark", "--mark", flow, "-j", "CONNMARK", "--save-mark"); err != nil {
			return err
		}
		if _, err := run(queueSpec...); err != nil {
			return err
		}

		discoveryDelRuleLoop(bin, "OUTPUT", "-j", discoveryChainIPT)
		discoveryDelRuleLoop(bin, "PREROUTING", "-j", discoveryChainIPT)
		discoveryDelRuleLoop(bin, "OUTPUT", "-m", "mark", "--mark", flow, "-j", "ACCEPT")
		discoveryDelRuleLoop(bin, "PREROUTING", "-m", "mark", "--mark", flow, "-j", "ACCEPT")
		discoveryDelRuleLoop(bin, "OUTPUT", "-m", "mark", "--mark", injected, "-j", "ACCEPT")
		discoveryDelRuleLoop(bin, "PREROUTING", "-m", "mark", "--mark", injected, "-j", "ACCEPT")

		if _, err := run(bin, "-w", "-t", "mangle", "-I", "OUTPUT", "1", "-j", discoveryChainIPT); err != nil {
			return err
		}
		if _, err := run(bin, "-w", "-t", "mangle", "-I", "OUTPUT", "2", "-m", "mark", "--mark", flow, "-j", "ACCEPT"); err != nil {
			return err
		}
		if _, err := run(bin, "-w", "-t", "mangle", "-I", "OUTPUT", "3", "-m", "mark", "--mark", injected, "-j", "ACCEPT"); err != nil {
			return err
		}
		if _, err := run(bin, "-w", "-t", "mangle", "-I", "PREROUTING", "1", "-j", discoveryChainIPT); err != nil {
			return err
		}
		if _, err := run(bin, "-w", "-t", "mangle", "-I", "PREROUTING", "2", "-m", "mark", "--mark", flow, "-j", "ACCEPT"); err != nil {
			return err
		}
		if _, err := run(bin, "-w", "-t", "mangle", "-I", "PREROUTING", "3", "-m", "mark", "--mark", injected, "-j", "ACCEPT"); err != nil {
			return err
		}

		discoveryKeepOutOfQueueChain(bin, flow, injected)
	}

	return nil
}

func discoveryKeepOutOfQueueChain(bin, flow, injected string) {
	discoveryDelRuleLoop(bin, iptChainName, "-m", "mark", "--mark", flow, "-j", "RETURN")
	discoveryDelRuleLoop(bin, iptChainName, "-m", "mark", "--mark", injected, "-j", "RETURN")
	for _, mark := range []string{injected, flow} {
		if _, err := run(bin, "-w", "-t", "mangle", "-I", iptChainName, "1", "-m", "mark", "--mark", mark, "-j", "RETURN"); err != nil {
			log.Tracef("Discovery: %s has no %s chain to keep mark %s out of the packet queue: %v", bin, iptChainName, mark, err)
		}
	}
}

func (b *discoveryIptBackend) clear(flowMark uint, injectedMark uint) {
	for _, bin := range []string{b.ipt4(), b.ipt6()} {
		if !hasBinary(bin) {
			continue
		}
		flow := fmt.Sprintf("0x%x/0xffffffff", flowMark)
		injected := fmt.Sprintf("0x%x/0xffffffff", injectedMark)

		discoveryDelRuleLoop(bin, "OUTPUT", "-m", "mark", "--mark", flow, "-j", "ACCEPT")
		discoveryDelRuleLoop(bin, "PREROUTING", "-m", "mark", "--mark", flow, "-j", "ACCEPT")
		discoveryDelRuleLoop(bin, "OUTPUT", "-m", "mark", "--mark", injected, "-j", "ACCEPT")
		discoveryDelRuleLoop(bin, "PREROUTING", "-m", "mark", "--mark", injected, "-j", "ACCEPT")
		discoveryDelRuleLoop(bin, "OUTPUT", "-j", discoveryChainIPT)
		discoveryDelRuleLoop(bin, "PREROUTING", "-j", discoveryChainIPT)
		discoveryDelRuleLoop(bin, iptChainName, "-m", "mark", "--mark", flow, "-j", "RETURN")
		discoveryDelRuleLoop(bin, iptChainName, "-m", "mark", "--mark", injected, "-j", "RETURN")
		_, _ = run(bin, "-w", "-t", "mangle", "-F", discoveryChainIPT)
		_, _ = run(bin, "-w", "-t", "mangle", "-X", discoveryChainIPT)
	}
}

func discoveryDelRuleLoop(bin string, chain string, ruleArgs ...string) {
	args := append([]string{bin, "-w", "-t", "mangle", "-D", chain}, ruleArgs...)
	for range 100 {
		if _, err := run(args...); err != nil {
			return
		}
	}
}
