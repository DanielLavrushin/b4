package sni

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func portRoutedSet(name string) *config.SetConfig {
	s := portSet(name, "", "50000-50100")
	s.Routing.Enabled = true
	s.Routing.Mode = config.RoutingModeProxy
	s.Routing.Upstream.Port = 1080
	s.Routing.Upstream.UDP = true
	s.Routing.SourceInterfaces = []string{"br0"}
	return s
}

func TestAPortRoutedSetMatchesNoUDPInTheEngine(t *testing.T) {
	routed := portRoutedSet("routed")
	ss := NewSuffixSet([]*config.SetConfig{routed})
	if matched, set := ss.MatchUDPPort(50050, ""); matched {
		t.Errorf("the firewall takes a port-routed set's UDP, so what reaches the engine is outside its scope, got %q", set.Name)
	}

	below := portSet("below", "", "50000-50100")
	ss = NewSuffixSet([]*config.SetConfig{routed, below})
	if matched, set := ss.MatchUDPPort(50050, ""); !matched || set != below {
		t.Errorf("UDP the port-routed set leaves must reach the next set that lists the port, got matched=%v set=%v", matched, set)
	}

	routed.Routing.SourceInterfaces = nil
	ss = NewSuffixSet([]*config.SetConfig{routed})
	if matched, set := ss.MatchUDPPort(50050, ""); !matched || set != routed {
		t.Errorf("without a source scope the set routes nothing and stays a port-only set in the engine, got matched=%v", matched)
	}
}
