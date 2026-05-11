package tables

import (
	"fmt"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

const (
	routeNftTable      = "b4_route"
	routeNftPrerouting = "prerouting"
	routeNftOutput     = "output"
	routeNftPostroute  = "postrouting"
)

type routeNftBackend struct{}

func (b *routeNftBackend) name() string    { return backendNFTables }
func (b *routeNftBackend) available() bool { return hasBinary("nft") }

func (b *routeNftBackend) ensureBase() error {
	if err := runEnsure("nft", "add", "table", "inet", routeNftTable); err != nil {
		return fmt.Errorf("ensure table: %w", err)
	}
	if err := runEnsure("nft", "add", "chain", "inet", routeNftTable, routeNftPrerouting,
		"{", "type", "filter", "hook", "prerouting", "priority", "-151", ";", "policy", "accept", ";", "}"); err != nil {
		return fmt.Errorf("ensure prerouting chain: %w", err)
	}
	if err := runEnsure("nft", "add", "chain", "inet", routeNftTable, routeNftOutput,
		"{", "type", "route", "hook", "output", "priority", "-151", ";", "policy", "accept", ";", "}"); err != nil {
		return fmt.Errorf("ensure output chain: %w", err)
	}
	if err := runEnsure("nft", "add", "chain", "inet", routeNftTable, routeNftPostroute,
		"{", "type", "nat", "hook", "postrouting", "priority", "100", ";", "policy", "accept", ";", "}"); err != nil {
		return fmt.Errorf("ensure postrouting chain: %w", err)
	}
	return nil
}

func (b *routeNftBackend) ensureIPSet(name string, v6 bool) error {
	typ := "ipv4_addr"
	if v6 {
		typ = "ipv6_addr"
	}

	out, err := run("nft", "list", "set", "inet", routeNftTable, name)
	if err == nil && out != "" && !strings.Contains(out, "interval") {
		runLogged("routing: recreate set "+name, "nft", "flush", "set", "inet", routeNftTable, name)
		runLogged("routing: delete old set "+name, "nft", "delete", "set", "inet", routeNftTable, name)
	}

	if err := runEnsure("nft", "add", "set", "inet", routeNftTable, name,
		"{", "type", typ, ";", "flags", "interval,timeout", ";", "auto-merge", ";", "}"); err != nil {
		return fmt.Errorf("ensure set %s: %w", name, err)
	}
	return nil
}

func (b *routeNftBackend) addElements(setName string, ips []string, ttlSec int) {
	if len(ips) == 0 {
		return
	}

	const chunkSize = 128

	for i := 0; i < len(ips); i += chunkSize {
		end := i + chunkSize
		if end > len(ips) {
			end = len(ips)
		}
		chunk := ips[i:end]

		args := []string{"nft", "add", "element", "inet", routeNftTable, setName, "{"}
		for idx, ip := range chunk {
			if ttlSec > 0 {
				args = append(args, ip, "timeout", fmt.Sprintf("%ds", ttlSec))
			} else {
				args = append(args, ip)
			}
			if idx < len(chunk)-1 {
				args = append(args, ",")
			}
		}
		args = append(args, "}")
		if out, err := run(args...); err != nil {
			log.Tracef("routing: batch add to %s failed (%v: %s), falling back to individual adds", setName, err, strings.TrimSpace(out))
			for _, ip := range chunk {
				if ttlSec > 0 {
					runLogged("routing: add element "+ip,
						"nft", "add", "element", "inet", routeNftTable, setName,
						"{", ip, "timeout", fmt.Sprintf("%ds", ttlSec), "}")
				} else {
					runLogged("routing: add element "+ip,
						"nft", "add", "element", "inet", routeNftTable, setName,
						"{", ip, "}")
				}
			}
		}
	}
}

func (b *routeNftBackend) ensureChain(chain string, _ bool) error {
	if err := runEnsure("nft", "add", "chain", "inet", routeNftTable, chain); err != nil {
		return fmt.Errorf("ensure chain %s: %w", chain, err)
	}
	return nil
}

func (b *routeNftBackend) flushChain(chain string, _ bool) {
	runLogged("routing: flush chain "+chain, "nft", "flush", "chain", "inet", routeNftTable, chain)
}

func (b *routeNftBackend) deleteChain(chain string, _ bool) {
	runLogged("routing: delete chain "+chain, "nft", "delete", "chain", "inet", routeNftTable, chain)
}

func (b *routeNftBackend) addBypassRule(chain string, mark uint32) {
	markHex := fmt.Sprintf("0x%x", mark)
	runLogged("routing: add bypass rule "+chain,
		"nft", "add", "rule", "inet", routeNftTable, chain,
		"meta", "mark", "&", markHex, "==", markHex, "return")
}

func (b *routeNftBackend) addMarkRule(chain string, v6 bool, setName string, mark uint32, sourceIface string, tagHostConntrack bool) {
	args := []string{"add", "rule", "inet", routeNftTable, chain}
	if sourceIface != "" {
		args = append(args, "iifname", fmt.Sprintf("%q", sourceIface))
	}
	if v6 {
		args = append(args, "ip6", "daddr", "@"+setName, "meta", "mark", "set", fmt.Sprintf("0x%x", mark))
	} else {
		args = append(args, "ip", "daddr", "@"+setName, "meta", "mark", "set", fmt.Sprintf("0x%x", mark))
	}
	if tagHostConntrack {
		args = append(args, "ct", "mark", "set", "ct", "mark", "or", fmt.Sprintf("0x%x", hostRouteCTMark))
	}
	runLogged("routing: add mark rule "+chain, append([]string{"nft"}, args...)...)
}

func nftRouteBaseChain(generic string) string {
	switch generic {
	case "PREROUTING":
		return routeNftPrerouting
	case "OUTPUT":
		return routeNftOutput
	case "POSTROUTING":
		return routeNftPostroute
	default:
		return strings.ToLower(generic)
	}
}

func (b *routeNftBackend) ensureJumpRule(baseChain, targetChain string, _ bool) {
	base := nftRouteBaseChain(baseChain)
	b.deleteJumpRules(baseChain, targetChain, true)
	runLogged("routing: add jump "+base+"->"+targetChain,
		"nft", "add", "rule", "inet", routeNftTable, base, "jump", targetChain)
}

func (b *routeNftBackend) deleteJumpRules(baseChain, targetChain string, _ bool) {
	base := nftRouteBaseChain(baseChain)
	out, err := run("nft", "-a", "list", "chain", "inet", routeNftTable, base)
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "jump "+targetChain) {
			continue
		}
		idx := strings.Index(line, "# handle ")
		if idx < 0 {
			continue
		}
		handle := strings.TrimSpace(line[idx+len("# handle "):])
		if handle == "" {
			continue
		}
		runLogged("routing: delete jump rule handle "+handle,
			"nft", "delete", "rule", "inet", routeNftTable, base, "handle", handle)
	}
}

