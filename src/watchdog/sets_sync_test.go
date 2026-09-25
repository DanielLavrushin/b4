package watchdog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

func TestAdoptAndRollbackKeepConcurrentSaves(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL), watchedSet("tr", otherURL))
	h.check.set(ytURL, failCheck)
	h.driver.verdict = coveredVerdict("disorder")
	var calls atomic.Int32
	h.beforeUpdate = func() {
		switch calls.Add(1) {
		case 1:
			h.edit("tr", func(s *config.SetConfig) { s.Fragmentation.Strategy = "oob" })
		case 2:
			h.edit("tr", func(s *config.SetConfig) { s.TCP.Seg2Delay = 42 })
		}
	}

	h.healNow("yt")

	if tr := h.set("tr"); tr.Fragmentation.Strategy != "oob" || tr.TCP.Seg2Delay != 42 {
		t.Errorf("saves that land while the watchdog writes are kept, got %s / %d", tr.Fragmentation.Strategy, tr.TCP.Seg2Delay)
	}
	if got := h.set("yt").Fragmentation.Strategy; got != "tcp" {
		t.Errorf("the rollback still restores the healed set, got %q", got)
	}
	if h.saves.Load() != 2 {
		t.Errorf("adopt and rollback are two saves, got %d", h.saves.Load())
	}
	if st := h.state("yt"); st.Reason != ReasonVerifyFailed || st.HealFailures != 1 {
		t.Fatalf("a failed verification is a heal failure: %+v", st)
	}
}

func TestAdoptKeepsAConcurrentSaveWhenVerified(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL), watchedSet("tr", otherURL))
	h.check.set(ytURL, okCheck)
	h.driver.verdict = coveredVerdict("disorder")
	var once sync.Once
	h.beforeUpdate = func() {
		once.Do(func() {
			h.edit("tr", func(s *config.SetConfig) { s.Fragmentation.Strategy = "oob" })
		})
	}

	h.healNow("yt")

	if got := h.set("tr").Fragmentation.Strategy; got != "oob" {
		t.Errorf("a save that lands just before the adopt is not overwritten, got %q", got)
	}
	if got := h.set("yt").Fragmentation.Strategy; got != "disorder" {
		t.Errorf("the adopt is written, got %q", got)
	}
	if st := h.state("yt"); st.Status != SetStatusHealthy {
		t.Fatalf("the heal succeeded: %+v", st)
	}
}

func TestRollbackComparesWithTheRevisionThatWasWritten(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.check.set(ytURL, failCheck)
	h.driver.verdict = coveredVerdict("disorder")
	var once sync.Once
	h.afterUpdate = func() {
		once.Do(func() {
			h.edit("yt", func(s *config.SetConfig) { s.TCP.Seg2Delay = 42 })
		})
	}

	h.healNow("yt")

	set := h.set("yt")
	if set.Fragmentation.Strategy != "disorder" || set.TCP.Seg2Delay != 42 {
		t.Errorf("an edit that lands right after the heal was written is never rolled back, got %s / %d", set.Fragmentation.Strategy, set.TCP.Seg2Delay)
	}
	if h.saves.Load() != 1 {
		t.Errorf("no rollback save, got %d saves", h.saves.Load())
	}
}

func TestAnEditDuringACheckDropsItsResult(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.check.set(ytURL, failCheck)
	var once sync.Once
	h.check.onCheck = func(string) {
		once.Do(func() {
			h.edit("yt", func(s *config.SetConfig) { s.Fragmentation.Strategy = "oob" })
		})
	}

	h.w.tick()

	st := h.state("yt")
	if st.Status != SetStatusQueued || st.Reason != ReasonEdited || st.ConsecutiveFailures != 0 || !st.LastCheck.IsZero() {
		t.Fatalf("a result checked against the old set is dropped and the set is checked again: %+v", st)
	}

	h.check.onCheck = nil
	h.w.tick()
	if st := h.state("yt"); st.Status != SetStatusDegraded || st.ConsecutiveFailures != 1 {
		t.Fatalf("the next tick checks the edited set: %+v", st)
	}
}

