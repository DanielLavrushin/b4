package discovery

import (
	"fmt"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func (ds *DiscoverySuite) optimizeFakeSNI() ConfigPreset {
	log.DiscoveryLogf("  Optimizing FakeSNI with TTL scan + strategy rotation")

	ds.CheckSuite.mu.Lock()
	ds.TotalChecks += 3 + ds.ttlSweepChecks()
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

func (ds *DiscoverySuite) optimizeCombo() ConfigPreset {
	log.DiscoveryLogf("  Optimizing Combo with TTL scan + strategy rotation")

	ds.CheckSuite.mu.Lock()
	ds.TotalChecks += 11 + ds.ttlSweepChecks()
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
		if ds.interrupted() {
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
			if ds.interrupted() {
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
	if _, ok := ds.getOptimalTTL(); ok {
		base.Faking.SNI = true
		base.Faking.Strategy = "pastseq"
		ds.applyBestPayload(&base.Faking)
	} else {
		base.Faking.SNI = false
	}

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
	if _, ok := ds.getOptimalTTL(); ok {
		base.Faking.SNI = true
		base.Faking.Strategy = "pastseq"
		ds.applyBestPayload(&base.Faking)
	} else {
		base.Faking.SNI = false
	}

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
		if ds.interrupted() {
			return bestPreset
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

// FindOptimalPosition binary searches for minimum working fragmentation position
func (ds *DiscoverySuite) findOptimalPosition(basePreset ConfigPreset, maxPos int) (int, float64) {
	low, high := 1, maxPos
	var bestPos int
	var bestSpeed float64

	log.DiscoveryLogf("Binary search for optimal position (range %d-%d)", low, high)

	for low < high {
		if ds.interrupted() {
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
