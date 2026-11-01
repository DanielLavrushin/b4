package tproxy

import (
	"bytes"
	"context"
	"io"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/sni"
	"golang.org/x/sys/unix"
)

type heldBack struct {
	net.Conn
	head []byte
}

func (c *heldBack) Read(p []byte) (int, error) {
	if len(c.head) > 0 {
		n := copy(p, c.head)
		c.head = c.head[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

func (c *heldBack) CloseWrite() error {
	return c.Conn.(*net.TCPConn).CloseWrite()
}

func (c *heldBack) NetConn() net.Conn {
	return c.Conn
}

type passOnBridge struct {
	handled bool
	consume int
	order   [2]bool
	worker  func(client net.Conn) bool

	mu       sync.Mutex
	fellBack bool
}

func (b *passOnBridge) Handle(client net.Conn, origIP net.IP, origPort int) (bool, net.Conn) {
	if b.handled {
		return true, nil
	}
	head := make([]byte, b.consume)
	if _, err := io.ReadFull(client, head); err != nil {
		return true, nil
	}
	return false, &heldBack{Conn: client, head: head}
}

func (b *passOnBridge) FailOpenOrder(net.IP, int) (bool, bool) {
	b.mu.Lock()
	b.fellBack = true
	b.mu.Unlock()
	return b.order[0], b.order[1]
}

func (b *passOnBridge) FailOpenViaWorker(client net.Conn, origIP net.IP, origPort int) bool {
	if b.worker == nil {
		return false
	}
	return b.worker(client)
}

func (b *passOnBridge) NoteFailOpenDirect(net.IP, bool, int64) {}

func (b *passOnBridge) failedOpen() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.fellBack
}

type workerRead struct {
	data []byte
	conn net.Conn
}

func workerReading(n int) (*passOnBridge, chan workerRead) {
	got := make(chan workerRead, 1)
	return &passOnBridge{consume: 4, order: [2]bool{false, true}, worker: func(c net.Conn) bool {
		buf := make([]byte, n)
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		m, _ := io.ReadFull(c, buf)
		got <- workerRead{data: buf[:m], conn: c}
		return true
	}}, got
}

func fellBackWith(t *testing.T, got chan workerRead, want []byte) net.Conn {
	t.Helper()
	select {
	case r := <-got:
		if !bytes.Equal(r.data, want) {
			t.Fatalf("the bridge's fail-open path read %d bytes, want the client's %d bytes intact", len(r.data), len(want))
		}
		return r.conn
	case <-time.After(5 * time.Second):
		t.Fatal("the connection never reached the bridge's fail-open path")
		return nil
	}
}

type matchingNames struct {
	fakeNames
	matcher *sni.SuffixSet
	asked   atomic.Int32
}

func (n *matchingNames) resolver() *Resolver {
	n.asked.Add(1)
	r := &Resolver{}
	r.Set(n.matcher)
	return r
}

func (n *matchingNames) PassOnSet(host string, dst net.IP, dport uint16, srcMAC string, applies sni.SetFilter, takes func(set *config.SetConfig, byName bool) bool) (*config.SetConfig, bool) {
	return n.resolver().PassOnSet(host, dst, dport, srcMAC, applies, takes)
}

func proxySetFor(id string, domains, ips []string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Name = id
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeProxy
	set.Routing.Upstream.Port = 1080
	set.Targets.DomainsToMatch = domains
	set.Targets.IpsToMatch = ips
	return &set
}

func bridgeListener(t *testing.T, b MTProtoBridge, sets []*config.SetConfig, peers ...*Listener) (*Listener, *matchingNames) {
	t.Helper()
	names := &matchingNames{matcher: sni.NewSuffixSet(sets)}
	l := &Listener{
		SetID:     config.TelegramBridgeSetID,
		SetName:   config.TelegramBridgeSetName,
		MTProtoWS: true,
		Bridge:    b,
		Names:     names,
	}
	l.peers = func() []*Listener { return append([]*Listener{l}, peers...) }
	l.ctx, l.cancel = context.WithCancel(context.Background())
	t.Cleanup(l.cancel)
	return l, names
}

func peerListener(t *testing.T, upstreamPort int, set *config.SetConfig) *Listener {
	t.Helper()
	l := newTestListener(t, upstreamPort, fakeNames{inSet: set.Targets.DomainsToMatch})
	l.SetID, l.SetName = set.Id, set.Name
	l.set.Store(set)
	return l
}

func noSocksRequest(t *testing.T, up *mockSocks, d time.Duration) {
	t.Helper()
	select {
	case r := <-up.reqs:
		t.Fatalf("the set's upstream got a CONNECT for %q", r.host)
	case <-time.After(d):
	}
}

func asLANClient(t *testing.T, mac string) {
	t.Helper()
	orig := isRouterClient
	isRouterClient = func(net.IP) bool { return false }
	metrics.SetMACProvider(func(string) string { return mac })
	t.Cleanup(func() {
		isRouterClient = orig
		metrics.SetMACProvider(nil)
	})
}

func okReply(byte) byte { return 0 }

func TestTheBridgePassesAHelloForAListedHostToThatSet(t *testing.T) {
	lines := watchConnLines(t)
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, len(hello), okReply)
	set := proxySetFor("tg-api", []string{"api.telegram.org"}, nil)
	peer := peerListener(t, up.port(), set)
	b := &passOnBridge{consume: 4}
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{set}, peer)

	p := acceptedPair(t)
	done := handleInBackground(t, bridge, p)
	if _, err := p.client.Write(hello); err != nil {
		t.Fatal(err)
	}
	r := up.next(t)
	if r.atyp != 3 || r.host != "api.telegram.org" {
		t.Fatalf("CONNECT asked for atyp %d host %q, want the name api.telegram.org", r.atyp, r.host)
	}
	if !bytes.Equal(r.payload, hello) {
		t.Fatalf("the set's upstream got %d bytes, want the client's %d-byte hello intact, the bytes the bridge read included", len(r.payload), len(hello))
	}
	if line := nextConnLine(t, lines, set.Name); line.domain != "api.telegram.org" || line.meta != "proxy" {
		t.Fatalf("connection line %+v, want the set's own proxy line for api.telegram.org", line)
	}
	p.client.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handle did not return after the client closed")
	}
	if b.failedOpen() {
		t.Fatal("a connection a set took over also went down the bridge's own fail-open")
	}
}

