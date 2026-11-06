package nfq

import (
	"net"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
)

var fakeIPPool = dns.NewFakeIPPool()

func (w *Worker) fakeIPAnswer(set *config.SetConfig, query []byte, domain string, clientIP net.IP) []byte {
	if set == nil || len(query) == 0 || domain == "" {
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

	switch qtype {
	case dnsTypeA:
		ip := fakeIPPool.Address(domain, network)
		if ip == nil {
			return nil
		}
		resp := dns.BuildAnswerFromIPs(query, uint32(ttl), []net.IP{ip})
		if resp == nil {
			return nil
		}
		observeDNSNames(clientIP, domain, []net.IP{ip})
		log.Tracef("DNS fake: answering %s with %s from %s (set: %s)", dns.SafeName(domain), ip, network, set.Name)
		return resp
	case dnsTypeAAAA:
		if empty := dns.BuildEmptyAnswer(query); empty != nil {
			log.Tracef("DNS fake: answering the AAAA query for %s empty so the client uses the fake A record (set: %s)", dns.SafeName(domain), set.Name)
			return empty
		}
	}
	return nil
}
