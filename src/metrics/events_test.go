package metrics

import (
	"fmt"
	"testing"
	"time"
)

func TestEventsKeepFiftyNewestFirst(t *testing.T) {
	r := newRig(t, rigStart, 0)
	for i := 1; i <= 60; i++ {
		r.m.Event(LevelInfo, EventSettingsApplied, map[string]string{"sets": fmt.Sprint(i)}, fmt.Sprintf("event %d", i))
	}
	log := r.m.Hello().Events
	if len(log.Items) != EventsKept {
		t.Fatalf("kept %d events, want %d", len(log.Items), EventsKept)
	}
	if log.Items[0].ID != 60 || log.Items[0].Message != "event 60" || log.Items[EventsKept-1].ID != 11 {
		t.Fatalf("items must be newest first with monotonic ids: first %+v last %+v", log.Items[0], log.Items[EventsKept-1])
	}
	if log.Items[0].Args["sets"] != "60" || log.Items[0].Code != EventSettingsApplied || log.Items[0].Level != LevelInfo {
		t.Fatalf("event fields %+v", log.Items[0])
	}
	if len(log.Errors) != 0 {
		t.Fatalf("info events never land in errors: %+v", log.Errors)
	}
	if log.Rev != 60 {
		t.Fatalf("rev must move on every change, got %d", log.Rev)
	}
}

func TestErrorEventsSurviveInfoFloodsAndReset(t *testing.T) {
	r := newRig(t, rigStart, 0)
	r.m.Event(LevelError, EventSOCKS5Failed, map[string]string{"error": "bind: address in use"}, "Failed to start SOCKS5 server: bind: address in use")
	r.m.Event(LevelWarning, EventWatchdogGaveUp, map[string]string{"set_id": "video", "set": "Video", "reason": "no strategy works"}, "Watchdog gave up on Video")
	r.m.Event(LevelError, EventWebTLSUnusable, nil, "tls")
	for i := 0; i < 100; i++ {
		r.m.Event(LevelInfo, EventSettingsApplied, map[string]string{"sets": "3"}, "Settings applied")
	}
	r.m.ResetCounters()
	log := r.m.Hello().Events
	if len(log.Errors) != 2 || log.Errors[0].Code != EventWebTLSUnusable || log.Errors[1].Code != EventSOCKS5Failed {
		t.Fatalf("errors must survive info events and reset, newest first: %+v", log.Errors)
	}
	for _, e := range log.Items {
		if e.Level != LevelInfo {
			t.Fatalf("the info flood must have pushed older events out of items: %+v", e)
		}
	}

	for i := 0; i < 15; i++ {
		r.m.Event(LevelError, EventTargetsWarning, map[string]string{"error": fmt.Sprint(i)}, "warning")
	}
	log = r.m.Hello().Events
	if len(log.Errors) != ErrorEventsKept || log.Errors[0].Args["error"] != "14" || log.Errors[ErrorEventsKept-1].Args["error"] != "5" {
		t.Fatalf("errors keep the %d newest: %+v", ErrorEventsKept, log.Errors)
	}
}

func TestEventTimesAreWallClock(t *testing.T) {
	r := newRig(t, rigStart, 0)
	r.step(90 * time.Second)
	r.clk.advance(400 * time.Millisecond)
	r.m.Event(LevelInfo, EventStarted, map[string]string{"version": "1.85.0", "engine": EngineModeNFQueue, "threads": "4"}, "B4 is fully operational")
	e := r.m.Hello().Events.Items[0]
	if e.T != ms(rigStart.Add(90400*time.Millisecond)) {
		t.Fatalf("event time %d, want %d", e.T, ms(rigStart.Add(90400*time.Millisecond)))
	}
}

func TestEventArgsAreCopied(t *testing.T) {
	r := newRig(t, rigStart, 0)
	args := map[string]string{"path": "queue.threads"}
	r.m.Event(LevelInfo, EventMCPWrite, args, "MCP write")
	args["path"] = "changed"
	if got := r.m.Hello().Events.Items[0].Args["path"]; got != "queue.threads" {
		t.Fatalf("the collector must not share the caller's map, got %q", got)
	}
}

func TestRulesRestoredCoalescesPerWallClockHour(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 10, 15, 0, 0, time.UTC), 0)
	r.m.NoteRulesRestored()
	rev1 := r.m.Hello().Events.Rev
	r.step(5 * time.Minute)
	r.m.Event(LevelInfo, EventSettingsApplied, nil, "Settings applied")
	r.m.NoteRulesRestored()
	r.step(30 * time.Minute)
	r.m.NoteRulesRestored()

	log := r.m.Hello().Events
	if len(log.Items) != 2 {
		t.Fatalf("restores in one hour are one row, got %+v", log.Items)
	}
	restored := log.Items[1]
	if restored.Code != EventRulesRestored || restored.Args["count"] != "3" || restored.Message != "Firewall rules restored 3 times" {
		t.Fatalf("coalesced event %+v", restored)
	}
	if restored.T != ms(time.Date(2026, 10, 3, 10, 15, 0, 0, time.UTC)) {
		t.Fatalf("the row keeps the time of the first restore in the hour, got %d", restored.T)
	}
	if log.Rev != rev1+3 {
		t.Fatalf("every restore bumps rev: %d -> %d", rev1, log.Rev)
	}

	r.at(time.Date(2026, 10, 3, 11, 2, 0, 0, time.UTC))
	r.m.NoteRulesRestored()
	log = r.m.Hello().Events
	if len(log.Items) != 3 || log.Items[0].Code != EventRulesRestored || log.Items[0].Args["count"] != "1" || log.Items[0].Message != "Firewall rules restored" {
		t.Fatalf("a new hour starts a new row: %+v", log.Items)
	}
	if log.Items[2].Args["count"] != "3" {
		t.Fatalf("the previous hour's row keeps its count: %+v", log.Items[2])
	}
}

