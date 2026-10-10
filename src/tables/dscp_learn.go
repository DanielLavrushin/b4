package tables

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	dscpLearnQueueSize  = 1024
	dscpLearnBatchMax   = 128
	dscpLearnKeysMax    = 65536
	dscpPreResolveEvery = 5 * time.Minute
)

type dscpLearnSource int

const (
	dscpLearnFromDNS dscpLearnSource = iota
	dscpLearnFromTLS
	dscpLearnFromPreResolve
)

func dscpLearnSourceOf(fromTLS bool) dscpLearnSource {
	if fromTLS {
		return dscpLearnFromTLS
	}
	return dscpLearnFromDNS
}

func (s dscpLearnSource) keep(ttl time.Duration) time.Duration {
	if s == dscpLearnFromPreResolve {
		return ttl
	}
	return ttl / 2
}

type dscpLearnTarget struct {
	sid    string
	dnsTTL time.Duration
	tlsTTL time.Duration
	v4     bool
	v6     bool
}

func (t dscpLearnTarget) ttl(source dscpLearnSource) time.Duration {
	if source == dscpLearnFromTLS {
		return t.tlsTTL
	}
	return t.dnsTTL
}

func (t dscpLearnTarget) takes(addr netip.Addr) bool {
	if addr.Is4() {
		return t.v4
	}
	return t.v6
}

type dscpLearnView struct {
	backend string
	sets    map[string]dscpLearnTarget
}

func (v *dscpLearnView) target(setID string) (dscpLearnTarget, bool) {
	if v == nil {
		return dscpLearnTarget{}, false
	}
	t, ok := v.sets[setID]
	return t, ok
}

func (v *dscpLearnView) learnsFamily(v6 bool) bool {
	if v == nil {
		return false
	}
	for _, t := range v.sets {
		if (v6 && t.v6) || (!v6 && t.v4) {
			return true
		}
	}
	return false
}

type dscpLearnKey struct {
	sid  string
	addr netip.Addr
}

type dscpLearnOpenKey struct {
	setID  string
	source dscpLearnSource
}

type dscpLearnBatch struct {
	setID  string
	sid    string
	source dscpLearnSource
	addrs  []netip.Addr
	done   chan struct{}
}

func (b *dscpLearnBatch) openKey() dscpLearnOpenKey {
	return dscpLearnOpenKey{setID: b.setID, source: b.source}
}

type dscpLearnCount struct {
	dns        uint64
	tls        uint64
	preResolve uint64
}

func (c *dscpLearnCount) add(source dscpLearnSource, n int) {
	switch source {
	case dscpLearnFromTLS:
		c.tls += uint64(n)
	case dscpLearnFromPreResolve:
		c.preResolve += uint64(n)
	default:
		c.dns += uint64(n)
	}
}

var (
	dscpLearnMu      sync.Mutex
	dscpLearnCur     atomic.Pointer[dscpLearnView]
	dscpLearnClaimMu sync.Mutex
	dscpLearnCh      chan *dscpLearnBatch
	dscpLearnKeys    = map[dscpLearnKey]time.Time{}
	dscpLearnUnion   = map[netip.Addr]time.Time{}
	dscpLearnPending = map[dscpLearnKey]*dscpLearnBatch{}
	dscpLearnOpen    = map[dscpLearnOpenKey]*dscpLearnBatch{}
	dscpLearnCounts  = map[string]dscpLearnCount{}
	dscpLearnDropped atomic.Uint64
	dscpLearnLastLog atomic.Int64
	dscpLearnNow     = time.Now
	dscpLearnLifeMu  sync.Mutex
	dscpLearnStopCh  chan struct{}
	dscpLearnDoneCh  chan struct{}
)

func DSCPLearn(cfg *config.Config, set *config.SetConfig, ips []net.IP, fromTLS bool) []<-chan struct{} {
	if cfg == nil || set == nil || len(ips) == 0 || set.Targets.DomainOnly || cfg.Queue.IsDiscovery {
		return nil
	}
	return dscpLearnClaim(set.Id, ips, dscpLearnSourceOf(fromTLS))
}

func DSCPLearnAsync(cfg *config.Config, set *config.SetConfig, ips []net.IP, fromTLS bool) {
	DSCPLearn(cfg, set, ips, fromTLS)
}

