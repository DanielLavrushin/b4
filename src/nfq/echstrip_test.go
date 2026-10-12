package nfq

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

func testECHConfigList(publicName string) []byte {
	var c []byte
	c = append(c, 0x42)
	c = binary.BigEndian.AppendUint16(c, 0x0020)
	c = binary.BigEndian.AppendUint16(c, 32)
	c = append(c, bytes.Repeat([]byte{0xab}, 32)...)
	c = binary.BigEndian.AppendUint16(c, 4)
	c = binary.BigEndian.AppendUint16(c, 0x0001)
	c = binary.BigEndian.AppendUint16(c, 0x0001)
	c = append(c, 0, byte(len(publicName)))
	c = append(c, publicName...)
	c = binary.BigEndian.AppendUint16(c, 0)
	list := binary.BigEndian.AppendUint16(nil, 0xfe0d)
	list = binary.BigEndian.AppendUint16(list, uint16(len(c)))
	list = append(list, c...)
	return append(binary.BigEndian.AppendUint16(nil, uint16(len(list))), list...)
}

func httpsAnswerWithECH(qname string, qtype uint16, covers ...string) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], 0x4242)
	binary.BigEndian.PutUint16(msg[2:4], 0x8180)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	binary.BigEndian.PutUint16(msg[6:8], uint16(len(covers)))
	msg = append(msg, encodeTestName(qname)...)
	msg = binary.BigEndian.AppendUint16(msg, qtype)
	msg = binary.BigEndian.AppendUint16(msg, 1)

	for _, cover := range covers {
		rdata := []byte{0, 1, 0}
		rdata = append(rdata, 0, 1, 0, 3, 2, 'h', '2')
		list := testECHConfigList(cover)
		rdata = binary.BigEndian.AppendUint16(rdata, 5)
		rdata = binary.BigEndian.AppendUint16(rdata, uint16(len(list)))
		rdata = append(rdata, list...)

		msg = append(msg, 0xC0, 0x0C)
		msg = binary.BigEndian.AppendUint16(msg, qtype)
		msg = binary.BigEndian.AppendUint16(msg, 1)
		msg = binary.BigEndian.AppendUint32(msg, 300)
		msg = binary.BigEndian.AppendUint16(msg, uint16(len(rdata)))
		msg = append(msg, rdata...)
	}
	return msg
}

func echTestSet(id string, strip bool, domains ...string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Name = id
	set.Enabled = true
	set.Targets.SNIDomains = domains
	set.Targets.DomainsToMatch = domains
	set.DNS.StripECH = strip
	return &set
}

func echTestWorker(t *testing.T, sets ...*config.SetConfig) (*Worker, *config.Config) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Sets = sets
	return newTestWorker(t, &cfg), &cfg
}

func TestFilterDNSAnswerStripsECHForTheQueriedSet(t *testing.T) {
	site := echTestSet("steamdb", true, "steamdb.info")
	w, cfg := echTestWorker(t, site)
	resp := httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")

	out, action, actor := w.filterDNSAnswer(cfg, site, "steamdb.info", "", resp, false)
	if out == nil || action != dnsActionECHStripped || actor != site {
		t.Fatalf("action %q by %v, want %q by the site's set", action, actor, dnsActionECHStripped)
	}
	if dns.ParseResponseECH(out) != nil {
		t.Fatal("the answer still carries the ECH key")
	}
}

func TestFilterDNSAnswerStripsECHByTheKeyCoverName(t *testing.T) {
	cover := echTestSet("cloudflare-ech", true, "cloudflare-ech.com")
	other := echTestSet("other", false, "steamdb.info")
	w, cfg := echTestWorker(t, other, cover)

	for name, querySet := range map[string]*config.SetConfig{"no set": nil, "set without the switch": other} {
		resp := httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")
		out, action, actor := w.filterDNSAnswer(cfg, querySet, "steamdb.info", "", resp, false)
		if out == nil || action != dnsActionECHStripped || actor != cover {
			t.Fatalf("%s: action %q by %v, want the cover set to strip it", name, action, actor)
		}
	}

	resp := httpsAnswerWithECH("example.org", dnsTypeHTTPS, "cover.example.net")
	if out, _, _ := w.filterDNSAnswer(cfg, nil, "example.org", "", resp, false); out != nil {
		t.Fatal("stripped a key whose cover name no set lists")
	}
}

func TestFilterDNSAnswerKeepsECHWhenNoSetAsks(t *testing.T) {
	plain := echTestSet("cloudflare-ech", false, "cloudflare-ech.com")
	off := echTestSet("off", true, "steamdb.info")
	off.Enabled = false
	w, cfg := echTestWorker(t, plain, off)

	resp := httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")
	if out, _, _ := w.filterDNSAnswer(cfg, off, "steamdb.info", "", resp, false); out != nil {
		t.Fatal("a disabled set stripped the key")
	}
	if out, _, _ := w.filterDNSAnswer(cfg, plain, "cloudflare-ech.com", "", resp, false); out != nil {
		t.Fatal("stripped although no enabled set has the switch on")
	}
}

