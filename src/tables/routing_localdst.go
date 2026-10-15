package tables

import (
	"maps"
	"net"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/log"
)

type routeLocalListKey struct {
	chain string
	v6    bool
}

var (
	routeLocalGuardMu       sync.Mutex
	routeLocalGuardRejected = map[string]bool{}
	routeLocalGuardWarned   = map[string]bool{}
	routeLocalGuardListed   = map[routeLocalListKey]string{}
	routeLocalGuardKmodNft  sync.Once
	routeLocalGuardKmodIpt  sync.Once
	routeHostAddrs          = net.InterfaceAddrs
)

func routeLocalGuardIsRejected(tool string) bool {
	routeLocalGuardMu.Lock()
	defer routeLocalGuardMu.Unlock()
	return routeLocalGuardRejected[tool]
}

func routeLocalGuardReject(tool, module, out string) {
	kmodNoteRejected(module, tool, out)
	routeLocalGuardMu.Lock()
	routeLocalGuardRejected[tool] = true
	warned := routeLocalGuardWarned[tool]
	routeLocalGuardWarned[tool] = true
	routeLocalGuardMu.Unlock()
	if warned {
		return
	}
	msg := strings.TrimSpace(out)
	if msg == "" {
		msg = "no error text"
	}
	log.Warnf("Routing: %s rejected the address-type match (%s), so the sets that must leave traffic addressed to this router alone match its addresses from a list instead; the tables monitor rebuilds those sets when the addresses change. %s",
		tool, msg, kmodMissingHint([]string{module}))
}

func routeLocalGuardForget() {
	routeLocalGuardMu.Lock()
	routeLocalGuardRejected = map[string]bool{}
	routeLocalGuardWarned = map[string]bool{}
	routeLocalGuardListed = map[routeLocalListKey]string{}
	routeLocalGuardMu.Unlock()
}

func routeLocalGuardForgetLists() {
	routeLocalGuardMu.Lock()
	routeLocalGuardListed = map[routeLocalListKey]string{}
	routeLocalGuardMu.Unlock()
}

func routeLocalGuardList(chain string, v6 bool) []string {
	list := routeLocalAddressList(v6)
	routeLocalGuardMu.Lock()
	routeLocalGuardListed[routeLocalListKey{chain: chain, v6: v6}] = strings.Join(list, ",")
	routeLocalGuardMu.Unlock()
	return list
}

func routeLocalGuardListsMoved() bool {
	routeLocalGuardMu.Lock()
	listed := maps.Clone(routeLocalGuardListed)
	routeLocalGuardMu.Unlock()
	current := make(map[bool]string, 2)
	for key, was := range listed {
		now, ok := current[key.v6]
		if !ok {
			list, err := routeLocalAddresses(key.v6)
			if err != nil {
				return false
			}
			now = strings.Join(list, ",")
			current[key.v6] = now
		}
		if now != was {
			return true
		}
	}
	return false
}

func routeAddLocalDestinationGuard(be routeBackend, chain string, ipv4, ipv6 bool) {
	if be.name() == backendNFTables {
		routeAddLocalDestinationGuardNft(chain, ipv4, ipv6)
		return
	}
	legacy := isLegacyIptBackend(be)
	if ipv4 {
		routeAddLocalDestinationGuardIpt(iptCmdFor(false, legacy), chain, false)
	}
	if ipv6 {
		routeAddLocalDestinationGuardIpt(iptCmdFor(true, legacy), chain, true)
	}
}

func routeAddLocalDestinationGuardNft(chain string, ipv4, ipv6 bool) {
	if !routeLocalGuardIsRejected("nft") {
		routeLocalGuardKmodNft.Do(func() { loadKernelModuleList("nft_fib_inet") })
		out, err := run("nft", "add", "rule", "inet", routeNftTable, chain,
			"fib", "daddr", "type", "{", "local", ",", "broadcast", ",", "multicast", "}", "return")
		if err == nil {
			if ipv6 {
				runLogged("routing: local destination guard "+chain,
					"nft", "add", "rule", "inet", routeNftTable, chain, "ip6", "daddr", "fe80::/10", "return")
			}
			return
		}
		routeLocalGuardReject("nft", "nft_fib_inet", out)
	}
	if ipv4 {
		routeAddLocalDestinationFallbackNft(chain, false)
	}
	if ipv6 {
		routeAddLocalDestinationFallbackNft(chain, true)
	}
}

func routeAddLocalDestinationGuardIpt(cmd, chain string, v6 bool) {
	if !hasBinary(cmd) {
		return
	}
	if !routeLocalGuardIsRejected(cmd) {
		routeLocalGuardKmodIpt.Do(func() { loadKernelModuleList("xt_addrtype") })
		types := "LOCAL,BROADCAST,MULTICAST"
		if v6 {
			types = "LOCAL"
		}
		out, err := run(cmd, "-w", "-t", "mangle", "-A", chain, "-m", "addrtype", "--dst-type", types, "-j", "RETURN")
		if err == nil {
			if v6 {
				for _, prefix := range []string{"ff00::/8", "fe80::/10"} {
					runLogged("routing: local destination guard "+chain,
						cmd, "-w", "-t", "mangle", "-A", chain, "-d", prefix, "-j", "RETURN")
				}
			}
			return
		}
		routeLocalGuardReject(cmd, "xt_addrtype", out)
	}
	routeAddLocalDestinationFallbackIpt(cmd, chain, v6)
}

func routeAddLocalDestinationFallbackIpt(cmd, chain string, v6 bool) {
	for _, prefix := range routeLocalGuardList(chain, v6) {
		runLogged("routing: local destination guard "+chain,
			cmd, "-w", "-t", "mangle", "-A", chain, "-d", prefix, "-j", "RETURN")
	}
}

func routeAddLocalDestinationFallbackNft(chain string, v6 bool) {
	field := "ip"
	if v6 {
		field = "ip6"
	}
	for _, prefix := range routeLocalGuardList(chain, v6) {
		runLogged("routing: local destination guard "+chain,
			"nft", "add", "rule", "inet", routeNftTable, chain, field, "daddr", prefix, "return")
	}
}

func routeLocalAddressList(v6 bool) []string {
	list, _ := routeLocalAddresses(v6)
	return list
}

func routeLocalAddresses(v6 bool) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	add := func(prefix string) {
		if _, ok := seen[prefix]; ok {
			return
		}
		seen[prefix] = struct{}{}
		out = append(out, prefix)
	}
	if v6 {
		add("::1/128")
		add("ff00::/8")
		add("fe80::/10")
	} else {
		add("127.0.0.0/8")
		add("224.0.0.0/4")
		add("255.255.255.255/32")
	}
	addrs, err := routeHostAddrs()
	if err != nil {
		return out, err
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn == nil || ipn.IP == nil || ipn.IP.IsLoopback() {
			continue
		}
		ip4 := ipn.IP.To4()
		if v6 {
			if ip4 != nil || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			add(ipn.IP.String() + "/128")
			continue
		}
		if ip4 == nil {
			continue
		}
		add(ip4.String() + "/32")
		ones, bits := ipn.Mask.Size()
		if bits != 32 || ones >= 31 || len(ipn.Mask) != net.IPv4len {
			continue
		}
		bcast := make(net.IP, net.IPv4len)
		for i := range bcast {
			bcast[i] = ip4[i] | ^ipn.Mask[i]
		}
		add(bcast.String() + "/32")
	}
	return out, nil
}
