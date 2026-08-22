package config

import (
	"fmt"
	"net/netip"
	"strings"
)

func ParseSourcePrefix(entry string) (netip.Prefix, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return netip.Prefix{}, fmt.Errorf("empty entry")
	}
	if strings.Contains(entry, "/") {
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, err
		}
		addr, bits := p.Addr(), p.Bits()
		if addr.Is4In6() {
			addr = addr.Unmap()
			bits -= 96
			if bits < 0 {
				return netip.Prefix{}, fmt.Errorf("prefix length %d is not valid for an IPv4 address", p.Bits())
			}
		}
		out := netip.PrefixFrom(addr, bits)
		if !out.IsValid() {
			return netip.Prefix{}, fmt.Errorf("invalid prefix")
		}
		return out.Masked(), nil
	}
	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, err
	}
	if addr.Zone() != "" {
		return netip.Prefix{}, fmt.Errorf("zoned addresses are not supported")
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func ParseSourceACL(entries []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		p, err := ParseSourcePrefix(entry)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", strings.TrimSpace(entry), err)
		}
		out = append(out, p)
	}
	return out, nil
}
