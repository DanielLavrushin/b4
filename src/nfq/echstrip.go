package nfq

import (
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
	cover := dns.ECHPublicName(dns.ParseResponseECH(resp))
	if cover == "" {
		return nil, ""
	}
	matched, coverSet := w.getMatcher().MatchSNIWithSource(cover, clientMac)
	if !matched || !stripsECH(coverSet) {
		return nil, ""
	}
	return coverSet, cover
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
