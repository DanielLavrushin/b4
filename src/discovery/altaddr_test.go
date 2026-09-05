package discovery

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestECSScanSubnetsCoverOnlyPublicSpace(t *testing.T) {
	subnets := ecsScanSubnets()
	if len(subnets) < 400 {
		t.Fatalf("the grid should cover most of the public IPv4 space, got %d subnets", len(subnets))
	}
	for _, s := range subnets {
		if !publicIPv4(s.IP) {
			t.Errorf("%s is not public", s.String())
		}
		if ones, _ := s.Mask.Size(); ones != 16 {
			t.Errorf("%s: want a /16, got /%d", s.String(), ones)
		}
	}
	for _, bad := range []string{"10.0.0.0", "192.168.1.1", "100.70.0.0", "127.0.0.1", "224.0.0.1", "0.0.0.0"} {
		if publicIPv4(net.ParseIP(bad)) {
			t.Errorf("%s must not be scanned", bad)
		}
	}
}

func altSuite(t *testing.T, domain string, dnsResult *DNSDiscoveryResult) *DiscoverySuite {
	t.Helper()
	ds := suiteWithResults(map[string]map[string]*DomainPresetResult{domain: {}})
	ds.Domains = []DomainInput{{Domain: domain, CheckURL: "https://" + domain + "/"}}
	ds.dnsResults = map[string]*DNSDiscoveryResult{domain: dnsResult}
	ds.domainResults[domain].DNSResult = dnsResult
	cfg := config.NewConfig()
	ds.cfg = &cfg
	return ds
}

func TestProbeTargetsPreferAlternativeAddresses(t *testing.T) {
	blocked := altSuite(t, "www.instagram.com", &DNSDiscoveryResult{
		TransportBlocked: true, ExpectedIPs: []string{"31.13.72.174"}, AlternativeIPs: []string{"157.240.253.174", "57.144.248.34"},
	})
	ips := blocked.collectTargetIPs("www.instagram.com", 2)
	if len(ips) != 2 || ips[0] != "157.240.253.174" || ips[1] != "57.144.248.34" {
		t.Fatalf("blocked addresses waste a timeout each, the probe must use only the alternatives: %v", ips)
	}

	filtered := altSuite(t, "www.instagram.com", &DNSDiscoveryResult{
		IsPoisoned: true, ExpectedIPs: []string{"57.144.248.34", "57.144.244.34"}, AlternativeIPs: []string{"157.240.253.174"},
	})
	ips = filtered.collectTargetIPs("www.instagram.com", 2)
	if len(ips) != 3 || ips[0] != "157.240.253.174" || ips[1] != "57.144.248.34" {
		t.Fatalf("a clean alternative goes first, the reachable known addresses stay as fallbacks: %v", ips)
	}
}

func TestPlainFetchThroughAnAlternativeAddressIsAFoundStrategy(t *testing.T) {
	ds := altSuite(t, "www.instagram.com", &DNSDiscoveryResult{
		TransportBlocked: true, ExpectedIPs: []string{"31.13.72.174"}, AlternativeIPs: []string{"157.240.253.174"},
	})
	set := config.NewSetConfig()

	ds.storeResultsMulti(GetPhase1Presets()[0], map[string]CheckResult{
		"www.instagram.com": {Domain: "www.instagram.com", Status: CheckStatusComplete, Speed: 9000, UsedIP: "157.240.253.174", Set: &set},
	})
	dr := ds.domainResults["www.instagram.com"]
	alt := dr.Results[presetAltAddress]
	if alt == nil || alt.Status != CheckStatusComplete || alt.Set == nil {
		t.Fatalf("a plain fetch that only worked through the alternative address must be recorded as its own preset: %+v", alt)
	}
	if alt.Set.Fragmentation.Strategy != config.ConfigNone || alt.Set.Faking.SNI {
		t.Errorf("the alternative-address set must carry no packet changes: %+v", alt.Set.Fragmentation.Strategy)
	}
	if pins := alt.Set.DNS.Pins["www.instagram.com"]; len(pins) != 1 || pins[0] != "157.240.253.174" {
		t.Errorf("the set must pin the site to the reachable address, got %v", alt.Set.DNS.Pins)
	}

	ds.storeResultsMulti(GetPhase1Presets()[1], map[string]CheckResult{
		"www.instagram.com": {Domain: "www.instagram.com", Status: CheckStatusComplete, Speed: 20000, UsedIP: "157.240.253.174", Set: &set},
	})
	ds.determineBest()
	if dr.BaselineWorks {
		t.Error("the site does not load through its own addresses, so it is not a no-bypass site")
	}
	if dr.BestPreset != presetAltAddress {
		t.Errorf("a faster packet strategy adds nothing when the plain fetch works, best = %q", dr.BestPreset)
	}
	if dr.Outcome != OutcomeFound {
		t.Errorf("outcome = %q, want found", dr.Outcome)
	}
	if ds.needsBypass("www.instagram.com") || ds.anyDomainNeedsBypass() {
		t.Error("a site that loads through the alternative address needs no strategy search")
	}
	if ds.allDomainsTransportBlocked() {
		t.Error("a site with alternatives is not transport-blocked for the purpose of the extended search")
	}
}

