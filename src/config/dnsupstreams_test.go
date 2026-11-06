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
