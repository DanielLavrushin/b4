package nfq

import (
	"maps"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/florianl/go-nfqueue"
)

func TestBlockSetsLearnNoAddressesFromDNSAnswers(t *testing.T) {
	client := net.ParseIP("192.168.1.100").To4()
	server := net.ParseIP("192.168.1.1").To4()
	sites := []struct {
		name  string
		hook  string
		learn func(t *testing.T, set *config.SetConfig)
	}{
		{"an answer from the resolver the device asked", "await", func(t *testing.T, set *config.SetConfig) {
			w, _ := dscpHookWorker(t, set)
			q := newFakeQueue()
			w.processDnsPacket(&verdictCtx{id: 1, q: q}, answerPacket(), 53, 40000, buildDNSResponse(1, "www.example.com", []net.IP{net.ParseIP("203.0.113.10")}))
			if v := q.now(t); v != nfqueue.NfAccept {
				t.Fatalf("verdict %d, want accept", v)
			}
			holdsDone(t, w)
		}},
		{"an answer to a query that noted its set", "await", func(t *testing.T, set *config.SetConfig) {
			t.Cleanup(stopDNSRouteCleanup)
			w, _ := dscpHookWorker(t, set)
			storeDNSPendingRoute(dnsRouteKeyRequest(IPv4, client, 40000, server, 53, 2, "www.other.org"), set.Id)
			q := newFakeQueue()
			w.processDnsPacket(&verdictCtx{id: 2, q: q}, answerPacket(), 53, 40000, buildDNSResponse(2, "www.other.org", []net.IP{net.ParseIP("203.0.113.11")}))
			if v := q.now(t); v != nfqueue.NfAccept {
				t.Fatalf("verdict %d, want accept", v)
			}
			holdsDone(t, w)
			if _, left := consumeDNSPendingRoute(dnsRouteKeyRequest(IPv4, client, 40000, server, 53, 2, "www.other.org")); left {
				t.Fatal("the answer did not use the route its query noted")
			}
		}},
		{"an answer from the set's own resolver", "sync", func(t *testing.T, set *config.SetConfig) {
			w, cfg := dscpHookWorker(t, set)
			set.DNS.Enabled, set.DNS.DoHURL = true, dscpDoHServer(t, w, "203.0.113.12")
			replies := watchReplies(t)
			w.resolveDNSRedirect(IPv4, set, cfg, dns.BuildQuery("www.example.com", 3, dnsTypeA), client, 40000, server, nil, 0)
			onlyAddress(t, oneReply(t, replies), "203.0.113.12")
		}},
		{"a pinned answer", "await", func(t *testing.T, set *config.SetConfig) {
			w, cfg := dscpHookWorker(t, set)
			set.DNS.Pins = map[string][]string{"pin.example.com": {"203.0.113.13"}}
			pinned := w.pinnedAnswer(set, dns.BuildQuery("pin.example.com", 4, dnsTypeA), "pin.example.com")
			if pinned == nil {
				t.Fatal("the set gave no pinned answer")
			}
			if action, waits := w.applyPinnedAnswerAwait(cfg, set, client, "pin.example.com", pinned); action != dnsActionPin || len(waits) != 0 {
				t.Fatalf("action %q with %d waits, want %q with none", action, len(waits), dnsActionPin)
			}
		}},
		{"an escalation to the set", "async", func(t *testing.T, set *config.SetConfig) {
			_, cfg := dscpHookWorker(t, set)
			registerEscalatedRoute(cfg, set, []net.IP{net.ParseIP("203.0.113.14")})
		}},
	}
	for _, site := range sites {
		for _, block := range []bool{true, false} {
			name := site.name + " for a routed set"
			if block {
				name = site.name + " for a block set"
			}
			t.Run(name, func(t *testing.T) {
				route := stubRouteHooks(t)
				set := dscpHookSet("learner", "example.com", false, true)
				if block {
					set.Routing.Mode = config.RoutingModeBlock
				}
				site.learn(t, set)
				want := map[string]int{site.hook + " learner": 1}
				if block {
					want = map[string]int{}
				}
				if got := route.snapshot(); !maps.Equal(got, want) {
					t.Fatalf("routing hook calls %v, want %v", got, want)
				}
			})
		}
	}
}
