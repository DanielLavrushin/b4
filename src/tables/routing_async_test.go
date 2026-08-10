package tables

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func resetRouteAsync() {
	routeAsyncForgetAll()
	routeAsyncDropped.Store(0)
	routeAsyncLastLog.Store(0)
}

func asyncTestSet(id string, ttl int) *config.SetConfig {
	s := &config.SetConfig{Id: id, Name: id, Enabled: true}
	s.Routing.Enabled = true
	s.Routing.Mode = config.RoutingModeInterface
	s.Routing.EgressInterface = "wg0"
	s.Routing.IPTTLSeconds = ttl
	return s
}

func TestRouteAsyncClaimDeduplicates(t *testing.T) {
	resetRouteAsync()
	set := asyncTestSet("set-a", 3600)
	ips := []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("2.2.2.2")}

	first := routeAsyncClaim(set, ips)
	if len(first) != 2 {
		t.Fatalf("first claim = %d IPs, want 2", len(first))
	}

	second := routeAsyncClaim(set, ips)
	if len(second) != 0 {
		t.Fatalf("second claim = %d IPs, want 0", len(second))
	}

	third := routeAsyncClaim(set, append(ips, net.ParseIP("3.3.3.3")))
	if len(third) != 1 || !third[0].Equal(net.ParseIP("3.3.3.3")) {
		t.Fatalf("third claim = %v, want [3.3.3.3]", third)
	}
}

func TestRouteAsyncClaimIsPerSet(t *testing.T) {
	resetRouteAsync()
	a := asyncTestSet("set-a", 3600)
	b := asyncTestSet("set-b", 3600)
	ips := []net.IP{net.ParseIP("1.1.1.1")}

	if len(routeAsyncClaim(a, ips)) != 1 {
		t.Fatal("claim for set-a should succeed")
	}
	if len(routeAsyncClaim(b, ips)) != 1 {
		t.Fatal("claim for set-b must not be blocked by set-a")
	}
}

func TestRouteAsyncReleaseAllowsRetry(t *testing.T) {
	resetRouteAsync()
	set := asyncTestSet("set-a", 3600)
	ips := []net.IP{net.ParseIP("1.1.1.1")}

	claimed := routeAsyncClaim(set, ips)
	if len(claimed) != 1 {
		t.Fatal("first claim should succeed")
	}
	routeAsyncRelease(set, claimed)

	if len(routeAsyncClaim(set, ips)) != 1 {
		t.Fatal("release must allow the IP to be claimed again")
	}
}

func TestRouteAsyncClaimExpires(t *testing.T) {
	resetRouteAsync()
	set := asyncTestSet("set-a", 2)
	ips := []net.IP{net.ParseIP("1.1.1.1")}

	if len(routeAsyncClaim(set, ips)) != 1 {
		t.Fatal("first claim should succeed")
	}

	routeAsyncSeenMu.Lock()
	routeAsyncSeen[set.Id+"|1.1.1.1"] = time.Now().Add(-2 * time.Second)
	routeAsyncSeenMu.Unlock()

	if len(routeAsyncClaim(set, ips)) != 1 {
		t.Fatal("claim should be renewable once ttl/2 has elapsed")
	}
}

func TestRouteAsyncForgetAll(t *testing.T) {
	resetRouteAsync()
	set := asyncTestSet("set-a", 3600)
	ips := []net.IP{net.ParseIP("1.1.1.1")}

	routeAsyncClaim(set, ips)
	routeAsyncForgetAll()

	if len(routeAsyncClaim(set, ips)) != 1 {
		t.Fatal("a config resync must re-arm every IP")
	}
}

func TestRouteAsyncSubmitRunsJobOffCaller(t *testing.T) {
	resetRouteAsync()

	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})
	routeAsyncSubmit(func() {
		defer wg.Done()
		close(done)
	}, func() {
		t.Error("job must not be dropped on an idle queue")
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("submitted job never ran")
	}
	wg.Wait()
}

func TestRouteAsyncSubmitDropsWhenFull(t *testing.T) {
	resetRouteAsync()
	routeAsyncStart()

	block := make(chan struct{})
	routeAsyncCh <- func() { <-block }
	defer close(block)

	for len(routeAsyncCh) < cap(routeAsyncCh) {
		routeAsyncCh <- func() {}
	}

	dropped := false
	routeAsyncSubmit(func() { t.Error("job should not have been queued") }, func() { dropped = true })

	if !dropped {
		t.Fatal("expected the drop callback to fire on a full queue")
	}
	if routeAsyncDropped.Load() == 0 {
		t.Fatal("expected the drop counter to advance")
	}
}
