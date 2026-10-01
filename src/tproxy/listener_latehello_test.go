package tproxy

import (
	"errors"
	"io"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"
)

type connPair struct {
	client, accepted net.Conn
}

func acceptedPair(t *testing.T) connPair {
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
		client.Close()
		t.Fatal(err)
	}
	return connPair{client: client, accepted: accepted}
}

func handleInBackground(t *testing.T, l *Listener, p connPair) chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		l.handle(p.accepted)
		close(done)
	}()
	t.Cleanup(func() {
		p.client.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("handle did not return after the client closed")
		}
	})
	return done
}

func startSilentUpstream(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range held {
			c.Close()
		}
		mu.Unlock()
	})
	return ln.Addr().(*net.TCPAddr).Port
}

func TestProxyLineNamesAHelloThatArrivesDuringTheDial(t *testing.T) {
	lines := watchConnLines(t)
	hello := realClientHello(t, "www.formula1.com")
	p := acceptedPair(t)
	up := startMockSocks(t, len(hello), func(byte) byte {
		if _, err := p.client.Write(hello); err != nil {
			return 1
		}
		deadline := time.Now().Add(2 * time.Second)
		for len(peekClient(p.accepted, sniffMaxBytes)) < len(hello) && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		return 0
	})
	l := newTestListener(t, up.port(), fakeNames{})
	l.SetName = "names-during-dial"
	l.UseDomain = false

	handleInBackground(t, l, p)
	if line := nextConnLine(t, lines, l.SetName); line.domain != "www.formula1.com" {
		t.Fatalf("connection line %+v, want the name from a ClientHello that arrived while the upstream was dialled", line)
	}
	up.next(t)
}

func TestProxyLineNamesAHelloSentAfterAFastDial(t *testing.T) {
	lines := watchConnLines(t)
	hello := realClientHello(t, "api.formula1.com")
	up := startMockSocks(t, len(hello), func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.SetName = "names-after-dial"
	l.UseDomain = false
	p := acceptedPair(t)

	handleInBackground(t, l, p)
	time.Sleep(20 * time.Millisecond)
	if _, err := p.client.Write(hello); err != nil {
		t.Fatal(err)
	}
	if line := nextConnLine(t, lines, l.SetName); line.domain != "api.formula1.com" || line.meta != "proxy" {
		t.Fatalf("connection line %+v, want api.formula1.com from a ClientHello sent after the upstream answered", line)
	}
	r := up.next(t)
	if r.atyp != 1 || string(r.payload) != string(hello) {
		t.Fatalf("CONNECT atyp %d with %d bytes after it, want the address and the intact hello", r.atyp, len(r.payload))
	}
	noMoreConnLines(t, lines, l.SetName, 100*time.Millisecond)
}

func TestProxyLineIsWrittenWhileTheUpstreamHangs(t *testing.T) {
	lines := watchConnLines(t)
	hello := realClientHello(t, "f1tv.formula1.com")
	l := newTestListener(t, startSilentUpstream(t), fakeNames{})
	l.SetName = "names-hanging-upstream"
	l.UseDomain = false
	p := acceptedPair(t)
	if _, err := p.client.Write(hello); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	handleInBackground(t, l, p)
	line := nextConnLine(t, lines, l.SetName)
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("the line waited %s for an upstream that never answered", waited)
	}
	if line.domain != "f1tv.formula1.com" || line.meta != "proxy" {
		t.Fatalf("connection line %+v, want f1tv.formula1.com marked proxy", line)
	}
}

func TestProxyLineForAConsumedNonTLSPrefixIsWrittenBeforeTheDial(t *testing.T) {
	lines := watchConnLines(t)
	l := newTestListener(t, startSilentUpstream(t), fakeNames{inSet: []string{"example.com"}})
	l.SetName = "names-ssh-banner"
	p := acceptedPair(t)
	if _, err := p.client.Write([]byte("SSH-2.0-OpenSSH_9.6\r\n")); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	handleInBackground(t, l, p)
	line := nextConnLine(t, lines, l.SetName)
	if waited := time.Since(start); waited > 150*time.Millisecond {
		t.Fatalf("the line waited %s, although no name could follow the bytes the sniff consumed", waited)
	}
	if line.domain != "" || line.meta != "proxy" {
		t.Fatalf("connection line %+v, want one without a name, marked proxy", line)
	}
}

