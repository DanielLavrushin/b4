package discovery

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nfq"
)

type stubName struct {
	rcode     byte
	aRcode    byte
	aaaaRcode byte
	a         string
	aaaa      string
	aaaaLag   time.Duration
	aAnswers  int
}

func stubQuestion(msg []byte) (string, uint16, int, bool) {
	if len(msg) < 12 {
		return "", 0, 0, false
	}
	var labels []string
	i := 12
	for i < len(msg) && msg[i] != 0 {
		l := int(msg[i])
		if i+1+l > len(msg) {
			return "", 0, 0, false
		}
		labels = append(labels, string(msg[i+1:i+1+l]))
		i += 1 + l
	}
	if i+5 > len(msg) {
		return "", 0, 0, false
	}
	qtype := uint16(msg[i+1])<<8 | uint16(msg[i+2])
	return strings.ToLower(strings.Join(labels, ".")), qtype, i + 5, true
}

func stubReply(question []byte, entry stubName, known bool, qtype uint16, served int) []byte {
	reply := append([]byte(nil), question...)
	reply[2] = 0x80 | question[2]&0x01
	reply[3] = 0x80
	reply[4], reply[5] = 0, 1
	for i := 6; i < 12; i++ {
		reply[i] = 0
	}
	switch {
	case !known:
		reply[3] |= 3
		return reply
	case entry.rcode != 0:
		reply[3] |= entry.rcode
		return reply
	case qtype == 1 && entry.aRcode != 0:
		reply[3] |= entry.aRcode
		return reply
	case qtype == 28 && entry.aaaaRcode != 0:
		reply[3] |= entry.aaaaRcode
		return reply
	}
	var rdata net.IP
	switch qtype {
	case 1:
		if entry.aAnswers > 0 && served > entry.aAnswers {
			reply[3] |= 3
			return reply
		}
		rdata = net.ParseIP(entry.a).To4()
	case 28:
		if entry.aaaa != "" {
			rdata = net.ParseIP(entry.aaaa).To16()
		}
	}
	if rdata == nil {
		return reply
	}
	reply[7] = 1
	rr := []byte{0xc0, 0x0c, 0, byte(qtype), 0, 1, 0, 0, 0, 60, 0, byte(len(rdata))}
	return append(append(reply, rr...), rdata...)
}

func stubResolver(t *testing.T, names map[string]stubName) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { pc.Close() })

	var mu sync.Mutex
	served := map[string]int{}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			name, qtype, end, ok := stubQuestion(buf[:n])
			if !ok {
				continue
			}
			mu.Lock()
			entry, known := names[name]
			if qtype == 1 {
				served[name]++
			}
			count := served[name]
			mu.Unlock()
			reply := stubReply(buf[:end], entry, known, qtype, count)
			if qtype == 28 && entry.aaaaLag > 0 {
				go func(reply []byte, addr net.Addr, lag time.Duration) {
					time.Sleep(lag)
					pc.WriteTo(reply, addr)
				}(reply, addr, entry.aaaaLag)
				continue
			}
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

func nxdomainResolver(t *testing.T) {
	t.Helper()
	stubResolver(t, nil)
}

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
	nxdomainResolver(t)
	return ds
}

