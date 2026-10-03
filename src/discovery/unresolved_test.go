package discovery

import (
	"context"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
)

func unresolvedSuite(t *testing.T, dnsChecked bool, domains ...string) *DiscoverySuite {
	t.Helper()
	results := map[string]map[string]*DomainPresetResult{}
	for _, d := range domains {
		results[d] = map[string]*DomainPresetResult{}
	}
	ds := suiteWithResults(results)
	ds.dnsResults = map[string]*DNSDiscoveryResult{}
	for _, d := range domains {
		ds.Domains = append(ds.Domains, DomainInput{Domain: d, CheckURL: "https://" + d + "/"})
	}
	ds.Domain, ds.CheckURL = ds.Domains[0].Domain, ds.Domains[0].CheckURL
	ds.skipDNS = !dnsChecked
	ds.initCancelContext()
	t.Cleanup(ds.ctxCancel)
	return ds
}

func failedLookup(lookup nameLookup) CheckResult {
	return CheckResult{Status: CheckStatusFailed, Error: "DNS resolution failed", lookup: lookup}
}

func dialError(err error) error {
	return &url.Error{Op: "Get", URL: "https://typo.example/", Err: &net.OpError{Op: "dial", Net: "tcp4", Err: err}}
}

func TestLookupFailureOf(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want nameLookup
	}{
		{"no such host", dialError(&net.DNSError{Err: "no such host", Name: "typo.example", IsNotFound: true}), lookupNotFound},
		{"resolver timeout", dialError(&net.DNSError{Err: "i/o timeout", Name: "typo.example", IsTimeout: true}), lookupFailed},
		{"server failure", dialError(&net.DNSError{Err: "server misbehaving", Name: "typo.example", IsTemporary: true}), lookupFailed},
		{"canceled run", dialError(&net.DNSError{Err: "operation was canceled", Name: "typo.example", UnwrapErr: context.Canceled}), lookupOK},
		{"connection reset", dialError(syscall.ECONNRESET), lookupOK},
	}
	for _, tc := range cases {
		if got := lookupFailureOf(tc.err); got != tc.want {
			t.Errorf("%s: lookupFailureOf = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func nxdomainResolver(t *testing.T) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 12 {
				continue
			}
			reply := append([]byte(nil), buf[:n]...)
			reply[2] |= 0x80
			reply[3] = 0x80 | 0x03
			pc.WriteTo(reply, addr)
		}
	}()

	orig := probeResolver
	probeResolver = func(int, time.Duration) *net.Resolver {
		return &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "udp", pc.LocalAddr().String())
			},
		}
	}
	t.Cleanup(func() { probeResolver = orig })
}

func TestFetchReportsANameWithoutAddress(t *testing.T) {
	nxdomainResolver(t)
	ds := unresolvedSuite(t, false, "typo.example")

	r := ds.fetchForDomain(ds.Domains[0], 2*time.Second)

	if r.Status != CheckStatusFailed || r.lookup != lookupNotFound {
		t.Fatalf("the fetch must fail on the lookup and say so, got status=%q lookup=%v error=%q", r.Status, r.lookup, r.Error)
	}

	ds.markUnresolved(map[string]CheckResult{"typo.example": r})
	if !ds.unresolved("typo.example") || !ds.nothingLeftToTest() {
		t.Fatal("with the DNS check off, the resolver's answer that the name has no address ends the run")
	}
}

func TestNXDomainEndsTheRunAfterTheBaseline(t *testing.T) {
	ds := unresolvedSuite(t, true, "typo.example")
	ds.dnsResults["typo.example"] = &DNSDiscoveryResult{NXDomain: true}

	ds.markUnresolved(map[string]CheckResult{"typo.example": failedLookup(lookupNotFound)})

	dr := ds.domainResults["typo.example"]
	if !dr.Unresolved || dr.Outcome != OutcomeUnresolved {
		t.Fatalf("the domain must be marked unresolved, got unresolved=%v outcome=%q", dr.Unresolved, dr.Outcome)
	}
	if ds.needsBypass("typo.example") {
		t.Fatal("a name without an address cannot need a strategy, or the run goes on to the extended search")
	}
	if !ds.nothingLeftToTest() {
		t.Fatal("with no domain left to test, the run must stop after the baseline")
	}
}

