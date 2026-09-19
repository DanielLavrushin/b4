package tables

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

var errTestClear = errors.New("clear failed")

func newLockTestMonitor(t *testing.T) (*Monitor, *atomic.Pointer[config.Config]) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = false
	cfg.Queue.IPv6Enabled = false
	var ptr atomic.Pointer[config.Config]
	ptr.Store(&cfg)
	m := &Monitor{
		cfgPtr:       &ptr,
		stop:         make(chan struct{}),
		backend:      backendIPTables,
		kick:         make(chan struct{}, 1),
		ifaceState:   make(map[string]ifaceSnapshot),
		egressIPHere: make(map[string]bool),
	}
	return m, &ptr
}

func TestMonitorCheckWaitsForARefreshInFlight(t *testing.T) {
	m, _ := newLockTestMonitor(t)

	rulesMu.Lock()
	done := make(chan struct{})
	go func() {
		m.ensureRules(false)
		close(done)
	}()

	select {
	case <-done:
		rulesMu.Unlock()
		t.Fatalf("monitor check ran while a refresh held the rules lock")
	case <-time.After(100 * time.Millisecond):
	}

	rulesMu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("monitor check never ran after the refresh released the rules lock")
	}
}

func TestMonitorCheckUsesTheConfigTheRefreshApplied(t *testing.T) {
	m, ptr := newLockTestMonitor(t)

	rulesMu.Lock()
	got := make(chan *config.Config, 1)
	go func() {
		cfg, _ := m.ensureRules(false)
		got <- cfg
	}()
	time.Sleep(50 * time.Millisecond)

	applied := config.NewConfig()
	applied.Queue.IPv4Enabled = false
	applied.Queue.IPv6Enabled = false
	ptr.Store(&applied)
	rulesMu.Unlock()

	select {
	case cfg := <-got:
		if cfg != &applied {
			t.Fatalf("monitor checked against the config snapshot taken before the refresh, want the one the refresh applied")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("monitor check never returned")
	}
}

type pausedPass struct {
	started chan struct{}
	release chan struct{}
}

func newPausedPass() *pausedPass {
	return &pausedPass{started: make(chan struct{}), release: make(chan struct{})}
}

func (p *pausedPass) run(*config.Config) error {
	close(p.started)
	<-p.release
	return nil
}

func TestRefreshRulesHoldsTheLockAcrossClearAndAdd(t *testing.T) {
	origAdd, origClear := addRulesFn, clearRulesFn
	clear, add := newPausedPass(), newPausedPass()
	clearRulesFn, addRulesFn = clear.run, add.run
	t.Cleanup(func() { addRulesFn, clearRulesFn = origAdd, origClear })

	cfg := config.NewConfig()
	refreshed := make(chan error, 1)
	go func() { refreshed <- RefreshRules(&cfg) }()
	<-clear.started

	m, _ := newLockTestMonitor(t)
	checked := make(chan struct{})
	go func() {
		m.ensureRules(false)
		close(checked)
	}()

	select {
	case <-checked:
		t.Fatalf("monitor check ran while the refresh was still clearing")
	case <-time.After(100 * time.Millisecond):
	}

	close(clear.release)
	<-add.started
	select {
	case <-checked:
		t.Fatalf("monitor check ran between the refresh's clear and its add")
	case <-time.After(100 * time.Millisecond):
	}

	close(add.release)
	select {
	case err := <-refreshed:
		if err != nil {
			t.Fatalf("RefreshRules: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RefreshRules never finished")
	}
	select {
	case <-checked:
	case <-time.After(2 * time.Second):
		t.Fatalf("monitor check never ran after the refresh finished")
	}
}

func TestRefreshRulesStopsAtAFailedClear(t *testing.T) {
	origAdd, origClear := addRulesFn, clearRulesFn
	added := false
	clearRulesFn = func(*config.Config) error { return errTestClear }
	addRulesFn = func(*config.Config) error { added = true; return nil }
	t.Cleanup(func() { addRulesFn, clearRulesFn = origAdd, origClear })

	cfg := config.NewConfig()
	if err := RefreshRules(&cfg); err != errTestClear {
		t.Fatalf("RefreshRules error = %v, want the clear error", err)
	}
	if added {
		t.Fatalf("RefreshRules added rules after the clear failed")
	}
	if !rulesMu.TryLock() {
		t.Fatalf("RefreshRules left the rules lock held")
	}
	rulesMu.Unlock()
}

func TestRoutingIsSkippedWhenConfigMovedDuringTheCheck(t *testing.T) {
	m, ptr := newLockTestMonitor(t)
	stale := ptr.Load()
	m.ifaceState = map[string]ifaceSnapshot{"b4test0": {v4: "203.0.113.1"}}

	moved := config.NewConfig()
	moved.Queue.IPv4Enabled = false
	moved.Queue.IPv6Enabled = false
	ptr.Store(&moved)

	if m.reconcileRouting(stale, false) {
		t.Fatalf("routing phase ran with the stale config")
	}
	if _, tracked := m.ifaceState["b4test0"]; !tracked {
		t.Fatalf("routing state was rewritten from a config the refresh had already replaced")
	}
	if !m.reconcileRouting(stale, true) {
		t.Fatalf("the restore result was lost when routing was skipped")
	}
}
