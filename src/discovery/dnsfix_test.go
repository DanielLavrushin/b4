package discovery

import (
	"context"
	"net"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns/endpoint"
	"github.com/daniellavrushin/b4/nfq"
)

func udpDNSServer(t *testing.T, names map[string]stubName) string {
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
			name, qtype, end, ok := stubQuestion(buf[:n])
			if !ok {
				continue
			}
			entry, known := names[name]
			pc.WriteTo(stubReply(buf[:end], entry, known, qtype, 1), addr)
		}
	}()
	return pc.LocalAddr().String()
}

func closedUDPAddr(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := pc.LocalAddr().String()
	pc.Close()
	return addr
}

func trustedProber(t *testing.T, server string) *DNSProber {
	t.Helper()
	ep, err := endpoint.Parse(server)
	if err != nil {
		t.Fatalf("parse %q: %v", server, err)
	}
	p := &DNSProber{timeout: time.Second, ipVersion: "ipv4", trusted: ep, tlsPort: 443}
	p.serves = func(context.Context, string) bool { return false }
	return p
}

func TestTrustedServerIsTheReference(t *testing.T) {
	addr := udpDNSServer(t, map[string]stubName{
		"site.example":  {a: "203.0.113.7"},
		"empty.example": {},
	})
	p := trustedProber(t, addr)

	p.domain = "site.example"
	ref := p.reference(context.Background())
	if !reflect.DeepEqual(ref.ips, []string{"203.0.113.7"}) || !ref.trusted || !ref.overUDP || ref.source != addr {
		t.Fatalf("the trusted server's answer is the reference, got %+v", ref)
	}

	p.domain = "typo.example"
	if ref := p.reference(context.Background()); !ref.nxdomain || len(ref.ips) != 0 || ref.source != addr {
		t.Fatalf("the trusted server's NXDOMAIN is final, got %+v", ref)
	}

	p.domain = "empty.example"
	if ref := p.reference(context.Background()); !ref.nodata || ref.nxdomain {
		t.Fatalf("NOERROR without an address means the name exists, got %+v", ref)
	}
}

func TestTrustedServerThatDoesNotAnswerIsNotReplacedByPublicResolvers(t *testing.T) {
	p := trustedProber(t, closedUDPAddr(t))
	p.domain = "site.example"

	ref := p.reference(context.Background())

	if ref.failure == "" || len(ref.ips) != 0 || ref.source != p.trusted.String() {
		t.Fatalf("a silent trusted server leaves the name without a reference, got %+v", ref)
	}
}

func TestTrustedServerGoesIntoTheSetOnlyWhenASetCanUseIt(t *testing.T) {
	orig := ownAddress
	ownAddress = func(addr netip.Addr) bool { return addr == netip.MustParseAddr("192.0.2.1") }
	t.Cleanup(func() { ownAddress = orig })

	cases := []struct {
		server  string
		overUDP bool
		want    config.DNSConfig
		usable  bool
	}{
		{"https://dns.example/dns-query", false, config.DNSConfig{Enabled: true, DoHURL: "https://dns.example/dns-query"}, true},
		{"203.0.113.53", true, config.DNSConfig{Enabled: true, TargetDNS: "203.0.113.53"}, true},
		{"udp://[2001:db8::53]", true, config.DNSConfig{Enabled: true, TargetDNS: "2001:db8::53"}, true},
		{"tcp+udp://203.0.113.53", true, config.DNSConfig{Enabled: true, TargetDNS: "203.0.113.53"}, true},
		{"tcp+udp://203.0.113.53", false, config.DNSConfig{}, false},
		{"tcp://203.0.113.53", false, config.DNSConfig{}, false},
		{"203.0.113.53:5353", true, config.DNSConfig{}, false},
		{"127.0.0.1", true, config.DNSConfig{}, false},
		{"tcp+udp://127.0.0.1:53053", true, config.DNSConfig{}, false},
		{"192.0.2.1", true, config.DNSConfig{}, false},
	}
	for _, tc := range cases {
		p := trustedProber(t, tc.server)
		p.ref = referenceAnswer{ips: []string{"198.51.100.7"}, trusted: true, overUDP: tc.overUDP}
		got, ok := p.trustedSetDNS()
		if ok != tc.usable || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s (overUDP=%v): got %+v %v, want %+v %v", tc.server, tc.overUDP, got, ok, tc.want, tc.usable)
		}
	}

	silent := trustedProber(t, "203.0.113.53")
	silent.ref = referenceAnswer{failure: "timeout"}
	if _, ok := silent.trustedSetDNS(); ok {
		t.Fatal("a server that gave no answer cannot become the set's DNS")
	}
}

