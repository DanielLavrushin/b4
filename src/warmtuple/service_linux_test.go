package warmtuple

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"golang.org/x/sys/unix"
)

func serviceFixture(t *testing.T) (*Service, *config.Config, *config.SetConfig, *atomic.Int32) {
	t.Helper()
	cfg := config.NewConfig()
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "reuse", "reuse", true
	set.Targets.IPs = []string{"203.0.113.2"}
	set.Targets.IpsToMatch = set.Targets.IPs
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	set.TCP.WSSizeReuse.Enabled = true
	cfg.Sets = []*config.SetConfig{&set}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	s := NewService(NewFlows())
	t.Cleanup(func() { s.Close() })
	s.UpdateConfig(&cfg)
	s.route = func(context.Context, netip.AddrPort, netip.Addr) (string, error) { return "route-1", nil }
	s.source = func(context.Context, netip.AddrPort) (netip.Addr, error) {
		return netip.MustParseAddr("203.0.113.1"), nil
	}
	s.timestamps = func() bool { return true }
	s.roots = func() (*x509.CertPool, bool) { return nil, true }
	count := &atomic.Int32{}
	s.newManager = func(c Config) (*Manager, error) {
		c.ReconnectDelay = 20 * time.Millisecond
		m := fakeManager(t, c)
		m.warm = func(context.Context, net.Conn) error { count.Add(1); return nil }
		return m, nil
	}
	s.direct = func(context.Context, netip.Addr, netip.AddrPort, uint32, func(netip.AddrPort) (func(), error)) (net.Conn, func(), error) {
		t.Error("a connection fell back to the set's strategy where no fallback is expected")
		return nil, nil, errors.New("unexpected fallback")
	}
	return s, &cfg, &set, count
}

type directRecorder struct {
	mu      sync.Mutex
	next    uint16
	fail    error
	marks   []uint32
	sources []netip.Addr
	locals  []netip.AddrPort
	conns   []*fakeConn
}

func recordDirect(t *testing.T, s *Service) *directRecorder {
	t.Helper()
	r := &directRecorder{next: 40000}
	s.direct = func(_ context.Context, source netip.Addr, target netip.AddrPort, mark uint32, register func(netip.AddrPort) (func(), error)) (net.Conn, func(), error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.marks = append(r.marks, mark)
		r.sources = append(r.sources, source)
		if r.fail != nil {
			return nil, nil, r.fail
		}
		local := netip.AddrPortFrom(source, r.next)
		r.next++
		release, err := register(local)
		if err != nil {
			return nil, nil, err
		}
		a, b := net.Pipe()
		t.Cleanup(func() { a.Close(); b.Close() })
		c := &fakeConn{Conn: a, local: net.TCPAddrFromAddrPort(local), remote: net.TCPAddrFromAddrPort(target)}
		r.locals = append(r.locals, local)
		r.conns = append(r.conns, c)
		return c, release, nil
	}
	return r
}

func (r *directRecorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.marks)
}

func isFallback(c net.Conn) bool {
	f, ok := c.(interface{ Fallback() bool })
	return ok && f.Fallback()
}

func savedConfig(cfg *config.Config, edit func(*config.SetConfig)) *config.Config {
	n := *cfg
	set := *cfg.Sets[0]
	edit(&set)
	n.Sets = append([]*config.SetConfig{&set}, cfg.Sets[1:]...)
	return &n
}

func serviceDial(t *testing.T, s *Service, set *config.SetConfig) net.Conn {
	t.Helper()
	c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443"))
	if err != nil {
		t.Fatal(err)
	}
	c.(*serviceConn).received.Store(1)
	t.Cleanup(func() { c.Close() })
	return c
}

func TestIdleLeaseReuseAndAge(t *testing.T) {
	s, _, set, count := serviceFixture(t)
	first := serviceDial(t, s, set)
	local := first.LocalAddr().String()
	first.Close()
	second := serviceDial(t, s, set)
	if second.LocalAddr().String() != local || count.Load() != 1 {
		t.Fatal("a fresh free tuple was not reused")
	}
	second.Close()
	aged := second.(*serviceConn).lease
	s.mu.Lock()
	for _, l := range s.leases {
		l.manager.mu.Lock()
		l.manager.warmed[l.port] = time.Now().Add(-l.manager.cfg.WarmAge - time.Second)
		l.manager.mu.Unlock()
	}
	s.mu.Unlock()
	third := serviceDial(t, s, set)
	if third.LocalAddr().String() == local || count.Load() != 2 {
		t.Fatalf("after its age the free tuple %s gave %s with %d warms, want a warm on a new source port", local, third.LocalAddr(), count.Load())
	}
	s.mu.Lock()
	until := s.cooling[coolKey{port: aged.port, source: aged.key.source, target: aged.key.target}]
	_, kept := s.leases[aged.port]
	s.mu.Unlock()
	if kept || time.Until(until) <= 9*time.Minute {
		t.Fatalf("the aged tuple is kept=%t and cools for %s, want it dropped and cooling about 10 minutes for its destination", kept, time.Until(until))
	}
	if s.flows.Active() != 0 {
		t.Fatal("profile survived warm completion")
	}
}

