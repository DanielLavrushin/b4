package nfq

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"slices"
	"sort"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/utils"
)

const (
	maxCollectedIPs       = 1024
	silentHellosToCollect = 2
	maxCollectedRuns      = 1024
	collectedRetryFirst   = 10 * time.Second
	collectedRetryMax     = 5 * time.Minute
)

type collectedCandidate struct {
	setID string
	host  string
	port  uint16
	at    time.Time
}

func (p *Pool) SetCollectedTargetsInstaller(install func(context.Context, *config.Config) error, changed func(*sni.SuffixSet)) {
	p.configMu.Lock()
	defer p.configMu.Unlock()
	p.collectedInstall, p.collectedChanged = install, changed
}

func (p *Pool) CollectedConfig(cfg *config.Config) *config.Config {
	p.collectedMu.Lock()
	defer p.collectedMu.Unlock()
	if p.collectedView != nil && p.collectedViewGen == p.collectedPubGen && (cfg == p.collectedViewBase || cfg == p.collectedView) {
		return p.collectedView
	}
	view := cfg.WithCollectedIPs(p.collectedPublished)
	p.collectedViewBase, p.collectedViewGen, p.collectedView = cfg, p.collectedPubGen, view
	return view
}

func (p *Pool) collectedRelevantLocked(cfg *config.Config) bool {
	if cfg == nil || p.collectedEngineTUN || cfg.Queue.IsDiscovery || cfg.Queue.Mode == "tun" || cfg.System.Tables.SkipSetup {
		return false
	}
	if len(p.collected) > 0 || cfg.CollectedIPsInUse() {
		return true
	}
	p.collectedMu.RLock()
	defer p.collectedMu.RUnlock()
	return len(p.collectedPublished) > 0
}

func (p *Pool) StopCollected() {
	if p == nil || p.collectedCancel == nil {
		return
	}
	p.collectedCancel()
	p.configMu.Lock()
	p.collectedDirty = false
	p.configMu.Unlock()
}

func eligibleCollectedIP(ip net.IP) (netip.Addr, bool) {
	addr, ok := netip.AddrFromSlice(ip)
	addr = addr.Unmap()
	return addr, ok && addr.IsGlobalUnicast() && !addr.IsPrivate() && !utils.IsPrivateIP(ip)
}

func (p *Pool) collectHello(h helloWatch, confirm func() bool) bool {
	if p == nil || p.collectedCtx == nil || p.collectedCtx.Err() != nil {
		return false
	}
	addr, ok := eligibleCollectedIP(h.dst)
	if !ok {
		return false
	}
	unlock := config.LockWrites()
	defer unlock()
	p.configMu.Lock()
	defer p.configMu.Unlock()
	if h.gen != 0 && p.state != nil && p.state.destState != nil {
		t := p.state.destState
		t.watchMu.RLock()
		cur := t.watches[h.key]
		valid := cur != nil && cur.gen == h.gen && !cur.closed
		t.watchMu.RUnlock()
		if !valid {
			return false
		}
	}
	cfg := p.collectedBase
	if cfg == nil || cfg.Queue.IsDiscovery || cfg.Queue.Mode == "tun" || cfg.System.Tables.SkipSetup {
		return false
	}
	producer := cfg.GetSetById(h.setID)
	if producer == nil || !producer.Enabled || !producer.TCP.IPBlockDetect.Collects() || producer.Targets.UseCollectedIPs {
		return false
	}
	if p.collected == nil {
		p.collected = make(map[netip.Addr]collectedCandidate)
	}
	_, existed := p.collected[addr]
	if !existed {
		if len(p.collected) >= maxCollectedIPs {
			return false
		}
		p.collected[addr] = collectedCandidate{setID: h.setID, host: h.host, port: h.port, at: time.Now()}
	}
	if !p.publishCollectedLocked() {
		if !existed {
			delete(p.collected, addr)
		}
		return false
	}
	current := p.GetFirstWorkerConfig()
	matcher := p.Workers[0].getMatcher()
	keep := func(set *config.SetConfig) bool {
		return set.Targets.UseCollectedIPs && set.MatchesTCPDPort(h.port) && collectedSourceAllowed(set, h.scope)
	}
	matched, consumer := matcher.MatchIPWithSource(h.dst, h.mac, keep)
	accepted := matched && current.IPFamilyEnabled(h.ver == IPv6)
	if accepted {
		winner, selected := matcher.MatchIPWithSource(h.dst, h.mac, func(set *config.SetConfig) bool {
			return set.MatchesTCPDPort(h.port) && collectedSourceAllowed(set, h.scope)
		})
		accepted = winner && selected.Id == consumer.Id && confirm != nil && confirm()
	}
	if !accepted && !existed {
		delete(p.collected, addr)
		if !p.publishCollectedLocked() {
			log.Warnf("Collected IPs: interrupted candidate rollback incomplete for %s", addr)
		}
	}
	if accepted && !existed {
		log.Infof("Collected IP: candidate %s from set %q", addr, producer.Name)
		p.updateCollectedMetricsLocked()
	}
	return accepted
}

