package discovery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nfq"
)

const (
	settleDomain = "www.svoboda.org"
	settleDoH    = "https://1.1.1.1/dns-query"
)

var (
	settleSystem    = []string{"2.16.6.221", "2.16.6.200"}
	settleReference = []string{"2.19.183.10", "2.19.183.43"}
)

func settleProber(serving map[string]bool) (*DNSProber, *int) {
	calls := 0
	p := &DNSProber{
		domain:    settleDomain,
		timeout:   time.Second,
		ipVersion: "ipv4",
		serves: func(_ context.Context, ip string) bool {
			return serving[ip]
		},
		connectable: func(_ context.Context, ips []string) bool {
			return len(ips) > 0
		},
		gateway: func(context.Context, string) bool {
			return false
		},
		bypass: func(_ context.Context, r *DNSDiscoveryResult, _ []string) {
			calls++
			r.BestDoHURL = settleDoH
		},
	}
	return p, &calls
}

func TestAPublicSystemAnswerNobodyServesIsOnlyACandidate(t *testing.T) {
	p, calls := settleProber(nil)

	r := p.evaluate(context.Background(), settleSystem, settleReference, false)

	if !r.IsPoisoned || !r.Unverified {
		t.Fatalf("neither side completes a handshake, so the run tests with the DNS fix but must not call it proven: poisoned=%v unverified=%v", r.IsPoisoned, r.Unverified)
	}
	if !reflect.DeepEqual(r.systemIPs, settleSystem) {
		t.Errorf("the system addresses are kept for the settle step, got %v", r.systemIPs)
	}
	if *calls != 1 || r.BestDoHURL != settleDoH {
		t.Errorf("the DNS fix is still searched once, as today: calls=%d fix=%q", *calls, r.BestDoHURL)
	}
	if r.ExpectedIPs[0] != settleReference[0] {
		t.Errorf("the reference addresses stay the first targets, got %v", r.ExpectedIPs)
	}
}

func TestOnlyAnUnprovenAnswerIsLeftUnverified(t *testing.T) {
	cases := []struct {
		name      string
		system    []string
		reference []string
		serves    bool
		poisoned  bool
	}{
		{"the reference serves the site", settleSystem, settleReference, true, true},
		{"the answer holds a benchmark address", []string{"2.16.6.221", "198.18.0.7"}, settleReference, false, true},
		{"the answer holds a private address", []string{"10.0.0.5"}, settleReference, false, true},
		{"the system resolver gave nothing", nil, settleReference, false, true},
		{"both answers share a subnet", []string{"2.19.183.99"}, settleReference, false, false},
	}
	for _, tc := range cases {
		p, _ := settleProber(nil)
		r := p.evaluate(context.Background(), tc.system, tc.reference, tc.serves)
		if r.IsPoisoned != tc.poisoned || r.Unverified {
			t.Errorf("%s: poisoned=%v unverified=%v, want poisoned=%v and a verdict that needs no settling", tc.name, r.IsPoisoned, r.Unverified, tc.poisoned)
		}
	}
}

func TestAProberWithoutABypassSeamSearchesTheRealWay(t *testing.T) {
	p, _ := settleProber(nil)
	p.bypass = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := p.evaluate(ctx, settleSystem, settleReference, false)

	if !r.IsPoisoned || r.BestDoHURL != "" {
		t.Fatalf("a prober built as a literal falls back to the real search, which a canceled run ends at once: %+v", r)
	}
}

