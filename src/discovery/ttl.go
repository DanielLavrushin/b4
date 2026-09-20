package discovery

import (
	"fmt"

	"github.com/daniellavrushin/b4/log"
)

var ttlSweepValues = []uint8{3, 4, 5, 6, 7, 8, 9, 10, 12, 15}

// getOptimalTTL reports the lowest fake TTL that reached the censor without reaching the
// origin, and whether one exists at all. A sweep where every TTL failed is evidence that a
// fake packet does not help here, so the caller must drop the fake rather than pick a value.
func (ds *DiscoverySuite) getOptimalTTL() (uint8, bool) {
	if ds.optimalTTL > 0 {
		return ds.optimalTTL, true
	}
	if ds.ttlProbed {
		return 0, false
	}

	base := baseConfig()
	base.Faking.SNI = true
	base.Faking.Strategy = "ttl"
	base.Faking.SeqOffset = 10000
	base.Faking.SNISeqLength = 1
	ds.applyBestPayload(&base.Faking)
	base.Fragmentation.Strategy = "combo"
	base.Fragmentation.SNIPosition = 1

	tmpPreset := ConfigPreset{Name: "ttl-probe", Config: base}
	ttl, _ := ds.findOptimalTTL(tmpPreset)

	if ttl == 0 {
		log.DiscoveryLogf("  No fake TTL worked; testing this family without a fake packet")
		return 0, false
	}

	return ttl, true
}

func (ds *DiscoverySuite) ttlSweepChecks() int {
	if ds.optimalTTL > 0 {
		return 1
	}
	if ds.ttlProbed {
		return 0
	}
	return len(ttlSweepValues)
}

func (ds *DiscoverySuite) memoisedTTLPreset(basePreset ConfigPreset) (ConfigPreset, bool) {
	if ds.optimalTTL == 0 {
		return basePreset, false
	}
	preset := basePreset
	preset.Name = fmt.Sprintf("%s-ttl%d-confirm", basePreset.Name, ds.optimalTTL)
	preset.Config.Faking.Strategy = "ttl"
	preset.Config.Faking.TTL = ds.optimalTTL
	return preset, true
}

func (ds *DiscoverySuite) findOptimalTTL(basePreset ConfigPreset) (uint8, float64) {
	if preset, ok := ds.memoisedTTLPreset(basePreset); ok {
		result := ds.testPresetWithBestPayload(preset)
		ds.storeResult(preset, result)
		if result.Status == CheckStatusComplete {
			log.DiscoveryLogf("  fake TTL %d found earlier in this run holds for this family (%.2f KB/s)", ds.optimalTTL, result.Speed/1024)
			return ds.optimalTTL, result.Speed
		}
		log.DiscoveryLogf("  fake TTL %d found earlier in this run does not hold for this family, testing it without a fake packet", ds.optimalTTL)
		return 0, 0
	}
	if ds.ttlProbed {
		log.DiscoveryLogf("  fake TTL sweep already found no working TTL on this path, not repeating it")
		return 0, 0
	}

	var bestTTL uint8
	var bestSpeed float64

	// Use "ttl" strategy for probing since that's the only strategy where
	// Faking.TTL actually affects the fake SNI packet's IP TTL field.
	// Other strategies (pastseq, timestamp, etc.) ignore Faking.TTL in the packet builder.
	origStrategy := basePreset.Config.Faking.Strategy
	basePreset.Config.Faking.Strategy = "ttl"

	// Scan key TTL values from low to high to find the minimum working TTL.
	// We use a linear scan over common hop counts instead of binary search
	// because the working range is bounded on both sides (too low = doesn't
	// reach DPI, too high = reaches server and breaks the connection).
	log.DiscoveryLogf("Scanning for minimum working TTL (%d values)", len(ttlSweepValues))

	swept := 0
	for _, ttl := range ttlSweepValues {
		if ds.interrupted() {
			break
		}
		preset := basePreset
		preset.Name = fmt.Sprintf("ttl-search-%d", ttl)
		preset.Config.Faking.TTL = ttl

		result := ds.testPresetWithBestPayload(preset)
		ds.storeResult(preset, result)
		swept++

		if result.Status == CheckStatusComplete {
			bestTTL = ttl
			bestSpeed = result.Speed
			log.DiscoveryLogf("  TTL %d: SUCCESS (%.2f KB/s)", ttl, result.Speed/1024)
			break // First working TTL is the minimum
		} else {
			log.Tracef("  TTL %d: FAILED", ttl)
		}
	}

	// Restore original strategy
	basePreset.Config.Faking.Strategy = origStrategy

	ds.ttlProbed = true
	if bestTTL > 0 {
		log.DiscoveryLogf("Minimum working TTL: %d (%.2f KB/s)", bestTTL, bestSpeed/1024)
		ds.optimalTTL = bestTTL
	} else if swept == len(ttlSweepValues) {
		log.DiscoveryLogf("  every fake TTL from %d to %d killed the flow: this path discards low-TTL data segments, fakes here need full TTL with tcp_check or timestamp fooling",
			ttlSweepValues[0], ttlSweepValues[len(ttlSweepValues)-1])
	}
	return bestTTL, bestSpeed
}
