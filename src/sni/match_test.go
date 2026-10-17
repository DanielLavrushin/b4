package sni

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func makeSet(name string) *config.SetConfig {
	s := config.NewSetConfig()
	s.Name = name
	s.Enabled = true
	return &s
}

func makeSetWithDomains(name string, domains ...string) *config.SetConfig {
	s := makeSet(name)
	s.Targets.DomainsToMatch = domains
	return s
}

func makeSetWithIPs(name string, ips ...string) *config.SetConfig {
	s := makeSet(name)
	s.Targets.IpsToMatch = ips
	return s
}

func makeSetWithSourceDevices(name string, macs ...string) *config.SetConfig {
	s := makeSet(name)
	s.Targets.SourceDevices = macs
	return s
}

// --- parsePortRange ---

func TestParsePortRange_SinglePort(t *testing.T) {
	set := makeSet("test")
	pr, ok := parsePortRange("443", set)
	if !ok {
		t.Fatal("expected ok")
	}
	if pr.min != 443 || pr.max != 443 {
		t.Errorf("got min=%d max=%d, want 443-443", pr.min, pr.max)
	}
}

func TestParsePortRange_Range(t *testing.T) {
	set := makeSet("test")
	pr, ok := parsePortRange("1000-2000", set)
	if !ok {
		t.Fatal("expected ok")
	}
	if pr.min != 1000 || pr.max != 2000 {
		t.Errorf("got min=%d max=%d, want 1000-2000", pr.min, pr.max)
	}
}

func TestParsePortRange_Invalid(t *testing.T) {
	set := makeSet("test")
	cases := []string{"", "abc", "-1", "2000-1000", "a-b"}
	for _, c := range cases {
		if _, ok := parsePortRange(c, set); ok {
			t.Errorf("expected not ok for %q", c)
		}
	}
}

func TestParsePortRange_Whitespace(t *testing.T) {
	set := makeSet("test")
	pr, ok := parsePortRange("  80  ", set)
	if !ok {
		t.Fatal("expected ok")
	}
	if pr.min != 80 || pr.max != 80 {
		t.Errorf("got min=%d max=%d, want 80-80", pr.min, pr.max)
	}
}

// --- NewSuffixSet construction ---

func TestNewSuffixSet_DisabledSetsSkipped(t *testing.T) {
	s := makeSetWithDomains("disabled", "example.com")
	s.Enabled = false

	ss := NewSuffixSet([]*config.SetConfig{s})
	if len(ss.sets) != 0 {
		t.Error("disabled set should not be added")
	}
}

func TestNewSuffixSet_DomainNormalization(t *testing.T) {
	s := makeSetWithDomains("test", "  Example.COM.  ")
	ss := NewSuffixSet([]*config.SetConfig{s})

	if _, ok := ss.sets["example.com"]; !ok {
		t.Error("domain should be lowercased and trimmed")
	}
}

func TestNewSuffixSet_EmptyDomainsSkipped(t *testing.T) {
	s := makeSetWithDomains("test", "", "  ", "valid.com")
	ss := NewSuffixSet([]*config.SetConfig{s})
	if len(ss.sets) != 1 {
		t.Errorf("expected 1 set entry, got %d", len(ss.sets))
	}
}

func TestNewSuffixSet_RegexDomains(t *testing.T) {
	s := makeSetWithDomains("test", "regexp:.*\\.google\\.com$")
	ss := NewSuffixSet([]*config.SetConfig{s})

	if len(ss.regexes) != 1 {
		t.Fatalf("expected 1 regex, got %d", len(ss.regexes))
	}
	if len(ss.sets) != 0 {
		t.Error("regex domain should not be in plain sets")
	}
}

func TestNewSuffixSet_DuplicateRegexSkipped(t *testing.T) {
	s := makeSetWithDomains("test", "regexp:.*\\.com$", "regexp:.*\\.com$")
	ss := NewSuffixSet([]*config.SetConfig{s})

	if len(ss.regexes) != 1 {
		t.Errorf("expected 1 regex (deduped), got %d", len(ss.regexes))
	}
}

func TestNewSuffixSet_IPv4(t *testing.T) {
	s := makeSetWithIPs("test", "1.2.3.4")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, set := ss.MatchIP(net.ParseIP("1.2.3.4"))
	if !matched || set.Name != "test" {
		t.Error("expected IP match")
	}
}

func TestNewSuffixSet_CIDR(t *testing.T) {
	s := makeSetWithIPs("test", "10.0.0.0/8")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchIP(net.ParseIP("10.1.2.3"))
	if !matched {
		t.Error("expected CIDR match")
	}

	matched, _ = ss.MatchIP(net.ParseIP("11.0.0.1"))
	if matched {
		t.Error("should not match outside CIDR")
	}
}

func TestNewSuffixSet_IPv6(t *testing.T) {
	s := makeSetWithIPs("test", "::1")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchIP(net.ParseIP("::1"))
	if !matched {
		t.Error("expected IPv6 match")
	}
}

func TestNewSuffixSet_PortRanges(t *testing.T) {
	s := makeSet("test")
	s.TCP.DPortFilter = "80,443,8000-9000"
	s.UDP.DPortFilter = "53,1000-2000"
	ss := NewSuffixSet([]*config.SetConfig{s})

	if len(ss.tcpPortRanges) != 3 {
		t.Errorf("expected 3 TCP port ranges, got %d", len(ss.tcpPortRanges))
	}
	if len(ss.udpPortRanges) != 2 {
		t.Errorf("expected 2 UDP port ranges, got %d", len(ss.udpPortRanges))
	}
}

// --- MatchSNI ---

func TestMatchSNI_ExactMatch(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, set := ss.MatchSNI("example.com")
	if !matched || set.Name != "test" {
		t.Error("expected exact domain match")
	}
}

func TestMatchSNI_SuffixMatch(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, set := ss.MatchSNI("www.example.com")
	if !matched || set.Name != "test" {
		t.Error("expected suffix match for subdomain")
	}

	matched, set = ss.MatchSNI("deep.sub.example.com")
	if !matched || set.Name != "test" {
		t.Error("expected suffix match for deep subdomain")
	}
}

func TestMatchSNI_CaseInsensitive(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchSNI("EXAMPLE.COM")
	if !matched {
		t.Error("expected case-insensitive match")
	}
}

