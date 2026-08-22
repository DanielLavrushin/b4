package sni

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func portSetScopedTo(name, tcpPorts, udpPorts string, devices []string, exclude bool) *config.SetConfig {
	s := makeSet(name)
	s.TCP.DPortFilter = tcpPorts
	s.UDP.DPortFilter = udpPorts
	s.Targets.SourceDevices = devices
	s.Targets.SourceDevicesExclude = exclude
	return s
}

func TestPortOnlySetsHonourSourceDevices(t *testing.T) {
	const mine, other = "AA:BB:CC:DD:EE:01", "AA:BB:CC:DD:EE:02"

	t.Run("tcp port-only set matches only its source device", func(t *testing.T) {
		ss := NewSuffixSet([]*config.SetConfig{portSetScopedTo("tcp-scoped", "8443", "", []string{mine}, false)})

		if matched, _ := ss.MatchTCPPort(8443, mine); !matched {
			t.Error("expected the bound device to match")
		}
		if matched, set := ss.MatchTCPPort(8443, other); matched {
			t.Errorf("a set bound to one device must not match another, got %q", set.Name)
		}
		if matched, set := ss.MatchTCPPort(8443, ""); matched {
			t.Errorf("an unknown source must not match a device-bound set, got %q", set.Name)
		}
	})

	t.Run("udp port-only set matches only its source device", func(t *testing.T) {
		ss := NewSuffixSet([]*config.SetConfig{portSetScopedTo("udp-scoped", "", "5353", []string{mine}, false)})

		if matched, _ := ss.MatchUDPPort(5353, mine); !matched {
			t.Error("expected the bound device to match")
		}
		if matched, set := ss.MatchUDPPort(5353, other); matched {
			t.Errorf("a set bound to one device must not match another, got %q", set.Name)
		}
	})

	t.Run("exclude mode inverts the test", func(t *testing.T) {
		ss := NewSuffixSet([]*config.SetConfig{portSetScopedTo("tcp-excluded", "8443", "", []string{mine}, true)})

		if matched, set := ss.MatchTCPPort(8443, mine); matched {
			t.Errorf("an excluded device must not match, got %q", set.Name)
		}
		if matched, _ := ss.MatchTCPPort(8443, other); !matched {
			t.Error("a device that is not excluded must still match")
		}
	})

	t.Run("an unbound port-only set still matches every device", func(t *testing.T) {
		ss := NewSuffixSet([]*config.SetConfig{portSetScopedTo("tcp-open", "8443", "", nil, false)})

		for _, mac := range []string{mine, other, ""} {
			if matched, _ := ss.MatchTCPPort(8443, mac); !matched {
				t.Errorf("expected a match for source %q", mac)
			}
		}
	})
}
