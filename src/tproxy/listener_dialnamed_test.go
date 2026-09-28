package tproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/socks5"
)

func TestDialNamedSendsTheNameTheClientAskedFor(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})

	conn, err := l.DialNamed("site.example.org", 443)
	if err != nil {
		t.Fatalf("DialNamed: %v", err)
	}
	conn.Close()
	r := up.next(t)
	if r.atyp != 3 || r.host != "site.example.org" || r.port != 443 {
		t.Fatalf("upstream got atyp %d %s:%d, want the name site.example.org:443", r.atyp, r.host, r.port)
	}
	if l.upstreamFails.Load() != 0 || l.upstreamLastOK.Load() == 0 {
		t.Errorf("a working upstream was not recorded as reached: fails %d", l.upstreamFails.Load())
	}
}

func TestDialNamedResolvesTheNameWhenUseDomainIsOff(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.UseDomain = false

	conn, err := l.DialNamed("localhost", 443)
	if err != nil {
		t.Fatalf("DialNamed: %v", err)
	}
	conn.Close()
	if r := up.next(t); r.atyp != 1 || r.host != "127.0.0.1" {
		t.Fatalf("use_domain off sent atyp %d host %q, want the IPv4 address", r.atyp, r.host)
	}
}

func TestDialNamedSendsThePinnedAddress(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	set := &config.SetConfig{}
	set.DNS.Pins = map[string][]string{"pinned.example.com": {"203.0.113.7"}}
	l.set.Store(set)

	conn, err := l.DialNamed("www.pinned.example.com", 443)
	if err != nil {
		t.Fatalf("DialNamed: %v", err)
	}
	conn.Close()
	if r := up.next(t); r.atyp != 1 || r.host != "203.0.113.7" {
		t.Fatalf("a pinned name went upstream as atyp %d host %q", r.atyp, r.host)
	}
}

func TestDialNamedRetriesWithTheAddressWhenTheNameIsRefused(t *testing.T) {
	up := startMockSocks(t, 0, func(atyp byte) byte {
		if atyp == 3 {
			return 4
		}
		return 0
	})
	l := newTestListener(t, up.port(), fakeNames{})

	conn, err := l.DialNamed("localhost", 443)
	if err != nil {
		t.Fatalf("DialNamed: %v", err)
	}
	conn.Close()
	first, second := up.next(t), up.next(t)
	if first.atyp != 3 || first.host != "localhost" {
		t.Fatalf("first CONNECT: atyp %d host %q", first.atyp, first.host)
	}
	if second.atyp != 1 || second.host != "127.0.0.1" {
		t.Fatalf("retry CONNECT: atyp %d host %q, want the address", second.atyp, second.host)
	}
}

func TestDialNamedFailsOpenToADirectConnection(t *testing.T) {
	dead, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadPort := dead.Addr().(*net.TCPAddr).Port
	dead.Close()

	direct, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer direct.Close()
	go func() {
		c, err := direct.Accept()
		if err != nil {
			return
		}
		_, _ = c.Write([]byte("direct"))
		c.Close()
	}()
	directPort := direct.Addr().(*net.TCPAddr).Port

	l := newTestListener(t, deadPort, fakeNames{})
	if _, err := l.DialNamed("localhost", directPort); err == nil {
		t.Fatal("with fail-open off an unreachable upstream must fail the dial")
	}
	if l.upstreamFails.Load() != 1 {
		t.Fatalf("upstream failures = %d, want 1", l.upstreamFails.Load())
	}

	l.FailOpen = true
	conn, err := l.DialNamed("localhost", directPort)
	if err != nil {
		t.Fatalf("fail-open dial: %v", err)
	}
	defer conn.Close()
	got, _ := io.ReadAll(conn)
	if string(got) != "direct" {
		t.Fatalf("fail-open reached %q, want the direct server on port %s", got, strconv.Itoa(directPort))
	}
}

