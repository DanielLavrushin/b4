package mtproto

import (
	"context"
	"net"
	"testing"
)

func withIPv6(t *testing.T, allowed bool) {
	t.Helper()
	prev := dialIPv6Probe
	dialIPv6Probe = func() bool { return allowed }
	t.Cleanup(func() { dialIPv6Probe = prev })
}

func setIPv6(allowed bool) { dialIPv6Probe = func() bool { return allowed } }

func TestDialNetworkFollowsHostIPv6Reachability(t *testing.T) {
	withIPv6(t, true)
	if got := dialNetwork(); got != "tcp" {
		t.Fatalf("dialNetwork() = %q with IPv6 on, want tcp", got)
	}
	setIPv6(false)
	if got := dialNetwork(); got != "tcp4" {
		t.Fatalf("dialNetwork() = %q with IPv6 off, want tcp4", got)
	}
}

func TestDialFamilyAllows(t *testing.T) {
	v4 := net.ParseIP("104.21.84.223")
	v6 := net.ParseIP("2606:4700:3034::6815:54df")

	withIPv6(t, true)
	if !dialFamilyAllows(v4) || !dialFamilyAllows(v6) {
		t.Fatalf("with IPv6 on both families must be allowed")
	}

	setIPv6(false)
	if !dialFamilyAllows(v4) {
		t.Fatalf("IPv4 must stay allowed when IPv6 is off")
	}
	if dialFamilyAllows(v6) {
		t.Fatalf("IPv6 must be refused when IPv6 is off, a dial to it fails with EPERM on a box with no v6 route")
	}
}

func TestWsDialEndpointsDropsAnIPv6LiteralWhenIPv6IsOff(t *testing.T) {
	withIPv6(t, false)
	if eps := wsDialEndpoints(context.Background(), "2606:4700:3034::6815:54df", "kws2.example"); len(eps) != 0 {
		t.Fatalf("got %v, want no endpoint", eps)
	}
	if eps := wsDialEndpoints(context.Background(), "104.21.84.223", "kws2.example"); len(eps) != 1 || eps[0] != "104.21.84.223" {
		t.Fatalf("got %v, want the IPv4 literal back", eps)
	}
}

func TestWsDialEndpointsKeepsAnIPv6LiteralWhenIPv6IsOn(t *testing.T) {
	withIPv6(t, true)
	eps := wsDialEndpoints(context.Background(), "2606:4700:3034::6815:54df", "kws2.example")
	if len(eps) != 1 || eps[0] != "2606:4700:3034::6815:54df" {
		t.Fatalf("got %v, want the IPv6 literal back", eps)
	}
}