func TestAFixCandidateMustAgreeWithTheReference(t *testing.T) {
	p := &DNSProber{domain: "site.example"}
	serving := map[string]bool{"192.0.2.80": true}
	p.serves = func(_ context.Context, ip string) bool { return serving[ip] }
	refs := []string{"203.0.113.7", "203.0.113.8"}

	for _, tc := range []struct {
		name string
		ips  []string
		want bool
	}{
		{"same address", []string{"198.51.100.1", "203.0.113.8"}, true},
		{"same /24", []string{"203.0.113.99"}, true},
		{"another address that serves the site", []string{"192.0.2.80"}, true},
		{"a forged address", []string{"198.51.100.1"}, false},
		{"no address", nil, false},
	} {
		if _, ok := p.candidateWorks(context.Background(), tc.ips, refs); ok != tc.want {
			t.Errorf("%s: works=%v, want %v", tc.name, ok, tc.want)
		}
	}
}

func TestPoisonedSiteWithoutADNSFixGetsItsVerifiedAddressesPinned(t *testing.T) {
	poisoned := func() *DNSDiscoveryResult {
		return &DNSDiscoveryResult{
			IsPoisoned:       true,
			ExpectedIPs:      []string{"203.0.113.7", "198.51.100.66"},
			Reference:        "https://dns.example/dns-query",
			referenceIPs:     []string{"203.0.113.7"},
			referenceTrusted: true,
		}
	}

	r := poisoned()
	pinReferenceWhenNoFix("site.example", r)
	if !reflect.DeepEqual(r.AlternativeIPs, []string{"203.0.113.7"}) {
		t.Fatalf("no DNS fix exists, the address from DNS over HTTPS must be pinned, got %v", r.AlternativeIPs)
	}

	forgeable := poisoned()
	forgeable.referenceTrusted = false
	pinReferenceWhenNoFix("site.example", forgeable)
	if len(forgeable.AlternativeIPs) != 0 {
		t.Fatal("a reference from plain UDP can be forged on the path and is never pinned")
	}

	fixed := poisoned()
	fixed.BestDoHURL = "https://fix.example/dns-query"
	pinReferenceWhenNoFix("site.example", fixed)
	if len(fixed.AlternativeIPs) != 0 {
		t.Fatal("a site with a working DNS fix gets the fix, not pins")
	}

	scanned := poisoned()
	scanned.AlternativeIPs = []string{"192.0.2.10"}
	pinReferenceWhenNoFix("site.example", scanned)
	if !reflect.DeepEqual(scanned.AlternativeIPs, []string{"192.0.2.10"}) {
		t.Fatal("addresses another region's DNS gave stay as they are")
	}
}

func TestTheRunWideDNSFixIsDeterministic(t *testing.T) {
	ds := unresolvedSuite(t, true, "a.example", "b.example", "c.example")
	ds.dnsResults["a.example"] = &DNSDiscoveryResult{IsPoisoned: true, BestServer: "9.9.9.9"}
	ds.dnsResults["b.example"] = &DNSDiscoveryResult{IsPoisoned: true, BestDoHURL: "https://fix.example/dns-query"}
	ds.dnsResults["c.example"] = &DNSDiscoveryResult{IsPoisoned: true, BestServer: "1.1.1.1", NeedsFragment: true}

	for i := 0; i < 20; i++ {
		ds.discoveredDNS = config.DNSConfig{}
		ds.applyBestDNSConfig()
		if ds.discoveredDNS.DoHURL != "https://fix.example/dns-query" {
			t.Fatalf("a DoH fix wins over a plain server, got %+v", ds.discoveredDNS)
		}
	}

	ds.dnsResults["b.example"].BestDoHURL = ""
	for i := 0; i < 20; i++ {
		ds.discoveredDNS = config.DNSConfig{}
		ds.applyBestDNSConfig()
		if ds.discoveredDNS.TargetDNS != "9.9.9.9" || ds.discoveredDNS.FragmentQuery {
			t.Fatalf("the first domain's server wins, with its own fragment flag, got %+v", ds.discoveredDNS)
		}
	}
}

