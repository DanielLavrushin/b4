package config

import (
	"fmt"
	"net/netip"
)

func (s *SetConfig) ReusesWSSize() bool {
	return s != nil && s.Enabled && s.TCP.WSSizeReuse.Enabled
}

func (c *Config) reuseRoutingSets(sets []*SetConfig) []*SetConfig {
	var out []*SetConfig
	for i, set := range sets {
		if !set.ReusesWSSize() || c.Queue.IsDiscovery || c.Queue.Mode == "tun" {
			if out != nil {
				out = append(out, set)
			}
			continue
		}
		if out == nil {
			out = append(make([]*SetConfig, 0, len(sets)), sets[:i]...)
		}
		view := *set
		view.Routing = RoutingConfig{Enabled: true, Mode: RoutingModeWSReuse}
		view.Targets = set.Targets
		view.Targets.DomainsToMatch = nil
		view.Targets.IpsToMatch = nil
		for _, address := range set.Targets.IpsToMatch {
			prefix, err := netip.ParsePrefix(address)
			if err != nil {
				if ip, e := netip.ParseAddr(address); e == nil {
					prefix = netip.PrefixFrom(ip, ip.BitLen())
				}
			}
			if prefix.IsValid() && (set.Targets.IPVersion == "" || (set.Targets.IPVersion == "4") == prefix.Addr().Is4()) {
				view.Targets.IpsToMatch = append(view.Targets.IpsToMatch, address)
			}
		}
		out = append(out, &view)
	}
	if out == nil {
		return sets
	}
	return out
}

func (c *Config) validateWSReuse(v *validator) {
	for i, set := range c.Sets {
		reuse := &set.TCP.WSSizeReuse
		if reuse.MaxAgeSeconds == 0 {
			reuse.MaxAgeSeconds = DefaultWSReuseMaxAgeSeconds
		}
		if reuse.MaxAgeSeconds < 1 || reuse.MaxAgeSeconds > MaxWSReuseMaxAgeSeconds {
			v.addf(fmt.Sprintf("sets[%d].tcp.wssize_reuse.max_age_seconds", i), "out_of_range", map[string]any{"value": reuse.MaxAgeSeconds, "min": 1, "max": MaxWSReuseMaxAgeSeconds}, "warm age must be between 1 and %d seconds", MaxWSReuseMaxAgeSeconds)
		}
		if !set.ReusesWSSize() {
			continue
		}
		if c.MainInjectedMark()&uint(SelfDialRelayMark) != 0 || c.Queue.IsDiscovery || c.Queue.Mode == "tun" || c.System.Tables.SkipSetup || !set.TCP.WSSize.Rewrites() || set.TCP.WSSize.Window == 0 || set.Routing.Enabled || set.TCP.Duplicate.Enabled || !set.DeclaresIPTargets() || set.Targets.DomainOnly || len(set.Targets.SNIDomains) > 0 || len(set.Targets.GeoSiteCategories) > 0 || set.Targets.TLSVersion != "" || set.Escalate.To != "" || !c.IsTCPPort(443) || !set.MatchesTCPDPort(443) {
			v.addf(fmt.Sprintf("sets[%d].tcp.wssize_reuse", i), "wssize_reuse_unsupported", map[string]any{"set": set.Name}, "set %q: WSSIZE reuse needs NFQUEUE, firewall setup, real window rewriting, an IP/GeoIP/ASN target and TCP/443, without domain/TLS filters, routing, duplication or escalation", set.Name)
		}
	}
}

func wsReuseRoutingShape(c *Config) []string {
	var ids []string
	for _, set := range c.Sets {
		if set.ReusesWSSize() {
			ids = append(ids, set.Id)
		}
	}
	return ids
}
