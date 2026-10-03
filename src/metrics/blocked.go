package metrics

import (
	"slices"
	"strings"
	"sync"
)

type blockedItem struct {
	count uint64
	last  int64
	seq   uint64
}

type blockedState struct {
	mu      sync.Mutex
	rev     uint64
	seq     uint64
	domains map[string]blockedItem
	devices map[string]blockedItem
}

func (b *blockedState) note(target, mac string, now int64) {
	if target == "" && mac == "" {
		return
	}
	b.mu.Lock()
	b.seq++
	if target != "" {
		b.domains = bumpBlocked(b.domains, target, now, b.seq, BlockedDomainsKept)
	}
	if mac != "" {
		b.devices = bumpBlocked(b.devices, mac, now, b.seq, BlockedDevicesKept)
	}
	b.rev++
	b.mu.Unlock()
}

func bumpBlocked(m map[string]blockedItem, key string, now int64, seq uint64, keep int) map[string]blockedItem {
	if m == nil {
		m = make(map[string]blockedItem)
	}
	if it, ok := m[key]; ok {
		it.count++
		it.last = now
		it.seq = seq
		m[key] = it
		return m
	}
	for len(m) >= keep {
		oldest, first := "", true
		var oldestSeq uint64
		for k, it := range m {
			if first || it.seq < oldestSeq {
				oldest, oldestSeq, first = k, it.seq, false
			}
		}
		delete(m, oldest)
	}
	m[strings.Clone(key)] = blockedItem{count: 1, last: now, seq: seq}
	return m
}

func (b *blockedState) reset() {
	b.mu.Lock()
	clear(b.domains)
	clear(b.devices)
	b.rev++
	b.mu.Unlock()
}

func (b *blockedState) currentRev() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rev
}

type blockedRow struct {
	key string
	it  blockedItem
}

func (b *blockedState) lists(off int64) *BlockedLists {
	b.mu.Lock()
	rev := b.rev
	domains := blockedRows(b.domains)
	devices := blockedRows(b.devices)
	b.mu.Unlock()
	return &BlockedLists{Rev: rev, Domains: blockedEntries(domains, off), Devices: blockedEntries(devices, off)}
}

func blockedRows(m map[string]blockedItem) []blockedRow {
	rows := make([]blockedRow, 0, len(m))
	for k, it := range m {
		rows = append(rows, blockedRow{key: k, it: it})
	}
	return rows
}

func blockedEntries(rows []blockedRow, off int64) []BlockedEntry {
	slices.SortFunc(rows, func(a, b blockedRow) int {
		switch {
		case a.it.seq > b.it.seq:
			return -1
		case a.it.seq < b.it.seq:
			return 1
		}
		return 0
	})
	out := make([]BlockedEntry, len(rows))
	for i, r := range rows {
		out[i] = BlockedEntry{Key: r.key, Count: r.it.count, Last: wallMs(r.it.last, off)}
	}
	return out
}
