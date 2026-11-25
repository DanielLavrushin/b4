package nfq

import (
	"fmt"
	"net"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
)

func countedUnder(t *testing.T, host string) []string {
	t.Helper()
	top := metrics.GetMetricsCollector().Hello().TopDomains
	if top == nil {
		t.Fatal("the dashboard has no top domains")
	}
	for _, d := range top.Items {
		if d.Key == host {
			return d.Sets
		}
	}
	t.Fatalf("%q is not among the top domains: %+v", host, top.Items)
	return nil
}

func TestTheDashboardCountsAConnectionUnderTheRouteSetWhoseTunnelCarriesIt(t *testing.T) {
	metrics.GetMetricsCollector().ResetCounters()
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stubInjectRoute(t, idx["eth7"], true)
	hotspot := keeneticHotspotMark
	into := func(set *config.SetConfig, iface string) {
		set.Routing.Enabled, set.Routing.Mode, set.Routing.EgressInterface = true, config.RoutingModeInterface, iface
	}
	for i, tc := range []struct {
		name   string
		out    string
		mark   *uint32
		want   string
		change func(named, address *config.SetConfig)
	}{
		{"through the tunnel of the set", "wgt0", nil, "address-set", func(named, address *config.SetConfig) {}},
		{"through the tunnel of the set, the name set is domain only", "wgt0", nil, "address-set", func(named, address *config.SetConfig) { named.Targets.DomainOnly = true }},
		{"through the tunnel of the set, the name set blocks", "wgt0", nil, "named-set", func(named, address *config.SetConfig) {
			named.Routing.Enabled, named.Routing.Mode, named.Routing.BlockAction = true, config.RoutingModeBlock, config.BlockActionDrop
		}},
		{"through the tunnel of both sets", "wgt0", nil, "named-set", func(named, address *config.SetConfig) { into(named, "wgt0") }},
		{"through the tunnel of both sets, marked by another service", "wgt0", &hotspot, "named-set", func(named, address *config.SetConfig) { into(named, "wgt0") }},
		{"through the interface of the set, rerouted by another service", "ppp9", &hotspot, "address-set", func(named, address *config.SetConfig) { into(address, "ppp9") }},
		{"through the uplink", "eth7", nil, "named-set", func(named, address *config.SetConfig) {}},
		{"through another tunnel", "awgt0", nil, "named-set", func(named, address *config.SetConfig) {}},
		{"through a tunnel, the set does not route", "wgt0", nil, "named-set", func(named, address *config.SetConfig) { address.Routing.Enabled = false }},
	} {
		domain := fmt.Sprintf("carried%d.example", i)
		dst := net.IPv4(198, 51, 100, byte(31+i))
		newWorker := func(t *testing.T) *Worker {
			t.Helper()
			named := newNamedRowSet("named-set", config.ConfigOff, domain)
			address := newRowSet("address-set", config.ConfigOff)
			address.Targets.IPs = []string{"198.51.100.0/24"}
			into(&address, "wgt0")
			tc.change(&named, &address)
			return newQUICFlowWorker(t, &named, &address)
		}
		check := func(t *testing.T, host string) {
			t.Helper()
			if got := countedUnder(t, host); !slices.Equal(got, []string{tc.want}) {
				t.Errorf("counted under %v, want %s", got, tc.want)
			}
		}
		t.Run(tc.name+", TCP", func(t *testing.T) {
			w := newWorker(t)
			host := "tcp." + domain
			for range 2 {
				port := freshPorts(1)
				syn := tcpFlowPacket(nil, dst, port)
				syn[33] = 0x02
				for _, raw := range [][]byte{syn, tcpFlowPacket(buildClientHello(host, 16, 0xAB), dst, port), tcpFlowPacket(tlsAppData(64), dst, port)} {
					queuedVerdict(t, w, raw, tc.mark, idx[tc.out], 0)
				}
				check(t, host)
			}
		})
		t.Run(tc.name+", QUIC", func(t *testing.T) {
			w := newWorker(t)
			host := "quic." + domain
			split := newSplitQUICHello(t, []byte{0x41, byte(i), 2, 3, 4, 5, 6, 7}, []byte{0x42, byte(i), 2, 3, 4, 5, 6, 7}, host)
			odcid := []byte{0x43, byte(i), 2, 3, 4, 5, 6, 7}
			whole := sealQUICInitial(t, odcid, odcid, 1, quicCryptoFrame(0, buildClientHelloHandshake(host)))
			for _, flow := range [][][]byte{{split.first, split.completing, split.oneRTT}, {whole}} {
				port := freshPorts(1)
				for _, payload := range flow {
					queuedVerdict(t, w, makeV4UDPPacket(payload, quicClient, dst, port, 443), tc.mark, idx[tc.out], 0)
				}
				check(t, host)
			}
		})
	}
}

