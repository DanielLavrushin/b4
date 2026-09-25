package watchdog

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/log"
)

const (
	healFinishAfter = 15 * time.Minute
	healCancelAfter = 20 * time.Minute
)

var (
	errSetEdited      = errors.New("the set was edited")
	errHealNotWatched = errors.New("the set is no longer watched as it was")
)

type setRunOutcome int

const (
	setRunDone setRunOutcome = iota
	setRunStopped
	setRunAborted
	setRunOverBudget
)

type strategySections struct {
	TCP           config.TCPConfig
	UDP           config.UDPConfig
	Fragmentation config.FragmentationConfig
	Faking        config.FakingConfig
	DNS           config.DNSConfig
}

func saveSections(set *config.SetConfig) strategySections {
	saved := strategySections{
		TCP:           set.TCP,
		UDP:           set.UDP,
		Fragmentation: set.Fragmentation,
		Faking:        set.Faking,
		DNS:           set.DNS,
	}
	saved.TCP.Win.Values = slices.Clone(set.TCP.Win.Values)
	saved.UDP.FakePayloadData = slices.Clone(set.UDP.FakePayloadData)
	saved.Fragmentation.StrategyPool = slices.Clone(set.Fragmentation.StrategyPool)
	saved.Fragmentation.SeqOverlapPattern = slices.Clone(set.Fragmentation.SeqOverlapPattern)
	saved.Fragmentation.SeqOverlapBytes = slices.Clone(set.Fragmentation.SeqOverlapBytes)
	saved.Faking.PayloadData = slices.Clone(set.Faking.PayloadData)
	saved.Faking.TLSMod = slices.Clone(set.Faking.TLSMod)
	saved.Faking.SNIMutation.FakeSNIs = slices.Clone(set.Faking.SNIMutation.FakeSNIs)
	if set.DNS.Pins != nil {
		saved.DNS.Pins = make(map[string][]string, len(set.DNS.Pins))
		for domain, ips := range set.DNS.Pins {
			saved.DNS.Pins[domain] = slices.Clone(ips)
		}
	}
	return saved
}

func (s strategySections) restoreInto(set *config.SetConfig) {
	set.TCP = s.TCP
	set.UDP = s.UDP
	set.Fragmentation = s.Fragmentation
	set.Faking = s.Faking
	set.DNS = s.DNS
}

