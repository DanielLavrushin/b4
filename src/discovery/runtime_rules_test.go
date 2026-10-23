package discovery

import (
	"errors"
	"sync"
	"testing"
)

func newTestSuite(t *testing.T) *CheckSuite {
	t.Helper()
	suite := NewCheckSuite([]DomainInput{{Domain: "example.com", CheckURL: "https://example.com/"}})
	t.Cleanup(func() {
		suitesMu.Lock()
		delete(activeSuites, suite.Id)
		suitesMu.Unlock()
	})
	return suite
}

func newTestState() *runtimeState {
	return &runtimeState{pool: &fakeStopPool{}, clearRules: func() {}}
}

func (s *CheckSuite) canceledForTest() bool {
	select {
	case <-s.cancel:
		return true
	default:
		return false
	}
}

func TestLostFirewallRulesStopTheRunTheyBelongTo(t *testing.T) {
	suite := newTestSuite(t)
	rt := NewRuntime()
	state := newTestState()
	rt.state = state
	rt.activateSuite(suite)

	rt.rulesLost(state, errors.New("iptables rejected the rule that writes DSCP 25 into Discovery's probes"))

	if suite.Status != CheckStatusCanceled || !suite.canceledForTest() {
		t.Errorf("results tested without the set's DSCP value come from another network path, so the run must stop; status %q", suite.Status)
	}

	stopping := newTestSuite(t)
	rt2 := NewRuntime()
	state2 := newTestState()
	rt2.state = state2
	rt2.activateSuite(stopping)
	state2.stopping = true
	rt2.rulesLost(state2, errors.New("signal while the run is already stopping"))
	if stopping.Status == CheckStatusCanceled {
		t.Error("a run that is already stopping is left to finish its own way")
	}
}

func TestALateLossSignalDoesNotStopTheNextRun(t *testing.T) {
	runA, runB := newTestSuite(t), newTestSuite(t)
	rt := NewRuntime()
	stateA := newTestState()
	rt.state = stateA
	rt.activateSuite(runA)

	rt.state = nil
	stateB := newTestState()
	rt.state = stateB
	rt.activateSuite(runB)

	rt.rulesLost(stateA, errors.New("run A's rules could not be put back"))
	if runB.Status == CheckStatusCanceled || runB.canceledForTest() {
		t.Error("run A's firewall error arrived after run B started and stopped run B, whose rules are intact")
	}
}

func TestALossSignalDuringStartupStopsTheRunOnceItIsRegistered(t *testing.T) {
	suite := newTestSuite(t)
	rt := NewRuntime()
	state := newTestState()
	rt.state = state

	rt.rulesLost(state, errors.New("the rules went missing before the run was registered"))
	if state.lostErr == nil {
		t.Fatal("a signal before registration must be kept, or the run starts with missing rules")
	}
	rt.activateSuite(suite)
	if suite.Status != CheckStatusCanceled || !suite.canceledForTest() {
		t.Errorf("the kept signal must stop the run when it is registered; status %q", suite.Status)
	}
	if got, ok := GetCheckSuite(suite.Id); !ok || got != suite {
		t.Error("the run must still be registered, so its result can be read")
	}
}

func TestALossSignalRacingRegistrationAlwaysStopsTheRun(t *testing.T) {
	for i := range 200 {
		suite := newTestSuite(t)
		rt := NewRuntime()
		state := newTestState()
		rt.state = state

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			rt.rulesLost(state, errors.New("the rules went missing during startup"))
		}()
		rt.activateSuite(suite)
		wg.Wait()

		if suite.Status != CheckStatusCanceled {
			t.Fatalf("round %d: the signal raced registration and was lost, the run goes on without its rules (status %q)", i, suite.Status)
		}
	}
}
