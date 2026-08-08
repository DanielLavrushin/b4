package iphealth

import (
	"net"
	"testing"
	"time"
)

func TestKnownGoodRemembersNewestFirst(t *testing.T) {
	s := NewKnownGood()

	s.Remember("raw.githubusercontent.com", net.ParseIP("185.199.108.133"))
	time.Sleep(2 * time.Millisecond)
	s.Remember("raw.githubusercontent.com", net.ParseIP("185.199.111.133"))

	got := s.Lookup("raw.githubusercontent.com", false)
	if len(got) != 2 {
		t.Fatalf("Lookup returned %d addresses, want 2", len(got))
	}
	if got[0].String() != "185.199.111.133" {
		t.Errorf("first address = %s, want the most recently seen one", got[0])
	}
}

func TestKnownGoodSeparatesFamilies(t *testing.T) {
	s := NewKnownGood()

	s.Remember("example.com", net.ParseIP("1.1.1.1"))
	s.Remember("example.com", net.ParseIP("2001:db8::1"))

	if got := s.Lookup("example.com", false); len(got) != 1 || got[0].String() != "1.1.1.1" {
		t.Errorf("IPv4 lookup = %v, want [1.1.1.1]", got)
	}
	if got := s.Lookup("example.com", true); len(got) != 1 || got[0].String() != "2001:db8::1" {
		t.Errorf("IPv6 lookup = %v, want [2001:db8::1]", got)
	}
}

func TestKnownGoodIsCaseAndDotInsensitive(t *testing.T) {
	s := NewKnownGood()

	s.Remember("Example.COM.", net.ParseIP("1.1.1.1"))

	if got := s.Lookup("example.com", false); len(got) != 1 {
		t.Errorf("Lookup = %v, want the address stored under a differently cased name", got)
	}
}

func TestKnownGoodCapsPerHost(t *testing.T) {
	s := NewKnownGood()

	for i := 1; i <= maxKnownGoodPerHost+3; i++ {
		s.Remember("example.com", net.IPv4(10, 0, 0, byte(i)))
		time.Sleep(time.Millisecond)
	}

	if got := s.Lookup("example.com", false); len(got) != maxKnownGoodPerHost {
		t.Errorf("Lookup returned %d addresses, want the cap of %d", len(got), maxKnownGoodPerHost)
	}
}

func TestKnownGoodExpires(t *testing.T) {
	s := NewKnownGood()
	s.Remember("example.com", net.ParseIP("1.1.1.1"))

	s.mu.Lock()
	entries := s.hosts["example.com"]
	entries[0].seen = time.Now().Add(-2 * knownGoodTTL)
	s.hosts["example.com"] = entries
	s.mu.Unlock()

	if got := s.Lookup("example.com", false); len(got) != 0 {
		t.Errorf("Lookup = %v, want nothing once the entry aged out", got)
	}
	s.Cleanup()
	if s.Len() != 0 {
		t.Errorf("Len = %d, want the expired host removed", s.Len())
	}
}

func TestKnownGoodIgnoresEmptyInput(t *testing.T) {
	s := NewKnownGood()

	s.Remember("", net.ParseIP("1.1.1.1"))
	s.Remember("example.com", nil)

	if s.Len() != 0 {
		t.Errorf("Len = %d, want nothing stored for empty input", s.Len())
	}
	if got := s.Lookup("", false); got != nil {
		t.Errorf("Lookup = %v, want nil for an empty host", got)
	}
}

func TestKnownGoodPrefersVerifiedOverObserved(t *testing.T) {
	s := NewKnownGood()

	s.Observe("example.com", net.ParseIP("1.1.1.1"))
	time.Sleep(2 * time.Millisecond)
	s.Remember("example.com", net.ParseIP("2.2.2.2"))

	got := s.Lookup("example.com", false)
	if len(got) != 2 {
		t.Fatalf("Lookup returned %d addresses, want 2", len(got))
	}
	if got[0].String() != "2.2.2.2" {
		t.Errorf("first = %s, want the handshake-verified address ahead of one merely seen in DNS", got[0])
	}
}

func TestKnownGoodObserveDoesNotDowngradeVerified(t *testing.T) {
	s := NewKnownGood()

	s.Remember("example.com", net.ParseIP("1.1.1.1"))
	s.Observe("example.com", net.ParseIP("1.1.1.1"))
	s.Observe("example.com", net.ParseIP("2.2.2.2"))

	got := s.Lookup("example.com", false)
	if len(got) != 2 || got[0].String() != "1.1.1.1" {
		t.Errorf("Lookup = %v, want the verified address still ranked first", got)
	}
}

func TestKnownGoodObserveAloneIsUsable(t *testing.T) {
	s := NewKnownGood()

	s.Observe("www.instagram.com", net.ParseIP("157.240.205.174"))
	s.Observe("www.instagram.com", net.ParseIP("157.240.0.174"))

	got := s.Lookup("www.instagram.com", false)
	if len(got) != 2 {
		t.Fatalf("Lookup returned %d addresses, want both seen in DNS answers even with no handshake", len(got))
	}
}
