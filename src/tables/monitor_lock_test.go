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

func routedTestConfig(iface string) *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = false
	cfg.Queue.IPv6Enabled = false
	set := config.NewSetConfig()
	set.Id = "routed"
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.EgressInterface = iface
	cfg.Sets = []*config.SetConfig{&set}
	return &cfg
}

func stubSyncedConfig(t *testing.T, cfg *config.Config) {
	t.Helper()
	routeMu.Lock()
	prev := routeSyncedCfg
	routeSyncedCfg = cfg
	routeMu.Unlock()
	t.Cleanup(func() {
		routeMu.Lock()
		routeSyncedCfg = prev
		routeMu.Unlock()
	})
}

func TestRoutingPhaseWaitsForASyncInFlight(t *testing.T) {
	m, _ := newLockTestMonitor(t)

	routePhaseMu.Lock()
	done := make(chan bool, 1)
	go func() { done <- m.reconcileRouting(false) }()

	select {
	case <-done:
		routePhaseMu.Unlock()
		t.Fatalf("routing phase ran while a routing sync held the lock")
	case <-time.After(100 * time.Millisecond):
	}

	routePhaseMu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("routing phase never ran after the sync released the lock")
	}
}

func TestRoutingPhaseFollowsTheLastCompletedSync(t *testing.T) {
	m, ptr := newLockTestMonitor(t)
	unsynced := ptr.Load()
	published := routedTestConfig("b4test0")
	ptr.Store(published)
	stubSyncedConfig(t, unsynced)

	m.reconcileRouting(false)
	if _, tracked := m.ifaceState["b4test0"]; tracked {
		t.Fatalf("routing phase reconciled against a published config the routing sync had not applied yet")
	}

	stubSyncedConfig(t, published)
	m.reconcileRouting(false)
	if _, tracked := m.ifaceState["b4test0"]; !tracked {
		t.Fatalf("routing phase ignored the config the last sync applied")
	}
}

func TestMonitorLeavesAPendingApplyToTheRefresh(t *testing.T) {
	origRun, origAdd, origApplied := run, addRulesFn, rulesAppliedCfg
	hasBinaryCache.Store(backendIPTables, true)
	t.Cleanup(func() {
		run, addRulesFn = origRun, origAdd
		rulesMu.Lock()
		rulesAppliedCfg = origApplied
		rulesMu.Unlock()
		hasBinaryCache.Delete(backendIPTables)
	})

	present := "-A B4_PREROUTING -p udp --sport 53 -j NFQUEUE -p tcp --dport 53 -j NFQUEUE -m mark 0x8000 -j B4"
	calls := 0
	run = func(args ...string) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("chain missing")
		}
		return present, nil
	}
	restores := 0
	addRulesFn = func(*config.Config) error {
		restores++
		return nil
	}

	m, ptr := newLockTestMonitor(t)
	applied := config.NewConfig()
	applied.Queue.IPv4Enabled = true
	published := config.NewConfig()
	published.Queue.IPv4Enabled = true
	published.Queue.TCPConnBytesLimit = applied.Queue.TCPConnBytesLimit + 2
	ptr.Store(&published)
	rulesMu.Lock()
	rulesAppliedCfg = &applied
	rulesMu.Unlock()

	if _, acted := m.ensureRules(false); !acted {
		t.Fatalf("a deferred check must not read as all present")
	}
	if restores != 0 {
		t.Fatalf("monitor rebuilt the firewall under a pending apply")
	}

	calls = 0
	if _, acted := m.ensureRules(false); !acted {
		t.Fatalf("monitor kept waiting for an apply that never came")
	}
	if restores != 1 {
		t.Fatalf("monitor did not rebuild the firewall on the second tick, restores=%d", restores)
	}

	calls = 0
	devices := config.NewConfig()
	devices.Queue.IPv4Enabled = true
	devices.Queue.Devices.Enabled = true
	ptr.Store(&devices)
	rulesMu.Lock()
	rulesAppliedCfg = &applied
	rulesMu.Unlock()
	m.ensureRules(false)
	if restores != 2 {
		t.Fatalf("a change that no refresh will apply was deferred instead of restored, restores=%d", restores)
	}
}
