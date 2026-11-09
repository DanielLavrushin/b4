package warmtuple

import (
	"context"
	"errors"
	"net"
	"net/netip"
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
		RetryInterval: 20 * time.Millisecond, PassLifetime: time.Second,
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
		func(c *Config) { c.PassLifetime = 0 },
		func(c *Config) { c.ReuseTimeout = 6 * time.Second },
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
			if c, err := m.DialContext(ctx, "warm.example.org:443"); err == nil || c != nil {
				t.Fatal("failure returned a pass connection")
			}
			if phase != "reuse" && calls.Load() != 1 {
				t.Fatalf("warm failure opened another socket: %d", calls.Load())
			}
			if phase == "reuse" && calls.Load() < 3 {
				t.Fatal("TIME_WAIT bind failures were not retried")
			}
			if m.Active() != 0 {
				t.Fatal("failure leaked a lease")
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

func TestQueueProfileHasOnlyExplicitWarmManipulation(t *testing.T) {
	cfg := QueueConfig(fixture().Target, 541)
	s := cfg.Sets[0]
	if len(cfg.Sets) != 1 || len(s.Targets.IpsToMatch) != 1 || s.Targets.IpsToMatch[0] != "203.0.113.2" || s.TCP.DPortFilter != "443" ||
		s.Targets.DomainOnly || s.Routing.Enabled || s.Faking.SNI || s.TCP.SynFake || s.TCP.Duplicate.Enabled || s.TCP.Win.Mode != config.ConfigOff ||
		!s.TCP.WSSize.Enabled || s.TCP.WSSize.Cutoff != "client_hello" || s.TCP.WSSize.Scale != 1 || s.TCP.WSSize.Window != 1 ||
		s.Fragmentation.Strategy != "combo" || s.Fragmentation.SeqOverlapLength != 1 || !s.Fragmentation.Combo.FirstByteSplit ||
		!cfg.Queue.IsDiscovery || !cfg.System.DNS.TCPDisabled {
		t.Fatalf("unexpected experiment profile: %+v", s)
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