func TestOnlyAReferencePinIsMarkedAsOne(t *testing.T) {
	pinned := &DNSDiscoveryResult{IsPoisoned: true, referenceIPs: settleReference[:1], referenceTrusted: true}
	pinReferenceWhenNoFix(settleDomain, pinned)
	if !pinned.referencePinned || len(pinned.AlternativeIPs) != 1 {
		t.Fatalf("the reference addresses were pinned and must be known as such: %+v", pinned)
	}

	scanned := &DNSDiscoveryResult{IsPoisoned: true, AlternativeIPs: []string{"2.20.1.1"}, referenceIPs: settleReference[:1], referenceTrusted: true}
	pinReferenceWhenNoFix(settleDomain, scanned)
	if scanned.referencePinned {
		t.Fatal("addresses another region's DNS gave are not reference pins")
	}

	fixed := &DNSDiscoveryResult{IsPoisoned: true, BestDoHURL: settleDoH, referenceIPs: settleReference[:1], referenceTrusted: true}
	pinReferenceWhenNoFix(settleDomain, fixed)
	if fixed.referencePinned {
		t.Fatal("a site with a DNS fix gets no pins")
	}
}

type settleStub struct {
	calls  []string
	addrs  []map[string][]string
	served map[string]bool
}

func (s *settleStub) check(name string, _ *config.SetConfig, inputs []DomainInput, addrs map[string][]string) (map[string]bool, bool) {
	s.calls = append(s.calls, name)
	s.addrs = append(s.addrs, addrs)
	out := map[string]bool{}
	for _, di := range inputs {
		out[di.Domain] = s.served[di.Domain]
	}
	return out, true
}

func settleSuite(t *testing.T, domains ...string) *DiscoverySuite {
	t.Helper()
	inputs := make([]DomainInput, 0, len(domains))
	for _, d := range domains {
		inputs = append(inputs, DomainInput{Domain: d, CheckURL: "https://" + d + "/"})
	}
	suite := NewCheckSuite(inputs)
	suite.Status = CheckStatusRunning
	ds := &DiscoverySuite{
		CheckSuite:    suite,
		domainResults: map[string]*DomainDiscoveryResult{},
		dnsResults:    map[string]*DNSDiscoveryResult{},
	}
	for _, d := range domains {
		ds.domainResults[d] = &DomainDiscoveryResult{Domain: d, Url: "https://" + d + "/", Results: map[string]*DomainPresetResult{}}
	}
	ds.initCancelContext()
	t.Cleanup(ds.ctxCancel)
	return ds
}

func (ds *DiscoverySuite) candidate(domain string, mutate func(*DNSDiscoveryResult)) *DNSDiscoveryResult {
	r := &DNSDiscoveryResult{
		IsPoisoned:  true,
		Unverified:  true,
		ExpectedIPs: append(append([]string(nil), settleReference...), settleSystem...),
		BestDoHURL:  settleDoH,
		ProbeResults: []DNSProbeResult{
			{ResolvedIP: settleSystem[0], ExpectedIP: settleReference[0], IsPoisoned: true},
			{Server: settleDoH, ResolvedIP: settleReference[0], ExpectedIP: settleReference[0], Works: true},
		},
		systemIPs: append([]string(nil), settleSystem...),
	}
	if mutate != nil {
		mutate(r)
	}
	ds.dnsResults[domain] = r
	ds.domainResults[domain].DNSResult = r
	return r
}

func (ds *DiscoverySuite) found(domain, preset string, speed float64) {
	ds.put(domain, preset, CheckStatusComplete, PhaseCached, 0)
	r := ds.domainResults[domain].Results[preset]
	r.Speed = speed
	r.Set.Name = preset
	r.Set.DNS = config.DNSConfig{Enabled: true, DoHURL: settleDoH}
}

func groupSetFor(t *testing.T, ds *DiscoverySuite, domain string) *config.SetConfig {
	t.Helper()
	ds.buildStrategyGroups()
	for _, g := range ds.StrategyGroups {
		if containsString(g.Domains, domain) {
			return g.Set
		}
	}
	t.Fatalf("%s is in no group: %+v", domain, ds.StrategyGroups)
	return nil
}

