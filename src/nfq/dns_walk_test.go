package nfq

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

func walkSet(upstreams ...string) *config.SetConfig {
	return &config.SetConfig{
		Name: "walk",
		DNS:  config.DNSConfig{Enabled: true, Upstreams: upstreams},
	}
}

func rcodeAnswer(t *testing.T, rcode uint8) []byte {
	t.Helper()
	resp := dns.BuildServfailResponse(dns.BuildQuery("walk.example", 0x4242, dnsTypeA))
	if len(resp) < 4 {
		t.Fatal("no response to work with")
	}
	resp[3] = resp[3]&0xf0 | rcode&0x0f
	return resp
}

func addressAnswer(t *testing.T, ip net.IP) []byte {
	t.Helper()
	resp := dns.BuildAnswerFromIPs(dns.BuildQuery("walk.example", 0x4242, dnsTypeA), 60, []net.IP{ip})
	if len(resp) == 0 {
		t.Fatal("no answer to work with")
	}
	return resp
}

func walkHealth(t *testing.T) {
	t.Helper()
	dns.ResetSourceHealth()
	t.Cleanup(dns.ResetSourceHealth)
}

func TestWalkResolversWaitsPastAFastRetryableAnswer(t *testing.T) {
	walkHealth(t)
	good := net.IPv4(203, 0, 113, 5)
	for _, rcode := range []uint8{rcodeServfail, rcodeNotImp, rcodeRefused} {
		bad := rcodeAnswer(t, rcode)
		set := walkSet("198.51.100.1", "198.51.100.2")
		walk := walkResolvers("test", "walk.example", set, func(upstream string) ([]byte, error) {
			if upstream == "198.51.100.1" {
				return bad, nil
			}
			time.Sleep(40 * time.Millisecond)
			return addressAnswer(t, good), nil
		})
		if walk.err != nil || walk.transient {
			t.Fatalf("rcode %d: err %v, transient %v; a retryable answer must not end the walk", rcode, walk.err, walk.transient)
		}
		if walk.source != "198.51.100.2" {
			t.Fatalf("rcode %d: source %q, want the resolver that answered", rcode, walk.source)
		}
		if ips := dns.ParseResponseIPs(walk.resp); len(ips) != 1 || !ips[0].Equal(good) {
			t.Fatalf("rcode %d: answer %v, want %s", rcode, ips, good)
		}
	}
}

func TestWalkResolversPrefersAnEncryptedAnswer(t *testing.T) {
	walkHealth(t)
	plain := net.IPv4(203, 0, 113, 6)
	encrypted := net.IPv4(203, 0, 113, 7)
	set := walkSet("198.51.100.1", "https://doh.example/dns-query")
	walk := walkResolvers("test", "walk.example", set, func(upstream string) ([]byte, error) {
		if config.IsDoHUpstream(upstream) {
			time.Sleep(30 * time.Millisecond)
			return addressAnswer(t, encrypted), nil
		}
		return addressAnswer(t, plain), nil
	})
	if ips := dns.ParseResponseIPs(walk.resp); len(ips) != 1 || !ips[0].Equal(encrypted) {
		t.Fatalf("answer %v, want the encrypted resolver's %s", ips, encrypted)
	}
	if walk.source != "https://doh.example/dns-query" {
		t.Fatalf("source %q, want the DoH URL", walk.source)
	}
}

func TestWalkResolversNamesTheResolverThatAnswered(t *testing.T) {
	walkHealth(t)
	bad := rcodeAnswer(t, rcodeServfail)
	set := walkSet("198.51.100.1", "198.51.100.2")
	walk := walkResolvers("test", "walk.example", set, func(upstream string) ([]byte, error) {
		if upstream == "198.51.100.1" {
			return bad, nil
		}
		return nil, errors.New("timeout")
	})
	if walk.err != nil {
		t.Fatalf("a SERVFAIL should still answer the client: %v", walk.err)
	}
	if !walk.transient {
		t.Fatal("a served SERVFAIL must not look like a healthy answer")
	}
	if walk.source != "198.51.100.1" {
		t.Fatalf("source = %q, want the resolver that answered", walk.source)
	}
}

func TestWalkResolversKeepsTheFailureWhenEveryoneFails(t *testing.T) {
	walkHealth(t)
	set := walkSet("198.51.100.1", "198.51.100.2")
	walk := walkResolvers("test", "walk.example", set, func(upstream string) ([]byte, error) {
		return nil, errors.New("unreachable")
	})
	if walk.err == nil {
		t.Fatal("no error after every resolver failed")
	}
	if walk.resp != nil {
		t.Fatal("a response was returned although nobody answered")
	}
}

func TestWalkResolversSkipsACoolingResolver(t *testing.T) {
	walkHealth(t)
	set := walkSet("198.51.100.1", "198.51.100.2")
	for i := 0; i < dns.SourceFailuresToTrip; i++ {
		dns.NoteSourceFailure("198.51.100.1")
	}
	var asked []string
	walk := walkResolvers("test", "walk.example", set, func(upstream string) ([]byte, error) {
		asked = append(asked, upstream)
		return addressAnswer(t, net.IPv4(203, 0, 113, 8)), nil
	})
	if walk.err != nil {
		t.Fatalf("walk: %v", walk.err)
	}
	if len(asked) != 1 || asked[0] != "198.51.100.2" {
		t.Fatalf("asked %v, want only the healthy resolver", asked)
	}
}

func TestWalkResolversAsksACoolingResolverForAStrictSet(t *testing.T) {
	walkHealth(t)
	set := walkSet("198.51.100.1")
	set.DNS.Strict = true
	for i := 0; i < dns.SourceFailuresToTrip; i++ {
		dns.NoteSourceFailure("198.51.100.1")
	}
	walk := walkResolvers("test", "walk.example", set, func(upstream string) ([]byte, error) {
		return addressAnswer(t, net.IPv4(203, 0, 113, 9)), nil
	})
	if walk.err != nil {
		t.Fatalf("a strict set must still ask its only resolver: %v", walk.err)
	}
}