func TestRulesRestoredStartsAFreshRowOnceTheOldOneIsEvicted(t *testing.T) {
	r := newRig(t, time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC), 0)
	r.m.NoteRulesRestored()
	for i := 0; i < EventsKept; i++ {
		r.m.Event(LevelInfo, EventSettingsApplied, nil, "settings applied")
	}
	r.m.NoteRulesRestored()
	items := r.m.Hello().Events.Items
	if items[0].Code != EventRulesRestored || items[0].Args["count"] != "1" {
		t.Fatalf("got %+v", items[0])
	}
}

func TestEscalationRevMovesOnlyWhenTheContentChanges(t *testing.T) {
	r := newRig(t, rigStart, 0)
	setAt := time.Now()
	a := EscalationEntry{Host: "a.example", ToSet: "Fallback", Hops: 1, SetAt: setAt, ExpiresAt: setAt.Add(time.Hour)}
	b := EscalationEntry{Host: "b.example", ToSet: "Fallback", Hops: 2, SetAt: setAt.Add(time.Second), ExpiresAt: setAt.Add(time.Hour)}

	r.m.UpdateEscalations([]EscalationEntry{a, b})
	rev := r.m.Hello().Escalations.Rev
	r.m.UpdateEscalations([]EscalationEntry{b, a})
	r.m.UpdateEscalations([]EscalationEntry{a, b})
	list := r.m.Hello().Escalations
	if list.Rev != rev {
		t.Fatalf("the same entries in another order are no change: %d -> %d", rev, list.Rev)
	}
	if len(list.Items) != 2 || list.Items[0].Host != "b.example" {
		t.Fatalf("items newest first: %+v", list.Items)
	}

	b.Hops = 3
	r.m.UpdateEscalations([]EscalationEntry{a, b})
	if got := r.m.Hello().Escalations.Rev; got != rev+1 {
		t.Fatalf("a changed entry bumps rev once: %d -> %d", rev, got)
	}
	r.m.UpdateEscalations([]EscalationEntry{})
	list = r.m.Hello().Escalations
	if list.Rev != rev+2 || len(list.Items) != 0 || list.Items == nil {
		t.Fatalf("an empty push clears the list: %+v", list)
	}
}

func TestEscalationListIsResentAfterAClockStep(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	var sent SentRevs
	r.clk.jump(time.Hour)
	r.step(time.Second)
	if fr := m.Tick(&sent); fr.Escalations != nil {
		t.Fatal("a clock step with no escalations has nothing to resend")
	}

	setAt := time.Now()
	m.UpdateEscalations([]EscalationEntry{{Host: "a.example", ToSet: "Fallback", Hops: 1, SetAt: setAt, ExpiresAt: setAt.Add(time.Hour)}})
	first := m.Tick(&sent).Escalations
	if first == nil {
		t.Fatal("a new list goes out on the next tick")
	}
	r.step(time.Second)
	if fr := m.Tick(&sent); fr.Escalations != nil {
		t.Fatal("an unchanged list is not resent")
	}

	r.clk.jump(2 * time.Hour)
	r.step(time.Second)
	fr := m.Tick(&sent)
	if fr.Escalations == nil {
		t.Fatal("after a wall-clock step the list must go out again, relabelled to the new clock")
	}
	if d := fr.Escalations.Items[0].ExpiresAt.Sub(first.Items[0].ExpiresAt); d != 2*time.Hour {
		t.Fatalf("expires_at moved %v, want the 2h step", d)
	}
	if fr.Escalations.Rev <= first.Rev {
		t.Fatalf("the resent list needs a newer rev for clients to take it: %d after %d", fr.Escalations.Rev, first.Rev)
	}
	r.step(time.Second)
	if fr = m.Tick(&sent); fr.Escalations != nil {
		t.Fatal("one step resends the list once")
	}

	r.clk.jump(50 * time.Millisecond)
	r.step(time.Second)
	if fr = m.Tick(&sent); fr.Escalations != nil {
		t.Fatal("clock noise below the slack does not resend the list")
	}
}

func TestEscalationTimesFollowAClockJump(t *testing.T) {
	r := newRig(t, rigStart, 0)
	setAt := time.Now()
	r.m.UpdateEscalations([]EscalationEntry{{Host: "a.example", SetAt: setAt, ExpiresAt: setAt.Add(time.Hour)}})
	before := r.m.Hello().Escalations.Items[0]
	r.clk.jump(48 * time.Hour)
	r.step(time.Second)
	after := r.m.Hello().Escalations.Items[0]
	if d := after.ExpiresAt.Sub(before.ExpiresAt); d != 48*time.Hour {
		t.Fatalf("expires_at must move with the wall clock like every other time, moved %v", d)
	}
	if d := after.ExpiresAt.Sub(after.SetAt); d != time.Hour {
		t.Fatalf("the ttl is kept: %v", d)
	}
}