func TestASettledCandidateLeavesTheReferencePinsOut(t *testing.T) {
	ds := settleSuite(t, settleDomain)
	r := ds.candidate(settleDomain, func(r *DNSDiscoveryResult) {
		r.BestDoHURL = ""
		r.AlternativeIPs = append([]string(nil), settleReference...)
		r.referencePinned = true
	})
	ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.found(settleDomain, "cached-1-tcpmd5", 1000)
	ds.determineBest()
	stub := &settleStub{served: map[string]bool{settleDomain: true}}
	ds.dnsSettleFn = stub.check

	ds.settleUnverifiedDNS()

	if !reflect.DeepEqual(stub.calls, []string{"cached-1-tcpmd5"}) || !reflect.DeepEqual(stub.addrs[0][settleDomain], settleSystem) {
		t.Fatalf("both system addresses are tried with the strategy the card offers: calls %v, addresses %v", stub.calls, stub.addrs)
	}
	if r.IsPoisoned || r.Unverified || !r.SystemServesWithStrategy {
		t.Fatalf("the system addresses loaded the site, so its DNS is not poisoned: %+v", r)
	}
	if r.BestDoHURL != "" || r.BestServer != "" || r.NeedsFragment || len(r.AlternativeIPs) != 0 || r.referencePinned {
		t.Errorf("no DNS fix and no reference pin is left: %+v", r)
	}
	if r.ProbeResults[0].IsPoisoned || !r.ProbeResults[0].Works {
		t.Errorf("the system resolver's probe entry now reads as working: %+v", r.ProbeResults[0])
	}
	set := groupSetFor(t, ds, settleDomain)
	if set.DNS.Enabled || set.DNS.DoHURL != "" || len(set.DNS.Pins) != 0 {
		t.Errorf("the offered set carries no DNS fix and no pin: %+v", set.DNS)
	}
	if set.TCP.IPBlockDetect != config.DefaultSetConfig.TCP.IPBlockDetect {
		t.Errorf("without pins the set keeps the default address-block detection: %+v", set.TCP.IPBlockDetect)
	}
}

func TestASettledCandidateKeepsTheAddressesAnotherRegionGave(t *testing.T) {
	ds := settleSuite(t, settleDomain)
	r := ds.candidate(settleDomain, func(r *DNSDiscoveryResult) {
		r.AlternativeIPs = []string{"2.20.1.1"}
		r.BestServer = "8.8.8.8:53"
		r.NeedsFragment = true
	})
	ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.found(settleDomain, "cached-1-tcpmd5", 1000)
	ds.determineBest()
	ds.dnsSettleFn = (&settleStub{served: map[string]bool{settleDomain: true}}).check

	ds.settleUnverifiedDNS()

	if r.IsPoisoned || !reflect.DeepEqual(r.AlternativeIPs, []string{"2.20.1.1"}) {
		t.Fatalf("an address that passed TLS without a strategy stays pinned: %+v", r)
	}
	if r.BestDoHURL != "" || r.BestServer != "" || r.NeedsFragment {
		t.Errorf("no DNS fix is left next to a verdict that is not poisoned: %+v", r)
	}
	set := groupSetFor(t, ds, settleDomain)
	if set.DNS.Enabled || !reflect.DeepEqual(set.DNS.Pins[settleDomain], []string{"2.20.1.1"}) {
		t.Errorf("the DNS fix goes, the scanned pin stays: %+v", set.DNS)
	}
	if !set.TCP.IPBlockDetect.Enabled {
		t.Error("a pinned set watches for dead addresses")
	}
}

func TestAnUnservedCandidateKeepsTheDNSFix(t *testing.T) {
	ds := settleSuite(t, settleDomain)
	r := ds.candidate(settleDomain, nil)
	ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.found(settleDomain, "cached-1-tcpmd5", 1000)
	ds.determineBest()
	ds.dnsSettleFn = (&settleStub{served: map[string]bool{settleDomain: false}}).check

	ds.settleUnverifiedDNS()

	if !r.IsPoisoned || r.Unverified || r.SystemServesWithStrategy || r.BestDoHURL != settleDoH {
		t.Fatalf("a system address that fails even with the strategy proves the poisoning: %+v", r)
	}
	if set := groupSetFor(t, ds, settleDomain); !set.DNS.Enabled || set.DNS.DoHURL != settleDoH {
		t.Errorf("the offered set keeps the DNS fix: %+v", set.DNS)
	}
}

