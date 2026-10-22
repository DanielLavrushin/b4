package sni

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func rejecting(names ...string) (SetFilter, *int) {
	calls := 0
	return func(set *config.SetConfig) bool {
		calls++
		for _, n := range names {
			if set.Name == n {
				return false
			}
		}
		return true
	}, &calls
}

func scopedBlockSet(name string, ifaces ...string) *config.SetConfig {
	s := makeSet(name)
	s.Routing.Enabled = true
	s.Routing.Mode = config.RoutingModeBlock
	s.Routing.SourceInterfaces = ifaces
	return s
}

func scopedBlockSetFor(name, domain string) *config.SetConfig {
	s := makeSetWithDomains(name, domain)
	s.Routing.Enabled = true
	s.Routing.Mode = config.RoutingModeBlock
	s.Routing.SourceInterfaces = []string{"br-guest"}
	return s
}

func wantSet(t *testing.T, what string, matched bool, set *config.SetConfig, want string) {
	t.Helper()
	switch {
	case want == "" && matched:
		t.Errorf("%s: want no set, got %q", what, set.Name)
	case want != "" && !matched:
		t.Errorf("%s: want %q, got no set", what, want)
	case want != "" && set.Name != want:
		t.Errorf("%s: want %q, got %q", what, want, set.Name)
	}
}

func TestASetFilterPassesTheDomainToTheNextCandidate(t *testing.T) {
	block := makeSetWithDomains("block", "example.com")
	dpi := makeSetWithDomains("dpi", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{block, dpi})
	skipBlock, _ := rejecting("block")
	skipAll, _ := rejecting("block", "dpi")

	m, s := ss.MatchSNIWithSource("www.example.com", "")
	wantSet(t, "no filter", m, s, "block")
	m, s = ss.MatchSNIWithSource("www.example.com", "", nil)
	wantSet(t, "a nil filter", m, s, "block")
	m, s = ss.MatchSNIWithSource("www.example.com", "", skipBlock)
	wantSet(t, "the first set refused", m, s, "dpi")
	m, s = ss.MatchSNIWithSource("www.example.com", "", skipAll)
	wantSet(t, "every set refused", m, s, "")

	m, s = ss.MatchSNIWithSourceTLS("www.example.com", "", 0x0304, 4, skipBlock)
	wantSet(t, "with TLS and IP versions", m, s, "dpi")
}

func TestASetFilterIsAppliedLikeASourceDeviceMismatch(t *testing.T) {
	const mac = "aa:bb:cc:dd:ee:ff"
	scoped := makeSetWithDomains("scoped", "example.com")
	scoped.Targets.SourceDevices = []string{mac}
	open := makeSetWithDomains("open", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{open, scoped})
	skipScoped, _ := rejecting("scoped")

	m, s := ss.MatchSNIWithSource("example.com", mac)
	wantSet(t, "the device's own set", m, s, "scoped")
	m, s = ss.MatchSNIWithSource("example.com", mac, skipScoped)
	wantSet(t, "the device's own set refused", m, s, "open")
}

func TestASetFilterFallsBackFromADomainToARegex(t *testing.T) {
	block := makeSetWithDomains("block", "example.com")
	dpi := makeSetWithDomains("dpi", "regexp:^www\\.example\\.com$")
	ss := NewSuffixSet([]*config.SetConfig{block, dpi})
	skipBlock, _ := rejecting("block")

	m, s := ss.MatchSNIWithSource("www.example.com", "")
	wantSet(t, "no filter", m, s, "block")
	m, s = ss.MatchSNIWithSource("www.example.com", "", skipBlock)
	wantSet(t, "the domain set refused", m, s, "dpi")
}