func (p *Pool) publishCollectedLocked() bool {
	return p.publishCollectedViewLocked(true)
}

func (p *Pool) publishCollectedViewLocked(requireConsumer bool) bool {
	cfg := p.collectedBase
	if cfg == nil || p.collectedInstall == nil || p.collectedCtx.Err() != nil || p.collectedEngineTUN || cfg.Queue.IsDiscovery || cfg.Queue.Mode == "tun" || cfg.System.Tables.SkipSetup {
		return false
	}
	ips := make([]string, 0, len(p.collected))
	for ip := range p.collected {
		ips = append(ips, ip.String())
	}
	sort.Strings(ips)
	consumers := false
	for _, set := range cfg.Sets {
		consumers = consumers || set.Enabled && set.Targets.UseCollectedIPs
	}
	if requireConsumer && !consumers {
		return false
	}
	p.collectedMu.RLock()
	unchanged := slices.Equal(ips, p.collectedPublished)
	p.collectedMu.RUnlock()
	var view *config.Config
	if unchanged {
		view = p.CollectedConfig(cfg)
	} else {
		view = cfg.WithCollectedIPs(ips)
	}
	ctx, cancel := context.WithTimeout(p.collectedCtx, 5*time.Second)
	defer cancel()
	installErr := p.collectedInstall(ctx, view)
	if installErr == nil {
		installErr = ctx.Err()
	}
	if err := installErr; err != nil {
		log.Warnf("Collected IPs: interception not ready: %v", err)
		if unchanged || p.collectedCtx.Err() != nil {
			return false
		}
		rollback, done := context.WithTimeout(p.collectedCtx, 5*time.Second)
		defer done()
		if err := p.collectedInstall(rollback, p.CollectedConfig(cfg)); err != nil {
			log.Warnf("Collected IPs: rollback incomplete: %v", err)
		}
		return false
	}
	if unchanged && len(p.Workers) > 0 && p.Workers[0].getConfig() == view {
		return true
	}
	matcher := buildMatcher(view)
	if len(p.Workers) > 0 {
		matcher.TransferLearnedIPs(p.Workers[0].getMatcher())
		matcher.KeepConnectionPolicy(p.Workers[0].getMatcher())
	}
	p.state.tlsCache.PublishCollected(matcher)
	for _, w := range p.Workers {
		w.cfg.Store(view)
		w.matcher.Store(matcher)
	}
	p.collectedMu.Lock()
	if !unchanged {
		p.collectedPublished = ips
		p.collectedPubGen++
	}
	p.collectedViewBase, p.collectedViewGen, p.collectedView = cfg, p.collectedPubGen, view
	p.collectedMu.Unlock()
	if p.collectedChanged != nil {
		p.collectedChanged(matcher)
	}
	return true
}

func (p *Pool) collectedDueLocked(now time.Time) bool {
	if !p.collectedDirty {
		return false
	}
	if !p.collectedRelevantLocked(p.collectedBase) {
		p.collectedDirty = false
		return false
	}
	return !now.Before(p.collectedRetryAt)
}

func (p *Pool) reconcileCollected(now time.Time) {
	if p == nil || p.collectedCtx == nil || p.collectedCtx.Err() != nil {
		return
	}
	p.configMu.Lock()
	due := p.collectedDueLocked(now)
	p.configMu.Unlock()
	if !due {
		return
	}
	unlock := config.LockWrites()
	defer unlock()
	p.configMu.Lock()
	defer p.configMu.Unlock()
	if !p.collectedDueLocked(now) {
		return
	}
	if !p.publishCollectedViewLocked(false) {
		p.collectedRetryWait = min(max(2*p.collectedRetryWait, collectedRetryFirst), collectedRetryMax)
		p.collectedRetryAt = now.Add(p.collectedRetryWait)
		return
	}
	p.collectedDirty = false
	p.collectedRetryWait, p.collectedRetryAt = 0, time.Time{}
	p.updateCollectedMetricsLocked()
}