func (w *Watchdog) healSet(id string) {
	cfg := w.cfgPtr.Load()
	set := cfg.GetSetById(id)

	w.mu.Lock()
	st := w.setStates[id]
	if st == nil {
		w.mu.Unlock()
		return
	}
	key := st.urlKey
	expectedRevision := st.queuedRevision
	unusable := map[string]bool{}
	for _, u := range st.URLs {
		if u.Status == URLStatusUnusable {
			unusable[u.URL] = true
		}
	}
	w.mu.Unlock()

	if !cfg.System.Checker.Watchdog.Enabled || !set.WatchdogActive() || urlKeyOf(set) != key {
		w.healAborted(id, "the set is no longer watched as it was")
		return
	}
	if expectedRevision == "" {
		expectedRevision = SetRevision(set)
	}
	if SetRevision(set) != expectedRevision {
		w.healRequeuedEdited(id)
		return
	}

	var urls []string
	reason := ""
	for _, raw := range set.Discovery.URLs {
		u := URLWatchStatus{URL: raw, Host: hostOfURL(raw)}
		if !w.classifyOwnership(id, &u) {
			if reason == "" {
				reason = ReasonNotOwned
			}
			continue
		}
		if unusable[raw] {
			if reason == "" {
				reason = ReasonUnusable
			}
			continue
		}
		urls = append(urls, raw)
	}
	if len(urls) == 0 {
		w.healUnverifiable(id, reason)
		return
	}

	tries := cfg.System.Checker.Watchdog.HealValidationTries
	if tries < 1 {
		tries = 1
	}
	tlsVersion, ipVersion := discovery.SetRunVersions(set, "", "")
	suiteID, err := w.disc.Start(cfg, urls, discovery.StartSuiteOptions{
		SkipDNS:         true,
		ValidationTries: tries,
		Source:          discovery.SourceWatchdog,
		SetId:           set.Id,
		SetStrategy:     discovery.SetRunStrategy(set),
		StopWhenCovered: true,
		TLSVersion:      tlsVersion,
		IPVersion:       ipVersion,
	})
	if errors.Is(err, discovery.ErrDiscoveryAlreadyRunning) {
		log.Infof("[WATCHDOG] set %q: another discovery run holds the runtime, the heal waits", set.Name)
		w.mu.Lock()
		if st := w.setStates[id]; st != nil {
			st.Status = SetStatusHealQueued
			st.Reason = ReasonBusy
			w.enqueueHealLocked(id, true)
		}
		w.mu.Unlock()
		return
	}
	if err != nil {
		w.healFailed(id, ReasonStartFailed, fmt.Sprintf("discovery could not start: %v", err))
		return
	}
	log.Infof("[WATCHDOG] set %q: started a discovery run for %s", set.Name, strings.Join(urls, ", "))

	verdict, outcome := w.awaitSetRun(id, suiteID, key)
	switch outcome {
	case setRunStopped:
		return
	case setRunAborted:
		w.healAborted(id, "the set changed or the watchdog was turned off during the run")
		return
	case setRunOverBudget:
		w.healFailed(id, ReasonBudget, fmt.Sprintf("the discovery run did not finish within %s", healCancelAfter))
		return
	}

	switch verdict.Status {
	case discovery.SetVerdictCovered:
		if !verdict.Confirmed || verdict.Set == nil {
			w.healFailed(id, ReasonIncomplete, "the run ended without a confirmed strategy")
			return
		}
	case discovery.SetVerdictCurrentWorks:
		w.healFailed(id, ReasonCurrentWorks,
			"the set's current strategy works in an isolated discovery run, but the live check fails: likely the host is handled by another set, or DNS or IPv6 differ on the live path")
		return
	case discovery.SetVerdictNotNeeded:
		w.healFailed(id, ReasonNotNeeded, "every URL loads without b4 in the discovery run, but the live check fails")
		return
	case discovery.SetVerdictPartial:
		w.healFailed(id, ReasonPartial, fmt.Sprintf("no single strategy works for every URL; %q covers %s, not %s",
			verdict.WinnerPreset, strings.Join(verdict.Covered, ", "), strings.Join(verdict.Uncovered, ", ")))
		return
	case discovery.SetVerdictNone:
		w.healFailed(id, ReasonNone, "no strategy loads "+strings.Join(verdict.Uncovered, ", "))
		return
	default:
		w.healFailed(id, ReasonIncomplete, "the discovery run ended without a verdict")
		return
	}

	w.adoptAndVerify(id, key, expectedRevision, urls, verdict)
}

