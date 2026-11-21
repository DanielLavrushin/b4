package warmtuple

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestAServiceHoldingNothingTakesANewConfigWithoutComparingIt(t *testing.T) {
	s := NewService(NewFlows())
	t.Cleanup(func() { s.Close() })
	cfg := config.NewConfig()
	s.UpdateConfig(&cfg)
	epoch := s.epoch
	same := cfg
	s.UpdateConfig(&same)
	if s.epoch != epoch+1 || s.cfg != &same {
		t.Fatalf("with no lease, pending warm or failure to keep, a save compared the whole config before storing it: epoch %d -> %d, stored=%v", epoch, s.epoch, s.cfg == &same)
	}
}

func TestAServiceHoldingALeaseStillKeepsItAcrossACollectedOnlyUpdate(t *testing.T) {
	s, cfg, set, _ := serviceFixture(t)
	first := serviceDial(t, s, set)
	lease := first.(*serviceConn).lease
	first.Close()
	epoch := s.epoch
	same := *cfg
	s.UpdateConfig(&same)
	if s.epoch != epoch || s.leases[lease.port] != lease {
		t.Fatalf("an update that changes nothing but collected IPs dropped the idle lease: epoch %d -> %d, kept=%v", epoch, s.epoch, s.leases[lease.port] == lease)
	}
}

func TestAPayloadReloadThatChangesTheFakeBytesForgetsFailuresAndCancelsAPendingWarm(t *testing.T) {
	s, cfg, set, _ := serviceFixture(t)
	s.mu.Lock()
	s.rememberFailureLocked(failureKey{target: netip.MustParseAddr("203.0.113.2"), host: "other.example.org"}, time.Now())
	s.mu.Unlock()
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
		_, err := s.Dial(context.Background(), set, "192.0.2.10", "warm.example.org", netip.MustParseAddrPort("203.0.113.2:443"))
		done <- err
	}()
	<-entered
	s.UpdateConfig(savedConfig(cfg, func(set *config.SetConfig) { set.Faking.PayloadData = []byte("reloaded payload") }))
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a payload reload that changed the set's fake bytes did not cancel the pending warm")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the pending warm was not cancelled by a payload reload that changed the set's fake bytes")
	}
	if n := s.failureCount(); n != 0 {
		t.Fatalf("a payload reload that changed the set's fake bytes kept %d remembered failures", n)
	}
}

func TestAServiceHoldingAFailureStillKeepsItAcrossACollectedOnlyUpdate(t *testing.T) {
	s, cfg, _, _ := serviceFixture(t)
	key := failureKey{target: netip.MustParseAddr("203.0.113.2"), host: "warm.example.org"}
	s.mu.Lock()
	s.rememberFailureLocked(key, time.Now())
	s.mu.Unlock()
	epoch := s.epoch
	same := *cfg
	s.UpdateConfig(&same)
	if _, kept := s.failures[key]; s.epoch != epoch || !kept {
		t.Fatalf("an update that changes nothing but collected IPs forgot a failed warm: epoch %d -> %d, kept=%v", epoch, s.epoch, kept)
	}
}
