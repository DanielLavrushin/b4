package sni

import (
	"net"
	"slices"

	"github.com/daniellavrushin/b4/config"
	"github.com/yl2chen/cidranger"
)

func (s *SuffixSet) MatchIPWithSourceDetail(ip net.IP, srcMAC string, keep ...SetFilter) (bool, *config.SetConfig, int) {
	if s == nil || s.ipRanger == nil || ip == nil {
		return false, nil, -1
	}

	entries, err := s.ipRanger.ContainingNetworks(ip)
	if err != nil || len(entries) == 0 {
		return false, nil, -1
	}

	sortEntriesBySpecificity(entries)

	var candidates []*config.SetConfig
	for _, e := range entries {
		candidates = append(candidates, e.(*ipRange).sets...)
	}

	matched, set := selectSetBySource(candidates, srcMAC, ipVersionOf(ip), joinFilters(keep))
	if !matched {
		return false, nil, -1
	}
	return true, set, listedPrefixLen(entries, set)
}

func listedPrefixLen(entries []cidranger.RangerEntry, set *config.SetConfig) int {
	for _, e := range entries {
		r := e.(*ipRange)
		for _, listed := range r.sets {
			if listed == set {
				ones, _ := r.ipNet.Mask.Size()
				return ones
			}
		}
	}
	return -1
}

func (s *SuffixSet) AddressHolds(addr *config.SetConfig, prefixLen int, name *config.SetConfig, srcMAC string) bool {
	if s == nil || addr == nil || name == nil || prefixLen <= 0 || !addr.IPPriorityApplies() || addr.Id == name.Id {
		return false
	}
	addrIndex, known := s.setIndex[addr.Id]
	if !known || !setMatchesSource(addr, srcMAC) {
		return false
	}
	nameIndex, known := s.setIndex[name.Id]
	if !known {
		return false
	}
	addrTier, nameTier := sourceTier(addr, srcMAC), sourceTier(name, srcMAC)
	if addrTier != nameTier {
		return addrTier < nameTier
	}
	return addrIndex < nameIndex
}

func sourceTier(set *config.SetConfig, srcMAC string) int {
	if len(set.Targets.SourceDevices) > 0 && setMatchesSource(set, srcMAC) {
		return 0
	}
	return 1
}

func (s *SuffixSet) KeepsLearned(cfg *config.Config, set *config.SetConfig, ip net.IP) bool {
	if s == nil || !s.anyIPPriority || s.ipRanger == nil || cfg == nil || set == nil || ip == nil {
		return true
	}
	if len(set.Targets.SourceDevices) > 0 {
		return true
	}
	nameIndex, agreed := s.agreedIndex(cfg, set.Id)
	if !agreed || len(cfg.Sets[nameIndex].Targets.SourceDevices) > 0 || s.scopedSet[set.Id] {
		return true
	}

	entries, err := s.ipRanger.ContainingNetworks(ip)
	if err != nil || len(entries) == 0 {
		return true
	}
	sortEntriesBySpecificity(entries)

	version := ipVersionOf(ip)
	for _, e := range entries {
		r := e.(*ipRange)
		winner := -1
		var built *config.SetConfig
		for _, listed := range r.sets {
			index, agreed := s.agreedIndex(cfg, listed.Id)
			if !agreed || !sameAddressTargets(cfg.Sets[index], listed) {
				return true
			}
			general := heldForEveryDevice(cfg.Sets[index], version)
			if general != heldForEveryDevice(listed, version) {
				return true
			}
			if general && winner < 0 {
				winner, built = index, listed
			}
		}
		if winner < 0 {
			continue
		}
		ones, _ := r.ipNet.Mask.Size()
		if ones == 0 || winner >= nameIndex || !cfg.Sets[winner].IPPriorityApplies() || !built.IPPriorityApplies() {
			return true
		}
		return escalatesTo(cfg, cfg.Sets[winner], set.Id)
	}
	return true
}

func sameAddressTargets(a, b *config.SetConfig) bool {
	return slices.Equal(a.Targets.IPs, b.Targets.IPs) &&
		slices.Equal(a.Targets.ASNs, b.Targets.ASNs) &&
		slices.Equal(a.Targets.GeoIpCategories, b.Targets.GeoIpCategories)
}

const escalationChainHops = 8

func escalatesTo(cfg *config.Config, from *config.SetConfig, id string) bool {
	current := from
	for hop := 0; hop < escalationChainHops && current.Escalate.Active(); hop++ {
		if current.Escalate.To == id {
			return true
		}
		next := cfg.GetSetById(current.Escalate.To)
		if next == nil || !next.Enabled || next.Id == from.Id {
			return false
		}
		current = next
	}
	return false
}

func (s *SuffixSet) agreedIndex(cfg *config.Config, id string) (int, bool) {
	index, known := s.setIndex[id]
	if !known || index >= len(cfg.Sets) {
		return 0, false
	}
	current := cfg.Sets[index]
	if current == nil || current.Id != id || !current.Enabled {
		return 0, false
	}
	return index, true
}

func heldForEveryDevice(set *config.SetConfig, ipVersion uint8) bool {
	return len(set.Targets.SourceDevices) == 0 && !set.BlockRoutingScoped() && set.MatchesIPVersion(ipVersion)
}
