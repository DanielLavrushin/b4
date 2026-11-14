package sni

import (
	"net"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

var coverGeoSite = map[string][]string{
	"meta":      {"facebook.com", "fbcdn.net", "regexp:^ig-[a-z]+\\.example$"},
	"instagram": {"instagram.com", "cdninstagram.com"},
	"social":    {"facebook.com", "instagram.com", "cdninstagram.com"},
	"youtube":   {"youtube.com", "ytimg.com"},
}

var coverGeoIP = map[string][]string{
	"facebook": {"157.240.0.0/16", "2a03:2880::/32"},
	"meta-net": {"157.240.0.0/16", "2a03:2880::/32", "31.13.24.0/21"},
	"google":   {"142.250.0.0/15"},
}

var coverASN = map[string][]string{
	"32934": {"31.13.24.0/21"},
	"15169": {"8.8.8.0/24"},
}

type coverTargets struct {
	geosite []string
	geoip   []string
	asns    []string
	domains []string
	ips     []string
}

func coverSet(id string, t coverTargets) *config.SetConfig {
	set := &config.SetConfig{Id: id, Name: id, Enabled: true}
	set.Targets.GeoSiteCategories = t.geosite
	set.Targets.GeoIpCategories = t.geoip
	set.Targets.ASNs = t.asns
	set.Targets.SNIDomains = t.domains
	set.Targets.IPs = t.ips
	for _, category := range t.geosite {
		set.Targets.DomainsToMatch = append(set.Targets.DomainsToMatch, coverGeoSite[categoryKey(category)]...)
	}
	set.Targets.DomainsToMatch = append(set.Targets.DomainsToMatch, t.domains...)
	for _, category := range t.geoip {
		set.Targets.IpsToMatch = append(set.Targets.IpsToMatch, coverGeoIP[categoryKey(category)]...)
	}
	for _, asn := range t.asns {
		set.Targets.IpsToMatch = append(set.Targets.IpsToMatch, coverASN[asnKey(asn)]...)
	}
	set.Targets.IpsToMatch = append(set.Targets.IpsToMatch, t.ips...)
	return set
}

func expectCover(t *testing.T, got Cover, geosite, geoip, asns []string, idle bool) {
	t.Helper()
	if !slices.Equal(got.GeoSite, geosite) || !slices.Equal(got.GeoIP, geoip) || !slices.Equal(got.ASNs, asns) || got.Idle != idle {
		t.Errorf("cover = %+v, want geosite %v geoip %v asns %v idle %v", got, geosite, geoip, asns, idle)
	}
}

func TestCoverNamesTheCategoriesAnEarlierSetTakes(t *testing.T) {
	owner := coverSet("community", coverTargets{geosite: []string{"meta", "instagram"}, geoip: []string{"facebook"}, asns: []string{"AS32934"}})
	coverage := CoverageOf(owner)

	same := coverSet("local", coverTargets{geosite: []string{"Meta", " instagram "}, geoip: []string{"FACEBOOK"}, asns: []string{"32934"}})
	expectCover(t, coverage.Of(same), []string{"Meta", "instagram"}, []string{"FACEBOOK"}, []string{"32934"}, true)

	wider := coverSet("wider", coverTargets{geosite: []string{"meta", "youtube"}, geoip: []string{"facebook", "google"}, asns: []string{"32934", "15169"}})
	expectCover(t, coverage.Of(wider), []string{"meta"}, []string{"facebook"}, []string{"32934"}, false)

	other := coverSet("other", coverTargets{geosite: []string{"youtube"}})
	if got := coverage.Of(other); !got.Empty() {
		t.Errorf("a set that shares nothing with the owner must report nothing, got %+v", got)
	}
}

func TestCoverAgreesWithTheMatcher(t *testing.T) {
	owner := coverSet("community", coverTargets{geosite: []string{"meta", "instagram"}, geoip: []string{"facebook"}})
	local := coverSet("local", coverTargets{geosite: []string{"meta", "instagram"}, geoip: []string{"facebook"}})
	if !CoverageOf(owner).Of(local).Idle {
		t.Fatalf("the later set lists nothing of its own, so it must read as idle")
	}

	matcher := NewSuffixSet([]*config.SetConfig{owner, local})
	for _, host := range []string{"instagram.com", "static.cdninstagram.com", "facebook.com", "ig-feed.example"} {
		if matched, set := matcher.MatchSNIWithSource(host, "aa:bb:cc:00:00:01"); !matched || set.Id != owner.Id {
			t.Errorf("%s must match the earlier set, got %v", host, set)
		}
	}
	for _, addr := range []string{"157.240.1.1", "2a03:2880::1"} {
		if matched, set := matcher.MatchIPWithSource(net.ParseIP(addr), "aa:bb:cc:00:00:01"); !matched || set.Id != owner.Id {
			t.Errorf("%s must match the earlier set, got %v", addr, set)
		}
	}
}

func TestCoverCountsManualEntriesTheOwnerExpandsTo(t *testing.T) {
	owner := coverSet("community", coverTargets{geosite: []string{"instagram"}, geoip: []string{"facebook"}})
	coverage := CoverageOf(owner)

	listed := coverSet("listed", coverTargets{domains: []string{"Instagram.com.", "cdninstagram.com"}, ips: []string{"157.240.0.0/16"}})
	expectCover(t, coverage.Of(listed), nil, nil, nil, true)

	narrower := coverSet("narrower", coverTargets{domains: []string{"api.instagram.com"}})
	expectCover(t, coverage.Of(narrower), nil, nil, nil, false)

	subnet := coverSet("subnet", coverTargets{ips: []string{"157.240.1.0/24"}})
	expectCover(t, coverage.Of(subnet), nil, nil, nil, false)

	matcher := NewSuffixSet([]*config.SetConfig{owner, listed, narrower, subnet})
	if _, set := matcher.MatchSNIWithSource("instagram.com", ""); set == nil || set.Id != owner.Id {
		t.Errorf("an exact name the owner expands to must match the owner, got %v", set)
	}
	if _, set := matcher.MatchSNIWithSource("api.instagram.com", ""); set == nil || set.Id != narrower.Id {
		t.Errorf("a more specific name must stay with the set that lists it, got %v", set)
	}
	if _, set := matcher.MatchIPWithSource(net.ParseIP("157.240.1.9"), ""); set == nil || set.Id != subnet.Id {
		t.Errorf("a more specific network must stay with the set that lists it, got %v", set)
	}
}

func TestCoverReadsIdlenessFromExpandedTargets(t *testing.T) {
	owner := coverSet("community", coverTargets{geosite: []string{"social"}, geoip: []string{"meta-net"}})
	coverage := CoverageOf(owner)

	inside := coverSet("inside", coverTargets{geosite: []string{"social", "instagram"}, geoip: []string{"meta-net", "facebook"}})
	expectCover(t, coverage.Of(inside), []string{"social"}, []string{"meta-net"}, nil, true)

	partly := coverSet("partly", coverTargets{geosite: []string{"social", "meta"}})
	expectCover(t, coverage.Of(partly), []string{"social"}, nil, nil, false)

	pending := coverSet("pending", coverTargets{geosite: []string{"social"}, asns: []string{"13335"}})
	if len(pending.Targets.IpsToMatch) != 0 {
		t.Fatalf("the fixture must leave AS13335 without prefixes, got %v", pending.Targets.IpsToMatch)
	}
	expectCover(t, coverage.Of(pending), []string{"social"}, nil, nil, false)

	absent := coverSet("absent", coverTargets{geosite: []string{"social", "no-such-category"}})
	expectCover(t, coverage.Of(absent), []string{"social"}, nil, nil, true)

	matcher := NewSuffixSet([]*config.SetConfig{owner, inside, partly})
	for _, host := range inside.Targets.DomainsToMatch {
		if _, set := matcher.MatchSNIWithSource(host, ""); set == nil || set.Id != owner.Id {
			t.Errorf("%s: a category whose entries all sit in the owner's must match the owner, got %v", host, set)
		}
	}
	for _, addr := range []string{"157.240.1.1", "2a03:2880::1"} {
		if _, set := matcher.MatchIPWithSource(net.ParseIP(addr), ""); set == nil || set.Id != owner.Id {
			t.Errorf("%s: must match the owner, got %v", addr, set)
		}
	}
	if _, set := matcher.MatchSNIWithSource("static.fbcdn.net", ""); set == nil || set.Id != partly.Id {
		t.Errorf("a name outside the owner's categories must stay with the set that lists it, got %v", set)
	}
}

func TestCoverLeavesSetsTheOwnerCannotOutrank(t *testing.T) {
	targets := coverTargets{geosite: []string{"instagram"}}

	scoped := coverSet("scoped", targets)
	scoped.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	disabled := coverSet("disabled", targets)
	disabled.Enabled = false
	tls12 := coverSet("tls12", targets)
	tls12.Targets.TLSVersion = "1.2"
	ipv6 := coverSet("ipv6", targets)
	ipv6.Targets.IPVersion = "6"
	plain := coverSet("plain", targets)

	open := CoverageOf(coverSet("community", targets))
	for _, set := range []*config.SetConfig{scoped, disabled} {
		if got := open.Of(set); !got.Empty() {
			t.Errorf("%s: must report nothing, got %+v", set.Name, got)
		}
	}
	for _, set := range []*config.SetConfig{tls12, ipv6, plain} {
		expectCover(t, open.Of(set), []string{"instagram"}, nil, nil, true)
	}

	narrow := coverSet("community", targets)
	narrow.Targets.TLSVersion = "1.3"
	narrowed := CoverageOf(narrow)
	for _, set := range []*config.SetConfig{plain, tls12} {
		if got := narrowed.Of(set); !got.Empty() {
			t.Errorf("%s: an owner limited to TLS 1.3 leaves the other connections to it, got %+v", set.Name, got)
		}
	}
	tls13 := coverSet("tls13", targets)
	tls13.Targets.TLSVersion = "1.3"
	expectCover(t, narrowed.Of(tls13), []string{"instagram"}, nil, nil, true)

	matcher := NewSuffixSet([]*config.SetConfig{narrow, plain})
	if _, set := matcher.MatchSNIWithSourceTLS("instagram.com", "", 0x0303, 4); set == nil || set.Id != plain.Id {
		t.Errorf("a TLS 1.2 connection must fall to the set without a TLS filter, got %v", set)
	}

	for name, owner := range map[string]*config.SetConfig{"nil": nil, "disabled": disabled, "scoped": scoped} {
		if got := CoverageOf(owner).Of(plain); !got.Empty() {
			t.Errorf("%s owner: takes nothing, got %+v", name, got)
		}
	}
	if got := open.Of(open.owner); !got.Empty() {
		t.Errorf("a set takes nothing from itself, got %+v", got)
	}
}

func TestCoverKeepsSetsThatMatchWithoutDestinations(t *testing.T) {
	owner := coverSet("community", coverTargets{geosite: []string{"instagram"}})
	coverage := CoverageOf(owner)

	voice := coverSet("voice", coverTargets{geosite: []string{"instagram"}})
	voice.UDP.FilterL7 = []string{"stun"}
	expectCover(t, coverage.Of(voice), []string{"instagram"}, nil, nil, false)

	ports := coverSet("ports", coverTargets{})
	ports.TCP.DPortFilter = "443"
	if got := coverage.Of(ports); !got.Empty() {
		t.Errorf("a set matched by port alone loses nothing, got %+v", got)
	}

	empty := coverSet("empty", coverTargets{})
	if got := coverage.Of(empty); !got.Idle {
		t.Errorf("a set with no targets and no ports matches nothing, got %+v", got)
	}
	if got := coverage.Of(empty).Since(CoverageOf(nil).Of(empty)); !got.Empty() {
		t.Errorf("a set that matched nothing before is not news, got %+v", got)
	}
}

func TestCoverSinceReportsOnlyWhatChanged(t *testing.T) {
	before := Cover{GeoSite: []string{"meta"}, GeoIP: []string{"facebook"}, ASNs: []string{"AS32934"}}
	after := Cover{GeoSite: []string{"Meta", "instagram"}, GeoIP: []string{"facebook"}, ASNs: []string{"32934", "15169"}, Idle: true}
	expectCover(t, after.Since(before), []string{"instagram"}, nil, []string{"15169"}, true)

	if got := after.Since(after); !got.Empty() {
		t.Errorf("nothing changed, got %+v", got)
	}
	if !after.Since(Cover{}).Idle {
		t.Errorf("a set that was not idle before must be reported as idle now")
	}
}
