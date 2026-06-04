package tables

import (
	"fmt"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	routeNftBlockFwd = "block_fwd"
	routeNftBlockOut = "block_out"
)

func routeEnsureBlockRule(be routeBackend, cfg *config.Config, set *config.SetConfig, st routeState, sources []string) error {
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

	switch be.name() {
	case backendNFTables:
		ensureBlockBaseNft()
		if err := be.ensureChain(st.chainPre, true); err != nil {
			return err
		}
		be.flushChain(st.chainPre, true)
		if cfg.Queue.IPv4Enabled {
			addBlockRuleNft(st.chainPre, false, st.setV4, st.blockAction, sources)
		}
		if cfg.Queue.IPv6Enabled {
			addBlockRuleNft(st.chainPre, true, st.setV6, st.blockAction, sources)
		}
		ensureBlockJumpNft(routeNftBlockFwd, st.chainPre)
		ensureBlockJumpNft(routeNftBlockOut, st.chainPre)
	default:
		legacy := isLegacyIptBackend(be)
		ensureBlockChainIpt(st.chainPre, legacy)
		if cfg.Queue.IPv4Enabled {
			addBlockRuleIpt(false, st.chainPre, st.setV4, st.blockAction, sources, legacy)
		}
		if cfg.Queue.IPv6Enabled {
			addBlockRuleIpt(true, st.chainPre, st.setV6, st.blockAction, sources, legacy)
		}
		ensureBlockJumpIpt("FORWARD", st.chainPre, legacy)
		ensureBlockJumpIpt("OUTPUT", st.chainPre, legacy)
	}
	return nil
}

func routeCleanupBlockRule(be routeBackend, st routeState) {
	switch be.name() {
	case backendNFTables:
		deleteNftJumpRules(routeNftTable, routeNftBlockFwd, st.chainPre)
		deleteNftJumpRules(routeNftTable, routeNftBlockOut, st.chainPre)
		be.flushChain(st.chainPre, true)
		be.deleteChain(st.chainPre, true)
	default:
		legacy := isLegacyIptBackend(be)
		deleteBlockJumpIpt("FORWARD", st.chainPre, legacy)
		deleteBlockJumpIpt("OUTPUT", st.chainPre, legacy)
		flushDeleteBlockChainIpt(st.chainPre, legacy)
	}

	be.flushIPSet(st.setV4)
	be.destroyIPSet(st.setV4)
	be.flushIPSet(st.setV6)
	be.destroyIPSet(st.setV6)
}

func ensureBlockBaseNft() {
	runEnsure("nft", "add", "chain", "inet", routeNftTable, routeNftBlockFwd,
		"{", "type", "filter", "hook", "forward", "priority", "-150", ";", "policy", "accept", ";", "}")
	runEnsure("nft", "add", "chain", "inet", routeNftTable, routeNftBlockOut,
		"{", "type", "filter", "hook", "output", "priority", "-150", ";", "policy", "accept", ";", "}")
}

func addBlockRuleNft(chain string, v6 bool, setName, action string, sources []string) {
	daddr := []string{"ip", "daddr", "@" + setName}
	if v6 {
		daddr = []string{"ip6", "daddr", "@" + setName}
	}

	emit := func(src string) {
		base := []string{"nft", "add", "rule", "inet", routeNftTable, chain}
		if src != "" {
			base = append(base, "iifname", fmt.Sprintf("%q", src))
		}

		switch action {
		case config.BlockActionReject:
			args := append(append([]string{}, base...), daddr...)
			args = append(args, "reject", "with", "icmpx", "type", "admin-prohibited")
			runLogged("routing: add block reject "+chain, args...)
		case config.BlockActionRejectRST:
			rst := append(append([]string{}, base...), "meta", "l4proto", "tcp")
			rst = append(rst, daddr...)
			rst = append(rst, "reject", "with", "tcp", "reset")
			runLogged("routing: add block reset "+chain, rst...)
			drop := append(append([]string{}, base...), daddr...)
			drop = append(drop, "drop")
			runLogged("routing: add block drop "+chain, drop...)
		default:
			args := append(append([]string{}, base...), daddr...)
			args = append(args, "drop")
			runLogged("routing: add block drop "+chain, args...)
		}
	}

	if len(sources) == 0 {
		emit("")
		return
	}
	for _, src := range sources {
		emit(src)
	}
}