func failedLookup(host string, lookup nameLookup) CheckResult {
	return CheckResult{Status: CheckStatusFailed, Error: "DNS resolution failed", lookup: lookup, lookupHost: host}
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

func TestFetchReportsANameWithoutAddress(t *testing.T) {
	ds := unresolvedSuite(t, false, "typo.example")

	r := ds.fetchForDomain(ds.Domains[0], 2*time.Second)

	if r.Status != CheckStatusFailed || r.lookup != lookupNotFound || r.lookupHost != "typo.example" {
		t.Fatalf("the fetch must fail on the lookup of its own host and say so, got status=%q lookup=%v host=%q error=%q", r.Status, r.lookup, r.lookupHost, r.Error)
	}

	ds.markUnresolved(map[string]CheckResult{"typo.example": r})
	if !ds.unresolved("typo.example") || !ds.nothingLeftToTest() {
		t.Fatal("with the DNS check off, the resolver's answer that the name has no address ends the run")
	}
}

func TestNotFoundBaselineLeavesNothingToTest(t *testing.T) {
	ds := unresolvedSuite(t, true, "typo.example")
	ds.dnsResults["typo.example"] = &DNSDiscoveryResult{NXDomain: true}

	ds.markUnresolved(map[string]CheckResult{"typo.example": failedLookup("typo.example", lookupNotFound)})

	dr := ds.domainResults["typo.example"]
	if !dr.Unresolved || dr.Outcome != OutcomeUnresolved || dr.MissingFamily != "" {
		t.Fatalf("the domain must be marked unresolved, got unresolved=%v outcome=%q family=%q", dr.Unresolved, dr.Outcome, dr.MissingFamily)
	}
	if ds.needsBypass("typo.example") {
		t.Fatal("a name without an address cannot need a strategy, or the run goes on to the extended search")
	}
	if !ds.nothingLeftToTest() {
		t.Fatal("with no domain left to test, the run must stop after the baseline")
	}
}

func TestLookupFailureCountsOnlyWhenDNSFoundNothing(t *testing.T) {
	ds := unresolvedSuite(t, true, "typo.example", "slow.example")
	stubResolver(t, map[string]stubName{"typo.example": {rcode: 2}, "slow.example": {rcode: 2}})
	ds.dnsResults["typo.example"] = &DNSDiscoveryResult{}
	ds.dnsResults["slow.example"] = &DNSDiscoveryResult{ExpectedIPs: []string{"203.0.113.7"}}

	ds.markUnresolved(map[string]CheckResult{
		"typo.example": failedLookup("typo.example", lookupFailed),
		"slow.example": failedLookup("slow.example", lookupFailed),
	})

	if !ds.unresolved("typo.example") {
		t.Fatal("no resolver had an address and the probe's own lookups failed too, the name does not resolve")
	}
	if ds.unresolved("slow.example") {
		t.Fatal("the DNS check found an address, a failed lookup does not make the name unresolved")
	}
}

func TestASystemNXDomainDoesNotOverruleTheDNSCheck(t *testing.T) {
	ds := unresolvedSuite(t, true, "censored.example")
	ds.dnsResults["censored.example"] = &DNSDiscoveryResult{IsPoisoned: true, ExpectedIPs: []string{"203.0.113.7"}}

	ds.markUnresolved(map[string]CheckResult{"censored.example": failedLookup("censored.example", lookupNotFound)})

	if ds.unresolved("censored.example") {
		t.Fatal("DNS over HTTPS has an address for the name, an NXDOMAIN from the system resolver is tampering, not a missing name")
	}
}

func TestWithoutTheDNSCheckOnlyAnAnswerCounts(t *testing.T) {
	ds := unresolvedSuite(t, false, "typo.example", "slow.example")
	stubResolver(t, map[string]stubName{"slow.example": {rcode: 2}})

	ds.markUnresolved(map[string]CheckResult{
		"typo.example": failedLookup("typo.example", lookupNotFound),
		"slow.example": failedLookup("slow.example", lookupFailed),
	})

	if !ds.unresolved("typo.example") {
		t.Fatal("with the DNS check off, every probe asks the same resolver, and it answered that the name has no address")
	}
	if ds.unresolved("slow.example") {
		t.Fatal("a failure of the only resolver asked is not an answer, the search goes on")
	}
}

func TestAnAddressFoundOnSecondLookKeepsTheName(t *testing.T) {
	ds := unresolvedSuite(t, false, "flaky.example")
	stubResolver(t, map[string]stubName{"flaky.example": {a: "203.0.113.7"}})

	ds.markUnresolved(map[string]CheckResult{"flaky.example": failedLookup("flaky.example", lookupNotFound)})

	if ds.unresolved("flaky.example") {
		t.Fatal("the name resolves when asked again, one failed lookup does not make it unresolved")
	}
}

func TestAnIPv6RunNamesTheMissingFamily(t *testing.T) {
	for _, dnsChecked := range []bool{false, true} {
		ds := unresolvedSuite(t, dnsChecked, "v4only.example")
		stubResolver(t, map[string]stubName{"v4only.example": {a: "203.0.113.7"}})
		ds.ipVersion = "ipv6"
		if dnsChecked {
			ds.dnsResults["v4only.example"] = &DNSDiscoveryResult{}
		}

		r := ds.fetchForDomain(ds.Domains[0], 2*time.Second)
		ds.markUnresolved(map[string]CheckResult{"v4only.example": r})

		dr := ds.domainResults["v4only.example"]
		if !dr.Unresolved || dr.MissingFamily != "ipv6" {
			t.Fatalf("dnsChecked=%v: a name with IPv4 addresses only has nothing to test over IPv6, got unresolved=%v family=%q lookup=%v error=%q", dnsChecked, dr.Unresolved, dr.MissingFamily, r.lookup, r.Error)
		}
		if reason := unresolvedReason(dr.MissingFamily, ds.dnsResults["v4only.example"]); !strings.Contains(reason, "IPv4 addresses only") {
			t.Fatalf("dnsChecked=%v: the reason must name the family the name has, got %q", dnsChecked, reason)
		}
		if !ds.nothingLeftToTest() {
			t.Fatalf("dnsChecked=%v: nothing can be tested over IPv6, the run must stop", dnsChecked)
		}
	}
}

func TestAServfailMergedWithNodataIsNotAMissingName(t *testing.T) {
	ds := unresolvedSuite(t, false, "v4only.example")
	stubResolver(t, map[string]stubName{"v4only.example": {aRcode: 2, aaaaLag: 150 * time.Millisecond}})

	ds.markUnresolved(map[string]CheckResult{"v4only.example": failedLookup("v4only.example", lookupNotFound)})

	if ds.unresolved("v4only.example") {
		t.Fatal("an A query that fails is a resolver failure, not a missing name, even when the AAAA answer is empty")
	}
}

func TestAnIPv4RunNamesTheMissingFamily(t *testing.T) {
	ds := unresolvedSuite(t, false, "v6only.example")
	stubResolver(t, map[string]stubName{"v6only.example": {aaaa: "2001:db8::7"}})
	ds.ipVersion = "ipv4"

	r := ds.fetchForDomain(ds.Domains[0], 2*time.Second)
	ds.markUnresolved(map[string]CheckResult{"v6only.example": r})

	if dr := ds.domainResults["v6only.example"]; !dr.Unresolved || dr.MissingFamily != "ipv4" {
		t.Fatalf("a name with IPv6 addresses only has nothing to test over IPv4, got unresolved=%v family=%q lookup=%v error=%q", dr.Unresolved, dr.MissingFamily, r.lookup, r.Error)
	}

	flaky := unresolvedSuite(t, false, "flaky.example")
	stubResolver(t, map[string]stubName{"flaky.example": {a: "203.0.113.7"}})
	flaky.ipVersion = "ipv4"
	flaky.markUnresolved(map[string]CheckResult{"flaky.example": failedLookup("flaky.example", lookupNotFound)})
	if flaky.unresolved("flaky.example") {
		t.Fatal("the probed family resolves when asked again, the name is not missing anything")
	}
}

func TestAFailedProbedFamilyIsNotAMissingFamily(t *testing.T) {
	for _, tc := range []struct {
		ipVersion string
		entry     stubName
	}{
		{"ipv4", stubName{aRcode: 2, aaaa: "2001:db8::7"}},
		{"ipv6", stubName{aaaaRcode: 2, a: "203.0.113.7"}},
	} {
		ds := unresolvedSuite(t, false, "dual.example")
		stubResolver(t, map[string]stubName{"dual.example": tc.entry})
		ds.ipVersion = tc.ipVersion

		r := ds.fetchForDomain(ds.Domains[0], 2*time.Second)
		ds.markUnresolved(map[string]CheckResult{"dual.example": r})

		if dr := ds.domainResults["dual.example"]; dr.Unresolved {
			t.Fatalf("%s: the probed family failed instead of answering, which proves no missing family, got family=%q lookup=%v error=%q", tc.ipVersion, dr.MissingFamily, r.lookup, r.Error)
		}
	}
}

func TestNXDomainStillEndsAnIPv6Run(t *testing.T) {
	ds := unresolvedSuite(t, false, "typo.example")
	ds.ipVersion = "ipv6"

	r := ds.fetchForDomain(ds.Domains[0], 2*time.Second)
	ds.markUnresolved(map[string]CheckResult{"typo.example": r})

	if dr := ds.domainResults["typo.example"]; !dr.Unresolved || dr.MissingFamily != "" || !ds.nothingLeftToTest() {
		t.Fatalf("a name that exists in neither family ends the run without a family note, got unresolved=%v family=%q", dr.Unresolved, dr.MissingFamily)
	}
}

func TestInternationalizedNameIsNotReportedAsNXDomain(t *testing.T) {
	p := &DNSProber{domain: "пример.test", ref: referenceAnswer{source: "https://dns.example/resolve", trusted: true, nxdomain: true}}
	r := &DNSDiscoveryResult{}

	p.noteMissingAddress(r)

	if r.NXDomain {
		t.Fatal("DNS over HTTPS was asked for the name as typed, not for its ASCII form, so its NXDOMAIN says nothing")
	}
}

func TestInternationalizedNameNeedsAnAnswerForItsASCIIForm(t *testing.T) {
	ds := unresolvedSuite(t, true, "пример.test")
	stubResolver(t, map[string]stubName{"xn--e1afmkfd.test": {rcode: 2}})
	ds.dnsResults["пример.test"] = &DNSDiscoveryResult{}

	ds.markUnresolved(map[string]CheckResult{"пример.test": failedLookup("xn--e1afmkfd.test", lookupFailed)})
	if ds.unresolved("пример.test") {
		t.Fatal("the DNS check could not query an internationalized name, so its empty result and a resolver failure prove nothing")
	}

	missing := unresolvedSuite(t, true, "пример.test")
	missing.dnsResults["пример.test"] = &DNSDiscoveryResult{}
	missing.markUnresolved(map[string]CheckResult{"пример.test": failedLookup("xn--e1afmkfd.test", lookupNotFound)})
	if !missing.unresolved("пример.test") {
		t.Fatal("the ASCII form has no address in either family, the name does not resolve")
	}
}

func TestFetchOfAnInternationalizedNameLooksUpItsASCIIForm(t *testing.T) {
	ds := unresolvedSuite(t, false, "пример.test")

	r := ds.fetchForDomain(ds.Domains[0], 2*time.Second)

	if r.lookup != lookupNotFound || r.lookupHost != "xn--e1afmkfd.test" {
		t.Fatalf("the probe dials the ASCII form, the lookup must be recorded for it, got lookup=%v host=%q error=%q", r.lookup, r.lookupHost, r.Error)
	}
}

func TestResolvingDomainsKeepTheSearchGoing(t *testing.T) {
	ds := unresolvedSuite(t, true, "typo.example", "blocked.example")
	ds.dnsResults["typo.example"] = &DNSDiscoveryResult{NXDomain: true}
	ds.dnsResults["blocked.example"] = &DNSDiscoveryResult{ExpectedIPs: []string{"203.0.113.7"}}

	ds.markUnresolved(map[string]CheckResult{
		"typo.example":    failedLookup("typo.example", lookupNotFound),
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
		"typo.example":     failedLookup("typo.example", lookupNotFound),
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

	ds.markUnresolved(map[string]CheckResult{"typo.example": failedLookup("typo.example", lookupNotFound)})

	if !ds.allDomainsTransportBlocked() {
		t.Fatal("the only domain that resolves is IP-blocked, the extended search must be skipped")
	}

	only := unresolvedSuite(t, true, "typo.example")
	only.dnsResults["typo.example"] = &DNSDiscoveryResult{}
	only.markUnresolved(map[string]CheckResult{"typo.example": failedLookup("typo.example", lookupNotFound)})
	if only.allDomainsTransportBlocked() {
		t.Fatal("a name without an address says nothing about an IP block")
	}
}

func TestCanceledBaselineMarksNothing(t *testing.T) {
	ds := unresolvedSuite(t, true, "typo.example")
	ds.dnsResults["typo.example"] = &DNSDiscoveryResult{}
	close(ds.cancel)

	ds.markUnresolved(map[string]CheckResult{"typo.example": failedLookup("typo.example", lookupNotFound)})

	if ds.unresolved("typo.example") || ds.nothingLeftToTest() {
		t.Fatal("a canceled run keeps its normal ending, the lookup may have been cut short")
	}
}

func TestStopDuringTheBaselineKeepsTheVerdict(t *testing.T) {
	ds := unresolvedSuite(t, true, "typo.example")
	ds.dnsResults["typo.example"] = &DNSDiscoveryResult{}
	close(ds.finish)

	ds.markUnresolved(map[string]CheckResult{"typo.example": failedLookup("typo.example", lookupNotFound)})

	if !ds.unresolved("typo.example") {
		t.Fatal("a stop keeps what was found, and the baseline already showed that the name has no address")
	}
}

func TestBaselineThatLoadedIsNeverUnresolved(t *testing.T) {
	ds := unresolvedSuite(t, false, "open.example")

	ds.markUnresolved(map[string]CheckResult{"open.example": {Status: CheckStatusComplete, lookup: lookupNotFound, lookupHost: "open.example"}})

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
	nxRef := referenceAnswer{source: "https://dns.example/dns-query", trusted: true, nxdomain: true}
	p := &DNSProber{domain: "typo.example", ref: nxRef}

	nx := &DNSDiscoveryResult{}
	p.noteMissingAddress(nx)
	if !nx.NXDomain || shouldScanAlternatives(nx) {
		t.Fatalf("other regions cannot answer a name DNS over HTTPS says does not exist, got %+v", nx)
	}

	p.ref = referenceAnswer{}
	silent := &DNSDiscoveryResult{}
	p.noteMissingAddress(silent)
	if silent.NXDomain || !shouldScanAlternatives(silent) {
		t.Fatalf("without an answer the region scan may still find an address, got %+v", silent)
	}

	p.ref = nxRef
	known := &DNSDiscoveryResult{ExpectedIPs: []string{"203.0.113.7"}}
	p.noteMissingAddress(known)
	if known.NXDomain {
		t.Fatal("a name with an address from any resolver exists")
	}
}

func TestANameWithoutAnAddressIsNotATypo(t *testing.T) {
	p := &DNSProber{domain: "ntc.example", ipVersion: "ipv4", ref: referenceAnswer{source: "https://dns.example/dns-query", trusted: true, nodata: true}}
	r := &DNSDiscoveryResult{}

	p.noteMissingAddress(r)

	if r.NXDomain || r.NoAddressFamily != "ipv4" {
		t.Fatalf("NOERROR without an address means the name exists with no IPv4 address, got nxdomain=%v family=%q", r.NXDomain, r.NoAddressFamily)
	}
	if !shouldScanAlternatives(r) {
		t.Fatal("another region's DNS may still publish an address, the scan stays on")
	}
	reason, advice := unresolvedReason("", r), unresolvedAdvice("", r)
	if !strings.Contains(reason, "exists") || strings.Contains(advice, "spelling") || !strings.Contains(advice, "pin it") {
		t.Fatalf("the verdict must say the name exists and point to a pin, got %q / %q", reason, advice)
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
		family string
		dns    *DNSDiscoveryResult
		want   string
	}{
		{"", nil, "DNS check is off"},
		{"", &DNSDiscoveryResult{NXDomain: true}, "NXDOMAIN"},
		{"", &DNSDiscoveryResult{}, "no resolver returned an address"},
		{"ipv6", &DNSDiscoveryResult{}, "IPv4 addresses only, and this run probes over IPv6"},
		{"ipv4", nil, "IPv6 addresses only, and this run probes over IPv4"},
	} {
		if got := unresolvedReason(tc.family, tc.dns); !strings.Contains(got, tc.want) {
			t.Errorf("unresolvedReason(%q, %+v) = %q, want it to mention %q", tc.family, tc.dns, got, tc.want)
		}
	}
}

func presetRunSuite(t *testing.T, tries int, domains ...string) *DiscoverySuite {
	t.Helper()
	ds := unresolvedSuite(t, false, domains...)
	cfg := config.NewConfig()
	cfg.Queue.IsDiscovery = true
	cfg.System.Checker.ConfigPropagateMs = 0
	cfg.System.Checker.DiscoveryTimeoutSec = 3
	ds.cfg = &cfg
	ds.pool = &nfq.Pool{}
	ds.validationTries = tries
	ds.ipVersion = "ipv4"

	orig := probeRefusesAddr
	probeRefusesAddr = nil
	t.Cleanup(func() { probeRefusesAddr = orig })
	return ds
}

func pageOn(t *testing.T, ds *DiscoverySuite, domain string) {
	t.Helper()
	page := "<html><body>" + strings.Repeat("x", 2048) + "</body></html>"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(page))
	}))
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	for i := range ds.Domains {
		if ds.Domains[i].Domain == domain {
			ds.Domains[i].CheckURL = "https://" + domain + ":" + port + "/"
		}
	}
	ds.Domain, ds.CheckURL = ds.Domains[0].Domain, ds.Domains[0].CheckURL
}

