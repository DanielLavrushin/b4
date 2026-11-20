package warmtuple

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/leaktest"
	"golang.org/x/sys/unix"
)

func TestMain(m *testing.M) {
	leaktest.VerifyTestMain(m)
}

func fixture() Config {
	return Config{
		Host: "warm.example.org", Target: netip.MustParseAddrPort("203.0.113.2:443"),
		Source: netip.MustParseAddr("203.0.113.1"), FirstPort: 40000, LastPort: 40000,
		WarmTimeout: time.Second, ReuseTimeout: time.Second,
		RetryInterval: 20 * time.Millisecond, ReconnectDelay: 20 * time.Millisecond, PassLifetime: time.Second,
	}
}

type fakeConn struct {
	net.Conn
	local, remote *net.TCPAddr
	closed        atomic.Bool
}

func (c *fakeConn) LocalAddr() net.Addr  { return c.local }
func (c *fakeConn) RemoteAddr() net.Addr { return c.remote }
func (c *fakeConn) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

func fakeSocket(t *testing.T, cfg Config, port uint16) *fakeConn {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	return &fakeConn{Conn: a,
		local:  net.TCPAddrFromAddrPort(netip.AddrPortFrom(cfg.Source, port)),
		remote: net.TCPAddrFromAddrPort(cfg.Target)}
}

func fakeManager(t *testing.T, cfg Config) *Manager {
	t.Helper()
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	m.dial = func(_ context.Context, port uint16, _ uint32) (net.Conn, error) {
		return fakeSocket(t, cfg, port), nil
	}
	m.warm = func(context.Context, net.Conn) error { return nil }
	m.timestamped = func(net.Conn) (bool, error) { return true, nil }
	return m
}

func TestSelectionAndValidation(t *testing.T) {
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Source = netip.MustParseAddr("::1") },
		func(c *Config) { c.Target = netip.MustParseAddrPort("[::1]:443") },
		func(c *Config) { c.Host = "203.0.113.2" },
		func(c *Config) { c.Host = "bad\r\nhost" },
		func(c *Config) { c.LastPort = c.FirstPort + 32 },
		func(c *Config) { c.FirstPort = 0 },
		func(c *Config) { c.PassLifetime = -time.Second },
		func(c *Config) { c.ReuseTimeout = 61 * time.Second },
	} {
		cfg := fixture()
		mutate(&cfg)
		if m, err := New(cfg); err == nil {
			m.Close()
			t.Fatalf("accepted unsupported configuration: %+v", cfg)
		}
	}
	m := fakeManager(t, fixture())
	m.dial = func(context.Context, uint16, uint32) (net.Conn, error) {
		t.Fatal("unselected request opened a socket")
		return nil, nil
	}
	for _, dest := range []string{"203.0.113.2:443", "other.example.org:443", "warm.example.org:80", "bad"} {
		if _, err := m.DialContext(context.Background(), dest); !errors.Is(err, ErrTarget) {
			t.Fatalf("%s: %v", dest, err)
		}
	}
	if m.Active() != 0 {
		t.Fatal("unselected requests leased ports")
	}
}

func TestIntegratedPassHasNoFixedLifetime(t *testing.T) {
	cfg := fixture()
	cfg.PassLifetime = 0
	cfg.WarmTimeout = 15 * time.Second
	cfg.ReuseTimeout = 10 * time.Second
	m := fakeManager(t, cfg)
	c, err := m.DialContext(context.Background(), cfg.Host+":443")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.(*Conn).timer != nil {
		t.Fatal("ordinary pass has an absolute expiry")
	}
}

