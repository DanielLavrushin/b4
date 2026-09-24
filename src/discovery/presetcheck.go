package discovery

import (
	"fmt"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

// testPresetInternal tests a single preset against the primary domain.
// Used during Phase 2 optimization (via withSingleDomain helper).
func nonOKStatusNote(result CheckResult) string {
	if result.StatusCode >= 200 && result.StatusCode < 300 {
		return ""
	}
	if result.StatusCode == 0 {
		return ""
	}
	return fmt.Sprintf(", HTTP %d", result.StatusCode)
}

func (ds *DiscoverySuite) testPresetInternal(preset ConfigPreset) CheckResult {
	log.DiscoveryLogf("  Testing '%s'...", preset.Name)

	di := DomainInput{Domain: ds.Domain, CheckURL: ds.CheckURL}
	testConfig := ds.buildTestConfig(preset)

	if err := ds.pool.UpdateConfig(testConfig); err != nil {
		log.DiscoveryLogf("    → FAILED (config error: %v)", err)
		return CheckResult{
			Domain: ds.Domain,
			Status: CheckStatusFailed,
			Error:  err.Error(),
		}
	}

	time.Sleep(time.Duration(ds.cfg.System.Checker.ConfigPropagateMs) * time.Millisecond)

	// Run validation tries with early exit on first failure.
	timeout := time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec) * time.Second
	successCount := 0
	var lastResult CheckResult

	for i := 0; i < ds.validationTries; i++ {
		result := ds.fetchForDomain(di, timeout)
		if len(testConfig.Sets) > 0 {
			result.Set = testConfig.Sets[0]
		}
		lastResult = result

		if result.Status != CheckStatusComplete {
			// Early exit: no point continuing if a try failed.
			break
		}
		successCount++

		if i < ds.validationTries-1 {
			time.Sleep(validationRetryDelay)
		}
	}

	if successCount == ds.validationTries {
		if ds.validationTries > 1 {
			log.DiscoveryLogf("    → OK (%.2f KB/s, %d bytes%s) - %d/%d tries succeeded",
				lastResult.Speed/1024, lastResult.BytesRead, nonOKStatusNote(lastResult), successCount, ds.validationTries)
		} else {
			log.DiscoveryLogf("    → OK (%.2f KB/s, %d bytes%s)", lastResult.Speed/1024, lastResult.BytesRead, nonOKStatusNote(lastResult))
		}
		return lastResult
	}

	lastResult.Status = CheckStatusFailed
	if ds.validationTries > 1 {
		log.DiscoveryLogf("    → FAILED (%d/%d tries succeeded: %s)", successCount, ds.validationTries, lastResult.Error)
		lastResult.Error = fmt.Sprintf("%s (%d/%d tries)", lastResult.Error, successCount, ds.validationTries)
	} else {
		log.DiscoveryLogf("    → FAILED (%s)", lastResult.Error)
	}
	return lastResult
}

func (ds *DiscoverySuite) testPreset(preset ConfigPreset) CheckResult {
	defer func() {
		ds.CheckSuite.mu.Lock()
		ds.CompletedChecks++
		ds.CheckSuite.mu.Unlock()
	}()

	return ds.testPresetInternal(preset)
}

// testPresetAllDomains applies the config ONCE and tests ALL domains.
// This is the core multi-domain optimization: 1 config switch, N fetches.
func (ds *DiscoverySuite) presetDomains(preset ConfigPreset) []DomainInput {
	if len(preset.Domains) == 0 {
		return ds.Domains
	}
	out := make([]DomainInput, 0, len(preset.Domains))
	for _, di := range ds.Domains {
		if preset.covers(di.Domain) {
			out = append(out, di)
		}
	}
	return out
}

func (ds *DiscoverySuite) testPresetAllDomains(preset ConfigPreset) map[string]CheckResult {
	domains := ds.presetDomains(preset)
	log.DiscoveryLogf("  Testing '%s' across %d domains...", preset.Name, len(domains))

	results := make(map[string]CheckResult)

	testConfig := ds.buildTestConfigMulti(preset)

	if err := ds.pool.UpdateConfig(testConfig); err != nil {
		log.DiscoveryLogf("    → FAILED (config error: %v)", err)
		for _, di := range domains {
			results[di.Domain] = CheckResult{
				Domain: di.Domain,
				Status: CheckStatusFailed,
				Error:  err.Error(),
			}
		}
		ds.CheckSuite.mu.Lock()
		ds.CompletedChecks += len(domains)
		ds.CheckSuite.mu.Unlock()
		return results
	}

	time.Sleep(time.Duration(ds.cfg.System.Checker.ConfigPropagateMs) * time.Millisecond)

	timeout := time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec) * time.Second

	var mu sync.Mutex
	var wg sync.WaitGroup

spawn:
	for _, di := range domains {
		if ds.interrupted() {
			break spawn
		}

		wg.Add(1)
		go func(di DomainInput) {
			defer wg.Done()

			successCount := 0
			var lastResult CheckResult

			for i := 0; i < ds.validationTries; i++ {
				if ds.interrupted() {
					return
				}

				result := ds.fetchForDomain(di, timeout)
				if len(testConfig.Sets) > 0 {
					result.Set = testConfig.Sets[0]
				}
				lastResult = result

				if result.Status != CheckStatusComplete {
					break
				}
				successCount++

				if i < ds.validationTries-1 {
					time.Sleep(validationRetryDelay)
				}
			}

			if successCount == ds.validationTries {
				log.DiscoveryLogf("    [%s] → OK (%.2f KB/s, %d bytes%s)", di.Domain, lastResult.Speed/1024, lastResult.BytesRead, nonOKStatusNote(lastResult))
				mu.Lock()
				results[di.Domain] = lastResult
				mu.Unlock()
			} else {
				lastResult.Status = CheckStatusFailed
				if ds.validationTries > 1 {
					lastResult.Error = fmt.Sprintf("%s (%d/%d tries)", lastResult.Error, successCount, ds.validationTries)
				}
				log.DiscoveryLogf("    [%s] → FAILED (%s)", di.Domain, lastResult.Error)
				mu.Lock()
				results[di.Domain] = lastResult
				mu.Unlock()
			}

			ds.CheckSuite.mu.Lock()
			ds.CompletedChecks++
			ds.CheckSuite.mu.Unlock()
		}(di)
	}

	wg.Wait()
	return results
}
