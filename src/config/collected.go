package config

import "net/netip"

func (c *IPBlockDetectConfig) Collects() bool {
	return c.Enabled && c.Action == "collect"
}

func (s *SetConfig) TargetIPs() []string {
	if !s.Targets.UseCollectedIPs || len(s.Targets.CollectedIPs) == 0 {
		return s.Targets.IpsToMatch
	}
	ips := make([]string, 0, len(s.Targets.IpsToMatch)+len(s.Targets.CollectedIPs))
	ips = append(ips, s.Targets.IpsToMatch...)
	return append(ips, s.Targets.CollectedIPs...)
}

func (c *Config) CollectedIPsInUse() bool {
	for _, set := range c.Sets {
		if set != nil && set.Enabled && (set.TCP.IPBlockDetect.Collects() || set.Targets.UseCollectedIPs) {
			return true
		}
	}
	return false
}

func (c *Config) WithCollectedIPs(ips []string) *Config {
	view := *c
	view.Sets = make([]*SetConfig, len(c.Sets))
	for i, set := range c.Sets {
		copySet := *set
		copySet.Targets.CollectedIPs = nil
		if set.Enabled && set.Targets.UseCollectedIPs && !c.Queue.IsDiscovery {
			for _, raw := range ips {
				ip, err := netip.ParseAddr(raw)
				if err != nil {
					continue
				}
				version := uint8(4)
				if ip.Is6() {
					version = 6
				}
				if c.IPFamilyEnabled(ip.Is6()) && set.MatchesIPVersion(version) {
					copySet.Targets.CollectedIPs = append(copySet.Targets.CollectedIPs, raw)
				}
			}
		}
		view.Sets[i] = &copySet
	}
	return &view
}
