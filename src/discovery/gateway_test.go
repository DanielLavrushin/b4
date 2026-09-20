package discovery

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/netprobe"
)

func gatewayProber(t *testing.T, serving, gateway map[string]bool) (*DNSProber, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var probed []string
	p := &DNSProber{
		domain:  "www.whatsapp.com",
		timeout: time.Second,
		serves: func(_ context.Context, ip string) bool {
			return serving[ip]
		},
		connectable: func(_ context.Context, ips []string) bool {
			return len(ips) > 0
		},
		gateway: func(_ context.Context, ip string) bool {
			mu.Lock()
			probed = append(probed, ip)
			mu.Unlock()
			return gateway[ip]
		},
	}
	return p, &probed
}

func TestGatewayProbeRunsOncePerAddressAndConcurrently(t *testing.T) {
	p, probed := gatewayProber(t, map[string]bool{}, map[string]bool{"31.13.72.60": true})
	inner := p.gateway
	p.gateway = func(ctx context.Context, ip string) bool {
		time.Sleep(300 * time.Millisecond)
		return inner(ctx, ip)
	}

	started := time.Now()
	r := p.evaluate(context.Background(),
		[]string{"31.13.72.60"},
		[]string{"31.13.72.60", "157.240.253.60", "57.144.248.34", "57.144.244.34"},
		false)

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("four probes took %s, they must run concurrently", elapsed)
	}
	if len(*probed) != 4 {
		t.Fatalf("an address shared by both lists must be probed once, probed %v", *probed)
	}
	if len(r.GatewayIPs) != 1 || r.GatewayIPs[0] != "31.13.72.60" {
		t.Fatalf("GatewayIPs = %v", r.GatewayIPs)
	}
	if r.IsPoisoned || len(r.ExpectedIPs) != 3 || containsString(r.ExpectedIPs, "31.13.72.60") {
		t.Fatalf("the remaining reference addresses stay as targets without a poisoning verdict: poisoned=%v expected=%v", r.IsPoisoned, r.ExpectedIPs)
	}
}

func TestGatewayProbeIsSkippedWhenThePhaseIsNearlyOut(t *testing.T) {
	p, probed := gatewayProber(t, map[string]bool{}, map[string]bool{"31.13.72.60": true})
	ctx, cancel := context.WithTimeout(context.Background(), gatewayProbeTimeout+connectableTimeout-time.Second)
	defer cancel()

	r := p.evaluate(ctx, []string{"31.13.72.60"}, []string{"31.13.72.60"}, false)

	if len(*probed) != 0 {
		t.Fatalf("no probe may start without budget for it and the connect check, probed %v", *probed)
	}
	if len(r.GatewayIPs) != 0 || r.gatewayIntercepted() {
		t.Fatalf("nothing was measured, got %+v", r)
	}
	if len(r.ExpectedIPs) != 1 || r.TransportBlocked {
		t.Fatalf("the domain keeps its addresses as before: %+v", r)
	}
}

func TestGatewayTerminatedSystemAnswerIsNotDNSPoisoning(t *testing.T) {
	p, probed := gatewayProber(t,
		map[string]bool{"157.240.253.60": true},
		map[string]bool{"31.13.72.60": true},
	)

	r := p.evaluate(context.Background(), []string{"31.13.72.60"}, []string{"157.240.253.60"}, true)

	if r.IsPoisoned {
		t.Fatal("a system answer the gateway terminates says nothing about the resolver, it must not read as poisoning")
	}
	if len(r.GatewayIPs) != 1 || r.GatewayIPs[0] != "31.13.72.60" {
		t.Fatalf("GatewayIPs = %v, want the intercepted system address", r.GatewayIPs)
	}
	if len(r.ExpectedIPs) != 1 || r.ExpectedIPs[0] != "157.240.253.60" {
		t.Fatalf("ExpectedIPs = %v, the intercepted address must not be a target", r.ExpectedIPs)
	}
	if len(r.AlternativeIPs) != 1 || r.AlternativeIPs[0] != "157.240.253.60" {
		t.Fatalf("AlternativeIPs = %v, the serving reference address is the pin that fixes the site", r.AlternativeIPs)
	}
	if r.gatewayIntercepted() {
		t.Fatal("a reachable reference address means there is still something to test")
	}
	if len(*probed) != 1 {
		t.Fatalf("validated reference addresses must not be probed, probed %v", *probed)
	}
}

