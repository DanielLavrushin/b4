package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestCollectedDefaultsAndSparseRoundTrip(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "collected-test"
	cfg.Sets = []*SetConfig{&set}
	if set.TCP.IPBlockDetect.Collects() || set.Targets.UseCollectedIPs {
		t.Fatal("new settings must preserve legacy behavior")
	}
	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("use_collected_ips")) || bytes.Contains(data, []byte(`"action"`)) {
		t.Fatalf("default fields exported: %s", data)
	}
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.Action = "collect"
	set.Targets.UseCollectedIPs = true
	set.Targets.IPs = []string{"8.8.8.8"}
	set.Targets.IpsToMatch = set.Targets.IPs
	view := cfg.WithCollectedIPs([]string{"1.1.1.1"})
	data, err = MarshalSparse(view)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("1.1.1.1")) || !bytes.Contains(data, []byte(`"collect"`)) {
		t.Fatalf("runtime pool leaked or settings lost: %s", data)
	}
	loaded := NewConfig()
	if err = json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if err = loaded.Validate(); err != nil {
		t.Fatal(err)
	}
	if !loaded.Sets[0].TCP.IPBlockDetect.Collects() || !loaded.Sets[0].Targets.UseCollectedIPs {
		t.Fatal("settings did not round trip")
	}
	if len(set.Targets.CollectedIPs) != 0 || len(set.Targets.IPs) != 1 {
		t.Fatal("runtime expansion mutated source")
	}
}

func TestCollectedEmptyFamilyAndDeclaration(t *testing.T) {
	cfg := NewConfig()
	cfg.Queue.IPv6Enabled = true
	set := NewSetConfig()
	set.Id = "collected-test"
	set.Enabled = true
	set.Targets.UseCollectedIPs = true
	set.Targets.IPVersion = "4"
	cfg.Sets = []*SetConfig{&set}
	view := cfg.WithCollectedIPs(nil)
	if !set.DeclaresDestinationTargets() || !set.DeclaresIPTargets() || view.Sets[0].HasIPOrDomainTargets() {
		t.Fatal("empty pool became a catch-all or lost its declaration")
	}
	view = cfg.WithCollectedIPs([]string{"1.1.1.1", "2606:4700:4700::1111"})
	ips := view.Sets[0].TargetIPs()
	if len(ips) != 1 || ips[0] != "1.1.1.1" {
		t.Fatalf("family filter lost: %v", ips)
	}
	cfg.Queue.IsDiscovery = true
	if len(cfg.WithCollectedIPs([]string{"1.1.1.1"}).Sets[0].TargetIPs()) != 0 {
		t.Fatal("Discovery consumed the runtime pool")
	}
}

func TestCollectedInvalidActionAndReplyCapture(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "collected-test"
	set.Enabled = true
	set.TCP.IPBlockDetect.Enabled = true
	cfg.Sets = []*SetConfig{&set}
	set.TCP.IPBlockDetect.Action = "collect"
	if !cfg.AnySetTCPIncoming() {
		t.Fatal("collection needs server progress capture")
	}
	set.TCP.IPBlockDetect.Action = "probe"
	if err := cfg.Validate(); err == nil {
		t.Fatal("unknown detector action accepted")
	}
	set.TCP.IPBlockDetect.Action = ""
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if set.TCP.IPBlockDetect.Action != "block" {
		t.Fatal("legacy config did not keep Block")
	}
}

func TestCollectedTimeoutBounds(t *testing.T) {
	for _, timeout := range []int{-1, 999, 10001, int(^uint(0) >> 1)} {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "bound"
		set.TCP.IPBlockDetect.Action = "collect"
		set.TCP.IPBlockDetect.TimeoutMs = timeout
		cfg.Sets = []*SetConfig{&set}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("unsafe timeout accepted: %d", timeout)
		}
	}
}

func TestTargetChecksAllocateNothingWithAFullPool(t *testing.T) {
	set := NewSetConfig()
	set.Enabled = true
	set.Targets.UseCollectedIPs = true
	set.Targets.DomainOnly = true
	set.Targets.IpsToMatch = []string{"198.51.100.1"}
	for i := 0; i < 1024; i++ {
		set.Targets.CollectedIPs = append(set.Targets.CollectedIPs, fmt.Sprintf("203.0.%d.%d", i/256, i%256))
	}
	set.Routing.Enabled = true
	set.Routing.Mode = RoutingModeInterface
	set.Routing.EgressInterface = "wg0"
	if !set.HasIPOrDomainTargets() || !set.RoutesAddressesToInterface() {
		t.Fatal("a set with static and collected addresses lost its address targets")
	}
	if n := testing.AllocsPerRun(100, func() { _ = set.HasIPOrDomainTargets(); _ = set.RoutesAddressesToInterface() }); n != 0 {
		t.Fatalf("target checks allocated %v times per call", n)
	}
	set.Targets.IpsToMatch = nil
	if !set.RoutesAddressesToInterface() {
		t.Fatal("collected addresses alone no longer route a domain-only set")
	}
	set.Targets.UseCollectedIPs = false
	if set.HasIPOrDomainTargets() || set.RoutesAddressesToInterface() {
		t.Fatal("collected addresses a set does not use counted as its targets")
	}
}

func TestSwitchingASetToCollectRefreshesTheFirewall(t *testing.T) {
	withDetect := func(action string, useCollected bool) *Config {
		cfg := NewConfig()
		incoming := NewSetConfig()
		incoming.Id, incoming.Name, incoming.Enabled = "incoming", "incoming", true
		incoming.TCP.Incoming.Mode = "fake"
		detect := NewSetConfig()
		detect.Id, detect.Name, detect.Enabled = "detect", "detect", true
		detect.TCP.IPBlockDetect.Enabled = true
		detect.TCP.IPBlockDetect.Action = action
		detect.Targets.UseCollectedIPs = useCollected
		cfg.Sets = []*SetConfig{&incoming, &detect}
		return &cfg
	}
	for name, change := range map[string][2]*Config{
		"block to collect":                {withDetect("block", false), withDetect("collect", false)},
		"collect to block":                {withDetect("collect", false), withDetect("block", false)},
		"a collect set starts consuming":  {withDetect("collect", false), withDetect("collect", true)},
		"a collect set stops consuming":   {withDetect("collect", true), withDetect("collect", false)},
		"a collect set is turned off":     {withDetect("collect", false), withDetect("", false)},
		"a collect set is turned back on": {withDetect("", false), withDetect("collect", false)},
	} {
		t.Run(name, func(t *testing.T) {
			if !change[0].AnySetTCPIncoming() || !change[1].AnySetTCPIncoming() {
				t.Fatal("another set keeps Incoming on in both configs, or this test proves nothing")
			}
			if !FirewallRefreshNeeded(change[0], change[1]) {
				t.Fatal("whether a set collects decides whether the queue sees server replies, so the change needs a firewall refresh")
			}
		})
	}
	if FirewallRefreshNeeded(withDetect("collect", false), withDetect("collect", false)) {
		t.Fatal("an unchanged Collect set needs no firewall refresh")
	}
	if FirewallRefreshNeeded(withDetect("block", false), withDetect("block", true)) {
		t.Fatal("Use collected IPs on a Block set changes nothing the queue sees")
	}
}
