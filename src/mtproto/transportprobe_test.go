package mtproto

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func probeTestPlans(n int) []transportPlan {
	plans := make([]transportPlan, n)
	for i := range plans {
		plans[i] = transportPlan{kind: transportTCP, addr: fmt.Sprintf("192.0.2.%d:443", i+1)}
	}
	return plans
}

func TestProbePlansTestsRoutesThreeAtATime(t *testing.T) {
	plans := probeTestPlans(7)
	var running, peak, calls atomic.Int32
	probe := func(p transportPlan) TransportProbeResult {
		calls.Add(1)
		now := running.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(60 * time.Millisecond)
		running.Add(-1)
		return TransportProbeResult{Transport: p.describe(), OK: true}
	}

	start := time.Now()
	out := probePlans(plans, dialRaceMaxInFlight, time.Now().Add(time.Minute), probe)
	took := time.Since(start)

	if got := peak.Load(); got != int32(dialRaceMaxInFlight) {
		t.Errorf("routes in flight at once: %d, want %d", got, dialRaceMaxInFlight)
	}
	if got := calls.Load(); got != int32(len(plans)) {
		t.Errorf("probed %d routes, want %d", got, len(plans))
	}
	if sequential := time.Duration(len(plans)) * 60 * time.Millisecond; took >= sequential {
		t.Errorf("run took %v, no faster than testing one route after another (%v)", took, sequential)
	}
	for i, r := range out {
		if want := plans[i].describe(); r.Transport != want || !r.OK {
			t.Errorf("result %d: %+v, want an OK result for %s in plan order", i, r, want)
		}
	}
}

func TestProbePlansReportsRoutesPastTheDeadlineAsUntested(t *testing.T) {
	plans := probeTestPlans(5)
	deadline := time.Now().Add(200 * time.Millisecond)
	var calls atomic.Int32
	probe := func(p transportPlan) TransportProbeResult {
		calls.Add(1)
		time.Sleep(time.Until(deadline) + 50*time.Millisecond)
		return TransportProbeResult{Transport: p.describe(), OK: true}
	}

	out := probePlans(plans, 2, deadline, probe)

	if got := calls.Load(); got != 2 {
		t.Fatalf("probed %d routes, want only the 2 started before the deadline", got)
	}
	if len(out) != len(plans) {
		t.Fatalf("got %d results for %d routes", len(out), len(plans))
	}
	for i, r := range out {
		want := plans[i].describe()
		if r.Transport != want {
			t.Errorf("result %d names %s, want %s", i, r.Transport, want)
		}
		if i < 2 {
			if !r.OK {
				t.Errorf("result %d: %+v, a route started before the deadline must report its own outcome", i, r)
			}
			continue
		}
		if r.OK || r.Stage != probeStageUntested || r.Error != "" {
			t.Errorf("result %d: %+v, want stage %q with no error", i, r, probeStageUntested)
		}
	}
}

func TestProbePlansWithNoRoutesReturnsNothing(t *testing.T) {
	out := probePlans(nil, dialRaceMaxInFlight, time.Now().Add(time.Minute), func(transportPlan) TransportProbeResult {
		t.Error("no route to probe")
		return TransportProbeResult{}
	})
	if len(out) != 0 {
		t.Errorf("got %d results, want none", len(out))
	}
}
