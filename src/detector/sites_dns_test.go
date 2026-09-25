package detector

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/netprobe"
	"golang.org/x/net/dns/dnsmessage"
)

func TestDNSErrorKind(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, ""},
		{&net.DNSError{Err: "i/o timeout", IsTimeout: true}, dnsErrTimeout},
		{context.DeadlineExceeded, dnsErrTimeout},
		{fmt.Errorf("lookup: %w", context.DeadlineExceeded), dnsErrTimeout},
		{&net.DNSError{Err: "no such host", IsNotFound: true}, dnsErrNotFound},
		{errNoAddress, dnsErrNotFound},
		{&net.DNSError{Err: "server misbehaving", IsTemporary: true}, dnsErrServerFailure},
		{&net.DNSError{Err: "server misbehaving"}, dnsErrServerFailure},
		{&net.DNSError{Err: "lame referral"}, dnsErrServerFailure},
		{&net.DNSError{Err: "operation was canceled", UnwrapErr: context.Canceled}, dnsErrOther},
		{errors.New("connection refused"), dnsErrOther},
	} {
		if got := dnsErrorKind(tc.err); got != tc.want {
			t.Errorf("dnsErrorKind(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestOutcomeForDNSFail(t *testing.T) {
	fail := &Fetch{Status: FetchDNSFail}
	if isBlockedStatus(FetchDNSFail) {
		t.Fatal("DNS_FAIL is not a blocked status")
	}
	for _, through := range []*Fetch{nil, {Status: FetchOk}, {Status: FetchDNSFail}} {
		if got := outcomeFor(fail, through); got != OutcomeDNS {
			t.Fatalf("direct DNS_FAIL with through %+v = %s, want dns", through, got)
		}
	}
	if got := outcomeFor(fail, &Fetch{Status: netprobe.DomainTLSDrop}); got != OutcomeBrokenByB4 {
		t.Fatalf("loads at the DoH address, dropped at the set's address = %s, want broken_by_b4", got)
	}
	if got := outcomeFor(&Fetch{Status: netprobe.DomainTLSDrop}, fail); got != OutcomeStillBlocked {
		t.Fatalf("dropped at the DoH address, no address through b4 = %s, want still_blocked", got)
	}
}

func TestDNSFailIsCountedApartFromBlocks(t *testing.T) {
	direct := &Fetch{Status: FetchDNSFail, IP: "87.249.137.3"}
	through := &Fetch{Status: FetchDNSFail}
	ok := &Fetch{Status: FetchOk}
	result := &SitesResult{Sites: []SiteResult{
		{Input: "www.cdn77.com", Family: "ipv4", Direct: direct, ThroughB4: through, Outcome: outcomeFor(direct, through), Done: true},
		{Input: "www.cdn77.com", Family: "ipv6", Direct: direct, ThroughB4: through, Outcome: outcomeFor(direct, through), Done: true},
		{Input: "example.com", Family: "ipv4", Direct: ok, ThroughB4: ok, Outcome: outcomeFor(ok, ok), Done: true},
	}}
	s := &Suite{ctx: context.Background(), Sites: result}
	s.tallySites(result)
	if result.DNSFail != 2 || result.Errors != 0 || result.Blocked != 0 || result.Ok != 1 {
		t.Fatalf("tally = %+v, want 2 dns_fail, 1 ok, no errors or blocks", result)
	}

	s.refreshVerdict()
	v := s.Verdict
	if v.DNSFail != 2 || len(v.DNSFailSites) != 1 || v.DNSFailSites[0] != "www.cdn77.com" {
		t.Fatalf("verdict dns_fail %d %v, want 2 rows of one site", v.DNSFail, v.DNSFailSites)
	}
	if v.BlockedByISP != 0 || v.StillBlocked != 0 || len(v.StillBlockedAt) != 0 || len(v.BlockKinds) != 0 {
		t.Fatalf("a site this host's resolver cannot name is not blocked by the ISP: %+v", v)
	}
}

type testResolver struct {
	conn      net.PacketConn
	recovered atomic.Bool
	mu        sync.Mutex
	queries   map[string]int
}

func (r *testResolver) asked(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.queries[name+"."]
}

func startTestResolver(t *testing.T) *testResolver {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &testResolver{conn: conn, queries: make(map[string]int)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if reply := r.reply(buf[:n]); reply != nil {
				conn.WriteTo(reply, from)
			}
		}
	}()
	t.Cleanup(func() {
		conn.Close()
		<-done
	})

	orig := systemResolver
	addr := conn.LocalAddr().String()
	systemResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "udp", addr)
	}}
	t.Cleanup(func() { systemResolver = orig })
	return r
}

