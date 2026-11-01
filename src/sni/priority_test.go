package sni

import (
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const (
	priorityMAC   = "aa:bb:cc:00:00:01"
	otherMAC      = "aa:bb:cc:00:00:02"
	priorityAddr  = "37.18.153.140"
	priorityRange = "37.18.153.136/29"
)

func idSet(id string) *config.SetConfig {
	s := makeSet(id)
	s.Id = id
	return s
}

func addressSet(id string, priority bool, ips ...string) *config.SetConfig {
	s := idSet(id)
	s.Targets.IpsToMatch = ips
	s.Targets.IPPriority = priority
	return s
}

func nameSet(id string, domains ...string) *config.SetConfig {
	s := idSet(id)
	s.Targets.DomainsToMatch = domains
	return s
}

func onDevices(s *config.SetConfig, macs ...string) *config.SetConfig {
	s.Targets.SourceDevices = macs
	return s
}

func scopedBlock(s *config.SetConfig) *config.SetConfig {
	s.Routing.Enabled = true
	s.Routing.Mode = config.RoutingModeBlock
	s.Routing.SourceInterfaces = []string{"br-guest"}
	return s
}

func configOf(sets ...*config.SetConfig) *config.Config {
	cfg := config.NewConfig()
	cfg.Sets = sets
	return &cfg
}

func copyOf(s *config.SetConfig) *config.SetConfig {
	c := *s
	return &c
}

func TestAddressHoldsTruthTable(t *testing.T) {
	top := nameSet("top", "googlevideo.com")
	devTop := onDevices(nameSet("dev-top", "googlevideo.com"), priorityMAC)
	ggc := addressSet("ggc", true, priorityRange)
	devGGC := onDevices(addressSet("dev-ggc", true, priorityRange), priorityMAC)
	gw := nameSet("gw", "googlevideo.com")
	devBelow := onDevices(nameSet("dev-below", "googlevideo.com"), priorityMAC)
	excluding := onDevices(nameSet("excluding", "googlevideo.com"), priorityMAC)
	excluding.Targets.SourceDevicesExclude = true
	off := addressSet("off", false, "37.18.0.0/16")
	disabled := nameSet("disabled", "googlevideo.com")
	disabled.Enabled = false
	unnamed := nameSet("", "googlevideo.com")

	ss := NewSuffixSet([]*config.SetConfig{top, devTop, ggc, devGGC, gw, devBelow, excluding, off, disabled, unnamed})
	stranger := addressSet("stranger", true, priorityRange)
	strangerName := nameSet("stranger-name", "googlevideo.com")

	cases := []struct {
		what   string
		addr   *config.SetConfig
		prefix int
		name   *config.SetConfig
		mac    string
		want   bool
	}{
		{"an address set above the name set holds", ggc, 29, gw, "", true},
		{"the same with a device that no set lists", ggc, 29, gw, otherMAC, true},
		{"the switch off never holds", off, 16, gw, "", false},
		{"a catch-all prefix never holds", ggc, 0, gw, "", false},
		{"an unknown prefix never holds", ggc, -1, gw, "", false},
		{"a name set above the address set wins", ggc, 29, top, "", false},
		{"a set does not hold against itself", ggc, 29, ggc, "", false},
		{"the same id under another pointer is the same set", ggc, 29, copyOf(ggc), "", false},
		{"a device-scoped name set below wins for its device", ggc, 29, devBelow, priorityMAC, false},
		{"a device-scoped name set below ranks by order for other devices", ggc, 29, devBelow, otherMAC, true},
		{"a device-scoped name set above wins for its device", ggc, 29, devTop, priorityMAC, false},
		{"a device-scoped address set beats a general name set above it", devGGC, 29, top, priorityMAC, true},
		{"two device-scoped sets: the name set above wins", devGGC, 29, devTop, priorityMAC, false},
		{"two device-scoped sets: the address set above holds", devGGC, 29, devBelow, priorityMAC, true},
		{"a device-scoped address set does not hold for a device it does not admit", devGGC, 29, gw, otherMAC, false},
		{"an exclude list that admits the device ranks first", ggc, 29, excluding, otherMAC, false},
		{"an exclude list that names the device ranks by order", ggc, 29, excluding, priorityMAC, true},
		{"an exclude list admits a device with no MAC", ggc, 29, excluding, "", false},
		{"a name set disabled when the matcher was built is unknown", ggc, 29, disabled, "", false},
		{"an address set the matcher never saw is unknown", stranger, 29, gw, "", false},
		{"a device-scoped address set against a name set the matcher never saw", devGGC, 29, strangerName, priorityMAC, false},
		{"a name set without an id is unknown", ggc, 29, unnamed, "", false},
		{"no name set", ggc, 29, nil, "", false},
		{"no address set", nil, 29, gw, "", false},
	}
	for _, c := range cases {
		if got := ss.AddressHolds(c.addr, c.prefix, c.name, c.mac); got != c.want {
			t.Errorf("%s: AddressHolds = %v, want %v", c.what, got, c.want)
		}
	}

	var none *SuffixSet
	if none.AddressHolds(ggc, 29, gw, "") {
		t.Error("a nil matcher must not hold")
	}
	if NewSuffixSet(nil).AddressHolds(ggc, 29, gw, "") {
		t.Error("an empty matcher knows no set and must not hold")
	}
}

func TestAddressHoldsOnTheReportersSets(t *testing.T) {
	var sets []*config.SetConfig
	for i := 0; i < 5; i++ {
		sets = append(sets, nameSet(fmt.Sprintf("filler-%d", i), fmt.Sprintf("site%d.example", i)))
	}
	ggc := addressSet("yt-ggc-ru", true, "79.133.77.32/27", "188.128.43.192/27", "213.59.213.96/24", priorityRange, "188.43.61.140/27")
	gw := nameSet("yt-gw-as15169", "googlevideo.com")
	rt := nameSet("yt-rt-v9", "googlevideo.com", "youtube.com", "ytimg.com")
	sets = append(sets, ggc, gw, rt)

	for _, on := range []bool{true, false} {
		ggc.Targets.IPPriority = on
		ss := NewSuffixSet(sets)

		matched, addr, prefix := ss.MatchIPWithSourceDetail(net.ParseIP(priorityAddr), otherMAC)
		if !matched || addr != ggc || prefix != 29 {
			t.Fatalf("switch %v: address match = %v %v /%d, want yt-ggc-ru /29", on, matched, setName(addr), prefix)
		}
		found, name := ss.MatchSNIWithSourceTLS("rr1---sn-nxu-nufl.googlevideo.com", otherMAC, 0x0304, 4)
		if !found || name != gw {
			t.Fatalf("switch %v: name match = %v %s, want yt-gw-as15169", on, found, setName(name))
		}
		if got := ss.AddressHolds(addr, prefix, name, otherMAC); got != on {
			t.Errorf("switch %v: yt-ggc-ru against yt-gw-as15169 holds = %v", on, got)
		}
		if got := ss.AddressHolds(addr, prefix, rt, otherMAC); got != on {
			t.Errorf("switch %v: yt-ggc-ru against yt-rt-v9 holds = %v", on, got)
		}
		if matched, set, _ := ss.MatchIPWithSourceDetail(net.ParseIP("173.194.0.10"), otherMAC); matched {
			t.Errorf("switch %v: a Google address outside the GGC ranges matched %s by address", on, setName(set))
		}
	}
}

func TestMatchIPWithSourceDetailAgreesWithMatchIPWithSource(t *testing.T) {
	devWide := onDevices(addressSet("dev-wide", false, "37.0.0.0/8"), priorityMAC)
	ggc := addressSet("ggc", true, priorityRange)
	both := addressSet("both", false, "79.0.0.0/8", "79.133.77.32/27")
	v6only := addressSet("v6-only", false, "188.128.43.192/27")
	v6only.Targets.IPVersion = "6"
	wideV4 := addressSet("wide-v4", false, "188.128.0.0/16")
	lonelyV6 := addressSet("lonely-v6", false, "213.59.213.0/24")
	lonelyV6.Targets.IPVersion = "6"
	rejected := addressSet("rejected", false, "188.43.61.128/27")
	fallback := addressSet("fallback", false, "188.43.0.0/16")
	v6 := addressSet("v6", true, "2a00:1450::/32")
	main := NewSuffixSet([]*config.SetConfig{devWide, ggc, both, v6only, wideV4, lonelyV6, rejected, fallback, v6})
	skipRejected, _ := rejecting("rejected")

	catchAll := addressSet("catch-all", true, "0.0.0.0/0", priorityRange)
	wide := NewSuffixSet([]*config.SetConfig{catchAll})

	cases := []struct {
		what   string
		ss     *SuffixSet
		ip     string
		mac    string
		keep   SetFilter
		want   string
		prefix int
	}{
		{"a device-scoped /8 goes before a general /29 for its device", main, priorityAddr, priorityMAC, nil, "dev-wide", 8},
		{"the general /29 for any other device", main, priorityAddr, otherMAC, nil, "ggc", 29},
		{"a set listed at /27 and at /8 matches through the /27", main, "79.133.77.40", "", nil, "both", 27},
		{"the version filter passes a /27 over to a /16", main, "188.128.43.200", "", nil, "wide-v4", 16},
		{"the version fallback keeps the only candidate", main, "213.59.213.100", "", nil, "lonely-v6", 24},
		{"a keep filter passes over to the next prefix", main, "188.43.61.140", "", skipRejected, "fallback", 16},
		{"an IPv6 range", main, "2a00:1450:4001::1", "", nil, "v6", 32},
		{"no address", main, "2001:db8::1", "", nil, "", -1},
		{"a catch-all matches through /0", wide, "8.8.8.8", "", nil, "catch-all", 0},
		{"a catch-all set matches through its narrower range", wide, priorityAddr, "", nil, "catch-all", 29},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		matched, set, prefix := c.ss.MatchIPWithSourceDetail(ip, c.mac, c.keep)
		plainMatched, plainSet := c.ss.MatchIPWithSource(ip, c.mac, c.keep)
		if matched != plainMatched || set != plainSet {
			t.Errorf("%s: detail = %v %s, plain = %v %s", c.what, matched, setName(set), plainMatched, setName(plainSet))
		}
		wantSet(t, c.what, matched, set, c.want)
		if prefix != c.prefix {
			t.Errorf("%s: prefix = %d, want %d", c.what, prefix, c.prefix)
		}
	}

	var none *SuffixSet
	if matched, set, prefix := none.MatchIPWithSourceDetail(net.ParseIP(priorityAddr), ""); matched || set != nil || prefix != -1 {
		t.Errorf("a nil matcher = %v %v %d", matched, set, prefix)
	}
	if matched, _, prefix := main.MatchIPWithSourceDetail(nil, ""); matched || prefix != -1 {
		t.Errorf("a nil address = %v %d", matched, prefix)
	}
}

func TestKeepsLearned(t *testing.T) {
	addr := net.ParseIP(priorityAddr)

	type fixture struct {
		what    string
		built   []*config.SetConfig
		cfg     []*config.SetConfig
		set     string
		ip      net.IP
		keep    bool
		learner *config.SetConfig
	}
	ggc := addressSet("ggc", true, priorityRange)
	gw := nameSet("gw", "googlevideo.com")
	ggcOff := addressSet("ggc", false, priorityRange)
	gwDev := onDevices(nameSet("gw-dev", "googlevideo.com"), priorityMAC)
	gwListing := nameSet("gw-listing", "googlevideo.com")
	gwListing.Targets.IpsToMatch = []string{priorityAddr + "/32"}
	devExact := onDevices(addressSet("dev-exact", false, priorityAddr+"/32"), priorityMAC)
	plainExact := addressSet("plain-exact", false, priorityAddr+"/32")
	catchAll := addressSet("catch-all", true, "0.0.0.0/0")
	catchAllNarrow := addressSet("catch-all-narrow", true, "0.0.0.0/0", priorityRange)
	ggcV6Only := addressSet("ggc", true, priorityRange)
	ggcV6Only.Targets.IPVersion = "6"
	blockExact := scopedBlock(addressSet("block-exact", true, priorityAddr+"/32"))
	ggc6 := addressSet("ggc6", true, "2a00:1450::/32")
	extra := nameSet("extra", "example.org")
	ggcDisabled := copyOf(ggc)
	ggcDisabled.Enabled = false
	ggcRenamed := addressSet("ggc-renamed", true, priorityRange)
	fresh := nameSet("fresh", "googlevideo.com")
	elsewhere := addressSet("elsewhere", true, "79.133.77.32/27")
	exact := addressSet("exact", false, priorityAddr+"/32")
	exactNowScoped := onDevices(copyOf(exact), priorityMAC)
	exactRenamed := addressSet("exact-renamed", false, priorityAddr+"/32")
	gwScopedSince := onDevices(copyOf(gw), priorityMAC)
	plain := addressSet("plain", false, priorityRange)
	ggcListed := addressSet("ggc", true, priorityRange)
	ggcListed.Targets.IPs = []string{priorityRange}
	ggcListed.Targets.ASNs = []string{"15169"}
	ggcListed.Targets.GeoIpCategories = []string{"ru"}
	ggcMovedIPs := copyOf(ggcListed)
	ggcMovedIPs.Targets.IPs = []string{"79.133.77.32/27"}
	ggcMovedIPs.Targets.IpsToMatch = []string{"79.133.77.32/27"}
	ggcMovedASNs := copyOf(ggcListed)
	ggcMovedASNs.Targets.ASNs = []string{"13335"}
	ggcMovedGeoIP := copyOf(ggcListed)
	ggcMovedGeoIP.Targets.GeoIpCategories = []string{"de"}

	cases := []fixture{
		{"a set above holds the address for everyone", []*config.SetConfig{ggc, gw}, nil, "gw", addr, false, nil},
		{"an address outside the held range", []*config.SetConfig{ggc, gw}, nil, "gw", net.ParseIP("37.18.154.1"), true, nil},
		{"the address set sits below the learning set", []*config.SetConfig{gw, ggc}, nil, "gw", addr, true, nil},
		{"the switch is off", []*config.SetConfig{ggcOff, gw}, nil, "gw", addr, true, nil},
		{"the learning set has source devices", []*config.SetConfig{ggc, gwDev}, nil, "gw-dev", addr, true, nil},
		{"the learning set is the best address match itself", []*config.SetConfig{ggc, gwListing}, nil, "gw-listing", addr, true, nil},
		{"a device-scoped longer prefix is passed over", []*config.SetConfig{devExact, ggc, gw}, nil, "gw", addr, false, nil},
		{"the best general match has the switch off", []*config.SetConfig{ggc, plainExact, gw}, nil, "gw", addr, true, nil},
		{"the first general set listing a range decides it: the switch on", []*config.SetConfig{ggc, plain, gw}, nil, "gw", addr, false, nil},
		{"the first general set listing a range decides it: the switch off", []*config.SetConfig{plain, ggc, gw}, nil, "gw", addr, true, nil},
		{"a catch-all never holds", []*config.SetConfig{catchAll, gw}, nil, "gw", net.ParseIP("8.8.8.8"), true, nil},
		{"a catch-all set holds through a narrower range", []*config.SetConfig{catchAllNarrow, gw}, nil, "gw", addr, false, nil},
		{"an IP version filter that does not fit", []*config.SetConfig{ggcV6Only, gw}, nil, "gw", addr, true, nil},
		{"a block set scoped to interfaces is passed over", []*config.SetConfig{blockExact, ggc, gw}, nil, "gw", addr, false, nil},
		{"a block set scoped to interfaces never holds for everyone", []*config.SetConfig{blockExact, gw}, nil, "gw", addr, true, nil},
		{"an IPv6 range", []*config.SetConfig{ggc6, gw}, nil, "gw", net.ParseIP("2a00:1450:4001::1"), false, nil},
		{"a matcher built from equal copies of the sets", []*config.SetConfig{ggc, gw}, []*config.SetConfig{copyOf(ggc), copyOf(gw)}, "gw", addr, false, nil},
		{"a learning set the matcher does not know", []*config.SetConfig{ggc, gw}, []*config.SetConfig{ggc, gw, fresh}, "fresh", addr, true, nil},
		{"a reordered config", []*config.SetConfig{ggc, gw}, []*config.SetConfig{gw, ggc}, "gw", addr, true, nil},
		{"a set inserted above both", []*config.SetConfig{ggc, gw}, []*config.SetConfig{extra, ggc, gw}, "gw", addr, true, nil},
		{"the switch turned off in the config", []*config.SetConfig{ggc, gw}, []*config.SetConfig{ggcOff, gw}, "gw", addr, true, nil},
		{"the switch turned on in the config before the matcher has it", []*config.SetConfig{ggcOff, gw}, []*config.SetConfig{ggc, gw}, "gw", addr, true, nil},
		{"the address set disabled in the config", []*config.SetConfig{ggc, gw}, []*config.SetConfig{ggcDisabled, gw}, "gw", addr, true, nil},
		{"the address set replaced in the config", []*config.SetConfig{ggc, gw}, []*config.SetConfig{ggcRenamed, gw}, "gw", addr, true, nil},
		{"the matcher still has the switch off on the address set", []*config.SetConfig{elsewhere, ggcOff, gw}, []*config.SetConfig{elsewhere, ggc, gw}, "gw", addr, true, nil},
		{"a longer prefix scoped to devices since the matcher was built", []*config.SetConfig{exact, ggc, gw}, []*config.SetConfig{exactNowScoped, ggc, gw}, "gw", addr, true, nil},
		{"a longer prefix whose set was replaced since the matcher was built", []*config.SetConfig{exact, ggc, gw}, []*config.SetConfig{exactRenamed, ggc, gw}, "gw", addr, true, nil},
		{"the caller's copy of the learning set has source devices", []*config.SetConfig{ggc, gw}, nil, "gw", addr, true, gwScopedSince},
		{"the config's copy of the learning set has source devices", []*config.SetConfig{ggc, gwScopedSince}, nil, "gw", addr, true, gw},
		{"the matcher's copy of the learning set has source devices", []*config.SetConfig{ggc, gwScopedSince}, []*config.SetConfig{ggc, gw}, "gw", addr, true, nil},
		{"the first set with the learning set's id decides its device scope", []*config.SetConfig{ggc, gw, gwScopedSince}, nil, "gw", addr, false, nil},
		{"the holder's IP targets changed since the matcher was built", []*config.SetConfig{ggcListed, gw}, []*config.SetConfig{ggcMovedIPs, gw}, "gw", addr, true, nil},
		{"the holder's ASN targets changed since the matcher was built", []*config.SetConfig{ggcListed, gw}, []*config.SetConfig{ggcMovedASNs, gw}, "gw", addr, true, nil},
		{"the holder's GeoIP targets changed since the matcher was built", []*config.SetConfig{ggcListed, gw}, []*config.SetConfig{ggcMovedGeoIP, gw}, "gw", addr, true, nil},
		{"the holder's address targets are the same in both copies", []*config.SetConfig{ggcListed, gw}, []*config.SetConfig{copyOf(ggcListed), gw}, "gw", addr, false, nil},
	}
	for _, c := range cases {
		ss := NewSuffixSet(c.built)
		sets := c.cfg
		if sets == nil {
			sets = c.built
		}
		cfg := configOf(sets...)
		learner := c.learner
		for _, s := range sets {
			if learner == nil && s.Id == c.set {
				learner = s
			}
		}
		if learner == nil {
			t.Fatalf("%s: no set %q in the config", c.what, c.set)
		}
		if got := ss.KeepsLearned(cfg, learner, c.ip); got != c.keep {
			t.Errorf("%s: KeepsLearned = %v, want %v", c.what, got, c.keep)
		}
	}
}

func TestKeepsLearnedFailsOpen(t *testing.T) {
	ggc := addressSet("ggc", true, priorityRange)
	gw := nameSet("gw", "googlevideo.com")
	cfg := configOf(ggc, gw)
	ss := NewSuffixSet(cfg.Sets)
	addr := net.ParseIP(priorityAddr)

	if ss.KeepsLearned(cfg, gw, addr) {
		t.Fatal("the fixture must filter the address before the fail-open cases mean anything")
	}
	builtWithASetAbove := NewSuffixSet([]*config.SetConfig{ggc, nameSet("extra", "example.org"), gw})

	var none *SuffixSet
	cases := []struct {
		what string
		keep bool
	}{
		{"a nil matcher (the pool has not started)", none.KeepsLearned(cfg, gw, addr)},
		{"an empty matcher", NewSuffixSet(nil).KeepsLearned(cfg, gw, addr)},
		{"a matcher of an empty list", NewSuffixSet([]*config.SetConfig{}).KeepsLearned(cfg, gw, addr)},
		{"a nil config", ss.KeepsLearned(nil, gw, addr)},
		{"a nil set", ss.KeepsLearned(cfg, nil, addr)},
		{"a nil address", ss.KeepsLearned(cfg, gw, nil)},
		{"a set without an id", ss.KeepsLearned(cfg, nameSet("", "googlevideo.com"), addr)},
		{"a config with no sets", ss.KeepsLearned(configOf(), gw, addr)},
		{"a config with a nil set", ss.KeepsLearned(configOf(nil, gw), gw, addr)},
		{"the learning set deleted from the config", ss.KeepsLearned(configOf(ggc), gw, addr)},
		{"a set above the learning set deleted from the config", builtWithASetAbove.KeepsLearned(cfg, gw, addr)},
	}
	for _, c := range cases {
		if !c.keep {
			t.Errorf("%s: the address must be kept", c.what)
		}
	}
}

func TestKeepsLearnedAllocatesNothingWhileNoSetHasTheSwitchOn(t *testing.T) {
	ggcOff := addressSet("ggc", false, priorityRange)
	gw := nameSet("gw", "googlevideo.com")
	cfg := configOf(ggcOff, gw)
	ss := NewSuffixSet(cfg.Sets)
	addr := net.ParseIP(priorityAddr)

	kept := true
	allocs := testing.AllocsPerRun(1000, func() {
		kept = ss.KeepsLearned(cfg, gw, addr) && kept
	})
	if !kept {
		t.Fatal("an address no set holds was dropped")
	}
	if allocs != 0 {
		t.Errorf("KeepsLearned allocates %v times per address while no set has the switch on, so every learned write pays for a lookup that cannot drop anything", allocs)
	}
}

func escalating(s *config.SetConfig, to string) *config.SetConfig {
	s.Escalate.To = to
	return s
}

func TestKeepsLearnedKeepsTheHoldersEscalationChain(t *testing.T) {
	addr := net.ParseIP(priorityAddr)
	holder := func(to string) *config.SetConfig {
		return escalating(addressSet("ggc", true, priorityRange), to)
	}
	hop := func(id, to string) *config.SetConfig {
		return escalating(nameSet(id, id+".invalid"), to)
	}
	gw := nameSet("gw", "googlevideo.com")
	disabledHop := hop("hop-1", "hop-2")
	disabledHop.Enabled = false
	chain := []*config.SetConfig{holder("hop-1"), gw}
	for i := 1; i <= 9; i++ {
		chain = append(chain, hop(fmt.Sprintf("hop-%d", i), fmt.Sprintf("hop-%d", i+1)))
	}

	cases := []struct {
		what  string
		built []*config.SetConfig
		cfg   []*config.SetConfig
		set   string
		keep  bool
	}{
		{"the holder's own escalation target", []*config.SetConfig{holder("backup"), gw, hop("backup", "")}, nil, "backup", true},
		{"a name set the holder does not escalate to", []*config.SetConfig{holder("backup"), gw, hop("backup", "")}, nil, "gw", false},
		{"the second hop of the chain", []*config.SetConfig{holder("hop-1"), gw, hop("hop-1", "hop-2"), hop("hop-2", "")}, nil, "hop-2", true},
		{"the first hop of a two-hop chain", []*config.SetConfig{holder("hop-1"), gw, hop("hop-1", "hop-2"), hop("hop-2", "")}, nil, "hop-1", true},
		{"a hop behind a disabled set", []*config.SetConfig{holder("hop-1"), gw, disabledHop, hop("hop-2", "")}, nil, "hop-2", false},
		{"a set the chain never reaches", []*config.SetConfig{holder("hop-1"), gw, hop("hop-1", ""), hop("hop-2", "")}, nil, "hop-2", false},
		{"a holder that escalates to a missing set", []*config.SetConfig{holder("gone"), gw}, nil, "gw", false},
		{"a set on a loop through the holder", []*config.SetConfig{holder("hop-1"), gw, hop("hop-1", "ggc")}, nil, "hop-1", true},
		{"a set off a loop through the holder", []*config.SetConfig{holder("hop-1"), gw, hop("hop-1", "ggc")}, nil, "gw", false},
		{"a set off a loop that never returns to the holder", []*config.SetConfig{holder("hop-1"), gw, hop("hop-1", "hop-2"), hop("hop-2", "hop-1")}, nil, "gw", false},
		{"the eighth hop", chain, nil, "hop-8", true},
		{"the ninth hop, past the cap", chain, nil, "hop-9", false},
		{"a chain the config has and the matcher does not", []*config.SetConfig{holder(""), gw, hop("backup", "")}, []*config.SetConfig{holder("backup"), gw, hop("backup", "")}, "backup", true},
		{"a chain the matcher has and the config does not", []*config.SetConfig{holder("backup"), gw, hop("backup", "")}, []*config.SetConfig{holder(""), gw, hop("backup", "")}, "backup", false},
	}
	for _, c := range cases {
		ss := NewSuffixSet(c.built)
		sets := c.cfg
		if sets == nil {
			sets = c.built
		}
		cfg := configOf(sets...)
		learner := cfg.GetSetById(c.set)
		if learner == nil {
			t.Fatalf("%s: no set %q in the config", c.what, c.set)
		}
		if got := ss.KeepsLearned(cfg, learner, addr); got != c.keep {
			t.Errorf("%s: KeepsLearned = %v, want %v", c.what, got, c.keep)
		}
	}
}

func TestASetWithATLSFilterNeverHolds(t *testing.T) {
	held, plainAddr := net.ParseIP(priorityAddr), net.ParseIP("79.133.77.40")
	ggcWith := func(tls string) *config.SetConfig {
		s := addressSet("ggc", true, priorityRange)
		s.Targets.TLSVersion = tls
		return s
	}
	plain := addressSet("plain", true, "79.133.77.32/27")
	gw := nameSet("gw", "googlevideo.com")

	for _, tls := range []string{"1.2", "1.3"} {
		cfg := configOf(ggcWith(tls), plain, gw)
		ss := NewSuffixSet(cfg.Sets)
		if ss.AddressHolds(cfg.Sets[0], 29, gw, "") {
			t.Errorf("filter %s: a set with a TLS version filter holds against the name set", tls)
		}
		if !ss.KeepsLearned(cfg, gw, held) {
			t.Errorf("filter %s: the kernel filter drops an address that a set with a TLS version filter lists", tls)
		}
		if !ss.AddressHolds(plain, 27, gw, "") {
			t.Errorf("filter %s: the set without a TLS version filter no longer holds", tls)
		}
		if ss.KeepsLearned(cfg, gw, plainAddr) {
			t.Errorf("filter %s: the set without a TLS version filter no longer keeps its address from the name set", tls)
		}
		if NewSuffixSet([]*config.SetConfig{ggcWith(tls), gw}).anyIPPriority {
			t.Errorf("filter %s: a matcher whose only switch-on set has a TLS version filter takes the slow path", tls)
		}
	}

	cases := []struct {
		what       string
		built, cfg []*config.SetConfig
	}{
		{"a TLS version filter added in the config since the matcher was built", []*config.SetConfig{ggcWith(""), plain, gw}, []*config.SetConfig{ggcWith("1.2"), plain, gw}},
		{"a TLS version filter the matcher has and the config no longer does", []*config.SetConfig{ggcWith("1.2"), plain, gw}, []*config.SetConfig{ggcWith(""), plain, gw}},
	}
	for _, c := range cases {
		cfg := configOf(c.cfg...)
		ss := NewSuffixSet(c.built)
		if !ss.KeepsLearned(cfg, gw, held) {
			t.Errorf("%s: the kernel filter drops the address", c.what)
		}
		if ss.KeepsLearned(cfg, gw, plainAddr) {
			t.Errorf("%s: the set without a TLS version filter no longer keeps its address from the name set", c.what)
		}
	}
}

func TestPriorityHelpersAreSafeForConcurrentReaders(t *testing.T) {
	ggc := addressSet("ggc", true, priorityRange)
	gw := nameSet("gw", "googlevideo.com")
	devBelow := onDevices(nameSet("dev-below", "googlevideo.com"), priorityMAC)
	cfg := configOf(ggc, gw, devBelow)
	ss := NewSuffixSet(cfg.Sets)
	addr := net.ParseIP(priorityAddr)

	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				ss.LearnIPToDomain(net.IPv4(37, 18, 153, byte(136+i%8)), "rr1---sn-nxu-nufl.googlevideo.com", gw)
				matched, set, prefix := ss.MatchIPWithSourceDetail(addr, otherMAC)
				if !matched || set != ggc || prefix != 29 {
					errs <- fmt.Sprintf("goroutine %d: address match %v %s /%d", g, matched, setName(set), prefix)
					return
				}
				if !ss.AddressHolds(set, prefix, gw, otherMAC) || ss.AddressHolds(set, prefix, devBelow, priorityMAC) {
					errs <- fmt.Sprintf("goroutine %d: AddressHolds changed its answer", g)
					return
				}
				if ss.KeepsLearned(cfg, gw, addr) {
					errs <- fmt.Sprintf("goroutine %d: KeepsLearned changed its answer", g)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