func TestMatchSNI_NoMatch(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchSNI("notexample.com")
	if matched {
		t.Error("should not match different domain")
	}

	matched, _ = ss.MatchSNI("exampleXcom")
	if matched {
		t.Error("should not match without dots")
	}
}

func TestMatchSNI_EmptyHost(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchSNI("")
	if matched {
		t.Error("empty host should not match")
	}
}

func TestMatchSNI_NilSuffixSet(t *testing.T) {
	var ss *SuffixSet
	matched, _ := ss.MatchSNI("example.com")
	if matched {
		t.Error("nil SuffixSet should not match")
	}
}

func TestMatchSNI_RegexMatch(t *testing.T) {
	s := makeSetWithDomains("test", "regexp:^.*\\.google\\.com$")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, set := ss.MatchSNI("www.google.com")
	if !matched || set.Name != "test" {
		t.Error("expected regex match")
	}

	matched, _ = ss.MatchSNI("google.com")
	if matched {
		t.Error("regex requires subdomain, should not match bare domain")
	}
}

// --- MatchIP ---

func TestMatchIP_NilSuffixSet(t *testing.T) {
	var ss *SuffixSet
	matched, _ := ss.MatchIP(net.ParseIP("1.2.3.4"))
	if matched {
		t.Error("nil SuffixSet should not match")
	}
}

func TestMatchIP_NilIP(t *testing.T) {
	s := makeSetWithIPs("test", "1.2.3.4")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchIP(nil)
	if matched {
		t.Error("nil IP should not match")
	}
}

func TestMatchIP_MostSpecificWins(t *testing.T) {
	broad := makeSetWithIPs("broad", "10.0.0.0/8")
	specific := makeSetWithIPs("specific", "10.1.2.0/24")
	ss := NewSuffixSet([]*config.SetConfig{broad, specific})

	matched, set := ss.MatchIP(net.ParseIP("10.1.2.5"))
	if !matched {
		t.Fatal("expected match")
	}
	if set.Name != "specific" {
		t.Errorf("expected most specific (/24) match, got %s", set.Name)
	}
}

func TestMatchIP_Caching(t *testing.T) {
	s := makeSetWithIPs("test", "1.2.3.4")
	ss := NewSuffixSet([]*config.SetConfig{s})

	// First call populates cache
	ss.MatchIP(net.ParseIP("1.2.3.4"))
	// Second call should use cache
	matched, set := ss.MatchIP(net.ParseIP("1.2.3.4"))
	if !matched || set.Name != "test" {
		t.Error("cached result should still match")
	}

	// Negative result should also be cached
	ss.MatchIP(net.ParseIP("9.9.9.9"))
	matched, _ = ss.MatchIP(net.ParseIP("9.9.9.9"))
	if matched {
		t.Error("cached negative should not match")
	}
}

// --- MatchUDPPort / MatchTCPPort ---

func TestMatchUDPPort_GlobalOnly(t *testing.T) {
	// Set with NO IP/domain targets — should be matched as global
	s := makeSet("udp-global")
	s.UDP.DPortFilter = "53,1000-2000"
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, set := ss.MatchUDPPort(53, "")
	if !matched || set.Name != "udp-global" {
		t.Error("expected match on port 53")
	}

	matched, set = ss.MatchUDPPort(1500, "")
	if !matched || set.Name != "udp-global" {
		t.Error("expected match on port 1500 in range")
	}

	matched, _ = ss.MatchUDPPort(80, "")
	if matched {
		t.Error("should not match port 80")
	}
}

func TestMatchUDPPort_SkipsSetsWithTargets(t *testing.T) {
	s := makeSet("with-targets")
	s.UDP.DPortFilter = "53"
	s.Targets.DomainsToMatch = []string{"example.com"}
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchUDPPort(53, "")
	if matched {
		t.Error("should skip sets that have IP/domain targets")
	}
}

func TestMatchTCPPort_GlobalOnly(t *testing.T) {
	s := makeSet("tcp-global")
	s.TCP.DPortFilter = "80,443"
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, set := ss.MatchTCPPort(443, "")
	if !matched || set.Name != "tcp-global" {
		t.Error("expected match on port 443")
	}

	matched, _ = ss.MatchTCPPort(8080, "")
	if matched {
		t.Error("should not match port 8080")
	}
}

func TestMatchTCPPort_NilSuffixSet(t *testing.T) {
	var ss *SuffixSet
	matched, _ := ss.MatchTCPPort(443, "")
	if matched {
		t.Error("nil SuffixSet should not match")
	}
}

// --- LearnIPToDomain / MatchLearnedIP ---

func TestLearnedIP_BasicFlow(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})

	ip := net.ParseIP("1.2.3.4")
	ss.LearnIPToDomain(ip, "example.com", s)

	matched, set, domain := ss.MatchLearnedIP(ip)
	if !matched {
		t.Fatal("expected learned IP match")
	}
	if set.Name != "test" {
		t.Errorf("expected set 'test', got %s", set.Name)
	}
	if domain != "example.com" {
		t.Errorf("expected domain 'example.com', got %s", domain)
	}
}

func TestLearnedIP_DomainOnlySetIsNotLearned(t *testing.T) {
	s := makeSetWithDomains("domain-only", "shared-cdn.example.com")
	s.Targets.DomainOnly = true
	ss := NewSuffixSet([]*config.SetConfig{s})

	ip := net.ParseIP("1.2.3.4")
	ss.LearnIPToDomain(ip, "shared-cdn.example.com", s)

	if matched, _, _ := ss.MatchLearnedIP(ip); matched {
		t.Error("domain-only set must not create learned IP matches")
	}
	if matched, _, _ := ss.MatchLearnedIPWithSource(ip, ""); matched {
		t.Error("domain-only set must not create source-aware learned IP matches")
	}
}

func TestDomainOnly_StillMatchesBySNI(t *testing.T) {
	s := makeSetWithDomains("domain-only", "shared-cdn.example.com")
	s.Targets.DomainOnly = true
	ss := NewSuffixSet([]*config.SetConfig{s})

	if matched, set := ss.MatchSNI("shared-cdn.example.com"); !matched || set.Name != "domain-only" {
		t.Error("domain-only set must still match its exact domain by SNI")
	}
	if matched, set := ss.MatchSNI("sub.shared-cdn.example.com"); !matched || set.Name != "domain-only" {
		t.Error("domain-only set must still suffix-match subdomains by SNI")
	}
}

