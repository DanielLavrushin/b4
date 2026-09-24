package watchdog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type fakeEngine struct {
	mu        sync.Mutex
	cfgPtr    *atomic.Pointer[config.Config]
	owners    map[string]string
	escalated map[string]string
	cleared   []string
}

func (e *fakeEngine) Owner(host string) *config.SetConfig {
	e.mu.Lock()
	id, ok := e.owners[host]
	e.mu.Unlock()
	if !ok {
		return nil
	}
	return e.cfgPtr.Load().GetSetById(id)
}

func (e *fakeEngine) EscalatedTo(host string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.escalated[host]
}

func (e *fakeEngine) ClearEscalation(host string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cleared = append(e.cleared, host)
	delete(e.escalated, host)
}

type fakeDriver struct {
	mu         sync.Mutex
	active     bool
	startErr   error
	starts     []discovery.StartSuiteOptions
	startURLs  [][]string
	runningFor int
	snapshots  int
	verdict    *discovery.SetVerdict
	results    map[string]*discovery.DomainDiscoveryResult
	finished   int
	canceled   int
	onSnapshot func(n int)
}

func (d *fakeDriver) IsActive() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.active
}

func (d *fakeDriver) setActive(v bool) {
	d.mu.Lock()
	d.active = v
	d.mu.Unlock()
}

func (d *fakeDriver) Start(cfg *config.Config, urls []string, opts discovery.StartSuiteOptions) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.startErr != nil {
		return "", d.startErr
	}
	d.starts = append(d.starts, opts)
	d.startURLs = append(d.startURLs, append([]string(nil), urls...))
	return "suite-1", nil
}

func (d *fakeDriver) Snapshot(id string) (*discovery.CheckSuite, bool) {
	d.mu.Lock()
	d.snapshots++
	n := d.snapshots
	hook := d.onSnapshot
	status := discovery.CheckStatusRunning
	if n > d.runningFor {
		status = discovery.CheckStatusComplete
	}
	verdict := d.verdict
	results := d.results
	d.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	suite := &discovery.CheckSuite{Id: id, Status: status}
	if status == discovery.CheckStatusComplete {
		suite.SetVerdict = verdict
		suite.DomainDiscoveryResults = results
	}
	return suite, true
}

func (d *fakeDriver) Finish(string) {
	d.mu.Lock()
	d.finished++
	d.mu.Unlock()
}

func (d *fakeDriver) Cancel(string) {
	d.mu.Lock()
	d.canceled++
	d.mu.Unlock()
}

type fakeChecker struct {
	mu      sync.Mutex
	results map[string]URLCheck
	calls   map[string]int
	onCheck func(url string)
}

func (c *fakeChecker) set(url string, res URLCheck) {
	c.mu.Lock()
	c.results[url] = res
	c.mu.Unlock()
}

func (c *fakeChecker) callsFor(url string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[url]
}

func (c *fakeChecker) check(_ context.Context, url string, _ bool, _ time.Duration) URLCheck {
	c.mu.Lock()
	c.calls[url]++
	res, ok := c.results[url]
	hook := c.onCheck
	c.mu.Unlock()
	if hook != nil {
		hook(url)
	}
	if !ok {
		return URLCheck{Status: URLStatusFailed, Error: "no answer"}
	}
	return res
}

var (
	okCheck   = URLCheck{Status: URLStatusOK, StatusCode: 200, BytesRead: 50000}
	failCheck = URLCheck{Status: URLStatusFailed, Error: "TLS handshake timed out (drop) [TLS_DROP]"}
)

type harness struct {
	t      *testing.T
	w      *Watchdog
	ptr    *atomic.Pointer[config.Config]
	engine *fakeEngine
	driver *fakeDriver
	check  *fakeChecker
	clock  *fakeClock
	saves  atomic.Int32

	beforeUpdate func()
	afterUpdate  func()
}

func watchedSet(id string, urls ...string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = id, id, true
	set.Discovery.URLs = urls
	set.Discovery.Watchdog = true
	set.Fragmentation.Strategy = "tcp"
	for _, raw := range urls {
		host := hostOfURL(raw)
		set.Targets.SNIDomains = append(set.Targets.SNIDomains, host)
		set.Targets.DomainsToMatch = append(set.Targets.DomainsToMatch, host)
	}
	return &set
}

