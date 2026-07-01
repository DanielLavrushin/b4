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

	gate := routeSetDeviceGate(cfg, set)
	switch be.name() {
	case backendNFTables:
		if err := ensureBlockBaseNft(); err != nil {
			return err
		}
		if err := be.ensureChain(st.chainPre, true); err != nil {
			return err
		}
		be.flushChain(st.chainPre, true)
		routeAddBlacklistGate(be, "filter", st.chainPre, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled, gate)
		if cfg.Queue.IPv4Enabled {
			addBlockRuleNft(st.chainPre, false, st.setV4, st.blockAction, sources)
		}
		if cfg.Queue.IPv6Enabled {
			addBlockRuleNft(st.chainPre, true, st.setV6, st.blockAction, sources)
		}
		ensureBlockJumpNft(routeNftBlockFwd, st.chainPre, gate)
		ensureBlockJumpNft(routeNftBlockOut, st.chainPre, routeDeviceGate{})
	default:
		legacy := isLegacyIptBackend(be)
		if err := ensureBlockChainIpt(st.chainPre, legacy); err != nil {
			return err
		}
		routeAddBlacklistGate(be, "filter", st.chainPre, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled, gate)
		if cfg.Queue.IPv4Enabled {
			addBlockRuleIpt(false, st.chainPre, st.setV4, st.blockAction, sources, legacy)
		}
		if cfg.Queue.IPv6Enabled {
			addBlockRuleIpt(true, st.chainPre, st.setV6, st.blockAction, sources, legacy)
		}
		ensureBlockJumpIpt("FORWARD", st.chainPre, legacy, gate)
		ensureBlockJumpIpt("OUTPUT", st.chainPre, legacy, routeDeviceGate{})
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

func ensureBlockBaseNft() error {
	if err := runEnsure("nft", "add", "chain", "inet", routeNftTable, routeNftBlockFwd,
		"{", "type", "filter", "hook", "forward", "priority", "-150", ";", "policy", "accept", ";", "}"); err != nil {
		return fmt.Errorf("ensure block forward chain: %w", err)
	}
	if err := runEnsure("nft", "add", "chain", "inet", routeNftTable, routeNftBlockOut,
		"{", "type", "filter", "hook", "output", "priority", "-150", ";", "policy", "accept", ";", "}"); err != nil {
		return fmt.Errorf("ensure block output chain: %w", err)
	}
	return nil
}

func addBlockRuleNft(chain string, v6 bool, setName, action string, sources []string) {
	emit := func(sn, src string) {
		daddr := []string{"ip", "daddr", "@" + sn}
		if v6 {
			daddr = []string{"ip6", "daddr", "@" + sn}
		}

		base := []string{"nft", "add", "rule", "inet", routeNftTable, chain}
		if src != "" {
			base = append(base, "iifname", fmt.Sprintf("%q", src))
		}

		switch action {
		case config.BlockActionReject:
			rst := append(append([]string{}, base...), "meta", "l4proto", "tcp")
			rst = append(rst, daddr...)
			rst = append(rst, "reject", "with", "tcp", "reset")
			runLogged("routing: add block reset "+chain, rst...)
			rej := append(append([]string{}, base...), daddr...)
			rej = append(rej, "reject", "with", "icmpx", "type", "port-unreachable")
			runLogged("routing: add block reject "+chain, rej...)
		default:
			args := append(append([]string{}, base...), daddr...)
			args = append(args, "drop")
			runLogged("routing: add block drop "+chain, args...)
		}
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

func ensureBlockJumpNft(base, target string, gate routeDeviceGate) {
	deleteNftJumpRules(routeNftTable, base, target)
	nftEmitGatedJump(base, target, false, gate)
}

func iptBlockCmd(v6, legacy bool) string {
	return iptCmdFor(v6, legacy)
}

func ensureBlockChainIpt(chain string, legacy bool) error {
	ipt4 := iptBlockCmd(false, legacy)
	for _, v6 := range []bool{false, true} {
		cmd := iptBlockCmd(v6, legacy)
		if !hasBinary(cmd) {
			continue
		}
		out, err := run(cmd, "-w", "-t", "filter", "-N", chain)
		if err != nil && !strings.Contains(strings.TrimSpace(out), "already exists") {
			if cmd == ipt4 {
				return fmt.Errorf("%s -N %s in filter: %v: %s", cmd, chain, err, strings.TrimSpace(out))
			}
			log.Tracef("routing: %s -N %s in filter failed (non-fatal): %s", cmd, chain, strings.TrimSpace(out))
		}
		runLogged("routing: flush block chain "+chain, cmd, "-w", "-t", "filter", "-F", chain)
	}
	return nil
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
			rst := []string{cmd, "-w", "-t", "filter", "-A", chain}
			if src != "" {
				rst = append(rst, "-i", src)
			}
			rst = append(rst, "-p", "tcp", "-m", "set", "--match-set", setName, "dst",
				"-j", "REJECT", "--reject-with", "tcp-reset")
			runLogged("routing: add block reset "+chain, rst...)
			icmpReject := "icmp-port-unreachable"
			if v6 {
				icmpReject = "icmp6-port-unreachable"
			}
			rej := append(append([]string{}, match...), "-j", "REJECT", "--reject-with", icmpReject)
			runLogged("routing: add block reject "+chain, rej...)
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

func ensureBlockJumpIpt(parent, chain string, legacy bool, gate routeDeviceGate) {
	for _, v6 := range []bool{false, true} {
		cmd := iptBlockCmd(v6, legacy)
		if !hasBinary(cmd) {
			continue
		}
		iptDeleteJumpsTo(cmd, "filter", parent, chain)
		iptEmitGatedJump(cmd, "filter", parent, chain, false, gate)
	}
}

func deleteBlockJumpIpt(parent, chain string, legacy bool) {
	for _, v6 := range []bool{false, true} {
		cmd := iptBlockCmd(v6, legacy)
		if !hasBinary(cmd) {
			continue
		}
		iptDeleteJumpsTo(cmd, "filter", parent, chain)
	}
}