func TestRunPinsAndTheSetsPinsAreUsed(t *testing.T) {
	ds := unresolvedSuite(t, false, "ntc.party", "www.ntc.party", "other.example")
	ds.setStrategy = &config.SetConfig{DNS: config.DNSConfig{Pins: map[string][]string{"ntc.party": {"130.255.77.28"}}}}
	ds.runPins = map[string][]string{
		"NTC.party.":    {"130.255.77.29", "2001:db8::28", "130.255.77.28"},
		"other.example": {"not-an-ip", "198.51.100.4"},
	}

	ds.pins = ds.collectPins()

	if !reflect.DeepEqual(ds.pins["ntc.party"], []string{"130.255.77.28", "130.255.77.29", "2001:db8::28"}) || !reflect.DeepEqual(ds.pins["other.example"], []string{"198.51.100.4"}) {
		t.Fatalf("the set's pins and the run's pins are merged per domain, got %v", ds.pins)
	}

	ds.ipVersion = "ipv4"
	if got := ds.pinnedFor("www.ntc.party"); !reflect.DeepEqual(got, []string{"130.255.77.28", "130.255.77.29"}) {
		t.Fatalf("a pin covers its subdomains, and an IPv4 run uses the IPv4 pins only, got %v", got)
	}
	ds.ipVersion = "ipv6"
	if got := ds.pinnedFor("ntc.party"); !reflect.DeepEqual(got, []string{"2001:db8::28"}) {
		t.Fatalf("an IPv6 run uses the IPv6 pins only, got %v", got)
	}
	if got := ds.pinnedFor("unpinned.example"); len(got) != 0 {
		t.Fatalf("an unpinned name has no pins, got %v", got)
	}
}

func TestAPinnedNameIsTestedThroughItsPinsAndTheSetKeepsThem(t *testing.T) {
	ds := unresolvedSuite(t, false, "ntc.party")
	ds.ipVersion = "ipv4"
	ds.runPins = map[string][]string{"ntc.party": {"130.255.77.28"}}
	ds.pins = ds.collectPins()

	result := ds.pinnedResult(ds.Domains[0], ds.pinnedFor("ntc.party"))
	ds.dnsResults["ntc.party"] = result

	if !result.Pinned || !reflect.DeepEqual(ds.collectTargetIPs("ntc.party", 2), []string{"130.255.77.28"}) {
		t.Fatalf("the probes must connect to the pinned address, got %+v targets=%v", result, ds.collectTargetIPs("ntc.party", 2))
	}

	ds.markUnresolved(map[string]CheckResult{"ntc.party": failedLookup("ntc.party", lookupNotFound)})
	if ds.unresolved("ntc.party") {
		t.Fatal("a pinned name has an address, whatever DNS says about it")
	}

	set := config.NewSetConfig()
	scoped := ds.scopeSetToDomains(&set, []string{"ntc.party"})
	if !reflect.DeepEqual(scoped.DNS.Pins, map[string][]string{"ntc.party": {"130.255.77.28"}}) || !scoped.TCP.IPBlockDetect.Enabled {
		t.Fatalf("the proposed set carries the pin, got pins=%v ipblock=%v", scoped.DNS.Pins, scoped.TCP.IPBlockDetect.Enabled)
	}
}

func TestASilentTrustedServerFailsTheRun(t *testing.T) {
	previous := suiteRetention
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })
	nxdomainResolver(t)

	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Queue.IsDiscovery = true
	cfg.System.Checker.ConfigPropagateMs = 0
	cfg.System.Checker.DiscoveryTimeoutSec = 1
	cfg.System.Checker.DNSServer = "udp://" + closedUDPAddr(t)
	pool := nfq.NewPool(&cfg)
	t.Cleanup(pool.Stop)

	ds := NewDiscoverySuite([]string{"site.example"}, pool, false, true, nil, 1, "", "", 0)
	RegisterSuite(ds.CheckSuite)
	ds.RunDiscovery()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := GetCheckSuite(ds.Id); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the failed run is never dropped from the active suites")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if ds.Status != CheckStatusFailed || ds.CompletedChecks != 0 {
		t.Fatalf("a trusted DNS server that does not answer must stop the run before any check, got status=%q checks=%d", ds.Status, ds.CompletedChecks)
	}
	if !strings.Contains(ds.trusted.String(), "127.0.0.1") {
		t.Fatalf("the run must use the trusted server from the settings, got %q", ds.trusted.String())
	}
}