func newHarness(t *testing.T, sets ...*config.SetConfig) *harness {
	t.Helper()
	cfg := config.NewConfig()
	cfg.System.Checker.ConfigPropagateMs = 0
	wd := &cfg.System.Checker.Watchdog
	wd.Enabled = true
	wd.MaxRetries = 2
	wd.IntervalSec = 300
	wd.FailureInterval = 60
	wd.Cooldown = 900
	wd.VerifyTries = 2
	cfg.Sets = sets
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fixture config: %v", err)
	}

	h := &harness{
		t:      t,
		ptr:    &atomic.Pointer[config.Config]{},
		driver: &fakeDriver{},
		check:  &fakeChecker{results: map[string]URLCheck{}, calls: map[string]int{}},
		clock:  &fakeClock{t: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)},
	}
	h.ptr.Store(&cfg)
	h.engine = &fakeEngine{cfgPtr: h.ptr, owners: map[string]string{}, escalated: map[string]string{}}
	for _, set := range sets {
		for _, raw := range set.Discovery.URLs {
			h.engine.owners[hostOfURL(raw)] = set.Id
		}
	}

	h.w = New(h.ptr, nil, func(mutate func(*config.Config) (*config.Config, error)) error {
		if hook := h.beforeUpdate; hook != nil {
			hook()
		}
		err := func() error {
			unlock := config.LockWrites()
			defer unlock()
			next, err := mutate(h.ptr.Load())
			if err != nil {
				return err
			}
			if err := next.Validate(); err != nil {
				return err
			}
			h.ptr.Store(next)
			h.saves.Add(1)
			return nil
		}()
		if hook := h.afterUpdate; hook != nil && err == nil {
			hook()
		}
		return err
	})
	h.w.stop = make(chan struct{})
	h.w.SetEngine(h.engine)
	h.w.disc = h.driver
	h.w.checkURL = h.check.check
	h.w.checkLegacy = func(ctx context.Context, domains []string, _ uint, _ time.Duration) map[string]CheckResult {
		out := map[string]CheckResult{}
		for _, d := range domains {
			res := h.check.check(ctx, d, false, 0)
			out[d] = CheckResult{OK: res.Status == URLStatusOK, Error: res.Error}
		}
		return out
	}
	h.w.now = h.clock.Now
	h.w.pollEvery = time.Millisecond
	h.w.verifyDelay = 0
	h.w.idleWait = 10 * time.Millisecond
	return h
}

func (h *harness) state(id string) SetWatchStatus {
	h.t.Helper()
	h.w.mu.Lock()
	defer h.w.mu.Unlock()
	st := h.w.setStates[id]
	if st == nil {
		h.t.Fatalf("no state for set %q", id)
	}
	return st.snapshot()
}

func (h *harness) gaveUp(id string) bool {
	h.w.mu.Lock()
	defer h.w.mu.Unlock()
	return h.w.setStates[id] != nil && h.w.setStates[id].gaveUp
}

func (h *harness) queue() []string {
	h.w.mu.Lock()
	defer h.w.mu.Unlock()
	return append([]string(nil), h.w.healQueue...)
}

func (h *harness) set(id string) *config.SetConfig {
	return h.ptr.Load().GetSetById(id)
}

func (h *harness) edit(id string, mutate func(*config.SetConfig)) {
	h.t.Helper()
	unlock := config.LockWrites()
	defer unlock()
	next := h.ptr.Load().Clone()
	mutate(next.GetSetById(id))
	if err := next.Validate(); err != nil {
		h.t.Fatalf("edit: %v", err)
	}
	h.ptr.Store(next)
}

func (h *harness) tickUntilQueued(id string) {
	h.t.Helper()
	h.driver.mu.Lock()
	wasActive := h.driver.active
	h.driver.active = true
	h.driver.mu.Unlock()
	defer h.driver.setActive(wasActive)
	for i := 0; i < 10; i++ {
		h.w.tick()
		if h.state(id).Status == SetStatusHealQueued || h.state(id).Status == SetStatusHealing {
			return
		}
		h.clock.Advance(2 * time.Minute)
	}
	h.t.Fatalf("set %q never queued for healing: %+v", id, h.state(id))
}