func TestAnAnswerTheFirstHopPartlyTakesIsNeverSettled(t *testing.T) {
	run := func(t *testing.T, hijacked string) (*DiscoverySuite, *DNSDiscoveryResult, *settleStub) {
		t.Helper()
		p, _ := settleProber(nil)
		p.gateway = func(_ context.Context, ip string) bool { return ip == hijacked }
		r := p.evaluate(context.Background(), settleSystem, settleReference, false)
		ds := settleSuite(t, settleDomain)
		ds.dnsResults[settleDomain] = r
		ds.domainResults[settleDomain].DNSResult = r
		ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		ds.found(settleDomain, "cached-1-tcpmd5", 1000)
		ds.determineBest()
		stub := &settleStub{served: map[string]bool{settleDomain: true}}
		ds.dnsSettleFn = stub.check
		return ds, r, stub
	}

	ds, r, stub := run(t, settleSystem[0])
	if !r.IsPoisoned || r.Unverified || len(r.systemIPs) != 0 || r.BestDoHURL != settleDoH {
		t.Fatalf("the system resolver also hands out an address the first hop answers, which no strategy gets past, so the poisoning stands with its fix: %+v", r)
	}
	ds.settleUnverifiedDNS()
	if len(stub.calls) != 0 || !r.IsPoisoned {
		t.Errorf("nothing is left to settle: tried %v, %+v", stub.calls, r)
	}
	if set := groupSetFor(t, ds, settleDomain); !set.DNS.Enabled || set.DNS.DoHURL != settleDoH {
		t.Errorf("the offered set keeps the DNS fix: %+v", set.DNS)
	}

	ds, r, stub = run(t, settleReference[1])
	if !r.Unverified || !reflect.DeepEqual(r.systemIPs, settleSystem) {
		t.Fatalf("a reference address the first hop answers says nothing about the system answer, which stays a candidate: %+v %v", r, r.systemIPs)
	}
	ds.settleUnverifiedDNS()
	if len(stub.addrs) != 1 || !reflect.DeepEqual(stub.addrs[0][settleDomain], settleSystem) || r.IsPoisoned {
		t.Errorf("both system addresses are tried and serve the site: %v %+v", stub.addrs, r)
	}
}

func TestNothingToSettleWithLeavesTheCandidateAsItIs(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T, ds *DiscoverySuite)
	}{
		{"nothing worked", func(t *testing.T, ds *DiscoverySuite) {
			ds.put(settleDomain, "cached-1-tcpmd5", CheckStatusFailed, PhaseCached, 0)
		}},
		{"a plain fix won", func(t *testing.T, ds *DiscoverySuite) {
			ds.found(settleDomain, "cached-1-tcpmd5", 1000)
			ds.put(settleDomain, presetDNSRedirect, CheckStatusComplete, PhaseBaseline, 0)
		}},
		{"the run was canceled", func(t *testing.T, ds *DiscoverySuite) {
			ds.found(settleDomain, "cached-1-tcpmd5", 1000)
			close(ds.cancel)
		}},
		{"the check URL is plain HTTP", func(t *testing.T, ds *DiscoverySuite) {
			ds.found(settleDomain, "cached-1-tcpmd5", 1000)
			ds.Domains[0].CheckURL = "http://" + settleDomain + "/"
		}},
		{"the rules changed", func(t *testing.T, ds *DiscoverySuite) {
			ds.found(settleDomain, "cached-1-tcpmd5", 1000)
			previous := rulesInvalidated
			rulesInvalidated = func() bool { return true }
			t.Cleanup(func() { rulesInvalidated = previous })
		}},
	}
	for _, tc := range cases {
		ds := settleSuite(t, settleDomain)
		r := ds.candidate(settleDomain, nil)
		ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		tc.build(t, ds)
		ds.determineBest()
		stub := &settleStub{served: map[string]bool{settleDomain: true}}
		ds.dnsSettleFn = stub.check

		ds.settleUnverifiedDNS()

		if len(stub.calls) != 0 {
			t.Errorf("%s: nothing may be tried, tried %v", tc.name, stub.calls)
		}
		if !r.IsPoisoned || !r.Unverified || r.BestDoHURL != settleDoH {
			t.Errorf("%s: the candidate keeps today's DNS fix and stays unverified: %+v", tc.name, r)
		}
	}
}

