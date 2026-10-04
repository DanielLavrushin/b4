package nfq

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/florianl/go-nfqueue"
)

func thisHost(t *testing.T, addrs ...string) {
	t.Helper()
	prev := hostAddress
	hostAddress = func(ip net.IP) bool {
		if ip.IsLoopback() {
			return true
		}
		for _, a := range addrs {
			if ip.Equal(net.ParseIP(a)) {
				return true
			}
		}
		return false
	}
	t.Cleanup(func() { hostAddress = prev })
}

func redirectSet(target, doh string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "lan", "lan", true
	set.Targets.DomainsToMatch = []string{"example.com"}
	set.DNS.Enabled = true
	set.DNS.TargetDNS = target
	set.DNS.DoHURL = doh
	return &set
}

func redirectWorker(t *testing.T, ipv6 bool, sets ...*config.SetConfig) *Worker {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Queue.IPv6Enabled = ipv6
	cfg.Sets = sets
	w := passiveDNSWorker(t, &cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.ctx = ctx
	t.Cleanup(w.wg.Wait)
	return w
}

func lanQuery(src, dst string) *pktInfo {
	s, d := net.ParseIP(src), net.ParseIP(dst)
	ver := uint8(IPv6)
	if s.To4() != nil {
		s, d, ver = s.To4(), d.To4(), IPv4
	}
	return &pktInfo{ver: ver, proto: 17, src: s, dst: d, srcStr: s.String(), dstStr: d.String()}
}

func connectionLines(t *testing.T) chan string {
	t.Helper()
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	ch, _ := log.GetConnectionHub().Subscribe()
	t.Cleanup(func() {
		log.GetConnectionHub().Unsubscribe(ch)
		log.Init(os.Stderr, previous, true)
	})
	return ch
}

func drainLines(ch chan string) []string {
	var lines []string
	for {
		select {
		case l := <-ch:
			lines = append(lines, l)
		default:
			return lines
		}
	}
}

func fromTargetLoggedOnce(t *testing.T, ch chan string) {
	t.Helper()
	lines := drainLines(ch)
	n := 0
	for _, l := range lines {
		if strings.Contains(l, ","+dnsActionForwardPrefix) || strings.Contains(l, ","+dnsActionDoHPrefix) {
			t.Errorf("a redirect was logged for a query that was forwarded unchanged: %s", l)
		}
		if strings.HasSuffix(l, ","+dnsActionFromTarget) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d %s lines among %q, want one", n, dnsActionFromTarget, lines)
	}
}

func TestQueryFromTheRedirectTarget(t *testing.T) {
	thisHost(t, "192.0.2.1")
	ip := net.ParseIP

	for _, tc := range []struct {
		name          string
		target, doh   string
		client        net.IP
		fromTheTarget bool
	}{
		{"the LAN resolver itself", "192.0.2.5", "", ip("192.0.2.5"), true},
		{"the LAN resolver in the four bytes a packet carries", "192.0.2.5", "", ip("192.0.2.5").To4(), true},
		{"an IPv6 resolver itself", "2001:db8::5", "", ip("2001:db8::5"), true},
		{"a DoH server by address, asking itself", "", "https://192.0.2.5/dns-query", ip("192.0.2.5"), true},
		{"a DoH server by IPv6 address with a port", "", "https://[2001:db8::5]:8443/dns-query", ip("2001:db8::5"), true},
		{"both fields filled, the DoH server asking", "192.0.2.9", "https://192.0.2.5/dns-query", ip("192.0.2.5"), true},
		{"both fields filled, the ignored DNS server asking", "192.0.2.9", "https://192.0.2.5/dns-query", ip("192.0.2.9"), false},
		{"both fields filled, a DoH server by name, the ignored DNS server asking", "192.0.2.9", "https://dns.example/dns-query", ip("192.0.2.9"), false},
		{"a client of the LAN resolver", "192.0.2.5", "", ip("192.0.2.100"), false},
		{"the LAN resolver's other address", "2001:db8::5", "", ip("192.0.2.5"), false},
		{"a resolver on this host, asked from its own address", "192.0.2.1", "", ip("192.0.2.1"), false},
		{"a resolver on this host's loopback, asked over loopback", "127.0.0.1", "", ip("127.0.0.1"), false},
		{"a DoH server on this host by address", "", "https://192.0.2.1/dns-query", ip("192.0.2.1"), false},
		{"a DoH server by name", "", "https://dns.example/dns-query", ip("192.0.2.5"), false},
		{"a DoH URL that does not parse", "", "https://%zz/dns-query", ip("192.0.2.5"), false},
		{"a target that is not an address", "dns.example", "", ip("192.0.2.5"), false},
		{"no client address", "192.0.2.5", "", nil, false},
	} {
		if got := queryFromTarget(redirectSet(tc.target, tc.doh), tc.client); got != tc.fromTheTarget {
			t.Errorf("%s: queryFromTarget = %v, want %v", tc.name, got, tc.fromTheTarget)
		}
	}
}

func TestAQueryFromTheSetsOwnResolverIsForwardedUnchanged(t *testing.T) {
	thisHost(t)

	for _, tc := range []struct {
		name   string
		set    *config.SetConfig
		pkt    *pktInfo
		qtype  uint16
		ipv6On bool
	}{
		{"a LAN resolver", redirectSet("192.0.2.5", ""), lanQuery("192.0.2.5", "203.0.113.53"), dnsTypeA, false},
		{"a DoH server by address", redirectSet("", "https://192.0.2.5/dns-query"), lanQuery("192.0.2.5", "203.0.113.53"), dnsTypeA, false},
		{"an IPv6 LAN resolver", redirectSet("2001:db8::5", ""), lanQuery("2001:db8::5", "2001:db8:53::53"), dnsTypeAAAA, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := redirectWorker(t, tc.ipv6On, tc.set)
			lines := connectionLines(t)
			replies := watchReplies(t)
			q := newFakeQueue()

			w.processDnsPacket(&verdictCtx{id: 61, q: q}, tc.pkt, 40000, 53, dns.BuildQuery("www.example.com", 61, tc.qtype))

			if v := q.now(t); v != nfqueue.NfAccept {
				t.Fatalf("verdict %d, want the query forwarded unchanged", v)
			}
			fromTargetLoggedOnce(t, lines)
			noReply(t, replies, 30*time.Millisecond, "b4 answered a query its own resolver sent upstream")
		})
	}
}

