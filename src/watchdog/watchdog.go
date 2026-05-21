package watchdog

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/log"
)

type Watchdog struct {
	cfgPtr       *atomic.Pointer[config.Config]
	discoveryRT  *discovery.Runtime
	mu           sync.Mutex
	domainStates map[string]*DomainStatus
	stop         chan struct{}
	stopped      chan struct{}
	saveFunc     func(*config.Config) error
	healing      atomic.Bool
	healWG       sync.WaitGroup
}

func New(cfgPtr *atomic.Pointer[config.Config], discoveryRT *discovery.Runtime, saveFunc func(*config.Config) error) *Watchdog {
	return &Watchdog{
		cfgPtr:       cfgPtr,
		discoveryRT:  discoveryRT,
		domainStates: make(map[string]*DomainStatus),
		saveFunc:     saveFunc,
	}
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
				Status:   StatusHealthy,
				Interval: cfg.System.Checker.Watchdog.IntervalSec,
			}
		}
		domain := ExtractDomain(d)
		copy.DisplayDomain = domain
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
	}
}

func (w *Watchdog) ForceCheck(domain string) {
	w.mu.Lock()
	st, ok := w.domainStates[domain]
	if !ok {
		st = &DomainStatus{
			Domain: domain,
			Status: StatusHealthy,
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
	wdCfg := cfg.System.Checker.Watchdog
	if !wdCfg.Enabled || len(wdCfg.Domains) == 0 {
		return
	}

	now := time.Now()
	mark := cfg.Queue.Mark
	timeout := time.Duration(wdCfg.TimeoutSec) * time.Second

	w.mu.Lock()
	w.syncDomainStates(wdCfg)

	var domainsToCheck []string
	for _, domain := range wdCfg.Domains {
		st := w.domainStates[domain]
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

	results := checkAllConcurrently(domainsToCheck, mark, timeout)

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

		st.ConsecutiveFailures++
		st.LastFailure = now
		st.Status = StatusDegraded
		st.Interval = wdCfg.FailureInterval
		st.LastError = result.Error
		log.Warnf("[WATCHDOG] %s: check FAILED (%s) [%d/%d]", domain, result.Error, st.ConsecutiveFailures, wdCfg.MaxRetries)

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

	if w.discoveryRT.IsActive() {
		log.Infof("[WATCHDOG] deferring healing — user discovery active")
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

	suite, err := w.discoveryRT.StartSuite(cfg, domains, discovery.StartSuiteOptions{
		SkipDNS:         true,
		ValidationTries: 1,
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

	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()
	for {
		select {
		case <-w.stop:
			log.Infof("[WATCHDOG] shutting down, canceling active discovery")
			discovery.CancelCheckSuite(suite.Id)
			w.discoveryRT.Stop(cfg, suite.Id)
			return
		case <-pollTicker.C:
		}

		currentCfg := w.cfgPtr.Load()
		if !currentCfg.System.Checker.Watchdog.Enabled {
			log.Infof("[WATCHDOG] disabled during healing, canceling discovery")
			discovery.CancelCheckSuite(suite.Id)
			w.discoveryRT.Stop(currentCfg, suite.Id)
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

		cs, ok := discovery.GetCheckSuite(suite.Id)
		if !ok {
			break
		}
		if cs.Status == discovery.CheckStatusComplete || cs.Status == discovery.CheckStatusFailed || cs.Status == discovery.CheckStatusCanceled {
			break
		}
		if cs.SuccessfulChecks >= len(domains) {
			log.Infof("[WATCHDOG] working strategies found for all domains, canceling discovery early")
			discovery.CancelCheckSuite(suite.Id)
			time.Sleep(1 * time.Second)
			break
		}
	}

	cs, ok := discovery.GetCheckSuite(suite.Id)
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

	freshCfg := w.cfgPtr.Load().Clone()
	applyErrors := applyBatchResults(freshCfg, domains, cs, w.saveFunc)

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, domain := range domains {
		st, ok := w.domainStates[domain]
		if !ok {
			continue
		}
		if err, failed := applyErrors[domain]; failed && err != nil {
			log.Warnf("[WATCHDOG] %s: %v, cooldown %ds", domain, err, wdCfg.Cooldown)
			st.Status = StatusDegraded
			st.ConsecutiveFailures = 0
			st.CooldownUntil = time.Now().Add(time.Duration(wdCfg.Cooldown) * time.Second)
			continue
		}

		dr := cs.DomainDiscoveryResults[ExtractDomain(domain)]
		if dr != nil && dr.BestSuccess {
			log.Infof("[WATCHDOG] %s: healed (%s, %.0f KB/s)", domain, dr.BestPreset, dr.BestSpeed/1024)
		}
		st.Status = StatusHealthy
		st.ConsecutiveFailures = 0
		st.Interval = wdCfg.IntervalSec
		st.LastHeal = time.Now()
		st.LastError = ""
		st.CooldownUntil = time.Now().Add(time.Duration(wdCfg.Cooldown) * time.Second)
	}
}

func (w *Watchdog) syncDomainStates(wdCfg config.WatchdogConfig) {
	active := make(map[string]bool, len(wdCfg.Domains))
	for _, d := range wdCfg.Domains {
		active[d] = true
		if _, ok := w.domainStates[d]; !ok {
			w.domainStates[d] = &DomainStatus{
				Domain:   d,
				Status:   StatusHealthy,
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