func TestTheSettleTriesTheStrategyTheCardOffers(t *testing.T) {
	ds := settleSuite(t, settleDomain)
	ds.candidate(settleDomain, nil)
	ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.found(settleDomain, "cached-1-tcpmd5", 1000)
	ds.found(settleDomain, "cached-10-fake_sni", 5000)
	ds.determineBest()
	if ds.domainResults[settleDomain].BestPreset != "cached-10-fake_sni" {
		t.Fatal("precondition: the faster preset is the best one")
	}
	stub := &settleStub{served: map[string]bool{settleDomain: true}}
	ds.dnsSettleFn = stub.check

	ds.settleUnverifiedDNS()

	if !reflect.DeepEqual(stub.calls, []string{"cached-1-tcpmd5"}) {
		t.Fatalf("the group winner is what the card offers, so it is the one tried, got %v", stub.calls)
	}

	set := setRunFixture(t, settleDomain)
	set.candidate(settleDomain, nil)
	set.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	set.found(settleDomain, "cached-1-tcpmd5", 1000)
	set.found(settleDomain, "p-cover", 500)
	set.domainResults[settleDomain].Results["p-cover"].Phase = PhaseStrategy
	set.coverWinner = "p-cover"
	set.jointConfirmed = map[string]bool{"p-cover": true}
	set.domainResults[settleDomain].Results["p-cover"].Confirmed = confirmTries
	set.domainResults[settleDomain].Results["p-cover"].ConfirmTries = confirmTries
	set.determineBest()
	stub = &settleStub{served: map[string]bool{settleDomain: true}}
	set.dnsSettleFn = stub.check

	set.settleUnverifiedDNS()

	if !reflect.DeepEqual(stub.calls, []string{"p-cover"}) {
		t.Fatalf("in a set run the jointly confirmed cover winner comes first, got %v", stub.calls)
	}
}

