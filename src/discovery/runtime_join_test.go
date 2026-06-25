package discovery

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/leaktest"
	"go.uber.org/goleak"
)

type fakeStopPool struct{ stopped atomic.Bool }

func (f *fakeStopPool) Stop() { f.stopped.Store(true) }

// TestStopJoinsDiscoveryGoroutine asserts that Stop does not return until the
// background discovery goroutine (and the probe goroutines it joins) have fully
// exited. The previous implementation slept a fixed 500ms instead of joining,
// so it would return while the run was still in flight; that race is what let a
// restart spin up a second run on the same queue.
func TestStopJoinsDiscoveryGoroutine(t *testing.T) {
	defer goleak.VerifyNone(t, leaktest.Options()...)

	rt := NewRuntime()
	pool := &fakeStopPool{}

	suite := NewCheckSuite([]DomainInput{{Domain: "example.com", CheckURL: "https://example.com/"}})
	suite.Status = CheckStatusRunning
	RegisterSuite(suite)

	rt.state = &runtimeState{
		pool:          pool,
		clearRules:    func() {},
		activeSuiteID: suite.Id,
	}

	proceed := make(chan struct{})
	probeJoined := atomic.Bool{}

	rt.launchSuite(suite.Id, func() {
		// Mimic RunDiscovery: spawn a probe that unwinds on cancel, then join it
		// (as RunDiscovery does via wg.Wait) before returning.
		var probes sync.WaitGroup
		probes.Add(1)
		go func() {
			defer probes.Done()
			<-suite.cancel
		}()
		<-suite.cancel
		probes.Wait()
		probeJoined.Store(true)
		<-proceed
	})

	stopDone := make(chan struct{})
	go func() {
		rt.Stop(suite.Id)
		close(stopDone)
	}()

	// Stop must still be blocked: the run goroutine is parked on `proceed`.
	// 700ms comfortably exceeds the old hardcoded 500ms grace sleep, so the
	// pre-fix implementation would have returned here and failed the test.
	select {
	case <-stopDone:
		t.Fatal("Stop returned before the discovery goroutine exited (no join)")
	case <-time.After(700 * time.Millisecond):
	}

	if !probeJoined.Load() {
		t.Fatal("expected Stop to have canceled the suite and the probe to unwind")
	}

	close(proceed)

	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after the discovery goroutine exited")
	}

	if !pool.stopped.Load() {
		t.Fatal("expected pool.Stop to be called during teardown")
	}
	if rt.IsActive() {
		t.Fatal("expected runtime state to be cleared after Stop")
	}
}
