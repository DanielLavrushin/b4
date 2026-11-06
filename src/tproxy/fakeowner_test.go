package tproxy

import (
	"net"
	"strconv"
	"testing"

	"github.com/daniellavrushin/b4/dns"
)

func fakeTestCIDR(t *testing.T, raw string) *net.IPNet {
	t.Helper()
	_, network, err := net.ParseCIDR(raw)
	if err != nil {
		t.Fatal(err)
	}
	return network
}

func collidingFakePair(t *testing.T, network *net.IPNet) (string, string) {
	t.Helper()
	owner := map[string]string{}
	for i := 0; i < 20000; i++ {
		name := "tproxyfake" + strconv.Itoa(i) + ".example"
		ip := dns.FakeIPAddress(name, network)
		if ip == nil {
			t.Fatalf("%s got no address", name)
		}
		key := ip.String()
		if prev, taken := owner[key]; taken && prev != name {
			return prev, name
		}
		owner[key] = name
	}
	t.Fatal("no two names landed on one address")
	return "", ""
}

func TestAFakeAddressNamesItsSiteForAnyDevice(t *testing.T) {
	network := fakeTestCIDR(t, "198.18.0.0/15")
	first, second := collidingFakePair(t, network)
	if !dns.FakeIPAddress(first, network).Equal(dns.FakeIPAddress(second, network)) {
		t.Fatalf("%s and %s stopped colliding, the fixture is stale", first, second)
	}

	pool := dns.DefaultFakeIPPool()
	a := pool.Address(first, network)
	b := pool.Address(second, network)
	if a == nil || b == nil {
		t.Fatal("the pool refused a name")
	}
	if a.Equal(b) {
		t.Fatalf("both names got %s, so a connection without a hostname cannot be dialled", a)
	}

	resolver := NewResolver(dns.NewNameCache())
	stranger := net.ParseIP("192.168.1.99")
	for _, tc := range []struct {
		name string
		ip   net.IP
	}{{first, a}, {second, b}} {
		names := resolver.ObservedNames(stranger, tc.ip)
		if names.Owner != tc.name {
			t.Fatalf("%s names %q, want %q", tc.ip, names.Owner, tc.name)
		}
		name, source := pickName(targetInputs{names: names})
		if name != tc.name || source != "fakeip" {
			t.Fatalf("dialled %q (%s) for %s, want %q", name, source, tc.ip, tc.name)
		}
	}
}
