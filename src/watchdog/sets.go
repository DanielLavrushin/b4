package watchdog

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const maxHealFailures = 3

type setState struct {
	SetWatchStatus
	urlKey         string
	gaveUp         bool
	queuedRevision string
}

func urlKeyOf(set *config.SetConfig) string {
	if set == nil {
		return ""
	}
	return strings.Join(set.Discovery.URLs, "\n")
}

func hostOfURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return normalizeHost(ExtractDomain(raw))
	}
	return normalizeHost(u.Hostname())
}

type setRef struct {
	id   string
	name string
}

func watchedHostsOf(cfg *config.Config) map[string]setRef {
	hosts := map[string]setRef{}
	for _, set := range cfg.Sets {
		if !set.WatchdogActive() {
			continue
		}
		for _, raw := range set.Discovery.URLs {
			if host := hostOfURL(raw); host != "" {
				if _, taken := hosts[host]; !taken {
					hosts[host] = setRef{id: set.Id, name: set.Name}
				}
			}
		}
	}
	return hosts
}

func legacyWatchers(cfg *config.Config, owners map[string]*config.SetConfig) map[string]setRef {
	hosts := watchedHostsOf(cfg)
	watchers := map[string]setRef{}
	for _, d := range cfg.System.Checker.Watchdog.Domains {
		host := normalizeHost(ExtractDomain(d))
		if ref, ok := hosts[host]; ok {
			watchers[d] = ref
			continue
		}
		owner := owners[host]
		if owner == nil {
			continue
		}
		if set := cfg.GetSetById(owner.Id); set.WatchdogActive() {
			watchers[d] = setRef{id: set.Id, name: set.Name}
		}
	}
	return watchers
}

func intervalOr(v, fallback int) int {
	if v > 0 {
		return v
	}
	if fallback > 0 {
		return fallback
	}
	return 60
}

func freshURLStates(urls []string, previous []URLWatchStatus) []URLWatchStatus {
	byURL := make(map[string]URLWatchStatus, len(previous))
	for _, u := range previous {
		byURL[u.URL] = u
	}
	out := make([]URLWatchStatus, 0, len(urls))
	for _, raw := range urls {
		if existing, ok := byURL[raw]; ok {
			out = append(out, existing)
			continue
		}
		out = append(out, URLWatchStatus{URL: raw, Host: hostOfURL(raw), Status: URLStatusQueued})
	}
	return out
}

func newSetState(set *config.SetConfig, wdCfg config.WatchdogConfig) *setState {
	return &setState{
		SetWatchStatus: SetWatchStatus{
			SetId:    set.Id,
			SetName:  set.Name,
			Status:   SetStatusQueued,
			URLs:     freshURLStates(set.Discovery.URLs, nil),
			Interval: intervalOr(wdCfg.IntervalSec, 300),
		},
		urlKey: urlKeyOf(set),
	}
}

func (st *setState) snapshot() SetWatchStatus {
	out := st.SetWatchStatus
	out.URLs = append([]URLWatchStatus(nil), st.URLs...)
	if out.URLs == nil {
		out.URLs = []URLWatchStatus{}
	}
	return out
}

func (st *setState) resetToQueued(reason string) {
	st.Status = SetStatusQueued
	st.Reason = reason
	st.ConsecutiveFailures = 0
	st.LastCheck = time.Time{}
	st.queuedRevision = ""
}

func (w *Watchdog) syncSetStatesLocked(cfg *config.Config) {
	if w.setStates == nil {
		w.setStates = make(map[string]*setState)
	}
	wdCfg := cfg.System.Checker.Watchdog
	seen := make(map[string]bool, len(cfg.Sets))
	for _, set := range cfg.Sets {
		if !set.WatchdogActive() {
			continue
		}
		seen[set.Id] = true
		st := w.setStates[set.Id]
		if st == nil {
			w.setStates[set.Id] = newSetState(set, wdCfg)
			continue
		}
		st.SetName = set.Name
		if key := urlKeyOf(set); st.urlKey != key {
			st.urlKey = key
			st.URLs = freshURLStates(set.Discovery.URLs, st.URLs)
			if st.Status != SetStatusHealing {
				w.dropFromHealQueueLocked(set.Id)
				st.resetToQueued("")
			}
			continue
		}
		if st.Status == SetStatusHealQueued && st.queuedRevision != "" && SetRevision(set) != st.queuedRevision {
			log.Infof("[WATCHDOG] set %q was edited while waiting to heal, checking it again first", set.Name)
			w.dropFromHealQueueLocked(set.Id)
			st.resetToQueued(ReasonEdited)
		}
	}
	for id := range w.setStates {
		if !seen[id] {
			delete(w.setStates, id)
			w.dropFromHealQueueLocked(id)
		}
	}
}