func TestHealAbortsWhenTheSetChangedSinceItWasQueued(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.check.set(ytURL, failCheck)
	h.tickUntilQueued("yt")

	h.w.mu.Lock()
	queued := h.w.setStates["yt"].queuedRevision
	h.w.mu.Unlock()
	if queued == "" || queued != SetRevision(h.set("yt")) {
		t.Fatalf("the heal carries the revision the failing check saw: %q", queued)
	}

	h.edit("yt", func(s *config.SetConfig) { s.Fragmentation.Strategy = "oob" })
	h.driver.verdict = coveredVerdict("disorder")
	h.w.pumpHealQueue()
	h.w.healWG.Wait()

	if len(h.driver.starts) != 0 {
		t.Fatal("no discovery run starts for a set edited after it was queued")
	}
	st := h.state("yt")
	if st.Status != SetStatusQueued || st.Reason != ReasonEdited || st.HealFailures != 0 {
		t.Fatalf("an edited set is checked again, not counted as a failure: %+v", st)
	}
	if h.saves.Load() != 0 || h.set("yt").Fragmentation.Strategy != "oob" {
		t.Error("nothing is written over the edit")
	}
}

func TestStopDuringVerifyKeepsTheAdoptWithoutCountingIt(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.check.set(ytURL, failCheck)
	h.driver.verdict = coveredVerdict("disorder")
	var once sync.Once
	h.check.onCheck = func(string) { once.Do(func() { close(h.w.stop) }) }

	h.healNow("yt")

	if h.saves.Load() != 1 {
		t.Errorf("only the adopt is written, no rollback, got %d saves", h.saves.Load())
	}
	if got := h.set("yt").Fragmentation.Strategy; got != "disorder" {
		t.Errorf("the adopted strategy stays until the next start checks it, got %q", got)
	}
	if n := h.check.callsFor(ytURL); n != 1 {
		t.Errorf("no verification round starts after the stop, got %d checks", n)
	}
	st := h.state("yt")
	if st.HealFailures != 0 || st.Status != SetStatusQueued || st.Reason == ReasonVerifyFailed {
		t.Fatalf("an interrupted verification is not a heal failure: %+v", st)
	}
}

func TestStopBeforeVerifyStartsIsNotAFailure(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.check.set(ytURL, failCheck)
	h.driver.verdict = coveredVerdict("disorder")
	h.afterUpdate = func() { close(h.w.stop) }

	h.healNow("yt")

	if h.saves.Load() != 1 || h.check.callsFor(ytURL) != 0 {
		t.Errorf("the adopt is kept and nothing is probed, got %d saves, %d checks", h.saves.Load(), h.check.callsFor(ytURL))
	}
	if st := h.state("yt"); st.HealFailures != 0 {
		t.Fatalf("not a failure: %+v", st)
	}
}

func TestStopCancelsSetChecksInFlight(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	started := make(chan struct{})
	var once sync.Once
	h.w.checkURL = func(ctx context.Context, _ string, _ bool, _ time.Duration) URLCheck {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return failCheck
	}

	done := make(chan struct{})
	go func() {
		h.w.tick()
		close(done)
	}()
	<-started
	close(h.w.stop)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a stop must cancel a check in flight")
	}

	if st := h.state("yt"); st.ConsecutiveFailures != 0 || !st.LastCheck.IsZero() || st.Status != SetStatusQueued {
		t.Fatalf("a check cut short by a stop is not recorded: %+v", st)
	}
}

func TestStopCancelsLegacyChecksInFlight(t *testing.T) {
	h := newHarness(t)
	next := h.ptr.Load().Clone()
	next.System.Checker.Watchdog.Domains = []string{"rutracker.org"}
	h.ptr.Store(next)
	started := make(chan struct{})
	h.w.checkLegacy = func(ctx context.Context, domains []string, _ uint, _ time.Duration) map[string]CheckResult {
		close(started)
		<-ctx.Done()
		out := map[string]CheckResult{}
		for _, d := range domains {
			out[d] = CheckResult{Error: "canceled"}
		}
		return out
	}

	done := make(chan struct{})
	go func() {
		h.w.tick()
		close(done)
	}()
	<-started
	close(h.w.stop)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a stop must cancel a legacy check in flight")
	}

	h.w.mu.Lock()
	st := *h.w.domainStates["rutracker.org"]
	h.w.mu.Unlock()
	if st.ConsecutiveFailures != 0 || !st.LastCheck.IsZero() {
		t.Fatalf("a legacy check cut short by a stop is not recorded: %+v", st)
	}
}

