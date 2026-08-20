package config

import "testing"

func TestRoutingDivertsPackets(t *testing.T) {
	base := func() *SetConfig {
		s := NewSetConfig()
		s.Routing.Enabled = true
		s.Routing.Mode = RoutingModeProxy
		return &s
	}

	t.Run("routing off never diverts", func(t *testing.T) {
		s := base()
		s.Routing.Enabled = false
		s.Targets.IpsToMatch = []string{"1.2.3.4"}
		if s.RoutingDivertsPackets() {
			t.Error("routing is off")
		}
	})

	t.Run("a destination target diverts", func(t *testing.T) {
		s := base()
		s.Targets.IpsToMatch = []string{"1.2.3.4"}
		if !s.RoutingDivertsPackets() {
			t.Error("an IP target is a destination the kernel can steer")
		}
		s = base()
		s.Targets.DomainsToMatch = []string{"example.com"}
		if !s.RoutingDivertsPackets() {
			t.Error("a domain target is a destination the kernel can steer")
		}
	})

	t.Run("source devices alone do not divert", func(t *testing.T) {
		s := base()
		s.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
		if s.RoutingDivertsPackets() {
			t.Error("every routing rule matches a destination, so a device-only set steers nothing")
		}
	})

	t.Run("a port filter alone does not divert", func(t *testing.T) {
		s := base()
		s.TCP.DPortFilter = "8443"
		if s.RoutingDivertsPackets() {
			t.Error("a port-only set has no address set to match, so it must not claim the packet")
		}
	})
}
