package nfq

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/florianl/go-nfqueue"
)

func ipv6OffWorker(t *testing.T, sets ...*config.SetConfig) *Worker {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	cfg.Sets = sets
	return passiveDNSWorker(t, &cfg)
}

func v6Answer(payload []byte) []byte {
	return makeV6UDPPacketWithExt(payload, 53, 40000, 0, nil)
}

func TestAnIPv6AnswerLosesItsECHKeyWhileIPv6SupportIsOff(t *testing.T) {
	w := ipv6OffWorker(t, echTestSet("steamdb", true, "steamdb.info"))
	sent := captureSends(t)
	q := newFakeModQueue()
	raw := v6Answer(httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com"))

	w.dispatch(&verdictCtx{id: 1, q: q}, raw)

	v := q.now(t)
	if v.verdict != nfqueue.NfAccept || v.packet == nil {
		t.Fatalf("verdict %d carrying %d bytes, want the answer accepted with its key removed in place", v.verdict, len(v.packet))
	}
	if dns.ParseResponseECH(v.packet[48:]) != nil {
		t.Fatal("the answer that came over IPv6 kept its ECH key: a validating resolver that reaches the zone's servers over IPv6 caches it and hands it to every device")
	}
	if binary.BigEndian.Uint16(v.packet[48+6:48+8]) != 1 {
		t.Fatal("the HTTPS record itself must stay, only its key goes")
	}
	if !bytes.Equal(v.packet[8:40], raw[8:40]) {
		t.Fatal("the rewritten answer changed its addresses")
	}
	sent.none(t, 30*time.Millisecond, "b4 sent a packet of its own although it rewrote the answer in place")
}

func TestAnIPv6AnswerLosesTheKeyNamingACoverSet(t *testing.T) {
	w := ipv6OffWorker(t, echTestSet("cloudflare-ech", true, "cloudflare-ech.com"))
	q := newFakeModQueue()

	w.dispatch(&verdictCtx{id: 1, q: q}, v6Answer(httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")))

	v := q.now(t)
	if v.packet == nil || dns.ParseResponseECH(v.packet[48:]) != nil {
		t.Fatal("a key whose public name a set strips must go from an IPv6 answer too")
	}
}

func TestAnIPv6AnswerIsLeftAloneWithoutASetThatRemovesKeys(t *testing.T) {
	w := ipv6OffWorker(t, echTestSet("steamdb", false, "steamdb.info"))
	q := newFakeModQueue()

	w.dispatch(&verdictCtx{id: 1, q: q}, v6Answer(httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")))

	if v := q.now(t); v.verdict != nfqueue.NfAccept || v.packet != nil {
		t.Fatalf("verdict %d carrying %d bytes, want the answer passed unchanged", v.verdict, len(v.packet))
	}
}

func TestAnIPv6AnswerOnlyLosesItsKeyWhileIPv6SupportIsOff(t *testing.T) {
	set := echTestSet("example", true, "example.com")
	set.Routing.Enabled = true
	w := ipv6OffWorker(t, set)
	q := newFakeModQueue()

	w.dispatch(&verdictCtx{id: 1, q: q}, v6Answer(buildTestMixedResponse("www.example.com", dnsTypeA, "203.0.113.11", "2001:db8::11")))

	if v := q.now(t); v.verdict != nfqueue.NfAccept || v.packet != nil {
		t.Fatalf("verdict %d carrying %d bytes: an answer that comes over IPv6 while IPv6 support is off is queued only for its ECH key, so its IPv6 addresses stay", v.verdict, len(v.packet))
	}
	if _, host, ok := w.hostHints.Lookup(v6TestDst, "203.0.113.11"); ok || host != "" {
		t.Fatalf("b4 learned %q from an answer it queued only for its ECH key", host)
	}
}

func TestAnIPv6QueryIsNotRedirectedWhileIPv6SupportIsOff(t *testing.T) {
	set := echTestSet("steamdb", true, "steamdb.info")
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "203.0.113.53"
	w := ipv6OffWorker(t, set)
	sent := captureSends(t)
	q := newFakeModQueue()

	w.dispatch(&verdictCtx{id: 1, q: q}, makeV6UDPPacketWithExt(dns.BuildQuery("steamdb.info", 7, dnsTypeA), 40000, 53, 0, nil))

	if v := q.now(t); v.verdict != nfqueue.NfAccept || v.packet != nil {
		t.Fatalf("verdict %d carrying %d bytes, want the query passed unchanged", v.verdict, len(v.packet))
	}
	sent.none(t, 30*time.Millisecond, "b4 answered an IPv6 query although IPv6 support is off")
}

func tcpQueriesFrom(t *testing.T, w *Worker, client net.IP, queries ...[]byte) [][]byte {
	t.Helper()
	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: client, Port: 40000}})
	}()

	_ = clientEnd.SetDeadline(time.Now().Add(5 * time.Second))
	var answers [][]byte
	for i, query := range queries {
		if err := writeDNSTCPMessage(clientEnd, query, time.Second); err != nil {
			t.Fatal(err)
		}
		resp, err := readDNSTCPMessage(clientEnd)
		if err != nil {
			t.Fatalf("query %d: no answer: %v", i, err)
		}
		answers = append(answers, resp)
	}
	clientEnd.Close()
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection handler never returned")
	}
	return answers
}

