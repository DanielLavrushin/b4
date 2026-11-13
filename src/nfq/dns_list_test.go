package nfq

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/florianl/go-nfqueue"
)

func listWorker(t *testing.T) (*Worker, *config.Config, *config.SetConfig) {
	t.Helper()
	stubRouteHooks(t)
	stubDSCPHooks(t)
	set := dscpHookSet("list", "example.com", false, false)
	set.DNS.Enabled = true
	w, cfg := dscpHookWorker(t, set)
	dscpDoHServer(t, w, "192.0.2.254")
	return w, cfg, set
}

func failingDoH(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	asked := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		asked.Add(1)
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/dns-query", asked
}

func slowDoH(t *testing.T) (string, chan struct{}, func()) {
	t.Helper()
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	released := false
	free := func() {
		if !released {
			released = true
			close(release)
		}
	}
	t.Cleanup(func() {
		free()
		srv.Close()
	})
	return srv.URL + "/dns-query", entered, free
}

func dohLabel(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return dnsActionDoHPrefix + u.Host
}

func labels(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, line[strings.LastIndex(line, ",")+1:])
	}
	return out
}

func askOverUDP(w *Worker, cfg *config.Config, set *config.SetConfig, txid uint16) {
	servers := redirectServers(cfg, set)
	w.resolveDNSRedirect(IPv4, set, cfg, dns.BuildQuery("www.example.com", txid, dnsTypeA), net.ParseIP("192.168.1.100").To4(), 40000, net.ParseIP("192.168.1.1").To4(), servers, namedServer(servers).Source, 0, false)
}

func TestADeviceQueryMovesOnToTheNextResolverOfTheList(t *testing.T) {
	w, cfg, set := listWorker(t)
	good := dscpDoHServer(t, w, "203.0.113.31")
	failing, asked := failingDoH(t)
	set.DNS.Upstreams = []string{failing, good}

	lines := connectionLines(t)
	replies := watchReplies(t)
	askOverUDP(w, cfg, set, 21)
	onlyAddress(t, oneReply(t, replies), "203.0.113.31")
	if got := labels(drainLines(lines)); len(got) != 1 || got[0] != dohLabel(t, good) {
		t.Fatalf("connection labels %q, want one line naming the resolver that answered, %s", got, dohLabel(t, good))
	}

	tcpDNSUpstream(t)
	lines = connectionLines(t)
	resp := dscpWithin(t, dscpTCPAsk(t, w, dns.BuildQuery("www.example.com", 22, dnsTypeA)), "the TCP answer never came")
	onlyAddress(t, resp, "203.0.113.31")
	if got, want := labels(drainLines(lines)), []string{dohLabel(t, failing), dohLabel(t, good)}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("connection labels over TCP %q, want the first resolver and then the one that answered, %q", got, want)
	}
	if got := asked.Load(); got != 2 {
		t.Fatalf("the failing resolver was asked %d times by two queries, want 2", got)
	}
}

func TestAQueuedQueryThroughAListIsLabelledWithBothResolvers(t *testing.T) {
	w, _, set := listWorker(t)
	good := dscpDoHServer(t, w, "203.0.113.38")
	failing, _ := failingDoH(t)
	set.DNS.Upstreams = []string{failing, good}

	lines := connectionLines(t)
	replies := watchReplies(t)
	q := newFakeQueue()
	w.processDnsPacket(&verdictCtx{id: 34, q: q}, queryPacket(), 41000, 53, dns.BuildQuery("www.example.com", 34, dnsTypeA))
	if v := q.now(t); v != nfqueue.NfDrop {
		t.Fatalf("verdict %d, want the query taken from the queue", v)
	}
	onlyAddress(t, oneReply(t, replies), "203.0.113.38")
	holdsDone(t, w)
	if got, want := labels(drainLines(lines)), []string{dohLabel(t, failing), dohLabel(t, good)}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("connection labels %q, want the first resolver and then the one that answered, %q", got, want)
	}
}

func TestAListOnlySetRedirectsDeviceQueries(t *testing.T) {
	w, cfg, set := listWorker(t)
	set.DNS.Upstreams = []string{dscpDoHServer(t, w, "203.0.113.32")}

	if !cfg.HasDNSRedirect() {
		t.Fatal("a set that asks only a list must turn the DNS redirect on")
	}
	replies := watchReplies(t)
	askOverUDP(w, cfg, set, 23)
	onlyAddress(t, oneReply(t, replies), "203.0.113.32")

	resp := dscpWithin(t, dscpTCPAsk(t, w, dns.BuildQuery("www.example.com", 24, dnsTypeA)), "the TCP answer never came")
	onlyAddress(t, resp, "203.0.113.32")
}

