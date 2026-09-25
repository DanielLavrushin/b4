package discovery

import (
	"sort"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func phaseRank(p DiscoveryPhase) int {
	switch p {
	case PhaseBaseline, PhaseCached:
		return 0
	case PhaseStrategy:
		return 1
	case PhaseOptimize:
		return 2
	case PhaseCombination:
		return 3
	default:
		return 4
	}
}

func presetRanksBefore(a string, aPhase DiscoveryPhase, aPriority int, b string, bPhase DiscoveryPhase, bPriority int) bool {
	ap, bp := phaseRank(aPhase), phaseRank(bPhase)
	if ap != bp {
		return ap < bp
	}
	if aPriority != bPriority {
		return aPriority < bPriority
	}
	return a < b
}

func (ds *DiscoverySuite) buildStrategyGroups() {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()

	ds.StrategyGroups = nil
	if len(ds.domainResults) == 0 {
		return
	}

	type presetInfo struct {
		phase    DiscoveryPhase
		priority int
		family   StrategyFamily
		set      *config.SetConfig
		speeds   map[string]float64
	}

	presets := map[string]*presetInfo{}
	remaining := map[string]bool{}

	for domain, dr := range ds.domainResults {
		if dr == nil || !dr.BestSuccess || dr.BaselineWorks {
			continue
		}
		remaining[domain] = true
		for name, r := range dr.Results {
			if r == nil || r.Status != CheckStatusComplete || name == presetNoBypass {
				continue
			}
			info := presets[name]
			if info == nil {
				info = &presetInfo{
					phase:    r.Phase,
					priority: r.Priority,
					family:   r.Family,
					set:      r.Set,
					speeds:   map[string]float64{},
				}
				presets[name] = info
			}
			if info.set == nil && r.Set != nil {
				info.set = r.Set
			}
			info.speeds[domain] = r.Speed
		}
	}

	betterWinner := func(a, b string) bool {
		ai, bi := presets[a], presets[b]
		return presetRanksBefore(a, ai.phase, ai.priority, b, bi.phase, bi.priority)
	}

	var groups []StrategyGroup

	for len(remaining) > 0 {
		var winner string
		var winnerCoverage int
		for name, info := range presets {
			count := 0
			for d := range info.speeds {
				if remaining[d] {
					count++
				}
			}
			if count == 0 {
				continue
			}
			if count > winnerCoverage || (count == winnerCoverage && winner != "" && betterWinner(name, winner)) {
				winner = name
				winnerCoverage = count
			}
		}
		if winner == "" {
			break
		}

		info := presets[winner]
		var groupDomains []string
		var speeds []float64
		for d, s := range info.speeds {
			if remaining[d] {
				groupDomains = append(groupDomains, d)
				speeds = append(speeds, s)
				delete(remaining, d)
			}
		}
		sort.Strings(groupDomains)
		sort.Float64s(speeds)

		var median float64
		if n := len(speeds); n > 0 {
			if n%2 == 1 {
				median = speeds[n/2]
			} else {
				median = (speeds[n/2-1] + speeds[n/2]) / 2
			}
		}

		scoped := ds.scopeSetToDomains(info.set, groupDomains)
		if info.set != nil && info.set.DNS.Enabled && scoped != nil && !scoped.DNS.Enabled {
			log.DiscoveryLogf("  DNS resolves cleanly for %v, leaving the DNS redirect out of the set", groupDomains)
		}

		groups = append(groups, StrategyGroup{
			WinnerPreset: winner,
			Family:       info.family,
			Domains:      groupDomains,
			Set:          scoped,
			MedianSpeed:  median,
		})
	}

	ds.StrategyGroups = groups
}