func TestActiveExpiryNeverInterruptsAndConcurrentTuplesDiffer(t *testing.T) {
	s, _, set, count := serviceFixture(t)
	first := serviceDial(t, s, set)
	entry := first.(*serviceConn).lease
	entry.manager.mu.Lock()
	entry.manager.warmed[entry.port] = time.Now().Add(-301 * time.Second)
	entry.manager.mu.Unlock()
	second := serviceDial(t, s, set)
	if first.LocalAddr().String() == second.LocalAddr().String() || count.Load() != 2 {
		t.Fatal("concurrent requests shared an active tuple")
	}
	if first.(*serviceConn).Conn.(*Conn).Conn.(*fakeConn).closed.Load() {
		t.Fatal("active expiry closed the pass")
	}
	first.Close()
	second.Close()
	if entry.manager.Active() != 0 {
		t.Fatal("closed pass leaked its lease")
	}
}

func TestServiceConfigChangeInvalidatesIdleAndCancelsWarm(t *testing.T) {
	s, cfg, set, _ := serviceFixture(t)
	first := serviceDial(t, s, set)
	first.Close()
	old := first.LocalAddr().String()
	next := savedConfig(cfg, func(set *config.SetConfig) { set.TCP.Seg2Delay++ })
	s.UpdateConfig(next)
	second := serviceDial(t, s, next.Sets[0])
	if second.LocalAddr().String() == old {
		t.Fatal("changed config reused old tuple")
	}
	entered := make(chan struct{})
	s.newManager = func(c Config) (*Manager, error) {
		m := fakeManager(t, c)
		m.warm = func(ctx context.Context, _ net.Conn) error { close(entered); <-ctx.Done(); return ctx.Err() }
		return m, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.Dial(context.Background(), next.Sets[0], "192.0.2.11", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443"))
		done <- err
	}()
	<-entered
	s.UpdateConfig(cfg)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("configuration change did not cancel pending warm")
		}
	case <-time.After(time.Second):
		t.Fatal("pending warm stuck")
	}
	if s.flows.Active() != 0 {
		t.Fatal("cancelled profile leaked")
	}
	if second.(*serviceConn).Conn.(*Conn).Conn.(*fakeConn).closed.Load() {
		t.Fatal("configuration update interrupted active pass")
	}
}

func TestServiceSelectionAndClose(t *testing.T) {
	s, _, set, count := serviceFixture(t)
	if _, err := s.Dial(context.Background(), set, "client", "warm.example.org", netip.MustParseAddrPort("203.0.113.3:443")); !errors.Is(err, ErrTarget) {
		t.Fatal("unselected target accepted", err)
	}
	if count.Load() != 0 {
		t.Fatal("unselected target warmed")
	}
	c := serviceDial(t, s, set)
	s.Close()
	c.Close()
	if _, err := s.Dial(context.Background(), set, "client", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443")); !errors.Is(err, ErrClosed) {
		t.Fatal("closed service dialed", err)
	}
	if s.flows.Active() != 0 || len(s.leases) != 0 {
		t.Fatal("shutdown left state")
	}
}

func TestServiceClientBudget(t *testing.T) {
	s, _, _, _ := serviceFixture(t)
	var releases []func()
	for i := 0; i < 256; i++ {
		release, ok := s.AcquireClient()
		if !ok {
			t.Fatal("early limit")
		}
		releases = append(releases, release)
	}
	if _, ok := s.AcquireClient(); ok {
		t.Fatal("client budget exceeded")
	}
	for _, release := range releases {
		release()
		release()
	}
	release, ok := s.AcquireClient()
	if !ok {
		t.Fatal("client slots leaked")
	}
	release()
}

func TestFailedWarmsCoolOnlyTheirDestination(t *testing.T) {
	s, _, set, count := serviceFixture(t)
	set.Targets.IPs = []string{"203.0.113.2", "203.0.113.3"}
	set.Targets.IpsToMatch = set.Targets.IPs
	failing := netip.MustParseAddrPort("203.0.113.2:443")
	s.newManager = func(c Config) (*Manager, error) {
		c.ReconnectDelay = 20 * time.Millisecond
		m := fakeManager(t, c)
		m.warm = func(context.Context, net.Conn) error {
			if c.Target == failing {
				return errors.New("remote error: tls: handshake failure")
			}
			count.Add(1)
			return nil
		}
		return m, nil
	}
	recordDirect(t, s)
	failures := 0
	for i := 0; i <= lastPort-firstPort; i++ {
		c, err := s.Dial(context.Background(), set, "192.0.2.10", fmt.Sprintf("site%d.example.org", i), failing)
		if err != nil {
			t.Fatalf("a failed warm closed the client: %v", err)
		}
		if isFallback(c) {
			failures++
		}
		c.Close()
	}
	if failures != lastPort-firstPort+1 {
		t.Fatalf("%d of %d warms to the failing address failed", failures, lastPort-firstPort+1)
	}
	c, err := s.Dial(context.Background(), set, "192.0.2.11", "warm.example.org", netip.MustParseAddrPort("203.0.113.3:443"))
	if err != nil {
		t.Fatalf("after %d failed warms to %s, a warm to another address got: %v", failures, failing, err)
	}
	defer c.Close()
	backend, ok := c.(*serviceConn)
	if !ok {
		t.Fatalf("the other address got a %T, not a warmed pass", c)
	}
	pass, ok := backend.Conn.(*Conn)
	if !ok || !pass.Warmed || count.Load() != 1 {
		t.Fatalf("the other address got %T warmed=%t after %d good warms, want a pass on a tuple warmed by this call", backend.Conn, ok && pass.Warmed, count.Load())
	}
}