func (h *harness) healNow(id string) {
	h.t.Helper()
	h.w.mu.Lock()
	if h.w.setStates[id] == nil {
		h.w.syncSetStatesLocked(h.ptr.Load())
	}
	h.w.dropFromHealQueueLocked(id)
	h.w.setStates[id].Status = SetStatusHealing
	h.w.mu.Unlock()
	h.w.healSet(id)
}

func coveredVerdict(strategy string) *discovery.SetVerdict {
	winner := config.NewSetConfig()
	winner.Fragmentation.Strategy = strategy
	winner.Faking.TTL = 7
	return &discovery.SetVerdict{Status: discovery.SetVerdictCovered, WinnerPreset: "preset-" + strategy, Set: &winner, Confirmed: true}
}

const (
	ytURL    = "https://www.youtube.com/"
	videoURL = "https://i.ytimg.com/vi/x.jpg"
	otherURL = "https://rutracker.org/"
)

func TestSyncTracksOnlyWatchedSets(t *testing.T) {
	watched := watchedSet("yt", ytURL, videoURL)
	off := watchedSet("off", otherURL)
	off.Discovery.Watchdog = false
	h := newHarness(t, watched, off)

	h.w.mu.Lock()
	h.w.syncSetStatesLocked(h.ptr.Load())
	h.w.mu.Unlock()

	st := h.state("yt")
	if st.Status != SetStatusQueued || len(st.URLs) != 2 || st.URLs[0].Status != URLStatusQueued || st.URLs[0].Host != "www.youtube.com" {
		t.Fatalf("a newly watched set starts queued with every URL queued: %+v", st)
	}
	h.w.mu.Lock()
	_, tracked := h.w.setStates["off"]
	h.w.mu.Unlock()
	if tracked {
		t.Fatal("a set whose watchdog is off must not be tracked")
	}

	h.check.set(ytURL, okCheck)
	h.check.set(videoURL, okCheck)
	h.w.tick()
	if st := h.state("yt"); st.Status != SetStatusHealthy || st.URLs[1].Status != URLStatusOK {
		t.Fatalf("both URLs load, the set is healthy: %+v", st)
	}

	h.edit("yt", func(s *config.SetConfig) { s.Discovery.URLs = []string{ytURL, otherURL} })
	h.w.mu.Lock()
	h.w.syncSetStatesLocked(h.ptr.Load())
	h.w.mu.Unlock()
	st = h.state("yt")
	if len(st.URLs) != 2 || st.URLs[0].Status != URLStatusOK || st.URLs[1].URL != otherURL || st.URLs[1].Status != URLStatusQueued {
		t.Fatalf("a kept URL keeps its state and a new one is queued: %+v", st.URLs)
	}
	if st.Status != SetStatusQueued || !st.LastCheck.IsZero() {
		t.Errorf("a URL change asks for a fresh check: %+v", st)
	}

	h.edit("yt", func(s *config.SetConfig) { s.Discovery.Watchdog = false })
	h.w.mu.Lock()
	h.w.syncSetStatesLocked(h.ptr.Load())
	_, tracked = h.w.setStates["yt"]
	h.w.mu.Unlock()
	if tracked {
		t.Fatal("turning the set's watchdog off drops its state")
	}
}

func TestOwnershipEscalationAndUnusableNeverHeal(t *testing.T) {
	notOwned := watchedSet("a", ytURL)
	escalated := watchedSet("b", videoURL)
	unusable := watchedSet("c", otherURL)
	other := config.NewSetConfig()
	other.Id, other.Name, other.Enabled = "other", "Other", true
	h := newHarness(t, notOwned, escalated, unusable, &other)

	h.engine.owners["www.youtube.com"] = "other"
	h.engine.escalated["i.ytimg.com"] = "fallback"
	h.check.set(otherURL, URLCheck{Status: URLStatusUnusable, Error: "certificate verification failed"})

	for i := 0; i < 6; i++ {
		h.w.tick()
		h.clock.Advance(10 * time.Minute)
	}

	a := h.state("a")
	if a.Status != SetStatusUnverifiable || a.Reason != ReasonNotOwned || a.URLs[0].OwnerSetId != "other" || a.URLs[0].OwnerSetName != "Other" {
		t.Errorf("a URL another set handles is not_owned and names the owner: %+v", a)
	}
	b := h.state("b")
	if b.Status != SetStatusUnverifiable || b.Reason != ReasonEscalated || b.URLs[0].EscalatedTo != "fallback" {
		t.Errorf("an escalated URL is reported with its target: %+v", b)
	}
	c := h.state("c")
	if c.Status != SetStatusUnverifiable || c.Reason != ReasonUnusable || c.URLs[0].Status != URLStatusUnusable {
		t.Errorf("an unusable URL can never be healed: %+v", c)
	}
	for _, st := range []SetWatchStatus{a, b, c} {
		if st.ConsecutiveFailures != 0 {
			t.Errorf("set %s: nothing here counts as a failure: %d", st.SetId, st.ConsecutiveFailures)
		}
	}
	if q := h.queue(); len(q) != 0 {
		t.Fatalf("nothing may be queued for healing: %v", q)
	}
	if len(h.driver.starts) != 0 {
		t.Fatal("no discovery run may start")
	}
	if h.check.callsFor(ytURL) != 0 || h.check.callsFor(videoURL) != 0 {
		t.Error("a URL that is not owned or is escalated is never fetched")
	}
}