func dscpLearnAddrs(target dscpLearnTarget, ips []net.IP) []netip.Addr {
	addrs := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		if addr.IsUnspecified() || !target.takes(addr) || slices.Contains(addrs, addr) {
			continue
		}
		addrs = append(addrs, addr)
	}
	return addrs
}

func dscpLearnFreshLocked(key dscpLearnKey, now time.Time, keep time.Duration) bool {
	expiry, ok := dscpLearnKeys[key]
	return ok && expiry.Sub(now) >= keep
}

func dscpLearnJoin(waits []<-chan struct{}, done chan struct{}) []<-chan struct{} {
	for _, w := range waits {
		if w == done {
			return waits
		}
	}
	return append(waits, done)
}

func dscpLearnClaim(setID string, ips []net.IP, source dscpLearnSource) []<-chan struct{} {
	target, ok := dscpLearnCur.Load().target(setID)
	if !ok {
		return nil
	}
	addrs := dscpLearnAddrs(target, ips)
	if len(addrs) == 0 {
		return nil
	}
	keep := source.keep(target.ttl(source))

	dscpLearnClaimMu.Lock()
	defer dscpLearnClaimMu.Unlock()
	if dscpLearnCh == nil {
		return nil
	}
	now := dscpLearnNow()
	open := dscpLearnOpenKey{setID: setID, source: source}
	var waits []<-chan struct{}
	var queued []*dscpLearnBatch
	for _, addr := range addrs {
		key := dscpLearnKey{sid: target.sid, addr: addr}
		if pending, ok := dscpLearnPending[key]; ok && target.ttl(pending.source) >= target.ttl(source) {
			waits = dscpLearnJoin(waits, pending.done)
			continue
		}
		if dscpLearnFreshLocked(key, now, keep) {
			continue
		}
		batch := dscpLearnOpen[open]
		if batch == nil {
			batch = &dscpLearnBatch{setID: setID, sid: target.sid, source: source, done: make(chan struct{})}
			dscpLearnOpen[open] = batch
			queued = append(queued, batch)
		}
		batch.addrs = append(batch.addrs, addr)
		dscpLearnPending[key] = batch
		waits = dscpLearnJoin(waits, batch.done)
		if len(batch.addrs) >= dscpLearnBatchMax {
			delete(dscpLearnOpen, open)
		}
	}
	for _, batch := range queued {
		select {
		case dscpLearnCh <- batch:
		default:
			dscpLearnReleaseLocked(batch)
			dscpLearnNoteDropped()
		}
	}
	return waits
}

func dscpLearnNoteDropped() {
	n := dscpLearnDropped.Add(1)
	now := time.Now().Unix()
	last := dscpLearnLastLog.Load()
	if now-last >= 10 && dscpLearnLastLog.CompareAndSwap(last, now) {
		log.Warnf("DSCP stamp: the backlog of learned addresses is full, %d updates skipped so far; those addresses get their set's DSCP value once b4 sees them again", n)
	}
}

func dscpLearnReleaseLocked(batch *dscpLearnBatch) {
	if dscpLearnOpen[batch.openKey()] == batch {
		delete(dscpLearnOpen, batch.openKey())
	}
	for _, addr := range batch.addrs {
		key := dscpLearnKey{sid: batch.sid, addr: addr}
		if dscpLearnPending[key] == batch {
			delete(dscpLearnPending, key)
		}
	}
	close(batch.done)
}

func dscpLearnSeal(batch *dscpLearnBatch) []netip.Addr {
	dscpLearnClaimMu.Lock()
	defer dscpLearnClaimMu.Unlock()
	if dscpLearnOpen[batch.openKey()] == batch {
		delete(dscpLearnOpen, batch.openKey())
	}
	return batch.addrs
}

func dscpLearnSettle(batch *dscpLearnBatch) {
	dscpLearnClaimMu.Lock()
	defer dscpLearnClaimMu.Unlock()
	dscpLearnReleaseLocked(batch)
}

