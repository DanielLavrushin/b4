package warmtuple

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
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
	count := &atomic.Int32{}
	s.newManager = func(c Config) (*Manager, error) {
		c.ReconnectDelay = 20 * time.Millisecond
		m := fakeManager(t, c)
		m.warm = func(context.Context, net.Conn) error { count.Add(1); return nil }
		return m, nil
	}
	return s, &cfg, &set, count
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
	s.mu.Lock()
	for _, l := range s.leases {
		l.manager.mu.Lock()
		l.manager.warmed[l.port] = time.Now().Add(-301 * time.Second)
		l.manager.mu.Unlock()
	}
	s.mu.Unlock()
	third := serviceDial(t, s, set)
	if third.LocalAddr().String() != local || count.Load() != 2 {
		t.Fatal("stale free tuple was not warmed again")
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
	next := cfg.Clone()
	next.Sets[0].TCP.Seg2Delay++
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
	third.Close()
}