func TestDomainWithOnlyGatewayTerminatedAddressesIsIntercepted(t *testing.T) {
	p, _ := gatewayProber(t,
		map[string]bool{},
		map[string]bool{"31.13.72.60": true, "157.240.253.60": true},
	)

	r := p.evaluate(context.Background(), []string{"31.13.72.60"}, []string{"157.240.253.60"}, false)

	if !r.gatewayIntercepted() {
		t.Fatalf("every candidate is terminated on the gateway, got %+v", r)
	}
	if r.IsPoisoned || r.TransportBlocked {
		t.Fatalf("neither poisoning nor an IP block was measured: poisoned=%v transport=%v", r.IsPoisoned, r.TransportBlocked)
	}
	if len(r.ExpectedIPs) != 0 || len(r.AlternativeIPs) != 0 {
		t.Fatalf("no address may remain as a target: expected=%v alternative=%v", r.ExpectedIPs, r.AlternativeIPs)
	}
	if len(r.GatewayIPs) != 2 {
		t.Fatalf("GatewayIPs = %v, want both addresses", r.GatewayIPs)
	}
}

func TestGatewayAddressesAreExcludedFromTargets(t *testing.T) {
	ds := altSuite(t, "www.whatsapp.com", &DNSDiscoveryResult{
		ExpectedIPs:  []string{"157.240.253.60"},
		GatewayIPs:   []string{"31.13.72.60"},
		ProbeResults: []DNSProbeResult{{ResolvedIP: "31.13.72.60"}, {ResolvedIP: "157.240.253.60", Works: true}},
	})

	ips := ds.collectTargetIPs("www.whatsapp.com", 2)
	if len(ips) != 1 || ips[0] != "157.240.253.60" {
		t.Fatalf("collectTargetIPs = %v, the gateway address must never be dialled", ips)
	}
	cidrs := ds.targetIPsFor([]string{"www.whatsapp.com"})
	if len(cidrs) != 1 || cidrs[0] != "157.240.253.60/32" {
		t.Fatalf("targetIPsFor = %v, the gateway address must not enter the set", cidrs)
	}
}

func TestGatewayInterceptedDomainOutcome(t *testing.T) {
	dr := &DomainDiscoveryResult{
		Domain:    "www.whatsapp.com",
		Results:   map[string]*DomainPresetResult{},
		DNSResult: &DNSDiscoveryResult{GatewayIPs: []string{"31.13.72.60"}},
	}
	dr.refreshOutcome(false)
	if dr.Outcome != OutcomeGatewayIntercepted {
		t.Fatalf("Outcome = %q, want %q before the run ends", dr.Outcome, OutcomeGatewayIntercepted)
	}
	dr.refreshOutcome(true)
	if dr.Outcome != OutcomeGatewayIntercepted {
		t.Fatalf("Outcome = %q, want %q", dr.Outcome, OutcomeGatewayIntercepted)
	}

	e := HistoryEntry{DNSResult: dr.DNSResult}
	if got := e.EffectiveOutcome(); got != OutcomeGatewayIntercepted {
		t.Fatalf("EffectiveOutcome = %q, want %q", got, OutcomeGatewayIntercepted)
	}

	withAlternative := &DNSDiscoveryResult{GatewayIPs: []string{"31.13.72.60"}, AlternativeIPs: []string{"157.240.253.60"}}
	if withAlternative.gatewayIntercepted() {
		t.Fatal("an alternative address leaves something to test")
	}
}

func TestGatewayInterceptedDomainIsNotFetched(t *testing.T) {
	ds := altSuite(t, "www.whatsapp.com", &DNSDiscoveryResult{GatewayIPs: []string{"31.13.72.60"}})
	if !ds.allDomainsGatewayIntercepted() || !ds.allDomainsTransportBlocked() {
		t.Fatal("a run where every domain is gateway-terminated has nothing left to search")
	}

	orig := netprobe.GatewayProbe
	netprobe.GatewayProbe = func(context.Context, string, int, int, time.Duration) bool {
		t.Fatal("no probe may run once the domain is known to be intercepted")
		return false
	}
	t.Cleanup(func() { netprobe.GatewayProbe = orig })

	r := ds.fetchForDomain(DomainInput{Domain: "www.whatsapp.com", CheckURL: "https://www.whatsapp.com/"}, time.Second)
	if r.Status != CheckStatusFailed || r.Error == "" {
		t.Fatalf("the fetch must fail without touching the network, got %+v", r)
	}
}

func TestGatewayProbeSkipsCanceledContexts(t *testing.T) {
	ds := altSuite(t, "www.whatsapp.com", &DNSDiscoveryResult{})
	orig := netprobe.GatewayProbe
	called := false
	netprobe.GatewayProbe = func(context.Context, string, int, int, time.Duration) bool {
		called = true
		return true
	}
	t.Cleanup(func() { netprobe.GatewayProbe = orig })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ds.gatewayTerminates(ctx, "31.13.72.60") || called {
		t.Fatal("a canceled scan must not report a gateway")
	}
	if !ds.gatewayTerminates(context.Background(), "31.13.72.60") || !called {
		t.Fatal("the stubbed probe must be consulted")
	}
}