func TestLearnedIP_Update(t *testing.T) {
	s1 := makeSetWithDomains("set1", "a.com")
	s2 := makeSetWithDomains("set2", "b.com")
	ss := NewSuffixSet([]*config.SetConfig{s1, s2})

	ip := net.ParseIP("1.2.3.4")
	ss.LearnIPToDomain(ip, "a.com", s1)
	ss.LearnIPToDomain(ip, "b.com", s2) // update

	_, set, domain := ss.MatchLearnedIP(ip)
	if set.Name != "set2" || domain != "b.com" {
		t.Error("learned IP should be updated")
	}
}

func TestLearnedIP_NotFound(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _, _ := ss.MatchLearnedIP(net.ParseIP("9.9.9.9"))
	if matched {
		t.Error("unlearned IP should not match")
	}
}

func TestLearnedIP_NilGuards(t *testing.T) {
	var ss *SuffixSet
	ss.LearnIPToDomain(net.ParseIP("1.1.1.1"), "x.com", makeSet("x"))
	matched, _, _ := ss.MatchLearnedIP(net.ParseIP("1.1.1.1"))
	if matched {
		t.Error("nil SuffixSet should not match")
	}

	ss2 := NewSuffixSet(nil)
	ss2.LearnIPToDomain(nil, "x.com", makeSet("x"))
	ss2.LearnIPToDomain(net.ParseIP("1.1.1.1"), "", makeSet("x"))
	ss2.LearnIPToDomain(net.ParseIP("1.1.1.1"), "x.com", nil)
	// none of these should panic
}

func TestLearnedIP_TTLExpiry(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})
	ss.learnedIPTTL = 1 * time.Millisecond // very short TTL

	ip := net.ParseIP("1.2.3.4")
	ss.LearnIPToDomain(ip, "example.com", s)

	time.Sleep(5 * time.Millisecond)

	matched, _, _ := ss.MatchLearnedIP(ip)
	if matched {
		t.Error("expired learned IP should not match")
	}
}

func TestLearnedIP_CacheEviction(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})
	ss.learnedIPCacheLimit = 2

	ss.LearnIPToDomain(net.ParseIP("1.0.0.1"), "example.com", s)
	ss.LearnIPToDomain(net.ParseIP("1.0.0.2"), "example.com", s)
	ss.LearnIPToDomain(net.ParseIP("1.0.0.3"), "example.com", s) // evicts 1.0.0.1

	matched, _, _ := ss.MatchLearnedIP(net.ParseIP("1.0.0.1"))
	if matched {
		t.Error("evicted IP should not match")
	}

	matched, _, _ = ss.MatchLearnedIP(net.ParseIP("1.0.0.3"))
	if !matched {
		t.Error("most recent IP should still match")
	}
}

// --- MatchSNIWithSource ---

func TestMatchSNIWithSource_NoSourceFilter(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, set := ss.MatchSNIWithSource("example.com", "aa:bb:cc:dd:ee:ff")
	if !matched || set.Name != "test" {
		t.Error("set without source filter should match any MAC")
	}
}

func TestMatchSNIWithSource_SourceMatch(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	s.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchSNIWithSource("example.com", "aa:bb:cc:dd:ee:ff")
	if !matched {
		t.Error("expected match with correct MAC")
	}

	matched, _ = ss.MatchSNIWithSource("example.com", "11:22:33:44:55:66")
	if matched {
		t.Error("should not match with wrong MAC")
	}
}

func TestMatchSNIWithSource_PrefersSourceSpecific(t *testing.T) {
	generic := makeSetWithDomains("generic", "example.com")
	specific := makeSetWithDomains("specific", "example.com")
	specific.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	ss := NewSuffixSet([]*config.SetConfig{generic, specific})

	matched, set := ss.MatchSNIWithSource("example.com", "aa:bb:cc:dd:ee:ff")
	if !matched {
		t.Fatal("expected match")
	}
	if set.Name != "specific" {
		t.Errorf("expected source-specific set, got %s", set.Name)
	}
}

func TestMatchSNIWithSource_FallsBackToGeneric(t *testing.T) {
	generic := makeSetWithDomains("generic", "example.com")
	specific := makeSetWithDomains("specific", "example.com")
	specific.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	ss := NewSuffixSet([]*config.SetConfig{generic, specific})

	matched, set := ss.MatchSNIWithSource("example.com", "11:22:33:44:55:66")
	if !matched {
		t.Fatal("expected fallback match")
	}
	if set.Name != "generic" {
		t.Errorf("expected generic fallback, got %s", set.Name)
	}
}

func excludingSetWithDomains(name string, domains ...string) *config.SetConfig {
	s := makeSetWithDomains(name, domains...)
	s.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	s.Targets.SourceDevicesExclude = true
	return s
}

func setName(s *config.SetConfig) string {
	if s == nil {
		return "no set"
	}
	return s.Name
}

func TestMatchSNIForUnknownDevice_SkipsAnExcludeListAheadOfAGenericSet(t *testing.T) {
	ss := NewSuffixSet([]*config.SetConfig{excludingSetWithDomains("excluding", "example.com"), makeSetWithDomains("generic", "example.com")})

	if _, set := ss.MatchSNIWithSource("www.example.com", ""); setName(set) != "excluding" {
		t.Fatalf("the source path gave %s to a connection without a MAC, this test needs it to pick the exclude list", setName(set))
	}
	matched, set := ss.MatchSNIForUnknownDevice("www.example.com")
	if !matched {
		t.Fatal("expected the set without a device list to match")
	}
	if set.Name != "generic" {
		t.Errorf("expected the set without a device list, got %s", set.Name)
	}
}

func TestMatchSNIForUnknownDevice_IgnoresSetsWithADeviceList(t *testing.T) {
	including := makeSetWithDomains("including", "example.com", "example.org")
	including.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	ss := NewSuffixSet([]*config.SetConfig{including, excludingSetWithDomains("excluding", "example.com")})

	for _, host := range []string{"example.com", "example.org"} {
		if matched, set := ss.MatchSNIForUnknownDevice(host); matched {
			t.Errorf("%s matched %s: a set with a device list must not match a device b4 cannot identify", host, setName(set))
		}
	}
}