func TestWarmThenMarkedPassOnExactlyTheSameTuple(t *testing.T) {
	cfg := fixture()
	m := fakeManager(t, cfg)
	var calls []uint32
	var sockets []*fakeConn
	m.dial = func(_ context.Context, port uint16, mark uint32) (net.Conn, error) {
		if port != cfg.FirstPort {
			t.Fatal("source port changed")
		}
		calls = append(calls, mark)
		c := fakeSocket(t, cfg, port)
		sockets = append(sockets, c)
		return c, nil
	}
	m.warm = func(_ context.Context, c net.Conn) error {
		if len(calls) != 1 || c != sockets[0] {
			t.Fatal("pass dial happened before warm completed")
		}
		return nil
	}
	c, err := m.DialContext(context.Background(), "WARM.EXAMPLE.ORG.:443")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != 0 || calls[1] != config.SelfDialRelayMark || !sockets[0].closed.Load() || sockets[1].closed.Load() {
		t.Fatalf("incorrect lifecycle: %v", calls)
	}
	if m.Active() != 1 {
		t.Fatal("pass lost its lease")
	}
	c.Close()
	c.Close()
	if m.Active() != 0 || !sockets[1].closed.Load() {
		t.Fatal("closing pass did not release the lease")
	}
}

func TestFreshTupleSkipsReconnectSettle(t *testing.T) {
	cfg := fixture()
	cfg.ReconnectDelay = 300 * time.Millisecond
	cfg.ReuseTimeout = time.Second
	cfg.WarmAge = time.Minute
	m := fakeManager(t, cfg)
	started := time.Now()
	first, err := m.DialContext(context.Background(), cfg.Host+":443")
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(started); took < cfg.ReconnectDelay || !first.(*Conn).Warmed {
		t.Fatalf("the warming call took %s with warmed=%t, want the %s settle after its warm", took, first.(*Conn).Warmed, cfg.ReconnectDelay)
	}
	first.Close()
	started = time.Now()
	second, err := m.DialContext(context.Background(), cfg.Host+":443")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if took := time.Since(started); took >= 150*time.Millisecond || second.(*Conn).Warmed {
		t.Fatalf("a reuse of the warmed tuple took %s with warmed=%t, want it to connect without the settle", took, second.(*Conn).Warmed)
	}
}

func TestWarmAgeCountsFromTheWarmSYN(t *testing.T) {
	cfg := fixture()
	cfg.WarmAge = time.Minute
	m := fakeManager(t, cfg)
	var connects []time.Time
	m.dial = func(_ context.Context, port uint16, _ uint32) (net.Conn, error) {
		connects = append(connects, time.Now())
		return fakeSocket(t, cfg, port), nil
	}
	m.warm = func(context.Context, net.Conn) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	}
	c, err := m.DialContext(context.Background(), cfg.Host+":443")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	m.mu.Lock()
	stamp, ok := m.warmed[cfg.FirstPort]
	m.mu.Unlock()
	if !ok || len(connects) == 0 || stamp.After(connects[0]) {
		t.Fatalf("the warm age starts %s after the warm's connect, want it to start no later than the SYN", stamp.Sub(connects[0]))
	}
}

func TestStampedTupleIsNeverWarmedAgain(t *testing.T) {
	cfg := fixture()
	cfg.WarmAge = time.Nanosecond
	m := fakeManager(t, cfg)
	var warms atomic.Int32
	m.warm = func(context.Context, net.Conn) error { warms.Add(1); return nil }
	for i := range 2 {
		c, err := m.DialContext(context.Background(), cfg.Host+":443")
		if err != nil {
			t.Fatal(err)
		}
		if warmed := c.(*Conn).Warmed; warmed != (i == 0) {
			t.Fatalf("call %d reported warmed=%t", i+1, warmed)
		}
		c.Close()
	}
	if warms.Load() != 1 {
		t.Fatalf("a tuple past its age was warmed %d times, want once: an aged tuple is replaced, never warmed again", warms.Load())
	}
}

func TestAgedOutCountsFromTheStamp(t *testing.T) {
	cfg := fixture()
	cfg.WarmAge = time.Minute
	m := fakeManager(t, cfg)
	now := time.Now()
	if m.agedOut(cfg.FirstPort, now) {
		t.Fatal("a tuple that was never warmed counts as aged")
	}
	m.warmed[cfg.FirstPort] = now.Add(-cfg.WarmAge + time.Second)
	if m.agedOut(cfg.FirstPort, now) {
		t.Fatal("a tuple inside its age counts as aged")
	}
	m.warmed[cfg.FirstPort] = now.Add(-cfg.WarmAge)
	if !m.agedOut(cfg.FirstPort, now) {
		t.Fatal("a tuple at its age is still fresh")
	}
	m.cfg.WarmAge = 0
	m.warmed[cfg.FirstPort] = now
	if !m.agedOut(cfg.FirstPort, now) {
		t.Fatal("without a warm age a warmed tuple is still fresh")
	}
}

