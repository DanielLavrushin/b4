package config

import "testing"

const ipv6RouteSample = `fe800000000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000001 00000000 00000001 eth0
00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000000 00000000 00000003 eth0
00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 ffffffff 00000001 00000000 00200001 lo
`

func TestParseIPv6DefaultRouteFindsARealDefault(t *testing.T) {
	if !parseIPv6DefaultRoute(ipv6RouteSample) {
		t.Fatalf("a default route via fe80::1 on eth0 must count")
	}
}

func TestParseIPv6DefaultRouteIgnoresLoopbackAndUnreachable(t *testing.T) {
	onlyLoopback := `00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 ffffffff 00000001 00000000 00200001 lo
`
	if parseIPv6DefaultRoute(onlyLoopback) {
		t.Fatalf("the kernel's unreachable default on lo is not connectivity")
	}

	rejected := `00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 00000400 00000000 00000000 00000201 eth0
`
	if parseIPv6DefaultRoute(rejected) {
		t.Fatalf("a rejecting default route is not connectivity")
	}

	down := `00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000000 00000000 00000002 eth0
`
	if parseIPv6DefaultRoute(down) {
		t.Fatalf("a route without RTF_UP is not connectivity")
	}
}

func TestParseIPv6DefaultRouteIgnoresNonDefaults(t *testing.T) {
	onSubnet := `fe800000000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000001 00000000 00000001 eth0
`
	if parseIPv6DefaultRoute(onSubnet) {
		t.Fatalf("a link-local subnet route is not a default route")
	}
	if parseIPv6DefaultRoute("") {
		t.Fatalf("no routes means no default route")
	}
	if parseIPv6DefaultRoute("garbage\n00000000 00\n") {
		t.Fatalf("short lines must be skipped")
	}
}

func TestHostCanReachIPv6NeedsBothAnAddressAndARoute(t *testing.T) {
	prevAddr, prevRoute := hostIPv6Probe, hostIPv6RouteProbe
	t.Cleanup(func() {
		hostIPv6Probe, hostIPv6RouteProbe = prevAddr, prevRoute
		hostIPv6Known, hostIPv6RouteKnown = false, false
	})

	cases := []struct {
		addr, route, want bool
	}{
		{true, true, true},
		{true, false, false},
		{false, true, false},
		{false, false, false},
	}
	for _, c := range cases {
		hostIPv6Probe = func() bool { return c.addr }
		hostIPv6RouteProbe = func() bool { return c.route }
		hostIPv6Known, hostIPv6RouteKnown = false, false
		if got := HostCanReachIPv6(); got != c.want {
			t.Fatalf("address=%v route=%v: HostCanReachIPv6() = %v, want %v", c.addr, c.route, got, c.want)
		}
	}
}