func TestCoolingDropsExpiredEntriesPastItsLimit(t *testing.T) {
	s, _, set, _ := serviceFixture(t)
	c := serviceDial(t, s, set)
	lease := c.(*serviceConn).lease
	c.Close()
	expired := time.Now().Add(-time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range coolingPrune {
		s.cooling[coolKey{port: firstPort, source: lease.key.source, target: netip.AddrPortFrom(netip.AddrFrom4([4]byte{198, 18, byte(i >> 8), byte(i)}), 443)}] = expired
	}
	s.discardLocked(lease, time.Now())
	if len(s.cooling) != 1 {
		t.Fatalf("%d cooling entries after a discard past the limit, want only the new one", len(s.cooling))
	}
	if until := s.cooling[coolKey{port: lease.port, source: lease.key.source, target: lease.key.target}]; !time.Now().Before(until) {
		t.Fatal("the discarded tuple is not cooling for its destination")
	}
}

func TestRouteChangeAndSilentPassInvalidateCache(t *testing.T) {
	s, _, set, count := serviceFixture(t)
	first := serviceDial(t, s, set)
	local := first.LocalAddr().String()
	first.Close()
	s.route = func(context.Context, netip.AddrPort, netip.Addr) (string, error) { return "route-2", nil }
	second := serviceDial(t, s, set)
	if second.LocalAddr().String() == local || count.Load() != 2 {
		t.Fatal("changed egress reused a warm tuple")
	}
	second.(*serviceConn).received.Store(0)
	second.Close()
	third := serviceDial(t, s, set)
	if count.Load() != 3 {
		t.Fatal("silent failed pass retained warm status")
	}
	if third.LocalAddr().String() == second.LocalAddr().String() {
		t.Fatalf("after a silent pass the next connection warmed the same tuple %s again, want a new source port", third.LocalAddr())
	}
	third.Close()
}

type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLog) warnings(substr string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, line := range strings.Split(b.buf.String(), "\n") {
		if strings.Contains(line, "[WARN]") && strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

func captureWarnings(t *testing.T) *lockedLog {
	t.Helper()
	previous := log.Level(log.CurLevel.Load())
	out := &lockedLog{}
	log.Init(out, log.LevelInfo, true)
	t.Cleanup(func() { log.Init(os.Stderr, previous, true) })
	return out
}

func failWarms(t *testing.T, s *Service, cause error) {
	t.Helper()
	s.newManager = func(c Config) (*Manager, error) {
		c.ReconnectDelay = 20 * time.Millisecond
		m := fakeManager(t, c)
		m.warm = func(context.Context, net.Conn) error { return cause }
		return m, nil
	}
}

func (s *Service) fallbackCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fallbacks
}

func TestWarmFailureRelaysThroughSetStrategy(t *testing.T) {
	for name, cause := range map[string]error{
		"no response": io.ErrUnexpectedEOF,
		"certificate": &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}},
		"TLS version": tls.AlertError(70),
		"timeout":     context.DeadlineExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			s, _, set, _ := serviceFixture(t)
			set.TCP.IPBlockDetect.Enabled = true
			failWarms(t, s, cause)
			direct := recordDirect(t, s)
			target := netip.MustParseAddrPort("203.0.113.2:443")
			c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", target)
			if err != nil {
				t.Fatalf("a failed warm closed the client: %v", err)
			}
			if !isFallback(c) || direct.calls() != 1 {
				t.Fatalf("got a %T after %d fallback dials, want one connection relayed with the set's strategy", c, direct.calls())
			}
			if direct.marks[0] != config.SelfDialMark || direct.sources[0] != netip.MustParseAddr("203.0.113.1") {
				t.Fatalf("the fallback dialed from %s with mark %#x, want the warm's source address and the self-dial mark %#x", direct.sources[0], direct.marks[0], config.SelfDialMark)
			}
			local := direct.locals[0]
			profile := s.flows.Lookup(local, target)
			if profile == nil {
				t.Fatal("the fallback connection has no strategy profile, so the queue would leave it without the set's strategy")
			}
			got := profile.Config.Sets[0].TCP
			if got.WSSizeReuse.Enabled || got.WSSize != set.TCP.WSSize || got.IPBlockDetect.Enabled {
				t.Fatalf("the fallback profile has reuse=%t wssize=%+v block detection=%t, want reuse off, the set's %+v and no IP Block Detection", got.WSSizeReuse.Enabled, got.WSSize, got.IPBlockDetect.Enabled, set.TCP.WSSize)
			}
			c.Close()
			c.Close()
			if s.flows.Lookup(local, target) != nil || s.flows.Active() != 0 || s.fallbackCount() != 0 {
				t.Fatalf("the closed fallback left %d flows and %d fallbacks", s.flows.Active(), s.fallbackCount())
			}
		})
	}
}

func TestCancelledDialNeverFallsBack(t *testing.T) {
	t.Run("warm", func(t *testing.T) {
		s, _, set, _ := serviceFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s.newManager = func(c Config) (*Manager, error) {
			m := fakeManager(t, c)
			m.warm = func(warmCtx context.Context, _ net.Conn) error { cancel(); <-warmCtx.Done(); return warmCtx.Err() }
			return m, nil
		}
		if c, err := s.Dial(ctx, set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443")); err == nil || c != nil {
			t.Fatalf("a client that hung up during the warm got %T, %v", c, err)
		}
	})
	t.Run("egress", func(t *testing.T) {
		s, _, set, _ := serviceFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		s.route = func(context.Context, netip.AddrPort, netip.Addr) (string, error) {
			cancel()
			return "", context.Canceled
		}
		if c, err := s.Dial(ctx, set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443")); err == nil || c != nil {
			t.Fatalf("a client that hung up during the egress lookup got %T, %v", c, err)
		}
	})
}

