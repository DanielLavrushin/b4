package tables

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
)

var (
	dscpSyncTick     = 5 * time.Minute
	dscpSyncWanted   atomic.Pointer[config.Config]
	dscpSyncKick     = make(chan struct{}, 1)
	dscpSyncClosed   atomic.Bool
	dscpSyncMu       sync.Mutex
	dscpSyncStop     chan struct{}
	dscpSyncDone     chan struct{}
	dscpSyncPassFn   = dscpSyncPass
	dscpPreResolveFn = dscpPreResolve
)

func SyncDSCP(cfg *config.Config) {
	if cfg == nil {
		return
	}
	dscpSyncWanted.Store(cfg)
	select {
	case dscpSyncKick <- struct{}{}:
	default:
	}
}

func StartDSCPSync() {
	dscpSyncMu.Lock()
	defer dscpSyncMu.Unlock()
	if dscpSyncStop != nil {
		return
	}
	rulesMu.Lock()
	dscpSyncClosed.Store(false)
	rulesMu.Unlock()
	dscpLearnStart()
	dscpPreResolveStart()
	dscpSyncStop, dscpSyncDone = make(chan struct{}), make(chan struct{})
	go dscpSyncLoop(dscpSyncStop, dscpSyncDone, dscpSyncTick)
}

func StopDSCPSync() {
	dscpSyncMu.Lock()
	defer dscpSyncMu.Unlock()
	if dscpSyncStop == nil {
		return
	}
	close(dscpSyncStop)
	<-dscpSyncDone
	dscpPreResolveStop()
	dscpLearnStop()
	dscpSyncStop, dscpSyncDone = nil, nil
}

func dscpSyncLoop(stop <-chan struct{}, done chan<- struct{}, every time.Duration) {
	defer close(done)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-dscpSyncKick:
			dscpSyncPassFn(dscpSyncWanted.Load(), false, stop)
		case <-ticker.C:
			dscpSyncPassFn(dscpSyncWanted.Load(), true, stop)
		}
	}
}

func dscpSyncPass(cfg *config.Config, periodic bool, stop <-chan struct{}) {
	rulesMu.Lock()
	plan := dscpSyncLocked(cfg, periodic, stop)
	rulesMu.Unlock()
	if plan != nil {
		dscpPreResolveFn(plan, periodic)
	}
}

func dscpSyncLocked(cfg *config.Config, periodic bool, stop <-chan struct{}) *dscpPlan {
	select {
	case <-stop:
		return nil
	default:
	}
	applied := dscpApplied.Load()
	if cfg == nil && applied != nil {
		cfg = applied.cfg
	}
	if dscpSyncClosed.Load() || cfg == nil || cfg.System.Tables.SkipSetup {
		return nil
	}
	plan := dscpPlanFor(cfg)
	planned := !plan.empty() || (applied != nil && !applied.plan.empty())
	if !planned && !applied.perSet() {
		return nil
	}
	IPTablesLockBudgetReset()
	if planned && !periodic && (applied == nil || applied.pending || !applied.plan.equal(plan)) {
		if !plan.empty() {
			loadKernelModules()
		}
		applyDSCPLogged(cfg, dscpSyncBackend(cfg, applied))
	}
	retryPendingDSCPSets(dscpApplied.Load())
	if applied = dscpApplied.Load(); applied == nil || applied.plan.empty() {
		return nil
	}
	return applied.plan
}

func dscpSyncBackend(cfg *config.Config, applied *dscpState) string {
	if applied != nil {
		return applied.backend
	}
	if cfg.Queue.Mode != "tun" && rulesAppliedBackend != "" {
		return rulesAppliedBackend
	}
	return detectFirewallBackend(cfg)
}
