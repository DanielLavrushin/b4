package tables

import (
	"strings"
)

type iptChainInfo struct {
	refs  int
	rules []string
}

// parseIptDump reads one `iptables -L -n -v -x` listing into a chain map. One
// dump answers every question the routing check asks, and asking the kernel for a
// single chain costs the same as asking for the whole table, so the only thing
// worth economising is the number of times b4 asks.
func parseIptDump(out string) map[string]iptChainInfo {
	chains := make(map[string]iptChainInfo)
	cur := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Chain ") {
			fields := strings.Fields(line[len("Chain "):])
			if len(fields) == 0 {
				cur = ""
				continue
			}
			cur = fields[0]
			info := chains[cur]
			for i, f := range fields {
				if f == "references)" && i > 0 {
					info.refs = atoiSafe(strings.TrimPrefix(fields[i-1], "("))
				}
			}
			chains[cur] = info
			continue
		}
		if cur == "" || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] == "pkts" {
			continue
		}
		if _, ok := parseUintField(fields[0]); !ok {
			continue
		}
		info := chains[cur]
		info.rules = append(info.rules, line)
		chains[cur] = info
	}
	return chains
}

func atoiSafe(s string) int {
	n, ok := parseUintField(s)
	if !ok {
		return 0
	}
	return int(n)
}

func parseUintField(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	var n uint64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + uint64(c-'0')
	}
	return n, true
}

// iptDumpJumpsTo reports whether the parent chain itself carries a jump to target.
// The reference count alone cannot say which chain holds the jump.
func iptDumpJumpsTo(chains map[string]iptChainInfo, parent, target string) bool {
	for _, line := range chains[parent].rules {
		fields := strings.Fields(line)
		if len(fields) > 2 && fields[2] == target {
			return true
		}
	}
	return false
}

// iptDumpReturnsOn reports whether the chain returns on the given mark, whatever
// spelling the listing uses for it.
func iptDumpReturnsOn(chains map[string]iptChainInfo, chain string, mark uint32) bool {
	for _, line := range chains[chain].rules {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[2] != "RETURN" {
			continue
		}
		if m, ok := iptMarkFromRule(line); ok && m == mark {
			return true
		}
	}
	return false
}