func TestEgressLookupErrorFallsBack(t *testing.T) {
	s, _, set, count := serviceFixture(t)
	direct := recordDirect(t, s)
	s.route = func(context.Context, netip.AddrPort, netip.Addr) (string, error) { return "", errors.New("no route") }
	c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443"))
	if err != nil || !isFallback(c) {
		t.Fatalf("an egress lookup error gave %T, %v, want a relay with the set's strategy", c, err)
	}
	defer c.Close()
	if count.Load() != 0 || direct.calls() != 1 || len(s.leases) != 0 {
		t.Fatalf("an egress lookup error warmed %d times, dialed %d fallbacks and holds %d leases", count.Load(), direct.calls(), len(s.leases))
	}
	s.source = func(context.Context, netip.AddrPort) (netip.Addr, error) {
		return netip.Addr{}, errors.New("no source")
	}
	if c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443")); err == nil || c != nil {
		t.Fatalf("without a source address the client got %T, %v, want it closed", c, err)
	}
	if direct.calls() != 1 {
		t.Fatal("a fallback was dialed without a source address")
	}
}

func TestCapacityFallsBack(t *testing.T) {
	logged := captureWarnings(t)
	s, _, set, count := serviceFixture(t)
	direct := recordDirect(t, s)
	target := netip.MustParseAddrPort("203.0.113.2:443")
	s.mu.Lock()
	for port := firstPort; port <= lastPort; port++ {
		s.cooling[coolKey{port: uint16(port), source: netip.MustParseAddr("203.0.113.1"), target: target}] = time.Now().Add(time.Hour)
	}
	s.mu.Unlock()
	for i := range 3 {
		c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", target)
		if err != nil || !isFallback(c) {
			t.Fatalf("connection %d with every warm port cooling got %T, %v, want a relay with the set's strategy", i+1, c, err)
		}
		c.Close()
	}
	if count.Load() != 0 || direct.calls() != 3 {
		t.Fatalf("%d warms and %d fallbacks, want 0 and 3", count.Load(), direct.calls())
	}
	if n := logged.warnings(ErrCapacity.Error()); n != 1 {
		t.Fatalf("%d warnings for 3 connections relayed within a minute because every warm port is leased, want 1", n)
	}
}