func TestTheBridgePassesAnHTTPRequestByItsHost(t *testing.T) {
	req := []byte("GET /bot1/getMe HTTP/1.1\r\nHost: API.Telegram.org.\r\nUser-Agent: test\r\n\r\n")
	up := startMockSocks(t, len(req), okReply)
	set := proxySetFor("tg-api", []string{"api.telegram.org"}, nil)
	peer := peerListener(t, up.port(), set)
	bridge, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{set}, peer)

	proxyThrough(t, bridge, req)
	r := up.next(t)
	if r.atyp != 3 || r.host != "api.telegram.org" {
		t.Fatalf("CONNECT asked for atyp %d host %q, want the name from the Host header", r.atyp, r.host)
	}
	if !bytes.Equal(r.payload, req) {
		t.Fatal("the request did not reach the set's upstream intact")
	}
}

func TestTheBridgePassesAConnectionToASetThatListsItsAddress(t *testing.T) {
	hello := realClientHello(t, "www.example.com")
	up := startMockSocks(t, len(hello), okReply)
	set := proxySetFor("by-address", nil, []string{"127.0.0.1/32"})
	peer := peerListener(t, up.port(), set)
	bridge, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{set}, peer)

	proxyThrough(t, bridge, hello)
	r := up.next(t)
	if r.atyp != 1 || r.host != "127.0.0.1" {
		t.Fatalf("CONNECT asked for atyp %d host %q, want the original address", r.atyp, r.host)
	}
	if !bytes.Equal(r.payload, hello) {
		t.Fatal("the hello did not reach the set's upstream intact")
	}
}

func TestTheSetThatListsTheNameGoesBeforeTheSetThatListsTheAddress(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	byAddress := proxySetFor("by-address", nil, []string{"127.0.0.1/32"})
	byName := proxySetFor("by-name", []string{"api.telegram.org"}, nil)
	upAddress := startMockSocks(t, 0, okReply)
	upName := startMockSocks(t, len(hello), okReply)
	bridge, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{byAddress, byName},
		peerListener(t, upAddress.port(), byAddress), peerListener(t, upName.port(), byName))

	proxyThrough(t, bridge, hello)
	if r := upName.next(t); r.host != "api.telegram.org" {
		t.Fatalf("CONNECT asked for %q", r.host)
	}
	noSocksRequest(t, upAddress, 200*time.Millisecond)
}

func TestAPassedOnConnectionIsNamedBeforeTheUpstreamAnswers(t *testing.T) {
	lines := watchConnLines(t)
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, len(hello), func(byte) byte {
		time.Sleep(3 * sniffFirstWait)
		return 0
	})
	set := proxySetFor("by-address-name", []string{"api.telegram.org"}, nil)
	peer := peerListener(t, up.port(), set)
	peer.UseDomain = false
	bridge, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{set}, peer)

	proxyThrough(t, bridge, hello)
	if line := nextConnLine(t, lines, set.Name); line.domain != "api.telegram.org" || line.tls != "1.3" {
		t.Fatalf("connection line %+v, want api.telegram.org over TLS 1.3 although the upstream answered late", line)
	}
	if r := up.next(t); r.atyp != 1 || !bytes.Equal(r.payload, hello) {
		t.Fatalf("CONNECT asked for atyp %d host %q, want the address and the hello intact", r.atyp, r.host)
	}
}