func TestMatchSNIForUnknownDevice_FallsThroughToARegexSet(t *testing.T) {
	ss := NewSuffixSet([]*config.SetConfig{
		excludingSetWithDomains("excluding", "example.com"),
		excludingSetWithDomains("excluding-regex", "regexp:^www\\.example\\.com$"),
		makeSetWithDomains("regex", "regexp:.*\\.example\\.com$"),
	})

	matched, set := ss.MatchSNIForUnknownDevice("www.example.com")
	if !matched {
		t.Fatal("expected the regex set without a device list to match")
	}
	if set.Name != "regex" {
		t.Errorf("expected the regex set without a device list, got %s", set.Name)
	}
}

func TestMatchSNIForUnknownDevice_ReachesAParentDomainPastASetWithADeviceList(t *testing.T) {
	for _, exclude := range []bool{true, false} {
		for _, devicesFirst := range []bool{true, false} {
			devices := excludingSetWithDomains("tv", "www.example.com")
			devices.Targets.SourceDevicesExclude = exclude
			generic := makeSetWithDomains("generic", "example.com")
			sets := []*config.SetConfig{generic, devices}
			if devicesFirst {
				sets = []*config.SetConfig{devices, generic}
			}
			ss := NewSuffixSet(sets)

			for _, host := range []string{"www.example.com", "a.www.example.com"} {
				if _, set := ss.MatchSNIForUnknownDevice(host); setName(set) != "generic" {
					t.Errorf("exclude=%v devicesFirst=%v %s: got %s, want the parent domain's set without a device list", exclude, devicesFirst, host, setName(set))
				}
			}
		}
	}
}

func TestMatchSNIForUnknownDevice_MatchesLikeAMatcherWithoutDeviceListSets(t *testing.T) {
	scoped := func(s *config.SetConfig, exclude bool) *config.SetConfig {
		s.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
		s.Targets.SourceDevicesExclude = exclude
		return s
	}
	invalidTLS := makeSetWithDomains("invalid-tls", "tls.example.com")
	invalidTLS.Targets.TLSVersion = "bogus"
	v6 := makeSetWithDomains("v6", "example.org")
	v6.Targets.IPVersion = "6"
	sets := []*config.SetConfig{
		scoped(makeSetWithDomains("tv-www", "www.example.com", "video.example.org"), true),
		scoped(makeSetWithDomains("tv-only", "example.com", "deep.a.example.net"), false),
		makeSetWithDomains("generic", "example.com", "regexp:.*\\.example\\.net$"),
		invalidTLS,
		v6,
		scoped(makeSetWithDomains("tv-regex", "regexp:^x\\.example\\.org$", "regexp:^y\\.example\\.edu$"), true),
		makeSetWithDomains("late", "www.example.com"),
		makeSetWithDomains("regex-copy", "regexp:^y\\.example\\.edu$"),
	}
	var withoutDevices []*config.SetConfig
	for _, s := range sets {
		if len(s.Targets.SourceDevices) == 0 {
			withoutDevices = append(withoutDevices, s)
		}
	}
	ss, oracle := NewSuffixSet(sets), NewSuffixSet(withoutDevices)

	for _, host := range []string{
		"example.com", "www.example.com", "a.www.example.com", "EXAMPLE.COM",
		"tls.example.com", "a.tls.example.com",
		"example.org", "video.example.org", "x.example.org",
		"a.example.net", "deep.a.example.net",
		"y.example.edu", "example.edu", "",
	} {
		wantMatched, want := oracle.MatchSNIWithSource(host, "")
		matched, set := ss.MatchSNIForUnknownDevice(host)
		if matched != wantMatched || set != want {
			t.Errorf("%q: got %v %s, a matcher without the sets that have a device list gives %v %s", host, matched, setName(set), wantMatched, setName(want))
		}
	}
}

func TestARegexInTwoSetsReachesTheSecondSetForADeviceTheFirstLeavesOut(t *testing.T) {
	first := makeSetWithDomains("tv", "regexp:(^|\\.)example\\.com$")
	first.Id = "tv"
	first.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	second := makeSetWithDomains("everyone", "regexp:(^|\\.)example\\.com$")
	second.Id = "everyone"
	ss := NewSuffixSet([]*config.SetConfig{first, second})

	if _, set := ss.MatchSNIWithSource("www.example.com", "aa:bb:cc:dd:ee:ff"); setName(set) != "tv" {
		t.Errorf("the listed device got %s, want the set limited to it", setName(set))
	}
	if _, set := ss.MatchSNIWithSource("www.example.com", "11:22:33:44:55:66"); setName(set) != "everyone" {
		t.Errorf("another device got %s, want the second set, which lists the same pattern", setName(set))
	}
	if _, set := ss.MatchSNI("www.example.com"); setName(set) != "tv" {
		t.Errorf("MatchSNI got %s, want the first set that lists the pattern", setName(set))
	}
	if !ss.SetHasDomain(second.Id, "www.example.com") {
		t.Error("the second set lists the pattern, so the name belongs to it too")
	}
}

func TestEachRegexPatternGetsItsOwnResultWithinALookup(t *testing.T) {
	netSet := makeSetWithDomains("net", "regexp:\\.example\\.net$")
	orgSet := makeSetWithDomains("org", "regexp:\\.example\\.org$")
	tvSet := makeSetWithDomains("org-tv", "regexp:\\.example\\.org$")
	tvSet.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	ss := NewSuffixSet([]*config.SetConfig{netSet, orgSet, tvSet})

	for name, got := range map[string]*config.SetConfig{
		"MatchSNI":                  matchedSet(ss.MatchSNI("www.example.org")),
		"MatchSNIWithSource":        matchedSet(ss.MatchSNIWithSource("www.example.org", "11:22:33:44:55:66")),
		"MatchSNIWithSource for TV": matchedSet(ss.MatchSNIWithSource("www.example.org", "aa:bb:cc:dd:ee:ff")),
		"MatchSNIForUnknownDevice":  matchedSet(ss.MatchSNIForUnknownDevice("www.example.org")),
	} {
		want := "org"
		if name == "MatchSNIWithSource for TV" {
			want = "org-tv"
		}
		if setName(got) != want {
			t.Errorf("%s got %s, want %s: the failed .net pattern decided the .org one", name, setName(got), want)
		}
	}
}

