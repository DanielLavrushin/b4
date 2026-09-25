package nfq

import (
	"net"
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/engine"
)

func withDNSNames(t *testing.T, wanted bool) *dns.NameCache {
	t.Helper()
	c := dns.NewNameCache()
	c.SetWanted(wanted)
	prev := DNSNames
	DNSNames = c
	t.Cleanup(func() { DNSNames = prev })
	return c
}

func feedDNSQuery(t *testing.T, w *Worker, txid uint16, domain string) {
	t.Helper()
	server := net.ParseIP("192.168.1.1").To4()
	client := net.ParseIP("192.168.1.100").To4()
	pkt := &pktInfo{
		ver:    IPv4,
		proto:  17,
		src:    client,
		dst:    server,
		srcStr: client.String(),
		dstStr: server.String(),
	}
	vc := &verdictCtx{verdict: engine.VerdictAccept}
	w.processDnsPacket(vc, pkt, 40000, 53, dns.BuildQuery(domain, txid, dnsTypeA))
}

func TestDNSAnswersFeedTheNameCacheForAnyName(t *testing.T) {
	names := withDNSNames(t, true)
	cfg, _, _ := passiveDNSPair(t, 5)
	w := passiveDNSWorker(t, cfg)
	client := net.ParseIP("192.168.1.100")

	feedDNSQuery(t, w, 1, "api.ipify.org")
	feedDNSResponse(t, w, buildDNSResponse(1, "api.ipify.org", []net.IP{net.ParseIP("104.26.12.205")}))
	feedDNSQuery(t, w, 2, "www.youtube.com")
	feedDNSResponse(t, w, buildDNSResponse(2, "www.youtube.com", []net.IP{net.ParseIP("142.250.74.14")}))

	if got := names.Lookup(client, net.ParseIP("104.26.12.205")).Own; !reflect.DeepEqual(got, []string{"api.ipify.org"}) {
		t.Fatalf("a name that matches no set: got %v", got)
	}
	if got := names.Lookup(client, net.ParseIP("142.250.74.14")).Own; !reflect.DeepEqual(got, []string{"www.youtube.com"}) {
		t.Fatalf("a name that matches a set: got %v", got)
	}
}

func TestDNSAnswersWithoutAQueryAreNotRecorded(t *testing.T) {
	names := withDNSNames(t, true)
	cfg, _, _ := passiveDNSPair(t, 5)
	w := passiveDNSWorker(t, cfg)

	feedDNSResponse(t, w, buildDNSResponse(7, "evil.example", []net.IP{net.ParseIP("93.184.216.34")}))
	feedDNSQuery(t, w, 8, "good.example")
	feedDNSResponse(t, w, buildDNSResponse(9, "good.example", []net.IP{net.ParseIP("93.184.216.35")}))
	feedDNSResponse(t, w, buildDNSResponse(8, "other.example", []net.IP{net.ParseIP("93.184.216.36")}))

	for _, ip := range []string{"93.184.216.34", "93.184.216.35", "93.184.216.36"} {
		if got := names.Lookup(nil, net.ParseIP(ip)); !got.Empty() {
			t.Errorf("%s: an answer that matches no query was recorded: %+v", ip, got)
		}
	}
}

func TestDNSAnswersAreNotParsedForNamesNobodyWants(t *testing.T) {
	names := withDNSNames(t, false)
	cfg, _, _ := passiveDNSPair(t, 5)
	w := passiveDNSWorker(t, cfg)

	feedDNSQuery(t, w, 1, "api.ipify.org")
	feedDNSResponse(t, w, buildDNSResponse(1, "api.ipify.org", []net.IP{net.ParseIP("104.26.12.205")}))
	names.SetWanted(true)
	if got := names.Lookup(nil, net.ParseIP("104.26.12.205")); !got.Empty() {
		t.Fatalf("an answer seen with no proxy set wanting names was kept: %+v", got)
	}
}

func TestPinnedAnswersStayOutOfTheNameCache(t *testing.T) {
	names := withDNSNames(t, true)
	cfg, primary, _ := passiveDNSPair(t, 5)
	w := passiveDNSWorker(t, cfg)
	pinned := dns.BuildAnswerFromIPs(dns.BuildQuery("youtube.com", 3, dnsTypeA), 60, []net.IP{net.ParseIP("203.0.113.9")})

	w.applyPinnedAnswer(cfg, primary, net.ParseIP("192.168.1.100"), "youtube.com", pinned)
	if got := names.Lookup(nil, net.ParseIP("203.0.113.9")); !got.Empty() {
		t.Fatalf("a pinned answer was recorded, so another proxy set could send the pinned name: %+v", got)
	}
}