func dscpLearnStart() {
	dscpLearnLifeMu.Lock()
	defer dscpLearnLifeMu.Unlock()
	if dscpLearnStopCh != nil {
		return
	}
	ch := make(chan *dscpLearnBatch, dscpLearnQueueSize)
	dscpLearnStopCh, dscpLearnDoneCh = make(chan struct{}), make(chan struct{})
	dscpLearnClaimMu.Lock()
	dscpLearnCh = ch
	dscpLearnClaimMu.Unlock()
	go dscpLearnLoop(ch, dscpLearnStopCh, dscpLearnDoneCh)
}

func dscpLearnStop() {
	dscpLearnLifeMu.Lock()
	defer dscpLearnLifeMu.Unlock()
	if dscpLearnStopCh == nil {
		return
	}
	dscpLearnClaimMu.Lock()
	ch := dscpLearnCh
	dscpLearnCh = nil
	dscpLearnClaimMu.Unlock()
	close(dscpLearnStopCh)
	<-dscpLearnDoneCh
	dscpLearnStopCh, dscpLearnDoneCh = nil, nil
	for {
		select {
		case batch := <-ch:
			dscpLearnSettle(batch)
		default:
			return
		}
	}
}

func dscpLearnLoop(ch <-chan *dscpLearnBatch, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-stop:
			return
		case batch := <-ch:
			dscpLearnRunSafe(batch)
		}
	}
}

func dscpLearnRunSafe(batch *dscpLearnBatch) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("DSCP stamp: writing learned addresses panicked: %v", r)
		}
	}()
	dscpLearnRun(batch)
}

func dscpLearnRun(batch *dscpLearnBatch) {
	dscpLearnMu.Lock()
	defer dscpLearnMu.Unlock()
	addrs := dscpLearnSeal(batch)
	defer dscpLearnSettle(batch)
	view := dscpLearnCur.Load()
	target, ok := view.target(batch.setID)
	if !ok {
		log.Tracef("DSCP stamp: dropping %d addresses learned for set %s, its DSCP value is not applied any more", len(addrs), batch.setID)
		return
	}
	ttl := target.ttl(batch.source)
	ipt := view.backend != backendNFTables
	now, due, union := dscpLearnDue(target, addrs, ttl, batch.source.keep(ttl), ipt)
	if len(due) == 0 {
		return
	}
	var failed map[netip.Addr]bool
	if ipt {
		failed = dscpLearnWriteIpt(batch.setID, target.sid, due, union, ttl)
	} else {
		failed = dscpLearnWriteNft(target.sid, due, ttl)
	}
	dscpLearnRecord(batch, now, due, union, failed, ttl)
}

func dscpLearnDue(target dscpLearnTarget, addrs []netip.Addr, ttl, keep time.Duration, withUnion bool) (time.Time, []netip.Addr, []bool) {
	dscpLearnClaimMu.Lock()
	defer dscpLearnClaimMu.Unlock()
	now := dscpLearnNow()
	expiry := now.Add(ttl)
	var due []netip.Addr
	var union []bool
	for _, addr := range addrs {
		if !target.takes(addr) || dscpLearnFreshLocked(dscpLearnKey{sid: target.sid, addr: addr}, now, keep) {
			continue
		}
		due = append(due, addr)
		if withUnion {
			union = append(union, dscpLearnUnion[addr].Before(expiry))
		}
	}
	return now, due, union
}

func dscpLearnSeconds(ttl time.Duration) int {
	return max(1, int(ttl/time.Second))
}

func dscpLearnWriteIpt(setID, sid string, due []netip.Addr, union []bool, ttl time.Duration) map[netip.Addr]bool {
	timeout := strconv.Itoa(dscpLearnSeconds(ttl))
	names := func(i int) []string {
		v6 := due[i].Is6()
		if union[i] {
			return []string{dscpIptLearnedSet(sid, v6), dscpIptLearnedUnionSet(v6)}
		}
		return []string{dscpIptLearnedSet(sid, v6)}
	}
	var b strings.Builder
	for i, addr := range due {
		for _, name := range names(i) {
			fmt.Fprintf(&b, "add %s %s timeout %s\n", name, addr, timeout)
		}
	}
	err := runStdin(b.String(), "ipset", "restore", "-exist")
	if err == nil {
		return nil
	}
	log.Tracef("DSCP stamp: loading %d addresses learned for set %s in one ipset restore failed (%v), adding them one by one", len(due), setID, err)
	failed := map[netip.Addr]bool{}
	for i, addr := range due {
		for _, name := range names(i) {
			if out, err := run("ipset", "add", name, addr.String(), "timeout", timeout, "-exist"); err != nil {
				log.Tracef("DSCP stamp: could not add the learned address %s to the ipset %s, b4 tries again when it sees the address next: %s", addr, name, iptErrText(out, err))
				failed[addr] = true
				break
			}
		}
	}
	return failed
}