func TestSitesSettleOnePresetAtATime(t *testing.T) {
	ds := settleSuite(t, "a.example", "b.example")
	a := ds.candidate("a.example", nil)
	b := ds.candidate("b.example", nil)
	for _, d := range []string{"a.example", "b.example"} {
		ds.put(d, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		ds.found(d, "cached-1-tcpmd5", 1000)
	}
	ds.determineBest()
	stub := &settleStub{served: map[string]bool{"a.example": true}}
	ds.dnsSettleFn = stub.check

	ds.settleUnverifiedDNS()

	if len(stub.calls) != 1 || len(stub.addrs[0]) != 2 {
		t.Fatalf("two sites on one winner share one configuration, got %v %v", stub.calls, stub.addrs)
	}
	if a.IsPoisoned || !b.IsPoisoned || b.Unverified {
		t.Fatalf("each site gets its own verdict: a=%+v b=%+v", a, b)
	}
	if set := groupSetFor(t, ds, "a.example"); !set.DNS.Enabled {
		t.Error("one site of the group still needs the DNS fix, so the group's set keeps it")
	}

	split := settleSuite(t, "a.example", "b.example")
	split.candidate("a.example", nil)
	split.candidate("b.example", nil)
	for d, preset := range map[string]string{"a.example": "cached-1-tcpmd5", "b.example": "cached-2-desync"} {
		split.put(d, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		split.found(d, preset, 1000)
	}
	split.determineBest()
	stub = &settleStub{served: map[string]bool{"a.example": true}}
	split.dnsSettleFn = stub.check

	split.settleUnverifiedDNS()

	if len(stub.calls) != 2 {
		t.Fatalf("two winners, two configurations: %v", stub.calls)
	}
	if set := groupSetFor(t, split, "a.example"); set.DNS.Enabled {
		t.Errorf("the settled site's own set needs no DNS fix: %+v", set.DNS)
	}
	if set := groupSetFor(t, split, "b.example"); !set.DNS.Enabled {
		t.Errorf("the poisoned site's own set keeps it: %+v", set.DNS)
	}
}

func TestASettledCandidateLetsTheSetsOwnStrategyStand(t *testing.T) {
	build := func(t *testing.T, served bool) *SetVerdict {
		ds := setRunFixture(t, settleDomain)
		ds.candidate(settleDomain, nil)
		ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		ds.found(settleDomain, presetSetCurrent, 1000)
		ds.coverWinner = presetSetCurrent
		ds.jointConfirmed = map[string]bool{presetSetCurrent: true}
		ds.domainResults[settleDomain].Results[presetSetCurrent].Confirmed = confirmTries
		ds.domainResults[settleDomain].Results[presetSetCurrent].ConfirmTries = confirmTries
		withFakeConfirm(ds, nil)
		ds.determineBest()
		ds.setStrategy = &config.SetConfig{}
		ds.dnsSettleFn = (&settleStub{served: map[string]bool{settleDomain: served}}).check

		ds.settleUnverifiedDNS()
		ds.resolveSetVerdict()
		return ds.setVerdict
	}

	if v := build(t, true); v == nil || v.Status != SetVerdictCurrentWorks || v.Set.DNS.Enabled || len(v.Set.DNS.Pins) != 0 {
		t.Fatalf("the system resolver serves the site, so the set's strategy works as it is: %+v", v)
	}
	if v := build(t, false); v == nil || v.Status != SetVerdictCovered || !v.Set.DNS.Enabled {
		t.Fatalf("the poisoning is proven, so the set needs the DNS fix it lacks: %+v", v)
	}
}

func TestASetRunWithoutACoverWinnerSettlesWithTheSetsOwnStrategy(t *testing.T) {
	build := func(t *testing.T, ownServes bool) (*SetVerdict, []string) {
		t.Helper()
		ds := setRunFixture(t, settleDomain)
		ds.stopWhenCovered = false
		ds.candidate(settleDomain, nil)
		ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		ds.found(settleDomain, "cached-1-tcpmd5", 1000)
		ds.found(settleDomain, presetSetCurrent, 1000)
		ds.determineBest()
		ds.setStrategy = &config.SetConfig{}
		withFakeConfirm(ds, nil)
		var tried []string
		ds.dnsSettleFn = func(name string, _ *config.SetConfig, _ []DomainInput, _ map[string][]string) (map[string]bool, bool) {
			tried = append(tried, name)
			return map[string]bool{settleDomain: name == "cached-1-tcpmd5" || ownServes}, true
		}

		ds.settleUnverifiedDNS()
		ds.resolveSetVerdict()
		return ds.setVerdict, tried
	}

	v, tried := build(t, false)
	if !reflect.DeepEqual(tried, []string{presetSetCurrent}) {
		t.Fatalf("the verdict tries the set's own strategy first, so the settle tries it too, got %v", tried)
	}
	if v == nil || v.Status != SetVerdictCovered || v.WinnerPreset != presetSetCurrent || !v.Set.DNS.Enabled {
		t.Fatalf("the set's own strategy fails on the system addresses, so the set needs the DNS fix: %+v", v)
	}
	if v, _ := build(t, true); v == nil || v.Status != SetVerdictCurrentWorks || v.Set.DNS.Enabled {
		t.Fatalf("the set's own strategy loads the system addresses, so nothing needs to change: %+v", v)
	}
}

func TestHistoryKeepsTheSettledVerdict(t *testing.T) {
	ds := settleSuite(t, settleDomain)
	ds.candidate(settleDomain, nil)
	ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.found(settleDomain, "cached-1-tcpmd5", 1000)
	ds.determineBest()
	ds.dnsSettleFn = (&settleStub{served: map[string]bool{settleDomain: true}}).check
	ds.settleUnverifiedDNS()
	ds.buildStrategyGroups()
	ds.Status = CheckStatusComplete
	ds.EndTime = time.Now()
	ds.DomainDiscoveryResults = ds.domainResults
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	SaveToHistory(ds.CheckSuite, cfgPath)

	hist := LoadDiscoveryHistory(cfgPath)
	if len(hist.Entries) != 1 {
		t.Fatalf("entries = %d", len(hist.Entries))
	}
	e := hist.Entries[0]
	if e.DNSResult == nil || e.DNSResult.IsPoisoned || !e.DNSResult.SystemServesWithStrategy || e.DNSResult.Unverified {
		t.Errorf("history stores the settled verdict: %+v", e.DNSResult)
	}
	if e.Set == nil || e.Set.DNS.Enabled {
		t.Errorf("history re-applies the set without a DNS fix: %+v", e.Set)
	}
}

func TestTheEndOfARunSettlesTheCandidateBeforeItBuildsTheSets(t *testing.T) {
	previous := suiteRetention
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })

	ds := settleSuite(t, settleDomain)
	r := ds.candidate(settleDomain, nil)
	ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.found(settleDomain, "cached-1-tcpmd5", 1000)
	ds.dnsSettleFn = (&settleStub{served: map[string]bool{settleDomain: true}}).check
	RegisterSuite(ds.CheckSuite)

	ds.finishRun()
	t.Cleanup(func() { ds.ctxCancel() })

	if r.IsPoisoned || r.Unverified || !r.SystemServesWithStrategy {
		t.Fatalf("a finished run settles the candidate: %+v", r)
	}
	if len(ds.StrategyGroups) != 1 || ds.StrategyGroups[0].Set == nil || ds.StrategyGroups[0].Set.DNS.Enabled {
		t.Errorf("the set the run offers carries no DNS fix: %+v", ds.StrategyGroups)
	}
}