func TestFilterDNSAnswerECHStripHonoursSourceDevices(t *testing.T) {
	const phone = "aa:bb:cc:dd:ee:01"
	cover := echTestSet("cloudflare-ech", true, "cloudflare-ech.com")
	cover.Targets.SourceDevices = []string{phone}
	w, cfg := echTestWorker(t, cover)

	resp := httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")
	if out, _, _ := w.filterDNSAnswer(cfg, nil, "steamdb.info", "aa:bb:cc:dd:ee:02", resp, false); out != nil {
		t.Fatal("stripped the answer for a device the set does not cover")
	}
	if out, _, actor := w.filterDNSAnswer(cfg, nil, "steamdb.info", phone, resp, false); out == nil || actor != cover {
		t.Fatal("kept the key for the device the set covers")
	}
}

func TestFilterDNSAnswerECHStripScope(t *testing.T) {
	site := echTestSet("steamdb", true, "steamdb.info")
	w, cfg := echTestWorker(t, site)

	svcb := httpsAnswerWithECH("steamdb.info", dnsTypeSVCB, "cloudflare-ech.com")
	if out, action, _ := w.filterDNSAnswer(cfg, site, "steamdb.info", "", svcb, true); out == nil || action != dnsActionECHStripped {
		t.Fatal("an SVCB answer kept its key")
	}

	discovery := *cfg
	discovery.Queue.IsDiscovery = true
	resp := httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com")
	if out, _, _ := w.filterDNSAnswer(&discovery, site, "steamdb.info", "", resp, false); out != nil {
		t.Fatal("the Discovery queue stripped a key Discovery itself asked for")
	}
}

