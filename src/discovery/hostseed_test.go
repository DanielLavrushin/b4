package discovery

import (
	"context"
	"net"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nfq"
)

const (
	pageHost   = "cdn.example.net"
	pageHostV4 = "198.51.100.7"
	pageHostV6 = "2001:db8::7"
)

func synFakeSet(t *testing.T) *config.SetConfig {
	t.Helper()
	set := synFakePreset(t).Config
	set.Name = "synfake"
	set.Enabled = true
	return &set
}

func blockedPageHost(f *fakeHosts, hosts ...string) []HostCandidate {
	var cands []HostCandidate
	for _, host := range hosts {
		f.without[sample(host)] = []hostProbe{blockedProbe, blockedProbe}
		cands = append(cands, cand(host, HostContextScript, true))
	}
	return cands
}

func TestAPageHostOnAnUnseenAddressIsTestedWithTheSYNTimePart(t *testing.T) {
	for _, carrier := range []bool{false, true} {
		cfg := config.NewConfig()
		cfg.Queue.IsDiscovery = true
		if carrier {
			cfg.Queue.Mode = "tun"
		}
		pool := nfq.NewPool(&cfg)
		t.Cleanup(pool.Stop)
		ds, f := hostRunFixture(t, "news.example.com")
		ds.carrier = carrier
		ds.cfg.Queue.IsDiscovery, ds.cfg.Queue.Mode = true, cfg.Queue.Mode
		ds.pool = pool
		ds.hostConfigFn = ds.applyPoolConfig
		stubResolver(t, map[string]stubName{pageHost: {a: pageHostV4}})
		cands := blockedPageHost(f, pageHost)
		ds.hostProbeFn = func(ctx context.Context, rawURL, origin string, with bool) hostProbe {
			if with {
				matched, set := pool.GetMatcher().MatchIPWithSource(net.ParseIP(pageHostV4), "")
				if !matched || !set.TCP.SynFake {
					return blockedProbe
				}
			}
			return f.probe(ctx, rawURL, origin, with)
		}

		results := ds.CheckHosts(context.Background(), HostTarget{
			Preset: "synfake", Set: synFakeSet(t), Domains: []string{"news.example.com"}, Candidates: cands,
		})

		if len(results) != 1 || results[0].Verdict != HostNeeded {
			t.Errorf("carrier=%v: a SYN carries no name, so the first connection gets the fake SYN only when the address is already matched: %+v", carrier, results)
		}
	}
}

func TestPageHostAddressesAreLookedUpAsTheStrategyResolvesThem(t *testing.T) {
	ds, f := hostRunFixture(t, "news.example.com")
	ds.dnsResults["news.example.com"] = &DNSDiscoveryResult{IsPoisoned: true}
	set := synFakeSet(t)
	set.DNS = config.DNSConfig{Enabled: true, DoHURL: settleDoH}
	set.Targets.IpsToMatch = make([]string, 0, 8)
	stored := setJSON(t, set)
	stubResolver(t, map[string]stubName{pageHost: {a: pageHostV4}, "img.example.net": {a: "198.51.100.8"}})
	resolver := probeResolver
	var appliedAtLookup []int
	probeResolver = func(mark int, timeout time.Duration) *net.Resolver {
		f.mu.Lock()
		appliedAtLookup = append(appliedAtLookup, len(f.configs))
		f.mu.Unlock()
		return resolver(mark, timeout)
	}

	ds.CheckHosts(context.Background(), HostTarget{
		Preset: "synfake", Set: set, Domains: []string{"news.example.com"}, Candidates: blockedPageHost(f, pageHost, "img.example.net", "gone.example.net"),
	})

	if len(f.configs) != 3 || !reflect.DeepEqual(appliedAtLookup, []int{2}) {
		t.Fatalf("b4 off, then the strategy with the hosts by name, then the same with their addresses: %d configurations, lookups after %v", len(f.configs), appliedAtLookup)
	}
	named, matched := f.configs[1].Sets[0], f.configs[2].Sets[0]
	if !named.DNS.Enabled || !slices.Contains(named.Targets.SNIDomains, pageHost) || len(named.Targets.IpsToMatch) != 0 {
		t.Errorf("the lookup runs with the set's DNS fix and the hosts already in the set, so it gets the answer the test's own connection gets: %+v %+v", named.DNS, named.Targets)
	}
	if got := slices.Sorted(slices.Values(matched.Targets.IpsToMatch)); !reflect.DeepEqual(got, []string{pageHostV4 + "/32", "198.51.100.8/32"}) {
		t.Errorf("every address the hosts resolve to is matched, got %v", got)
	}
	if !reflect.DeepEqual(matched.Targets.SNIDomains, named.Targets.SNIDomains) || matched.DNS.DoHURL != named.DNS.DoHURL {
		t.Errorf("only the matched addresses differ: %+v, %+v", matched.Targets, named.Targets)
	}
	if len(matched.Targets.IPs) != 0 || strings.Contains(setJSON(t, matched), pageHostV4) {
		t.Errorf("a page host's address is matched for the test and listed nowhere: %v", matched.Targets.IPs)
	}
	backing := set.Targets.IpsToMatch[:cap(set.Targets.IpsToMatch)]
	if setJSON(t, set) != stored || len(set.Targets.IpsToMatch) != 0 || slices.ContainsFunc(backing, func(s string) bool { return s != "" }) {
		t.Errorf("the set the run offers stays untouched, its matched addresses included: %v, backing %v", set.Targets.IpsToMatch, backing)
	}
}