func TestDialViaSetLeavesSetsWithoutAListenerToTheCaller(t *testing.T) {
	m := NewManager(nil)
	t.Cleanup(m.Stop)
	if _, handled, _ := m.DialViaSet("missing", "site.example.org", 443); handled {
		t.Fatal("a set with no listener was handled")
	}

	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	m.mu.Lock()
	m.listeners["set-1"] = l
	m.listeners["bridge"] = &Listener{MTProtoWS: true}
	m.mu.Unlock()

	if _, handled, _ := m.DialViaSet("bridge", "site.example.org", 443); handled {
		t.Fatal("a Telegram over WebSocket set was handled as a proxy upstream")
	}
	conn, handled, err := m.DialViaSet("set-1", "site.example.org", 443)
	if !handled || err != nil {
		t.Fatalf("DialViaSet: handled %v err %v", handled, err)
	}
	conn.Close()
	if r := up.next(t); r.host != "site.example.org" {
		t.Fatalf("upstream got %q", r.host)
	}
}

func TestDialNamedTreatsARefusalAsTheDestinationsFailure(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 5 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.FailOpen = true

	_, err := l.DialNamed("x2.onion", 443)
	var rejected *socks5.ConnectRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("DialNamed = %v, want the upstream's refusal", err)
	}
	if r := up.next(t); r.atyp != 3 || r.host != "x2.onion" {
		t.Fatalf("upstream got atyp %d host %q", r.atyp, r.host)
	}
	select {
	case r := <-up.reqs:
		t.Fatalf("a refused .onion name was retried as %q", r.host)
	default:
	}
	if l.upstreamFails.Load() != 0 {
		t.Errorf("an upstream that answered was counted as unreachable %d times", l.upstreamFails.Load())
	}
}

func TestDialNamedNeverFailsOpenForAnOnionName(t *testing.T) {
	dead, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadPort := dead.Addr().(*net.TCPAddr).Port
	dead.Close()

	l := newTestListener(t, deadPort, fakeNames{})
	l.FailOpen = true
	if _, err := l.DialNamed("x2.onion", 443); err == nil {
		t.Fatal("a .onion name has no address to reach directly, the dial must fail")
	}
}

func TestDialNamedHoldsTheRelayUntilTheConnectionCloses(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	keys := relayKeys("tcp", 443, "site.example.org")

	conn, err := l.DialNamed("site.example.org", 443)
	if err != nil {
		t.Fatalf("DialNamed: %v", err)
	}
	up.next(t)
	if !l.relaying(keys) {
		t.Fatal("the relay is not held while the connection is open, so the upstream's own outbound connection would loop back into it")
	}
	if _, ok := conn.(interface{ CloseWrite() error }); !ok {
		t.Fatal("the returned connection cannot half-close")
	}
	conn.Close()
	conn.Close()
	if l.relaying(keys) {
		t.Fatal("the relay is still held after the connection closed")
	}
}

func withSetDNS(t *testing.T, l *Listener, strict bool, answer func(host string) ([]net.IP, error)) *[]dns.Server {
	t.Helper()
	set := &config.SetConfig{}
	set.DNS.Enabled = true
	set.DNS.DoHURL = "https://dns.example/dns-query"
	set.DNS.Strict = strict
	l.set.Store(set)
	l.SetDNSOptions(0x8000, 3*time.Second, false)
	dns.ResetSourceHealth()
	t.Cleanup(dns.ResetSourceHealth)
	var used []dns.Server
	orig := setLookup
	t.Cleanup(func() { setLookup = orig })
	setLookup = func(_ context.Context, srv dns.Server, host string, _, _ bool) ([]net.IP, error) {
		used = append(used, srv)
		return answer(host)
	}
	return &used
}

func TestDialNamedResolvesThroughTheSetsDNSServer(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.UseDomain = false
	used := withSetDNS(t, l, false, func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("2001:db8::5"), net.IPv4(203, 0, 113, 50)}, nil
	})

	conn, err := l.DialNamed("blocked.example", 443)
	if err != nil {
		t.Fatalf("DialNamed: %v", err)
	}
	conn.Close()
	if r := up.next(t); r.atyp != 1 || r.host != "203.0.113.50" {
		t.Fatalf("upstream got atyp %d host %q, want the set's DNS answer", r.atyp, r.host)
	}
	if len(*used) != 1 || (*used)[0].DoHURL != "https://dns.example/dns-query" || (*used)[0].Mark != 0x8000 || (*used)[0].Timeout != 3*time.Second {
		t.Fatalf("lookup went to %+v", *used)
	}
}

