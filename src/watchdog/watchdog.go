package watchdog

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	verifyRetryDelay  = 3 * time.Second
	discoveryIdleWait = 30 * time.Second
	healPollInterval  = 2 * time.Second
	setIdleWait       = 60 * time.Second
)

type UpdateFunc func(mutate func(current *config.Config) (*config.Config, error)) error

func NewUpdateFunc(load func() *config.Config, commit func(previous, next *config.Config) error, refreshFirewall func()) UpdateFunc {
	return func(mutate func(current *config.Config) (*config.Config, error)) error {
		refresh, err := func() (bool, error) {
			unlock := config.LockWrites()
			defer unlock()
			previous := load()
			next, err := mutate(previous)
			if err != nil {
				return false, err
			}
			if err := commit(previous, next); err != nil {
				return false, err
			}
			return config.FirewallRefreshNeeded(previous, next), nil
		}()
		if err != nil {
			return err
		}
		if refresh && refreshFirewall != nil {
			refreshFirewall()
		}
		return nil
	}
}

var (
	errNothingToWrite = errors.New("nothing to write")
	errConfigChanged  = errors.New("the configuration changed since the heal was written")
)

type Watchdog struct {
	cfgPtr           *atomic.Pointer[config.Config]
	disc             discoveryDriver
	engine           EngineView
	mu               sync.Mutex
	domainStates     map[string]*DomainStatus
	setStates        map[string]*setState
	healQueue        []string
	legacySkipLogged map[string]bool
	stop             chan struct{}
	stopped          chan struct{}
	update           UpdateFunc
	healing          atomic.Bool
	healWG           sync.WaitGroup

	now         func() time.Time
	checkURL    func(ctx context.Context, rawURL string, ipv6Enabled bool, timeout time.Duration) URLCheck
	checkLegacy func(ctx context.Context, domains []string, mark uint, timeout time.Duration) map[string]CheckResult
	pollEvery   time.Duration
	verifyDelay time.Duration
	idleWait    time.Duration
}

func New(cfgPtr *atomic.Pointer[config.Config], discoveryRT *discovery.Runtime, update UpdateFunc) *Watchdog {
	return &Watchdog{
		cfgPtr:           cfgPtr,
		disc:             runtimeDriver{rt: discoveryRT},
		engine:           NewEngineView(nil, cfgPtr),
		domainStates:     make(map[string]*DomainStatus),
		setStates:        make(map[string]*setState),
		legacySkipLogged: make(map[string]bool),
		update:           update,
		now:              time.Now,
		checkURL:         checkURLStrict,
		checkLegacy:      checkAllConcurrently,
		pollEvery:        healPollInterval,
		verifyDelay:      verifyRetryDelay,
		idleWait:         setIdleWait,
	}
}

func (w *Watchdog) SetEngine(engine EngineView) {
	if engine != nil {
		w.engine = engine
	}
}

func (w *Watchdog) clock() time.Time {
	if w.now != nil {
		return w.now()
	}
	return time.Now()
}

func (w *Watchdog) stopContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	stop := w.stop
	if stop == nil {
		return ctx, cancel
	}
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (w *Watchdog) stopping() bool {
	select {
	case <-w.stop:
		return true
	default:
		return false
	}
}

func (w *Watchdog) legacyOwners(cfg *config.Config) map[string]*config.SetConfig {
	owners := make(map[string]*config.SetConfig, len(cfg.System.Checker.Watchdog.Domains))
	if w.engine == nil {
		return owners
	}
	for _, d := range cfg.System.Checker.Watchdog.Domains {
		host := normalizeHost(ExtractDomain(d))
		if _, done := owners[host]; !done {
			owners[host] = w.engine.Owner(host)
		}
	}
	return owners
}

func (w *Watchdog) Start() {
	w.stop = make(chan struct{})
	w.stopped = make(chan struct{})
	log.Infof("[WATCHDOG] starting watchdog service")
	go w.run()
}

