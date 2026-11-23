package nfq

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/florianl/go-nfqueue"
)

func dohByName(id, doh string, names ...string) *config.SetConfig {
	set := redirectSet("", doh)
	set.Id, set.Name = id, id
	set.Targets.DomainsToMatch = names
	return set
}

func strictSet(set *config.SetConfig) *config.SetConfig {
	set.DNS.Strict = true
	return set
}

func dohHostLoggedOnce(t *testing.T, ch chan string, why string) {
	t.Helper()
	lines := drainLines(ch)
	for _, l := range lines {
		if strings.Contains(l, ","+dnsActionForwardPrefix) || strings.Contains(l, ","+dnsActionDoHPrefix) {
			t.Errorf("%s: a redirect was logged for a lookup that was forwarded unchanged: %s", why, l)
		}
	}
	if n := countAction(lines, dnsActionDoHHost); n != 1 {
		t.Errorf("%s: %d %s lines among %q, want one", why, n, dnsActionDoHHost, lines)
	}
}

func TestALookupTheSetsResolversNeedIsForwardedUnchanged(t *testing.T) {
	thisHost(t)
	const catchAll = "regexp:.*"
	first := dohByName("first", "https://192.0.2.5/dns-query", catchAll)
	first.DNS.Upstreams = []string{"https://b.example/dns-query"}
	backup := dohByName("backup", "https://b.example/dns-query", "other.example")
	plain := func(name string) *config.SetConfig {
		set := config.NewSetConfig()
		set.Id, set.Name, set.Enabled = "plain", "plain", true
		set.Targets.DomainsToMatch = []string{name}
		return &set
	}

	for _, tc := range []struct {
		name      string
		sets      []*config.SetConfig
		asked     string
		escalated string
	}{
		{"a set that targets every name", []*config.SetConfig{dohByName("all", "https://dns.example/dns-query", catchAll)}, "dns.example", ""},
		{"a set that lists its resolver's domain", []*config.SetConfig{dohByName("own", "https://dns.example:8443/dns-query", "www.example.com", "example")}, "DNS.example", ""},
		{"a set that fails closed", []*config.SetConfig{strictSet(dohByName("strict", "https://dns.example/dns-query", catchAll))}, "dns.example", ""},
		{"a later resolver of the list", []*config.SetConfig{first}, "b.example", ""},
		{"two sets that list each other's resolver, the first", []*config.SetConfig{
			dohByName("a", "https://a.example/dns-query", "b.example"),
			dohByName("b", "https://b.example/dns-query", "a.example"),
		}, "b.example", ""},
		{"two sets that list each other's resolver, the second", []*config.SetConfig{
			strictSet(dohByName("a", "https://a.example/dns-query", "b.example")),
			strictSet(dohByName("b", "https://b.example/dns-query", "a.example")),
		}, "a.example", ""},
		{"three sets in a ring", []*config.SetConfig{
			dohByName("a", "https://a.example/dns-query", "c.example"),
			dohByName("b", "https://b.example/dns-query", "a.example"),
			dohByName("c", "https://c.example/dns-query", "b.example"),
		}, "c.example", ""},
		{"the set the name escalated to", []*config.SetConfig{plain("b.example"), backup}, "b.example", "b.example"},
		{"the set a resolver's name escalated to", []*config.SetConfig{
			dohByName("a", "https://a.example/dns-query", "b.example"),
			plain("a.example"),
			backup,
		}, "b.example", "a.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := redirectWorker(t, false, tc.sets...)
			if tc.escalated != "" {
				w.destState.SetEscalation(tc.escalated, backup.Id, "test", 0)
			}
			lines := connectionLines(t)
			replies := watchReplies(t)
			q := newFakeQueue()

			w.processDnsPacket(&verdictCtx{id: 81, q: q}, lanQuery("192.0.2.1", "203.0.113.53"), 40000, 53, dns.BuildQuery(tc.asked, 81, dnsTypeA))

			if v := q.now(t); v != nfqueue.NfAccept {
				t.Fatalf("verdict %d, want the lookup forwarded unchanged", v)
			}
			dohHostLoggedOnce(t, lines, "UDP")
			noReply(t, replies, 30*time.Millisecond, "b4 answered a lookup its own resolver waits for")

			upstream := tcpDNSUpstream(t)
			tcpFromTarget(t, w, "192.0.2.1", upstream, dns.BuildQuery(tc.asked, 82, dnsTypeA))
			dohHostLoggedOnce(t, lines, "TCP")
		})
	}
}

