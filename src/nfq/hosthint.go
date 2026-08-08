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

type hostHintKey struct {
	client string
	dest   string
}

type hostHintCache struct {
	mu   sync.RWMutex
	keys map[hostHintKey]*hostHintEntry
}

func newHostHintCache() *hostHintCache {
	return &hostHintCache{keys: make(map[hostHintKey]*hostHintEntry)}
}

func (c *hostHintCache) Store(clientIP, destIP, setId, host string) {
	if c == nil || clientIP == "" || destIP == "" || setId == "" {
		return
	}

	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	key := hostHintKey{client: clientIP, dest: destIP}
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
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry := c.keys[hostHintKey{client: clientIP, dest: destIP}]
	if entry == nil {
		return "", "", false
	}

	setId := ""
	host := ""
	for _, candidate := range entry.candidates {
		if !now.Before(candidate.expires) {
			continue
		}
		if setId == "" {
			setId = candidate.setId
			host = candidate.host
			continue
		}
		if candidate.setId != setId {
			log.Tracef("host hint for %s -> %s is ambiguous between sets %s and %s, ignoring",
				clientIP, destIP, setId, candidate.setId)
			return "", "", false
		}
	}

	if setId == "" {
		return "", "", false
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
		var oldestKey hostHintKey
		var oldestAt time.Time
		found := false
		for key, entry := range c.keys {
			at := entry.candidates[0].expires
			for _, candidate := range entry.candidates[1:] {
				if candidate.expires.Before(at) {
					at = candidate.expires
				}
			}
			if !found || at.Before(oldestAt) {
				oldestKey = key
				oldestAt = at
				found = true
			}
		}
		if !found {
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

func (w *Worker) storeHostHint(clientIP, destIP string, set *config.SetConfig, host, source string) {
	if w == nil || set == nil || set.Id == "" || host == "" || clientIP == "" || destIP == "" {
		return
	}

	w.hostHints.Store(clientIP, destIP, set.Id, host)
	log.Tracef("host hint from %s: %s -> %s is %s (set: %s)", source, clientIP, destIP, host, set.Name)
}

func (w *Worker) storeHostHints(clientIP net.IP, set *config.SetConfig, host string, ips []net.IP) {
	if w == nil || clientIP == nil || len(ips) == 0 {
		return
	}

	observe := w.goodIPs != nil && host != "" && set != nil &&
		set.TCP.IPBlockDetect.Enabled && set.TCP.IPBlockDetect.HealDNS

	client := clientIP.String()
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		w.storeHostHint(client, ip.String(), set, host, "dns")
		if observe {
			w.goodIPs.Observe(host, ip)
		}
	}
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.keys)
}
