package mtproto

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

type probeEventLog struct {
	mu       sync.Mutex
	events   []string
	started  map[int]int
	finished map[int]TransportProbeResult
	inside   atomic.Int32
	overlap  atomic.Bool
}

func newProbeEventLog() *probeEventLog {
	return &probeEventLog{started: map[int]int{}, finished: map[int]TransportProbeResult{}}
}

func (l *probeEventLog) enter() {
	if l.inside.Add(1) > 1 {
		l.overlap.Store(true)
	}
}

func (l *probeEventLog) observer() TransportProbeObserver {
	return TransportProbeObserver{
		Started: func(i int) {
			l.enter()
			defer l.inside.Add(-1)
			l.mu.Lock()
			defer l.mu.Unlock()
			l.started[i]++
			l.events = append(l.events, fmt.Sprintf("start %d", i))
		},
		Finished: func(i int, r TransportProbeResult) {
			l.enter()
			defer l.inside.Add(-1)
			time.Sleep(time.Millisecond)
			l.mu.Lock()
			defer l.mu.Unlock()
			if _, dup := l.finished[i]; dup {
				l.events = append(l.events, fmt.Sprintf("duplicate result %d", i))
			}
			l.finished[i] = r
			l.events = append(l.events, fmt.Sprintf("result %d", i))
		},
	}
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
	out := probePlans(context.Background(), plans, dialRaceMaxInFlight, probe, TransportProbeObserver{})
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

func TestProbePlansReportsEachRouteWhileOthersAreStillRunning(t *testing.T) {
	plans := probeTestPlans(6)
	release := make(chan struct{})
	probe := func(p transportPlan) TransportProbeResult {
		if p.addr == plans[0].addr {
			select {
			case <-release:
			case <-time.After(5 * time.Second):
				return TransportProbeResult{Transport: p.describe(), Error: "never released"}
			}
		}
		return TransportProbeResult{Transport: p.describe(), OK: true}
	}
	events := newProbeEventLog()
	obs := events.observer()
	finished := obs.Finished
	obs.Finished = func(i int, r TransportProbeResult) {
		finished(i, r)
		if i == len(plans)-1 {
			close(release)
		}
	}

	out := probePlans(context.Background(), plans, dialRaceMaxInFlight, probe, obs)

	if !out[0].OK {
		t.Fatalf("the first route was released only after the last route's result went out, got %+v: results were held back until the run ended", out[0])
	}
	if events.overlap.Load() {
		t.Error("two observer calls ran at once")
	}
	for i := range plans {
		if events.started[i] != 1 {
			t.Errorf("route %d started %d times, want once", i, events.started[i])
		}
		if r, ok := events.finished[i]; !ok || r != out[i] {
			t.Errorf("route %d reported %+v, returned %+v", i, r, out[i])
		}
	}
	seen := map[int]bool{}
	for _, e := range events.events {
		var i int
		switch {
		case strings.HasPrefix(e, "duplicate"):
			t.Errorf("%s: %v", e, events.events)
		case strings.HasPrefix(e, "start"):
			fmt.Sscanf(e, "start %d", &i)
			seen[i] = true
		default:
			fmt.Sscanf(e, "result %d", &i)
			if !seen[i] {
				t.Errorf("result for route %d came before its start: %v", i, events.events)
			}
		}
	}
}

func TestProbePlansReportsRoutesPastTheDeadlineAsUntested(t *testing.T) {
	plans := probeTestPlans(5)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	deadline, _ := ctx.Deadline()
	var calls atomic.Int32
	probe := func(p transportPlan) TransportProbeResult {
		calls.Add(1)
		time.Sleep(time.Until(deadline) + 50*time.Millisecond)
		return TransportProbeResult{Transport: p.describe(), OK: true}
	}
	events := newProbeEventLog()

	out := probePlans(ctx, plans, 2, probe, events.observer())

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
		if got := events.finished[i]; got != r {
			t.Errorf("result %d reported as %+v, returned as %+v", i, got, r)
		}
		if i < 2 {
			if !r.OK {
				t.Errorf("result %d: %+v, a route started before the deadline must report its own outcome", i, r)
			}
			continue
		}
		if events.started[i] != 0 {
			t.Errorf("route %d past the deadline was reported as started", i)
		}
		if r.OK || r.Stage != probeStageUntested || r.Error != "" {
			t.Errorf("result %d: %+v, want stage %q with no error", i, r, probeStageUntested)
		}
	}
}

func TestProbePlansStartsNoRouteOnceTheRequestIsGone(t *testing.T) {
	plans := probeTestPlans(8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	probe := func(p transportPlan) TransportProbeResult {
		if calls.Add(1) == 1 {
			cancel()
		}
		time.Sleep(20 * time.Millisecond)
		return TransportProbeResult{Transport: p.describe(), OK: true}
	}

	start := time.Now()
	out := probePlans(ctx, plans, 1, probe, TransportProbeObserver{})

	if got := calls.Load(); got != 1 {
		t.Errorf("probed %d routes after the request was cancelled, want only the one already running", got)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("run took %v after cancellation", took)
	}
	for i, r := range out[1:] {
		if r.Stage != probeStageUntested {
			t.Errorf("result %d: %+v, want stage %q", i+1, r, probeStageUntested)
		}
	}
}

func TestProbePlansWithNoRoutesReturnsNothing(t *testing.T) {
	obs := TransportProbeObserver{
		Started:  func(int) { t.Error("no route to start") },
		Finished: func(int, TransportProbeResult) { t.Error("no route to report") },
	}
	out := probePlans(context.Background(), nil, dialRaceMaxInFlight, func(transportPlan) TransportProbeResult {
		t.Error("no route to probe")
		return TransportProbeResult{}
	}, obs)
	if len(out) != 0 {
		t.Errorf("got %d results, want none", len(out))
	}
}
