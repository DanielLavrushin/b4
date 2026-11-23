package nfq

import (
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
