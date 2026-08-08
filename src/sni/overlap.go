package sni

import (
	"github.com/daniellavrushin/b4/config"
)

type DomainOverlap struct {
	Entry    string
	SetNames []string
}

func FindDomainOverlaps(sets []*config.SetConfig) []DomainOverlap {
	owners := make(map[string][]string)
	var order []string

	for _, set := range sets {
		if set == nil || !set.Enabled {
			continue
		}
		seen := make(map[string]bool, len(set.Targets.SNIDomains))
		for _, entry := range set.Targets.SNIDomains {
			canonical := CanonicalDomainEntry(entry)
			if canonical == "" || seen[canonical] {
				continue
			}
			seen[canonical] = true
			if _, exists := owners[canonical]; !exists {
				order = append(order, canonical)
			}
			owners[canonical] = append(owners[canonical], set.Name)
		}
	}

	var overlaps []DomainOverlap
	for _, entry := range order {
		if names := owners[entry]; len(names) > 1 {
			overlaps = append(overlaps, DomainOverlap{Entry: entry, SetNames: names})
		}
	}
	return overlaps
}