func TestASetFilterPassesARegexMatchToTheNextRegex(t *testing.T) {
	block := makeSetWithDomains("block", `regexp:^www\.example\.com$`)
	dpi := makeSetWithDomains("dpi", `regexp:\.example\.com$`)
	ss := NewSuffixSet([]*config.SetConfig{block, dpi})
	skipBlock, _ := rejecting("block")
	skipAll, _ := rejecting("block", "dpi")

	m, s := ss.MatchSNIWithSourceTLS("www.example.com", "", 0x0304, 4)
	wantSet(t, "no filter", m, s, "block")
	m, s = ss.MatchSNIWithSourceTLS("www.example.com", "", 0x0304, 4, skipBlock)
	wantSet(t, "the first regex refused", m, s, "dpi")
	m, s = ss.MatchSNIWithSourceTLS("www.example.com", "", 0x0304, 4, skipAll)
	wantSet(t, "every regex refused", m, s, "")

	m, s = ss.MatchSNIForUnknownDevice("www.example.com")
	wantSet(t, "an unknown device without a filter", m, s, "block")
	m, s = ss.MatchSNIForUnknownDevice("www.example.com", skipBlock)
	wantSet(t, "an unknown device with the first regex refused", m, s, "dpi")
	m, s = ss.MatchSNIForUnknownDevice("www.example.com", skipAll)
	wantSet(t, "an unknown device with every regex refused", m, s, "")
}

func TestAScopedBlockSetWinsOnlyWhereItWouldHaveBeenChosen(t *testing.T) {
	const mac = "aa:bb:cc:dd:ee:ff"
	for _, c := range []struct {
		name string
		sets func() []*config.SetConfig
		host string
		mac  string
		want bool
	}{
		{"the scoped block set lists the name first", func() []*config.SetConfig {
			return []*config.SetConfig{scopedBlockSetFor("block", "example.com"), makeSetWithDomains("dpi", "example.com")}
		}, "www.example.com", "", true},
		{"another set lists the name first", func() []*config.SetConfig {
			return []*config.SetConfig{makeSetWithDomains("dpi", "example.com"), scopedBlockSetFor("block", "example.com")}
		}, "www.example.com", "", false},
		{"another set lists the name more closely", func() []*config.SetConfig {
			return []*config.SetConfig{scopedBlockSetFor("block", "example.com"), makeSetWithDomains("dpi", "www.example.com")}
		}, "www.example.com", "", false},
		{"only the block set's regex lists the name", func() []*config.SetConfig {
			return []*config.SetConfig{scopedBlockSetFor("block", `regexp:^www\.example\.com$`), makeSetWithDomains("dpi", `regexp:example`)}
		}, "www.example.com", "", true},
		{"a domain of another set comes before the block set's regex", func() []*config.SetConfig {
			return []*config.SetConfig{scopedBlockSetFor("block", `regexp:^www\.example\.com$`), makeSetWithDomains("dpi", "example.com")}
		}, "www.example.com", "", false},
		{"a name no set lists", func() []*config.SetConfig {
			return []*config.SetConfig{scopedBlockSetFor("block", "example.com")}
		}, "www.example.org", "", false},
		{"an empty name", func() []*config.SetConfig {
			return []*config.SetConfig{scopedBlockSetFor("block", "example.com")}
		}, "", "", false},
		{"an unscoped block set", func() []*config.SetConfig {
			block := scopedBlockSetFor("block", "example.com")
			block.Routing.SourceInterfaces = nil
			return []*config.SetConfig{block, makeSetWithDomains("dpi", "example.com"), scopedBlockSetFor("other", "other.example")}
		}, "www.example.com", "", false},
		{"a disabled scoped block set", func() []*config.SetConfig {
			block := scopedBlockSetFor("block", "example.com")
			block.Enabled = false
			return []*config.SetConfig{block, makeSetWithDomains("dpi", "example.com"), scopedBlockSetFor("other", "other.example")}
		}, "www.example.com", "", false},
		{"a block set for another device", func() []*config.SetConfig {
			block := scopedBlockSetFor("block", "example.com")
			block.Targets.SourceDevices = []string{mac}
			return []*config.SetConfig{block, makeSetWithDomains("dpi", "example.com")}
		}, "www.example.com", "11:22:33:44:55:66", false},
		{"a block set for this device", func() []*config.SetConfig {
			block := scopedBlockSetFor("block", "example.com")
			block.Targets.SourceDevices = []string{mac}
			return []*config.SetConfig{block, makeSetWithDomains("dpi", "example.com")}
		}, "www.example.com", mac, true},
	} {
		if got := NewSuffixSet(c.sets()).ScopedBlockSetWins(c.host, c.mac, 0, 0); got != c.want {
			t.Errorf("%s: ScopedBlockSetWins = %v, want %v", c.name, got, c.want)
		}
	}
	var none *SuffixSet
	if none.ScopedBlockSetWins("www.example.com", "", 0, 0) {
		t.Error("a nil matcher has no scoped block set")
	}
}