func TestPlainFetchThroughHonestDNSIsARedirectStrategy(t *testing.T) {
	ds := altSuite(t, "meduza.io", &DNSDiscoveryResult{
		IsPoisoned: true, ReferenceServes: true, ExpectedIPs: []string{"8.47.69.0"}, BestDoHURL: "https://1.1.1.1/dns-query",
	})
	ds.discoveredDNS = config.DNSConfig{Enabled: true, DoHURL: "https://1.1.1.1/dns-query"}
	set := config.NewSetConfig()

	ds.storeResultsMulti(GetPhase1Presets()[0], map[string]CheckResult{
		"meduza.io": {Domain: "meduza.io", Status: CheckStatusComplete, Speed: 5000, UsedIP: "8.47.69.0", Set: &set},
	})
	ds.determineBest()
	dr := ds.domainResults["meduza.io"]
	if dr.BestPreset != presetDNSRedirect || dr.Outcome != OutcomeFound || dr.BaselineWorks {
		t.Fatalf("a poisoned site that loads through its real address needs only the DNS fix: best=%q outcome=%q baseline=%v", dr.BestPreset, dr.Outcome, dr.BaselineWorks)
	}
	fix := dr.Results[presetDNSRedirect]
	if fix == nil || fix.Set == nil || !fix.Set.DNS.Enabled || fix.Set.DNS.DoHURL == "" {
		t.Fatalf("the redirect set must carry the DNS fix: %+v", fix)
	}
	if len(fix.Set.DNS.Pins) != 0 {
		t.Errorf("no alternative address was needed, so no pin: %v", fix.Set.DNS.Pins)
	}
	if ds.anyDomainNeedsBypass() {
		t.Error("nothing is left to search once the DNS fix makes the site load")
	}
}

func TestUnblockedSiteStaysANoBypassSite(t *testing.T) {
	ds := altSuite(t, "example.com", &DNSDiscoveryResult{ExpectedIPs: []string{"93.184.216.34"}})
	set := config.NewSetConfig()
	ds.storeResultsMulti(GetPhase1Presets()[0], map[string]CheckResult{
		"example.com": {Domain: "example.com", Status: CheckStatusComplete, Speed: 5000, UsedIP: "93.184.216.34", Set: &set},
	})
	ds.determineBest()
	dr := ds.domainResults["example.com"]
	if !dr.BaselineWorks || dr.Outcome != OutcomeWorksWithoutBypass {
		t.Fatalf("an honest answer that serves the site is the no-bypass case: baseline=%v outcome=%q", dr.BaselineWorks, dr.Outcome)
	}
	if ds.needsBypass("example.com") {
		t.Error("no search is needed for it")
	}
}

func TestAddressBlockedOnlyWithoutAlternatives(t *testing.T) {
	ds := altSuite(t, "www.instagram.com", &DNSDiscoveryResult{TransportBlocked: true, ExpectedIPs: []string{"31.13.72.174"}})
	ds.storeResultsMulti(GetPhase1Presets()[0], map[string]CheckResult{
		"www.instagram.com": {Domain: "www.instagram.com", Status: CheckStatusFailed},
	})
	ds.determineBest()
	if got := ds.domainResults["www.instagram.com"].Outcome; got != OutcomeAddressBlocked {
		t.Fatalf("outcome = %q, want address_blocked", got)
	}
	if !ds.allDomainsTransportBlocked() {
		t.Error("with no alternative the extended search is pointless")
	}

	ds.dnsResults["www.instagram.com"].AlternativeIPs = []string{"157.240.253.174"}
	ds.refreshOutcomes(true)
	if got := ds.domainResults["www.instagram.com"].Outcome; got == OutcomeAddressBlocked {
		t.Fatal("an alternative address means the site is not out of reach")
	}
}