func TestStrictCheckEndsWhenItsContextIsCanceled(t *testing.T) {
	reached := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case reached <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-reached
		cancel()
	}()

	start := time.Now()
	res := strictCheck(ctx, srv.URL+"/", strictOptions{Timeout: 20 * time.Second, isReserved: allowLoopback})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("a canceled probe must end at once, took %s", elapsed)
	}
	if res.Status == URLStatusOK {
		t.Fatalf("a canceled probe is never a success: %+v", res)
	}
}

func TestLegacyEntriesASetWatchesAreSkippedAndLabelled(t *testing.T) {
	yt := watchedSet("yt", ytURL)
	plain := config.NewSetConfig()
	plain.Id, plain.Name, plain.Enabled = "plain", "Plain", true
	plain.Targets.SNIDomains = []string{"example.org"}
	h := newHarness(t, yt, &plain)
	next := h.ptr.Load().Clone()
	next.System.Checker.Watchdog.Domains = []string{"www.youtube.com", "m.youtube.com", "example.org"}
	h.ptr.Store(next)
	h.engine.mu.Lock()
	h.engine.owners["m.youtube.com"] = "yt"
	h.engine.owners["example.org"] = "plain"
	h.engine.mu.Unlock()
	for _, d := range []string{ytURL, "www.youtube.com", "m.youtube.com", "example.org"} {
		h.check.set(d, okCheck)
	}

	h.w.tick()

	if n := h.check.callsFor("www.youtube.com"); n != 0 {
		t.Errorf("an entry a watched set probes is not checked, got %d", n)
	}
	if n := h.check.callsFor("m.youtube.com"); n != 0 {
		t.Errorf("an entry a watched set handles from the router's view is not checked, got %d", n)
	}
	if n := h.check.callsFor("example.org"); n != 1 {
		t.Errorf("an entry an unwatched set handles is checked, got %d", n)
	}

	rows := map[string]*DomainStatus{}
	for _, d := range h.w.GetState().Domains {
		rows[d.Domain] = d
	}
	for _, d := range []string{"www.youtube.com", "m.youtube.com"} {
		if rows[d].WatchedBySetId != "yt" || rows[d].WatchedBySetName != "yt" {
			t.Errorf("%s names the set that checks it: %+v", d, rows[d])
		}
	}
	if rows["example.org"].WatchedBySetId != "" {
		t.Errorf("an entry checked on its own names no set: %+v", rows["example.org"])
	}

	watched, _ := json.Marshal(rows["m.youtube.com"])
	own, _ := json.Marshal(rows["example.org"])
	if !strings.Contains(string(watched), `"watched_by_set_id":"yt"`) || !strings.Contains(string(watched), `"watched_by_set_name":"yt"`) {
		t.Errorf("the JSON carries watched_by_set_id and watched_by_set_name: %s", watched)
	}
	if strings.Contains(string(own), "watched_by") {
		t.Errorf("the fields are omitted when empty: %s", own)
	}

	h.edit("yt", func(s *config.SetConfig) { s.Discovery.Watchdog = false })
	h.w.tick()
	if h.check.callsFor("www.youtube.com") != 1 || h.check.callsFor("m.youtube.com") != 1 {
		t.Error("once the set is no longer watched the entries are checked on their own again")
	}
	for _, d := range h.w.GetState().Domains {
		if d.WatchedBySetId != "" {
			t.Errorf("%s no longer names a watching set: %+v", d.Domain, d)
		}
	}
	h.w.mu.Lock()
	stale := h.w.domainStates["m.youtube.com"].WatchedBySetId
	h.w.mu.Unlock()
	if stale != "" {
		t.Errorf("the stored row is cleared too: %q", stale)
	}
}

