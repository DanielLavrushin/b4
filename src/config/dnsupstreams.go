package config

import (
	"fmt"
	"net"
	"strings"
)

const (
	DefaultFakeIPCIDR  = "198.18.0.0/15"
	DefaultFakeIPTTL   = 60
	MaxDNSUpstreams    = 8
	MinFakeIPPrefixLen = 24
	MaxFakeIPTTL       = 1500
	MinFakeIPBlockOnes = 8
)

func SplitUpstreamURL(raw string) (scheme, rest string, ok bool) {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	for _, candidate := range []string{"https://", "http://"} {
		if strings.HasPrefix(lower, candidate) {
			return raw[:len(candidate)], raw[len(candidate):], true
		}
	}
	return "", "", false
}

func IsDoHUpstream(raw string) bool {
	_, _, ok := SplitUpstreamURL(raw)
	return ok
}

func NormalizeUpstream(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if scheme, rest, ok := SplitUpstreamURL(raw); ok {
		if strings.TrimSpace(rest) == "" {
			return "", false
		}
		return scheme + strings.TrimSpace(rest), true
	}
	ip := net.ParseIP(raw)
	if ip == nil {
		return "", false
	}
	return ip.String(), true
}

func (c *DNSConfig) UpstreamList() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, 2+len(c.Upstreams))
	seen := make(map[string]struct{}, 2+len(c.Upstreams))
	add := func(raw string) {
		norm, ok := NormalizeUpstream(raw)
		if !ok {
			return
		}
		if _, dup := seen[norm]; dup {
			return
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	add(c.DoHURL)
	if strings.TrimSpace(c.DoHURL) == "" {
		add(c.TargetDNS)
	}
	for _, raw := range c.Upstreams {
		add(raw)
	}
	return out
}

func (c *DNSConfig) HasUpstream() bool {
	if c == nil || !c.Enabled {
		return false
	}
	return len(c.UpstreamList()) > 0
}

func (c *DNSConfig) FakeIPNetwork() (*net.IPNet, int, bool) {
	if c == nil || !c.FakeIP.Enabled {
		return nil, 0, false
	}
	cidr := strings.TrimSpace(c.FakeIP.CIDR)
	if cidr == "" {
		cidr = DefaultFakeIPCIDR
	}
	_, network, err := net.ParseCIDR(cidr)
	if err != nil || network == nil || network.IP.To4() == nil {
		return nil, 0, false
	}
	ttl := c.FakeIP.TTLSec
	if ttl <= 0 {
		ttl = DefaultFakeIPTTL
	}
	if ttl > MaxFakeIPTTL {
		ttl = MaxFakeIPTTL
	}
	return network, ttl, true
}

func (c *DNSConfig) NormalizeUpstreams(onDrop func(value string)) {
	if c == nil || len(c.Upstreams) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(c.Upstreams))
	out := make([]string, 0, len(c.Upstreams))
	for _, raw := range c.Upstreams {
		norm, ok := NormalizeUpstream(raw)
		if !ok {
			if onDrop != nil {
				onDrop(raw)
			}
			continue
		}
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	if len(out) > MaxDNSUpstreams {
		if onDrop != nil {
			for _, raw := range out[MaxDNSUpstreams:] {
				onDrop(raw)
			}
		}
		out = out[:MaxDNSUpstreams]
	}
	c.Upstreams = out
}

func (c *DNSConfig) ValidateFakeIP() error {
	if !c.FakeIP.Enabled {
		return nil
	}
	cidr := strings.TrimSpace(c.FakeIP.CIDR)
	if cidr == "" {
		c.FakeIP.CIDR = DefaultFakeIPCIDR
		cidr = DefaultFakeIPCIDR
	}
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil || network == nil {
		return fmt.Errorf("%q is not a CIDR block", cidr)
	}
	if ip.To4() == nil {
		return fmt.Errorf("%q is not an IPv4 block", cidr)
	}
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return fmt.Errorf("%q is not an IPv4 block", cidr)
	}
	if ones > MinFakeIPPrefixLen {
		return fmt.Errorf("%q is smaller than a /%d and would run out of addresses", cidr, MinFakeIPPrefixLen)
	}
	if ones < MinFakeIPBlockOnes {
		return fmt.Errorf("%q is larger than a /%d, so most of the internet would be routed into the set", cidr, MinFakeIPBlockOnes)
	}
	switch {
	case network.IP.IsLoopback(), network.IP.IsMulticast(), network.IP.IsUnspecified():
		return fmt.Errorf("%q is not a usable block", cidr)
	}
	if c.FakeIP.TTLSec < 0 {
		return fmt.Errorf("ttl_sec must not be negative")
	}
	if c.FakeIP.TTLSec == 0 {
		c.FakeIP.TTLSec = DefaultFakeIPTTL
	}
	if c.FakeIP.TTLSec > MaxFakeIPTTL {
		c.FakeIP.TTLSec = MaxFakeIPTTL
	}
	c.FakeIP.CIDR = network.String()
	return nil
}
