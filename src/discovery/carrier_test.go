package discovery

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nfq"
)

func replySet(incoming string, rst bool) config.SetConfig {
	set := config.NewSetConfig()
	set.Fragmentation.Strategy = "tcp"
	set.TCP.Incoming.Mode = incoming
	set.TCP.RSTProtection.Enabled = rst
	return set
}

func TestTheCarrierCannotTestStrategiesThatActOnReplies(t *testing.T) {
	cases := []struct {
		name     string
		set      config.SetConfig
		untested bool
	}{
		{"plain strategy", replySet(config.ConfigOff, false), false},
		{"incoming unset", replySet("", false), false},
		{"incoming fake", replySet("fake", false), true},
		{"RST protection", replySet(config.ConfigOff, true), true},
		{"both", replySet("reset", true), true},
	}
	for _, tc := range cases {
		if got := carrierUntestable(&tc.set) != ""; got != tc.untested {
			t.Errorf("%s: untestable = %v, want %v", tc.name, got, tc.untested)
		}
	}
	if carrierUntestable(nil) != "" {
		t.Error("a missing set has nothing to strip")
	}
}

func TestTheCarrierStripsReplyFieldsFromThePoolCopyOnly(t *testing.T) {
	plain := replySet(config.ConfigOff, false)
	current := replySet("fake", true)
	cfg := &config.Config{Sets: []*config.SetConfig{&plain, &current}}

	pooled := carrierPoolConfig(cfg)
	if pooled == cfg || pooled.Sets[1] == cfg.Sets[1] {
		t.Fatal("the pool must get a copy, never the test config the results keep")
	}
	if pooled.Sets[0] != cfg.Sets[0] {
		t.Error("a set with nothing to strip is passed through unchanged")
	}
	if got := pooled.Sets[1]; got.TCP.Incoming.Mode != config.ConfigOff || got.TCP.RSTProtection.Enabled {
		t.Errorf("the pool copy must run without the reply fields, got incoming %q rst %v", got.TCP.Incoming.Mode, got.TCP.RSTProtection.Enabled)
	}
	if current.TCP.Incoming.Mode != "fake" || !current.TCP.RSTProtection.Enabled {
		t.Error("the test config the results keep must not lose its reply fields")
	}
	if pooled.Sets[1].Fragmentation.Strategy != "tcp" {
		t.Error("the rest of the strategy must reach the pool unchanged")
	}

	clean := &config.Config{Sets: []*config.SetConfig{&plain}}
	if carrierPoolConfig(clean) != clean {
		t.Error("a config with nothing to strip is passed through as it is")
	}
}

func TestPresetsThatActOnRepliesAreSkippedOnlyOnTheCarrier(t *testing.T) {
	ds := setRunFixture(t, "a.example")
	incoming := ConfigPreset{Name: "incoming-fake-combo", Family: FamilyIncoming, Config: replySet("fake", false)}
	current := ConfigPreset{Name: presetSetCurrent, Family: FamilyCurrent, Config: replySet("fake", true)}

	if ds.skipOnCarrier(incoming) {
		t.Fatal("without the carrier nothing is skipped, so the NFQUEUE path tests every preset")
	}

	ds.carrier = true
	if ds.skipOnCarrier(current) {
		t.Error("the set's current strategy is tested without its reply fields, not skipped")
	}
	results := ds.testPresetAllDomains(incoming)
	r, ok := results["a.example"]
	if !ok || !r.untried || r.Status != CheckStatusFailed {
		t.Fatalf("a skipped preset reports an untried result, got %+v", results)
	}
	if ds.CompletedChecks != 1 || ds.SkippedChecks != 1 {
		t.Errorf("a skipped preset counts as a completed, skipped check, got %d/%d", ds.CompletedChecks, ds.SkippedChecks)
	}
	ds.storeResultsMulti(incoming, results)
	if _, stored := ds.domainResults["a.example"].Results[incoming.Name]; stored {
		t.Error("an untried preset must not be stored, or its family could count as working")
	}
	ds.Domain = "a.example"
	if r := ds.testPresetInternal(incoming); !r.untried {
		t.Errorf("the single-domain tester skips it too, got %+v", r)
	}
}