func TestSettlingRacesNoStatusRead(t *testing.T) {
	ds := settleSuite(t, settleDomain)
	ds.candidate(settleDomain, nil)
	ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.found(settleDomain, "cached-1-tcpmd5", 1000)
	ds.determineBest()
	ds.DomainDiscoveryResults = ds.domainResults
	ds.dnsSettleFn = (&settleStub{served: map[string]bool{settleDomain: true}}).check

	stop := make(chan struct{})
	first := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			if _, err := json.Marshal(ds.CheckSuite); err != nil {
				t.Error(err)
				return
			}
			if i == 0 {
				close(first)
			}
			select {
			case <-stop:
				return
			default:
			}
		}
	}()
	<-first
	for i := 0; i < 20; i++ {
		ds.CheckSuite.mu.Lock()
		r := ds.candidate(settleDomain, nil)
		ds.CheckSuite.mu.Unlock()
		ds.settleUnverifiedDNS()
		if r.IsPoisoned {
			t.Errorf("round %d: the candidate was not settled", i)
			break
		}
	}
	close(stop)
	<-done
}

func settleServer(t *testing.T) (port string, roots *x509.CertPool) {
	t.Helper()
	page := "<html><body>" + strings.Repeat("x", 2048) + "</body></html>"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			w.WriteHeader(http.StatusNotFound)
		}
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	roots = x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	return port, roots
}

func trustSettleRoots(t *testing.T, pool *x509.CertPool, verify bool) {
	t.Helper()
	orig := dnsSettleRoots
	dnsSettleRoots = func() (*x509.CertPool, bool) { return pool, verify }
	t.Cleanup(func() { dnsSettleRoots = orig })
}

func liveSettleSuite(t *testing.T, domain, checkURL string) *DiscoverySuite {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Queue.IsDiscovery = true
	cfg.System.Checker.ConfigPropagateMs = 0
	cfg.System.Checker.DiscoveryTimeoutSec = 3
	pool := nfq.NewPool(&cfg)
	t.Cleanup(pool.Stop)

	ds := settleSuite(t, domain)
	ds.Domains[0].CheckURL = checkURL
	ds.pool = pool
	ds.cfg = &cfg
	ds.ipVersion = "ipv4"
	return ds
}

