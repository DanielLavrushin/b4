package discovery

import (
	"sort"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/log"
)

// runPhase1Multi tests all Phase 1 presets across all domains.
// Each preset config is applied ONCE and all domains are tested.
func (ds *DiscoverySuite) runPhase1Multi(presets []ConfigPreset) []StrategyFamily {
	var workingFamilies []StrategyFamily

	log.DiscoveryLogf("Phase 1: Testing %d strategy families across %d domains", len(presets), len(ds.Domains))

	baselineResults := ds.baselineResults(presets[0])

	if !ds.anyDomainNeedsBypass() {
		log.DiscoveryLogf("  Every domain loads without a packet strategy - testing the presets for comparison only")
	}

	// Payload detection uses the primary domain
	ds.detectWorkingPayloads(presets)

	if ds.setMode() && len(ds.Domains) > 1 && !ds.interrupted() {
		if variant, ok := ds.bestPayloadVariant(presets); ok {
			ds.CheckSuite.mu.Lock()
			ds.TotalChecks += len(ds.presetDomains(variant))
			ds.CheckSuite.mu.Unlock()
			log.DiscoveryLogf("  Re-testing '%s' on every address of the set", variant.Name)
			results := ds.testPresetAllDomains(variant)
			ds.storeResultsMulti(variant, results)
			for domain, r := range results {
				if r.Status == CheckStatusComplete && ds.needsBypass(domain) {
					if !containsFamily(workingFamilies, FamilyCombo) {
						workingFamilies = append(workingFamilies, FamilyCombo)
					}
					break
				}
			}
		}
	}

	strategyPresets := ds.filterTestedPresets(presets)

	// Use primary domain baseline for failure mode analysis
	primaryResult := baselineResults[ds.Domain]
	baselineFailureMode := analyzeFailure(primaryResult)
	suggestedFamilies := suggestFamiliesForFailure(baselineFailureMode)

	if len(suggestedFamilies) > 0 {
		strategyPresets = reorderByFamilies(strategyPresets, suggestedFamilies)
		log.DiscoveryLogf("  Failure mode: %s - prioritizing: %v", baselineFailureMode, suggestedFamilies)
	}

	for _, preset := range strategyPresets {
		if ds.interrupted() {
			return workingFamilies
		}

		ds.applyBestPayload(&preset.Config.Faking)
		domainResults := ds.testPresetAllDomains(preset)
		ds.storeResultsMulti(preset, domainResults)

		for domain, r := range domainResults {
			if r.Status != CheckStatusComplete || preset.Family == FamilyNone || !ds.needsBypass(domain) {
				continue
			}
			if !containsFamily(workingFamilies, preset.Family) {
				workingFamilies = append(workingFamilies, preset.Family)
			}
			break
		}
	}

	return workingFamilies
}

func (ds *DiscoverySuite) baselineResults(baseline ConfigPreset) map[string]CheckResult {
	ds.CheckSuite.mu.RLock()
	stored := make(map[string]CheckResult, len(ds.domainResults))
	for domain, dr := range ds.domainResults {
		if r := dr.Results[baseline.Name]; r != nil {
			stored[domain] = CheckResult{
				Domain:     domain,
				Status:     r.Status,
				Duration:   r.Duration,
				Speed:      r.Speed,
				BytesRead:  r.BytesRead,
				Error:      r.Error,
				StatusCode: r.StatusCode,
			}
		}
	}
	ds.CheckSuite.mu.RUnlock()

	if len(stored) == len(ds.domainResults) {
		return stored
	}

	results := ds.testPresetAllDomains(baseline)
	ds.storeResultsMulti(baseline, results)
	return results
}

