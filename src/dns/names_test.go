package dns

import (
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"
)

func testNameCache() (*NameCache, *time.Time) {
	c := NewNameCache()
	c.SetWanted(true)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	return c, &now
}

func ips(s ...string) []net.IP {
	out := make([]net.IP, 0, len(s))
	for _, v := range s {
		out = append(out, net.ParseIP(v))
	}
	return out
}

func TestNameCacheIgnoresEverythingUntilWanted(t *testing.T) {
	c := NewNameCache()
	c.Observe(net.ParseIP("192.168.1.10"), "example.com", ips("93.184.216.34"))
	c.SetWanted(true)
	if got := c.Lookup(nil, net.ParseIP("93.184.216.34")); !got.Empty() {
		t.Fatalf("an answer seen while not wanted was kept: %+v", got)
	}
	var nilCache *NameCache
	nilCache.Observe(nil, "example.com", ips("1.1.1.1"))
	if !nilCache.Lookup(nil, net.ParseIP("1.1.1.1")).Empty() || nilCache.Wanted() {
		t.Fatal("a nil cache must be inert")
	}
}

func TestNameCacheSeparatesOwnLocalAndOtherNames(t *testing.T) {
	c, now := testNameCache()
	cdn := "104.26.12.205"
	a, b := net.ParseIP("192.168.1.10"), net.ParseIP("192.168.1.20")
	c.Observe(a, "api.ipify.org", ips(cdn))
	*now = now.Add(time.Second)
	c.Observe(b, "Other.Example.COM.", ips(cdn, "104.26.13.205"))
	*now = now.Add(time.Second)
	c.Observe(nil, "set.example.org", ips(cdn))

	got := c.Lookup(a, net.ParseIP(cdn))
	want := NameMatches{Own: []string{"api.ipify.org"}, Local: []string{"set.example.org"}, Others: []string{"other.example.com"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("for client a: got %+v, want %+v", got, want)
	}
	got = c.Lookup(net.ParseIP("192.168.1.99"), net.ParseIP(cdn))
	if len(got.Own) != 0 || !got.Has("api.ipify.org") || !got.Has("other.example.com") {
		t.Fatalf("an unknown client must see every name as someone else's: %+v", got)
	}
	if got := c.Lookup(nil, net.ParseIP(cdn)); len(got.Own) != 0 {
		t.Fatalf("a lookup without a client owns nothing: %+v", got)
	}
}

func TestNameCacheKeepsOneRecordPerNameAcrossClients(t *testing.T) {
	c, now := testNameCache()
	addr := net.ParseIP("203.0.113.1")
	c.Observe(net.ParseIP("192.168.1.2"), "keep.example.com", []net.IP{addr})
	for i := 0; i < 6; i++ {
		*now = now.Add(time.Second)
		c.Observe(net.ParseIP(fmt.Sprintf("192.168.1.%d", 10+i)), "busy.example.com", []net.IP{addr})
	}
	got := c.Lookup(net.ParseIP("192.168.1.2"), addr)
	if !reflect.DeepEqual(got.Own, []string{"keep.example.com"}) {
		t.Fatalf("many clients asking for one name pushed out another client's name: %+v", got)
	}
	if got := c.Lookup(net.ParseIP("192.168.1.15"), addr); !reflect.DeepEqual(got.Own, []string{"busy.example.com"}) {
		t.Fatalf("the latest client of a shared name lost it: %+v", got)
	}
}

func TestNameCacheExpiresAndCaps(t *testing.T) {
	c, now := testNameCache()
	addr := net.ParseIP("203.0.113.1")
	for i := 0; i < namesPerAddress+2; i++ {
		c.Observe(nil, fmt.Sprintf("n%d.example.com", i), []net.IP{addr})
		*now = now.Add(time.Second)
	}
	got := c.Lookup(nil, addr).Local
	if len(got) != namesPerAddress || got[0] != fmt.Sprintf("n%d.example.com", namesPerAddress+1) {
		t.Fatalf("expected the %d newest names, newest first, got %v", namesPerAddress, got)
	}
	*now = now.Add(nameCacheTTL + time.Second)
	if got := c.Lookup(nil, addr); !got.Empty() {
		t.Fatalf("names outlived the TTL: %+v", got)
	}
}

func TestNameCacheEvictsTheLeastRecentAddress(t *testing.T) {
	c, _ := testNameCache()
	c.limit = 2
	c.Observe(nil, "a.example.com", ips("10.0.0.1"))
	c.Observe(nil, "b.example.com", ips("10.0.0.2"))
	c.Observe(nil, "a.example.com", ips("10.0.0.1"))
	c.Observe(nil, "c.example.com", ips("10.0.0.3"))
	if !c.Lookup(nil, net.ParseIP("10.0.0.2")).Empty() {
		t.Fatal("the least recently seen address survived")
	}
	if c.Lookup(nil, net.ParseIP("10.0.0.1")).Empty() || c.Lookup(nil, net.ParseIP("10.0.0.3")).Empty() {
		t.Fatal("a recent address was evicted")
	}
}

func TestNameCacheDropsEntriesWhenNoLongerWanted(t *testing.T) {
	c, _ := testNameCache()
	c.Observe(nil, "a.example.com", ips("10.0.0.1"))
	c.SetWanted(false)
	c.SetWanted(true)
	if !c.Lookup(nil, net.ParseIP("10.0.0.1")).Empty() {
		t.Fatal("entries survived switching the cache off")
	}
}

func TestCleanHostName(t *testing.T) {
	cases := map[string]string{
		"Example.COM.":          "example.com",
		" api.ipify.org":        "api.ipify.org",
		"_dmarc.x.org":          "_dmarc.x.org",
		"localhost":             "",
		"1.2.3.4":               "",
		"2001:db8::1":           "",
		"bad host.com":          "",
		"":                      "",
		"xn--e1afmkfd.xn--p1ai": "xn--e1afmkfd.xn--p1ai",
	}
	for in, want := range cases {
		if got := CleanHostName(in); got != want {
			t.Errorf("CleanHostName(%q) = %q, want %q", in, got, want)
		}
	}
}