func TestServiceCloseEndsFallbackRelays(t *testing.T) {
	s, _, set, _ := serviceFixture(t)
	failWarms(t, s, errors.New("remote error: tls: handshake failure"))
	recordDirect(t, s)
	c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443"))
	if err != nil || !isFallback(c) {
		t.Fatalf("got %T, %v, want a fallback relay", c, err)
	}
	read := make(chan error, 1)
	go func() {
		_, err := c.Read(make([]byte, 1))
		read <- err
	}()
	select {
	case err := <-read:
		t.Fatalf("the fallback relay ended before shutdown: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	s.Close()
	select {
	case err := <-read:
		if err == nil {
			t.Fatal("a fallback read succeeded after shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("Service.Close left a fallback relay open")
	}
	if s.flows.Active() != 0 || s.fallbackCount() != 0 || len(s.directs) != 0 {
		t.Fatalf("shutdown left %d flows, %d fallbacks and %d relays", s.flows.Active(), s.fallbackCount(), len(s.directs))
	}
}

func TestAFailedPassAfterAGoodWarmFallsBack(t *testing.T) {
	s, _, set, count := serviceFixture(t)
	direct := recordDirect(t, s)
	s.newManager = func(c Config) (*Manager, error) {
		c.ReconnectDelay, c.ReuseTimeout = 20*time.Millisecond, 200*time.Millisecond
		m := fakeManager(t, c)
		m.warm = func(context.Context, net.Conn) error { count.Add(1); return nil }
		m.dial = func(_ context.Context, port uint16, mark uint32) (net.Conn, error) {
			if mark != c.WarmMark {
				return nil, unix.EADDRNOTAVAIL
			}
			return fakeSocket(t, c, port), nil
		}
		return m, nil
	}
	target := netip.MustParseAddrPort("203.0.113.2:443")
	c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", target)
	if err != nil || !isFallback(c) {
		t.Fatalf("a pass that failed after a good warm gave %T, %v, want a relay with the set's strategy", c, err)
	}
	defer c.Close()
	if count.Load() != 1 || direct.calls() != 1 || s.flows.Lookup(direct.locals[0], target) == nil {
		t.Fatalf("%d warms and %d fallbacks, want the warm once and one fallback with a profile", count.Load(), direct.calls())
	}
	s.mu.Lock()
	leases := len(s.leases)
	s.mu.Unlock()
	if leases != 0 {
		t.Fatalf("the tuple whose pass failed is still leased (%d leases)", leases)
	}
}

func TestAFullFallbackRegistryClosesWithAPlainError(t *testing.T) {
	t.Run("fallback slots", func(t *testing.T) {
		s, _, set, _ := serviceFixture(t)
		failWarms(t, s, errors.New("remote error: tls: handshake failure"))
		s.mu.Lock()
		s.fallbacks = maxFallbacks
		s.mu.Unlock()
		c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443"))
		if c != nil || !errors.Is(err, ErrFallbackCapacity) || errors.Is(err, ErrWarm) {
			t.Fatalf("with %d fallbacks relaying, a failed warm gave %T, %v, want the client closed with a plain error", maxFallbacks, c, err)
		}
		s.mu.Lock()
		s.fallbacks = 0
		s.mu.Unlock()
	})
	t.Run("flow registry", func(t *testing.T) {
		s, _, set, count := serviceFixture(t)
		direct := recordDirect(t, s)
		var releases []func()
		defer func() {
			for _, release := range releases {
				release()
			}
		}()
		for i := 0; s.flows.Active() < 4096; i++ {
			release, err := s.flows.Register(netip.AddrPortFrom(netip.MustParseAddr("198.51.100.1"), uint16(10000+i)), netip.MustParseAddrPort("198.51.100.2:443"), s.cfg, set, "filler.example.org")
			if err != nil {
				t.Fatal(err)
			}
			releases = append(releases, release)
		}
		c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443"))
		if c != nil || err == nil || errors.Is(err, ErrCapacity) || errors.Is(err, ErrFallbackCapacity) || errors.Is(err, ErrWarm) {
			t.Fatalf("with the flow registry full the client got %T, %v, want it closed with the registry's plain error", c, err)
		}
		if count.Load() != 0 || direct.calls() != 1 || s.fallbackCount() != 0 || s.failureCount() != 0 {
			t.Fatalf("%d warms, %d fallback dials, %d fallbacks held and %d remembered failures, want 0, 1, 0 and 0", count.Load(), direct.calls(), s.fallbackCount(), s.failureCount())
		}
	})
}

func TestFallbackReResolvesTheSet(t *testing.T) {
	t.Run("egress error", func(t *testing.T) {
		s, cfg, set, _ := serviceFixture(t)
		next := savedConfig(cfg, func(set *config.SetConfig) { set.TCP.WSSizeReuse.Enabled = false })
		s.route = func(context.Context, netip.AddrPort, netip.Addr) (string, error) {
			s.UpdateConfig(next)
			return "", errors.New("no route")
		}
		if c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443")); c != nil || !errors.Is(err, ErrTarget) {
			t.Fatalf("a set that stopped reusing during the egress lookup got %T, %v, want ErrTarget", c, err)
		}
	})
	t.Run("warm slots full", func(t *testing.T) {
		s, cfg, set, _ := serviceFixture(t)
		s.UpdateConfig(savedConfig(cfg, func(set *config.SetConfig) { set.TCP.WSSizeReuse.Enabled = false }))
		if c, err := s.Fallback(context.Background(), set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443")); c != nil || !errors.Is(err, ErrTarget) {
			t.Fatalf("a stale reuse set got %T, %v, want ErrTarget", c, err)
		}
		s.Close()
		if c, err := s.Fallback(context.Background(), set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443")); c != nil || !errors.Is(err, ErrClosed) {
			t.Fatalf("a closed service got %T, %v, want ErrClosed", c, err)
		}
	})
}

func TestAnInactiveReuseSetIsNeitherWarmedNorRelayed(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(*config.Config)
	}{
		{"the TUN engine", func(c *config.Config) { c.Queue.Mode = "tun" }},
		{"firewall setup skipped", func(c *config.Config) { c.System.Tables.SkipSetup = true }},
		{"a queue mark with the relay bit", func(c *config.Config) { c.Queue.Mark = 0x48000 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, cfg, set, count := serviceFixture(t)
			idle := serviceDial(t, s, set)
			idle.Close()
			if count.Load() != 1 || len(s.leases) != 1 {
				t.Fatalf("setup: an active reuse set warmed %d times and holds %d leases, want 1 and 1", count.Load(), len(s.leases))
			}
			next := *cfg
			c.edit(&next)
			s.UpdateConfig(&next)
			if len(s.leases) != 0 {
				t.Errorf("%s: a save that keeps reuse from working left %d idle leases", c.name, len(s.leases))
			}
			target := netip.MustParseAddrPort("203.0.113.2:443")
			if conn, err := s.Dial(context.Background(), next.Sets[0], "192.0.2.10", "warm.example.org", target); conn != nil || !errors.Is(err, ErrTarget) {
				t.Errorf("%s: Dial got %T, %v, want ErrTarget; reuse does nothing here", c.name, conn, err)
			}
			if conn, err := s.Fallback(context.Background(), next.Sets[0], "192.0.2.10", "warm.example.org", target); conn != nil || !errors.Is(err, ErrTarget) {
				t.Errorf("%s: Fallback got %T, %v, want ErrTarget; reuse does nothing here", c.name, conn, err)
			}
			if count.Load() != 1 {
				t.Errorf("%s: %d warms, want only the setup's one", c.name, count.Load())
			}
		})
	}
}

func TestFallbackDialerRelaysThroughSetStrategy(t *testing.T) {
	s, _, set, count := serviceFixture(t)
	direct := recordDirect(t, s)
	target := netip.MustParseAddrPort("203.0.113.2:443")
	c, err := s.Fallback(context.Background(), set, "192.0.2.10", "WARM.example.org.", target)
	if err != nil || !isFallback(c) {
		t.Fatalf("got %T, %v, want a relay with the set's strategy", c, err)
	}
	defer c.Close()
	profile := s.flows.Lookup(direct.locals[0], target)
	if count.Load() != 0 || len(s.leases) != 0 || profile == nil || profile.Config.Sets[0].Targets.SNIDomains[0] != "warm.example.org" {
		t.Fatalf("the fallback warmed %d times, leased %d ports and registered %+v", count.Load(), len(s.leases), profile)
	}
	if _, err := s.Fallback(context.Background(), set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:80")); !errors.Is(err, ErrTarget) {
		t.Fatalf("a fallback to port 80 got %v, want ErrTarget", err)
	}
}

func (s *Service) failureCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.failures)
}