func TestALookupNoResolverWaitsForIsStillRedirected(t *testing.T) {
	thisHost(t)
	disabled := dohByName("disabled", "https://dns.example/dns-query", "unused.example")
	disabled.Enabled = false
	off := dohByName("off", "https://dns.example/dns-query", "unused.example")
	off.DNS.Enabled = false
	pinned := dohByName("b", "https://b.example/dns-query", "a.example")
	pinned.DNS.Pins = map[string][]string{"a.example": {"203.0.113.9"}}
	blocked := dohByName("b", "https://b.example/dns-query", "a.example")
	blocked.Routing.Enabled, blocked.Routing.Mode = true, config.RoutingModeBlock
	passing := dohByName("b", "https://b.example/dns-query", "a.example")
	passing.DNS.Upstreams = []string{"https://a.example/dns-query"}

	for _, tc := range []struct {
		name  string
		sets  []*config.SetConfig
		asked string
	}{
		{"another set's resolver", []*config.SetConfig{
			dohByName("all", "https://dns.example/dns-query", "regexp:.*"),
			dohByName("geo", "https://geo.example/dns-query", "chat.example"),
		}, "geo.example"},
		{"a name below the resolver's", []*config.SetConfig{dohByName("all", "https://dns.example/dns-query", "regexp:.*")}, "www.dns.example"},
		{"a name the resolver's ends with", []*config.SetConfig{dohByName("all", "https://dns.example/dns-query", "regexp:.*")}, "example"},
		{"the resolver of a disabled set", []*config.SetConfig{dohByName("all", "https://other.example/dns-query", "regexp:.*"), disabled}, "dns.example"},
		{"the resolver of a set with the redirect off", []*config.SetConfig{dohByName("all", "https://other.example/dns-query", "regexp:.*"), off}, "dns.example"},
		{"a resolver another set reaches by address", []*config.SetConfig{
			dohByName("a", "https://a.example/dns-query", "b.example"),
			dohByName("b", "https://192.0.2.5/dns-query", "a.example"),
			dohByName("c", "https://b.example/dns-query", "c.example"),
		}, "b.example"},
		{"a resolver whose own name is pinned", []*config.SetConfig{dohByName("a", "https://a.example/dns-query", "b.example"), pinned}, "b.example"},
		{"a resolver whose own name is blocked", []*config.SetConfig{dohByName("a", "https://a.example/dns-query", "b.example"), blocked}, "b.example"},
		{"a resolver whose own name is forwarded unchanged", []*config.SetConfig{dohByName("a", "https://a.example/dns-query", "b.example"), passing}, "b.example"},
		{"a resolver past two sets that list each other's", []*config.SetConfig{
			dohByName("x", "https://x.example/dns-query", "unused.example"),
			dohByName("s", "https://a.example/dns-query", "x.example"),
			dohByName("a", "https://b.example/dns-query", "a.example"),
			dohByName("b", "https://a.example/dns-query", "b.example"),
		}, "x.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := redirectWorker(t, false, tc.sets...)
			lines := connectionLines(t)
			q := newFakeQueue()

			w.processDnsPacket(&verdictCtx{id: 83, q: q}, lanQuery("192.0.2.1", "203.0.113.53"), 40000, 53, dns.BuildQuery(tc.asked, 83, dnsTypeA))

			if v := q.now(t); v != nfqueue.NfDrop {
				t.Fatalf("verdict %d, want the set's resolver to answer", v)
			}
			got := drainLines(lines)
			if countAction(got, dnsActionDoHHost) != 0 {
				t.Fatalf("rows %q, want the lookup redirected", got)
			}
		})
	}
}

