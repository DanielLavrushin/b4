package nfq

import (
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/tables"
	"github.com/daniellavrushin/b4/utils"
)

const (
	maxCollectedIPs       = 1024
	silentHellosToCollect = 2
	maxCollectedRuns      = 1024
	maxCollectedRefusals  = 8
	collectedRetryFirst   = 10 * time.Second
	collectedRetryMax     = 5 * time.Minute
)

type collectedCandidate struct {
	setID     string
	host      string
	port      uint16
	at        time.Time
	published uint64
	lateNoted bool
	drop      bool
}

func (c collectedCandidate) until() time.Time {
	return c.at.Add(config.CollectedIPTTL)
}

type collectRun struct {
	count   int
	until   time.Time
	refused map[string]uint64
}

func collectedRefusalScope(h helloWatch) string {
	name := ""
	if h.scope != nil {
		name = h.scope.name
	}
	return strings.ToLower(h.mac) + "|" + name
}

type collectedPrepared struct {
	base    *config.Config
	ips     []string
	view    *config.Config
	matcher *sni.SuffixSet
}

func (p *Pool) SetCollectedTargetsInstaller(install func(context.Context, *config.Config, func() bool) error, changed func(*sni.SuffixSet)) {
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
	p.collectedApplyMu.Lock()
	defer p.collectedApplyMu.Unlock()
	p.configMu.Lock()
	p.collectedDirty = false
	p.configMu.Unlock()
}

func eligibleCollectedIP(ip net.IP) (netip.Addr, bool) {
	addr, ok := netip.AddrFromSlice(ip)
	addr = addr.Unmap()
	return addr, ok && addr.IsGlobalUnicast() && !addr.IsPrivate() && !utils.IsPrivateIP(ip)
}

type collectedEntry struct {
	watch helloWatch
	addr  netip.Addr
	added bool
}

type collectedTarget struct {
	addr  netip.Addr
	port  uint16
	added bool
}

type collectedOutcome int

const (
	collectedApplied collectedOutcome = iota
	collectedFailed
	collectedSuperseded
	collectedStopped
)

type collectedPublish struct {
	outcome   collectedOutcome
	mutated   bool
	unchanged bool
}

func (r collectedPublish) needsRollback() bool {
	return r.outcome == collectedFailed && r.mutated && !r.unchanged
}

func (p *Pool) SetCollectedPause(paused func() bool) {
	p.configMu.Lock()
	defer p.configMu.Unlock()
	p.collectedPaused = paused
}

func (p *Pool) collectedPass(now time.Time) {
	if p == nil || p.collectedCtx == nil || len(p.Workers) == 0 {
		return
	}
	p.collectedApplyMu.Lock()
	defer p.collectedApplyMu.Unlock()
	if p.collectedCtx.Err() != nil {
		return
	}
	w := p.Workers[0]
	p.configMu.Lock()
	paused := p.collectedPaused
	p.configMu.Unlock()
	if paused != nil && paused() {
		w.endDueCollected(now)
		return
	}
	p.applyCollected(w, w.countDueCollected(w.getConfig(), now), now)
}

func (p *Pool) applyCollected(w *Worker, completed []helloWatch, now time.Time) {
	removed := p.expireCollected(w, now)
	fresh, again := p.sortCollected(w, completed, removed)
	owned, prepared := p.ownCollected(w, fresh)
	p.configMu.Lock()
	batch := p.addCollectedLocked(w, owned, now)
	due := len(batch) > 0 || len(removed) > 0 || p.collectedDueLocked(now)
	before := p.publishedCollected()
	p.configMu.Unlock()
	if due {
		p.publishCompleted(w, batch, removed, before, prepared, now)
	}
	if len(again) > 0 {
		settler := p.newCollectedSettler()
		settler.settleAgain(w, again)
		settler.noteLate()
	}
}

