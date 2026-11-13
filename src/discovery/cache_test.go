package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func tally(entry *CacheEntry) string {
	if entry == nil {
		return "no entry"
	}
	return fmt.Sprintf("count %d, failed runs %d, speed %.0f, origin %q", entry.SuccessCount, entry.FailedRuns, entry.Speed, entry.OriginDomain)
}

func shapeWithFake(sniType int, file, domain string) config.SetConfig {
	sc := makeTestConfig("combo", 5, true)
	sc.Faking.SNIType = sniType
	sc.Faking.PayloadFile = file
	sc.Faking.PayloadDomain = domain
	return sc
}

func TestAPassWithAnotherFakeIsNotCountedForTheEntryOfTheSameShape(t *testing.T) {
	capture := shapeWithFake(config.FakePayloadCapture, "captures/tls_capture_example.bin", "")
	dc := &DiscoveryCache{Entries: []CacheEntry{{
		Config:       stripTargets(capture),
		Family:       FamilyCombo,
		SuccessCount: 207,
		Speed:        1000,
		OriginDomain: "first.example",
	}}}

	dc.AddEntry(shapeWithFake(config.FakePayloadSTUN, "", ""), FamilyCombo, 5000, "second.example")

	if len(dc.Entries) != 2 {
		t.Fatalf("a strategy that sends another fake is another strategy, got %d entries, the first with %s, fake type %d and file %q",
			len(dc.Entries), tally(&dc.Entries[0]), dc.Entries[0].Config.Faking.SNIType, dc.Entries[0].Config.Faking.PayloadFile)
	}
	kept := dc.Entries[0]
	if kept.SuccessCount != 207 || kept.Speed != 1000 || kept.OriginDomain != "first.example" {
		t.Errorf("the capture entry did not pass, so nothing of it may change: count %d, speed %.0f, origin %q", kept.SuccessCount, kept.Speed, kept.OriginDomain)
	}
	if kept.Config.Faking.SNIType != config.FakePayloadCapture || kept.Config.Faking.PayloadFile != "captures/tls_capture_example.bin" {
		t.Errorf("the capture entry must keep its own fake, got type %d file %q", kept.Config.Faking.SNIType, kept.Config.Faking.PayloadFile)
	}
	added := dc.Entries[1]
	if added.Config.Faking.SNIType != config.FakePayloadSTUN || added.SuccessCount != 1 || added.Speed != 5000 || added.OriginDomain != "second.example" {
		t.Errorf("the fake that passed must be stored with its own count, got type %d count %d speed %.0f origin %q",
			added.Config.Faking.SNIType, added.SuccessCount, added.Speed, added.OriginDomain)
	}
}

