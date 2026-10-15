package metrics

import (
	"runtime"
	"sync"
)

const (
	cpuWindow    = 10
	slowEveryNs  = int64(10e9)
	cpuRingSlots = cpuWindow + 1
)

type liveValues struct {
	engine         EngineInfo
	statsSince     int64
	rss            uint64
	memTotal       uint64
	cpuPct         float64
	threads        int
	lastPacket     int64
	hasPacket      bool
	binaryReplaced bool
	conntrack      Conntrack
	conntrackOK    bool
}

type liveState struct {
	mu sync.Mutex
	v  liveValues
}

func (s *liveState) values() liveValues {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v
}

type tickState struct {
	cpu         [cpuRingSlots]cpuSample
	cpuN        int
	cpuPos      int
	lastSlow    int64
	slowDone    bool
	lastForget  int64
	lastPackets uint64
}

func (m *MetricsCollector) init() {
	if m.sampler == nil {
		m.sampler = procfsSampler{}
	}
	if m.flowCap <= 0 {
		m.flowCap = flowTableCap
	}
	if m.clkTck == 0 {
		m.clkTck = clkTck()
	}
	if m.cores <= 0 {
		m.cores = runtime.NumCPU()
	}
	mono, wall := m.now()
	off := wall - mono
	m.off.Store(off)
	m.tickMono.Store(mono)
	ticks, ok := m.sampler.cpuTicks()
	cpu := cpuSample{mono: mono, ticks: ticks, ok: ok}
	if ok {
		m.ts.pushCPU(cpu)
	}
	m.ts.lastForget = mono
	m.fl.mu.Lock()
	m.fl.init(m.flowCap, mono, off, cpu)
	m.fl.mu.Unlock()
	total, rss, threads := m.sampler.memTotal(), m.sampler.rss(), m.sampler.threads()
	m.st.mu.Lock()
	m.st.v.memTotal = total
	m.st.v.rss = rss
	m.st.v.threads = threads
	m.st.mu.Unlock()
}

func (t *tickState) pushCPU(s cpuSample) {
	t.cpu[t.cpuPos] = s
	t.cpuPos = (t.cpuPos + 1) % cpuRingSlots
	if t.cpuN < cpuRingSlots {
		t.cpuN++
	}
}

func (t *tickState) cpuPercent(clk uint64, cores int) float64 {
	if t.cpuN < 2 {
		return 0
	}
	newest := t.cpu[(t.cpuPos-1+cpuRingSlots)%cpuRingSlots]
	oldest := t.cpu[(t.cpuPos-t.cpuN+cpuRingSlots)%cpuRingSlots]
	return cpuPercent(oldest, newest, clk, cores)
}

func (m *MetricsCollector) SetTickListener(fn func()) {
	if fn == nil {
		m.listener.Store(nil)
		return
	}
	m.listener.Store(&fn)
}

func (m *MetricsCollector) tick() {
	m.initOnce.Do(m.init)
	mono, wall := m.now()
	m.adoptOffset(mono, wall)
	off := m.off.Load()
	m.tickMono.Store(mono)
	ts := &m.ts

	ticks, ok := m.sampler.cpuTicks()
	cpu := cpuSample{mono: mono, ticks: ticks, ok: ok}
	if ok {
		ts.pushCPU(cpu)
	}
	cpuPct := ts.cpuPercent(m.clkTck, m.cores)
	rss := m.sampler.rss()
	threads := m.sampler.threads()
	checkThreadPressure(threads)
	ovl := loadOverload()

	m.fl.mu.Lock()
	m.fl.roll(mono, off, cpu, ovl, m.clkTck, m.cores)
	if b := m.fl.openTen(); rss > b.rssMax {
		b.rssMax = rss
	}
	if mono-m.fl.rotatedAt >= rotateEveryNs {
		m.fl.rotate(mono)
	}
	m.fl.mu.Unlock()

	packets, counted := loadPacketCounter()
	newPackets := counted && packets > ts.lastPackets
	if counted {
		ts.lastPackets = packets
	}

	slow := !ts.slowDone || mono-ts.lastSlow >= slowEveryNs
	var ct Conntrack
	var ctOK, replaced bool
	if slow {
		ts.slowDone = true
		ts.lastSlow = mono
		ct, ctOK = m.sampler.conntrack()
		replaced = loadBinaryReplaced()
	}

	m.st.mu.Lock()
	v := &m.st.v
	v.rss = rss
	v.cpuPct = cpuPct
	v.threads = threads
	if newPackets {
		v.lastPacket = mono
		v.hasPacket = true
	}
	if slow {
		v.conntrack = ct
		v.conntrackOK = ctOK
		v.binaryReplaced = replaced
	}
	m.st.mu.Unlock()

	if mono-ts.lastForget >= rotateEveryNs {
		ts.lastForget = mono
		present, _ := loadSets()
		m.fl.mu.Lock()
		m.fl.forgetSets(present, mono)
		m.fl.mu.Unlock()
	}

	m.addr.settle(m.tickN.Add(1))

	if fn := m.listener.Load(); fn != nil {
		(*fn)()
	}
}

func (m *MetricsCollector) SetEngine(info EngineInfo) {
	m.st.mu.Lock()
	m.st.v.engine = info
	m.st.mu.Unlock()
}

func (m *MetricsCollector) SetEngineState(state string) {
	m.st.mu.Lock()
	m.st.v.engine.State = state
	m.st.mu.Unlock()
}

func (m *MetricsCollector) Engine() EngineInfo {
	return m.effectiveEngine(m.st.values().engine)
}

func (m *MetricsCollector) effectiveEngine(e EngineInfo) EngineInfo {
	if e.State == "" {
		e.State = EngineStarting
	}
	if m.GetEngineFailure() != nil {
		e.State = EngineFailed
	}
	return e
}

func (m *MetricsCollector) ResetCounters() {
	m.initOnce.Do(m.init)
	mono, _ := m.now()
	m.fl.mu.Lock()
	m.fl.resetCounters()
	m.fl.mu.Unlock()
	m.rstDropped.Store(0)
	m.escTotal.Store(0)
	m.bl.reset()
	m.dom.reset()
	m.addr.reset()
	m.st.mu.Lock()
	m.st.v.statsSince = mono
	m.st.mu.Unlock()
}
