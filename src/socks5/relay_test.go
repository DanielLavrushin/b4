package socks5

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func connPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ch := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		ch <- c
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	peer := <-ch
	if peer == nil {
		t.Fatal("accept failed")
	}
	return c, peer
}

func reset(t *testing.T, c net.Conn) {
	t.Helper()
	tc, ok := c.(*net.TCPConn)
	if !ok {
		t.Fatal("not a TCPConn")
	}
	_ = tc.SetLinger(0)
	_ = tc.Close()
}

func relayReturns(t *testing.T, a, b net.Conn, within time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_ = Relay(a, b)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("Relay still blocked after %s", within)
	}
}

func withTimeouts(t *testing.T, idle, halfIdle time.Duration) {
	t.Helper()
	oldIdle, oldHalf := relayIdleTimeout, relayHalfCloseIdle
	relayIdleTimeout, relayHalfCloseIdle = idle, halfIdle
	t.Cleanup(func() {
		relayIdleTimeout, relayHalfCloseIdle = oldIdle, oldHalf
	})
}

func TestRelayTearsDownWhenUpstreamResets(t *testing.T) {
	a, aPeer := connPair(t)
	b, bPeer := connPair(t)
	defer aPeer.Close()

	reset(t, bPeer)
	relayReturns(t, a, b, 5*time.Second)
}

func TestRelayTearsDownWhenClientResets(t *testing.T) {
	a, aPeer := connPair(t)
	b, bPeer := connPair(t)
	defer bPeer.Close()

	reset(t, aPeer)
	relayReturns(t, a, b, 5*time.Second)
}

func TestRelayTearsDownWhenBothSidesGoIdle(t *testing.T) {
	withTimeouts(t, 300*time.Millisecond, 300*time.Millisecond)

	a, aPeer := connPair(t)
	b, bPeer := connPair(t)
	defer aPeer.Close()
	defer bPeer.Close()

	relayReturns(t, a, b, 5*time.Second)
}

func TestRelayTearsDownAfterHalfCloseThenSilence(t *testing.T) {
	withTimeouts(t, time.Hour, 300*time.Millisecond)

	a, aPeer := connPair(t)
	b, bPeer := connPair(t)
	defer aPeer.Close()
	defer bPeer.Close()

	_ = aPeer.(*net.TCPConn).CloseWrite()
	relayReturns(t, a, b, 5*time.Second)
}

func TestRelayKeepsLongTransferAliveAcrossIdleWindows(t *testing.T) {
	withTimeouts(t, 150*time.Millisecond, 150*time.Millisecond)

	a, aPeer := connPair(t)
	b, bPeer := connPair(t)

	done := make(chan struct{})
	go func() {
		_ = Relay(a, b)
		close(done)
	}()

	const chunks = 40
	chunk := bytes.Repeat([]byte("x"), 1024)

	go func() {
		for i := 0; i < chunks; i++ {
			if _, err := bPeer.Write(chunk); err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		_ = bPeer.(*net.TCPConn).CloseWrite()
	}()

	got := 0
	buf := make([]byte, 4096)
	_ = aPeer.SetReadDeadline(time.Now().Add(15 * time.Second))
	for got < chunks*len(chunk) {
		n, err := aPeer.Read(buf)
		got += n
		if err != nil {
			break
		}
	}

	if got != chunks*len(chunk) {
		t.Fatalf("transfer was cut: relayed %d of %d bytes across idle windows", got, chunks*len(chunk))
	}

	aPeer.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Relay did not finish")
	}
}

func TestRelayReleasesDescriptors(t *testing.T) {
	kinds := func() (sockets, pipes int) {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Skip("no /proc")
		}
		for _, e := range entries {
			target, err := os.Readlink(filepath.Join("/proc/self/fd", e.Name()))
			if err != nil {
				continue
			}
			if strings.HasPrefix(target, "socket:") {
				sockets++
			} else if strings.HasPrefix(target, "pipe:") {
				pipes++
			}
		}
		return
	}

	const n = 100
	hold := make([]net.Conn, 0, n)
	s0, p0 := kinds()

	for i := 0; i < n; i++ {
		a, aPeer := connPair(t)
		b, bPeer := connPair(t)
		go func() { _ = Relay(a, b) }()
		reset(t, bPeer)
		hold = append(hold, aPeer)
	}

	time.Sleep(2 * time.Second)
	s1, p1 := kinds()

	if s1-s0 > n+20 {
		t.Errorf("sockets leaked: +%d for %d relays that should hold only the %d test peers", s1-s0, n, n)
	}
	if p1-p0 > 20 {
		t.Errorf("splice pipes leaked: +%d", p1-p0)
	}
	t.Logf("%d reset relays: sockets +%d (test holds %d), pipes +%d", n, s1-s0, n, p1-p0)

	for _, c := range hold {
		c.Close()
	}
}
