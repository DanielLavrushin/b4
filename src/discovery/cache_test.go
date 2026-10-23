package discovery

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func makeTestConfig(strategy string, ttl uint8, fakeSNI bool) config.SetConfig {
	return config.SetConfig{
		Id:   "test-id",
		Name: "test-name",
		Fragmentation: config.FragmentationConfig{
			Strategy:     strategy,
			ReverseOrder: true,
		},
		Faking: config.FakingConfig{
			SNI:      fakeSNI,
			TTL:      ttl,
			Strategy: "pastseq",
		},
		Targets: config.TargetsConfig{
			SNIDomains: []string{"example.com"},
			IPs:        []string{"1.2.3.4/32"},
		},
		DNS: config.DNSConfig{
			Enabled:   true,
			TargetDNS: "9.9.9.9",
		},
		Enabled: true,
	}
}

func TestStripTargets(t *testing.T) {
	sc := makeTestConfig("combo", 5, true)
	sc.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
	stripped := stripTargets(sc)

	if stripped.DSCP.Enabled {
		t.Errorf("the DSCP value names the path a run took, not the strategy, so the cache must not keep it: %+v", stripped.DSCP)
	}

	if stripped.Id != "" {
		t.Errorf("expected empty Id, got %q", stripped.Id)
	}
	if stripped.Name != "" {
		t.Errorf("expected empty Name, got %q", stripped.Name)
	}
	if len(stripped.Targets.SNIDomains) != 0 {
		t.Errorf("expected empty SNIDomains, got %v", stripped.Targets.SNIDomains)
	}
	if len(stripped.Targets.IPs) != 0 {
		t.Errorf("expected empty IPs, got %v", stripped.Targets.IPs)
	}
	if stripped.DNS.Enabled {
		t.Errorf("expected DNS disabled")
	}
	if stripped.Enabled {
		t.Errorf("expected Enabled=false")
	}

	// Bypass config should be preserved
	if stripped.Fragmentation.Strategy != "combo" {
		t.Errorf("expected strategy 'combo', got %q", stripped.Fragmentation.Strategy)
	}
	if stripped.Faking.TTL != 5 {
		t.Errorf("expected TTL 5, got %d", stripped.Faking.TTL)
	}
}

func TestConfigsAreSimilar(t *testing.T) {
	a := makeTestConfig("combo", 5, true)
	b := makeTestConfig("combo", 5, true)

	if !configsAreSimilar(&a, &b) {
		t.Error("identical configs should be similar")
	}

	// Different strategy
	c := makeTestConfig("tcp", 5, true)
	if configsAreSimilar(&a, &c) {
		t.Error("different strategy should not be similar")
	}

	// Different TTL
	d := makeTestConfig("combo", 8, true)
	if configsAreSimilar(&a, &d) {
		t.Error("different TTL should not be similar")
	}

	// Different SNI
	e := makeTestConfig("combo", 5, false)
	if configsAreSimilar(&a, &e) {
		t.Error("different SNI should not be similar")
	}
}

func TestAddEntryDedup(t *testing.T) {
	dc := &DiscoveryCache{}
	sc := makeTestConfig("combo", 5, true)

	dc.AddEntry(sc, FamilyCombo, 1000.0, "youtube.com")
	dc.AddEntry(sc, FamilyCombo, 2000.0, "twitch.tv")

	if len(dc.Entries) != 1 {
		t.Fatalf("expected 1 entry after dedup, got %d", len(dc.Entries))
	}
	if dc.Entries[0].SuccessCount != 2 {
		t.Errorf("expected SuccessCount=2, got %d", dc.Entries[0].SuccessCount)
	}
	if dc.Entries[0].Speed != 2000.0 {
		t.Errorf("expected Speed=2000, got %.0f", dc.Entries[0].Speed)
	}
	if dc.Entries[0].OriginDomain != "twitch.tv" {
		t.Errorf("expected OriginDomain='twitch.tv', got %q", dc.Entries[0].OriginDomain)
	}
}

func TestAddEntryDistinct(t *testing.T) {
	dc := &DiscoveryCache{}
	a := makeTestConfig("combo", 5, true)
	b := makeTestConfig("tcp", 7, true)

	dc.AddEntry(a, FamilyCombo, 1000.0, "youtube.com")
	dc.AddEntry(b, FamilyTCPFrag, 800.0, "youtube.com")

	if len(dc.Entries) != 2 {
		t.Fatalf("expected 2 distinct entries, got %d", len(dc.Entries))
	}
}

