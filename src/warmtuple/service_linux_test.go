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
)

func serviceFixture(t *testing.T) (*Service, *config.Config, *atomic.Int32) {
	t.Helper()
	cfg := config.NewConfig()
	set := config.NewSetConfig()
	set.Id = "warm"
	set.Name = "Warm"
	set.TCP.WarmTuple.Enabled = true
	set.Targets.SNIDomains = []string{"googlevideo.com"}
	set.Targets.DomainsToMatch = []string{"googlevideo.com"}
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1, Cutoff: "client_hello"}
	cfg.Sets = []*config.SetConfig{&set}
	cfg.System.Socks5.WarmTuple = config.Socks5WarmTupleConfig{FirstPort: 40000, LastPort: 40002, CompactHello: true}
	s := NewService(&cfg, NewFlows())
	t.Cleanup(func() { s.Close() })
	s.lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.2")}, nil
	}
	s.source = func(netip.AddrPort) (netip.Addr, error) { return netip.MustParseAddr("203.0.113.1"), nil }
	warms := &atomic.Int32{}
	s.newManager = func(c Config) (*Manager, error) {
		if c.WarmTimeout != 15*time.Second || c.PassLifetime != 0 || !c.CompactHello {
			t.Errorf("bad integrated limits %+v", c)
		}
		m, err := New(c)
		if err != nil {
			return nil, err
		}
		m.dial = func(ctx context.Context, p uint16, mark uint32) (net.Conn, error) {
			registered := s.flows.Lookup(netip.AddrPortFrom(c.Source, p), c.Target) != nil
			if registered != (mark == 0) {
				t.Error("warm flow registration leaked into pass or missed SYN")
			}
			return fakeSocket(t, c, p), nil
		}
		m.warm = func(context.Context, net.Conn) error { warms.Add(1); return nil }
		return m, nil
	}
	return s, &cfg, warms
}

func TestServiceDomainSelectionRotationAndCleanup(t *testing.T) {
	s, _, warms := serviceFixture(t)
	for _, dest := range []string{"notgooglevideo.com:443", "googlevideo.com.evil.example:443", "203.0.113.2:443"} {
		if c, _, handled, _, err := s.DialContext(context.Background(), dest, ""); c != nil || handled || err != nil {
			t.Fatalf("claimed %s", dest)
		}
	}
	for i := 0; i < 3; i++ {
		c, id, handled, _, err := s.DialContext(context.Background(), "rr3.googlevideo.com:443", "")
		if err != nil || !handled || id != "warm" {
			t.Fatalf("dial: %s %v %v", id, handled, err)
		}
		if got := c.LocalAddr().(*net.TCPAddr).Port; got != 40000+i {
			t.Fatalf("port=%d", got)
		}
		if s.Status().Active != 1 || s.flows.Active() != 0 {
			t.Fatal("bad active/registry lifecycle")
		}
		c.Close()
		c.Close()
	}
	if warms.Load() != 3 || s.Status().Active != 0 || s.Status().Pending != 0 || s.Status().CoolingPorts != 3 {
		t.Fatalf("bad cleanup: %+v", s.Status())
	}
	if _, _, _, _, err := s.DialContext(context.Background(), "rr3.googlevideo.com:443", ""); !errors.Is(err, ErrCapacity) {
		t.Fatal("reused a quarantined port", err)
	}
}

func TestServiceFailureAndConfigChangeDoNotFallback(t *testing.T) {
	s, cfg, _ := serviceFixture(t)
	entered := make(chan struct{})
	makeManager := s.newManager
	s.newManager = func(c Config) (*Manager, error) {
		m, e := makeManager(c)
		if e == nil {
			m.warm = func(ctx context.Context, _ net.Conn) error { close(entered); <-ctx.Done(); return ctx.Err() }
		}
		return m, e
	}
	done := make(chan error, 1)
	go func() {
		c, _, handled, _, err := s.DialContext(context.Background(), "rr.googlevideo.com:443", "")
		if c != nil || !handled {
			t.Error("fell through")
		}
		done <- err
	}()
	<-entered
	next := *cfg
	s.UpdateConfig(&next)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("config change did not cancel warm")
		}
	case <-time.After(time.Second):
		t.Fatal("pending warm did not stop")
	}
	if s.flows.Active() != 0 || s.Status().Pending != 0 || s.Status().Active != 0 {
		t.Fatalf("leaked %+v", s.Status())
	}
}

func TestServiceFirstSetAndSourceSelection(t *testing.T) {
	s, cfg, warms := serviceFixture(t)
	first := config.NewSetConfig()
	first.Id = "first"
	first.Targets.DomainsToMatch = []string{"googlevideo.com"}
	next := *cfg
	next.Sets = append([]*config.SetConfig{&first}, cfg.Sets...)
	s.UpdateConfig(&next)
	if _, _, handled, _, _ := s.DialContext(context.Background(), "rr.googlevideo.com:443", ""); handled {
		t.Fatal("ignored earlier ordinary set")
	}
	if warms.Load() != 0 {
		t.Fatal("warmed outside selected set")
	}
}

func TestServiceConcurrentPortsAndShutdown(t *testing.T) {
	s, _, _ := serviceFixture(t)
	var wg sync.WaitGroup
	conns := make(chan net.Conn, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _, handled, _, err := s.DialContext(context.Background(), "rr.googlevideo.com:443", "")
			if err != nil || !handled {
				t.Errorf("dial %v %v", handled, err)
				return
			}
			conns <- c
		}()
	}
	wg.Wait()
	close(conns)
	ports := map[int]bool{}
	for c := range conns {
		port := c.LocalAddr().(*net.TCPAddr).Port
		if ports[port] {
			t.Fatal("duplicate live port")
		}
		ports[port] = true
	}
	if len(ports) != 3 {
		t.Fatal("missing connection")
	}
	s.Close()
	if st := s.Status(); st.Active != 0 || st.Pending != 0 || st.CaptureFlows != 0 {
		t.Fatalf("shutdown leaked %+v", st)
	}
	if _, _, _, _, err := s.DialContext(context.Background(), "rr.googlevideo.com:443", ""); !errors.Is(err, ErrClosed) {
		t.Fatal("closed service accepted a connection", err)
	}
}
