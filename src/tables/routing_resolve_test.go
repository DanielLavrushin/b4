package tables

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

func stubSetLookup(t *testing.T, answer func(dns.Server, string) ([]net.IP, error)) {
	t.Helper()
	orig := routeSetLookup
	dns.ResetSourceHealth()
	t.Cleanup(func() {
		routeSetLookup = orig
		dns.ResetSourceHealth()
	})
	routeSetLookup = func(_ context.Context, srv dns.Server, host string, _, _ bool) ([]net.IP, error) {
		return answer(srv, host)
	}
}

func TestPreResolutionAsksTheSetsDNSServer(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	set := config.NewSetConfig()
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "9.9.9.9"

	var used dns.Server
	stubSetLookup(t, func(srv dns.Server, host string) ([]net.IP, error) {
		used = srv
		return []net.IP{net.IPv4(203, 0, 113, 60)}, nil
	})

	ips := routeResolveHost(&cfg, &set, "blocked.example")
	if len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 60)) {
		t.Fatalf("got %v, want the set's DNS answer", ips)
	}
	if !used.UDP.Equal(net.IPv4(9, 9, 9, 9)) || used.Mark != int(cfg.MainInjectedMark()) || used.Timeout != cfg.DNSQueryTimeout() {
		t.Fatalf("lookup went to %+v", used)
	}
}

func TestPreResolutionFallsBackUnlessStrict(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	set := config.NewSetConfig()
	set.DNS.Enabled = true
	set.DNS.DoHURL = "https://dns.example/dns-query"
	stubSetLookup(t, func(dns.Server, string) ([]net.IP, error) { return nil, errors.New("resolver down") })

	if ips := routeResolveHost(&cfg, &set, "localhost"); len(ips) == 0 {
		t.Fatal("with Strict off a failed set resolver must fall back to the router's resolver")
	}
	set.DNS.Strict = true
	if ips := routeResolveHost(&cfg, &set, "localhost"); len(ips) != 0 {
		t.Fatalf("with Strict on the router's resolver was used anyway: %v", ips)
	}
}

func TestPreResolutionWithoutSetDNSUsesTheRoutersResolver(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	set := config.NewSetConfig()
	stubSetLookup(t, func(dns.Server, string) ([]net.IP, error) {
		t.Error("a set with DNS off must not ask a set resolver")
		return nil, nil
	})
	if ips := routeResolveHost(&cfg, &set, "localhost"); len(ips) == 0 {
		t.Fatal("localhost did not resolve through the router's resolver")
	}
}

func TestPreResolutionSkipsAStrictSetsResolverWhileItCoolsDown(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	set := config.NewSetConfig()
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "9.9.9.9"
	set.DNS.Strict = true
	asked := 0
	stubSetLookup(t, func(dns.Server, string) ([]net.IP, error) {
		asked++
		return nil, errors.New("resolver down")
	})

	for i := 0; i < dns.SourceFailuresToTrip+5; i++ {
		if ips := routeResolveHost(&cfg, &set, "blocked.example"); len(ips) != 0 {
			t.Fatalf("a strict set got %v from a dead resolver", ips)
		}
	}
	if asked != dns.SourceFailuresToTrip {
		t.Fatalf("the dead resolver was asked %d times in one pass, want %d before it cools down", asked, dns.SourceFailuresToTrip)
	}
}

func TestChangingTheSetsResolverResolvesItAgain(t *testing.T) {
	a := config.NewSetConfig()
	a.Targets.SNIDomains = []string{"blocked.example"}
	b := a
	if !routeSameResolveTargets(&a, &b) {
		t.Fatal("identical sets differ")
	}
	b.DNS.Enabled = true
	b.DNS.DoHURL = "https://dns.example/dns-query"
	if routeSameResolveTargets(&a, &b) {
		t.Fatal("turning on the set's resolver must re-resolve its domains")
	}
	c := b
	c.DNS.Strict = true
	if routeSameResolveTargets(&b, &c) {
		t.Fatal("changing Fail closed must re-resolve its domains")
	}
}
