package config

import (
	"slices"
	"testing"
)

func portRoutedSet() *SetConfig {
	s := NewSetConfig()
	s.Id = "port-routed"
	s.Name = "PortRouted"
	s.Enabled = true
	s.Routing.Enabled = true
	s.Routing.Mode = RoutingModeProxy
	s.Routing.Upstream.Port = 1080
	s.Routing.Upstream.UDP = true
	s.Routing.SourceInterfaces = []string{"br0"}
	s.UDP.DPortFilter = "50000-50100,19294-19344"
	return &s
}

func TestRoutesUDPByPort(t *testing.T) {
	if s := portRoutedSet(); !s.RoutesUDPByPort() || !s.WantsUDPPortRouting() {
		t.Fatal("a proxy set with UDP through the upstream, UDP ports, no destination target and a source interface routes its UDP by port")
	}

	for _, tc := range []struct {
		name   string
		change func(*SetConfig)
		wants  bool
	}{
		{"no source scope", func(s *SetConfig) { s.Routing.SourceInterfaces = nil }, true},
		{"an excluded device list is no scope", func(s *SetConfig) {
			s.Routing.SourceInterfaces = nil
			s.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
			s.Targets.SourceDevicesExclude = true
		}, true},
		{"routing off", func(s *SetConfig) { s.Routing.Enabled = false }, false},
		{"UDP stays off the upstream", func(s *SetConfig) { s.Routing.Upstream.UDP = false }, false},
		{"interface mode", func(s *SetConfig) { s.Routing.Mode = RoutingModeInterface }, false},
		{"block mode", func(s *SetConfig) { s.Routing.Mode = RoutingModeBlock }, false},
		{"no UDP ports", func(s *SetConfig) { s.UDP.DPortFilter = "" }, false},
		{"only the DNS port", func(s *SetConfig) { s.UDP.DPortFilter = "53" }, false},
		{"an IP target", func(s *SetConfig) { s.Targets.IPs = []string{"1.2.3.4"} }, false},
		{"a domain target", func(s *SetConfig) { s.Targets.SNIDomains = []string{"example.com"} }, false},
		{"an ASN that resolved to nothing yet", func(s *SetConfig) { s.Targets.ASNs = []string{"15169"} }, false},
		{"handles only calls", func(s *SetConfig) { s.UDP.FilterL7 = []string{"stun"} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := portRoutedSet()
			tc.change(s)
			if s.RoutesUDPByPort() {
				t.Error("the set must not route UDP by port")
			}
			if got := s.WantsUDPPortRouting(); got != tc.wants {
				t.Errorf("WantsUDPPortRouting = %v, want %v", got, tc.wants)
			}
			if spec := s.RoutedUDPPortSpec(); spec != "" {
				t.Errorf("a set that routes nothing by port still names ports %q", spec)
			}
		})
	}

	t.Run("included source devices are a scope", func(t *testing.T) {
		s := portRoutedSet()
		s.Routing.SourceInterfaces = nil
		s.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
		if !s.RoutesUDPByPort() {
			t.Error("a set limited to listed devices is scoped")
		}
	})
}

func TestRoutedUDPPortSpecDropsDNSAndMergesRanges(t *testing.T) {
	s := portRoutedSet()
	s.UDP.DPortFilter = "50-60,53,55-70,100,101,443"
	if got, want := s.RoutedUDPPortSpec(), "50-52,54-70,100-101,443"; got != want {
		t.Errorf("RoutedUDPPortSpec = %q, want %q: port 53 stays with DNS handling and overlapping ranges merge", got, want)
	}
}

func TestAPortRoutedSetStaysOutOfTheEnginesPorts(t *testing.T) {
	routed := portRoutedSet()
	routed.UDP.DPortFilter = "50000-50100"
	calls := NewSetConfig()
	calls.Id = "calls"
	calls.Enabled = true
	calls.UDP.FilterL7 = []string{"stun"}
	calls.UDP.DPortFilter = "50000-50100"
	plain := NewSetConfig()
	plain.Id = "plain"
	plain.Enabled = true
	plain.Targets.IPs = []string{"1.2.3.4"}

	cfg := NewConfig()
	cfg.Sets = []*SetConfig{routed, &calls, &plain}
	cfg.BuildSetPortRanges()

	if plain.MatchesUDPDPort(50050) {
		t.Error("a port that a set for calls lists stays out of an address set, also when a port-routed set lists it too")
	}

	cfg.Sets = []*SetConfig{routed}
	if ports := cfg.CollectUDPPorts(); !slices.Equal(ports, []string{"443"}) {
		t.Errorf("queued UDP ports = %v, want only 443: the firewall takes a port-routed set's UDP before the queue", ports)
	}

	routed.Routing.SourceInterfaces = nil
	if ports := cfg.CollectUDPPorts(); !slices.Equal(ports, []string{"443", "50000-50100"}) {
		t.Errorf("queued UDP ports = %v: without a source scope the set routes nothing and acts on its ports in the engine as before", ports)
	}
}
