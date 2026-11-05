package dns

import (
	"net"
	"testing"
)

func TestFakeIPAddressIsStableAndInsideTheBlock(t *testing.T) {
	_, network, err := net.ParseCIDR("198.18.0.0/15")
	if err != nil {
		t.Fatal(err)
	}
	first := FakeIPAddress("example.com", network)
	if first == nil {
		t.Fatal("FakeIPAddress returned nothing")
	}
	if !network.Contains(first) {
		t.Fatalf("%s is outside %s", first, network)
	}
	if again := FakeIPAddress("EXAMPLE.com.", network); !again.Equal(first) {
		t.Fatalf("the same name mapped to %s and %s", first, again)
	}
	if first.Equal(network.IP) {
		t.Fatal("the network address was handed out")
	}
}

func TestFakeIPAddressSpreadsNames(t *testing.T) {
	_, network, err := net.ParseCIDR("198.18.0.0/15")
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, 64)
	for _, name := range []string{"a.example", "b.example", "c.example", "d.example", "e.example", "f.example", "g.example", "h.example"} {
		ip := FakeIPAddress(name, network)
		if ip == nil {
			t.Fatalf("%s got no address", name)
		}
		seen[ip.String()] = true
	}
	if len(seen) < 6 {
		t.Fatalf("eight names landed on %d addresses, the block is not being used", len(seen))
	}
}

func TestFakeIPAddressRejectsUnusableBlocks(t *testing.T) {
	_, v6, err := net.ParseCIDR("fd00::/64")
	if err != nil {
		t.Fatal(err)
	}
	if ip := FakeIPAddress("example.com", v6); ip != nil {
		t.Fatalf("an IPv6 block produced %s", ip)
	}
	if ip := FakeIPAddress("example.com", nil); ip != nil {
		t.Fatalf("a nil block produced %s", ip)
	}
	if ip := FakeIPAddress("", mustCIDR(t, "198.18.0.0/15")); ip != nil {
		t.Fatalf("an empty name produced %s", ip)
	}
}

func mustCIDR(t *testing.T, raw string) *net.IPNet {
	t.Helper()
	_, network, err := net.ParseCIDR(raw)
	if err != nil {
		t.Fatal(err)
	}
	return network
}
