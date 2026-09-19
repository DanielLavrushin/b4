package tables

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

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
		m.ensureRules()
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
		cfg, _ := m.ensureRules()
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

func TestRefreshRulesHoldsTheLockAcrossClearAndAdd(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.Tables.SkipSetup = true

	rulesMu.Lock()
	done := make(chan error, 1)
	go func() {
		done <- RefreshRules(&cfg)
	}()

	select {
	case <-done:
		rulesMu.Unlock()
		t.Fatalf("RefreshRules ran while another pass held the rules lock")
	case <-time.After(100 * time.Millisecond):
	}

	rulesMu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RefreshRules: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RefreshRules never ran after the lock was released")
	}

	if !rulesMu.TryLock() {
		t.Fatalf("RefreshRules left the rules lock held")
	}
	rulesMu.Unlock()
}
