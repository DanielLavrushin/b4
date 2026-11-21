package tproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type answerTestBackend struct {
	net.Conn
	answered   atomic.Int32
	closed     atomic.Bool
	halfClosed chan struct{}
	once       sync.Once
}

func (b *answerTestBackend) Close() error {
	b.closed.Store(true)
	return b.Conn.Close()
}

func newAnswerTestBackend() (*answerTestBackend, net.Conn) {
	backend, server := net.Pipe()
	return &answerTestBackend{Conn: backend, halfClosed: make(chan struct{})}, server
}

func (b *answerTestBackend) Answered() { b.answered.Add(1) }

func (b *answerTestBackend) CloseWrite() error {
	b.once.Do(func() { close(b.halfClosed) })
	return nil
}

func reuseListener(t *testing.T, warm WSReuseDialer) *Listener {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	set := config.NewSetConfig()
	set.Enabled = true
	set.TCP.WSSizeReuse.Enabled = true
	l := &Listener{ctx: ctx, SetName: "reuse", Warm: warm}
	l.set.Store(&set)
	return l
}

func passOnly(pass net.Conn) reuseTestDialer {
	return reuseTestDialer{dial: func(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error) {
		return pass, nil
	}}
}

func TestWSReuseAnswerReachingTheClientMarksThePass(t *testing.T) {
	pass, server := newAnswerTestBackend()
	defer server.Close()
	l := reuseListener(t, passOnly(pass))
	a, b := net.Pipe()
	defer a.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.serveWSReuse(reuseTestClient{b}, net.ParseIP("203.0.113.2"), 443, &sniffResult{host: "video.example.org", tlsVersion: 0x0304, prefix: []byte("hello")})
	}()
	server.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(server, make([]byte, 5)); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = server.Write([]byte("reply")) }()
	a.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(a, make([]byte, 5)); err != nil {
		t.Fatalf("the pass's answer did not reach the client: %v", err)
	}
	a.Close()
	server.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the relay did not end with the client")
	}
	if got := pass.answered.Load(); got != 1 {
		t.Fatalf("the server's answer reached the client but the pass was marked answered %d times, want once, so its tuple is kept", got)
	}
}

func TestWSReuseServerBytesAfterTheClientLeftAreNoAnswer(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	app, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	client, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	pass, server := newAnswerTestBackend()
	defer server.Close()
	l := reuseListener(t, passOnly(pass))
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.serveWSReuse(client, net.ParseIP("203.0.113.2"), 443, &sniffResult{host: "video.example.org", tlsVersion: 0x0304, prefix: []byte("hello")})
	}()
	server.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(server, make([]byte, 5)); err != nil {
		t.Fatal(err)
	}
	app.Close()
	select {
	case <-pass.halfClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("the relay did not half-close the pass after the client left")
	}
	if _, err := server.Write([]byte("answer after the client left")); err != nil {
		t.Fatalf("the relay stopped reading the pass after the client left: %v", err)
	}
	server.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the relay did not end")
	}
	if got := pass.answered.Load(); got != 0 {
		t.Fatalf("server bytes that came after the client had left marked the pass answered %d times, so a hung tuple would be kept", got)
	}
}

type silentPassDialer struct {
	pass      net.Conn
	fallbacks atomic.Int32
	servers   chan net.Conn
	dialing   func()
}

func newSilentPassDialer(pass net.Conn) *silentPassDialer {
	return &silentPassDialer{pass: pass, servers: make(chan net.Conn, 1)}
}

func (d *silentPassDialer) Dial(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error) {
	return d.pass, nil
}

func (d *silentPassDialer) Fallback(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error) {
	d.fallbacks.Add(1)
	if d.dialing != nil {
		d.dialing()
	}
	c, s := net.Pipe()
	d.servers <- s
	return fallbackTestConn{c}, nil
}

func (d *silentPassDialer) AcquireClient() (func(), bool) { return func() {}, true }

func shortPassWindow(t *testing.T, window time.Duration) {
	t.Helper()
	previous := passAnswerTimeout
	passAnswerTimeout = window
	t.Cleanup(func() { passAnswerTimeout = previous })
}

func servePass(l *Listener, client net.Conn, prefix []byte) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.serveWSReuse(client, net.ParseIP("203.0.113.2"), 443, &sniffResult{host: "video.example.org", tlsVersion: 0x0304, prefix: prefix})
	}()
	return done
}

func waitDone(t *testing.T, done chan struct{}, limit time.Duration, why string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatal(why)
	}
}

func nextFallback(t *testing.T, d *silentPassDialer, limit time.Duration) net.Conn {
	t.Helper()
	select {
	case s := <-d.servers:
		t.Cleanup(func() { s.Close() })
		return s
	case <-time.After(limit):
		t.Fatalf("the pass never answered and no relay with the set's strategy was dialed within %s, so the client is left waiting for its own timeout", limit)
		return nil
	}
}

