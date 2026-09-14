package discovery

import (
	"testing"
	"time"

	"github.com/daniellavrushin/b4/leaktest"
	"go.uber.org/goleak"
)

func finishFixture(t *testing.T, phase DiscoveryPhase) *DiscoverySuite {
	t.Helper()
	suite := NewCheckSuite([]DomainInput{{Domain: "example.com", CheckURL: "https://example.com/"}})
	suite.Status = CheckStatusRunning
	suite.CurrentPhase = phase
	RegisterSuite(suite)
	ds := &DiscoverySuite{CheckSuite: suite}
	ds.initCancelContext()
	t.Cleanup(func() {
		ds.ctxCancel()
		suitesMu.Lock()
		delete(activeSuites, suite.Id)
		suitesMu.Unlock()
	})
	return ds
}

func ctxDone(ds *DiscoverySuite) bool {
	select {
	case <-ds.ctx.Done():
		return true
	case <-time.After(200 * time.Millisecond):
		return false
	}
}

func TestFinishStopsTheSearchButNotTheRun(t *testing.T) {
	defer goleak.VerifyNone(t, leaktest.Options()...)
	ds := finishFixture(t, PhaseStrategy)

	if err := FinishCheckSuite(ds.Id); err != nil {
		t.Fatalf("finish: %v", err)
	}

	if !ds.interrupted() || !ds.finishing() {
		t.Fatal("a finished suite must read as interrupted so the search loops unwind")
	}
	if ds.canceled() {
		t.Fatal("finish must not cancel the suite, confirmation still has to run")
	}
	if ds.Status != CheckStatusRunning {
		t.Fatalf("Status = %q, want running until confirmation ends", ds.Status)
	}
	if !ds.StoppedEarly || ds.StoppedPhase != PhaseStrategy {
		t.Fatalf("StoppedEarly = %v, StoppedPhase = %q, want true and %q", ds.StoppedEarly, ds.StoppedPhase, PhaseStrategy)
	}
	if !ctxDone(ds) {
		t.Fatal("finish must abort the in-flight probe")
	}

	if err := FinishCheckSuite(ds.Id); err != nil {
		t.Fatalf("second finish: %v", err)
	}
}

func TestResetFetchContextSurvivesFinishButNotCancel(t *testing.T) {
	defer goleak.VerifyNone(t, leaktest.Options()...)
	ds := finishFixture(t, PhaseStrategy)

	if err := FinishCheckSuite(ds.Id); err != nil {
		t.Fatalf("finish: %v", err)
	}
	ds.resetFetchContext()
	if ctxDone(ds) {
		t.Fatal("the confirmation context must not be canceled by the earlier finish request")
	}

	if err := CancelCheckSuite(ds.Id); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if !ctxDone(ds) {
		t.Fatal("a hard stop must still abort confirmation fetches")
	}
	if !ds.canceled() {
		t.Fatal("cancel after finish must read as canceled")
	}
}

func TestFinishDuringConfirmationChangesNothing(t *testing.T) {
	defer goleak.VerifyNone(t, leaktest.Options()...)
	ds := finishFixture(t, PhaseConfirm)

	if err := FinishCheckSuite(ds.Id); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if ds.finishing() || ds.StoppedEarly {
		t.Fatal("confirmation is the last phase, finishing early there must be a no-op")
	}
	if ctxDone(ds) {
		t.Fatal("confirmation fetches must not be aborted by a finish request")
	}
	ds.ctxCancel()
}

func TestFinishSuiteWithoutAChannelDoesNotPanic(t *testing.T) {
	suite := &CheckSuite{Id: "hand-built-finish", Status: CheckStatusRunning}
	RegisterSuite(suite)
	t.Cleanup(func() {
		suitesMu.Lock()
		delete(activeSuites, suite.Id)
		suitesMu.Unlock()
	})

	if err := FinishCheckSuite(suite.Id); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if suite.StoppedEarly {
		t.Fatal("a suite without a finish channel cannot be finished early")
	}
	ds := &DiscoverySuite{CheckSuite: suite}
	if ds.interrupted() {
		t.Fatal("nil channels must never read as interrupted")
	}
}