func failWarmsCounted(t *testing.T, s *Service, cause error) *atomic.Int32 {
	t.Helper()
	warms := &atomic.Int32{}
	s.newManager = func(c Config) (*Manager, error) {
		c.ReconnectDelay = 20 * time.Millisecond
		m := fakeManager(t, c)
		m.warm = func(context.Context, net.Conn) error { warms.Add(1); return cause }
		return m, nil
	}
	return warms
}

func dialExpectingFallback(t *testing.T, s *Service, set *config.SetConfig, host string) {
	t.Helper()
	c, err := s.Dial(context.Background(), set, "192.0.2.10", host, netip.MustParseAddrPort("203.0.113.2:443"))
	if err != nil || !isFallback(c) {
		t.Fatalf("%s got %T, %v, want a relay with the set's strategy", host, c, err)
	}
	c.Close()
}

func TestFailedWarmIsRememberedPerAddressAndName(t *testing.T) {
	s, cfg, set, _ := serviceFixture(t)
	set.Targets.UseCollectedIPs = true
	warms := failWarmsCounted(t, s, errors.New("remote error: tls: handshake failure"))
	direct := recordDirect(t, s)
	dialExpectingFallback(t, s, set, "warm.example.org")
	if warms.Load() != 1 || s.failureCount() != 1 {
		t.Fatalf("%d warms and %d remembered failures after the first failed warm, want 1 and 1", warms.Load(), s.failureCount())
	}
	s.mu.Lock()
	cooling := len(s.cooling)
	s.mu.Unlock()
	dialExpectingFallback(t, s, set, "WARM.example.org.")
	s.mu.Lock()
	leases, coolingAfter := len(s.leases), len(s.cooling)
	s.mu.Unlock()
	if warms.Load() != 1 || leases != 0 || coolingAfter != cooling || direct.calls() != 2 {
		t.Fatalf("the remembered address and name warmed %d times, holds %d leases, cools %d ports (%d before) and fell back %d times, want 1 warm, no port and 2 fallbacks", warms.Load(), leases, coolingAfter, cooling, direct.calls())
	}
	dialExpectingFallback(t, s, set, "other.example.org")
	if warms.Load() != 2 {
		t.Fatalf("another name on the same address warmed %d times in all, want a second warm", warms.Load())
	}
	s.mu.Lock()
	s.failures[failureKey{target: netip.MustParseAddr("203.0.113.2"), host: "warm.example.org"}] = time.Now().Add(-time.Second)
	s.mu.Unlock()
	dialExpectingFallback(t, s, set, "warm.example.org")
	if warms.Load() != 3 {
		t.Fatalf("an expired failure still skipped the warm (%d warms)", warms.Load())
	}
	s.UpdateConfig(cfg.WithCollectedIPs([]string{"203.0.113.9"}))
	if s.failureCount() != 2 {
		t.Fatalf("a collected-only update left %d remembered failures, want 2", s.failureCount())
	}
	next := savedConfig(cfg, func(set *config.SetConfig) { set.TCP.Seg2Delay++ })
	s.UpdateConfig(next)
	if s.failureCount() != 0 {
		t.Fatalf("a settings change kept %d remembered failures", s.failureCount())
	}
	dialExpectingFallback(t, s, next.Sets[0], "warm.example.org")
	if warms.Load() != 4 {
		t.Fatalf("after a settings change the address and name warmed %d times in all, want a fourth warm", warms.Load())
	}
}

func TestAWarmCancelledByTheClientOrASaveIsNotRemembered(t *testing.T) {
	target := netip.MustParseAddrPort("203.0.113.2:443")
	t.Run("client hung up", func(t *testing.T) {
		s, _, set, _ := serviceFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		warms := &atomic.Int32{}
		s.newManager = func(c Config) (*Manager, error) {
			c.ReconnectDelay = 20 * time.Millisecond
			m := fakeManager(t, c)
			m.warm = func(warmCtx context.Context, _ net.Conn) error {
				if warms.Add(1) > 1 {
					return nil
				}
				cancel()
				<-warmCtx.Done()
				return warmCtx.Err()
			}
			return m, nil
		}
		if c, err := s.Dial(ctx, set, "192.0.2.10", "warm.example.org", target); c != nil || err == nil {
			t.Fatalf("a client that hung up during the warm got %T, %v", c, err)
		}
		if s.failureCount() != 0 {
			t.Fatalf("a warm the client hung up on left %d remembered failures, so a client action would skip the warm to this address and name for 10 minutes", s.failureCount())
		}
		if c := serviceDial(t, s, set); isFallback(c) || warms.Load() != 2 {
			t.Fatalf("the next connection got %T after %d warms, want a second warm", c, warms.Load())
		}
	})
	t.Run("a save", func(t *testing.T) {
		s, cfg, set, _ := serviceFixture(t)
		entered := make(chan struct{})
		var once sync.Once
		s.newManager = func(c Config) (*Manager, error) {
			c.ReconnectDelay = 20 * time.Millisecond
			m := fakeManager(t, c)
			m.warm = func(warmCtx context.Context, _ net.Conn) error {
				once.Do(func() { close(entered) })
				<-warmCtx.Done()
				return warmCtx.Err()
			}
			return m, nil
		}
		done := make(chan error, 1)
		go func() {
			_, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", target)
			done <- err
		}()
		<-entered
		s.UpdateConfig(savedConfig(cfg, func(set *config.SetConfig) { set.TCP.Seg2Delay++ }))
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("a save did not cancel the pending warm")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the pending warm was not cancelled by the save")
		}
		if s.failureCount() != 0 {
			t.Fatalf("a warm the save cancelled left %d remembered failures after the save forgot the earlier ones", s.failureCount())
		}
	})
}