func (w *Watchdog) parkHealQueueLocked() {
	parked := 0
	for _, st := range w.setStates {
		if st.Status == SetStatusHealQueued {
			st.resetToQueued("")
			parked++
		}
	}
	w.healQueue = nil
	if parked > 0 {
		log.Infof("[WATCHDOG] the watchdog is off, %d set(s) waiting to heal are checked again once it is back on", parked)
	}
}

func (w *Watchdog) skipLegacyDomainLocked(domain string, st *DomainStatus, watchers map[string]setRef) bool {
	ref, covered := watchers[domain]
	if !covered {
		st.WatchedBySetId, st.WatchedBySetName = "", ""
		delete(w.legacySkipLogged, domain)
		return false
	}
	st.WatchedBySetId, st.WatchedBySetName = ref.id, ref.name
	if w.legacySkipLogged == nil {
		w.legacySkipLogged = make(map[string]bool)
	}
	if !w.legacySkipLogged[domain] {
		w.legacySkipLogged[domain] = true
		log.Infof("[WATCHDOG] %s: checked through set %q, which has its own watchdog; the global list entry is not checked on its own", domain, ref.name)
	}
	return true
}

func (w *Watchdog) setSnapshotsLocked(cfg *config.Config) []SetWatchStatus {
	out := []SetWatchStatus{}
	wdCfg := cfg.System.Checker.Watchdog
	for _, set := range cfg.Sets {
		if !set.WatchdogActive() {
			continue
		}
		st := w.setStates[set.Id]
		if st == nil || st.urlKey != urlKeyOf(set) {
			st = newSetState(set, wdCfg)
		}
		snap := st.snapshot()
		snap.SetName = set.Name
		out = append(out, snap)
	}
	return out
}

