package config

import "testing"

func TestWithCollectedIPsLeavesAConfigWithoutConsumersAlone(t *testing.T) {
	cfg := NewConfig()
	plain := NewSetConfig()
	plain.Id, plain.Enabled = "plain", true
	off := NewSetConfig()
	off.Id, off.Enabled, off.Targets.UseCollectedIPs = "off", false, true
	cfg.Sets = []*SetConfig{&plain, &off}
	if cfg.WithCollectedIPs([]string{"203.0.113.7"}) != &cfg {
		t.Fatal("a config without an enabled consumer got a copy")
	}
	off.Enabled = true
	view := cfg.WithCollectedIPs([]string{"203.0.113.7"})
	if view == &cfg || len(view.Sets[1].Targets.CollectedIPs) != 1 || len(off.Targets.CollectedIPs) != 0 {
		t.Fatal("an enabled consumer must get a separate view that carries the address")
	}
	discovery := *view
	discovery.Queue.IsDiscovery = true
	stripped := discovery.WithCollectedIPs(nil)
	if stripped == &discovery || len(stripped.Sets[1].Targets.CollectedIPs) != 0 {
		t.Fatal("a Discovery copy of a view kept the collected addresses")
	}
}