func (r *testResolver) reply(query []byte) []byte {
	var p dnsmessage.Parser
	h, err := p.Start(query)
	if err != nil {
		return nil
	}
	q, err := p.Question()
	if err != nil {
		return nil
	}
	r.mu.Lock()
	r.queries[q.Name.String()]++
	r.mu.Unlock()
	label, _, _ := strings.Cut(q.Name.String(), ".")
	rh := dnsmessage.Header{ID: h.ID, Response: true, RecursionDesired: h.RecursionDesired, RecursionAvailable: true}
	var answer net.IP
	switch strings.TrimRight(label, "0123456789") {
	case "timeout":
		return nil
	case "servfail":
		rh.RCode = dnsmessage.RCodeServerFailure
	case "refused":
		rh.RCode = dnsmessage.RCodeRefused
	case "nxdomain":
		rh.RCode = dnsmessage.RCodeNameError
	case "nodata":
	case "flaky":
		if !r.recovered.Load() {
			rh.RCode = dnsmessage.RCodeServerFailure
			break
		}
		answer = net.IPv4(93, 184, 216, 35)
	case "stub":
		answer = net.IPv4(10, 10, 34, 36)
	default:
		answer = net.IPv4(93, 184, 216, 34)
	}
	b := dnsmessage.NewBuilder(nil, rh)
	b.StartQuestions()
	b.Question(q)
	if answer != nil && q.Type == dnsmessage.TypeA {
		b.StartAnswers()
		b.AResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 60}, dnsmessage.AResource{A: [4]byte(answer.To4())})
	}
	msg, err := b.Finish()
	if err != nil {
		return nil
	}
	return msg
}

func stubHonest(t *testing.T, onLookup func(domain string)) {
	t.Helper()
	orig := resolveHonest
	resolveHonest = func(_ context.Context, _ uint, domain, _ string) []string {
		onLookup(domain)
		return []string{"93.184.216.99"}
	}
	t.Cleanup(func() { resolveHonest = orig })
}

func TestResolveAllTellsNoAddressFromSubstitution(t *testing.T) {
	srv := startTestResolver(t)
	stubHonest(t, func(domain string) {
		if strings.HasPrefix(domain, "flaky.") {
			srv.recovered.Store(true)
		}
	})

	names := []string{"ok", "stub", "flaky", "servfail", "refused", "nxdomain", "nodata", "timeout"}
	result := &SitesResult{}
	for _, n := range names {
		d := n + ".detector.test"
		result.Sites = append(result.Sites, SiteResult{Input: d, Domain: d, URL: "https://" + d + "/", Family: "ipv4"})
	}
	s := &Suite{ctx: context.Background(), Options: normalizeOptions(Options{})}

	start := time.Now()
	s.resolveAll(result)
	if took := time.Since(start); took > 12*time.Second {
		t.Fatalf("resolveAll took %v; one timeout plus its retry should take about 10s", took)
	}
	for _, name := range []string{"nxdomain", "nodata"} {
		if got := srv.asked(name + ".detector.test"); got != 1 {
			t.Errorf("%s asked %d times, want once: a name the resolver says has no address is not retried", name, got)
		}
	}

	got := make(map[string]SiteResult)
	for _, site := range result.Sites {
		got[strings.TrimSuffix(site.Domain, ".detector.test")] = site
	}
	for name, want := range map[string]string{
		"servfail": dnsErrServerFailure,
		"refused":  dnsErrServerFailure,
		"nxdomain": dnsErrNotFound,
		"nodata":   dnsErrNotFound,
		"timeout":  dnsErrTimeout,
	} {
		site := got[name]
		if site.IP != "" || site.HonestIP != "93.184.216.99" {
			t.Errorf("%s: ip %q honest %q, want no resolver address and the DoH one", name, site.IP, site.HonestIP)
		}
		if site.FakeDNS {
			t.Errorf("%s: a resolver that gave no address did not substitute one", name)
		}
		if site.DNSError != want {
			t.Errorf("%s: dns_error %q, want %q", name, site.DNSError, want)
		}
	}

	if flaky := got["flaky"]; flaky.IP != "93.184.216.35" || flaky.DNSError != "" || flaky.FakeDNS {
		t.Errorf("flaky: ip %q dns_error %q fake %v; the retry answered, so the row is as if the first lookup had", flaky.IP, flaky.DNSError, flaky.FakeDNS)
	}
	if ok := got["ok"]; ok.IP != "93.184.216.34" || ok.DNSError != "" || ok.FakeDNS {
		t.Errorf("ok: %+v, want a plain answer", ok)
	}
	if stub := got["stub"]; stub.IP != "10.10.34.36" || !stub.FakeDNS || stub.DNSError != "" {
		t.Errorf("stub: ip %q fake %v dns_error %q, want a substituted stub answer", stub.IP, stub.FakeDNS, stub.DNSError)
	}
}

