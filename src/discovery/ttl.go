package discovery

import (
	"fmt"

	"github.com/daniellavrushin/b4/log"
)

func (ds *DiscoverySuite) getOptimalTTL() uint8 {
	if ds.optimalTTL > 0 {
		return ds.optimalTTL
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
	ds.optimalTTL, _ = ds.findOptimalTTL(tmpPreset)

	if ds.optimalTTL == 0 {
		ds.optimalTTL = 7
	}

	return ds.optimalTTL
}

func (ds *DiscoverySuite) findOptimalTTL(basePreset ConfigPreset) (uint8, float64) {
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
	ttlValues := []uint8{3, 4, 5, 6, 7, 8, 9, 10, 12, 15}

	log.DiscoveryLogf("Scanning for minimum working TTL (%d values)", len(ttlValues))

	for _, ttl := range ttlValues {
		if ds.canceled() {
			break
		}
		preset := basePreset
		preset.Name = fmt.Sprintf("ttl-search-%d", ttl)
		preset.Config.Faking.TTL = ttl

		result := ds.testPresetWithBestPayload(preset)
		ds.storeResult(preset, result)

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

	if bestTTL > 0 {
		log.DiscoveryLogf("Minimum working TTL: %d (%.2f KB/s)", bestTTL, bestSpeed/1024)
		ds.optimalTTL = bestTTL
	}
	return bestTTL, bestSpeed
}
