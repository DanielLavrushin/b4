package nfq

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

const (
	plainAnswer = "198.51.100.1"
	setAnswer   = "203.0.113.7"
)

type plainTCPResolver struct {
	conns   atomic.Int32
	mu      sync.Mutex
	queries [][]byte
}

func (r *plainTCPResolver) received() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]byte(nil), r.queries...)
}

func plainEcho(query []byte) []byte {
	echo := append([]byte(nil), query...)
	echo[2] |= 0x80
	return echo
}

func plainReply(query []byte) []byte {
	if reply := answerWith(plainAnswer)(query); reply != nil {
		return reply
	}
	return plainEcho(query)
}

func plainTCPUpstream(t *testing.T, reply func([]byte) []byte, closeAfterAnswer bool) *plainTCPResolver {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	r := &plainTCPResolver{}
	var wg sync.WaitGroup
	var open []net.Conn
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			r.conns.Add(1)
			r.mu.Lock()
			open = append(open, conn)
			r.mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				for {
					query, err := readDNSTCPMessage(conn)
					if err != nil {
						return
					}
					r.mu.Lock()
					r.queries = append(r.queries, query)
					r.mu.Unlock()
					answer := reply(query)
					if answer == nil {
						continue
					}
					if writeDNSTCPMessage(conn, answer, time.Second) != nil || closeAfterAnswer {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		r.mu.Lock()
		for _, c := range open {
			c.Close()
		}
		r.mu.Unlock()
		wg.Wait()
	})

	prevDst, prevDial := dnsTCPOriginalDst, dialDNSTCPUpstream
	dnsTCPOriginalDst = func(net.Conn) (net.IP, int, error) { return addr.IP, addr.Port, nil }
	dialDNSTCPUpstream = func(ctx context.Context, _ *config.Config, a string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", a)
	}
	t.Cleanup(func() { dnsTCPOriginalDst, dialDNSTCPUpstream = prevDst, prevDial })
	return r
}

func relaySets() []*config.SetConfig {
	secret := config.NewSetConfig()
	secret.Id, secret.Name, secret.Enabled = "secret", "secret", true
	secret.Targets.DomainsToMatch = []string{"secret.example"}
	secret.DNS.Enabled = true
	secret.DNS.Strict = true

	blocked := config.NewSetConfig()
	blocked.Id, blocked.Name, blocked.Enabled = "blocked", "blocked", true
	blocked.Targets.DomainsToMatch = []string{"blocked.example"}
	blocked.Routing.Enabled = true
	blocked.Routing.Mode = config.RoutingModeBlock

	bare := config.NewSetConfig()
	bare.Id, bare.Name, bare.Enabled = "bare", "bare", true
	bare.Targets.DomainsToMatch = []string{"bare.example"}

	return []*config.SetConfig{&secret, &blocked, &bare, redirectSet("192.0.2.5", "")}
}

func relayWorker(t *testing.T) *Worker {
	t.Helper()
	thisHost(t)
	sets := relaySets()
	w := redirectWorker(t, false, sets...)
	sets[0].DNS.DoHURL = copyDoHServer(t, w, nil, answerWith(setAnswer)).url
	return w
}

func tcpSession(t *testing.T, w *Worker, from string, queries [][]byte, pipelined bool) [][]byte {
	t.Helper()
	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP(from), Port: 40000}})
	}()
	defer clientEnd.Close()

	_ = clientEnd.SetDeadline(time.Now().Add(10 * time.Second))
	if pipelined {
		sent := make(chan error, 1)
		go func() {
			var err error
			for _, q := range queries {
				if err = writeDNSTCPMessage(clientEnd, q, 5*time.Second); err != nil {
					break
				}
			}
			sent <- err
		}()
		defer func() {
			clientEnd.Close()
			if err := <-sent; err != nil && !t.Failed() {
				t.Errorf("write: %v", err)
			}
		}()
	}

	answers := make([][]byte, len(queries))
	for i, q := range queries {
		if !pipelined {
			if err := writeDNSTCPMessage(clientEnd, q, 5*time.Second); err != nil {
				t.Fatalf("query %d: write: %v", i, err)
			}
		}
		resp, err := readDNSTCPMessage(clientEnd)
		if err != nil {
			t.Fatalf("answer %d: none on the same connection: %v", i, err)
		}
		id := binary.BigEndian.Uint16(resp[:2])
		at := slices.IndexFunc(queries, func(q []byte) bool { return binary.BigEndian.Uint16(q[:2]) == id })
		if at < 0 || answers[at] != nil || (!pipelined && at != i) {
			t.Fatalf("answer %d carries id %#x, which answers no query still waiting", i, id)
		}
		answers[at] = resp
	}
	clientEnd.Close()
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection handler never returned")
	}
	return answers
}