func TestLookupTimeoutCountsOnlyWhenDNSFoundNothing(t *testing.T) {
	ds := unresolvedSuite(t, true, "typo.example", "slow.example")
	ds.dnsResults["typo.example"] = &DNSDiscoveryResult{}
	ds.dnsResults["slow.example"] = &DNSDiscoveryResult{ExpectedIPs: []string{"203.0.113.7"}}

	ds.markUnresolved(map[string]CheckResult{
		"typo.example": failedLookup(lookupFailed),
		"slow.example": failedLookup(lookupFailed),
	})

	if !ds.unresolved("typo.example") {
		t.Fatal("no resolver had an address and the probe's own lookup failed too, the name does not resolve")
	}
	if ds.unresolved("slow.example") {
		t.Fatal("the DNS check found an address, one failed lookup does not make the name unresolved")
	}
}

func TestWithoutTheDNSCheckOnlyNoSuchHostCounts(t *testing.T) {
	ds := unresolvedSuite(t, false, "typo.example", "slow.example")

	ds.markUnresolved(map[string]CheckResult{
		"typo.example": failedLookup(lookupNotFound),
		"slow.example": failedLookup(lookupFailed),
	})

	if !ds.unresolved("typo.example") {
		t.Fatal("with the DNS check off, every probe asks the same resolver, and it answered that the name has no address")
	}
	if ds.unresolved("slow.example") {
		t.Fatal("a timeout from the only resolver asked is not an answer, the search goes on")
	}
}

func TestResolvingDomainsKeepTheSearchGoing(t *testing.T) {
	ds := unresolvedSuite(t, true, "typo.example", "blocked.example")
	ds.dnsResults["typo.example"] = &DNSDiscoveryResult{NXDomain: true}
	ds.dnsResults["blocked.example"] = &DNSDiscoveryResult{ExpectedIPs: []string{"203.0.113.7"}}

	ds.markUnresolved(map[string]CheckResult{
		"typo.example":    failedLookup(lookupNotFound),
		"blocked.example": {Status: CheckStatusFailed, Error: "TCP RST during handshake (active reset)"},
	})

	if ds.nothingLeftToTest() {
		t.Fatal("blocked.example resolves and still needs a strategy")
	}
	if ds.Domain != "blocked.example" || ds.CheckURL != "https://blocked.example/" {
		t.Fatalf("the single-domain steps must move to a domain that resolves, primary is %q %q", ds.Domain, ds.CheckURL)
	}
	if !ds.needsBypass("blocked.example") || ds.needsBypass("typo.example") {
		t.Fatal("only the domain that resolves needs a strategy")
	}

	r := ds.fetchForDomain(ds.Domains[0], time.Second)
	if r.Status != CheckStatusFailed || !r.untried {
		t.Fatalf("the unresolved domain must not be fetched again, got %+v", r)
	}

	ds.recordResultsMulti(ConfigPreset{Name: "combo-random"}, map[string]CheckResult{
		"typo.example":    r,
		"blocked.example": {Status: CheckStatusFailed, Error: "TCP RST during handshake (active reset)"},
	})
	if _, stored := ds.domainResults["typo.example"].Results["combo-random"]; stored {
		t.Fatal("a fetch that was never made must not be stored as a tested configuration")
	}
	if _, stored := ds.domainResults["blocked.example"].Results["combo-random"]; !stored {
		t.Fatal("the domain that resolves keeps its result")
	}
}

func TestUnresolvedAndInterceptedDomainsLeaveNothingToTest(t *testing.T) {
	ds := unresolvedSuite(t, true, "typo.example", "www.whatsapp.com")
	ds.dnsResults["typo.example"] = &DNSDiscoveryResult{}
	ds.dnsResults["www.whatsapp.com"] = &DNSDiscoveryResult{GatewayIPs: []string{"31.13.72.60"}}

	ds.markUnresolved(map[string]CheckResult{
		"typo.example":     failedLookup(lookupNotFound),
		"www.whatsapp.com": {Status: CheckStatusFailed, Error: "TCP to every known address is answered by the first hop, not tried"},
	})

	if !ds.nothingLeftToTest() {
		t.Fatal("one domain does not resolve and the other is answered by the gateway, there is nothing left to search")
	}
}

