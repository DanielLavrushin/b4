package tables

import (
	"fmt"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	routeNftTable      = "b4_route"
	routeNftPrerouting = "prerouting"
	routeNftOutput     = "output"
	routeNftPostroute  = "postrouting"

	routeNftPostrouteEarly = "postrouting_early"
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

func routeNftDynSet(name string) string {
	return name + "_d"
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

	dyn := routeNftDynSet(name)
	dout, derr := run("nft", "list", "set", "inet", routeNftTable, dyn)
	if derr == nil && dout != "" && (!strings.Contains(dout, "timeout") || strings.Contains(dout, "interval") || strings.Contains(dout, "auto-merge")) {
		runLogged("routing: recreate set "+dyn, "nft", "flush", "set", "inet", routeNftTable, dyn)
		runLogged("routing: delete old set "+dyn, "nft", "delete", "set", "inet", routeNftTable, dyn)
	}
	if err := runEnsure("nft", "add", "set", "inet", routeNftTable, dyn,
		"{", "type", typ, ";", "flags", "timeout", ";", "}"); err != nil {
		return fmt.Errorf("ensure set %s: %w", dyn, err)
	}
	return nil
}

func (b *routeNftBackend) addElements(setName string, ips []string, ttlSec int) {
	if len(ips) == 0 {
		return
	}

	if ttlSec > 0 {
		setName = routeNftDynSet(setName)
	} else {
		ips = expandZeroPrefix(ips)
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

func (b *routeNftBackend) addEgressLoopGuard(chain, iface string) bool {
	if iface == "" {
		return true
	}
	return runLogged("routing: add egress loop guard "+chain,
		"nft", "add", "rule", "inet", routeNftTable, chain,
		"iifname", fmt.Sprintf("%q", iface), "return")
}

func (b *routeNftBackend) sharesFamilies() bool { return true }

func (b *routeNftBackend) addMarkRestoreRule(chain string, v6 bool, sourceIface string, mark uint32) {
	args := []string{"add", "rule", "inet", routeNftTable, chain}
	if sourceIface != "" {
		args = append(args, "iifname", fmt.Sprintf("%q", sourceIface))
	}
	args = append(args,
		"ct", "mark", "&", fmt.Sprintf("0x%x", hostRouteCTMark), "==", fmt.Sprintf("0x%x", hostRouteCTMark),
		"ct", "mark", "&", fmt.Sprintf("0x%x", routeSetMarkMask), "==", fmt.Sprintf("0x%x", mark),
		"ct", "direction", "original",
		"ct", "state", "!=", "new")
	args = append(args, routeNftSetMarkArgs(mark)...)
	runLogged("routing: add mark restore rule "+chain, append([]string{"nft"}, args...)...)
}

func (b *routeNftBackend) addMarkFallbackRule(chain string, v6 bool, setName string, mark uint32, sourceIface string) {
	sn := setName
	args := []string{"add", "rule", "inet", routeNftTable, chain}
	if sourceIface != "" {
		args = append(args, "iifname", fmt.Sprintf("%q", sourceIface))
	}
	args = append(args, "meta", "mark", "&", fmt.Sprintf("0x%x", routeSetMarkMask), "==", "0x0")
	if v6 {
		args = append(args, "ip6", "daddr", "@"+sn)
	} else {
		args = append(args, "ip", "daddr", "@"+sn)
	}
	args = append(args, routeNftSetMarkArgs(mark)...)
	runLogged("routing: add mark fallback rule "+chain, append([]string{"nft"}, args...)...)
}

func (b *routeNftBackend) addClaimedBypassRule(chain string) {
	maskHex := fmt.Sprintf("0x%x", routeSetMarkMask)
	runLogged("routing: add claimed bypass rule "+chain,
		"nft", "add", "rule", "inet", routeNftTable, chain,
		"meta", "mark", "&", maskHex, "!=", "0x0", "return")
}

func (b *routeNftBackend) addRouterTrafficGuard(chain string, v6 bool, setName string, mark uint32) bool {
	family := "ip"
	if v6 {
		family = "ip6"
	}
	added := true
	for _, sn := range []string{setName, routeNftDynSet(setName)} {
		if _, err := run("nft", "add", "rule", "inet", routeNftTable, chain,
			"ct", "state", "new",
			family, "daddr", "@"+sn,
			"limit", "rate", "over", fmt.Sprintf("%d/second", routeRouterTrafficRate),
			"burst", fmt.Sprintf("%d", routeRouterTrafficRate*2), "packets",
			"counter", "return"); err != nil {
			log.Tracef("routing: %s takes no router-traffic rate guard on @%s (%v); a routing loop through this set would not be capped", chain, sn, err)
			added = false
		}
	}
	return added
}

func routeNftSweepBaseOutputBypasses() {
	out, err := run("nft", "-a", "list", "chain", "inet", routeNftTable, routeNftOutput)
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		m, verb, ok := nftParseMarkRule(line)
		if !ok || verb != "return" {
			continue
		}
		handle := nftHandleFromLine(line)
		if handle == "" {
			continue
		}
		runLogged(fmt.Sprintf("routing: drop base output bypass on 0x%x", m),
			"nft", "delete", "rule", "inet", routeNftTable, routeNftOutput, "handle", handle)
	}
}

func routeNftSetMarkArgs(mark uint32) []string {
	return []string{"meta", "mark", "set", "meta", "mark", "&",
		fmt.Sprintf("0x%x", ^routeSetMarkMask), "or", fmt.Sprintf("0x%x", mark)}
}

func (b *routeNftBackend) addMarkRule(chain string, v6 bool, setName string, mark uint32, sourceIface string, tagHostConntrack bool) {
	emit := func(sn string) {
		args := []string{"add", "rule", "inet", routeNftTable, chain}
		if sourceIface != "" {
			args = append(args, "iifname", fmt.Sprintf("%q", sourceIface))
		}
		args = append(args, "ct", "state", "new")
		if v6 {
			args = append(args, "ip6", "daddr", "@"+sn)
			args = append(args, routeNftSetMarkArgs(mark)...)
		} else {
			args = append(args, "ip", "daddr", "@"+sn)
			args = append(args, routeNftSetMarkArgs(mark)...)
		}
		if tagHostConntrack {
			args = append(args, "ct", "mark", "set", "ct", "mark", "or", fmt.Sprintf("0x%x", hostRouteCTMark))
		}
		args = append(args, "ct", "mark", "set", "ct", "mark", "&", fmt.Sprintf("0x%x", ^routeSetMarkMask),
			"or", fmt.Sprintf("0x%x", mark))
		runLogged("routing: add mark rule "+chain, append([]string{"nft"}, args...)...)
	}
	emit(setName)
	emit(routeNftDynSet(setName))
}

func routeNftInjectedMarkArgs(chain string, v6 bool, setName string, mark, queueMark uint32, source []string) []string {
	queueHex := fmt.Sprintf("0x%x", queueMark)
	family := "ip"
	if v6 {
		family = "ip6"
	}
	args := []string{
		"add", "rule", "inet", routeNftTable, chain,
		"meta", "mark", "&", queueHex, "==", queueHex,
	}
	args = append(args, source...)
	args = append(args, family, "daddr", "@"+setName)
	return append(args, routeNftSetMarkArgs(mark)...)
}

func (b *routeNftBackend) addInjectedMarkRule(chain string, v6 bool, setName string, mark, queueMark uint32, sources []config.DeviceMatch) {
	usable := routeInjectedSourcesForFamily(sources, v6)
	for _, sn := range []string{setName, routeNftDynSet(setName)} {
		if len(usable) == 0 {
			runLogged("routing: add injected mark rule "+chain,
				append([]string{"nft"}, routeNftInjectedMarkArgs(chain, v6, sn, mark, queueMark, nil)...)...)
			continue
		}
		for _, m := range usable {
			runLogged("routing: add injected mark rule "+chain,
				append([]string{"nft"}, routeNftInjectedMarkArgs(chain, v6, sn, mark, queueMark, nftMatchArgs(m))...)...)
		}
	}
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

func (b *routeNftBackend) ensureEarlyPostrouteChain() bool {
	if err := runEnsure("nft", "add", "chain", "inet", routeNftTable, routeNftPostrouteEarly,
		"{", "type", "nat", "hook", "postrouting", "priority", "90", ";", "policy", "accept", ";", "}"); err != nil {
		log.Warnf("Routing: cannot create an early srcnat chain (%v); the set's source rewrite stays at the same priority as any other masquerade on this box and may lose to it", err)
		return false
	}
	return true
}

func (b *routeNftBackend) ensureJumpRule(baseChain, targetChain string, _ bool, atTop bool) {
	base := nftRouteBaseChain(baseChain)
	if atTop && base == routeNftPostroute && b.ensureEarlyPostrouteChain() {
		base = routeNftPostrouteEarly
	}
	b.deleteJumpRules(baseChain, targetChain, true)
	runLogged("routing: add jump "+base+"->"+targetChain,
		"nft", "add", "rule", "inet", routeNftTable, base, "jump", targetChain)
}

func (b *routeNftBackend) jumpPrepends(bool) bool { return false }

func (b *routeNftBackend) deleteJumpRules(baseChain, targetChain string, _ bool) {
	base := nftRouteBaseChain(baseChain)
	b.deleteJumpRulesFrom(base, targetChain)
	if base == routeNftPostroute {
		b.deleteJumpRulesFrom(routeNftPostrouteEarly, targetChain)
	}
}

func (b *routeNftBackend) deleteJumpRulesFrom(base, targetChain string) {
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
	maskHex := fmt.Sprintf("0x%x", routeSetMarkMask)
	hostCTMask := fmt.Sprintf("0x%x", hostRouteCTMark)
	nfproto := "ipv4"
	if v6 {
		nfproto = "ipv6"
	}
	runLogged("routing: add masquerade rule",
		"nft", "add", "rule", "inet", routeNftTable, chain,
		"meta", "nfproto", nfproto,
		"meta", "mark", "&", maskHex, "==", markHex,
		"ct", "mark", "&", hostCTMask, "==", hostCTMask,
		"oifname", fmt.Sprintf("%q", iface),
		"masquerade",
	)
}

func (b *routeNftBackend) addSNATRule(chain, setName, iface, srcIP string, mark uint32, v6 bool) {
	hostCTMask := fmt.Sprintf("0x%x", hostRouteCTMark)
	markHex := fmt.Sprintf("0x%x", mark)
	maskHex := fmt.Sprintf("0x%x", routeSetMarkMask)
	nfproto := "ipv4"
	family := "ip"
	if v6 {
		nfproto = "ipv6"
		family = "ip6"
	}
	for _, sn := range []string{setName, routeNftDynSet(setName)} {
		runLogged("routing: add snat rule",
			"nft", "add", "rule", "inet", routeNftTable, chain,
			"meta", "nfproto", nfproto,
			"meta", "mark", "&", maskHex, "==", markHex,
			family, "daddr", "@"+sn,
			"ct", "mark", "&", hostCTMask, "==", hostCTMask,
			"oifname", fmt.Sprintf("%q", iface),
			"snat", family, "to", srcIP,
		)
	}
}

func (b *routeNftBackend) flushIPSet(name string) {
	runLogged("routing: flush set "+name, "nft", "flush", "set", "inet", routeNftTable, name)
	dyn := routeNftDynSet(name)
	runLogged("routing: flush set "+dyn, "nft", "flush", "set", "inet", routeNftTable, dyn)
}

func (b *routeNftBackend) destroyIPSet(name string) {
	runLogged("routing: delete set "+name, "nft", "delete", "set", "inet", routeNftTable, name)
	dyn := routeNftDynSet(name)
	runLogged("routing: delete set "+dyn, "nft", "delete", "set", "inet", routeNftTable, dyn)
}

func (b *routeNftBackend) clearAll() {
	sweepProxyInputAcceptsNft()
	runLogged("routing: flush route table", "nft", "flush", "table", "inet", routeNftTable)
	runLogged("routing: delete route table", "nft", "delete", "table", "inet", routeNftTable)
}
