package discovery

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
	"github.com/daniellavrushin/b4/nfq"
)

type FailureMode string

const (
	FailureRSTImmediate FailureMode = "rst_immediate"
	FailureTimeout      FailureMode = "timeout"
	FailureTLSError     FailureMode = "tls_error"
	FailureUnknown      FailureMode = "unknown"

	validationRetryDelay = 100 * time.Millisecond
)

func NewDiscoverySuite(inputs []string, pool *nfq.Pool, skipDNS bool, skipCache bool, payloadFiles []string, validationTries int, tlsVersion string, ipVersion string, flowMark uint) *DiscoverySuite {
	domainInputs := parseDiscoveryInputs(inputs)
	if len(domainInputs) == 0 {
		suite := NewCheckSuite(domainInputs)
		ds := &DiscoverySuite{CheckSuite: suite}
		ds.initCancelContext()
		return ds
	}
	suite := NewCheckSuite(domainInputs)

	// Ensure validationTries is at least 1
	if validationTries < 1 {
		validationTries = 1
	}

	if tlsVersion == "" {
		tlsVersion = "auto"
	}

	if ipVersion == "" {
		ipVersion = "auto"
	}

	domainResults := make(map[string]*DomainDiscoveryResult)
	for _, di := range domainInputs {
		domainResults[di.Domain] = &DomainDiscoveryResult{
			Domain:  di.Domain,
			Url:     di.CheckURL,
			Results: make(map[string]*DomainPresetResult),
		}
	}

	ds := &DiscoverySuite{
		CheckSuite:      suite,
		pool:            pool,
		domainResults:   domainResults,
		dnsResults:      make(map[string]*DNSDiscoveryResult),
		workingPayloads: []PayloadTestResult{},
		bestPayload:     config.FakePayloadDefault1,
		skipDNS:         skipDNS,
		skipCache:       skipCache,
		validationTries: validationTries,
		tlsVersion:      tlsVersion,
		ipVersion:       ipVersion,
		flowMark:        flowMark,
	}

	if len(payloadFiles) > 0 {
		cfg := pool.GetFirstWorkerConfig()
		if cfg != nil {
			ds.customPayloads = loadCustomPayloads(cfg, payloadFiles)
		}
	}

	ds.initCancelContext()
	return ds
}

func parseDiscoveryInput(input string) (domain string, testURL string) {
	input = strings.TrimSpace(input)
	input = strings.Trim(input, "\"'`")
	input = strings.TrimSpace(input)

	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		u, err := url.Parse(input)
		if err == nil && u.Host != "" {
			return u.Hostname(), input
		}
	}

	return input, "https://" + input + "/"
}

func parseDiscoveryInputs(inputs []string) []DomainInput {
	seen := make(map[string]bool)
	var result []DomainInput
	for _, input := range inputs {
		domain, checkURL := parseDiscoveryInput(input)
		if domain == "" {
			continue
		}
		if !seen[domain] {
			seen[domain] = true
			result = append(result, DomainInput{Domain: domain, CheckURL: checkURL})
		}
	}
	return result
}

func (ds *DiscoverySuite) RunDiscovery() {
	defer ds.ctxCancel()
	log.DiscoveryLogf("═══════════════════════════════════════")
	domainNames := make([]string, len(ds.Domains))
	for i, di := range ds.Domains {
		domainNames[i] = di.Domain
	}
	tlsSet := ds.tlsVersion != "" && ds.tlsVersion != "auto"
	ipSet := ds.ipVersion != "" && ds.ipVersion != "auto"
	switch {
	case tlsSet && ipSet:
		log.DiscoveryLogf("Starting discovery for %d domains: %v (TLS: %s, IP: %s)", len(ds.Domains), domainNames, ds.tlsVersion, ds.ipVersion)
	case tlsSet:
		log.DiscoveryLogf("Starting discovery for %d domains: %v (TLS: %s)", len(ds.Domains), domainNames, ds.tlsVersion)
	case ipSet:
		log.DiscoveryLogf("Starting discovery for %d domains: %v (IP: %s)", len(ds.Domains), domainNames, ds.ipVersion)
	default:
		log.DiscoveryLogf("Starting discovery for %d domains: %v", len(ds.Domains), domainNames)
	}
	log.DiscoveryLogf("═══════════════════════════════════════")

	defer func() {
		ds.EndTime = time.Now()
	}()

	select {
	case <-ds.cancel:
		ds.setStatus(CheckStatusCanceled)
		ds.finalize()
		ds.logDiscoverySummary()
		return
	default:
	}

	ds.setStatus(CheckStatusRunning)

	phase1Count := len(GetPhase1Presets())
	ds.CheckSuite.mu.Lock()
	ds.TotalChecks = phase1Count * len(ds.Domains)
	ds.CheckSuite.mu.Unlock()

	ds.cfg = ds.pool.GetFirstWorkerConfig()

	if ds.cfg == nil {
		log.Errorf("Failed to get original configuration")
		ds.setStatus(CheckStatusFailed)
		return
	}

	ds.discoveryCache = LoadDiscoveryCache(ds.cfg.ConfigPath)
	defer ds.saveResultsToCache()

	ds.networkBaseline = ds.measureNetworkBaseline()

	// DNS phase: per-domain
	anyDNSPoisoned := false
	if ds.skipDNS {
		log.DiscoveryLogf("Skipping DNS discovery (user requested)")
	} else {
		ds.setPhase(PhaseDNS)
		for _, di := range ds.Domains {
			ds.setCurrentDomain(di.Domain)
			log.DiscoveryLogf("Running DNS discovery for %s", di.Domain)
			dnsResult := ds.runDNSDiscoveryForDomain(di.Domain)
			ds.dnsResults[di.Domain] = dnsResult
			ds.domainResults[di.Domain].DNSResult = dnsResult

			if dnsResult != nil && len(dnsResult.ExpectedIPs) > 0 {
				log.DiscoveryLogf("  [%s] Stored %d target IPs: %v", di.Domain, len(dnsResult.ExpectedIPs), dnsResult.ExpectedIPs)
			}

			if dnsResult != nil && dnsResult.IsPoisoned {
				anyDNSPoisoned = true
				if dnsResult.hasWorkingConfig() {
					log.DiscoveryLogf("  [%s] DNS poisoned - bypass config found", di.Domain)
				} else if len(dnsResult.ExpectedIPs) > 0 {
					log.DiscoveryLogf("  [%s] DNS poisoned, no bypass - using direct IPs", di.Domain)
				} else {
					log.DiscoveryLogf("  [%s] DNS poisoned but no expected IP known", di.Domain)
				}
			}
		}

		// Apply DNS config if any domain needs it
		if anyDNSPoisoned {
			ds.applyBestDNSConfig()
		}
	}

	// Phase 0: Test previously successful cached configurations
	var cachedPresets []ConfigPreset
	if ds.skipCache {
		log.DiscoveryLogf("Skipping cached strategies (user requested)")
	} else {
		cachedPresets = ds.discoveryCache.GetCachedPresets()
	}
	phase1Presets := GetPhase1Presets()

	ds.CheckSuite.mu.Lock()
	ds.TotalChecks = (len(phase1Presets) + len(cachedPresets)) * len(ds.Domains)
	ds.CheckSuite.mu.Unlock()

	if len(cachedPresets) > 0 {
		ds.setPhase(PhaseCached)
		log.DiscoveryLogf("Phase 0: Testing %d cached configurations across %d domains", len(cachedPresets), len(ds.Domains))

		for _, preset := range cachedPresets {
			select {
			case <-ds.cancel:
				ds.finalize()
				ds.logDiscoverySummary()
				return
			default:
			}

			if preset.Config.Faking.SNIType == config.FakePayloadRandom {
				ds.applyBestPayload(&preset.Config.Faking)
			}
			results := ds.testPresetAllDomains(preset)
			ds.storeResultsMulti(preset, results)
		}
	}

	// Phase 1: Strategy detection across all domains
	ds.setPhase(PhaseStrategy)
	workingFamilies, baselineSpeed, allBaselineWorks := ds.runPhase1Multi(phase1Presets)
	ds.determineBest(baselineSpeed)

	if allBaselineWorks {
		dnsNeeded := anyDNSPoisoned

		if !dnsNeeded {
			ds.CheckSuite.mu.Lock()
			for _, domainResult := range ds.domainResults {
				domainResult.BestPreset = "no-bypass"
				domainResult.BestSpeed = baselineSpeed
				domainResult.BestSuccess = true
				domainResult.BaselineSpeed = baselineSpeed
				domainResult.Improvement = 0
			}
			ds.CheckSuite.mu.Unlock()

			log.DiscoveryLogf("Verified: no DPI bypass needed for any domain")
			ds.finalize()
			ds.logDiscoverySummary()
			return
		}

		log.DiscoveryLogf("Baseline works but DNS bypass required - continuing")
	}

	if len(workingFamilies) == 0 {
		// Skip extended search if all domains have transport-level blocking.
		// When neither system nor reference IPs can even establish a TCP connection,
		// no packet manipulation strategy will help — the blocking is at IP level.
		if ds.allDomainsTransportBlocked() {
			log.Warnf("All domains have transport-level blocking (IP blocked) — extended search skipped")
			ds.finalize()
			ds.logDiscoverySummary()
			return
		}

		log.Warnf("Phase 1 found no working families, trying extended search")

		ds.setPhase(PhaseOptimize)
		workingFamilies = ds.runExtendedSearch()

		if len(workingFamilies) == 0 {
			log.Warnf("No working bypass strategies found")
			ds.finalize()
			ds.logDiscoverySummary()
			return
		}
	}

	log.Infof("Phase 1 complete: %d working families: %v", len(workingFamilies), workingFamilies)

	// Phase 2: Optimization using representative domain per family
	ds.setPhase(PhaseOptimize)
	bestParams := ds.runPhase2WithRepresentative(workingFamilies)
	ds.determineBest(baselineSpeed)

	// Phase 3: Combinations across all domains
	if len(workingFamilies) >= 2 {
		ds.setPhase(PhaseCombination)
		ds.runPhase3Multi(workingFamilies, bestParams)
	}

	ds.determineBest(baselineSpeed)
	ds.finalize()
	ds.logDiscoverySummary()
}

