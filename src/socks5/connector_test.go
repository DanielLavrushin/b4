package socks5

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/warmtuple"
)

type connectorConn struct{ net.Conn }

func TestGeoIPOrdinaryConnectorUsesFrozenBackend(t *testing.T) {
	cfg := config.NewConfig()
	warm := config.NewSetConfig()
	warm.Id = "warm"
	warm.TCP.WarmTuple.Enabled = true
	warm.Targets.IPs = []string{"203.0.113.2/32"}
	warm.Targets.IpsToMatch = []string{"203.0.113.2/32"}
	warm.Targets.IPPriority = true
	ordinary := config.NewSetConfig()
	ordinary.Id = "ordinary"
	ordinary.Targets.SNIDomains = []string{"ordinary.invalid"}
	ordinary.Targets.DomainsToMatch = []string{"ordinary.invalid"}
	cfg.Sets = []*config.SetConfig{&warm, &ordinary}
	cfg.System.Socks5.WarmTuple.Targets = map[string]string{"ordinary.invalid": "203.0.113.9"}
	backend, peer := net.Pipe()
	defer backend.Close()
	defer peer.Close()
	s := NewServerWithConnector(&cfg, func(_ context.Context, dest string) (net.Conn, error) {
		if dest != "203.0.113.9:443" {
			t.Fatalf("ordinary dial repeated hostname resolution: %s", dest)
		}
		return backend, nil
	})
	s.ctx = context.Background()
	s.warmTuple = warmtuple.NewService(&cfg, warmtuple.NewFlows())
	defer s.warmTuple.Close()
	c, _, err := s.dial(nil, "ordinary.invalid:443")
	if err != nil || c != backend {
		t.Fatal("frozen ordinary destination did not reach connector", err)
	}
}

func (c connectorConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(203, 0, 113, 1), Port: 40000}
}

func connectorServer(t *testing.T, connector TCPConnector) (*Server, ClientConfig) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.System.Socks5 = config.Socks5Config{Enabled: true, BindAddress: "127.0.0.1", Port: 0}
	s := NewServerWithConnector(&cfg, connector)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop() })
	return s, ClientConfig{Host: "127.0.0.1", Port: s.listener.Addr().(*net.TCPAddr).Port, Timeout: time.Second}
}

func TestConnectorFailureHasNoDirectFallback(t *testing.T) {
	failure := errors.New("warm failed")
	s := NewServerWithConnector(&config.Config{}, func(ctx context.Context, dest string) (net.Conn, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("connector is unbounded")
		}
		return nil, failure
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if c, _, err := s.dial(nil, listener.Addr().String()); c != nil || !errors.Is(err, failure) {
		t.Fatal("connector failure was bypassed by direct dial")
	}
	ordinary := NewServer(&config.Config{})
	c, _, err := ordinary.dial(nil, listener.Addr().String())
	if err != nil {
		t.Fatal("default constructor changed direct dial behavior", err)
	}
	c.Close()
}

func TestConnectorWaitsBeforeSuccessAndRelaysOriginalBytes(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	backend, peer := net.Pipe()
	defer backend.Close()
	defer peer.Close()
	_, cfg := connectorServer(t, func(ctx context.Context, dest string) (net.Conn, error) {
		if dest != "warm.example.org:443" {
			t.Errorf("connector got %s", dest)
		}
		close(entered)
		select {
		case <-release:
			return connectorConn{backend}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	type result struct {
		conn net.Conn
		err  error
	}
	done := make(chan result, 1)
	go func() {
		c, err := DialUpstream(context.Background(), cfg, "warm.example.org", 443)
		done <- result{c, err}
	}()
	<-entered
	select {
	case r := <-done:
		if r.conn != nil {
			r.conn.Close()
		}
		t.Fatal("CONNECT replied before the connector returned")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	defer r.conn.Close()
	_ = r.conn.SetDeadline(time.Now().Add(time.Second))
	_ = peer.SetDeadline(time.Now().Add(time.Second))
	const original = "original application ClientHello bytes"
	wrote := make(chan error, 1)
	go func() { _, err := r.conn.Write([]byte(original)); wrote <- err }()
	got := make([]byte, len(original))
	if _, err := io.ReadFull(peer, got); err != nil || string(got) != original {
		t.Fatalf("relay changed application bytes: %q, %v", got, err)
	}
	if err := <-wrote; err != nil {
		t.Fatal(err)
	}
}

func TestConnectorCancelledOnStopAndUDPRejected(t *testing.T) {
	entered := make(chan struct{})
	s, cfg := connectorServer(t, func(ctx context.Context, _ string) (net.Conn, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	done := make(chan error, 1)
	go func() { _, err := DialUpstream(context.Background(), cfg, "warm.example.org", 443); done <- err }()
	<-entered
	s.Stop()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stopped connector succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the connector")
	}
	var calls atomic.Int32
	s, _ = connectorServer(t, func(context.Context, string) (net.Conn, error) { calls.Add(1); return nil, errors.New("unexpected") })
	c, err := net.Dial("tcp", s.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	c.Write([]byte{5, 1, 0})
	if _, err := io.ReadFull(c, make([]byte, 2)); err != nil {
		t.Fatal(err)
	}
	c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	reply := make([]byte, 10)
	if _, err := io.ReadFull(c, reply); err != nil || reply[1] != repCmdNotSupported || calls.Load() != 0 {
		t.Fatalf("UDP was not rejected: %v %v", reply, err)
	}
}