func TestDNSOverTCPFromIPv6OnlyRemovesKeysWhileIPv6SupportIsOff(t *testing.T) {
	set := echTestSet("steamdb", true, "steamdb.info")
	set.DNS.Pins = map[string][]string{"steamdb.info": {"198.51.100.7"}}
	cfg := config.NewConfig()
	cfg.Queue.IPv6Enabled = false
	cfg.Sets = []*config.SetConfig{set}
	w := newTestWorker(t, &cfg)
	tcpECHUpstream(t)

	answers := tcpQueriesFrom(t, w, net.ParseIP("2001:db8:15::116"),
		dns.BuildQuery("steamdb.info", 1, dnsTypeA),
		dns.BuildQuery("steamdb.info", 2, dnsTypeHTTPS))

	if ips := dns.ParseResponseIPs(answers[0]); len(ips) != 1 || !ips[0].Equal(net.ParseIP("203.0.113.80")) {
		t.Fatalf("A answer %v, want the upstream's: a connection over IPv6 while IPv6 support is off is relayed only to remove ECH keys, so the set's pin does not answer it", ips)
	}
	if names := dns.ResponseECHPublicNames(answers[1]); len(names) != 0 {
		t.Fatalf("the HTTPS answer kept its key %v", names)
	}
}

func TestDNSOverTCPFromIPv4StillUsesThePin(t *testing.T) {
	set := echTestSet("steamdb", true, "steamdb.info")
	set.DNS.Pins = map[string][]string{"steamdb.info": {"198.51.100.7"}}
	cfg := config.NewConfig()
	cfg.Queue.IPv6Enabled = false
	cfg.Sets = []*config.SetConfig{set}
	w := newTestWorker(t, &cfg)
	tcpECHUpstream(t)

	answers := tcpQueriesFrom(t, w, net.ParseIP("192.0.2.10"), dns.BuildQuery("steamdb.info", 1, dnsTypeA))

	if ips := dns.ParseResponseIPs(answers[0]); len(ips) != 1 || !ips[0].Equal(net.ParseIP("198.51.100.7")) {
		t.Fatalf("A answer %v, want the pin for a connection of a family b4 processes", ips)
	}
}

func deviceListSet(id string, exclude bool, domains ...string) *config.SetConfig {
	set := echTestSet(id, true, domains...)
	set.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
	set.Targets.SourceDevicesExclude = exclude
	return set
}

func TestAnIPv6AnswerToAnUnknownDeviceIsLeftAloneByASetWithADeviceList(t *testing.T) {
	for _, exclude := range []bool{true, false} {
		w := ipv6OffWorker(t, deviceListSet("steamdb", exclude, "steamdb.info"))
		q := newFakeModQueue()

		w.dispatch(&verdictCtx{id: 1, q: q}, v6Answer(httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")))

		if v := q.now(t); v.verdict != nfqueue.NfAccept || v.packet != nil {
			t.Errorf("exclude=%v: verdict %d carrying %d bytes: b4 cannot tell which device an IPv6 answer goes to, so a set limited by a device list must leave it alone", exclude, v.verdict, len(v.packet))
		}
	}
}

func TestTheIPv4PathStillRemovesKeysForAnExcludeListWithAnUnknownDevice(t *testing.T) {
	set := deviceListSet("steamdb", true, "steamdb.info")
	w, cfg := echTestWorker(t, set)

	out, action, _ := w.filterDNSAnswer(cfg, set, "steamdb.info", "", httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com"), false)
	if out == nil || action != dnsActionECHStripped {
		t.Fatalf("action %q: the router's own lookups carry no MAC, and an exclude-list set has always removed keys from them", action)
	}
}

func TestDNSOverTCPFromIPv6LeavesKeysToASetWithADeviceList(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.IPv6Enabled = false
	cfg.Sets = []*config.SetConfig{deviceListSet("steamdb", true, "steamdb.info")}
	w := newTestWorker(t, &cfg)
	tcpECHUpstream(t)

	answers := tcpQueriesFrom(t, w, net.ParseIP("2001:db8:15::116"), dns.BuildQuery("steamdb.info", 2, dnsTypeHTTPS))
	if names := dns.ResponseECHPublicNames(answers[0]); len(names) != 1 {
		t.Fatalf("keys %v: a set limited by a device list must not act on a connection from a device b4 cannot identify", names)
	}
}

func TestADNSTCPListenerWithoutItsIPv6SocketIsNotRestartedOnEverySave(t *testing.T) {
	recordDNSTCPReady(t)
	held, err := net.Listen("tcp6", "[::]:0")
	if err != nil {
		t.Skipf("no IPv6 socket on this host: %v", err)
	}
	defer held.Close()

	cfg := config.NewConfig()
	cfg.Queue.IPv6Enabled = false
	cfg.System.DNS.TCPPort = held.Addr().(*net.TCPAddr).Port
	cfg.Sets = []*config.SetConfig{echTestSet("steamdb", true, "steamdb.info")}
	p := &Pool{Workers: []*Worker{NewWorkerWithQueue(&cfg, 0)}}
	p.startDNSTCP()
	t.Cleanup(func() {
		if p.dnsTCP != nil {
			p.dnsTCP.Stop()
		}
	})
	if p.dnsTCP == nil || !p.dnsTCP.ReadyV4() || p.dnsTCP.ReadyV6() {
		t.Skip("could not set up a listener with only its IPv4 socket bound")
	}

	first := p.dnsTCP
	p.reconcileDNSTCP(&cfg)
	if p.dnsTCP != first {
		t.Fatal("an unchanged config restarted the DNS-over-TCP listener because its IPv6 socket could not bind, dropping every open TCP DNS connection on each save")
	}
}
