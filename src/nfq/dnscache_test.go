package nfq

import (
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/dns"
)

func TestRecallDNSAnswerReplaysTheLastGoodAddresses(t *testing.T) {
	t.Cleanup(resetDNSAnswerCache)
	resetDNSAnswerCache()

	query := dns.BuildQuery("example.test", 0x1234, dnsTypeA)
	answer := dns.BuildAnswerFromIPs(query, 300, []net.IP{net.ParseIP("93.184.216.34")})
	rememberDNSAnswer("example.test", query, answer)

	retry := dns.BuildQuery("example.test", 0x4321, dnsTypeA)
	got := recallDNSAnswer("example.test", retry)
	if got == nil {
		t.Fatal("recallDNSAnswer returned nothing for a domain that was just remembered")
	}

	txid, ok := dns.ParseTransactionID(got)
	if !ok || txid != 0x4321 {
		t.Fatalf("replayed answer carries transaction id %#x, want 0x4321", txid)
	}
	ips := dns.ParseResponseIPs(got)
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("93.184.216.34")) {
		t.Fatalf("replayed answer carries %v, want [93.184.216.34]", ips)
	}
}

func TestRecallDNSAnswerKeepsRecordTypesApart(t *testing.T) {
	t.Cleanup(resetDNSAnswerCache)
	resetDNSAnswerCache()

	query := dns.BuildQuery("example.test", 1, dnsTypeA)
	rememberDNSAnswer("example.test", query, dns.BuildAnswerFromIPs(query, 300, []net.IP{net.ParseIP("1.2.3.4")}))

	if got := recallDNSAnswer("example.test", dns.BuildQuery("example.test", 2, dnsTypeAAAA)); got != nil {
		t.Fatal("an A answer must not be replayed for an AAAA query")
	}
	if got := recallDNSAnswer("other.test", dns.BuildQuery("other.test", 2, dnsTypeA)); got != nil {
		t.Fatal("an answer must not be replayed for another domain")
	}
}

func TestRememberDNSAnswerIgnoresFailures(t *testing.T) {
	t.Cleanup(resetDNSAnswerCache)
	resetDNSAnswerCache()

	query := dns.BuildQuery("servfail.test", 7, dnsTypeA)
	rememberDNSAnswer("servfail.test", query, dns.BuildServfailResponse(query))

	if got := recallDNSAnswer("servfail.test", query); got != nil {
		t.Fatal("a SERVFAIL must never be cached as the last good answer")
	}
}

func TestRecallDNSAnswerDropsExpiredEntries(t *testing.T) {
	t.Cleanup(resetDNSAnswerCache)
	resetDNSAnswerCache()

	query := dns.BuildQuery("stale.test", 3, dnsTypeA)
	rememberDNSAnswer("stale.test", query, dns.BuildAnswerFromIPs(query, 300, []net.IP{net.ParseIP("5.6.7.8")}))

	dnsAnswerMu.Lock()
	key := dnsAnswerKey{domain: "stale.test", qtype: dnsTypeA}
	entry := dnsAnswerCache[key]
	entry.expires = time.Now().Add(-time.Second)
	dnsAnswerCache[key] = entry
	dnsAnswerMu.Unlock()

	if got := recallDNSAnswer("stale.test", query); got != nil {
		t.Fatal("an expired entry must not be replayed")
	}
}

func TestDNSSourceBreakerTripsAfterRepeatedFailures(t *testing.T) {
	t.Cleanup(resetDNSAnswerCache)
	resetDNSAnswerCache()

	const source = "https://1.1.1.1/dns-query"

	for i := 0; i < dnsSourceFailuresToTrip-1; i++ {
		noteDNSSourceFailure(source)
		if dnsSourceUnreachable(source) {
			t.Fatalf("breaker tripped after %d failures, want %d", i+1, dnsSourceFailuresToTrip)
		}
	}

	noteDNSSourceFailure(source)
	if !dnsSourceUnreachable(source) {
		t.Fatalf("breaker did not trip after %d failures", dnsSourceFailuresToTrip)
	}

	noteDNSSourceSuccess(source)
	if dnsSourceUnreachable(source) {
		t.Fatal("breaker stayed tripped after the source answered again")
	}
}

func TestDNSSourceBreakerLetsOneProbeThroughAfterTheCooldown(t *testing.T) {
	t.Cleanup(resetDNSAnswerCache)
	resetDNSAnswerCache()

	const source = "9.9.9.9"

	for i := 0; i < dnsSourceFailuresToTrip; i++ {
		noteDNSSourceFailure(source)
	}
	if !dnsSourceUnreachable(source) {
		t.Fatal("breaker did not trip")
	}

	dnsSourceMu.Lock()
	dnsSourceHealth[source].retryAt = time.Now().Add(-time.Second)
	dnsSourceMu.Unlock()

	if dnsSourceUnreachable(source) {
		t.Fatal("the cooldown expired, one probe must be let through")
	}
	if !dnsSourceUnreachable(source) {
		t.Fatal("the probe was let through, the breaker must close again until the next cooldown")
	}
}

func TestAnEmptiedAnswerIsNotCacheable(t *testing.T) {
	t.Cleanup(resetDNSAnswerCache)
	resetDNSAnswerCache()

	query := dns.BuildQuery("stripped.test", 11, dnsTypeA)
	full := dns.BuildAnswerFromIPs(query, 300, []net.IP{net.ParseIP("1.2.3.4")})

	emptied := dns.BuildEmptyAnswer(full)
	if emptied == nil {
		t.Fatal("BuildEmptyAnswer returned nothing")
	}
	if got := classifyDNSAnswer(emptied); got != dnsVerdictFailed {
		t.Fatalf("classifyDNSAnswer(emptied) = %v, want dnsVerdictFailed so the cache gate rejects it", got)
	}

	rememberDNSAnswer("stripped.test", query, emptied)
	if got := recallDNSAnswer("stripped.test", query); got != nil {
		t.Fatal("an answer left with no addresses must not become the last good answer")
	}
}
