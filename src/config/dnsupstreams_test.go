package config

import (
	"reflect"
	"testing"
)

func TestUpstreamListMergesEverySource(t *testing.T) {
	c := DNSConfig{
		Enabled:   true,
		TargetDNS: "1.1.1.1",
		DoHURL:    "https://dns.google/dns-query",
		Upstreams: []string{"9.9.9.9", "https://doh.pub/dns-query", "1.1.1.1"},
	}
	want := []string{"https://dns.google/dns-query", "9.9.9.9", "https://doh.pub/dns-query", "1.1.1.1"}
	if got := c.UpstreamList(); !reflect.DeepEqual(got, want) {
		t.Fatalf("UpstreamList = %v, want %v", got, want)
	}
	if !c.HasUpstream() {
		t.Fatal("HasUpstream = false for a configured set")
	}

	c.DoHURL = ""
	want = []string{"1.1.1.1", "9.9.9.9", "https://doh.pub/dns-query"}
	if got := c.UpstreamList(); !reflect.DeepEqual(got, want) {
		t.Fatalf("UpstreamList without a URL = %v, want %v", got, want)
	}
}

func TestUpstreamListIgnoresJunk(t *testing.T) {
	c := DNSConfig{Upstreams: []string{"", "  ", "dns.example", "::1"}}
	want := []string{"::1"}
	if got := c.UpstreamList(); !reflect.DeepEqual(got, want) {
		t.Fatalf("UpstreamList = %v, want %v", got, want)
	}
}

func TestUpstreamListCapsTheEffectiveList(t *testing.T) {
	c := DNSConfig{Enabled: true, DoHURL: "https://doh.example/dns-query", TargetDNS: "1.1.1.1"}
	for i := 0; i < MaxDNSUpstreams+4; i++ {
		c.Upstreams = append(c.Upstreams, "10.0.0."+upstreamItoa(i+1))
	}
	if got := len(c.UpstreamList()); got != MaxDNSUpstreams {
		t.Fatalf("effective list has %d entries, want the cap %d", got, MaxDNSUpstreams)
	}
}