func TestPageHostAddressesFollowTheFamiliesTheProbesDial(t *testing.T) {
	for _, tc := range []struct {
		name       string
		ipVersion  string
		ipv4, ipv6 bool
		carrier    bool
		want       []string
	}{
		{"IPv4 only", "", true, false, false, []string{pageHostV4 + "/32"}},
		{"IPv6 only", "", false, true, false, []string{pageHostV6 + "/128"}},
		{"both", "", true, true, false, []string{pageHostV4 + "/32", pageHostV6 + "/128"}},
		{"a run over IPv6", "ipv6", true, true, false, []string{pageHostV6 + "/128"}},
		{"the TUN carrier", "", true, true, true, []string{pageHostV4 + "/32"}},
	} {
		ds, f := hostRunFixture(t, "news.example.com")
		ds.ipVersion, ds.carrier = tc.ipVersion, tc.carrier
		ds.cfg.Queue.IPv4Enabled, ds.cfg.Queue.IPv6Enabled = tc.ipv4, tc.ipv6
		stubResolver(t, map[string]stubName{pageHost: {a: pageHostV4, aaaa: pageHostV6}})

		ds.CheckHosts(context.Background(), HostTarget{
			Preset: "synfake", Set: synFakeSet(t), Domains: []string{"news.example.com"}, Candidates: blockedPageHost(f, pageHost),
		})

		got := slices.Sorted(slices.Values(f.configs[len(f.configs)-1].Sets[0].Targets.IpsToMatch))
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: matched %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStoppingDuringThePageHostLookupsAppliesNothingMore(t *testing.T) {
	ds, f := hostRunFixture(t, "news.example.com")
	ds.ipVersion = ""
	ds.cfg.Queue.IPv4Enabled, ds.cfg.Queue.IPv6Enabled = true, true
	stubResolver(t, map[string]stubName{pageHost: {a: pageHostV4}, "slow.example.net": {a: "198.51.100.8", aaaaLag: 600 * time.Millisecond}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resolver := probeResolver
	var once sync.Once
	probeResolver = func(mark int, timeout time.Duration) *net.Resolver {
		once.Do(func() { time.AfterFunc(150*time.Millisecond, cancel) })
		return resolver(mark, timeout)
	}

	results := ds.CheckHosts(ctx, HostTarget{
		Preset: "synfake", Set: synFakeSet(t), Domains: []string{"news.example.com"}, Candidates: blockedPageHost(f, pageHost, "slow.example.net"),
	})

	if len(f.configs) != 2 || f.count("with ") != 0 {
		t.Errorf("a stopped step applies no further configuration and probes nothing, even with one host already looked up: %d configurations, %v", len(f.configs), f.calls)
	}
	for _, r := range results {
		if r.Verdict != HostUntested {
			t.Errorf("a host the stop cut off is untested: %+v", r)
		}
	}
}

func TestAnAddressSetThatAddsNothingIsNotAppliedAgain(t *testing.T) {
	ds, f := hostRunFixture(t, "news.example.com")
	stubResolver(t, map[string]stubName{"a.example.net": {a: pageHostV4}, "b.example.net": {a: pageHostV4}})
	base := ds.deployedConfig("synfake", synFakeSet(t), []string{"news.example.com"}, nil)
	base.Sets[0].Targets.IpsToMatch = []string{pageHostV4 + "/32"}

	if err := ds.applyHostsWithAddresses(context.Background(), withHosts(base, []string{"a.example.net", "b.example.net"}), []string{"a.example.net", "b.example.net"}); err != nil {
		t.Fatal(err)
	}
	if len(f.configs) != 1 {
		t.Errorf("the addresses are matched already, so the hosts are applied once, got %d configurations", len(f.configs))
	}
	if withAddresses(nil, []string{pageHostV4 + "/32"}) != nil {
		t.Error("no configuration, nothing to add to")
	}
	empty := &config.Config{}
	if withAddresses(empty, []string{pageHostV4 + "/32"}) != empty {
		t.Error("a configuration without a set is left alone")
	}
}