func TestARegexPastTheMemoBufferStillMatches(t *testing.T) {
	domains := make([]string, regexMemoSize+10)
	for i := range domains {
		domains[i] = fmt.Sprintf("regexp:^host%d\\.example\\.com$", i)
	}
	many := makeSetWithDomains("many", domains...)
	tv := makeSetWithDomains("tv", domains[len(domains)-1])
	tv.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	ss := NewSuffixSet([]*config.SetConfig{many, tv})
	host := fmt.Sprintf("host%d.example.com", len(domains)-1)

	if _, set := ss.MatchSNIWithSource(host, "aa:bb:cc:dd:ee:ff"); setName(set) != "tv" {
		t.Errorf("the TV got %s, want the set limited to it", setName(set))
	}
	if _, set := ss.MatchSNIWithSource(host, ""); setName(set) != "many" {
		t.Errorf("a device without a MAC got %s, want the set without a device list", setName(set))
	}
	if _, set := ss.MatchSNI(host); setName(set) != "many" {
		t.Errorf("MatchSNI got %s, want the first set that lists the pattern", setName(set))
	}
}

func matchedSet(_ bool, set *config.SetConfig) *config.SetConfig {
	return set
}

// --- MatchSNIWithSourceTLS ---

func TestMatchSNIWithSourceTLS_TLSFilter(t *testing.T) {
	s := makeSetWithDomains("tls12", "example.com")
	s.Targets.TLSVersion = "1.2"
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchSNIWithSourceTLS("example.com", "", 0x0303, 0)
	if !matched {
		t.Error("expected match for TLS 1.2")
	}
}

func TestMatchSNIWithSourceTLS_PrefersExactTLSMatch(t *testing.T) {
	tls12 := makeSetWithDomains("tls12", "example.com")
	tls12.Targets.TLSVersion = "1.2"
	tls13 := makeSetWithDomains("tls13", "example.com")
	tls13.Targets.TLSVersion = "1.3"
	ss := NewSuffixSet([]*config.SetConfig{tls12, tls13})

	_, set := ss.MatchSNIWithSourceTLS("example.com", "", 0x0304, 0)
	if set.Name != "tls13" {
		t.Errorf("expected tls13 set for TLS 1.3 client, got %s", set.Name)
	}

	_, set = ss.MatchSNIWithSourceTLS("example.com", "", 0x0303, 0)
	if set.Name != "tls12" {
		t.Errorf("expected tls12 set for TLS 1.2 client, got %s", set.Name)
	}
}

func TestMatchSNIWithSourceTLS_FallbackWhenNoTLSMatch(t *testing.T) {
	// selectSetBySourceAndTLS retries with tlsVersion=0 when no exact TLS match,
	// and MatchesTLSVersion(0) returns true — so it falls back to the set
	tls12 := makeSetWithDomains("tls12", "example.com")
	tls12.Targets.TLSVersion = "1.2"
	ss := NewSuffixSet([]*config.SetConfig{tls12})

	matched, set := ss.MatchSNIWithSourceTLS("example.com", "", 0x0304, 0)
	if !matched {
		t.Error("expected fallback match (retry with tlsVersion=0)")
	}
	if set.Name != "tls12" {
		t.Errorf("expected tls12 set as fallback, got %s", set.Name)
	}
}

func TestMatchSNIWithSourceTLS_ZeroTLSMatchesAny(t *testing.T) {
	s := makeSetWithDomains("tls12", "example.com")
	s.Targets.TLSVersion = "1.2"
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchSNIWithSourceTLS("example.com", "", 0, 0)
	if !matched {
		t.Error("tlsVersion=0 should match any set")
	}
}

// --- IP version filtering ---

func TestMatchSNIWithSourceTLS_IPVersionFilter(t *testing.T) {
	s := makeSetWithDomains("v6only", "example.com")
	s.Targets.IPVersion = "6"
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchSNIWithSourceTLS("example.com", "", 0, 6)
	if !matched {
		t.Error("expected match for IPv6 packet")
	}
}

func TestMatchSNIWithSourceTLS_PrefersExactIPVersion(t *testing.T) {
	v4 := makeSetWithDomains("v4", "example.com")
	v4.Targets.IPVersion = "4"
	v6 := makeSetWithDomains("v6", "example.com")
	v6.Targets.IPVersion = "6"
	ss := NewSuffixSet([]*config.SetConfig{v4, v6})

	_, set := ss.MatchSNIWithSourceTLS("example.com", "", 0, 4)
	if set == nil || set.Name != "v4" {
		t.Errorf("expected v4 set for IPv4 packet, got %v", set)
	}

	_, set = ss.MatchSNIWithSourceTLS("example.com", "", 0, 6)
	if set == nil || set.Name != "v6" {
		t.Errorf("expected v6 set for IPv6 packet, got %v", set)
	}
}

func TestMatchSNIWithSourceTLS_IPVersionFallback(t *testing.T) {
	v4 := makeSetWithDomains("v4", "example.com")
	v4.Targets.IPVersion = "4"
	ss := NewSuffixSet([]*config.SetConfig{v4})

	matched, set := ss.MatchSNIWithSourceTLS("example.com", "", 0, 6)
	if !matched || set == nil || set.Name != "v4" {
		t.Errorf("expected fallback to v4 set when only mismatched-version set exists, got %v", set)
	}
}

func TestMatchSNIWithSourceTLS_ZeroIPVersionMatchesAny(t *testing.T) {
	s := makeSetWithDomains("v6only", "example.com")
	s.Targets.IPVersion = "6"
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchSNIWithSourceTLS("example.com", "", 0, 0)
	if !matched {
		t.Error("ipVersion=0 should match any set")
	}
}

func TestMatchSNIWithSourceTLS_InvalidIPVersionNeverMatches(t *testing.T) {
	s := makeSetWithDomains("bad", "example.com")
	s.Targets.IPVersion = "ipv6" // invalid: should be "6"
	ss := NewSuffixSet([]*config.SetConfig{s})

	if matched, _ := ss.MatchSNIWithSourceTLS("example.com", "", 0, 4); matched {
		t.Error("invalid ip_version must not match, even via the ipVersion=0 fallback")
	}
}