// runPhase1Multi tests all Phase 1 presets across all domains.
// Each preset config is applied ONCE and all domains are tested.
func (ds *DiscoverySuite) runPhase1Multi(presets []ConfigPreset) ([]StrategyFamily, float64, bool) {
	var workingFamilies []StrategyFamily

	log.DiscoveryLogf("Phase 1: Testing %d strategy families across %d domains", len(presets), len(ds.Domains))

	// Test baseline (no-bypass) across all domains
	baselineResults := ds.testPresetAllDomains(presets[0])
	ds.storeResultsMulti(presets[0], baselineResults)

	allBaselineWorks := true
	var totalBaselineSpeed float64
	baselineCount := 0
	for _, r := range baselineResults {
		if r.Status != CheckStatusComplete {
			allBaselineWorks = false
		} else {
			totalBaselineSpeed += r.Speed
			baselineCount++
		}
	}
	var baselineSpeed float64
	if baselineCount > 0 {
		baselineSpeed = totalBaselineSpeed / float64(baselineCount)
	}

	if allBaselineWorks {
		log.DiscoveryLogf("  Baseline succeeded for all domains - verifying with bypass test...")
	}

	// Payload detection uses the primary domain
	ds.detectWorkingPayloads(presets)

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
		select {
		case <-ds.cancel:
			return workingFamilies, baselineSpeed, allBaselineWorks
		default:
		}

		ds.applyBestPayload(&preset.Config.Faking)
		domainResults := ds.testPresetAllDomains(preset)
		ds.storeResultsMulti(preset, domainResults)

		// A family "works" if it succeeds for ANY domain
		for _, r := range domainResults {
			if r.Status == CheckStatusComplete && r.Speed > baselineSpeed*0.8 && preset.Family != FamilyNone {
				if !containsFamily(workingFamilies, preset.Family) {
					workingFamilies = append(workingFamilies, preset.Family)
				}
				break
			}
		}
	}

	// Check if bypass is significantly faster for any domain (even if baseline works)
	if allBaselineWorks && len(workingFamilies) > 0 {
		for _, domainResult := range ds.domainResults {
			if domainResult.BestSpeed > baselineSpeed*1.5 {
				log.DiscoveryLogf("  Bypass 50%%+ faster than baseline - DPI bypass needed")
				allBaselineWorks = false
				break
			}
		}
	}

	return workingFamilies, baselineSpeed, allBaselineWorks
}

