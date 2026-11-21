package warmtuple

import (
	"context"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type halfClosingConn struct{ *fakeConn }

func (halfClosingConn) CloseWrite() error { return nil }

func passSockets(t *testing.T, s *Service) (*atomic.Int32, chan net.Conn) {
	t.Helper()
	warms := &atomic.Int32{}
	servers := make(chan net.Conn, 8)
	s.newManager = func(c Config) (*Manager, error) {
		c.ReconnectDelay = 20 * time.Millisecond
		m := fakeManager(t, c)
		m.warm = func(context.Context, net.Conn) error { warms.Add(1); return nil }
		m.dial = func(_ context.Context, port uint16, mark uint32) (net.Conn, error) {
			a, b := net.Pipe()
			t.Cleanup(func() { a.Close(); b.Close() })
			if mark == config.SelfDialRelayMark {
				servers <- b
			}
			return halfClosingConn{&fakeConn{Conn: a, local: net.TCPAddrFromAddrPort(netip.AddrPortFrom(c.Source, port)), remote: net.TCPAddrFromAddrPort(c.Target)}}, nil
		}
		return m, nil
	}
	return warms, servers
}

func TestLateServerBytesDoNotKeepAHungTuple(t *testing.T) {
	s, _, set, _ := serviceFixture(t)
	warms, servers := passSockets(t, s)
	target := netip.MustParseAddrPort("203.0.113.2:443")
	hung, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", target)
	if err != nil {
		t.Fatal(err)
	}
	server := <-servers
	if err := hung.(interface{ CloseWrite() error }).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = server.Write([]byte("answer after the client left")) }()
	_ = hung.SetReadDeadline(time.Now().Add(2 * time.Second))
	if n, err := hung.Read(make([]byte, 64)); n == 0 {
		t.Fatalf("the late server bytes did not reach the relay: %v", err)
	}
	hung.Close()
	next, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", target)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.LocalAddr().String() == hung.LocalAddr().String() || warms.Load() != 2 {
		t.Fatalf("server bytes the relay read after the client had left kept the tuple %s: the next pass used %s after %d warms, want a warm on a new source port", hung.LocalAddr(), next.LocalAddr(), warms.Load())
	}
}
