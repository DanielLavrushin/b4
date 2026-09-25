package tproxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/socks5"
)

type socksRequest struct {
	atyp    byte
	host    string
	port    int
	payload []byte
}

type mockSocks struct {
	ln      net.Listener
	reqs    chan socksRequest
	reply   func(atyp byte) byte
	readLen int
	wg      sync.WaitGroup
}

func startMockSocks(t *testing.T, readLen int, reply func(atyp byte) byte) *mockSocks {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := &mockSocks{ln: ln, reqs: make(chan socksRequest, 8), reply: reply, readLen: readLen}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			m.wg.Add(1)
			go func() {
				defer m.wg.Done()
				m.serve(c)
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		m.wg.Wait()
	})
	return m
}

func (m *mockSocks) port() int { return m.ln.Addr().(*net.TCPAddr).Port }

func (m *mockSocks) serve(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
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
	r := socksRequest{atyp: req[3]}
	switch req[3] {
	case 1:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(c, ip); err != nil {
			return
		}
		r.host = net.IP(ip).String()
	case 3:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return
		}
		name := make([]byte, l[0])
		if _, err := io.ReadFull(c, name); err != nil {
			return
		}
		r.host = string(name)
	default:
		return
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(c, port); err != nil {
		return
	}
	r.port = int(binary.BigEndian.Uint16(port))
	code := m.reply(r.atyp)
	if _, err := c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	if code == 0 && m.readLen > 0 {
		r.payload = make([]byte, m.readLen)
		n, _ := io.ReadFull(c, r.payload)
		r.payload = r.payload[:n]
	}
	m.reqs <- r
}

func (m *mockSocks) next(t *testing.T) socksRequest {
	t.Helper()
	select {
	case r := <-m.reqs:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no request reached the upstream")
		return socksRequest{}
	}
}

type fakeNames struct {
	names   dns.NameMatches
	learned string
	inSet   []string
}

func (f fakeNames) ObservedNames(client, dst net.IP) dns.NameMatches { return f.names }
func (f fakeNames) LearnedName(dst net.IP, setID string) string      { return f.learned }
func (f fakeNames) SetHasDomain(setID, host string) bool             { return containsHost(f.inSet, host) }
func (f fakeNames) WantNames(bool)                                   {}

func newTestListener(t *testing.T, upstreamPort int, names fakeNames) *Listener {
	t.Helper()
	l := &Listener{
		SetID:     "set-1",
		SetName:   "test",
		Upstream:  socks5.ClientConfig{Host: "127.0.0.1", Port: upstreamPort, Timeout: 2 * time.Second},
		UseDomain: true,
		Names:     names,
	}
	set := &config.SetConfig{}
	set.Targets.DomainsToMatch = names.inSet
	l.set.Store(set)
	l.ctx, l.cancel = context.WithCancel(context.Background())
	t.Cleanup(l.cancel)
	return l
}

func proxyThrough(t *testing.T, l *Listener, send []byte) {
	t.Helper()
	dst, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	client, err := net.Dial("tcp4", dst.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := dst.Accept()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		l.handle(accepted)
		close(done)
	}()
	if len(send) > 0 {
		if _, err := client.Write(send); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		client.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("handle did not return after the client closed")
		}
	})
}

func TestListenerSendsTheSniffedNameTheSetLists(t *testing.T) {
	hello := realClientHello(t, "api.ipify.org")
	up := startMockSocks(t, len(hello), func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{inSet: []string{"api.ipify.org"}})

	proxyThrough(t, l, hello)
	r := up.next(t)
	if r.atyp != 3 || r.host != "api.ipify.org" {
		t.Fatalf("CONNECT asked for atyp %d host %q, want the name api.ipify.org", r.atyp, r.host)
	}
	if !bytes.Equal(r.payload, hello) {
		t.Fatalf("the upstream got %d bytes after CONNECT, want the client's %d-byte hello intact", len(r.payload), len(hello))
	}
}

func TestListenerSendsTheClientsOwnResolvedNameOverADecoySNI(t *testing.T) {
	hello := realClientHello(t, "www.microsoft.com")
	up := startMockSocks(t, len(hello), func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{names: dns.NameMatches{Own: []string{"vps.example.net"}}})

	proxyThrough(t, l, hello)
	r := up.next(t)
	if r.atyp != 3 || r.host != "vps.example.net" {
		t.Fatalf("CONNECT asked for atyp %d host %q, want vps.example.net", r.atyp, r.host)
	}
	if !bytes.Equal(r.payload, hello) {
		t.Fatal("the hello did not reach the upstream intact")
	}
}

func TestListenerKeepsTheAddressForAnUnlistedSNI(t *testing.T) {
	hello := realClientHello(t, "www.microsoft.com")
	up := startMockSocks(t, len(hello), func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{inSet: []string{"api.ipify.org"}})

	proxyThrough(t, l, hello)
	r := up.next(t)
	if r.atyp != 1 || r.host != "127.0.0.1" {
		t.Fatalf("CONNECT asked for atyp %d host %q, want the address", r.atyp, r.host)
	}
	if !bytes.Equal(r.payload, hello) {
		t.Fatal("the hello did not reach the upstream intact")
	}
}