func TestATryThatLoadedTheSiteKeepsTheName(t *testing.T) {
	ds := presetRunSuite(t, 2, "flaky.example")
	stubResolver(t, map[string]stubName{"flaky.example": {a: "127.0.0.1", aAnswers: 1}})
	pageOn(t, ds, "flaky.example")

	results := ds.testPresetAllDomains(ConfigPreset{Name: presetNoBypass})
	r := results["flaky.example"]
	if r.Status != CheckStatusFailed {
		t.Fatalf("the second try must fail on the lookup, got %+v", r)
	}

	ds.markUnresolved(results)
	if ds.unresolved("flaky.example") {
		t.Fatalf("the first try loaded the page, so the name resolves whatever the next lookup says: %q", r.Error)
	}
}

func TestSkippedFetchesAreNotCountedAsTested(t *testing.T) {
	ds := presetRunSuite(t, 1, "open.example", "typo.example")
	stubResolver(t, map[string]stubName{"open.example": {a: "127.0.0.1"}})
	pageOn(t, ds, "open.example")
	ds.domainResults["typo.example"].Unresolved = true
	ds.TotalChecks = 2

	results := ds.testPresetAllDomains(ConfigPreset{Name: presetNoBypass})

	if !results["typo.example"].untried || results["open.example"].Status != CheckStatusComplete {
		t.Fatalf("only open.example is fetched, got %+v", results)
	}
	if ds.CompletedChecks != 2 || ds.TotalChecks != 2 || ds.SkippedChecks != 1 {
		t.Fatalf("a skipped fetch moves the progress but is not a tested configuration, got completed=%d total=%d skipped=%d", ds.CompletedChecks, ds.TotalChecks, ds.SkippedChecks)
	}
}

