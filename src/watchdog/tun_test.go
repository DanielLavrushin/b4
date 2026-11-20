package watchdog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
)

type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) lines(parts ...string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, line := range strings.Split(b.buf.String(), "\n") {
		matched := true
		for _, part := range parts {
			if !strings.Contains(line, part) {
				matched = false
				break
			}
		}
		if matched && line != "" {
			out = append(out, line)
		}
	}
	return out
}

func captureWatchdogLog(t *testing.T) *logBuffer {
	t.Helper()
	buf := &logBuffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	log.StartCapture(buf)
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
	})
	return buf
}

var errCarrierDown = fmt.Errorf("%w: the TUN engine is not running", discovery.ErrTUNCarrierUnavailable)

func TestATUNRefusedHealGivesUpAtOnceUntilItsSettingChanges(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		err    error
		setup  func(*config.SetConfig)
		lift   func(*config.SetConfig)
	}{
		{
			name:   "ipv6",
			reason: ReasonIPv6UnsupportedTUN,
			err:    discovery.ErrIPv6UnsupportedTUN,
			setup:  func(s *config.SetConfig) { s.Targets.IPVersion = "6" },
			lift:   func(s *config.SetConfig) { s.Targets.IPVersion = "" },
		},
		{
			name:   "dscp",
			reason: ReasonDSCPUnsupportedTUN,
			err:    fmt.Errorf("%w (DSCP 25)", discovery.ErrDSCPUnsupportedTUN),
			setup:  func(s *config.SetConfig) { s.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25} },
			lift:   func(s *config.SetConfig) { s.DSCP.Enabled = false },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := "tun-" + tc.name
			set := watchedSet(id, ytURL)
			tc.setup(set)
			h := newHarness(t, set)
			h.check.set(ytURL, failCheck)
			h.driver.startErr = tc.err
			logs := captureWatchdogLog(t)
			since := newestEventID()

			h.tickUntilQueued(id)
			h.healNow(id)

			st := h.state(id)
			if st.Status != SetStatusGaveUp || st.Reason != tc.reason || st.HealFailures != 0 || !h.gaveUp(id) {
				t.Fatalf("a heal the TUN engine refuses for the set's own settings gives up at once, without counting a failed heal: %+v", st)
			}
			events := watchdogEvents(since, metrics.EventWatchdogGaveUp, id)
			if len(events) != 1 || events[0].Args["reason"] != tc.reason {
				t.Fatalf("giving up raises one watchdog_gave_up event with the reason: %+v", events)
			}
			named := logs.lines("[INFO]", "[WATCHDOG]", fmt.Sprintf("set %q", id), tc.err.Error())
			if len(named) != 1 {
				t.Fatalf("one Info line names the cause, got %q", named)
			}
			if warned := logs.lines("[WARN]", fmt.Sprintf("set %q", id), "heal failed"); len(warned) != 0 {
				t.Fatalf("a refusal that retrying cannot change is no warning: %q", warned)
			}

			for i := 0; i < 6; i++ {
				h.w.tick()
				h.clock.Advance(20 * time.Minute)
			}
			if h.driver.attempts != 1 || len(h.queue()) != 0 {
				t.Fatalf("no retry while the setting stays: %d attempts, queue %v", h.driver.attempts, h.queue())
			}
			if st := h.state(id); st.Status != SetStatusGaveUp || st.Reason != tc.reason || st.ConsecutiveFailures == 0 {
				t.Fatalf("the set stays given up with its reason and its checks go on: %+v", st)
			}
			if events := watchdogEvents(since, metrics.EventWatchdogGaveUp, id); len(events) != 1 {
				t.Fatalf("staying given up raises nothing more: %+v", events)
			}

			h.edit(id, func(s *config.SetConfig) { s.Fragmentation.Strategy = "oob" })
			h.w.tick()
			if !h.gaveUp(id) {
				t.Fatal("an edit that leaves the refused setting alone keeps the set given up")
			}

			h.edit(id, tc.lift)
			h.w.tick()
			if st := h.state(id); h.gaveUp(id) || st.Status == SetStatusGaveUp || st.HealFailures != 0 {
				t.Fatalf("changing the refused setting lets heals run again: %+v", st)
			}

			h.driver.startErr = nil
			h.driver.verdict = coveredVerdict("disorder")
			h.tickUntilQueued(id)
			h.check.set(ytURL, okCheck)
			h.healNow(id)
			if st := h.state(id); h.driver.attempts != 2 || st.Status != SetStatusHealthy {
				t.Fatalf("the next heal runs: %d attempts, %+v", h.driver.attempts, st)
			}
		})
	}
}