func dscpLearnWriteNft(sid string, due []netip.Addr, ttl time.Duration) map[netip.Addr]bool {
	seconds := dscpLearnSeconds(ttl)
	var failed map[netip.Addr]bool
	for _, v6 := range []bool{false, true} {
		var ips []string
		for _, addr := range due {
			if addr.Is6() == v6 {
				ips = append(ips, addr.String())
			}
		}
		if len(ips) == 0 {
			continue
		}
		for _, ip := range routeNftRefreshElements(dscpNftTable, dscpNftLearnedSet(sid, v6), ips, seconds) {
			if addr, err := netip.ParseAddr(ip); err == nil {
				if failed == nil {
					failed = map[netip.Addr]bool{}
				}
				failed[addr] = true
			}
		}
	}
	return failed
}

func dscpLearnRecord(batch *dscpLearnBatch, now time.Time, due []netip.Addr, union []bool, failed map[netip.Addr]bool, ttl time.Duration) {
	dscpLearnClaimMu.Lock()
	defer dscpLearnClaimMu.Unlock()
	expiry := now.Add(ttl)
	written := 0
	for i, addr := range due {
		if failed[addr] {
			continue
		}
		dscpLearnKeys[dscpLearnKey{sid: batch.sid, addr: addr}] = expiry
		if i < len(union) && union[i] {
			dscpLearnUnion[addr] = expiry
		}
		written++
	}
	dscpLearnPrune(dscpLearnKeys, now)
	dscpLearnPrune(dscpLearnUnion, now)
	if written == 0 {
		return
	}
	count := dscpLearnCounts[batch.setID]
	count.add(batch.source, written)
	dscpLearnCounts[batch.setID] = count
}

func dscpLearnPrune[K comparable](expiries map[K]time.Time, now time.Time) {
	if len(expiries) < dscpLearnKeysMax {
		return
	}
	for k, expiry := range expiries {
		if !expiry.After(now) {
			delete(expiries, k)
		}
	}
	keep := dscpLearnKeysMax * 3 / 4
	if len(expiries) <= keep {
		return
	}
	order := make([]time.Time, 0, len(expiries))
	for _, expiry := range expiries {
		order = append(order, expiry)
	}
	slices.SortFunc(order, time.Time.Compare)
	cut := order[len(order)-keep]
	for k, expiry := range expiries {
		if expiry.Before(cut) {
			delete(expiries, k)
		}
	}
	for k := range expiries {
		if len(expiries) <= keep {
			return
		}
		delete(expiries, k)
	}
}

func dscpLearnViewOf(st *dscpState) *dscpLearnView {
	if st == nil || st.plan.empty() {
		return nil
	}
	view := &dscpLearnView{backend: st.backend, sets: make(map[string]dscpLearnTarget, len(st.plan.sets))}
	for _, set := range st.plan.sets {
		if !set.learn {
			continue
		}
		target := dscpLearnTarget{
			sid:    set.sid,
			dnsTTL: set.dnsTTL,
			tlsTTL: set.tlsTTL,
			v4:     set.v4 && st.learnsInto(set.sid, false),
			v6:     set.v6 && st.learnsInto(set.sid, true),
		}
		if target.v4 || target.v6 {
			view.sets[set.id] = target
		}
	}
	if len(view.sets) == 0 {
		return nil
	}
	return view
}

func (st *dscpState) learnsInto(sid string, v6 bool) bool {
	if st.backend == backendNFTables {
		return st.nft.perSet() && slices.Contains(st.nft.learned, sid)
	}
	return st.ipt != nil && slices.Contains(st.ipt.sets, dscpIptLearnedSet(sid, v6)) && slices.Contains(st.ipt.sets, dscpIptLearnedUnionSet(v6))
}