func TestAScopedBlockSetWinsByTheVersionsTheSelectionUsed(t *testing.T) {
	v6Block := scopedBlockSetFor("block", "example.com")
	v6Block.Targets.IPVersion = "6"
	tls12Block := scopedBlockSetFor("block", "example.com")
	tls12Block.Targets.TLSVersion = "1.2"
	v6DPI := makeSetWithDomains("v6", "example.com")
	v6DPI.Targets.IPVersion = "6"
	for _, c := range []struct {
		name string
		sets []*config.SetConfig
		tls  uint16
		ip   uint8
		want bool
	}{
		{"an IPv6 only block set and an IPv4 ClientHello", []*config.SetConfig{v6Block, makeSetWithDomains("dpi", "example.com")}, 0x0304, 4, false},
		{"an IPv6 only block set and an IPv6 ClientHello", []*config.SetConfig{v6Block, makeSetWithDomains("dpi", "example.com")}, 0x0304, 6, true},
		{"an IPv6 only block set and a DNS answer", []*config.SetConfig{v6Block, makeSetWithDomains("dpi", "example.com")}, 0, 0, true},
		{"a TLS 1.2 only block set and a TLS 1.3 ClientHello", []*config.SetConfig{tls12Block, makeSetWithDomains("dpi", "example.com")}, 0x0304, 4, false},
		{"a TLS 1.2 only block set and a TLS 1.2 ClientHello", []*config.SetConfig{tls12Block, makeSetWithDomains("dpi", "example.com")}, 0x0303, 4, true},
		{"an earlier IPv6 only set and an IPv4 ClientHello", []*config.SetConfig{v6DPI, scopedBlockSetFor("block", "example.com")}, 0x0304, 4, true},
		{"an earlier IPv6 only set and a DNS answer", []*config.SetConfig{v6DPI, scopedBlockSetFor("block", "example.com")}, 0, 0, false},
	} {
		if got := NewSuffixSet(c.sets).ScopedBlockSetWins("www.example.com", "", c.tls, c.ip); got != c.want {
			t.Errorf("%s: ScopedBlockSetWins = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestASetFilterPassesTheAddressToTheNextCandidate(t *testing.T) {
	block := makeSetWithIPs("block", "203.0.113.7")
	geo := makeSetWithIPs("geo", "203.0.113.0/24")
	ss := NewSuffixSet([]*config.SetConfig{block, geo})
	skipBlock, _ := rejecting("block")
	skipAll, _ := rejecting("block", "geo")
	ip := net.ParseIP("203.0.113.7")

	m, s := ss.MatchIPWithSource(ip, "")
	wantSet(t, "no filter", m, s, "block")
	m, s = ss.MatchIPWithSource(ip, "", nil)
	wantSet(t, "a nil filter", m, s, "block")
	m, s = ss.MatchIPWithSource(ip, "", skipBlock)
	wantSet(t, "the most specific set refused", m, s, "geo")
	m, s = ss.MatchIPWithSource(ip, "", skipAll)
	wantSet(t, "every set refused", m, s, "")
}

func TestASetFilterPassesALearnedAddressToTheDomainsNextSet(t *testing.T) {
	block := makeSetWithDomains("block", "example.com")
	dpi := makeSetWithDomains("dpi", "example.com")
	only := makeSetWithDomains("only", "only.example")
	ss := NewSuffixSet([]*config.SetConfig{block, dpi, only})
	ip := net.ParseIP("198.51.100.9")
	ss.LearnIPToDomain(ip, "www.example.com", block)
	skipBlock, _ := rejecting("block")

	m, s, domain := ss.MatchLearnedIPWithSource(ip, "")
	wantSet(t, "no filter", m, s, "block")
	m, s, _ = ss.MatchLearnedIPWithSource(ip, "", nil)
	wantSet(t, "a nil filter", m, s, "block")
	m, s, domain = ss.MatchLearnedIPWithSource(ip, "", skipBlock)
	wantSet(t, "the learned set refused", m, s, "dpi")
	if domain != "www.example.com" {
		t.Errorf("the learned domain must come back with the next set, got %q", domain)
	}

	other := net.ParseIP("198.51.100.10")
	ss.LearnIPToDomain(other, "only.example", only)
	skipOnly, _ := rejecting("only")
	m, s, _ = ss.MatchLearnedIPWithSource(other, "", skipOnly)
	wantSet(t, "the only set refused", m, s, "")
	m, s, _ = ss.MatchLearnedIPWithSource(other, "")
	wantSet(t, "the refusal is not remembered", m, s, "only")
}

func TestASetFilterPassesAPortToTheNextPortSet(t *testing.T) {
	first := makeSet("first")
	first.TCP.DPortFilter = "8443"
	first.UDP.DPortFilter = "5000"
	second := makeSet("second")
	second.TCP.DPortFilter = "8000-9000"
	second.UDP.DPortFilter = "4000-6000"
	ss := NewSuffixSet([]*config.SetConfig{first, second})
	skipFirst, calls := rejecting("first")

	m, s := ss.MatchTCPPort(8443, "")
	wantSet(t, "tcp without a filter", m, s, "first")
	m, s = ss.MatchTCPPort(8443, "", nil)
	wantSet(t, "tcp with a nil filter", m, s, "first")
	m, s = ss.MatchTCPPort(8443, "", skipFirst)
	wantSet(t, "tcp with the first set refused", m, s, "second")
	m, s = ss.MatchUDPPort(5000, "")
	wantSet(t, "udp without a filter", m, s, "first")
	m, s = ss.MatchUDPPort(5000, "", skipFirst)
	wantSet(t, "udp with the first set refused", m, s, "second")

	*calls = 0
	ss.MatchTCPPort(7000, "", skipFirst)
	if *calls != 0 {
		t.Errorf("a set whose ports do not match must not be offered to the filter, it was asked %d times", *calls)
	}
}

func TestASetFilterPassesAnL7PacketToTheNextL7Set(t *testing.T) {
	first := makeSet("first")
	first.UDP.DPortFilter = "50000-50100"
	first.UDP.FilterL7 = []string{config.UDPL7Discord}
	second := makeSet("second")
	second.UDP.DPortFilter = "50000-50100"
	second.UDP.FilterL7 = []string{config.UDPL7Discord}
	ss := NewSuffixSet([]*config.SetConfig{first, second})
	skipFirst, _ := rejecting("first")
	skipAll, _ := rejecting("first", "second")

	m, s := ss.MatchUDPL7(50005, "", config.UDPL7Discord, 4)
	wantSet(t, "no filter", m, s, "first")
	m, s = ss.MatchUDPL7(50005, "", config.UDPL7Discord, 4, nil)
	wantSet(t, "a nil filter", m, s, "first")
	m, s = ss.MatchUDPL7(50005, "", config.UDPL7Discord, 4, skipFirst)
	wantSet(t, "the first set refused", m, s, "second")
	m, s = ss.MatchUDPL7(50005, "", config.UDPL7Discord, 4, skipAll)
	wantSet(t, "every set refused", m, s, "")
}

func TestASetFilterPassesAnUnknownDeviceToTheNextSet(t *testing.T) {
	block := makeSetWithDomains("block", "example.com")
	dpi := makeSetWithDomains("dpi", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{block, dpi})
	skipBlock, _ := rejecting("block")

	m, s := ss.MatchSNIForUnknownDevice("example.com")
	wantSet(t, "no filter", m, s, "block")
	m, s = ss.MatchSNIForUnknownDevice("example.com", skipBlock)
	wantSet(t, "the first set refused", m, s, "dpi")

	regex := makeSetWithDomains("regex", "regexp:^example\\.com$")
	ss = NewSuffixSet([]*config.SetConfig{block, regex})
	m, s = ss.MatchSNIForUnknownDevice("example.com", skipBlock)
	wantSet(t, "a refused domain set falls back to a regex", m, s, "regex")
}

func TestSetMatchesSourceHonoursTheFilter(t *testing.T) {
	set := makeSet("block")
	skipBlock, _ := rejecting("block")
	if !SetMatchesSource(set, "") || !SetMatchesSource(set, "", nil) {
		t.Fatal("an unscoped set matches without a filter")
	}
	if SetMatchesSource(set, "", skipBlock) {
		t.Fatal("a set the filter refuses must not match")
	}
}

func TestSetFiltersAreJoined(t *testing.T) {
	set := makeSetWithDomains("block", "example.com")
	ss := NewSuffixSet([]*config.SetConfig{set})
	keepAll, first := rejecting()
	skipBlock, second := rejecting("block")
	if m, _ := ss.MatchSNIWithSource("example.com", "", keepAll, nil, skipBlock); m {
		t.Fatal("every filter must agree before a set is chosen")
	}
	if *first == 0 || *second == 0 {
		t.Fatalf("both filters must be asked, got %d and %d", *first, *second)
	}
	if m, _ := ss.MatchSNIWithSource("example.com", "", keepAll, nil); !m {
		t.Fatal("filters that keep the set must still match")
	}
}

func TestTheMatcherKnowsWhetherABlockSetIsScopedToAnInterface(t *testing.T) {
	interfaceSet := makeSetWithDomains("interface", "example.com")
	interfaceSet.Routing.Enabled = true
	interfaceSet.Routing.Mode = config.RoutingModeInterface
	interfaceSet.Routing.SourceInterfaces = []string{"br-guest"}

	disabled := scopedBlockSet("disabled", "br-guest")
	disabled.Enabled = false

	routingOff := scopedBlockSet("off", "br-guest")
	routingOff.Routing.Enabled = false

	for _, c := range []struct {
		name string
		sets []*config.SetConfig
		want bool
	}{
		{"a block set scoped to an interface", []*config.SetConfig{makeSet("plain"), scopedBlockSet("block", "br-guest")}, true},
		{"a block set with blank interfaces", []*config.SetConfig{scopedBlockSet("block", "", " ")}, false},
		{"an unscoped block set", []*config.SetConfig{scopedBlockSet("block")}, false},
		{"an interface set scoped to an interface", []*config.SetConfig{interfaceSet}, false},
		{"a disabled scoped block set", []*config.SetConfig{disabled}, false},
		{"a scoped block set with routing off", []*config.SetConfig{routingOff}, false},
		{"no sets", nil, false},
	} {
		if got := NewSuffixSet(c.sets).ScopesBlockSets(); got != c.want {
			t.Errorf("%s: ScopesBlockSets = %v, want %v", c.name, got, c.want)
		}
	}
	var none *SuffixSet
	if none.ScopesBlockSets() {
		t.Error("a nil matcher scopes nothing")
	}
}