func TestTheSetsOwnFailOpenGetsEveryByte(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, 0, func(byte) byte { return 1 })
	set := proxySetFor("tg-api", []string{"api.telegram.org"}, nil)
	peer := peerListener(t, up.port(), set)
	peer.FailOpen = true
	b := &passOnBridge{consume: 4}
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{set}, peer)

	dst, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	client, err := net.Dial("tcp4", dst.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	accepted, err := dst.Accept()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		bridge.handle(accepted)
		close(done)
	}()
	if _, err := client.Write(hello); err != nil {
		t.Fatal(err)
	}
	_ = dst.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
	direct, err := dst.Accept()
	if err != nil {
		t.Fatalf("the set's fail_open never dialled the original address: %v", err)
	}
	defer direct.Close()
	got := make([]byte, len(hello))
	_ = direct.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(direct, got); err != nil || !bytes.Equal(got, hello) {
		t.Fatalf("the set's fail_open delivered %d bytes (%v), want the client's hello intact", len(got), err)
	}
	client.Close()
	direct.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handle did not return")
	}
	if b.failedOpen() {
		t.Fatal("the set's own fail_open was followed by the bridge's")
	}
}

func TestTheBridgeKeepsItsFailOpenForAHostNoSetLists(t *testing.T) {
	hello := realClientHello(t, "web.telegram.org")
	up := startMockSocks(t, 0, okReply)
	set := proxySetFor("tg-api", []string{"api.telegram.org"}, nil)
	peer := peerListener(t, up.port(), set)
	b, got := workerReading(len(hello))
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{set}, peer)

	proxyThrough(t, bridge, hello)
	fellBackWith(t, got, hello)
	noSocksRequest(t, up, 200*time.Millisecond)
}

func TestNothingIsReadAheadWithoutASetToPassTo(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	set := proxySetFor("tg-api", []string{"api.telegram.org"}, nil)
	b, got := workerReading(len(hello))
	bridge, names := bridgeListener(t, b, []*config.SetConfig{set})

	proxyThrough(t, bridge, hello)
	if c := fellBackWith(t, got, hello); c == nil {
		return
	} else if _, ok := c.(*heldBack); !ok {
		t.Fatalf("the fail-open path got a %T, want the bridge's own connection with nothing read ahead", c)
	}
	if n := names.asked.Load(); n != 0 {
		t.Fatalf("the sets were consulted %d times with no proxy set running", n)
	}
}

func TestAStoppedSetIsNotPassedOn(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, 0, okReply)
	set := proxySetFor("tg-api", []string{"api.telegram.org"}, nil)
	peer := peerListener(t, up.port(), set)
	peer.cancel()
	b, got := workerReading(len(hello))
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{set}, peer)

	proxyThrough(t, bridge, hello)
	fellBackWith(t, got, hello)
	noSocksRequest(t, up, 200*time.Millisecond)
}

func TestTheBridgeKeepsTheSessionsItCarries(t *testing.T) {
	up := startMockSocks(t, 0, okReply)
	set := proxySetFor("everything", nil, []string{"0.0.0.0/0"})
	peer := peerListener(t, up.port(), set)
	bridge, names := bridgeListener(t, &passOnBridge{handled: true}, []*config.SetConfig{set}, peer)

	p := acceptedPair(t)
	done := handleInBackground(t, bridge, p)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handle did not return for a session the bridge carried")
	}
	if n := names.asked.Load(); n != 0 {
		t.Fatalf("the sets were consulted %d times for a session the bridge carried", n)
	}
	noSocksRequest(t, up, 100*time.Millisecond)
}

func TestTheFirstSetThatListsTheHostDecides(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, 0, okReply)
	dpiOnly := proxySetFor("dpi-only", []string{"api.telegram.org"}, nil)
	dpiOnly.Routing.Enabled = false
	routed := proxySetFor("tg-api", []string{"api.telegram.org"}, nil)
	peer := peerListener(t, up.port(), routed)
	b, got := workerReading(len(hello))
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{dpiOnly, routed}, peer)

	proxyThrough(t, bridge, hello)
	fellBackWith(t, got, hello)
	noSocksRequest(t, up, 200*time.Millisecond)
}

func TestAnAddressStillCountsWhenTheHostsSetDoesNotRoute(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, len(hello), okReply)
	dpiOnly := proxySetFor("dpi-only", []string{"api.telegram.org"}, nil)
	dpiOnly.Routing.Enabled = false
	byAddress := proxySetFor("by-address", nil, []string{"127.0.0.1/32"})
	peer := peerListener(t, up.port(), byAddress)
	bridge, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{dpiOnly, byAddress}, peer)

	proxyThrough(t, bridge, hello)
	if r := up.next(t); !bytes.Equal(r.payload, hello) {
		t.Fatal("the hello did not reach the upstream of the set that lists the address")
	}
}

func TestAnAddressInTheHostHeaderIsNotMatchedAsAName(t *testing.T) {
	req := []byte("POST /api HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Length: 0\r\n\r\n")
	up := startMockSocks(t, 0, okReply)
	set := proxySetFor("odd", []string{"127.0.0.1"}, nil)
	peer := peerListener(t, up.port(), set)
	b, got := workerReading(len(req))
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{set}, peer)

	proxyThrough(t, bridge, req)
	fellBackWith(t, got, req)
	noSocksRequest(t, up, 200*time.Millisecond)
}