// filterTestedPresets removes presets we've already tested
func (ds *DiscoverySuite) filterTestedPresets(presets []ConfigPreset) []ConfigPreset {
	filtered := []ConfigPreset{}
	for _, p := range presets {
		if p.Name == "no-bypass" || p.Name == "combo-pastseq" {
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
		select {
		case <-ds.cancel:
			return bestParams
		default:
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
		select {
		case <-ds.cancel:
			return
		default:
		}

		ds.applyBestPayload(&preset.Config.Faking)
		results := ds.testPresetAllDomains(preset)
		ds.storeResultsMulti(preset, results)
	}
}

func (ds *DiscoverySuite) optimizeFakeSNI() ConfigPreset {
	log.DiscoveryLogf("  Optimizing FakeSNI with TTL scan + strategy rotation")

	ds.CheckSuite.mu.Lock()
	ds.TotalChecks += 13
	ds.CheckSuite.mu.Unlock()

	base := baseConfig()
	base.Faking.SNI = true
	base.Faking.Strategy = "pastseq"
	base.Faking.SeqOffset = 10000
	base.Faking.SNISeqLength = 1
	ds.applyBestPayload(&base.Faking)
	base.Fragmentation.Strategy = "combo"
	base.Fragmentation.SNIPosition = 1
	base.Fragmentation.ReverseOrder = true

	basePreset := ConfigPreset{
		Name:   "fake-optimize",
		Family: FamilyFakeSNI,
		Phase:  PhaseOptimize,
		Config: base,
	}

	optimalTTL, speed := ds.findOptimalTTL(basePreset)
	if optimalTTL == 0 {
		log.DiscoveryLogf("  No working TTL found for FakeSNI")
		return basePreset
	}

	basePreset.Config.Faking.TTL = optimalTTL
	basePreset.Name = fmt.Sprintf("fake-ttl%d-optimized", optimalTTL)

	strategies := []string{"pastseq", "timestamp", "ttl", "randseq"}
	var bestStrategy string = "ttl"
	var bestSpeed = speed

	for _, strat := range strategies {
		if strat == "ttl" {
			continue
		}

		preset := basePreset
		preset.Name = fmt.Sprintf("fake-%s-ttl%d", strat, optimalTTL)
		preset.Config.Faking.Strategy = strat
		if strat == "timestamp" {
			preset.Config.Faking.TimestampDecrease = 600000
		}

		result := ds.testPresetWithBestPayload(preset)
		ds.storeResult(preset, result)

		if result.Status == CheckStatusComplete && result.Speed > bestSpeed {
			bestStrategy = strat
			bestSpeed = result.Speed
		}
	}

	basePreset.Config.Faking.Strategy = bestStrategy
	if bestStrategy == "timestamp" {
		basePreset.Config.Faking.TimestampDecrease = 600000
	}
	basePreset.Name = fmt.Sprintf("fake-%s-ttl%d-optimized", bestStrategy, optimalTTL)

	log.DiscoveryLogf("  Best FakeSNI: TTL=%d, strategy=%s (%.2f KB/s)", optimalTTL, bestStrategy, bestSpeed/1024)
	return basePreset
}

func (ds *DiscoverySuite) canceled() bool {
	select {
	case <-ds.cancel:
		return true
	default:
		return false
	}
}

func (ds *DiscoverySuite) initCancelContext() {
	ds.ctx, ds.ctxCancel = context.WithCancel(context.Background())
	go func() {
		select {
		case <-ds.cancel:
			ds.ctxCancel()
		case <-ds.ctx.Done():
		}
	}()
}

func (ds *DiscoverySuite) fetchContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ds.ctx, timeout)
}

func (ds *DiscoverySuite) optimizeCombo() ConfigPreset {
	log.DiscoveryLogf("  Optimizing Combo with TTL scan + strategy rotation")

	ds.CheckSuite.mu.Lock()
	ds.TotalChecks += 21
	ds.CheckSuite.mu.Unlock()

	combo := comboFrag()
	base := baseConfig()
	base.Faking.SNI = true
	base.Faking.Strategy = "pastseq"
	base.Faking.SeqOffset = 10000
	base.Faking.SNISeqLength = 1
	ds.applyBestPayload(&base.Faking)
	base.Fragmentation = combo
	base.TCP = config.TCPConfig{
		ConnBytesLimit: 19,
		Seg2Delay:      20,
		Seg2DelayMax:   60,
	}

	basePreset := ConfigPreset{
		Name:   "combo-optimize",
		Family: FamilyCombo,
		Phase:  PhaseOptimize,
		Config: base,
	}

	// Step 1: Find optimal TTL via linear scan
	optimalTTL, speed := ds.findOptimalTTL(basePreset)
	if optimalTTL == 0 {
		log.DiscoveryLogf("  No working TTL found for Combo, falling back to preset optimization")
		return ds.optimizeWithPresets(FamilyCombo)
	}

	basePreset.Config.Faking.TTL = optimalTTL
	basePreset.Name = fmt.Sprintf("combo-ttl%d-optimized", optimalTTL)

	// Step 2: Test faking strategies with optimal TTL
	bestStrategy, bestSpeed := ds.optimizeComboStrategy(basePreset, optimalTTL, speed)
	basePreset.Config.Faking.Strategy = bestStrategy
	if bestStrategy == "timestamp" {
		basePreset.Config.Faking.TimestampDecrease = 600000
	}

	// Step 3: Test shuffle modes and delays with best strategy + TTL
	bestShuffle, bestDelay, bestSpeed := ds.optimizeComboShuffleDelay(basePreset, optimalTTL, bestStrategy, bestSpeed)
	basePreset.Config.Fragmentation.Combo.ShuffleMode = bestShuffle
	basePreset.Config.Fragmentation.Combo.FirstDelayMs = bestDelay
	basePreset.Name = fmt.Sprintf("combo-%s-ttl%d-optimized", bestStrategy, optimalTTL)

	log.DiscoveryLogf("  Best Combo: TTL=%d, strategy=%s, shuffle=%s, delay=%d (%.2f KB/s)",
		optimalTTL, bestStrategy, bestShuffle, bestDelay, bestSpeed/1024)
	return basePreset
}

func (ds *DiscoverySuite) optimizeComboStrategy(basePreset ConfigPreset, optimalTTL uint8, initialSpeed float64) (string, float64) {
	strategies := []string{"pastseq", "timestamp", "ttl", "randseq"}
	bestStrategy := "ttl" // TTL strategy was used during findOptimalTTL probe
	bestSpeed := initialSpeed

	for _, strat := range strategies {
		if ds.canceled() {
			return bestStrategy, bestSpeed
		}
		if strat == "ttl" {
			continue // Already tested during TTL search
		}

		preset := basePreset
		preset.Name = fmt.Sprintf("combo-%s-ttl%d", strat, optimalTTL)
		preset.Config.Faking.Strategy = strat
		if strat == "timestamp" {
			preset.Config.Faking.TimestampDecrease = 600000
		}

		result := ds.testPresetWithBestPayload(preset)
		ds.storeResult(preset, result)

		if result.Status == CheckStatusComplete && result.Speed > bestSpeed {
			bestStrategy = strat
			bestSpeed = result.Speed
		}
	}

	return bestStrategy, bestSpeed
}