func TestTheDashboardCountsAConnectionUnderTheRouteSetThatLearnedItsAddress(t *testing.T) {
	metrics.GetMetricsCollector().ResetCounters()
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	for _, tc := range []struct{ out, want string }{{"wgt0", "route-set"}, {"eth7", "named-set"}} {
		t.Run("through "+tc.out, func(t *testing.T) {
			routed, other := "routed."+tc.out+".example", "other."+tc.out+".example"
			route := newNamedRowSet("route-set", config.ConfigOff, routed)
			route.Routing.Enabled, route.Routing.Mode, route.Routing.EgressInterface = true, config.RoutingModeInterface, "wgt0"
			named := newNamedRowSet("named-set", config.ConfigOff, other)
			named.Targets.DomainOnly = true
			w := newQUICFlowWorker(t, &route, &named)
			dst := net.IPv4(198, 51, 100, 41)
			for _, host := range []string{routed, other, other} {
				port := freshPorts(1)
				syn := tcpFlowPacket(nil, dst, port)
				syn[33] = 0x02
				for _, raw := range [][]byte{syn, tcpFlowPacket(buildClientHello(host, 16, 0xAB), dst, port), tcpFlowPacket(tlsAppData(64), dst, port)} {
					queuedVerdict(t, w, raw, nil, idx[tc.out], 0)
				}
			}
			if got := countedUnder(t, routed); !slices.Equal(got, []string{"route-set"}) {
				t.Errorf("the routed name is counted under %v", got)
			}
			if got := countedUnder(t, other); !slices.Equal(got, []string{tc.want}) {
				t.Errorf("counted under %v, want %s", got, tc.want)
			}
		})
	}
}

func TestTheDashboardCountsAConnectionUnderTheSetOfItsName(t *testing.T) {
	metrics.GetMetricsCollector().ResetCounters()
	newWorker := func(t *testing.T) *Worker {
		t.Helper()
		named := newNamedRowSet("named-set", config.ConfigOff, "dashboard.example")
		address := newRowSet("address-set", config.ConfigOff)
		address.Targets.IPs = []string{"198.51.100.0/24"}
		return newQUICFlowWorker(t, &named, &address)
	}

	t.Run("TCP from the SYN on", func(t *testing.T) {
		w := newWorker(t)
		port := freshPorts(1)
		dst := net.IPv4(198, 51, 100, 21)
		syn := tcpFlowPacket(nil, dst, port)
		syn[33] = 0x02
		hello := buildClientHello("tcp.dashboard.example", 16, 0xAB)
		w.ProcessPacket(syn)
		w.ProcessPacket(tcpFlowPacket(hello, dst, port))
		w.ProcessPacket(tcpFlowPacket(tlsAppData(64), dst, port))
		if got := countedUnder(t, "tcp.dashboard.example"); !slices.Equal(got, []string{"named-set"}) {
			t.Errorf("counted under %v, want the set of the name", got)
		}
	})
	t.Run("QUIC with a ClientHello in two datagrams", func(t *testing.T) {
		w := newWorker(t)
		port := freshPorts(1)
		flow := newSplitQUICHello(t, []byte{0x31, 1, 2, 3, 4, 5, 6, 7}, []byte{0x32, 1, 2, 3, 4, 5, 6, 7}, "quic.dashboard.example")
		for _, payload := range [][]byte{flow.first, flow.completing, flow.oneRTT} {
			w.ProcessPacket(makeV4UDPPacket(payload, quicClient, net.IPv4(198, 51, 100, 22), port, 443))
		}
		if got := countedUnder(t, "quic.dashboard.example"); !slices.Equal(got, []string{"named-set"}) {
			t.Errorf("counted under %v, want the set of the name", got)
		}
	})
}
