package warmtuple

import (
	"net/netip"
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
