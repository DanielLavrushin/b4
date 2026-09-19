package tables

import (
	"net"
	"testing"
	"time"
)

func TestResolvedIPsFromAnOlderRoutingGenerationAreDiscarded(t *testing.T) {
	familyResetGlobals(t)
	pushed := map[string][]string{}
	routeEngine = &mockRouteBackend{addElementsFn: func(name string, ips []string, ttlSec int) {
		pushed[name] = append(pushed[name], ips...)
	}}
	set := familyTestSet()
	cfg := familyTestConfig(true, false)
	ips := []net.IP{net.ParseIP("203.0.113.9")}

	routeMu.Lock()
	routeRuleCache[set.Id] = routeState{setV4: "b4r_famtest_v4", ipv4: true}
	gen := routeGen
	routeMu.Unlock()

	routeMu.Lock()
	routeLastReResolve[set.Id] = time.Now()
	routeMu.Unlock()
	routeAddResolvedIPsAt(cfg, set, ips, gen-1)
	if len(pushed) != 0 {
		t.Fatalf("addresses resolved under an older routing generation were written: %v", pushed)
	}
	routeMu.Lock()
	_, stamped := routeLastReResolve[set.Id]
	routeMu.Unlock()
	if stamped {
		t.Fatalf("a dropped re-resolve kept its timestamp, so the set would wait a full interval before being resolved under the new state")
	}

	routeAddResolvedIPsAt(cfg, set, ips, gen)
	if got := len(pushed["b4r_famtest_v4"]); got != 1 {
		t.Fatalf("addresses resolved under the current generation were not written, got %d entries", got)
	}

	routeMu.Lock()
	routeGen++
	routeMu.Unlock()
	routeAddResolvedIPsAt(cfg, set, ips, gen)
	if got := len(pushed["b4r_famtest_v4"]); got != 1 {
		t.Fatalf("addresses were written after the routing state had been rebuilt, got %d entries", got)
	}
}

func TestEveryRoutingRebuildAdvancesTheGeneration(t *testing.T) {
	familyResetGlobals(t)
	routeEngine = &mockRouteBackend{}
	cfg := familyTestConfig(true, false)

	routeMu.Lock()
	before := routeGen
	routeMu.Unlock()

	RoutingSyncConfig(cfg)
	routeMu.Lock()
	afterSync := routeGen
	routeMu.Unlock()
	if afterSync == before {
		t.Fatalf("a routing sync did not advance the generation")
	}

	RoutingForceResync(cfg)
	routeMu.Lock()
	afterResync := routeGen
	routeMu.Unlock()
	if afterResync <= afterSync {
		t.Fatalf("a forced resync did not advance the generation")
	}

	RoutingClearAll()
	routeMu.Lock()
	afterClear := routeGen
	routeMu.Unlock()
	if afterClear <= afterResync {
		t.Fatalf("clearing the routing state did not advance the generation")
	}
}