func TestMatchIPWithSource_IPVersionDispatch(t *testing.T) {
	v4 := makeSetWithIPs("v4", "203.0.113.0/24")
	v4.Targets.IPVersion = "4"
	v6 := makeSetWithIPs("v6", "2001:db8::/32")
	v6.Targets.IPVersion = "6"
	ss := NewSuffixSet([]*config.SetConfig{v4, v6})

	if _, set := ss.MatchIPWithSource(net.ParseIP("203.0.113.5"), ""); set == nil || set.Name != "v4" {
		t.Errorf("expected v4 set for IPv4 destination, got %v", set)
	}
	if _, set := ss.MatchIPWithSource(net.ParseIP("2001:db8::1"), ""); set == nil || set.Name != "v6" {
		t.Errorf("expected v6 set for IPv6 destination, got %v", set)
	}
}

func TestListsPrivateTCPTargetNeedsAPrivateNetworkOnThePort(t *testing.T) {
	anyIP := makeSetWithIPs("any", "0.0.0.0/0", "::/0")
	host := makeSetWithIPs("host", "10.1.2.3/32")
	host.TCP.DPortFilter = "8443"
	lan := makeSetWithIPs("lan", "10.0.0.0/8")
	lan.TCP.DPortFilter = "443"
	device := makeSetWithIPs("device", "192.168.7.0/24")
	device.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:01"}
	v6only := makeSetWithIPs("v6only", "172.16.0.0/12")
	v6only.Targets.IPVersion = "6"
	cfg := config.Config{Sets: []*config.SetConfig{anyIP, host, lan, device, v6only}}
	cfg.BuildSetPortRanges()
	ss := NewSuffixSet(cfg.Sets)

	for _, c := range []struct {
		name string
		ip   string
		mac  string
		port uint16
		want bool
	}{
		{"the overlapping set that takes the port", "10.1.2.3", "", 443, true},
		{"the most specific set on its own port", "10.1.2.3", "", 8443, true},
		{"a port no listing set takes", "10.1.2.3", "", 80, false},
		{"an address only Match any IP address covers", "192.168.1.50", "", 443, false},
		{"a public address", "8.8.8.8", "", 443, false},
		{"a set for this device", "192.168.7.9", "AA:BB:CC:DD:EE:01", 443, true},
		{"a set for another device", "192.168.7.9", "AA:BB:CC:DD:EE:02", 443, false},
		{"a set for the other IP family", "172.16.0.9", "", 443, false},
		{"an IPv6 address only ::/0 covers", "fd00::5", "", 443, false},
	} {
		if got := ss.ListsPrivateTCPTarget(net.ParseIP(c.ip), c.mac, c.port); got != c.want {
			t.Errorf("%s: ListsPrivateTCPTarget(%s, %q, %d) = %v, want %v", c.name, c.ip, c.mac, c.port, got, c.want)
		}
	}
}

// --- MatchIPWithSource ---

func TestMatchIPWithSource_SourceFilter(t *testing.T) {
	s := makeSetWithIPs("test", "10.0.0.0/8")
	s.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchIPWithSource(net.ParseIP("10.1.2.3"), "aa:bb:cc:dd:ee:ff")
	if !matched {
		t.Error("expected match with correct MAC")
	}

	matched, _ = ss.MatchIPWithSource(net.ParseIP("10.1.2.3"), "11:22:33:44:55:66")
	if matched {
		t.Error("should not match with wrong MAC")
	}
}

func TestMatchIPWithSource_NilGuards(t *testing.T) {
	var ss *SuffixSet
	matched, _ := ss.MatchIPWithSource(net.ParseIP("1.2.3.4"), "")
	if matched {
		t.Error("nil SuffixSet should not match")
	}
}

// --- MatchLearnedIPWithSource ---

func TestMatchLearnedIPWithSource_SourceMatch(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	s.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	ss := NewSuffixSet([]*config.SetConfig{s})

	ip := net.ParseIP("1.2.3.4")
	ss.LearnIPToDomain(ip, "example.com", s)

	matched, _, _ := ss.MatchLearnedIPWithSource(ip, "aa:bb:cc:dd:ee:ff")
	if !matched {
		t.Error("expected match with correct MAC")
	}
}

func TestMatchLearnedIPWithSource_SourceMismatchFallback(t *testing.T) {
	generic := makeSetWithDomains("generic", "example.com")
	specific := makeSetWithDomains("specific", "example.com")
	specific.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	ss := NewSuffixSet([]*config.SetConfig{generic, specific})

	ip := net.ParseIP("1.2.3.4")
	ss.LearnIPToDomain(ip, "example.com", specific)

	// Wrong MAC but there's a generic set for the same domain
	matched, set, _ := ss.MatchLearnedIPWithSource(ip, "11:22:33:44:55:66")
	if !matched {
		t.Fatal("expected fallback match via MatchSNIWithSource")
	}
	if set.Name != "generic" {
		t.Errorf("expected generic set fallback, got %s", set.Name)
	}
}

func TestMatchLearnedIPWithSource_Expired(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})
	ss.learnedIPTTL = 1 * time.Millisecond

	ip := net.ParseIP("1.2.3.4")
	ss.LearnIPToDomain(ip, "example.com", s)
	time.Sleep(5 * time.Millisecond)

	matched, _, _ := ss.MatchLearnedIPWithSource(ip, "")
	if matched {
		t.Error("expired learned IP should not match")
	}
}

// --- TransferLearnedIPs ---

func TestTransferLearnedIPs(t *testing.T) {
	s1 := makeSetWithDomains("old-set", "example.com")
	old := NewSuffixSet([]*config.SetConfig{s1})
	old.LearnIPToDomain(net.ParseIP("1.2.3.4"), "example.com", s1)

	s2 := makeSetWithDomains("new-set", "example.com")
	newSuffix := NewSuffixSet([]*config.SetConfig{s2})

	newSuffix.TransferLearnedIPs(old)

	matched, set, domain := newSuffix.MatchLearnedIP(net.ParseIP("1.2.3.4"))
	if !matched {
		t.Fatal("expected transferred IP to match")
	}
	if set.Name != "new-set" {
		t.Errorf("expected new set, got %s", set.Name)
	}
	if domain != "example.com" {
		t.Errorf("expected domain preserved, got %s", domain)
	}
}