func answeredBy(t *testing.T, query, resp []byte) string {
	t.Helper()
	if bytes.Equal(resp, plainEcho(query)) {
		return "plain"
	}
	switch rcode, _ := dns.ResponseRcode(resp); rcode {
	case dns.RcodeNXDomain:
		return "NXDOMAIN"
	case dns.RcodeServFail:
		return "SERVFAIL"
	}
	ips := dns.ParseResponseIPs(resp)
	switch {
	case len(ips) == 1 && ips[0].Equal(net.ParseIP(plainAnswer)):
		return "plain"
	case len(ips) == 1 && ips[0].Equal(net.ParseIP(setAnswer)):
		return "set"
	}
	return "other"
}

func expectAnswers(t *testing.T, queries, answers [][]byte, want ...string) {
	t.Helper()
	for i, w := range want {
		if got := answeredBy(t, queries[i], answers[i]); got != w {
			t.Fatalf("answer %d came from %s, want %s", i, got, w)
		}
	}
}

func rootDNSKEYQuery(t *testing.T) []byte {
	t.Helper()
	q := make([]byte, 12, 17)
	binary.BigEndian.PutUint16(q[0:2], 1)
	binary.BigEndian.PutUint16(q[2:4], 0x0100)
	binary.BigEndian.PutUint16(q[4:6], 1)
	q = append(q, 0, 0, 48, 0, 1)
	if _, ok := dns.ParseQueryDomain(q); ok {
		t.Fatal("the root name was expected to be unreadable")
	}
	return q
}

func TestDNSOverTCPForwardsEachUnchangedQueryOnItsOwn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		from  string
		first func(t *testing.T) []byte
	}{
		{"a name in no set", "192.0.2.10", func(*testing.T) []byte { return dns.BuildQuery("www.other.test", 1, dnsTypeA) }},
		{"a name b4 cannot read", "192.0.2.10", rootDNSKEYQuery},
		{"a set without a resolver", "192.0.2.10", func(*testing.T) []byte { return dns.BuildQuery("www.bare.example", 1, dnsTypeA) }},
		{"a query from the set's own resolver", "192.0.2.5", func(*testing.T) []byte { return dns.BuildQuery("www.example.com", 1, dnsTypeA) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := relayWorker(t)
			plain := plainTCPUpstream(t, plainReply, false)
			queries := [][]byte{
				tc.first(t),
				dns.BuildQuery("a.secret.example", 2, dnsTypeA),
				dns.BuildQuery("blocked.example", 3, dnsTypeA),
			}

			answers := tcpSession(t, w, tc.from, queries, false)

			expectAnswers(t, queries, answers, "plain", "set", "NXDOMAIN")
			got := plain.received()
			if len(got) != 1 || !bytes.Equal(got[0], queries[0]) {
				t.Fatalf("the asked resolver got %x, want only the first query unchanged %x", got, queries[0])
			}
		})
	}
}

func TestDNSOverTCPRedialsAResolverThatClosesAfterEachAnswer(t *testing.T) {
	w := relayWorker(t)
	plain := plainTCPUpstream(t, plainReply, true)
	queries := [][]byte{
		dns.BuildQuery("www.other.test", 1, dnsTypeA),
		dns.BuildQuery("www.another.test", 2, dnsTypeA),
		dns.BuildQuery("www.third.test", 3, dnsTypeA),
	}

	answers := tcpSession(t, w, "192.0.2.10", queries, false)

	expectAnswers(t, queries, answers, "plain", "plain", "plain")
	if n := plain.conns.Load(); n != 3 {
		t.Fatalf("%d upstream connections, want one per query", n)
	}
}

