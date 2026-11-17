package nfq

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
)

func newPortRoutedSet() config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "port-routed"
	set.Name = "PortRouted"
	set.Enabled = true
	set.UDP.DPortFilter = "50000-50100"
	set.UDP.Mode = "drop"
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeProxy
	set.Routing.Upstream.Port = 1080
	set.Routing.Upstream.UDP = true
	set.Routing.SourceInterfaces = []string{"br0"}
	set.Fragmentation.Strategy = config.ConfigNone
	return set
}

func TestTheEngineLeavesAPortRoutedSetsUDPAlone(t *testing.T) {
	voice := func() []byte {
		return makeV4UDPPacket([]byte("plain udp to a routed port"), net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4"), 51000, 50050)
	}

	t.Run("routed by port", func(t *testing.T) {
		set := newPortRoutedSet()
		cfg := newUDPGateConfig(t, &set)
		w := newTestWorker(t, &cfg)
		if v := w.ProcessPacket(voice()); v != engine.VerdictAccept {
			t.Errorf("UDP that reaches the engine on a port-routed set's port came from outside its scope and must pass untouched, got verdict %v", v)
		}
	})

	t.Run("left to the next set", func(t *testing.T) {
		set := newPortRoutedSet()
		below := config.NewSetConfig()
		below.Id = "below"
		below.Name = "Below"
		below.Enabled = true
		below.UDP.DPortFilter = "50000-50100"
		below.UDP.Mode = "drop"
		below.Fragmentation.Strategy = config.ConfigNone
		cfg := newUDPGateConfig(t, &set, &below)
		w := newTestWorker(t, &cfg)
		if v := w.ProcessPacket(voice()); v != engine.VerdictDrop {
			t.Errorf("a set below that lists the port must take the UDP the port-routed set leaves, got verdict %v", v)
		}
	})

	t.Run("without a source scope it is a port-only set as before", func(t *testing.T) {
		set := newPortRoutedSet()
		set.Routing.SourceInterfaces = nil
		cfg := newUDPGateConfig(t, &set)
		w := newTestWorker(t, &cfg)
		if v := w.ProcessPacket(voice()); v != engine.VerdictDrop {
			t.Errorf("an unscoped set routes nothing by port, so its udp.mode applies as before, got verdict %v", v)
		}
	})
}