func TestTheCarrierForcesIPv4Probes(t *testing.T) {
	ds := suiteWithQueue("auto", true, true)
	if ds.dialNetwork() != "" {
		t.Fatal("precondition: a dual-stack queue leaves the family open")
	}
	ds.carrier = true
	if got := ds.dialNetwork(); got != "tcp4" {
		t.Errorf("the carrier is IPv4 only, dialNetwork = %q", got)
	}
	if ds.ipFilterVersion() != suiteWithQueue("auto", true, true).ipFilterVersion() {
		t.Error("the carrier must not change the IP version written into the tested set")
	}

	cfg := &config.Config{Queue: config.QueueConfig{IPv4Enabled: false, IPv6Enabled: true}}
	p := &DNSProber{cfg: cfg, ipVersion: "auto"}
	if p.ipNetwork() != "ip6" {
		t.Fatal("precondition: an IPv6-only queue resolves AAAA")
	}
	p.v4Only = true
	if got := p.ipNetwork(); got != "ip4" {
		t.Errorf("the carrier resolves A records only, ipNetwork = %q", got)
	}
}

func tunConfig() *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.Mode = "tun"
	return &cfg
}

func tunRuntime() *Runtime {
	rt := NewRuntime()
	rt.SetEngineTUN(true)
	return rt
}

func TestTheCarrierFollowsTheRunningEngineNotTheSavedMode(t *testing.T) {
	rt := NewRuntime()
	if rt.usesTUNCarrier() {
		t.Error("a process that started the NFQUEUE engine must keep the queue path even when queue.mode tun is saved and waits for a restart")
	}
	rt.SetEngineTUN(true)
	if !rt.usesTUNCarrier() {
		t.Error("a process that started the TUN engine must use the carrier")
	}
}

func TestDiscoveryUnderTUNWithoutACarrierIsRefusedWithACode(t *testing.T) {
	rt := tunRuntime()
	if _, err := rt.Start(tunConfig(), nil); !errors.Is(err, ErrTUNCarrierUnavailable) {
		t.Fatalf("got %v, want ErrTUNCarrierUnavailable", err)
	}
	if rt.IsActive() {
		t.Error("a refused start must leave the runtime idle")
	}
}

func TestDiscoveryUnderTUNRefusesIPv6BeforeTouchingTheCarrier(t *testing.T) {
	rt := tunRuntime()
	called := false
	rt.SetTUNCarrier(func(CarrierRequest) (Carrier, error) {
		called = true
		return nil, errors.New("must not be reached")
	})
	_, err := rt.StartSuite(tunConfig(), []string{"https://example.com/"}, StartSuiteOptions{IPVersion: "ipv6"})
	if !errors.Is(err, ErrIPv6UnsupportedTUN) {
		t.Fatalf("got %v, want ErrIPv6UnsupportedTUN", err)
	}
	if called || rt.IsActive() {
		t.Error("an IPv6 run must be refused before the carrier is started")
	}
}

type fakeCarrier struct {
	stopped atomic.Bool
	order   *[]string
}

func (c *fakeCarrier) Stop() {
	c.stopped.Store(true)
	if c.order != nil {
		*c.order = append(*c.order, "carrier")
	}
}

func stubCarrierPool(t *testing.T) {
	t.Helper()
	orig := newCarrierPool
	t.Cleanup(func() { newCarrierPool = orig })
	newCarrierPool = func(cfg *config.Config) (*nfq.Pool, error) {
		pool := nfq.NewPool(cfg)
		pool.SetTUNLocalOnly(true)
		return pool, nil
	}
}

