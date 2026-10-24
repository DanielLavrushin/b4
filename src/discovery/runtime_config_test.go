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

func TestARunRecordsTheIPVersionItsDSCPValueWasLimitedTo(t *testing.T) {
	both := &config.DSCPProbe{Value: 25, IPv4: true, IPv6: true}
	ipv4 := &config.DSCPProbe{Value: 25, IPv4: true}
	ipv6 := &config.DSCPProbe{Value: 25, IPv6: true}
	for _, tc := range []struct {
		name           string
		asked, stamped *config.DSCPProbe
		want           string
	}{
		{"both families asked, IPv4 stamped", both, ipv4, "4"},
		{"both families asked, IPv6 stamped", both, ipv6, "6"},
		{"both families asked and stamped", both, both, ""},
		{"an IPv4 run stamped", ipv4, ipv4, ""},
	} {
		suite := &CheckSuite{}
		suite.recordStamp(tc.asked, tc.stamped, nil)
		if suite.DSCP == nil || *suite.DSCP != 25 || suite.DSCPIPVersion != tc.want {
			t.Errorf("%s: want DSCP 25 limited to %q, got %v, %q", tc.name, tc.want, suite.DSCP, suite.DSCPIPVersion)
		}
	}
	global := 30
	path := &config.DSCPPath{IPv4: &global, Interfaces: []string{"wan0"}}
	for _, asked := range []*config.DSCPProbe{both, nil} {
		suite := &CheckSuite{}
		suite.recordStamp(asked, nil, path)
		if suite.DSCP != nil || suite.DSCPIPVersion != "" || suite.DSCPPath != path {
			t.Errorf("a run whose probes carry no value of the set records none, but keeps the path its probes took, got %v, %q, %+v", suite.DSCP, suite.DSCPIPVersion, suite.DSCPPath)
		}
	}
}
