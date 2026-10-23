package discovery

import (
	"errors"
	"testing"
)

func registerRunningSuite(t *testing.T) *CheckSuite {
	t.Helper()
	suite := NewCheckSuite([]DomainInput{{Domain: "example.com", CheckURL: "https://example.com/"}})
	suite.Status = CheckStatusRunning
	RegisterSuite(suite)
	t.Cleanup(func() {
		suitesMu.Lock()
		delete(activeSuites, suite.Id)
		suitesMu.Unlock()
	})
	return suite
}

func TestLostFirewallRulesStopTheRunTheyBelongTo(t *testing.T) {
	suite := registerRunningSuite(t)
	other := registerRunningSuite(t)
	rt := NewRuntime()
	rt.state = &runtimeState{pool: &fakeStopPool{}, clearRules: func() {}, activeSuiteID: suite.Id}

	rt.rulesLost(errors.New("iptables rejected the rule that writes DSCP 25 into Discovery's probes"))

	if suite.Status != CheckStatusCanceled || !suite.canceledForTest() {
		t.Errorf("results tested without the set's DSCP value come from another network path, so the run must stop; status %q", suite.Status)
	}
	if other.Status != CheckStatusRunning {
		t.Errorf("only the run whose rules were lost stops, another suite went %q", other.Status)
	}

	idle := NewRuntime()
	idle.rulesLost(errors.New("late signal"))
	stopping := NewRuntime()
	stopping.state = &runtimeState{pool: &fakeStopPool{}, clearRules: func() {}, activeSuiteID: other.Id, stopping: true}
	stopping.rulesLost(errors.New("signal while the run is already stopping"))
	if other.Status != CheckStatusRunning {
		t.Errorf("a signal with no run, or for a run already stopping, must not touch other suites, got %q", other.Status)
	}
}

func (s *CheckSuite) canceledForTest() bool {
	select {
	case <-s.cancel:
		return true
	default:
		return false
	}
}