func TestDiscoveryUnderTUNStartsTheCarrierWithTheDiscoveryMarksAndWorker(t *testing.T) {
	stubCarrierPool(t)
	rt := tunRuntime()
	var req CarrierRequest
	carrier := &fakeCarrier{}
	rt.SetTUNCarrier(func(r CarrierRequest) (Carrier, error) {
		req = r
		return carrier, nil
	})
	cfg := tunConfig()

	res, err := rt.Start(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Carrier || res.DSCP != nil || res.Path != nil {
		t.Errorf("a carrier run records no DSCP stamp and says it is carried, got %+v", res)
	}
	if req.FlowMark != cfg.DiscoveryFlowMark() || req.InjectedMark != cfg.DiscoveryInjectedMark() || req.Worker == nil || req.Worker != res.Pool.Workers[0] {
		t.Errorf("the carrier must get Discovery's marks and the pool's worker, got %+v", req)
	}
	poolCfg := res.Pool.GetFirstWorkerConfig()
	if !poolCfg.Queue.IsDiscovery || poolCfg.Queue.Mark != cfg.DiscoveryInjectedMark() || poolCfg.Queue.Mode != "tun" {
		t.Errorf("the pool must be a Discovery pool whose sends carry the injected mark, got %+v", poolCfg.Queue)
	}

	lostErr := errors.New("rule 81 removed")
	req.Lost(lostErr)
	rt.mu.Lock()
	recorded := rt.state.lostErr
	rt.mu.Unlock()
	if recorded != lostErr {
		t.Errorf("a loss reported before the suite is active must be kept for it, got %v", recorded)
	}

	rt.Stop("")
	if !carrier.stopped.Load() || rt.IsActive() {
		t.Error("Stop must stop the carrier and free the runtime")
	}
}

func TestTheCarrierPoolRunsInTUNModeWhateverModeIsSaved(t *testing.T) {
	stubCarrierPool(t)
	rt := tunRuntime()
	rt.SetTUNCarrier(func(CarrierRequest) (Carrier, error) { return &fakeCarrier{}, nil })
	saved := config.NewConfig()
	saved.Queue.Mode = "nfqueue"

	res, err := rt.Start(&saved, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Stop("")
	if mode := res.Pool.GetFirstWorkerConfig().Queue.Mode; mode != "tun" {
		t.Errorf("the carrier pool must send like the TUN engine this process runs, not like the saved mode, got %q", mode)
	}
}

func TestAFailedCarrierStartIsRefusedWithACodeAndFreesThePool(t *testing.T) {
	stubCarrierPool(t)
	rt := tunRuntime()
	cause := errors.New("the TUN engine has no source address")
	rt.SetTUNCarrier(func(CarrierRequest) (Carrier, error) { return nil, cause })

	_, err := rt.Start(tunConfig(), nil)
	if !errors.Is(err, ErrTUNCarrierUnavailable) || !errors.Is(err, cause) {
		t.Fatalf("got %v, want both ErrTUNCarrierUnavailable and the cause", err)
	}
	if rt.IsActive() {
		t.Error("a failed carrier must leave the runtime idle")
	}
}

type orderPool struct{ order *[]string }

func (p *orderPool) Stop() { *p.order = append(*p.order, "pool") }

func TestStopRemovesTheCarrierBeforeThePoolButKeepsTheQueueOrder(t *testing.T) {
	for _, tc := range []struct {
		carrier bool
		want    []string
	}{
		{true, []string{"carrier", "pool"}},
		{false, []string{"pool", "carrier"}},
	} {
		var order []string
		c := &fakeCarrier{order: &order}
		rt := NewRuntime()
		rt.state = &runtimeState{pool: &orderPool{order: &order}, clearRules: c.Stop, carrier: tc.carrier}
		rt.Stop("")
		if !slices.Equal(order, tc.want) {
			t.Errorf("carrier=%v: stop order %v, want %v", tc.carrier, order, tc.want)
		}
	}
}

func TestACarrierRunForASetsOwnDSCPIsRefusedBeforeTouchingAnything(t *testing.T) {
	rt := tunRuntime()
	called := false
	rt.SetTUNCarrier(func(CarrierRequest) (Carrier, error) {
		called = true
		return nil, errors.New("must not be reached")
	})
	_, err := rt.StartSuite(tunConfig(), []string{"https://example.com/"}, StartSuiteOptions{DSCP: &config.DSCPProbe{Value: 25, IPv4: true, IPv6: true}})
	if !errors.Is(err, ErrDSCPUnsupportedTUN) {
		t.Fatalf("got %v, want ErrDSCPUnsupportedTUN", err)
	}
	if called || rt.IsActive() {
		t.Error("a run that asks for a set's own DSCP must be refused before the carrier is started")
	}
}

func TestTheDSCPPathGuardStillRefusesAResultTestedWithoutTheSetsValue(t *testing.T) {
	set := config.NewSetConfig()
	set.Name = "yt"
	set.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
	tested := config.NewSetConfig()
	stamps := func(*config.Config, *config.DSCPProbe) (*config.DSCPProbe, *config.DSCPPath, error) {
		return &config.DSCPProbe{Value: 25, IPv4: true, IPv6: true}, nil, nil
	}
	cfg := tunConfig()
	cfg.Sets = []*config.SetConfig{&set}
	if change, err := DSCPPathChanged(cfg, &set, &tested, nil, stamps); err != nil || change == nil {
		t.Errorf("a result tested without the set's DSCP must not be adopted for it in TUN mode either, got %v, %v", change, err)
	}
}

type carriedFetch struct {
	mu       sync.Mutex
	incoming map[string][]string
	rst      map[string][]bool
}

func (f *carriedFetch) record(set *config.SetConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.incoming[set.Name] = append(f.incoming[set.Name], set.TCP.Incoming.Mode)
	f.rst[set.Name] = append(f.rst[set.Name], set.TCP.RSTProtection.Enabled)
}

func TestASetHealOnTheCarrierTestsTheCurrentStrategyWithoutReplyFieldsAndKeepsThem(t *testing.T) {
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
	cfg.Queue.Mode = "tun"
	cfg.System.Checker.ConfigPropagateMs = 0
	cfg.System.Checker.DiscoveryTimeoutSec = 2

	pool := nfq.NewPool(&cfg)
	t.Cleanup(pool.Stop)

	seen := &carriedFetch{incoming: map[string][]string{}, rst: map[string][]bool{}}
	var running atomic.Pointer[DiscoverySuite]
	page := "<html><body>" + strings.Repeat("x", 2048) + "</body></html>"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		set := pool.GetFirstWorkerConfig().Sets[0]
		seen.record(set)
		if set.Name == presetSetCurrent {
			_, _ = w.Write([]byte(page))
			return
		}
		if set.Name != presetNoBypass {
			_ = FinishCheckSuite(running.Load().Id)
		}
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	live := replySet("fake", true)
	ds := NewDiscoverySuite([]string{"https://open.example:" + port + "/"}, pool, true, true, nil, 1, "", "auto", 0)
	ds.SetId = "set-a"
	ds.setStrategy = SetRunStrategy(&live)
	ds.carrier = true
	ds.hubPresetsFn = func() []ConfigPreset {
		return []ConfigPreset{{Name: "hub-incoming", Family: FamilyCombo, Phase: PhaseCached, Config: replySet("reset", false)}}
	}
	running.Store(ds)
	RegisterSuite(ds.CheckSuite)
	ds.RunDiscovery()

	seen.mu.Lock()
	modes, rsts, hub := seen.incoming[presetSetCurrent], seen.rst[presetSetCurrent], len(seen.incoming["hub-incoming"])
	seen.mu.Unlock()
	if len(modes) < 1+confirmTries {
		t.Fatalf("the current strategy must be tested and confirmed through the pool, got %d fetches", len(modes))
	}
	for i := range modes {
		if modes[i] != config.ConfigOff || rsts[i] {
			t.Errorf("fetch %d of the current strategy ran with incoming %q, rst %v; the carrier's pool must run without them", i, modes[i], rsts[i])
		}
	}
	if hub != 0 {
		t.Errorf("a community preset with an incoming mode must be skipped on the carrier, it was fetched %d time(s)", hub)
	}

	v := ds.SetVerdict
	if v == nil || (v.Status != SetVerdictCurrentWorks && v.Status != SetVerdictCovered) || v.WinnerPreset != presetSetCurrent {
		t.Fatalf("the current strategy loads the site, so it wins: %+v", v)
	}
	if v.Set.TCP.Incoming.Mode != "fake" || !v.Set.TCP.RSTProtection.Enabled {
		t.Errorf("the verdict must keep the set's reply fields, got incoming %q rst %v", v.Set.TCP.Incoming.Mode, v.Set.TCP.RSTProtection.Enabled)
	}
	adopted := live
	adopted.AdoptStrategy(v.Set)
	if adopted.TCP.Incoming != live.TCP.Incoming || adopted.TCP.RSTProtection != live.TCP.RSTProtection {
		t.Errorf("adopting the verdict must leave the set's reply fields as they were, got %+v / %+v", adopted.TCP.Incoming, adopted.TCP.RSTProtection)
	}
}
