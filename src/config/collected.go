package config

import (
	"net/netip"
	"time"
)

const CollectedIPTTL = 6 * time.Hour

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

func (s *SetConfig) HasTargetIPs() bool {
	return len(s.Targets.IpsToMatch) > 0 || s.Targets.UseCollectedIPs && len(s.Targets.CollectedIPs) > 0
}

func (c *Config) CollectedIPsInUse() bool {
	for _, set := range c.Sets {
		if set != nil && set.Enabled && (set.TCP.IPBlockDetect.Collects() || set.Targets.UseCollectedIPs) {
			return true
		}
	}
	return false
}

func (c *Config) CollectsReplies() bool {
	if c.Queue.IsDiscovery || c.Queue.Mode == "tun" {
		return false
	}
	for _, set := range c.Sets {
		if set != nil && set.Enabled && set.TCP.IPBlockDetect.Collects() && !set.Targets.UseCollectedIPs {
			return true
		}
	}
	return false
}

func (c *Config) CollectedDuplicateIPs() (ipv4, ipv6 []string, used bool) {
	seen := make(map[netip.Addr]struct{})
	for _, set := range c.Sets {
		if set == nil || !set.Enabled || !set.TCP.Duplicate.Enabled || !set.Targets.UseCollectedIPs {
			continue
		}
		used = true
		for _, raw := range set.Targets.CollectedIPs {
			ip, err := netip.ParseAddr(raw)
			if err != nil {
				continue
			}
			ip = ip.Unmap()
			if _, dup := seen[ip]; dup {
				continue
			}
			seen[ip] = struct{}{}
			if ip.Is4() {
				ipv4 = append(ipv4, ip.String())
			} else {
				ipv6 = append(ipv6, ip.String())
			}
		}
	}
	return ipv4, ipv6, used
}

func (c *Config) collectedDuplicateSetIDs() []string {
	var ids []string
	for _, set := range c.Sets {
		if set != nil && set.Enabled && set.TCP.Duplicate.Enabled && set.Targets.UseCollectedIPs {
			ids = append(ids, set.Id)
		}
	}
	return ids
}

func (c *Config) CollectedViewNeeded() bool {
	for _, set := range c.Sets {
		if len(set.Targets.CollectedIPs) > 0 || set.Enabled && set.Targets.UseCollectedIPs && !c.Queue.IsDiscovery {
			return true
		}
	}
	return false
}

func (c *Config) Origin() *Config {
	if c != nil && c.viewOf != nil && c.viewAt == c {
		return c.viewOf
	}
	return c
}

func (c *Config) WithCollectedIPs(ips []string) *Config {
	if !c.CollectedViewNeeded() {
		return c
	}
	view := *c
	view.viewOf, view.viewAt = c.Origin(), &view
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