func (b *routeNftBackend) addMasqueradeRule(chain string, mark uint32, iface string, v6 bool) {
	markHex := fmt.Sprintf("0x%x", mark)
	hostCTMask := fmt.Sprintf("0x%x", hostRouteCTMark)
	nfproto := "ipv4"
	if v6 {
		nfproto = "ipv6"
	}
	runLogged("routing: add masquerade rule",
		"nft", "add", "rule", "inet", routeNftTable, chain,
		"meta", "nfproto", nfproto,
		"meta", "mark", "&", markHex, "==", markHex,
		"ct", "mark", "&", hostCTMask, "==", hostCTMask,
		"oifname", fmt.Sprintf("%q", iface),
		"masquerade",
	)
}

func (b *routeNftBackend) flushIPSet(name string) {
	runLogged("routing: flush set "+name, "nft", "flush", "set", "inet", routeNftTable, name)
}

func (b *routeNftBackend) destroyIPSet(name string) {
	runLogged("routing: delete set "+name, "nft", "delete", "set", "inet", routeNftTable, name)
}

func (b *routeNftBackend) clearAll() {
	runLogged("routing: flush route table", "nft", "flush", "table", "inet", routeNftTable)
	runLogged("routing: delete route table", "nft", "delete", "table", "inet", routeNftTable)
}
