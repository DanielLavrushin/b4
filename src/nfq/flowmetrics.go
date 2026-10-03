package nfq

import (
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
)

var queueOverflows atomic.Uint64

func QueueOverflows() uint64 {
	return queueOverflows.Load()
}

func (p *pktInfo) flowKey(sport, dport uint16) metrics.FlowKey {
	k := metrics.FlowKey{SPort: sport, DPort: dport, Proto: p.proto}
	if p.ver == IPv4 {
		copy(k.Addr[0:4], p.addr[0:4])
		copy(k.Addr[16:20], p.addr[16:20])
		return k
	}
	copy(k.Addr[:], p.addr[:])
	return k
}

func setIDOf(set *config.SetConfig) string {
	if set == nil {
		return ""
	}
	return set.Id
}

func countsTraffic(cfg *config.Config) bool {
	return cfg != nil && !cfg.Queue.IsDiscovery
}

func observeFlow(cfg *config.Config, pkt *pktInfo, sport, dport uint16, set *config.SetConfig) {
	if !countsTraffic(cfg) {
		return
	}
	metrics.GetMetricsCollector().ObserveFlow(pkt.flowKey(sport, dport), setIDOf(set))
}

func recordBlockedFlow(cfg *config.Config, pkt *pktInfo, sport, dport uint16, set *config.SetConfig, target string) {
	if !countsTraffic(cfg) {
		return
	}
	metrics.GetMetricsCollector().RecordBlockedFlow(pkt.flowKey(sport, dport), setIDOf(set), target, pkt.srcMac)
}

func recordBlockedDNS(cfg *config.Config, set *config.SetConfig, domain, srcMac string) {
	if !countsTraffic(cfg) {
		return
	}
	metrics.GetMetricsCollector().RecordBlockedDNS(setIDOf(set), domain, srcMac)
}

func recordRSTDrop(cfg *config.Config) {
	if !countsTraffic(cfg) {
		return
	}
	metrics.GetMetricsCollector().RecordRSTDrop()
}

func recordEscalation(cfg *config.Config) {
	if !countsTraffic(cfg) {
		return
	}
	metrics.GetMetricsCollector().RecordEscalation()
}

func udpViaTProxy(set *config.SetConfig) bool {
	return set != nil && set.RoutingDivertsPackets() && config.RoutingUsesTProxy(set.Routing.Mode) && set.Routing.Upstream.UDP
}

func (p *Pool) PacketsProcessed() uint64 {
	var sum uint64
	for _, w := range p.Workers {
		sum += w.GetStats()
	}
	return sum
}

func (p *Pool) isDiscovery() bool {
	cfg := p.GetFirstWorkerConfig()
	return cfg != nil && cfg.Queue.IsDiscovery
}

func (p *Pool) publishEscalations() {
	if p.isDiscovery() {
		return
	}
	metrics.GetMetricsCollector().UpdateEscalations(p.GetEscalations())
}