func TestPeekKeepsAPendingReset(t *testing.T) {
	p := acceptedPair(t)
	defer p.accepted.Close()
	if err := p.client.(*net.TCPConn).SetLinger(0); err != nil {
		t.Fatal(err)
	}
	p.client.Close()
	time.Sleep(50 * time.Millisecond)

	if got := peekClient(p.accepted, sniffMaxBytes); got != nil {
		t.Fatalf("a peek at a reset connection returned %d bytes", len(got))
	}
	_ = p.accepted.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := p.accepted.Read(make([]byte, 16))
	if !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("read after the peek returned %v, want the connection reset the peer sent", err)
	}
}

func TestNamingConnPassesTheHalfCloseOn(t *testing.T) {
	p := acceptedPair(t)
	defer p.client.Close()
	defer p.accepted.Close()
	namer := &proxyNamer{log: func(string, uint16) {}}
	wrapped := namer.wrap(p.accepted)

	cw, ok := wrapped.(interface{ CloseWrite() error })
	if !ok {
		t.Fatal("the wrapped connection hides CloseWrite, so the relay cannot half-close it")
	}
	if err := cw.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	_ = p.client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := p.client.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("the peer read %v after the half-close, want EOF", err)
	}
}

func TestNamingConnNamesTheLineOnceFromTheFirstBytes(t *testing.T) {
	p := acceptedPair(t)
	defer p.client.Close()
	defer p.accepted.Close()
	var names []string
	var mu sync.Mutex
	namer := &proxyNamer{log: func(host string, _ uint16) {
		mu.Lock()
		names = append(names, host)
		mu.Unlock()
	}}
	wrapped := namer.wrap(p.accepted)
	hello := realClientHello(t, "www.formula1.com")

	go func() {
		_, _ = p.client.Write(hello[:100])
		time.Sleep(20 * time.Millisecond)
		_, _ = p.client.Write(hello[100:])
		_, _ = p.client.Write([]byte("more application data"))
	}()
	got := 0
	buf := make([]byte, 64)
	_ = wrapped.SetReadDeadline(time.Now().Add(2 * time.Second))
	for got < len(hello)+len("more application data") {
		n, err := wrapped.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		got += n
	}
	namer.deadline(p.accepted)
	mu.Lock()
	defer mu.Unlock()
	if len(names) != 1 || names[0] != "www.formula1.com" {
		t.Fatalf("names %v, want www.formula1.com exactly once", names)
	}
}

func namesSeen(t *testing.T) (*proxyNamer, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var names []string
	namer := &proxyNamer{log: func(host string, _ uint16) {
		mu.Lock()
		names = append(names, host)
		mu.Unlock()
	}}
	return namer, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), names...)
	}
}

func queued(t *testing.T, c net.Conn, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(peekClient(c, sniffMaxBytes)) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d bytes never reached the socket", n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFirstWaitNamesAHelloTheRelayHasNotReadYet(t *testing.T) {
	p := acceptedPair(t)
	defer p.client.Close()
	defer p.accepted.Close()
	namer, names := namesSeen(t)
	hello := realClientHello(t, "www.formula1.com")
	if _, err := p.client.Write(hello); err != nil {
		t.Fatal(err)
	}
	queued(t, p.accepted, len(hello))

	wrapped := namer.wrap(p.accepted)
	namer.deadline(p.accepted)
	buf := make([]byte, len(hello))
	_ = wrapped.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(wrapped, buf); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 1 || got[0] != "www.formula1.com" {
		t.Fatalf("names %v, want www.formula1.com once from a hello that was queued when the relay started", got)
	}
}