func TestDNSOverTCPAnswersPipelinedQueries(t *testing.T) {
	w := relayWorker(t)
	plain := plainTCPUpstream(t, plainReply, false)
	queries := [][]byte{
		dns.BuildQuery("www.other.test", 1, dnsTypeA),
		dns.BuildQuery("a.secret.example", 2, dnsTypeA),
		dns.BuildQuery("www.another.test", 3, dnsTypeA),
		dns.BuildQuery("blocked.example", 4, dnsTypeA),
	}

	answers := tcpSession(t, w, "192.0.2.10", queries, true)

	expectAnswers(t, queries, answers, "plain", "set", "plain", "NXDOMAIN")
	got := plain.received()
	if len(got) != 2 || !slices.ContainsFunc(got, func(q []byte) bool { return bytes.Equal(q, queries[0]) }) ||
		!slices.ContainsFunc(got, func(q []byte) bool { return bytes.Equal(q, queries[2]) }) {
		t.Fatalf("the asked resolver got %x, want the two names in no set unchanged", got)
	}
}

func TestDNSOverTCPAnswersAForwardedQueryWithoutWaitingForASlowOne(t *testing.T) {
	w := relayWorker(t)
	slow := dns.BuildQuery("www.slow.test", 1, dnsTypeA)
	fast := dns.BuildQuery("www.fast.test", 2, dnsTypeA)
	release := make(chan struct{})
	free := sync.OnceFunc(func() { close(release) })
	t.Cleanup(free)
	plainTCPUpstream(t, func(q []byte) []byte {
		if bytes.Equal(q, slow) {
			select {
			case <-release:
			case <-time.After(3 * time.Second):
			}
		}
		return plainReply(q)
	}, false)
	s := newDNSTCPServer(w, 0)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 40000}})
	}()
	sent := make(chan error, 1)
	t.Cleanup(func() {
		s.cancel()
		clientEnd.Close()
		<-sent
		<-handled
	})
	_ = clientEnd.SetDeadline(time.Now().Add(10 * time.Second))
	go func() {
		err := writeDNSTCPMessage(clientEnd, slow, 5*time.Second)
		if err == nil {
			err = writeDNSTCPMessage(clientEnd, fast, 5*time.Second)
		}
		sent <- err
	}()

	var order []uint16
	for i := range 2 {
		resp, err := readDNSTCPMessage(clientEnd)
		if err != nil {
			t.Fatalf("answer %d: none on the same connection: %v", i, err)
		}
		order = append(order, binary.BigEndian.Uint16(resp[:2]))
		free()
	}
	if !reflect.DeepEqual(order, []uint16{2, 1}) {
		t.Fatalf("answers came in the order %v, want the fast query answered while the slow one is still with the resolver", order)
	}
}

func TestDNSOverTCPAnswersSERVFAILWhenTheResolverStaysSilent(t *testing.T) {
	w := relayWorker(t)
	w.getConfig().System.DNS.TCPIOSec = 1
	silent := dns.BuildQuery("www.silent.test", 2, dnsTypeA)
	plain := plainTCPUpstream(t, func(q []byte) []byte {
		if bytes.Equal(q, silent) {
			return nil
		}
		return plainReply(q)
	}, false)
	queries := [][]byte{
		dns.BuildQuery("www.other.test", 1, dnsTypeA),
		silent,
		dns.BuildQuery("www.after.test", 3, dnsTypeA),
	}

	answers := tcpSession(t, w, "192.0.2.10", queries, false)

	expectAnswers(t, queries, answers, "plain", "SERVFAIL", "plain")
	asked := 0
	for _, q := range plain.received() {
		if bytes.Equal(q, silent) {
			asked++
		}
	}
	if asked != 1 {
		t.Fatalf("the resolver got the unanswered query %d times, want once", asked)
	}
}

