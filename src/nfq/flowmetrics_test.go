package nfq

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/mdlayher/netlink"
)

func newDropBlockSet() config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "block-set"
	set.Name = "Blocked"
	set.Enabled = true
	set.Targets.DomainOnly = true
	set.Targets.DomainsToMatch = []string{"blocked.example"}
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeBlock
	set.Routing.BlockAction = config.BlockActionDrop
	return set
}

var testFlowPorts atomic.Uint32

func freshPorts(n uint32) uint16 {
	return uint16(20000 + testFlowPorts.Add(n)%40000)
}

func tcpFlowPacket(payload []byte, dst net.IP, sport uint16) []byte {
	pkt := makeV4TCPPacket(payload, 1000)
	copy(pkt[16:20], dst.To4())
	binary.BigEndian.PutUint16(pkt[20:22], sport)
	return pkt
}

func totalsDelta(before, after metrics.Totals) metrics.Totals {
	return metrics.Totals{
		Conns:        after.Conns - before.Conns,
		ConnsInSets:  after.ConnsInSets - before.ConnsInSets,
		RSTDropped:   after.RSTDropped - before.RSTDropped,
		Escalations:  after.Escalations - before.Escalations,
		BlockedDNS:   after.BlockedDNS - before.BlockedDNS,
		BlockedConns: after.BlockedConns - before.BlockedConns,
	}
}

func TestPacketPathCountsFlowsOnlyOutsideDiscovery(t *testing.T) {
	mc := metrics.GetMetricsCollector()
	for _, discovery := range []bool{true, false} {
		passive := newPassiveSet()
		block := newDropBlockSet()
		cfg := config.NewConfig()
		cfg.Sets = []*config.SetConfig{&passive, &block}
		cfg.Queue.IsDiscovery = discovery
		w := newTestWorker(t, &cfg)

		port := freshPorts(4)
		before := mc.Totals()
		hello := buildClientHello("i.ytimg.com", 16, 0xAB)
		w.ProcessPacket(tcpFlowPacket(hello, net.IPv4(198, 51, 100, 4), port))
		w.ProcessPacket(tcpFlowPacket(hello, net.IPv4(198, 51, 100, 4), port))
		w.ProcessPacket(tcpFlowPacket(buildClientHello("blocked.example", 16, 0xAB), net.IPv4(198, 51, 100, 5), port+1))
		w.ProcessPacket(makeV4UDPPacket([]byte("not quic"), net.ParseIP("10.0.0.1"), net.ParseIP("198.51.100.6"), port+2, 443))
		w.ProcessPacket(makeV4UDPPacket(buildDNSQuery("blocked.example", 7), net.ParseIP("10.0.0.1"), net.ParseIP("8.8.8.8"), port+3, 53))
		got := totalsDelta(before, mc.Totals())

		want := metrics.Totals{}
		if !discovery {
			want = metrics.Totals{Conns: 3, ConnsInSets: 2, BlockedConns: 1, BlockedDNS: 1}
		}
		if got != want {
			t.Fatalf("discovery=%v: got %+v, want %+v", discovery, got, want)
		}
	}
}

func TestRSTDropsAndEscalationsSkipDiscovery(t *testing.T) {
	mc := metrics.GetMetricsCollector()
	main := config.NewConfig()
	disc := config.NewConfig()
	disc.Queue.IsDiscovery = true

	before := mc.Totals()
	recordRSTDrop(&disc)
	recordEscalation(&disc)
	recordRSTDrop(nil)
	if got := totalsDelta(before, mc.Totals()); got != (metrics.Totals{}) {
		t.Fatalf("a discovery worker must record nothing, got %+v", got)
	}
	recordRSTDrop(&main)
	recordEscalation(&main)
	if got := totalsDelta(before, mc.Totals()); got != (metrics.Totals{RSTDropped: 1, Escalations: 1}) {
		t.Fatalf("the main pool records RST drops and escalations, got %+v", got)
	}
}

func newTProxySet(t *testing.T, cfg *config.Config, ip string, udp bool) *config.SetConfig {
	t.Helper()
	set := config.NewSetConfig()
	set.Id = "proxy-" + ip
	set.Name = "Proxy " + ip
	set.Enabled = true
	set.Targets.IPs = []string{ip}
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeProxy
	set.Routing.Upstream.UDP = udp
	set.UDP.Mode = config.ConfigOff
	if _, _, err := cfg.GetTargetsForSet(&set); err != nil {
		t.Fatalf("expand targets: %v", err)
	}
	return &set
}

