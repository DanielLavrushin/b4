package tables

import (
	"fmt"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
)

const masqChainName = "B4_MASQ"

func masqueradeInterfaces(cfg *config.Config) []string {
	raw := cfg.System.Tables.Masquerade.Interfaces
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, iface := range raw {
		iface = strings.TrimSpace(iface)
		if iface == "" {
			continue
		}
		if _, dup := seen[iface]; dup {
			continue
		}
		seen[iface] = struct{}{}
		out = append(out, iface)
	}
	return out
}

func masqueradeSpecs(cfg *config.Config) [][]string {
	ifaces := masqueradeInterfaces(cfg)
	if len(ifaces) == 0 {
		return [][]string{{"-j", "MASQUERADE"}}
	}
	specs := make([][]string, 0, len(ifaces))
	for _, iface := range ifaces {
		specs = append(specs, []string{"-o", iface, "-j", "MASQUERADE"})
	}
	return specs
}

func masqueradeLogLabel(cfg *config.Config) string {
	ifaces := masqueradeInterfaces(cfg)
	if len(ifaces) == 0 {
		return "all"
	}
	return strings.Join(ifaces, ", ")
}

func (im *IPTablesManager) ApplyMasquerade() error {
	if !im.cfg.System.Tables.Masquerade.Enabled {
		return nil
	}

	iptBin := im.iptablesBin()

	im.ensureChain(iptBin, "nat", masqChainName)
	if _, err := run(iptBin, "-w", "-t", "nat", "-F", masqChainName); err != nil {
		return fmt.Errorf("failed to flush masquerade chain: %w", err)
	}

	for _, masqSpec := range masqueradeSpecs(im.cfg) {
		if _, err := run(append([]string{iptBin, "-w", "-t", "nat", "-A", masqChainName}, masqSpec...)...); err != nil {
			return fmt.Errorf("failed to add masquerade rule (%s): %w", strings.Join(masqSpec, " "), err)
		}
	}

	returnSpec := []string{"-m", "mark", "--mark", im.masqClientMark(), "-j", "RETURN"}
	if !im.existsRule(iptBin, "nat", "POSTROUTING", returnSpec) {
		if _, err := run(append([]string{iptBin, "-w", "-t", "nat", "-I", "POSTROUTING"}, returnSpec...)...); err != nil {
			return fmt.Errorf("failed to add masquerade mark-bypass rule: %w", err)
		}
	}

	jumpSpec := []string{"-j", masqChainName}
	if !im.existsRule(iptBin, "nat", "POSTROUTING", jumpSpec) {
		if _, err := run(append([]string{iptBin, "-w", "-t", "nat", "-A", "POSTROUTING"}, jumpSpec...)...); err != nil {
			return fmt.Errorf("failed to add masquerade jump rule (%s): %w", strings.Join(jumpSpec, " "), err)
		}
	}

	log.Infof("IPTABLES: masquerade enabled (interfaces: %s)", masqueradeLogLabel(im.cfg))
	return nil
}

func masqueradeRulesPresent(postroutingOut, masqChainOut string) bool {
	return strings.Contains(postroutingOut, masqChainName) &&
		strings.Contains(masqChainOut, "MASQUERADE")
}

func (manager *IPTablesManager) buildMasqueradeManifest(ipt string) ([]Chain, []Rule) {
	markClient := fmt.Sprintf("0x%x/0x%x", engine.ClientMark, engine.ClientMark)
	chains := []Chain{{manager: manager, IPT: ipt, Table: "nat", Name: masqChainName}}
	rules := []Rule{
		{manager: manager, IPT: ipt, Table: "nat", Chain: "POSTROUTING", Action: "I", Spec: []string{"-m", "mark", "--mark", markClient, "-j", "RETURN"}},
		{manager: manager, IPT: ipt, Table: "nat", Chain: "POSTROUTING", Action: "A", Spec: []string{"-j", masqChainName}},
	}
	for _, masqSpec := range masqueradeSpecs(manager.cfg) {
		rules = append(rules, Rule{manager: manager, IPT: ipt, Table: "nat", Chain: masqChainName, Action: "A", Spec: masqSpec})
	}
	return chains, rules
}

func (im *IPTablesManager) masqueradeBinaries() []string {
	var bins []string
	iptBin := im.iptablesBin()
	ip6tBin := im.ip6tablesBin()
	if im.cfg.Queue.IPv4Enabled && hasBinary(iptBin) {
		bins = append(bins, iptBin)
	}
	if im.cfg.Queue.IPv6Enabled && hasBinary(ip6tBin) {
		bins = append(bins, ip6tBin)
	}
	return bins
}

func (im *IPTablesManager) teardownMasqueradeChain(ipt string) {
	im.delAll(ipt, "nat", "POSTROUTING", []string{"-j", masqChainName})
	if im.existsChain(ipt, "nat", masqChainName) {
		_, _ = run(ipt, "-w", "-t", "nat", "-F", masqChainName)
		_, _ = run(ipt, "-w", "-t", "nat", "-X", masqChainName)
	}
}

func (im *IPTablesManager) ClearMasquerade() {
	iptBin := im.iptablesBin()
	im.teardownMasqueradeChain(iptBin)

	specs := append(masqueradeSpecs(im.cfg), []string{"-j", "MASQUERADE"})
	for _, spec := range specs {
		im.delAll(iptBin, "nat", "POSTROUTING", spec)
	}
	for _, mk := range []string{im.masqClientMark(), im.masqMarkAccept()} {
		im.delAll(iptBin, "nat", "POSTROUTING", []string{"-m", "mark", "--mark", mk, "-j", "RETURN"})
	}
}

func (im *IPTablesManager) masqMarkAccept() string {
	if im.cfg.Queue.Mark == 0 {
		return "0x8000/0x8000"
	}
	return fmt.Sprintf("0x%x/0x%x", im.cfg.Queue.Mark, im.cfg.Queue.Mark)
}

func (im *IPTablesManager) masqClientMark() string {
	return fmt.Sprintf("0x%x/0x%x", engine.ClientMark, engine.ClientMark)
}