func TestListenerPrefersTheSNIOverAnotherDevicesName(t *testing.T) {
	hello := realClientHello(t, "www.youtube.com")
	up := startMockSocks(t, len(hello), func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{
		names: dns.NameMatches{Others: []string{"mail.google.com"}},
		inSet: []string{"www.youtube.com"},
	})

	proxyThrough(t, l, hello)
	if r := up.next(t); r.atyp != 3 || r.host != "www.youtube.com" {
		t.Fatalf("CONNECT asked for atyp %d host %q, want the client's own SNI", r.atyp, r.host)
	}
}

func TestListenerRetriesWithTheAddressWhenTheNameIsRefused(t *testing.T) {
	up := startMockSocks(t, 0, func(atyp byte) byte {
		if atyp == 3 {
			return 4
		}
		return 0
	})
	l := newTestListener(t, up.port(), fakeNames{names: dns.NameMatches{Own: []string{"only.example.com"}}})

	proxyThrough(t, l, nil)
	first, second := up.next(t), up.next(t)
	if first.atyp != 3 || first.host != "only.example.com" {
		t.Fatalf("first CONNECT: atyp %d host %q", first.atyp, first.host)
	}
	if second.atyp != 1 || second.host != "127.0.0.1" {
		t.Fatalf("retry CONNECT: atyp %d host %q, want the address", second.atyp, second.host)
	}
	if l.upstreamFails.Load() != 0 {
		t.Fatalf("a refused name that worked as an address counted %d upstream failures", l.upstreamFails.Load())
	}
}

func TestListenerSendsTheAddressWhenUseDomainIsOff(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{names: dns.NameMatches{Own: []string{"only.example.com"}}})
	l.UseDomain = false

	proxyThrough(t, l, nil)
	if r := up.next(t); r.atyp != 1 {
		t.Fatalf("use_domain off still sent atyp %d host %q", r.atyp, r.host)
	}
}

func TestListenerKeepsTheAddressForAPinnedName(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{names: dns.NameMatches{Own: []string{"pinned.example.com"}}})
	set := &config.SetConfig{}
	set.DNS.Pins = map[string][]string{"pinned.example.com": {"203.0.113.7"}}
	l.set.Store(set)

	proxyThrough(t, l, nil)
	if r := up.next(t); r.atyp != 1 {
		t.Fatalf("a pinned name was sent upstream: atyp %d host %q", r.atyp, r.host)
	}
}

func lanHostListener(t *testing.T, ips ...string) *Listener {
	t.Helper()
	l := &Listener{SetID: "set-1", SetName: "lan"}
	set := &config.SetConfig{}
	set.Targets.IPs = ips
	l.set.Store(set)
	l.guard = &loopGuard{
		upstreamIPs:  []net.IP{net.ParseIP("192.168.1.50")},
		upstreamPort: 1080,
		procRoot:     t.TempDir(),
		locals:       fixedAddrs("192.168.1.1/24"),
	}
	return l
}

func TestAnUpstreamOnAnotherDeviceKeepsItsOwnTrafficProxiedUnlessItLoops(t *testing.T) {
	box, phone := net.ParseIP("192.168.1.50"), net.ParseIP("192.168.1.60")
	l := lanHostListener(t, "149.154.160.0/20")
	keys := relayKeys("tcp", 443, "149.154.167.51", "web.telegram.org")

	if l.relayedForUpstreamHost(box, keys) {
		t.Fatal("the upstream device's own connection was sent direct with nothing relayed to that destination")
	}
	release := l.holdRelay(relayKeys("tcp", 443, "149.154.167.99", "web.telegram.org"))
	if !l.relayedForUpstreamHost(box, keys) {
		t.Fatal("the upstream device reaching a destination b4 is relaying through it was not recognised")
	}
	if l.relayedForUpstreamHost(phone, keys) {
		t.Fatal("another device was taken for the upstream")
	}
	release()
	if l.relayedForUpstreamHost(box, keys) {
		t.Fatal("a finished relay still counted")
	}
	if l.coversEverything() {
		t.Fatal("a narrow set was taken for a catch-all")
	}
}

func TestACatchAllSetSendsTheUpstreamDevicesTrafficDirect(t *testing.T) {
	l := lanHostListener(t, "0.0.0.0/0")
	if !l.coversEverything() {
		t.Fatal("0.0.0.0/0 is a catch-all")
	}
	owner, loops := l.upstreamLoop(tcpAddr("192.168.1.50:40000"), tcpAddr("20.215.224.201:4455"))
	if !loops || owner != "host 192.168.1.50" {
		t.Fatalf("got (%q, %v)", owner, loops)
	}
	if _, loops := l.upstreamLoop(tcpAddr("192.168.1.60:40000"), tcpAddr("20.215.224.201:4455")); loops {
		t.Fatal("another device's connection was sent direct")
	}
}