func TestDNSOverTCPStopEndsAForwardedQueryAtOnce(t *testing.T) {
	w := relayWorker(t)
	w.getConfig().System.DNS.TCPIOSec = 5
	plain := plainTCPUpstream(t, func([]byte) []byte { return nil }, false)
	s := newDNSTCPServer(w, 0)
	clientEnd, serverEnd := net.Pipe()
	defer clientEnd.Close()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 40000}})
	}()
	if err := writeDNSTCPMessage(clientEnd, dns.BuildQuery("www.silent.test", 1, dnsTypeA), time.Second); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, func() bool { return len(plain.received()) == 1 }) {
		t.Fatal("the query never reached the resolver")
	}

	s.cancel()
	select {
	case <-handled:
	case <-time.After(time.Second):
		t.Fatal("the connection handler still waits for the resolver after the server stopped")
	}
}

func TestDNSOverTCPForwardedAnswerTeachesItsSet(t *testing.T) {
	names := withDNSNames(t, true)
	w, _ := dscpHookWorker(t, dscpHookSet("bare", "bare.example", true, true))
	learned := stubHealLearning(t, nil)
	plainTCPUpstream(t, plainReply, false)

	dscpWithin(t, dscpTCPAsk(t, w, dns.BuildQuery("www.bare.example", 1, dnsTypeA)), "the set's name got no answer")
	dscpWithin(t, dscpTCPAsk(t, w, dns.BuildQuery("www.other.test", 2, dnsTypeA)), "the name in no set got no answer")

	for _, want := range []string{"dscp " + plainAnswer, "route " + plainAnswer} {
		if !slices.Contains(learned(), want) {
			t.Errorf("%s was never learned, learned %v", want, learned())
		}
	}
	if _, host, ok := w.hostHints.Lookup(rigDevice, plainAnswer); !ok || host != "www.bare.example" {
		t.Errorf("the host hint for %s names %q, want www.bare.example", plainAnswer, host)
	}
	if got := names.Lookup(net.ParseIP(rigDevice), net.ParseIP(plainAnswer)).Own; !slices.Contains(got, "www.other.test") {
		t.Errorf("the name in no set was not recorded for its address, got %v", got)
	}
}

func TestDNSOverTCPForwardedAnswerDropsIPv6WhileIPv6IsOff(t *testing.T) {
	w := relayWorker(t)
	plainTCPUpstream(t, answerWith("2001:db8::7"), false)
	queries := [][]byte{dns.BuildQuery("www.bare.example", 1, dnsTypeAAAA)}

	answers := tcpSession(t, w, "192.0.2.10", queries, false)

	if ips := dns.ParseResponseIPs(answers[0]); len(ips) != 0 {
		t.Fatalf("the answer still carries %v while IPv6 support is off", ips)
	}
}

func TestDNSOverTCPAnswersPassedOnForASetDropIPv6WhileIPv6IsOff(t *testing.T) {
	for _, tc := range []struct {
		name   string
		worker func(t *testing.T) *Worker
		from   string
		query  []byte
	}{
		{"a query from the set's own resolver", relayWorker, "192.0.2.5", dns.BuildQuery("www.example.com", 1, dnsTypeAAAA)},
		{"a lookup of the set's DoH host", func(t *testing.T) *Worker {
			thisHost(t)
			return redirectWorker(t, false, strictSet(dohByName("all", "https://dns.invalid/dns-query", "regexp:.*")))
		}, "192.0.2.1", dns.BuildQuery("dns.invalid", 1, dnsTypeAAAA)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.worker(t)
			plain := plainTCPUpstream(t, answerWith("2001:db8::7"), false)

			answers := tcpSession(t, w, tc.from, [][]byte{tc.query}, false)

			if got := plain.received(); len(got) != 1 || !bytes.Equal(got[0], tc.query) {
				t.Fatalf("the asked resolver got %x, want the query unchanged", got)
			}
			if ips := dns.ParseResponseIPs(answers[0]); len(ips) != 0 {
				t.Fatalf("the answer still carries %v while IPv6 support is off, as an answer over UDP would not", ips)
			}
		})
	}
}
