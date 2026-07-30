package geodat

import (
	"sort"
	"sync"

	"github.com/daniellavrushin/b4/log"
)

type GeodataType int

const (
	GEOSITE GeodataType = iota
	GEOIP
)

// GeodataManager handles geodata file operations with caching and statistics
type GeodataManager struct {
	mu          sync.RWMutex
	geositePath string
	geoipPath   string

	categoryDomains       map[string][]string // category -> domains (cached)
	categoryDomainsCounts map[string]int      // category -> domain count (fast lookup)

	categoryIps       map[string][]string // category -> IPs (cached)
	categoryIpsCounts map[string]int      // category -> IP count (fast lookup)
}

// NewGeodataManager creates a new geodata manager instance
func NewGeodataManager(geositePath, geoipPath string) *GeodataManager {
	return &GeodataManager{
		geositePath:           geositePath,
		geoipPath:             geoipPath,
		categoryDomains:       make(map[string][]string),
		categoryDomainsCounts: make(map[string]int),
		categoryIps:           make(map[string][]string),
		categoryIpsCounts:     make(map[string]int),
	}
}

// UpdatePaths updates the geodata file paths and clears cache if paths changed
func (gm *GeodataManager) UpdatePaths(geositePath, geoipPath string) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	pathsChanged := gm.geositePath != geositePath || gm.geoipPath != geoipPath

	gm.geositePath = geositePath
	gm.geoipPath = geoipPath

	if pathsChanged {
		gm.categoryDomains = make(map[string][]string)
		gm.categoryDomainsCounts = make(map[string]int)
		gm.categoryIps = make(map[string][]string)
		gm.categoryIpsCounts = make(map[string]int)
		log.Infof("Geodata paths updated, cache cleared")
	}
}

func (gm *GeodataManager) LoadGeoipCategory(category string) ([]string, error) {
	gm.mu.RLock()
	ips, exists := gm.categoryIps[category]
	path := gm.geoipPath
	gm.mu.RUnlock()

	if exists {
		log.Tracef("Using cached IPs for category: %s (%d IPs)", category, len(ips))
		return ips, nil
	}

	if path == "" {
		return nil, log.Errorf("geoip path not configured")
	}

	ips, err := LoadIpsFromCategories(path, []string{category})
	if err != nil {
		return nil, err
	}

	// Cache the result
	gm.mu.Lock()
	gm.categoryIps[category] = ips
	gm.categoryIpsCounts[category] = len(ips)
	gm.mu.Unlock()

	log.Tracef("Loaded and cached %d IPs for category: %s", len(ips), category)
	return ips, nil
}

func (gm *GeodataManager) LoadGeositeCategory(category string) ([]string, error) {
	gm.mu.RLock()
	domains, exists := gm.categoryDomains[category]
	path := gm.geositePath
	gm.mu.RUnlock()

	if exists {
		log.Tracef("Using cached domains for category: %s (%d domains)", category, len(domains))
		return domains, nil
	}

	if path == "" {
		return nil, log.Errorf("geosite path not configured")
	}

	domains, err := LoadDomainsFromCategories(path, []string{category})
	if err != nil {
		return nil, err
	}

	gm.mu.Lock()
	gm.categoryDomains[category] = domains
	gm.categoryDomainsCounts[category] = len(domains)
	gm.mu.Unlock()

	log.Tracef("Loaded and cached %d domains for category: %s", len(domains), category)
	return domains, nil
}

func (gm *GeodataManager) GetGeositeCategoryCounts(categories []string) (map[string]int, error) {
	if len(categories) == 0 {
		return make(map[string]int), nil
	}

	counts := make(map[string]int, len(categories))
	missing := make([]string, 0, len(categories))

	gm.mu.RLock()
	for _, category := range categories {
		if count, exists := gm.categoryDomainsCounts[category]; exists {
			counts[category] = count
			continue
		}
		missing = append(missing, category)
	}
	path := gm.geositePath
	gm.mu.RUnlock()

	if len(missing) == 0 {
		return counts, nil
	}

	streamed, err := CountDomainsInCategories(path, missing)
	if err != nil {
		log.Errorf("Failed to count geosite categories %v: %v", missing, err)
		for _, category := range missing {
			counts[category] = 0
		}
		return counts, nil
	}

	gm.mu.Lock()
	for _, category := range missing {
		counts[category] = streamed[category]
		gm.categoryDomainsCounts[category] = streamed[category]
	}
	gm.mu.Unlock()

	return counts, nil
}