type stubbedFetch struct {
	mu      sync.Mutex
	fetched []string
}

func stubFetches(t *testing.T, byIP map[string]Fetch) *stubbedFetch {
	t.Helper()
	rec := &stubbedFetch{}
	origFetch, origHTTP := fetchAddress, probeHTTP
	fetchAddress = func(_ *Suite, _ context.Context, _, _, ip string, _ uint, maxTLS uint16) Fetch {
		rec.mu.Lock()
		rec.fetched = append(rec.fetched, fmt.Sprintf("%s/%d", ip, maxTLS))
		rec.mu.Unlock()
		f, ok := byIP[ip]
		if !ok {
			t.Errorf("fetched %s, which is not an address of the site", ip)
			return Fetch{Status: netprobe.DomainError, Detail: "unexpected address"}
		}
		return f
	}
	probeHTTP = func(_ *Suite, _ context.Context, _, _ string, _ uint) (FetchStatus, string) {
		return FetchOk, "HTTP 301"
	}
	t.Cleanup(func() { fetchAddress, probeHTTP = origFetch, origHTTP })
	return rec
}

func noAddressSite() SiteResult {
	return SiteResult{
		Input: "www.cdn77.com", Domain: "www.cdn77.com", URL: "https://www.cdn77.com/", Family: "ipv4",
		HonestIP: "87.249.137.3", HonestIPs: []string{"87.249.137.3"}, DNSError: dnsErrTimeout,
		SetId: "set-1", SetName: "CDN", SetEnabled: true,
	}
}

func TestNoAddressSiteLoadsAtTheDoHAddress(t *testing.T) {
	rec := stubFetches(t, map[string]Fetch{
		"87.249.137.3": {Status: FetchOk, Detail: "HTTP 200, TLS 1.3"},
		"93.184.216.9": {Status: FetchOk, Detail: "HTTP 200, TLS 1.3"},
	})
	s := &Suite{ctx: context.Background(), directMark: 0x40000, Options: normalizeOptions(Options{})}
	site := noAddressSite()

	direct := s.fetchMode(site, s.directMark, true)
	if direct.Status != FetchDNSFail || direct.IP != "87.249.137.3" {
		t.Fatalf("direct = %s at %q, want DNS_FAIL at the DoH address", direct.Status, direct.IP)
	}
	want := "the resolver gave no address (no answer), DoH answers 87.249.137.3; the site loads at 87.249.137.3 (HTTP 200, TLS 1.3)"
	if direct.Detail != want {
		t.Fatalf("direct detail = %q, want %q", direct.Detail, want)
	}
	if direct.HTTP != FetchOk {
		t.Fatalf("plain HTTP = %q, want the normal path to probe it", direct.HTTP)
	}
	if len(rec.fetched) != 1 || rec.fetched[0] != "87.249.137.3/0" {
		t.Fatalf("fetched %v, want only the DoH address", rec.fetched)
	}

	through := s.fetchMode(site, markThroughB4, false)
	if through.Status != FetchDNSFail || through.Detail != "the resolver gave no address (no answer); the set has no DNS redirect or pin for it" {
		t.Fatalf("through = %s %q", through.Status, through.Detail)
	}
	if got := outcomeFor(&direct, &through); got != OutcomeDNS {
		t.Fatalf("outcome = %s, want dns", got)
	}

	withRedirect := site
	withRedirect.SetDNS, withRedirect.B4IPs, withRedirect.B4Source = true, []string{"93.184.216.9"}, "doh"
	redirected := s.fetchMode(withRedirect, markThroughB4, false)
	if redirected.Status != FetchOk || redirected.Source != "doh" || redirected.IP != "93.184.216.9" {
		t.Fatalf("through a set with a DNS redirect = %+v, want OK at the set's answer", redirected)
	}

	server := stubFetches(t, map[string]Fetch{"87.249.137.3": {Status: FetchServer, Detail: "HTTP 503 from the site itself"}})
	failed := s.fetchMode(site, s.directMark, true)
	if failed.Status != FetchServer || failed.Detail != "HTTP 503 from the site itself" {
		t.Fatalf("server error at the DoH address = %s %q, want the fetch's own result", failed.Status, failed.Detail)
	}
	if got := outcomeFor(&failed, &through); got != OutcomeServer {
		t.Fatalf("outcome = %s, want server", got)
	}
	if len(server.fetched) != 1 {
		t.Fatalf("fetched %v, want one attempt", server.fetched)
	}
}