func TestDialNamedFallsBackToTheRoutersResolverUnlessStrict(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.UseDomain = false
	withSetDNS(t, l, false, func(string) ([]net.IP, error) { return nil, errors.New("resolver down") })

	conn, err := l.DialNamed("localhost", 443)
	if err != nil {
		t.Fatalf("DialNamed: %v", err)
	}
	conn.Close()
	if r := up.next(t); r.host != "127.0.0.1" {
		t.Fatalf("upstream got %q, want the router's answer after the set's resolver failed", r.host)
	}

	strict := newTestListener(t, up.port(), fakeNames{})
	strict.UseDomain = false
	withSetDNS(t, strict, true, func(string) ([]net.IP, error) { return nil, errors.New("resolver down") })
	if _, err := strict.DialNamed("localhost", 443); err == nil {
		t.Fatal("with Strict on, a failed set resolver must fail the connection")
	}
	select {
	case r := <-up.reqs:
		t.Fatalf("a strict set still reached the upstream with %q", r.host)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDialNamedRetriesARefusedNameWithTheSetsDNSAnswer(t *testing.T) {
	up := startMockSocks(t, 0, func(atyp byte) byte {
		if atyp == 3 {
			return 4
		}
		return 0
	})
	l := newTestListener(t, up.port(), fakeNames{})
	withSetDNS(t, l, false, func(string) ([]net.IP, error) { return []net.IP{net.IPv4(203, 0, 113, 51)}, nil })

	conn, err := l.DialNamed("blocked.example", 443)
	if err != nil {
		t.Fatalf("DialNamed: %v", err)
	}
	conn.Close()
	first, second := up.next(t), up.next(t)
	if first.host != "blocked.example" || second.host != "203.0.113.51" {
		t.Fatalf("CONNECTs %q then %q, want the name then the set's DNS answer", first.host, second.host)
	}
}

func TestDialNamedFailsOpenToTheSetsDNSAnswer(t *testing.T) {
	dead, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadPort := dead.Addr().(*net.TCPAddr).Port
	dead.Close()
	direct, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer direct.Close()
	go func() {
		c, err := direct.Accept()
		if err != nil {
			return
		}
		_, _ = c.Write([]byte("direct"))
		c.Close()
	}()

	l := newTestListener(t, deadPort, fakeNames{})
	l.FailOpen = true
	withSetDNS(t, l, false, func(string) ([]net.IP, error) { return []net.IP{net.IPv4(127, 0, 0, 1)}, nil })

	conn, err := l.DialNamed("blocked.example", direct.Addr().(*net.TCPAddr).Port)
	if err != nil {
		t.Fatalf("fail-open dial: %v", err)
	}
	defer conn.Close()
	if got, _ := io.ReadAll(conn); string(got) != "direct" {
		t.Fatalf("fail-open reached %q", got)
	}
}

func TestDialNamedSendsAnOnionNameEvenWithUseDomainOff(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.UseDomain = false
	withSetDNS(t, l, false, func(host string) ([]net.IP, error) {
		t.Errorf("%s was looked up locally; a .onion name only exists inside the upstream", host)
		return nil, errors.New("no")
	})

	conn, err := l.DialNamed("x3.onion", 443)
	if err != nil {
		t.Fatalf("DialNamed: %v", err)
	}
	conn.Close()
	if r := up.next(t); r.atyp != 3 || r.host != "x3.onion" {
		t.Fatalf("upstream got atyp %d host %q, want the .onion name", r.atyp, r.host)
	}
}

func TestDialNamedHoldsTheRetriedAddressForTheLoopGuard(t *testing.T) {
	up := startMockSocks(t, 0, func(atyp byte) byte {
		if atyp == 3 {
			return 4
		}
		return 0
	})
	l := newTestListener(t, up.port(), fakeNames{})

	conn, err := l.DialNamed("localhost", 443)
	if err != nil {
		t.Fatalf("DialNamed: %v", err)
	}
	up.next(t)
	up.next(t)
	addrKeys := relayKeys("tcp", 443, "127.0.0.1")
	if !l.relaying(addrKeys) {
		t.Fatal("the address the retry went to is not held, so the upstream's own connection to it would not be recognised")
	}
	conn.Close()
	if l.relaying(addrKeys) || l.relaying(relayKeys("tcp", 443, "localhost")) {
		t.Fatal("the relay keys outlived the connection")
	}
}