func TestTransportBlockVerdictIgnoresUnresolvedDomains(t *testing.T) {
	ds := unresolvedSuite(t, true, "typo.example", "blocked.example")
	ds.dnsResults["typo.example"] = &DNSDiscoveryResult{}
	ds.dnsResults["blocked.example"] = &DNSDiscoveryResult{TransportBlocked: true, ExpectedIPs: []string{"203.0.113.7"}}

	ds.markUnresolved(map[string]CheckResult{"typo.example": failedLookup(lookupNotFound)})

	if !ds.allDomainsTransportBlocked() {
		t.Fatal("the only domain that resolves is IP-blocked, the extended search must be skipped")
	}

	only := unresolvedSuite(t, true, "typo.example")
	only.dnsResults["typo.example"] = &DNSDiscoveryResult{}
	only.markUnresolved(map[string]CheckResult{"typo.example": failedLookup(lookupNotFound)})
	if only.allDomainsTransportBlocked() {
		t.Fatal("a name without an address says nothing about an IP block")
	}
}

func TestInterruptedBaselineMarksNothing(t *testing.T) {
	ds := unresolvedSuite(t, true, "typo.example")
	ds.dnsResults["typo.example"] = &DNSDiscoveryResult{}
	close(ds.cancel)

	ds.markUnresolved(map[string]CheckResult{"typo.example": failedLookup(lookupNotFound)})

	if ds.unresolved("typo.example") || ds.nothingLeftToTest() {
		t.Fatal("a canceled run keeps its normal ending, the lookup may have been cut short")
	}
}

func TestBaselineThatLoadedIsNeverUnresolved(t *testing.T) {
	ds := unresolvedSuite(t, false, "open.example")

	ds.markUnresolved(map[string]CheckResult{"open.example": {Status: CheckStatusComplete, lookup: lookupNotFound}})

	if ds.unresolved("open.example") {
		t.Fatal("a fetch that completed resolved the name")
	}
}

func TestUnresolvedOutcomeIsFinal(t *testing.T) {
	dr := &DomainDiscoveryResult{Domain: "typo.example", Unresolved: true, Results: map[string]*DomainPresetResult{}}
	for _, finished := range []bool{false, true} {
		dr.refreshOutcome(finished)
		if dr.Outcome != OutcomeUnresolved || dr.Unconfirmed {
			t.Fatalf("finished=%v: outcome=%q unconfirmed=%v, want %q", finished, dr.Outcome, dr.Unconfirmed, OutcomeUnresolved)
		}
	}
}

func TestEncryptedNXDomainSkipsTheRegionScan(t *testing.T) {
	p := &DNSProber{domain: "typo.example"}

	nx := &DNSDiscoveryResult{}
	p.noteMissingAddress(nx, "https://dns.example/dns-query")
	if !nx.NXDomain || shouldScanAlternatives(nx) {
		t.Fatalf("other regions cannot answer a name DNS over HTTPS says does not exist, got %+v", nx)
	}

	silent := &DNSDiscoveryResult{}
	p.noteMissingAddress(silent, "")
	if silent.NXDomain || !shouldScanAlternatives(silent) {
		t.Fatalf("without an answer the region scan may still find an address, got %+v", silent)
	}

	known := &DNSDiscoveryResult{ExpectedIPs: []string{"203.0.113.7"}}
	p.noteMissingAddress(known, "https://dns.example/dns-query")
	if known.NXDomain {
		t.Fatal("a name with an address from any resolver exists")
	}
}

func TestEvaluateWithoutAnyAddress(t *testing.T) {
	p, probed := gatewayProber(t, map[string]bool{}, map[string]bool{})

	r := p.evaluate(context.Background(), nil, nil, false)

	if !r.noAddress() || r.IsPoisoned || r.TransportBlocked {
		t.Fatalf("no resolver had an address, nothing else can be concluded: %+v", r)
	}
	if len(*probed) != 0 {
		t.Fatalf("there is no address to probe, probed %v", *probed)
	}
}

func TestUnresolvedReasonNamesTheEvidence(t *testing.T) {
	for _, tc := range []struct {
		dns  *DNSDiscoveryResult
		want string
	}{
		{nil, "DNS check is off"},
		{&DNSDiscoveryResult{NXDomain: true}, "NXDOMAIN"},
		{&DNSDiscoveryResult{}, "no resolver returned an address"},
	} {
		if got := unresolvedReason(tc.dns); !strings.Contains(got, tc.want) {
			t.Errorf("unresolvedReason(%+v) = %q, want it to mention %q", tc.dns, got, tc.want)
		}
	}
}
