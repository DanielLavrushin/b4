package discovery

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/sock"
)

const cacheRunDomain = "open.example"

type cacheRun struct {
	cfg     config.Config
	pool    *nfq.Pool
	port    string
	fakes   *sentFakes
	running atomic.Pointer[DiscoverySuite]
}

func newCacheRun(t *testing.T, loads func(fake []byte) bool) *cacheRun {
	t.Helper()
	previous := suiteRetention
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })
	refuses := probeRefusesAddr
	probeRefusesAddr = nil
	t.Cleanup(func() { probeRefusesAddr = refuses })
	lookup := lookupECHConfigList
	lookupECHConfigList = func(context.Context, uint, string) []byte { return nil }
	t.Cleanup(func() { lookupECHConfigList = lookup })
	stubResolver(t, map[string]stubName{cacheRunDomain: {a: "127.0.0.1"}})

	env := &cacheRun{cfg: config.NewConfig(), fakes: &sentFakes{payloads: map[string][][]byte{}}}
	env.cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	env.cfg.Queue.IsDiscovery = true
	env.cfg.System.Checker.ConfigPropagateMs = 0
	env.cfg.System.Checker.DiscoveryTimeoutSec = 2
	env.pool = nfq.NewPool(&env.cfg)
	t.Cleanup(env.pool.Stop)

	page := "<html><body>" + strings.Repeat("x", 2048) + "</body></html>"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		set := env.pool.GetFirstWorkerConfig().Sets[0]
		fake := sock.GetPayload(&set.Faking)
		env.fakes.record(set.Name, fake)
		if set.Name == presetNoBypass {
			panic(http.ErrAbortHandler)
		}
		if loads(fake) {
			_, _ = w.Write([]byte(page))
			return
		}
		if _, cached := cachedIndex(set.Name); !cached {
			if ds := env.running.Load(); ds != nil {
				_ = FinishCheckSuite(ds.Id)
			}
		}
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	env.port = port
	return env
}

func (env *cacheRun) suite() *DiscoverySuite {
	return NewDiscoverySuite([]string{"https://" + cacheRunDomain + ":" + env.port + "/"}, env.pool, true, false, nil, 1, "", "ipv4", 0)
}

func (env *cacheRun) run() *DiscoverySuite {
	ds := env.suite()
	env.running.Store(ds)
	RegisterSuite(ds.CheckSuite)
	ds.RunDiscovery()
	return ds
}

func testedShape(t *testing.T, cfgPath, presetName string) config.SetConfig {
	t.Helper()
	probe := setRunFixture(t, cacheRunDomain)
	probe.Domain = cacheRunDomain
	probe.cfg = &config.Config{ConfigPath: cfgPath}
	set := stripTargets(*probe.buildTestConfig(presetByName(t, GetPhase1Presets(), presetName)).Sets[0])
	set.Faking.PayloadData = nil
	return set
}

func entryWithFake(entries []CacheEntry, sniType int) *CacheEntry {
	for i := range entries {
		if entries[i].Config.Faking.SNIType == sniType {
			return &entries[i]
		}
	}
	return nil
}

func TestARunCountsAFailingCaptureEntryAsFailedAndRemembersTheFakeThatPassed(t *testing.T) {
	env := newCacheRun(t, func(fake []byte) bool { return bytes.Equal(fake, config.FakeSTUN) })
	cfgPath := env.cfg.ConfigPath

	capture := bytes.Repeat([]byte{0x16, 0x03, 0x01}, 218)
	stored := testedShape(t, cfgPath, "combo-pastseq")
	stored.Faking.SNIType = config.FakePayloadCapture
	stored.Faking.PayloadFile = writeCapture(t, cfgPath, "tls_capture_example.bin", capture)
	storedCache(t, cfgPath, CacheEntry{Config: stored, Family: FamilyCombo, SuccessCount: 207, Speed: 1000, OriginDomain: "earlier.example"})

	ds := env.run()
	if r := ds.DomainDiscoveryResults[cacheRunDomain].Results["cached-1-combo"]; r == nil || r.Status != CheckStatusFailed {
		t.Fatalf("the capture entry must be tested and fail, got %+v", r)
	}
	if r := ds.DomainDiscoveryResults[cacheRunDomain].Results["combo-pastseq"]; r == nil || r.Status != CheckStatusComplete {
		t.Fatalf("the same strategy with the STUN fake must pass, got %+v", r)
	}
	for i, payload := range env.fakes.of("cached-1-combo") {
		if !bytes.Equal(payload, capture) {
			t.Fatalf("fetch %d of the capture entry went out with a %d byte fake", i, len(payload))
		}
	}

	cache := LoadDiscoveryCache(cfgPath)
	failing := entryWithFake(cache.Entries, config.FakePayloadCapture)
	if failing == nil {
		t.Fatalf("one failed run does not remove the capture entry, got %d entries", len(cache.Entries))
	}
	if failing.SuccessCount != 207 || failing.FailedRuns != 1 || failing.OriginDomain != "earlier.example" {
		t.Fatalf("a pass with the STUN fake must not count for the capture entry, and its own failure must: count %d, failed runs %d, origin %q",
			failing.SuccessCount, failing.FailedRuns, failing.OriginDomain)
	}
	passing := entryWithFake(cache.Entries, config.FakePayloadSTUN)
	if passing == nil || passing.SuccessCount < 1 || passing.FailedRuns != 0 {
		t.Fatalf("the strategy that passed must be remembered with the fake it sent, got %d entries", len(cache.Entries))
	}
	if first := cache.GetCachedPresets()[0]; first.Config.Faking.SNIType != config.FakePayloadSTUN {
		t.Errorf("the next run must start with the strategy that passed, not with the one that failed: type %d", first.Config.Faking.SNIType)
	}
}

