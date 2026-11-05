package dns

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestSetServersKeepsEveryConfiguredResolver(t *testing.T) {
	servers, ok := SetServers(true, "1.1.1.1", "https://dns.google/dns-query", []string{"9.9.9.9", "https://doh.pub/dns-query", "1.1.1.1"})
	if !ok {
		t.Fatal("SetServers reported no resolver for a configured set")
	}
	want := []string{"https://dns.google/dns-query", "1.1.1.1", "9.9.9.9", "https://doh.pub/dns-query"}
	if len(servers) != len(want) {
		t.Fatalf("servers = %v, want %v", servers, want)
	}
	for i, raw := range want {
		if servers[i].String() != raw {
			t.Fatalf("servers[%d] = %q, want %q", i, servers[i].String(), raw)
		}
	}
}

func TestSetServersNeedsAnEnabledSet(t *testing.T) {
	if servers, ok := SetServers(false, "1.1.1.1", "", []string{"9.9.9.9"}); ok || len(servers) != 0 {
		t.Fatalf("SetServers(false) = %v, %t, want no resolvers", servers, ok)
	}
}

func TestSetServersDropsJunk(t *testing.T) {
	servers, ok := SetServers(true, "not-an-address", "", []string{"", "  ", "https://"})
	if ok || len(servers) != 0 {
		t.Fatalf("SetServers accepted junk: %v", servers)
	}
}

func TestLookupAnyTakesTheAnswerThatArrivesFirst(t *testing.T) {
	ResetSourceHealth()
	good := net.ParseIP("203.0.113.7")
	lookup := func(ctx context.Context, srv Server, name string, v4, v6 bool) ([]net.IP, error) {
		switch srv.String() {
		case "198.51.100.1":
			time.Sleep(40 * time.Millisecond)
			return nil, errors.New("blocked")
		case "198.51.100.2":
			return []net.IP{good}, nil
		}
		return nil, errors.New("unexpected resolver")
	}
	servers := []Server{
		{Source: "198.51.100.1", UDP: net.ParseIP("198.51.100.1")},
		{Source: "198.51.100.2", UDP: net.ParseIP("198.51.100.2")},
	}
	ips, err := LookupAny(context.Background(), lookup, servers, true, true, "example.com", true, false)
	if err != nil {
		t.Fatalf("LookupAny: %v", err)
	}
	if len(ips) != 1 || !ips[0].Equal(good) {
		t.Fatalf("ips = %v, want [%s]", ips, good)
	}
}

func TestLookupAnySkipsACoolingResolver(t *testing.T) {
	ResetSourceHealth()
	t.Cleanup(ResetSourceHealth)
	for i := 0; i < SourceFailuresToTrip; i++ {
		NoteSourceFailure("198.51.100.1")
	}
	var asked []string
	lookup := func(ctx context.Context, srv Server, name string, v4, v6 bool) ([]net.IP, error) {
		asked = append(asked, srv.String())
		return []net.IP{net.ParseIP("203.0.113.9")}, nil
	}
	servers := []Server{
		{Source: "198.51.100.1", UDP: net.ParseIP("198.51.100.1")},
		{Source: "198.51.100.2", UDP: net.ParseIP("198.51.100.2")},
	}
	if _, err := LookupAny(context.Background(), lookup, servers, true, false, "example.com", true, false); err != nil {
		t.Fatalf("LookupAny: %v", err)
	}
	if len(asked) != 1 || asked[0] != "198.51.100.2" {
		t.Fatalf("asked %v, want only the healthy resolver", asked)
	}
}

func TestLookupAnyAsksEveryResolverWhenAllAreCooling(t *testing.T) {
	ResetSourceHealth()
	t.Cleanup(ResetSourceHealth)
	for i := 0; i < SourceFailuresToTrip; i++ {
		NoteSourceFailure("198.51.100.1")
		NoteSourceFailure("198.51.100.2")
	}
	called := 0
	lookup := func(ctx context.Context, srv Server, name string, v4, v6 bool) ([]net.IP, error) {
		called++
		return nil, errors.New("blocked")
	}
	servers := []Server{
		{Source: "198.51.100.1", UDP: net.ParseIP("198.51.100.1")},
		{Source: "198.51.100.2", UDP: net.ParseIP("198.51.100.2")},
	}
	if _, err := LookupAny(context.Background(), lookup, servers, true, true, "example.com", true, false); err == nil {
		t.Fatal("LookupAny succeeded with every resolver failing")
	}
	if called != 2 {
		t.Fatalf("asked %d resolvers, want both", called)
	}
}

func TestLookupAnyStopsOnADefinitiveNoAddress(t *testing.T) {
	ResetSourceHealth()
	t.Cleanup(ResetSourceHealth)
	lookup := func(ctx context.Context, srv Server, name string, v4, v6 bool) ([]net.IP, error) {
		return nil, ErrNoAddress
	}
	servers := []Server{{Source: "198.51.100.1", UDP: net.ParseIP("198.51.100.1")}}
	ips, err := LookupAny(context.Background(), lookup, servers, true, false, "nope.example", true, false)
	if !errors.Is(err, ErrNoAddress) || ips != nil {
		t.Fatalf("LookupAny = %v, %v, want ErrNoAddress", ips, err)
	}
}

func TestLookupAnyFallsBackToTheRouterResolver(t *testing.T) {
	ResetSourceHealth()
	t.Cleanup(ResetSourceHealth)
	lookup := func(ctx context.Context, srv Server, name string, v4, v6 bool) ([]net.IP, error) {
		return nil, errors.New("blocked")
	}
	servers := []Server{{Source: "198.51.100.1", UDP: net.ParseIP("198.51.100.1")}}
	ips, err := LookupAny(context.Background(), lookup, servers, true, false, "localhost", true, false)
	if err != nil {
		t.Fatalf("LookupAny: %v", err)
	}
	if len(ips) == 0 || !ips[0].IsLoopback() {
		t.Fatalf("ips = %v, want the router's answer for localhost", ips)
	}
}
