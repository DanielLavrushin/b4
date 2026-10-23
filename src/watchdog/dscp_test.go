package watchdog

import (
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

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