func TestACachedRandomFakeIsTestedWithRandomBytes(t *testing.T) {
	env := newCacheRun(t, func([]byte) bool { return false })
	cfgPath := env.cfg.ConfigPath

	stored := testedShape(t, cfgPath, "combo-pastseq")
	stored.Faking.SNIType = config.FakePayloadRandom
	storedCache(t, cfgPath, CacheEntry{Config: stored, Family: FamilyCombo, SuccessCount: 3, Speed: 1000})

	env.run()

	sent := env.fakes.of("cached-1-combo")
	if len(sent) == 0 {
		t.Fatal("the cached strategy must be tested")
	}
	for i, payload := range sent {
		if len(payload) != randomFakeSize || bytes.Equal(payload, config.FakeSTUN) {
			t.Errorf("fetch %d went out with a %d byte fake, the entry stands for %d random bytes", i, len(payload), randomFakeSize)
		}
	}

	entry := entryWithFake(LoadDiscoveryCache(cfgPath).Entries, config.FakePayloadRandom)
	if entry == nil || entry.SuccessCount != 3 || entry.FailedRuns != 0 {
		t.Errorf("nothing passed in this run, so the failure says nothing about the entry: %s", tally(entry))
	}
}

func TestTheDeadEndRetestKeepsTheFakeOfACachedStrategy(t *testing.T) {
	env := newCacheRun(t, func([]byte) bool { return false })

	ds := env.suite()
	t.Cleanup(ds.ctxCancel)
	ds.cfg = env.pool.GetFirstWorkerConfig()
	random := testedShape(t, env.cfg.ConfigPath, "combo-pastseq")
	random.Faking.SNIType = config.FakePayloadRandom

	ds.retestEarlyPresets([]ConfigPreset{
		{Name: "cached-1-combo", Family: FamilyCombo, Phase: PhaseCached, Config: random, Domains: []string{cacheRunDomain}},
		{Name: "community-shared-v1", Family: FamilyCommunity, Phase: PhaseCached, Config: random, Domains: []string{cacheRunDomain}},
	})

	cached := env.fakes.of("cached-1-combo")
	if len(cached) == 0 {
		t.Fatal("the cached strategy must be tested again")
	}
	for i, payload := range cached {
		if len(payload) != randomFakeSize {
			t.Errorf("fetch %d of the cached strategy went out with a %d byte fake, the entry stands for %d random bytes", i, len(payload), randomFakeSize)
		}
	}
	shared := env.fakes.of("community-shared-v1")
	if len(shared) == 0 {
		t.Fatal("the community strategy must be tested again")
	}
	for i, payload := range shared {
		if !bytes.Equal(payload, config.FakeSTUN) {
			t.Errorf("fetch %d of the community strategy went out with a %d byte fake, a shared strategy without a fake type still gets the run's best payload", i, len(payload))
		}
	}
}

func cachedSuite(t *testing.T, domains []string, entries ...CacheEntry) *DiscoverySuite {
	t.Helper()
	ds := setRunFixture(t, domains...)
	ds.cfg = &config.Config{ConfigPath: filepath.Join(t.TempDir(), "b4.json")}
	ds.discoveryCache = &DiscoveryCache{Entries: entries}
	ds.cachedPresets = ds.discoveryCache.GetCachedPresets()
	return ds
}

