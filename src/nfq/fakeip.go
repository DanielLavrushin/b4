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
	case dnsTypeAAAA:
		if empty := dns.BuildEmptyAnswer(query); empty != nil {
			log.Tracef("DNS fake: answering the AAAA query for %s empty so the client uses the fake A record (set: %s)", dns.SafeName(domain), set.Name)
			return empty
		}
	}
	return nil
}