func TestScopedSetCarriesPinsAndIPBlockDetection(t *testing.T) {
	ds := altSuite(t, "www.instagram.com", &DNSDiscoveryResult{
		TransportBlocked: true, ExpectedIPs: []string{"31.13.72.174"}, AlternativeIPs: []string{"157.240.253.174"},
	})
	ds.Domains = append(ds.Domains, DomainInput{Domain: "meduza.io", CheckURL: "https://meduza.io/"})
	ds.dnsResults["meduza.io"] = &DNSDiscoveryResult{ExpectedIPs: []string{"8.47.69.0"}}

	base := config.NewSetConfig()
	base.DNS = config.DNSConfig{Enabled: true, DoHURL: "https://1.1.1.1/dns-query"}

	scoped := ds.scopeSetToDomains(&base, []string{"www.instagram.com", "meduza.io"})
	if pins := scoped.DNS.Pins["www.instagram.com"]; len(pins) != 1 {
		t.Fatalf("the scoped set must pin the blocked site, got %v", scoped.DNS.Pins)
	}
	if _, ok := scoped.DNS.Pins["meduza.io"]; ok {
		t.Error("a site with reachable addresses gets no pin")
	}
	if scoped.DNS.Enabled {
		t.Error("no site was poisoned, so the DoH redirect must go while the pins stay")
	}
	ibd := scoped.TCP.IPBlockDetect
	if !ibd.Enabled || !ibd.HealDNS || !ibd.SynDetect {
		t.Errorf("a set for an address-blocked site must watch for dead addresses: %+v", ibd)
	}

	other := ds.scopeSetToDomains(&base, []string{"meduza.io"})
	if other.TCP.IPBlockDetect.Enabled || len(other.DNS.Pins) != 0 {
		t.Errorf("a group without a blocked site is left alone: %+v %v", other.TCP.IPBlockDetect, other.DNS.Pins)
	}
}

func TestAlternativeScanRunsWheneverNothingServedTheSite(t *testing.T) {
	cases := []struct {
		name   string
		result *DNSDiscoveryResult
		want   bool
	}{
		{"system answer serves", &DNSDiscoveryResult{SystemServes: true}, false},
		{"reference serves, system poisoned", &DNSDiscoveryResult{IsPoisoned: true, ReferenceServes: true}, false},
		{"address blocked", &DNSDiscoveryResult{TransportBlocked: true}, true},
		{"both fail TLS in one subnet", &DNSDiscoveryResult{}, true},
		{"poisoned and reference fails TLS", &DNSDiscoveryResult{IsPoisoned: true}, true},
		{"no result", nil, false},
	}
	for _, tc := range cases {
		if got := shouldScanAlternatives(tc.result); got != tc.want {
			t.Errorf("%s: scan=%v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPlainFixSetIsNamedAfterItsStrategy(t *testing.T) {
	ds := altSuite(t, "meduza.io", &DNSDiscoveryResult{
		IsPoisoned: true, ReferenceServes: true, ExpectedIPs: []string{"8.47.69.0"}, BestDoHURL: "https://1.1.1.1/dns-query",
	})
	ds.discoveredDNS = config.DNSConfig{Enabled: true, DoHURL: "https://1.1.1.1/dns-query"}
	set := config.NewSetConfig()
	ds.storeResultsMulti(GetPhase1Presets()[0], map[string]CheckResult{
		"meduza.io": {Domain: "meduza.io", Status: CheckStatusComplete, Speed: 5000, UsedIP: "8.47.69.0", Set: &set},
	})
	fix := ds.domainResults["meduza.io"].Results[presetDNSRedirect]
	if fix == nil || fix.Set == nil || fix.Set.Name != presetDNSRedirect {
		t.Fatalf("the history shows the set's name as the strategy, so it must match the preset: %+v", fix)
	}
}