func TestAForcedCheckClearsATUNRefusal(t *testing.T) {
	set := watchedSet("tun-forced", ytURL)
	set.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
	h := newHarness(t, set)
	h.check.set(ytURL, failCheck)
	h.driver.startErr = fmt.Errorf("%w (DSCP 25)", discovery.ErrDSCPUnsupportedTUN)
	since := newestEventID()

	h.tickUntilQueued("tun-forced")
	h.healNow("tun-forced")
	if !h.gaveUp("tun-forced") {
		t.Fatalf("precondition: gave up, %+v", h.state("tun-forced"))
	}

	h.w.ForceCheckSet("tun-forced", false)
	if !h.gaveUp("tun-forced") {
		t.Fatal("a forced check without write access keeps the set given up")
	}
	h.w.ForceCheckSet("tun-forced", true)
	if h.gaveUp("tun-forced") {
		t.Fatal("a forced check clears the give-up")
	}

	h.tickUntilQueued("tun-forced")
	h.healNow("tun-forced")
	st := h.state("tun-forced")
	if h.driver.attempts != 2 || st.Status != SetStatusGaveUp || st.Reason != ReasonDSCPUnsupportedTUN || st.HealFailures != 0 {
		t.Fatalf("the forced heal is tried and refused again: %d attempts, %+v", h.driver.attempts, st)
	}
	if events := watchdogEvents(since, metrics.EventWatchdogGaveUp, "tun-forced"); len(events) != 2 {
		t.Fatalf("each give-up raises its event: %+v", events)
	}
}

func TestAHealWithoutTheTUNCarrierCoolsDownWithoutAStrike(t *testing.T) {
	h := newHarness(t, watchedSet("a", ytURL))
	h.check.set(ytURL, failCheck)
	h.driver.startErr = errCarrierDown
	logs := captureWatchdogLog(t)
	since := newestEventID()
	episodes := func() []string {
		return logs.lines("[INFO]", `set "a"`, discovery.ErrTUNCarrierUnavailable.Error())
	}

	for round := 1; round <= maxHealFailures+1; round++ {
		h.tickUntilQueued("a")
		h.healNow("a")
		st := h.state("a")
		if st.Status != SetStatusCooldown || st.Reason != ReasonTUNCarrierUnavailable || st.HealFailures != 0 || h.gaveUp("a") {
			t.Fatalf("round %d: a carrier that cannot start is no failed heal; the set cools down with the reason: %+v", round, st)
		}
		if want := h.clock.Now().Add(900 * time.Second); !st.CooldownUntil.Equal(want) {
			t.Fatalf("round %d: the next try waits for the cooldown: %v, want %v", round, st.CooldownUntil, want)
		}
		if q := h.queue(); len(q) != 0 {
			t.Fatalf("round %d: nothing stays queued while the set cools down: %v", round, q)
		}
		h.clock.Advance(16 * time.Minute)
	}
	if h.driver.attempts != maxHealFailures+1 {
		t.Fatalf("each round tries the heal once: %d attempts", h.driver.attempts)
	}
	if events := watchdogEvents(since, metrics.EventWatchdogGaveUp, "a"); len(events) != 0 {
		t.Fatalf("waiting for the carrier never gives up: %+v", events)
	}
	if got := episodes(); len(got) != 1 {
		t.Fatalf("the wait is logged once per episode, got %q", got)
	}
	if got := logs.lines("[WARN]", `set "a"`, "heal failed"); len(got) != 0 {
		t.Fatalf("waiting for the carrier is no warning: %q", got)
	}

	attempts := h.driver.attempts
	h.check.set(ytURL, okCheck)
	for i := 0; i < 3; i++ {
		h.w.tick()
		h.clock.Advance(2 * time.Minute)
	}
	if st := h.state("a"); st.Status != SetStatusHealthy || h.driver.attempts != attempts {
		t.Fatalf("checks resume after the cooldown, and a set that loads again needs no heal: %d attempts, %+v", h.driver.attempts, st)
	}

	h.check.set(ytURL, failCheck)
	h.tickUntilQueued("a")
	h.healNow("a")
	if got := episodes(); len(got) != 2 {
		t.Fatalf("a new outage after the set loaded again is a new episode, got %q", got)
	}

	h.driver.startErr = nil
	h.driver.verdict = coveredVerdict("disorder")
	h.clock.Advance(16 * time.Minute)
	h.tickUntilQueued("a")
	h.check.set(ytURL, okCheck)
	h.healNow("a")
	if st := h.state("a"); st.Status != SetStatusHealthy {
		t.Fatalf("once the carrier starts the heal runs: %+v", st)
	}

	h.check.set(ytURL, failCheck)
	h.driver.startErr = errCarrierDown
	h.clock.Advance(16 * time.Minute)
	h.tickUntilQueued("a")
	h.healNow("a")
	if got := episodes(); len(got) != 3 {
		t.Fatalf("a new outage after a run that started is a new episode, got %q", got)
	}
}

