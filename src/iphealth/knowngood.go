package iphealth

import (
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxKnownGoodHosts   = 2048
	maxKnownGoodPerHost = 4
	knownGoodTTL        = 24 * time.Hour
)

type knownGoodIP struct {
	ip   net.IP
	seen time.Time
}

type KnownGood struct {
	mu    sync.Mutex
	hosts map[string][]knownGoodIP
}

func NewKnownGood() *KnownGood {
	return &KnownGood{hosts: make(map[string][]knownGoodIP)}
}

func (k *KnownGood) Remember(host string, ip net.IP) {
	if k == nil || host == "" || ip == nil {
		return
	}
	host = normalizeHost(host)

	now := time.Now()
	k.mu.Lock()
	defer k.mu.Unlock()

	entries := k.hosts[host]
	if entries == nil {
		k.evictLocked(now)
		if len(k.hosts) >= maxKnownGoodHosts {
			return
		}
	}

	for i := range entries {
		if entries[i].ip.Equal(ip) {
			entries[i].seen = now
			k.hosts[host] = entries
			return
		}
	}

	entries = append(pruneKnownGood(entries, now), knownGoodIP{ip: append(net.IP(nil), ip...), seen: now})
	if len(entries) > maxKnownGoodPerHost {
		sort.Slice(entries, func(i, j int) bool { return entries[i].seen.After(entries[j].seen) })
		entries = entries[:maxKnownGoodPerHost]
	}
	k.hosts[host] = entries
}

func (k *KnownGood) Lookup(host string, want6 bool) []net.IP {
	if k == nil || host == "" {
		return nil
	}
	host = normalizeHost(host)

	now := time.Now()
	k.mu.Lock()
	defer k.mu.Unlock()

	entries := k.hosts[host]
	if len(entries) == 0 {
		return nil
	}

	candidates := make([]knownGoodIP, 0, len(entries))
	for _, e := range entries {
		if now.Sub(e.seen) > knownGoodTTL {
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

func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

func pruneKnownGood(entries []knownGoodIP, now time.Time) []knownGoodIP {
	live := entries[:0]
	for _, e := range entries {
		if now.Sub(e.seen) <= knownGoodTTL {
			live = append(live, e)
		}
	}
	return live
}

func (k *KnownGood) evictLocked(now time.Time) {
	for host, entries := range k.hosts {
		entries = pruneKnownGood(entries, now)
		if len(entries) == 0 {
			delete(k.hosts, host)
			continue
		}
		k.hosts[host] = entries
	}

	for len(k.hosts) >= maxKnownGoodHosts {
		oldest := ""
		var oldestAt time.Time
		for host, entries := range k.hosts {
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
		delete(k.hosts, oldest)
	}
}

func (k *KnownGood) Cleanup() {
	if k == nil {
		return
	}
	now := time.Now()
	k.mu.Lock()
	defer k.mu.Unlock()
	for host, entries := range k.hosts {
		entries = pruneKnownGood(entries, now)
		if len(entries) == 0 {
			delete(k.hosts, host)
			continue
		}
		k.hosts[host] = entries
	}
}

func (k *KnownGood) Len() int {
	if k == nil {
		return 0
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.hosts)
}
