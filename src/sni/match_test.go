package sni

import (
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

	matched, set := ss.MatchUDPPort(53)
	if !matched || set.Name != "udp-global" {
		t.Error("expected match on port 53")
	}

	matched, set = ss.MatchUDPPort(1500)
	if !matched || set.Name != "udp-global" {
		t.Error("expected match on port 1500 in range")
	}

	matched, _ = ss.MatchUDPPort(80)
	if matched {
		t.Error("should not match port 80")
	}
}

func TestMatchUDPPort_SkipsSetsWithTargets(t *testing.T) {
	s := makeSet("with-targets")
	s.UDP.DPortFilter = "53"
	s.Targets.DomainsToMatch = []string{"example.com"}
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, _ := ss.MatchUDPPort(53)
	if matched {
		t.Error("should skip sets that have IP/domain targets")
	}
}

func TestMatchTCPPort_GlobalOnly(t *testing.T) {
	s := makeSet("tcp-global")
	s.TCP.DPortFilter = "80,443"
	ss := NewSuffixSet([]*config.SetConfig{s})

	matched, set := ss.MatchTCPPort(443)
	if !matched || set.Name != "tcp-global" {
		t.Error("expected match on port 443")
	}

	matched, _ = ss.MatchTCPPort(8080)
	if matched {
		t.Error("should not match port 8080")
	}
}

func TestMatchTCPPort_NilSuffixSet(t *testing.T) {
	var ss *SuffixSet
	matched, _ := ss.MatchTCPPort(443)
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
