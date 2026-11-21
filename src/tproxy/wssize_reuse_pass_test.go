package tproxy

import (
	"context"
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
	halfClosed chan struct{}
	once       sync.Once
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
