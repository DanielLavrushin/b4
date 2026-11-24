package dns

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type freezingProxy struct {
	ln      net.Listener
	backend string

	mu       sync.Mutex
	accepted int
	frozen   chan struct{}
	conns    []net.Conn
	wg       sync.WaitGroup
}

func startFreezingProxy(t *testing.T, backend string) *freezingProxy {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &freezingProxy{ln: ln, backend: backend, frozen: make(chan struct{})}
	p.wg.Add(1)
	go p.serve()
	t.Cleanup(func() {
		ln.Close()
		p.mu.Lock()
		for _, c := range p.conns {
			c.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
	})
	return p
}

func (p *freezingProxy) serve() {
	defer p.wg.Done()
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return
		}
		server, err := net.Dial("tcp4", p.backend)
		if err != nil {
			client.Close()
			continue
		}
		p.mu.Lock()
		p.accepted++
		p.conns = append(p.conns, client, server)
		frozen := p.frozen
		p.mu.Unlock()
		p.wg.Add(2)
		go p.pipe(server, client, frozen)
		go p.pipe(client, server, frozen)
	}
}

func (p *freezingProxy) pipe(dst, src net.Conn, frozen chan struct{}) {
	defer p.wg.Done()
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if err != nil {
			dst.Close()
			return
		}
		select {
		case <-frozen:
			continue
		default:
		}
		if _, err := dst.Write(buf[:n]); err != nil {
			return
		}
	}
}

func (p *freezingProxy) freeze() {
	p.mu.Lock()
	defer p.mu.Unlock()
	close(p.frozen)
	p.frozen = make(chan struct{})
}

func (p *freezingProxy) connections() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.accepted
}

func http2DoH(t *testing.T, pingIdle, pingTimeout, timeout time.Duration) (*freezingProxy, func() error) {
	t.Helper()
	proxy, client, url := http2DoHClient(t, pingIdle, pingTimeout, timeout)
	return proxy, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		answer, err := ResolveSetDoH(ctx, client, url, BuildQuery("www.example.com", 7, rrTypeA))
		if err == nil && len(ParseResponseIPs(answer)) != 1 {
			err = errors.New("the answer carries no address")
		}
		return err
	}
}

func http2DoHClient(t *testing.T, pingIdle, pingTimeout, timeout time.Duration) (*freezingProxy, *http.Client, string) {
	t.Helper()
	previousIdle, previousTimeout := dohPingIdle, dohPingTimeout
	dohPingIdle, dohPingTimeout = pingIdle, pingTimeout
	t.Cleanup(func() { dohPingIdle, dohPingTimeout = previousIdle, previousTimeout })

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			http.Error(w, "the test needs HTTP/2", http.StatusHTTPVersionNotSupported)
			return
		}
		query, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", DoHContentType)
		_, _ = w.Write(BuildAnswerFromIPs(query, 60, []net.IP{net.IPv4(203, 0, 113, 9)}))
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)
	proxy := startFreezingProxy(t, ts.Listener.Addr().String())

	client := MarkedDoHClient(0, timeout)
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{
		RootCAs:    ts.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs,
		ServerName: "example.com",
	}
	t.Cleanup(client.CloseIdleConnections)

	return proxy, client, "https://" + proxy.ln.Addr().String() + "/dns-query"
}

func TestADeadDoHConnectionIsReplaced(t *testing.T) {
	proxy, client, url := http2DoHClient(t, 100*time.Millisecond, 2*time.Second, 10*time.Second)
	ask := func(timeout time.Duration) error {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		_, err := ResolveSetDoH(ctx, client, url, BuildQuery("www.example.com", 7, rrTypeA))
		return err
	}

	if err := ask(10 * time.Second); err != nil {
		t.Fatalf("the resolver must answer before its connection dies: %v", err)
	}
	if n := proxy.connections(); n != 1 {
		t.Fatalf("%d connections after one query, want 1", n)
	}

	proxy.freeze()
	if err := ask(200 * time.Millisecond); err == nil {
		t.Fatal("a query over the dead connection was answered")
	}
	if err := ask(10 * time.Second); err != nil {
		t.Fatalf("the resolver is reachable, yet a query goes to the dead connection: %v", err)
	}
	if n := proxy.connections(); n != 2 {
		t.Fatalf("%d connections, want one new connection in place of the dead one", n)
	}
}

