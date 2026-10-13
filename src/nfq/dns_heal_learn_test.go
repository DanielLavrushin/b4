package nfq

import (
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

const (
	healDeadIP = "203.0.113.60"
	healGoodIP = "198.51.100.60"
)

func markDead(t *testing.T, w *Worker, ip string) {
	t.Helper()
	for range 3 {
		w.ipHealth.RecordSyn(ip, 443, 0, 3, time.Hour)
	}
	if !waitFor(t, func() bool { return w.ipHealth.IsDead(ip) }) {
		t.Fatalf("%s never became dead", ip)
	}
}

func stubHealLearning(t *testing.T, gate <-chan struct{}) func() []string {
	t.Helper()
	prevDSCP, prevDSCPAsync := DSCPLearnFunc, DSCPLearnAsyncFunc
	prevSync, prevAsync, prevAwait := RoutingHandleDNSFunc, RoutingHandleDNSAsyncFunc, RoutingHandleDNSAwaitFunc
	t.Cleanup(func() {
		DSCPLearnFunc, DSCPLearnAsyncFunc = prevDSCP, prevDSCPAsync
		RoutingHandleDNSFunc, RoutingHandleDNSAsyncFunc, RoutingHandleDNSAwaitFunc = prevSync, prevAsync, prevAwait
	})
	var mu sync.Mutex
	var learned []string
	note := func(hook string, ips []net.IP) bool {
		mu.Lock()
		defer mu.Unlock()
		gained := false
		for _, ip := range ips {
			learned = append(learned, hook+" "+ip.String())
			gained = gained || ip.Equal(net.ParseIP(healGoodIP))
		}
		return gained
	}
	DSCPLearnFunc = func(_ *config.Config, _ *config.SetConfig, ips []net.IP, _ bool) []<-chan struct{} {
		if note("dscp", ips) {
			return []<-chan struct{}{gate}
		}
		return nil
	}
	DSCPLearnAsyncFunc = nil
	RoutingHandleDNSFunc = func(_ *config.Config, _ *config.SetConfig, ips []net.IP) { note("route", ips) }
	RoutingHandleDNSAsyncFunc = nil
	RoutingHandleDNSAwaitFunc = func(_ *config.Config, _ *config.SetConfig, ips []net.IP) []<-chan struct{} {
		note("route", ips)
		return nil
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(learned)
	}
}

func receive[T any](ch <-chan T, d time.Duration) (T, bool) {
	select {
	case v := <-ch:
		return v, true
	case <-time.After(d):
		var zero T
		return zero, false
	}
}

type healedAnswer func(d time.Duration) ([]byte, bool)

func TestAHealedAnswerWaitsForTheAddressItWasGiven(t *testing.T) {
	holdLimit(t, time.Minute)
	query := dns.BuildQuery("www.example.com", 0x4242, dnsTypeA)
	for _, site := range []struct {
		name string
		ask  func(t *testing.T, w *Worker, cfg *config.Config, set *config.SetConfig) healedAnswer
	}{
		{"a forwarded reply", func(t *testing.T, w *Worker, _ *config.Config, _ *config.SetConfig) healedAnswer {
			q := newFakeModQueue()
			handleReply(t, w, &verdictCtx{id: 1, q: q}, replyRaw(buildTestAResponse("www.example.com", healDeadIP), rigRouter, rigDevice, 40000))
			return func(d time.Duration) ([]byte, bool) {
				v, ok := receive(q.verdicts, d)
				if !ok || v.packet == nil {
					return nil, ok
				}
				return v.packet[28:], true
			}
		}},
		{"a redirected answer", func(t *testing.T, w *Worker, cfg *config.Config, set *config.SetConfig) healedAnswer {
			set.DNS.Enabled, set.DNS.DoHURL = true, dscpDoHServer(t, w, healDeadIP)
			replies := watchReplies(t)
			done := make(chan struct{})
			go func() {
				defer close(done)
				w.resolveDNSRedirect(IPv4, set, cfg, query, net.ParseIP(rigDevice).To4(), 40000, net.ParseIP(rigRouter).To4(), nil, 0, false)
			}()
			t.Cleanup(func() { <-done })
			return func(d time.Duration) ([]byte, bool) { return receive(replies.sent, d) }
		}},
		{"a TCP answer", func(t *testing.T, w *Worker, _ *config.Config, set *config.SetConfig) healedAnswer {
			set.DNS.Enabled, set.DNS.DoHURL = true, dscpDoHServer(t, w, healDeadIP)
			answered := dscpTCPAsk(t, w, query)
			return func(d time.Duration) ([]byte, bool) { return receive(answered, d) }
		}},
	} {
		t.Run(site.name, func(t *testing.T) {
			set := dscpHookSet("heal", "example.com", true, true)
			set.TCP.IPBlockDetect.Enabled, set.TCP.IPBlockDetect.HealDNS = true, true
			w, cfg := dscpHookWorker(t, set)
			markDead(t, w, healDeadIP)
			w.goodIPs.Remember("www.example.com", net.ParseIP(healGoodIP))
			gate := make(chan struct{})
			open := sync.OnceFunc(func() { close(gate) })
			learned := stubHealLearning(t, gate)
			answer := site.ask(t, w, cfg, set)
			t.Cleanup(open)

			if resp, ok := answer(40 * time.Millisecond); ok {
				t.Fatalf("the healed answer %v went out before b4 learned the address heal put in it, so the device connects to an address its set does not route", dns.ParseResponseIPs(resp))
			}
			open()
			resp, ok := answer(5 * time.Second)
			if !ok {
				t.Fatal("the healed answer never came")
			}
			onlyAddress(t, resp, healGoodIP)
			for _, want := range []string{"dscp " + healGoodIP, "route " + healGoodIP} {
				if !slices.Contains(learned(), want) {
					t.Errorf("%s was never learned, learned %v", want, learned())
				}
			}
			if _, host, ok := w.hostHints.Lookup(rigDevice, healGoodIP); !ok || host != "www.example.com" {
				t.Errorf("the host hint for %s names %q, want www.example.com", healGoodIP, host)
			}
		})
	}
}
