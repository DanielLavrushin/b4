package tproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/sys/unix"
)

type reuseTestDialer struct {
	dial func(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error)
}

func (d reuseTestDialer) Dial(ctx context.Context, s *config.SetConfig, owner, host string, target netip.AddrPort) (net.Conn, error) {
	return d.dial(ctx, s, owner, host, target)
}
func (d reuseTestDialer) AcquireClient() (func(), bool) { return func() {}, true }

type reuseTestClient struct{ net.Conn }

func (c reuseTestClient) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 50000}
}

func TestWSReuseReplaysOriginalFragmentedClientHello(t *testing.T) {
	hello := realClientHello(t, "video.example.org")
	body := []byte("original encrypted application bytes")
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	server, backend := net.Pipe()
	defer server.Close()
	defer backend.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	set := config.NewSetConfig()
	set.Enabled = true
	set.TCP.WSSizeReuse.Enabled = true
	l := &Listener{ctx: ctx, Warm: reuseTestDialer{dial: func(_ context.Context, _ *config.SetConfig, owner, host string, target netip.AddrPort) (net.Conn, error) {
		if owner != "192.0.2.10" || host != "video.example.org" || target.String() != "203.0.113.2:443" {
			return nil, errors.New("selector changed")
		}
		return backend, nil
	}}}
	l.set.Store(&set)
	done := make(chan struct{})
	go func() { defer close(done); l.serveWSReuse(reuseTestClient{b}, net.ParseIP("203.0.113.2"), 443, nil) }()
	received := make(chan []byte, 1)
	go func() {
		buf := make([]byte, len(hello)+len(body))
		_, err := io.ReadFull(server, buf)
		if err != nil {
			received <- nil
			return
		}
		received <- buf
		server.Write([]byte("reply"))
		server.Close()
	}()
	a.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := a.Write(hello[:11]); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write(hello[11:]); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write(body); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 5)
	if _, err := io.ReadFull(a, reply); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(<-received, append(hello, body...)) || string(reply) != "reply" {
		t.Fatal("client stream changed")
	}
	a.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay failed to clean up")
	}
}

func TestWSReuseWithoutSNIOrWithWarmFailureNeverForwards(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(map[bool]string{true: "missing SNI", false: "warm failure"}[missing], func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			set := config.NewSetConfig()
			set.Enabled = true
			set.TCP.WSSizeReuse.Enabled = true
			l := &Listener{ctx: ctx, Warm: reuseTestDialer{dial: func(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error) {
				calls++
				return nil, errors.New("warm failed")
			}}}
			l.set.Store(&set)
			result := sniffResult{prefix: []byte("must not reach server")}
			if !missing {
				result.host = "video.example.org"
				result.tlsVersion = 0x0304
			}
			l.serveWSReuse(reuseTestClient{b}, net.ParseIP("203.0.113.2"), 443, &result)
			if (calls == 0) != missing {
				t.Fatal("wrong warm decision")
			}
		})
	}
}

func TestWSReuseClientCloseCancelsPendingWarm(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	client, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	set := config.NewSetConfig()
	set.Enabled = true
	set.TCP.WSSizeReuse.Enabled = true
	l := &Listener{ctx: ctx, Warm: reuseTestDialer{dial: func(ctx context.Context, _ *config.SetConfig, _, _ string, _ netip.AddrPort) (net.Conn, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}}}
	l.set.Store(&set)
	done := make(chan struct{})
	go func() { defer close(done); l.serveWSReuse(server, net.ParseIP("203.0.113.2"), 443, nil) }()
	if _, err := client.Write(realClientHello(t, "video.example.org")); err != nil {
		t.Fatal(err)
	}
	<-entered
	client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closed client left a warm running")
	}
}

func TestWarmClientPollErrorsDoNotCancel(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	a, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	old := warmClientPoll
	defer func() { warmClientPoll = old }()
	var calls atomic.Int32
	var closed atomic.Bool
	polled := make(chan struct{})
	warmClientPoll = func(fds []unix.PollFd, _ int) (int, error) {
		n := calls.Add(1)
		if n == 1 {
			return 0, unix.EINTR
		}
		if n == 2 {
			fds[0].Revents = unix.POLLERR
			return 1, nil
		}
		if n == 3 {
			close(polled)
		}
		if closed.Load() {
			fds[0].Revents = unix.POLLRDHUP
			return 1, nil
		}
		return 0, nil
	}
	ctx, stop := watchWarmClient(context.Background(), b)
	defer stop()
	select {
	case <-polled:
	case <-time.After(time.Second):
		t.Fatal("transient poll error cancelled warm")
	}
	if ctx.Err() != nil {
		t.Fatal("live client cancelled")
	}
	closed.Store(true)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("closed client not cancelled")
	}
}

func TestWSReuseRelaySurvivesListenerStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend, server := net.Pipe()
	defer server.Close()
	set := config.NewSetConfig()
	set.Enabled = true
	set.TCP.WSSizeReuse.Enabled = true
	l := &Listener{ctx: ctx, Warm: reuseTestDialer{dial: func(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error) {
		return backend, nil
	}}}
	l.set.Store(&set)
	a, b := net.Pipe()
	defer a.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.serveWSReuse(reuseTestClient{b}, net.ParseIP("203.0.113.2"), 443, &sniffResult{host: "video.example.org", tlsVersion: 0x0304, prefix: []byte("hello")})
	}()
	buf := make([]byte, 5)
	server.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(server, buf); err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(100 * time.Millisecond)
	a.SetDeadline(time.Now().Add(time.Second))
	go server.Write([]byte("reply"))
	if _, err := io.ReadFull(a, buf); err != nil {
		t.Fatalf("the listener stop cut a relayed connection: %v", err)
	}
	if string(buf) != "reply" {
		t.Fatalf("client read %q, want the server's reply", buf)
	}
	a.Close()
	server.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the relay did not end with the client")
	}
}

func TestWSReuseListenerStopAbortsPendingWarm(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, cancelled := make(chan struct{}), make(chan struct{})
	set := config.NewSetConfig()
	set.Enabled = true
	set.TCP.WSSizeReuse.Enabled = true
	l := &Listener{ctx: ctx, Warm: reuseTestDialer{dial: func(ctx context.Context, _ *config.SetConfig, _, _ string, _ netip.AddrPort) (net.Conn, error) {
		close(entered)
		<-ctx.Done()
		<-cancelled
		return nil, ctx.Err()
	}}}
	l.set.Store(&set)
	a, b := net.Pipe()
	defer a.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.serveWSReuse(reuseTestClient{b}, net.ParseIP("203.0.113.2"), 443, &sniffResult{host: "video.example.org", tlsVersion: 0x0304, prefix: []byte("hello")})
	}()
	<-entered
	cancel()
	close(cancelled)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a listener stop left the warm running")
	}
	a.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := a.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the client was left open after the listener stop aborted its warm: %v", err)
	}
}
