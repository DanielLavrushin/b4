package nfq

import (
	"errors"
	"net"
	"testing"

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

func TestWalkResolversMovesOnFromEveryRetryableRcode(t *testing.T) {
	good := dns.BuildAnswerFromIPs(dns.BuildQuery("walk.example", 0x4242, dnsTypeA), 60, []net.IP{net.IPv4(203, 0, 113, 5)})
	for _, rcode := range []uint8{rcodeServfail, rcodeNotImp, rcodeRefused} {
		bad := rcodeAnswer(t, rcode)
		set := walkSet("198.51.100.1", "198.51.100.2", "198.51.100.3")
		var asked []string
		walk := walkResolvers("test", "walk.example", set, func(upstream string) ([]byte, error) {
			asked = append(asked, upstream)
			if upstream == "198.51.100.3" {
				return good, nil
			}
			return bad, nil
		})
		if walk.err != nil {
			t.Fatalf("rcode %d: %v", rcode, walk.err)
		}
		if len(asked) != 3 {
			t.Fatalf("rcode %d: asked %v, want every resolver before the healthy one", rcode, asked)
		}
		if walk.source != "198.51.100.3" || walk.transient {
			t.Fatalf("rcode %d: source %q transient %v, want the healthy resolver", rcode, walk.source, walk.transient)
		}
	}
}

func TestWalkResolversNamesTheResolverThatAnswered(t *testing.T) {
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
