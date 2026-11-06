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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/sock"
)

func writeCapture(t *testing.T, cfgPath, name string, data []byte) string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(cfgPath), "captures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return "captures/" + name
}

func fakeEntry(strategy string, successes, sniType int, file, domain string) CacheEntry {
	sc := stripTargets(makeTestConfig(strategy, 5, true))
	sc.Faking.SNIType = sniType
	sc.Faking.PayloadFile = file
	sc.Faking.PayloadDomain = domain
	return CacheEntry{Config: sc, Family: FamilyCombo, SuccessCount: successes, Speed: 1000}
}

func storedCache(t *testing.T, cfgPath string, entries ...CacheEntry) *DiscoveryCache {
	t.Helper()
	if err := (&DiscoveryCache{Entries: entries}).Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	return LoadDiscoveryCache(cfgPath)
}

func TestCachedPresetsCarryTheBytesOfTheirFake(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "b4.json")
	capture := []byte{0x16, 0x03, 0x01, 0x02, 0x8a, 0x01}
	file := writeCapture(t, cfgPath, "tls_capture_example.bin", capture)
	emptyFile := writeCapture(t, cfgPath, "tls_empty_example.bin", nil)

	cache := storedCache(t, cfgPath,
		fakeEntry("capture", 7, config.FakePayloadCapture, file, ""),
		fakeEntry("domain", 6, config.FakePayloadDomain, "", "payload.example"),
		fakeEntry("builtin", 5, config.FakePayloadSTUN, "", ""),
		fakeEntry("gone", 4, config.FakePayloadCapture, "captures/tls_gone_example.bin", ""),
		fakeEntry("noname", 3, config.FakePayloadCapture, "", ""),
		fakeEntry("escapes", 2, config.FakePayloadCapture, "../tls_outside.bin", ""),
		fakeEntry("empty", 1, config.FakePayloadCapture, emptyFile, ""),
		fakeEntry("nodomain", 0, config.FakePayloadDomain, "", ""),
	)
	for _, p := range cache.GetCachedPresets() {
		if len(p.Config.Faking.PayloadData) != 0 {
			t.Fatalf("the fixture must hold no bytes, the way a stored cache does: %s", p.Name)
		}
	}

	ds := setRunFixture(t, "a.example")
	ds.cfg = &config.Config{ConfigPath: cfgPath}
	ds.Domain = "a.example"
	presets := ds.presetsWithPayloads(cache.GetCachedPresets())

	byStrategy := map[string]ConfigPreset{}
	for _, p := range presets {
		byStrategy[p.Config.Fragmentation.Strategy] = p
	}
	if len(presets) != 5 {
		t.Fatalf("a preset is skipped only when the capture or domain it names cannot be loaded, got %v", byStrategy)
	}

	sent := func(strategy string) []byte {
		t.Helper()
		preset, ok := byStrategy[strategy]
		if !ok {
			t.Fatalf("%s must be tested", strategy)
		}
		return sock.GetPayload(&ds.buildTestConfigMulti(preset).Sets[0].Faking)
	}
	if got := sent("capture"); !bytes.Equal(got, capture) {
		t.Errorf("the worker must send the file's bytes, got %d bytes", len(got))
	}
	if got := sent("domain"); bytes.Equal(got, config.FakeSNI1) || !bytes.Contains(got, []byte("payload.example")) {
		t.Errorf("the worker must send a hello generated for the named domain, got %d bytes", len(got))
	}
	if got := sent("builtin"); !bytes.Equal(got, config.FakeSTUN) {
		t.Errorf("a built-in fake stays what it was, got %d bytes", len(got))
	}
	for _, strategy := range []string{"noname", "nodomain"} {
		if got := sent(strategy); !bytes.Equal(got, config.FakeSNI1) {
			t.Errorf("%s names nothing to load, so a set made from it sends the built-in fake and so does its test, got %d bytes", strategy, len(got))
		}
	}

	for _, entry := range cache.Entries {
		if len(entry.Config.Faking.PayloadData) != 0 {
			t.Errorf("the cache keeps no bytes, %s holds %d", entry.Config.Fragmentation.Strategy, len(entry.Config.Faking.PayloadData))
		}
	}
}

func TestAMissingFakeSkipsOnlyAPresetThatSendsAFake(t *testing.T) {
	ds := setRunFixture(t, "a.example")
	ds.cfg = &config.Config{ConfigPath: filepath.Join(t.TempDir(), "b4.json")}
	gone := func(name string, change func(*config.SetConfig)) ConfigPreset {
		set := stripTargets(makeTestConfig(name, 5, false))
		set.Faking.SNIType = config.FakePayloadCapture
		set.Faking.PayloadFile = "captures/tls_gone_example.bin"
		change(&set)
		return ConfigPreset{Name: name, Config: set}
	}

	presets := ds.presetsWithPayloads([]ConfigPreset{
		gone("no-fake", func(*config.SetConfig) {}),
		gone("fake-sni", func(set *config.SetConfig) { set.Faking.SNI = true }),
		gone("syn-fake", func(set *config.SetConfig) { set.TCP.SynFake = true }),
		gone("decoy", func(set *config.SetConfig) { set.Fragmentation.Combo.DecoyEnabled = true }),
	})

	kept := map[string]bool{}
	for _, p := range presets {
		kept[p.Name] = true
	}
	if !kept["no-fake"] {
		t.Error("a strategy that sends no fake is not changed by a capture it names")
	}
	for _, name := range []string{"fake-sni", "syn-fake", "decoy"} {
		if kept[name] {
			t.Errorf("%s reads the fake's bytes, so its test would send the built-in fake instead", name)
		}
	}
}