func TestUpstreamLabelNeverCarriesCredentials(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://user:secret@doh.example:8443/dns-query?x=1", "https://doh.example:8443"},
		{"http://plain.example/dns-query", "http://plain.example"},
		{"9.9.9.9", "9.9.9.9"},
		{"junk", ""},
	} {
		if got := UpstreamLabel(tc.raw); got != tc.want {
			t.Fatalf("UpstreamLabel(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestNormalizeUpstreamsDropsAndCaps(t *testing.T) {
	c := DNSConfig{Upstreams: []string{"1.1.1.1", "junk", "1.1.1.1"}}
	for i := 0; i < MaxDNSUpstreams+3; i++ {
		c.Upstreams = append(c.Upstreams, "10.0.0."+upstreamItoa(i+1))
	}
	var dropped []string
	c.NormalizeUpstreams(func(value string) { dropped = append(dropped, value) })
	if len(c.Upstreams) != MaxDNSUpstreams {
		t.Fatalf("NormalizeUpstreams kept %d entries, want %d", len(c.Upstreams), MaxDNSUpstreams)
	}
	if len(dropped) == 0 {
		t.Fatal("NormalizeUpstreams dropped nothing without reporting it")
	}
	for i := 1; i < len(c.Upstreams); i++ {
		if c.Upstreams[i] == c.Upstreams[i-1] {
			t.Fatalf("duplicate %q survived", c.Upstreams[i])
		}
	}
}

func TestValidateFakeIPDefaultsAndBounds(t *testing.T) {
	c := DNSConfig{FakeIP: FakeIPConfig{Enabled: true}}
	if err := c.ValidateFakeIP(); err != nil {
		t.Fatalf("ValidateFakeIP: %v", err)
	}
	if c.FakeIP.CIDR != DefaultFakeIPCIDR {
		t.Fatalf("CIDR = %q, want %q", c.FakeIP.CIDR, DefaultFakeIPCIDR)
	}
	if c.FakeIP.TTLSec != DefaultFakeIPTTL {
		t.Fatalf("TTLSec = %d, want %d", c.FakeIP.TTLSec, DefaultFakeIPTTL)
	}

	for _, raw := range []string{"not-a-block", "fd00::/64", "198.18.0.0/28", "127.0.0.0/24", "224.0.0.0/24"} {
		bad := DNSConfig{FakeIP: FakeIPConfig{Enabled: true, CIDR: raw}}
		if err := bad.ValidateFakeIP(); err == nil {
			t.Fatalf("ValidateFakeIP accepted %q", raw)
		}
	}
}

func TestValidateFakeIPIgnoresADisabledConfig(t *testing.T) {
	c := DNSConfig{FakeIP: FakeIPConfig{CIDR: "junk"}}
	if err := c.ValidateFakeIP(); err != nil {
		t.Fatalf("ValidateFakeIP failed for a disabled config: %v", err)
	}
}

func TestFakeIPNetworkNeedsTheSwitch(t *testing.T) {
	c := DNSConfig{FakeIP: FakeIPConfig{CIDR: "198.18.0.0/15"}}
	if _, _, ok := c.FakeIPNetwork(); ok {
		t.Fatal("FakeIPNetwork reported a block while fake DNS is off")
	}
	c.FakeIP.Enabled = true
	network, ttl, ok := c.FakeIPNetwork()
	if !ok || network.String() != DefaultFakeIPCIDR || ttl != DefaultFakeIPTTL {
		t.Fatalf("FakeIPNetwork = %v, %d, %t", network, ttl, ok)
	}
}

func TestSameResolverComparesWhatAnswers(t *testing.T) {
	base := DNSConfig{
		Enabled:   true,
		TargetDNS: "1.1.1.1",
		Upstreams: []string{"9.9.9.9"},
		Strict:    true,
	}
	other := base
	if !base.SameResolver(&other) {
		t.Fatal("identical resolver configs were treated as different")
	}

	loose := base
	loose.Strict = false
	if base.SameResolver(&loose) {
		t.Fatal("a strict change was ignored")
	}

	fragmented := base
	fragmented.FragmentQuery = true
	if base.SameResolver(&fragmented) {
		t.Fatal("a fragment_query change was ignored")
	}

	stripped := base
	stripped.StripECH = true
	if base.SameResolver(&stripped) {
		t.Fatal("a strip_ech change was ignored")
	}

	added := base
	added.Upstreams = []string{"9.9.9.9", "8.8.8.8"}
	if base.SameResolver(&added) {
		t.Fatal("an added upstream was ignored")
	}

	swapped := base
	swapped.TargetDNS = "9.9.9.9"
	swapped.Upstreams = []string{"1.1.1.1"}
	if base.SameResolver(&swapped) {
		t.Fatal("a swapped resolver priority was ignored")
	}

	faked := base
	faked.FakeIP = FakeIPConfig{Enabled: true, CIDR: "198.18.0.0/15", TTLSec: 300}
	if base.SameResolver(&faked) {
		t.Fatal("a fake DNS change was ignored")
	}
	longer := faked
	longer.FakeIP.TTLSec = 900
	if faked.SameResolver(&longer) {
		t.Fatal("a fake DNS ttl change was ignored")
	}

	pinned := base
	pinned.Pins = map[string][]string{"a.example": {"1.2.3.4"}}
	if base.SameResolver(&pinned) {
		t.Fatal("a pin change was ignored")
	}

	disabled := base
	disabled.Enabled = false
	if base.SameResolver(&disabled) {
		t.Fatal("an enabled change was ignored")
	}

	var none *DNSConfig
	if none.SameResolver(nil) != true {
		t.Fatal("two absent configs were treated as different")
	}
	if base.SameResolver(nil) {
		t.Fatal("an absent config matched a configured one")
	}
}

func TestSameResolverIgnoresTheTargetBehindDoH(t *testing.T) {
	a := DNSConfig{Enabled: true, DoHURL: "https://dns.google/dns-query", TargetDNS: "8.8.8.8"}
	b := DNSConfig{Enabled: true, DoHURL: "https://dns.google/dns-query", TargetDNS: "1.1.1.1"}
	if !a.SameResolver(&b) {
		t.Fatal("a target_dns that the DoH URL overrides was treated as a change")
	}
}

func TestSameResolverTreatsTheDefaultFakeIPTTLAsEqual(t *testing.T) {
	a := DNSConfig{Enabled: true, FakeIP: FakeIPConfig{Enabled: true}}
	b := DNSConfig{Enabled: true, FakeIP: FakeIPConfig{Enabled: true, TTLSec: DefaultFakeIPTTL}}
	if !a.SameResolver(&b) {
		t.Fatal("an unset fake DNS ttl was treated as different from the default")
	}
}

func TestResolverKeyTracksEveryInput(t *testing.T) {
	base := DNSConfig{Enabled: true, TargetDNS: "1.1.1.1", Upstreams: []string{"9.9.9.9"}}
	if base.ResolverKey() != base.ResolverKey() {
		t.Fatal("ResolverKey is not stable")
	}
	same := base
	if base.ResolverKey() != same.ResolverKey() {
		t.Fatal("identical configs produced different keys")
	}
	changed := base
	changed.Strict = true
	if base.ResolverKey() == changed.ResolverKey() {
		t.Fatal("a strict change left the key alone")
	}
	pinned := base
	pinned.Pins = map[string][]string{"a.example": {"1.2.3.4"}, "b.example": {"5.6.7.8"}}
	reordered := base
	reordered.Pins = map[string][]string{"b.example": {"5.6.7.8"}, "a.example": {"1.2.3.4"}}
	if pinned.ResolverKey() != reordered.ResolverKey() {
		t.Fatal("the pin map order changed the key")
	}
	if base.ResolverKey() == pinned.ResolverKey() {
		t.Fatal("pins left the key alone")
	}
	var none *DNSConfig
	if none.ResolverKey() != "" {
		t.Fatal("an absent config produced a key")
	}
}

func upstreamItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