func TestAddEntryEviction(t *testing.T) {
	dc := &DiscoveryCache{}

	// Add maxCacheEntries + 1 distinct entries
	for i := 0; i <= maxCacheEntries; i++ {
		sc := makeTestConfig("combo", uint8(i%255), true)
		sc.Faking.Strategy = "pastseq"
		// Make each entry have unique TTL so they're not deduped
		sc.Faking.TTL = uint8(i % 255)
		if i < 255 {
			sc.Fragmentation.SNIPosition = i
		}
		dc.AddEntry(sc, FamilyCombo, float64(i*100), "test.com")
	}

	if len(dc.Entries) > maxCacheEntries {
		t.Errorf("expected at most %d entries, got %d", maxCacheEntries, len(dc.Entries))
	}
}

func TestGetCachedPresets_SortOrder(t *testing.T) {
	dc := &DiscoveryCache{}

	// Entry with low success count
	dc.Entries = append(dc.Entries, CacheEntry{
		Config:       stripTargets(makeTestConfig("tcp", 7, true)),
		Family:       FamilyTCPFrag,
		SuccessCount: 1,
		Speed:        500.0,
		LastSuccess:  time.Now(),
	})

	// Entry with high success count
	dc.Entries = append(dc.Entries, CacheEntry{
		Config:       stripTargets(makeTestConfig("combo", 5, true)),
		Family:       FamilyCombo,
		SuccessCount: 10,
		Speed:        1000.0,
		LastSuccess:  time.Now(),
	})

	// Entry with high success count but lower speed
	dc.Entries = append(dc.Entries, CacheEntry{
		Config:       stripTargets(makeTestConfig("combo", 8, true)),
		Family:       FamilyCombo,
		SuccessCount: 10,
		Speed:        800.0,
		LastSuccess:  time.Now(),
	})

	presets := dc.GetCachedPresets()
	if len(presets) != 3 {
		t.Fatalf("expected 3 presets, got %d", len(presets))
	}

	// First should be highest success count + highest speed
	if presets[0].Family != FamilyCombo {
		t.Errorf("first preset should be combo, got %s", presets[0].Family)
	}
	if presets[0].Phase != PhaseCached {
		t.Errorf("phase should be PhaseCached, got %s", presets[0].Phase)
	}

	// Last should be lowest success count
	if presets[2].Family != FamilyTCPFrag {
		t.Errorf("last preset should be tcp_frag, got %s", presets[2].Family)
	}
}

func TestGetCachedPresets_Empty(t *testing.T) {
	dc := &DiscoveryCache{}
	presets := dc.GetCachedPresets()
	if presets != nil {
		t.Errorf("expected nil for empty cache, got %v", presets)
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "b4.json")

	dc := &DiscoveryCache{}
	sc := makeTestConfig("combo", 5, true)
	dc.AddEntry(sc, FamilyCombo, 1234.5, "youtube.com")

	if err := dc.Save(configPath); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	cachePath := filepath.Join(tmpDir, discoveryCacheFile)
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatalf("cache file not created at %s", cachePath)
	}

	// Load and verify
	loaded := LoadDiscoveryCache(configPath)
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry after load, got %d", len(loaded.Entries))
	}
	if loaded.Entries[0].Family != FamilyCombo {
		t.Errorf("expected family combo, got %s", loaded.Entries[0].Family)
	}
	if loaded.Entries[0].Speed != 1234.5 {
		t.Errorf("expected speed 1234.5, got %.1f", loaded.Entries[0].Speed)
	}
	if loaded.Entries[0].SuccessCount != 1 {
		t.Errorf("expected success_count 1, got %d", loaded.Entries[0].SuccessCount)
	}
}

func TestLoadMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "b4.json")

	cache := LoadDiscoveryCache(configPath)
	if cache == nil {
		t.Fatal("expected non-nil cache for missing file")
	}
	if len(cache.Entries) != 0 {
		t.Errorf("expected 0 entries for missing file, got %d", len(cache.Entries))
	}
}

func TestLoadCorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, discoveryCacheFile)
	os.WriteFile(cachePath, []byte("not valid json{{{"), 0666)

	configPath := filepath.Join(tmpDir, "b4.json")
	cache := LoadDiscoveryCache(configPath)
	if cache == nil {
		t.Fatal("expected non-nil cache for corrupt file")
	}
	if len(cache.Entries) != 0 {
		t.Errorf("expected 0 entries for corrupt file, got %d", len(cache.Entries))
	}
}

func TestLoadEmptyConfigPath(t *testing.T) {
	cache := LoadDiscoveryCache("")
	if cache == nil {
		t.Fatal("expected non-nil cache for empty config path")
	}
	if len(cache.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(cache.Entries))
	}
}

func TestSaveEmptyConfigPath(t *testing.T) {
	dc := &DiscoveryCache{}
	dc.AddEntry(makeTestConfig("combo", 5, true), FamilyCombo, 1000, "test.com")

	// Should be a no-op, not an error
	if err := dc.Save(""); err != nil {
		t.Errorf("Save with empty path should return nil, got %v", err)
	}
}
