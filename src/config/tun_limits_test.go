package config

import "testing"

func hasLimit(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestTUNLimitationsEmptyForNFQueue(t *testing.T) {
	c := &Config{}
	if got := TUNLimitations(c); got != nil {
		t.Errorf("nfqueue mode must report no TUN limitations, got %v", got)
	}
}

func TestTUNLimitationsAlwaysReportsDiscovery(t *testing.T) {
	c := &Config{}
	c.Queue.Mode = "tun"
	if !hasLimit(TUNLimitations(c), "discovery") {
		t.Error("discovery is unavailable in TUN mode and must always be reported")
	}
}

func TestTUNLimitationsFlagsIPv6AndWatchdog(t *testing.T) {
	c := &Config{}
	c.Queue.Mode = "tun"
	c.Queue.IPv6Enabled = true
	c.System.Checker.Watchdog.Enabled = true

	got := TUNLimitations(c)
	if !hasLimit(got, "ipv6") {
		t.Error("IPv6 enabled in TUN mode must be reported as a limitation")
	}
	if !hasLimit(got, "watchdog_heal") {
		t.Error("watchdog healing is unavailable in TUN mode")
	}
}

func TestTUNLimitationsFlagsEgressIP(t *testing.T) {
	c := &Config{}
	c.Queue.Mode = "tun"
	set := &SetConfig{Name: "s", Enabled: true}
	set.Routing.Enabled = true
	set.Routing.EgressIP = "10.0.0.9"
	c.Sets = []*SetConfig{set}

	if !hasLimit(TUNLimitations(c), "egress_ip") {
		t.Error("a set pinning an egress IP must be reported in TUN mode")
	}
}

func TestIPv6BypassWarningCoversTUN(t *testing.T) {
	stubIPv6Host(t, true)

	cfg := NewConfig()
	cfg.Queue.Mode = "tun"
	cfg.Queue.IPv6Enabled = true
	if !IPv6BypassesSets(&cfg) {
		t.Error("TUN mode never forwards IPv6, so enabling the toggle must not silence the warning")
	}

	cfg.Queue.Mode = "nfqueue"
	if IPv6BypassesSets(&cfg) {
		t.Error("nfqueue mode with IPv6 enabled must stay silent")
	}
}