func TestFailuresQueueAHealAtMaxRetries(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL, videoURL))
	h.check.set(ytURL, okCheck)
	h.check.set(videoURL, failCheck)
	h.driver.setActive(true)

	h.w.tick()
	st := h.state("yt")
	if st.Status != SetStatusDegraded || st.ConsecutiveFailures != 1 || st.Interval != 60 {
		t.Fatalf("one failed URL degrades the set and shortens the interval: %+v", st)
	}
	if !strings.Contains(st.LastError, "i.ytimg.com") {
		t.Errorf("the error names the failing host: %q", st.LastError)
	}

	h.clock.Advance(30 * time.Second)
	h.w.tick()
	if h.state("yt").ConsecutiveFailures != 1 {
		t.Fatal("a check is not repeated before the failure interval")
	}

	h.clock.Advance(31 * time.Second)
	h.w.tick()
	st = h.state("yt")
	if st.Status != SetStatusHealQueued || st.ConsecutiveFailures != 2 {
		t.Fatalf("at max_retries the set is queued for healing: %+v", st)
	}
	if q := h.queue(); len(q) != 1 || q[0] != "yt" {
		t.Fatalf("queue = %v", q)
	}

	h.clock.Advance(10 * time.Minute)
	h.w.tick()
	if q := h.queue(); len(q) != 1 {
		t.Fatalf("a queued set is not queued twice: %v", q)
	}
	if h.check.callsFor(ytURL) != 2 {
		t.Errorf("a set waiting to heal is not checked again, got %d checks", h.check.callsFor(ytURL))
	}
}

func TestHealQueueWaitsWhileTheRuntimeIsBusy(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.check.set(ytURL, failCheck)
	h.driver.setActive(true)
	h.tickUntilQueued("yt")

	h.w.tick()
	st := h.state("yt")
	if st.Status != SetStatusHealQueued || st.Reason != ReasonBusy {
		t.Fatalf("while a user run holds the runtime the heal waits and says why: %+v", st)
	}
	if len(h.driver.starts) != 0 {
		t.Fatal("no run may start while the runtime is busy")
	}

	h.driver.verdict = coveredVerdict("disorder")
	h.check.set(ytURL, okCheck)
	h.driver.setActive(false)
	h.w.pumpHealQueue()
	h.w.healWG.Wait()

	if len(h.driver.starts) != 1 {
		t.Fatalf("the heal starts once the runtime is free, got %d starts", len(h.driver.starts))
	}
	if st := h.state("yt"); st.Status != SetStatusHealthy {
		t.Fatalf("the heal ran: %+v", st)
	}
	if h.w.healing.Load() {
		t.Error("the single-flight flag is released after the heal")
	}
}

func TestAlreadyRunningRequeuesAtTheHeadWithoutCounting(t *testing.T) {
	h := newHarness(t, watchedSet("a", ytURL), watchedSet("b", otherURL))
	h.w.mu.Lock()
	h.w.syncSetStatesLocked(h.ptr.Load())
	h.w.setStates["b"].Status = SetStatusHealQueued
	h.w.healQueue = []string{"b"}
	h.w.mu.Unlock()

	h.driver.startErr = discovery.ErrDiscoveryAlreadyRunning
	h.healNow("a")

	st := h.state("a")
	if st.Status != SetStatusHealQueued || st.Reason != ReasonBusy || st.HealFailures != 0 {
		t.Fatalf("a lost race for the runtime is not a failure: %+v", st)
	}
	if q := h.queue(); len(q) != 2 || q[0] != "a" {
		t.Fatalf("the set goes back to the head of the queue: %v", q)
	}

	h.driver.startErr = errors.New("nfqueue is not available")
	h.healNow("a")
	st = h.state("a")
	if st.Reason != ReasonStartFailed || st.HealFailures != 1 || st.Status != SetStatusCooldown {
		t.Fatalf("any other start error is a heal failure: %+v", st)
	}
}

