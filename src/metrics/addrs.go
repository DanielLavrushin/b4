package metrics

import (
	"net/netip"
	"sync"
)

const (
	addrSlots       = 4
	addrSettleTicks = 3
)

type addrItem struct {
	count    uint64
	pend     [addrSlots]uint32
	pendLast [addrSlots]int64
	last     int64
	seq      uint64
	sets     [TopSetsKept]string
}

func (it *addrItem) idle() bool {
	return it.count == 0 && it.pend == [addrSlots]uint32{}
}

type addrState struct {
	mu    sync.Mutex
	rev   uint64
	seq   uint64
	items map[netip.Addr]*addrItem
}

func (a *addrState) entry(dst netip.Addr) *addrItem {
	if it, ok := a.items[dst]; ok {
		return it
	}
	if a.items == nil {
		a.items = make(map[netip.Addr]*addrItem)
	}
	for len(a.items) >= TopAddressesKept {
		a.evict()
	}
	a.seq++
	it := &addrItem{seq: a.seq}
	a.items[dst] = it
	return it
}

func (a *addrState) evict() {
	var victim netip.Addr
	var worst *addrItem
	for k, it := range a.items {
		if worst == nil || it.count < worst.count || (it.count == worst.count && it.seq < worst.seq) {
			victim, worst = k, it
		}
	}
	delete(a.items, victim)
}

func (a *addrState) hold(dst netip.Addr, setID string, born uint32, now int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.entry(dst.Unmap())
	slot := born % addrSlots
	it.pend[slot]++
	it.pendLast[slot] = now
	if setID != "" {
		it.sets = withSet(it.sets, setID)
	}
}

func (a *addrState) drop(dst netip.Addr, born uint32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	it, ok := a.items[dst.Unmap()]
	if !ok {
		return
	}
	if slot := born % addrSlots; it.pend[slot] > 0 {
		it.pend[slot]--
	}
}

func (a *addrState) record(dst netip.Addr, setID string, now int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.entry(dst.Unmap())
	a.seq++
	it.count++
	it.last = now
	it.seq = a.seq
	if setID != "" {
		it.sets = withSet(it.sets, setID)
	}
	a.rev++
}

func (a *addrState) settle(tick uint32) {
	slot := (tick - addrSettleTicks) % addrSlots
	a.mu.Lock()
	defer a.mu.Unlock()
	moved := false
	for dst, it := range a.items {
		if n := it.pend[slot]; n > 0 {
			a.seq++
			it.count += uint64(n)
			it.pend[slot] = 0
			it.last = max(it.last, it.pendLast[slot])
			it.seq = a.seq
			moved = true
		}
		if it.idle() {
			delete(a.items, dst)
		}
	}
	if moved {
		a.rev++
	}
}

func (a *addrState) reset() {
	a.mu.Lock()
	clear(a.items)
	a.rev++
	a.mu.Unlock()
}

func (a *addrState) currentRev() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rev
}

func (a *addrState) list(off int64) *TopList {
	top := make([]topRow[netip.Addr], 0, TopAddressesSent)
	a.mu.Lock()
	rev := a.rev
	for k, it := range a.items {
		if it.count == 0 {
			continue
		}
		top = insertTop(top, topRow[netip.Addr]{key: k, count: it.count, last: it.last, seq: it.seq, sets: it.sets}, TopAddressesSent)
	}
	a.mu.Unlock()
	return &TopList{Rev: rev, Items: topEntries(top, netip.Addr.String, off)}
}