func answerAfterHello(t *testing.T, server net.Conn, hello []byte, answer string) chan []byte {
	t.Helper()
	replayed := make(chan []byte, 1)
	go func() {
		buf := make([]byte, len(hello))
		_ = server.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := io.ReadFull(server, buf); err != nil {
			replayed <- nil
			return
		}
		replayed <- buf
		_, _ = server.Write([]byte(answer))
	}()
	return replayed
}

func readUntilQuiet(c net.Conn, quiet time.Duration) []byte {
	var got []byte
	buf := make([]byte, 256)
	for {
		_ = c.SetReadDeadline(time.Now().Add(quiet))
		n, err := c.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			return got
		}
	}
}

func TestWSReuseSilentPassFallsBackWithTheHello(t *testing.T) {
	logged := captureWarnings(t)
	shortPassWindow(t, 200*time.Millisecond)
	pass, passServer := newAnswerTestBackend()
	defer passServer.Close()
	d := newSilentPassDialer(pass)
	l := reuseListener(t, d)
	hello := realClientHello(t, "video.example.org")
	a, b := net.Pipe()
	defer a.Close()
	done := servePass(l, reuseTestClient{b}, hello)
	passServer.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(passServer, make([]byte, len(hello))); err != nil {
		t.Fatal(err)
	}
	fallback := nextFallback(t, d, 3*time.Second)
	replayed := answerAfterHello(t, fallback, hello, "ok")
	a.SetDeadline(time.Now().Add(2 * time.Second))
	reply := make([]byte, 2)
	if _, err := io.ReadFull(a, reply); err != nil || string(reply) != "ok" {
		t.Fatalf("the client did not get the answer of the relay with the set's strategy: %q %v", reply, err)
	}
	if got := <-replayed; !bytes.Equal(got, hello) {
		t.Fatal("the relay with the set's strategy did not get the client's hello")
	}
	if n, err := passServer.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("the silent pass was left open: read %d bytes, %v", n, err)
	}
	if pass.answered.Load() != 0 {
		t.Fatal("a silent pass was marked answered, so its tuple would be kept")
	}
	a.Close()
	fallback.Close()
	waitDone(t, done, 2*time.Second, "the relay did not end with the client")
	if d.fallbacks.Load() != 1 || logged.lines("[wssize-reuse-fallback]") != 1 || logged.lines("[wssize-reuse]") != 0 {
		t.Fatalf("%d relays with the set's strategy, log %q; want one connection line flagged as a reuse fallback", d.fallbacks.Load(), logged.buf.String())
	}
	if n := logged.warnings("no answer to the hello"); n != 1 {
		t.Fatalf("%d warnings for a silent pass, want 1", n)
	}
}

func TestWSReuseAnsweredPassIsKeptPastTheWindow(t *testing.T) {
	logged := captureWarnings(t)
	shortPassWindow(t, 200*time.Millisecond)
	pass, passServer := newAnswerTestBackend()
	defer passServer.Close()
	d := newSilentPassDialer(pass)
	l := reuseListener(t, d)
	hello := realClientHello(t, "video.example.org")
	a, b := net.Pipe()
	defer a.Close()
	done := servePass(l, reuseTestClient{b}, hello)
	passServer.SetDeadline(time.Now().Add(5 * time.Second))
	a.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(passServer, make([]byte, len(hello))); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = passServer.Write([]byte("server hello")) }()
	first := make([]byte, len("server hello"))
	if _, err := io.ReadFull(a, first); err != nil || string(first) != "server hello" {
		t.Fatalf("the pass's answer within the window did not reach the client: %q %v", first, err)
	}
	time.Sleep(3 * passAnswerTimeout)
	go func() { _, _ = a.Write([]byte("finished")) }()
	finished := make([]byte, len("finished"))
	if _, err := io.ReadFull(passServer, finished); err != nil || string(finished) != "finished" {
		t.Fatalf("after the window the client's bytes did not reach the pass: %q %v", finished, err)
	}
	go func() { _, _ = passServer.Write([]byte("data")) }()
	data := make([]byte, len("data"))
	if _, err := io.ReadFull(a, data); err != nil || string(data) != "data" {
		t.Fatalf("after the window the pass's bytes did not reach the client: %q %v", data, err)
	}
	a.Close()
	passServer.Close()
	waitDone(t, done, 2*time.Second, "the relay did not end with the client")
	if d.fallbacks.Load() != 0 || pass.answered.Load() != 1 {
		t.Fatalf("a pass answered within the window gave %d relays with the set's strategy and was marked answered %d times, want 0 and 1", d.fallbacks.Load(), pass.answered.Load())
	}
	if logged.lines("[wssize-reuse]") != 1 || logged.lines("[wssize-reuse-fallback]") != 0 {
		t.Fatalf("an answered pass was not logged once as a reuse relay: %s", logged.buf.String())
	}
}