func TestCoveredVerdictIsAdoptedAndVerified(t *testing.T) {
	yt := watchedSet("yt", ytURL, videoURL)
	yt.TCP.DPortFilter = "443,2053"
	yt.Targets.TLSVersion = "1.3"
	h := newHarness(t, yt)
	h.engine.escalated["www.youtube.com"] = ""
	h.check.set(ytURL, okCheck)
	h.check.set(videoURL, okCheck)
	h.driver.verdict = coveredVerdict("disorder")
	h.driver.runningFor = 2

	h.healNow("yt")

	if len(h.driver.starts) != 1 {
		t.Fatalf("one run, got %d", len(h.driver.starts))
	}
	opts := h.driver.starts[0]
	if opts.SetId != "yt" || !opts.StopWhenCovered || !opts.SkipDNS || opts.SetStrategy == nil || opts.Source != discovery.SourceWatchdog || opts.TLSVersion != "tls13" {
		t.Errorf("the run is a set run for this set: %+v", opts)
	}
	if got := h.driver.startURLs[0]; len(got) != 2 {
		t.Errorf("every owned URL is probed: %v", got)
	}

	set := h.set("yt")
	if set.Fragmentation.Strategy != "disorder" || set.Faking.TTL != 7 {
		t.Errorf("the covered strategy is written into the set: %s ttl %d", set.Fragmentation.Strategy, set.Faking.TTL)
	}
	if set.TCP.DPortFilter != "443,2053" || len(set.Discovery.URLs) != 2 || !set.Discovery.Watchdog {
		t.Errorf("only the strategy changes: %+v %+v", set.TCP.DPortFilter, set.Discovery)
	}
	if h.saves.Load() != 1 {
		t.Errorf("one save, got %d", h.saves.Load())
	}

	st := h.state("yt")
	if st.Status != SetStatusHealthy || st.LastHealPreset != "preset-disorder" || st.LastHeal.IsZero() || st.HealFailures != 0 {
		t.Fatalf("a verified heal leaves the set healthy: %+v", st)
	}
	if st.CooldownUntil.Sub(h.clock.Now()) != 900*time.Second {
		t.Errorf("a heal outcome starts the cooldown: %v", st.CooldownUntil)
	}
	h.engine.mu.Lock()
	cleared := strings.Join(h.engine.cleared, ",")
	h.engine.mu.Unlock()
	if !strings.Contains(cleared, "www.youtube.com") || !strings.Contains(cleared, "i.ytimg.com") {
		t.Errorf("escalations for every owned host are cleared before verifying: %q", cleared)
	}
}

func TestVerifyFailureRollsBackOnlyThatSet(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL), watchedSet("tr", otherURL))
	h.check.set(ytURL, failCheck)
	h.driver.verdict = coveredVerdict("disorder")

	var once sync.Once
	h.check.onCheck = func(string) {
		once.Do(func() {
			h.edit("tr", func(s *config.SetConfig) { s.Fragmentation.Strategy = "oob" })
		})
	}

	h.healNow("yt")

	if got := h.set("yt").Fragmentation.Strategy; got != "tcp" {
		t.Errorf("a failed verification restores the set's previous strategy, got %q", got)
	}
	if got := h.set("tr").Fragmentation.Strategy; got != "oob" {
		t.Errorf("the rollback must not touch another set, got %q", got)
	}
	if h.saves.Load() != 2 {
		t.Errorf("adopt and rollback are two saves, got %d", h.saves.Load())
	}
	st := h.state("yt")
	if st.Status != SetStatusCooldown || st.Reason != ReasonVerifyFailed || st.HealFailures != 1 {
		t.Fatalf("a failed verification is a heal failure: %+v", st)
	}
	if h.check.callsFor(ytURL) != 2 {
		t.Errorf("verification retries up to verify_tries, got %d", h.check.callsFor(ytURL))
	}
}

