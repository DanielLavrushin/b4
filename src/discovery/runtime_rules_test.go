package discovery

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/nfq"
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
	if !suite.RulesChanged {
		t.Error("the run must record why it stopped, so a watchdog heal can tell a rules change from a failed search")
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
	if suite.Status != CheckStatusCanceled || !suite.canceledForTest() || !suite.RulesChanged {
		t.Errorf("the kept signal must stop the run when it is registered and say why; status %q, rules changed %t", suite.Status, suite.RulesChanged)
	}
	if got, ok := GetCheckSuite(suite.Id); !ok || got != suite {
		t.Error("the run must still be registered, so its result can be read")
	}
}

func TestARunWhoseRulesChangedIsNotPublishedAsComplete(t *testing.T) {
	previous, previousCheck := suiteRetention, rulesInvalidated
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention, rulesInvalidated = previous, previousCheck })

	for _, tc := range []struct {
		name                 string
		invalidated, flagged bool
	}{
		{"an undisturbed run", false, false},
		{"rules invalidated while the run was finishing", true, false},
		{"flagged by the stop signal before its cancel landed", false, true},
	} {
		invalidated := tc.invalidated || tc.flagged
		rulesInvalidated = func() bool { return tc.invalidated }
		suite := NewCheckSuite([]DomainInput{{Domain: "example.com", CheckURL: "https://example.com/"}})
		suite.SetId = "set-a"
		suite.Status = CheckStatusRunning
		suite.RulesChanged = tc.flagged
		ds := &DiscoverySuite{
			CheckSuite: suite,
			pool:       &nfq.Pool{},
			domainResults: map[string]*DomainDiscoveryResult{"example.com": {
				Domain: "example.com", BestPreset: "p", BestSuccess: true,
				Results: map[string]*DomainPresetResult{"p": {PresetName: "p", Status: CheckStatusComplete}},
			}},
			dnsResults: map[string]*DNSDiscoveryResult{},
			setVerdict: &SetVerdict{Status: SetVerdictCovered, WinnerPreset: "p", Confirmed: true},
		}
		ds.initCancelContext()
		RegisterSuite(ds.CheckSuite)

		ds.finalize()

		switch {
		case invalidated && (ds.Status != CheckStatusCanceled || !ds.RulesChanged || ds.SetVerdict == nil || ds.SetVerdict.Status != SetVerdictIncomplete):
			t.Errorf("%s: the DSCP rules changed while the run was finishing, so its covered verdict must not be published: status %q, rules changed %t, verdict %+v", tc.name, ds.Status, ds.RulesChanged, ds.SetVerdict)
		case !invalidated && (ds.Status != CheckStatusComplete || ds.RulesChanged || ds.SetVerdict == nil || ds.SetVerdict.Status != SetVerdictCovered):
			t.Errorf("%s: publishes its verdict: status %q, rules changed %t, verdict %+v", tc.name, ds.Status, ds.RulesChanged, ds.SetVerdict)
		}

		ds.ctxCancel()
		deadline := time.Now().Add(2 * time.Second)
		for {
			if _, ok := GetCheckSuite(ds.Id); !ok {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the finished run was never released")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestALateLossSignalLeavesAFinishedRunAlone(t *testing.T) {
	suite := newTestSuite(t)
	suite.Status = CheckStatusComplete
	RegisterSuite(suite)

	stopForLostRules(suite.Id, errors.New("a rewrite that began after the run had finished"))

	if suite.Status != CheckStatusComplete || suite.RulesChanged {
		t.Errorf("the run finished before its rules changed, so its result stands: status %q, rules changed %t", suite.Status, suite.RulesChanged)
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