func TestTheSetsOwnResolverStillGetsPinnedAnswers(t *testing.T) {
	thisHost(t)
	set := redirectSet("192.0.2.5", "")
	set.DNS.Pins = map[string][]string{"www.example.com": {"203.0.113.77"}}
	w := redirectWorker(t, false, set)
	replies := watchReplies(t)
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 64, q: q}, lanQuery("192.0.2.5", "203.0.113.53"), 40000, 53, dns.BuildQuery("www.example.com", 64, dnsTypeA))

	if v := q.now(t); v != nfqueue.NfDrop {
		t.Fatalf("verdict %d, want the pin to answer before the source is looked at", v)
	}
	onlyAddress(t, oneReply(t, replies), "203.0.113.77")
	noReply(t, replies, 30*time.Millisecond, "a pinned name was answered twice")

	upstream := tcpDNSUpstream(t)
	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.5"), Port: 40000}})
	}()
	_ = clientEnd.SetDeadline(time.Now().Add(5 * time.Second))
	if err := writeDNSTCPMessage(clientEnd, dns.BuildQuery("www.example.com", 67, dnsTypeA), time.Second); err != nil {
		t.Fatal(err)
	}
	resp, err := readDNSTCPMessage(clientEnd)
	if err != nil {
		t.Fatalf("the pinned TCP answer never came: %v", err)
	}
	onlyAddress(t, resp, "203.0.113.77")
	clientEnd.Close()
	<-handled
	select {
	case q := <-upstream.queries:
		t.Fatalf("a pinned name was forwarded over TCP as well: %x", q)
	default:
	}
}

