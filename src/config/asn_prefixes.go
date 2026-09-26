package config

import (
	"cmp"
	"math"
	"net/netip"
	"slices"
	"strings"
)

var asnBogonsV4 = mustParsePrefixes(
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/3",
)

var asnGlobalV6 = netip.MustParsePrefix("2000::/3")

var asnBogonsV6 = mustParsePrefixes(
	"2001:2::/48",
	"2001:10::/28",
	"2001:20::/28",
	"2001:db8::/32",
	"3fff::/20",
)

func mustParsePrefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, s := range list {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

func IsBogonPrefix(p netip.Prefix) bool {
	if !p.IsValid() || p.Bits() == 0 {
		return true
	}
	p = p.Masked()
	if p.Addr().Is4() {
		for _, b := range asnBogonsV4 {
			if b.Overlaps(p) {
				return true
			}
		}
		return false
	}
	if p.Bits() < asnGlobalV6.Bits() || !asnGlobalV6.Contains(p.Addr()) {
		return true
	}
	for _, b := range asnBogonsV6 {
		if b.Overlaps(p) {
			return true
		}
	}
	return false
}

func parseASNPrefix(raw string) (netip.Prefix, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return netip.Prefix{}, false
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		addr, aerr := netip.ParseAddr(s)
		if aerr != nil {
			return netip.Prefix{}, false
		}
		addr = addr.WithZone("")
		p = netip.PrefixFrom(addr, addr.BitLen())
	}
	p = p.Masked()
	if IsBogonPrefix(p) {
		return netip.Prefix{}, false
	}
	return p, true
}

func SanitizeASNPrefixes(raw []string) []string {
	parsed := make([]netip.Prefix, 0, len(raw))
	for _, entry := range raw {
		if p, ok := parseASNPrefix(entry); ok {
			parsed = append(parsed, p)
		}
	}
	collapsed := CollapsePrefixes(parsed)
	out := make([]string, len(collapsed))
	for i, p := range collapsed {
		out[i] = p.String()
	}
	return out
}

func CollapsePrefixes(in []netip.Prefix) []netip.Prefix {
	ps := make([]netip.Prefix, 0, len(in))
	for _, p := range in {
		if !p.IsValid() || p.Bits() == 0 {
			continue
		}
		ps = append(ps, p.Masked())
	}
	slices.SortFunc(ps, func(a, b netip.Prefix) int {
		if c := a.Addr().Compare(b.Addr()); c != 0 {
			return c
		}
		return cmp.Compare(a.Bits(), b.Bits())
	})
	out := make([]netip.Prefix, 0, len(ps))
	for _, p := range ps {
		if n := len(out); n > 0 && out[n-1].Bits() <= p.Bits() && out[n-1].Contains(p.Addr()) {
			continue
		}
		out = append(out, p)
		for len(out) >= 2 {
			parent, ok := siblingParent(out[len(out)-2], out[len(out)-1])
			if !ok {
				break
			}
			out = append(out[:len(out)-2], parent)
		}
	}
	return out
}

func siblingParent(a, b netip.Prefix) (netip.Prefix, bool) {
	if a == b || a.Bits() != b.Bits() || a.Bits() <= 1 || a.Addr().BitLen() != b.Addr().BitLen() {
		return netip.Prefix{}, false
	}
	pa, err := a.Addr().Prefix(a.Bits() - 1)
	if err != nil {
		return netip.Prefix{}, false
	}
	pb, err := b.Addr().Prefix(b.Bits() - 1)
	if err != nil || pa != pb {
		return netip.Prefix{}, false
	}
	return pa, true
}

type AsnCounts struct {
	Prefixes      int    `json:"prefix_count"`
	V4            int    `json:"v4_count"`
	V6            int    `json:"v6_count"`
	IPv4Addresses uint64 `json:"ipv4_addresses"`
	IPv6Slash64s  uint64 `json:"ipv6_slash64s"`
}

func CountPrefixes(prefixes []string) AsnCounts {
	var c AsnCounts
	for _, raw := range prefixes {
		p, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		c.Prefixes++
		if p.Addr().Is4() {
			c.V4++
			c.IPv4Addresses = saturatingAdd(c.IPv4Addresses, uint64(1)<<(32-p.Bits()))
			continue
		}
		c.V6++
		span := uint64(1)
		switch {
		case p.Bits() == 0:
			span = math.MaxUint64
		case p.Bits() < 64:
			span = uint64(1) << (64 - p.Bits())
		}
		c.IPv6Slash64s = saturatingAdd(c.IPv6Slash64s, span)
	}
	return c
}

func saturatingAdd(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}