func (w *Watchdog) Stop() {
	close(w.stop)
	<-w.stopped
	w.healWG.Wait()
	log.Infof("[WATCHDOG] watchdog service stopped")
}

func (w *Watchdog) GetState() WatchdogState {
	cfg := w.cfgPtr.Load()
	owners := w.legacyOwners(cfg)
	watchers := legacyWatchers(cfg, owners)

	w.mu.Lock()
	defer w.mu.Unlock()

	domains := make([]*DomainStatus, 0)
	for _, d := range cfg.System.Checker.Watchdog.Domains {
		var copy DomainStatus
		if existing, ok := w.domainStates[d]; ok {
			copy = *existing
		} else {
			copy = DomainStatus{
				Domain:   d,
				Status:   StatusQueued,
				Interval: cfg.System.Checker.Watchdog.IntervalSec,
			}
		}
		domain := ExtractDomain(d)
		copy.DisplayDomain = domain
		copy.OwnerSetId, copy.OwnerSetName = "", ""
		if owner := owners[normalizeHost(domain)]; owner != nil {
			copy.OwnerSetId, copy.OwnerSetName = owner.Id, owner.Name
		}
		copy.WatchedBySetId, copy.WatchedBySetName = "", ""
		if ref, ok := watchers[d]; ok {
			copy.WatchedBySetId, copy.WatchedBySetName = ref.id, ref.name
		}
		for _, set := range cfg.Sets {
			if !set.Enabled {
				continue
			}
			if setContainsAnyDomain(set, []string{domain}) {
				copy.MatchedSet = set.Name
				copy.MatchedSetId = set.Id
				break
			}
		}
		domains = append(domains, &copy)
	}
	return WatchdogState{
		Enabled: cfg.System.Checker.Watchdog.Enabled,
		Domains: domains,
		Sets:    w.setSnapshotsLocked(cfg),
	}
}

func (w *Watchdog) ForceCheck(domain string) {
	w.mu.Lock()
	st, ok := w.domainStates[domain]
	if !ok {
		st = &DomainStatus{
			Domain: domain,
			Status: StatusQueued,
		}
		w.domainStates[domain] = st
	}
	st.LastCheck = time.Time{}
	st.CooldownUntil = time.Time{}
	w.mu.Unlock()
}

func (w *Watchdog) run() {
	defer close(w.stopped)

	select {
	case <-w.stop:
		return
	case <-time.After(30 * time.Second):
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.tick()
		}
	}
}

func (w *Watchdog) tick() {
	cfg := w.cfgPtr.Load()
	enabled := cfg.System.Checker.Watchdog.Enabled

	w.mu.Lock()
	w.syncSetStatesLocked(cfg)
	if !enabled {
		w.parkHealQueueLocked()
	}
	w.mu.Unlock()

	if !enabled {
		return
	}

	w.tickSets(cfg)
	w.tickLegacy(cfg, legacyWatchers(cfg, w.legacyOwners(cfg)))
	w.pumpHealQueue()
}

