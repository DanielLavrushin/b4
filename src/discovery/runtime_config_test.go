package discovery

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestDiscoveryPoolConfigKeepsTheRunsMarks(t *testing.T) {
	cfg := config.NewConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
	flow, injected := cfg.DiscoveryFlowMark(), cfg.DiscoveryInjectedMark()

	pool := discoveryPoolConfig(&cfg, 541, 1, flow, injected)

	if pool.Queue.Mark != injected {
		t.Fatalf("the Discovery pool sends its packets with the injected mark 0x%x, got 0x%x", injected, pool.Queue.Mark)
	}
	if pool.DiscoveryFlowMark() != flow || pool.DiscoveryInjectedMark() != injected {
		t.Errorf("the pool runs with its queue mark set to the injected mark, so marks derived from it read 0x%x and 0x%x instead of the run's 0x%x and 0x%x",
			pool.DiscoveryFlowMark(), pool.DiscoveryInjectedMark(), flow, injected)
	}
	if !pool.Queue.IsDiscovery || !pool.System.Tables.SkipSetup || pool.Queue.StartNum != 541 || pool.Queue.Threads != 1 {
		t.Errorf("pool config lost its Discovery queue settings: %+v", pool.Queue)
	}
	if cfg.Queue.Mark == injected || cfg.System.Checker.DiscoveryFlowMark != 0 {
		t.Errorf("building the pool config changed the main configuration")
	}
}

func TestTheRunsProbesCarryTheSetsDSCPInEveryFamilyTheRunTests(t *testing.T) {
	ipv4Only := &config.DSCPProbe{Value: 25, IPv4: true}
	for _, tc := range []struct {
		ipVersion  string
		ipv4, ipv6 bool
	}{
		{"ipv6", false, true},
		{"ipv4", true, false},
		{"auto", true, true},
		{"", true, true},
	} {
		got := probeForRun(ipv4Only, tc.ipVersion)
		if got == nil || got.Value != 25 || got.IPv4 != tc.ipv4 || got.IPv6 != tc.ipv6 {
			t.Errorf("%q: a set made from the run's result carries DSCP 25 in every family the run tested, so the probes must carry it there too, got %+v", tc.ipVersion, got)
		}
	}
	if !ipv4Only.IPv4 || ipv4Only.IPv6 {
		t.Errorf("the set's own probe must stay as it was, got %+v", ipv4Only)
	}
	if probeForRun(nil, "ipv6") != nil {
		t.Error("a run for no set value carries none")
	}
}