func TestAStrictSetAsksItsCoolingResolverOnBothTransports(t *testing.T) {
	w, cfg, set := listWorker(t)
	set.DNS.Strict = true
	set.DNS.DoHURL = dscpDoHServer(t, w, "203.0.113.33")

	dscpCoolDown(set.DNS.DoHURL)
	replies := watchReplies(t)
	askOverUDP(w, cfg, set, 25)
	onlyAddress(t, oneReply(t, replies), "203.0.113.33")

	dscpCoolDown(set.DNS.DoHURL)
	resp := dscpWithin(t, dscpTCPAsk(t, w, dns.BuildQuery("www.example.com", 26, dnsTypeA)), "the TCP answer never came")
	onlyAddress(t, resp, "203.0.113.33")
}

func TestTheNameStaysInFlightWhileAnEntryIsStillAsked(t *testing.T) {
	w, cfg, set := listWorker(t)
	fast := dscpDoHServer(t, w, "203.0.113.34")
	slow, entered, free := slowDoH(t)
	set.DNS.Upstreams = []string{slow, fast}

	replies := watchReplies(t)
	askOverUDP(w, cfg, set, 27)
	onlyAddress(t, oneReply(t, replies), "203.0.113.34")
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the first resolver was never asked")
	}

	if !dnsRedirectInFlight("www.example.com", dnsTypeA) {
		t.Fatal("the name is no longer marked as resolved by b4 while one of its resolvers has not answered: a failed reply from that resolver's own upstream would now count toward escalation")
	}
	idle := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(idle)
	}()
	select {
	case <-idle:
		t.Fatal("the worker counts as idle while a query to a resolver is still out, so a stop would close the sockets under it")
	case <-time.After(50 * time.Millisecond):
	}

	free()
	select {
	case <-idle:
	case <-time.After(2 * time.Second):
		t.Fatal("the worker never became idle after the last resolver returned")
	}
	if dnsRedirectInFlight("www.example.com", dnsTypeA) {
		t.Fatal("the name is still marked as resolved by b4 after every resolver returned")
	}
}

func TestARedirectInFlightAtAStopSendsNothing(t *testing.T) {
	w, cfg, set := listWorker(t)
	slow, entered, _ := slowDoH(t)
	set.DNS.DoHURL = slow
	dns.NoteSourceFailure(slow)
	dns.NoteSourceFailure(slow)
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	w.ctx = ctx

	replies := watchReplies(t)
	lines := connectionLines(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		askOverUDP(w, cfg, set, 28)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the resolver was never asked")
	}
	stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the redirect did not end with the worker")
	}
	noReply(t, replies, 50*time.Millisecond, "a redirect cut by the stop answered the device from a fallback")
	if got := labels(drainLines(lines)); len(got) != 0 {
		t.Fatalf("a redirect cut by the stop logged %q, want nothing: the resolver did not fail", got)
	}
	if dns.SourceCooling(slow) {
		t.Fatal("a redirect cut by the stop was counted against the resolver")
	}
}

func TestAFallbackInFlightAtAStopSendsNothing(t *testing.T) {
	for _, answers := range []bool{true, false} {
		w, cfg, set := listWorker(t)
		failing, _ := failingDoH(t)
		set.DNS.DoHURL = failing
		t.Cleanup(resetDNSAnswerCache)
		resetDNSAnswerCache()

		entered := make(chan struct{}, 1)
		prev := askOriginalResolver
		t.Cleanup(func() { askOriginalResolver = prev })
		askOriginalResolver = func(ctx context.Context, query []byte, _ net.IP, _ dns.ForwardOptions) ([]byte, error) {
			entered <- struct{}{}
			<-ctx.Done()
			if answers {
				return dns.BuildAnswerFromIPs(query, 60, []net.IP{net.ParseIP("203.0.113.41")}), nil
			}
			return nil, ctx.Err()
		}
		ctx, stop := context.WithCancel(context.Background())
		t.Cleanup(stop)
		w.ctx = ctx

		replies := watchReplies(t)
		lines := connectionLines(t)
		done := make(chan struct{})
		go func() {
			defer close(done)
			askOverUDP(w, cfg, set, 30)
		}()
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("the device's resolver was never asked after the set's resolver failed")
		}
		stop()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("fallback answers=%v: the redirect did not end with the worker while it asked the device's resolver", answers)
		}
		noReply(t, replies, 50*time.Millisecond, "a redirect cut by the stop during its fallback answered the device")
		if got := labels(drainLines(lines)); len(got) != 0 {
			t.Fatalf("fallback answers=%v: a redirect cut by the stop during its fallback logged %q, want nothing", answers, got)
		}
	}
}

