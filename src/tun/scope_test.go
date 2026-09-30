package tun

import (
	"strings"
	"testing"
)

func TestMissingForwardRules(t *testing.T) {
	if got := missingForwardRules(true, true, true, "b4tun0"); len(got) != 0 {
		t.Fatalf("with NOTRACK and SNAT in place forwarded traffic is safe to capture, got %v", got)
	}
	cases := []struct {
		name                       string
		notrack, clientNotrack, sn bool
		want                       string
	}{
		{"no raw table", false, false, true, "NOTRACK"},
		{"client NOTRACK only", true, false, true, "NOTRACK"},
		{"no SNAT", true, true, false, "the SNAT for b4tun0"},
	}
	for _, c := range cases {
		got := strings.Join(missingForwardRules(c.notrack, c.clientNotrack, c.sn, "b4tun0"), " and ")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: missing = %q, want it to name %q", c.name, got, c.want)
		}
	}
	if got := missingForwardRules(false, false, false, "b4tun0"); len(got) != 2 {
		t.Fatalf("both failures must be named once each, got %v", got)
	}
}

func TestOwnTunNatRuleMatchesOnlyExactSpecs(t *testing.T) {
	cases := []struct {
		line string
		own  bool
	}{
		{"-A POSTROUTING -o b4tun0 -j ACCEPT", true},
		{"-A POSTROUTING -o b4tun0 -j SNAT --to-source 192.168.0.10", true},
		{"-A POSTROUTING -s 10.1.0.0/16 -o b4tun0 -j ACCEPT", false},
		{"-A POSTROUTING ! -o b4tun0 -j ACCEPT", false},
		{"-A POSTROUTING -s 10.3.0.0/16 ! -o b4tun0 -j SNAT --to-source 10.9.0.2", false},
		{"-A POSTROUTING -o b4tun0 -j MASQUERADE", false},
		{"-A POSTROUTING -o b4tun1 -j ACCEPT", false},
		{"-A POSTROUTING -o b4tun0 -j SNAT --to-source 10.9.0.2 --random", false},
		{"-P POSTROUTING ACCEPT", false},
	}
	for _, c := range cases {
		if _, got := ownTunNatRule(c.line, "b4tun0"); got != c.own {
			t.Errorf("ownTunNatRule(%q) = %v, want %v", c.line, got, c.own)
		}
	}
}

func TestCaptureSummaryNamesTheScope(t *testing.T) {
	r := &routeManager{tcpPorts: []string{"443"}, udpPorts: []string{"443"}, tcpLimit: 19, udpLimit: 8}
	if got := r.captureSummary(); got != "first 19 tcp / 8 udp packets on tcp ports 443, udp ports 443 + DNS" {
		t.Errorf("full scope summary = %q", got)
	}
	r.noConnbytes, r.localOnly = true, true
	if got := r.captureSummary(); got != "whole connections on tcp ports 443, udp ports 443 + DNS, this device's own traffic only" {
		t.Errorf("local-only summary without connbytes = %q", got)
	}
}

func TestNatGuardSpec(t *testing.T) {
	r := &routeManager{tunName: "b4tun0"}
	if got := strings.Join(r.natGuardSpec(), " "); got != "-o b4tun0 -j ACCEPT" {
		t.Fatalf("natGuardSpec = %q", got)
	}
}

func TestNatGuardShadowedLikeTheSNAT(t *testing.T) {
	own := "-A POSTROUTING -o b4tun0 -j ACCEPT\n"
	guard := "-o b4tun0 -j ACCEPT"
	cases := []struct {
		name string
		dump string
		want bool
	}{
		{"guard first", own + "-A POSTROUTING -j MASQUERADE\n", false},
		{"catch-all masquerade first", "-A POSTROUTING -j MASQUERADE\n" + own, true},
		{"docker style negated interface first", "-A POSTROUTING -s 172.17.0.0/16 ! -o docker0 -j MASQUERADE\n" + own, true},
		{"masquerade pinned to the uplink first", "-A POSTROUTING -o eth0 -j MASQUERADE\n" + own, false},
		{"stale SNAT for the tunnel first", "-A POSTROUTING -o b4tun0 -j SNAT --to-source 192.168.0.10\n" + own, true},
	}
	for _, c := range cases {
		if got := natRuleShadowed(c.dump, guard, "b4tun0"); got != c.want {
			t.Errorf("%s: natRuleShadowed = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSteerSpecsWithoutConnbytesTakeWholeConnections(t *testing.T) {
	for _, multiport := range []bool{true, false} {
		r := &routeManager{
			multiport:   multiport,
			noConnbytes: true,
			tcpPorts:    []string{"443", "8443"},
			udpPorts:    []string{"443"},
			tcpLimit:    19,
			udpLimit:    8,
		}
		specs := r.steerSpecs()
		if len(specs) == 0 {
			t.Fatalf("multiport=%v: no steer specs", multiport)
		}
		for _, s := range specs {
			j := strings.Join(s, " ")
			if strings.Contains(j, "connbytes") {
				t.Errorf("multiport=%v: a kernel without xt_connbytes cannot load %q", multiport, j)
			}
			if !strings.HasSuffix(j, "-j MARK --set-xmark 0x40000000/0x40000000") {
				t.Errorf("multiport=%v: steer spec does not end in MARK: %q", multiport, j)
			}
		}
	}
}
