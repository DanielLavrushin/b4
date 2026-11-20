package config

import (
	"fmt"
	"net/netip"
	"sync/atomic"
)

var runningEngineTUN atomic.Bool

func SetRunningEngineTUN(on bool) { runningEngineTUN.Store(on) }

func RunningEngineTUN() bool { return runningEngineTUN.Load() }

func (s *SetConfig) ReusesWSSize() bool {
	return s != nil && s.Enabled && s.TCP.WSSizeReuse.Enabled
}

func (c *Config) WSReuseInactiveReason() string {
	switch {
	case c == nil:
		return ""
	case c.Queue.IsDiscovery:
		return "discovery"
	case c.Queue.Mode == "tun" || RunningEngineTUN():
		return "tun"
	case c.System.Tables.SkipSetup:
		return "skip_setup"
	case c.MainInjectedMark()&uint(SelfDialRelayMark) != 0:
		return "queue_mark"
	}
	return ""
}

func (c *Config) WSReuseActive(set *SetConfig) bool {
	return c != nil && set.ReusesWSSize() && c.WSReuseInactiveReason() == ""
}

func (c *Config) wsReuseInactiveCause(reason string) string {
	switch reason {
	case "tun":
		return "the engine is TUN"
	case "skip_setup":
		return "Skip IPTables/NFTables Setup is on"
	case "queue_mark":
		return fmt.Sprintf("the queue mark 0x%x shares bits with 0x%x, the mark of its relay connections", c.MainInjectedMark(), SelfDialRelayMark)
	}
	return ""
}

func (c *Config) reuseRoutingSets(sets []*SetConfig) []*SetConfig {
	var out []*SetConfig
	for i, set := range sets {
		if !c.WSReuseActive(set) {
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
		refusals := len(v.fields)
		reuse := &set.TCP.WSSizeReuse
		if reuse.MaxAgeSeconds == 0 {
			reuse.MaxAgeSeconds = DefaultWSReuseMaxAgeSeconds
		}
		if reuse.MaxAgeSeconds < 1 || reuse.MaxAgeSeconds > MaxWSReuseMaxAgeSeconds {
			v.addf(fmt.Sprintf("sets[%d].tcp.wssize_reuse.max_age_seconds", i), "wssize_reuse_age_range", map[string]any{"set": set.Name, "value": reuse.MaxAgeSeconds, "min": 1, "max": MaxWSReuseMaxAgeSeconds}, "set %q: Reuse WSSIZE connection warm validity %d is outside 1-%d seconds", set.Name, reuse.MaxAgeSeconds, MaxWSReuseMaxAgeSeconds)
		}
		if !set.ReusesWSSize() {
			continue
		}
		if !set.TCP.WSSize.Rewrites() || set.TCP.WSSize.Window == 0 {
			v.addf(fmt.Sprintf("sets[%d].tcp.wssize.window", i), "wssize_reuse_window", map[string]any{"set": set.Name}, "set %q: Reuse WSSIZE connection needs tcp.wssize on with a window above 0", set.Name)
		}
		if !set.DeclaresIPTargets() {
			v.addf(fmt.Sprintf("sets[%d].targets.ip", i), "wssize_reuse_targets", map[string]any{"set": set.Name}, "set %q: Reuse WSSIZE connection needs IP targets: addresses, GeoIP categories, ASNs or collected IPs", set.Name)
		}
		if t := &set.Targets; t.DomainOnly || len(t.SNIDomains) > 0 || len(t.GeoSiteCategories) > 0 {
			v.addf(fmt.Sprintf("sets[%d].targets.sni_domains", i), "wssize_reuse_domains", map[string]any{"set": set.Name}, "set %q: Reuse WSSIZE connection does not work with domains, GeoSite categories or domain-only matching", set.Name)
		}
		if set.Targets.TLSVersion != "" {
			v.addf(fmt.Sprintf("sets[%d].targets.tls", i), "wssize_reuse_tls", map[string]any{"set": set.Name}, "set %q: Reuse WSSIZE connection does not work with a TLS version filter", set.Name)
		}
		if !c.IsTCPPort(443) || !set.MatchesTCPDPort(443) {
			v.addf(fmt.Sprintf("sets[%d].tcp.dport_filter", i), "wssize_reuse_port", map[string]any{"set": set.Name}, "set %q: Reuse WSSIZE connection needs TCP port 443, which the port filter leaves out", set.Name)
		}
		if set.Routing.Enabled || set.TCP.Duplicate.Enabled || set.Escalate.To != "" {
			v.addf(fmt.Sprintf("sets[%d].tcp.wssize_reuse", i), "wssize_reuse_conflict", map[string]any{"set": set.Name}, "set %q: Reuse WSSIZE connection does not work together with routing, packet duplication or escalation in the same set", set.Name)
		}
		if len(v.fields) > refusals {
			continue
		}
		if cause := c.wsReuseInactiveCause(c.WSReuseInactiveReason()); cause != "" {
			warnEgressGatewayOnce("Set '%s': Reuse WSSIZE connection does nothing while %s; the set's connections get its usual strategy", set.Name, cause)
		}
	}
}

func wsReuseRoutingShape(c *Config) []string {
	var ids []string
	for _, set := range c.Sets {
		if c.WSReuseActive(set) {
			ids = append(ids, set.Id)
		}
	}
	return ids
}