func TestFirstWaitLeavesAPartialHelloForLater(t *testing.T) {
	p := acceptedPair(t)
	defer p.client.Close()
	defer p.accepted.Close()
	namer, names := namesSeen(t)
	hello := realClientHello(t, "api.formula1.com")
	if _, err := p.client.Write(hello[:100]); err != nil {
		t.Fatal(err)
	}
	queued(t, p.accepted, 100)

	namer.deadline(p.accepted)
	if got := names(); len(got) != 0 {
		t.Fatalf("names %v after the first wait, want none while the hello is incomplete", got)
	}
	if _, err := p.client.Write(hello[100:]); err != nil {
		t.Fatal(err)
	}
	queued(t, p.accepted, len(hello))
	namer.expire(p.accepted)
	if got := names(); len(got) != 1 || got[0] != "api.formula1.com" {
		t.Fatalf("names %v, want api.formula1.com once the rest of the hello arrived", got)
	}
}

func TestProxyLineNamesAHelloSplitAcrossTheFirstWait(t *testing.T) {
	lines := watchConnLines(t)
	hello := realClientHello(t, "www.formula1.com")
	up := startMockSocks(t, len(hello), func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.SetName = "names-split-hello"
	l.UseDomain = false
	p := acceptedPair(t)
	if _, err := p.client.Write(hello[:100]); err != nil {
		t.Fatal(err)
	}

	handleInBackground(t, l, p)
	time.Sleep(sniffFirstWait + 150*time.Millisecond)
	if _, err := p.client.Write(hello[100:]); err != nil {
		t.Fatal(err)
	}
	if line := nextConnLine(t, lines, l.SetName); line.domain != "www.formula1.com" {
		t.Fatalf("connection line %+v, want the name from a ClientHello whose second part came after the first wait", line)
	}
	up.next(t)
}

func TestProxyLineWaitsForTheRestOfASplitHelloWhileTheUpstreamHangs(t *testing.T) {
	lines := watchConnLines(t)
	hello := realClientHello(t, "f1tv.formula1.com")
	l := newTestListener(t, startSilentUpstream(t), fakeNames{})
	l.SetName = "names-split-hanging"
	l.UseDomain = false
	p := acceptedPair(t)
	if _, err := p.client.Write(hello[:100]); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	handleInBackground(t, l, p)
	time.Sleep(sniffFirstWait + 150*time.Millisecond)
	if _, err := p.client.Write(hello[100:]); err != nil {
		t.Fatal(err)
	}
	line := nextConnLine(t, lines, l.SetName)
	if waited := time.Since(start); waited > sniffTotalWait+500*time.Millisecond {
		t.Fatalf("the line waited %s for an upstream that never answered", waited)
	}
	if line.domain != "f1tv.formula1.com" || line.meta != "proxy" {
		t.Fatalf("connection line %+v, want f1tv.formula1.com marked proxy", line)
	}
}

func TestProxyLineForAnIncompleteFirstMessageIsNotHeldBack(t *testing.T) {
	lines := watchConnLines(t)
	first := []byte("GET key\r\n")
	up := startMockSocks(t, len(first), func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.SetName = "names-incomplete-first"
	l.UseDomain = false
	p := acceptedPair(t)

	start := time.Now()
	handleInBackground(t, l, p)
	time.Sleep(20 * time.Millisecond)
	if _, err := p.client.Write(first); err != nil {
		t.Fatal(err)
	}
	line := nextConnLine(t, lines, l.SetName)
	if waited := time.Since(start); waited > sniffTotalWait+500*time.Millisecond {
		t.Fatalf("the line waited %s for the rest of a message that never came", waited)
	}
	if line.domain != "" || line.meta != "proxy" {
		t.Fatalf("connection line %+v, want one without a name, marked proxy", line)
	}
	if r := up.next(t); string(r.payload) != string(first) {
		t.Fatalf("the upstream got %q, want the client's bytes intact", r.payload)
	}
}
