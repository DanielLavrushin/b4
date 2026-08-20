package nfq

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
)

func newUDPGateConfig(t *testing.T, sets ...*config.SetConfig) config.Config {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Sets = sets
	for _, s := range sets {
		if _, _, err := cfg.GetTargetsForSet(s); err != nil {
			t.Fatalf("expand targets for %q: %v", s.Name, err)
		}
	}
	cfg.BuildTCPPortMap()
	cfg.BuildSetPortRanges()
	return cfg
}

func newDomainUDPSet(filterQUIC string) config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "domain-set"
	set.Name = "Domain"
	set.Enabled = true
	set.Targets.SNIDomains = []string{"googlevideo.com"}
	set.UDP.FilterQUIC = filterQUIC
	set.UDP.Mode = "drop"
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil
	return set
}

func TestSNIMatchedQUICReachesSetUDPModeRegardlessOfQUICMatching(t *testing.T) {
	modes := []struct {
		mode string
		want engine.PacketVerdict
	}{
		{mode: "drop", want: engine.VerdictDrop},
		{mode: config.ConfigOff, want: engine.VerdictAccept},
	}

	for _, filterQUIC := range []string{config.QUICFilterSNI, config.QUICFilterAll} {
		for _, m := range modes {
			t.Run(filterQUIC+"/"+m.mode, func(t *testing.T) {
				set := newDomainUDPSet(filterQUIC)
				set.UDP.Mode = m.mode
				cfg := newUDPGateConfig(t, &set)
				w := newTestWorker(t, &cfg)

				initial := buildQUICInitialWithSNI(t, []byte{6, 6, 6, 6, 9, 9, 9, 9}, "rr1.googlevideo.com")
				pkt := makeV4UDPPacket(initial, net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4"), 51000, 443)

				if v := w.ProcessPacket(pkt); v != m.want {
					t.Errorf("a QUIC ClientHello matching the set by SNI must reach udp.mode=%s, want verdict %v, got %v", m.mode, m.want, v)
				}
			})
		}
	}
}

func TestUDPScopedAwayByPortFilterIsNotHandled(t *testing.T) {
	set := newDomainUDPSet("all")
	set.UDP.DPortFilter = "8443"
	cfg := newUDPGateConfig(t, &set)
	w := newTestWorker(t, &cfg)

	initial := buildQUICInitialWithSNI(t, []byte{2, 2, 2, 2, 3, 3, 3, 3}, "rr1.googlevideo.com")
	pkt := makeV4UDPPacket(initial, net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4"), 51000, 443)

	if v := w.ProcessPacket(pkt); v != engine.VerdictAccept {
		t.Errorf("udp.dport_filter is the gate for a targeted set, got verdict %v on a port it excludes", v)
	}
}

func TestNonQUICUDPIsHandledForIPTargetedSet(t *testing.T) {
	set := config.NewSetConfig()
	set.Id = "ip-set"
	set.Name = "IPSet"
	set.Enabled = true
	set.Targets.IPs = []string{"1.2.3.4/32"}
	set.UDP.Mode = "drop"
	set.Fragmentation.Strategy = config.ConfigNone

	cfg := newUDPGateConfig(t, &set)
	w := newTestWorker(t, &cfg)

	pkt := makeV4UDPPacket([]byte("plain udp, not quic"), net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4"), 51000, 443)

	if v := w.ProcessPacket(pkt); v != engine.VerdictDrop {
		t.Errorf("udp.mode applies to all UDP of a matched set, not only QUIC, got verdict %v", v)
	}
}

func TestQUICFilterAllGatesBlockingForPortOnlySet(t *testing.T) {
	cases := map[string]engine.PacketVerdict{
		config.QUICFilterSNI: engine.VerdictAccept,
		config.QUICFilterAll: engine.VerdictDrop,
	}

	for filterQUIC, want := range cases {
		t.Run(filterQUIC, func(t *testing.T) {
			set := config.NewSetConfig()
			set.Id = "port-only"
			set.Name = "PortOnly"
			set.Enabled = true
			set.UDP.DPortFilter = "443"
			set.UDP.FilterQUIC = filterQUIC
			set.UDP.Mode = config.ConfigNone
			set.Routing.Enabled = true
			set.Routing.Mode = config.RoutingModeBlock
			set.Routing.BlockAction = config.BlockActionDrop
			set.Fragmentation.Strategy = config.ConfigNone

			cfg := newUDPGateConfig(t, &set)
			w := newTestWorker(t, &cfg)

			initial := buildQUICInitialWithSNI(t, []byte{4, 4, 4, 4, 8, 8, 8, 8}, "rr1.googlevideo.com")
			pkt := makeV4UDPPacket(initial, net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4"), 51000, 443)

			if v := w.ProcessPacket(pkt); v != want {
				t.Errorf("port-only blocking set with filter_quic=%s: want verdict %v, got %v", filterQUIC, want, v)
			}
		})
	}
}
