package config

import (
	"encoding/json"
	"errors"
	"testing"
)

func l7Set(t *testing.T) *SetConfig {
	t.Helper()
	set := NewSetConfig()
	set.Id = "l7"
	set.Name = "L7"
	set.Enabled = true
	set.UDP.DPortFilter = "50000-50100"
	set.UDP.FilterL7 = []string{UDPL7Discord}
	return &set
}

func l7Config(t *testing.T, set *SetConfig) *Config {
	t.Helper()
	cfg := NewConfig()
	cfg.Sets = []*SetConfig{set}
	return &cfg
}

func TestNormalizeL7FilterCleansTheList(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []string
		want []string
	}{
		{"empty stays nil", nil, nil},
		{"blanks are dropped", []string{"", "  "}, nil},
		{"case and spacing are normalised", []string{" Discord "}, []string{UDPL7Discord}},
		{"duplicates are dropped", []string{"stun", "STUN", "stun"}, []string{UDPL7STUN}},
		{"order is kept", []string{"stun", "discord"}, []string{UDPL7STUN, UDPL7Discord}},
		{"an unknown value is kept", []string{"wireguard"}, []string{"wireguard"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizeL7Filter(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestAnL7SetNeedsItsOwnPorts(t *testing.T) {
	set := l7Set(t)
	set.UDP.DPortFilter = ""
	if err := l7Config(t, set).Validate(); err == nil {
		t.Fatal("a set selecting UDP protocols without a port filter must be refused")
	}
}

func TestAnL7SetRefusesDestinationTargets(t *testing.T) {
	set := l7Set(t)
	set.Targets.IPs = []string{"1.2.3.4/32"}
	if err := l7Config(t, set).Validate(); err == nil {
		t.Fatal("a set selecting UDP protocols with destination targets must be refused")
	}
}

func TestAnL7SetRefusesATCPPortFilter(t *testing.T) {
	for _, classes := range [][]string{{UDPL7Discord}, {UDPL7Discord, UDPL7STUN}, {"wireguard"}} {
		set := l7Set(t)
		set.UDP.FilterL7 = classes
		set.TCP.DPortFilter = "443, 2053"
		err := l7Config(t, set).Validate()
		var invalid *ValidationError
		if !errors.As(err, &invalid) || len(invalid.Fields) != 1 {
			t.Fatalf("%v: a set that selects UDP protocols has no targets, so a TCP port filter must be refused, got %v", classes, err)
		}
		if f := invalid.Fields[0]; f.Path != "sets[0].tcp.dport_filter" || f.Code != "conflict" || f.Params["ports"] != "443,2053" {
			t.Errorf("%v: expected a conflict on sets[0].tcp.dport_filter naming the ports, got %+v", classes, f)
		}
	}
}

func TestOnlyAnL7SetRefusesATCPPortFilter(t *testing.T) {
	l7 := l7Set(t)
	if err := l7Config(t, l7).Validate(); err != nil {
		t.Fatalf("a set that selects UDP protocols without a TCP port filter must save: %v", err)
	}

	ports := NewSetConfig()
	ports.Id = "ports"
	ports.Name = "Ports"
	ports.Enabled = true
	ports.TCP.DPortFilter = "443,2053"
	ports.UDP.DPortFilter = "50000-50100"
	if err := l7Config(t, &ports).Validate(); err != nil {
		t.Fatalf("a set without targets or protocol selection keeps its TCP port filter: %v", err)
	}
	if ports.TCP.DPortFilter != "443,2053" {
		t.Fatalf("the TCP port filter of a port-only set must stay, got %q", ports.TCP.DPortFilter)
	}
}

func TestAnL7SetAcceptsASourceDeviceScope(t *testing.T) {
	set := l7Set(t)
	set.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	if err := l7Config(t, set).Validate(); err != nil {
		t.Fatalf("source devices must stay allowed on an L7 set: %v", err)
	}
}

func TestAnUnknownL7ClassIsKeptAndValidates(t *testing.T) {
	set := l7Set(t)
	set.UDP.FilterL7 = []string{"wireguard"}
	if err := l7Config(t, set).Validate(); err != nil {
		t.Fatalf("an unknown class must not refuse the config: %v", err)
	}
	if !set.SelectsL7() || set.SelectsL7Class(UDPL7Discord) || SelectsKnownL7(set.UDP.FilterL7) {
		t.Fatal("an unknown class must select nothing this version knows")
	}
	if !SelectsKnownL7([]string{"wireguard", UDPL7STUN}) {
		t.Fatal("a known class next to an unknown one selects something this version knows")
	}
}

func TestL7FilterIsOmittedFromASparseSet(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "plain"
	set.Name = "Plain"
	cfg.Sets = []*SetConfig{&set}

	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatalf("MarshalSparse: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sets, _ := raw["sets"].([]any)
	if len(sets) != 1 {
		t.Fatalf("got %d sets", len(sets))
	}
	entry, _ := sets[0].(map[string]any)
	udp, _ := entry["udp"].(map[string]any)
	if _, found := udp["filter_l7"]; found {
		t.Fatal("a set that selects no protocol must not write filter_l7")
	}
}

func TestASTUNFakeLengthIsNormalised(t *testing.T) {
	for _, c := range []struct{ in, want int }{
		{0, 20}, {19, 20}, {20, 20}, {21, 24}, {23, 24}, {24, 24}, {64, 64}, {100, 100}, {101, 100}, {1203, 1200},
		{65504, 65504}, {65507, 65504}, {65556, 65504}, {1 << 20, 65504},
	} {
		if got := NormalizeSTUNFakeLen(c.in); got != c.want {
			t.Fatalf("NormalizeSTUNFakeLen(%d) = %d, want %d", c.in, got, c.want)
		}
	}

	set := NewSetConfig()
	set.Id = "stun-fake"
	set.Name = "StunFake"
	set.UDP.Mode = UDPModeFake
	set.UDP.FakePayloadFile = FakePayloadAutoSTUN
	set.UDP.FakeLen = 101
	if err := l7Config(t, &set).Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if set.UDP.FakeLen != 100 {
		t.Fatalf("fake length %d, want it adjusted to 100", set.UDP.FakeLen)
	}
}