func TestMasterSwitchOffDropsQueuedHeals(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.check.set(ytURL, failCheck)
	h.tickUntilQueued("yt")

	off := h.ptr.Load().Clone()
	off.System.Checker.Watchdog.Enabled = false
	h.ptr.Store(off)
	h.w.tick()

	if q := h.queue(); len(q) != 0 {
		t.Fatalf("the heal queue is dropped while the watchdog is off: %v", q)
	}
	st := h.state("yt")
	if st.Status != SetStatusQueued || st.ConsecutiveFailures != 0 || st.HealFailures != 0 {
		t.Fatalf("a set waiting to heal goes back to queued without a failure: %+v", st)
	}

	before := h.check.callsFor(ytURL)
	on := h.ptr.Load().Clone()
	on.System.Checker.Watchdog.Enabled = true
	h.ptr.Store(on)
	h.driver.setActive(true)
	h.w.tick()

	if n := h.check.callsFor(ytURL); n != before+1 {
		t.Errorf("turning it back on checks the set again, got %d checks after %d", n, before)
	}
	if st := h.state("yt"); st.Status != SetStatusDegraded || st.ConsecutiveFailures != 1 {
		t.Fatalf("the set needs max_retries fresh failures before it heals: %+v", st)
	}
	if len(h.driver.starts) != 0 || len(h.queue()) != 0 {
		t.Error("no heal on the first check after the switch comes back on")
	}
}

func TestForceCheckSetOutcomes(t *testing.T) {
	unwatched := watchedSet("off", "https://example.org/")
	unwatched.Discovery.Watchdog = false
	h := newHarness(t, watchedSet("yt", ytURL), watchedSet("tr", otherURL), unwatched)
	h.check.set(ytURL, failCheck)
	h.driver.setActive(true)
	h.w.tick()
	if st := h.state("yt"); st.ConsecutiveFailures != 1 {
		t.Fatalf("precondition: one failure: %+v", st)
	}

	if got := h.w.ForceCheckSet("yt", false); got != ForceCheckScheduled {
		t.Fatalf("got %q", got)
	}
	if st := h.state("yt"); st.ConsecutiveFailures != 0 || !st.LastCheck.IsZero() {
		t.Fatalf("a forced check starts counting failures again: %+v", st)
	}

	if got := h.w.ForceCheckSet("off", true); got != ForceCheckNotWatched {
		t.Errorf("a set whose watchdog is off: got %q", got)
	}
	if got := h.w.ForceCheckSet("nope", true); got != ForceCheckNotWatched {
		t.Errorf("an unknown set: got %q", got)
	}

	h.w.mu.Lock()
	h.w.setStates["tr"].Status = SetStatusHealing
	h.w.setStates["tr"].HealFailures = 2
	h.w.mu.Unlock()
	if got := h.w.ForceCheckSet("tr", false); got != ForceCheckHealing {
		t.Errorf("a set being healed: got %q", got)
	}
	if st := h.state("tr"); st.HealFailures != 2 || st.Status != SetStatusHealing {
		t.Errorf("a heal in progress is left alone: %+v", st)
	}

	off := h.ptr.Load().Clone()
	off.System.Checker.Watchdog.Enabled = false
	h.ptr.Store(off)
	if got := h.w.ForceCheckSet("yt", true); got != ForceCheckMasterOff {
		t.Errorf("with the master switch off: got %q", got)
	}

	fresh := watchedSet("new", "https://www.wikipedia.org/")
	on := h.ptr.Load().Clone()
	on.System.Checker.Watchdog.Enabled = true
	on.Sets = append(on.Sets, fresh)
	if err := on.Validate(); err != nil {
		t.Fatal(err)
	}
	h.ptr.Store(on)
	if got := h.w.ForceCheckSet("new", false); got != ForceCheckScheduled {
		t.Errorf("a set watched since the last tick is found: got %q", got)
	}
}

