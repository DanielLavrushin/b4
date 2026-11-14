package sni

import (
	"fmt"
	"math/rand"
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

func TestCoverCountsANetworkTiledBySmallerPrefixes(t *testing.T) {
	owner := coverSet("community", coverTargets{ips: []string{
		"203.0.113.0/25", "203.0.113.128/26", "203.0.113.192/27", "203.0.113.224/27", "203.0.113.200/30",
		"198.51.100.0/25", "198.51.100.128/26", "198.51.100.192/27", "198.51.100.224/28",
		"2001:db8::/33", "2001:db8:8000::/33",
		"10.0.0.0/8",
		"192.0.2.0/25",
	}})
	coverage := CoverageOf(owner)

	tiled := coverSet("tiled", coverTargets{ips: []string{"203.0.113.0/24", "2001:db8::/32", "203.0.113.128/25"}})
	expectCover(t, coverage.Of(tiled), nil, nil, nil, true)

	holed := coverSet("holed", coverTargets{ips: []string{"198.51.100.0/24"}})
	narrower := coverSet("narrower", coverTargets{ips: []string{"10.1.0.0/16"}})
	single := coverSet("single", coverTargets{ips: []string{"192.0.2.7"}})
	for _, set := range []*config.SetConfig{holed, narrower, single} {
		if got := coverage.Of(set); !got.Empty() {
			t.Errorf("%s: keeps addresses of its own, so it must report nothing, got %+v", set.Name, got)
		}
	}
	if got := CoverageOf(nil).Of(tiled); !got.Empty() {
		t.Errorf("without an owner nothing is taken, got %+v", got)
	}

	matcher := NewSuffixSet([]*config.SetConfig{owner, tiled, holed, narrower, single})
	owned := func(addr string) string {
		_, set := matcher.MatchIPWithSource(net.ParseIP(addr), "")
		if set == nil {
			return ""
		}
		return set.Id
	}
	for _, addr := range []string{"203.0.113.0", "203.0.113.127", "203.0.113.130", "203.0.113.201", "203.0.113.255", "2001:db8::1", "2001:db8:ffff::1"} {
		if got := owned(addr); got != owner.Id {
			t.Errorf("%s lies in a smaller prefix of the owner, so it must match the owner, got %q", addr, got)
		}
	}
	for addr, want := range map[string]string{
		"198.51.100.1":   owner.Id,
		"198.51.100.250": holed.Id,
		"10.1.2.3":       narrower.Id,
		"192.0.2.7":      single.Id,
	} {
		if got := owned(addr); got != want {
			t.Errorf("%s must match %q, got %q", addr, want, got)
		}
	}
}

func TestCoverAgreesWithTheMatcherOnRandomPrefixes(t *testing.T) {
	random := rand.New(rand.NewSource(564))
	prefix := func() (string, uint32, int) {
		ones := 24 + random.Intn(7)
		base := uint32(198)<<24 | uint32(18)<<16 | uint32(random.Intn(256))
		base &^= 1<<(32-ones) - 1
		return fmt.Sprintf("198.18.0.%d/%d", base&0xff, ones), base, 1 << (32 - ones)
	}

	idle, live := 0, 0
	for trial := 0; trial < 2000; trial++ {
		var listed []string
		for i := random.Intn(10); i > 0; i-- {
			entry, _, _ := prefix()
			listed = append(listed, entry)
		}
		entry, base, size := prefix()
		owner := coverSet("community", coverTargets{ips: listed})
		local := coverSet("local", coverTargets{ips: []string{entry}})

		matcher := NewSuffixSet([]*config.SetConfig{owner, local})
		kept := 0
		for i := 0; i < size; i++ {
			addr := base + uint32(i)
			_, set := matcher.MatchIPWithSource(net.IPv4(byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr)), "")
			if set != nil && set.Id == local.Id {
				kept++
			}
		}

		got := CoverageOf(owner).Of(local).Idle
		if got != (kept == 0) {
			t.Fatalf("owner %v, local %s: the matcher leaves the local set %d of %d addresses, idle reported as %v", listed, entry, kept, size, got)
		}
		if got {
			idle++
		} else {
			live++
		}
	}
	if idle < 50 || live < 50 {
		t.Fatalf("the trials must cover both outcomes, got %d idle and %d live", idle, live)
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

func TestCoverJoinAddsUpWhatTwoOwnersTook(t *testing.T) {
	first := Cover{GeoSite: []string{"meta"}, ASNs: []string{"AS32934"}}
	second := Cover{GeoSite: []string{"Meta", "instagram"}, GeoIP: []string{"facebook"}, ASNs: []string{"32934", "15169"}, Idle: true}
	expectCover(t, first.Join(second), []string{"meta", "instagram"}, []string{"facebook"}, []string{"AS32934", "15169"}, true)
	expectCover(t, first.Join(Cover{}), []string{"meta"}, nil, []string{"AS32934"}, false)
	if len(first.GeoSite) != 1 || len(second.GeoSite) != 2 {
		t.Errorf("joining must not change its operands: %+v %+v", first, second)
	}
}

func TestCoverSinceSkipsASetThatWasAlreadyIdle(t *testing.T) {
	older := coverSet("community", coverTargets{geosite: []string{"social"}})
	newer := coverSet("community", coverTargets{geosite: []string{"social", "instagram", "youtube"}})

	inside := coverSet("inside", coverTargets{geosite: []string{"social", "instagram"}})
	before, after := CoverageOf(older).Of(inside), CoverageOf(newer).Of(inside)
	if !before.Idle || !after.Idle {
		t.Fatalf("the set must handle nothing under either version: before %+v, after %+v", before, after)
	}
	if got := after.Since(before); !got.Empty() {
		t.Errorf("a set that handled nothing before the update lost nothing to it, got %+v", got)
	}

	wider := coverSet("wider", coverTargets{geosite: []string{"social", "youtube"}})
	expectCover(t, CoverageOf(newer).Of(wider).Since(CoverageOf(older).Of(wider)), []string{"youtube"}, nil, nil, true)

	still := coverSet("still", coverTargets{geosite: []string{"social", "youtube"}, domains: []string{"example.org"}})
	expectCover(t, CoverageOf(newer).Of(still).Since(CoverageOf(older).Of(still)), []string{"youtube"}, nil, nil, false)
}
