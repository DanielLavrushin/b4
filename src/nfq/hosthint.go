package nfq

import (
	"net"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
)

const (
	maxHostHintEntries    = 4096
	maxHostHintCandidates = 4
	hostHintTTL           = 120 * time.Second
)

type hostHintCandidate struct {
	setId   string
	host    string
	expires time.Time
}

type hostHintEntry struct {
	candidates []hostHintCandidate
}

type hostHintCache struct {
	mu   sync.Mutex
	keys map[string]*hostHintEntry
}

func newHostHintCache() *hostHintCache {
	return &hostHintCache{keys: make(map[string]*hostHintEntry)}
}

func hostHintKey(clientIP, destIP string) string {
	return clientIP + "|" + destIP
}

func (c *hostHintCache) Store(clientIP, destIP, setId, host string) {
	if c == nil || clientIP == "" || destIP == "" || setId == "" {
		return
	}

	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	key := hostHintKey(clientIP, destIP)
	entry := c.keys[key]
	if entry == nil {
		c.evictLocked(now)
		if len(c.keys) >= maxHostHintEntries {
			return
		}
		entry = &hostHintEntry{}
		c.keys[key] = entry
	}

	for i := range entry.candidates {
		if entry.candidates[i].setId == setId && entry.candidates[i].host == host {
			entry.candidates[i].expires = now.Add(hostHintTTL)
			return
		}
	}

	entry.candidates = append(pruneCandidates(entry.candidates, now), hostHintCandidate{
		setId:   setId,
		host:    host,
		expires: now.Add(hostHintTTL),
	})
	if len(entry.candidates) > maxHostHintCandidates {
		entry.candidates = entry.candidates[len(entry.candidates)-maxHostHintCandidates:]
	}
}

func (c *hostHintCache) Lookup(clientIP, destIP string) (string, string, bool) {
	if c == nil {
		return "", "", false
	}

	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	key := hostHintKey(clientIP, destIP)
	entry := c.keys[key]
	if entry == nil {
		return "", "", false
	}

	entry.candidates = pruneCandidates(entry.candidates, now)
	if len(entry.candidates) == 0 {
		delete(c.keys, key)
		return "", "", false
	}

	setId := entry.candidates[0].setId
	host := entry.candidates[0].host
	for _, candidate := range entry.candidates[1:] {
		if candidate.setId != setId {
			log.Tracef("host hint for %s -> %s is ambiguous between sets %s and %s, ignoring",
				clientIP, destIP, setId, candidate.setId)
			return "", "", false
		}
	}

	return setId, host, true
}

func pruneCandidates(candidates []hostHintCandidate, now time.Time) []hostHintCandidate {
	live := candidates[:0]
	for _, candidate := range candidates {
		if now.Before(candidate.expires) {
			live = append(live, candidate)
		}
	}
	return live
}

func (c *hostHintCache) evictLocked(now time.Time) {
	if len(c.keys) < maxHostHintEntries {
		return
	}

	for key, entry := range c.keys {
		entry.candidates = pruneCandidates(entry.candidates, now)
		if len(entry.candidates) == 0 {
			delete(c.keys, key)
		}
	}

	for len(c.keys) >= maxHostHintEntries {
		var oldestKey string
		var oldestAt time.Time
		for key, entry := range c.keys {
			at := entry.candidates[0].expires
			for _, candidate := range entry.candidates[1:] {
				if candidate.expires.Before(at) {
					at = candidate.expires
				}
			}
			if oldestAt.IsZero() || at.Before(oldestAt) {
				oldestKey = key
				oldestAt = at
			}
		}
		if oldestKey == "" {
			return
		}
		delete(c.keys, oldestKey)
	}
}

func (c *hostHintCache) Cleanup() {
	if c == nil {
		return
	}

	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.keys {
		entry.candidates = pruneCandidates(entry.candidates, now)
		if len(entry.candidates) == 0 {
			delete(c.keys, key)
		}
	}
}

func (w *Worker) storeHostHints(clientIP net.IP, set *config.SetConfig, host string, ips []net.IP) {
	if w == nil || clientIP == nil || set == nil || set.Id == "" || len(ips) == 0 {
		return
	}

	client := clientIP.String()
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		w.hostHints.Store(client, ip.String(), set.Id, host)
	}
	log.Tracef("host hint: %s -> %d address(es) for %s (set: %s)", client, len(ips), host, set.Name)
}

func (w *Worker) lookupHostHint(cfg *config.Config, clientIP, destIP, srcMac string) (*config.SetConfig, string) {
	if w == nil || cfg == nil {
		return nil, ""
	}

	setId, host, ok := w.hostHints.Lookup(clientIP, destIP)
	if !ok {
		return nil, ""
	}

	set := cfg.GetSetById(setId)
	if set == nil || !set.Enabled {
		return nil, ""
	}
	if set.Targets.DomainOnly {
		log.Tracef("host hint for %s -> %s names domain-only set %s, not applied", clientIP, destIP, set.Name)
		return nil, ""
	}
	if !sni.SetMatchesSource(set, srcMac) {
		return nil, ""
	}

	return set, host
}

func (c *hostHintCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.keys)
}
