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
	sets  [DomainSetsKept]string
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

func withSet(sets [DomainSetsKept]string, id string) [DomainSetsKept]string {
	if sets[0] == id {
		return sets
	}
	carry := id
	for i := range sets {
		sets[i], carry = carry, sets[i]
		if carry == id || carry == "" {
			break
		}
	}
	return sets
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

type domainRow struct {
	key string
	it  domainItem
}

func (r domainRow) ranksAbove(o domainRow) bool {
	if r.it.count != o.it.count {
		return r.it.count > o.it.count
	}
	return r.it.seq > o.it.seq
}

func insertTop(top []domainRow, r domainRow, n int) []domainRow {
	i := len(top)
	for i > 0 && r.ranksAbove(top[i-1]) {
		i--
	}
	if i >= n {
		return top
	}
	if len(top) < n {
		top = append(top, domainRow{})
	}
	copy(top[i+1:], top[i:len(top)-1])
	top[i] = r
	return top
}

func (d *domainState) list(off int64) *DomainList {
	top := make([]domainRow, 0, TopDomainsSent)
	d.mu.Lock()
	rev := d.rev
	for k, it := range d.items {
		top = insertTop(top, domainRow{key: k, it: it}, TopDomainsSent)
	}
	d.mu.Unlock()
	items := make([]DomainHit, len(top))
	for i, r := range top {
		items[i] = DomainHit{Key: r.key, Count: r.it.count, Last: wallMs(r.it.last, off), Sets: setIDs(r.it.sets)}
	}
	return &DomainList{Rev: rev, Items: items}
}

func setIDs(sets [DomainSetsKept]string) []string {
	var out []string
	for _, id := range sets {
		if id == "" {
			break
		}
		out = append(out, id)
	}
	return out
}
