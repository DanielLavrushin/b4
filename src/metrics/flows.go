package metrics

import (
	"strings"
	"sync"
)

const (
	flowTableCap  = 16384
	maxSetStates  = 1024
	setUntracked  = uint16(0xFFFF)
	setRingSlots  = 60
	setForgetNs   = 2 * hourNs
	rotateEveryNs = minuteNs
	staleGen      = uint8(0)
)

type flowEntry struct {
	minSeq  uint32
	tenSeq  uint32
	set     uint16
	blocked bool
	gen     uint8
}

type setSlot struct {
	seq uint32
	n   uint32
}

type setState struct {
	id          string
	ring        [setRingSlots]setSlot
	lastMatch   int64
	matched     bool
	lastPresent int64
	dnsBlocked  uint64
	live        bool
}

type flowState struct {
	mu           sync.Mutex
	cap          int
	cur          map[FlowKey]flowEntry
	old          map[FlowKey]flowEntry
	rotatedAt    int64
	gen          uint8
	setIdx       map[string]uint16
	sets         []setState
	free         []uint16
	conns        uint64
	connsInSets  uint64
	blockedConns uint64
	blockedDNS   uint64
	minute       []minuteBucket
	ten          []tenBucket
	minSeq       uint32
	tenSeq       uint32
	cpuLast      cpuSample
}

func (f *flowState) init(capacity int, mono, off int64, cpu cpuSample) {
	f.cap = capacity
	f.cur = make(map[FlowKey]flowEntry)
	f.old = make(map[FlowKey]flowEntry)
	f.rotatedAt = mono
	f.gen = staleGen + 1
	f.setIdx = make(map[string]uint16)
	f.minute = make([]minuteBucket, MinuteBuckets)
	f.ten = make([]tenBucket, TenMinuteBuckets)
	f.minute[0] = minuteBucket{end: nextBoundary(0, off, minuteNs), cpuStart: cpu}
	f.ten[0] = tenBucket{end: nextBoundary(0, off, tenMinuteNs)}
	f.cpuLast = cpu
	f.roll(mono, off, cpu, [3]uint64{}, 0, 0)
}

func (m *MetricsCollector) ObserveFlow(key FlowKey, setID string) {
	m.initOnce.Do(m.init)
	now := m.tickMono.Load()
	f := &m.fl
	f.mu.Lock()
	f.touch(key, setID, now, false)
	f.mu.Unlock()
}

func (m *MetricsCollector) CountConnection(setID string) {
	m.initOnce.Do(m.init)
	now := m.tickMono.Load()
	f := &m.fl
	f.mu.Lock()
	f.addConn(f.setIndex(setID, now), now)
	f.mu.Unlock()
}

func (m *MetricsCollector) RecordBlockedFlow(key FlowKey, setID, target, mac string) {
	m.initOnce.Do(m.init)
	now := m.tickMono.Load()
	f := &m.fl
	f.mu.Lock()
	blocked := f.touch(key, setID, now, true)
	f.mu.Unlock()
	if blocked {
		m.bl.note(target, mac, now)
	}
}

func (m *MetricsCollector) RecordBlockedDNS(setID, target, mac string) {
	m.initOnce.Do(m.init)
	now := m.tickMono.Load()
	f := &m.fl
	f.mu.Lock()
	f.blockedDNS++
	if idx := f.setIndex(setID, now); idx != 0 && idx != setUntracked {
		s := &f.sets[idx-1]
		s.dnsBlocked++
		s.lastPresent = now
	}
	f.mu.Unlock()
	m.bl.note(target, mac, now)
}

func (f *flowState) touch(key FlowKey, setID string, now int64, block bool) bool {
	if e, ok := f.cur[key]; ok {
		changed, blocked := f.update(&e, setID, now, block)
		if changed {
			f.cur[key] = e
		}
		return blocked
	}
	if e, ok := f.old[key]; ok {
		_, blocked := f.update(&e, setID, now, block)
		f.insert(key, e, now)
		return blocked
	}
	idx := f.setIndex(setID, now)
	f.addConn(idx, now)
	if block {
		f.blockedConns++
	}
	f.insert(key, flowEntry{minSeq: f.minSeq, tenSeq: f.tenSeq, set: idx, blocked: block, gen: f.gen}, now)
	return block
}

