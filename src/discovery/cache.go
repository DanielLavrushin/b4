package discovery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	discoveryCacheFile = "discovery_cache.json"
	maxCacheEntries    = 50
	maxFailedRuns      = 3
)

type CacheEntry struct {
	Config       config.SetConfig `json:"config"`
	Family       StrategyFamily   `json:"family"`
	SuccessCount int              `json:"success_count"`
	FailedRuns   int              `json:"failed_runs,omitempty"`
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
	sc.DSCP = config.SetDSCPConfig{}
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
		a.TCP.DropSACK == b.TCP.DropSACK &&
		desyncMode(a) == desyncMode(b) &&
		sentFake(a) == sentFake(b)
}

func desyncMode(set *config.SetConfig) string {
	if set.TCP.Desync.Mode == "" {
		return config.ConfigOff
	}
	return set.TCP.Desync.Mode
}

func sentFake(set *config.SetConfig) string {
	if !set.SendsFake() {
		return ""
	}
	faking := &set.Faking
	switch faking.SNIType {
	case config.FakePayloadCapture:
		return "capture:" + faking.PayloadFile
	case config.FakePayloadDomain:
		return "domain:" + faking.PayloadDomain
	case config.FakePayloadCustom:
		return "custom:" + faking.CustomPayload
	}
	return "type:" + strconv.Itoa(faking.SNIType)
}

func (dc *DiscoveryCache) AddEntry(sc config.SetConfig, family StrategyFamily, speed float64, domain string) bool {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	stripped := stripTargets(sc)

	if i := dc.indexOf(&stripped); i >= 0 {
		entry := &dc.Entries[i]
		if entry.FailedRuns > 0 {
			return false
		}
		entry.SuccessCount++
		entry.LastSuccess = time.Now()
		if speed > entry.Speed {
			entry.Speed = speed
		}
		entry.OriginDomain = domain
		return true
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
	return true
}

func (dc *DiscoveryCache) indexOf(sc *config.SetConfig) int {
	for i := range dc.Entries {
		if configsAreSimilar(&dc.Entries[i].Config, sc) {
			return i
		}
	}
	return -1
}

func (dc *DiscoveryCache) MarkPassed(sc *config.SetConfig) bool {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	i := dc.indexOf(sc)
	if i < 0 || dc.Entries[i].FailedRuns == 0 {
		return false
	}
	dc.Entries[i].FailedRuns = 0
	return true
}

func (dc *DiscoveryCache) MarkFailed(sc *config.SetConfig) (int, bool) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	i := dc.indexOf(sc)
	if i < 0 {
		return 0, false
	}
	dc.Entries[i].FailedRuns++
	runs := dc.Entries[i].FailedRuns
	if runs < maxFailedRuns {
		return runs, false
	}
	dc.Entries = append(dc.Entries[:i], dc.Entries[i+1:]...)
	return runs, true
}

func (dc *DiscoveryCache) evictLeastSuccessful() {
	if len(dc.Entries) == 0 {
		return
	}

	worstIdx := 0
	for i := 1; i < len(dc.Entries); i++ {
		if lessSuccessful(&dc.Entries[i], &dc.Entries[worstIdx]) {
			worstIdx = i
		}
	}

	dc.Entries = append(dc.Entries[:worstIdx], dc.Entries[worstIdx+1:]...)
}

func lessSuccessful(a, b *CacheEntry) bool {
	if a.FailedRuns != b.FailedRuns {
		return a.FailedRuns > b.FailedRuns
	}
	if a.SuccessCount != b.SuccessCount {
		return a.SuccessCount < b.SuccessCount
	}
	return a.LastSuccess.Before(b.LastSuccess)
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
		if sorted[i].FailedRuns != sorted[j].FailedRuns {
			return sorted[i].FailedRuns < sorted[j].FailedRuns
		}
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

func cachedIndex(name string) (int, bool) {
	rest, ok := strings.CutPrefix(name, "cached-")
	if !ok {
		return 0, false
	}
	digits, _, _ := strings.Cut(rest, "-")
	n, err := strconv.Atoi(digits)
	return n, err == nil
}

func (ds *DiscoverySuite) saveResultsToCache() {
	if ds.discoveryCache == nil || ds.cfg == nil {
		return
	}

	ds.CheckSuite.mu.RLock()
	allResults := ds.domainResults
	invalidated := ds.RulesChanged
	ds.CheckSuite.mu.RUnlock()

	if invalidated {
		return
	}

	judged := ds.judgeCachedPresets(allResults)

	savedCount := 0
	for domain, domainResult := range allResults {
		if domainResult == nil {
			continue
		}
		if _, byPlainFix := loadedBy(domainResult); byPlainFix {
			continue
		}
		for _, result := range domainResult.Results {
			if result.Status != CheckStatusComplete {
				continue
			}
			if plainFixName(result.PresetName) || result.PresetName == presetSetCurrent {
				continue
			}
			if result.Set == nil {
				continue
			}

			if ds.discoveryCache.AddEntry(*result.Set, result.Family, result.Speed, domain) {
				savedCount++
			}
		}
	}

	if savedCount == 0 && !judged {
		return
	}
	if err := ds.discoveryCache.Save(ds.cfg.ConfigPath); err != nil {
		log.Errorf("Failed to save discovery cache: %v", err)
	} else if savedCount > 0 {
		log.DiscoveryLogf("Saved %d successful configs to discovery cache", savedCount)
	}
}

func (ds *DiscoverySuite) judgeCachedPresets(results map[string]*DomainDiscoveryResult) bool {
	changed := false
	for _, preset := range ds.cachedPresets {
		passed, failed := cachedOutcome(preset.Name, results)
		switch {
		case passed:
			if ds.discoveryCache.MarkPassed(&preset.Config) {
				changed = true
			}
		case failed:
			runs, removed := ds.discoveryCache.MarkFailed(&preset.Config)
			switch {
			case removed:
				log.DiscoveryLogf("Cached strategy '%s' failed on a blocked site that another strategy opened in %d runs in a row and was removed from the cache", preset.Name, runs)
			case runs > 0:
				log.DiscoveryLogf("Cached strategy '%s' failed on a blocked site that another strategy opened: failed run %d of %d in a row, after which it is removed from the cache", preset.Name, runs, maxFailedRuns)
			}
			if runs > 0 {
				changed = true
			}
		}
	}
	return changed
}

func cachedOutcome(name string, results map[string]*DomainDiscoveryResult) (passed, failed bool) {
	for _, dr := range results {
		if dr == nil {
			continue
		}
		r := dr.Results[name]
		if r == nil {
			continue
		}
		byStrategy, byPlainFix := loadedBy(dr)
		if byPlainFix {
			continue
		}
		switch r.Status {
		case CheckStatusComplete:
			passed = true
		case CheckStatusFailed:
			failed = failed || byStrategy
		}
	}
	return passed, failed
}

func loadedBy(dr *DomainDiscoveryResult) (strategy, plainFix bool) {
	for name, r := range dr.Results {
		if r == nil || r.Status != CheckStatusComplete {
			continue
		}
		if plainFixName(name) {
			plainFix = true
		} else {
			strategy = true
		}
	}
	return strategy, plainFix
}
