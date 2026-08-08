package nfq

import (
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
)

const (
	dnsTypeA    = 1
	dnsTypeAAAA = 28
)

func (w *Worker) pinnedAnswer(set *config.SetConfig, query []byte, domain string) []byte {
	if set == nil || len(query) == 0 {
		return nil
	}
	pins := set.DNS.PinnedAddresses(domain)
	if len(pins) == 0 {
		return nil
	}

	qtype, ok := dns.QuestionType(query)
	if !ok || (qtype != dnsTypeA && qtype != dnsTypeAAAA) {
		return nil
	}
	want6 := qtype == dnsTypeAAAA

	ips := make([]net.IP, 0, len(pins))
	for _, pin := range pins {
		ip := net.ParseIP(pin)
		if ip == nil {
			continue
		}
		if (ip.To4() == nil) != want6 {
			continue
		}
		ips = append(ips, ip)
	}
	if len(ips) == 0 {
		return nil
	}

	return dns.BuildAnswerFromIPs(query, config.DefaultDNSPinTTLSec, ips)
}

func (w *Worker) applyPinnedAnswer(cfg *config.Config, set *config.SetConfig, clientIP net.IP, domain string, pinned []byte) {
	ips := dns.ParseResponseIPs(pinned)
	if len(ips) == 0 {
		return
	}
	w.storeHostHints(clientIP, set, domain, ips)
	if cfg != nil && set.Routing.Enabled && !set.Targets.DomainOnly && !cfg.Queue.IsDiscovery && RoutingHandleDNSFunc != nil {
		RoutingHandleDNSFunc(cfg, set, ips)
	}
	log.Infof("DNS pin: answering %s with %s (set: %s)", domain, ips[0], set.Name)
}

func synDetectEnabled(set *config.SetConfig) bool {
	return set != nil && set.TCP.IPBlockDetect.Enabled && set.TCP.IPBlockDetect.SynDetect
}

func (w *Worker) handleSynHealth(vc *verdictCtx, pkt *pktInfo, cfg *config.Config, set *config.SetConfig, sport, dport uint16) bool {
	if w == nil || w.ipHealth == nil || cfg == nil || cfg.Queue.IsDiscovery || !synDetectEnabled(set) {
		return false
	}

	ibd := &set.TCP.IPBlockDetect

	if !w.ipHealth.IsDead(pkt.dstStr) {
		timeout := time.Duration(ibd.TimeoutMs) * time.Millisecond
		if timeout <= 0 {
			timeout = 3000 * time.Millisecond
		}
		w.ipHealth.RecordSyn(pkt.dstStr, dport, int(cfg.MainInjectedMark()), ibd.ResolvedSynThreshold(), timeout)
		return false
	}

	host := w.hostForDest(cfg, pkt.srcStr, pkt.dstStr, pkt.srcMac)
	log.LogConnection("TCP", set.Name, host, pkt.srcStr, sport, "", pkt.dstStr, dport, pkt.srcMac, "", "ipblock-syn")

	m := metrics.GetMetricsCollector()
	m.RecordConnection("TCP", host, pkt.srcStr, pkt.dstStr, true, pkt.srcMac, set.Name, "")
	m.RecordPacket(uint64(len(pkt.raw)))

	if pkt.ver == IPv4 {
		w.sendSynRSTToClientV4(pkt.raw, pkt.ihl, pkt.src, pkt.dst)
	} else {
		w.sendSynRSTToClientV6(pkt.raw, pkt.src, pkt.dst)
	}
	vc.drop()
	return true
}

func (w *Worker) hostForDest(cfg *config.Config, clientIP, destIP, srcMac string) string {
	if w == nil {
		return ""
	}
	if _, host := w.lookupHostHint(cfg, clientIP, destIP, srcMac); host != "" {
		return host
	}
	return ""
}

func (w *Worker) recordDestAlive(serverIP, clientIP string, isSynAck bool) {
	if w == nil || w.ipHealth == nil {
		return
	}
	if !isSynAck {
		w.ipHealth.RecordResponse(serverIP)
		return
	}
	w.ipHealth.RecordHandshake(serverIP)

	if w.goodIPs == nil || clientIP == "" {
		return
	}
	if _, host, ok := w.hostHints.Lookup(clientIP, serverIP); ok && host != "" {
		w.goodIPs.Remember(host, net.ParseIP(serverIP))
	}
}

func (w *Worker) healingSet(cfg *config.Config, set *config.SetConfig) bool {
	if cfg == nil || set == nil || cfg.Queue.IsDiscovery {
		return false
	}
	ibd := &set.TCP.IPBlockDetect
	return ibd.Enabled && ibd.HealDNS
}

func (w *Worker) healDNSResponse(cfg *config.Config, set *config.SetConfig, domain string, resp []byte, overTCP bool) []byte {
	if w == nil || w.ipHealth == nil || len(resp) == 0 || !w.healingSet(cfg, set) {
		return nil
	}

	maxSize := 0
	if overTCP {
		maxSize = dns.MaxTCPMessageSize
	}

	ttlCap := set.TCP.IPBlockDetect.ResolvedHealTTL()
	out, verdict := dns.FilterAnswerIPs(resp, ttlCap, maxSize, func(ip net.IP) bool {
		return w.ipHealth.IsDead(ip.String())
	})

	switch verdict {
	case dns.FilterTooLarge:
		log.Warnf("DNS heal: the curated answer for %s would not fit the response size the client allows, passing the original through (set: %s)", domain, set.Name)
	case dns.FilterRewritten:
		log.Infof("DNS heal: dropped unreachable addresses from the answer for %s (set: %s)", domain, set.Name)
		return out
	case dns.FilterAllDropped:
		qtype, ok := dns.QuestionType(resp)
		if !ok {
			return nil
		}
		live := make([]net.IP, 0, 4)
		for _, ip := range w.goodIPs.Lookup(domain, qtype == dnsTypeAAAA) {
			if !w.ipHealth.IsDead(ip.String()) {
				live = append(live, ip)
			}
		}
		if len(live) > 0 {
			if built := dns.BuildAnswerFromIPs(resp, ttlCap, live); built != nil {
				log.Warnf("DNS heal: every address the answer for %s carries is unreachable, answering with %s instead (set: %s)", domain, live[0], set.Name)
				return built
			}
		}
		log.Warnf("DNS heal: every address for %s is unreachable and no reachable one has been seen for it, passing the answer through unchanged (set: %s)", domain, set.Name)
	}
	return nil
}