func TestADisabledSetKeepsItsWatchdog(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.w.tick()

	h.edit("yt", func(s *config.SetConfig) { s.Enabled = false })
	if !h.set("yt").Discovery.Watchdog {
		t.Fatal("disabling a set keeps its watchdog flag")
	}
	h.w.tick()
	if state := h.w.GetState(); len(state.Sets) != 0 {
		t.Fatalf("a disabled set is not watched: %+v", state.Sets)
	}

	h.edit("yt", func(s *config.SetConfig) { s.Enabled = true })
	h.w.tick()
	if state := h.w.GetState(); len(state.Sets) != 1 || state.Sets[0].SetId != "yt" {
		t.Fatalf("enabling it again watches it again: %+v", state.Sets)
	}

	h.edit("yt", func(s *config.SetConfig) {
		s.Targets.SNIDomains = nil
		s.Targets.IPs = []string{"203.0.113.7"}
	})
	if !h.set("yt").Discovery.Watchdog {
		t.Fatal("an IP-only set keeps its watchdog flag")
	}
	if state := h.w.GetState(); len(state.Sets) != 0 {
		t.Fatalf("an IP-only set is not watched: %+v", state.Sets)
	}
}

func legacyHarness(t *testing.T) *harness {
	t.Helper()
	return legacyHarnessFor(t, "youtube.com")
}

func legacyHarnessFor(t *testing.T, domain string) *harness {
	t.Helper()
	yt := config.NewSetConfig()
	yt.Id, yt.Name, yt.Enabled = "yt", "YouTube", true
	yt.Targets.SNIDomains = []string{"youtube.com"}
	yt.Fragmentation.Strategy = "tcp"
	tr := config.NewSetConfig()
	tr.Id, tr.Name, tr.Enabled = "tr", "Tracker", true
	tr.Targets.SNIDomains = []string{"rutracker.org"}
	h := newHarness(t, &yt, &tr)
	winner := config.NewSetConfig()
	winner.Fragmentation.Strategy = "disorder"
	h.driver.results = map[string]*discovery.DomainDiscoveryResult{
		domain: {
			Domain:      domain,
			BestPreset:  "p",
			BestSuccess: true,
			Results:     map[string]*discovery.DomainPresetResult{"p": {Status: discovery.CheckStatusComplete, Set: &winner}},
		},
	}
	h.check.set(domain, failCheck)
	return h
}

func (h *harness) editGlobal(mutate func(*config.Config)) {
	h.t.Helper()
	unlock := config.LockWrites()
	defer unlock()
	next := h.ptr.Load().Clone()
	mutate(next)
	if err := next.Validate(); err != nil {
		h.t.Fatalf("edit: %v", err)
	}
	h.ptr.Store(next)
}

func (h *harness) afterFirstUpdate(edit func()) {
	var once sync.Once
	h.afterUpdate = func() { once.Do(edit) }
}

func TestLegacyHealWritesOverTheCurrentConfigAndRollsBack(t *testing.T) {
	h := legacyHarness(t)
	var once sync.Once
	h.beforeUpdate = func() {
		once.Do(func() {
			h.edit("tr", func(s *config.SetConfig) { s.Fragmentation.Strategy = "oob" })
		})
	}

	h.w.healBatch([]string{"youtube.com"})

	if got := h.set("tr").Fragmentation.Strategy; got != "oob" {
		t.Errorf("a save that lands just before the legacy heal writes is kept, got %q", got)
	}
	if got := h.set("yt").Fragmentation.Strategy; got != "tcp" {
		t.Errorf("a failed verification rolls the heal back, got %q", got)
	}
	if got := h.set("tr").Fragmentation.Strategy; got != "oob" {
		t.Errorf("the rollback keeps the save made before the heal, got %q", got)
	}
	if h.saves.Load() != 2 {
		t.Errorf("heal and rollback, got %d saves", h.saves.Load())
	}
}

