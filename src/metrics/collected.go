package metrics

import (
	"cmp"
	"slices"
	"strings"
	"sync"
)

type CollectedEntry struct {
	IP    string `json:"ip"`
	Host  string `json:"host"`
	Set   string `json:"set"`
	Since int64  `json:"since"`
	Until int64  `json:"until"`
}

type CollectedList struct {
	Rev     uint64           `json:"rev"`
	Items   []CollectedEntry `json:"items"`
	Refused uint64           `json:"refused"`
}

type collectedState struct {
	mu      sync.Mutex
	rev     uint64
	items   []CollectedEntry
	refused uint64
}

func (m *MetricsCollector) UpdateCollectedRefused(refused uint64) {
	s := &m.collected
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refused != refused {
		s.refused = refused
		s.rev++
	}
}

func (m *MetricsCollector) UpdateCollectedIPs(entries []CollectedEntry) {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b CollectedEntry) int {
		if c := cmp.Compare(b.Since, a.Since); c != 0 {
			return c
		}
		return strings.Compare(a.IP, b.IP)
	})
	s := &m.collected
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Equal(s.items, sorted) {
		s.items = sorted
		s.rev++
	}
}

func (s *collectedState) currentRev() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rev
}

func (s *collectedState) list() *CollectedList {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]CollectedEntry, len(s.items))
	copy(items, s.items)
	return &CollectedList{Rev: s.rev, Items: items, Refused: s.refused}
}
