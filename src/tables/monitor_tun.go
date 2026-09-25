package tables

import (
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const tunMonitorFloor = 10 * time.Second

type tunRuleParts struct {
	masq bool
	mss  bool
}

var (
	masqApplied     atomic.Pointer[config.Config]
	mssApplied      atomic.Pointer[config.Config]
	mssAppliedRules atomic.Int64
	mssNftVerbose   atomic.Bool

	rulesRestoreCount atomic.Int64
	rulesLastRestore  atomic.Int64
)

func RulesRestores() (int64, time.Time) {
	n := rulesRestoreCount.Load()
	if n == 0 {
		return 0, time.Time{}
	}
	return n, time.Unix(0, rulesLastRestore.Load())
}

func noteRulesRestore() {
	rulesLastRestore.Store(time.Now().UnixNano())
	rulesRestoreCount.Add(1)
}

func recordMSSApplied(cfg *config.Config, backend string) {
	n := mssClampRuleCount(cfg, backend)
	mssAppliedRules.Store(int64(n))
	if n > 0 {
		mssApplied.Store(cfg)
		return
	}
	mssApplied.Store(nil)
}

func masqueradeActive(cfg *config.Config, backend string) bool {
	if backend == backendNFTables {
		n := NewNFTablesManager(cfg)
		if !n.natTableExists() {
			return false
		}
		out, err := n.runNft("list", "chain", "ip", nftNatTableName, nftNatChainName)
		return err == nil && strings.Contains(out, "masquerade")
	}
	bin := NewIPTablesManager(cfg, backend == backendIPTablesLegacy).iptablesBin()
	postOut, _ := run(bin, "-w", "-t", "nat", "-S", "POSTROUTING")
	masqOut, _ := run(bin, "-w", "-t", "nat", "-S", masqChainName)
	return masqueradeRulesPresent(postOut, masqOut)
}

func mssClampRuleCount(cfg *config.Config, backend string) int {
	if backend == backendNFTables {
		out, err := listMSSNftTable()
		if err != nil {
			return 0
		}
		return strings.Count(out, "maxseg size set")
	}
	count := 0
	for _, bin := range NewIPTablesManager(cfg, backend == backendIPTablesLegacy).mssClampBinaries() {
		for _, chain := range []string{"OUTPUT", "FORWARD", "PREROUTING"} {
			out, err := run(bin, "-w", "-t", "mangle", "-S", chain)
			if err != nil {
				continue
			}
			count += countB4MSSRules(out)
		}
	}
	return count
}

func listMSSNftTable() (string, error) {
	if !mssNftVerbose.Load() {
		out, err := run("nft", "-t", "list", "table", "inet", nftTableName)
		if err == nil {
			return out, nil
		}
		mssNftVerbose.Store(true)
	}
	return run("nft", "list", "table", "inet", nftTableName)
}

func countB4MSSRules(dump string) int {
	n := 0
	for _, line := range strings.Split(dump, "\n") {
		if !strings.Contains(line, "-j TCPMSS --set-mss ") {
			continue
		}
		if strings.Contains(line, "--dport 443 ") || strings.Contains(line, "--sport 443 ") {
			n++
		}
	}
	return n
}

func (m *Monitor) checkTUNRules() bool {
	m.tunLost = tunRuleParts{}
	if c := masqApplied.Load(); c != nil && !masqueradeActive(c, m.backend) {
		log.Tracef("Monitor: masquerade rules missing in TUN mode (POSTROUTING jump or %s chain)", masqChainName)
		m.tunLost.masq = true
	}
	if c := mssApplied.Load(); c != nil {
		if have, want := mssClampRuleCount(c, m.backend), int(mssAppliedRules.Load()); have < want {
			log.Tracef("Monitor: MSS clamp rules missing in TUN mode (%d of %d present)", have, want)
			m.tunLost.mss = true
		}
	}
	return !m.tunLost.masq && !m.tunLost.mss
}

func restoreTUNRules(backend string, lost tunRuleParts) error {
	IPTablesLockBudgetReset()
	var errs []error
	if c := masqApplied.Load(); lost.masq && c != nil {
		if err := applyMasqueradeFor(c, backend); err != nil {
			errs = append(errs, err)
		}
	}
	if c := mssApplied.Load(); lost.mss && c != nil {
		if err := restoreMSSClamp(c, backend); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func restoreMSSClamp(cfg *config.Config, backend string) error {
	if backend == backendNFTables {
		NewNFTablesManager(cfg).ClearMSSClamp()
	}
	err := applyMSSClampFor(cfg, backend)
	if n := int64(mssClampRuleCount(cfg, backend)); err == nil || n > mssAppliedRules.Load() {
		mssAppliedRules.Store(n)
	}
	return err
}