// filterTestedPresets removes presets we've already tested
func (ds *DiscoverySuite) filterTestedPresets(presets []ConfigPreset) []ConfigPreset {
	filtered := []ConfigPreset{}
	for _, p := range presets {
		if p.Name == presetNoBypass || p.Name == "combo-pastseq" {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

// runPhase2WithRepresentative optimizes each working family using a representative domain,
// then validates the optimized config against all domains.
func (ds *DiscoverySuite) runPhase2WithRepresentative(families []StrategyFamily) map[StrategyFamily]ConfigPreset {
	bestParams := make(map[StrategyFamily]ConfigPreset)

	log.DiscoveryLogf("Phase 2: Optimizing %d working families", len(families))

	for _, family := range families {
		if ds.interrupted() {
			return bestParams
		}

		// Find the best representative domain for this family
		repDomain := ds.findRepresentativeDomain(family)
		log.DiscoveryLogf("  Using %s as representative for %s optimization", repDomain, family)

		var bestPreset ConfigPreset
		ds.withSingleDomain(repDomain, func() {
			switch family {
			case FamilyFakeSNI:
				bestPreset = ds.optimizeFakeSNI()
			case FamilyCombo:
				bestPreset = ds.optimizeCombo()
			case FamilyTCPFrag:
				bestPreset = ds.optimizeTCPFrag()
			case FamilyTLSRec:
				bestPreset = ds.optimizeTLSRec()
			default:
				bestPreset = ds.optimizeWithPresets(family)
			}
		})

		// Validate optimized config against all domains
		if bestPreset.Name != "" {
			ds.CheckSuite.mu.Lock()
			ds.TotalChecks += len(ds.Domains)
			ds.CheckSuite.mu.Unlock()
			validationResults := ds.testPresetAllDomains(bestPreset)
			ds.storeResultsMulti(bestPreset, validationResults)
		}

		bestParams[family] = bestPreset
	}

	return bestParams
}

// findRepresentativeDomain finds the domain with the best Phase 1 speed for a given family.
func (ds *DiscoverySuite) findRepresentativeDomain(family StrategyFamily) string {
	var bestDomain string
	var bestSpeed float64

	for domain, domainResult := range ds.domainResults {
		if !ds.needsBypass(domain) {
			continue
		}
		for _, result := range domainResult.Results {
			if result.Family == family && result.Status == CheckStatusComplete && result.Speed > bestSpeed {
				bestSpeed = result.Speed
				bestDomain = domain
			}
		}
	}

	if bestDomain == "" {
		return ds.Domain // fallback to primary
	}
	return bestDomain
}

// runPhase3Multi tests combination presets across all domains.
func (ds *DiscoverySuite) runPhase3Multi(workingFamilies []StrategyFamily, bestParams map[StrategyFamily]ConfigPreset) {
	presets := GetCombinationPresets(workingFamilies, bestParams)
	if len(presets) == 0 {
		return
	}

	ds.CheckSuite.mu.Lock()
	ds.TotalChecks += len(presets) * len(ds.Domains)
	ds.CheckSuite.mu.Unlock()

	log.DiscoveryLogf("Phase 3: Testing %d combination presets across %d domains", len(presets), len(ds.Domains))

	for _, preset := range presets {
		if ds.interrupted() {
			return
		}

		ds.applyBestPayload(&preset.Config.Faking)
		results := ds.testPresetAllDomains(preset)
		ds.storeResultsMulti(preset, results)
	}
}

// withSingleDomain temporarily scopes the suite to a single domain for Phase 2 optimization.
// This allows existing optimization methods (TTL scan, position search, etc.) to work unchanged.
func (ds *DiscoverySuite) withSingleDomain(domain string, fn func()) {
	origDomain := ds.Domain
	origURL := ds.CheckURL

	for _, di := range ds.Domains {
		if di.Domain == domain {
			ds.Domain = di.Domain
			ds.CheckURL = di.CheckURL
			break
		}
	}

	fn()

	ds.Domain = origDomain
	ds.CheckURL = origURL
}

func (ds *DiscoverySuite) runExtendedSearch() []StrategyFamily {
	families := []StrategyFamily{
		FamilyCombo,
		FamilyDisorder,
		FamilyOverlap,
		FamilyExtSplit,
		FamilyFirstByte,
		FamilyTCPFrag,
		FamilyTLSRec,
		FamilyOOB,
		FamilyFakeSNI,
		FamilyIPFrag,
		FamilySACK,
		FamilyDesync,
		FamilySynFake,
		FamilyDelay,
		FamilyHybrid,
	}

	var workingFamilies []StrategyFamily

	for _, family := range families {
		if ds.interrupted() {
			return workingFamilies
		}

		presets := GetPhase2Presets(family)

		ds.CheckSuite.mu.Lock()
		ds.TotalChecks += len(presets)
		ds.CheckSuite.mu.Unlock()

		log.DiscoveryLogf("  Extended search: %s (%d variants)", family, len(presets))

		for _, preset := range presets {
			if ds.interrupted() {
				return workingFamilies
			}

			result := ds.testPresetWithBestPayload(preset)
			ds.storeResult(preset, result)

			if result.Status == CheckStatusComplete {
				log.DiscoveryLogf("    %s: SUCCESS (%.2f KB/s)", preset.Name, result.Speed/1024)
				if !containsFamily(workingFamilies, family) {
					workingFamilies = append(workingFamilies, family)
				}
			}
		}
	}

	return workingFamilies
}

func analyzeFailure(result CheckResult) FailureMode {
	if result.Error == "" {
		return FailureUnknown
	}
	err := strings.ToLower(result.Error)

	if strings.Contains(err, "reset") || strings.Contains(err, "rst") {
		if result.Duration < 100*time.Millisecond {
			return FailureRSTImmediate
		}
	}
	if strings.Contains(err, "timeout") || strings.Contains(err, "deadline") || strings.Contains(err, "stalled") {
		return FailureTimeout
	}
	if strings.Contains(err, "tls") || strings.Contains(err, "certificate") {
		return FailureTLSError
	}
	return FailureUnknown
}

func suggestFamiliesForFailure(mode FailureMode) []StrategyFamily {
	switch mode {
	case FailureRSTImmediate:
		return []StrategyFamily{FamilyDesync, FamilyFakeSNI, FamilySynFake}
	case FailureTimeout:
		return []StrategyFamily{FamilyTCPFrag, FamilyTLSRec, FamilyOOB}
	default:
		return nil
	}
}

func reorderByFamilies(presets []ConfigPreset, priority []StrategyFamily) []ConfigPreset {
	priorityMap := make(map[StrategyFamily]int)
	for i, f := range priority {
		priorityMap[f] = i
	}

	sort.SliceStable(presets, func(i, j int) bool {
		pi, oki := priorityMap[presets[i].Family]
		pj, okj := priorityMap[presets[j].Family]
		if oki && !okj {
			return true
		}
		if !oki && okj {
			return false
		}
		if oki && okj {
			return pi < pj
		}
		return false
	})
	return presets
}
