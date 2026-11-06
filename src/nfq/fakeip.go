package nfq

import (
	"net"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
)

var fakeIPPool = dns.DefaultFakeIPPool()

var fakeIPExhausted sync.Map

func logFakeIPExhausted(set *config.SetConfig, network *net.IPNet) {
	key := set.Id + "|" + network.String()
	if _, seen := fakeIPExhausted.LoadOrStore(key, struct{}{}); seen {
		return
	}
	log.Warnf("DNS fake: no free address left in %s, so the set's fake answers are SERVFAIL until leases expire (set: %s)", network.String(), set.Name)
}

func fakeIPAdmits(set *config.SetConfig, clientIP net.IP, from []*ingressScope) bool {
	if queryFromTarget(set, clientIP) {
		return false
	}
	sc := scopeFrom(from)
	if sc == nil {
		return true
	}
	sc.resolve()
	if sc.router || sc.uplink || sc.unattributed {
		return false
	}
	if set.SourceInterfaceScoped() && sc.name != "" && !set.ListsSourceInterface(sc.name) {
		return false
	}
	if set.SourceDeviceScoped() && sc.mac == "" {
		return false
	}
	return true
}

func fakeIPRoutingLive(cfg *config.Config, set *config.SetConfig) bool {
	if cfg == nil {
		return true
	}
	if !cfg.Queue.IPv4Enabled || !set.Routing.Enabled {
		return false
	}
	if cfg.Queue.Mode == "tun" {
		return true
	}
	return config.RoutingLive(set.Id)
}

func (w *Worker) fakeIPAnswer(cfg *config.Config, set *config.SetConfig, query []byte, domain string, clientIP net.IP, from ...*ingressScope) []byte {
	if set == nil || len(query) == 0 || domain == "" {
		return nil
	}
	if !fakeIPRoutingLive(cfg, set) {
		return nil
	}
	network, ttl, ok := set.DNS.FakeIPNetwork()
	if !ok {
		return nil
	}
	qtype, ok := dns.QuestionType(query)
	if !ok {
		return nil
	}
	if !fakeIPAdmits(set, clientIP, from) {
		return nil
	}

	switch qtype {
	case dnsTypeA:
		ip := fakeIPPool.AddressFor(domain, network, time.Duration(ttl)*time.Second)
		if ip == nil {
			logFakeIPExhausted(set, network)
			return dns.BuildServfailResponse(query)
		}
		resp := dns.BuildAnswerFromIPs(query, uint32(ttl), []net.IP{ip})
		if resp == nil {
			return nil
		}
		observeDNSNames(clientIP, domain, []net.IP{ip})
		log.Tracef("DNS fake: answering %s with %s from %s (set: %s)", dns.SafeName(domain), ip, network, set.Name)
		return resp
	case dnsTypeAAAA, dnsTypeCNAME, dnsTypeSVCB, dnsTypeHTTPS, dnsTypeANY:
		if empty := dns.BuildEmptyAnswer(query); empty != nil {
			log.Tracef("DNS fake: answering the type %d query for %s empty so no real hint or key reaches the device (set: %s)", qtype, dns.SafeName(domain), set.Name)
			return empty
		}
	}
	return nil
}