func TestRunStopsAfterTheBaselineWhenTheNameDoesNotResolve(t *testing.T) {
	previous := suiteRetention
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })
	nxdomainResolver(t)

	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Queue.IsDiscovery = true
	cfg.System.Checker.ConfigPropagateMs = 0
	cfg.System.Checker.DiscoveryTimeoutSec = 2
	pool := nfq.NewPool(&cfg)
	t.Cleanup(pool.Stop)

	ds := NewDiscoverySuite([]string{"typo.example"}, pool, true, true, nil, 1, "", "", 0)
	RegisterSuite(ds.CheckSuite)
	ds.RunDiscovery()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := GetCheckSuite(ds.Id); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the finished run is never dropped from the active suites")
		}
		time.Sleep(10 * time.Millisecond)
	}

	dr := ds.DomainDiscoveryResults["typo.example"]
	if dr == nil {
		t.Fatal("the finished run published no result for typo.example")
	}
	if !dr.Unresolved || dr.Outcome != OutcomeUnresolved {
		t.Fatalf("typo.example ended with unresolved=%v outcome=%q, want unresolved", dr.Unresolved, dr.Outcome)
	}
	if ds.CompletedChecks != 1 || ds.Status != CheckStatusComplete {
		t.Fatalf("%d checks ran with status %q, the run must end after the baseline", ds.CompletedChecks, ds.Status)
	}
}
