package config

import (
	"math"
	"net/netip"
	"reflect"
	"slices"
	"testing"
)

func prefixes(t *testing.T, list ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, len(list))
	for i, s := range list {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			t.Fatalf("bad test prefix %q: %v", s, err)
		}
		out[i] = p
	}
	return out
}

func prefixStrings(ps []netip.Prefix) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return out
}

func TestCollapsePrefixes(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, []string{}},
		{"covered prefix is dropped", []string{"8.8.8.0/24", "8.8.0.0/16"}, []string{"8.8.0.0/16"}},
		{"duplicate collapses", []string{"1.1.1.0/24", "1.1.1.0/24"}, []string{"1.1.1.0/24"}},
		{"adjacent siblings merge", []string{"10.0.1.0/24", "10.0.0.0/24"}, []string{"10.0.0.0/23"}},
		{"merge cascades", []string{"20.0.0.0/24", "20.0.1.0/24", "20.0.2.0/24", "20.0.3.0/24"}, []string{"20.0.0.0/22"}},
		{"cascade across sizes", []string{"30.0.0.0/23", "30.0.2.0/24", "30.0.3.0/24"}, []string{"30.0.0.0/22"}},
		{"adjacent but not siblings stay apart", []string{"40.0.1.0/24", "40.0.2.0/24"}, []string{"40.0.1.0/24", "40.0.2.0/24"}},
		{"host bits are masked", []string{"50.0.0.7/24"}, []string{"50.0.0.0/24"}},
		{"mixed families sort v4 first", []string{"2a00:1450::/32", "2a00:1451::/32", "1.0.0.0/24", "1.0.1.0/24"}, []string{"1.0.0.0/23", "2a00:1450::/31"}},
		{"v4 and v6 never merge together", []string{"0.0.0.0/8", "::/8"}, []string{"0.0.0.0/8", "::/8"}},
		{"default route rejected", []string{"0.0.0.0/0", "::/0", "9.9.9.0/24"}, []string{"9.9.9.0/24"}},
		{"halves never merge into a default route", []string{"0.0.0.0/1", "128.0.0.0/1"}, []string{"0.0.0.0/1", "128.0.0.0/1"}},
		{"v6 covered and adjacent", []string{"2001:4860::/32", "2001:4860:4860::/48", "2001:4861::/32"}, []string{"2001:4860::/31"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := prefixes(t, tc.in...)
			snapshot := slices.Clone(in)
			got := prefixStrings(CollapsePrefixes(in))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("CollapsePrefixes(%v) = %v, want %v", tc.in, got, tc.want)
			}
			if !reflect.DeepEqual(in, snapshot) {
				t.Fatalf("CollapsePrefixes modified its input: %v", in)
			}
		})
	}
}

func TestCollapsePrefixesSkipsInvalid(t *testing.T) {
	got := CollapsePrefixes([]netip.Prefix{{}, netip.MustParsePrefix("5.5.5.0/24")})
	if !reflect.DeepEqual(prefixStrings(got), []string{"5.5.5.0/24"}) {
		t.Fatalf("got %v", got)
	}
}

func TestIsBogonPrefix(t *testing.T) {
	bogons := []string{
		"0.0.0.0/0", "::/0", "10.1.0.0/16", "192.168.1.0/24", "172.16.0.0/12", "100.64.0.0/10",
		"127.0.0.0/8", "169.254.0.0/16", "192.0.2.0/24", "198.18.0.0/15", "203.0.113.0/24",
		"224.0.0.0/4", "240.0.0.0/4", "255.255.255.255/32", "8.0.0.0/4", "192.0.0.0/8",
		"fe80::/10", "fc00::/7", "::1/128", "::ffff:0:0/96", "ff02::/16", "2001:db8::/32",
		"2001:db8:1::/48", "2000::/3", "3fff::/24",
	}
	for _, s := range bogons {
		if !IsBogonPrefix(netip.MustParsePrefix(s)) {
			t.Errorf("IsBogonPrefix(%s) = false, want true", s)
		}
	}
	public := []string{"8.8.8.0/24", "1.1.1.0/24", "172.32.0.0/11", "100.128.0.0/9", "2a00:1450::/32", "2001:4860::/32", "2606:4700::/32"}
	for _, s := range public {
		if IsBogonPrefix(netip.MustParsePrefix(s)) {
			t.Errorf("IsBogonPrefix(%s) = true, want false", s)
		}
	}
	if !IsBogonPrefix(netip.Prefix{}) {
		t.Error("the zero prefix must count as a bogon")
	}
}

func TestSanitizeASNPrefixes(t *testing.T) {
	got := SanitizeASNPrefixes([]string{
		"8.8.8.0/24",
		" 8.8.4.0/24 ",
		"8.8.0.0/16",
		"10.0.0.0/8",
		"0.0.0.0/0",
		"::/0",
		"not-a-prefix",
		"",
		"1.1.1.1",
		"1.1.1.0/24",
		"2001:4860::/32",
		"2001:4860:1::/48",
		"fe80::/64",
		"2001:db8::/48",
		"9.9.9.9/24",
		"fe80::1%eth0",
	})
	want := []string{"1.1.1.0/24", "8.8.0.0/16", "9.9.9.0/24", "2001:4860::/32"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SanitizeASNPrefixes = %v, want %v", got, want)
	}
	if got := SanitizeASNPrefixes(nil); got == nil || len(got) != 0 {
		t.Fatalf("SanitizeASNPrefixes(nil) = %#v, want an empty non-nil slice", got)
	}
}

func TestCountPrefixes(t *testing.T) {
	c := CountPrefixes([]string{"1.1.1.0/24", "8.8.0.0/16", "2001:4860::/32", "2a00::/64", "2a01::1/128", "junk"})
	want := AsnCounts{Prefixes: 5, V4: 2, V6: 3, IPv4Addresses: 256 + 65536, IPv6Slash64s: (1 << 32) + 1 + 1}
	if c != want {
		t.Fatalf("CountPrefixes = %+v, want %+v", c, want)
	}
	if got := CountPrefixes([]string{"::/0", "2000::/3"}).IPv6Slash64s; got != math.MaxUint64 {
		t.Fatalf("an oversized v6 span must saturate, got %d", got)
	}
	info := &AsnInfo{Prefixes: []string{"1.0.0.0/24"}}
	if info.Counts().IPv4Addresses != 256 {
		t.Fatalf("AsnInfo.Counts = %+v", info.Counts())
	}
	var nilInfo *AsnInfo
	if nilInfo.Counts() != (AsnCounts{}) {
		t.Fatal("a nil AsnInfo must count as empty")
	}
}
