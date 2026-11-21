package warmtuple

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestServiceCollectedUpdatePreservesIdleLease(t *testing.T) {
	s, cfg, set, count := serviceFixture(t)
	set.Targets.UseCollectedIPs = true
	first := serviceDial(t, s, set)
	local := first.LocalAddr().String()
	first.Close()
	next := cfg.WithCollectedIPs([]string{"203.0.113.3"})
	s.UpdateConfig(next)
	second := serviceDial(t, s, next.Sets[0])
	if second.LocalAddr().String() != local || count.Load() != 1 {
		t.Fatal("membership update discarded the compatible warmed tuple")
	}
	second.Close()
	s.UpdateConfig(cfg.WithCollectedIPs([]string{"203.0.113.3"}))
	third := serviceDial(t, s, next.Sets[0])
	if third.LocalAddr().String() != local || count.Load() != 1 {
		t.Fatal("equal configuration discarded the compatible warmed tuple")
	}
}

func TestServiceCollectedUpdatePreservesPendingWarm(t *testing.T) {
	s, cfg, set, _ := serviceFixture(t)
	set.Targets.UseCollectedIPs = true
	entered := make(chan struct{})
	resume := make(chan struct{})
	s.newManager = func(c Config) (*Manager, error) {
		c.ReconnectDelay = 20 * time.Millisecond
		m := fakeManager(t, c)
		m.warm = func(ctx context.Context, _ net.Conn) error {
			close(entered)
			select {
			case <-resume:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return m, nil
	}
	type result struct {
		conn net.Conn
		err  error
	}
	done := make(chan result, 1)
	go func() {
		c, err := s.Dial(context.Background(), set, "client", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443"))
		done <- result{c, err}
	}()
	<-entered
	s.UpdateConfig(cfg.WithCollectedIPs([]string{"203.0.113.3"}))
	s.UpdateConfig(cfg.WithCollectedIPs(nil))
	close(resume)
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal("compatible pending warm was cancelled", got.err)
		}
		got.conn.(*serviceConn).Answered()
		got.conn.Close()
	case <-time.After(time.Second):
		t.Fatal("compatible pending warm stuck")
	}
}

func TestServiceCollectedRemovalDropsOnlyRemovedTargets(t *testing.T) {
	s, cfg, _, count := serviceFixture(t)
	next := cfg.Clone()
	set := next.Sets[0]
	set.Targets.IPs = nil
	set.Targets.IpsToMatch = nil
	set.Targets.UseCollectedIPs = true
	current := next.WithCollectedIPs([]string{"203.0.113.2", "203.0.113.3"})
	s.UpdateConfig(current)
	first := serviceDial(t, s, current.Sets[0])
	lease := first.(*serviceConn).lease
	s.UpdateConfig(next.WithCollectedIPs([]string{"203.0.113.3"}))
	if first.(*serviceConn).Conn.(*Conn).Conn.(*fakeConn).closed.Load() {
		t.Fatal("membership removal closed an active pass")
	}
	first.Close()
	if _, ok := s.leases[lease.port]; ok {
		t.Fatal("removed target retained idle lease")
	}
	if _, err := s.Dial(context.Background(), current.Sets[0], "client", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443")); err != ErrTarget {
		t.Fatal("removed target remained eligible", err)
	}
	if count.Load() != 1 {
		t.Fatal("removed target was warmed")
	}
}

func TestServiceCollectedRemovalCancelsPendingTarget(t *testing.T) {
	s, cfg, _, _ := serviceFixture(t)
	next := cfg.Clone()
	next.Sets[0].Targets.IPs = nil
	next.Sets[0].Targets.IpsToMatch = nil
	next.Sets[0].Targets.UseCollectedIPs = true
	current := next.WithCollectedIPs([]string{"203.0.113.2"})
	s.UpdateConfig(current)
	entered := make(chan struct{})
	done := make(chan error, 1)
	s.newManager = func(c Config) (*Manager, error) {
		c.ReconnectDelay = 20 * time.Millisecond
		m := fakeManager(t, c)
		m.warm = func(ctx context.Context, _ net.Conn) error { close(entered); <-ctx.Done(); return ctx.Err() }
		return m, nil
	}
	go func() {
		_, err := s.Dial(context.Background(), current.Sets[0], "client", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443"))
		done <- err
	}()
	<-entered
	s.UpdateConfig(next.WithCollectedIPs(nil))
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("removed target completed a pending warm")
		}
	case <-time.After(time.Second):
		t.Fatal("removed target warm stuck")
	}
}
