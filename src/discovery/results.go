package discovery

import (
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

// storeResult stores a single-domain result (used during Phase 2 optimization via withSingleDomain).
func (ds *DiscoverySuite) storeResult(preset ConfigPreset, result CheckResult) {
	ds.recordResult(preset, result)
	ds.checkCoverage()
}

func (ds *DiscoverySuite) recordResult(preset ConfigPreset, result CheckResult) {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()

	domainResult := ds.domainResults[ds.Domain]

	switch result.Status {
	case CheckStatusComplete:
		ds.SuccessfulChecks++
	case CheckStatusFailed:
		ds.FailedChecks++
	}

	domainResult.Results[preset.Name] = &DomainPresetResult{
		PresetName: preset.Name,
		Family:     preset.Family,
		Phase:      preset.Phase,
		Priority:   preset.Priority,
		Status:     result.Status,
		Duration:   result.Duration,
		Speed:      result.Speed,
		BytesRead:  result.BytesRead,
		Error:      result.Error,
		StatusCode: result.StatusCode,
		Set:        setForResult(result),
	}

	if result.Status == CheckStatusComplete && result.FinalHost != "" {
		domainResult.FinalHost = result.FinalHost
	}

	if preset.Name == presetNoBypass {
		ds.recordPlainFix(ds.Domain, domainResult, result)
	}

	if result.Status == CheckStatusComplete && preset.Name != presetNoBypass {
		if result.Speed > domainResult.BestSpeed {
			oldBest := domainResult.BestSpeed
			domainResult.BestPreset = preset.Name
			domainResult.BestSpeed = result.Speed
			domainResult.BestSuccess = true
			if oldBest > 0 {
				improvement := ((result.Speed - oldBest) / oldBest) * 100
				log.DiscoveryLogf("★ New best: %s at %.2f KB/s (+%.0f%%)", preset.Name, result.Speed/1024, improvement)
			} else {
				log.DiscoveryLogf("★ First success: %s at %.2f KB/s", preset.Name, result.Speed/1024)
			}
		}
	}

	ds.DomainDiscoveryResults = ds.domainResults
	ds.refreshOutcomes(false)
}

// storeResultsMulti stores per-domain results from testPresetAllDomains.
func (ds *DiscoverySuite) storeResultsMulti(preset ConfigPreset, results map[string]CheckResult) {
	ds.recordResultsMulti(preset, results)
	ds.checkCoverage()
}

func (ds *DiscoverySuite) recordResultsMulti(preset ConfigPreset, results map[string]CheckResult) {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()

	for domain, result := range results {
		domainResult := ds.domainResults[domain]

		switch result.Status {
		case CheckStatusComplete:
			ds.SuccessfulChecks++
		case CheckStatusFailed:
			ds.FailedChecks++
		}

		domainResult.Results[preset.Name] = &DomainPresetResult{
			PresetName: preset.Name,
			Family:     preset.Family,
			Phase:      preset.Phase,
			Priority:   preset.Priority,
			Status:     result.Status,
			Duration:   result.Duration,
			Speed:      result.Speed,
			BytesRead:  result.BytesRead,
			Error:      result.Error,
			StatusCode: result.StatusCode,
			Set:        setForResult(result),
		}

		if result.Status == CheckStatusComplete && result.FinalHost != "" {
			domainResult.FinalHost = result.FinalHost
		}

		if preset.Name == presetNoBypass {
			ds.recordPlainFix(domain, domainResult, result)
		}

		if result.Status == CheckStatusComplete && preset.Name != presetNoBypass {
			if result.Speed > domainResult.BestSpeed {
				oldBest := domainResult.BestSpeed
				domainResult.BestPreset = preset.Name
				domainResult.BestSpeed = result.Speed
				domainResult.BestSuccess = true
				if oldBest > 0 {
					improvement := ((result.Speed - oldBest) / oldBest) * 100
					log.DiscoveryLogf("  ★ [%s] New best: %s at %.2f KB/s (+%.0f%%)", domain, preset.Name, result.Speed/1024, improvement)
				} else {
					log.DiscoveryLogf("  ★ [%s] First success: %s at %.2f KB/s", domain, preset.Name, result.Speed/1024)
				}
			}
		}
	}

	ds.DomainDiscoveryResults = ds.domainResults
	ds.refreshOutcomes(false)
}

func (ds *DiscoverySuite) determineBest() {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()

	for _, domainResult := range ds.domainResults {
		domainBaseline := 0.0
		if r := domainResult.Results[presetNoBypass]; r != nil && r.Status == CheckStatusComplete {
			domainBaseline = r.Speed
		}

		domainResult.BaselineSpeed = domainBaseline

		if name, r := plainFixResult(domainResult); r != nil {
			domainResult.BaselineWorks = false
			domainResult.BestPreset = name
			domainResult.BestSpeed = r.Speed
			domainResult.BestSuccess = true
			continue
		}

		if domainBaseline > 0 {
			domainResult.BaselineWorks = true
			domainResult.BestPreset = presetNoBypass
			domainResult.BestSpeed = domainBaseline
			domainResult.BestSuccess = true
			continue
		}

		var bestPreset string
		var bestSpeed float64
		for presetName, result := range domainResult.Results {
			if presetName == presetNoBypass || result == nil || result.Status != CheckStatusComplete {
				continue
			}
			if result.Speed > bestSpeed {
				bestPreset = presetName
				bestSpeed = result.Speed
			}
		}

		domainResult.BaselineWorks = false
		domainResult.BestPreset = bestPreset
		domainResult.BestSpeed = bestSpeed
		domainResult.BestSuccess = bestSpeed > 0
	}
	ds.refreshOutcomes(false)
}

func (ds *DiscoverySuite) refreshOutcomes(finished bool) {
	for _, dr := range ds.domainResults {
		if dr != nil {
			dr.refreshOutcome(finished)
		}
	}
}

func setForResult(result CheckResult) *config.SetConfig {
	if result.Status != CheckStatusComplete {
		return nil
	}
	return result.Set
}