func ensureBlockJumpNft(base, target string) {
	deleteNftJumpRules(routeNftTable, base, target)
	runLogged("routing: add block jump "+base+"->"+target,
		"nft", "add", "rule", "inet", routeNftTable, base, "jump", target)
}

func iptBlockCmd(v6, legacy bool) string {
	if legacy {
		if v6 {
			return backendIP6TablesLegacy
		}
		return backendIPTablesLegacy
	}
	if v6 {
		return backendIP6Tables
	}
	return backendIPTables
}

func ensureBlockChainIpt(chain string, legacy bool) {
	for _, v6 := range []bool{false, true} {
		cmd := iptBlockCmd(v6, legacy)
		if !hasBinary(cmd) {
			continue
		}
		out, err := run(cmd, "-w", "-t", "filter", "-N", chain)
		if err != nil && !strings.Contains(strings.TrimSpace(out), "already exists") {
			log.Tracef("routing: %s -N %s in filter failed: %s", cmd, chain, strings.TrimSpace(out))
		}
		runLogged("routing: flush block chain "+chain, cmd, "-w", "-t", "filter", "-F", chain)
	}
}

func flushDeleteBlockChainIpt(chain string, legacy bool) {
	for _, v6 := range []bool{false, true} {
		cmd := iptBlockCmd(v6, legacy)
		if !hasBinary(cmd) {
			continue
		}
		runLogged("routing: flush block chain "+chain, cmd, "-w", "-t", "filter", "-F", chain)
		runLogged("routing: delete block chain "+chain, cmd, "-w", "-t", "filter", "-X", chain)
	}
}

func addBlockRuleIpt(v6 bool, chain, setName, action string, sources []string, legacy bool) {
	cmd := iptBlockCmd(v6, legacy)
	if !hasBinary(cmd) {
		return
	}

	emit := func(src string) {
		match := []string{cmd, "-w", "-t", "filter", "-A", chain}
		if src != "" {
			match = append(match, "-i", src)
		}
		match = append(match, "-m", "set", "--match-set", setName, "dst")

		switch action {
		case config.BlockActionReject:
			rw := "icmp-admin-prohibited"
			if v6 {
				rw = "icmp6-adm-prohibited"
			}
			args := append(append([]string{}, match...), "-j", "REJECT", "--reject-with", rw)
			runLogged("routing: add block reject "+chain, args...)
		case config.BlockActionRejectRST:
			rst := []string{cmd, "-w", "-t", "filter", "-A", chain}
			if src != "" {
				rst = append(rst, "-i", src)
			}
			rst = append(rst, "-p", "tcp", "-m", "set", "--match-set", setName, "dst",
				"-j", "REJECT", "--reject-with", "tcp-reset")
			runLogged("routing: add block reset "+chain, rst...)
			drop := append(append([]string{}, match...), "-j", "DROP")
			runLogged("routing: add block drop "+chain, drop...)
		default:
			args := append(append([]string{}, match...), "-j", "DROP")
			runLogged("routing: add block drop "+chain, args...)
		}
	}

	if len(sources) == 0 {
		emit("")
		return
	}
	for _, src := range sources {
		emit(src)
	}
}

func ensureBlockJumpIpt(parent, chain string, legacy bool) {
	for _, v6 := range []bool{false, true} {
		cmd := iptBlockCmd(v6, legacy)
		if !hasBinary(cmd) {
			continue
		}
		for i := 0; i < 100; i++ {
			if _, err := run(cmd, "-w", "-t", "filter", "-D", parent, "-j", chain); err != nil {
				break
			}
		}
		runLogged("routing: add block jump "+parent+"->"+chain,
			cmd, "-w", "-t", "filter", "-A", parent, "-j", chain)
	}
}

func deleteBlockJumpIpt(parent, chain string, legacy bool) {
	for _, v6 := range []bool{false, true} {
		cmd := iptBlockCmd(v6, legacy)
		if !hasBinary(cmd) {
			continue
		}
		for i := 0; i < 100; i++ {
			if _, err := run(cmd, "-w", "-t", "filter", "-D", parent, "-j", chain); err != nil {
				break
			}
		}
	}
}
