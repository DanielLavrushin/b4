package discovery

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
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

const randomFakeSize = 1200

func wantRandomFake(t *testing.T, name string, faking *config.FakingConfig) {
	t.Helper()
	if faking.SNIType != config.FakePayloadRandom {
		t.Errorf("%s: the test config must keep the Random fake, got type %d", name, faking.SNIType)
	}
	if got := sock.GetPayload(faking); len(got) != randomFakeSize {
		t.Errorf("%s: the worker must send %d random bytes, got %d", name, randomFakeSize, len(got))
	}
}

func presetByName(t *testing.T, presets []ConfigPreset, name string) ConfigPreset {
	t.Helper()
	for _, p := range presets {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no preset named %q", name)
	return ConfigPreset{}
}

func TestBothTestConfigsKeepARandomFake(t *testing.T) {
	ds := setRunFixture(t, "a.example")
	ds.Domain = "a.example"
	ds.cfg = &config.Config{ConfigPath: filepath.Join(t.TempDir(), "b4.json")}
	live := config.NewSetConfig()
	live.Faking.SNIType = config.FakePayloadRandom
	ds.setStrategy = SetRunStrategy(&live)

	current, ok := ds.setCurrentPreset()
	if !ok {
		t.Fatal("a set run must test the set's current strategy")
	}
	for _, preset := range []ConfigPreset{current, presetByName(t, GetPhase2Presets(FamilyFakeSNI), "fake-payloadRand")} {
		wantRandomFake(t, preset.Name+" single", &ds.buildTestConfig(preset).Sets[0].Faking)
		wantRandomFake(t, preset.Name+" multi", &ds.buildTestConfigMulti(preset).Sets[0].Faking)
	}
}

func TestPresetsThatSendNoFakeKeepTheDefaultFakeType(t *testing.T) {
	ds := setRunFixture(t, "a.example")
	ds.Domain = "a.example"
	ds.cfg = &config.Config{ConfigPath: filepath.Join(t.TempDir(), "b4.json")}
	want := config.DefaultSetConfig.Faking.SNIType

	for _, preset := range []ConfigPreset{
		GetPhase1Presets()[0],
		plainFixPreset(presetECHStrip, FamilyECHStrip),
		plainFixPreset("alt-address", FamilyAltAddress),
	} {
		if got := ds.buildTestConfigMulti(preset).Sets[0].Faking.SNIType; got != want {
			t.Errorf("%s: a set made from a strategy that sends no fake must not switch to another type if its fake is turned on later, got %d, want %d", preset.Name, got, want)
		}
	}
}

func TestOnlyTheRandomPayloadPresetNamesARandomFake(t *testing.T) {
	families := []StrategyFamily{
		FamilyTCPFrag, FamilyTLSRec, FamilyOOB, FamilyIPFrag, FamilyFakeSNI, FamilySACK, FamilySynFake,
		FamilyDesync, FamilyWindow, FamilyDelay, FamilyMutation, FamilyDisorder, FamilyOverlap,
		FamilyExtSplit, FamilyFirstByte, FamilyCombo, FamilyHybrid, FamilyIncoming,
	}
	presets := GetPhase1Presets()
	best := map[StrategyFamily]ConfigPreset{}
	for _, family := range families {
		optimized := GetPhase2Presets(family)
		if len(optimized) > 0 {
			best[family] = optimized[0]
		}
		presets = append(presets, optimized...)
	}
	presets = append(presets, GetCombinationPresets(families, best)...)

	for _, preset := range presets {
		if preset.Config.Faking.SNIType == config.FakePayloadRandom && preset.Name != "fake-payloadRand" {
			t.Errorf("%s leaves the fake type at 0, which sends random bytes", preset.Name)
		}
	}
}

type fetchedFakes struct {
	mu    sync.Mutex
	sizes map[string][]int
}

func (f *fetchedFakes) record(name string, size int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizes[name] = append(f.sizes[name], size)
}

func (f *fetchedFakes) of(name string) []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sizes[name]
}

func setRunWithRandomFake(t *testing.T, loads func(fake []byte) bool) (*DiscoverySuite, *fetchedFakes) {
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
	stubResolver(t, map[string]stubName{"open.example": {a: "127.0.0.1"}})

	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Queue.IsDiscovery = true
	cfg.System.Checker.ConfigPropagateMs = 0
	cfg.System.Checker.DiscoveryTimeoutSec = 2

	pool := nfq.NewPool(&cfg)
	t.Cleanup(pool.Stop)

	fakes := &fetchedFakes{sizes: map[string][]int{}}
	var running atomic.Pointer[DiscoverySuite]
	page := "<html><body>" + strings.Repeat("x", 2048) + "</body></html>"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		set := pool.GetFirstWorkerConfig().Sets[0]
		fake := sock.GetPayload(&set.Faking)
		fakes.record(set.Name, len(fake))
		if set.Name != presetNoBypass && loads(fake) {
			_, _ = w.Write([]byte(page))
			return
		}
		if set.Name != presetNoBypass && set.Name != presetSetCurrent {
			_ = FinishCheckSuite(running.Load().Id)
		}
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	live := config.NewSetConfig()
	live.Faking.SNIType = config.FakePayloadRandom

	ds := NewDiscoverySuite([]string{"https://open.example:" + port + "/"}, pool, true, true, nil, 1, "", "ipv4", 0)
	ds.SetId = "set-a"
	ds.setStrategy = SetRunStrategy(&live)
	running.Store(ds)
	RegisterSuite(ds.CheckSuite)
	ds.RunDiscovery()
	return ds, fakes
}

func TestASetWithARandomFakeIsNotCalledWorkingBecauseFakeSNI1Passes(t *testing.T) {
	ds, fakes := setRunWithRandomFake(t, func(fake []byte) bool { return bytes.Equal(fake, config.FakeSNI1) })

	if len(fakes.of(presetSetCurrent)) == 0 {
		t.Fatal("the set's current strategy must be tested")
	}
	if v := ds.SetVerdict; v != nil && v.Status == SetVerdictCurrentWorks {
		t.Errorf("the live set sends random bytes, which this network refuses, so its strategy does not work: %+v", v)
	}
}

func TestASetRunTestsAndConfirmsARandomFakeWithRandomBytes(t *testing.T) {
	ds, fakes := setRunWithRandomFake(t, func(fake []byte) bool { return len(fake) == randomFakeSize })

	sent := fakes.of(presetSetCurrent)
	if len(sent) < 1+confirmTries {
		t.Fatalf("the test and every confirmation try must reach the server, got %d fetches", len(sent))
	}
	for i, size := range sent {
		if size != randomFakeSize {
			t.Errorf("fetch %d of the current strategy went out with a %d byte fake, the live set sends %d random bytes", i, size, randomFakeSize)
		}
	}
	if v := ds.SetVerdict; v == nil || v.Status != SetVerdictCurrentWorks {
		t.Errorf("the set's own fake loads the site, so its strategy works: %+v", v)
	}
}
