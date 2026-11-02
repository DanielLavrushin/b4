package discovery

import "sort"

func (ds *DiscoverySuite) listAlternatesLocked() {
	grouped := map[string]bool{}
	for i := range ds.StrategyGroups {
		g := &ds.StrategyGroups[i]
		g.Alternates = alternatesAcross(g.WinnerPreset, g.Domains, ds.domainResults)
		for _, domain := range g.Domains {
			grouped[domain] = true
			if dr := ds.domainResults[domain]; dr != nil {
				dr.Alternates = g.Alternates
			}
		}
	}
	for domain, dr := range ds.domainResults {
		if dr != nil && !grouped[domain] {
			dr.Alternates = alternatesAcross(presetNoBypass, []string{domain}, ds.domainResults)
		}
	}
}

func alternatesAcross(winner string, domains []string, results map[string]*DomainDiscoveryResult) []string {
	names := []string{}
	if len(domains) == 0 || results[domains[0]] == nil {
		return names
	}
	slowest := map[string]float64{}
	for name := range results[domains[0]].Results {
		if name == winner || name == presetNoBypass || !completeEverywhere(name, domains, results) {
			continue
		}
		speed := results[domains[0]].Results[name].Speed
		for _, domain := range domains[1:] {
			speed = min(speed, results[domain].Results[name].Speed)
		}
		slowest[name] = speed
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if a, b := slowest[names[i]], slowest[names[j]]; a != b {
			return a > b
		}
		return names[i] < names[j]
	})
	if len(names) > maxAlternateSets {
		names = names[:maxAlternateSets]
	}
	return names
}