func (ds *DiscoverySuite) putCached(domain string, preset ConfigPreset, status CheckStatus) {
	r := &DomainPresetResult{PresetName: preset.Name, Status: status, Phase: PhaseCached, Family: preset.Family}
	if status == CheckStatusComplete {
		set := preset.Config
		r.Speed = 1000
		r.Set = &set
	}
	ds.domainResults[domain].Results[preset.Name] = r
}

func cacheFileWritten(ds *DiscoverySuite) bool {
	_, err := os.Stat(cacheFilePath(ds.cfg.ConfigPath))
	return err == nil
}

func comboEntry(count, failedRuns int) CacheEntry {
	return CacheEntry{Config: stripTargets(makeTestConfig("combo", 5, true)), Family: FamilyCombo, SuccessCount: count, FailedRuns: failedRuns, Speed: 1000}
}

func TestACachedStrategyThatFailsWhereAnotherPassedGetsAFailedRun(t *testing.T) {
	ds := cachedSuite(t, []string{"a.example"}, comboEntry(207, 0))
	ds.putCached("a.example", ds.cachedPresets[0], CheckStatusFailed)
	ds.put("a.example", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put("a.example", "tcp", CheckStatusComplete, PhaseStrategy, 0)

	ds.saveResultsToCache()

	entry := ds.discoveryCache.Entries[0]
	if entry.FailedRuns != 1 || entry.SuccessCount != 207 {
		t.Fatalf("the site is blocked, another strategy opened it and this one did not: runs %d count %d", entry.FailedRuns, entry.SuccessCount)
	}
	if saved := LoadDiscoveryCache(ds.cfg.ConfigPath); len(saved.Entries) != 2 || saved.Entries[0].FailedRuns != 1 {
		t.Errorf("the failed run must reach the file, got %d entries", len(saved.Entries))
	}
}

func TestAFailedRunIsWrittenEvenWhenTheRunAddsNothing(t *testing.T) {
	ds := cachedSuite(t, []string{"a.example"}, comboEntry(207, 0))
	ds.putCached("a.example", ds.cachedPresets[0], CheckStatusFailed)
	ds.put("a.example", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put("a.example", presetSetCurrent, CheckStatusComplete, PhaseCached, 0)

	ds.saveResultsToCache()

	saved := LoadDiscoveryCache(ds.cfg.ConfigPath)
	if len(saved.Entries) != 1 || saved.Entries[0].FailedRuns != 1 {
		t.Fatalf("the set's own strategy opened the site and is never cached, so the failed run is the only change and must still reach the file: %d entries", len(saved.Entries))
	}
}

func TestAFailureWhereNothingPassedSaysNothingAboutACachedStrategy(t *testing.T) {
	ds := cachedSuite(t, []string{"a.example"}, comboEntry(207, 0))
	ds.putCached("a.example", ds.cachedPresets[0], CheckStatusFailed)
	ds.put("a.example", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put("a.example", "tcp", CheckStatusFailed, PhaseStrategy, 0)

	ds.saveResultsToCache()

	if got := ds.discoveryCache.Entries[0].FailedRuns; got != 0 {
		t.Errorf("a site no strategy opens, such as one blocked by address, must not cost the cache its entries: runs %d", got)
	}
	if cacheFileWritten(ds) {
		t.Error("nothing changed, so nothing is written")
	}
}

func TestAFailureOnASiteThatLoadsWithoutAStrategyIsNotAFailedRun(t *testing.T) {
	for _, plain := range []string{presetNoBypass, presetAltAddress, presetDNSRedirect, presetECHStrip} {
		ds := cachedSuite(t, []string{"a.example"}, comboEntry(207, 0))
		ds.putCached("a.example", ds.cachedPresets[0], CheckStatusFailed)
		ds.put("a.example", plain, CheckStatusComplete, PhaseBaseline, 0)
		ds.put("a.example", "tcp", CheckStatusComplete, PhaseStrategy, 0)

		ds.saveResultsToCache()

		if got := entryWithStrategy(ds.discoveryCache.Entries, "combo").FailedRuns; got != 0 {
			t.Errorf("%s: the site needs no packet strategy, so a strategy failing on it was not beaten by a block: runs %d", plain, got)
		}
	}
}

func entryWithStrategy(entries []CacheEntry, strategy string) *CacheEntry {
	for i := range entries {
		if entries[i].Config.Fragmentation.Strategy == strategy {
			return &entries[i]
		}
	}
	return nil
}

func TestAPassOnOneSiteOfTheRunClearsTheFailedRuns(t *testing.T) {
	ds := cachedSuite(t, []string{"a.example", "b.example"}, comboEntry(207, 2))
	for _, domain := range []string{"a.example", "b.example"} {
		ds.put(domain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		ds.put(domain, "tcp", CheckStatusComplete, PhaseStrategy, 0)
	}
	ds.putCached("a.example", ds.cachedPresets[0], CheckStatusComplete)
	ds.putCached("b.example", ds.cachedPresets[0], CheckStatusFailed)

	ds.saveResultsToCache()

	entry := entryWithStrategy(ds.discoveryCache.Entries, "combo")
	if entry == nil || entry.FailedRuns != 0 || entry.SuccessCount != 208 {
		t.Fatalf("the strategy opened a blocked site in this run, so it did not fail the run and its pass counts: %s", tally(entry))
	}
}

func TestAPassOnASiteThatLoadsWithoutAStrategyDoesNotClearTheFailedRuns(t *testing.T) {
	ds := cachedSuite(t, []string{"a.example"}, comboEntry(207, 2))
	ds.put("a.example", presetNoBypass, CheckStatusComplete, PhaseBaseline, 0)
	ds.putCached("a.example", ds.cachedPresets[0], CheckStatusComplete)

	ds.saveResultsToCache()

	entry := ds.discoveryCache.Entries[0]
	if entry.FailedRuns != 2 || entry.SuccessCount != 207 {
		t.Errorf("a site that loads anyway does not show that the strategy gets through a block again: %s", tally(&entry))
	}
}

func TestTheLastFailedRunMakesRoomForTheSimilarStrategyThatPassed(t *testing.T) {
	ds := cachedSuite(t, []string{"a.example"}, comboEntry(207, maxFailedRuns-1))
	ds.putCached("a.example", ds.cachedPresets[0], CheckStatusFailed)
	ds.put("a.example", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)

	variant := makeTestConfig("combo", 5, true)
	variant.Fragmentation.SNIPosition = 5
	ds.domainResults["a.example"].Results["combo-pos5"] = &DomainPresetResult{
		PresetName: "combo-pos5", Status: CheckStatusComplete, Phase: PhaseOptimize, Family: FamilyCombo, Speed: 5000, Set: &variant,
	}

	ds.saveResultsToCache()

	entries := ds.discoveryCache.Entries
	if len(entries) != 1 {
		t.Fatalf("the failing entry leaves and the strategy that passed takes its place, got %d entries", len(entries))
	}
	if entries[0].Config.Fragmentation.SNIPosition != 5 || entries[0].SuccessCount != 1 || entries[0].FailedRuns != 0 {
		t.Errorf("the cache must now hold the strategy that passed, with its own count: position %d count %d runs %d",
			entries[0].Config.Fragmentation.SNIPosition, entries[0].SuccessCount, entries[0].FailedRuns)
	}
}

func TestARunWhoseRulesChangedLeavesTheCacheAlone(t *testing.T) {
	ds := cachedSuite(t, []string{"a.example"}, comboEntry(207, 0))
	ds.putCached("a.example", ds.cachedPresets[0], CheckStatusFailed)
	ds.put("a.example", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put("a.example", "tcp", CheckStatusComplete, PhaseStrategy, 0)
	ds.RulesChanged = true

	ds.saveResultsToCache()

	if entries := ds.discoveryCache.Entries; len(entries) != 1 || entries[0].FailedRuns != 0 {
		t.Errorf("results that may come from another network path judge no strategy: %d entries, runs %d", len(entries), entries[0].FailedRuns)
	}
	if cacheFileWritten(ds) {
		t.Error("nothing changed, so nothing is written")
	}
}

func TestARunThatSkippedTheCacheJudgesNoCachedStrategy(t *testing.T) {
	ds := cachedSuite(t, []string{"a.example"}, comboEntry(207, 1))
	ds.cachedPresets = nil
	ds.put("a.example", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put("a.example", "tcp", CheckStatusComplete, PhaseStrategy, 0)

	ds.saveResultsToCache()

	if entry := entryWithStrategy(ds.discoveryCache.Entries, "combo"); entry == nil || entry.FailedRuns != 1 || entry.SuccessCount != 207 {
		t.Errorf("an entry the run did not test keeps what it had: %s", tally(entry))
	}
}