func TestSystemAddressesMustLoadThePageWithTheSitesCertificate(t *testing.T) {
	port, roots := settleServer(t)
	set := strategySet("tcp")
	check := func(domain, path string, addrs ...string) bool {
		t.Helper()
		ds := liveSettleSuite(t, domain, "https://"+net.JoinHostPort(domain, port)+path)
		served, completed := ds.checkSystemAddresses("tcp", set, ds.Domains, map[string][]string{domain: addrs})
		if !completed {
			t.Fatalf("%s %v: a run that was not canceled completes its check", domain, addrs)
		}
		return served[domain]
	}

	trustSettleRoots(t, roots, true)
	if !check("example.com", "/", "127.0.0.1") {
		t.Fatal("an address that serves the page with a certificate that chains to a trusted root serves the site")
	}
	if check("example.com", "/", "127.0.0.1", "127.0.0.2") {
		t.Error("every tried address must serve the site, a refused one fails the check")
	}
	if check("example.com", "/gone", "127.0.0.1") {
		t.Error("a 404 is not the site loading")
	}

	trustSettleRoots(t, x509.NewCertPool(), true)
	if check("example.com", "/", "127.0.0.1") {
		t.Error("a certificate that chains to no trusted root proves nothing")
	}

	trustSettleRoots(t, nil, false)
	if !check("example.com", "/", "127.0.0.1") {
		t.Error("without a root store the name on the certificate is the check")
	}
	if check("other.example", "/", "127.0.0.1") {
		t.Error("a certificate for another name is not the site's")
	}
}

func TestTheCertificateThatCountsIsTheFirstAnswers(t *testing.T) {
	finalPort, landing := settleServer(t)
	cert, own := echTestCertificate(t, "example.com")
	first := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://"+net.JoinHostPort("127.0.0.1", finalPort)+"/", http.StatusFound)
	}))
	first.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	first.StartTLS()
	t.Cleanup(first.Close)
	_, port, err := net.SplitHostPort(first.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	check := func() bool {
		t.Helper()
		ds := liveSettleSuite(t, "example.com", "https://"+net.JoinHostPort("example.com", port)+"/")
		served, _ := ds.checkSystemAddresses("tcp", strategySet("tcp"), ds.Domains, map[string][]string{"example.com": {"127.0.0.1"}})
		return served["example.com"]
	}

	trustSettleRoots(t, landing, true)
	if check() {
		t.Error("the pinned address answered with a certificate no root vouches for; the trusted page a redirect lands on, reached without the pin, proves nothing about it")
	}
	trustSettleRoots(t, own, true)
	if !check() {
		t.Error("a valid certificate on the first answer is what counts, wherever its redirect leads")
	}
}

func TestTheSettleCheckRunsInTheShapeItWouldDeploy(t *testing.T) {
	ds := settleSuite(t, settleDomain)
	ds.candidate(settleDomain, func(r *DNSDiscoveryResult) {
		r.AlternativeIPs = append([]string(nil), settleReference...)
		r.referencePinned = true
	})
	cfg := config.NewConfig()
	ds.cfg = &cfg
	stored := strategySet("tcp")
	stored.DNS = config.DNSConfig{Enabled: true, DoHURL: settleDoH, StripECH: true}

	built := ds.settleConfig(stored, []string{settleDomain})

	set := built.Sets[0]
	if set.DNS.Enabled || set.DNS.DoHURL != "" || len(set.DNS.Pins) != 0 || !set.DNS.StripECH {
		t.Errorf("the check runs without the DNS fix and the pins, keeping the ECH strip: %+v", set.DNS)
	}
	if set.TCP.IPBlockDetect != config.DefaultSetConfig.TCP.IPBlockDetect {
		t.Errorf("address-block detection is at its default, as in the set that would ship: %+v", set.TCP.IPBlockDetect)
	}
	if !stored.DNS.Enabled {
		t.Error("the stored set is never changed")
	}
}
