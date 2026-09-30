package tproxy

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/log"
)

type connLine struct {
	set, domain, tls, meta string
}

func watchConnLines(t *testing.T) chan string {
	t.Helper()
	ch, _ := log.GetConnectionHub().Subscribe()
	t.Cleanup(func() { log.GetConnectionHub().Unsubscribe(ch) })
	return ch
}

func parseConnLine(raw string) connLine {
	f := strings.Split(raw, ",")
	if len(f) < 9 {
		return connLine{}
	}
	cl := connLine{set: f[2], domain: f[3], tls: f[8]}
	if len(f) > 9 {
		cl.meta = f[9]
	}
	return cl
}

func nextConnLine(t *testing.T, ch chan string, set string) connLine {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case raw := <-ch:
			if cl := parseConnLine(raw); cl.set == set {
				return cl
			}
		case <-deadline:
			t.Fatalf("no connection line for set %q", set)
			return connLine{}
		}
	}
}

func noMoreConnLines(t *testing.T, ch chan string, set string, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case raw := <-ch:
			if cl := parseConnLine(raw); cl.set == set {
				t.Fatalf("a second connection line was written for one connection: %+v", cl)
			}
		case <-deadline:
			return
		}
	}
}

func proxySent(t *testing.T, l *Listener, send []byte) chan struct{} {
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
	if len(send) > 0 {
		if _, err := client.Write(send); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for len(peekClient(accepted, sniffMaxBytes)) < len(send) {
			if time.Now().After(deadline) {
				t.Fatal("the client's bytes never reached the accepted socket")
			}
			time.Sleep(time.Millisecond)
		}
	}
	done := make(chan struct{})
	go func() {
		l.handle(accepted)
		close(done)
	}()
	t.Cleanup(func() {
		client.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("handle did not return after the client closed")
		}
	})
	return done
}

func TestPeekClientLeavesTheBytesInTheSocket(t *testing.T) {
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
	defer accepted.Close()

	if got := peekClient(accepted, sniffMaxBytes); got != nil {
		t.Fatalf("a peek at a socket with nothing to read returned %d bytes", len(got))
	}
	hello := realClientHello(t, "www.formula1.com")
	if _, err := client.Write(hello); err != nil {
		t.Fatal(err)
	}
	var peeked []byte
	deadline := time.Now().Add(2 * time.Second)
	for len(peeked) < len(hello) && time.Now().Before(deadline) {
		peeked = peekClient(accepted, sniffMaxBytes)
		time.Sleep(time.Millisecond)
	}
	if host, _ := sniffedHost(peeked); host != "www.formula1.com" {
		t.Fatalf("peeked host %q, want www.formula1.com", host)
	}
	got := make([]byte, len(hello))
	_ = accepted.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := accepted.Read(got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, hello) {
		t.Fatal("the peek consumed bytes the relay needs")
	}
}

func TestProxyLineNamesTheSiteWhenUseDomainIsOff(t *testing.T) {
	lines := watchConnLines(t)
	hello := realClientHello(t, "www.formula1.com")
	up := startMockSocks(t, len(hello), func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.SetName = "names-use-domain-off"
	l.UseDomain = false

	proxySent(t, l, hello)
	line := nextConnLine(t, lines, l.SetName)
	if line.domain != "www.formula1.com" || line.meta != "proxy" || line.tls == "" {
		t.Fatalf("connection line %+v, want www.formula1.com with its TLS version, marked proxy", line)
	}
	r := up.next(t)
	if r.atyp != 1 || r.host != "127.0.0.1" {
		t.Fatalf("CONNECT asked for atyp %d host %q, want the address as before", r.atyp, r.host)
	}
	if !bytes.Equal(r.payload, hello) {
		t.Fatalf("the upstream got %d bytes after CONNECT, want the client's %d-byte hello intact", len(r.payload), len(hello))
	}
	noMoreConnLines(t, lines, l.SetName, 100*time.Millisecond)
}

func TestProxyLineNamesTheSiteOfAnAddressOnlySet(t *testing.T) {
	lines := watchConnLines(t)
	hello := realClientHello(t, "api.formula1.com")
	up := startMockSocks(t, len(hello), func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.SetName = "names-address-only"

	proxySent(t, l, hello)
	if line := nextConnLine(t, lines, l.SetName); line.domain != "api.formula1.com" {
		t.Fatalf("connection line %+v, want api.formula1.com", line)
	}
	r := up.next(t)
	if r.atyp != 1 || !bytes.Equal(r.payload, hello) {
		t.Fatalf("CONNECT atyp %d with %d bytes after it, want the address and the intact hello", r.atyp, len(r.payload))
	}
}

func TestProxyLineIsStillWrittenForASilentClient(t *testing.T) {
	lines := watchConnLines(t)
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.SetName = "names-silent"
	l.UseDomain = false

	proxySent(t, l, nil)
	if line := nextConnLine(t, lines, l.SetName); line.domain != "" || line.meta != "proxy" {
		t.Fatalf("connection line %+v, want one without a name, marked proxy", line)
	}
	if r := up.next(t); r.atyp != 1 {
		t.Fatalf("CONNECT asked for atyp %d, want the address", r.atyp)
	}
	noMoreConnLines(t, lines, l.SetName, 100*time.Millisecond)
}

func TestProxyLineIsWrittenWhenTheUpstreamRefuses(t *testing.T) {
	lines := watchConnLines(t)
	hello := realClientHello(t, "f1tv.formula1.com")
	up := startMockSocks(t, 0, func(byte) byte { return 5 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.SetName = "names-refused"
	l.UseDomain = false

	done := proxySent(t, l, hello)
	if line := nextConnLine(t, lines, l.SetName); line.domain != "f1tv.formula1.com" || line.meta != "proxy" {
		t.Fatalf("connection line %+v, want f1tv.formula1.com marked proxy", line)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a refused connection was not closed")
	}
}

func TestProxyLineKeepsTheNameTheListenerChose(t *testing.T) {
	lines := watchConnLines(t)
	hello := realClientHello(t, "www.formula1.com")
	up := startMockSocks(t, len(hello), func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{inSet: []string{"www.formula1.com"}})
	l.SetName = "names-listed"

	proxySent(t, l, hello)
	if line := nextConnLine(t, lines, l.SetName); line.domain != "www.formula1.com" {
		t.Fatalf("connection line %+v, want www.formula1.com", line)
	}
	if r := up.next(t); r.atyp != 3 || r.host != "www.formula1.com" || !bytes.Equal(r.payload, hello) {
		t.Fatalf("CONNECT atyp %d host %q, want the listed name with the hello intact", r.atyp, r.host)
	}
	noMoreConnLines(t, lines, l.SetName, 100*time.Millisecond)
}
