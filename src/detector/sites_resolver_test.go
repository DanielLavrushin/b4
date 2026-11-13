package detector

import (
	"context"
	"net"
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

func TestTheDetectorLeavesResolverHealthAlone(t *testing.T) {
	previous := newDNSQuerier
	t.Cleanup(func() { newDNSQuerier = previous })
	dns.ResetSourceHealth()
	t.Cleanup(func() { dns.ResetSourceHealth() })

	newDNSQuerier = func(s *Suite, srv dns.Server) dnsQuerier {
		return &fakeQuerier{answer: func(domain string) []byte {
			return dns.BuildServfailResponse(dns.BuildAQuery(domain, 3))
		}}
	}
	s := &Suite{setDNS: map[string]config.DNSConfig{
		"set1": {Enabled: true, TargetDNS: "192.0.2.1"},
	}}
	for i := 0; i < dns.SourceFailuresToTrip+1; i++ {
		s.resolveThroughB4(context.Background(), SiteResult{SetId: "set1", SetEnabled: true, Domain: "x.test"}, "ipv4")
	}
	if dns.SourceUnreachable("192.0.2.1") {
		t.Fatal("a detector run cooled down a resolver that device queries use")
	}
}