func TestADNSOverTCPRedirectInFlightAtAStopWritesNothing(t *testing.T) {
	w, _, set := listWorker(t)
	slow, entered, _ := slowDoH(t)
	set.DNS.DoHURL = slow
	dns.NoteSourceFailure(slow)
	dns.NoteSourceFailure(slow)
	tcpDNSUpstream(t)
	lines := connectionLines(t)

	s := newDNSTCPServer(w, 0)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 40000}})
	}()
	answered := make(chan []byte, 1)
	go func() {
		if resp, err := readDNSTCPMessage(clientEnd); err == nil {
			answered <- resp
		}
	}()
	t.Cleanup(func() { clientEnd.Close() })
	if err := writeDNSTCPMessage(clientEnd, dns.BuildQuery("www.example.com", 30, dnsTypeA), time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the resolver was never asked")
	}

	s.cancel()
	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("the connection's handler did not end with the server")
	}
	select {
	case resp := <-answered:
		t.Fatalf("a redirect cut by the stop wrote %d bytes to the client", len(resp))
	case <-time.After(50 * time.Millisecond):
	}
	if got := labels(drainLines(lines)); len(got) != 1 || got[0] != dohLabel(t, slow) {
		t.Fatalf("connection labels %q, want only the redirect's own line: the resolver did not fail", got)
	}
	if dns.SourceCooling(slow) {
		t.Fatal("a redirect cut by the stop was counted as the resolver's third failure")
	}
}

func TestAStrictListAsksItsCoolingResolverOnlyWhenTheOtherFails(t *testing.T) {
	w, cfg, set := listWorker(t)
	set.DNS.Strict = true
	good := dscpDoHServer(t, w, "203.0.113.37")
	failing, asked := failingDoH(t)

	set.DNS.Upstreams = []string{good, failing}
	dscpCoolDown(good)
	replies := watchReplies(t)
	askOverUDP(w, cfg, set, 31)
	onlyAddress(t, oneReply(t, replies), "203.0.113.37")
	if got := asked.Load(); got != 1 {
		t.Fatalf("the resolver that is not cooling was asked %d times, want it asked first", got)
	}

	dscpCoolDown(good)
	resp := dscpWithin(t, dscpTCPAsk(t, w, dns.BuildQuery("www.example.com", 32, dnsTypeA)), "the TCP answer never came")
	onlyAddress(t, resp, "203.0.113.37")

	set.DNS.Upstreams = []string{failing, good}
	dns.ResetSourceHealth()
	dscpCoolDown(failing)
	before := asked.Load()
	askOverUDP(w, cfg, set, 33)
	onlyAddress(t, oneReply(t, replies), "203.0.113.37")
	if got := asked.Load() - before; got != 0 {
		t.Fatalf("the cooling resolver was asked %d times although the other one answered", got)
	}
}

func TestEscalationReachesASetThatAsksOnlyAList(t *testing.T) {
	w, cfg, set := listWorker(t)
	set.DNS.Upstreams = []string{dscpDoHServer(t, w, "203.0.113.35")}
	client, server := net.ParseIP("192.168.1.100").To4(), net.ParseIP("192.168.1.1").To4()
	query := dns.BuildQuery("www.example.com", 29, dnsTypeA)

	replies := watchReplies(t)
	if !w.answerViaSetInline(IPv4, cfg, set, "www.example.com", query, client, 40000, server, false) {
		t.Fatal("a set that asks only a list cannot answer an escalated query")
	}
	onlyAddress(t, oneReply(t, replies), "203.0.113.35")

	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	resp, err := s.answerVia(set, cfg, query, "www.example.com", client)
	if err != nil {
		t.Fatalf("the TCP escalation: %v", err)
	}
	onlyAddress(t, resp, "203.0.113.35")
}

