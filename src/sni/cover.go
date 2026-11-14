package sni

import (
	"strings"

	"github.com/daniellavrushin/b4/config"
)

type Cover struct {
	GeoSite []string
	GeoIP   []string
	ASNs    []string
	Idle    bool
}

func (c Cover) Empty() bool {
	return len(c.GeoSite) == 0 && len(c.GeoIP) == 0 && len(c.ASNs) == 0 && !c.Idle
}

func (c Cover) Since(before Cover) Cover {
	if before.Idle {
		return Cover{}
	}
	return Cover{
		GeoSite: outside(c.GeoSite, keySet(before.GeoSite, categoryKey), categoryKey),
		GeoIP:   outside(c.GeoIP, keySet(before.GeoIP, categoryKey), categoryKey),
		ASNs:    outside(c.ASNs, keySet(before.ASNs, asnKey), asnKey),
		Idle:    c.Idle,
	}
}

type Coverage struct {
	owner    *config.SetConfig
	geosite  map[string]bool
	geoip    map[string]bool
	asns     map[string]bool
	domains  map[string]bool
	networks map[string]bool
}

var noCoverage = &Coverage{}

func CoverageOf(owner *config.SetConfig) *Coverage {
	if owner == nil || !owner.Enabled || len(owner.Targets.SourceDevices) > 0 {
		return &Coverage{}
	}
	return &Coverage{
		owner:   owner,
		geosite: keySet(owner.Targets.GeoSiteCategories, categoryKey),
		geoip:   keySet(owner.Targets.GeoIpCategories, categoryKey),
		asns:    keySet(owner.Targets.ASNs, asnKey),
	}
}

func (c *Coverage) Of(set *config.SetConfig) Cover {
	var out Cover
	if set == nil || !set.Enabled {
		return out
	}
	taken := noCoverage
	if c.takesFrom(set) {
		taken = c
		out.GeoSite = inside(set.Targets.GeoSiteCategories, c.geosite, categoryKey)
		out.GeoIP = inside(set.Targets.GeoIpCategories, c.geoip, categoryKey)
		out.ASNs = inside(set.Targets.ASNs, c.asns, asnKey)
	}
	out.Idle = !taken.leaves(set)
	return out
}

func (c *Coverage) takesFrom(set *config.SetConfig) bool {
	if c == nil || c.owner == nil || set.Id == c.owner.Id || len(set.Targets.SourceDevices) > 0 {
		return false
	}
	own, other := &c.owner.Targets, &set.Targets
	return filterIncludes(own.TLSVersion, other.TLSVersion) && filterIncludes(own.IPVersion, other.IPVersion)
}

func filterIncludes(owner, other string) bool {
	return owner == "" || owner == other
}

func (c *Coverage) leaves(set *config.SetConfig) bool {
	if set.SelectsL7() {
		return true
	}
	t := &set.Targets
	if anyOutside(t.ASNs, c.asns, asnKey) ||
		anyOutside(t.DomainsToMatch, c.domainKeys(), CanonicalDomainEntry) ||
		anyOutside(t.IpsToMatch, c.networkKeys(), networkKey) {
		return true
	}
	if set.DeclaresDestinationTargets() {
		return false
	}
	return strings.TrimSpace(set.TCP.DPortFilter) != "" || strings.TrimSpace(set.UDP.DPortFilter) != ""
}

func (c *Coverage) domainKeys() map[string]bool {
	if c.owner != nil && c.domains == nil {
		c.domains = keySet(c.owner.Targets.DomainsToMatch, CanonicalDomainEntry)
	}
	return c.domains
}

func (c *Coverage) networkKeys() map[string]bool {
	if c.owner != nil && c.networks == nil {
		c.networks = keySet(c.owner.Targets.IpsToMatch, networkKey)
	}
	return c.networks
}

func categoryKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func asnKey(raw string) string {
	id, ok := config.NormalizeASN(raw)
	if !ok {
		return ""
	}
	return id
}

func networkKey(raw string) string {
	network := parseTargetNetwork(raw)
	if network == nil {
		return ""
	}
	return network.String()
}

func keySet(entries []string, key func(string) string) map[string]bool {
	keys := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if k := key(entry); k != "" {
			keys[k] = true
		}
	}
	return keys
}

func anyOutside(entries []string, keys map[string]bool, key func(string) string) bool {
	for _, entry := range entries {
		if k := key(entry); k != "" && !keys[k] {
			return true
		}
	}
	return false
}

func inside(entries []string, keys map[string]bool, key func(string) string) []string {
	return pick(entries, keys, key, true)
}

func outside(entries []string, keys map[string]bool, key func(string) string) []string {
	return pick(entries, keys, key, false)
}

func pick(entries []string, keys map[string]bool, key func(string) string, listed bool) []string {
	var out []string
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		k := key(entry)
		if k == "" || seen[k] || keys[k] != listed {
			continue
		}
		seen[k] = true
		out = append(out, strings.TrimSpace(entry))
	}
	return out
}
