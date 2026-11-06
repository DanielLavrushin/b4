package sni

import (
	"cmp"
	"slices"
	"strings"

	"github.com/daniellavrushin/b4/config"
)

const maxOverlapExamples = 5

const (
	OverlapReasonSpecific = "specific"
	OverlapReasonOrder    = "order"
	OverlapReasonFilter   = "filter"
	OverlapReasonOther    = "other"
)

type OverlapEntry struct {
	Entry  string `json:"entry"`
	Source string `json:"source,omitempty"`
}

type OverlapExample struct {
	Domain     string          `json:"domain"`
	Entries    [2]OverlapEntry `json:"entries"`
	Relation   DomainRelation  `json:"relation"`
	WinnerId   string          `json:"winner_id,omitempty"`
	WinnerName string          `json:"winner_name,omitempty"`
	Reason     string          `json:"reason,omitempty"`
}

type EntrySource func(set *config.SetConfig, entry string) string

type SetOverlap struct {
	SetIds        [2]string        `json:"set_ids"`
	SetNames      [2]string        `json:"set_names"`
	SharedGeosite []string         `json:"shared_geosite,omitempty"`
	SharedGeoip   []string         `json:"shared_geoip,omitempty"`
	Domains       int              `json:"domains"`
	Examples      []OverlapExample `json:"examples,omitempty"`
}

type overlapPair struct{ a, b int }

func FindSetOverlaps(sets []*config.SetConfig, matcher *SuffixSet, source EntrySource) []SetOverlap {
	var enabled []*config.SetConfig
	for _, set := range sets {
		if set != nil && set.Enabled {
			enabled = append(enabled, set)
		}
	}

	entries := make([][]string, len(enabled))
	owners := make(map[string][]int)
	for i, set := range enabled {
		seen := make(map[string]bool, len(set.Targets.DomainsToMatch))
		for _, raw := range set.Targets.DomainsToMatch {
			value, isRegex := ParseDomainEntry(raw)
			if value == "" || isRegex || seen[value] {
				continue
			}
			seen[value] = true
			entries[i] = append(entries[i], value)
			owners[value] = append(owners[value], i)
		}
	}

	byPair := make(map[overlapPair]*SetOverlap)
	get := func(i, j int) *SetOverlap {
		key := overlapPair{min(i, j), max(i, j)}
		if o, ok := byPair[key]; ok {
			return o
		}
		o := &SetOverlap{
			SetIds:   [2]string{enabled[key.a].Id, enabled[key.b].Id},
			SetNames: [2]string{enabled[key.a].Name, enabled[key.b].Name},
		}
		byPair[key] = o
		return o
	}

	for i := range enabled {
		for _, entry := range entries[i] {
			var counted map[int]bool
			for parent := entry; parent != ""; parent = parentDomain(parent) {
				relation := RelationCovered
				if parent == entry {
					relation = RelationExact
				}
				for _, j := range owners[parent] {
					if j == i || counted[j] {
						continue
					}
					if counted == nil {
						counted = make(map[int]bool)
					}
					counted[j] = true
					if relation == RelationExact && j < i {
						continue
					}
					o := get(i, j)
					o.Domains++
					if len(o.Examples) < maxOverlapExamples {
						example := OverlapExample{Domain: entry, Relation: relation}
						own, other := 0, 1
						if i > j {
							own, other = 1, 0
						}
						example.Entries[own].Entry = entry
						example.Entries[other].Entry = parent
						o.Examples = append(o.Examples, example)
					}
				}
			}
		}
	}

	for i := range enabled {
		for j := i + 1; j < len(enabled); j++ {
			geosite := sharedTags(enabled[i].Targets.GeoSiteCategories, enabled[j].Targets.GeoSiteCategories)
			geoip := sharedTags(enabled[i].Targets.GeoIpCategories, enabled[j].Targets.GeoIpCategories)
			if len(geosite) == 0 && len(geoip) == 0 {
				continue
			}
			o := get(i, j)
			o.SharedGeosite = geosite
			o.SharedGeoip = geoip
		}
	}

	keys := make([]overlapPair, 0, len(byPair))
	for key := range byPair {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(x, y overlapPair) int {
		return cmp.Or(cmp.Compare(x.a, y.a), cmp.Compare(x.b, y.b))
	})

	overlaps := make([]SetOverlap, 0, len(keys))
	for _, key := range keys {
		o := byPair[key]
		pair := [2]*config.SetConfig{enabled[key.a], enabled[key.b]}
		for k := range o.Examples {
			example := &o.Examples[k]
			for side := range example.Entries {
				if source != nil {
					example.Entries[side].Source = source(pair[side], example.Entries[side].Entry)
				}
			}
			if matched, set := matcher.MatchSNI(example.Domain); matched && set != nil {
				example.WinnerId = set.Id
				example.WinnerName = set.Name
				example.Reason = overlapReason(*example, o.SetIds)
			}
		}
		overlaps = append(overlaps, *o)
	}
	return overlaps
}

func overlapReason(example OverlapExample, ids [2]string) string {
	winner := slices.Index(ids[:], example.WinnerId)
	switch {
	case winner < 0:
		return OverlapReasonOther
	case example.Entries[0].Entry == example.Entries[1].Entry:
		if winner == 0 {
			return OverlapReasonOrder
		}
		return OverlapReasonFilter
	case len(example.Entries[winner].Entry) > len(example.Entries[1-winner].Entry):
		return OverlapReasonSpecific
	}
	return OverlapReasonFilter
}

func parentDomain(domain string) string {
	if i := strings.IndexByte(domain, '.'); i >= 0 {
		return domain[i+1:]
	}
	return ""
}

func sharedTags(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, tag := range b {
		inB[strings.ToLower(tag)] = true
	}
	var shared []string
	for _, tag := range a {
		lower := strings.ToLower(tag)
		if inB[lower] && !slices.Contains(shared, lower) {
			shared = append(shared, lower)
		}
	}
	return shared
}
