package discovery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	discoveryCacheFile = "discovery_cache.json"
	maxCacheEntries    = 50
)

type CacheEntry struct {
	Config       config.SetConfig `json:"config"`
	Family       StrategyFamily   `json:"family"`
	SuccessCount int              `json:"success_count"`
	LastSuccess  time.Time        `json:"last_success"`
	OriginDomain string           `json:"origin_domain"`
	Speed        float64          `json:"speed"`
}

type DiscoveryCache struct {
	Entries []CacheEntry `json:"entries"`
	mu      sync.Mutex   `json:"-"`
}

func cacheFilePath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), discoveryCacheFile)
}

func LoadDiscoveryCache(configPath string) *DiscoveryCache {
	cache := &DiscoveryCache{}
	path := cacheFilePath(configPath)
	if path == "" {
		return cache
	}

	data, err := os.ReadFile(path)
	if err != nil {
		log.Tracef("No discovery cache found at %s", path)
		return cache
	}

	if err := json.Unmarshal(data, cache); err != nil {
		log.Errorf("Failed to parse discovery cache: %v", err)
		return &DiscoveryCache{}
	}

	log.Tracef("Loaded discovery cache with %d entries", len(cache.Entries))
	return cache
}

func (dc *DiscoveryCache) Save(configPath string) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	path := cacheFilePath(configPath)
	if path == "" {
		return nil
	}

	data, err := json.MarshalIndent(dc, "", "  ")
	if err != nil {
		return log.Errorf("failed to marshal discovery cache: %v", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return log.Errorf("failed to write discovery cache: %v", err)
	}

	log.Tracef("Saved discovery cache with %d entries to %s", len(dc.Entries), path)
	return nil
}

func stripTargets(sc config.SetConfig) config.SetConfig {
	sc.Targets = config.TargetsConfig{}
	sc.DNS = config.DNSConfig{}
	sc.Enabled = false
	sc.Id = ""
	sc.Name = ""
	return sc
}

func configsAreSimilar(a, b *config.SetConfig) bool {
	return a.Fragmentation.Strategy == b.Fragmentation.Strategy &&
		a.Fragmentation.ReverseOrder == b.Fragmentation.ReverseOrder &&
		a.Fragmentation.MiddleSNI == b.Fragmentation.MiddleSNI &&
		a.Faking.Strategy == b.Faking.Strategy &&
		a.Faking.TTL == b.Faking.TTL &&
		a.Faking.SNI == b.Faking.SNI &&
		a.TCP.DropSACK == b.TCP.DropSACK
}

func (dc *DiscoveryCache) AddEntry(sc config.SetConfig, family StrategyFamily, speed float64, domain string) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	stripped := stripTargets(sc)

	for i, entry := range dc.Entries {
		if configsAreSimilar(&entry.Config, &stripped) {
			dc.Entries[i].SuccessCount++
			dc.Entries[i].LastSuccess = time.Now()
			if speed > dc.Entries[i].Speed {
				dc.Entries[i].Speed = speed
			}
			dc.Entries[i].OriginDomain = domain
			return
		}
	}

	dc.Entries = append(dc.Entries, CacheEntry{
		Config:       stripped,
		Family:       family,
		SuccessCount: 1,
		LastSuccess:  time.Now(),
		OriginDomain: domain,
		Speed:        speed,
	})

	if len(dc.Entries) > maxCacheEntries {
		dc.evictLeastSuccessful()
	}
}

func (dc *DiscoveryCache) evictLeastSuccessful() {
	if len(dc.Entries) == 0 {
		return
	}

	worstIdx := 0
	for i := 1; i < len(dc.Entries); i++ {
		if dc.Entries[i].SuccessCount < dc.Entries[worstIdx].SuccessCount ||
			(dc.Entries[i].SuccessCount == dc.Entries[worstIdx].SuccessCount &&
				dc.Entries[i].LastSuccess.Before(dc.Entries[worstIdx].LastSuccess)) {
			worstIdx = i
		}
	}

	dc.Entries = append(dc.Entries[:worstIdx], dc.Entries[worstIdx+1:]...)
}

func (dc *DiscoveryCache) GetCachedPresets() []ConfigPreset {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if len(dc.Entries) == 0 {
		return nil
	}

	sorted := make([]CacheEntry, len(dc.Entries))
	copy(sorted, dc.Entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].SuccessCount != sorted[j].SuccessCount {
			return sorted[i].SuccessCount > sorted[j].SuccessCount
		}
		return sorted[i].Speed > sorted[j].Speed
	})

	presets := make([]ConfigPreset, len(sorted))
	for i, entry := range sorted {
		presets[i] = ConfigPreset{
			Name:   fmt.Sprintf("cached-%d-%s", i+1, entry.Family),
			Family: entry.Family,
			Phase:  PhaseCached,
			Config: entry.Config,
		}
	}

	return presets
}

func (ds *DiscoverySuite) saveResultsToCache() {
	if ds.discoveryCache == nil || ds.cfg == nil {
		return
	}

	ds.CheckSuite.mu.RLock()
	allResults := ds.domainResults
	ds.CheckSuite.mu.RUnlock()

	savedCount := 0
	for domain, domainResult := range allResults {
		for _, result := range domainResult.Results {
			if result.Status != CheckStatusComplete {
				continue
			}
			if result.PresetName == presetNoBypass || result.PresetName == presetAltAddress || result.PresetName == presetDNSRedirect {
				continue
			}
			if result.Set == nil {
				continue
			}

			ds.discoveryCache.AddEntry(*result.Set, result.Family, result.Speed, domain)
			savedCount++
		}
	}

	if savedCount > 0 {
		if err := ds.discoveryCache.Save(ds.cfg.ConfigPath); err != nil {
			log.Errorf("Failed to save discovery cache: %v", err)
		} else {
			log.DiscoveryLogf("Saved %d successful configs to discovery cache", savedCount)
		}
	}
}