func TestADomainOnlySetTakesOnlyTheAddressesItLists(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, 0, okReply)
	byName := proxySetFor("domain-only", []string{"api.telegram.org"}, nil)
	byName.Targets.DomainOnly = true
	peer := peerListener(t, up.port(), byName)
	b, got := workerReading(len(hello))
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{byName}, peer)

	proxyThrough(t, bridge, hello)
	fellBackWith(t, got, hello)
	noSocksRequest(t, up, 200*time.Millisecond)

	listed := proxySetFor("domain-only-listed", []string{"api.telegram.org"}, []string{"127.0.0.1/32"})
	listed.Targets.DomainOnly = true
	up2 := startMockSocks(t, 0, okReply)
	bridge2, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{listed}, peerListener(t, up2.port(), listed))
	proxyThrough(t, bridge2, hello)
	up2.next(t)
}

func TestADeviceNeverGetsASetLimitedToSourceInterfaces(t *testing.T) {
	asLANClient(t, "")
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, 0, okReply)
	set := proxySetFor("guest", []string{"api.telegram.org"}, nil)
	set.Routing.SourceInterfaces = []string{"br-guest"}
	b, got := workerReading(len(hello))
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{set}, peerListener(t, up.port(), set))

	proxyThrough(t, bridge, hello)
	fellBackWith(t, got, hello)
	noSocksRequest(t, up, 200*time.Millisecond)
}

func TestOnlyAnIncludedDeviceGetsItsDeviceSet(t *testing.T) {
	for _, tc := range []struct {
		mac  string
		want bool
	}{
		{"aa:bb:cc:dd:ee:01", true},
		{"aa:bb:cc:dd:ee:02", false},
	} {
		t.Run(tc.mac, func(t *testing.T) {
			asLANClient(t, tc.mac)
			hello := realClientHello(t, "api.telegram.org")
			up := startMockSocks(t, len(hello), okReply)
			set := proxySetFor("phone", []string{"api.telegram.org"}, nil)
			set.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:01"}
			b, got := workerReading(len(hello))
			bridge, _ := bridgeListener(t, b, []*config.SetConfig{set}, peerListener(t, up.port(), set))

			proxyThrough(t, bridge, hello)
			if tc.want {
				up.next(t)
				return
			}
			fellBackWith(t, got, hello)
			noSocksRequest(t, up, 200*time.Millisecond)
		})
	}
}

func TestTheRoutersOwnConnectionSkipsASetLimitedToDevices(t *testing.T) {
	metrics.SetMACProvider(func(string) string { return "aa:bb:cc:dd:ee:01" })
	t.Cleanup(func() { metrics.SetMACProvider(nil) })
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, 0, okReply)
	set := proxySetFor("phone", []string{"api.telegram.org"}, nil)
	set.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:01"}
	b, got := workerReading(len(hello))
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{set}, peerListener(t, up.port(), set))

	proxyThrough(t, bridge, hello)
	fellBackWith(t, got, hello)
	noSocksRequest(t, up, 200*time.Millisecond)
}

func TestManagerListenersSeeTheRunningSets(t *testing.T) {
	m := NewManager(nil)
	t.Cleanup(m.cancel)
	cfg := config.NewConfig()
	cfg.System.MTProto.Bridge.Enabled = true
	bridge := m.newListener(&cfg, cfg.TelegramBridgeSet(), 0)
	other := &Listener{SetID: "tg-api"}
	m.mu.Lock()
	m.listeners[other.SetID] = other
	m.publishLocked()
	m.mu.Unlock()
	if bridge.peers == nil || !slices.Contains(bridge.peers(), other) {
		t.Fatal("a listener the Manager builds does not see the sets the Manager runs")
	}
}

