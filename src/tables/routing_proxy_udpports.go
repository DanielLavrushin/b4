package tables

import (
	"fmt"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const iptMultiportSlots = 15

func proxyUDPPortChunksIpt(ports string) [][]string {
	var entries []string
	for _, entry := range strings.Split(ports, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			entries = append(entries, strings.Replace(entry, "-", ":", 1))
		}
	}
	return chunkPorts(entries, iptMultiportSlots)
}

func proxyUDPPortRuleFailed(chain string, args []string, out string, err error) error {
	return fmt.Errorf("the rule that takes UDP by port into %s was refused, so the set routes no UDP: %w (%s) | cmd=%s",
		chain, err, strings.TrimSpace(out), strings.Join(args, " "))
}

func addProxyUDPPortRuleNft(chain string, v6 bool, ports string, mark uint32, port int, sources []string, original bool) error {
	fam, to := "ipv4", "ip"
	if v6 {
		fam, to = "ipv6", "ip6"
	}
	portSet := "{ " + strings.ReplaceAll(ports, ",", ", ") + " }"
	emit := func(src string) error {
		args := []string{"nft", "add", "rule", "inet", routeNftTable, chain}
		if src != "" {
			args = append(args, "iifname", fmt.Sprintf("%q", src))
		}
		args = append(args, "meta", "nfproto", fam, "meta", "l4proto", "udp", "udp", "dport", portSet)
		if original {
			args = append(args, "ct", "direction", "original")
		}
		args = append(args,
			"meta", "mark", "set", fmt.Sprintf("0x%x", mark),
			"tproxy", to, "to", fmt.Sprintf(":%d", port),
			"accept",
		)
		if out, err := run(args...); err != nil {
			return proxyUDPPortRuleFailed(chain, args, out, err)
		}
		return nil
	}
	if len(sources) == 0 {
		return emit("")
	}
	for _, src := range sources {
		if err := emit(src); err != nil {
			return err
		}
	}
	return nil
}

func addProxyUDPPortRuleIpt(v6 bool, chain, ports string, mark uint32, port int, sources []string, legacy bool, original bool) error {
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
		return nil
	}
	tail := []string{}
	if original {
		tail = append(tail, "-m", "conntrack", "--ctdir", "ORIGINAL")
	}
	tail = append(tail,
		"-j", "TPROXY",
		"--tproxy-mark", fmt.Sprintf("0x%x/0x%x", mark, routeProxyMarkMask),
		"--on-port", fmt.Sprintf("%d", port),
	)
	emit := func(src string) error {
		head := []string{cmd, "-w", "-t", "mangle", "-A", chain, "-p", "udp"}
		if src != "" {
			head = append(head, "-i", src)
		}
		for _, chunk := range proxyUDPPortChunksIpt(ports) {
			multi := append(append(append([]string{}, head...), "-m", "multiport", "--dports", strings.Join(chunk, ",")), tail...)
			out, err := run(multi...)
			if err == nil {
				continue
			}
			log.Tracef("routing: %s took no multiport rule in %s (%s), so each port gets its own rule", cmd, chain, strings.TrimSpace(out))
			for _, entry := range chunk {
				single := append(append(append([]string{}, head...), "--dport", entry), tail...)
				if out, err := run(single...); err != nil {
					return proxyUDPPortRuleFailed(chain, single, out, err)
				}
			}
		}
		return nil
	}
	if len(sources) == 0 {
		return emit("")
	}
	for _, src := range sources {
		if err := emit(src); err != nil {
			return err
		}
	}
	return nil
}

func addProxyUDPPortRules(be routeBackend, cfg *config.Config, st routeState, port int, sources []string) error {
	if st.udpPorts == "" {
		return nil
	}
	if be.name() == backendNFTables {
		if cfg.Queue.IPv4Enabled {
			if err := addProxyUDPPortRuleNft(st.chainPre, false, st.udpPorts, st.mark, port, sources, true); err != nil {
				return err
			}
		}
		if cfg.Queue.IPv6Enabled {
			return addProxyUDPPortRuleNft(st.chainPre, true, st.udpPorts, st.mark, port, sources, true)
		}
		return nil
	}
	legacy := isLegacyIptBackend(be)
	if cfg.Queue.IPv4Enabled {
		if err := addProxyUDPPortRuleIpt(false, st.chainPre, st.udpPorts, st.mark, port, sources, legacy, true); err != nil {
			return err
		}
	}
	if cfg.Queue.IPv6Enabled {
		return addProxyUDPPortRuleIpt(true, st.chainPre, st.udpPorts, st.mark, port, sources, legacy, true)
	}
	return nil
}

func proxyUDPPortFamilies(cfg *config.Config) string {
	switch {
	case cfg.Queue.IPv4Enabled && cfg.Queue.IPv6Enabled:
		return "over IPv4 and IPv6"
	case cfg.Queue.IPv6Enabled:
		return "over IPv6 only, because IPv4 support is off"
	}
	return "over IPv4 only, because IPv6 support is off and their IPv6 UDP to those ports leaves directly"
}