func TestTheSetsDoHHostStillGetsPinnedAnswers(t *testing.T) {
	thisHost(t)
	set := dohByName("all", "https://dns.example/dns-query", "regexp:.*")
	set.DNS.Pins = map[string][]string{"dns.example": {"203.0.113.77"}}
	w := redirectWorker(t, false, set)
	lines := connectionLines(t)
	replies := watchReplies(t)
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 84, q: q}, lanQuery("192.0.2.1", "203.0.113.53"), 40000, 53, dns.BuildQuery("dns.example", 84, dnsTypeA))

	if v := q.now(t); v != nfqueue.NfDrop {
		t.Fatalf("verdict %d, want the pin to answer first", v)
	}
	onlyAddress(t, oneReply(t, replies), "203.0.113.77")
	if got := drainLines(lines); countAction(got, dnsActionDoHHost) != 0 || countAction(got, dnsActionPin) != 1 {
		t.Fatalf("rows %q, want the pin alone", got)
	}
}

func TestTheSetsDoHHostStaysBlocked(t *testing.T) {
	thisHost(t)
	set := dohByName("block", "https://dns.example/dns-query", "dns.example")
	set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeBlock
	w := redirectWorker(t, false, set)
	lines := connectionLines(t)
	replies := watchReplies(t)
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 85, q: q}, lanQuery("192.0.2.1", "203.0.113.53"), 40000, 53, dns.BuildQuery("dns.example", 85, dnsTypeA))

	if v := q.now(t); v != nfqueue.NfDrop {
		t.Fatalf("verdict %d, want the block to answer first", v)
	}
	if rcode, _ := dns.ResponseRcode(oneReply(t, replies)); rcode != dns.RcodeNXDomain {
		t.Fatalf("rcode %d, want NXDOMAIN", rcode)
	}
	if got := drainLines(lines); countAction(got, dnsActionDoHHost) != 0 || countAction(got, dnsActionSinkhole) != 1 {
		t.Fatalf("rows %q, want the sinkhole alone", got)
	}
}

func TestEscalationDoesNotSendAResolverItsOwnHostName(t *testing.T) {
	thisHost(t)
	next := dohByName("next", "https://dns.example/dns-query", "other.example")
	w := redirectWorker(t, false, next)
	replies := watchReplies(t)
	query := dns.BuildQuery("dns.example", 86, dnsTypeA)
	client, server := net.ParseIP("192.0.2.100").To4(), net.ParseIP("203.0.113.53").To4()

	if w.answerViaSetInline(IPv4, w.getConfig(), next, "dns.example", query, client, 40000, server, false) {
		t.Fatal("the escalated set answered through a resolver that waits for this very name")
	}
	if w.answerViaSet(&verdictCtx{id: 86, q: newFakeQueue()}, IPv4, w.getConfig(), next, "dns.example", query, client, 40000, server) {
		t.Fatal("the escalated set answered the reply path through a resolver that waits for this very name")
	}
	if len(w.answeringServers(w.getConfig(), next, "other.example", client, nil)) != 1 {
		t.Fatal("another name of the same set still gets the set's resolver")
	}
	noReply(t, replies, 30*time.Millisecond, "an escalated answer was sent for the resolver's own host name")

	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	if resp, err := s.answerVia(next, w.getConfig(), query, "dns.example", client); err != errDoHHost || resp != nil {
		t.Fatalf("a TCP escalation through a resolver that waits for this very name must be refused, got %d bytes and %v", len(resp), err)
	}
}