func TestPassOnEligible(t *testing.T) {
	proxy := func(edit func(*config.SetConfig)) *config.SetConfig {
		set := proxySetFor("s", []string{"api.telegram.org"}, nil)
		if edit != nil {
			edit(set)
		}
		return set
	}
	domainOnly := proxy(func(s *config.SetConfig) { s.Targets.DomainOnly = true })
	cases := []struct {
		name   string
		set    *config.SetConfig
		byName bool
		want   bool
	}{
		{"proxy set by name", proxy(nil), true, true},
		{"proxy set by address", proxy(nil), false, true},
		{"disabled", proxy(func(s *config.SetConfig) { s.Enabled = false }), true, false},
		{"routing off", proxy(func(s *config.SetConfig) { s.Routing.Enabled = false }), true, false},
		{"interface mode", proxy(func(s *config.SetConfig) { s.Routing.Mode = config.RoutingModeInterface }), true, false},
		{"block mode", proxy(func(s *config.SetConfig) { s.Routing.Mode = config.RoutingModeBlock }), true, false},
		{"mtproto-ws mode", proxy(func(s *config.SetConfig) { s.Routing.Mode = config.RoutingModeMTProtoWS }), true, false},
		{"the bridge itself", proxy(func(s *config.SetConfig) { s.Id = config.TelegramBridgeSetID }), true, false},
		{"domain only by name", domainOnly, true, false},
		{"domain only by address", domainOnly, false, true},
		{"nil", nil, true, false},
	}
	for _, tc := range cases {
		if got := passOnEligible(tc.set, tc.byName); got != tc.want {
			t.Errorf("%s: passOnEligible = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPassOnApplies(t *testing.T) {
	proxy := func(edit func(*config.SetConfig)) *config.SetConfig {
		set := proxySetFor("s", []string{"api.telegram.org"}, nil)
		if edit != nil {
			edit(set)
		}
		return set
	}
	notRouting := proxy(func(s *config.SetConfig) {
		s.Routing.Enabled = false
		s.Routing.SourceInterfaces = []string{"br0"}
		s.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	})
	ifaces := proxy(func(s *config.SetConfig) { s.Routing.SourceInterfaces = []string{"br0"} })
	included := proxy(func(s *config.SetConfig) { s.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"} })
	excluded := proxy(func(s *config.SetConfig) {
		s.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
		s.Targets.SourceDevicesExclude = true
	})
	routerExcluded := proxy(func(s *config.SetConfig) { s.Routing.RouterTraffic = config.RouterTrafficExclude })
	const mac = "aa:bb:cc:dd:ee:01"
	cases := []struct {
		name        string
		set         *config.SetConfig
		mac         string
		routerLocal bool
		want        bool
	}{
		{"a set that does not route, device", notRouting, "", false, true},
		{"a set that does not route, router", notRouting, "", true, true},
		{"unscoped, device", proxy(nil), mac, false, true},
		{"unscoped, device without a known MAC", proxy(nil), "", false, true},
		{"unscoped, router", proxy(nil), "", true, true},
		{"source interfaces, device", ifaces, mac, false, false},
		{"source interfaces, router", ifaces, "", true, false},
		{"included devices, device", included, mac, false, true},
		{"included devices, device without a known MAC", included, "", false, false},
		{"included devices, router", included, "", true, false},
		{"excluded devices, device", excluded, mac, false, true},
		{"excluded devices, device without a known MAC", excluded, "", false, false},
		{"excluded devices, router", excluded, "", true, true},
		{"router traffic is not read for a proxy set", routerExcluded, "", true, true},
		{"nil", nil, "", false, true},
	}
	for _, tc := range cases {
		if got := passOnApplies(tc.set, tc.mac, tc.routerLocal); got != tc.want {
			t.Errorf("%s: passOnApplies = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestASetLimitedToSourceInterfacesIsSkippedForADevice(t *testing.T) {
	asLANClient(t, "aa:bb:cc:dd:ee:01")
	hello := realClientHello(t, "api.telegram.org")
	upGuest := startMockSocks(t, 0, okReply)
	upAll := startMockSocks(t, len(hello), okReply)
	guest := proxySetFor("guest", []string{"api.telegram.org"}, nil)
	guest.Routing.SourceInterfaces = []string{"br-guest"}
	all := proxySetFor("all", []string{"api.telegram.org"}, nil)
	bridge, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{guest, all},
		peerListener(t, upGuest.port(), guest), peerListener(t, upAll.port(), all))

	proxyThrough(t, bridge, hello)
	if r := upAll.next(t); !bytes.Equal(r.payload, hello) {
		t.Fatal("the hello did not reach the unscoped set's upstream intact")
	}
	noSocksRequest(t, upGuest, 100*time.Millisecond)
}

func TestADeviceWithoutAKnownMACIsNotPassedToASetWithADeviceList(t *testing.T) {
	asLANClient(t, "")
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, 0, okReply)
	set := proxySetFor("not-the-nas", []string{"api.telegram.org"}, nil)
	set.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:50"}
	set.Targets.SourceDevicesExclude = true
	b, got := workerReading(len(hello))
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{set}, peerListener(t, up.port(), set))

	proxyThrough(t, bridge, hello)
	fellBackWith(t, got, hello)
	noSocksRequest(t, up, 200*time.Millisecond)
}

func TestADeviceWithoutAKnownMACGoesPastASetWithADeviceList(t *testing.T) {
	asLANClient(t, "")
	hello := realClientHello(t, "api.telegram.org")
	upScoped := startMockSocks(t, 0, okReply)
	upAll := startMockSocks(t, len(hello), okReply)
	scoped := proxySetFor("not-the-nas", []string{"api.telegram.org"}, nil)
	scoped.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:50"}
	scoped.Targets.SourceDevicesExclude = true
	all := proxySetFor("all", []string{"api.telegram.org"}, nil)
	bridge, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{scoped, all},
		peerListener(t, upScoped.port(), scoped), peerListener(t, upAll.port(), all))

	proxyThrough(t, bridge, hello)
	if r := upAll.next(t); !bytes.Equal(r.payload, hello) {
		t.Fatal("the hello did not reach the unscoped set's upstream intact")
	}
	noSocksRequest(t, upScoped, 100*time.Millisecond)
}

func TestADeviceWithoutAKnownMACStillGetsAnUnscopedSet(t *testing.T) {
	asLANClient(t, "")
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, len(hello), okReply)
	set := proxySetFor("tg-api", []string{"api.telegram.org"}, nil)
	bridge, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{set}, peerListener(t, up.port(), set))

	proxyThrough(t, bridge, hello)
	if r := up.next(t); !bytes.Equal(r.payload, hello) {
		t.Fatal("the hello did not reach the set's upstream intact")
	}
}

func TestAUserMTProtoSetKeepsItsOwnFailOpen(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, 0, okReply)
	set := proxySetFor("tg-api", []string{"api.telegram.org"}, nil)
	b, got := workerReading(len(hello))
	bridge, names := bridgeListener(t, b, []*config.SetConfig{set}, peerListener(t, up.port(), set))
	bridge.SetID, bridge.SetName = "my-telegram", "my-telegram"

	proxyThrough(t, bridge, hello)
	if c := fellBackWith(t, got, hello); c != nil {
		if _, ok := c.(*heldBack); !ok {
			t.Fatalf("the fail-open path got a %T, want the bridge's own connection with nothing read ahead", c)
		}
	}
	if n := names.asked.Load(); n != 0 {
		t.Fatalf("a user's own mtproto-ws set consulted the other sets %d times", n)
	}
	noSocksRequest(t, up, 200*time.Millisecond)
}

func TestAConnectionOpeningWithBytesASetIsRelayingIsNotPassedBackToIt(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	up := startMockSocks(t, 0, okReply)
	set := proxySetFor("lan-upstream", []string{"api.telegram.org"}, nil)
	peer := peerListener(t, up.port(), set)
	b, got := workerReading(len(hello))
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{set}, peer)

	release := peer.holdFlight(hello)
	proxyThrough(t, bridge, hello)
	fellBackWith(t, got, hello)
	noSocksRequest(t, up, 200*time.Millisecond)
	release()

	other := realClientHello(t, "api.telegram.org")
	up2 := startMockSocks(t, len(other), okReply)
	peer2 := peerListener(t, up2.port(), set)
	bridge2, _ := bridgeListener(t, &passOnBridge{consume: 4}, []*config.SetConfig{set}, peer2)
	hold := peer2.holdFlight(hello)
	defer hold()
	proxyThrough(t, bridge2, other)
	if r := up2.next(t); !bytes.Equal(r.payload, other) {
		t.Fatal("a different client's hello was not passed on while the set relayed another")
	}
}