func TestPresetsWithPayloadsLeavesFakesThatNeedNoBytesAlone(t *testing.T) {
	ds := setRunFixture(t, "a.example")
	ds.cfg = &config.Config{ConfigPath: filepath.Join(t.TempDir(), "b4.json")}
	presets := ds.presetsWithPayloads([]ConfigPreset{
		{Name: "random", Config: config.SetConfig{Faking: config.FakingConfig{SNIType: config.FakePayloadRandom}}},
		{Name: "zero", Config: config.SetConfig{Faking: config.FakingConfig{SNIType: config.FakePayloadZero}}},
		{Name: "inverted", Config: config.SetConfig{Faking: config.FakingConfig{SNIType: config.FakePayloadInverted}}},
		{Name: "custom", Config: config.SetConfig{Faking: config.FakingConfig{SNIType: config.FakePayloadCustom, CustomPayload: "abc"}}},
	})
	if len(presets) != 4 {
		t.Fatalf("only a capture or a domain fake can be missing, got %d presets", len(presets))
	}
	if got := sock.GetPayload(&presets[3].Config.Faking); string(got) != "abc" {
		t.Errorf("a custom payload stays what it was, got %q", got)
	}
}

type sentFakes struct {
	mu       sync.Mutex
	payloads map[string][][]byte
}

func (s *sentFakes) record(name string, payload []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payloads[name] = append(s.payloads[name], payload)
}

func (s *sentFakes) of(name string) [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.payloads[name]
}

func TestARunTestsAndConfirmsAPresetWithItsOwnFake(t *testing.T) {
	previous := suiteRetention
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })
	refuses := probeRefusesAddr
	probeRefusesAddr = nil
	t.Cleanup(func() { probeRefusesAddr = refuses })
	lookup := lookupECHConfigList
	lookupECHConfigList = func(context.Context, uint, string) []byte { return nil }
	t.Cleanup(func() { lookupECHConfigList = lookup })
	stubResolver(t, map[string]stubName{"open.example": {a: "127.0.0.1"}})

	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Queue.IsDiscovery = true
	cfg.System.Checker.ConfigPropagateMs = 0
	cfg.System.Checker.DiscoveryTimeoutSec = 2

	capture := bytes.Repeat([]byte{0x16, 0x03, 0x01}, 218)
	shared := bytes.Repeat([]byte{0x16, 0x03, 0x03}, 100)
	cachedFile := writeCapture(t, cfg.ConfigPath, "tls_capture_example.bin", capture)
	sharedFile := writeCapture(t, cfg.ConfigPath, "tls_shared_example.bin", shared)
	storedCache(t, cfg.ConfigPath,
		fakeEntry("capture", 3, config.FakePayloadCapture, cachedFile, ""),
		fakeEntry("gone", 2, config.FakePayloadCapture, "captures/tls_gone_example.bin", ""),
	)

	pool := nfq.NewPool(&cfg)
	t.Cleanup(pool.Stop)

	const (
		cachedCapture = "cached-1-combo"
		cachedGone    = "cached-2-combo"
		community     = "community-shared-v1"
	)
	fakes := &sentFakes{payloads: map[string][][]byte{}}
	var running atomic.Pointer[DiscoverySuite]
	page := "<html><body>" + strings.Repeat("x", 2048) + "</body></html>"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		set := pool.GetFirstWorkerConfig().Sets[0]
		fakes.record(set.Name, sock.GetPayload(&set.Faking))
		switch set.Name {
		case cachedCapture:
			_, _ = w.Write([]byte(page))
			return
		case presetNoBypass, cachedGone, community:
		default:
			_ = FinishCheckSuite(running.Load().Id)
		}
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	ds := NewDiscoverySuite([]string{"https://open.example:" + port + "/"}, pool, true, false, nil, 1, "", "ipv4", 0)
	running.Store(ds)
	sharedSet := stripTargets(makeTestConfig("shared", 9, true))
	sharedSet.Faking.SNIType = config.FakePayloadCapture
	sharedSet.Faking.PayloadFile = sharedFile
	ds.hubPresetsFn = func() []ConfigPreset {
		return []ConfigPreset{{Name: community, Family: FamilyCommunity, Phase: PhaseCached, Config: sharedSet, FixedPayload: true}}
	}
	RegisterSuite(ds.CheckSuite)
	ds.RunDiscovery()

	r := ds.DomainDiscoveryResults["open.example"].Results[cachedCapture]
	if r == nil || r.Status != CheckStatusComplete || r.Confirmed != confirmTries {
		t.Fatalf("the cached capture must pass and be confirmed, got %+v", r)
	}

	sent := fakes.of(cachedCapture)
	if len(sent) < 1+confirmTries {
		t.Fatalf("the test and every confirmation try must reach the server, got %d fetches", len(sent))
	}
	for i, payload := range sent {
		if !bytes.Equal(payload, capture) {
			t.Fatalf("fetch %d of the cached preset went out with a %d byte fake, the set made from it sends the %d byte capture", i, len(payload), len(capture))
		}
	}

	shareSent := fakes.of(community)
	if len(shareSent) == 0 {
		t.Fatal("the community preset must be tested")
	}
	for i, payload := range shareSent {
		if !bytes.Equal(payload, shared) {
			t.Fatalf("fetch %d of the community preset went out with a %d byte fake, the set made from it sends the %d byte capture", i, len(payload), len(shared))
		}
	}

	if got := fakes.of(cachedGone); len(got) != 0 {
		t.Errorf("a preset whose capture is gone must not be tested with another fake, %d fetches went out", len(got))
	}
	if ds.DomainDiscoveryResults["open.example"].Results[cachedGone] != nil {
		t.Error("a preset that was skipped must not report a result")
	}
}
