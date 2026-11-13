package tproxy

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/dns"
)

func TestAStrictSetStillAsksItsResolverWhileItCoolsDown(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.UseDomain = false
	used := withSetDNS(t, l, true, func(string) ([]net.IP, error) {
		return []net.IP{net.IPv4(203, 0, 113, 61)}, nil
	})
	for i := 0; i < dns.SourceFailuresToTrip; i++ {
		dns.NoteSourceFailure("https://dns.example/dns-query")
	}

	conn, err := l.DialNamed("blocked.example", 443)
	if err != nil {
		t.Fatalf("the dial failed after %d lookups: %v; a strict set has nothing else to ask, so its cooling resolver is asked every time", len(*used), err)
	}
	conn.Close()
	if r := up.next(t); r.host != "203.0.113.61" {
		t.Fatalf("upstream got %q, want the resolver's answer 203.0.113.61", r.host)
	}
}

func TestDialNamedMovesOnToTheNextResolverOfTheList(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	l.UseDomain = false
	used := withSetDNS(t, l, true, func(string) ([]net.IP, error) { return nil, nil })
	set := *l.set.Load()
	set.DNS.Upstreams = []string{"192.0.2.53"}
	l.set.Store(&set)
	setLookup = func(_ context.Context, srv dns.Server, _ string, _, _ bool) ([]net.IP, error) {
		*used = append(*used, srv)
		if srv.DoHURL != "" {
			return nil, errors.New("resolver down")
		}
		return []net.IP{net.IPv4(203, 0, 113, 63)}, nil
	}

	conn, err := l.DialNamed("blocked.example", 443)
	if err != nil {
		t.Fatalf("DialNamed: %v", err)
	}
	conn.Close()
	if r := up.next(t); r.host != "203.0.113.63" {
		t.Fatalf("upstream got %q, want the answer of the list's resolver", r.host)
	}
	if len(*used) != 2 || (*used)[0].DoHURL == "" || !(*used)[1].UDP.Equal(net.IPv4(192, 0, 2, 53)) {
		t.Fatalf("asked %v, want the primary and then the list entry", *used)
	}
	for _, srv := range *used {
		if srv.Mark != 0x8000 || srv.Timeout == 0 {
			t.Fatalf("resolver %s was asked with mark %#x and timeout %s, want the listener's DNS options on every entry", srv, srv.Mark, srv.Timeout)
		}
	}
}