func (w *Watchdog) awaitSetRun(id, suiteID, key string) (*discovery.SetVerdict, setRunOutcome) {
	start := w.clock()
	finishRequested := false
	pollEvery := w.pollEvery
	if pollEvery <= 0 {
		pollEvery = healPollInterval
	}

poll:
	for {
		select {
		case <-w.stop:
			log.Infof("[WATCHDOG] shutting down, canceling the set heal run")
			w.disc.Cancel(suiteID)
			return nil, setRunStopped
		case <-time.After(pollEvery):
		}

		cfg := w.cfgPtr.Load()
		set := cfg.GetSetById(id)
		if !cfg.System.Checker.Watchdog.Enabled || !set.WatchdogActive() || urlKeyOf(set) != key {
			log.Infof("[WATCHDOG] set heal canceled: the set or the watchdog changed")
			w.disc.Cancel(suiteID)
			return nil, setRunAborted
		}

		elapsed := w.clock().Sub(start)
		if elapsed >= healCancelAfter {
			log.Warnf("[WATCHDOG] set %q: the heal run exceeded %s, canceling it", set.Name, healCancelAfter)
			w.disc.Cancel(suiteID)
			return nil, setRunOverBudget
		}
		if elapsed >= healFinishAfter && !finishRequested {
			log.Infof("[WATCHDOG] set %q: the heal run passed %s, stopping the search and confirming what was found", set.Name, healFinishAfter)
			w.disc.Finish(suiteID)
			finishRequested = true
		}

		snap, ok := w.disc.Snapshot(suiteID)
		if !ok || suiteFinished(snap.Status) {
			break poll
		}
	}

	limit := w.idleWait
	if limit <= 0 {
		limit = setIdleWait
	}
	w.waitDiscoveryIdle(limit)
	if w.stopping() {
		return nil, setRunStopped
	}

	snap, ok := w.disc.Snapshot(suiteID)
	if !ok || snap.SetVerdict == nil {
		return &discovery.SetVerdict{Status: discovery.SetVerdictIncomplete}, setRunDone
	}
	return snap.SetVerdict, setRunDone
}

func (w *Watchdog) adoptAndVerify(id, key, expectedRevision string, urls []string, verdict *discovery.SetVerdict) {
	var saved strategySections
	var written *config.Config
	err := w.update(func(current *config.Config) (*config.Config, error) {
		set := current.GetSetById(id)
		if !current.System.Checker.Watchdog.Enabled || !set.WatchdogActive() || urlKeyOf(set) != key {
			return nil, errHealNotWatched
		}
		if SetRevision(set) != expectedRevision {
			return nil, errSetEdited
		}
		fresh := current.Clone()
		target := fresh.GetSetById(id)
		if target == nil {
			return nil, errHealNotWatched
		}
		saved = saveSections(target)
		target.AdoptStrategy(verdict.Set)
		if pins := verdict.CoveredPins(); len(pins) > 0 {
			target.ReplacePins(config.PinDomains(pins), pins)
		}
		written = fresh
		return fresh, nil
	})
	switch {
	case errors.Is(err, errHealNotWatched):
		w.healAborted(id, "the set changed or the watchdog was turned off before the result could be written")
		return
	case errors.Is(err, errSetEdited):
		w.healEdited(id)
		return
	case err != nil:
		w.healFailed(id, ReasonVerifyFailed, fmt.Sprintf("saving the healed strategy failed: %v", err))
		return
	}
	adopted := written.GetSetById(id)
	postRevision := SetRevision(adopted)
	log.Infof("[WATCHDOG] set %q: adopted %q, verifying every URL through the live engine", adopted.Name, verdict.WinnerPreset)

	results, passed, interrupted := w.verifySetURLs(id, urls)
	if interrupted {
		w.healInterrupted(id)
		return
	}
	if passed {
		w.healSucceeded(id, verdict.WinnerPreset, results)
		return
	}

	detail := "the adopted strategy did not load every URL through the live engine"
	for _, raw := range urls {
		if res, ok := results[raw]; ok && res.Status != URLStatusOK {
			detail += fmt.Sprintf(" (%s: %s)", res.Host, res.LastError)
			break
		}
	}
	if w.rollbackSet(id, postRevision, saved) {
		detail += "; the set's previous strategy was restored"
	} else {
		detail += "; the set was not restored"
	}
	w.healFailedWithResults(id, ReasonVerifyFailed, detail, results)
}

