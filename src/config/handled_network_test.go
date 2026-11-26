package config

import "testing"

func TestHandledNetworkIsTheFamilyTheRunningEngineProcesses(t *testing.T) {
	t.Cleanup(func() { SetRunningEngineTUN(false) })
	for _, tc := range []struct {
		name       string
		ipv4, ipv6 bool
		mode       string
		running    bool
		want       string
	}{
		{"both families", true, true, "nfqueue", false, ""},
		{"IPv4 only", true, false, "nfqueue", false, "tcp4"},
		{"IPv6 only", false, true, "nfqueue", false, "tcp6"},
		{"neither", false, false, "nfqueue", false, "tcp4"},
		{"TUN handles no IPv6", true, true, "tun", true, "tcp4"},
		{"TUN handles IPv4 whatever the switch says", false, true, "tun", true, "tcp4"},
		{"a saved switch to TUN waits for the restart", true, true, "tun", false, ""},
		{"a saved switch away from TUN waits for the restart", true, true, "nfqueue", true, "tcp4"},
	} {
		SetRunningEngineTUN(tc.running)
		cfg := NewConfig()
		cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled, cfg.Queue.Mode = tc.ipv4, tc.ipv6, tc.mode
		if got := cfg.HandledNetwork(); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