func (w *Watchdog) GetSetState(id string) (SetWatchStatus, bool) {
	cfg := w.cfgPtr.Load()
	set := cfg.GetSetById(id)
	if !set.WatchdogActive() {
		return SetWatchStatus{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.setStates[id]
	if st == nil || st.urlKey != urlKeyOf(set) {
		st = newSetState(set, cfg.System.Checker.Watchdog)
	}
	snap := st.snapshot()
	snap.SetName = set.Name
	return snap, true
}

func (w *Watchdog) ForceCheckSet(id string, clearGiveUp bool) string {
	cfg := w.cfgPtr.Load()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.syncSetStatesLocked(cfg)
	st := w.setStates[id]
	if st == nil {
		return ForceCheckNotWatched
	}
	if clearGiveUp {
		st.gaveUp = false
		st.HealFailures = 0
	}
	if st.Status == SetStatusHealing {
		return ForceCheckHealing
	}
	w.dropFromHealQueueLocked(id)
	st.CooldownUntil = time.Time{}
	st.LastCheck = time.Time{}
	st.ConsecutiveFailures = 0
	st.queuedRevision = ""
	switch {
	case st.gaveUp:
		st.Status = SetStatusGaveUp
	case st.Status == SetStatusHealQueued || st.Status == SetStatusCooldown || st.Status == SetStatusGaveUp:
		st.Status = SetStatusQueued
		st.Reason = ""
	}
	if !cfg.System.Checker.Watchdog.Enabled {
		return ForceCheckMasterOff
	}
	return ForceCheckScheduled
}

func (w *Watchdog) enqueueHealLocked(id string, front bool) {
	w.dropFromHealQueueLocked(id)
	if front {
		w.healQueue = append([]string{id}, w.healQueue...)
		return
	}
	w.healQueue = append(w.healQueue, id)
}

func (w *Watchdog) dropFromHealQueueLocked(id string) {
	kept := w.healQueue[:0]
	for _, queued := range w.healQueue {
		if queued != id {
			kept = append(kept, queued)
		}
	}
	w.healQueue = kept
}

func (st *setState) dueLocked(now time.Time) bool {
	switch st.Status {
	case SetStatusHealQueued, SetStatusHealing:
		return false
	}
	if !st.CooldownUntil.IsZero() && now.Before(st.CooldownUntil) {
		return false
	}
	if st.LastCheck.IsZero() {
		return true
	}
	return !now.Before(st.LastCheck.Add(time.Duration(intervalOr(st.Interval, 60)) * time.Second))
}

type setCheckJob struct {
	setId    string
	urlKey   string
	revision string
	urls     []URLWatchStatus
}

func (w *Watchdog) tickSets(cfg *config.Config) {
	now := w.clock()
	w.mu.Lock()
	var jobs []*setCheckJob
	for _, set := range cfg.Sets {
		st := w.setStates[set.Id]
		if st == nil || !st.dueLocked(now) {
			continue
		}
		jobs = append(jobs, &setCheckJob{
			setId:    set.Id,
			urlKey:   st.urlKey,
			revision: SetRevision(set),
			urls:     freshURLStates(set.Discovery.URLs, nil),
		})
	}
	w.mu.Unlock()

	if len(jobs) == 0 {
		return
	}

	w.runSetChecks(cfg, jobs, now)
	if w.stopping() {
		return
	}

	current := w.cfgPtr.Load()
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, job := range jobs {
		st := w.setStates[job.setId]
		if st == nil || st.urlKey != job.urlKey || st.Status == SetStatusHealing || st.Status == SetStatusHealQueued {
			continue
		}
		if SetRevision(current.GetSetById(job.setId)) != job.revision {
			log.Infof("[WATCHDOG] set %q was edited during its check, the result is dropped and the set is checked again", st.SetName)
			st.resetToQueued(ReasonEdited)
			continue
		}
		w.applySetCheckLocked(st, job, now, cfg.System.Checker.Watchdog)
	}
}

func (w *Watchdog) classifyOwnership(setId string, u *URLWatchStatus) bool {
	if w.engine == nil {
		return true
	}
	owner := w.engine.Owner(u.Host)
	if owner == nil || owner.Id != setId {
		u.Status = URLStatusNotOwned
		if owner != nil {
			u.OwnerSetId, u.OwnerSetName = owner.Id, owner.Name
			u.LastError = fmt.Sprintf("from the router's view %s is handled by set %q", u.Host, owner.Name)
		} else {
			u.LastError = fmt.Sprintf("from the router's view no set handles %s", u.Host)
		}
		return false
	}
	u.OwnerSetId, u.OwnerSetName = owner.Id, owner.Name
	return true
}

func (w *Watchdog) runSetChecks(cfg *config.Config, jobs []*setCheckJob, now time.Time) {
	timeout := time.Duration(cfg.System.Checker.Watchdog.TimeoutSec) * time.Second
	ipv6 := cfg.Queue.IPv6Enabled
	check := w.checkURL
	if check == nil {
		check = checkURLStrict
	}
	ctx, cancel := w.stopContext()
	defer cancel()

	var wg sync.WaitGroup
	for _, job := range jobs {
		for i := range job.urls {
			u := &job.urls[i]
			u.LastCheck = now
			if !w.classifyOwnership(job.setId, u) {
				continue
			}
			if w.engine != nil {
				if to := w.engine.EscalatedTo(u.Host); to != "" {
					u.Status = URLStatusEscalated
					u.EscalatedTo = to
					u.LastError = fmt.Sprintf("an escalation sends %s to set %q", u.Host, to)
					continue
				}
			}
			wg.Add(1)
			go func(u *URLWatchStatus) {
				defer wg.Done()
				res := check(ctx, u.URL, ipv6, timeout)
				u.Status = res.Status
				u.StatusCode = res.StatusCode
				u.BytesRead = res.BytesRead
				u.Speed = res.Speed
				u.LastError = res.Error
			}(u)
		}
	}
	wg.Wait()
}

func (w *Watchdog) applySetCheckLocked(st *setState, job *setCheckJob, now time.Time, wdCfg config.WatchdogConfig) {
	results := job.urls
	st.URLs = results
	st.LastCheck = now

	checked, failed := 0, 0
	reason, reasonText, failText := "", "", ""
	for _, u := range results {
		switch u.Status {
		case URLStatusOK:
			checked++
		case URLStatusFailed:
			checked++
			failed++
			if failText == "" {
				failText = u.Host + ": " + u.LastError
			}
		default:
			if reason == "" {
				reason, reasonText = u.Status, u.LastError
			}
		}
	}

	maxRetries := wdCfg.MaxRetries
	if maxRetries < 1 {
		maxRetries = 1
	}

	switch {
	case checked == 0:
		if st.Status != SetStatusUnverifiable || st.Reason != reason {
			log.Warnf("[WATCHDOG] set %q: no URL can be checked (%s), it is never healed while this lasts", st.SetName, reasonText)
		}
		st.Status = SetStatusUnverifiable
		st.Reason = reason
		st.LastError = reasonText
		st.ConsecutiveFailures = 0
		st.Interval = intervalOr(wdCfg.IntervalSec, 300)

	case failed == 0:
		if st.Status == SetStatusDegraded || st.Status == SetStatusGaveUp || st.Status == SetStatusCooldown {
			log.Infof("[WATCHDOG] set %q: every URL loads again", st.SetName)
		}
		st.Status = SetStatusHealthy
		st.Reason = ""
		st.LastError = ""
		st.ConsecutiveFailures = 0
		st.HealFailures = 0
		st.gaveUp = false
		st.Interval = intervalOr(wdCfg.IntervalSec, 300)

	default:
		st.ConsecutiveFailures++
		st.LastError = failText
		log.Warnf("[WATCHDOG] set %q: check FAILED (%s) [%d/%d]", st.SetName, failText, st.ConsecutiveFailures, maxRetries)
		if st.gaveUp {
			st.Status = SetStatusGaveUp
			st.Interval = intervalOr(wdCfg.IntervalSec, 300)
			return
		}
		st.Reason = ""
		st.Interval = intervalOr(wdCfg.FailureInterval, 60)
		if st.ConsecutiveFailures < maxRetries {
			st.Status = SetStatusDegraded
			return
		}
		st.Status = SetStatusHealQueued
		st.queuedRevision = job.revision
		w.enqueueHealLocked(st.SetId, false)
		log.Infof("[WATCHDOG] set %q: queued for healing", st.SetName)
	}
}

func (w *Watchdog) pumpHealQueue() {
	if w.stopping() {
		return
	}
	w.mu.Lock()
	queued := len(w.healQueue)
	w.mu.Unlock()
	if queued == 0 {
		return
	}

	if w.healing.Load() {
		return
	}
	busy := w.disc.IsActive()
	w.mu.Lock()
	for _, id := range w.healQueue {
		if st := w.setStates[id]; st != nil && st.Status == SetStatusHealQueued {
			st.Reason = map[bool]string{true: ReasonBusy, false: ""}[busy]
		}
	}
	w.mu.Unlock()
	if busy {
		return
	}
	if !w.healing.CompareAndSwap(false, true) {
		return
	}

	w.mu.Lock()
	var id string
	for len(w.healQueue) > 0 {
		next := w.healQueue[0]
		w.healQueue = w.healQueue[1:]
		if st := w.setStates[next]; st != nil && st.Status == SetStatusHealQueued {
			st.Status = SetStatusHealing
			st.Reason = ""
			id = next
			break
		}
	}
	w.mu.Unlock()

	if id == "" {
		w.healing.Store(false)
		return
	}

	w.healWG.Add(1)
	go func() {
		defer w.healWG.Done()
		defer w.healing.Store(false)
		w.healSet(id)
	}()
}
