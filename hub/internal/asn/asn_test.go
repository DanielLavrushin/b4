package asn

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLookupCachesPerPrefix(t *testing.T) {
	calls := 0
	lookup := func(_ context.Context, name string) ([]string, error) {
		calls++
		switch name {
		case "1.113.0.203.origin.asn.cymru.com", "2.113.0.203.origin.asn.cymru.com":
			return []string{"64500 | 203.0.113.0/24 | ru | ripencc | 2020-01-01"}, nil
		case "AS64500.asn.cymru.com":
			return []string{"64500 | RU | ripencc | 2020-01-01 | EXAMPLE-AS Example ISP, RU"}, nil
		}
		return nil, context.DeadlineExceeded
	}
	clock := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	r := New(lookup, nil, func() time.Time { return clock })
	info := r.Lookup(context.Background(), net.ParseIP("203.0.113.1"))
	if info.ASN != "64500" || info.Country != "RU" || info.Name != "EXAMPLE-AS Example ISP, RU" {
		t.Fatalf("unexpected info %+v", info)
	}
	if calls != 2 {
		t.Fatalf("expected origin and description lookups, got %d calls", calls)
	}
	r.Lookup(context.Background(), net.ParseIP("203.0.113.2"))
	if calls != 2 {
		t.Errorf("a second address in the same /24 must hit the cache")
	}
	clock = clock.Add(25 * time.Hour)
	r.Lookup(context.Background(), net.ParseIP("203.0.113.2"))
	if calls != 4 {
		t.Errorf("the cache must expire after a day, got %d calls", calls)
	}
	if info := r.Lookup(context.Background(), net.ParseIP("10.0.0.1")); info.ASN != "" {
		t.Errorf("private addresses must not be looked up")
	}
}

func TestCymruNames(t *testing.T) {
	if n := CymruName(net.ParseIP("8.8.8.8")); n != "8.8.8.8.origin.asn.cymru.com" {
		t.Errorf("v4 name %s", n)
	}
	if n := CymruName(net.ParseIP("2001:db8::1")); n != "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.origin6.asn.cymru.com" {
		t.Errorf("v6 name %s", n)
	}
}

func TestClientIPHonoursForwardedOnlyFromLoopback(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:4000"
	req.Header.Set("X-Forwarded-For", "198.51.100.9, 203.0.113.5")
	if ip := ClientIP(req); ip.String() != "203.0.113.5" {
		t.Errorf("loopback peer must yield the last forwarded address, got %s", ip)
	}
	req.RemoteAddr = "192.0.2.10:4000"
	if ip := ClientIP(req); ip.String() != "192.0.2.10" {
		t.Errorf("a non-loopback peer must ignore the header, got %s", ip)
	}
	req.RemoteAddr = "[::1]:4000"
	req.Header.Del("X-Forwarded-For")
	req.Header.Set("X-Real-IP", "203.0.113.7")
	if ip := ClientIP(req); ip.String() != "203.0.113.7" {
		t.Errorf("X-Real-IP fallback failed, got %s", ip)
	}
}