func dscpLearnAdopt(st *dscpState, rebuilt bool) {
	if dscpLearnSwap(dscpLearnViewOf(st), rebuilt) {
		dscpPreResolveForget()
	}
}

func dscpLearnSwap(view *dscpLearnView, rebuilt bool) bool {
	dscpLearnMu.Lock()
	defer dscpLearnMu.Unlock()
	prev := dscpLearnCur.Swap(view)
	dscpLearnClaimMu.Lock()
	defer dscpLearnClaimMu.Unlock()
	reset := rebuilt || prev == nil || view == nil || prev.backend != view.backend
	if reset {
		clear(dscpLearnKeys)
		clear(dscpLearnUnion)
	} else {
		dscpLearnForgetDepartedLocked(prev, view)
	}
	for id := range dscpLearnCounts {
		if _, ok := view.target(id); !ok {
			delete(dscpLearnCounts, id)
		}
	}
	return reset && prev != nil
}

type dscpLearnFamily struct {
	sid string
	v6  bool
}

func dscpLearnForgetDepartedLocked(prev, view *dscpLearnView) {
	if prev == nil {
		return
	}
	gone := map[dscpLearnFamily]bool{}
	for id, was := range prev.sets {
		now, _ := view.target(id)
		if was.v4 && !now.v4 {
			gone[dscpLearnFamily{sid: was.sid}] = true
		}
		if was.v6 && !now.v6 {
			gone[dscpLearnFamily{sid: was.sid, v6: true}] = true
		}
	}
	if len(gone) > 0 {
		for key := range dscpLearnKeys {
			if gone[dscpLearnFamily{sid: key.sid, v6: key.addr.Is6()}] {
				delete(dscpLearnKeys, key)
			}
		}
	}
	for _, v6 := range []bool{false, true} {
		if view.learnsFamily(v6) {
			continue
		}
		for addr := range dscpLearnUnion {
			if addr.Is6() == v6 {
				delete(dscpLearnUnion, addr)
			}
		}
	}
}

func dscpLearnForgetSet(sid string, v6 bool) {
	dscpLearnMu.Lock()
	defer dscpLearnMu.Unlock()
	dscpLearnClaimMu.Lock()
	defer dscpLearnClaimMu.Unlock()
	for key := range dscpLearnKeys {
		if key.addr.Is6() == v6 && (sid == "" || key.sid == sid) {
			delete(dscpLearnKeys, key)
		}
	}
	if sid != "" {
		return
	}
	for addr := range dscpLearnUnion {
		if addr.Is6() == v6 {
			delete(dscpLearnUnion, addr)
		}
	}
}

func dscpLearnForgetBackend(backend string) {
	dscpLearnMu.Lock()
	defer dscpLearnMu.Unlock()
	if view := dscpLearnCur.Load(); view == nil || view.backend != backend {
		return
	}
	dscpLearnClaimMu.Lock()
	clear(dscpLearnKeys)
	clear(dscpLearnUnion)
	dscpLearnClaimMu.Unlock()
}

func dscpLearnStats() map[string]dscpLearnCount {
	dscpLearnClaimMu.Lock()
	defer dscpLearnClaimMu.Unlock()
	out := make(map[string]dscpLearnCount, len(dscpLearnCounts))
	for id, count := range dscpLearnCounts {
		out[id] = count
	}
	return out
}

type dscpPreRun struct {
	domains  string
	resolver string
	last     time.Time
	running  bool
	cancel   context.CancelFunc
}

var (
	dscpPreMu     sync.Mutex
	dscpPreRuns   = map[string]*dscpPreRun{}
	dscpPreCtx    context.Context
	dscpPreCancel context.CancelFunc
	dscpPreWG     sync.WaitGroup
	dscpPreLookup = routeResolveHostContext
)

func dscpPreResolveStart() {
	dscpPreMu.Lock()
	defer dscpPreMu.Unlock()
	if dscpPreCancel != nil {
		return
	}
	dscpPreCtx, dscpPreCancel = context.WithCancel(context.Background())
	clear(dscpPreRuns)
}

func dscpPreResolveStop() {
	dscpPreMu.Lock()
	cancel := dscpPreCancel
	dscpPreCtx, dscpPreCancel = nil, nil
	dscpPreMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	dscpPreWG.Wait()
}

