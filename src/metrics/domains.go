package metrics

import (
	"strings"
	"sync"
)

const maxDomainLen = 253

type domainItem struct {
	count uint64
	last  int64
	seq   uint64
	sets  [TopSetsKept]string
}

type domainState struct {
	mu    sync.Mutex
	rev   uint64
	seq   uint64
	items map[string]domainItem
}

func (d *domainState) note(host, setID string, now int64) {
	host = domainKey(host)
	if host == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seq++
	it, ok := d.items[host]
	if !ok {
		if d.items == nil {
			d.items = make(map[string]domainItem)
		}
		for len(d.items) >= TopDomainsKept {
			d.evict()
		}
		host = strings.Clone(host)
	}
	it.count++
	it.last = now
	it.seq = d.seq
	if setID != "" {
		it.sets = withSet(it.sets, setID)
	}
	d.items[host] = it
	d.rev++
}

func (d *domainState) evict() {
	victim, first := "", true
	var count, seq uint64
	for k, it := range d.items {
		if first || it.count < count || (it.count == count && it.seq < seq) {
			victim, count, seq, first = k, it.count, it.seq, false
		}
	}
	delete(d.items, victim)
}

func domainKey(host string) string {
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(host) > maxDomainLen || isIPLiteral(host) {
		return ""
	}
	return strings.ToLower(host)
}

func isIPLiteral(s string) bool {
	if strings.IndexByte(s, ':') >= 0 {
		return true
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c != '.' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func (d *domainState) reset() {
	d.mu.Lock()
	clear(d.items)
	d.rev++
	d.mu.Unlock()
}

func (d *domainState) currentRev() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rev
}

func (d *domainState) list(off int64) *TopList {
	top := make([]topRow[string], 0, TopDomainsSent)
	d.mu.Lock()
	rev := d.rev
	for k, it := range d.items {
		top = insertTop(top, topRow[string]{key: k, count: it.count, last: it.last, seq: it.seq, sets: it.sets}, TopDomainsSent)
	}
	d.mu.Unlock()
	return &TopList{Rev: rev, Items: topEntries(top, func(k string) string { return k }, off)}
}
