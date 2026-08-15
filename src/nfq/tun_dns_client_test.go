package nfq

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/engine"
)

const tunTestWAN = "94.189.76.227"

func newTunDNSSet() config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "yt-dns"
	set.Name = "YT dns"
	set.Enabled = true
	set.Targets.DomainOnly = false
	set.Targets.DomainsToMatch = []string{"ytimg.com"}
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil
	set.Faking.SNI = false
	set.Faking.SNIMutation.Mode = config.ConfigOff
	set.TCP.Desync.Mode = config.ConfigOff
	set.TCP.Win.Mode = config.ConfigOff
	set.TCP.DropSACK = false
	return set
}

func TestTunDNSRequestKeysOnLANClient(t *testing.T) {
	set := newTunDNSSet()
	set.Routing.Enabled = true

	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	t.Cleanup(stopDNSRouteCleanup)

	w := newTestWorker(t, &cfg)
	w.srcResolver = newResolverWith(t, tunTestWAN,
		"ipv4 2 udp 17 29 src=192.168.1.100 dst=8.8.8.8 sport=45001 dport=53 src=8.8.8.8 dst="+tunTestWAN+" sport=53 dport=45001 use=1\n")

	const txid = 0x1234
	query := dns.BuildQuery("i.ytimg.com", txid, 1)
	pkt := makeV4UDPPacket(query, net.ParseIP(tunTestWAN), net.ParseIP("8.8.8.8"), 45001, 53)

	if v := w.ProcessPacket(pkt); v != engine.VerdictAccept {
		t.Fatalf("a matched set with no redirect target should pass the query through, got verdict %v", v)
	}

	lanKey := dnsRouteKeyRequest(IPv4, net.ParseIP("192.168.1.100"), 45001, net.ParseIP("8.8.8.8"), 53, txid, "i.ytimg.com")
	if _, ok := consumeDNSPendingRoute(lanKey); !ok {
		wanKey := dnsRouteKeyRequest(IPv4, net.ParseIP(tunTestWAN), 45001, net.ParseIP("8.8.8.8"), 53, txid, "i.ytimg.com")
		if _, stale := consumeDNSPendingRoute(wanKey); stale {
			t.Fatal("pending route was keyed on the post-SNAT WAN address instead of the LAN client")
		}
		t.Fatal("no pending route was stored for the query")
	}
}

func TestTunDNSUnknownClientFailsOpen(t *testing.T) {
	set := newTunDNSSet()
	set.Routing.Enabled = true
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "1.1.1.1"

	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	t.Cleanup(stopDNSRouteCleanup)

	w := newTestWorker(t, &cfg)
	w.srcResolver = newResolverWith(t, tunTestWAN, "")

	const txid = 0x4321
	query := dns.BuildQuery("i.ytimg.com", txid, 1)
	pkt := makeV4UDPPacket(query, net.ParseIP(tunTestWAN), net.ParseIP("8.8.8.8"), 45009, 53)

	if v := w.ProcessPacket(pkt); v != engine.VerdictAccept {
		t.Fatalf("a query whose client cannot be named must be forwarded untouched, got verdict %v", v)
	}

	key := dnsRouteKeyRequest(IPv4, net.ParseIP(tunTestWAN), 45009, net.ParseIP("8.8.8.8"), 53, txid, "i.ytimg.com")
	if _, ok := consumeDNSPendingRoute(key); ok {
		t.Fatal("an unattributable query must not leave state keyed on the router address")
	}
}

func TestTunDNSLocalRouterQueryStillIntercepted(t *testing.T) {
	set := newTunDNSSet()
	set.Routing.Enabled = true

	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	t.Cleanup(stopDNSRouteCleanup)

	w := newTestWorker(t, &cfg)
	w.srcResolver = newResolverWith(t, tunTestWAN,
		"ipv4 2 udp 17 29 src="+tunTestWAN+" dst=8.8.8.8 sport=45100 dport=53 src=8.8.8.8 dst="+tunTestWAN+" sport=53 dport=45100 use=1\n")

	const txid = 0x5678
	query := dns.BuildQuery("i.ytimg.com", txid, 1)
	pkt := makeV4UDPPacket(query, net.ParseIP(tunTestWAN), net.ParseIP("8.8.8.8"), 45100, 53)

	if v := w.ProcessPacket(pkt); v != engine.VerdictAccept {
		t.Fatalf("verdict %v", v)
	}

	key := dnsRouteKeyRequest(IPv4, net.ParseIP(tunTestWAN), 45100, net.ParseIP("8.8.8.8"), 53, txid, "i.ytimg.com")
	if _, ok := consumeDNSPendingRoute(key); !ok {
		t.Fatal("a router-local query is correctly addressed at the WAN address and must still be intercepted")
	}
}

func TestDNSRedirectFailsOpenWhenInflightFull(t *testing.T) {
	for i := 0; i < maxDNSResolveInflight; i++ {
		dnsResolveInflight <- struct{}{}
	}
	defer func() {
		for len(dnsResolveInflight) > 0 {
			<-dnsResolveInflight
		}
	}()

	set := newTunDNSSet()
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "1.1.1.1"

	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}

	w := newTestWorker(t, &cfg)

	query := dns.BuildQuery("i.ytimg.com", 0x1111, 1)
	pkt := makeV4UDPPacket(query, net.ParseIP("192.168.1.100"), net.ParseIP("8.8.8.8"), 45200, 53)

	if v := w.ProcessPacket(pkt); v != engine.VerdictAccept {
		t.Fatalf("with the resolver pool saturated the query must be forwarded, got verdict %v", v)
	}
}

func TestDNSRedirectReleasesInflightSlot(t *testing.T) {
	if n := len(dnsResolveInflight); n != 0 {
		t.Fatalf("test started with %d slots already held", n)
	}

	set := newTunDNSSet()
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "127.0.0.1"

	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}
	cfg.System.DNS.QueryTimeoutSec = 1

	w := newTestWorker(t, &cfg)

	query := dns.BuildQuery("i.ytimg.com", 0x2222, 1)
	pkt := makeV4UDPPacket(query, net.ParseIP("192.168.1.100"), net.ParseIP("8.8.8.8"), 45300, 53)

	if v := w.ProcessPacket(pkt); v != engine.VerdictDrop {
		t.Fatalf("an intercepted query must be dropped, got verdict %v", v)
	}

	w.wg.Wait()

	if n := len(dnsResolveInflight); n != 0 {
		t.Fatalf("the slot must be released when the resolver returns, %d still held", n)
	}
}