func (w *Watchdog) tickLegacy(cfg *config.Config, watchers map[string]setRef) {
	wdCfg := cfg.System.Checker.Watchdog
	if len(wdCfg.Domains) == 0 {
		return
	}

	now := time.Now()
	mark := markThroughEngine
	timeout := time.Duration(wdCfg.TimeoutSec) * time.Second

	w.mu.Lock()
	w.syncDomainStates(wdCfg)

	var domainsToCheck []string
	for _, domain := range wdCfg.Domains {
		st := w.domainStates[domain]
		if w.skipLegacyDomainLocked(domain, st, watchers) {
			continue
		}
		if st.Status == StatusEscalating {
			continue
		}
		if !st.LastCheck.IsZero() && now.Before(st.LastCheck.Add(time.Duration(st.Interval)*time.Second)) {
			continue
		}
		if !st.CooldownUntil.IsZero() && now.Before(st.CooldownUntil) {
			continue
		}
		domainsToCheck = append(domainsToCheck, domain)
	}
	w.mu.Unlock()

	if len(domainsToCheck) == 0 {
		return
	}

	check := w.checkLegacy
	if check == nil {
		check = checkAllConcurrently
	}
	ctx, cancel := w.stopContext()
	results := check(ctx, domainsToCheck, mark, timeout)
	cancel()
	if w.stopping() {
		return
	}

	w.mu.Lock()
	var needsHealing []string
	for domain, result := range results {
		st, ok := w.domainStates[domain]
		if !ok {
			continue
		}
		st.LastCheck = now

		if result.OK {
			if st.Status == StatusDegraded {
				log.Infof("[WATCHDOG] %s: recovered (%.0f KB/s)", domain, result.Speed/1024)
			}
			st.ConsecutiveFailures = 0
			st.Status = StatusHealthy
			st.Interval = wdCfg.IntervalSec
			st.LastError = ""
			st.LastSpeed = result.Speed
			continue
		}

		st.LastFailure = now
		st.Status = StatusDegraded
		st.Interval = wdCfg.FailureInterval
		st.LastError = result.Error

		if result.Verdict == netprobe.DomainMTLS {
			st.CooldownUntil = now.Add(time.Duration(wdCfg.Cooldown) * time.Second)
			log.Warnf("[WATCHDOG] %s: server requires client certificate (mTLS), no DPI bypass applies - skipping heal, cooldown %ds", domain, wdCfg.Cooldown)
			continue
		}

		if result.Unusable {
			st.CooldownUntil = now.Add(time.Duration(wdCfg.Cooldown) * time.Second)
			log.Warnf("[WATCHDOG] %s: %s - skipping heal, cooldown %ds", domain, result.Error, wdCfg.Cooldown)
			continue
		}

		st.ConsecutiveFailures++
		log.Warnf("[WATCHDOG] %s: check FAILED [%s] (%s) [%d/%d]", domain, result.Verdict, result.Error, st.ConsecutiveFailures, wdCfg.MaxRetries)

		if st.ConsecutiveFailures >= wdCfg.MaxRetries {
			needsHealing = append(needsHealing, domain)
		}
	}
	w.mu.Unlock()

	if len(needsHealing) > 0 && w.healing.CompareAndSwap(false, true) {
		w.healWG.Add(1)
		go func(domains []string) {
			defer w.healWG.Done()
			defer w.healing.Store(false)
			log.Infof("[WATCHDOG] starting heal for %d domain(s): %v", len(domains), domains)
			w.healBatch(domains)
		}(needsHealing)
	}
}

