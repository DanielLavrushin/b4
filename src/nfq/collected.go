package nfq

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"sort"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/utils"
)

const maxCollectedIPs = 1024

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
	p.collectedMu.RLock()
	ips := p.collectedPublished
	p.collectedMu.RUnlock()
	return cfg.WithCollectedIPs(ips)
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
	view := cfg.WithCollectedIPs(ips)
	ctx, cancel := context.WithTimeout(p.collectedCtx, 5*time.Second)
	defer cancel()
	installErr := p.collectedInstall(ctx, view)
	if installErr == nil {
		installErr = ctx.Err()
	}
	if err := installErr; err != nil {
		log.Warnf("Collected IPs: interception not ready: %v", err)
		if p.collectedCtx.Err() != nil {
			return false
		}
		rollback, done := context.WithTimeout(p.collectedCtx, 5*time.Second)
		defer done()
		if err := p.collectedInstall(rollback, p.CollectedConfig(cfg)); err != nil {
			log.Warnf("Collected IPs: rollback incomplete: %v", err)
		}
		return false
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
	p.collectedPublished = ips
	p.collectedMu.Unlock()
	if p.collectedChanged != nil {
		p.collectedChanged(matcher)
	}
	return true
}

func (p *Pool) collectedDueLocked() bool {
	if !p.collectedDirty {
		return false
	}
	if !p.collectedRelevantLocked(p.collectedBase) {
		p.collectedDirty = false
		return false
	}
	return true
}

func (p *Pool) reconcileCollected() {
	if p == nil || p.collectedCtx == nil || p.collectedCtx.Err() != nil {
		return
	}
	p.configMu.Lock()
	due := p.collectedDueLocked()
	p.configMu.Unlock()
	if !due {
		return
	}
	unlock := config.LockWrites()
	defer unlock()
	p.configMu.Lock()
	defer p.configMu.Unlock()
	if !p.collectedDueLocked() {
		return
	}
	p.collectedDirty = false
	if p.publishCollectedViewLocked(false) {
		p.updateCollectedMetricsLocked()
	}
}

func collectedFlow(h helloWatch) (ctnl.Flow, bool) {
	flow, found, err := helloFlow(h)
	if err != nil || !found || !flow.HasState || flow.TCPState != ctnl.TCPEstablished || !flow.HasCounters || flow.ID == 0 {
		return ctnl.Flow{}, false
	}
	return flow, true
}

func collectedStillSilent(h helloWatch) bool {
	if h.clientHelloLen == 0 || !h.clientHelloAcked {
		return false
	}
	flow, ok := collectedFlow(h)
	if !ok || flow.ID != h.flowID {
		return false
	}
	return flow.ReplyBytes == h.observedReplyBytes && flow.ReplyPackets == h.observedReplyPackets &&
		(!h.replyChecked || flow.ReplyBytes == h.replyBytes && flow.ReplyPackets == h.replyPackets)
}

func (w *Worker) collectSilentHello(h helloWatch) {
	w.destState.watchMu.RLock()
	cur := w.destState.watches[h.key]
	if cur == nil || cur.gen != h.gen || cur.closed {
		w.destState.watchMu.RUnlock()
		return
	}
	h = *cur
	w.destState.watchMu.RUnlock()
	if !collectedStillSilent(h) {
		log.Tracef("Collected IPs: no reliable silent-flow evidence for %s", h.key)
		w.destState.EndWatch(h)
		return
	}
	flow, ok := collectedFlow(h)
	if !ok || flow.ReplyBytes != h.observedReplyBytes || flow.ReplyPackets != h.observedReplyPackets {
		w.destState.EndWatch(h)
		return
	}
	h.replyBytes, h.replyPackets, h.replyChecked = flow.ReplyBytes, flow.ReplyPackets, true
	if w.pool == nil {
		w.destState.EndWatch(h)
		return
	}
	accepted := w.pool.collectHello(h, func() bool {
		w.destState.watchMu.Lock()
		defer w.destState.watchMu.Unlock()
		cur := w.destState.watches[h.key]
		if cur == nil || cur.gen != h.gen || cur.closed || !collectedStillSilent(h) {
			return false
		}
		w.destState.unwatchLocked(h.key)
		if h.ver == IPv4 {
			w.sendRSTToClientV4(h.hdr, h.ihl, h.client, h.ownPort, h.dst)
		} else {
			w.sendRSTToClientV6(h.hdr, h.client, h.dst)
		}
		log.LogConnection("TCP", h.sniSet, h.host, h.client.String(), h.cport, h.ipSet, h.dst.String(), h.port, h.mac, config.TLSVersionString(h.tls), "ipblock-collected")
		return true
	})
	if !accepted {
		w.destState.EndWatch(h)
	}
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
	if !t.Watching() || len(tcp) < TCPHeaderMinLen {
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
	if len(tcp) < TCPHeaderMinLen || tcp[13]&0x02 != 0 {
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