func (p *Pool) expireCollected(w *Worker, now time.Time) map[netip.Addr]collectedCandidate {
	answered := w.destState.takeCollectAnswered()
	p.configMu.Lock()
	defer p.configMu.Unlock()
	for addr, candidate := range p.collected {
		if !candidate.drop && answered[iphealth.BanKey(net.IP(addr.AsSlice()), int(candidate.port))] {
			candidate.drop = true
			p.collected[addr] = candidate
		}
	}
	if now.Before(p.collectedRetryAt) {
		return nil
	}
	var gone []netip.Addr
	for addr, candidate := range p.collected {
		if candidate.drop || !now.Before(candidate.until()) {
			gone = append(gone, addr)
		}
	}
	return p.removeCollectedLocked(w.destState, gone)
}

func (p *Pool) collectedTracker() *destStateTracker {
	if len(p.Workers) > 0 && p.Workers[0].destState != nil {
		return p.Workers[0].destState
	}
	if p.state != nil {
		return p.state.destState
	}
	return nil
}

func (p *Pool) removeCollectedLocked(t *destStateTracker, addrs []netip.Addr) map[netip.Addr]collectedCandidate {
	removed := make(map[netip.Addr]collectedCandidate, len(addrs))
	for _, addr := range addrs {
		if candidate, pooled := p.collected[addr]; pooled {
			removed[addr] = candidate
			delete(p.collected, addr)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	p.collectedEpoch.Add(1)
	p.mirrorPooledLocked(t)
	return removed
}

func (p *Pool) restoreCollectedLocked(removed map[netip.Addr]collectedCandidate, drop bool) {
	if len(removed) == 0 {
		return
	}
	for addr, candidate := range removed {
		if _, back := p.collected[addr]; !back {
			if p.collected == nil {
				p.collected = make(map[netip.Addr]collectedCandidate)
			}
			candidate.drop = candidate.drop || drop
			p.collected[addr] = candidate
		}
	}
	p.collectedEpoch.Add(1)
	p.mirrorPooledLocked(p.collectedTracker())
}

func (p *Pool) mirrorPooledLocked(t *destStateTracker) {
	if t == nil {
		return
	}
	pooled := make(map[string]bool, len(p.collected))
	for addr, candidate := range p.collected {
		pooled[iphealth.BanKey(net.IP(addr.AsSlice()), int(candidate.port))] = true
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	t.collectPooled = pooled
	for key := range t.collectAnswered {
		if !pooled[key] {
			delete(t.collectAnswered, key)
		}
	}
}

func (p *Pool) publishCompleted(w *Worker, batch []collectedEntry, removed map[netip.Addr]collectedCandidate, before []string, prepared *collectedPrepared, now time.Time) {
	result := p.publishCollectedWith(false, prepared)
	p.configMu.Lock()
	var targets []collectedTarget
	switch result.outcome {
	case collectedApplied:
		targets = p.collectedSettleTargetsLocked(before, batch)
	case collectedSuperseded:
		p.collectedDirty = true
	case collectedFailed:
		p.dropCollectedLocked(w, batch)
		p.restoreCollectedLocked(removed, true)
		p.collectedDirty = p.collectedDirty || len(removed) > 0
		if p.collectedDirty {
			p.collectedRetryWait = min(max(2*p.collectedRetryWait, collectedRetryFirst), collectedRetryMax)
			p.collectedRetryAt = now.Add(p.collectedRetryWait)
		}
	}
	p.configMu.Unlock()
	if result.outcome != collectedFailed {
		w.destState.forgetCollected(removed)
	}
	if result.needsRollback() {
		p.rollbackCollected()
	}
	if result.outcome != collectedApplied {
		return
	}
	settler := p.newCollectedSettler()
	kept, gone := settler.settle(w, targets)
	settler.noteLate()
	p.takeBackCollected(gone)
	p.configMu.Lock()
	defer p.configMu.Unlock()
	for _, addr := range kept {
		name := p.collected[addr].setID
		if set := p.collectedBase.GetSetById(name); set != nil {
			name = set.Name
		}
		log.Infof("Collected IP: candidate %s from set %q", addr, name)
	}
	p.updateCollectedMetricsLocked()
}

func collectedConsumerEnabled(cfg *config.Config) bool {
	for _, set := range cfg.Sets {
		if set.Enabled && set.Targets.UseCollectedIPs {
			return true
		}
	}
	return false
}

func (p *Pool) collectedProducerLocked(h helloWatch) (netip.Addr, bool) {
	cfg := p.collectedBase
	addr, eligible := eligibleCollectedIP(h.dst)
	if !eligible || cfg == nil || p.collectedInstall == nil || p.collectedEngineTUN || cfg.Queue.IsDiscovery || cfg.Queue.Mode == "tun" || cfg.System.Tables.SkipSetup || !collectedConsumerEnabled(cfg) {
		return addr, false
	}
	producer := cfg.GetSetById(h.setID)
	return addr, producer != nil && producer.Enabled && producer.TCP.IPBlockDetect.Collects() && !producer.Targets.UseCollectedIPs
}

func (p *Pool) sortCollected(w *Worker, completed []helloWatch, removed map[netip.Addr]collectedCandidate) (fresh, again []helloWatch) {
	if len(completed) == 0 {
		return nil, nil
	}
	p.configMu.Lock()
	defer p.configMu.Unlock()
	for _, h := range completed {
		addr, usable := p.collectedProducerLocked(h)
		if _, gone := removed[addr]; !usable || gone {
			w.destState.EndWatch(h)
			continue
		}
		candidate, pooled := p.collected[addr]
		switch {
		case pooled && candidate.published != 0:
			if w.destState.parkCompletedCollected(h) {
				again = append(again, h)
			}
		case !pooled && len(p.collected) >= maxCollectedIPs:
			p.refuseCollectedLocked()
			w.destState.EndWatch(h)
		default:
			fresh = append(fresh, h)
		}
	}
	return fresh, again
}

func (p *Pool) ownCollected(w *Worker, fresh []helloWatch) ([]helloWatch, *collectedPrepared) {
	if len(fresh) == 0 {
		return nil, nil
	}
	p.configMu.Lock()
	cfg := p.collectedBase
	mark := p.collectedEpoch.Load() + 1
	pool := make(map[netip.Addr]bool, len(p.collected)+len(fresh))
	for addr := range p.collected {
		pool[addr] = true
	}
	p.configMu.Unlock()
	if cfg == nil {
		return fresh, nil
	}
	var pending []helloWatch
	for _, h := range fresh {
		if w.destState.collectRunUnowned(h, mark) {
			w.destState.EndWatch(h)
			continue
		}
		addr, _ := eligibleCollectedIP(h.dst)
		pool[addr] = true
		pending = append(pending, h)
	}
	if len(pending) == 0 {
		return nil, nil
	}
	ips := make([]string, 0, len(pool))
	for addr := range pool {
		ips = append(ips, addr.String())
	}
	sort.Strings(ips)
	prepared := &collectedPrepared{base: cfg, ips: ips, view: cfg.WithCollectedIPs(ips)}
	prepared.matcher = buildMatcher(prepared.view)
	taken := make(map[netip.Addr]bool)
	for _, h := range pending {
		if _, owned := collectedOwner(prepared.view, prepared.matcher, h); owned {
			addr, _ := eligibleCollectedIP(h.dst)
			taken[addr] = true
		}
	}
	var owned []helloWatch
	refused := make(map[netip.AddrPort]bool)
	for _, h := range pending {
		addr, _ := eligibleCollectedIP(h.dst)
		if taken[addr] {
			owned = append(owned, h)
			continue
		}
		w.destState.EndWatch(h)
		scope := collectedRefusalScope(h)
		w.destState.markCollectRunUnowned(h, scope, mark)
		w.destState.endParkedCollectedFrom(h.dst, h.port, scope)
		if key := netip.AddrPortFrom(addr, h.port); !refused[key] {
			refused[key] = true
			log.Infof("Collected IP: %s not collected: no set with Use collected IPs takes its next connection (set order, IP priority or routing)", addr)
		}
	}
	return owned, prepared
}

func (p *Pool) addCollectedLocked(w *Worker, completed []helloWatch, now time.Time) []collectedEntry {
	if len(completed) == 0 {
		return nil
	}
	var batch []collectedEntry
	for _, h := range completed {
		addr, usable := p.collectedProducerLocked(h)
		if !usable {
			w.destState.EndWatch(h)
			continue
		}
		_, existed := p.collected[addr]
		if !existed && len(p.collected) >= maxCollectedIPs {
			p.refuseCollectedLocked()
			w.destState.EndWatch(h)
			continue
		}
		if !w.destState.parkCompletedCollected(h) {
			continue
		}
		if !existed {
			if p.collected == nil {
				p.collected = make(map[netip.Addr]collectedCandidate)
			}
			p.collected[addr] = collectedCandidate{setID: h.setID, host: h.host, port: h.port, at: now}
			p.collectedEpoch.Add(1)
			p.collectedFullNoted = false
		}
		batch = append(batch, collectedEntry{watch: h, addr: addr, added: !existed})
	}
	p.mirrorPooledLocked(w.destState)
	return batch
}

func (p *Pool) refuseCollectedLocked() {
	p.collectedRefused++
	metrics.GetMetricsCollector().UpdateCollectedRefused(p.collectedRefused)
	if p.collectedFullNoted {
		return
	}
	p.collectedFullNoted = true
	log.Warnf("Collected IPs: the pool holds %d addresses, the maximum; new addresses are not collected until some expire or are removed", maxCollectedIPs)
}

func (p *Pool) dropCollectedLocked(w *Worker, batch []collectedEntry) {
	for _, e := range batch {
		if e.added {
			delete(p.collected, e.addr)
			p.collectedEpoch.Add(1)
		}
		w.destState.EndWatch(e.watch)
	}
	p.mirrorPooledLocked(w.destState)
}

func (p *Pool) publishedCollected() []string {
	p.collectedMu.RLock()
	defer p.collectedMu.RUnlock()
	return p.collectedPublished
}

func (p *Pool) collectedSettleTargetsLocked(before []string, batch []collectedEntry) []collectedTarget {
	was := make(map[string]bool, len(before))
	for _, ip := range before {
		was[ip] = true
	}
	var targets []collectedTarget
	seen := make(map[netip.AddrPort]bool)
	for _, ip := range p.publishedCollected() {
		addr, err := netip.ParseAddr(ip)
		candidate, pooled := p.collected[addr]
		if was[ip] || err != nil || !pooled {
			continue
		}
		seen[netip.AddrPortFrom(addr, candidate.port)] = true
		targets = append(targets, collectedTarget{addr: addr, port: candidate.port, added: true})
	}
	for _, e := range batch {
		key := netip.AddrPortFrom(e.addr, e.watch.port)
		if !seen[key] {
			seen[key] = true
			targets = append(targets, collectedTarget{addr: e.addr, port: e.watch.port})
		}
	}
	return targets
}

func collectedConsumerTakesAway(consumer *config.SetConfig, port uint16) bool {
	return consumer.RoutingDivertsPackets() && !config.RoutingIsBlock(consumer.Routing.Mode) || consumer.ReusesWSSize() && port == 443
}

var collectedRouteOf = tables.CollectedRouteOf

func collectedRouteTakes(set *config.SetConfig, h helloWatch) bool {
	if !set.Targets.UseCollectedIPs {
		return false
	}
	route, routed := collectedRouteOf(set.Id)
	if !routed || route.Port != 0 && route.Port != h.port {
		return false
	}
	if h.ver == IPv6 && !route.IPv6 || h.ver != IPv6 && !route.IPv4 || set.ReusesWSSize() && !set.MatchesIPVersion(h.ver) {
		return false
	}
	if h.scope != nil && h.scope.router || hostAddress(h.client) {
		return route.Router
	}
	return set.ReusesWSSize() || collectedSourceAllowed(set, h.scope)
}

func collectedTakenAway(cfg *config.Config, matcher *sni.SuffixSet, h helloWatch) bool {
	if cfg == nil || matcher == nil {
		return false
	}
	found, _, _ := matcher.MatchIPWithSourceDetail(h.dst, h.mac, func(set *config.SetConfig) bool {
		return collectedRouteTakes(set, h)
	})
	return found
}

func collectedOwner(cfg *config.Config, matcher *sni.SuffixSet, h helloWatch) (*config.SetConfig, bool) {
	if cfg == nil || matcher == nil || !cfg.IPFamilyEnabled(h.ver == IPv6) {
		return nil, false
	}
	source := func(set *config.SetConfig) bool {
		return set.MatchesTCPDPort(h.port) && collectedSourceAllowed(set, h.scope)
	}
	found, consumer, _ := matcher.MatchIPWithSourceDetail(h.dst, h.mac, func(set *config.SetConfig) bool {
		return set.Targets.UseCollectedIPs && source(set)
	})
	if !found {
		return nil, false
	}
	if collectedConsumerTakesAway(consumer, h.port) {
		return consumer, true
	}
	held, hold, prefix := matcher.MatchIPWithSourceDetail(h.dst, h.mac, source)
	if !held || hold.Id != consumer.Id {
		return consumer, false
	}
	named, rival := matcher.MatchSNIWithSourceTLS(h.host, h.mac, h.tls, h.ver, source)
	if named && rival.Id != consumer.Id && rival.MatchesTCPDPort(h.port) && !(addressHold{set: hold, prefix: prefix}).against(matcher, rival, h.mac, h.ver) {
		return consumer, false
	}
	return consumer, true
}

type collectedSettler struct {
	p       *Pool
	cfg     *config.Config
	matcher *sni.SuffixSet
	before  map[netip.Addr]uint64
	late    map[netip.Addr]string
}

func (p *Pool) newCollectedSettler() *collectedSettler {
	s := &collectedSettler{p: p, before: make(map[netip.Addr]uint64), late: make(map[netip.Addr]string)}
	if len(p.Workers) > 0 {
		s.cfg, s.matcher = p.Workers[0].getConfig(), p.Workers[0].getMatcher()
	}
	p.configMu.Lock()
	defer p.configMu.Unlock()
	for addr, candidate := range p.collected {
		if candidate.published != 0 {
			s.before[addr] = candidate.published
		}
	}
	return s
}

func (s *collectedSettler) resettable(h helloWatch) bool {
	if _, owned := collectedOwner(s.cfg, s.matcher, h); !owned {
		return false
	}
	addr, _ := eligibleCollectedIP(h.dst)
	if before := s.before[addr]; before == 0 || h.gen >= before {
		s.late[addr] = h.set
		return false
	}
	return true
}

func (s *collectedSettler) settleAgain(w *Worker, again []helloWatch) {
	seen := make(map[netip.AddrPort]bool)
	for _, h := range again {
		addr, _ := eligibleCollectedIP(h.dst)
		if key := netip.AddrPortFrom(addr, h.port); !seen[key] {
			seen[key] = true
			w.resetParkedCollected(h.dst, h.port, s.resettable)
			w.destState.endParkedCollected(h.dst, h.port)
		}
	}
}

func (s *collectedSettler) noteLate() {
	if len(s.late) == 0 {
		return
	}
	s.p.configMu.Lock()
	defer s.p.configMu.Unlock()
	for addr, set := range s.late {
		candidate, pooled := s.p.collected[addr]
		if !pooled || candidate.lateNoted {
			continue
		}
		candidate.lateNoted = true
		s.p.collected[addr] = candidate
		log.Infof("Collected IPs: %s is collected, but set %q still takes new connections to it, so they are not reset (an escalation or a rule the ownership check does not model)", addr, set)
	}
}

func (s *collectedSettler) settle(w *Worker, targets []collectedTarget) (kept, gone []netip.Addr) {
	for _, target := range targets {
		dst := net.IP(target.addr.AsSlice())
		resets := 0
		if w.destState.collectRunOpen(dst, target.port) {
			resets = w.resetParkedCollected(dst, target.port, s.resettable)
		}
		switch {
		case !target.added:
		case resets == 0:
			w.destState.endParkedCollected(dst, target.port)
			gone = append(gone, target.addr)
		default:
			kept = append(kept, target.addr)
		}
	}
	return kept, gone
}

func (p *Pool) takeBackCollected(gone []netip.Addr) {
	if len(gone) == 0 {
		return
	}
	p.configMu.Lock()
	for _, addr := range gone {
		delete(p.collected, addr)
	}
	p.collectedEpoch.Add(1)
	p.mirrorPooledLocked(p.collectedTracker())
	p.configMu.Unlock()
	result := p.publishCollected(false)
	if result.outcome == collectedApplied {
		return
	}
	if result.outcome == collectedFailed {
		log.Warnf("Collected IPs: interrupted candidate rollback incomplete for %v", gone)
	}
	p.configMu.Lock()
	p.collectedDirty = true
	p.configMu.Unlock()
	if result.needsRollback() {
		p.rollbackCollected()
	}
}

func (p *Pool) collectedWatchGen() uint64 {
	if len(p.Workers) == 0 || p.Workers[0].destState == nil {
		return 0
	}
	t := p.Workers[0].destState
	t.watchMu.RLock()
	defer t.watchMu.RUnlock()
	return t.watchGen
}

func (p *Pool) stampPublishedLocked(ips []string) {
	before := p.collectedWatchGen() + 1
	for _, ip := range ips {
		addr, err := netip.ParseAddr(ip)
		candidate, pooled := p.collected[addr]
		if err != nil || !pooled || candidate.published != 0 {
			continue
		}
		candidate.published, candidate.lateNoted = before, false
		p.collected[addr] = candidate
	}
}

func (p *Pool) publishCollected(requireConsumer bool) collectedPublish {
	return p.publishCollectedWith(requireConsumer, nil)
}

func (p *Pool) publishCollectedWith(requireConsumer bool, prepared *collectedPrepared) collectedPublish {
	p.configMu.Lock()
	cfg, install, epoch := p.collectedBase, p.collectedInstall, p.collectedEpoch.Load()
	if cfg == nil || install == nil || p.collectedCtx == nil || p.collectedCtx.Err() != nil || p.collectedEngineTUN || cfg.Queue.IsDiscovery || cfg.Queue.Mode == "tun" || cfg.System.Tables.SkipSetup || requireConsumer && !collectedConsumerEnabled(cfg) {
		p.configMu.Unlock()
		return collectedPublish{outcome: collectedFailed}
	}
	ips := make([]string, 0, len(p.collected))
	for ip := range p.collected {
		ips = append(ips, ip.String())
	}
	p.configMu.Unlock()
	sort.Strings(ips)
	unchanged := slices.Equal(ips, p.publishedCollected())
	var view *config.Config
	var matcher *sni.SuffixSet
	switch {
	case unchanged:
		view = p.CollectedConfig(cfg)
	case prepared != nil && prepared.base == cfg && slices.Equal(prepared.ips, ips):
		view, matcher = prepared.view, prepared.matcher
	default:
		view = cfg.WithCollectedIPs(ips)
	}
	rebuild := !unchanged || len(p.Workers) == 0 || p.Workers[0].getConfig() != view
	if rebuild && matcher == nil {
		matcher = buildMatcher(view)
	}
	ctx, cancel := context.WithTimeout(p.collectedCtx, 5*time.Second)
	defer cancel()
	var mutated atomic.Bool
	err := install(ctx, view, func() bool {
		current := ctx.Err() == nil && p.collectedEpoch.Load() == epoch
		if current {
			mutated.Store(true)
		}
		return current
	})
	if err == nil {
		err = ctx.Err()
	}
	result := collectedPublish{mutated: mutated.Load(), unchanged: unchanged}
	p.configMu.Lock()
	defer p.configMu.Unlock()
	switch {
	case p.collectedCtx.Err() != nil:
		result.outcome = collectedStopped
		return result
	case p.collectedEpoch.Load() != epoch:
		log.Tracef("Collected IPs: a save or a pool change superseded the install, it runs again on the next pass")
		result.outcome = collectedSuperseded
		return result
	case errors.Is(err, tables.ErrFirewallRefreshPending):
		log.Tracef("Collected IPs: interception not ready: %v", err)
		result.outcome = collectedFailed
		return result
	case err != nil:
		log.Warnf("Collected IPs: interception not ready: %v", err)
		result.outcome = collectedFailed
		return result
	}
	if rebuild {
		if len(p.Workers) > 0 {
			matcher.TransferLearnedIPs(p.Workers[0].getMatcher())
			matcher.KeepConnectionPolicy(p.Workers[0].getMatcher())
		}
		p.state.tlsCache.PublishCollected(matcher)
		for _, w := range p.Workers {
			w.cfg.Store(view)
			w.matcher.Store(matcher)
		}
	}
	p.stampPublishedLocked(ips)
	p.collectedMu.Lock()
	if !unchanged {
		p.collectedPublished = ips
		p.collectedPubGen++
	}
	p.collectedViewBase, p.collectedViewGen, p.collectedView = cfg, p.collectedPubGen, view
	p.collectedMu.Unlock()
	p.collectedDirty = false
	p.collectedRetryWait, p.collectedRetryAt = 0, time.Time{}
	if rebuild && p.collectedChanged != nil {
		p.collectedChanged(matcher)
	}
	result.outcome = collectedApplied
	return result
}

func (p *Pool) rollbackCollected() {
	p.configMu.Lock()
	cfg, install, epoch := p.collectedBase, p.collectedInstall, p.collectedEpoch.Load()
	p.configMu.Unlock()
	if cfg == nil || install == nil || p.collectedCtx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(p.collectedCtx, 5*time.Second)
	defer cancel()
	err := install(ctx, p.CollectedConfig(cfg), func() bool {
		return ctx.Err() == nil && p.collectedEpoch.Load() == epoch
	})
	if err == nil {
		err = ctx.Err()
	}
	if err == nil || p.collectedCtx.Err() != nil {
		return
	}
	if p.collectedEpoch.Load() != epoch {
		log.Tracef("Collected IPs: a save or a pool change superseded the rollback, the next pass installs the pool again")
	} else {
		log.Warnf("Collected IPs: rollback incomplete: %v", err)
	}
	p.configMu.Lock()
	p.collectedDirty = true
	p.configMu.Unlock()
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

func readCollectedFlow(h helloWatch) (ctnl.Flow, serverSilence, bool) {
	flow, found, err := helloFlow(h)
	closed := err == nil && (!found || flow.ID == h.flowID && flow.HasState && flow.TCPState != ctnl.TCPEstablished)
	switch {
	case err != nil || !found || !flow.HasCounters || flow.ID == 0 || flow.ID != h.flowID:
		return flow, silenceUnknown, closed
	case collectedReplyUnseen(h, flow):
		return flow, silenceAnswered, false
	case collectedFlowSilent(&h, &h, flow):
		return flow, silenceOpen, false
	}
	return flow, silenceUnknown, closed
}

func (t *destStateTracker) forgetCollectRunLocked(h *helloWatch) {
	if h.consumer || h.takenAway {
		return
	}
	t.forgetCollectedHistoryLocked(h)
	key := iphealth.BanKey(h.dst, int(h.port))
	if len(t.collectRuns) > 0 {
		delete(t.collectRuns, key)
	}
	if t.collectPooled[key] && !t.collectAnswered[key] && len(t.collectAnswered) < maxCollectedIPs {
		if t.collectAnswered == nil {
			t.collectAnswered = make(map[string]bool)
		}
		t.collectAnswered[key] = true
	}
}

func (t *destStateTracker) takeCollectAnswered() map[string]bool {
	if t == nil {
		return nil
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	answered := t.collectAnswered
	t.collectAnswered = nil
	return answered
}

func (t *destStateTracker) forgetCollected(removed map[netip.Addr]collectedCandidate) {
	if t == nil {
		return
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	for key, h := range t.watches {
		if addr, ok := netip.AddrFromSlice(h.dst); ok && h.collect {
			if _, gone := removed[addr.Unmap()]; gone {
				t.unwatchLocked(key)
			}
		}
	}
	for addr, candidate := range removed {
		key := iphealth.BanKey(net.IP(addr.AsSlice()), int(candidate.port))
		delete(t.collectRuns, key)
		delete(t.collectAnswered, key)
	}
}

func (t *destStateTracker) countCollectRunLocked(key string, counted bool, now time.Time, window time.Duration) (int, bool) {
	run, known := t.collectRuns[key]
	if known && !now.Before(run.until) {
		run = collectRun{}
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
			if !t.runsFullNoted {
				t.runsFullNoted = true
				log.Warnf("Collected IPs: silent connections to %d addresses are being counted, the maximum; silent connections to other addresses are not counted until some counts end", maxCollectedRuns)
			}
			return 0, false
		}
	}
	if !known {
		t.runsFullNoted = false
	}
	if t.collectRuns == nil {
		t.collectRuns = make(map[string]collectRun)
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

func (t *destStateTracker) parkCompletedCollected(h helloWatch) bool {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	cur := t.watches[h.key]
	if cur == nil || cur.gen != h.gen || cur.closed {
		return false
	}
	if !cur.parked {
		cur.probing, cur.parked = false, true
		cur.deadline = cur.deadline.Add(helloWatchTTL)
		t.addCollectedDeadlineLocked(cur)
	}
	return true
}

func (t *destStateTracker) collectRunUnowned(h helloWatch, mark uint64) bool {
	t.watchMu.RLock()
	defer t.watchMu.RUnlock()
	run, known := t.collectRuns[iphealth.BanKey(h.dst, int(h.port))]
	return known && run.refused[collectedRefusalScope(h)] == mark
}

func (t *destStateTracker) markCollectRunUnowned(h helloWatch, scope string, mark uint64) {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	run, known := t.collectRuns[iphealth.BanKey(h.dst, int(h.port))]
	if !known {
		return
	}
	if _, marked := run.refused[scope]; !marked && len(run.refused) >= maxCollectedRefusals {
		for other, at := range run.refused {
			if at != mark {
				delete(run.refused, other)
			}
		}
		if len(run.refused) >= maxCollectedRefusals {
			return
		}
	}
	if run.refused == nil {
		run.refused = make(map[string]uint64)
	}
	run.refused[scope] = mark
	t.collectRuns[iphealth.BanKey(h.dst, int(h.port))] = run
}

func (t *destStateTracker) collectRunOpen(dst net.IP, port uint16) bool {
	t.watchMu.RLock()
	defer t.watchMu.RUnlock()
	_, open := t.collectRuns[iphealth.BanKey(dst, int(port))]
	return open
}

func (t *destStateTracker) endParkedCollected(dst net.IP, port uint16) {
	t.endParkedCollectedFrom(dst, port, "")
}

func (t *destStateTracker) endParkedCollectedFrom(dst net.IP, port uint16, scope string) {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	for key, h := range t.watches {
		if h.collect && h.parked && h.isTo(dst, port) && (scope == "" || collectedRefusalScope(*h) == scope) {
			t.unwatchLocked(key)
		}
	}
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

func (w *Worker) resetParkedCollected(dst net.IP, port uint16, owned func(helloWatch) bool) int {
	var parked []helloWatch
	for _, h := range w.destState.parkedCollected(dst, port, maxSilenceLookups) {
		if owned(h) {
			parked = append(parked, h)
		}
	}
	if len(parked) == 0 {
		return 0
	}
	slices.SortFunc(parked, func(a, b helloWatch) int { return cmp.Compare(a.gen, b.gen) })
	flows, found := make([]ctnl.Flow, len(parked)), make([]bool, len(parked))
	for i, h := range parked {
		flows[i], found[i] = collectedFlow(h)
	}
	silent := w.destState.takeParkedCollected(parked, flows, found)
	for _, h := range silent {
		w.resetCollectedClient(h)
	}
	return len(silent)
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
