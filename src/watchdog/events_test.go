package watchdog

import (
	"testing"
	"time"

	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/metrics"
)

func newestEventID() uint64 {
	if items := metrics.GetMetricsCollector().Hello().Events.Items; len(items) > 0 {
		return items[0].ID
	}
	return 0
}

func watchdogEvents(since uint64, code, setID string) []metrics.Event {
	var out []metrics.Event
	for _, ev := range metrics.GetMetricsCollector().Hello().Events.Items {
		if ev.ID > since && ev.Code == code && ev.Args["set_id"] == setID {
			out = append(out, ev)
		}
	}
	return out
}

func TestAVerifiedHealRaisesWatchdogHealed(t *testing.T) {
	h := newHarness(t, watchedSet("ev-heal", ytURL))
	h.check.set(ytURL, okCheck)
	h.driver.verdict = coveredVerdict("disorder")
	since := newestEventID()

	h.healNow("ev-heal")

	events := watchdogEvents(since, metrics.EventWatchdogHealed, "ev-heal")
	if len(events) != 1 {
		t.Fatalf("one heal, one event: %+v", events)
	}
	ev := events[0]
	if ev.Level != metrics.LevelInfo || ev.Args["set"] != "ev-heal" || ev.Args["preset"] != "preset-disorder" {
		t.Fatalf("unexpected watchdog_healed event: %+v", ev)
	}
}

func TestAFailedHealRaisesNoHealedEvent(t *testing.T) {
	h := newHarness(t, watchedSet("ev-fail", ytURL))
	h.check.set(ytURL, failCheck)
	h.driver.verdict = coveredVerdict("disorder")
	since := newestEventID()

	h.healNow("ev-fail")

	if events := watchdogEvents(since, metrics.EventWatchdogHealed, "ev-fail"); len(events) != 0 {
		t.Fatalf("a heal that did not survive verification is not a heal: %+v", events)
	}
}

func TestGivingUpRaisesOneWatchdogGaveUp(t *testing.T) {
	h := newHarness(t, watchedSet("ev-gaveup", ytURL))
	h.check.set(ytURL, failCheck)
	h.driver.verdict = &discovery.SetVerdict{Status: discovery.SetVerdictNone}
	since := newestEventID()

	for i := 1; i <= maxHealFailures; i++ {
		h.tickUntilQueued("ev-gaveup")
		h.healNow("ev-gaveup")
		h.clock.Advance(16 * time.Minute)
		if i < maxHealFailures && len(watchdogEvents(since, metrics.EventWatchdogGaveUp, "ev-gaveup")) != 0 {
			t.Fatalf("heal failure %d of %d does not give up yet", i, maxHealFailures)
		}
	}
	events := watchdogEvents(since, metrics.EventWatchdogGaveUp, "ev-gaveup")
	if len(events) != 1 {
		t.Fatalf("entering gave_up raises one event: %+v", events)
	}
	ev := events[0]
	if ev.Level != metrics.LevelWarning || ev.Args["set"] != "ev-gaveup" || ev.Args["reason"] != ReasonNone {
		t.Fatalf("unexpected watchdog_gave_up event: %+v", ev)
	}

	for i := 0; i < 3; i++ {
		h.w.tick()
		h.clock.Advance(10 * time.Minute)
	}
	if events := watchdogEvents(since, metrics.EventWatchdogGaveUp, "ev-gaveup"); len(events) != 1 {
		t.Fatalf("staying in gave_up raises nothing more: %+v", events)
	}
}