func (f *flowState) insert(key FlowKey, e flowEntry, now int64) {
	if len(f.cur) >= f.cap {
		f.rotate(now)
	}
	f.cur[key] = e
}

func (f *flowState) rotate(now int64) {
	f.cur, f.old = f.old, f.cur
	clear(f.cur)
	f.rotatedAt = now
}

func (f *flowState) update(e *flowEntry, setID string, now int64, block bool) (bool, bool) {
	changed := false
	if e.set == 0 && setID != "" {
		f.classify(e, f.setIndex(setID, now), now)
		changed = true
	}
	if block && !e.blocked {
		e.blocked = true
		if e.gen != f.gen {
			return true, false
		}
		f.blockedConns++
		return true, true
	}
	return changed, false
}

func (f *flowState) addConn(idx uint16, now int64) {
	f.conns++
	mb, tb := f.openMinute(), f.openTen()
	if idx == 0 {
		mb.notInSet++
		tb.notInSet++
		return
	}
	f.connsInSets++
	mb.inSets++
	tb.inSets++
	f.setHit(idx, now)
}

func (f *flowState) classify(e *flowEntry, idx uint16, now int64) {
	e.set = idx
	if e.gen == f.gen {
		f.connsInSets++
	}
	f.setHit(idx, now)
	if e.minSeq == f.minSeq {
		moveToSets(&f.openMinute().notInSet, &f.openMinute().inSets)
	}
	if e.tenSeq == f.tenSeq {
		moveToSets(&f.openTen().notInSet, &f.openTen().inSets)
	}
}

func moveToSets(notInSet, inSets *uint64) {
	if *notInSet > 0 {
		*notInSet--
	}
	*inSets++
}

func (f *flowState) setHit(idx uint16, now int64) {
	if idx == 0 || idx == setUntracked {
		return
	}
	s := &f.sets[idx-1]
	slot := &s.ring[f.minSeq%setRingSlots]
	if slot.seq != f.minSeq {
		slot.seq = f.minSeq
		slot.n = 0
	}
	slot.n++
	s.lastMatch = now
	s.matched = true
}

func (f *flowState) setIndex(setID string, now int64) uint16 {
	if setID == "" {
		return 0
	}
	if i, ok := f.setIdx[setID]; ok {
		return i + 1
	}
	if len(f.setIdx) >= maxSetStates {
		return setUntracked
	}
	var i uint16
	if n := len(f.free); n > 0 {
		i = f.free[n-1]
		f.free = f.free[:n-1]
	} else {
		i = uint16(len(f.sets))
		f.sets = append(f.sets, setState{})
	}
	id := strings.Clone(setID)
	f.sets[i] = setState{id: id, live: true, lastPresent: now}
	f.setIdx[id] = i
	return i + 1
}

func (f *flowState) set(id string) *setState {
	if i, ok := f.setIdx[id]; ok {
		return &f.sets[i]
	}
	return nil
}

func (s *setState) conns60m(open uint32) uint64 {
	var sum uint64
	for i := range s.ring {
		if open-s.ring[i].seq < setRingSlots {
			sum += uint64(s.ring[i].n)
		}
	}
	return sum
}

func (f *flowState) forgetSets(present []SetMeta, mono int64) {
	for _, meta := range present {
		if s := f.set(meta.ID); s != nil {
			s.lastPresent = mono
		}
	}
	for i := range f.sets {
		s := &f.sets[i]
		if !s.live {
			continue
		}
		last := s.lastPresent
		if s.matched && s.lastMatch > last {
			last = s.lastMatch
		}
		if mono-last > setForgetNs {
			delete(f.setIdx, s.id)
			*s = setState{}
			f.free = append(f.free, uint16(i))
		}
	}
}

func (f *flowState) resetCounters() {
	f.conns = 0
	f.connsInSets = 0
	f.blockedConns = 0
	f.blockedDNS = 0
	for i := range f.sets {
		f.sets[i].dnsBlocked = 0
	}
	f.gen++
	if f.gen == staleGen {
		f.gen++
		markStale(f.cur)
		markStale(f.old)
	}
}

func markStale(flows map[FlowKey]flowEntry) {
	for k, e := range flows {
		e.gen = staleGen
		flows[k] = e
	}
}