func (w *Watchdog) rollbackSet(id, postRevision string, saved strategySections) bool {
	name := id
	err := w.update(func(current *config.Config) (*config.Config, error) {
		set := current.GetSetById(id)
		if set == nil {
			return nil, errHealNotWatched
		}
		name = set.Name
		if SetRevision(set) != postRevision {
			return nil, errSetEdited
		}
		rollback := current.Clone()
		target := rollback.GetSetById(id)
		if target == nil {
			return nil, errHealNotWatched
		}
		saved.restoreInto(target)
		return rollback, nil
	})
	switch {
	case errors.Is(err, errHealNotWatched):
		return false
	case errors.Is(err, errSetEdited):
		log.Warnf("[WATCHDOG] set %q was edited since the heal was written, it is left as it is", name)
		return false
	case err != nil:
		log.Warnf("[WATCHDOG] set %q: restoring the previous strategy failed: %v", name, err)
		return false
	}
	log.Warnf("[WATCHDOG] set %q: verification failed, restored the previous strategy", name)
	return true
}

func (w *Watchdog) verifySetURLs(id string, urls []string) (map[string]URLWatchStatus, bool, bool) {
	cfg := w.cfgPtr.Load()
	wdCfg := cfg.System.Checker.Watchdog
	results := make(map[string]URLWatchStatus, len(urls))
	passed := make(map[string]bool, len(urls))

	if w.engine != nil {
		for _, raw := range urls {
			w.engine.ClearEscalation(hostOfURL(raw))
		}
	}
	if !w.pause(time.Duration(cfg.System.Checker.ConfigPropagateMs) * time.Millisecond) {
		return results, false, true
	}

	tries := wdCfg.VerifyTries
	if tries < 1 {
		tries = 1
	}
	timeout := time.Duration(wdCfg.TimeoutSec) * time.Second
	check := w.checkURL
	if check == nil {
		check = checkURLStrict
	}
	ctx, cancel := w.stopContext()
	defer cancel()

	for i := 0; i < tries && len(passed) < len(urls); i++ {
		if i > 0 && !w.pause(w.verifyDelay) {
			return results, false, true
		}
		now := w.clock()
		var round []URLWatchStatus
		for _, raw := range urls {
			if passed[raw] {
				continue
			}
			round = append(round, URLWatchStatus{URL: raw, Host: hostOfURL(raw), LastCheck: now})
		}
		var wg sync.WaitGroup
		for i := range round {
			u := &round[i]
			if !w.classifyOwnership(id, u) {
				continue
			}
			wg.Add(1)
			go func(u *URLWatchStatus) {
				defer wg.Done()
				res := check(ctx, u.URL, cfg.Queue.IPv6Enabled, timeout)
				u.Status, u.StatusCode, u.BytesRead, u.Speed, u.LastError = res.Status, res.StatusCode, res.BytesRead, res.Speed, res.Error
			}(u)
		}
		wg.Wait()
		if w.stopping() {
			return results, false, true
		}
		for _, u := range round {
			results[u.URL] = u
			if u.Status == URLStatusOK {
				passed[u.URL] = true
			}
		}
		for _, raw := range urls {
			if !passed[raw] {
				log.Warnf("[WATCHDOG] %s: post-heal verification try %d/%d failed (%s)", raw, i+1, tries, results[raw].LastError)
			}
		}
	}
	return results, len(passed) == len(urls), false
}

func (w *Watchdog) pause(d time.Duration) bool {
	if d <= 0 {
		select {
		case <-w.stop:
			return false
		default:
			return true
		}
	}
	select {
	case <-w.stop:
		return false
	case <-time.After(d):
		return true
	}
}

func mergeURLResults(st *setState, results map[string]URLWatchStatus) {
	for i := range st.URLs {
		if res, ok := results[st.URLs[i].URL]; ok {
			st.URLs[i] = res
		}
	}
}