func collectedFlow(h helloWatch) (ctnl.Flow, bool) {
	flow, found, err := helloFlow(h)
	if err != nil || !found || !flow.HasState || flow.TCPState != ctnl.TCPEstablished || !flow.HasCounters || flow.ID == 0 {
		return ctnl.Flow{}, false
	}
	return flow, true
}

func collectedFlowSilent(h, cur *helloWatch, flow ctnl.Flow) bool {
	if cur.clientHelloLen == 0 || !cur.clientHelloAcked || flow.ID != cur.flowID || !flow.HasState || flow.TCPState != ctnl.TCPEstablished {
		return false
	}
	return flow.ReplyBytes == cur.observedReplyBytes && flow.ReplyPackets == cur.observedReplyPackets &&
		(!h.replyChecked || flow.ReplyBytes == h.replyBytes && flow.ReplyPackets == h.replyPackets)
}

func collectedReplyUnseen(h helloWatch, flow ctnl.Flow) bool {
	if flow.ReplyPackets < h.observedReplyPackets || flow.ReplyBytes <= h.observedReplyBytes {
		return false
	}
	headers := uint64(60 + 20)
	if h.ver == IPv6 {
		headers = 60 + IPv6HeaderLen
	}
	return flow.ReplyBytes-h.observedReplyBytes > (flow.ReplyPackets-h.observedReplyPackets)*headers
}

func readCollectedFlow(h helloWatch) (ctnl.Flow, serverSilence) {
	flow, found, err := helloFlow(h)
	switch {
	case err != nil || !found || !flow.HasCounters || flow.ID == 0 || flow.ID != h.flowID:
		return flow, silenceUnknown
	case collectedReplyUnseen(h, flow):
		return flow, silenceAnswered
	case collectedFlowSilent(&h, &h, flow):
		return flow, silenceOpen
	}
	return flow, silenceUnknown
}

func (t *destStateTracker) forgetCollectRunLocked(h *helloWatch) {
	if !h.consumer && len(t.collectRuns) > 0 {
		delete(t.collectRuns, iphealth.BanKey(h.dst, int(h.port)))
	}
}

func (t *destStateTracker) countCollectRunLocked(key string, counted bool, now time.Time, window time.Duration) (int, bool) {
	run, known := t.collectRuns[key]
	if known && !now.Before(run.until) {
		run.count = 0
	}
	if counted {
		return run.count, true
	}
	if !known && len(t.collectRuns) >= maxCollectedRuns {
		for other, r := range t.collectRuns {
			if !now.Before(r.until) {
				delete(t.collectRuns, other)
			}
		}
		if len(t.collectRuns) >= maxCollectedRuns {
			return 0, false
		}
	}
	if t.collectRuns == nil {
		t.collectRuns = make(map[string]silentRun)
	}
	run.count++
	run.until = now.Add(window)
	t.collectRuns[key] = run
	return run.count, true
}

