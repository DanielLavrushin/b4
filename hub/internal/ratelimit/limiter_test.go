package ratelimit

import (
	"net"
	"testing"
	"time"
)

func TestFixedWindowLimit(t *testing.T) {
	clock := time.Date(2026, 9, 12, 23, 30, 0, 0, time.UTC)
	l := New(func() time.Time { return clock })
	for i := 0; i < SharesPerDay; i++ {
		if ok, _ := l.Allow(ScopeShare, "k", SharesPerDay, Day); !ok {
			t.Fatalf("share %d must be allowed", i+1)
		}
	}
	ok, retry := l.Allow(ScopeShare, "k", SharesPerDay, Day)
	if ok {
		t.Fatalf("sixth share must be refused")
	}
	if retry != 30*time.Minute {
		t.Errorf("retry_after must reach the next UTC day, got %v", retry)
	}
	if ok, _ := l.Allow(ScopeVote, "k", VotesPerDay, Day); !ok {
		t.Errorf("scopes must not share a budget")
	}
	if ok, _ := l.Allow(ScopeShare, "other", SharesPerDay, Day); !ok {
		t.Errorf("keys must not share a budget")
	}
	clock = clock.Add(31 * time.Minute)
	if ok, _ := l.Allow(ScopeShare, "k", SharesPerDay, Day); !ok {
		t.Errorf("the budget must reset with the next window")
	}
}

func TestPrefixAndAddressKey(t *testing.T) {
	if p := Prefix(net.ParseIP("203.0.113.77")); p != "203.0.113.0/24" {
		t.Errorf("v4 prefix: %s", p)
	}
	if p := Prefix(net.ParseIP("2001:db8:abcd:1234::1")); p != "2001:db8:abcd::/48" {
		t.Errorf("v6 prefix: %s", p)
	}
	secret := []byte("secret")
	day := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	a := AddressKey(secret, net.ParseIP("203.0.113.1"), day)
	b := AddressKey(secret, net.ParseIP("203.0.113.200"), day)
	if a != b {
		t.Errorf("addresses in one /24 must share a key")
	}
	if AddressKey(secret, net.ParseIP("203.0.113.1"), day.Add(Day)) == a {
		t.Errorf("the salt must rotate daily")
	}
	if AddressKey(secret, net.ParseIP("203.0.114.1"), day) == a {
		t.Errorf("different prefixes must differ")
	}
}

func TestSweepDropsBucketsByTheirOwnWindow(t *testing.T) {
	clock := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	l := New(func() time.Time { return clock })
	l.Allow(ScopeRequest, "net", RequestsPerHour, Hour)
	l.Allow(ScopeShare, "key", SharesPerDay, Day)
	if len(l.buckets) != 2 {
		t.Fatalf("two buckets expected, got %d", len(l.buckets))
	}
	clock = clock.Add(2 * time.Hour)
	l.Allow(ScopeVote, "other", VotesPerDay, Day)
	if _, ok := l.buckets[ScopeRequest+"|net"]; ok {
		t.Errorf("an hourly bucket must be swept once its hour is over")
	}
	if _, ok := l.buckets[ScopeShare+"|key"]; !ok {
		t.Errorf("a daily bucket must survive the sweep within its day")
	}
	clock = clock.Add(Day)
	l.Allow(ScopeVote, "other", VotesPerDay, Day)
	if _, ok := l.buckets[ScopeShare+"|key"]; ok {
		t.Errorf("a daily bucket must be swept once its day is over")
	}
}

func TestRefundOnlyWithinTheSameWindow(t *testing.T) {
	clock := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	l := New(func() time.Time { return clock })
	for i := 0; i < SharesPerDay; i++ {
		l.Allow(ScopeShare, "k", SharesPerDay, Day)
	}
	l.Refund(ScopeShare, "k", Day)
	if ok, _ := l.Allow(ScopeShare, "k", SharesPerDay, Day); !ok {
		t.Fatalf("a refund must free one slot")
	}
	clock = clock.Add(Day)
	l.Refund(ScopeShare, "k", Day)
	if l.buckets[ScopeShare+"|k"].count != SharesPerDay {
		t.Errorf("a refund after the window must not touch the old count, got %d", l.buckets[ScopeShare+"|k"].count)
	}
}
