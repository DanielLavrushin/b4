package detector

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

type fakeQuerier struct {
	answer func(domain string) []byte
}

func (f *fakeQuerier) query(_ context.Context, domain string, _ uint16) ([]byte, error) {
	return f.answer(domain), nil
}

func (f *fakeQuerier) close() {}

func TestANegativePrimaryIsNotHiddenByAPositiveBackup(t *testing.T) {
	previous := newDNSQuerier
	t.Cleanup(func() { newDNSQuerier = previous })

	positive := dns.BuildAnswerFromIPs(dns.BuildAQuery("x.test", 1), 60, []net.IP{net.IPv4(203, 0, 113, 40)})
	newDNSQuerier = func(s *Suite, srv dns.Server) dnsQuerier {
		if srv.Source == "192.0.2.1" {
			return &fakeQuerier{answer: func(domain string) []byte {
				return dns.BuildEmptyAnswer(dns.BuildAQuery(domain, 2))
			}}
		}
		return &fakeQuerier{answer: func(string) []byte { return positive }}
	}

	s := &Suite{setDNS: map[string]config.DNSConfig{
		"set1": {Enabled: true, TargetDNS: "192.0.2.1", Upstreams: []string{"192.0.2.2"}},
	}}
	ips, source := s.resolveThroughB4(context.Background(), SiteResult{SetId: "set1", SetEnabled: true, Domain: "x.test"}, "ipv4")
	if len(ips) != 0 {
		t.Fatalf("got %v from %q; a device gets the primary's empty answer, so the detector must not report the backup's address", ips, source)
	}
}

type countingQuerier struct {
	asked *int32
	body  []byte
	err   error
}

func (q *countingQuerier) query(context.Context, string, uint16) ([]byte, error) {
	atomic.AddInt32(q.asked, 1)
	return q.body, q.err
}

func (q *countingQuerier) close() {}

func detectorWithResolver(t *testing.T, dnsCfg config.DNSConfig, querier func(dns.Server) dnsQuerier) *Suite {
	t.Helper()
	previous := newDNSQuerier
	dns.ResetSourceHealth()
	t.Cleanup(func() {
		newDNSQuerier = previous
		dns.ResetSourceHealth()
	})
	newDNSQuerier = func(_ *Suite, srv dns.Server) dnsQuerier { return querier(srv) }
	return &Suite{setDNS: map[string]config.DNSConfig{"set1": dnsCfg}}
}

func throughB4(s *Suite) ([]string, string) {
	return s.resolveThroughB4(context.Background(), SiteResult{SetId: "set1", SetEnabled: true, Domain: "x.test"}, "ipv4")
}

func TestTheDetectorLeavesResolverHealthAlone(t *testing.T) {
	var asked int32
	s := detectorWithResolver(t, config.DNSConfig{Enabled: true, TargetDNS: "192.0.2.1"}, func(dns.Server) dnsQuerier {
		return &countingQuerier{asked: &asked, err: errors.New("i/o timeout")}
	})
	for i := 0; i < dns.SourceFailuresToTrip+1; i++ {
		throughB4(s)
	}
	if got := atomic.LoadInt32(&asked); got != dns.SourceFailuresToTrip+1 {
		t.Fatalf("the detector asked the resolver %d times in %d runs, want every time", got, dns.SourceFailuresToTrip+1)
	}
	if dns.SourceCooling("192.0.2.1") {
		t.Fatal("detector queries that failed cooled down a resolver that device queries use")
	}
}

func TestTheDetectorAsksAResolverThatDevicesCooledDown(t *testing.T) {
	var asked int32
	positive := dns.BuildAnswerFromIPs(dns.BuildAQuery("x.test", 1), 60, []net.IP{net.IPv4(203, 0, 113, 41)})
	s := detectorWithResolver(t, config.DNSConfig{Enabled: true, TargetDNS: "192.0.2.1"}, func(dns.Server) dnsQuerier {
		return &countingQuerier{asked: &asked, body: positive}
	})
	for i := 0; i < dns.SourceFailuresToTrip; i++ {
		dns.NoteSourceFailure("192.0.2.1")
	}

	ips, _ := throughB4(s)
	if atomic.LoadInt32(&asked) != 1 || len(ips) != 1 || ips[0] != "203.0.113.41" {
		t.Fatalf("asked %d times and got %v; the detector must ask a resolver whatever its health for device traffic", asked, ips)
	}
	if !dns.SourceCooling("192.0.2.1") {
		t.Fatal("the detector's answer ended the cooldown that device traffic set")
	}
}

func TestTheDetectorMovesOnToTheNextResolverOfTheList(t *testing.T) {
	positive := dns.BuildAnswerFromIPs(dns.BuildAQuery("x.test", 1), 60, []net.IP{net.IPv4(203, 0, 113, 42)})
	var asked int32
	dnsCfg := config.DNSConfig{Enabled: true, TargetDNS: "192.0.2.1", Upstreams: []string{"https://dns.example/dns-query"}}
	s := detectorWithResolver(t, dnsCfg, func(srv dns.Server) dnsQuerier {
		if srv.DoHURL == "" {
			return &countingQuerier{asked: &asked, err: errors.New("i/o timeout")}
		}
		return &countingQuerier{asked: &asked, body: positive}
	})

	ips, source := throughB4(s)
	if len(ips) != 1 || ips[0] != "203.0.113.42" || source != "doh" {
		t.Fatalf("got %v from %q, want the DoH entry's address after the plain resolver failed", ips, source)
	}
	if got := atomic.LoadInt32(&asked); got != 2 {
		t.Fatalf("%d resolvers were asked, want the primary and then the list entry", got)
	}
}
