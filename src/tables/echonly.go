package tables

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
)

var echOnlyDNSUnusable sync.Map

func echOnlyDNSUsable(ipt string) bool {
	_, unusable := echOnlyDNSUnusable.Load(ipt)
	return !unusable
}

func familyLabel(v6 bool) string {
	if v6 {
		return "IPv6"
	}
	return "IPv4"
}

func echOnlyNfproto(cfg *config.Config) (string, bool) {
	for _, v6 := range []bool{true, false} {
		if cfg.ECHOnlyFamily(v6) {
			if v6 {
				return "ipv6", true
			}
			return "ipv4", false
		}
	}
	return "", false
}

func nftECHOnlyAnswerRule(family string) []string {
	return []string{"meta", "nfproto", family, "udp", "sport", "53", "counter"}
}

func (n *NFTablesManager) addDNSAnswerRules(queriesFromRaw bool) error {
	answers := []string{"udp", "sport", "53", "counter"}
	if queriesFromRaw {
		answers = []string{"udp", "sport", "53", "udp", "dport", "!=", "53", "counter"}
	}
	for _, chain := range []string{"prerouting", "output"} {
		if err := n.addQueueRule(chain, answers...); err != nil {
			return err
		}
	}

	family, v6 := echOnlyNfproto(n.cfg)
	if family == "" {
		return nil
	}
	rule := append(nftECHOnlyAnswerRule(family), strings.Fields(n.buildNFQueueAction())...)
	for _, chain := range []string{"prerouting", "output"} {
		if err := n.addRule(chain, rule...); err != nil {
			return err
		}
	}
	log.Infof("NFTABLES: %s support is off, so %s DNS answers are queued only to remove ECH keys for the sets that ask for it", familyLabel(v6), familyLabel(v6))
	return nil
}

func fieldsFollow(fields []string, first, second string) bool {
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == first && fields[i+1] == second {
			return true
		}
	}
	return false
}

func nftListingQueuesAnswers(listing, family string) bool {
	for _, line := range strings.Split(listing, "\n") {
		f := strings.Fields(line)
		if fieldsFollow(f, "nfproto", family) && fieldsFollow(f, "sport", "53") && slices.Contains(f, "queue") {
			return true
		}
	}
	return false
}

func (manager *IPTablesManager) echOnlyBinaries() []string {
	cfg := manager.cfg
	var ipts []string
	for _, v6 := range []bool{false, true} {
		if !cfg.ECHOnlyFamily(v6) {
			continue
		}
		ipt := manager.iptablesBin()
		if v6 {
			ipt = manager.ip6tablesBin()
		}
		if !hasBinary(ipt) {
			echOnlyDNSUnusable.Store(ipt, true)
			log.Warnf("IPTABLES: %s is missing, so %s DNS answers keep their ECH keys while %s support is off", ipt, familyLabel(v6), familyLabel(v6))
			continue
		}
		if err := manager.checkNFQueueSupport(ipt); err != nil {
			echOnlyDNSUnusable.Store(ipt, true)
			log.Warnf("IPTABLES[%s]: %s DNS answers keep their ECH keys, %v", ipt, familyLabel(v6), err)
			continue
		}
		echOnlyDNSUnusable.Delete(ipt)
		ipts = append(ipts, ipt)
	}
	return ipts
}

func (manager *IPTablesManager) buildECHOnlyDNSManifest(ipt, preChainName string) ([]Chain, []Rule, []Rule) {
	cfg := manager.cfg
	markAccept := iptMarkAccept(cfg.Queue.Mark)
	answers := append(dnsAnswerMatch(false), manager.buildNFQSpec(cfg.Queue.StartNum, cfg.Queue.Threads)...)

	chains := []Chain{{manager: manager, IPT: ipt, Table: "mangle", Name: preChainName}}
	rules := []Rule{
		{manager: manager, IPT: ipt, Table: "mangle", Chain: preChainName, Action: "I", Spec: answers},
		{manager: manager, IPT: ipt, Table: "mangle", Chain: "PREROUTING", Action: "I", Spec: []string{"-j", preChainName}},
		{manager: manager, IPT: ipt, Table: "mangle", Chain: "OUTPUT", Action: "I", Spec: answers},
		{manager: manager, IPT: ipt, Table: "mangle", Chain: "OUTPUT", Action: "I",
			Spec: []string{"-m", "mark", "--mark", markAccept, "-j", "ACCEPT"}},
	}
	var stale []Rule
	if cfg.Queue.Mark != 0 && manager.hasConnmarkSupport(ipt) {
		markMaskHex := fmt.Sprintf("0x%x", cfg.Queue.Mark)
		ownMark := fmt.Sprintf("0x%x/0x%x", cfg.Queue.Mark, cfg.Queue.Mark|engine.ClientMark)
		own, ownStale := manager.ownReplyReturnRules(ipt, preChainName, markAccept, manager.queueRulesMatchDirection(ipt, false), false)
		rules = append(rules,
			Rule{manager: manager, IPT: ipt, Table: "mangle", Chain: "OUTPUT", Action: "I",
				Spec: []string{"-m", "mark", "--mark", ownMark, "-j", "CONNMARK",
					"--save-mark", "--nfmask", markMaskHex, "--ctmask", markMaskHex}},
			own,
		)
		stale = ownStale
	}
	return chains, rules, stale
}

func iptECHOnlyDNSPresent(ipt string) bool {
	pre, err := run(ipt, "-w", "-t", "mangle", "-S", "B4_PREROUTING")
	if err != nil || !strings.Contains(pre, dnsResponsePortMatch) || !strings.Contains(pre, "NFQUEUE") {
		return false
	}
	if _, err := run(ipt, "-w", "-t", "mangle", "-C", "PREROUTING", "-j", "B4_PREROUTING"); err != nil {
		return false
	}
	out, _ := run(ipt, "-w", "-t", "mangle", "-S", "OUTPUT")
	return strings.Contains(out, dnsResponsePortMatch) && strings.Contains(out, "NFQUEUE")
}