func TestWSReuseAnswerJustAfterTheWindowIsLost(t *testing.T) {
	shortPassWindow(t, 200*time.Millisecond)
	pass, passServer := newAnswerTestBackend()
	defer passServer.Close()
	late := make(chan error, 1)
	closedAtDial := make(chan bool, 1)
	d := newSilentPassDialer(pass)
	d.dialing = func() {
		closedAtDial <- pass.closed.Load()
		go func() {
			_, err := passServer.Write([]byte("late pass answer"))
			late <- err
		}()
	}
	l := reuseListener(t, d)
	hello := realClientHello(t, "video.example.org")
	a, b := net.Pipe()
	defer a.Close()
	done := servePass(l, reuseTestClient{b}, hello)
	passServer.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(passServer, make([]byte, len(hello))); err != nil {
		t.Fatal(err)
	}
	fallback := nextFallback(t, d, 3*time.Second)
	if !<-closedAtDial {
		t.Fatal("the silent pass was still open when the relay with the set's strategy was dialed")
	}
	select {
	case err := <-late:
		if err == nil {
			t.Fatal("the pass took the server's late answer after the window")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the server's late answer was still pending on the pass")
	}
	answerAfterHello(t, fallback, hello, "ok")
	if got := readUntilQuiet(a, 300*time.Millisecond); string(got) != "ok" {
		t.Fatalf("the client got %q, want only the answer of the relay with the set's strategy", got)
	}
	a.Close()
	fallback.Close()
	waitDone(t, done, 2*time.Second, "the relay did not end with the client")
}

func TestWSReuseHelloWithEarlyDataIsNotReplayed(t *testing.T) {
	shortPassWindow(t, 100*time.Millisecond)
	pass, passServer := newAnswerTestBackend()
	defer passServer.Close()
	d := newSilentPassDialer(pass)
	l := reuseListener(t, d)
	prefix := append(realClientHello(t, "video.example.org"), 0x17, 0x03, 0x03, 0x00, 0x05, 'e', 'a', 'r', 'l', 'y')
	a, b := net.Pipe()
	defer a.Close()
	done := servePass(l, reuseTestClient{b}, prefix)
	passServer.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(passServer, make([]byte, len(prefix))); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * passAnswerTimeout)
	go func() { _, _ = passServer.Write([]byte("late answer")) }()
	a.SetDeadline(time.Now().Add(2 * time.Second))
	reply := make([]byte, len("late answer"))
	if _, err := io.ReadFull(a, reply); err != nil || string(reply) != "late answer" {
		t.Fatalf("a hello sent with early data left the pass before its late answer: %q %v", reply, err)
	}
	if d.fallbacks.Load() != 0 {
		t.Fatalf("a hello sent with early data was replayed %d times through the set's strategy", d.fallbacks.Load())
	}
	a.Close()
	passServer.Close()
	waitDone(t, done, 2*time.Second, "the relay did not end with the client")
}

func TestWSReuseServerClosingWithoutAnswerIsNotReplayed(t *testing.T) {
	logged := captureWarnings(t)
	shortPassWindow(t, 200*time.Millisecond)
	pass, passServer := newAnswerTestBackend()
	d := newSilentPassDialer(pass)
	l := reuseListener(t, d)
	hello := realClientHello(t, "video.example.org")
	a, b := net.Pipe()
	defer a.Close()
	done := servePass(l, reuseTestClient{b}, hello)
	passServer.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(passServer, make([]byte, len(hello))); err != nil {
		t.Fatal(err)
	}
	passServer.Close()
	time.Sleep(3 * passAnswerTimeout)
	if n := d.fallbacks.Load(); n != 0 {
		t.Fatalf("a pass the server closed without an answer was replayed %d times through the set's strategy", n)
	}
	a.Close()
	waitDone(t, done, 2*time.Second, "the relay did not end with the client")
	if pass.answered.Load() != 0 {
		t.Fatal("a pass the server closed without an answer was marked answered")
	}
	if logged.lines("[wssize-reuse]") != 1 || logged.lines("[wssize-reuse-fallback]") != 0 {
		t.Fatalf("a pass the server closed without an answer was not logged once as a reuse relay: %s", logged.buf.String())
	}
}

func TestWSReuseClientLeavingDuringTheWatchEndsThePass(t *testing.T) {
	logged := captureWarnings(t)
	shortPassWindow(t, 3*time.Second)
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	app, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	client, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	pass, passServer := newAnswerTestBackend()
	defer passServer.Close()
	d := newSilentPassDialer(pass)
	l := reuseListener(t, d)
	hello := realClientHello(t, "video.example.org")
	done := servePass(l, client, hello)
	passServer.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(passServer, make([]byte, len(hello))); err != nil {
		t.Fatal(err)
	}
	app.Close()
	waitDone(t, done, 1500*time.Millisecond, "the pass kept waiting for an answer after its client had left")
	if n, err := passServer.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("the pass of a client that left was not closed: read %d bytes, %v", n, err)
	}
	if d.fallbacks.Load() != 0 || pass.answered.Load() != 0 {
		t.Fatalf("a client that left during the watch gave %d relays with the set's strategy and an answered mark %d, want 0 and 0", d.fallbacks.Load(), pass.answered.Load())
	}
	if logged.lines("[wssize-reuse]") != 1 {
		t.Fatalf("a pass whose client left during the watch was not logged once: %s", logged.buf.String())
	}
}
