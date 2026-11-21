package warmtuple

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type warmAttempts struct {
	mu    sync.Mutex
	ports map[uint16]int
}

func (a *warmAttempts) add(port uint16) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ports[port]++
}

func (a *warmAttempts) on(port uint16) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ports[port]
}

func (a *warmAttempts) total() (ports, dials int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, n := range a.ports {
		dials += n
	}
	return len(a.ports), dials
}

func holdWarmPorts(t *testing.T, s *Service, warmTimeout time.Duration, held func(uint16) bool) (*warmAttempts, *atomic.Int32) {
	t.Helper()
	attempts := &warmAttempts{ports: make(map[uint16]int)}
	warms := &atomic.Int32{}
	s.newManager = func(c Config) (*Manager, error) {
		c.ReconnectDelay, c.WarmTimeout = 20*time.Millisecond, warmTimeout
		m := fakeManager(t, c)
		m.warm = func(context.Context, net.Conn) error { warms.Add(1); return nil }
		m.dial = func(_ context.Context, port uint16, mark uint32) (net.Conn, error) {
			if mark == c.WarmMark {
				attempts.add(port)
				if held(port) {
					return nil, unix.EADDRNOTAVAIL
				}
			}
			return fakeSocket(t, c, port), nil
		}
		return m, nil
	}
	return attempts, warms
}

func TestAHeldWarmPortMovesToTheNextFreePort(t *testing.T) {
	s, _, set, _ := serviceFixture(t)
	const warmTimeout = 2 * time.Second
	attempts, warms := holdWarmPorts(t, s, warmTimeout, func(port uint16) bool { return port == firstPort })
	direct := recordDirect(t, s)
	source, target := netip.MustParseAddr("203.0.113.1"), netip.MustParseAddrPort("203.0.113.2:443")
	started := time.Now()
	c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", target)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("a warm whose first source port was held closed the client: %v", err)
	}
	defer c.Close()
	pass, ok := c.(*serviceConn)
	if !ok || direct.calls() != 0 {
		t.Fatalf("the warm dialed the held source port %d %d times over %s of its %s warm timeout, and the client got a %T after %d relays with the set's strategy; want one try on the held port and a warm on the next free port %d", firstPort, attempts.on(firstPort), elapsed.Round(10*time.Millisecond), warmTimeout, c, direct.calls(), firstPort+1)
	}
	if pass.lease.port != firstPort+1 || !pass.Conn.(*Conn).Warmed || warms.Load() != 1 || attempts.on(firstPort) != 1 {
		t.Fatalf("the pass runs from port %d (warmed=%t) after %d warms and %d tries on the held port %d, want a pass warmed by this call on port %d after one try on the held port", pass.lease.port, pass.Conn.(*Conn).Warmed, warms.Load(), attempts.on(firstPort), firstPort, firstPort+1)
	}
	if s.failureCount() != 0 {
		t.Fatalf("a held source port was remembered as a failed warm of the server (%d failures)", s.failureCount())
	}
	s.mu.Lock()
	until := s.cooling[coolKey{port: firstPort, source: source, target: target}]
	s.mu.Unlock()
	if !time.Now().Before(until) {
		t.Fatalf("the held port %d is not cooling for %s, so the next warm to it could try that port again", firstPort, target)
	}
}

func TestEveryHeldWarmPortEndsInARelayWithTheSetsStrategy(t *testing.T) {
	logged := captureWarnings(t)
	s, _, set, _ := serviceFixture(t)
	attempts, warms := holdWarmPorts(t, s, 2*time.Second, func(uint16) bool { return true })
	direct := recordDirect(t, s)
	c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443"))
	if err != nil || !isFallback(c) {
		t.Fatalf("a warm that found every source port held gave %T, %v, want a relay with the set's strategy", c, err)
	}
	defer c.Close()
	ports, dials := attempts.total()
	if ports != heldPortMoves+1 || dials != heldPortMoves+1 || warms.Load() != 0 || direct.calls() != 1 {
		t.Fatalf("the warm tried %d source ports with %d dials, warmed %d times and relayed %d connections with the set's strategy, want %d ports tried once each, then one relay", ports, dials, warms.Load(), direct.calls(), heldPortMoves+1)
	}
	if s.failureCount() != 0 {
		t.Fatalf("held source ports were remembered as a failed warm of the server (%d failures)", s.failureCount())
	}
	if n := logged.warnings("held"); n != 1 {
		t.Fatalf("%d warnings name the held source ports, want 1", n)
	}
}

func TestAWarmOnARealHeldTupleGivesUpThePortAtOnce(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 4)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
		close(accepted)
		for c := range accepted {
			c.Close()
		}
	})
	reuse := func(_, _ string, raw syscall.RawConn) error {
		var sockErr error
		if err := raw.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
		}); err != nil {
			return err
		}
		return sockErr
	}
	d := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}, Control: reuse}
	holder, err := d.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { holder.Close() })
	held := holder.LocalAddr().(*net.TCPAddr).AddrPort()
	cfg := fixture()
	cfg.Source, cfg.Target = held.Addr(), ln.Addr().(*net.TCPAddr).AddrPort()
	cfg.FirstPort, cfg.LastPort = held.Port(), held.Port()
	cfg.WarmTimeout = 2 * time.Second
	m := fakeManager(t, cfg)
	var dials atomic.Int32
	m.dial = func(ctx context.Context, port uint16, mark uint32) (net.Conn, error) {
		dials.Add(1)
		return m.dialSocket(ctx, port, mark)
	}
	started := time.Now()
	c, err := m.DialContext(context.Background(), cfg.Host+":"+strconv.Itoa(int(cfg.Target.Port())))
	elapsed := time.Since(started)
	if c != nil {
		c.Close()
		t.Fatalf("a warm from %s got a connection although another socket holds that tuple", held)
	}
	if dials.Load() != 1 || !errors.Is(err, errPortHeld) || !errors.Is(err, unix.EADDRNOTAVAIL) || errors.Is(err, ErrWarm) {
		t.Fatalf("a warm from %s, whose tuple another socket holds, dialed %d times over %s of its %s warm timeout and ended with %v; want one dial and a held-port error that wraps EADDRNOTAVAIL and is no failed warm of the server", held, dials.Load(), elapsed.Round(10*time.Millisecond), cfg.WarmTimeout, err)
	}
	if m.Active() != 0 {
		t.Fatal("the held port leaked a lease")
	}
}
