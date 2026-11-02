package sni

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestNameSetsAreTheEnabledSetsThatNameADomain(t *testing.T) {
	ipOnly := makeSetWithIPs("ip-only", "1.2.3.4")
	badRegex := makeSetWithDomains("bad-regex", "regexp:(")
	disabled := makeSetWithDomains("disabled", "example.org")
	disabled.Enabled = false
	blank := makeSetWithDomains("blank", "", "  ")

	if NewSuffixSet([]*config.SetConfig{ipOnly, badRegex, disabled, blank}).NameSetCoversTCPPort(443) {
		t.Fatal("no enabled set names a domain, yet port 443 is covered by a name set")
	}
	for _, set := range []*config.SetConfig{
		makeSetWithDomains("domain", "example.org"),
		makeSetWithDomains("regex", "regexp:^www\\.example\\.org$"),
		makeSetWithDomains("repeated regex", "regexp:\\.org$", "regexp:\\.org$"),
	} {
		if !NewSuffixSet([]*config.SetConfig{ipOnly, set}).NameSetCoversTCPPort(443) {
			t.Errorf("set %q names a domain, so port 443 must be covered by a name set", set.Name)
		}
	}
}

func TestNameSetsFollowTheSetsTCPPorts(t *testing.T) {
	scoped := makeSetWithDomains("scoped", "example.org")
	scoped.TCPPortRanges = config.ParsePortRanges("8443")
	ss := NewSuffixSet([]*config.SetConfig{scoped})

	if ss.NameSetCoversTCPPort(443) {
		t.Error("the only name set listens on 8443, yet 443 is covered")
	}
	if !ss.NameSetCoversTCPPort(8443) {
		t.Error("the name set listens on 8443, yet 8443 is not covered")
	}

	open := makeSetWithDomains("open", "example.com")
	if !NewSuffixSet([]*config.SetConfig{scoped, open}).NameSetCoversTCPPort(443) {
		t.Error("a name set without a port filter covers every TCP port")
	}
}

func TestKeepNameSetsNarrowsTheNameSets(t *testing.T) {
	acts := makeSetWithDomains("acts", "example.org")
	passive := makeSetWithDomains("passive", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{passive, acts})

	ss.KeepNameSets(func(set *config.SetConfig) bool { return set == passive })
	if !ss.NameSetCoversTCPPort(443) {
		t.Fatal("a kept name set must still cover its ports")
	}
	ss.KeepNameSets(func(set *config.SetConfig) bool { return set == acts })
	if ss.NameSetCoversTCPPort(443) {
		t.Fatal("no name set is left after narrowing, yet port 443 is covered")
	}
	if matched, set := ss.MatchSNI("www.example.org"); !matched || set != acts {
		t.Fatal("narrowing the name sets must not change what the matcher matches")
	}

	var nilSet *SuffixSet
	nilSet.KeepNameSets(nil)
	if nilSet.NameSetCoversTCPPort(443) {
		t.Fatal("a nil matcher covers no port")
	}
}
