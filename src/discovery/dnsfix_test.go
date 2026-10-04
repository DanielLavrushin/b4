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
	var checked []string
	p.serves = func(_ context.Context, ip string) bool {
		checked = append(checked, ip)
		return false
	}

	p.domain = "site.example"
	ref := p.reference(context.Background())
	if !reflect.DeepEqual(ref.ips, []string{"203.0.113.7"}) || !ref.trusted || !ref.overUDP || ref.source != addr {
		t.Fatalf("the trusted server's answer is the reference, got %+v", ref)
	}
	if !reflect.DeepEqual(checked, []string{"203.0.113.7"}) {
		t.Fatalf("the reference addresses are checked through the prober's own check, got %v", checked)
	}
	if ref.forgeable {
		t.Fatal("an answer from a server on this host cannot be forged on the way")
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
		refusal string
	}{
		{"https://dns.example/dns-query", false, config.DNSConfig{Enabled: true, DoHURL: "https://dns.example/dns-query"}, ""},
		{"203.0.113.53", true, config.DNSConfig{Enabled: true, TargetDNS: "203.0.113.53"}, ""},
		{"udp://[2001:db8::53]", true, config.DNSConfig{Enabled: true, TargetDNS: "2001:db8::53"}, ""},
		{"tcp+udp://203.0.113.53", true, config.DNSConfig{Enabled: true, TargetDNS: "203.0.113.53"}, ""},
		{"tcp+udp://203.0.113.53", false, config.DNSConfig{}, "answered over TCP only"},
		{"203.0.113.53", false, config.DNSConfig{}, "answered over TCP only"},
		{"tcp://203.0.113.53", false, config.DNSConfig{}, "tcp://"},
		{"203.0.113.53:5353", true, config.DNSConfig{}, "port 5353"},
		{"tcp+udp://127.0.0.1:53053", true, config.DNSConfig{}, "port 53053"},
		{"127.0.0.1", true, config.DNSConfig{}, "runs on this host"},
		{"192.0.2.1", true, config.DNSConfig{}, "runs on this host"},
		{"192.168.1.5", true, config.DNSConfig{}, "local network"},
		{"[fd00::53]:53", true, config.DNSConfig{}, "local network"},
	}
	for _, tc := range cases {
		p := trustedProber(t, tc.server)
		p.ref = referenceAnswer{ips: []string{"198.51.100.7"}, trusted: true, overUDP: tc.overUDP}
		got, refusal := p.trustedSetDNS()
		if !reflect.DeepEqual(got, tc.want) || (tc.refusal == "") != (refusal == "") || !strings.Contains(refusal, tc.refusal) {
			t.Errorf("%s (overUDP=%v): got %+v %q, want %+v and a refusal naming %q", tc.server, tc.overUDP, got, refusal, tc.want, tc.refusal)
		}
	}

	silent := trustedProber(t, "203.0.113.53")
	silent.ref = referenceAnswer{failure: "timeout"}
	if fix, refusal := silent.trustedSetDNS(); fix.Enabled || refusal != "" {
		t.Fatalf("a server that gave no answer is neither the set's DNS nor worth a refusal, got %+v %q", fix, refusal)
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

func TestRunPinsComeBeforeTheSetsPins(t *testing.T) {
	ds := unresolvedSuite(t, false, "ntc.party", "www.ntc.party", "other.example", "set.example")
	ds.setStrategy = &config.SetConfig{DNS: config.DNSConfig{Pins: map[string][]string{
		"ntc.party":   {"130.255.77.28"},
		"set.example": {"198.51.100.9", "2001:db8::9"},
	}}}
	ds.runPins = map[string][]string{
		"NTC.party.":    {"130.255.77.29", "2001:db8::28", "::ffff:130.255.77.30"},
		"other.example": {"not-an-ip", "198.51.100.4"},
		"пример.рф":     {"198.51.100.5"},
	}

	ds.collectPins()

	if !reflect.DeepEqual(ds.givenPins["ntc.party"], []string{"130.255.77.29", "2001:db8::28", "130.255.77.30"}) || !reflect.DeepEqual(ds.givenPins["other.example"], []string{"198.51.100.4"}) {
		t.Fatalf("the run's pins are normalized per domain, got %v", ds.givenPins)
	}

	ds.ipVersion = "ipv4"
	if got, fromSet := ds.pinnedFor("www.ntc.party"); !reflect.DeepEqual(got, []string{"130.255.77.29", "130.255.77.30"}) || fromSet {
		t.Fatalf("a run's pin covers its subdomains, wins over the set's pin for the same name, and an IPv4 run uses the IPv4 pins only, got %v fromSet=%v", got, fromSet)
	}
	if got, fromSet := ds.pinnedFor("set.example"); !reflect.DeepEqual(got, []string{"198.51.100.9"}) || !fromSet {
		t.Fatalf("a name only the set pins uses the set's pins, got %v fromSet=%v", got, fromSet)
	}
	ds.ipVersion = "ipv6"
	if got, _ := ds.pinnedFor("ntc.party"); !reflect.DeepEqual(got, []string{"2001:db8::28"}) {
		t.Fatalf("an IPv6 run uses the IPv6 pins only, got %v", got)
	}
	if got, fromSet := ds.pinnedFor("unpinned.example"); len(got) != 0 || fromSet {
		t.Fatalf("an unpinned name has no pins, got %v fromSet=%v", got, fromSet)
	}
	ds.ipVersion = "ipv4"
	if got, _ := ds.pinnedFor("пример.рф"); len(got) != 0 {
		t.Fatalf("the probes dial an internationalized name by its xn-- form, which a pin under the Unicode name never matches, got %v", got)
	}
}

func TestAPinnedNameIsTestedThroughItsPinsAndTheSetKeepsThem(t *testing.T) {
	ds := unresolvedSuite(t, false, "www.ntc.party")
	ds.ipVersion = "ipv4"
	ds.runPins = map[string][]string{"ntc.party": {"130.255.77.28", "2001:db8::28"}}
	ds.collectPins()

	pins, fromSet := ds.pinnedFor("www.ntc.party")
	result := ds.pinnedResult(ds.Domains[0], pins, fromSet)
	ds.dnsResults["www.ntc.party"] = result

	if !result.Pinned || !reflect.DeepEqual(ds.collectTargetIPs("www.ntc.party", 2), []string{"130.255.77.28"}) {
		t.Fatalf("the probes must connect to the pinned address, got %+v targets=%v", result, ds.collectTargetIPs("www.ntc.party", 2))
	}

	ds.markUnresolved(map[string]CheckResult{"www.ntc.party": failedLookup("www.ntc.party", lookupNotFound)})
	if ds.unresolved("www.ntc.party") {
		t.Fatal("a pinned name has an address, whatever DNS says about it")
	}

	set := config.NewSetConfig()
	scoped := ds.scopeSetToDomains(&set, []string{"www.ntc.party"})
	if !reflect.DeepEqual(scoped.DNS.Pins, map[string][]string{"ntc.party": {"130.255.77.28", "2001:db8::28"}}) || !scoped.TCP.IPBlockDetect.Enabled {
		t.Fatalf("the proposed set carries the pin as given, under its own name and with both families, got pins=%v ipblock=%v", scoped.DNS.Pins, scoped.TCP.IPBlockDetect.Enabled)
	}
}

func TestARunForASetDoesNotProposeTheSetsOwnPins(t *testing.T) {
	ds := unresolvedSuite(t, false, "www.ntc.party")
	ds.ipVersion = "ipv4"
	live := config.NewSetConfig()
	live.DNS.Pins = map[string][]string{"ntc.party": {"130.255.77.28", "2001:db8::28"}}
	live.TCP.IPBlockDetect.Enabled = true
	live.TCP.IPBlockDetect.RetransmitThreshold = 7
	ds.setStrategy = SetRunStrategy(&live)
	ds.collectPins()

	pins, fromSet := ds.pinnedFor("www.ntc.party")
	if !fromSet {
		t.Fatal("the pin comes from the set")
	}
	ds.dnsResults["www.ntc.party"] = ds.pinnedResult(ds.Domains[0], pins, fromSet)

	strategy := config.NewSetConfig()
	scoped := ds.scopeSetToDomains(&strategy, []string{"www.ntc.party"})
	if len(scoped.DNS.Pins) != 0 || scoped.TCP.IPBlockDetect.Enabled {
		t.Fatalf("the set already has its pins, the proposal must not rewrite them or reset IP block detection, got pins=%v ipblock=%+v", scoped.DNS.Pins, scoped.TCP.IPBlockDetect)
	}

	ds.runPins = map[string][]string{"ntc.party": {"2001:db8::99"}}
	ds.collectPins()
	pins, fromSet = ds.pinnedFor("www.ntc.party")
	ds.dnsResults["www.ntc.party"] = ds.pinnedResult(ds.Domains[0], pins, fromSet)
	if mixed := ds.scopeSetToDomains(&strategy, []string{"www.ntc.party"}); len(mixed.DNS.Pins) != 0 {
		t.Fatalf("an IPv6 pin given for an IPv4 run was never tested and must not replace the set's working pin, got %v", mixed.DNS.Pins)
	}

	applied := live
	applied.DNS.Pins = map[string][]string{"ntc.party": {"130.255.77.28", "2001:db8::28"}}
	verdict := &SetVerdict{Set: scoped, Covered: []string{"www.ntc.party"}}
	if covered := verdict.CoveredPins(); len(covered) != 0 {
		applied.ReplacePins(config.PinDomains(covered), covered)
	}
	applied.AdoptStrategy(scoped)
	if !reflect.DeepEqual(applied.DNS.Pins, map[string][]string{"ntc.party": {"130.255.77.28", "2001:db8::28"}}) || applied.TCP.IPBlockDetect.RetransmitThreshold != 7 {
		t.Fatalf("applying the verdict keeps the set's pins and its IP block detection, got pins=%v ipblock=%+v", applied.DNS.Pins, applied.TCP.IPBlockDetect)
	}
}

func TestDeadSetPinsFallBackToTheDNSCheck(t *testing.T) {
	dead := func(fromSet bool) *DNSDiscoveryResult {
		r := newPinnedResult([]string{"130.255.77.28"}, fromSet)
		r.TransportBlocked, r.AlternativeIPs = true, nil
		return r
	}
	checks := 0
	answering := func() *DNSDiscoveryResult {
		checks++
		return &DNSDiscoveryResult{AlternativeIPs: []string{"198.51.100.20"}, TransportBlocked: true, ExpectedIPs: []string{"192.0.2.1"}}
	}
	empty := func() *DNSDiscoveryResult {
		checks++
		return &DNSDiscoveryResult{NoAddressFamily: "ipv4"}
	}

	livePins := newPinnedResult([]string{"130.255.77.28"}, true)
	if got := checkDNSWhenSetPinsAreDead("ntc.party", livePins, answering); got != livePins || checks != 0 {
		t.Fatalf("the set's pins answer, so they are used without a DNS check, got %+v after %d checks", got, checks)
	}
	given := dead(false)
	if got := checkDNSWhenSetPinsAreDead("ntc.party", given, answering); got != given || checks != 0 {
		t.Fatalf("pins given for the run are what the run is asked to test, dead or not, got %+v after %d checks", got, checks)
	}
	if got := checkDNSWhenSetPinsAreDead("ntc.party", dead(true), answering); !reflect.DeepEqual(got.AlternativeIPs, []string{"198.51.100.20"}) || got.Pinned || checks != 1 {
		t.Fatalf("dead set pins are passed over for what DNS and the address scan find, as the set does at runtime, got %+v after %d checks", got, checks)
	}
	setDead := dead(true)
	if got := checkDNSWhenSetPinsAreDead("ntc.party", setDead, empty); got != setDead || !got.addressBlocked() {
		t.Fatalf("with no address in DNS either, the dead pins are the answer, got %+v", got)
	}
}

func TestPinsAreCheckedBeforeTheProbesUseThem(t *testing.T) {
	probe := func(live, serving, intercepted map[string]bool) *DNSProber {
		p := &DNSProber{domain: "ntc.party", timeout: time.Second, ipVersion: "ipv4"}
		p.connectable = func(_ context.Context, ips []string) bool {
			for _, ip := range ips {
				if live[ip] {
					return true
				}
			}
			return false
		}
		p.serves = func(_ context.Context, ip string) bool { return serving[ip] }
		p.gateway = func(_ context.Context, ip string) bool { return intercepted[ip] }
		return p
	}
	ctx, cancel := context.WithTimeout(context.Background(), pinCheckTimeout)
	defer cancel()
	pins := []string{"203.0.113.1", "203.0.113.2", "203.0.113.3", "203.0.113.4", "203.0.113.5"}

	none := probe(nil, nil, nil).checkPins(ctx, pins, false)
	if !none.addressBlocked() || !reflect.DeepEqual(none.ExpectedIPs, pins) {
		t.Fatalf("pins that refuse every connection leave the site address-blocked, got %+v", none)
	}
	ds := unresolvedSuite(t, false, "ntc.party")
	ds.dnsResults["ntc.party"] = none
	ds.domainResults["ntc.party"].DNSResult = none
	ds.refreshOutcomes(true)
	if got := ds.domainResults["ntc.party"].Outcome; got != OutcomeAddressBlocked {
		t.Fatalf("outcome %q, want %q", got, OutcomeAddressBlocked)
	}

	some := probe(map[string]bool{"203.0.113.3": true, "203.0.113.5": true}, map[string]bool{"203.0.113.3": true}, nil).checkPins(ctx, pins, false)
	ds.dnsResults["ntc.party"] = some
	if got := ds.collectTargetIPs("ntc.party", 2); !reflect.DeepEqual(got, []string{"203.0.113.3", "203.0.113.5"}) || some.addressBlocked() {
		t.Fatalf("the probes use the pins that answer, at most two, got %v from %+v", got, some)
	}

	many := probe(map[string]bool{"203.0.113.1": true, "203.0.113.2": true, "203.0.113.3": true}, map[string]bool{"203.0.113.1": true}, nil).checkPins(ctx, pins, false)
	ds.dnsResults["ntc.party"] = many
	if got := ds.collectTargetIPs("ntc.party", 2); len(got) != 2 {
		t.Fatalf("pins do not lift the two-address cap on the probes, got %v", got)
	}

	mixed := unresolvedSuite(t, false, "ntc.party", "unpinned.example")
	mixed.dnsResults["ntc.party"] = none
	if mixed.allDomainsTransportBlocked() {
		t.Fatal("a name the DNS check skipped has no result, which says nothing about its addresses, so the extended search must still run")
	}
	mixed.Domains = mixed.Domains[:1]
	if !mixed.allDomainsTransportBlocked() {
		t.Fatal("with every name's pins dead there is nothing left for the extended search")
	}

	firstHop := map[string]bool{"203.0.113.1": true, "203.0.113.2": true}
	gateway := probe(firstHop, nil, firstHop).checkPins(ctx, pins[:2], false)
	if !gateway.gatewayIntercepted() {
		t.Fatalf("pins that only the first hop answers are reported as intercepted, got %+v", gateway)
	}
}

func TestTheStartCheckAsksAServerOnlyWhenTheRunUsesIt(t *testing.T) {
	ds := unresolvedSuite(t, true, "ntc.party", "other.example")
	cfg := config.NewConfig()
	cfg.System.Checker.DiscoveryTimeoutSec = 1
	ds.cfg = &cfg
	ds.ipVersion = "ipv4"
	ep, err := endpoint.Parse("udp://" + closedUDPAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	ds.trusted = ep
	ds.runPins = map[string][]string{"ntc.party": {"130.255.77.28"}, "other.example": {"198.51.100.4"}}
	ds.collectPins()

	if err := ds.checkTrustedServer(); err != nil {
		t.Fatalf("every name is pinned, nothing in the run asks the trusted server, got %v", err)
	}

	delete(ds.runPins, "other.example")
	ds.collectPins()
	if err := ds.checkTrustedServer(); err == nil {
		t.Fatal("a name the run checks makes the silent trusted server an error")
	}
}

func TestTheStartCheckTakesAServerFailureForAnAnswer(t *testing.T) {
	ds := unresolvedSuite(t, true, "broken.example")
	cfg := config.NewConfig()
	cfg.System.Checker.DiscoveryTimeoutSec = 1
	ds.cfg = &cfg
	ds.ipVersion = "ipv4"

	for _, tc := range []struct {
		rcode byte
		alive bool
	}{{2, true}, {4, true}, {5, false}} {
		ep, err := endpoint.Parse(udpDNSServer(t, map[string]stubName{"broken.example": {rcode: tc.rcode}}))
		if err != nil {
			t.Fatal(err)
		}
		ds.trusted = ep
		err = ds.checkTrustedServer()
		if (err == nil) != tc.alive {
			t.Errorf("rcode %d: got %v, want alive=%v", tc.rcode, err, tc.alive)
		}
	}
}

func silentUDPAddr(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := pc.ReadFrom(buf); err != nil {
				return
			}
		}
	}()
	return pc.LocalAddr().String()
}