func TestTransferLearnedIPs_SkipsUnmatchedDomains(t *testing.T) {
	s1 := makeSetWithDomains("old-set", "old-domain.com")
	old := NewSuffixSet([]*config.SetConfig{s1})
	old.LearnIPToDomain(net.ParseIP("1.2.3.4"), "old-domain.com", s1)

	s2 := makeSetWithDomains("new-set", "new-domain.com")
	newSuffix := NewSuffixSet([]*config.SetConfig{s2})

	newSuffix.TransferLearnedIPs(old)

	matched, _, _ := newSuffix.MatchLearnedIP(net.ParseIP("1.2.3.4"))
	if matched {
		t.Error("domain not in new set should not transfer")
	}
}

func TestTransferLearnedIPs_NilGuards(t *testing.T) {
	var ss *SuffixSet
	ss.TransferLearnedIPs(nil) // should not panic

	ss2 := NewSuffixSet(nil)
	ss2.TransferLearnedIPs(nil) // should not panic
}

// --- setMatchesSource ---

func TestSetMatchesSource_NoFilter(t *testing.T) {
	s := makeSet("test")
	if !setMatchesSource(s, "aa:bb:cc:dd:ee:ff") {
		t.Error("no source filter should match any MAC")
	}
	if !setMatchesSource(s, "") {
		t.Error("no source filter should match empty MAC")
	}
}

func TestSetMatchesSource_WithFilter(t *testing.T) {
	s := makeSetWithSourceDevices("test", "AA:BB:CC:DD:EE:FF")

	if !setMatchesSource(s, "aa:bb:cc:dd:ee:ff") {
		t.Error("expected case-insensitive match")
	}
	if setMatchesSource(s, "11:22:33:44:55:66") {
		t.Error("should not match different MAC")
	}
	if setMatchesSource(s, "") {
		t.Error("should not match empty MAC when filter is set")
	}
}

func TestSetMatchesSource_Exclude(t *testing.T) {
	s := makeSetWithSourceDevices("test", "AA:BB:CC:DD:EE:FF")
	s.Targets.SourceDevicesExclude = true

	if setMatchesSource(s, "aa:bb:cc:dd:ee:ff") {
		t.Error("excluded MAC should not match")
	}
	if !setMatchesSource(s, "11:22:33:44:55:66") {
		t.Error("non-excluded MAC should match")
	}
	if !setMatchesSource(s, "") {
		t.Error("unknown source should match when filter is exclusive")
	}
}

// --- GetCacheStats ---

func TestGetCacheStats(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})

	stats := ss.GetCacheStats()
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats["ip_cache_limit"] != 2000 {
		t.Errorf("unexpected ip_cache_limit: %v", stats["ip_cache_limit"])
	}
	if stats["domain_cache_limit"] != 2000 {
		t.Errorf("unexpected domain_cache_limit: %v", stats["domain_cache_limit"])
	}
}

func TestGetCacheStats_Nil(t *testing.T) {
	var ss *SuffixSet
	stats := ss.GetCacheStats()
	if stats != nil {
		t.Error("nil SuffixSet should return nil stats")
	}
}

// --- selectSetBySourceAndTLS ---

func TestSelectSetBySourceAndTLS_NoMatchReturnsNil(t *testing.T) {
	matched, set := selectSetBySourceAndTLS(nil, "", 0, 0)
	if matched || set != nil {
		t.Error("empty candidates should not match")
	}
}

// --- Domain cache eviction ---

func TestDomainCache_Eviction(t *testing.T) {
	s := makeSetWithDomains("test", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})
	ss.domainCacheLimit = 2

	ss.MatchSNI("a.example.com")
	ss.MatchSNI("b.example.com")
	ss.MatchSNI("c.example.com") // should evict 'a'

	ss.domainCacheMu.RLock()
	_, aExists := ss.domainCache["a.example.com"]
	_, cExists := ss.domainCache["c.example.com"]
	ss.domainCacheMu.RUnlock()

	if aExists {
		t.Error("'a' should have been evicted")
	}
	if !cExists {
		t.Error("'c' should be in cache")
	}
}

func TestIPCache_Eviction(t *testing.T) {
	s := makeSetWithIPs("test", "10.0.0.0/8")
	ss := NewSuffixSet([]*config.SetConfig{s})
	ss.ipCacheLimit = 2

	ss.MatchIP(net.ParseIP("10.0.0.1"))
	ss.MatchIP(net.ParseIP("10.0.0.2"))
	ss.MatchIP(net.ParseIP("10.0.0.3")) // should evict 10.0.0.1

	ss.ipCacheMu.RLock()
	_, firstExists := ss.ipCache["10.0.0.1"]
	_, thirdExists := ss.ipCache["10.0.0.3"]
	ss.ipCacheMu.RUnlock()

	if firstExists {
		t.Error("first IP should have been evicted")
	}
	if !thirdExists {
		t.Error("third IP should be in cache")
	}
}

// --- Regex caching ---

func TestRegexCache(t *testing.T) {
	s := makeSetWithDomains("test", "regexp:.*\\.test\\.com$")
	ss := NewSuffixSet([]*config.SetConfig{s})

	// First call
	matched1, _ := ss.MatchSNI("www.test.com")
	// Second call (cached)
	matched2, _ := ss.MatchSNI("www.test.com")

	if !matched1 || !matched2 {
		t.Error("regex should match both times")
	}
}

// --- overflow map for domains claimed by more than one set ---

func TestNewSuffixSet_SingleSetDomainsStayOutOfOverflowMap(t *testing.T) {
	a := makeSetWithDomains("a", "one.example", "two.example")
	b := makeSetWithDomains("b", "three.example")
	ss := NewSuffixSet([]*config.SetConfig{a, b})

	if len(ss.sets) != 3 {
		t.Fatalf("sets = %d, want 3", len(ss.sets))
	}
	if len(ss.multiSets) != 0 {
		t.Fatalf("multiSets = %d, want 0 when no domain is shared", len(ss.multiSets))
	}
}

