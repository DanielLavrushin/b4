package nfq

import (
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxGoodIPHosts   = 2048
	maxGoodIPPerHost = 4
	goodIPTTL        = 24 * time.Hour
)

type goodIP struct {
	ip   net.IP
	seen time.Time
}

type goodIPStore struct {
	mu    sync.Mutex
	hosts map[string][]goodIP
}

func newGoodIPStore() *goodIPStore {
	return &goodIPStore{hosts: make(map[string][]goodIP)}
}

func (s *goodIPStore) Store(host string, ip net.IP) {
	if s == nil || host == "" || ip == nil {
		return
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := s.hosts[host]
	if entries == nil {
		s.evictLocked(now)
		if len(s.hosts) >= maxGoodIPHosts {
			return
		}
	}

	for i := range entries {
		if entries[i].ip.Equal(ip) {
			entries[i].seen = now
			s.hosts[host] = entries
			return
		}
	}

	entries = append(pruneGoodIPs(entries, now), goodIP{ip: append(net.IP(nil), ip...), seen: now})
	if len(entries) > maxGoodIPPerHost {
		sort.Slice(entries, func(i, j int) bool { return entries[i].seen.After(entries[j].seen) })
		entries = entries[:maxGoodIPPerHost]
	}
	s.hosts[host] = entries
}

func (s *goodIPStore) Lookup(host string, want6 bool) []net.IP {
	if s == nil || host == "" {
		return nil
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := s.hosts[host]
	if len(entries) == 0 {
		return nil
	}

	candidates := make([]goodIP, 0, len(entries))
	for _, e := range entries {
		if now.Sub(e.seen) > goodIPTTL {
			continue
		}
		if (e.ip.To4() == nil) != want6 {
			continue
		}
		candidates = append(candidates, e)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].seen.After(candidates[j].seen) })

	out := make([]net.IP, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, append(net.IP(nil), c.ip...))
	}
	return out
}

func pruneGoodIPs(entries []goodIP, now time.Time) []goodIP {
	live := entries[:0]
	for _, e := range entries {
		if now.Sub(e.seen) <= goodIPTTL {
			live = append(live, e)
		}
	}
	return live
}

func (s *goodIPStore) evictLocked(now time.Time) {
	for host, entries := range s.hosts {
		entries = pruneGoodIPs(entries, now)
		if len(entries) == 0 {
			delete(s.hosts, host)
			continue
		}
		s.hosts[host] = entries
	}

	for len(s.hosts) >= maxGoodIPHosts {
		oldest := ""
		var oldestAt time.Time
		for host, entries := range s.hosts {
			at := entries[0].seen
			for _, e := range entries[1:] {
				if e.seen.After(at) {
					at = e.seen
				}
			}
			if oldest == "" || at.Before(oldestAt) {
				oldest = host
				oldestAt = at
			}
		}
		if oldest == "" {
			return
		}
		delete(s.hosts, oldest)
	}
}

func (s *goodIPStore) Cleanup() {
	if s == nil {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for host, entries := range s.hosts {
		entries = pruneGoodIPs(entries, now)
		if len(entries) == 0 {
			delete(s.hosts, host)
			continue
		}
		s.hosts[host] = entries
	}
}

func (s *goodIPStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hosts)
}