func TestTheGuardLooksAtTheEscalatedSet(t *testing.T) {
	thisHost(t)
	plain := config.NewSetConfig()
	plain.Id, plain.Name, plain.Enabled = "plain", "plain", true
	plain.Targets.DomainsToMatch = []string{"example.com"}
	esc := redirectSet("192.0.2.5", "")
	esc.Id, esc.Name = "esc", "esc"
	esc.Targets.DomainsToMatch = []string{"other.org"}
	w := redirectWorker(t, false, &plain, esc)
	w.destState.SetEscalation("www.example.com", esc.Id, "test", 0)
	lines := connectionLines(t)
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 65, q: q}, lanQuery("192.0.2.5", "203.0.113.53"), 40000, 53, dns.BuildQuery("www.example.com", 65, dnsTypeA))

	if v := q.now(t); v != nfqueue.NfAccept {
		t.Fatalf("verdict %d, want the escalated set's resolver to have its own query forwarded unchanged", v)
	}
	fromTargetLoggedOnce(t, lines)

	upstream := tcpDNSUpstream(t)
	tcpFromTarget(t, w, "192.0.2.5", upstream, dns.BuildQuery("www.example.com", 66, dnsTypeA))
	fromTargetLoggedOnce(t, lines)
}

func TestEscalationDoesNotSendAResolverItsOwnQuery(t *testing.T) {
	thisHost(t)
	query := dns.BuildQuery("www.example.com", 62, dnsTypeA)
	client, server := net.ParseIP("192.0.2.100").To4(), net.ParseIP("203.0.113.53").To4()

	for _, next := range []*config.SetConfig{redirectSet("192.0.2.100", ""), redirectSet("", "https://192.0.2.100/dns-query")} {
		target := next.DNS.TargetDNS + next.DNS.DoHURL
		w := redirectWorker(t, false, next)
		replies := watchReplies(t)

		if w.answerViaSetInline(IPv4, w.getConfig(), next, "www.example.com", query, client, 40000, server) {
			t.Fatalf("%s: the escalated set answered through the resolver that asked", target)
		}
		if w.answerViaSet(&verdictCtx{id: 62, q: newFakeQueue()}, IPv4, w.getConfig(), next, "www.example.com", query, client, 40000, server) {
			t.Fatalf("%s: the escalated set answered the reply path through the resolver that asked", target)
		}
		if _, ok := w.dnsAnswerSource(next, client); ok {
			t.Fatalf("%s: the resolver that asked is no answer source for its own query", target)
		}
		if _, ok := w.dnsAnswerSource(next, net.ParseIP("192.0.2.101").To4()); !ok {
			t.Fatalf("%s: another client of the same set still gets the set's resolver", target)
		}
		noReply(t, replies, 30*time.Millisecond, "an escalated answer went to the resolver's own query")

		s := newDNSTCPServer(w, 0)
		t.Cleanup(s.cancel)
		if resp, err := s.answerVia(next, w.getConfig(), query, "www.example.com", client); err != errQueryFromTarget || resp != nil {
			t.Fatalf("%s: a TCP escalation through the resolver that asked must be refused, got %d bytes and %v", target, len(resp), err)
		}
	}
}

type remoteAddrConn struct {
	net.Conn
	remote net.Addr
}

func (c remoteAddrConn) RemoteAddr() net.Addr { return c.remote }

type dnsUpstream struct {
	addr    *net.TCPAddr
	queries chan []byte
	reply   []byte
}

func tcpDNSUpstream(t *testing.T) *dnsUpstream {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	u := &dnsUpstream{
		addr:    ln.Addr().(*net.TCPAddr),
		queries: make(chan []byte, 4),
		reply:   buildDNSResponse(0, "www.example.com", []net.IP{net.ParseIP("203.0.113.80")}),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if query, err := readDNSTCPMessage(conn); err == nil {
				u.queries <- query
				reply := append([]byte(nil), u.reply...)
				copy(reply[:2], query[:2])
				_ = writeDNSTCPMessage(conn, reply, time.Second)
				_, _ = io.Copy(io.Discard, conn)
			}
			conn.Close()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		<-done
	})

	prevDst, prevDial := dnsTCPOriginalDst, dialDNSTCPUpstream
	dnsTCPOriginalDst = func(net.Conn) (net.IP, int, error) { return u.addr.IP, u.addr.Port, nil }
	dialDNSTCPUpstream = func(ctx context.Context, _ *config.Config, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}
	t.Cleanup(func() { dnsTCPOriginalDst, dialDNSTCPUpstream = prevDst, prevDial })
	return u
}