func TestNewSuffixSet_SharedDomainEntersOverflowMapInOrder(t *testing.T) {
	a := makeSetWithDomains("a", "shared.example", "only-a.example")
	b := makeSetWithDomains("b", "shared.example")
	c := makeSetWithDomains("c", "shared.example")
	ss := NewSuffixSet([]*config.SetConfig{a, b, c})

	if len(ss.multiSets) != 1 {
		t.Fatalf("multiSets = %d, want only the shared domain", len(ss.multiSets))
	}

	multi := ss.multiSets["shared.example"]
	if len(multi) != 3 {
		t.Fatalf("shared.example has %d sets, want 3", len(multi))
	}
	for i, want := range []string{"a", "b", "c"} {
		if multi[i].Name != want {
			t.Errorf("multiSets[%d] = %s, want %s", i, multi[i].Name, want)
		}
	}
	if ss.sets["shared.example"].Name != "a" {
		t.Errorf("sets should keep the first claimant, got %s", ss.sets["shared.example"].Name)
	}
}

func TestFindDomainCandidates_MostSpecificLevelWins(t *testing.T) {
	specific := makeSetWithDomains("specific", "api.example.com")
	parentA := makeSetWithDomains("parentA", "example.com")
	parentB := makeSetWithDomains("parentB", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{specific, parentA, parentB})

	single, multi := ss.findDomainCandidates("api.example.com")
	if single == nil || single.Name != "specific" {
		t.Fatalf("expected the specific single-set level to win, got single=%v multi=%v", single, multi)
	}

	single, multi = ss.findDomainCandidates("www.example.com")
	if single != nil {
		t.Fatalf("expected the shared parent level, got single=%s", single.Name)
	}
	if len(multi) != 2 {
		t.Fatalf("expected both parent sets, got %d", len(multi))
	}
}

func TestFindDomainCandidates_SharedSpecificBeatsSingleParent(t *testing.T) {
	parent := makeSetWithDomains("parent", "example.com")
	sharedA := makeSetWithDomains("sharedA", "api.example.com")
	sharedB := makeSetWithDomains("sharedB", "api.example.com")
	ss := NewSuffixSet([]*config.SetConfig{parent, sharedA, sharedB})

	single, multi := ss.findDomainCandidates("v2.api.example.com")
	if single != nil {
		t.Fatalf("expected the shared api level, got single=%s", single.Name)
	}
	if len(multi) != 2 || multi[0].Name != "sharedA" {
		t.Fatalf("expected [sharedA sharedB], got %v", multi)
	}
}

func TestFindDomainCandidates_NoMatch(t *testing.T) {
	ss := NewSuffixSet([]*config.SetConfig{makeSetWithDomains("a", "example.com")})

	single, multi := ss.findDomainCandidates("nothing.test")
	if single != nil || multi != nil {
		t.Fatalf("expected no candidates, got single=%v multi=%v", single, multi)
	}
}

func TestMatchSNIWithSourceTLS_SingleSetDomainStillMatches(t *testing.T) {
	s := makeSetWithDomains("solo", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, set := ss.MatchSNIWithSourceTLS("deep.sub.example.com", "aa:bb:cc:dd:ee:ff", 0x0303, 4)
	if !matched || set.Name != "solo" {
		t.Fatalf("expected suffix match on a single-set domain, got matched=%v set=%v", matched, set)
	}
}

func TestMatchSNIWithSourceTLS_SingleSetRespectsSourceFilter(t *testing.T) {
	s := makeSetWithDomains("solo", "example.com")
	s.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	ss := NewSuffixSet([]*config.SetConfig{s})

	if matched, _ := ss.MatchSNIWithSourceTLS("example.com", "aa:bb:cc:dd:ee:ff", 0, 0); !matched {
		t.Error("expected match for the listed device")
	}
	if matched, _ := ss.MatchSNIWithSourceTLS("example.com", "11:22:33:44:55:66", 0, 0); matched {
		t.Error("expected no match for an unlisted device")
	}
}

func TestMatchIP_EqualPrefixResolvesBySetOrder(t *testing.T) {
	first := makeSetWithIPs("first", "10.0.0.0/8", "192.168.0.0/16")
	second := makeSetWithIPs("second", "10.0.0.0/8", "192.168.0.0/16")

	s := NewSuffixSet([]*config.SetConfig{first, second})

	matched, set := s.MatchIP(net.ParseIP("10.1.2.3"))
	if !matched || set != first {
		t.Fatalf("MatchIP: matched=%v set=%v, want first", matched, set)
	}

	matched, set = s.MatchIPWithSource(net.ParseIP("192.168.1.1"), "")
	if !matched || set != first {
		t.Fatalf("MatchIPWithSource: matched=%v set=%v, want first", matched, set)
	}
}

func TestMatchIP_LongestPrefixBeatsSetOrder(t *testing.T) {
	broad := makeSetWithIPs("broad", "0.0.0.0/0")
	narrow := makeSetWithIPs("narrow", "1.2.3.0/24")

	s := NewSuffixSet([]*config.SetConfig{broad, narrow})

	matched, set := s.MatchIP(net.ParseIP("1.2.3.4"))
	if !matched || set != narrow {
		t.Fatalf("MatchIP: matched=%v set=%v, want narrow", matched, set)
	}

	matched, set = s.MatchIP(net.ParseIP("8.8.8.8"))
	if !matched || set != broad {
		t.Fatalf("MatchIP: matched=%v set=%v, want broad", matched, set)
	}
}

func TestMatchIPWithSource_DeviceSetWinsSharedCIDR(t *testing.T) {
	device := makeSetWithIPs("device", "10.0.0.0/8")
	device.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	general := makeSetWithIPs("general", "10.0.0.0/8")

	s := NewSuffixSet([]*config.SetConfig{device, general})

	matched, set := s.MatchIPWithSource(net.ParseIP("10.1.2.3"), "aa:bb:cc:dd:ee:ff")
	if !matched || set != device {
		t.Fatalf("device client: matched=%v set=%v, want device", matched, set)
	}

	matched, set = s.MatchIPWithSource(net.ParseIP("10.1.2.3"), "11:22:33:44:55:66")
	if !matched || set != general {
		t.Fatalf("other client: matched=%v set=%v, want general", matched, set)
	}
}
