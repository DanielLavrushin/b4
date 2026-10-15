package nfq

import (
	"encoding/binary"
	"net"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
)

func serviceBindingQuery(qtype uint16) bool {
	return qtype == dnsTypeHTTPS || qtype == dnsTypeSVCB
}

func stripsECH(set *config.SetConfig) bool {
	return set != nil && set.Enabled && set.DNS.StripECH
}

func echStripWanted(cfg *config.Config, set *config.SetConfig, qtype uint16) bool {
	if cfg == nil || cfg.Queue.IsDiscovery || !serviceBindingQuery(qtype) {
		return false
	}
	return stripsECH(set) || cfg.HasECHStrip()
}

func (w *Worker) echStripSet(cfg *config.Config, set *config.SetConfig, clientMac string, resp []byte) (*config.SetConfig, string) {
	if stripsECH(set) {
		return set, ""
	}
	if !cfg.HasECHStrip() {
		return nil, ""
	}
	for _, cover := range dns.ResponseECHPublicNames(resp) {
		if matched, coverSet := w.getMatcher().MatchSNIWithSource(cover, clientMac); matched && stripsECH(coverSet) {
			return coverSet, cover
		}
	}
	return nil, ""
}

func (w *Worker) stripECHKeys(cfg *config.Config, set *config.SetConfig, domain, clientMac string, resp []byte, overTCP bool) ([]byte, *config.SetConfig) {
	qtype, ok := dns.QuestionType(resp)
	if !ok || !echStripWanted(cfg, set, qtype) {
		return nil, nil
	}
	actor, cover := w.echStripSet(cfg, set, clientMac, resp)
	if actor == nil {
		return nil, nil
	}

	maxSize := 0
	if overTCP {
		maxSize = dns.MaxTCPMessageSize
	}
	out, verdict := dns.StripECH(resp, maxSize)
	switch verdict {
	case dns.FilterRewritten:
		if cover != "" {
			log.Tracef("DNS ECH: removed the ECH key from the %s answer for %s, the key names %s (set: %s)", dnsTypeName(qtype), dns.SafeName(domain), dns.SafeName(cover), actor.Name)
		} else {
			log.Tracef("DNS ECH: removed the ECH key from the %s answer for %s (set: %s)", dnsTypeName(qtype), dns.SafeName(domain), actor.Name)
		}
		return out, actor
	case dns.FilterTooLarge:
		log.Warnf("DNS ECH: the answer for %s without its ECH key would not fit the response size the client allows, passing the original through (set: %s)", dns.SafeName(domain), actor.Name)
	}
	return nil, nil
}

func replyDomain(payload []byte) string {
	domain, _ := dns.ParseQueryDomain(payload)
	if domain == "" {
		if d, ok := parseDNSName(payload, 12); ok {
			domain = d
		}
	}
	return strings.ToLower(domain)
}

func (w *Worker) handleECHOnlyPacket(vc *verdictCtx, pkt *pktInfo, cfg *config.Config) int {
	if vc.copy != nil || vc.ifaceFiltered || pkt.proto != 17 || len(pkt.raw) < pkt.ihl+UDPHeaderLen {
		return vc.accept()
	}
	udp := pkt.raw[pkt.ihl:]
	payload := udp[UDPHeaderLen:]
	if binary.BigEndian.Uint16(udp[0:2]) != 53 || len(payload) < 12 || payload[2]&0x80 == 0 {
		return vc.accept()
	}

	domain := replyDomain(payload)
	client := append(net.IP(nil), pkt.dst...)
	server := append(net.IP(nil), pkt.src...)
	clientMac := w.getMacByIp(client.String())
	matched, set := w.getMatcher().MatchSNIWithSource(domain, clientMac)
	if !matched {
		set = nil
	}
	echless, actor := w.stripECHKeys(cfg, set, domain, clientMac, payload, false)
	if echless == nil {
		return vc.accept()
	}

	raw := inPlaceReply(vc, pkt, echless)
	if raw == nil && !vc.drop() {
		return 0
	}
	dport := binary.BigEndian.Uint16(udp[2:4])
	logDNSEvent("UDP", actor, domain, client, server, dport, clientMac, dnsActionECHStripped)
	w.releaseFilteredReply(vc, raw, pkt.ver, server, client, dport, echless)
	return 0
}
