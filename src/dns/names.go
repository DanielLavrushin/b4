package dns

import (
	"container/list"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	nameCacheLimit  = 8192
	nameCacheTTL    = 30 * time.Minute
	namesPerAddress = 8
	clientsPerName  = 4
	maxObservedName = 253
)

type nameRecord struct {
	name    string
	clients []string
	local   bool
	seen    time.Time
}

type nameEntry struct {
	records []nameRecord
	element *list.Element
}

type NameMatches struct {
	Own    []string
	Local  []string
	Others []string
}

func (m NameMatches) Empty() bool {
	return len(m.Own) == 0 && len(m.Local) == 0 && len(m.Others) == 0
}

func (m NameMatches) Has(name string) bool {
	return containsName(m.Own, name) || containsName(m.Local, name) || containsName(m.Others, name)
}

type NameCache struct {
	mu      sync.Mutex
	entries map[string]*nameEntry
	lru     *list.List
	limit   int
	ttl     time.Duration
	wanted  atomic.Bool
	now     func() time.Time
}

func NewNameCache() *NameCache {
	return &NameCache{
		entries: make(map[string]*nameEntry),
		lru:     list.New(),
		limit:   nameCacheLimit,
		ttl:     nameCacheTTL,
		now:     time.Now,
	}
}

func (c *NameCache) SetWanted(wanted bool) {
	if c == nil {
		return
	}
	if was := c.wanted.Swap(wanted); !was || wanted {
		return
	}
	c.mu.Lock()
	c.entries = make(map[string]*nameEntry)
	c.lru.Init()
	c.mu.Unlock()
}

func (c *NameCache) Wanted() bool {
	return c != nil && c.wanted.Load()
}

func CleanHostName(name string) string {
	n := strings.TrimRight(strings.ToLower(strings.TrimSpace(name)), ".")
	if n == "" || len(n) > maxObservedName || !strings.Contains(n, ".") || net.ParseIP(n) != nil {
		return ""
	}
	for i := 0; i < len(n); i++ {
		ch := n[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '-', ch == '.', ch == '_':
		default:
			return ""
		}
	}
	return n
}

func (c *NameCache) Observe(client net.IP, name string, ips []net.IP) {
	if !c.Wanted() || len(ips) == 0 {
		return
	}
	n := CleanHostName(name)
	if n == "" {
		return
	}
	who := ""
	if client != nil {
		who = client.String()
	}
	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		c.observeLocked(ip.String(), n, who, now)
	}
}

func (c *NameCache) observeLocked(key, name, who string, now time.Time) {
	entry, ok := c.entries[key]
	if !ok {
		if len(c.entries) >= c.limit {
			if oldest := c.lru.Back(); oldest != nil {
				delete(c.entries, oldest.Value.(string))
				c.lru.Remove(oldest)
			}
		}
		entry = &nameEntry{element: c.lru.PushFront(key)}
		c.entries[key] = entry
	} else {
		c.lru.MoveToFront(entry.element)
	}

	rec := nameRecord{name: name, seen: now}
	kept := make([]nameRecord, 0, namesPerAddress)
	for _, prev := range entry.records {
		if prev.name == name {
			rec.local = prev.local
			rec.clients = prev.clients
			continue
		}
		if now.Sub(prev.seen) <= c.ttl && len(kept) < namesPerAddress-1 {
			kept = append(kept, prev)
		}
	}
	if who == "" {
		rec.local = true
	} else {
		clients := make([]string, 0, clientsPerName)
		clients = append(clients, who)
		for _, cl := range rec.clients {
			if cl != who && len(clients) < clientsPerName {
				clients = append(clients, cl)
			}
		}
		rec.clients = clients
	}
	entry.records = append([]nameRecord{rec}, kept...)
}

func (c *NameCache) Lookup(client, ip net.IP) NameMatches {
	var m NameMatches
	if !c.Wanted() || ip == nil {
		return m
	}
	who := ""
	if client != nil {
		who = client.String()
	}
	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[ip.String()]
	if !ok {
		return m
	}
	for _, rec := range entry.records {
		if now.Sub(rec.seen) > c.ttl {
			continue
		}
		switch {
		case who != "" && containsName(rec.clients, who):
			m.Own = append(m.Own, rec.name)
		case rec.local:
			m.Local = append(m.Local, rec.name)
		default:
			m.Others = append(m.Others, rec.name)
		}
	}
	return m
}

func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