func TestNoRollbackWhenTheSetWasEditedAfterTheHeal(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.check.set(ytURL, failCheck)
	h.driver.verdict = coveredVerdict("disorder")

	var once sync.Once
	h.check.onCheck = func(string) {
		once.Do(func() {
			h.edit("yt", func(s *config.SetConfig) { s.TCP.Seg2Delay = 42 })
		})
	}

	h.healNow("yt")

	set := h.set("yt")
	if set.Fragmentation.Strategy != "disorder" || set.TCP.Seg2Delay != 42 {
		t.Errorf("an edit made after the heal must be kept, got %s / %d", set.Fragmentation.Strategy, set.TCP.Seg2Delay)
	}
	if h.saves.Load() != 1 {
		t.Errorf("no rollback save, got %d saves", h.saves.Load())
	}
	if st := h.state("yt"); st.Reason != ReasonVerifyFailed {
		t.Errorf("still a verify failure: %+v", st)
	}
}

func TestAnEditDuringTheRunSkipsTheWrite(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.driver.verdict = coveredVerdict("disorder")
	h.driver.runningFor = 1
	h.driver.onSnapshot = func(n int) {
		if n == 1 {
			h.edit("yt", func(s *config.SetConfig) { s.Fragmentation.Strategy = "oob" })
		}
	}

	h.healNow("yt")

	if h.saves.Load() != 0 {
		t.Fatalf("nothing is written over a set edited during the run, got %d saves", h.saves.Load())
	}
	if got := h.set("yt").Fragmentation.Strategy; got != "oob" {
		t.Errorf("the user's edit stands, got %q", got)
	}
	st := h.state("yt")
	if st.Reason != ReasonEdited || st.HealFailures != 0 || st.Status != SetStatusCooldown {
		t.Fatalf("an edited set is not a failure: %+v", st)
	}
}

func TestVerdictsOtherThanCoveredCountAsFailures(t *testing.T) {
	cases := map[discovery.SetVerdictStatus]string{
		discovery.SetVerdictCurrentWorks: ReasonCurrentWorks,
		discovery.SetVerdictNotNeeded:    ReasonNotNeeded,
		discovery.SetVerdictPartial:      ReasonPartial,
		discovery.SetVerdictNone:         ReasonNone,
		discovery.SetVerdictIncomplete:   ReasonIncomplete,
	}
	for status, reason := range cases {
		t.Run(string(status), func(t *testing.T) {
			h := newHarness(t, watchedSet("yt", ytURL))
			h.driver.verdict = &discovery.SetVerdict{Status: status, WinnerPreset: "p"}
			h.healNow("yt")
			st := h.state("yt")
			if st.Reason != reason || st.HealFailures != 1 || st.Status != SetStatusCooldown {
				t.Fatalf("got %+v, want reason %s", st, reason)
			}
			if h.saves.Load() != 0 {
				t.Error("no write for a verdict other than covered")
			}
			if status == discovery.SetVerdictCurrentWorks && !strings.Contains(st.LastError, "DNS or IPv6") {
				t.Errorf("current_works explains the likely cause: %q", st.LastError)
			}
		})
	}

	t.Run("unconfirmed covered", func(t *testing.T) {
		h := newHarness(t, watchedSet("yt", ytURL))
		v := coveredVerdict("disorder")
		v.Confirmed = false
		h.driver.verdict = v
		h.healNow("yt")
		if st := h.state("yt"); st.Reason != ReasonIncomplete || h.saves.Load() != 0 {
			t.Fatalf("an unconfirmed cover is never written: %+v", st)
		}
	})
}

