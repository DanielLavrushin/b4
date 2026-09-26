package sni

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func portSet(name, tcpPorts, udpPorts string) *config.SetConfig {
	s := makeSet(name)
	s.TCP.DPortFilter = tcpPorts
	s.UDP.DPortFilter = udpPorts
	return s
}

func TestPortMatchSkipsSetsWhoseDeclaredTargetsResolveToNothing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		declare func(*config.SetConfig)
	}{
		{"unresolved ASN", func(s *config.SetConfig) { s.Targets.ASNs = []string{"15169"} }},
		{"geoip category missing from the file", func(s *config.SetConfig) { s.Targets.GeoIpCategories = []string{"gone"} }},
		{"geosite file removed", func(s *config.SetConfig) { s.Targets.GeoSiteCategories = []string{"youtube"} }},
		{"manual IPs filtered by ip_version", func(s *config.SetConfig) { s.Targets.IPs = []string{"2001:db8::1"} }},
		{"domains that produced no entry", func(s *config.SetConfig) { s.Targets.SNIDomains = []string{"example.com"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := portSet("declared", "443", "443")
			tc.declare(s)
			if len(s.Targets.IpsToMatch) != 0 || len(s.Targets.DomainsToMatch) != 0 {
				t.Fatal("the fixture must resolve to nothing")
			}
			ss := NewSuffixSet([]*config.SetConfig{s})

			if matched, set := ss.MatchTCPPort(443, ""); matched {
				t.Errorf("a set with declared targets that resolve to nothing is not a TCP port-only set, got %q", set.Name)
			}
			if matched, set := ss.MatchUDPPort(443, ""); matched {
				t.Errorf("a set with declared targets that resolve to nothing is not a UDP port-only set, got %q", set.Name)
			}
		})
	}
}

func TestPortMatchUnresolvedSetDoesNotShadowARealPortOnlySet(t *testing.T) {
	unresolved := portSet("unresolved", "443", "443")
	unresolved.Targets.ASNs = []string{"15169"}
	portOnly := portSet("port-only", "443", "443")
	ss := NewSuffixSet([]*config.SetConfig{unresolved, portOnly})

	if matched, set := ss.MatchTCPPort(443, ""); !matched || set != portOnly {
		t.Errorf("expected the real port-only set on TCP, got matched=%v set=%v", matched, set)
	}
	if matched, set := ss.MatchUDPPort(443, ""); !matched || set != portOnly {
		t.Errorf("expected the real port-only set on UDP, got matched=%v set=%v", matched, set)
	}
}

func TestPortMatchSetWithoutDeclaredTargetsIsStillPortOnly(t *testing.T) {
	ss := NewSuffixSet([]*config.SetConfig{portSet("port-only", "8443", "5353")})

	if matched, set := ss.MatchTCPPort(8443, ""); !matched || set.Name != "port-only" {
		t.Errorf("a set with no declared targets and a TCP port filter must stay port-only, got matched=%v", matched)
	}
	if matched, set := ss.MatchUDPPort(5353, ""); !matched || set.Name != "port-only" {
		t.Errorf("a set with no declared targets and a UDP port filter must stay port-only, got matched=%v", matched)
	}
}

func TestPortMatchResolvedASNSetMatchesByAddressOnly(t *testing.T) {
	s := portSet("asn", "443", "443")
	s.Targets.ASNs = []string{"15169"}
	s.Targets.IpsToMatch = []string{"8.8.8.0/24"}
	ss := NewSuffixSet([]*config.SetConfig{s})

	if matched, _ := ss.MatchTCPPort(443, ""); matched {
		t.Error("a set with resolved ASN prefixes is matched by address, not as port-only")
	}
	if matched, set := ss.MatchIP(net.ParseIP("8.8.8.8")); !matched || set != s {
		t.Errorf("expected the resolved prefix to match by address, got matched=%v", matched)
	}
}

func TestPortMatchCodeBuiltSetWithOnlyCompiledIPsIsUnchanged(t *testing.T) {
	bridge := portSet("telegram-bridge", "443", "443")
	bridge.Targets.IpsToMatch = []string{"149.154.160.0/20"}
	if len(bridge.Targets.IPs) != 0 {
		t.Fatal("the fixture mimics a set built in code: compiled IPs, no declared IPs")
	}
	portOnly := portSet("port-only", "443", "443")
	ss := NewSuffixSet([]*config.SetConfig{bridge, portOnly})

	if matched, set := ss.MatchIP(net.ParseIP("149.154.167.51")); !matched || set != bridge {
		t.Errorf("the code-built set must keep matching its compiled addresses, got matched=%v", matched)
	}
	if matched, set := ss.MatchTCPPort(443, ""); !matched || set != portOnly {
		t.Errorf("the code-built set must not turn into a port-only set, got matched=%v set=%v", matched, set)
	}
}