func TestPassReconnectFailureIsNotRemembered(t *testing.T) {
	s, _, set, warms := serviceFixture(t)
	recordDirect(t, s)
	s.newManager = func(c Config) (*Manager, error) {
		c.ReconnectDelay, c.ReuseTimeout = 20*time.Millisecond, 100*time.Millisecond
		m := fakeManager(t, c)
		m.warm = func(context.Context, net.Conn) error { warms.Add(1); return nil }
		m.dial = func(_ context.Context, port uint16, mark uint32) (net.Conn, error) {
			if mark != c.WarmMark {
				return nil, unix.EADDRNOTAVAIL
			}
			return fakeSocket(t, c, port), nil
		}
		return m, nil
	}
	dialExpectingFallback(t, s, set, "warm.example.org")
	dialExpectingFallback(t, s, set, "warm.example.org")
	if warms.Load() != 2 || s.failureCount() != 0 {
		t.Fatalf("%d warms and %d remembered failures after two failed passes, want 2 and 0", warms.Load(), s.failureCount())
	}
}

func TestACaptureErrorIsNotRemembered(t *testing.T) {
	s, _, set, warms := serviceFixture(t)
	recordDirect(t, s)
	target := netip.MustParseAddrPort("203.0.113.2:443")
	release, err := s.flows.Register(netip.AddrPortFrom(netip.MustParseAddr("203.0.113.1"), firstPort), target, s.cfg, set, "warm.example.org")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	dialExpectingFallback(t, s, set, "warm.example.org")
	if warms.Load() != 0 || s.failureCount() != 0 {
		t.Fatalf("a refused flow registration ran %d warms and left %d remembered failures, want 0 and 0", warms.Load(), s.failureCount())
	}
	c, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", target)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if pass, ok := c.(*serviceConn); !ok || !pass.Conn.(*Conn).Warmed || warms.Load() != 1 {
		t.Fatalf("after a local registration error the next connection got %T with %d warms, want a warmed pass", c, warms.Load())
	}
}

func TestFailureMemoryIsBounded(t *testing.T) {
	s, _, _, _ := serviceFixture(t)
	target := netip.MustParseAddr("203.0.113.2")
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range maxFailures + 1 {
		s.rememberFailureLocked(failureKey{target: target, host: fmt.Sprintf("site%d.example.org", i)}, now.Add(time.Duration(i)*time.Millisecond))
	}
	if len(s.failures) != maxFailures {
		t.Fatalf("%d remembered failures, want at most %d", len(s.failures), maxFailures)
	}
	if _, kept := s.failures[failureKey{target: target, host: "site0.example.org"}]; kept {
		t.Fatal("a full memory kept its earliest expiry and dropped another entry")
	}
	for key := range s.failures {
		s.failures[key] = now.Add(-time.Second)
	}
	s.rememberFailureLocked(failureKey{target: target, host: "fresh.example.org"}, now)
	if len(s.failures) != 1 {
		t.Fatalf("%d remembered failures after a record into a memory of expired ones, want 1", len(s.failures))
	}
}

func TestServiceSkipsWarmWithoutTCPTimestamps(t *testing.T) {
	logged := captureWarnings(t)
	s, _, set, count := serviceFixture(t)
	var managers atomic.Int32
	newManager := s.newManager
	s.newManager = func(c Config) (*Manager, error) {
		managers.Add(1)
		return newManager(c)
	}
	s.timestamps = func() bool { return false }
	direct := recordDirect(t, s)
	for range 3 {
		dialExpectingFallback(t, s, set, "warm.example.org")
	}
	s.mu.Lock()
	leases, cooling := len(s.leases), len(s.cooling)
	s.mu.Unlock()
	if managers.Load() != 0 || count.Load() != 0 || leases != 0 || cooling != 0 || s.failureCount() != 0 || direct.calls() != 3 {
		t.Fatalf("with net.ipv4.tcp_timestamps at 0, 3 connections built %d warm managers, ran %d warms, left %d leases, %d cooling ports and %d remembered failures and fell back %d times, want only 3 fallbacks", managers.Load(), count.Load(), leases, cooling, s.failureCount(), direct.calls())
	}
	if n := logged.warnings("net.ipv4.tcp_timestamps is 0"); n != 1 {
		t.Fatalf("%d warnings for 3 connections without TCP timestamps, want 1", n)
	}
	limiter := s.limiter(warnKey{set: set.Id, kind: "timestamps"})
	limiter.last.Store(time.Now().Add(-2 * time.Minute).UnixNano())
	dialExpectingFallback(t, s, set, "warm.example.org")
	if n := logged.warnings("net.ipv4.tcp_timestamps is 0"); n != 1 {
		t.Fatalf("%d warnings 2 minutes after the first, want the warning at most every 10 minutes", n)
	}
	limiter.last.Store(time.Now().Add(-11 * time.Minute).UnixNano())
	dialExpectingFallback(t, s, set, "warm.example.org")
	if n := logged.warnings("net.ipv4.tcp_timestamps is 0"); n != 2 {
		t.Fatalf("%d warnings 11 minutes after the first, want a second one", n)
	}
	s.timestamps = func() bool { return true }
	c := serviceDial(t, s, set)
	if pass, ok := c.(*serviceConn); !ok || !pass.Conn.(*Conn).Warmed || count.Load() != 1 {
		t.Fatalf("after net.ipv4.tcp_timestamps came back on the next connection got %T with %d warms, want a warmed pass", c, count.Load())
	}
}