func (w *Watchdog) healSucceeded(id, preset string, results map[string]URLWatchStatus) {
	wdCfg := w.cfgPtr.Load().System.Checker.Watchdog
	now := w.clock()
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.setStates[id]
	if st == nil {
		return
	}
	defer st.settleURLChange()
	mergeURLResults(st, results)
	st.Status = SetStatusHealthy
	st.Reason = ""
	st.LastError = ""
	st.LastHeal = now
	st.LastHealPreset = preset
	st.LastCheck = now
	st.ConsecutiveFailures = 0
	st.HealFailures = 0
	st.gaveUp = false
	st.queuedRevision = ""
	st.Interval = intervalOr(wdCfg.IntervalSec, 300)
	st.CooldownUntil = now.Add(time.Duration(wdCfg.Cooldown) * time.Second)
	log.Infof("[WATCHDOG] set %q: healed with %q, verified on every URL", st.SetName, preset)
}

func (w *Watchdog) healFailed(id, reason, detail string) {
	w.healFailedWithResults(id, reason, detail, nil)
}

func (w *Watchdog) healFailedWithResults(id, reason, detail string, results map[string]URLWatchStatus) {
	wdCfg := w.cfgPtr.Load().System.Checker.Watchdog
	now := w.clock()
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.setStates[id]
	if st == nil {
		return
	}
	defer st.settleURLChange()
	mergeURLResults(st, results)
	st.HealFailures++
	st.Reason = reason
	st.LastError = detail
	st.ConsecutiveFailures = 0
	st.queuedRevision = ""
	st.Interval = intervalOr(wdCfg.FailureInterval, 60)
	st.CooldownUntil = now.Add(time.Duration(wdCfg.Cooldown) * time.Second)
	if st.HealFailures >= maxHealFailures {
		st.gaveUp = true
		st.Status = SetStatusGaveUp
		log.Warnf("[WATCHDOG] set %q: heal failed (%s: %s); %d heals failed in a row, no more automatic heals until it loads again or a check is forced",
			st.SetName, reason, detail, st.HealFailures)
		return
	}
	st.Status = SetStatusCooldown
	log.Warnf("[WATCHDOG] set %q: heal failed (%s: %s), cooldown %ds", st.SetName, reason, detail, wdCfg.Cooldown)
}

func (w *Watchdog) healEdited(id string) {
	wdCfg := w.cfgPtr.Load().System.Checker.Watchdog
	now := w.clock()
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.setStates[id]
	if st == nil {
		return
	}
	defer st.settleURLChange()
	st.Status = SetStatusCooldown
	st.Reason = ReasonEdited
	st.LastError = "the set was edited during the heal, the result was not written"
	st.ConsecutiveFailures = 0
	st.queuedRevision = ""
	st.CooldownUntil = now.Add(time.Duration(wdCfg.Cooldown) * time.Second)
	log.Infof("[WATCHDOG] set %q was edited during the heal, the result was not written", st.SetName)
}

func (w *Watchdog) healRequeuedEdited(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.setStates[id]
	if st == nil {
		return
	}
	defer st.settleURLChange()
	st.resetToQueued(ReasonEdited)
	log.Infof("[WATCHDOG] set %q was edited after it was queued to heal, checking it again first", st.SetName)
}

func (w *Watchdog) healInterrupted(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.setStates[id]
	if st == nil {
		return
	}
	defer st.settleURLChange()
	st.resetToQueued("")
	log.Infof("[WATCHDOG] set %q: shutting down during verification, the adopted strategy is kept and the set is checked again on the next start", st.SetName)
}

func (w *Watchdog) healAborted(id, why string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.setStates[id]
	if st == nil {
		return
	}
	defer st.settleURLChange()
	st.resetToQueued("")
	log.Infof("[WATCHDOG] set %q: heal canceled, %s", st.SetName, why)
}

func (w *Watchdog) healUnverifiable(id, reason string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.setStates[id]
	if st == nil {
		return
	}
	defer st.settleURLChange()
	st.Status = SetStatusUnverifiable
	st.Reason = reason
	st.ConsecutiveFailures = 0
	st.queuedRevision = ""
	log.Warnf("[WATCHDOG] set %q: no URL it owns can be checked (%s), nothing to heal", st.SetName, reason)
}