func startLoopingSocks(t *testing.T, target string, connects *atomic.Int32, split int) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var conns []net.Conn
	track := func(c net.Conn) {
		mu.Lock()
		conns = append(conns, c)
		mu.Unlock()
	}
	reply := func(c net.Conn, code byte) {
		_, _ = c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
	}
	serve := func(c net.Conn) {
		defer c.Close()
		head := make([]byte, 2)
		if _, err := io.ReadFull(c, head); err != nil {
			return
		}
		if _, err := io.ReadFull(c, make([]byte, head[1])); err != nil {
			return
		}
		if _, err := c.Write([]byte{5, 0}); err != nil {
			return
		}
		req := make([]byte, 4)
		if _, err := io.ReadFull(c, req); err != nil {
			return
		}
		addrLen := map[byte]int{1: 4, 4: 16}[req[3]]
		if req[3] == 3 {
			n := make([]byte, 1)
			if _, err := io.ReadFull(c, n); err != nil {
				return
			}
			addrLen = int(n[0])
		}
		if _, err := io.ReadFull(c, make([]byte, addrLen+2)); err != nil {
			return
		}
		if connects.Add(1) > 3 {
			reply(c, 1)
			return
		}
		out, err := net.Dial("tcp4", target)
		if err != nil {
			reply(c, 1)
			return
		}
		track(out)
		defer out.Close()
		reply(c, 0)
		if split > 0 {
			first := make([]byte, 64*1024)
			n, err := c.Read(first)
			if err != nil {
				return
			}
			if n > split {
				if _, err := out.Write(first[:split]); err != nil {
					return
				}
				time.Sleep(100 * time.Millisecond)
				first = first[split:n]
			} else {
				first = first[:n]
			}
			if _, err := out.Write(first); err != nil {
				return
			}
		}
		done := make(chan struct{}, 2)
		go func() {
			_, _ = io.Copy(out, c)
			_ = out.(*net.TCPConn).CloseWrite()
			done <- struct{}{}
		}()
		go func() {
			_, _ = io.Copy(c, out)
			_ = c.(*net.TCPConn).CloseWrite()
			done <- struct{}{}
		}()
		<-done
		<-done
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			track(c)
			wg.Add(1)
			go func() {
				defer wg.Done()
				serve(c)
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return ln.Addr().(*net.TCPAddr).Port
}

func throughLoopingUpstream(t *testing.T, set *config.SetConfig, split int, send []byte) (chan workerRead, *atomic.Int32) {
	t.Helper()
	b, got := workerReading(len(send))
	front, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var fwg sync.WaitGroup
	t.Cleanup(func() {
		front.Close()
		fwg.Wait()
	})
	connects := &atomic.Int32{}
	peer := peerListener(t, startLoopingSocks(t, front.Addr().String(), connects, split), set)
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{set}, peer)
	fwg.Add(1)
	go func() {
		defer fwg.Done()
		for {
			c, err := front.Accept()
			if err != nil {
				return
			}
			fwg.Add(1)
			go func() {
				defer fwg.Done()
				bridge.handle(c)
			}()
		}
	}()

	client, err := net.Dial("tcp4", front.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	if _, err := client.Write(send); err != nil {
		t.Fatal(err)
	}
	return got, connects
}

func TestALoopThroughTheSetsOwnUpstreamEndsAtItsFirstReturn(t *testing.T) {
	hello := realClientHello(t, "api.telegram.org")
	set := proxySetFor("lan-upstream", []string{"api.telegram.org"}, nil)
	got, connects := throughLoopingUpstream(t, set, 0, hello)
	fellBackWith(t, got, hello)
	time.Sleep(100 * time.Millisecond)
	if n := connects.Load(); n != 1 {
		t.Fatalf("the set's upstream got %d CONNECTs, want 1: its own connection was passed back to the set", n)
	}
}

func TestALoopIsCaughtWhenTheUpstreamSendsTheBodyInALaterRead(t *testing.T) {
	head := "POST /bot123456:AAE-test/sendMessage HTTP/1.1\r\nHost: api.telegram.org\r\nContent-Type: application/json\r\nContent-Length: 25\r\n\r\n"
	request := []byte(head + `{"chat_id":1,"text":"hi"}`)
	set := proxySetFor("lan-upstream", []string{"api.telegram.org"}, nil)
	got, connects := throughLoopingUpstream(t, set, len(head), request)
	fellBackWith(t, got, request)
	time.Sleep(200 * time.Millisecond)
	if n := connects.Load(); n != 1 {
		t.Fatalf("the set's upstream got %d CONNECTs, want 1: its own connection came back with the body in a later read and was passed back to the set", n)
	}
}

func TestAnOpeningIsKeyedUpToItsMessageBoundary(t *testing.T) {
	request := []byte("GET /bot123456:AAE-test/getMe HTTP/1.1\r\nHost: api.telegram.org\r\nAccept: */*\r\n\r\n")
	for name, first := range map[string][]byte{"a ClientHello": realClientHello(t, "api.telegram.org"), "an HTTP request": request} {
		whole, _, ok := flightKey(first)
		if !ok {
			t.Fatalf("%s was too short to track", name)
		}
		more := append(append([]byte(nil), first...), 0x17, 0x03, 0x03, 0x00, 0x05)
		if key, _, _ := flightKey(more); key != whole {
			t.Errorf("%s got another key once bytes after it were read along", name)
		}
	}
}

func TestRequestsWithTheSameHeadersAreToldApartByTheBodyReadWithThem(t *testing.T) {
	head := "POST /bot123456:AAE-test/sendMessage HTTP/1.1\r\nHost: api.telegram.org\r\nContent-Type: application/json\r\nContent-Length: 25\r\n\r\n"
	first := []byte(head + `{"chat_id":1,"text":"hi"}`)
	other := []byte(head + `{"chat_id":2,"text":"yo"}`)
	l := &Listener{}
	release := l.holdFlight(first)
	defer release()
	if l.carriesFlight(other) {
		t.Fatal("a request with the same headers and another body was taken for the held one")
	}
	for _, back := range [][]byte{[]byte(head), first[:len(head)+7], first} {
		if !l.carriesFlight(back) {
			t.Fatalf("the held request was not recognised when it came back with %d of its body bytes", len(back)-len(head))
		}
	}
	again := l.holdFlight(other)
	again()
	if !l.carriesFlight(first) {
		t.Fatal("releasing another request with the same headers released the held one too")
	}
}

type answeringSocks struct {
	ln       net.Listener
	readLen  int
	answer   []byte
	payloads chan []byte
	mu       sync.Mutex
	conns    []net.Conn
	wg       sync.WaitGroup
}

func startAnsweringSocks(t *testing.T, readLen int, answer []byte) *answeringSocks {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &answeringSocks{ln: ln, readLen: readLen, answer: answer, payloads: make(chan []byte, 8)}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns = append(s.conns, c)
			s.mu.Unlock()
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer c.Close()
				s.serve(c)
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		s.mu.Lock()
		for _, c := range s.conns {
			c.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
	return s
}

func (s *answeringSocks) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *answeringSocks) serve(c net.Conn) {
	head := make([]byte, 2)
	if _, err := io.ReadFull(c, head); err != nil {
		return
	}
	if _, err := io.ReadFull(c, make([]byte, head[1])); err != nil {
		return
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return
	}
	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil {
		return
	}
	addrLen := map[byte]int{1: 4, 4: 16}[req[3]]
	if req[3] == 3 {
		n := make([]byte, 1)
		if _, err := io.ReadFull(c, n); err != nil {
			return
		}
		addrLen = int(n[0])
	}
	if _, err := io.ReadFull(c, make([]byte, addrLen+2)); err != nil {
		return
	}
	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	payload := make([]byte, s.readLen)
	n, _ := io.ReadFull(c, payload)
	s.payloads <- payload[:n]
	if _, err := c.Write(s.answer); err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, c)
}

