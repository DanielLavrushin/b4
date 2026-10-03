package metrics

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type eventRec struct {
	id      uint64
	mono    int64
	level   string
	code    string
	args    map[string]string
	message string
}

type eventState struct {
	mu           sync.Mutex
	rev          uint64
	nextID       uint64
	items        []eventRec
	errors       []eventRec
	restoreHour  int64
	restoreID    uint64
	restoreCount int
}

type escState struct {
	mu    sync.Mutex
	rev   uint64
	items []EscalationEntry
}

func (m *MetricsCollector) Event(level, code string, args map[string]string, message string) {
	m.initOnce.Do(m.init)
	mono, _ := m.now()
	e := &m.ev
	e.mu.Lock()
	e.push(eventRec{level: level, code: code, args: maps.Clone(args), message: message, mono: mono})
	e.mu.Unlock()
}

func (m *MetricsCollector) NoteRulesRestored() {
	m.initOnce.Do(m.init)
	mono, _ := m.now()
	hour := floorDiv(mono+m.off.Load(), hourNs)
	e := &m.ev
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.restoreID != 0 && e.restoreHour == hour {
		for i := range e.items {
			if e.items[i].id != e.restoreID {
				continue
			}
			e.restoreCount++
			e.items[i].args = map[string]string{"count": strconv.Itoa(e.restoreCount)}
			e.items[i].message = rulesRestoredMessage(e.restoreCount)
			e.rev++
			return
		}
	}
	e.restoreCount = 1
	e.restoreHour = hour
	e.restoreID = e.push(eventRec{
		level:   LevelInfo,
		code:    EventRulesRestored,
		args:    map[string]string{"count": "1"},
		message: rulesRestoredMessage(1),
		mono:    mono,
	})
}

func rulesRestoredMessage(n int) string {
	if n == 1 {
		return "Firewall rules restored"
	}
	return fmt.Sprintf("Firewall rules restored %d times", n)
}

func (e *eventState) push(r eventRec) uint64 {
	e.nextID++
	r.id = e.nextID
	e.items = pushCapped(e.items, r, EventsKept)
	if r.level == LevelError {
		e.errors = pushCapped(e.errors, r, ErrorEventsKept)
	}
	e.rev++
	return r.id
}

func pushCapped(s []eventRec, r eventRec, keep int) []eventRec {
	if len(s) >= keep {
		n := copy(s, s[len(s)-keep+1:])
		s = s[:n]
	}
	return append(s, r)
}

func (e *eventState) currentRev() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rev
}

func (e *eventState) log(off int64) *EventLog {
	e.mu.Lock()
	defer e.mu.Unlock()
	return &EventLog{Rev: e.rev, Items: newestFirst(e.items, off), Errors: newestFirst(e.errors, off)}
}

func newestFirst(s []eventRec, off int64) []Event {
	out := make([]Event, 0, len(s))
	for i := len(s) - 1; i >= 0; i-- {
		r := &s[i]
		out = append(out, Event{ID: r.id, T: wallMs(r.mono, off), Level: r.level, Code: r.code, Args: r.args, Message: r.message})
	}
	return out
}

func (m *MetricsCollector) storeEscalations(entries []EscalationEntry) {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b EscalationEntry) int {
		if c := b.SetAt.Compare(a.SetAt); c != 0 {
			return c
		}
		return strings.Compare(a.Host, b.Host)
	})
	s := &m.esc
	s.mu.Lock()
	if !slices.EqualFunc(s.items, sorted, sameEscalation) {
		s.items = sorted
		s.rev++
	}
	s.mu.Unlock()
}

func sameEscalation(a, b EscalationEntry) bool {
	return a.Host == b.Host && a.ToSet == b.ToSet && a.Hops == b.Hops && a.SetAt.Equal(b.SetAt) && a.ExpiresAt.Equal(b.ExpiresAt)
}

func (s *escState) relabelled() {
	s.mu.Lock()
	if len(s.items) > 0 {
		s.rev++
	}
	s.mu.Unlock()
}

func (s *escState) currentRev() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rev
}

func (s *escState) list(off int64) *EscalationList {
	s.mu.Lock()
	items := make([]EscalationEntry, len(s.items))
	copy(items, s.items)
	rev := s.rev
	s.mu.Unlock()
	for i := range items {
		items[i].SetAt = relabelTime(items[i].SetAt, off)
		items[i].ExpiresAt = relabelTime(items[i].ExpiresAt, off)
	}
	return &EscalationList{Rev: rev, Items: items}
}
