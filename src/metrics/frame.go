package metrics

import "time"

type SentRevs struct {
	Blocked     uint64
	TopDomains  uint64
	Escalations uint64
	Events      uint64
}

func (m *MetricsCollector) Hello() Frame {
	return m.buildFrame(nil)
}

func (m *MetricsCollector) Tick(sent *SentRevs) Frame {
	if sent == nil {
		sent = &SentRevs{}
	}
	return m.buildFrame(sent)
}

func (m *MetricsCollector) buildFrame(sent *SentRevs) Frame {
	m.initOnce.Do(m.init)
	full := sent == nil
	mono, _ := m.now()
	off := m.off.Load()

	fr := Frame{Type: FrameTick, Now: wallMs(mono, off), UptimeS: mono / int64(time.Second)}
	if full {
		fr.Type = FrameHello
	}

	metas, disabled := loadSets()
	opens := loadProxyOpen()
	ovlNow := loadOverload()

	st := m.st.values()
	fr.Engine = m.effectiveEngine(st.engine)
	fr.StatsSince = wallMs(st.statsSince, off)
	if st.hasPacket {
		fr.LastPacketAt = wallMs(st.lastPacket, off)
	}
	fr.Process = m.processInfo(st)
	fr.Attention.BinaryReplaced = st.binaryReplaced
	if st.conntrackOK {
		ct := st.conntrack
		fr.Attention.Conntrack = &ct
	}

	m.fillFlows(&fr, full, off, metas, opens, ovlNow)
	fr.SetsDisabled = disabled
	fr.Totals.RSTDropped = m.rstDropped.Load()
	fr.Totals.Escalations = m.escTotal.Load()
	fr.Rules = loadRules()
	fr.Attention.Upstreams = failingUpstreams(loadUpstreams())

	if full || m.bl.currentRev() > sent.Blocked {
		fr.Blocked = m.bl.lists(off)
		if !full {
			sent.Blocked = fr.Blocked.Rev
		}
	}
	if full || m.dom.currentRev() > sent.TopDomains {
		fr.TopDomains = m.dom.list(off)
		if !full {
			sent.TopDomains = fr.TopDomains.Rev
		}
	}
	if full || m.esc.currentRev() > sent.Escalations {
		fr.Escalations = m.esc.list(off)
		if !full {
			sent.Escalations = fr.Escalations.Rev
		}
	}
	if full || m.ev.currentRev() > sent.Events {
		fr.Events = m.ev.log(off)
		if !full {
			sent.Events = fr.Events.Rev
		}
	}

	fr.MTProto = loadMTProto()
	if f := m.GetEngineFailure(); f != nil {
		fr.EngineFailure = f
		fr.Engine.State = EngineFailed
	}
	return fr
}

func (m *MetricsCollector) fillFlows(fr *Frame, full bool, off int64, metas []SetMeta, opens map[string]int64, ovlNow [3]uint64) {
	f := &m.fl
	f.mu.Lock()
	defer f.mu.Unlock()

	minFrom, tenFrom := firstSeq(f.minSeq, MinuteBuckets), firstSeq(f.tenSeq, TenMinuteBuckets)
	if !full {
		minFrom, tenFrom = firstSeq(f.minSeq, 2), firstSeq(f.tenSeq, 2)
	}
	fr.Activity.Minute = f.minuteBuckets(minFrom, off)
	fr.Activity.TenMinute, fr.RSSHistory = f.tenBuckets(tenFrom, off)

	if peak, at, ok := f.cpuPeak(m.clkTck, m.cores); ok {
		fr.Process.CPUPeakPercent = peak
		fr.Process.CPUPeakAt = wallMs(at, off)
	}

	base := f.overloadBase()
	fr.Attention.Overload = Overload{
		WindowS:        OverloadWindowSeconds,
		InjectSkipped:  deltaSince(ovlNow[0], base[0]),
		RawSendDropped: deltaSince(ovlNow[1], base[1]),
		QueueOverflow:  deltaSince(ovlNow[2], base[2]),
	}

	fr.Totals.Conns = f.conns
	fr.Totals.ConnsInSets = f.connsInSets
	fr.Totals.BlockedConns = f.blockedConns
	fr.Totals.BlockedDNS = f.blockedDNS

	fr.Sets = make([]SetActivity, 0, len(metas))
	for _, meta := range metas {
		sa := SetActivity{ID: meta.ID, Name: meta.Name, Kind: meta.Kind, Watched: meta.Watched}
		if s := f.set(meta.ID); s != nil {
			sa.Conns60m = s.conns60m(f.minSeq)
			sa.DNSBlocked = s.dnsBlocked
			if s.matched {
				sa.LastMatch = wallMs(s.lastMatch, off)
			}
		}
		if meta.Kind == SetKindProxy && opens != nil {
			n := opens[meta.ID]
			sa.Open = &n
		}
		fr.Sets = append(fr.Sets, sa)
	}
}

func failingUpstreams(in []UpstreamAttention) []UpstreamAttention {
	out := make([]UpstreamAttention, 0, len(in))
	for _, u := range in {
		if u.Failures > 0 {
			out = append(out, u)
		}
	}
	return out
}

func (m *MetricsCollector) Totals() Totals {
	m.initOnce.Do(m.init)
	f := &m.fl
	f.mu.Lock()
	t := Totals{Conns: f.conns, ConnsInSets: f.connsInSets, BlockedConns: f.blockedConns, BlockedDNS: f.blockedDNS}
	f.mu.Unlock()
	t.RSTDropped = m.rstDropped.Load()
	t.Escalations = m.escTotal.Load()
	return t
}

func (m *MetricsCollector) LastMinute() (inSets, notInSet uint64) {
	m.initOnce.Do(m.init)
	f := &m.fl
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.minSeq == 0 {
		return 0, 0
	}
	b := &f.minute[(f.minSeq-1)%MinuteBuckets]
	return b.inSets, b.notInSet
}

func (m *MetricsCollector) processInfo(v liveValues) ProcessInfo {
	return ProcessInfo{
		RSSBytes:      v.rss,
		MemTotalBytes: v.memTotal,
		CPUPercent:    v.cpuPct,
		CPUCores:      m.cores,
		OSThreads:     v.threads,
		ThreadWarn:    threadDumpTrigger,
		ThreadLimit:   maxOSThreads,
	}
}

func (m *MetricsCollector) Process() ProcessInfo {
	m.initOnce.Do(m.init)
	off := m.off.Load()
	p := m.processInfo(m.st.values())
	m.fl.mu.Lock()
	peak, at, ok := m.fl.cpuPeak(m.clkTck, m.cores)
	m.fl.mu.Unlock()
	if ok {
		p.CPUPeakPercent = peak
		p.CPUPeakAt = wallMs(at, off)
	}
	return p
}

func (m *MetricsCollector) UptimeSeconds() int64 {
	mono, _ := m.now()
	return mono / int64(time.Second)
}

func (m *MetricsCollector) UptimeString() string {
	mono, _ := m.now()
	return formatDuration(time.Duration(mono))
}

func (m *MetricsCollector) StatsSince() int64 {
	m.initOnce.Do(m.init)
	return wallMs(m.st.values().statsSince, m.off.Load())
}