func dscpPreResolveForget() {
	dscpPreMu.Lock()
	defer dscpPreMu.Unlock()
	for id, run := range dscpPreRuns {
		if !run.running {
			delete(dscpPreRuns, id)
		}
	}
}

func dscpPreResolveInterval(member dscpPlanSet) time.Duration {
	return max(member.dnsTTL/2, dscpPreResolveEvery)
}

func dscpPreResolverKey(cfg *config.Config, set *config.SetConfig) string {
	d := set.DNS
	return fmt.Sprintf("%t %q %q %t %t %t", d.Enabled, d.TargetDNS, d.DoHURL, d.Strict, cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled)
}

func dscpPreResolve(plan *dscpPlan, cfg *config.Config, _ bool) {
	st := dscpApplied.Load()
	if plan.empty() || st == nil || st.plan != plan {
		return
	}
	if cfg == nil {
		cfg = st.cfg
	}
	if cfg == nil {
		return
	}
	view := dscpLearnCur.Load()
	dscpPreMu.Lock()
	defer dscpPreMu.Unlock()
	if dscpPreCtx == nil {
		return
	}
	now := dscpLearnNow()
	listed := make(map[string]bool, len(plan.sets))
	for _, member := range plan.sets {
		if len(member.domains) == 0 {
			continue
		}
		if _, ok := view.target(member.id); !ok {
			continue
		}
		set := cfg.GetSetById(member.id)
		if set == nil {
			continue
		}
		listed[member.id] = true
		key, resolver := strings.Join(member.domains, "\n"), dscpPreResolverKey(cfg, set)
		prev := dscpPreRuns[member.id]
		same := prev != nil && prev.domains == key && prev.resolver == resolver
		if same && (prev.running || now.Sub(prev.last) < dscpPreResolveInterval(member)-dscpSyncTick/2) {
			continue
		}
		if prev != nil && prev.running {
			log.Tracef("DSCP stamp: the domains or the resolver of set %s changed during its lookups, so b4 drops that run and starts again", set.Name)
			prev.cancel()
		}
		if (prev == nil || prev.domains != key) && len(member.domains) == dscpPreResolveCap && len(set.Targets.SNIDomains) > dscpPreResolveCap {
			log.Infof("DSCP stamp: set %s lists more than %d domains, so b4 looks up only the first %d in advance and learns the addresses of the others from DNS answers and TLS names", set.Name, dscpPreResolveCap, dscpPreResolveCap)
		}
		ctx, cancel := context.WithCancel(dscpPreCtx)
		run := &dscpPreRun{domains: key, resolver: resolver, last: now, running: true, cancel: cancel}
		dscpPreRuns[member.id] = run
		dscpPreWG.Add(1)
		go dscpPreResolveSet(ctx, run, cfg, set, member.domains)
	}
	for id, run := range dscpPreRuns {
		if listed[id] {
			continue
		}
		if run.running {
			run.cancel()
		}
		delete(dscpPreRuns, id)
	}
}

func dscpPreResolveSet(ctx context.Context, run *dscpPreRun, cfg *config.Config, set *config.SetConfig, domains []string) {
	defer dscpPreWG.Done()
	defer dscpPreResolveDone(set.Id, run)
	learned := 0
	for _, domain := range domains {
		if ctx.Err() != nil {
			return
		}
		ips := dscpPreLookup(ctx, cfg, set, domain)
		if len(ips) == 0 {
			continue
		}
		if !dscpPreSubmit(set.Id, run, ips) {
			return
		}
		learned += len(ips)
	}
	log.Tracef("DSCP stamp: looked up %d domains of set %s in advance and passed %d addresses on to its DSCP value", len(domains), set.Name, learned)
}

func dscpPreSubmit(setID string, run *dscpPreRun, ips []net.IP) bool {
	dscpPreMu.Lock()
	defer dscpPreMu.Unlock()
	if dscpPreRuns[setID] != run {
		return false
	}
	dscpLearnClaim(setID, ips, dscpLearnFromPreResolve)
	return true
}

func dscpPreResolveDone(setID string, run *dscpPreRun) {
	run.cancel()
	dscpPreMu.Lock()
	defer dscpPreMu.Unlock()
	if dscpPreRuns[setID] == run {
		run.running = false
	}
}