func TestGiveUpAfterThreeHealFailuresAndItsClearingRules(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.check.set(ytURL, failCheck)
	h.driver.verdict = &discovery.SetVerdict{Status: discovery.SetVerdictNone}

	for i := 1; i <= maxHealFailures; i++ {
		h.tickUntilQueued("yt")
		h.healNow("yt")
		if got := h.state("yt").HealFailures; got != i {
			t.Fatalf("heal failures = %d, want %d", got, i)
		}
		h.clock.Advance(16 * time.Minute)
	}
	st := h.state("yt")
	if st.Status != SetStatusGaveUp || !h.gaveUp("yt") {
		t.Fatalf("three failed heals give up: %+v", st)
	}

	for i := 0; i < 5; i++ {
		h.w.tick()
		h.clock.Advance(10 * time.Minute)
	}
	st = h.state("yt")
	if st.Status != SetStatusGaveUp || len(h.queue()) != 0 || len(h.driver.starts) != maxHealFailures {
		t.Fatalf("a set that gave up is still checked but never healed automatically: %+v queue=%v", st, h.queue())
	}
	if st.ConsecutiveFailures == 0 {
		t.Error("checks continue after giving up")
	}

	h.edit("yt", func(s *config.SetConfig) { s.Fragmentation.Strategy = "oob" })
	h.w.tick()
	if !h.gaveUp("yt") {
		t.Fatal("editing the strategy does not clear gave_up")
	}

	if got := h.w.ForceCheckSet("yt", false); got != ForceCheckScheduled {
		t.Fatalf("a forced check finds the set, got %q", got)
	}
	if !h.gaveUp("yt") || h.state("yt").HealFailures != maxHealFailures {
		t.Fatal("a forced check without write access keeps gave_up and the heal failures")
	}
	if got := h.w.ForceCheckSet("yt", true); got != ForceCheckScheduled {
		t.Fatalf("a forced check finds the set, got %q", got)
	}
	if h.gaveUp("yt") || h.state("yt").HealFailures != 0 {
		t.Fatal("a forced check clears gave_up and the heal failures")
	}

	for i := 1; i <= maxHealFailures; i++ {
		h.tickUntilQueued("yt")
		h.healNow("yt")
		h.clock.Advance(16 * time.Minute)
	}
	if !h.gaveUp("yt") {
		t.Fatal("precondition: gave up again")
	}
	h.check.set(ytURL, okCheck)
	h.w.tick()
	if st := h.state("yt"); st.Status != SetStatusHealthy || h.gaveUp("yt") || st.HealFailures != 0 {
		t.Fatalf("a healthy check clears gave_up: %+v", st)
	}
}

func TestHealBudgetFinishesAt15AndCancelsAt20Minutes(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.driver.runningFor = 1000
	h.driver.onSnapshot = func(int) { h.clock.Advance(time.Minute) }

	h.healNow("yt")

	h.driver.mu.Lock()
	finished, canceled := h.driver.finished, h.driver.canceled
	h.driver.mu.Unlock()
	if finished != 1 {
		t.Errorf("the search is told to finish once at 15 minutes, got %d", finished)
	}
	if canceled != 1 {
		t.Errorf("the run is canceled at 20 minutes, got %d", canceled)
	}
	st := h.state("yt")
	if st.Reason != ReasonBudget || st.HealFailures != 1 || h.saves.Load() != 0 {
		t.Fatalf("an over-budget run is a failure without a write: %+v", st)
	}
}

func TestHealBudgetFinishStillReadsTheConfirmedVerdict(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.check.set(ytURL, okCheck)
	h.driver.runningFor = 16
	h.driver.verdict = coveredVerdict("disorder")
	h.driver.onSnapshot = func(int) { h.clock.Advance(time.Minute) }

	h.healNow("yt")

	if h.driver.finished != 1 || h.driver.canceled != 0 {
		t.Fatalf("finish, not cancel: finished=%d canceled=%d", h.driver.finished, h.driver.canceled)
	}
	if st := h.state("yt"); st.Status != SetStatusHealthy {
		t.Fatalf("what the finished run confirmed is adopted: %+v", st)
	}
}

func TestHealIsCanceledWhenTheSetStopsBeingWatched(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.driver.runningFor = 1000
	h.driver.onSnapshot = func(n int) {
		if n == 2 {
			h.edit("yt", func(s *config.SetConfig) { s.Discovery.Watchdog = false })
		}
	}

	h.healNow("yt")

	if h.driver.canceled != 1 {
		t.Fatalf("the run is canceled, got %d", h.driver.canceled)
	}
	if st := h.state("yt"); st.HealFailures != 0 || h.saves.Load() != 0 {
		t.Fatalf("a canceled heal is not a failure and writes nothing: %+v", st)
	}
}