func TestALiveDoHConnectionOutlastsItsPings(t *testing.T) {
	proxy, ask := http2DoH(t, 50*time.Millisecond, 2*time.Second, 2*time.Second)

	for i := 0; i < 3; i++ {
		if err := ask(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if n := proxy.connections(); n != 1 {
		t.Fatalf("%d connections, want the live one to be kept", n)
	}
}

func TestAQueryWaitingOnADeadDoHConnectionIsAskedAgain(t *testing.T) {
	proxy, client, url := http2DoHClient(t, 100*time.Millisecond, 2*time.Second, 10*time.Second)
	var mu sync.Mutex
	var reused []bool
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotConn: func(got httptrace.GotConnInfo) {
		mu.Lock()
		defer mu.Unlock()
		reused = append(reused, got.Reused)
	}})
	ask := func() error {
		_, err := ResolveSetDoH(ctx, client, url, BuildQuery("www.example.com", 7, rrTypeA))
		return err
	}

	if err := ask(); err != nil {
		t.Fatal(err)
	}
	proxy.freeze()
	if err := ask(); err != nil {
		t.Fatalf("the resolver is reachable, yet the query failed with the connection it waited on: %v", err)
	}
	if want := []bool{false, true, false}; !slices.Equal(reused, want) {
		t.Fatalf("connections reused %v, want %v: the old one, then one new in its place", reused, want)
	}
	if n := proxy.connections(); n != 2 {
		t.Fatalf("%d connections, want one new connection in place of the dead one", n)
	}
}

func TestOnlyTheLookupsOfASetAreAskedAgain(t *testing.T) {
	var asked atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, _ := io.ReadAll(r.Body)
		if asked.Add(1) == 2 {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
			return
		}
		_, _ = w.Write(BuildAnswerFromIPs(query, 60, []net.IP{net.IPv4(203, 0, 113, 9)}))
	}))
	t.Cleanup(ts.Close)
	client := MarkedDoHClient(0, 10*time.Second)
	t.Cleanup(client.CloseIdleConnections)
	ask := func() error {
		_, err := ResolveDoH(context.Background(), client, ts.URL+"/dns-query", BuildQuery("www.example.com", 7, rrTypeA))
		return err
	}

	if err := ask(); err != nil {
		t.Fatal(err)
	}
	if err := ask(); err == nil {
		t.Fatal("a measurement over a connection that died was repeated on another one")
	}
	if n := asked.Load(); n != 2 {
		t.Fatalf("%d queries reached the server, want the lost one asked once", n)
	}
}

func TestADeadDoHConnectionIsGivenUpBeforeTheQueryIs(t *testing.T) {
	proxy, ask := http2DoH(t, 100*time.Millisecond, 300*time.Millisecond, 5*time.Second)

	if err := ask(); err != nil {
		t.Fatal(err)
	}
	proxy.freeze()
	if err := ask(); err != nil {
		t.Fatalf("the query ran out of time together with its connection: %v", err)
	}
}

func TestAShortQueryTimeoutEndsADeadDoHConnectionSooner(t *testing.T) {
	proxy, ask := http2DoH(t, 100*time.Millisecond, time.Minute, 400*time.Millisecond)

	if err := ask(); err != nil {
		t.Fatal(err)
	}
	proxy.freeze()
	for start := time.Now(); time.Since(start) < 5*time.Second; {
		if ask() == nil {
			return
		}
	}
	t.Fatal("the resolver is reachable, yet queries still go to the dead connection")
}

func TestAQueryThatTimedOutIsNotAskedAgain(t *testing.T) {
	proxy, client, url := http2DoHClient(t, time.Minute, time.Second, time.Second)
	ask := func() error {
		_, err := ResolveSetDoH(context.Background(), client, url, BuildQuery("www.example.com", 7, rrTypeA))
		return err
	}

	if err := ask(); err != nil {
		t.Fatal(err)
	}
	proxy.freeze()
	start := time.Now()
	if err := ask(); err == nil {
		t.Fatal("a query over the dead connection was answered")
	}
	if took := time.Since(start); took > 1700*time.Millisecond {
		t.Errorf("the query took %s, want its own second and no second try", took)
	}
}

func TestAFailedNewDoHConnectionIsNotTriedTwice(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan struct{}, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- struct{}{}
			conn.Close()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		<-done
	})

	client := MarkedDoHClient(0, time.Second)
	t.Cleanup(client.CloseIdleConnections)
	if _, err := ResolveSetDoH(context.Background(), client, "https://"+ln.Addr().String()+"/dns-query", BuildQuery("www.example.com", 7, rrTypeA)); err == nil {
		t.Fatal("a server that hangs up answered")
	}
	if n := len(accepted); n != 1 {
		t.Errorf("%d connections, want one", n)
	}
}

func TestTheLookupsB4MakesForASetAreAskedAgain(t *testing.T) {
	var asked atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, _ := io.ReadAll(r.Body)
		if asked.Add(1) == 2 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		_, _ = w.Write(BuildAnswerFromIPs(query, 60, []net.IP{net.IPv4(203, 0, 113, 9)}))
	}))
	t.Cleanup(ts.Close)
	srv := Server{Source: ts.URL, DoHURL: ts.URL + "/dns-query", Timeout: 1234 * time.Millisecond}
	t.Cleanup(lookupDoHClient(0, srv.Timeout).CloseIdleConnections)

	for i := 0; i < 2; i++ {
		ips, err := LookupIPs(context.Background(), srv, "www.example.com", true, false)
		if err != nil || len(ips) != 1 {
			t.Fatalf("lookup %d: %v %v", i+1, ips, err)
		}
	}
	if n := asked.Load(); n != 3 {
		t.Fatalf("%d queries reached the server, want the lost one asked once more", n)
	}
}