func (s *answeringSocks) next(t *testing.T) []byte {
	t.Helper()
	select {
	case p := <-s.payloads:
		return p
	case <-time.After(5 * time.Second):
		t.Fatal("no request reached the set's upstream")
		return nil
	}
}

func asLANDevices(t *testing.T, macs map[string]string) {
	t.Helper()
	orig := isRouterClient
	isRouterClient = func(net.IP) bool { return false }
	metrics.SetMACProvider(func(ip string) string { return macs[ip] })
	t.Cleanup(func() {
		isRouterClient = orig
		metrics.SetMACProvider(nil)
	})
}

func throughFrom(t *testing.T, l *Listener, src string, send []byte) net.Conn {
	t.Helper()
	front, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer front.Close()
	d := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(src)}}
	client, err := d.Dial("tcp4", front.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := front.Accept()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		l.handle(accepted)
		close(done)
	}()
	if _, err := client.Write(send); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("handle did not return after the client closed")
		}
	})
	return client
}

func TestTheSameRequestFromAnotherDeviceIsPassedOnOnceTheSetHasAnswered(t *testing.T) {
	request := []byte("GET /bot123456:AAE-test/getMe HTTP/1.1\r\nHost: api.telegram.org\r\nUser-Agent: curl/8.5.0\r\nAccept: */*\r\n\r\n")
	answer := []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
	asLANDevices(t, map[string]string{"127.0.0.2": "02:00:00:00:00:02", "127.0.0.3": "02:00:00:00:00:03"})
	up := startAnsweringSocks(t, len(request), answer)
	set := proxySetFor("tg-api", []string{"api.telegram.org"}, nil)
	peer := peerListener(t, up.port(), set)
	b, got := workerReading(len(request))
	bridge, _ := bridgeListener(t, b, []*config.SetConfig{set}, peer)

	first := throughFrom(t, bridge, "127.0.0.2", request)
	if p := up.next(t); !bytes.Equal(p, request) {
		t.Fatal("the first device's request reached the set's upstream altered")
	}
	_ = first.SetReadDeadline(time.Now().Add(3 * time.Second))
	reply := make([]byte, len(answer))
	if _, err := io.ReadFull(first, reply); err != nil || !bytes.Equal(reply, answer) {
		t.Fatalf("the first device did not get the set's answer: %v", err)
	}

	throughFrom(t, bridge, "127.0.0.3", request)
	select {
	case p := <-up.payloads:
		if !bytes.Equal(p, request) {
			t.Fatal("the second device's request reached the set's upstream altered")
		}
	case r := <-got:
		t.Fatalf("the second device's request was taken for the set's upstream coming back, and its %d bytes went to the bridge's fail-open path", len(r.data))
	case <-time.After(5 * time.Second):
		t.Fatal("the second device's request went nowhere")
	}
}