func (w *Watchdog) healBatch(domains []string) {
	cfg := w.cfgPtr.Load()
	wdCfg := cfg.System.Checker.Watchdog

	if w.disc.IsActive() {
		log.Infof("[WATCHDOG] deferring healing - user discovery active")
		return
	}

	w.mu.Lock()
	for _, domain := range domains {
		if st, ok := w.domainStates[domain]; ok {
			st.Status = StatusEscalating
		}
	}
	w.mu.Unlock()

	log.Infof("[WATCHDOG] starting discovery for %d domains: %v", len(domains), domains)

	tries := wdCfg.HealValidationTries
	if tries < 1 {
		tries = 1
	}

	suiteID, err := w.disc.Start(cfg, discoveryInputs(domains), discovery.StartSuiteOptions{
		SkipDNS:         true,
		ValidationTries: tries,
		Source:          discovery.SourceWatchdog,
	})
	if err != nil {
		log.Warnf("[WATCHDOG] failed to start discovery: %v", err)
		w.mu.Lock()
		for _, domain := range domains {
			st, ok := w.domainStates[domain]
			if !ok {
				continue
			}
			st.Status = StatusDegraded
			st.ConsecutiveFailures = 0
			st.CooldownUntil = time.Now().Add(time.Duration(wdCfg.Cooldown) * time.Second)
		}
		w.mu.Unlock()
		return
	}

	finishRequested := false
	pollEvery := w.pollEvery
	if pollEvery <= 0 {
		pollEvery = healPollInterval
	}
	pollTicker := time.NewTicker(pollEvery)
	defer pollTicker.Stop()
	for {
		select {
		case <-w.stop:
			log.Infof("[WATCHDOG] shutting down, canceling active discovery")
			w.disc.Cancel(suiteID)
			return
		case <-pollTicker.C:
		}

		currentCfg := w.cfgPtr.Load()
		if !currentCfg.System.Checker.Watchdog.Enabled {
			log.Infof("[WATCHDOG] disabled during healing, canceling discovery")
			w.disc.Cancel(suiteID)
			w.mu.Lock()
			for _, domain := range domains {
				if st, ok := w.domainStates[domain]; ok {
					st.Status = StatusDegraded
					st.ConsecutiveFailures = 0
				}
			}
			w.mu.Unlock()
			return
		}

		snap, ok := w.disc.Snapshot(suiteID)
		if !ok {
			break
		}
		if suiteFinished(snap.Status) {
			break
		}
		if !finishRequested && tries > 1 && everyDomainSettled(snap, domains) {
			log.Infof("[WATCHDOG] every domain has a result, stopping the search and confirming what was found")
			w.disc.Finish(suiteID)
			finishRequested = true
		}
	}

	w.waitDiscoveryIdle(discoveryIdleWait)
	if w.stopping() {
		return
	}

	cs, ok := w.disc.Snapshot(suiteID)
	if !ok {
		log.Warnf("[WATCHDOG] discovery suite disappeared")
		w.mu.Lock()
		for _, domain := range domains {
			st, ok := w.domainStates[domain]
			if !ok {
				continue
			}
			st.Status = StatusDegraded
			st.ConsecutiveFailures = 0
			st.CooldownUntil = time.Now().Add(time.Duration(wdCfg.Cooldown) * time.Second)
		}
		w.mu.Unlock()
		return
	}

	applyErrors, undos := w.writeBatch(domains, cs)

	applied := make([]string, 0, len(domains))
	for _, domain := range domains {
		if err, failed := applyErrors[domain]; failed && err != nil {
			continue
		}
		applied = append(applied, domain)
	}

	verified, interrupted := w.verifyApplied(applied, wdCfg)
	if interrupted {
		log.Infof("[WATCHDOG] shutting down during verification, the healed configuration is kept and checked again on the next start")
		return
	}

	if len(applied) > 0 && len(verified) == 0 {
		w.rollbackBatch(undos)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, domain := range domains {
		st, ok := w.domainStates[domain]
		if !ok {
			continue
		}
		if err, failed := applyErrors[domain]; failed && err != nil {
			if errors.Is(err, ErrBaselineWorks) {
				log.Infof("[WATCHDOG] %s: reachable without a bypass, the set is left as it is", domain)
				st.Status = StatusHealthy
				st.ConsecutiveFailures = 0
				st.Interval = wdCfg.IntervalSec
				st.LastError = ""
				st.CooldownUntil = time.Now().Add(time.Duration(wdCfg.Cooldown) * time.Second)
				continue
			}
			log.Warnf("[WATCHDOG] %s: %v, cooldown %ds", domain, err, wdCfg.Cooldown)
			st.Status = StatusDegraded
			st.ConsecutiveFailures = 0
			st.CooldownUntil = time.Now().Add(time.Duration(wdCfg.Cooldown) * time.Second)
			continue
		}

		dr := cs.DomainDiscoveryResults[ExtractDomain(domain)]

		if res, ok := verified[domain]; ok {
			if dr != nil && dr.BestSuccess {
				log.Infof("[WATCHDOG] %s: healed with %s, verified at %.0f KB/s", domain, dr.BestPreset, res.Speed/1024)
			} else {
				log.Infof("[WATCHDOG] %s: healed, verified at %.0f KB/s", domain, res.Speed/1024)
			}
			st.Status = StatusHealthy
			st.ConsecutiveFailures = 0
			st.Interval = wdCfg.IntervalSec
			st.LastHeal = time.Now()
			st.LastError = ""
			st.CooldownUntil = time.Now().Add(time.Duration(wdCfg.Cooldown) * time.Second)
			continue
		}

		preset := "unknown"
		if dr != nil && dr.BestPreset != "" {
			preset = dr.BestPreset
		}
		log.Warnf("[WATCHDOG] %s: discovery reported %s working but it did not survive verification, not healed, cooldown %ds",
			domain, preset, wdCfg.Cooldown)
		st.Status = StatusDegraded
		st.ConsecutiveFailures = 0
		st.LastError = "applied strategy failed post-apply verification"
		st.CooldownUntil = time.Now().Add(time.Duration(wdCfg.Cooldown) * time.Second)
	}
}

type setUndo struct {
	id           string
	name         string
	created      bool
	saved        strategySections
	domains      []string
	added        []string
	postRevision string
}

func (w *Watchdog) writeBatch(domains []string, cs *discovery.CheckSuite) (map[string]error, []setUndo) {
	var applyErrors map[string]error
	var before, written *config.Config
	err := w.update(func(current *config.Config) (*config.Config, error) {
		before = current.Clone()
		written = nil
		applyErrors = applyBatchResults(current.Clone(), domains, cs, func(c *config.Config) error {
			written = c
			return nil
		})
		if written == nil {
			return nil, errNothingToWrite
		}
		return written, nil
	})
	if applyErrors == nil {
		applyErrors = make(map[string]error, len(domains))
	}
	switch {
	case errors.Is(err, errNothingToWrite):
		return applyErrors, nil
	case err != nil:
		for _, domain := range domains {
			if prior, failed := applyErrors[domain]; !failed || prior == nil {
				applyErrors[domain] = err
			}
		}
		return applyErrors, nil
	}
	return applyErrors, batchUndos(before, written)
}

func batchUndos(before, after *config.Config) []setUndo {
	var undos []setUndo
	for _, set := range after.Sets {
		if set == nil {
			continue
		}
		revision := SetRevision(set)
		prior := before.GetSetById(set.Id)
		switch {
		case prior == nil:
			undos = append(undos, setUndo{id: set.Id, name: set.Name, created: true, postRevision: revision})
		case SetRevision(prior) != revision:
			undo := setUndo{
				id:           set.Id,
				name:         set.Name,
				saved:        saveSections(prior),
				domains:      slices.Clone(prior.Targets.SNIDomains),
				postRevision: revision,
			}
			if len(set.Targets.SNIDomains) > len(prior.Targets.SNIDomains) {
				undo.added = slices.Clone(set.Targets.SNIDomains[len(prior.Targets.SNIDomains):])
			}
			undos = append(undos, undo)
		}
	}
	return undos
}

func (u setUndo) restoreInto(set *config.SetConfig) {
	u.saved.restoreInto(set)
	set.Targets.SNIDomains = slices.Clone(u.domains)
	for _, domain := range u.added {
		for i := len(set.Targets.DomainsToMatch) - 1; i >= 0; i-- {
			if set.Targets.DomainsToMatch[i] == domain {
				set.Targets.DomainsToMatch = slices.Delete(set.Targets.DomainsToMatch, i, i+1)
				break
			}
		}
	}
}

func (w *Watchdog) rollbackBatch(undos []setUndo) {
	if len(undos) == 0 {
		return
	}
	var restored, kept []string
	err := w.update(func(current *config.Config) (*config.Config, error) {
		restored, kept = nil, nil
		next := current.Clone()
		for _, undo := range undos {
			live := current.GetSetById(undo.id)
			if live == nil || SetRevision(live) != undo.postRevision {
				kept = append(kept, undo.name)
				continue
			}
			if undo.created {
				next.Sets = slices.DeleteFunc(next.Sets, func(s *config.SetConfig) bool { return s != nil && s.Id == undo.id })
			} else if target := next.GetSetById(undo.id); target != nil {
				undo.restoreInto(target)
			}
			restored = append(restored, undo.name)
		}
		if len(restored) == 0 {
			return nil, errConfigChanged
		}
		return next, nil
	})
	switch {
	case errors.Is(err, errConfigChanged):
		log.Warnf("[WATCHDOG] verification failed for all healed domains, but every set the heal wrote was changed after it, so they are left as they are")
		return
	case err != nil:
		log.Warnf("[WATCHDOG] failed to roll back config after failed verification: %v", err)
		return
	}
	for _, name := range kept {
		log.Warnf("[WATCHDOG] set %q was changed after the heal was written, it is left as it is", name)
	}
	log.Warnf("[WATCHDOG] verification failed for all healed domains, rolled back %s", strings.Join(restored, ", "))
}

func suiteFinished(status discovery.CheckStatus) bool {
	return status == discovery.CheckStatusComplete || status == discovery.CheckStatusFailed || status == discovery.CheckStatusCanceled
}

func everyDomainSettled(cs *discovery.CheckSuite, domains []string) bool {
	for _, domain := range domains {
		dr := cs.DomainDiscoveryResults[ExtractDomain(domain)]
		if dr == nil {
			return false
		}
		if dr.BestSuccess || dr.BaselineWorks {
			continue
		}
		if r := dr.Results["no-bypass"]; r != nil && r.Status == discovery.CheckStatusComplete {
			continue
		}
		return false
	}
	return true
}

func (w *Watchdog) waitDiscoveryIdle(limit time.Duration) {
	deadline := time.Now().Add(limit)
	for w.disc.IsActive() && time.Now().Before(deadline) {
		select {
		case <-w.stop:
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func discoveryInputs(domains []string) []string {
	inputs := make([]string, 0, len(domains))
	for _, domain := range domains {
		d := strings.TrimSpace(domain)
		if !strings.Contains(d, "://") && strings.ContainsAny(d, "/:?") {
			d = "https://" + d
		}
		inputs = append(inputs, d)
	}
	return inputs
}

// verifyApplied re-checks each domain through the live engine after the healed
// config has been applied. Discovery runs on its own queues with its own probe
// client, so a preset succeeding there is not evidence that normal traffic works.
func (w *Watchdog) verifyApplied(domains []string, wdCfg config.WatchdogConfig) (map[string]CheckResult, bool) {
	verified := make(map[string]CheckResult, len(domains))
	if len(domains) == 0 {
		return verified, false
	}

	tries := wdCfg.VerifyTries
	if tries < 1 {
		tries = 1
	}
	mark := markThroughEngine
	timeout := time.Duration(wdCfg.TimeoutSec) * time.Second

	check := w.checkLegacy
	if check == nil {
		check = checkAllConcurrently
	}
	ctx, cancel := w.stopContext()
	defer cancel()

	pending := append([]string(nil), domains...)
	for i := 0; i < tries && len(pending) > 0; i++ {
		if i > 0 && !w.pause(w.verifyDelay) {
			return verified, true
		}

		results := check(ctx, pending, mark, timeout)
		if w.stopping() {
			return verified, true
		}
		var stillFailing []string
		for _, domain := range pending {
			res := results[domain]
			if res.OK {
				verified[domain] = res
				continue
			}
			log.Warnf("[WATCHDOG] %s: post-apply verification try %d/%d failed (%s)", domain, i+1, tries, res.Error)
			stillFailing = append(stillFailing, domain)
		}
		pending = stillFailing
	}

	return verified, false
}

func (w *Watchdog) syncDomainStates(wdCfg config.WatchdogConfig) {
	active := make(map[string]bool, len(wdCfg.Domains))
	for _, d := range wdCfg.Domains {
		active[d] = true
		if _, ok := w.domainStates[d]; !ok {
			w.domainStates[d] = &DomainStatus{
				Domain:   d,
				Status:   StatusQueued,
				Interval: wdCfg.IntervalSec,
			}
		}
	}
	for d := range w.domainStates {
		if !active[d] {
			delete(w.domainStates, d)
		}
	}
}