func TestFailuresNeverFallbackAndReleaseTheLease(t *testing.T) {
	for _, phase := range []string{"warm", "tuple", "reuse", "cancel"} {
		t.Run(phase, func(t *testing.T) {
			cfg := fixture()
			cfg.ReuseTimeout = 65 * time.Millisecond
			m := fakeManager(t, cfg)
			var calls atomic.Int32
			failure := errors.New("warm rejected")
			m.dial = func(_ context.Context, port uint16, mark uint32) (net.Conn, error) {
				calls.Add(1)
				if port != cfg.FirstPort {
					t.Fatal("port fallback")
				}
				if mark != 0 {
					return nil, unix.EADDRNOTAVAIL
				}
				c := fakeSocket(t, cfg, port)
				if phase == "tuple" {
					c.remote = &net.TCPAddr{IP: net.IPv4(203, 0, 113, 9), Port: 443}
				}
				return c, nil
			}
			m.warm = func(ctx context.Context, _ net.Conn) error {
				if phase == "warm" {
					return failure
				}
				if phase == "cancel" {
					<-ctx.Done()
					return ctx.Err()
				}
				return nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			c, err := m.DialContext(ctx, "warm.example.org:443")
			if err == nil || c != nil {
				t.Fatal("failure returned a pass connection")
			}
			if phase != "reuse" && calls.Load() != 1 {
				t.Fatalf("warm failure opened another socket: %d", calls.Load())
			}
			if phase == "reuse" && calls.Load() < 3 {
				t.Fatal("TIME_WAIT bind failures were not retried")
			}
			if phase == "reuse" && (!errors.Is(err, unix.EADDRNOTAVAIL) || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "EADDRNOTAVAIL") || !strings.Contains(err.Error(), "TCP timestamps")) {
				t.Fatalf("a reconnect that kept getting EADDRNOTAVAIL ended with %q, want it to name EADDRNOTAVAIL and what holds the tuple, and to wrap both the deadline and the last connect error", err)
			}
			if m.Active() != 0 {
				t.Fatal("failure leaked a lease")
			}
		})
	}
}

func TestWarmWithoutTimestampsStopsBeforeTLS(t *testing.T) {
	cfg := fixture()
	m := fakeManager(t, cfg)
	var warms, dials atomic.Int32
	var sockets []*fakeConn
	m.dial = func(_ context.Context, port uint16, _ uint32) (net.Conn, error) {
		dials.Add(1)
		c := fakeSocket(t, cfg, port)
		sockets = append(sockets, c)
		return c, nil
	}
	m.warm = func(context.Context, net.Conn) error { warms.Add(1); return nil }
	m.timestamped = func(net.Conn) (bool, error) { return false, nil }
	c, err := m.DialContext(context.Background(), cfg.Host+":443")
	if c != nil || !errors.Is(err, ErrNoTimestamps) || !errors.Is(err, ErrWarm) {
		t.Fatalf("a warm connection without TCP timestamps gave %v, %v, want ErrNoTimestamps counted as a failed warm", c, err)
	}
	if warms.Load() != 0 || dials.Load() != 1 || !sockets[0].closed.Load() || m.Active() != 0 {
		t.Fatalf("without TCP timestamps the manager ran %d warms over %d sockets with the warm socket closed=%t and %d leases, want no TLS, one closed socket and no lease", warms.Load(), dials.Load(), sockets[0].closed.Load(), m.Active())
	}
	m.timestamped = func(net.Conn) (bool, error) { return false, errors.New("getsockopt: operation not supported") }
	c, err = m.DialContext(context.Background(), cfg.Host+":443")
	if err != nil {
		t.Fatalf("an unreadable TCP_INFO blocked the warm: %v", err)
	}
	defer c.Close()
	if warms.Load() != 1 || !c.(*Conn).Warmed {
		t.Fatalf("an unreadable TCP_INFO gave %d warms, want the warm to run", warms.Load())
	}
}