func TestACancelDuringTheStartCheckEndsTheRunCanceled(t *testing.T) {
	previous := suiteRetention
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })
	nxdomainResolver(t)

	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Queue.IsDiscovery = true
	cfg.System.Checker.ConfigPropagateMs = 0
	cfg.System.Checker.DiscoveryTimeoutSec = 5
	cfg.System.Checker.DNSServer = "udp://" + silentUDPAddr(t)
	pool := nfq.NewPool(&cfg)
	t.Cleanup(pool.Stop)

	ds := NewDiscoverySuite([]string{"site.example"}, pool, false, true, nil, 1, "", "", 0)
	RegisterSuite(ds.CheckSuite)
	time.AfterFunc(300*time.Millisecond, func() { CancelCheckSuite(ds.Id) })
	started := time.Now()
	ds.RunDiscovery()

	if took := time.Since(started); took > 3*time.Second {
		t.Fatalf("the cancel must cut the start check short, the run took %v", took)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := GetCheckSuite(ds.Id); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the canceled run is never dropped from the active suites")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ds.Status != CheckStatusCanceled {
		t.Fatalf("a cancel during the start check ends the run canceled, not failed, got %q", ds.Status)
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

func TestTheAdviceForANameWithoutAnAddress(t *testing.T) {
	if got := unresolvedAdvice("ipv6", nil); !strings.Contains(got, "an IPv6 address") {
		t.Errorf("missing family: %q", got)
	}
	forged := &DNSDiscoveryResult{NXDomain: true, ForgeableAnswer: true}
	if got := unresolvedAdvice("", forged); !strings.Contains(got, "check the spelling") || !strings.Contains(got, "plain DNS") {
		t.Errorf("an NXDOMAIN over plain DNS can be forged on the way, the advice must say so: %q", got)
	}
	noAddress := &DNSDiscoveryResult{NoAddressFamily: "ipv4", ForgeableAnswer: true}
	if got := unresolvedAdvice("", noAddress); !strings.Contains(got, "pin it") || !strings.Contains(got, "plain DNS") {
		t.Errorf("an empty answer over plain DNS can be forged as well: %q", got)
	}
	for _, r := range []*DNSDiscoveryResult{{NXDomain: true}, {ForgeableAnswer: true}} {
		if got := unresolvedAdvice("", r); strings.Contains(got, "plain DNS") {
			t.Errorf("%+v: the note belongs only to an answer about the name that came over plain DNS: %q", r, got)
		}
	}
}