func TestTheConnectionLabelNamesTheFirstResolverThatIsNotCooling(t *testing.T) {
	t.Cleanup(dns.ResetSourceHealth)
	dns.ResetSourceHealth()
	set := redirectSet("", "")
	set.DNS.Upstreams = []string{"https://dns.example/dns-query", "192.0.2.7"}
	servers := serversOf(set)

	if got, want := dnsRedirectAction(namedServer(servers)), dnsActionDoHPrefix+"dns.example"; got != want {
		t.Fatalf("label = %q, want the first resolver, %q", got, want)
	}
	dscpCoolDown("https://dns.example/dns-query")
	if got, want := dnsRedirectAction(namedServer(servers)), dnsActionForwardPrefix+"192.0.2.7"; got != want {
		t.Fatalf("label = %q, want %q: the first resolver is left out while it cools down", got, want)
	}
	dscpCoolDown("192.0.2.7")
	if got, want := dnsRedirectAction(namedServer(servers)), dnsActionDoHPrefix+"dns.example"; got != want {
		t.Fatalf("label = %q, want the first resolver again when every resolver cools down, %q", got, want)
	}
}

func TestTheFallbackAsksTheDevicesResolverUnlessTheSetAsksItOverPlainDNS(t *testing.T) {
	set := redirectSet("192.0.2.6", "")
	set.DNS.Upstreams = []string{"192.0.2.7", "https://192.0.2.8/dns-query"}
	w := redirectWorker(t, false, set)
	cfg := w.getConfig()
	servers := redirectServers(cfg, set)
	t.Cleanup(resetDNSAnswerCache)
	resetDNSAnswerCache()

	var asked []string
	prev := askOriginalResolver
	t.Cleanup(func() { askOriginalResolver = prev })
	askOriginalResolver = func(_ context.Context, query []byte, target net.IP, _ dns.ForwardOptions) ([]byte, error) {
		asked = append(asked, target.String())
		return dns.BuildAnswerFromIPs(query, 60, []net.IP{net.ParseIP("203.0.113.36")}), nil
	}
	query := dns.BuildAQuery("example.com", 7)

	for _, own := range []string{"192.0.2.6", "192.0.2.7"} {
		if resp, _ := w.dnsRedirectFallback(cfg, set, servers, "example.com", query, net.ParseIP(own), 0); resp != nil {
			t.Fatalf("the fallback asked %s again, a plain resolver of the set that has just failed", own)
		}
	}
	for _, other := range []string{"192.0.2.8", "192.0.2.9"} {
		resp, action := w.dnsRedirectFallback(cfg, set, servers, "example.com", query, net.ParseIP(other), 0)
		if resp == nil || action != dnsActionFallbackUpstream {
			t.Fatalf("the fallback did not ask %s over plain DNS (action %q): a DoH host's port 53 is another service, and so is any other resolver", other, action)
		}
	}
	if len(asked) != 2 || asked[0] != "192.0.2.8" || asked[1] != "192.0.2.9" {
		t.Fatalf("the fallback asked %v, want only the two resolvers the set does not ask over plain DNS", asked)
	}
}

func TestABadTargetIsLabelledOnlyWhileTheRedirectIsOn(t *testing.T) {
	set := redirectSet("resolver.local", "")
	if !dnsBadTarget(set) {
		t.Fatal("a non-empty primary that does not parse must keep the dns-bad-target label")
	}
	set.DNS.TargetDNS = " "
	if !dnsBadTarget(set) {
		t.Fatal("a primary of blanks is a target that does not parse, as before the list")
	}
	set.DNS.TargetDNS = "resolver.local"
	set.DNS.Enabled = false
	if dnsBadTarget(set) {
		t.Fatal("a disabled redirect passes through, it is not a bad target")
	}
	set.DNS.Enabled = true
	set.DNS.Upstreams = []string{"1.1.1.1"}
	if dnsBadTarget(set) {
		t.Fatal("a set with a usable list is not a bad target")
	}

	thisHost(t)
	for _, tc := range []struct {
		enabled bool
		label   string
	}{{true, dnsActionBadTarget}, {false, dnsActionPassthrough}} {
		bad := redirectSet("resolver.local", "")
		bad.DNS.Enabled = tc.enabled
		w := redirectWorker(t, false, bad)
		lines := connectionLines(t)
		upstream := tcpDNSUpstream(t)
		tcpFromTarget(t, w, "192.0.2.100", upstream, dns.BuildQuery("www.example.com", 64, dnsTypeA))
		if got := labels(drainLines(lines)); len(got) != 1 || got[0] != tc.label {
			t.Fatalf("redirect on=%v: connection labels over TCP %q, want %q", tc.enabled, got, tc.label)
		}
	}
}
