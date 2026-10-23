package watchdog

import (
	"fmt"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

func TestSetHealWaitsWhileTheDSCPRulesArePending(t *testing.T) {
	tagged := watchedSet("yt", ytURL)
	tagged.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
	h := newHarness(t, tagged)

	h.driver.startErr = fmt.Errorf("failed to apply discovery steering rules: %w", discovery.ErrDSCPRulesPending)
	h.healNow("yt")

	st := h.state("yt")
	if st.Status != SetStatusHealQueued || st.Reason != ReasonRulesChanged || st.HealFailures != 0 {
		t.Fatalf("no search ran while b4 was still applying its DSCP rules, so the heal waits without counting a failure: %+v", st)
	}
	if q := h.queue(); len(q) != 1 || q[0] != "yt" {
		t.Errorf("the set is queued again: %v", q)
	}
}

func TestSetHealWaitingForDSCPRulesLetsTheOtherSetsGoFirst(t *testing.T) {
	tagged := watchedSet("yt", ytURL)
	tagged.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
	plain := watchedSet("tr", otherURL)
	h := newHarness(t, tagged, plain)
	h.w.mu.Lock()
	h.w.syncSetStatesLocked(h.ptr.Load())
	h.w.setStates["tr"].Status = SetStatusHealQueued
	h.w.enqueueHealLocked("tr", false)
	h.w.mu.Unlock()

	h.driver.startErr = fmt.Errorf("failed to apply discovery steering rules: %w", discovery.ErrDSCPRulesPending)
	h.healNow("yt")
	if q := h.queue(); len(q) != 2 || q[0] != "tr" || q[1] != "yt" {
		t.Errorf("a set that waits for its DSCP rules goes behind the others, whose runs can start meanwhile: %v", q)
	}

	h.driver.startErr = discovery.ErrDiscoveryAlreadyRunning
	h.healNow("yt")
	if q := h.queue(); len(q) != 2 || q[0] != "yt" {
		t.Errorf("a set that only found the runtime busy keeps its place at the front: %v", q)
	}
}

func TestLegacyHealWritesNothingFromARunWhoseRulesChanged(t *testing.T) {
	h := legacyHarness(t)
	h.driver.rulesLost = true

	h.w.healBatch([]string{"youtube.com"}, &config.DSCPProbe{Value: 25, IPv4: true, IPv6: true})

	if got := h.set("yt").Fragmentation.Strategy; got != "tcp" {
		t.Errorf("the run's confirmed winner was found while b4 rewrote its DSCP rules and must not be written, the set's strategy became %q", got)
	}
	if n := h.saves.Load(); n != 0 {
		t.Errorf("nothing may be saved, got %d saves", n)
	}
}

func TestSetHealStoppedByADSCPRewriteWritesNothing(t *testing.T) {
	tagged := watchedSet("yt", ytURL)
	tagged.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
	h := newHarness(t, tagged)
	h.check.set(ytURL, okCheck)
	h.driver.verdict = coveredVerdict("disorder")
	h.driver.rulesLost = true

	h.healNow("yt")

	st := h.state("yt")
	if st.Reason != ReasonRulesChanged || st.HealFailures != 1 {
		t.Fatalf("a run stopped by a rules change must say so, and it still counts toward giving up on a firewall that keeps changing: %+v", st)
	}
	if got := h.set("yt").Fragmentation.Strategy; got != "tcp" {
		t.Errorf("results from a run whose path changed must not be written, the set's strategy became %q", got)
	}
}

func TestSetHealProbesWithTheSetsDSCP(t *testing.T) {
	tagged := watchedSet("yt", ytURL)
	tagged.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
	plain := watchedSet("plain", otherURL)
	h := newHarness(t, tagged, plain)
	h.check.set(ytURL, okCheck)
	h.check.set(otherURL, okCheck)
	h.driver.verdict = coveredVerdict("disorder")

	h.healNow("yt")
	h.healNow("plain")

	if len(h.driver.starts) != 2 {
		t.Fatalf("two heals, two runs; got %d", len(h.driver.starts))
	}
	if got := h.driver.starts[0].DSCP; got == nil || got.Value != 25 || !got.IPv4 || !got.IPv6 {
		t.Errorf("the set's traffic leaves with DSCP 25, so its heal must search on that path too, got %+v", got)
	}
	if got := h.driver.starts[1].DSCP; got != nil {
		t.Errorf("a set without a DSCP value is healed on the default path, got %+v", *got)
	}
}

func TestLegacyHealProbesEachDSCPPathInARunOfItsOwn(t *testing.T) {
	h := legacyHarness(t)
	h.edit("yt", func(s *config.SetConfig) { s.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25} })
	h.engine.owners["youtube.com"] = "yt"
	h.engine.owners["www.youtube.com"] = "yt"
	h.engine.owners["rutracker.org"] = "tr"

	group, dscp, later := h.w.legacyHealGroup(h.ptr.Load(), []string{"youtube.com", "rutracker.org", "https://www.youtube.com/", "unowned.example"})
	if !slices.Equal(group, []string{"youtube.com", "https://www.youtube.com/"}) || dscp == nil || dscp.Value != 25 {
		t.Fatalf("the first domain's set leaves with DSCP 25, so the run takes every domain on that path: group %v, dscp %+v", group, dscp)
	}
	if !slices.Equal(later, []string{"rutracker.org", "unowned.example"}) {
		t.Errorf("domains on the default path wait for a run of their own: %v", later)
	}

	group, dscp, later = h.w.legacyHealGroup(h.ptr.Load(), []string{"rutracker.org", "unowned.example", "youtube.com"})
	if !slices.Equal(group, []string{"rutracker.org", "unowned.example"}) || dscp != nil || !slices.Equal(later, []string{"youtube.com"}) {
		t.Errorf("a set without a value and a domain no set owns share the default path: group %v, dscp %v, later %v", group, dscp, later)
	}

	probe := &config.DSCPProbe{Value: 25, IPv4: true, IPv6: true}
	h.w.healBatch([]string{"youtube.com"}, probe)
	if len(h.driver.starts) != 1 {
		t.Fatalf("one run, got %d", len(h.driver.starts))
	}
	if got := h.driver.starts[0].DSCP; got == nil || *got != *probe {
		t.Errorf("the run must carry the group's DSCP value, got %+v", got)
	}
}