func TestFlowsTheTProxyListenerHandlesAreLeftToIt(t *testing.T) {
	mc := metrics.GetMetricsCollector()
	cfg := config.NewConfig()
	viaUDP := newTProxySet(t, &cfg, "1.2.3.7", true)
	tcpOnly := newTProxySet(t, &cfg, "1.2.3.8", false)
	cfg.Sets = []*config.SetConfig{viaUDP, tcpOnly}
	cfg.BuildTCPPortMap()
	cfg.BuildSetPortRanges()
	w := newTestWorker(t, &cfg)

	port := freshPorts(3)
	before := mc.Totals()
	w.ProcessPacket(tcpFlowPacket([]byte("x"), net.IPv4(1, 2, 3, 7), port))
	w.ProcessPacket(makeV4UDPPacket([]byte("not quic"), net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.7"), port+1, 443))
	if got := totalsDelta(before, mc.Totals()); got != (metrics.Totals{}) {
		t.Fatalf("TCP and UDP the TPROXY listener takes over are counted there, not here: %+v", got)
	}

	w.ProcessPacket(makeV4UDPPacket([]byte("not quic"), net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.8"), port+2, 443))
	if got := totalsDelta(before, mc.Totals()); got != (metrics.Totals{Conns: 1, ConnsInSets: 1}) {
		t.Fatalf("UDP of a proxy set without UDP relay stays with the packet engine: %+v", got)
	}
}

func TestQueueOverflowCountsENOBUFS(t *testing.T) {
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)
	before := QueueOverflows()
	w.handleNfqError(&netlink.OpError{Op: "receive", Err: syscall.ENOBUFS})
	w.handleNfqError(fmt.Errorf("receive: %w", syscall.ENOBUFS))
	w.handleNfqError(&netlink.OpError{Op: "receive", Err: syscall.ENOENT})
	if got := QueueOverflows() - before; got != 2 {
		t.Fatalf("each ENOBUFS is one queue overflow, got %d", got)
	}

	disc := config.NewConfig()
	disc.Queue.IsDiscovery = true
	before = QueueOverflows()
	newTestWorker(t, &disc).handleNfqError(&netlink.OpError{Op: "receive", Err: syscall.ENOBUFS})
	if got := QueueOverflows() - before; got != 0 {
		t.Fatalf("a discovery queue overflowing is not the user's traffic, got %d", got)
	}
}

func TestOnlyTheMainPoolPublishesEscalations(t *testing.T) {
	mc := metrics.GetMetricsCollector()
	t.Cleanup(func() { mc.UpdateEscalations([]metrics.EscalationEntry{}) })
	for _, discovery := range []bool{true, false} {
		cfg := config.NewConfig()
		cfg.Queue.IsDiscovery = discovery
		p := &Pool{Workers: []*Worker{newTestWorker(t, &cfg)}, state: newRuntimeState()}
		mc.UpdateEscalations([]metrics.EscalationEntry{})
		p.state.destState.SetEscalation("pool.example", "fallback", escalateReasonRST, time.Minute)

		p.publishEscalations()

		items := mc.Hello().Escalations.Items
		published := len(items) == 1 && items[0].Host == "pool.example"
		if published == discovery {
			t.Fatalf("discovery=%v: published=%v, items %+v", discovery, published, items)
		}
	}
}

func TestFlowKeyIsBuiltFromTheAddressesOnly(t *testing.T) {
	v4 := &pktInfo{ver: IPv4, proto: 6}
	for i := range v4.addr {
		v4.addr[i] = 0xEE
	}
	copy(v4.addr[0:4], []byte{10, 0, 0, 1})
	copy(v4.addr[16:20], []byte{1, 2, 3, 4})
	var want metrics.FlowKey
	copy(want.Addr[0:4], []byte{10, 0, 0, 1})
	copy(want.Addr[16:20], []byte{1, 2, 3, 4})
	want.SPort, want.DPort, want.Proto = 12345, 443, 6
	if got := v4.flowKey(12345, 443); got != want {
		t.Fatalf("IPv4 key must zero the unused bytes: got %x, want %x", got.Addr, want.Addr)
	}

	v6 := &pktInfo{ver: IPv6, proto: 17}
	for i := range v6.addr {
		v6.addr[i] = byte(i + 1)
	}
	got := v6.flowKey(5000, 443)
	if got.Addr != v6.addr || got.SPort != 5000 || got.DPort != 443 || got.Proto != 17 {
		t.Fatalf("IPv6 key carries both full addresses: %+v", got)
	}
}

func TestObservingAKnownFlowDoesNotAllocate(t *testing.T) {
	cfg := config.NewConfig()
	set := newPassiveSet()
	pkt := &pktInfo{ver: IPv4, proto: 6}
	copy(pkt.addr[0:4], []byte{10, 9, 9, 9})
	copy(pkt.addr[16:20], []byte{1, 9, 9, 9})
	observeFlow(&cfg, pkt, 40404, 443, &set, "video.example")
	if n := testing.AllocsPerRun(200, func() { observeFlow(&cfg, pkt, 40404, 443, &set, "video.example") }); n != 0 {
		t.Fatalf("a known flow costs %v allocations per packet", n)
	}
}

func TestPacketsProcessedSumsTheWorkers(t *testing.T) {
	cfg := config.NewConfig()
	a, b := newTestWorker(t, &cfg), newTestWorker(t, &cfg)
	p := &Pool{Workers: []*Worker{a, b}}
	a.ProcessPacket(makeV4TCPPacket(nil, 1))
	b.ProcessPacket(makeV4TCPPacket(nil, 1))
	b.ProcessPacket(makeV4TCPPacket(nil, 2))
	if got := p.PacketsProcessed(); got != 3 {
		t.Fatalf("got %d, want 3", got)
	}
	if got := (&Pool{}).PacketsProcessed(); got != 0 {
		t.Fatalf("a pool without workers processed nothing, got %d", got)
	}
}

func BenchmarkObserveFlowFromThePacketPath(b *testing.B) {
	cfg := config.NewConfig()
	set := newPassiveSet()
	pkt := &pktInfo{ver: IPv4, proto: 6}
	copy(pkt.addr[0:4], []byte{10, 8, 8, 8})
	copy(pkt.addr[16:20], []byte{1, 8, 8, 8})
	observeFlow(&cfg, pkt, 40405, 443, &set, "video.example")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		observeFlow(&cfg, pkt, 40405, 443, &set, "video.example")
	}
}
