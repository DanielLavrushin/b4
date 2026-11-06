package dns

import (
	"net"
	"strconv"
	"testing"
	"time"
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

func collidingFakeNames(t *testing.T, network *net.IPNet, count int) (string, string) {
	t.Helper()
	owner := map[string]string{}
	for i := 0; i < count; i++ {
		name := "host" + strconv.Itoa(i) + ".example"
		ip := FakeIPAddress(name, network)
		if ip == nil {
			t.Fatalf("%s got no address", name)
		}
		key := ip.String()
		if prev, taken := owner[key]; taken && prev != name {
			return prev, name
		}
		owner[key] = name
	}
	t.Fatalf("no two names out of %d landed on one address", count)
	return "", ""
}

func TestFakeIPPoolGivesCollidingNamesDifferentAddresses(t *testing.T) {
	network := mustCIDR(t, "198.18.0.0/15")
	first, second := collidingFakeNames(t, network, 20000)
	if !FakeIPAddress(first, network).Equal(FakeIPAddress(second, network)) {
		t.Fatalf("%s and %s stopped colliding, the fixture is stale", first, second)
	}

	pool := NewFakeIPPool()
	a := pool.Address(first, network)
	b := pool.Address(second, network)
	if a == nil || b == nil {
		t.Fatal("the pool refused a name")
	}
	if a.Equal(b) {
		t.Fatalf("both names got %s, so a non-sniffable connection cannot be dialled by name", a)
	}
	if !network.Contains(a) || !network.Contains(b) {
		t.Fatalf("addresses %s and %s are outside %s", a, b, network)
	}
	if again := pool.Address(first, network); !again.Equal(a) {
		t.Fatalf("%s moved from %s to %s", first, a, again)
	}
}

func TestFakeIPPoolReusesAnAddressOnlyAfterTheLeaseExpires(t *testing.T) {
	network := mustCIDR(t, "198.18.0.0/24")
	pool := NewFakeIPPool()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	pool.now = func() time.Time { return now }

	held := pool.Address("held.example", network)
	pool.now = func() time.Time { return now.Add(2 * FakeIPLeaseTTL) }
	if ip := pool.Address("held.example", network); !ip.Equal(held) {
		t.Fatalf("a name lost its address after the lease expired: %s then %s", held, ip)
	}
	fresh := pool.Address("fresh.example", network)
	if !network.Contains(fresh) {
		t.Fatalf("%s is outside %s", fresh, network)
	}
}

func TestFakeIPPoolServesAFullBlockWithSERVFAIL(t *testing.T) {
	network := mustCIDR(t, "198.18.0.0/24")
	pool := NewFakeIPPool()

	owners := map[string]string{}
	granted := 0
	refused := 0
	for i := 0; i < 300; i++ {
		name := "host" + strconv.Itoa(i) + ".example"
		ip := pool.Address(name, network)
		if ip == nil {
			refused++
			continue
		}
		granted++
		key := ip.String()
		if prev, taken := owners[key]; taken && prev != name {
			t.Fatalf("%s was handed to both %s and %s", key, prev, name)
		}
		owners[key] = name
		if owner, ok := pool.Owner(ip); !ok || owner != name {
			t.Fatalf("the reverse map says %q for %s, want %q", owner, key, name)
		}
	}
	if granted != 254 {
		t.Fatalf("a /24 gave out %d addresses, want 254", granted)
	}
	if refused != 46 {
		t.Fatalf("%d names were refused, want 46", refused)
	}
	if again := pool.Address("host0.example", network); again == nil {
		t.Fatal("a name that already holds an address was refused when the block filled up")
	}
}

func TestFakeIPPoolKeepsAnAddressForTheAnswerTTL(t *testing.T) {
	network := mustCIDR(t, "198.18.0.0/24")
	pool := NewFakeIPPool()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	pool.now = func() time.Time { return now }

	held := pool.AddressFor("long-ttl.example", network, 4*time.Hour)
	if held == nil {
		t.Fatal("no address for the first name")
	}
	now = now.Add(2 * time.Hour)
	if owner, ok := pool.Owner(held); !ok || owner != "long-ttl.example" {
		t.Fatalf("the address lost its owner while the answer was still live: %q, %v", owner, ok)
	}
	for i := 0; i < 250; i++ {
		if ip := pool.Address("other"+strconv.Itoa(i)+".example", network); ip != nil && ip.Equal(held) {
			t.Fatalf("%s was handed to a second name while the first answer was still live", held)
		}
	}
	now = now.Add(3 * time.Hour)
	if _, ok := pool.Owner(held); ok {
		t.Fatal("the address is still owned after both the lease and the answer expired")
	}
}