func TestConfigsAreSimilarComparesTheFakeThatIsSent(t *testing.T) {
	capture := shapeWithFake(config.FakePayloadCapture, "captures/tls_a_example.bin", "")
	custom := shapeWithFake(config.FakePayloadCustom, "", "")
	custom.Faking.CustomPayload = "abc"
	otherCustom := custom
	otherCustom.Faking.CustomPayload = "abd"
	staleNames := shapeWithFake(config.FakePayloadSTUN, "captures/tls_a_example.bin", "old.example")

	for _, tc := range []struct {
		name string
		a, b config.SetConfig
		want bool
	}{
		{"the same capture", capture, shapeWithFake(config.FakePayloadCapture, "captures/tls_a_example.bin", ""), true},
		{"another capture file", capture, shapeWithFake(config.FakePayloadCapture, "captures/tls_b_example.bin", ""), false},
		{"a capture against a built-in fake", capture, shapeWithFake(config.FakePayloadSTUN, "", ""), false},
		{"two built-in fakes", shapeWithFake(config.FakePayloadDefault1, "", ""), shapeWithFake(config.FakePayloadDefault2, "", ""), false},
		{"random against the default fake", shapeWithFake(config.FakePayloadRandom, "", ""), shapeWithFake(config.FakePayloadDefault1, "", ""), false},
		{"the same generated domain", shapeWithFake(config.FakePayloadDomain, "", "a.example"), shapeWithFake(config.FakePayloadDomain, "", "a.example"), true},
		{"another generated domain", shapeWithFake(config.FakePayloadDomain, "", "a.example"), shapeWithFake(config.FakePayloadDomain, "", "b.example"), false},
		{"the same custom payload", custom, custom, true},
		{"another custom payload", custom, otherCustom, false},
		{"a file and a domain the fake type does not read", shapeWithFake(config.FakePayloadSTUN, "", ""), staleNames, true},
	} {
		if got := configsAreSimilar(&tc.a, &tc.b); got != tc.want {
			t.Errorf("%s: similar = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestConfigsAreSimilarIgnoresAFakeThatIsNeverSent(t *testing.T) {
	a := shapeWithFake(config.FakePayloadCapture, "captures/tls_a_example.bin", "")
	b := shapeWithFake(config.FakePayloadSTUN, "", "")
	a.Faking.SNI, b.Faking.SNI = false, false

	if !configsAreSimilar(&a, &b) {
		t.Error("neither strategy sends a fake, so the fake they name changes nothing on the wire")
	}

	b.TCP.SynFake, b.TCP.SynFakeLen = true, 64
	if configsAreSimilar(&a, &b) {
		t.Error("a strategy that sends a fake SYN is not the one that sends no fake at all")
	}
}

func TestConfigsAreSimilarComparesTheDesyncMode(t *testing.T) {
	a := makeTestConfig("combo", 5, true)
	b := makeTestConfig("combo", 5, true)
	b.TCP.Desync.Mode = "rst"
	if configsAreSimilar(&a, &b) {
		t.Error("a strategy with desync packets is not the one without them")
	}

	b.TCP.Desync.Mode = config.ConfigOff
	if !configsAreSimilar(&a, &b) {
		t.Error("an unset desync mode is off, the way a test config stores it")
	}
}

func TestAFailedRunPutsACachedStrategyBehindTheOnesThatDidNotFail(t *testing.T) {
	failing := makeTestConfig("combo", 5, true)
	healthy := makeTestConfig("tcp", 7, true)
	dc := &DiscoveryCache{Entries: []CacheEntry{
		{Config: stripTargets(failing), Family: FamilyCombo, SuccessCount: 321, Speed: 9000},
		{Config: stripTargets(healthy), Family: FamilyTCPFrag, SuccessCount: 1, Speed: 100},
	}}

	if runs, removed := dc.MarkFailed(&failing); runs != 1 || removed {
		t.Fatalf("the first failed run is counted and the entry stays, got runs %d removed %v", runs, removed)
	}

	presets := dc.GetCachedPresets()
	if len(presets) != 2 || presets[0].Family != FamilyTCPFrag || presets[1].Family != FamilyCombo {
		t.Fatalf("a count earned earlier must not keep a strategy that just failed in front, got %s first", presets[0].Name)
	}
}

func TestThreeFailedRunsInARowRemoveACachedStrategy(t *testing.T) {
	failing := makeTestConfig("combo", 5, true)
	healthy := makeTestConfig("tcp", 7, true)
	dc := &DiscoveryCache{Entries: []CacheEntry{
		{Config: stripTargets(failing), Family: FamilyCombo, SuccessCount: 321},
		{Config: stripTargets(healthy), Family: FamilyTCPFrag, SuccessCount: 1},
	}}

	for run := 1; run < maxFailedRuns; run++ {
		if runs, removed := dc.MarkFailed(&failing); runs != run || removed {
			t.Fatalf("run %d: got runs %d removed %v", run, runs, removed)
		}
	}
	if runs, removed := dc.MarkFailed(&failing); runs != maxFailedRuns || !removed {
		t.Fatalf("the failed run number %d removes the entry, got runs %d removed %v", maxFailedRuns, runs, removed)
	}
	if len(dc.Entries) != 1 || dc.Entries[0].Family != FamilyTCPFrag {
		t.Fatalf("only the failing strategy leaves, got %d entries", len(dc.Entries))
	}
	if runs, removed := dc.MarkFailed(&failing); runs != 0 || removed {
		t.Errorf("a strategy the cache does not hold has nothing to count, got runs %d removed %v", runs, removed)
	}
}

func TestAPassOfItsOwnTestClearsTheFailedRuns(t *testing.T) {
	sc := makeTestConfig("combo", 5, true)
	dc := &DiscoveryCache{Entries: []CacheEntry{{Config: stripTargets(sc), Family: FamilyCombo, SuccessCount: 4}}}

	if dc.MarkPassed(&sc) {
		t.Error("an entry without failed runs has nothing to clear")
	}
	dc.MarkFailed(&sc)
	dc.MarkFailed(&sc)
	if !dc.MarkPassed(&sc) {
		t.Fatal("a pass after failed runs changes the entry")
	}
	if dc.Entries[0].FailedRuns != 0 {
		t.Fatalf("the failed runs must be in a row to count, got %d", dc.Entries[0].FailedRuns)
	}
	if runs, removed := dc.MarkFailed(&sc); runs != 1 || removed {
		t.Errorf("the count starts again after a pass, got runs %d removed %v", runs, removed)
	}
}

func TestAnEntryWithFailedRunsGainsNothingFromAnotherStrategysPass(t *testing.T) {
	stored := makeTestConfig("combo", 5, true)
	stored.Fragmentation.SNIPosition = 1
	dc := &DiscoveryCache{Entries: []CacheEntry{{
		Config: stripTargets(stored), Family: FamilyCombo, SuccessCount: 207, Speed: 1000, OriginDomain: "first.example",
	}}}
	dc.MarkFailed(&stored)

	variant := stored
	variant.Fragmentation.SNIPosition = 5
	dc.AddEntry(variant, FamilyCombo, 5000, "second.example")

	if len(dc.Entries) != 1 {
		t.Fatalf("a similar strategy does not get an entry next to the one that stands for its shape, got %d", len(dc.Entries))
	}
	entry := dc.Entries[0]
	if entry.SuccessCount != 207 || entry.Speed != 1000 || entry.OriginDomain != "first.example" || entry.FailedRuns != 1 {
		t.Errorf("an entry that failed its own test must not gain from a pass of another strategy: %s", tally(&entry))
	}

	dc.MarkPassed(&stored)
	dc.AddEntry(variant, FamilyCombo, 5000, "second.example")
	if got := dc.Entries[0].SuccessCount; got != 208 {
		t.Errorf("without failed runs a similar pass counts as before, got %d", got)
	}
}

func TestAFullCacheEvictsAStrategyWithFailedRunsFirst(t *testing.T) {
	dc := &DiscoveryCache{}
	for i := 0; i < maxCacheEntries; i++ {
		sc := makeTestConfig("combo", uint8(i+1), true)
		dc.Entries = append(dc.Entries, CacheEntry{Config: stripTargets(sc), Family: FamilyCombo, SuccessCount: 2, LastSuccess: time.Now()})
	}
	dc.Entries[10].SuccessCount = 321
	dc.Entries[10].FailedRuns = 1
	failingTTL := dc.Entries[10].Config.Faking.TTL

	dc.AddEntry(makeTestConfig("tcp", 7, true), FamilyTCPFrag, 1000, "new.example")

	if len(dc.Entries) != maxCacheEntries {
		t.Fatalf("the cache holds %d entries at most, got %d", maxCacheEntries, len(dc.Entries))
	}
	for _, entry := range dc.Entries {
		if entry.Config.Fragmentation.Strategy == "combo" && entry.Config.Faking.TTL == failingTTL {
			t.Fatal("the entry that failed its last run must leave before one that did not, whatever its count")
		}
	}
	if last := dc.Entries[len(dc.Entries)-1]; last.Config.Fragmentation.Strategy != "tcp" {
		t.Errorf("the new strategy must stay, got %q last", last.Config.Fragmentation.Strategy)
	}
}

func TestACacheWrittenBeforeFailedRunsWereCountedLoadsWithItsCounts(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "b4.json")
	cachePath := filepath.Join(filepath.Dir(configPath), discoveryCacheFile)
	old := `{"entries":[{"config":{"fragmentation":{"strategy":"combo"},"faking":{"sni":true,"ttl":5,"sni_type":4,"payload_file":"captures/tls_a_example.bin"}},"family":"combo","success_count":321,"last_success":"2026-11-06T10:00:00Z","origin_domain":"a.example","speed":1000}]}`
	if err := os.WriteFile(cachePath, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	dc := LoadDiscoveryCache(configPath)
	if len(dc.Entries) != 1 || dc.Entries[0].SuccessCount != 321 || dc.Entries[0].FailedRuns != 0 {
		t.Fatalf("an existing cache keeps its entries and counts, got %d entries", len(dc.Entries))
	}
	if err := dc.Save(configPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "failed_runs") {
		t.Error("an entry without failed runs is written the way older builds wrote it")
	}
}