func tcpFromTarget(t *testing.T, w *Worker, from string, upstream *dnsUpstream, query []byte) {
	t.Helper()
	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP(from), Port: 40000}})
	}()

	_ = clientEnd.SetDeadline(time.Now().Add(5 * time.Second))
	if err := writeDNSTCPMessage(clientEnd, query, time.Second); err != nil {
		t.Fatal(err)
	}
	resp, err := readDNSTCPMessage(clientEnd)
	if err != nil {
		t.Fatalf("no answer came back through the forwarded connection: %v", err)
	}
	select {
	case got := <-upstream.queries:
		if !bytes.Equal(got, query) {
			t.Fatalf("the upstream got %x, want the query unchanged %x", got, query)
		}
	case <-time.After(time.Second):
		t.Fatal("the query never reached the address it was sent to")
	}
	if !bytes.Equal(resp[2:], upstream.reply[2:]) || !bytes.Equal(resp[:2], query[:2]) {
		t.Fatalf("the client got %x, want the upstream's own answer", resp)
	}
	clientEnd.Close()
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the forwarded connection was never closed")
	}
}

func TestADNSOverTCPQueryFromTheSetsOwnResolverIsForwardedUnchanged(t *testing.T) {
	thisHost(t)

	for _, set := range []*config.SetConfig{redirectSet("192.0.2.5", ""), redirectSet("", "https://192.0.2.5/dns-query")} {
		w := redirectWorker(t, false, set)
		lines := connectionLines(t)
		upstream := tcpDNSUpstream(t)

		tcpFromTarget(t, w, "192.0.2.5", upstream, dns.BuildQuery("www.example.com", 63, dnsTypeA))
		fromTargetLoggedOnce(t, lines)
	}
}

func TestAReplyIsNotCountedTwiceWhileARedirectWaitsForIt(t *testing.T) {
	cfg, _, backup := passiveDNSPair(t, 1)
	backup.DNS.Pins = map[string][]string{"youtube.com": {"142.250.74.14"}}
	w := passiveDNSWorker(t, cfg)
	nx := dns.BuildBlockResponse(dns.BuildQuery("youtube.com", 0x1236, dnsTypeA))

	done := beginDNSRedirect(dns.BuildQuery("YouTube.com", 7, dnsTypeA))
	nested := beginDNSRedirect(dns.BuildQuery("youtube.com", 8, dnsTypeA))
	if vc := feedDNSResponse(t, w, nx); vc.verdict != engine.VerdictAccept {
		t.Fatalf("verdict = %v: a failed reply was counted while b4's own redirect of the name was still waiting for the same answer", vc.verdict)
	}
	if !dnsRedirectInFlight("youtube.com", dnsTypeA) || dnsRedirectInFlight("youtube.com", dnsTypeAAAA) {
		t.Fatal("a redirect in flight is tracked per name and record type")
	}
	aaaa := beginDNSRedirect(dns.BuildQuery("youtube.com", 9, dnsTypeAAAA))
	if !dnsRedirectInFlight("youtube.com", dnsTypeAAAA) {
		t.Fatal("an AAAA redirect in flight is tracked under its own record type")
	}
	aaaa()
	done()
	if !dnsRedirectInFlight("youtube.com", dnsTypeA) {
		t.Fatal("the name stays tracked while another redirect for it still waits")
	}
	nested()
	if dnsRedirectInFlight("youtube.com", dnsTypeA) {
		t.Fatal("the name is released once no redirect for it waits")
	}
	if _, _, ok := w.destState.GetEscalation("youtube.com"); ok {
		t.Fatal("the skipped reply escalated the name")
	}

	if vc := feedDNSResponse(t, w, nx); vc.verdict != engine.VerdictDrop {
		t.Fatalf("verdict = %v: with no redirect waiting, a failed reply counts again", vc.verdict)
	}
	if escId, _, ok := w.destState.GetEscalation("youtube.com"); !ok || escId != backup.Id {
		t.Fatalf("escalation = %q ok=%v, want %q", escId, ok, backup.Id)
	}
}
