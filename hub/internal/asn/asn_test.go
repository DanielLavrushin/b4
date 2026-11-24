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
		case "0.113.0.203.origin.asn.cymru.com":
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

func TestLookupNeverSendsAFullAddress(t *testing.T) {
	var asked []string
	lookup := func(_ context.Context, name string) ([]string, error) {
		asked = append(asked, name)
		return []string{"64500 | 203.0.113.0/24 | ru | ripencc | 2020-01-01"}, nil
	}
	r := New(lookup, nil, nil)
	for _, c := range []struct{ ip, want string }{
		{"203.0.113.77", "0.113.0.203.origin.asn.cymru.com"},
		{"::ffff:198.51.100.77", "0.100.51.198.origin.asn.cymru.com"},
		{"2001:db8:aaaa:bbbb:cccc:dddd:eeee:ffff", "0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.a.a.a.a.8.b.d.0.1.0.0.2.origin6.asn.cymru.com"},
	} {
		asked = nil
		if info := r.Lookup(context.Background(), net.ParseIP(c.ip)); info.ASN != "64500" {
			t.Fatalf("%s: unexpected info %+v", c.ip, info)
		}
		if len(asked) == 0 || asked[0] != c.want {
			t.Errorf("%s: the lookup must carry only the network, asked %v", c.ip, asked)
		}
	}
}

func TestLookupSkipsTunnelAddresses(t *testing.T) {
	for _, ip := range []string{"2002:cb00:714d::1", "2001:0:4136:e378:8000:63bf:3fff:fdd2"} {
		var asked []string
		r := New(func(_ context.Context, name string) ([]string, error) {
			asked = append(asked, name)
			return []string{"6939 | 2002::/16 |  | other |"}, nil
		}, nil, nil)
		if info := r.Lookup(context.Background(), net.ParseIP(ip)); info != (Info{}) || len(asked) != 0 {
			t.Errorf("%s is the address of a tunnel, not of a provider: asked %v, got %+v", ip, asked, info)
		}
	}
	for _, ip := range []string{"2001:db8::1", "2003:1::1", "::ffff:203.0.113.77", "203.0.113.77"} {
		if !Routable(net.ParseIP(ip)) {
			t.Errorf("%s must still be looked up", ip)
		}
	}
}

func TestLookupTakesTheMostSpecificOriginInAnyOrder(t *testing.T) {
	wide := "64501 | 203.0.0.0/16 | de | ripencc | 2005-01-24"
	narrow := "64502 | 203.0.112.0/23 | ru | ripencc | 2005-01-24"
	twin := "64503 | 203.0.112.0/23 | kz | ripencc | 2005-01-24"
	broken := "64504 | not a prefix | us | arin | 2005-01-24"
	for name, c := range map[string]struct {
		answers []string
		asn     string
		country string
	}{
		"wide first":                 {[]string{wide, narrow}, "64502", "RU"},
		"narrow first":               {[]string{narrow, wide}, "64502", "RU"},
		"equal prefixes":             {[]string{twin, narrow, wide}, "64502", "RU"},
		"equal prefixes reversed":    {[]string{wide, narrow, twin}, "64502", "RU"},
		"several origins in one":     {[]string{"64510 64509 | 203.0.112.0/23 | ru | ripencc |", wide}, "64509", "RU"},
		"the same the other way":     {[]string{"64509 64510 | 203.0.112.0/23 | ru | ripencc |", wide}, "64509", "RU"},
		"an unreadable prefix":       {[]string{broken, wide}, "64501", "DE"},
		"only an unreadable prefix":  {[]string{broken}, "64504", "US"},
		"a record without an origin": {[]string{" | 203.0.113.0/24 | us | arin |", wide}, "64501", "DE"},
	} {
		t.Run(name, func(t *testing.T) {
			r := New(func(_ context.Context, name string) ([]string, error) {
				if name == "0.113.0.203.origin.asn.cymru.com" {
					return c.answers, nil
				}
				return nil, context.DeadlineExceeded
			}, nil, nil)
			if info := r.Lookup(context.Background(), net.ParseIP("203.0.113.1")); info.ASN != c.asn || info.Country != c.country {
				t.Errorf("want AS%s %s, got %+v", c.asn, c.country, info)
			}
		})
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

func TestClientIPHonoursForwardedFromTrustedProxies(t *testing.T) {
	if err := SetTrustedProxies("10.84.0.0/24, 2001:db8::5"); err != nil {
		t.Fatal(err)
	}
	defer SetTrustedProxies("")
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.84.0.3:4000"
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	if ip := ClientIP(req); ip.String() != "203.0.113.5" {
		t.Errorf("a peer inside a trusted range must yield the forwarded address, got %s", ip)
	}
	req.RemoteAddr = "[2001:db8::5]:4000"
	if ip := ClientIP(req); ip.String() != "203.0.113.5" {
		t.Errorf("a trusted single address must yield the forwarded address, got %s", ip)
	}
	req.RemoteAddr = "10.84.1.3:4000"
	if ip := ClientIP(req); ip.String() != "10.84.1.3" {
		t.Errorf("a peer outside the trusted ranges must ignore the header, got %s", ip)
	}
	for _, bad := range []string{"nope", "10.84.0.0/99", "10.84.0.0/24,x"} {
		if err := SetTrustedProxies(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}