func TestNoAddressSiteBlockedAtTheDoHAddressKeepsTheBlock(t *testing.T) {
	rec := stubFetches(t, map[string]Fetch{"87.249.137.3": {Status: netprobe.DomainTLSDrop, Detail: "TLS handshake timed out"}})
	s := &Suite{ctx: context.Background(), directMark: 0x40000, Options: normalizeOptions(Options{})}
	site := noAddressSite()

	direct := s.fetchMode(site, s.directMark, true)
	if direct.Status != netprobe.DomainTLSDrop || direct.Detail != "TLS handshake timed out" {
		t.Fatalf("direct = %s %q, want the block at the real address with its own detail", direct.Status, direct.Detail)
	}
	if direct.TLS12 != netprobe.DomainTLSDrop {
		t.Fatalf("TLS 1.2 retry = %q, want it run as for any blocked site", direct.TLS12)
	}
	if len(rec.fetched) != 2 || rec.fetched[1] != fmt.Sprintf("87.249.137.3/%d", tls.VersionTLS12) {
		t.Fatalf("fetched %v, want the DoH address, then its TLS 1.2 retry", rec.fetched)
	}

	through := s.fetchMode(site, markThroughB4, false)
	if got := outcomeFor(&direct, &through); got != OutcomeStillBlocked {
		t.Fatalf("outcome = %s, want still_blocked", got)
	}

	stubFetches(t, map[string]Fetch{"87.249.137.3": {Status: netprobe.DomainGateway, Detail: netprobe.GatewayDetail}})
	gateway := s.fetchMode(site, s.directMark, true)
	if gateway.Status != netprobe.DomainGateway || gateway.Detail != netprobe.GatewayDetail {
		t.Fatalf("gateway at the DoH address = %s %q", gateway.Status, gateway.Detail)
	}
}

func TestResolveAllStopsRetryingAResolverThatTimesOut(t *testing.T) {
	srv := startTestResolver(t)
	stubHonest(t, func(string) {})
	orig := systemLookupTimeout
	systemLookupTimeout = 300 * time.Millisecond
	t.Cleanup(func() { systemLookupTimeout = orig })

	var names []string
	result := &SitesResult{}
	for i := 1; i <= 5; i++ {
		d := fmt.Sprintf("timeout%d.detector.test", i)
		names = append(names, d)
		result.Sites = append(result.Sites, SiteResult{Input: d, Domain: d, URL: "https://" + d + "/", Family: "ipv4"})
	}
	s := &Suite{ctx: context.Background(), Options: normalizeOptions(Options{})}
	s.resolveAll(result)

	retried := 0
	for _, name := range names {
		if srv.asked(name) > 1 {
			retried++
		}
	}
	if retried != maxFailedRetries {
		t.Fatalf("%d names retried, want %d: retries stop after that many timeouts in a row", retried, maxFailedRetries)
	}
	for _, site := range result.Sites {
		if site.DNSError != dnsErrTimeout {
			t.Fatalf("%s: dns_error %q, want timeout", site.Domain, site.DNSError)
		}
	}
}
