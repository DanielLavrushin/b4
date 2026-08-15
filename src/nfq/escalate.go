package nfq

import (
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/sni"
)

const (
	escalateReasonStall  = "no answer to the ClientHello"
	escalateReasonRST    = "forged RST"
	escalateReasonDeadIP = "destination does not answer"
	escalateReasonDNS    = "no usable DNS answer"
)

func (w *Worker) escalatedSetFor(cfg *config.Config, host, srcMac string) *config.SetConfig {
	if w == nil || w.destState == nil || cfg == nil || host == "" {
		return nil
	}
	escId, _, ok := w.destState.GetEscalation(host)
	if !ok {
		return nil
	}
	escSet := cfg.GetSetById(escId)
	if escSet == nil || !escSet.Enabled {
		w.destState.ClearEscalation(host)
		return nil
	}
	if !sni.SetMatchesSource(escSet, srcMac) {
		return nil
	}
	return escSet
}

func (w *Worker) tryEscalate(cfg *config.Config, set *config.SetConfig, host, srcMac string, dst net.IP, reason string) *config.SetConfig {
	if w == nil || w.destState == nil || cfg == nil || set == nil || host == "" || !set.Escalate.Active() {
		return nil
	}
	next := cfg.GetSetById(set.Escalate.To)
	if next == nil || !next.Enabled {
		return nil
	}
	if !sni.SetMatchesSource(next, srcMac) {
		log.Tracef("escalation for %s not applied: %s does not target the device it came from", host, next.Name)
		return nil
	}
	if !w.destState.SetEscalation(host, next.Id, reason, set.Escalate.ResolvedTTL()) {
		if w.destState.ShouldLogHopCap(host) {
			log.Warnf("escalation hop cap reached for %s, the chain stops at %s", host, set.Name)
		}
		return nil
	}
	metrics.GetMetricsCollector().RecordEscalation()
	w.registerEscalatedRoute(cfg, next, host, dst)
	log.Warnf("escalation: %s is not getting through with %s (%s), switching it to %s", host, set.Name, reason, next.Name)
	return next
}

func (w *Worker) hostAddresses(host string, dst net.IP) []net.IP {
	ips := make([]net.IP, 0, 4)
	if dst != nil {
		ips = append(ips, append(net.IP(nil), dst...))
	}
	if w == nil || w.goodIPs == nil || host == "" {
		return ips
	}
	for _, want6 := range []bool{false, true} {
		for _, ip := range w.goodIPs.Lookup(host, want6) {
			if ip == nil || containsIP(ips, ip) {
				continue
			}
			ips = append(ips, ip)
		}
	}
	return ips
}

func containsIP(list []net.IP, ip net.IP) bool {
	for _, known := range list {
		if known.Equal(ip) {
			return true
		}
	}
	return false
}

func (w *Worker) registerEscalatedRoute(cfg *config.Config, escSet *config.SetConfig, host string, dst net.IP) {
	registerEscalatedRoute(cfg, escSet, w.hostAddresses(host, dst))
}

func (w *Worker) refreshEscalatedRoute(cfg *config.Config, escSet *config.SetConfig, host string, dst net.IP) {
	if w == nil || w.destState == nil || cfg == nil || escSet == nil || !escSet.Routing.Enabled {
		return
	}
	if !w.destState.ShouldRefreshRoute(host, routeRefreshInterval(escSet)) {
		return
	}
	log.Tracef("escalation: refreshing the %s route entries for %s", escSet.Name, host)
	w.registerEscalatedRoute(cfg, escSet, host, dst)
}

func routeRefreshInterval(escSet *config.SetConfig) time.Duration {
	ttl := escSet.Routing.IPTTLSeconds
	if ttl <= 0 {
		ttl = config.DefaultSetConfig.Routing.IPTTLSeconds
	}
	if ttl <= 0 {
		ttl = 3600
	}
	interval := time.Duration(ttl/2) * time.Second
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	return interval
}

func escalateOnStall(escalate *config.EscalateConfig, count int, firstSeen time.Time) bool {
	if !escalate.Active() {
		return false
	}
	if count >= escalate.ResolvedStallThreshold() {
		return true
	}
	return count > 1 && time.Since(firstSeen) > escalate.ResolvedStallTimeout()
}