func TestNegotiatedTimestampsReadsTCPInfo(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			close(accepted)
			return
		}
		accepted <- c
	}()
	c, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if peer, ok := <-accepted; ok {
		defer peer.Close()
	}
	sysctl, err := os.ReadFile("/proc/sys/net/ipv4/tcp_timestamps")
	if err != nil {
		t.Skipf("cannot read net.ipv4.tcp_timestamps: %v", err)
	}
	got, err := negotiatedTimestamps(c)
	if want := strings.TrimSpace(string(sysctl)) != "0"; err != nil || got != want {
		t.Fatalf("TCP_INFO gave timestamps=%t, %v on a loopback connection with net.ipv4.tcp_timestamps=%s, want %t", got, err, strings.TrimSpace(string(sysctl)), want)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if _, err := negotiatedTimestamps(a); err == nil {
		t.Fatal("a connection without a socket reported its TCP options")
	}
}

func TestTCPTimestampsSysctl(t *testing.T) {
	dir := t.TempDir()
	for value, want := range map[string]bool{"0\n": false, "1\n": true, "2\n": true} {
		path := filepath.Join(dir, "tcp_timestamps")
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := tcpTimestampsOn(path); got != want {
			t.Fatalf("net.ipv4.tcp_timestamps=%q reads as on=%t, want %t", strings.TrimSpace(value), got, want)
		}
	}
	if !tcpTimestampsOn(filepath.Join(dir, "missing")) {
		t.Fatal("an unreadable net.ipv4.tcp_timestamps counts as off, want on so the warm still runs")
	}
}

func TestOnlyServerOutcomesAreErrWarm(t *testing.T) {
	refused := errors.New("connect: connection refused")
	for name, c := range map[string]struct {
		setup  func(*testing.T, *Manager, Config)
		server bool
	}{
		"TLS handshake": {func(_ *testing.T, m *Manager, _ Config) {
			m.warm = func(context.Context, net.Conn) error {
				return errors.New("remote error: tls: protocol version not supported")
			}
		}, true},
		"HTTP status": {func(_ *testing.T, m *Manager, _ Config) {
			m.warm = func(context.Context, net.Conn) error { return errors.New("warm HTTP status 404, expected 204") }
		}, true},
		"warm timeout": {func(_ *testing.T, m *Manager, _ Config) {
			m.warm = func(ctx context.Context, _ net.Conn) error { <-ctx.Done(); return ctx.Err() }
		}, true},
		"refused connect": {func(_ *testing.T, m *Manager, _ Config) {
			m.dial = func(context.Context, uint16, uint32) (net.Conn, error) { return nil, refused }
		}, true},
		"flow registration": {func(_ *testing.T, m *Manager, _ Config) {
			m.capture = func() (func(), error) { return nil, errors.New("warm flow is already registered or registry is full") }
		}, false},
		"different tuple": {func(t *testing.T, m *Manager, cfg Config) {
			m.dial = func(_ context.Context, port uint16, _ uint32) (net.Conn, error) {
				c := fakeSocket(t, cfg, port)
				c.remote = &net.TCPAddr{IP: net.IPv4(203, 0, 113, 9), Port: 443}
				return c, nil
			}
		}, false},
		"source port held": {func(_ *testing.T, m *Manager, _ Config) {
			m.dial = func(context.Context, uint16, uint32) (net.Conn, error) { return nil, unix.EADDRINUSE }
		}, false},
		"pass": {func(t *testing.T, m *Manager, cfg Config) {
			m.dial = func(_ context.Context, port uint16, mark uint32) (net.Conn, error) {
				if mark != cfg.WarmMark {
					return nil, unix.EADDRNOTAVAIL
				}
				return fakeSocket(t, cfg, port), nil
			}
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := fixture()
			cfg.WarmTimeout, cfg.ReuseTimeout = 100*time.Millisecond, 100*time.Millisecond
			m := fakeManager(t, cfg)
			c.setup(t, m, cfg)
			conn, err := m.DialContext(context.Background(), cfg.Host+":443")
			if err == nil || conn != nil {
				t.Fatalf("got %v, %v, want a failure", conn, err)
			}
			if errors.Is(err, ErrWarm) != c.server {
				t.Fatalf("%v counts as a server failure of the warm: %t, want %t", err, errors.Is(err, ErrWarm), c.server)
			}
			if name == "refused connect" && !errors.Is(err, refused) {
				t.Fatalf("%v lost the connect error it wraps", err)
			}
			if m.Active() != 0 {
				t.Fatal("the failure leaked a lease")
			}
		})
	}
}