func TestECHStripWanted(t *testing.T) {
	site := echTestSet("steamdb", true, "steamdb.info")
	plain := echTestSet("plain", false, "example.org")

	withStrip := config.NewConfig()
	withStrip.Sets = []*config.SetConfig{plain, site}
	without := config.NewConfig()
	without.Sets = []*config.SetConfig{plain}

	cases := []struct {
		name  string
		cfg   *config.Config
		set   *config.SetConfig
		qtype uint16
		want  bool
	}{
		{"https query, a set strips", &withStrip, nil, dnsTypeHTTPS, true},
		{"svcb query, its own set strips", &withStrip, site, dnsTypeSVCB, true},
		{"address query", &withStrip, site, dnsTypeA, false},
		{"no set strips", &without, plain, dnsTypeHTTPS, false},
		{"no config", nil, site, dnsTypeHTTPS, false},
	}
	for _, tc := range cases {
		if got := echStripWanted(tc.cfg, tc.set, tc.qtype); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFilterDNSAnswerStripsECHWhenAnyKeyNamesACover(t *testing.T) {
	cover := echTestSet("other-cdn", true, "ech.other-cdn.example")
	w, cfg := echTestWorker(t, cover)

	resp := httpsAnswerWithECH("multi.example", dnsTypeHTTPS, "cloudflare-ech.com", "ech.other-cdn.example")
	out, action, actor := w.filterDNSAnswer(cfg, nil, "multi.example", "", resp, false)
	if out == nil || action != dnsActionECHStripped || actor != cover {
		t.Fatalf("action %q by %v: a client may pick the second record, whose key names the set's cover", action, actor)
	}
	if names := dns.ResponseECHPublicNames(out); len(names) != 0 {
		t.Fatalf("keys left in the answer: %v", names)
	}
}

type echUpstream struct {
	conns atomic.Int32
}

func tcpECHUpstream(t *testing.T) *echUpstream {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	u := &echUpstream{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var open []net.Conn
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			u.conns.Add(1)
			mu.Lock()
			open = append(open, conn)
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				for {
					query, err := readDNSTCPMessage(conn)
					if err != nil {
						return
					}
					domain, _ := dns.ParseQueryDomain(query)
					qtype, _ := dns.QuestionType(query)
					reply := buildDNSResponse(0, domain, []net.IP{net.ParseIP("203.0.113.80")})
					if qtype == dnsTypeHTTPS {
						reply = httpsAnswerWithECH(domain, qtype, "cloudflare-ech.com")
					}
					copy(reply[:2], query[:2])
					messages := 1
					if qtype == dnsTypeAXFR {
						messages = 3
					}
					if !standardQuery(query) {
						reply, messages = append([]byte(nil), query...), 2
					}
					for range messages {
						if writeDNSTCPMessage(conn, reply, time.Second) != nil {
							return
						}
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range open {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})

	prevDst, prevDial := dnsTCPOriginalDst, dialDNSTCPUpstream
	dnsTCPOriginalDst = func(net.Conn) (net.IP, int, error) { return addr.IP, addr.Port, nil }
	dialDNSTCPUpstream = func(ctx context.Context, _ *config.Config, a string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", a)
	}
	t.Cleanup(func() { dnsTCPOriginalDst, dialDNSTCPUpstream = prevDst, prevDial })
	return u
}

func mixedTCPQueries(t *testing.T, w *Worker) [][]byte {
	t.Helper()
	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 40000}})
	}()

	_ = clientEnd.SetDeadline(time.Now().Add(5 * time.Second))
	var answers [][]byte
	for i, q := range []struct {
		name  string
		qtype uint16
	}{{"example.org", dnsTypeA}, {"steamdb.info", dnsTypeHTTPS}, {"example.org", dnsTypeA}} {
		if err := writeDNSTCPMessage(clientEnd, dns.BuildQuery(q.name, uint16(100+i), q.qtype), time.Second); err != nil {
			t.Fatal(err)
		}
		resp, err := readDNSTCPMessage(clientEnd)
		if err != nil {
			t.Fatalf("query %d (%s): no answer on the same connection: %v", i, q.name, err)
		}
		if got := binary.BigEndian.Uint16(resp[:2]); got != uint16(100+i) {
			t.Fatalf("query %d: answer id %d", i, got)
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

func TestDNSOverTCPStripsECHAfterAnotherQueryOnTheConnection(t *testing.T) {
	w, _ := echTestWorker(t, echTestSet("cloudflare-ech", true, "cloudflare-ech.com"))
	upstream := tcpECHUpstream(t)

	answers := mixedTCPQueries(t, w)
	if names := dns.ResponseECHPublicNames(answers[1]); len(names) != 0 {
		t.Fatalf("the HTTPS answer after an A query kept its key %v: the connection went raw", names)
	}
	if binary.BigEndian.Uint16(answers[1][6:8]) != 1 {
		t.Fatal("the HTTPS record itself must stay, only its key goes")
	}
	for _, i := range []int{0, 2} {
		if ips := dns.ParseResponseIPs(answers[i]); len(ips) != 1 {
			t.Fatalf("A answer %d changed: %v", i, ips)
		}
	}
	if n := upstream.conns.Load(); n != 1 {
		t.Fatalf("%d upstream connections, want the relay to reuse one", n)
	}
}

func TestDNSOverTCPStaysRawWithoutAnECHStripSet(t *testing.T) {
	w, _ := echTestWorker(t, echTestSet("plain", false, "cloudflare-ech.com"))
	tcpECHUpstream(t)

	answers := mixedTCPQueries(t, w)
	if names := dns.ResponseECHPublicNames(answers[1]); len(names) != 1 {
		t.Fatalf("keys %v: with no set stripping, the connection is passed through untouched", names)
	}
}

func TestDNSOverTCPZoneTransferStaysAStream(t *testing.T) {
	w, _ := echTestWorker(t, echTestSet("cloudflare-ech", true, "cloudflare-ech.com"))
	tcpECHUpstream(t)
	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 40000}})
	}()

	_ = clientEnd.SetDeadline(time.Now().Add(3 * time.Second))
	if err := writeDNSTCPMessage(clientEnd, dns.BuildQuery("example.org", 77, dnsTypeAXFR), time.Second); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		resp, err := readDNSTCPMessage(clientEnd)
		if err != nil {
			t.Fatalf("transfer message %d never arrived: a zone transfer answers one query with many messages: %v", i+1, err)
		}
		if binary.BigEndian.Uint16(resp[:2]) != 77 {
			t.Fatalf("message %d carries id %d", i+1, binary.BigEndian.Uint16(resp[:2]))
		}
	}
	clientEnd.Close()
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection handler never returned")
	}
}

func TestDNSOverTCPOtherOpcodesStayAStream(t *testing.T) {
	w, _ := echTestWorker(t, echTestSet("cloudflare-ech", true, "cloudflare-ech.com"))
	tcpECHUpstream(t)
	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 40000}})
	}()

	keepalive := make([]byte, 12)
	binary.BigEndian.PutUint16(keepalive[0:2], 0x0501)
	keepalive[2] = 6 << 3
	keepalive = append(keepalive, 0, 1, 0, 8, 0, 0, 0x75, 0x30, 0, 0, 0x27, 0x10)

	_ = clientEnd.SetDeadline(time.Now().Add(3 * time.Second))
	if err := writeDNSTCPMessage(clientEnd, keepalive, time.Second); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, err := readDNSTCPMessage(clientEnd); err != nil {
			t.Fatalf("stateful message %d never arrived: a DSO server sends messages of its own: %v", i+1, err)
		}
	}
	clientEnd.Close()
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection handler never returned")
	}
}