func TestACopiedLookupTheSetsResolversNeedIsNotHeld(t *testing.T) {
	injectAfter(t, time.Minute)
	set := strictSet(dohByName("all", "https://dns.example/dns-query", "regexp:.*"))
	r := newCopyRig(t, nil, set)
	lines := connectionLines(t)
	replies := watchReplies(t)

	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("dns.example", 0x1a1, dnsTypeA)))
	dohHostLoggedOnce(t, lines, "copy")

	q := newFakeModQueue()
	raw := replyRaw(buildDNSResponse(0x1a1, "dns.example", resolverIPs), rigRouter, rigDevice, 40000)
	r.reply(q, raw)
	passedUnchanged(t, q.now(t), "the reply b4's own lookup waits for")
	noReply(t, replies, 30*time.Millisecond, "b4 answered a lookup its own resolver waits for")
}

func plainDNSTCPDial(ctx context.Context, _ *config.Config, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

func keptTCPUpstream(t *testing.T) chan string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	asked := make(chan string, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			for {
				query, err := readDNSTCPMessage(conn)
				if err != nil {
					break
				}
				name, _ := dns.ParseQueryDomain(query)
				asked <- name
				if writeDNSTCPMessage(conn, dns.BuildAnswerFromIPs(query, 60, []net.IP{net.ParseIP("203.0.113.80")}), time.Second) != nil {
					break
				}
			}
			conn.Close()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		<-done
	})
	addr := ln.Addr().(*net.TCPAddr)
	prevDst := dnsTCPOriginalDst
	dnsTCPOriginalDst = func(net.Conn) (net.IP, int, error) { return addr.IP, addr.Port, nil }
	t.Cleanup(func() { dnsTCPOriginalDst = prevDst })
	return asked
}

func TestOneDoHHostLookupDoesNotHandOverItsTCPConnection(t *testing.T) {
	thisHost(t)
	block := config.NewSetConfig()
	block.Id, block.Name, block.Enabled = "block", "block", true
	block.Targets.DomainsToMatch = []string{"blocked.example"}
	block.Routing.Enabled, block.Routing.Mode = true, config.RoutingModeBlock
	w := redirectWorker(t, false, strictSet(dohByName("all", "https://dns.invalid/dns-query", "regexp:.*")), &block)
	asked := keptTCPUpstream(t)
	prevDial := dialDNSTCPUpstream
	dialDNSTCPUpstream = plainDNSTCPDial
	t.Cleanup(func() { dialDNSTCPUpstream = prevDial })
	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 40000}})
	}()
	_ = clientEnd.SetDeadline(time.Now().Add(10 * time.Second))

	for i, tc := range []struct {
		name  string
		rcode uint8
	}{
		{"dns.invalid", dns.RcodeNoError},
		{"www.example.com", dns.RcodeServFail},
		{"blocked.example", dns.RcodeNXDomain},
		{"dns.invalid", dns.RcodeNoError},
	} {
		if err := writeDNSTCPMessage(clientEnd, dns.BuildQuery(tc.name, uint16(90+i), dnsTypeA), time.Second); err != nil {
			t.Fatalf("query %d for %s: %v", i+1, tc.name, err)
		}
		resp, err := readDNSTCPMessage(clientEnd)
		if err != nil {
			t.Fatalf("query %d for %s got no answer: %v", i+1, tc.name, err)
		}
		if rcode, _ := dns.ResponseRcode(resp); rcode != tc.rcode {
			t.Errorf("query %d for %s: rcode %d, want %d", i+1, tc.name, rcode, tc.rcode)
		}
	}
	clientEnd.Close()
	_, _ = io.Copy(io.Discard, clientEnd)
	<-handled
	close(asked)
	for name := range asked {
		if name != "dns.invalid" {
			t.Errorf("%s left in plain DNS on the connection of a DoH host lookup", name)
		}
	}
}
