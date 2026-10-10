package tables

import (
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

func dscpPlanTestSet(id string, value int, targets ...string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Name = id
	set.DSCP = config.SetDSCPConfig{Enabled: true, Value: value}
	set.Targets.IPs = append([]string(nil), targets...)
	set.Targets.IpsToMatch = append([]string(nil), targets...)
	return &set
}

func dscpPlanTestConfig(sets ...*config.SetConfig) *config.Config {
	cfg := config.NewConfig()
	cfg.Sets = sets
	return &cfg
}

func dscpPlanTestRanges(ranges []dscpRange) []string {
	out := make([]string, len(ranges))
	for i, r := range ranges {
		out[i] = fmt.Sprintf("%s=%d", dscpRangeText(r), r.value)
	}
	return out
}

func dscpPlanTestKeys(prefixes []netip.Prefix) []string {
	out := make([]string, len(prefixes))
	for i, p := range prefixes {
		out[i] = dscpCanonicalKey(p)
	}
	return out
}

func dscpPlanTestCover(ranges []dscpRange) map[int][]string {
	out := map[int][]string{}
	for _, r := range ranges {
		out[r.value] = append(out[r.value], dscpPlanTestKeys(dscpRangeCover(r))...)
	}
	return out
}

func dscpPlanTestLookup(plan *dscpPlan, addr netip.Addr) (int, bool) {
	ranges := plan.static4
	if addr.Is6() {
		ranges = plan.static6
	}
	i, found := slices.BinarySearchFunc(ranges, addr, func(r dscpRange, a netip.Addr) int {
		switch {
		case r.to.Less(a):
			return -1
		case a.Less(r.from):
			return 1
		}
		return 0
	})
	if !found {
		return 0, false
	}
	return ranges[i].value, true
}

func dscpPlanTestExpect(t *testing.T, label string, got []dscpRange, want []string) {
	t.Helper()
	if g := dscpPlanTestRanges(got); !slices.Equal(g, want) {
		t.Errorf("%s ranges:\n got %q\nwant %q", label, g, want)
	}
}

func TestSetDSCPFlattenLPM(t *testing.T) {
	plan := dscpPlanFor(dscpPlanTestConfig(
		dscpPlanTestSet("a", 31, "10.0.0.0/8", "10.1.2.0/24", "fd00::/16"),
		dscpPlanTestSet("b", 6, "10.1.0.0/16", "fd00:1::/32"),
		dscpPlanTestSet("c", 25, "172.16.0.0/12"),
	))

	bounds := func(ranges []dscpRange) []string {
		out := make([]string, len(ranges))
		for i, r := range ranges {
			out[i] = fmt.Sprintf("%s-%s=%d", r.from, r.to, r.value)
		}
		return out
	}
	wantBounds4 := []string{
		"10.0.0.0-10.0.255.255=31",
		"10.1.0.0-10.1.1.255=6",
		"10.1.2.0-10.1.2.255=31",
		"10.1.3.0-10.1.255.255=6",
		"10.2.0.0-10.255.255.255=31",
		"172.16.0.0-172.31.255.255=25",
	}
	if got := bounds(plan.static4); !slices.Equal(got, wantBounds4) {
		t.Errorf("IPv4 bounds:\n got %q\nwant %q", got, wantBounds4)
	}
	dscpPlanTestExpect(t, "IPv4", plan.static4, []string{
		"10.0.0.0/16=31",
		"10.1.0.0/23=6",
		"10.1.2.0/24=31",
		"10.1.3.0-10.1.255.255=6",
		"10.2.0.0-10.255.255.255=31",
		"172.16.0.0/12=25",
	})
	dscpPlanTestExpect(t, "IPv6", plan.static6, []string{
		"fd00::/32=31",
		"fd00:1::/32=6",
		"fd00:2::-fd00:ffff:ffff:ffff:ffff:ffff:ffff:ffff=31",
	})

	wantCover := map[int][]string{
		31: {"10.0.0.0/16", "10.1.2.0/24", "10.2.0.0/15", "10.4.0.0/14", "10.8.0.0/13", "10.16.0.0/12", "10.32.0.0/11", "10.64.0.0/10", "10.128.0.0/9"},
		6:  {"10.1.0.0/23", "10.1.3.0/24", "10.1.4.0/22", "10.1.8.0/21", "10.1.16.0/20", "10.1.32.0/19", "10.1.64.0/18", "10.1.128.0/17"},
		25: {"172.16.0.0/12"},
	}
	if got := dscpPlanTestCover(plan.static4); !reflect.DeepEqual(got, wantCover) {
		t.Errorf("per-value ipset entries:\n got %v\nwant %v", got, wantCover)
	}
	if got, want := dscpPlanTestKeys(dscpUnionCover(plan.static4)), []string{"10.0.0.0/8", "172.16.0.0/12"}; !slices.Equal(got, want) {
		t.Errorf("union entries: got %v, want %v", got, want)
	}
	if !slices.Equal(plan.values, []int{6, 25, 31}) {
		t.Errorf("values: got %v, want [6 25 31]", plan.values)
	}
	if got := dscpRangeValues(plan.static6); !slices.Equal(got, []int{6, 31}) {
		t.Errorf("IPv6 values: got %v, want [6 31]", got)
	}

	for addr, want := range map[string]int{
		"10.0.0.9": 31, "10.1.0.5": 6, "10.1.2.5": 31, "10.1.9.9": 6, "10.3.0.1": 31, "172.16.1.1": 25, "10.250.0.1": 31,
		"fd00::5": 31, "fd00:1::5": 6, "fd00:5::1": 31,
	} {
		if got, ok := dscpPlanTestLookup(plan, netip.MustParseAddr(addr)); !ok || got != want {
			t.Errorf("%s: got DSCP %d (found %v), want %d", addr, got, ok, want)
		}
	}
	for _, addr := range []string{"9.255.255.255", "11.0.0.0", "172.32.0.0", "fd01::1"} {
		if got, ok := dscpPlanTestLookup(plan, netip.MustParseAddr(addr)); ok {
			t.Errorf("%s belongs to no set but got DSCP %d", addr, got)
		}
	}
}

func TestSetDSCPFlattenMatchesSetMatcher(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	values := []int{31, 6, 25, 7, 46}
	sets := make([]*config.SetConfig, len(values))
	for i, v := range values {
		sets[i] = dscpPlanTestSet(fmt.Sprintf("s%d", i), v)
	}
	for range 900 {
		set := sets[rng.IntN(len(sets))]
		v4 := netip.AddrFrom4([4]byte{10, byte(rng.IntN(4)), byte(rng.IntN(256)), byte(rng.IntN(256))})
		set.Targets.IpsToMatch = append(set.Targets.IpsToMatch, netip.PrefixFrom(v4, 12+rng.IntN(21)).Masked().String())
		var raw [16]byte
		copy(raw[:], netip.MustParseAddr("2001:db8::").AsSlice())
		raw[4], raw[5], raw[6], raw[7] = 0, byte(rng.IntN(4)), byte(rng.IntN(256)), byte(rng.IntN(256))
		v6 := netip.AddrFrom16(raw)
		set.Targets.IpsToMatch = append(set.Targets.IpsToMatch, netip.PrefixFrom(v6, 44+rng.IntN(21)).Masked().String())
	}
	cfg := dscpPlanTestConfig(sets...)
	plan := dscpPlanFor(cfg)
	matcher := sni.NewSuffixSet(cfg.Sets)

	var probes []netip.Addr
	for _, r := range slices.Concat(plan.static4, plan.static6) {
		probes = append(probes, r.from, r.to, r.from.Prev(), r.to.Next())
	}
	for range 4000 {
		probes = append(probes, netip.AddrFrom4([4]byte{10, byte(rng.IntN(5)), byte(rng.IntN(256)), byte(rng.IntN(256))}))
	}
	if len(plan.static4) < 50 || len(plan.static6) < 50 {
		t.Fatalf("the random targets should overlap into many ranges, got %d IPv4 and %d IPv6", len(plan.static4), len(plan.static6))
	}
	for _, addr := range probes {
		if !addr.IsValid() {
			continue
		}
		matched, set := matcher.MatchIPWithSource(net.IP(addr.AsSlice()), "")
		value, ok := dscpPlanTestLookup(plan, addr)
		switch {
		case matched != ok:
			t.Fatalf("%s: the set matcher says matched=%v, the plan says %v", addr, matched, ok)
		case ok && set.DSCP.Value != value:
			t.Fatalf("%s: the set matcher picks %s (DSCP %d), the plan stamps %d", addr, set.Id, set.DSCP.Value, value)
		}
	}
}

func TestSetDSCPFlattenTieListOrder(t *testing.T) {
	first := dscpPlanTestSet("first", 31, "10.0.0.77/24", "192.0.2.7", "2001:db8::/48")
	second := dscpPlanTestSet("second", 6, "10.0.0.0/24", "10.0.0.0/25", "192.0.2.7/32", "2001:db8::1/48")

	plan := dscpPlanFor(dscpPlanTestConfig(first, second))
	dscpPlanTestExpect(t, "first listed first, IPv4", plan.static4, []string{"10.0.0.0/25=6", "10.0.0.128/25=31", "192.0.2.7=31"})
	dscpPlanTestExpect(t, "first listed first, IPv6", plan.static6, []string{"2001:db8::/48=31"})

	plan = dscpPlanFor(dscpPlanTestConfig(second, first))
	dscpPlanTestExpect(t, "second listed first, IPv4", plan.static4, []string{"10.0.0.0/24=6", "192.0.2.7=6"})
	dscpPlanTestExpect(t, "second listed first, IPv6", plan.static6, []string{"2001:db8::/48=6"})
}

func TestSetDSCPFlattenMergeAdjacent(t *testing.T) {
	cases := []struct {
		name  string
		sets  []*config.SetConfig
		want  []string
		cover map[int][]string
	}{
		{"two sets with one value", []*config.SetConfig{
			dscpPlanTestSet("a", 31, "10.0.0.0/25"), dscpPlanTestSet("b", 31, "10.0.0.128/25"),
		}, []string{"10.0.0.0/24=31"}, map[int][]string{31: {"10.0.0.0/24"}}},
		{"two sets with two values", []*config.SetConfig{
			dscpPlanTestSet("a", 31, "10.0.0.0/25"), dscpPlanTestSet("b", 6, "10.0.0.128/25"),
		}, []string{"10.0.0.0/25=31", "10.0.0.128/25=6"}, map[int][]string{31: {"10.0.0.0/25"}, 6: {"10.0.0.128/25"}}},
		{"one set with touching prefixes", []*config.SetConfig{
			dscpPlanTestSet("a", 31, "10.0.1.0/24", "10.0.0.0/24", "10.0.2.0/23"),
		}, []string{"10.0.0.0/22=31"}, map[int][]string{31: {"10.0.0.0/22"}}},
		{"inner prefix with the outer value", []*config.SetConfig{
			dscpPlanTestSet("a", 31, "10.0.0.0/8"), dscpPlanTestSet("b", 31, "10.1.0.0/16"),
		}, []string{"10.0.0.0/8=31"}, map[int][]string{31: {"10.0.0.0/8"}}},
		{"a gap keeps two ranges", []*config.SetConfig{
			dscpPlanTestSet("a", 31, "10.0.0.0/24"), dscpPlanTestSet("b", 31, "10.0.2.0/24"),
		}, []string{"10.0.0.0/24=31", "10.0.2.0/24=31"}, map[int][]string{31: {"10.0.0.0/24", "10.0.2.0/24"}}},
		{"a run that is no single prefix", []*config.SetConfig{
			dscpPlanTestSet("a", 31, "10.0.0.0/24", "10.0.1.0/25"),
		}, []string{"10.0.0.0-10.0.1.127=31"}, map[int][]string{31: {"10.0.0.0/24", "10.0.1.0/25"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := dscpPlanFor(dscpPlanTestConfig(tc.sets...))
			dscpPlanTestExpect(t, "IPv4", plan.static4, tc.want)
			if got := dscpPlanTestCover(plan.static4); !reflect.DeepEqual(got, tc.cover) {
				t.Errorf("ipset entries: got %v, want %v", got, tc.cover)
			}
		})
	}
}

func TestSetDSCPFlattenZeroPrefix(t *testing.T) {
	noZero := func(t *testing.T, label string, prefixes []netip.Prefix) {
		t.Helper()
		for _, p := range prefixes {
			if p.Bits() == 0 {
				t.Errorf("%s: %s is a /0, which hash:net refuses", label, p)
			}
		}
	}

	plan := dscpPlanFor(dscpPlanTestConfig(dscpPlanTestSet("all", 25, "0.0.0.0/0", "::/0")))
	dscpPlanTestExpect(t, "IPv4 /0", plan.static4, []string{"0.0.0.0/1=25", "128.0.0.0/1=25"})
	dscpPlanTestExpect(t, "IPv6 /0", plan.static6, []string{"::/1=25", "8000::/1=25"})
	for _, r := range slices.Concat(plan.static4, plan.static6) {
		noZero(t, "range cover", dscpRangeCover(r))
	}
	if got := dscpPlanTestKeys(dscpUnionCover(plan.static4)); !slices.Equal(got, []string{"0.0.0.0/1", "128.0.0.0/1"}) {
		t.Errorf("IPv4 union entries: got %v", got)
	}
	if got := dscpPlanTestKeys(dscpUnionCover(plan.static6)); !slices.Equal(got, []string{"::/1", "8000::/1"}) {
		t.Errorf("IPv6 union entries: got %v", got)
	}

	plan = dscpPlanFor(dscpPlanTestConfig(
		dscpPlanTestSet("all", 25, "0.0.0.0/0", "::/0"),
		dscpPlanTestSet("a", 31, "10.0.0.0/8", "255.255.255.255", "ffff::/16"),
	))
	dscpPlanTestExpect(t, "IPv4 /0 with nested sets", plan.static4, []string{
		"0.0.0.0-9.255.255.255=25",
		"10.0.0.0/8=31",
		"11.0.0.0-255.255.255.254=25",
		"255.255.255.255=31",
	})
	dscpPlanTestExpect(t, "IPv6 /0 with the top /16", plan.static6, []string{
		"::-fffe:ffff:ffff:ffff:ffff:ffff:ffff:ffff=25",
		"ffff::/16=31",
	})
	if got := dscpPlanTestKeys(dscpRangeCover(plan.static4[0])); !slices.Equal(got, []string{"0.0.0.0/5", "8.0.0.0/7"}) {
		t.Errorf("cover of the range below 10.0.0.0/8: got %v", got)
	}
	wantTop := []string{"::/1", "8000::/2", "c000::/3", "e000::/4", "f000::/5", "f800::/6", "fc00::/7", "fe00::/8",
		"ff00::/9", "ff80::/10", "ffc0::/11", "ffe0::/12", "fff0::/13", "fff8::/14", "fffc::/15", "fffe::/16"}
	if got := dscpPlanTestKeys(dscpRangeCover(plan.static6[0])); !slices.Equal(got, wantTop) {
		t.Errorf("cover of the IPv6 range below ffff::/16:\n got %v\nwant %v", got, wantTop)
	}
	union4, union6 := dscpUnionCover(plan.static4), dscpUnionCover(plan.static6)
	noZero(t, "IPv4 union", union4)
	noZero(t, "IPv6 union", union6)
	if got := dscpPlanTestKeys(union4); !slices.Equal(got, []string{"0.0.0.0/1", "128.0.0.0/1"}) {
		t.Errorf("IPv4 union entries over the whole space: got %v", got)
	}
	if got := dscpPlanTestKeys(union6); !slices.Equal(got, []string{"::/1", "8000::/1"}) {
		t.Errorf("IPv6 union entries over the whole space: got %v", got)
	}

	plan = dscpPlanFor(dscpPlanTestConfig(
		dscpPlanTestSet("all", 25, "0.0.0.0/0"),
		dscpPlanTestSet("half", 25, "128.0.0.0/1"),
	))
	dscpPlanTestExpect(t, "a /0 rebuilt from a merge", plan.static4, []string{"0.0.0.0/1=25", "128.0.0.0/1=25"})

	full4 := dscpRange{from: netip.MustParseAddr("0.0.0.0"), to: netip.MustParseAddr("255.255.255.255"), value: 7}
	full6 := dscpRange{from: netip.MustParseAddr("::"), to: netip.MustParseAddr("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"), value: 7}
	if got := dscpPlanTestKeys(dscpRangeCover(full4)); !slices.Equal(got, []string{"0.0.0.0/1", "128.0.0.0/1"}) {
		t.Errorf("cover of the whole IPv4 space: got %v", got)
	}
	if got := dscpPlanTestKeys(dscpRangeCover(full6)); !slices.Equal(got, []string{"::/1", "8000::/1"}) {
		t.Errorf("cover of the whole IPv6 space: got %v", got)
	}
}

func TestSetDSCPFlattenIPv6(t *testing.T) {
	plan := dscpPlanFor(dscpPlanTestConfig(
		dscpPlanTestSet("a", 31,
			"2001:db8::/32", "2001:db8:1:2::/64", "  2001:db8:ff::1  ",
			"::ffff:192.0.2.1", "::ffff:198.51.100.0/120",
			"fe80::1%eth0", "2001:db8::zz", "2001:db8::/129", "not-an-address", ""),
		dscpPlanTestSet("b", 6, "2001:DB8:1::/48"),
	))
	dscpPlanTestExpect(t, "IPv6", plan.static6, []string{
		"2001:db8::/48=31",
		"2001:db8:1::/63=6",
		"2001:db8:1:2::/64=31",
		"2001:db8:1:3::-2001:db8:1:ffff:ffff:ffff:ffff:ffff=6",
		"2001:db8:2::-2001:db8:ffff:ffff:ffff:ffff:ffff:ffff=31",
	})
	dscpPlanTestExpect(t, "IPv4-mapped targets", plan.static4, []string{"192.0.2.1=31", "198.51.100.0/24=31"})

	want := []string{"2001:db8:1:3::/64", "2001:db8:1:4::/62", "2001:db8:1:8::/61", "2001:db8:1:10::/60", "2001:db8:1:20::/59",
		"2001:db8:1:40::/58", "2001:db8:1:80::/57", "2001:db8:1:100::/56", "2001:db8:1:200::/55", "2001:db8:1:400::/54",
		"2001:db8:1:800::/53", "2001:db8:1:1000::/52", "2001:db8:1:2000::/51", "2001:db8:1:4000::/50", "2001:db8:1:8000::/49"}
	if got := dscpPlanTestKeys(dscpRangeCover(plan.static6[3])); !slices.Equal(got, want) {
		t.Errorf("cover of the upper part of 2001:db8:1::/48:\n got %v\nwant %v", got, want)
	}
	if got := dscpPlanTestKeys(dscpUnionCover(plan.static6)); !slices.Equal(got, []string{"2001:db8::/32"}) {
		t.Errorf("IPv6 union entries: got %v", got)
	}
	for addr, want := range map[string]int{"2001:db8::1": 31, "2001:db8:1::1": 6, "2001:db8:1:2::9": 31, "2001:db8:1:9::1": 6, "2001:db8:ff::1": 31} {
		if got, ok := dscpPlanTestLookup(plan, netip.MustParseAddr(addr)); !ok || got != want {
			t.Errorf("%s: got DSCP %d (found %v), want %d", addr, got, ok, want)
		}
	}
}

func TestSetDSCPCanonicalKeys(t *testing.T) {
	groups := []struct {
		key  string
		raws []string
	}{
		{"1.2.3.4", []string{"1.2.3.4", "1.2.3.4/32", " 1.2.3.4 ", "::ffff:1.2.3.4", "::ffff:1.2.3.4/128"}},
		{"2001:db8::1", []string{"2001:db8::1", "2001:DB8::1/128", "2001:0db8:0:0::1"}},
		{"10.0.0.0/8", []string{"10.0.0.0/8", "10.1.2.3/8", "::ffff:10.0.0.0/104"}},
		{"2001:db8::/32", []string{"2001:db8::/32", "2001:db8:1::1/32"}},
	}
	for _, g := range groups {
		for _, raw := range g.raws {
			p, ok := dscpParseTarget(raw)
			if !ok {
				t.Errorf("%q was not parsed", raw)
				continue
			}
			if got := dscpCanonicalKey(p); got != g.key {
				t.Errorf("%q: key %q, want %q", raw, got, g.key)
			}
		}
	}
	for _, bad := range []string{"", "1.2.3", "1.2.3.4/33", "1.2.3.4/x", "fe80::1%eth0", "example.com"} {
		if p, ok := dscpParseTarget(bad); ok {
			t.Errorf("%q must not parse, got %s", bad, p)
		}
	}

	host := netip.MustParseAddr("1.2.3.4")
	record := map[string]bool{}
	for _, raw := range []string{"1.2.3.4/32", "10.0.0.0/8"} {
		p, _ := dscpParseTarget(raw)
		record[dscpCanonicalKey(p)] = true
	}
	plan := dscpPlanFor(dscpPlanTestConfig(dscpPlanTestSet("a", 31, "1.2.3.4", "10.9.9.9/8")))
	var stale, fresh []string
	wanted := map[string]bool{}
	for _, key := range dscpPlanTestKeys(dscpUnionCover(plan.static4)) {
		wanted[key] = true
		if !record[key] {
			fresh = append(fresh, key)
		}
	}
	for key := range record {
		if !wanted[key] {
			stale = append(stale, key)
		}
	}
	if len(stale) != 0 || len(fresh) != 0 {
		t.Errorf("a record written as %s/32 must match the plan's own entries: stale %v, fresh %v", host, stale, fresh)
	}
}

func TestSetDSCPPlanIPVersion(t *testing.T) {
	both := dscpPlanTestSet("both", 31, "10.0.0.0/8", "2001:db8::/32")
	only4 := dscpPlanTestSet("only4", 6, "10.1.0.0/16", "2001:db8:1::/48")
	only4.Targets.IPVersion = "4"
	only6 := dscpPlanTestSet("only6", 25, "10.2.0.0/16", "2001:db8:2::/48")
	only6.Targets.IPVersion = "6"
	bogus := dscpPlanTestSet("bogus", 46, "10.3.0.0/16", "2001:db8:3::/48")
	bogus.Targets.IPVersion = "5"
	mismatched := dscpPlanTestSet("mismatched", 12, "192.0.2.0/24")
	mismatched.Targets.IPVersion = "6"

	plan := dscpPlanFor(dscpPlanTestConfig(both, only4, only6, bogus, mismatched))
	dscpPlanTestExpect(t, "IPv4", plan.static4, []string{"10.0.0.0/16=31", "10.1.0.0/16=6", "10.2.0.0-10.255.255.255=31"})
	dscpPlanTestExpect(t, "IPv6", plan.static6, []string{"2001:db8::/47=31", "2001:db8:2::/48=25", "2001:db8:3::-2001:db8:ffff:ffff:ffff:ffff:ffff:ffff=31"})
	if !slices.Equal(plan.values, []int{6, 25, 31}) {
		t.Errorf("values: got %v, want [6 25 31]", plan.values)
	}

	families := map[string][2]bool{}
	for _, s := range plan.sets {
		families[s.id] = [2]bool{s.v4, s.v6}
	}
	want := map[string][2]bool{
		"both":       {true, true},
		"only4":      {true, false},
		"only6":      {false, true},
		"bogus":      {false, false},
		"mismatched": {false, true},
	}
	if !reflect.DeepEqual(families, want) {
		t.Errorf("families: got %v, want %v", families, want)
	}
	if len(plan.refusals) != 0 {
		t.Errorf("an IP version filter is no refusal, got %v", plan.refusals)
	}
}

func TestSetDSCPPlanRefusals(t *testing.T) {
	routed := func(set *config.SetConfig, mode string) *config.SetConfig {
		set.Routing.Enabled = true
		set.Routing.Mode = mode
		return set
	}
	devices := dscpPlanTestSet("devices", 46, "10.3.0.0/16")
	devices.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
	quiet := dscpPlanTestSet("quiet", 7, "10.4.0.0/16")
	quiet.DSCP.Enabled = false
	disabled := dscpPlanTestSet("disabled", 7, "10.5.0.0/16")
	disabled.Enabled = false
	plain := dscpPlanTestSet("plain", 0, "10.9.0.0/16")
	plain.DSCP = config.SetDSCPConfig{}

	build := func() *config.Config {
		return dscpPlanTestConfig(
			dscpPlanTestSet("wide", 31, "10.0.0.0/8"),
			routed(dscpPlanTestSet("proxy", 6, "10.1.0.0/16"), config.RoutingModeProxy),
			routed(dscpPlanTestSet("block", 25, "10.2.0.0/16"), config.RoutingModeBlock),
			devices,
			dscpPlanTestSet("empty", 12),
			quiet,
			disabled,
			dscpPlanTestSet("wide", 12, "10.6.0.0/16"),
			nil,
			routed(dscpPlanTestSet("telegram", 5, "10.7.0.0/16"), config.RoutingModeMTProtoWS),
			plain,
		)
	}

	plan := dscpPlanFor(build())
	wantRefusals := map[string]string{
		"proxy":    config.DSCPRefusedRoutingProxy,
		"block":    config.DSCPRefusedRoutingBlock,
		"devices":  config.DSCPRefusedSourceDevices,
		"empty":    config.DSCPRefusedNoAddresses,
		"telegram": config.DSCPRefusedRoutingMTProtoWS,
	}
	if !reflect.DeepEqual(plan.refusals, wantRefusals) {
		t.Errorf("refusals:\n got %v\nwant %v", plan.refusals, wantRefusals)
	}
	if len(plan.sets) != 1 || plan.sets[0].id != "wide" || plan.sets[0].value != 31 || plan.sets[0].index != 0 {
		t.Fatalf("only the first set named wide may stamp, got %+v", plan.sets)
	}
	dscpPlanTestExpect(t, "refused, quiet and later sets must not carve the range", plan.static4, []string{"10.0.0.0/8=31"})
	if !slices.Equal(plan.values, []int{31}) {
		t.Errorf("values: got %v, want [31]", plan.values)
	}

	cfg := build()
	cfg.Queue.Devices.Enabled = true
	cfg.Queue.Devices.Devices = []config.Device{{MAC: "02:00:00:00:00:01", Selected: true}}
	plan = dscpPlanFor(cfg)
	wantRefusals = map[string]string{
		"wide":     config.DSCPRefusedDeviceFilter,
		"proxy":    config.DSCPRefusedRoutingProxy,
		"block":    config.DSCPRefusedRoutingBlock,
		"devices":  config.DSCPRefusedSourceDevices,
		"empty":    config.DSCPRefusedDeviceFilter,
		"telegram": config.DSCPRefusedRoutingMTProtoWS,
	}
	if !reflect.DeepEqual(plan.refusals, wantRefusals) {
		t.Errorf("refusals with selected devices:\n got %v\nwant %v", plan.refusals, wantRefusals)
	}
	if !plan.empty() || len(plan.static4) != 0 || len(plan.values) != 0 {
		t.Errorf("selected devices must leave nothing to stamp, got sets %+v and ranges %v", plan.sets, dscpPlanTestRanges(plan.static4))
	}
}

func TestSetDSCPPlanKeepsUnresolvedTargets(t *testing.T) {
	asn := dscpPlanTestSet("asn", 31)
	asn.Targets.ASNs = []string{"64496"}
	geoip := dscpPlanTestSet("geoip", 6)
	geoip.Targets.GeoIpCategories = []string{"zz"}
	pinned := dscpPlanTestSet("pinned", 25)
	pinned.Targets.DomainOnly = true
	pinned.Targets.SNIDomains = []string{"example.com"}
	pinned.Targets.ASNs = []string{"64497"}
	named := dscpPlanTestSet("named", 46)
	named.Targets.DomainOnly = true
	named.Targets.SNIDomains = []string{"example.org"}
	cfg := dscpPlanTestConfig(asn, geoip, pinned, dscpPlanTestSet("static", 12, "10.0.0.0/8"), named)

	plan := dscpPlanFor(cfg)
	if want := map[string]string{"named": config.DSCPRefusedNoAddresses}; !reflect.DeepEqual(plan.refusals, want) {
		t.Errorf("refusals: got %v, want %v", plan.refusals, want)
	}
	member := func(id string, value, index int, learn bool) dscpPlanSet {
		return dscpPlanSet{id: id, sid: routeSanitizeSetID(id), value: value, index: index, learn: learn, dnsTTL: time.Hour, tlsTTL: 10 * time.Minute, v4: true, v6: true}
	}
	want := []dscpPlanSet{member("asn", 31, 0, true), member("geoip", 6, 1, true), member("pinned", 25, 2, false), member("static", 12, 3, true)}
	if !reflect.DeepEqual(plan.sets, want) {
		t.Errorf("sets whose targets are declared but not resolved yet must stay in the plan:\n got %+v\nwant %+v", plan.sets, want)
	}
	dscpPlanTestExpect(t, "targets that are not resolved yet", plan.static4, []string{"10.0.0.0/8=12"})
	if len(plan.static6) != 0 || !slices.Equal(plan.values, []int{12}) {
		t.Errorf("unresolved targets must add no static range or value, got %v and values %v", dscpPlanTestRanges(plan.static6), plan.values)
	}

	asn.Targets.IpsToMatch = []string{"192.0.2.0/24"}
	resolved := dscpPlanFor(cfg)
	if resolved.equal(plan) || resolved.sameStatic(plan) {
		t.Error("prefixes that arrive later must change the plan, or the next sync pass never applies them")
	}
	dscpPlanTestExpect(t, "after the prefixes arrived", resolved.static4, []string{"10.0.0.0/8=12", "192.0.2.0/24=31"})
}

func TestSetDSCPPlanSets(t *testing.T) {
	quiet := dscpPlanTestSet("quiet", 7, "100.64.0.0/10")
	quiet.DSCP.Enabled = false
	plain := dscpPlanTestSet("Video-1", 31, "203.0.113.0/24")
	plain.Targets.SNIDomains = []string{"Example.COM.", `regexp:^cdn[0-9]+\.example\.net$`, "example.com", " ", "video.example.org"}
	routed := dscpPlanTestSet("routed", 6, "198.51.100.0/24")
	routed.Routing.Enabled = true
	routed.Routing.EgressInterface = "wg0"
	routed.Routing.IPTTLSeconds = 900
	domainOnly := dscpPlanTestSet("domain-only", 25, "192.0.2.0/24")
	domainOnly.Targets.DomainOnly = true
	domainOnly.Targets.SNIDomains = []string{"example.net"}
	many := dscpPlanTestSet("many", 46, "100.64.0.0/10")
	for i := range 300 {
		many.Targets.SNIDomains = append(many.Targets.SNIDomains, fmt.Sprintf("h%d.example.com", i))
	}

	cfg := dscpPlanTestConfig(nil, quiet, plain, routed, domainOnly, many)
	cfg.System.Tables.DSCP = config.DSCPConfig{Enabled: false, Value: 9, Interfaces: []string{"eth0", " wan ", "eth0", "", "br$lan"}}
	plan := dscpPlanFor(cfg)
	if plan.globalOn || plan.global != 0 {
		t.Errorf("the global stamp is off, got on=%v value=%d", plan.globalOn, plan.global)
	}
	if want := []string{"eth0", "wan", "brlan"}; !slices.Equal(plan.scopes, want) {
		t.Errorf("scopes while the global stamp is off: got %q, want %q", plan.scopes, want)
	}

	want := []dscpPlanSet{
		{id: "Video-1", sid: routeSanitizeSetID("Video-1"), value: 31, index: 2, learn: true, dnsTTL: time.Hour, tlsTTL: 10 * time.Minute, v4: true, v6: true,
			domains: []string{"example.com", "video.example.org"}},
		{id: "routed", sid: routeSanitizeSetID("routed"), value: 6, index: 3, learn: true, dnsTTL: 900 * time.Second, tlsTTL: 10 * time.Minute, v4: true, v6: true},
		{id: "domain-only", sid: routeSanitizeSetID("domain-only"), value: 25, index: 4, dnsTTL: time.Hour, tlsTTL: 10 * time.Minute, v4: true, v6: true},
	}
	if len(plan.sets) != 4 {
		t.Fatalf("expected four sets, got %+v", plan.sets)
	}
	for i, w := range want {
		if got := plan.sets[i]; !reflect.DeepEqual(got, w) {
			t.Errorf("set %d:\n got %+v\nwant %+v", i, got, w)
		}
	}
	if got := plan.sets[3].domains; len(got) != dscpPreResolveCap || got[0] != "h0.example.com" || got[dscpPreResolveCap-1] != "h255.example.com" {
		t.Errorf("the domain list must keep the first %d names in order, got %d names", dscpPreResolveCap, len(got))
	}
	if sid := plan.sets[0].sid; len(sid) > 20 {
		t.Errorf("sanitized id %q is longer than 20 characters", sid)
	}

	cfg.System.Tables.DSCP.Enabled = true
	plan = dscpPlanFor(cfg)
	if !plan.globalOn || plan.global != 9 || !slices.Equal(plan.scopes, []string{"eth0", "wan", "brlan"}) {
		t.Errorf("global stamp on: got on=%v value=%d scopes=%q", plan.globalOn, plan.global, plan.scopes)
	}
}

func TestSetDSCPPlanSkipSetupEmpty(t *testing.T) {
	refused := dscpPlanTestSet("proxy", 6, "10.1.0.0/16")
	refused.Routing.Enabled = true
	refused.Routing.Mode = config.RoutingModeProxy
	cfg := dscpPlanTestConfig(dscpPlanTestSet("a", 31, "10.0.0.0/8", "2001:db8::/32"), refused)
	cfg.System.Tables.DSCP = config.DSCPConfig{Enabled: true, Value: 7, Interfaces: []string{"eth0"}}
	cfg.System.Tables.SkipSetup = true

	plan := dscpPlanFor(cfg)
	if !plan.empty() || plan.globalOn || plan.global != 0 || len(plan.scopes) != 0 || len(plan.refusals) != 0 ||
		len(plan.static4) != 0 || len(plan.static6) != 0 || len(plan.values) != 0 {
		t.Fatalf("skip_setup must give an empty plan, got %+v", plan)
	}
	if !plan.equal(dscpPlanFor(nil)) {
		t.Error("an empty plan under skip_setup must equal the plan of no configuration")
	}
	other := dscpPlanTestConfig(dscpPlanTestSet("b", 6, "192.0.2.0/24"))
	other.System.Tables.SkipSetup = true
	if !plan.equal(dscpPlanFor(other)) {
		t.Error("two configurations under skip_setup must give equal plans")
	}

	cfg.System.Tables.SkipSetup = false
	if live := dscpPlanFor(cfg); live.empty() || live.equal(plan) {
		t.Error("the same configuration without skip_setup must give a plan with sets")
	}
}

func TestSetDSCPPlanEqual(t *testing.T) {
	build := func(change func(*config.Config)) *dscpPlan {
		a := dscpPlanTestSet("a", 31, "10.0.0.0/8", "2001:db8::/32")
		a.Targets.SNIDomains = []string{"example.com"}
		cfg := dscpPlanTestConfig(a, dscpPlanTestSet("b", 6, "10.1.0.0/16"))
		cfg.System.Tables.DSCP = config.DSCPConfig{Enabled: true, Value: 7, Interfaces: []string{"eth0"}}
		if change != nil {
			change(cfg)
		}
		return dscpPlanFor(cfg)
	}
	base := build(nil)

	same := []struct {
		name   string
		change func(*config.Config)
	}{
		{"nothing", nil},
		{"targets in another order", func(c *config.Config) {
			c.Sets[0].Targets.IpsToMatch = []string{"2001:db8::/32", "10.0.0.0/8"}
		}},
		{"a prefix written as two halves", func(c *config.Config) {
			c.Sets[0].Targets.IpsToMatch = []string{"10.0.0.0/9", "10.128.0.0/9", "2001:db8::/32"}
		}},
		{"a renamed set", func(c *config.Config) { c.Sets[0].Name = "renamed" }},
		{"a set without DSCP that holds a longer prefix", func(c *config.Config) {
			other := dscpPlanTestSet("other", 0, "10.1.2.0/24")
			other.DSCP = config.SetDSCPConfig{}
			c.Sets = append(c.Sets, other)
		}},
		{"an interface list with blanks and repeats", func(c *config.Config) {
			c.System.Tables.DSCP.Interfaces = []string{" eth0", "", "eth0"}
		}},
		{"the same domain written differently", func(c *config.Config) {
			c.Sets[0].Targets.SNIDomains = []string{"Example.com.", "example.com"}
		}},
	}
	for _, tc := range same {
		if got := build(tc.change); !base.equal(got) || !base.sameStatic(got) {
			t.Errorf("%s: the plan must stay equal", tc.name)
		}
	}

	differ := []struct {
		name       string
		change     func(*config.Config)
		sameStatic bool
	}{
		{"a set value", func(c *config.Config) { c.Sets[1].DSCP.Value = 12 }, false},
		{"a target", func(c *config.Config) { c.Sets[1].Targets.IpsToMatch = []string{"10.2.0.0/16"} }, false},
		{"an IP version filter", func(c *config.Config) { c.Sets[0].Targets.IPVersion = "4" }, false},
		{"a domain", func(c *config.Config) { c.Sets[0].Targets.SNIDomains = []string{"example.org"} }, true},
		{"the global value", func(c *config.Config) { c.System.Tables.DSCP.Value = 8 }, true},
		{"the global switch", func(c *config.Config) { c.System.Tables.DSCP.Enabled = false }, true},
		{"the interface list", func(c *config.Config) { c.System.Tables.DSCP.Interfaces = []string{"eth0", "wan"} }, true},
		{"the list order", func(c *config.Config) { c.Sets[0], c.Sets[1] = c.Sets[1], c.Sets[0] }, true},
		{"a refused set", func(c *config.Config) {
			refused := dscpPlanTestSet("proxy", 46, "192.0.2.0/24")
			refused.Routing.Enabled = true
			refused.Routing.Mode = config.RoutingModeProxy
			c.Sets = append(c.Sets, refused)
		}, true},
		{"the learned address lifetime", func(c *config.Config) {
			c.Sets[0].Routing.Enabled = true
			c.Sets[0].Routing.EgressInterface = "wg0"
			c.Sets[0].Routing.IPTTLSeconds = 900
		}, true},
		{"domain only", func(c *config.Config) { c.Sets[0].Targets.DomainOnly = true }, true},
	}
	for _, tc := range differ {
		got := build(tc.change)
		if base.equal(got) || got.equal(base) {
			t.Errorf("%s: the plan must change", tc.name)
		}
		if base.sameStatic(got) != tc.sameStatic {
			t.Errorf("%s: sameStatic is %v, want %v", tc.name, !tc.sameStatic, tc.sameStatic)
		}
	}

	offA := build(func(c *config.Config) { c.System.Tables.DSCP = config.DSCPConfig{Value: 7} })
	offB := build(func(c *config.Config) { c.System.Tables.DSCP = config.DSCPConfig{Value: 46} })
	if !offA.equal(offB) {
		t.Error("the value of a global stamp that is off must not change the plan")
	}

	var none *dscpPlan
	if !none.equal(nil) || none.equal(base) || base.equal(nil) || !none.empty() || none.sameStatic(base) {
		t.Error("a missing plan equals only another missing plan and counts as empty")
	}
}

func BenchmarkDSCPFlatten30k(b *testing.B) {
	rng := rand.New(rand.NewPCG(11, 30000))
	cfg := dscpPlanTestConfig()
	for i, value := range []int{31, 6, 25} {
		set := dscpPlanTestSet(fmt.Sprintf("s%d", i), value)
		seen := map[netip.Prefix]bool{}
		for len(set.Targets.IpsToMatch) < 10000 {
			addr := netip.AddrFrom4([4]byte{byte(11 + rng.IntN(213)), byte(rng.IntN(256)), byte(rng.IntN(256)), byte(rng.IntN(256))})
			prefix := netip.PrefixFrom(addr, 16+rng.IntN(9)).Masked()
			if seen[prefix] {
				continue
			}
			seen[prefix] = true
			set.Targets.IpsToMatch = append(set.Targets.IpsToMatch, prefix.String())
		}
		cfg.Sets = append(cfg.Sets, set)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var plan *dscpPlan
	for i := 0; i < b.N; i++ {
		plan = dscpPlanFor(cfg)
	}
	b.StopTimer()
	if len(plan.static4) == 0 {
		b.Fatal("the benchmark plan has no ranges")
	}
	b.ReportMetric(float64(len(plan.static4)), "ranges")
}