func (gm *GeodataManager) GetGeoipCategoryCounts(categories []string) (map[string]int, error) {
	if len(categories) == 0 {
		return make(map[string]int), nil
	}

	counts := make(map[string]int, len(categories))
	missing := make([]string, 0, len(categories))

	gm.mu.RLock()
	for _, category := range categories {
		if count, exists := gm.categoryIpsCounts[category]; exists {
			counts[category] = count
			continue
		}
		missing = append(missing, category)
	}
	path := gm.geoipPath
	gm.mu.RUnlock()

	if len(missing) == 0 {
		return counts, nil
	}

	streamed, err := CountIpsInCategories(path, missing)
	if err != nil {
		log.Errorf("Failed to count geoip categories %v: %v", missing, err)
		for _, category := range missing {
			counts[category] = 0
		}
		return counts, nil
	}

	gm.mu.Lock()
	for _, category := range missing {
		counts[category] = streamed[category]
		gm.categoryIpsCounts[category] = streamed[category]
	}
	gm.mu.Unlock()

	return counts, nil
}

func (gm *GeodataManager) PreviewGeositeCategory(category string, limit int) ([]string, int, error) {
	gm.mu.RLock()
	domains, cached := gm.categoryDomains[category]
	path := gm.geositePath
	gm.mu.RUnlock()

	if cached {
		preview := domains
		if len(preview) > limit {
			preview = preview[:limit]
		}
		return preview, len(domains), nil
	}

	if path == "" {
		return nil, 0, log.Errorf("geosite path not configured")
	}

	return PreviewDomainsInCategory(path, category, limit)
}

func (gm *GeodataManager) RetainCategories(geositeCategories, geoipCategories []string) {
	keepSite := make(map[string]struct{}, len(geositeCategories))
	for _, c := range geositeCategories {
		keepSite[c] = struct{}{}
	}
	keepIp := make(map[string]struct{}, len(geoipCategories))
	for _, c := range geoipCategories {
		keepIp[c] = struct{}{}
	}

	gm.mu.Lock()
	defer gm.mu.Unlock()

	for category := range gm.categoryDomains {
		if _, ok := keepSite[category]; !ok {
			delete(gm.categoryDomains, category)
			delete(gm.categoryDomainsCounts, category)
			log.Tracef("Evicted geosite category %s from cache (no longer selected)", category)
		}
	}
	for category := range gm.categoryIps {
		if _, ok := keepIp[category]; !ok {
			delete(gm.categoryIps, category)
			delete(gm.categoryIpsCounts, category)
			log.Tracef("Evicted geoip category %s from cache (no longer selected)", category)
		}
	}
}

func (gm *GeodataManager) ListCategories(filePath string) ([]string, error) {
	log.Tracef("Listing geo dat tags from %s", filePath)

	set := map[string]struct{}{}
	err := scanEntries(filePath, func(tag string, _ *entryBody) error {
		set[tag] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}

	tags := make([]string, 0, len(set))
	for t := range set {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	return tags, nil
}

func (gm *GeodataManager) PreloadCategories(t GeodataType, categories []string) (map[string]int, error) {
	log.Infof("Preloading %d geodata categories...", len(categories))

	counts := make(map[string]int, len(categories))
	total := 0

	for _, category := range categories {
		var n int
		if t == GEOIP {
			ips, err := gm.LoadGeoipCategory(category)
			if err != nil {
				log.Errorf("Failed to preload category %s: %v", category, err)
				counts[category] = 0
				continue
			}
			n = len(ips)
		} else {
			domains, err := gm.LoadGeositeCategory(category)
			if err != nil {
				log.Errorf("Failed to preload category %s: %v", category, err)
				counts[category] = 0
				continue
			}
			n = len(domains)
		}
		counts[category] = n
		total += n
	}

	log.Infof("Preloaded %d entries across %d categories", total, len(counts))
	return counts, nil
}

// ClearCache clears all cached data
func (gm *GeodataManager) ClearCache() {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	gm.categoryDomains = make(map[string][]string)
	gm.categoryDomainsCounts = make(map[string]int)
	gm.categoryIps = make(map[string][]string)
	gm.categoryIpsCounts = make(map[string]int)
	log.Infof("Geodata cache cleared")
}

func (gm *GeodataManager) IsGeositeConfigured() bool {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return gm.geositePath != ""
}

func (gm *GeodataManager) IsGeoipConfigured() bool {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return gm.geoipPath != ""
}

// GetGeositePath returns the current geosite path
func (gm *GeodataManager) GetGeositePath() string {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return gm.geositePath
}

func (gm *GeodataManager) GetGeoipPath() string {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return gm.geoipPath
}
