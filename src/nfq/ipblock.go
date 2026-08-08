package nfq

import (
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
)

const dnsTypeAAAA = 28

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

	action := ibd.ResolvedAction()
	if action == config.IPBlockActionProxy {
		w.divertToProxy(cfg, set, pkt.dst)
	}

	host := w.hostForDest(cfg, pkt.srcStr, pkt.dstStr, pkt.srcMac)
	log.LogConnection("TCP", set.Name, host, pkt.srcStr, sport, "", pkt.dstStr, dport, pkt.srcMac, "", "ipblock-syn->"+action)

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

func (w *Worker) divertToProxy(cfg *config.Config, set *config.SetConfig, dst net.IP) {
	if cfg == nil || set == nil || dst == nil || RoutingLearnIPFunc == nil {
		return
	}
	if !set.Routing.Enabled || !config.RoutingUsesTProxy(set.Routing.Mode) {
		return
	}
	log.Tracef("ip-block: routing unreachable %s through the upstream proxy (set: %s)", dst, set.Name)
	RoutingLearnIPFunc(cfg, set, dst)
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
	w.ipHealth.RecordAlive(serverIP)

	if !isSynAck || w.goodIPs == nil || clientIP == "" {
		return
	}
	if _, host, ok := w.hostHints.Lookup(clientIP, serverIP); ok && host != "" {
		w.goodIPs.Store(host, net.ParseIP(serverIP))
	}
}

func (w *Worker) healingSet(cfg *config.Config, set *config.SetConfig) bool {
	if cfg == nil || set == nil || cfg.Queue.IsDiscovery {
		return false
	}
	ibd := &set.TCP.IPBlockDetect
	return ibd.Enabled && ibd.ResolvedAction() == config.IPBlockActionHeal
}

func (w *Worker) healDNSResponse(cfg *config.Config, set *config.SetConfig, domain string, resp []byte) []byte {
	if w == nil || w.ipHealth == nil || len(resp) == 0 || !w.healingSet(cfg, set) {
		return nil
	}

	ttlCap := set.TCP.IPBlockDetect.ResolvedHealTTL()
	out, verdict := dns.FilterAnswerIPs(resp, ttlCap, func(ip net.IP) bool {
		return w.ipHealth.IsDead(ip.String())
	})

	switch verdict {
	case dns.FilterRewritten:
		log.Infof("DNS heal: dropped unreachable addresses from the answer for %s (set: %s)", domain, set.Name)
		return out
	case dns.FilterAllDropped:
		qtype, ok := dns.QuestionType(resp)
		if !ok {
			return nil
		}
		if good := w.goodIPs.Lookup(domain, qtype == dnsTypeAAAA); len(good) > 0 {
			if built := dns.BuildAnswerFromIPs(resp, ttlCap, good); built != nil {
				log.Warnf("DNS heal: every address for %s is unreachable, answering with %s which last worked (set: %s)", domain, good[0], set.Name)
				return built
			}
		}
		log.Warnf("DNS heal: every address for %s is unreachable and none is known to have worked, passing the answer through unchanged (set: %s)", domain, set.Name)
	}
	return nil
}