func TestConcurrentLeaseShutdownAndExpiry(t *testing.T) {
	cfg := fixture()
	m := fakeManager(t, cfg)
	entered := make(chan struct{})
	m.warm = func(ctx context.Context, _ net.Conn) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	done := make(chan error, 1)
	go func() { _, err := m.DialContext(context.Background(), "warm.example.org:443"); done <- err }()
	<-entered
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.DialContext(context.Background(), "warm.example.org:443"); !errors.Is(err, ErrCapacity) {
				t.Errorf("over capacity: %v", err)
			}
		}()
	}
	wg.Wait()
	m.Close()
	select {
	case err := <-done:
		if err == nil || m.Active() != 0 {
			t.Fatal("shutdown did not unwind warm")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown left warm running")
	}
	if _, err := m.DialContext(context.Background(), "warm.example.org:443"); !errors.Is(err, ErrClosed) {
		t.Fatal("closed manager accepted a request")
	}
	for _, closeManager := range []bool{false, true} {
		cfg.PassLifetime = 30 * time.Millisecond
		m := fakeManager(t, cfg)
		c, err := m.DialContext(context.Background(), "warm.example.org:443")
		if err != nil {
			t.Fatal(err)
		}
		if closeManager {
			m.Close()
		}
		deadline := time.Now().Add(time.Second)
		for m.Active() > 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if m.Active() != 0 {
			t.Fatal("pass lifetime or manager shutdown leaked the port")
		}
		c.Close()
	}
}

func TestTrackAfterCloseDoesNotDeadlock(t *testing.T) {
	m := fakeManager(t, fixture())
	l, err := m.reserve()
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	c := &Conn{Conn: fakeSocket(t, fixture(), l.port), lease: l}
	if err := l.track(c); !errors.Is(err, ErrClosed) || m.Active() != 0 {
		t.Fatal("closed track leaked")
	}
}

func TestRetryRecoveryAndCloseAtHandoff(t *testing.T) {
	for _, closeManager := range []bool{false, true} {
		cfg := fixture()
		m := fakeManager(t, cfg)
		entered, release := make(chan struct{}), make(chan struct{})
		var attempts atomic.Int32
		m.dial = func(_ context.Context, port uint16, mark uint32) (net.Conn, error) {
			if port != cfg.FirstPort {
				return nil, errors.New("port fallback")
			}
			if mark != 0 {
				if attempts.Add(1) == 1 {
					return nil, unix.EADDRINUSE
				}
				close(entered)
				<-release
			}
			return fakeSocket(t, cfg, port), nil
		}
		done := make(chan error, 1)
		go func() {
			c, err := m.DialContext(context.Background(), "warm.example.org:443")
			if c != nil {
				c.Close()
			}
			done <- err
		}()
		<-entered
		if closeManager {
			m.Close()
		}
		close(release)
		select {
		case err := <-done:
			if (err != nil) != closeManager || m.Active() != 0 || attempts.Load() != 2 {
				t.Fatalf("handoff close=%v: %v active=%d attempts=%d", closeManager, err, m.Active(), attempts.Load())
			}
		case <-time.After(time.Second):
			t.Fatal("close at handoff deadlocked")
		}
	}
}
