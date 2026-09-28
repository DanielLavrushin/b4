package nfq

import (
	"net"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
)

const (
	dnsAnswerCacheTTL      = time.Hour
	dnsAnswerCacheMaxSize  = 4096
	dnsAnswerCacheServeTTL = 60
)

type dnsAnswerKey struct {
	domain string
	qtype  uint16
}

type dnsAnswerEntry struct {
	ips     []net.IP
	expires time.Time
}

var (
	dnsAnswerMu    sync.Mutex
	dnsAnswerCache = map[dnsAnswerKey]dnsAnswerEntry{}
)

func rememberDNSAnswer(domain string, query, resp []byte) {
	if domain == "" || len(query) == 0 || len(resp) == 0 {
		return
	}
	if rcode, ok := dns.ResponseRcode(resp); ok && rcode != dns.RcodeNoError {
		return
	}
	qtype, ok := dns.QuestionType(query)
	if !ok {
		return
	}
	ips := dns.ParseResponseIPs(resp)
	if len(ips) == 0 {
		return
	}

	stored := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		stored = append(stored, append(net.IP(nil), ip...))
	}

	dnsAnswerMu.Lock()
	defer dnsAnswerMu.Unlock()

	if len(dnsAnswerCache) >= dnsAnswerCacheMaxSize {
		pruneDNSAnswerCacheLocked()
	}
	dnsAnswerCache[dnsAnswerKey{domain: domain, qtype: qtype}] = dnsAnswerEntry{
		ips:     stored,
		expires: time.Now().Add(dnsAnswerCacheTTL),
	}
}

func recallDNSAnswer(domain string, query []byte) []byte {
	if domain == "" || len(query) == 0 {
		return nil
	}
	qtype, ok := dns.QuestionType(query)
	if !ok {
		return nil
	}

	dnsAnswerMu.Lock()
	entry, found := dnsAnswerCache[dnsAnswerKey{domain: domain, qtype: qtype}]
	if found && time.Now().After(entry.expires) {
		delete(dnsAnswerCache, dnsAnswerKey{domain: domain, qtype: qtype})
		found = false
	}
	dnsAnswerMu.Unlock()

	if !found || len(entry.ips) == 0 {
		return nil
	}
	return dns.BuildAnswerFromIPs(query, dnsAnswerCacheServeTTL, entry.ips)
}

func pruneDNSAnswerCacheLocked() {
	now := time.Now()
	for key, entry := range dnsAnswerCache {
		if now.After(entry.expires) {
			delete(dnsAnswerCache, key)
		}
	}
	if len(dnsAnswerCache) < dnsAnswerCacheMaxSize {
		return
	}
	drop := len(dnsAnswerCache) - dnsAnswerCacheMaxSize/2
	for key := range dnsAnswerCache {
		if drop <= 0 {
			return
		}
		delete(dnsAnswerCache, key)
		drop--
	}
}

func resetDNSAnswerCache() {
	dnsAnswerMu.Lock()
	dnsAnswerCache = map[dnsAnswerKey]dnsAnswerEntry{}
	dnsAnswerMu.Unlock()

	dns.ResetSourceHealth()
}

const dnsSourceFailuresToTrip = dns.SourceFailuresToTrip

func dnsSourceUnreachable(source string) bool {
	return dns.SourceUnreachable(source)
}

func noteDNSSourceFailure(source string) {
	if dns.NoteSourceFailure(source) {
		log.Warnf("DNS redirect: %s failed %d times in a row, answering from the fallback until it recovers", dnsUpstreamLabel(source), dnsSourceFailuresToTrip)
	}
}

func noteDNSSourceSuccess(source string) {
	if dns.NoteSourceSuccess(source) {
		log.Infof("DNS redirect: %s is answering again", dnsUpstreamLabel(source))
	}
}

func (w *Worker) dnsRedirectFallback(cfg *config.Config, set *config.SetConfig, domain string, query []byte, originalDst, failedTarget net.IP, delay int) ([]byte, string) {
	if w == nil || cfg == nil || set == nil || set.DNS.Strict {
		return nil, ""
	}

	if cached := recallDNSAnswer(domain, query); cached != nil {
		return cached, dnsActionFallbackCache
	}

	if originalDst == nil || originalDst.IsUnspecified() {
		return nil, ""
	}
	if failedTarget != nil && failedTarget.Equal(originalDst) {
		return nil, ""
	}

	resp, err := dns.ResolveUpstream(query, originalDst, dns.ForwardOptions{
		Sender:       w.sock,
		Fragment:     set.DNS.FragmentQuery,
		Seg2Delay:    delay,
		ReverseOrder: set.Fragmentation.ReverseOrder,
		Mark:         int(cfg.MainInjectedMark()),
		Timeout:      cfg.DNSQueryTimeout(),
	})
	if err != nil || len(resp) == 0 {
		return nil, ""
	}
	return resp, dnsActionFallbackUpstream
}