func (ds *DiscoverySuite) optimizeComboShuffleDelay(basePreset ConfigPreset, optimalTTL uint8, strategy string, initialSpeed float64) (string, int, float64) {
	shuffleModes := []string{"middle", "full", "edges"}
	delays := []int{30, 100, 200}
	bestShuffle := basePreset.Config.Fragmentation.Combo.ShuffleMode
	bestDelay := basePreset.Config.Fragmentation.Combo.FirstDelayMs
	bestSpeed := initialSpeed

	for _, mode := range shuffleModes {
		for _, d := range delays {
			if ds.canceled() {
				return bestShuffle, bestDelay, bestSpeed
			}
			if mode == bestShuffle && d == bestDelay {
				continue
			}

			preset := basePreset
			preset.Name = fmt.Sprintf("combo-%s-%s-d%d-ttl%d", strategy, mode, d, optimalTTL)
			preset.Config.Fragmentation.Combo.ShuffleMode = mode
			preset.Config.Fragmentation.Combo.FirstDelayMs = d

			result := ds.testPresetWithBestPayload(preset)
			ds.storeResult(preset, result)

			if result.Status == CheckStatusComplete && result.Speed > bestSpeed {
				bestShuffle = mode
				bestDelay = d
				bestSpeed = result.Speed
			}
		}
	}

	return bestShuffle, bestDelay, bestSpeed
}

func (ds *DiscoverySuite) optimizeTCPFrag() ConfigPreset {
	log.DiscoveryLogf("  Optimizing TCPFrag with binary search")

	ds.CheckSuite.mu.Lock()
	ds.TotalChecks += 6
	ds.CheckSuite.mu.Unlock()

	base := baseConfig()
	base.Fragmentation.Strategy = "tcp"
	base.Fragmentation.ReverseOrder = true
	base.Faking.SNI = true

	base.Faking.TTL = ds.getOptimalTTL()

	base.Faking.Strategy = "pastseq"
	ds.applyBestPayload(&base.Faking)

	basePreset := ConfigPreset{
		Name:   "tcp-optimize",
		Family: FamilyTCPFrag,
		Phase:  PhaseOptimize,
		Config: base,
	}

	optimalPos, speed := ds.findOptimalPosition(basePreset, 16)
	if optimalPos == 0 {
		optimalPos = 1
	}

	basePreset.Config.Fragmentation.SNIPosition = optimalPos
	basePreset.Name = fmt.Sprintf("tcp-pos%d-optimized", optimalPos)

	middlePreset := basePreset
	middlePreset.Name = fmt.Sprintf("tcp-pos%d-middle", optimalPos)
	middlePreset.Config.Fragmentation.MiddleSNI = true

	result := ds.testPresetWithBestPayload(middlePreset)
	ds.storeResult(middlePreset, result)

	if result.Status == CheckStatusComplete && result.Speed > speed {
		basePreset = middlePreset
		speed = result.Speed
		log.DiscoveryLogf("  MiddleSNI improves speed: %.2f KB/s", result.Speed/1024)
	}

	log.DiscoveryLogf("  Best TCPFrag: position=%d (%.2f KB/s)", optimalPos, speed/1024)
	return basePreset
}

func (ds *DiscoverySuite) optimizeTLSRec() ConfigPreset {
	log.DiscoveryLogf("  Optimizing TLSRec with binary search")

	ds.CheckSuite.mu.Lock()
	ds.TotalChecks += 6
	ds.CheckSuite.mu.Unlock()

	base := baseConfig()
	base.Fragmentation.Strategy = "tls"
	base.Faking.SNI = true

	base.Faking.TTL = ds.getOptimalTTL()

	base.Faking.Strategy = "pastseq"
	ds.applyBestPayload(&base.Faking)

	basePreset := ConfigPreset{
		Name:   "tls-optimize",
		Family: FamilyTLSRec,
		Phase:  PhaseOptimize,
		Config: base,
	}

	low, high := 1, 64
	var bestPos int
	var bestSpeed float64

	for low < high {
		mid := (low + high) / 2

		preset := basePreset
		preset.Name = fmt.Sprintf("tls-pos-search-%d", mid)
		preset.Config.Fragmentation.TLSRecordPosition = mid

		result := ds.testPresetWithBestPayload(preset)
		ds.storeResult(preset, result)

		if result.Status == CheckStatusComplete {
			bestPos = mid
			bestSpeed = result.Speed
			high = mid
		} else {
			low = mid + 1
		}
	}

	if bestPos > 0 {
		basePreset.Config.Fragmentation.TLSRecordPosition = bestPos
		basePreset.Name = fmt.Sprintf("tls-pos%d-optimized", bestPos)
	}

	log.DiscoveryLogf("  Best TLSRec: position=%d (%.2f KB/s)", bestPos, bestSpeed/1024)
	return basePreset
}

func (ds *DiscoverySuite) optimizeWithPresets(family StrategyFamily) ConfigPreset {
	presets := GetPhase2Presets(family)
	if len(presets) == 0 {
		return ConfigPreset{Family: family}
	}

	ds.CheckSuite.mu.Lock()
	ds.TotalChecks += len(presets)
	ds.CheckSuite.mu.Unlock()

	log.DiscoveryLogf("  Optimizing %s with %d presets", family, len(presets))

	var bestPreset ConfigPreset
	var bestSpeed float64

	for _, preset := range presets {
		select {
		case <-ds.cancel:
			return bestPreset
		default:
		}

		result := ds.testPresetWithBestPayload(preset)
		ds.storeResult(preset, result)

		if result.Status == CheckStatusComplete && result.Speed > bestSpeed {
			bestSpeed = result.Speed
			bestPreset = preset
			ds.applyBestPayload(&bestPreset.Config.Faking)
		}
	}

	return bestPreset
}

