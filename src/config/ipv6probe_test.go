package config

import (
	"net"
	"testing"
)

func TestIsGlobalRoutableIPv6(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"2001:db8::1", true},
		{"2a00:1450:4001:80f::200e", true},
		{"fd00::1", false},
		{"fdcc:1234::abcd", false},
		{"fc00::1", false},
		{"fe80::1", false},
		{"::1", false},
		{"ff02::1", false},
		{"::", false},
		{"192.0.2.1", false},
	}

	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			ip := net.ParseIP(tc.addr)
			if ip == nil {
				t.Fatalf("unparsable test address %q", tc.addr)
			}
			if got := isGlobalRoutableIPv6(ip); got != tc.want {
				t.Errorf("isGlobalRoutableIPv6(%s) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}

	if isGlobalRoutableIPv6(nil) {
		t.Error("a nil address is not a routable IPv6 address")
	}
}

func TestULAOnlyHostIsNotDualStack(t *testing.T) {
	ula := net.ParseIP("fd12:3456::1")
	if ula.To4() != nil || !ula.IsGlobalUnicast() {
		t.Fatal("fd12:3456::1 is expected to be an IsGlobalUnicast IPv6 address, which is why the probe needs the extra ULA check")
	}
	if isGlobalRoutableIPv6(ula) {
		t.Error("a host whose only IPv6 address is a ULA has no route to the IPv6 internet and must not be reported as dual-stack")
	}
}
