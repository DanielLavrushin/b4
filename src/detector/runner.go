package detector

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/google/uuid"
)

var (
	activeSuites = make(map[string]*Suite)
	suitesMu     sync.RWMutex
)

var ErrRunInProgress = errors.New("a detector run is already in progress")

func NewSuite(opts Options, directMark uint, lookup SetLookup) (*Suite, error) {
	suitesMu.Lock()
	defer suitesMu.Unlock()
	for _, other := range activeSuites {
		other.mu.RLock()
		busy := other.Status == StatusRunning || other.Status == StatusPending
		other.mu.RUnlock()
		if busy {
			return nil, ErrRunInProgress
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Suite{
		Id:         uuid.New().String(),
		Status:     StatusPending,
		Options:    normalizeOptions(opts),
		ListsDate:  Lists().ListsDate,
		directMark: directMark,
		ctx:        ctx,
		cancel:     cancel,
		setLookup:  lookup,
	}
	activeSuites[s.Id] = s
	return s, nil
}

func normalizeOptions(o Options) Options {
	if o.Parallel <= 0 {
		o.Parallel = 5
	}
	if o.Parallel > 20 {
		o.Parallel = 20
	}
	if o.IPVersion != "ipv6" && o.IPVersion != "both" {
		o.IPVersion = "ipv4"
	}
	if o.FetchMode != FetchDirect {
		o.FetchMode = FetchBoth
	}
	seen := make(map[Scope]bool)
	var scopes []Scope
	for _, sc := range []Scope{ScopeSites, ScopeDNS, ScopeHosting, ScopeTelegram} {
		for _, want := range o.Scopes {
			if want == sc && !seen[sc] {
				seen[sc] = true
				scopes = append(scopes, sc)
			}
		}
	}
	o.Scopes = scopes
	return o
}

func GetSuite(id string) (*Suite, bool) {
	suitesMu.RLock()
	defer suitesMu.RUnlock()
	s, ok := activeSuites[id]
	return s, ok
}

func RunningSuite() *Suite {
	suitesMu.RLock()
	defer suitesMu.RUnlock()
	for _, s := range activeSuites {
		s.mu.RLock()
		running := s.Status == StatusRunning || s.Status == StatusPending
		s.mu.RUnlock()
		if running {
			return s
		}
	}
	return nil
}

func CancelSuite(id string) bool {
	suitesMu.RLock()
	s, ok := activeSuites[id]
	suitesMu.RUnlock()
	if !ok {
		return false
	}
	s.mu.Lock()
	if s.Status == StatusRunning || s.Status == StatusPending {
		s.Status = StatusCanceled
	}
	s.mu.Unlock()
	s.cancel()
	return true
}

func (s *Suite) canceled() bool {
	return s.ctx.Err() != nil
}

func (s *Suite) MarshalJSON() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	type Alias Suite
	return json.Marshal((*Alias)(s))
}

func (s *Suite) snapshot() *Suite {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &Suite{
		Id: s.Id, Status: s.Status, StartTime: s.StartTime, EndTime: s.EndTime,
		Options: s.Options, Progress: s.Progress, ListsDate: s.ListsDate,
		Network: s.Network, Sites: s.Sites, DNS: s.DNS, Hosting: s.Hosting,
		Telegram: s.Telegram, Verdict: s.Verdict,
	}
}

func (s *Suite) setProgress(phase Scope, current string) {
	s.mu.Lock()
	s.Progress.Phase = phase
	s.Progress.Current = current
	s.mu.Unlock()
}

func (s *Suite) step(n int) {
	s.mu.Lock()
	s.Progress.Done += n
	s.mu.Unlock()
}
