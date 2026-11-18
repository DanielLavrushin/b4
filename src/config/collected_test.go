package config

import (
	"bytes"
	"encoding/json"
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
