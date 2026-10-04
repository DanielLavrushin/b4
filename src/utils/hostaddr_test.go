package utils

import (
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

func fakeHostAddrs(t *testing.T, cidrs ...string) *int {
	t.Helper()
	calls := 0
	prev := interfaceAddrs
	interfaceAddrs = func() ([]net.Addr, error) {
		calls++
		var out []net.Addr
		for _, c := range cidrs {
			ip, n, err := net.ParseCIDR(c)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, &net.IPNet{IP: ip, Mask: n.Mask})
		}
		return out, nil
	}
	t.Cleanup(func() {
		interfaceAddrs = prev
		hostAddrs.mu.Lock()
		hostAddrs.addrs = nil
		hostAddrs.mu.Unlock()
	})
	hostAddrs.mu.Lock()
	hostAddrs.addrs = nil
	hostAddrs.mu.Unlock()
	return &calls
}

func TestIsHostAddr(t *testing.T) {
	fakeHostAddrs(t, "192.168.1.1/24", "10.9.0.2/30", "fd00::1/64")

	for _, tc := range []struct {
		addr string
		want bool
	}{
		{"192.168.1.1", true},
		{"::ffff:192.168.1.1", true},
		{"10.9.0.2", true},
		{"fd00::1", true},
		{"127.0.0.1", true},
		{"127.0.0.53", true},
		{"::1", true},
		{"192.168.1.5", false},
		{"1.1.1.1", false},
		{"0.0.0.0", false},
		{"::", false},
	} {
		if got := IsHostAddr(netip.MustParseAddr(tc.addr)); got != tc.want {
			t.Errorf("IsHostAddr(%s) = %v, want %v", tc.addr, got, tc.want)
		}
	}
	if IsHostAddr(netip.Addr{}) {
		t.Error("the zero address belongs to no host")
	}
	if !IsHostIP(net.ParseIP("192.168.1.1")) || !IsHostIP(net.ParseIP("192.168.1.1").To4()) || IsHostIP(nil) || IsHostIP(net.ParseIP("192.168.1.5")) {
		t.Error("IsHostIP must agree with IsHostAddr for both byte forms of an IPv4 address")
	}
}

func TestHostAddrsAreReadAgainAfterTheyGoStale(t *testing.T) {
	calls := fakeHostAddrs(t, "192.168.1.1/24")

	for i := 0; i < 5; i++ {
		IsHostAddr(netip.MustParseAddr("1.1.1.1"))
	}
	if *calls != 1 {
		t.Fatalf("the interface addresses were read %d times for five lookups, want once", *calls)
	}
	hostAddrs.mu.Lock()
	hostAddrs.at = time.Now().Add(-2 * hostAddrsTTL)
	hostAddrs.mu.Unlock()
	IsHostAddr(netip.MustParseAddr("1.1.1.1"))
	if *calls != 2 {
		t.Fatalf("stale addresses must be read again, got %d reads", *calls)
	}
	if IsHostAddr(netip.MustParseAddr("127.0.0.1")); *calls != 2 {
		t.Fatal("a loopback address needs no interface lookup")
	}
}

func TestHostAddrsWithoutAnInterfaceList(t *testing.T) {
	fakeHostAddrs(t)
	interfaceAddrs = func() ([]net.Addr, error) { return nil, errors.New("netlink refused") }

	if IsHostAddr(netip.MustParseAddr("192.168.1.1")) {
		t.Error("without the interface list only loopback is known to be this host")
	}
	if !IsHostAddr(netip.MustParseAddr("127.0.0.1")) {
		t.Error("loopback is this host whatever the interface list says")
	}
}