func TestLegacyListSkipsHostsAWatchedSetCovers(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.edit("yt", func(*config.SetConfig) {})
	next := h.ptr.Load().Clone()
	next.System.Checker.Watchdog.Domains = []string{"www.youtube.com", "rutracker.org"}
	h.ptr.Store(next)
	h.check.set(ytURL, okCheck)
	h.check.set("rutracker.org", okCheck)

	h.w.tick()

	if n := h.check.callsFor("www.youtube.com"); n != 0 {
		t.Errorf("a legacy entry a watched set covers is not checked, got %d", n)
	}
	if n := h.check.callsFor("rutracker.org"); n != 1 {
		t.Errorf("other legacy entries keep working, got %d", n)
	}
	if n := h.check.callsFor(ytURL); n != 1 {
		t.Errorf("the set checks its own URL, got %d", n)
	}
}

func TestGetStateReportsSetsAndLegacyOwners(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	next := h.ptr.Load().Clone()
	next.System.Checker.Watchdog.Domains = []string{"www.youtube.com"}
	h.ptr.Store(next)

	state := h.w.GetState()
	if len(state.Sets) != 1 || state.Sets[0].SetId != "yt" || state.Sets[0].Status != SetStatusQueued {
		t.Fatalf("a watched set is listed before its first check: %+v", state.Sets)
	}
	if len(state.Domains) != 1 || state.Domains[0].OwnerSetId != "yt" || state.Domains[0].OwnerSetName != "yt" {
		t.Fatalf("a legacy entry names the set the engine uses: %+v", state.Domains[0])
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Sets []map[string]any `json:"sets"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"set_id", "set_name", "status", "urls", "consecutive_failures", "heal_failures", "interval_sec"} {
		if _, ok := raw.Sets[0][key]; !ok {
			t.Errorf("sets[0] lacks %q: %v", key, raw.Sets[0])
		}
	}
	for _, key := range []string{"last_check", "last_heal", "cooldown_until", "reason", "last_error"} {
		if _, ok := raw.Sets[0][key]; ok {
			t.Errorf("an unset %q is omitted: %v", key, raw.Sets[0][key])
		}
	}

	off := h.ptr.Load().Clone()
	off.Sets[0].Discovery.Watchdog = false
	h.ptr.Store(off)
	if state := h.w.GetState(); len(state.Sets) != 0 || state.Sets == nil {
		t.Fatalf("an unwatched set is not listed, and sets is never null: %+v", state.Sets)
	}
}

func TestEditingAQueuedSetDropsItsHeal(t *testing.T) {
	h := newHarness(t, watchedSet("yt", ytURL))
	h.check.set(ytURL, failCheck)
	h.driver.setActive(true)
	h.tickUntilQueued("yt")

	h.edit("yt", func(s *config.SetConfig) { s.Fragmentation.Strategy = "oob" })
	h.w.tick()

	if q := h.queue(); len(q) != 0 {
		t.Fatalf("an edited set is checked again before any heal: %v", q)
	}
	if st := h.state("yt"); st.Status == SetStatusHealQueued {
		t.Fatalf("status = %s", st.Status)
	}
}

func TestLegacyCheckRefusesPrivateDestinations(t *testing.T) {
	res := checkDomain(context.Background(), "http://127.0.0.1:9/", markThroughEngine, 2*time.Second)
	if !res.Unusable || res.OK {
		t.Fatalf("a private destination is unusable, never a failure to heal: %+v", res)
	}
}

func TestLegacyUnusableGoesStraightToCooldown(t *testing.T) {
	h := newHarness(t)
	next := h.ptr.Load().Clone()
	next.System.Checker.Watchdog.Domains = []string{"lan.example"}
	next.System.Checker.Watchdog.MaxRetries = 1
	h.ptr.Store(next)
	h.w.checkLegacy = func(_ context.Context, domains []string, _ uint, _ time.Duration) map[string]CheckResult {
		out := map[string]CheckResult{}
		for _, d := range domains {
			out[d] = CheckResult{Error: "10.0.0.7 is a private or local address", Unusable: true}
		}
		return out
	}

	h.w.tick()

	h.w.mu.Lock()
	st := *h.w.domainStates["lan.example"]
	h.w.mu.Unlock()
	if st.ConsecutiveFailures != 0 || st.CooldownUntil.IsZero() {
		t.Fatalf("an unusable legacy entry cools down without counting: %+v", st)
	}
	h.w.healWG.Wait()
	if len(h.driver.starts) != 0 {
		t.Fatal("no heal may start")
	}
}
