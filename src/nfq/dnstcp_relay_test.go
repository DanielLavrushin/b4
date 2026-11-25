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

func plainTCPUpstream(t *testing.T, closeAfterAnswer bool) *plainTCPResolver {
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
					reply := answerWith(plainAnswer)(query)
					if reply == nil {
						reply = append([]byte(nil), query...)
						reply[2] |= 0x80
					}
					if writeDNSTCPMessage(conn, reply, time.Second) != nil || closeAfterAnswer {
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

	_ = clientEnd.SetDeadline(time.Now().Add(10 * time.Second))
	send := func(q []byte) {
		if err := writeDNSTCPMessage(clientEnd, q, 5*time.Second); err != nil {
			t.Errorf("write: %v", err)
		}
	}
	if pipelined {
		go func() {
			for _, q := range queries {
				send(q)
			}
		}()
	}
	var answers [][]byte
	for i, q := range queries {
		if !pipelined {
			send(q)
		}
		resp, err := readDNSTCPMessage(clientEnd)
		if err != nil {
			t.Fatalf("query %d: no answer on the same connection: %v", i, err)
		}
		if !bytes.Equal(resp[:2], q[:2]) {
			t.Fatalf("query %d: answer id %#x, want %#x", i, resp[:2], q[:2])
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

func answeredBy(t *testing.T, resp []byte) string {
	t.Helper()
	if rcode, _ := dns.ResponseRcode(resp); rcode == dns.RcodeNXDomain {
		return "NXDOMAIN"
	}
	ips := dns.ParseResponseIPs(resp)
	switch {
	case len(ips) == 0 && resp[2]&0x80 != 0:
		return "plain"
	case len(ips) == 1 && ips[0].Equal(net.ParseIP(plainAnswer)):
		return "plain"
	case len(ips) == 1 && ips[0].Equal(net.ParseIP(setAnswer)):
		return "set"
	}
	return "other"
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
			plain := plainTCPUpstream(t, false)
			first := tc.first(t)

			answers := tcpSession(t, w, tc.from, [][]byte{
				first,
				dns.BuildQuery("a.secret.example", 2, dnsTypeA),
				dns.BuildQuery("blocked.example", 3, dnsTypeA),
			}, false)

			for i, want := range []string{"plain", "set", "NXDOMAIN"} {
				if got := answeredBy(t, answers[i]); got != want {
					t.Fatalf("answer %d came from %s, want %s", i, got, want)
				}
			}
			got := plain.received()
			if len(got) != 1 || !bytes.Equal(got[0], first) {
				t.Fatalf("the asked resolver got %x, want only the first query unchanged %x", got, first)
			}
		})
	}
}

func TestDNSOverTCPRedialsAResolverThatClosesAfterEachAnswer(t *testing.T) {
	w := relayWorker(t)
	plain := plainTCPUpstream(t, true)

	answers := tcpSession(t, w, "192.0.2.10", [][]byte{
		dns.BuildQuery("www.other.test", 1, dnsTypeA),
		dns.BuildQuery("www.another.test", 2, dnsTypeA),
		dns.BuildQuery("www.third.test", 3, dnsTypeA),
	}, false)
	for i, resp := range answers {
		if got := answeredBy(t, resp); got != "plain" {
			t.Fatalf("answer %d came from %s, want plain", i, got)
		}
	}
	if n := plain.conns.Load(); n != 3 {
		t.Fatalf("%d upstream connections, want one per query", n)
	}
}

func TestDNSOverTCPAnswersPipelinedQueriesInOrder(t *testing.T) {
	w := relayWorker(t)
	plainTCPUpstream(t, false)

	answers := tcpSession(t, w, "192.0.2.10", [][]byte{
		dns.BuildQuery("www.other.test", 1, dnsTypeA),
		dns.BuildQuery("a.secret.example", 2, dnsTypeA),
		dns.BuildQuery("www.another.test", 3, dnsTypeA),
		dns.BuildQuery("blocked.example", 4, dnsTypeA),
	}, true)
	for i, want := range []string{"plain", "set", "plain", "NXDOMAIN"} {
		if got := answeredBy(t, answers[i]); got != want {
			t.Fatalf("answer %d came from %s, want %s", i, got, want)
		}
	}
}
