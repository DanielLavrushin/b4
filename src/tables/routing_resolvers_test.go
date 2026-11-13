package tables

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

func TestAResolverListEditReResolvesTheSet(t *testing.T) {
	cfg := config.NewConfig()
	a := config.NewSetConfig()
	a.Targets.SNIDomains = []string{"blocked.example"}
	a.DNS.Enabled = true
	a.DNS.TargetDNS = "9.9.9.9"

	listed := a
	listed.DNS.Upstreams = []string{"1.1.1.1"}
	if routeSameResolveTargets(&a, &listed) {
		t.Error("adding a resolver to the list must re-resolve the set's domains")
	}
	if routeQueuedTargetsKept(&a, &listed) {
		t.Error("addresses queued under the old list must not be kept after the list changed")
	}
	if dscpPreResolverKey(&cfg, &a) == dscpPreResolverKey(&cfg, &listed) {
		t.Error("a list edit must restart the DSCP pre-resolve")
	}

	swapped := a
	swapped.DNS.Upstreams = []string{"1.1.1.1", "8.8.8.8"}
	other := a
	other.DNS.Upstreams = []string{"8.8.8.8", "1.1.1.1"}
	if routeSameResolveTargets(&swapped, &other) {
		t.Error("reordering the list changes who is asked first, so it must re-resolve")
	}

	pinned := a
	pinned.DNS.Pins = map[string][]string{"blocked.example": {"203.0.113.1"}}
	if !routeSameResolveTargets(&a, &pinned) || !routeQueuedTargetsKept(&a, &pinned) || dscpPreResolverKey(&cfg, &a) != dscpPreResolverKey(&cfg, &pinned) {
		t.Error("a pin edit must not re-resolve the set, tables never reads pins")
	}

	strict := a
	strict.DNS.Strict = true
	if routeSameResolveTargets(&a, &strict) || routeQueuedTargetsKept(&a, &strict) || dscpPreResolverKey(&cfg, &a) == dscpPreResolverKey(&cfg, &strict) {
		t.Error("a Fail closed toggle must re-resolve the set")
	}
}

func TestPreResolutionMovesOnToTheNextResolverOfTheList(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	set := config.NewSetConfig()
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "9.9.9.9"
	set.DNS.Upstreams = []string{"1.1.1.1"}

	var order []string
	stubSetLookup(t, func(srv dns.Server, host string) ([]net.IP, error) {
		order = append(order, srv.Source)
		if srv.Source == "9.9.9.9" {
			return nil, errors.New("resolver down")
		}
		return []net.IP{net.IPv4(203, 0, 113, 62)}, nil
	})

	ips := routeResolveHost(&cfg, &set, "blocked.example")
	if len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 62)) {
		t.Fatalf("got %v, want the answer of the list's resolver after the primary failed", ips)
	}
	if len(order) != 2 || order[0] != "9.9.9.9" || order[1] != "1.1.1.1" {
		t.Fatalf("asked %v, want the primary and then the list entry", order)
	}
}

func TestAStrictListsPreResolutionLeavesItsCoolingResolverOut(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	set := config.NewSetConfig()
	set.DNS.Enabled, set.DNS.Strict = true, true
	set.DNS.TargetDNS = "9.9.9.9"
	set.DNS.Upstreams = []string{"1.1.1.1"}

	asked := map[string]int{}
	stubSetLookup(t, func(srv dns.Server, _ string) ([]net.IP, error) {
		asked[srv.Source]++
		if srv.Source == "9.9.9.9" {
			return nil, errors.New("resolver down")
		}
		return nil, dns.ErrTransientAnswer
	})
	for i := 0; i < dns.SourceFailuresToTrip; i++ {
		dns.NoteSourceFailure("9.9.9.9")
	}

	const domains = 5
	for i := 0; i < domains; i++ {
		if ips := routeResolveHost(&cfg, &set, "blocked.example"); len(ips) != 0 {
			t.Fatalf("got %v from a strict set whose resolvers gave no address", ips)
		}
	}
	if asked["9.9.9.9"] != 0 {
		t.Fatalf("pre-resolution asked the cooling resolver for %d of %d domains because the other one answered with an error code: each of those waits out a timeout", asked["9.9.9.9"], domains)
	}
	if asked["1.1.1.1"] != domains {
		t.Fatalf("the resolver that answers was asked %d times, want %d", asked["1.1.1.1"], domains)
	}
}

func TestAPreResolveRunThatEndsStillRecordsItsSilentResolver(t *testing.T) {
	orig := routeSetLookup
	dns.ResetSourceHealth()
	dscpPreResolveStart()
	t.Cleanup(func() {
		dscpPreResolveStop()
		routeSetLookup = orig
		dns.ResetSourceHealth()
	})
	routeSetLookup = func(ctx context.Context, srv dns.Server, _ string, _, _ bool) ([]net.IP, error) {
		if srv.Source != "192.0.2.1" {
			return []net.IP{net.IPv4(203, 0, 113, 70)}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(dns.HeadStart + 100*time.Millisecond):
			return nil, errors.New("no reply")
		}
	}

	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	set := config.NewSetConfig()
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "192.0.2.1"
	set.DNS.Upstreams = []string{"192.0.2.2"}

	for i := 0; i < dns.SourceFailuresToTrip; i++ {
		run, endRun := context.WithCancel(dscpPreLife(context.Background()))
		ips := dscpPreLookup(run, &cfg, &set, "blocked.example")
		endRun()
		if len(ips) != 1 {
			t.Fatalf("run %d: got %v, want the backup's answer", i, ips)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for !dns.SourceCooling("192.0.2.1") {
		if time.Now().After(deadline) {
			t.Fatal("three pre-resolve runs that ended right after their answer never recorded the silent resolver, so every run pays the head start again")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
