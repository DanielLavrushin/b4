package config

import (
	"fmt"
	"net/netip"
	"strings"
)

func (c Socks5WarmTupleConfig) Effective() Socks5WarmTupleConfig {
	if c.FirstPort == 0 && c.LastPort == 0 {
		c.FirstPort, c.LastPort = 61000, 61999
	}
	if c.WarmTimeoutMs == 0 {
		c.WarmTimeoutMs = 15000
	}
	if c.ReuseTimeoutMs == 0 {
		c.ReuseTimeoutMs = 10000
	}
	return c
}

func (c *Config) validateWarmTuple(v *validator) {
	w := c.System.Socks5.WarmTuple.Effective()
	if w.FirstPort < 1024 || w.LastPort > 65535 || w.LastPort < w.FirstPort || w.LastPort-w.FirstPort >= 4096 ||
		w.WarmTimeoutMs < 100 || w.WarmTimeoutMs > 60000 || w.ReuseTimeoutMs < 100 || w.ReuseTimeoutMs > 60000 ||
		w.ReconnectDelayMs < 0 || w.ReconnectDelayMs > 10000 || w.ReconnectDelayMs >= w.ReuseTimeoutMs {
		v.addf("system.socks5.warm_tuple", "warm_tuple_invalid", nil, "Invalid port range, timeout or reconnect delay")
	}
	if w.SourceAddress != "" {
		a, err := netip.ParseAddr(w.SourceAddress)
		if err != nil || !a.Is4() || a.IsUnspecified() || a.IsMulticast() {
			v.addf("system.socks5.warm_tuple.source_address", "warm_tuple_invalid", nil, "Source must be a local IPv4 address or empty for route selection")
		}
	}
	for host, target := range w.Targets {
		a, err := netip.ParseAddr(target)
		if err != nil || !a.Is4() || a.IsUnspecified() || a.IsMulticast() ||
			host != strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), ".")) ||
			strings.ContainsAny(host, "/:* \t\r\n") || host == "" || len(host) > 253 {
			v.addf("system.socks5.warm_tuple.targets", "warm_tuple_invalid", nil, "Targets must map normalized exact hostnames to IPv4 addresses")
		}
	}
	for i, s := range c.Sets {
		if !s.TCP.WarmTuple.Enabled {
			continue
		}
		if c.Queue.Mode == "tun" || !c.Queue.IPv4Enabled || len(c.Queue.Interfaces) != 0 || !c.IsTCPPort(443) ||
			s.Routing.Enabled || s.TCP.Duplicate.Enabled || s.Targets.TLSVersion != "" || s.Targets.IPVersion == "6" ||
			!s.MatchesTCPDPort(443) || !s.DeclaresDestinationTargets() ||
			(s.Targets.DomainOnly && len(s.Targets.SNIDomains)+len(s.Targets.DomainsToMatch) == 0) ||
			!s.TCP.WSSize.Enabled || s.TCP.WSSize.Cutoff != "client_hello" {
			v.addf(fmt.Sprintf("sets[%d].tcp.warm_tuple", i), "warm_tuple_unsupported", nil,
				"Warm tuple requires SOCKS5 hostnames, domain or IP/GeoIP targets, NFQUEUE IPv4 TCP443, no interface filter/routing/duplication/TLS filter, and wssize with client_hello cutoff")
		}
	}
}