func TestACarrierWaitLastsAtLeastAMinuteWhateverTheCooldown(t *testing.T) {
	h := newHarness(t, watchedSet("a", ytURL))
	h.editGlobal(func(c *config.Config) { c.System.Checker.Watchdog.Cooldown = 0 })
	h.check.set(ytURL, failCheck)
	h.driver.startErr = errCarrierDown
	h.tickUntilQueued("a")
	h.healNow("a")
	if st, want := h.state("a"), h.clock.Now().Add(carrierRetryFloor); !st.CooldownUntil.Equal(want) {
		t.Fatalf("a cooldown of 0 still waits %v between tries: %v, want %v", carrierRetryFloor, st.CooldownUntil, want)
	}
}

func TestLegacyHealLogsATUNRefusalOnceAtInfo(t *testing.T) {
	h := legacyHarness(t)
	h.w.mu.Lock()
	h.w.domainStates["youtube.com"] = &DomainStatus{Domain: "youtube.com", Status: StatusQueued}
	h.w.mu.Unlock()
	logs := captureWatchdogLog(t)
	refusals := func() []string {
		return logs.lines("[INFO]", "[WATCHDOG]", "youtube.com", "TUN engine")
	}
	warnings := func() []string {
		return logs.lines("[WARN]", "failed to start discovery")
	}

	h.driver.startErr = errCarrierDown
	for i := 0; i < 3; i++ {
		before := time.Now()
		h.w.healBatch([]string{"youtube.com"}, nil)
		h.w.mu.Lock()
		st := *h.w.domainStates["youtube.com"]
		h.w.mu.Unlock()
		if st.Status != StatusDegraded || !st.CooldownUntil.After(before) {
			t.Fatalf("a refused legacy heal keeps its cooldown: %+v", st)
		}
	}
	if h.driver.attempts != 3 {
		t.Fatalf("the legacy heal keeps retrying, got %d attempts", h.driver.attempts)
	}
	if got := refusals(); len(got) != 1 {
		t.Fatalf("one Info line for the refusal, not one per retry: %q", got)
	}
	if got := warnings(); len(got) != 0 {
		t.Fatalf("a TUN refusal is no warning: %q", got)
	}

	h.driver.startErr = fmt.Errorf("%w (DSCP 25)", discovery.ErrDSCPUnsupportedTUN)
	h.w.healBatch([]string{"youtube.com"}, &config.DSCPProbe{Value: 25, IPv4: true, IPv6: true})
	h.w.healBatch([]string{"youtube.com"}, &config.DSCPProbe{Value: 25, IPv4: true, IPv6: true})
	if got := refusals(); len(got) != 2 {
		t.Fatalf("another refusal gets its own line, once: %q", got)
	}

	h.driver.startErr = nil
	h.w.healBatch([]string{"youtube.com"}, nil)
	h.driver.startErr = fmt.Errorf("%w (DSCP 25)", discovery.ErrDSCPUnsupportedTUN)
	h.w.healBatch([]string{"youtube.com"}, &config.DSCPProbe{Value: 25, IPv4: true, IPv6: true})
	if got := refusals(); len(got) != 3 {
		t.Fatalf("a run that started ends the episode, the next refusal is logged again: %q", got)
	}

	h.driver.startErr = errors.New("nfqueue is not available")
	h.w.healBatch([]string{"youtube.com"}, nil)
	h.w.healBatch([]string{"youtube.com"}, nil)
	if got := warnings(); len(got) != 2 {
		t.Fatalf("any other start error keeps its Warn on every retry: %q", got)
	}
}

func TestAnOrdinaryGiveUpIgnoresIPVersionAndDSCPEdits(t *testing.T) {
	h := newHarness(t, watchedSet("plain", ytURL))
	h.check.set(ytURL, failCheck)
	h.driver.verdict = &discovery.SetVerdict{Status: discovery.SetVerdictNone}
	for i := 1; i <= maxHealFailures; i++ {
		h.tickUntilQueued("plain")
		h.healNow("plain")
		h.clock.Advance(16 * time.Minute)
	}
	if !h.gaveUp("plain") {
		t.Fatalf("precondition: gave up after %d failed heals, %+v", maxHealFailures, h.state("plain"))
	}

	h.edit("plain", func(s *config.SetConfig) {
		s.Targets.IPVersion = "4"
		s.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
	})
	h.w.tick()
	if st := h.state("plain"); !h.gaveUp("plain") || st.Status != SetStatusGaveUp || st.HealFailures != maxHealFailures {
		t.Fatalf("only a TUN refusal is lifted by an IP version or DSCP edit: %+v", st)
	}
}
