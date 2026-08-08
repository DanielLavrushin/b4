package nfq

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func newTestIPHealth(reachable func(ip string) bool) *ipHealthStore {
	s := newIPHealthStore()
	s.dial = func(ip string, _ uint16, _ int) bool { return reachable(ip) }
	return s
}

func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

func TestIPHealthNeedsThresholdBeforeProbing(t *testing.T) {
	probed := false
	var mu sync.Mutex
	s := newTestIPHealth(func(string) bool {
		mu.Lock()
		probed = true
		mu.Unlock()
		return false
	})
	defer s.Stop()

	s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)

	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if probed {
		t.Errorf("probed after 2 SYNs, threshold is 3")
	}
	if s.IsDead("1.2.3.4") {
		t.Errorf("destination marked dead before the probe ran")
	}
}

func TestIPHealthMarksDeadWhenProbeFails(t *testing.T) {
	s := newTestIPHealth(func(string) bool { return false })
	defer s.Stop()

	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}

	if !waitFor(t, func() bool { return s.IsDead("1.2.3.4") }) {
		t.Fatal("destination never became dead after an unanswered probe")
	}
	if got := s.DeadIPs(); len(got) != 1 || got[0] != "1.2.3.4" {
		t.Errorf("DeadIPs = %v, want [1.2.3.4]", got)
	}
}

func TestIPHealthProbeRescuesFalsePositive(t *testing.T) {
	s := newTestIPHealth(func(string) bool { return true })
	defer s.Stop()

	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}

	if !waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		e := s.entries["1.2.3.4"]
		return e != nil && !e.probing
	}) {
		t.Fatal("probe never completed")
	}
	if s.IsDead("1.2.3.4") {
		t.Errorf("destination marked dead even though the router reached it")
	}
}

func TestIPHealthRecordAliveClearsDead(t *testing.T) {
	s := newTestIPHealth(func(string) bool { return false })
	defer s.Stop()

	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}
	if !waitFor(t, func() bool { return s.IsDead("1.2.3.4") }) {
		t.Fatal("destination never became dead")
	}

	s.RecordAlive("1.2.3.4")
	if s.IsDead("1.2.3.4") {
		t.Errorf("a reply from the destination did not clear the dead state")
	}
}

func TestIPHealthDeadStateExpiresAndReprobes(t *testing.T) {
	var mu sync.Mutex
	reachable := false
	s := newTestIPHealth(func(string) bool {
		mu.Lock()
		defer mu.Unlock()
		return reachable
	})
	defer s.Stop()

	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}
	if !waitFor(t, func() bool { return s.IsDead("1.2.3.4") }) {
		t.Fatal("destination never became dead")
	}

	mu.Lock()
	reachable = true
	mu.Unlock()

	s.Cleanup(time.Nanosecond)

	if !waitFor(t, func() bool { return !s.IsDead("1.2.3.4") }) {
		t.Fatal("dead state never expired, so an unbanned address would stay blocked forever")
	}
}

func TestIPHealthDeadStateSurvivesWhenStillUnreachable(t *testing.T) {
	s := newTestIPHealth(func(string) bool { return false })
	defer s.Stop()

	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}
	if !waitFor(t, func() bool { return s.IsDead("1.2.3.4") }) {
		t.Fatal("destination never became dead")
	}

	s.Cleanup(time.Nanosecond)

	if !waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		e := s.entries["1.2.3.4"]
		return e != nil && !e.probing && e.state == ipHealthDead
	}) {
		t.Fatal("re-probe did not restore the dead state for a still unreachable address")
	}
}

func TestIPHealthBurstSuspendsDetection(t *testing.T) {
	s := newTestIPHealth(func(string) bool { return false })
	defer s.Stop()

	ips := make([]string, 0, ipHealthDeadBurst)
	for i := 0; i < ipHealthDeadBurst; i++ {
		ip := "10.0.0." + strconv.Itoa(i+1)
		ips = append(ips, ip)
		for j := 0; j < 3; j++ {
			s.RecordSyn(ip, 443, 0, 3, time.Hour)
		}
	}

	if !waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.suspendedLocked(time.Now())
	}) {
		t.Fatal("a burst of unreachable destinations did not suspend detection")
	}

	for _, ip := range ips {
		if s.IsDead(ip) {
			t.Fatalf("%s still reported dead while detection is suspended", ip)
		}
	}
	if got := s.DeadIPs(); len(got) != 0 {
		t.Errorf("DeadIPs = %v, want none while suspended", got)
	}
}

func TestIPHealthStopIsIdempotent(t *testing.T) {
	s := newTestIPHealth(func(string) bool { return false })
	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}
	s.Stop()
	s.Stop()

	s.RecordSyn("5.6.7.8", 443, 0, 1, time.Hour)
	if s.IsDead("5.6.7.8") {
		t.Errorf("store kept condemning destinations after Stop")
	}
}