// testPresetInternal tests a single preset against the primary domain.
// Used during Phase 2 optimization (via withSingleDomain helper).
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
			log.DiscoveryLogf("    → OK (%.2f KB/s, %d bytes) - %d/%d tries succeeded",
				lastResult.Speed/1024, lastResult.BytesRead, successCount, ds.validationTries)
		} else {
			log.DiscoveryLogf("    → OK (%.2f KB/s, %d bytes)", lastResult.Speed/1024, lastResult.BytesRead)
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
func (ds *DiscoverySuite) testPresetAllDomains(preset ConfigPreset) map[string]CheckResult {
	log.DiscoveryLogf("  Testing '%s' across %d domains...", preset.Name, len(ds.Domains))

	results := make(map[string]CheckResult)

	testConfig := ds.buildTestConfigMulti(preset)

	if err := ds.pool.UpdateConfig(testConfig); err != nil {
		log.DiscoveryLogf("    → FAILED (config error: %v)", err)
		for _, di := range ds.Domains {
			results[di.Domain] = CheckResult{
				Domain: di.Domain,
				Status: CheckStatusFailed,
				Error:  err.Error(),
			}
		}
		ds.CheckSuite.mu.Lock()
		ds.CompletedChecks += len(ds.Domains)
		ds.CheckSuite.mu.Unlock()
		return results
	}

	time.Sleep(time.Duration(ds.cfg.System.Checker.ConfigPropagateMs) * time.Millisecond)

	timeout := time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec) * time.Second

	var mu sync.Mutex
	var wg sync.WaitGroup

spawn:
	for _, di := range ds.Domains {
		select {
		case <-ds.cancel:
			break spawn
		default:
		}

		wg.Add(1)
		go func(di DomainInput) {
			defer wg.Done()

			successCount := 0
			var lastResult CheckResult

			for i := 0; i < ds.validationTries; i++ {
				select {
				case <-ds.cancel:
					return
				default:
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
				log.DiscoveryLogf("    [%s] → OK (%.2f KB/s, %d bytes)", di.Domain, lastResult.Speed/1024, lastResult.BytesRead)
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

func (ds *DiscoverySuite) setCurrentDomain(domain string) {
	ds.CheckSuite.mu.Lock()
	ds.CurrentDomain = domain
	ds.CheckSuite.mu.Unlock()
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

// collectTargetIPs returns deduplicated IPs from DNS discovery results, limited to maxIPs.
func (ds *DiscoverySuite) collectTargetIPs(domain string, maxIPs int) []string {
	dnsResult := ds.dnsResults[domain]
	if dnsResult == nil {
		return nil
	}

	seen := make(map[string]bool)
	var ips []string
	for _, ip := range dnsResult.ExpectedIPs {
		if !seen[ip] {
			seen[ip] = true
			ips = append(ips, ip)
		}
	}
	for _, probe := range dnsResult.ProbeResults {
		if probe.ResolvedIP != "" && !seen[probe.ResolvedIP] {
			seen[probe.ResolvedIP] = true
			ips = append(ips, probe.ResolvedIP)
		}
	}
	if maxIPs > 0 && len(ips) > maxIPs {
		ips = ips[:maxIPs]
	}
	return ips
}

func (ds *DiscoverySuite) fetchForDomain(di DomainInput, timeout time.Duration) CheckResult {
	// Use IPs already collected during DNS discovery — no fresh DNS lookups.
	// Fresh lookups are slow (poisoned DNS can timeout) and redundant since
	// DNS discovery already gathered all valid IPs from DoH + system resolver.
	// Limit to 2 IPs to avoid slow sequential fallback.
	allIPs := ds.collectTargetIPs(di.Domain, 2)

	geoip, geosite := GetCDNCategories(di.Domain)
	if len(geoip) > 0 || len(geosite) > 0 {
		// CDN domains are matched by geoip/geosite in a real config, but the
		// validation fetch still needs a resolvable IP. Pin the IP discovered
		// during the DNS phase; only fall back to system DNS when none exist
		// (otherwise a poisoned system resolver fails every preset).
		ip := ""
		if len(allIPs) > 0 {
			ip = allIPs[0]
		}
		return ds.fetchUsingIPForDomain(di, timeout, ip)
	}

	for _, ip := range allIPs {
		result := ds.fetchUsingIPForDomain(di, timeout, ip)
		if result.Status == CheckStatusComplete {
			log.Tracef("Success with IP %s for %s", ip, di.Domain)
			return result
		}
		log.Tracef("IP %s failed for %s, trying next", ip, di.Domain)
	}

	if len(allIPs) > 0 {
		return CheckResult{
			Domain: di.Domain,
			Status: CheckStatusFailed,
			Error:  fmt.Sprintf("all %d IPs failed", len(allIPs)),
		}
	}

	return ds.fetchUsingIPForDomain(di, timeout, "")
}

// tlsFilterVersion converts the discovery TLS version setting to a config TLS filter value.
func (ds *DiscoverySuite) tlsFilterVersion() string {
	switch ds.tlsVersion {
	case "tls12":
		return "1.2"
	case "tls13":
		return "1.3"
	default:
		return ""
	}
}

// ipFilterVersion converts the discovery IP version setting to a config IP filter value.
func (ds *DiscoverySuite) ipFilterVersion() string {
	switch ds.ipVersion {
	case "ipv4":
		return "4"
	case "ipv6":
		return "6"
	default:
		return ""
	}
}

// dialNetwork forces the probe address family ("tcp4"/"tcp6") for an explicit
// IP version run, or "" to leave family selection to the resolver/OS.
func (ds *DiscoverySuite) dialNetwork() string {
	switch ds.ipVersion {
	case "ipv4":
		return "tcp4"
	case "ipv6":
		return "tcp6"
	default:
		return ""
	}
}

func (ds *DiscoverySuite) tlsConfig() *tls.Config {
	cfg := &tls.Config{InsecureSkipVerify: true}
	switch ds.tlsVersion {
	case "tls12":
		cfg.MinVersion = tls.VersionTLS12
		cfg.MaxVersion = tls.VersionTLS12
	case "tls13":
		cfg.MinVersion = tls.VersionTLS13
		cfg.MaxVersion = tls.VersionTLS13
	}
	return cfg
}

func (ds *DiscoverySuite) fetchUsingIPForDomain(di DomainInput, timeout time.Duration, ip string) CheckResult {
	result := CheckResult{
		Domain:    di.Domain,
		Status:    CheckStatusRunning,
		Timestamp: time.Now(),
	}

	ctx, cancel := ds.fetchContext(timeout)
	defer cancel()

	transport := &http.Transport{
		TLSClientConfig:       ds.tlsConfig(),
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       timeout,
	}

	baseDialer := netprobe.Dialer(int(ds.flowMark), timeout/2, timeout)
	forcedNet := ds.dialNetwork()

	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if forcedNet != "" {
			network = forcedNet
		}
		if ip != "" {
			_, port, _ := net.SplitHostPort(addr)
			if port == "" {
				port = "443"
			}
			directAddr := net.JoinHostPort(ip, port)
			log.Tracef("DNS bypass: connecting to %s instead of %s", directAddr, addr)
			return baseDialer.DialContext(ctx, network, directAddr)
		}
		return baseDialer.DialContext(ctx, network, addr)
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", di.CheckURL, nil)
	if err != nil {
		result.Status = CheckStatusFailed
		result.Error = err.Error()
		return result
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		result.Status = CheckStatusFailed
		_, detail := netprobe.ClassifyTLSError(err)
		result.Error = detail
		result.Duration = time.Since(start)
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.ContentSize = resp.ContentLength

	// Check for ISP block page indicators before reading body.
	if resp.StatusCode == 451 {
		result.Status = CheckStatusFailed
		result.Error = "ISP block page (HTTP 451)"
		result.Duration = time.Since(start)
		return result
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		if netprobe.IsBlockPageRedirect(loc) {
			result.Status = CheckStatusFailed
			result.Error = "ISP block page (redirect to " + loc + ")"
			result.Duration = time.Since(start)
			return result
		}
	}

	buf := make([]byte, 16*1024)
	tailBuf := make([]byte, 0, 64)     // rolling tail for </body></html> detection
	headBuf := make([]byte, 0, 4*1024) // first 4KB for ISP block page detection
	var bytesRead int64
	lastProgress := time.Now()

	maxRead := int64(100 * 1024)
	if result.ContentSize > 0 && result.ContentSize < maxRead {
		maxRead = result.ContentSize
	}

	for bytesRead < maxRead {
		select {
		case <-ctx.Done():
			goto evaluate
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			bytesRead += int64(n)
			lastProgress = time.Now()
			if len(headBuf) < 4*1024 {
				headBuf = append(headBuf, buf[:n]...)
				if len(headBuf) > 4*1024 {
					headBuf = headBuf[:4*1024]
				}
			}
			tailBuf = append(tailBuf, buf[:n]...)
			if len(tailBuf) > 64 {
				tailBuf = tailBuf[len(tailBuf)-64:]
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			result.Status = CheckStatusFailed
			result.Error = fmt.Sprintf("read error after %d bytes: %v", bytesRead, err)
			result.Duration = time.Since(start)
			result.BytesRead = bytesRead
			return result
		}

		if time.Since(lastProgress) > 2*time.Second {
			result.Status = CheckStatusFailed
			result.Error = fmt.Sprintf("stalled after %d bytes", bytesRead)
			result.Duration = time.Since(start)
			result.BytesRead = bytesRead
			return result
		}
	}

evaluate:
	duration := time.Since(start)
	result.Duration = duration
	result.BytesRead = bytesRead

	if duration.Seconds() > 0 {
		result.Speed = float64(bytesRead) / duration.Seconds()
	}

	// Check for ISP block page in response body before marking as success.
	if blockErr := netprobe.DetectBlockPageBody(headBuf); blockErr != "" {
		result.Status = CheckStatusFailed
		result.Error = blockErr
		return result
	}

	if len(tailBuf) > 0 {
		tailLower := bytes.ToLower(tailBuf)
		if bytes.Contains(tailLower, []byte("</body>")) && bytes.Contains(tailLower, []byte("</html>")) {
			result.Status = CheckStatusComplete
			return result
		}
	}

	if result.ContentSize > 0 {
		expectedBytes := result.ContentSize
		if expectedBytes > 100*1024 {
			expectedBytes = 100 * 1024
		}

		if bytesRead < expectedBytes*9/10 {
			result.Status = CheckStatusFailed
			result.Error = fmt.Sprintf("truncated: %d/%d bytes (%.0f%%)",
				bytesRead, expectedBytes, float64(bytesRead)*100/float64(expectedBytes))
			return result
		}
	}

	result.Status = CheckStatusComplete
	return result
}

// storeResult stores a single-domain result (used during Phase 2 optimization via withSingleDomain).
func (ds *DiscoverySuite) storeResult(preset ConfigPreset, result CheckResult) {
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
		Set:        result.Set,
	}

	if result.Status == CheckStatusComplete && preset.Name != "no-bypass" {
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
}

// storeResultsMulti stores per-domain results from testPresetAllDomains.
func (ds *DiscoverySuite) storeResultsMulti(preset ConfigPreset, results map[string]CheckResult) {
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
			Set:        result.Set,
		}

		if result.Status == CheckStatusComplete && preset.Name != "no-bypass" {
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
}

func (ds *DiscoverySuite) determineBest(baselineSpeed float64) {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()

	for _, domainResult := range ds.domainResults {
		var bestPreset string
		var bestSpeed float64

		for presetName, result := range domainResult.Results {
			if result.Status == CheckStatusComplete && result.Speed > bestSpeed {
				if presetName == "no-bypass" {
					continue
				}
				bestPreset = presetName
				bestSpeed = result.Speed
			}
		}

		domainResult.BestPreset = bestPreset
		domainResult.BestSpeed = bestSpeed
		domainResult.BestSuccess = bestSpeed > 0
		domainResult.BaselineSpeed = baselineSpeed

		if baselineSpeed > 0 && bestSpeed > 0 {
			domainResult.Improvement = ((bestSpeed - baselineSpeed) / baselineSpeed) * 100
		}
	}
}

func (ds *DiscoverySuite) cdnDNSConfig() config.DNSConfig {
	if ds.discoveredDNS.Enabled {
		return ds.discoveredDNS
	}
	doh := "https://1.1.1.1/dns-query"
	if len(netprobe.WireDoHServers) > 0 {
		doh = netprobe.WireDoHServers[0]
	}
	return config.DNSConfig{Enabled: true, DoHURL: doh}
}

func (ds *DiscoverySuite) buildTestConfig(preset ConfigPreset) *config.Config {
	testSet := config.NewSetConfig()
	testSet.Name = preset.Name
	testSet.TCP = preset.Config.TCP
	testSet.UDP = preset.Config.UDP
	testSet.Fragmentation = preset.Config.Fragmentation
	testSet.Faking = preset.Config.Faking
	testSet.DNS = ds.discoveredDNS

	config.ApplySetDefaults(&testSet)

	if testSet.TCP.Win.Mode == "" {
		testSet.TCP.Win.Mode = config.ConfigOff
	}
	if testSet.TCP.Desync.Mode == "" {
		testSet.TCP.Desync.Mode = config.ConfigOff
	}

	if testSet.Faking.SNIMutation.Mode == "" {
		testSet.Faking.SNIMutation.Mode = config.ConfigOff
	}
	if testSet.Faking.SNIMutation.FakeSNIs == nil {
		testSet.Faking.SNIMutation.FakeSNIs = []string{}
	}

	if preset.Name == "no-bypass" {
		testSet.Enabled = false
		testSet.DNS = config.DNSConfig{}
	} else {
		testSet.Enabled = true
		testSet.Targets.SNIDomains = []string{ds.Domain}
		testSet.Targets.DomainsToMatch = []string{ds.Domain}
		testSet.Targets.TLSVersion = ds.tlsFilterVersion()
		testSet.Targets.IPVersion = ds.ipFilterVersion()

		geoip, geosite := GetCDNCategories(ds.Domain)
		if len(geoip) > 0 || len(geosite) > 0 {
			if len(geoip) > 0 {
				testSet.Targets.GeoIpCategories = geoip
			}
			if len(geosite) > 0 {
				testSet.Targets.GeoSiteCategories = geosite
			}

			if !ds.skipDNS {
				testSet.DNS = ds.cdnDNSConfig()
			}
			tempCfg := &config.Config{System: ds.cfg.System}
			domains, ips, err := tempCfg.GetTargetsForSet(&testSet)
			if err != nil {
				log.DiscoveryLogf("Discovery: failed to load CDN categories: %v", err)
			} else {
				log.Tracef("Discovery: CDN %s - loaded %d domains, %d IPs", ds.Domain, len(domains), len(ips))
			}
		} else {
			var ipsToAdd []string
			dnsResult := ds.dnsResults[ds.Domain]
			if dnsResult != nil {
				ipsToAdd = append(ipsToAdd, dnsResult.ExpectedIPs...)
				for _, probe := range dnsResult.ProbeResults {
					if probe.ResolvedIP != "" {
						found := false
						for _, ip := range ipsToAdd {
							if ip == probe.ResolvedIP {
								found = true
								break
							}
						}
						if !found {
							ipsToAdd = append(ipsToAdd, probe.ResolvedIP)
						}
					}
				}
			}

			if len(ipsToAdd) > 0 {
				cidrIPs := make([]string, len(ipsToAdd))
				for i, ip := range ipsToAdd {
					if strings.Contains(ip, "/") {
						cidrIPs[i] = ip
					} else if strings.Contains(ip, ":") {
						cidrIPs[i] = ip + "/128"
					} else {
						cidrIPs[i] = ip + "/32"
					}
				}
				testSet.Targets.IPs = cidrIPs
				testSet.Targets.IpsToMatch = cidrIPs
				log.Tracef("Discovery: added %d IPs to test config: %v", len(cidrIPs), cidrIPs)
			}
		}
	}

	return &config.Config{
		ConfigPath: ds.cfg.ConfigPath,
		Queue:      ds.cfg.Queue,
		System:     ds.cfg.System,
		Sets:       []*config.SetConfig{&testSet},
	}
}

// buildTestConfigMulti creates a test config targeting ALL domains simultaneously.
func (ds *DiscoverySuite) buildTestConfigMulti(preset ConfigPreset) *config.Config {
	testSet := config.NewSetConfig()
	testSet.Name = preset.Name
	testSet.TCP = preset.Config.TCP
	testSet.UDP = preset.Config.UDP
	testSet.Fragmentation = preset.Config.Fragmentation
	testSet.Faking = preset.Config.Faking
	testSet.DNS = ds.discoveredDNS

	config.ApplySetDefaults(&testSet)

	if testSet.TCP.Win.Mode == "" {
		testSet.TCP.Win.Mode = config.ConfigOff
	}
	if testSet.TCP.Desync.Mode == "" {
		testSet.TCP.Desync.Mode = config.ConfigOff
	}

	if testSet.Faking.SNIMutation.Mode == "" {
		testSet.Faking.SNIMutation.Mode = config.ConfigOff
	}
	if testSet.Faking.SNIMutation.FakeSNIs == nil {
		testSet.Faking.SNIMutation.FakeSNIs = []string{}
	}

	if preset.Name == "no-bypass" {
		testSet.Enabled = false
		testSet.DNS = config.DNSConfig{}
	} else {
		testSet.Enabled = true

		var allDomains []string
		var allIPs []string
		hasCDN := false

		for _, di := range ds.Domains {
			allDomains = append(allDomains, di.Domain)

			geoip, geosite := GetCDNCategories(di.Domain)
			if len(geoip) > 0 || len(geosite) > 0 {
				hasCDN = true
				testSet.Targets.GeoIpCategories = appendUnique(testSet.Targets.GeoIpCategories, geoip...)
				testSet.Targets.GeoSiteCategories = appendUnique(testSet.Targets.GeoSiteCategories, geosite...)
			}

			// Collect IPs from per-domain DNS results
			if dnsResult, ok := ds.dnsResults[di.Domain]; ok && dnsResult != nil {
				for _, ip := range dnsResult.ExpectedIPs {
					allIPs = appendUnique(allIPs, ip)
				}
				for _, probe := range dnsResult.ProbeResults {
					if probe.ResolvedIP != "" {
						allIPs = appendUnique(allIPs, probe.ResolvedIP)
					}
				}
			}
		}

		testSet.Targets.SNIDomains = allDomains
		testSet.Targets.DomainsToMatch = allDomains
		testSet.Targets.TLSVersion = ds.tlsFilterVersion()
		testSet.Targets.IPVersion = ds.ipFilterVersion()

		if len(allIPs) > 0 {
			cidrIPs := make([]string, 0, len(allIPs))
			for _, ip := range allIPs {
				if strings.Contains(ip, "/") {
					cidrIPs = append(cidrIPs, ip)
				} else if strings.Contains(ip, ":") {
					cidrIPs = append(cidrIPs, ip+"/128")
				} else {
					cidrIPs = append(cidrIPs, ip+"/32")
				}
			}
			testSet.Targets.IPs = cidrIPs
			testSet.Targets.IpsToMatch = cidrIPs
		}

		if hasCDN && !ds.skipDNS {
			testSet.DNS = ds.cdnDNSConfig()
		}

		if len(testSet.Targets.GeoIpCategories) > 0 || len(testSet.Targets.GeoSiteCategories) > 0 {
			tempCfg := &config.Config{System: ds.cfg.System}
			domains, ips, err := tempCfg.GetTargetsForSet(&testSet)
			if err != nil {
				log.DiscoveryLogf("Discovery: failed to load CDN categories: %v", err)
			} else {
				log.Tracef("Discovery: CDN - loaded %d domains, %d IPs", len(domains), len(ips))
			}
		}
	}

	return &config.Config{
		ConfigPath: ds.cfg.ConfigPath,
		Queue:      ds.cfg.Queue,
		System:     ds.cfg.System,
		Sets:       []*config.SetConfig{&testSet},
	}
}

func appendUnique(slice []string, items ...string) []string {
	for _, item := range items {
		found := false
		for _, existing := range slice {
			if existing == item {
				found = true
				break
			}
		}
		if !found {
			slice = append(slice, item)
		}
	}
	return slice
}

func (ds *DiscoverySuite) setStatus(status CheckStatus) {
	ds.CheckSuite.mu.Lock()
	ds.Status = status
	ds.CheckSuite.mu.Unlock()
}

// allDomainsTransportBlocked returns true if DNS discovery flagged all domains
// as transport-blocked (neither system nor reference IPs could connect).
func (ds *DiscoverySuite) allDomainsTransportBlocked() bool {
	if len(ds.dnsResults) == 0 {
		return false
	}
	for _, result := range ds.dnsResults {
		if result == nil || !result.TransportBlocked {
			return false
		}
	}
	return true
}

func (ds *DiscoverySuite) setPhase(phase DiscoveryPhase) {
	ds.CheckSuite.mu.Lock()
	ds.CurrentPhase = phase
	ds.CheckSuite.mu.Unlock()
}

func (ds *DiscoverySuite) finalize() {
	ds.CheckSuite.mu.Lock()
	ds.DomainDiscoveryResults = ds.domainResults
	ds.EndTime = time.Now()
	if ds.Status != CheckStatusCanceled {
		ds.Status = CheckStatusComplete
	}
	ds.CheckSuite.mu.Unlock()

	ds.buildStrategyGroups()

	// Persist results to history
	if ds.cfg != nil {
		SaveToHistory(ds.CheckSuite, ds.cfg.ConfigPath)
	}

	go func() {
		time.Sleep(30 * time.Second)
		suitesMu.Lock()
		delete(activeSuites, ds.Id)
		suitesMu.Unlock()
	}()
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
		if dr == nil || !dr.BestSuccess {
			continue
		}
		remaining[domain] = true
		for name, r := range dr.Results {
			if r == nil || r.Status != CheckStatusComplete || name == "no-bypass" {
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

	phaseRank := func(p DiscoveryPhase) int {
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

	betterWinner := func(a, b string) bool {
		ai, bi := presets[a], presets[b]
		ap, bp := phaseRank(ai.phase), phaseRank(bi.phase)
		if ap != bp {
			return ap < bp
		}
		if ai.priority != bi.priority {
			return ai.priority < bi.priority
		}
		return a < b
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

		groups = append(groups, StrategyGroup{
			WinnerPreset: winner,
			Family:       info.family,
			Domains:      groupDomains,
			Set:          info.set,
			MedianSpeed:  median,
		})
	}

	ds.StrategyGroups = groups
}

func (ds *DiscoverySuite) logDiscoverySummary() {
	ds.CheckSuite.mu.RLock()
	defer ds.CheckSuite.mu.RUnlock()

	duration := time.Since(ds.StartTime)

	log.DiscoveryLogf("═══════════════════════════════════════")
	log.DiscoveryLogf("Discovery complete for %d domains in %v", len(ds.Domains), duration.Round(time.Second))

	for _, di := range ds.Domains {
		domainResult := ds.domainResults[di.Domain]
		dnsResult := ds.dnsResults[di.Domain]

		// DNS status line
		if dnsResult != nil {
			switch {
			case dnsResult.TransportBlocked:
				log.DiscoveryLogf("  ⊘ [%s] IP-blocked: TCP connections fail to all known IPs — VPN/proxy required", di.Domain)
				continue
			case dnsResult.IsPoisoned && dnsResult.BestServer != "":
				log.DiscoveryLogf("  ⚡ [%s] DNS poisoned, bypassed via %s", di.Domain, dnsResult.BestServer)
			case dnsResult.IsPoisoned && dnsResult.NeedsFragment:
				log.DiscoveryLogf("  ⚡ [%s] DNS poisoned, bypassed via fragmented queries", di.Domain)
			case dnsResult.IsPoisoned:
				log.DiscoveryLogf("  ✗ [%s] DNS poisoned, no bypass found", di.Domain)
			}
		}

		if domainResult.BestSuccess {
			improvement := ""
			if domainResult.Improvement > 0 {
				improvement = fmt.Sprintf(" (+%.0f%% vs baseline)", domainResult.Improvement)
			}
			log.DiscoveryLogf("  ✓ [%s] Best: %s (%.2f KB/s%s)", di.Domain, domainResult.BestPreset, domainResult.BestSpeed/1024, improvement)
		} else {
			log.DiscoveryLogf("  ✗ [%s] No working DPI bypass found", di.Domain)
		}
	}

	log.DiscoveryLogf("═══════════════════════════════════════")
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
		select {
		case <-ds.cancel:
			return workingFamilies
		default:
		}

		presets := GetPhase2Presets(family)

		ds.CheckSuite.mu.Lock()
		ds.TotalChecks += len(presets)
		ds.CheckSuite.mu.Unlock()

		log.DiscoveryLogf("  Extended search: %s (%d variants)", family, len(presets))

		for _, preset := range presets {
			select {
			case <-ds.cancel:
				return workingFamilies
			default:
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

// FindOptimalPosition binary searches for minimum working fragmentation position
func (ds *DiscoverySuite) findOptimalPosition(basePreset ConfigPreset, maxPos int) (int, float64) {
	low, high := 1, maxPos
	var bestPos int
	var bestSpeed float64

	log.DiscoveryLogf("Binary search for optimal position (range %d-%d)", low, high)

	for low < high {
		if ds.canceled() {
			break
		}
		mid := (low + high) / 2

		preset := basePreset
		preset.Name = fmt.Sprintf("pos-search-%d", mid)
		preset.Config.Fragmentation.SNIPosition = mid

		result := ds.testPresetWithBestPayload(preset)
		ds.storeResult(preset, result)

		if result.Status == CheckStatusComplete {
			bestPos = mid
			bestSpeed = result.Speed
			high = mid
			log.DiscoveryLogf("  Position %d: SUCCESS (%.2f KB/s)", mid, result.Speed/1024)
		} else {
			low = mid + 1
			log.Tracef("  Position %d: FAILED", mid)
		}
	}

	return bestPos, bestSpeed
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
	if strings.Contains(err, "timeout") || strings.Contains(err, "deadline") {
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

func (ds *DiscoverySuite) measureNetworkBaseline() float64 {
	// Test a known-good domain to establish actual network speed
	timeout := time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec) * time.Second
	referenceDomain := ds.cfg.System.Checker.ReferenceDomain
	if referenceDomain == "" {
		referenceDomain = config.DefaultConfig.System.Checker.ReferenceDomain
	}

	log.DiscoveryLogf("Measuring network baseline using %s", referenceDomain)

	testURL := fmt.Sprintf("https://%s/", referenceDomain)
	ctx, cancel := ds.fetchContext(timeout)
	defer cancel()

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: ds.tlsConfig(),
			DialContext:     netprobe.Dialer(int(ds.flowMark), timeout/2, timeout).DialContext,
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		log.DiscoveryLogf("Failed to create baseline request: %v", err)
		return 0
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.DiscoveryLogf("Baseline measurement failed: %v", err)
		return 0
	}
	defer resp.Body.Close()

	bytesRead, _ := io.CopyN(io.Discard, resp.Body, 100*1024)
	duration := time.Since(start)

	if bytesRead == 0 || duration.Seconds() == 0 {
		return 0
	}

	speed := float64(bytesRead) / duration.Seconds()
	log.DiscoveryLogf("Network baseline: %.2f KB/s (%d bytes in %v)", speed/1024, bytesRead, duration)

	return speed
}