func TestShortFirstFlightsAreNotTracked(t *testing.T) {
	l := &Listener{}
	release := l.holdFlight([]byte("GET "))
	defer release()
	if l.carriesFlight([]byte("GET ")) {
		t.Fatal("a 4-byte opening was tracked as a connection's first bytes")
	}
	long := bytes.Repeat([]byte{0x16}, flightBytes+10)
	hold := l.holdFlight(long)
	if !l.carriesFlight(append(append([]byte(nil), long[:flightBytes]...), 0xff, 0xff)) {
		t.Fatal("two openings that agree over the compared bytes were told apart")
	}
	hold()
	if l.carriesFlight(long) {
		t.Fatal("a released opening was still tracked")
	}
}

func TestReplayConnHandsTheBytesBackBeforeTheSocket(t *testing.T) {
	p := acceptedPair(t)
	defer p.client.Close()
	defer p.accepted.Close()
	if _, err := p.client.Write([]byte("rest")); err != nil {
		t.Fatal(err)
	}
	c := &replayConn{Conn: p.accepted, pending: []byte("head-")}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, len("head-rest"))
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "head-rest" {
		t.Fatalf("read %q, want the pending bytes first", got)
	}
}

func TestReplayConnPassesTheHalfCloseOn(t *testing.T) {
	p := acceptedPair(t)
	defer p.client.Close()
	defer p.accepted.Close()
	c := &replayConn{Conn: p.accepted, pending: []byte("unread")}
	if err := c.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	_ = p.client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := p.client.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("the peer read %v after CloseWrite, want EOF", err)
	}
}

func TestTheUserTimeoutReachesTheSocketBehindWrappers(t *testing.T) {
	p := acceptedPair(t)
	defer p.client.Close()
	defer p.accepted.Close()
	setTCPUserTimeout(&replayConn{Conn: &heldBack{Conn: p.accepted}}, failOpenUserTimeout)
	raw, err := p.accepted.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var got int
	var gerr error
	if err := raw.Control(func(fd uintptr) {
		got, gerr = unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT)
	}); err != nil || gerr != nil {
		t.Fatal(err, gerr)
	}
	if want := int(failOpenUserTimeout.Milliseconds()); got != want {
		t.Fatalf("TCP_USER_TIMEOUT is %d ms, want %d", got, want)
	}
}