func TestLegacyRollbackSurvivesAnUnrelatedSave(t *testing.T) {
	h := legacyHarness(t)
	h.afterFirstUpdate(func() {
		h.edit("tr", func(s *config.SetConfig) { s.TCP.Seg2Delay = 42 })
		h.editGlobal(func(c *config.Config) { c.System.Geo.AutoUpdate.LastRun = "2026-09-24T12:00:00Z" })
	})

	h.w.healBatch([]string{"youtube.com"})

	if got := h.set("yt").Fragmentation.Strategy; got != "tcp" {
		t.Errorf("a save to another set and to the geo settings does not stop the rollback, got %q", got)
	}
	if got := h.set("tr").TCP.Seg2Delay; got != 42 {
		t.Errorf("the later save to another set is kept, got %d", got)
	}
	if got := h.ptr.Load().System.Geo.AutoUpdate.LastRun; got != "2026-09-24T12:00:00Z" {
		t.Errorf("the later geo save is kept, got %q", got)
	}
	if h.saves.Load() != 2 {
		t.Errorf("heal and rollback, got %d saves", h.saves.Load())
	}
}

func TestLegacyRollbackLeavesASetEditedAfterTheHeal(t *testing.T) {
	h := legacyHarness(t)
	h.afterFirstUpdate(func() {
		h.edit("yt", func(s *config.SetConfig) { s.TCP.Seg2Delay = 42 })
	})

	h.w.healBatch([]string{"youtube.com"})

	yt := h.set("yt")
	if yt.Fragmentation.Strategy != "disorder" || yt.TCP.Seg2Delay != 42 {
		t.Errorf("a set edited after the heal is left as it is, got %q / %d", yt.Fragmentation.Strategy, yt.TCP.Seg2Delay)
	}
	if h.saves.Load() != 1 {
		t.Errorf("only the heal is written, got %d saves", h.saves.Load())
	}
}

func TestLegacyRollbackUndoesTheDomainsTheHealAdded(t *testing.T) {
	h := legacyHarnessFor(t, "www.youtube.com")
	h.afterFirstUpdate(func() {
		yt := h.set("yt")
		if !slices.Contains(yt.Targets.SNIDomains, "www.youtube.com") || yt.Fragmentation.Strategy != "disorder" {
			t.Errorf("precondition: the heal adopts into the listing set and adds the host: %v %q", yt.Targets.SNIDomains, yt.Fragmentation.Strategy)
		}
	})

	h.w.healBatch([]string{"www.youtube.com"})

	yt := h.set("yt")
	if yt.Fragmentation.Strategy != "tcp" {
		t.Errorf("the strategy is restored, got %q", yt.Fragmentation.Strategy)
	}
	if !slices.Equal(yt.Targets.SNIDomains, []string{"youtube.com"}) || slices.Contains(yt.Targets.DomainsToMatch, "www.youtube.com") {
		t.Errorf("the host the heal added is taken out again: %v / %v", yt.Targets.SNIDomains, yt.Targets.DomainsToMatch)
	}
}

func TestLegacyRollbackRemovesTheSetTheHealCreated(t *testing.T) {
	for _, c := range []struct {
		name        string
		editCreated bool
		wantKept    bool
	}{
		{"unrelated save", false, false},
		{"created set edited", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := legacyHarnessFor(t, "example.com")
			h.afterFirstUpdate(func() {
				h.edit("tr", func(s *config.SetConfig) { s.TCP.Seg2Delay = 42 })
				if c.editCreated {
					next := h.ptr.Load()
					for _, set := range next.Sets {
						if set.Name == "watchdog-example.com" {
							h.edit(set.Id, func(s *config.SetConfig) { s.TCP.Seg2Delay = 7 })
						}
					}
				}
			})

			h.w.healBatch([]string{"example.com"})

			created := false
			for _, set := range h.ptr.Load().Sets {
				if set.Name == "watchdog-example.com" {
					created = true
				}
			}
			if created != c.wantKept {
				t.Errorf("the set the heal created kept=%v, want %v", created, c.wantKept)
			}
			if got := h.set("tr").TCP.Seg2Delay; got != 42 {
				t.Errorf("the save to another set is kept, got %d", got)
			}
			if got := len(h.ptr.Load().Sets); got != 2+map[bool]int{true: 1, false: 0}[c.wantKept] {
				t.Errorf("no other set is touched, got %d sets", got)
			}
		})
	}
}