func TestServiceWarmUsesB4TLSRoots(t *testing.T) {
	s, _, set, _ := serviceFixture(t)
	_, roots := warmTestCA(t, "warm.example.org")
	s.roots = func() (*x509.CertPool, bool) { return roots, true }
	var captured Config
	newManager := s.newManager
	s.newManager = func(c Config) (*Manager, error) {
		captured = c
		return newManager(c)
	}
	serviceDial(t, s, set)
	if captured.RootCAs == nil || !captured.RootCAs.Equal(roots) {
		t.Fatalf("the warm manager was built with RootCAs set=%t, want b4's CA roots", captured.RootCAs != nil)
	}
}

func TestServiceWithoutCAStoreSkipsWarm(t *testing.T) {
	logged := captureWarnings(t)
	s, _, set, count := serviceFixture(t)
	var managers atomic.Int32
	newManager := s.newManager
	s.newManager = func(c Config) (*Manager, error) {
		managers.Add(1)
		return newManager(c)
	}
	s.roots = func() (*x509.CertPool, bool) { return nil, false }
	direct := recordDirect(t, s)
	for range 3 {
		dialExpectingFallback(t, s, set, "warm.example.org")
	}
	s.mu.Lock()
	leases, cooling := len(s.leases), len(s.cooling)
	s.mu.Unlock()
	if managers.Load() != 0 || count.Load() != 0 || leases != 0 || cooling != 0 || s.failureCount() != 0 || direct.calls() != 3 {
		t.Fatalf("without a CA store, 3 connections built %d warm managers, ran %d warms, left %d leases, %d cooling ports and %d remembered failures and fell back %d times, want only 3 fallbacks", managers.Load(), count.Load(), leases, cooling, s.failureCount(), direct.calls())
	}
	if n := logged.warnings("no CA certificates"); n != 1 {
		t.Fatalf("%d warnings for 3 connections without a CA store, want 1", n)
	}
	s.limiter(warnKey{set: set.Id, kind: "roots"}).last.Store(time.Now().Add(-2 * time.Minute).UnixNano())
	dialExpectingFallback(t, s, set, "warm.example.org")
	if n := logged.warnings("no CA certificates"); n != 1 {
		t.Fatalf("%d warnings 2 minutes after the first, want the warning at most every 10 minutes", n)
	}
}

func TestAWarmWithoutTimestampsIsRemembered(t *testing.T) {
	s, _, set, count := serviceFixture(t)
	s.newManager = func(c Config) (*Manager, error) {
		c.ReconnectDelay = 20 * time.Millisecond
		m := fakeManager(t, c)
		m.warm = func(context.Context, net.Conn) error { count.Add(1); return nil }
		m.timestamped = func(net.Conn) (bool, error) { return false, nil }
		return m, nil
	}
	recordDirect(t, s)
	dialExpectingFallback(t, s, set, "warm.example.org")
	if count.Load() != 0 || s.failureCount() != 1 {
		t.Fatalf("a warm connection without TCP timestamps ran %d TLS warms and left %d remembered failures, want none and 1", count.Load(), s.failureCount())
	}
}

func TestFailedWarmWarnsOncePerMinutePerSet(t *testing.T) {
	logged := captureWarnings(t)
	s, cfg, _, _ := serviceFixture(t)
	n := *cfg
	next := &n
	second := *cfg.Sets[0]
	second.Id, second.Name = "reuse2", "reuse2"
	next.Sets = append(append([]*config.SetConfig{}, cfg.Sets...), &second)
	s.UpdateConfig(next)
	failWarms(t, s, errors.New("remote error: tls: handshake failure"))
	recordDirect(t, s)
	for i := range 3 {
		dialExpectingFallback(t, s, next.Sets[0], fmt.Sprintf("site%d.example.org", i))
	}
	dialExpectingFallback(t, s, next.Sets[0], "site0.example.org")
	dialExpectingFallback(t, s, &second, "other.example.org")
	if n := logged.warnings("failed warms so far"); n != 2 {
		t.Fatalf("%d warnings for 4 failed warms in two sets within a minute, want one per set", n)
	}
	if logged.warnings("WSSIZE reuse [reuse]: warm to") != 1 || logged.warnings("WSSIZE reuse [reuse2]: warm to") != 1 {
		t.Fatal("each set did not get its own warning")
	}
}
