package discovery

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/nfq"
)

const (
	coldSeed = "198.51.100.7/32"
	cdnSeed  = "198.51.100.11/32"
	warmIP   = "203.0.113.10"
)

func synFakePreset(t *testing.T) ConfigPreset {
	t.Helper()
	for _, p := range GetPhase2Presets(FamilySynFake) {
		if p.Name == "synfake-len64" {
			return p
		}
	}
	t.Fatal("the catalogue has no synfake-len64 preset")
	return ConfigPreset{}
}

func seededSuite(t *testing.T, carrier bool, domains ...string) *DiscoverySuite {
	t.Helper()
	ds := setRunFixture(t, domains...)
	cfg := config.NewConfig()
	ds.cfg = &cfg
	ds.Domain = domains[0]
	ds.CheckURL = "https://" + domains[0] + "/"
	ds.carrier = carrier
	return ds
}

func missingGeoFiles(t *testing.T, ds *DiscoverySuite) {
	t.Helper()
	dir := t.TempDir()
	ds.cfg.System.Geo.GeoIpPath = filepath.Join(dir, "geoip.dat")
	ds.cfg.System.Geo.GeoSitePath = filepath.Join(dir, "geosite.dat")
}

func setJSON(t *testing.T, set *config.SetConfig) string {
	t.Helper()
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func seedOf(cidr string) string {
	return strings.TrimSuffix(cidr, "/32")
}

func TestACarrierSeedIsMatchedButNeverListedInTheTestedSet(t *testing.T) {
	ds := seededSuite(t, true, "cold.example")
	ds.carrierSeeds = map[string][]string{"cold.example": {coldSeed}}

	set := ds.buildTestConfig(synFakePreset(t)).Sets[0]
	if !slices.Contains(set.Targets.IpsToMatch, coldSeed) {
		t.Fatalf("the first SYN to the seeded address must be matched, IpsToMatch = %v", set.Targets.IpsToMatch)
	}
	if len(set.Targets.IPs) != 0 || len(set.DNS.Pins) != 0 {
		t.Errorf("a seed must never be listed as a target or a pin, got IPs %v pins %v", set.Targets.IPs, set.DNS.Pins)
	}
	if strings.Contains(setJSON(t, set), seedOf(coldSeed)) {
		t.Error("the tested set is kept with the result; its JSON must not carry the seed")
	}
}

func TestTheCDNBranchMatchesKnownAndSeededAddressesOnTheCarrier(t *testing.T) {
	const domain = "www.youtube.com"
	if geoip, geosite := GetCDNCategories(domain); len(geoip) == 0 && len(geosite) == 0 {
		t.Fatalf("precondition: %s must take the CDN branch", domain)
	}

	t.Run("no geo database, DNS-phase address", func(t *testing.T) {
		for _, carrier := range []bool{true, false} {
			ds := seededSuite(t, carrier, domain)
			ds.dnsResults[domain] = &DNSDiscoveryResult{ExpectedIPs: []string{warmIP}}
			set := ds.buildTestConfig(synFakePreset(t)).Sets[0]
			matched := slices.Contains(set.Targets.IpsToMatch, warmIP+"/32")
			if matched != carrier {
				t.Errorf("carrier=%v: the CDN branch matches the DNS-phase address = %v, IpsToMatch %v", carrier, matched, set.Targets.IpsToMatch)
			}
			if len(set.Targets.IPs) != 0 {
				t.Errorf("carrier=%v: the CDN branch lists no address, got %v", carrier, set.Targets.IPs)
			}
		}
	})

	t.Run("geo database configured, seed only", func(t *testing.T) {
		for _, carrier := range []bool{true, false} {
			ds := seededSuite(t, carrier, domain)
			missingGeoFiles(t, ds)
			ds.carrierSeeds = map[string][]string{domain: {cdnSeed}}
			set := ds.buildTestConfig(synFakePreset(t)).Sets[0]
			if len(set.Targets.GeoSiteCategories) == 0 {
				t.Fatal("precondition: the geo categories must be kept, so the geo targets are loaded before the seed is added")
			}
			if got := slices.Contains(set.Targets.IpsToMatch, cdnSeed); got != carrier {
				t.Errorf("carrier=%v: the seed survives loading the geo targets = %v, IpsToMatch %v", carrier, got, set.Targets.IpsToMatch)
			}
		}
	})
}

func TestTheMultiDomainTestSetMatchesTheSeedsOfColdDomains(t *testing.T) {
	ds := seededSuite(t, true, "warm.example", "cold.example", "i.ytimg.com")
	missingGeoFiles(t, ds)
	ds.dnsResults["warm.example"] = &DNSDiscoveryResult{ExpectedIPs: []string{warmIP}}
	ds.carrierSeeds = map[string][]string{"cold.example": {coldSeed}, "i.ytimg.com": {cdnSeed}}

	set := ds.buildTestConfigMulti(synFakePreset(t)).Sets[0]
	for _, want := range []string{warmIP + "/32", coldSeed, cdnSeed} {
		if !slices.Contains(set.Targets.IpsToMatch, want) {
			t.Errorf("IpsToMatch %v lacks %s", set.Targets.IpsToMatch, want)
		}
	}
	if !slices.Equal(set.Targets.IPs, []string{warmIP + "/32"}) {
		t.Errorf("only the DNS-phase address is listed, got %v", set.Targets.IPs)
	}
	if raw := setJSON(t, set); strings.Contains(raw, seedOf(coldSeed)) || strings.Contains(raw, seedOf(cdnSeed)) {
		t.Error("the tested set's JSON must not carry a seed")
	}
}

func TestScopedSetsMatchSeedsWithoutWritingThemIntoTheSet(t *testing.T) {
	ds := seededSuite(t, true, "cold.example")
	ds.carrierSeeds = map[string][]string{"cold.example": {coldSeed}}
	preset := synFakePreset(t)
	stored := ds.buildTestConfigMulti(preset).Sets[0]

	scoped := ds.scopePresetToDomains(preset.Name, stored, []string{"cold.example"})
	if !slices.Contains(scoped.Targets.IpsToMatch, coldSeed) {
		t.Errorf("the scoped set must match the seed, IpsToMatch = %v", scoped.Targets.IpsToMatch)
	}
	if len(scoped.Targets.IPs) != 0 || len(scoped.DNS.Pins) != 0 {
		t.Errorf("a seed must not become a listed address or a pin, got IPs %v pins %v", scoped.Targets.IPs, scoped.DNS.Pins)
	}
	if strings.Contains(setJSON(t, scoped), seedOf(coldSeed)) {
		t.Error("the set a run offers must not carry the seed in its JSON")
	}

	verdict := &SetVerdict{Status: SetVerdictCovered, Set: scoped, Covered: []string{"cold.example"}}
	if pins := verdict.CoveredPins(); len(pins) != 0 {
		t.Errorf("a heal writes the verdict's pins; a seed must not be one, got %v", pins)
	}
	live := config.NewSetConfig()
	live.Targets.IPs = []string{"192.0.2.1/32"}
	live.Targets.IpsToMatch = []string{"192.0.2.1/32"}
	live.AdoptStrategy(scoped)
	if !slices.Equal(live.Targets.IPs, []string{"192.0.2.1/32"}) || !slices.Equal(live.Targets.IpsToMatch, []string{"192.0.2.1/32"}) || len(live.DNS.Pins) != 0 {
		t.Errorf("adopting the strategy must leave the set's targets and pins alone, got %+v pins %v", live.Targets, live.DNS.Pins)
	}
}

func TestWithoutTheCarrierTheBuildersIgnoreSeeds(t *testing.T) {
	domains := []string{"warm.example", "cold.example", "www.youtube.com"}
	build := func(seeds map[string][]string) (*config.Config, *config.Config, *config.SetConfig) {
		ds := seededSuite(t, false, domains...)
		ds.dnsResults["warm.example"] = &DNSDiscoveryResult{ExpectedIPs: []string{warmIP}}
		ds.carrierSeeds = seeds
		preset := synFakePreset(t)
		multi := ds.buildTestConfigMulti(preset)
		ds.Domain = "www.youtube.com"
		single := ds.buildTestConfig(preset)
		return single, multi, ds.scopePresetToDomains(preset.Name, multi.Sets[0], domains)
	}
	single, multi, scoped := build(nil)
	seededSingle, seededMulti, seededScoped := build(map[string][]string{"cold.example": {coldSeed}, "www.youtube.com": {cdnSeed}})
	if !reflect.DeepEqual(single, seededSingle) || !reflect.DeepEqual(multi, seededMulti) || !reflect.DeepEqual(scoped, seededScoped) {
		t.Error("the NFQUEUE path must build exactly what it built before, whatever the seed field holds")
	}
}

func discoveryLines(t *testing.T, match string) func() []string {
	t.Helper()
	ch, _ := log.GetDiscoveryHub().Subscribe()
	t.Cleanup(func() { log.GetDiscoveryHub().Unsubscribe(ch) })
	return func() []string {
		var lines []string
		for {
			select {
			case line := <-ch:
				if strings.Contains(line, match) {
					lines = append(lines, line)
				}
			default:
				return lines
			}
		}
	}
}

func TestTheCarrierResolvesOnlyDomainsWithoutAKnownAddress(t *testing.T) {
	stubResolver(t, map[string]stubName{
		"cold.example": {a: seedOf(coldSeed)},
		"warm.example": {a: "198.51.100.20"},
		"hop.example":  {a: "198.51.100.21"},
		"v6.example":   {aaaa: "2001:db8::1"},
	})
	lines := discoveryLines(t, "gone.example")
	ds := seededSuite(t, true, "cold.example", "warm.example", "hop.example", "gone.example", "v6.example")
	ds.dnsResults["warm.example"] = &DNSDiscoveryResult{ExpectedIPs: []string{warmIP}}
	ds.dnsResults["hop.example"] = &DNSDiscoveryResult{GatewayIPs: []string{"10.0.0.1"}}

	ds.seedCarrierAddresses()

	want := map[string][]string{"cold.example": {coldSeed}}
	if !reflect.DeepEqual(ds.carrierSeeds, want) {
		t.Errorf("seeds = %v, want %v: a domain with a DNS-phase address, a gateway-answered one and names without an IPv4 address get none", ds.carrierSeeds, want)
	}
	got := lines()
	if len(got) != 1 || !strings.Contains(got[0], "v6.example") || !strings.Contains(got[0], "no such name") {
		t.Errorf("the failed lookups are reported in one Discovery line, got %q", got)
	}
	if r := ds.dnsResults["cold.example"]; r != nil {
		t.Errorf("a seed must not become a DNS result, which feeds pins and the dial list, got %+v", r)
	}
	if ds.givenPins != nil || ds.runPins != nil {
		t.Error("a seed must not become a pin")
	}
}

func TestTheCarrierNeverSeedsAnAddressTheProbeRefuses(t *testing.T) {
	stubResolver(t, map[string]stubName{"lan.example": {a: "192.168.1.20"}})
	refuses := probeRefusesAddr
	probeRefusesAddr = func(a netip.Addr) bool { return a == netip.MustParseAddr("192.168.1.20") }
	t.Cleanup(func() { probeRefusesAddr = refuses })
	lines := discoveryLines(t, "lan.example")
	ds := seededSuite(t, true, "lan.example")

	ds.seedCarrierAddresses()

	if len(ds.carrierSeeds) != 0 {
		t.Errorf("an address the probe refuses to dial must not be seeded, got %v", ds.carrierSeeds)
	}
	if got := lines(); len(got) != 1 || !strings.Contains(got[0], "no usable IPv4 address") {
		t.Errorf("the domain is named in the one failure line, got %q", got)
	}
}

func TestTheNFQUEUEPathResolvesNothingToSeed(t *testing.T) {
	orig := probeResolver
	t.Cleanup(func() { probeResolver = orig })
	called := false
	probeResolver = func(mark int, timeout time.Duration) *net.Resolver {
		called = true
		return orig(mark, timeout)
	}
	ds := seededSuite(t, false, "cold.example")
	ds.seedCarrierAddresses()
	if called || ds.carrierSeeds != nil {
		t.Error("without the carrier no lookup is made and nothing is seeded")
	}
}

func TestASeededSynFakePresetMatchesTheFirstSYNToAColdAddress(t *testing.T) {
	cold := net.ParseIP(seedOf(coldSeed))
	preset := synFakePreset(t)
	for _, carrier := range []bool{true, false} {
		cfg := config.NewConfig()
		cfg.Queue.IsDiscovery = true
		cfg.Queue.Mode = "tun"
		pool := nfq.NewPool(&cfg)
		t.Cleanup(pool.Stop)

		ds := seededSuite(t, carrier, "cold.example")
		ds.pool = pool
		ds.carrierSeeds = map[string][]string{"cold.example": {coldSeed}}
		if err := ds.applyPoolConfig(ds.buildTestConfigMulti(preset)); err != nil {
			t.Fatal(err)
		}

		m := pool.GetMatcher()
		matched, set := m.MatchIPWithSource(cold, "")
		learned, _, _ := m.MatchLearnedIPWithSource(cold, "")
		if carrier {
			if !matched || set.Name != preset.Name || !set.TCP.SynFake {
				t.Errorf("on the carrier the pool must match the first SYN to the seeded cold address with the SynFake preset, got %v %+v", matched, set)
			}
			continue
		}
		if matched || learned {
			t.Errorf("without seeds nothing matches the first SYN to a cold address, got ip %v learned %v", matched, learned)
		}
	}
}

func TestASetHealOnTheCarrierMatchesTheFirstConnectionOfItsCurrentStrategy(t *testing.T) {
	previous := suiteRetention
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })
	lookup := lookupECHConfigList
	lookupECHConfigList = func(context.Context, uint, string) []byte { return nil }
	t.Cleanup(func() { lookupECHConfigList = lookup })
	stubResolver(t, map[string]stubName{"open.example": {a: "127.0.0.1"}})
	loopback := net.ParseIP("127.0.0.1")

	for _, carrier := range []bool{true, false} {
		cfg := config.NewConfig()
		cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
		cfg.Queue.IsDiscovery = true
		cfg.Queue.Mode = "tun"
		cfg.System.Checker.ConfigPropagateMs = 0
		cfg.System.Checker.DiscoveryTimeoutSec = 2
		pool := nfq.NewPool(&cfg)
		t.Cleanup(pool.Stop)

		var running atomic.Pointer[DiscoverySuite]
		var once sync.Once
		var firstMatched atomic.Bool
		var seen atomic.Bool
		page := "<html><body>" + strings.Repeat("x", 2048) + "</body></html>"
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			set := pool.GetFirstWorkerConfig().Sets[0]
			if set.Name == presetSetCurrent {
				once.Do(func() {
					matched, by := pool.GetMatcher().MatchIPWithSource(loopback, "")
					firstMatched.Store(matched && by.Name == presetSetCurrent && by.TCP.SynFake)
					seen.Store(true)
				})
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

		live := config.NewSetConfig()
		live.Fragmentation.Strategy = "tcp"
		live.TCP.SynFake = true
		live.TCP.SynFakeLen = 64
		ds := NewDiscoverySuite([]string{"https://open.example:" + port + "/"}, pool, true, true, nil, 1, "", "auto", 0)
		ds.SetId = "set-a"
		ds.setStrategy = SetRunStrategy(&live)
		ds.carrier = carrier
		running.Store(ds)
		RegisterSuite(ds.CheckSuite)
		ds.RunDiscovery()

		if !seen.Load() {
			t.Fatalf("carrier=%v: the current strategy was never fetched", carrier)
		}
		if firstMatched.Load() != carrier {
			t.Errorf("carrier=%v: the first connection of the current strategy was matched at its SYN = %v; a heal seeds the address on the carrier only", carrier, firstMatched.Load())
		}
		if carrier && !reflect.DeepEqual(ds.carrierSeeds, map[string][]string{"open.example": {"127.0.0.1/32"}}) {
			t.Errorf("the heal resolves its domain before the tests, seeds = %v", ds.carrierSeeds)
		}
	}
}