func (t *destStateTracker) EndAnsweredWatch(h helloWatch) bool {
	if t == nil {
		return false
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	t.forgetCollectRunLocked(&h)
	if cur, ok := t.watches[h.key]; !ok || cur.gen != h.gen {
		return false
	}
	t.unwatchLocked(h.key)
	return true
}

func (t *destStateTracker) parkedCollected(dst net.IP, port uint16, limit int) []helloWatch {
	t.watchMu.RLock()
	defer t.watchMu.RUnlock()
	var parked []helloWatch
	for _, h := range t.watches {
		if len(parked) >= limit {
			break
		}
		if h.collect && h.parked && !h.closed && h.isTo(dst, port) {
			parked = append(parked, *h)
		}
	}
	return parked
}

func (t *destStateTracker) takeParkedCollected(parked []helloWatch, flows []ctnl.Flow, found []bool) []helloWatch {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	var silent []helloWatch
	for i, h := range parked {
		cur := t.watches[h.key]
		if cur == nil || cur.gen != h.gen || !cur.parked {
			continue
		}
		if !cur.closed && found[i] && collectedFlowSilent(&h, cur, flows[i]) {
			silent = append(silent, h)
		}
		t.unwatchLocked(h.key)
	}
	return silent
}

func (w *Worker) resetCollectedClient(h helloWatch) {
	if h.ver == IPv4 {
		w.sendRSTToClientV4(h.hdr, h.ihl, h.client, h.ownPort, h.dst)
	} else {
		w.sendRSTToClientV6(h.hdr, h.client, h.dst)
	}
	log.LogConnection("TCP", h.sniSet, h.host, h.client.String(), h.cport, h.ipSet, h.dst.String(), h.port, h.mac, config.TLSVersionString(h.tls), "ipblock-collected")
}

func (w *Worker) resetParkedCollected(h helloWatch) {
	parked := w.destState.parkedCollected(h.dst, h.port, maxSilenceLookups)
	if len(parked) == 0 {
		return
	}
	flows, found := make([]ctnl.Flow, len(parked)), make([]bool, len(parked))
	for i, p := range parked {
		flows[i], found[i] = collectedFlow(p)
	}
	for _, p := range w.destState.takeParkedCollected(parked, flows, found) {
		w.resetCollectedClient(p)
	}
}

func (w *Worker) collectSilentHello(h helloWatch) {
	if w.pool == nil {
		w.destState.EndWatch(h)
		return
	}
	accepted := w.pool.collectHello(h, func() bool {
		flow, ok := collectedFlow(h)
		w.destState.watchMu.Lock()
		cur := w.destState.watches[h.key]
		if cur == nil || cur.gen != h.gen || cur.closed || !ok || !collectedFlowSilent(&h, cur, flow) {
			w.destState.watchMu.Unlock()
			return false
		}
		w.destState.unwatchLocked(h.key)
		w.destState.watchMu.Unlock()
		w.resetCollectedClient(h)
		return true
	})
	if !accepted {
		w.destState.EndWatch(h)
		return
	}
	w.resetParkedCollected(h)
}

func collectedSourceAllowed(set *config.SetConfig, scope *ingressScope) bool {
	if scope != nil && !scope.admits(set) {
		return false
	}
	if len(set.Routing.SourceInterfaces) > 0 {
		if scope == nil {
			return false
		}
		scope.resolve()
		if scope.router || scope.name == "" || !set.ListsSourceInterface(scope.name) {
			return false
		}
	}
	return true
}

func (t *destStateTracker) NoteCollectedServerPacket(key string, tcp []byte, payloadLen, packetLen int) {
	if !t.CollectWatching() || len(tcp) < TCPHeaderMinLen {
		return
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	h := t.replyWatches[key]
	if h == nil {
		h = t.watches[key]
	}
	if h == nil || !h.collect {
		return
	}
	headerLen := int(tcp[12]>>4) * 4
	if payloadLen > 0 || tcp[13]&0x05 != 0 || packetLen < TCPHeaderMinLen || headerLen < TCPHeaderMinLen || headerLen > len(tcp) {
		if payloadLen > 0 {
			t.forgetCollectRunLocked(h)
		}
		t.unwatchLocked(h.key)
		return
	}
	if tcp[13]&0x10 != 0 && h.clientHelloLen > 0 {
		acked := binary.BigEndian.Uint32(tcp[8:12]) - h.clientHelloSeq
		sent := h.clientSentEnd - h.clientHelloSeq
		if sent < 1<<31 && acked >= h.clientHelloLen && acked <= sent {
			h.clientHelloAcked = true
		}
	}
	h.observedReplyPackets++
	h.observedReplyBytes += uint64(packetLen)
}

func (t *destStateTracker) NoteCollectedClientPacket(key string, tcp []byte) {
	if !t.CollectWatching() || len(tcp) < TCPHeaderMinLen || tcp[13]&0x02 != 0 {
		return
	}
	headerLen := int(tcp[12]>>4) * 4
	if headerLen < TCPHeaderMinLen || len(tcp) <= headerLen {
		return
	}
	end := binary.BigEndian.Uint32(tcp[4:8]) + uint32(len(tcp)-headerLen)
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	h := t.watches[key]
	if h == nil || !h.collect {
		return
	}
	if offset := end - h.clientHelloSeq; offset < 1<<31 && offset > h.clientSentEnd-h.clientHelloSeq {
		h.clientSentEnd = end
	}
}
