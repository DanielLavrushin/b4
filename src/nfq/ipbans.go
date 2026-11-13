package nfq

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
)

const (
	banProbeParallel  = 4
	banRetestsPerTick = 8
	banPendingMax     = 64
	banAnsweredMax    = 1024
	banSilentMax      = 1024
	silentHellosToBan = 2
)

type banProber func(ctx context.Context, ip net.IP, port int, mark int) iphealth.TLSResult

type banHostProber func(ctx context.Context, ip net.IP, port int, host string, bound func(localPort int)) iphealth.TLSResult

type banAddr struct {
	ip   netip.Addr
	port uint16
}

type silentRun struct {
	count int
	until time.Time
}

type hostRetest struct {
	sport     atomic.Uint32
	seen      atomic.Bool
	handedOff atomic.Bool
}

func (t *hostRetest) note(handedOff bool) {
	t.seen.Store(true)
	if handedOff {
		t.handedOff.Store(true)
	}
}

type ipBans struct {
	reg         *iphealth.BanRegistry
	good        *iphealth.KnownGood
	probe       banProber
	hostProbe   banHostProber
	prepare     func(ip net.IP, port int, host string)
	retestMu    sync.RWMutex
	retests     map[banAddr]*hostRetest
	retesting   atomic.Int32
	enabled     atomic.Bool
	flushFailed atomic.Bool
	answerTTL   atomic.Int64
	mu          sync.Mutex
	inflight    map[string]bool
	answered    map[string]time.Time
	silent      map[string]silentRun
	silentAddrs atomic.Int32
	sem         chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	changed     func()
	settled     func(ip net.IP, port int, banned bool)
	gotAnswer   func(ip net.IP, port int) bool
}

func newIPBans(reg *iphealth.BanRegistry, good *iphealth.KnownGood, probe banProber) *ipBans {
	if probe == nil {
		probe = iphealth.ProbeTLS
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &ipBans{
		reg:      reg,
		good:     good,
		probe:    probe,
		inflight: make(map[string]bool),
		answered: make(map[string]time.Time),
		silent:   make(map[string]silentRun),
		sem:      make(chan struct{}, banProbeParallel),
		ctx:      ctx,
		cancel:   cancel,
	}
	b.answerTTL.Store(int64(config.DefaultIPHealthRetestSec * time.Second))
	return b
}

func (b *ipBans) applyConfig(cfg *config.Config) {
	if b == nil {
		return
	}
	b.enabled.Store(cfg != nil && !cfg.Queue.IsDiscovery && cfg.System.IPHealth.BansEnabled())
	if cfg != nil {
		b.answerTTL.Store(int64(cfg.System.IPHealth.RetestInterval()))
	}
}

func (b *ipBans) on() bool {
	return b != nil && b.enabled.Load()
}

func (b *ipBans) banned(ip net.IP, port uint16) bool {
	return b.on() && b.reg.Banned(ip, int(port))
}

func (b *ipBans) bannedIP(ip net.IP) bool {
	return b.on() && b.reg.BannedIP(ip)
}

func (b *ipBans) any() bool {
	return b.on() && b.reg.Len() > 0
}

func (b *ipBans) bannedKey(dstIPPort string) bool {
	if !b.any() {
		return false
	}
	ip, port, ok := iphealth.ParseBanKey(dstIPPort)
	return ok && b.reg.Banned(ip, port)
}

func banEnforced(set *config.SetConfig) bool {
	return set != nil && set.TCP.IPBlockDetect.Enabled && set.TCP.IPBlockDetect.CacheBlockedIPs
}

func (w *Worker) refusesBannedQUIC(cfg *config.Config, set *config.SetConfig, dst net.IP, dport uint16) bool {
	return cfg != nil && !cfg.Queue.IsDiscovery && banEnforced(set) && !routingBlocks(set) && w.bans.banned(dst, dport) && !set.RoutingHandsOffPackets()
}

func (b *ipBans) confirm(cfg *config.Config, ip net.IP, port uint16, host, setName string) bool {
	return b.check(cfg, ip, port, host, setName, false)
}

func (b *ipBans) confirmSilence(cfg *config.Config, ip net.IP, port uint16, host, setName string) bool {
	return b.check(cfg, ip, port, host, setName, true)
}

func (b *ipBans) check(cfg *config.Config, ip net.IP, port uint16, host, setName string, bySilence bool) bool {
	if !b.on() || cfg == nil || ip == nil {
		return false
	}
	addr := append(net.IP(nil), ip...)
	first, repeat := cfg.System.IPHealth.BanTTL(), cfg.System.IPHealth.RepeatBanTTL()
	key := iphealth.BanKey(addr, int(port))
	mark := int(cfg.MainInjectedMark())
	if b.run(key, bySilence, func(ctx context.Context) iphealth.TLSResult {
		return b.probe(ctx, addr, int(port), mark)
	}, func(res iphealth.TLSResult) {
		b.settleStrike(key, addr, int(port), host, setName, first, repeat, res)
	}) {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	_, busy := b.inflight[key]
	if busy && !bySilence {
		b.inflight[key] = false
	}
	return busy
}

func (b *ipBans) settleStrike(key string, ip net.IP, port int, host, setName string, first, repeat time.Duration, res iphealth.TLSResult) {
	banned := false
	defer func() { b.settle(ip, port, banned) }()
	switch res {
	case iphealth.TLSReplied:
		log.Infof("IP bans: %s answers TLS for neutral names, so the address is not banned (host: %s, set: %s)", key, host, setName)
		b.rememberAnswer(key)
		b.forgetSilentKey(key)
		return
	case iphealth.TLSUnreachable:
		log.Tracef("IP bans: %s did not take a TCP connection from the router, not banning it (host: %s, set: %s)", key, host, setName)
		return
	}
	if !b.on() {
		return
	}
	if b.startedBySilence(key) && !b.stillSilent(key, ip, port) {
		log.Infof("IP bans: %s answered a device while it was checked with neutral names, so the address is not banned (host: %s, set: %s)", key, host, setName)
		return
	}
	until, isRepeat, ok := b.reg.Strike(ip, port, host, setName, first, repeat)
	if !ok {
		return
	}
	banned = true
	b.forgetSilentKey(key)
	b.good.Forget(host, ip)
	log.Infof("IP bans: %s does not answer TLS for neutral names either, banned until %s (host: %s, set: %s, repeat: %t)", key, until.Format("2006-01-02 15:04:05"), host, setName, isRepeat)
	b.notify()
}

func (b *ipBans) notify() {
	if b.changed != nil {
		b.changed()
	}
}

func (b *ipBans) settle(ip net.IP, port int, banned bool) {
	if b.settled != nil {
		b.settled(ip, port, banned)
	}
}

func (b *ipBans) rememberAnswer(key string) {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.answered) >= banAnsweredMax {
		b.pruneAnsweredLocked(now)
		if len(b.answered) >= banAnsweredMax {
			return
		}
	}
	b.answered[key] = now.Add(time.Duration(b.answerTTL.Load()))
}

func (b *ipBans) answeredRecently(ip net.IP, port uint16, now time.Time) bool {
	if b == nil || ip == nil {
		return false
	}
	key := iphealth.BanKey(ip, int(port))
	b.mu.Lock()
	defer b.mu.Unlock()
	until, ok := b.answered[key]
	if ok && !now.Before(until) {
		delete(b.answered, key)
		return false
	}
	return ok
}

func (b *ipBans) pruneAnsweredLocked(now time.Time) {
	for key, until := range b.answered {
		if !now.Before(until) {
			delete(b.answered, key)
		}
	}
}

func (b *ipBans) countSilentHello(key string, now time.Time) int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	run, known := b.silent[key]
	if known && !now.Before(run.until) {
		run.count = 0
	}
	if !known && len(b.silent) >= banSilentMax {
		b.pruneSilentLocked(now)
		if len(b.silent) >= banSilentMax {
			return 0
		}
	}
	run.count++
	run.until = now.Add(time.Duration(b.answerTTL.Load()))
	b.silent[key] = run
	b.silentAddrs.Store(int32(len(b.silent)))
	return run.count
}

func (b *ipBans) silentHellos(key string, now time.Time) int {
	if b == nil || b.silentAddrs.Load() == 0 {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	run, known := b.silent[key]
	if !known || !now.Before(run.until) {
		return 0
	}
	return run.count
}

func (b *ipBans) forgetSilentHellos(ip net.IP, port uint16) {
	if b == nil || ip == nil || b.silentAddrs.Load() == 0 {
		return
	}
	b.forgetSilentKey(iphealth.BanKey(ip, int(port)))
}

func (b *ipBans) forgetSilentKey(key string) {
	if b.silentAddrs.Load() == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.silent, key)
	b.silentAddrs.Store(int32(len(b.silent)))
}

func (b *ipBans) pruneSilentLocked(now time.Time) {
	for key, run := range b.silent {
		if _, checking := b.inflight[key]; !checking && !now.Before(run.until) {
			delete(b.silent, key)
		}
	}
	b.silentAddrs.Store(int32(len(b.silent)))
}

func (b *ipBans) startedBySilence(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inflight[key]
}

func (b *ipBans) stillSilent(key string, ip net.IP, port int) bool {
	if b.gotAnswer != nil && b.gotAnswer(ip, port) {
		b.forgetSilentKey(key)
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.silent[key].count >= silentHellosToBan
}

func (b *ipBans) retest(retest time.Duration, mark int) {
	if !b.any() {
		return
	}
	for _, ban := range b.reg.Due(retest, banRetestsPerTick) {
		ip := net.ParseIP(ban.IP)
		if ip == nil {
			continue
		}
		key, port := ban.Key(), ban.Port
		host := ""
		if b.hostProbe != nil {
			host = ban.Host
		}
		byHost := false
		b.run(key, false, func(ctx context.Context) iphealth.TLSResult {
			res, named := b.retestOnce(ctx, ip, port, mark, host)
			byHost = named
			return res
		}, func(res iphealth.TLSResult) {
			b.settleRetest(key, host, res, byHost)
		})
	}
}

func (b *ipBans) retestOnce(ctx context.Context, ip net.IP, port, mark int, host string) (iphealth.TLSResult, bool) {
	if host == "" {
		return b.probe(ctx, ip, port, mark), false
	}
	named := make(chan bool, 1)
	go func() {
		named <- b.answersForHost(ctx, ip, port, host)
	}()
	res := b.probe(ctx, ip, port, mark)
	if byHost := <-named; byHost && res != iphealth.TLSReplied {
		return iphealth.TLSReplied, true
	}
	return res, false
}

func (b *ipBans) answersForHost(ctx context.Context, ip net.IP, port int, host string) bool {
	flow, done := b.beginHostRetest(ip, port)
	if flow == nil {
		return false
	}
	defer done()
	if b.prepare != nil {
		b.prepare(ip, port, host)
	}
	res := b.hostProbe(ctx, ip, port, host, func(localPort int) { flow.sport.Store(uint32(localPort)) })
	if res != iphealth.TLSReplied {
		return false
	}
	switch {
	case !flow.seen.Load():
		log.Tracef("IP bans: %s answered for %s, but the re-test's ClientHello did not pass through b4, so the answer does not count", iphealth.BanKey(ip, port), host)
		return false
	case flow.handedOff.Load():
		log.Tracef("IP bans: %s answered for %s, but the re-test left through a proxy or another route, so the answer does not count", iphealth.BanKey(ip, port), host)
		return false
	}
	return true
}

func banAddrOf(ip net.IP, port int) (banAddr, bool) {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok || port <= 0 || port > 65535 {
		return banAddr{}, false
	}
	return banAddr{ip: addr.Unmap(), port: uint16(port)}, true
}

func (b *ipBans) beginHostRetest(ip net.IP, port int) (*hostRetest, func()) {
	key, ok := banAddrOf(ip, port)
	if b == nil || !ok {
		return nil, nil
	}
	flow := &hostRetest{}
	b.retestMu.Lock()
	if _, busy := b.retests[key]; busy {
		b.retestMu.Unlock()
		return nil, nil
	}
	if b.retests == nil {
		b.retests = make(map[banAddr]*hostRetest)
	}
	b.retests[key] = flow
	b.retesting.Add(1)
	b.retestMu.Unlock()
	return flow, func() {
		b.retestMu.Lock()
		delete(b.retests, key)
		b.retesting.Add(-1)
		b.retestMu.Unlock()
	}
}

func (b *ipBans) hostRetestTo(ip net.IP, port uint16) *hostRetest {
	if b == nil || b.retesting.Load() == 0 {
		return nil
	}
	key, ok := banAddrOf(ip, int(port))
	if !ok {
		return nil
	}
	b.retestMu.RLock()
	defer b.retestMu.RUnlock()
	return b.retests[key]
}

func (w *Worker) hostRetest(vc *verdictCtx, pkt *pktInfo, sport, dport uint16) *hostRetest {
	if w == nil {
		return nil
	}
	flow := w.bans.hostRetestTo(pkt.dst, dport)
	if flow == nil || sport == 0 || flow.sport.Load() != uint32(sport) {
		return nil
	}
	if !(vc.routerOriginated() || hostAddress(pkt.src)) {
		return nil
	}
	return flow
}

var routeSource = func(ip net.IP, port int) net.IP {
	conn, err := net.Dial("udp", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
	if err != nil {
		return nil
	}
	defer conn.Close()
	if local, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return local.IP
	}
	return nil
}

func (w *Worker) hintBanRetest(ip net.IP, port int, host string) {
	if w == nil || ip == nil || host == "" {
		return
	}
	cfg, matcher := w.getConfig(), w.getMatcher()
	if cfg == nil || matcher == nil || cfg.Queue.IsDiscovery {
		return
	}
	matched, set := matcher.MatchSNIWithSource(host, "")
	if !matched || set == nil || !set.Enabled {
		return
	}
	local := routeSource(ip, port)
	if local == nil {
		return
	}
	w.storeHostHint(local.String(), ip.String(), set, host, "ban re-test", 0, 0)
}

func (b *ipBans) settleRetest(key, host string, res iphealth.TLSResult, byHost bool) {
	lifted := b.reg.RecordCheck(key, res == iphealth.TLSReplied)
	if ip, port, ok := iphealth.ParseBanKey(key); ok {
		defer b.settle(ip, port, b.reg.Banned(ip, port))
	}
	switch {
	case lifted && byHost:
		log.Infof("IP bans: %s answered TLS for %s, the name it was banned for, in several re-tests in a row, the ban is lifted", key, host)
		b.notify()
	case lifted:
		log.Infof("IP bans: %s answered TLS for neutral names in several re-tests in a row, the ban is lifted", key)
		b.notify()
	case byHost:
		log.Tracef("IP bans: re-test of %s: answered for %s", key, host)
	default:
		log.Tracef("IP bans: re-test of %s: %s", key, res)
	}
}

func (b *ipBans) run(key string, bySilence bool, probe func(context.Context) iphealth.TLSResult, done func(iphealth.TLSResult)) bool {
	b.mu.Lock()
	if b.ctx.Err() != nil {
		b.mu.Unlock()
		return false
	}
	if _, busy := b.inflight[key]; busy || len(b.inflight) >= banPendingMax {
		b.mu.Unlock()
		return false
	}
	b.inflight[key] = bySilence
	b.wg.Add(1)
	b.mu.Unlock()

	go func() {
		defer b.wg.Done()
		defer b.release(key)
		select {
		case b.sem <- struct{}{}:
		case <-b.ctx.Done():
			return
		}
		res := probe(b.ctx)
		<-b.sem
		if b.ctx.Err() != nil {
			return
		}
		done(res)
	}()
	return true
}

func (b *ipBans) release(key string) {
	b.mu.Lock()
	delete(b.inflight, key)
	b.mu.Unlock()
}

func (b *ipBans) tick(cfg *config.Config) {
	if b == nil {
		return
	}
	b.reg.Prune()
	now := time.Now()
	b.mu.Lock()
	b.pruneAnsweredLocked(now)
	b.pruneSilentLocked(now)
	b.mu.Unlock()
	if cfg != nil {
		b.retest(cfg.System.IPHealth.RetestInterval(), int(cfg.MainInjectedMark()))
	}
	b.flush()
}

func (b *ipBans) flush() {
	if b == nil {
		return
	}
	if err := b.reg.Flush(); err != nil {
		if !b.flushFailed.Swap(true) {
			log.Warnf("IP bans: could not save %s, the bans are kept in memory and saved again later: %v", b.reg.Path(), err)
		} else {
			log.Tracef("IP bans: still cannot save %s: %v", b.reg.Path(), err)
		}
		return
	}
	b.flushFailed.Store(false)
}

func (b *ipBans) stop() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.cancel()
	b.mu.Unlock()
	b.wg.Wait()
	b.flush()
}

func (b *ipBans) list() []iphealth.Ban {
	if !b.on() {
		return nil
	}
	return b.reg.List()
}

func (b *ipBans) lift(key string) []iphealth.Ban {
	if b == nil {
		return nil
	}
	var lifted []iphealth.Ban
	if key == "" {
		lifted = b.reg.Clear()
	} else if ip, port, ok := iphealth.ParseBanKey(key); ok && b.reg.Lift(key) {
		lifted = []iphealth.Ban{{IP: ip.String(), Port: port}}
	}
	b.flush()
	return lifted
}

func shortCacheKey(ban iphealth.Ban) string {
	return fmt.Sprintf("%s:%d", ban.IP, ban.Port)
}

func (p *Pool) GetBans() []metrics.BanEntry {
	out := []metrics.BanEntry{}
	if p == nil || p.state == nil {
		return out
	}
	bans := p.state.bans.list()
	if len(bans) == 0 {
		return out
	}
	mc := metrics.GetMetricsCollector()
	for _, ban := range bans {
		out = append(out, metrics.BanEntry{
			Key:     ban.Key(),
			IP:      ban.IP,
			Port:    ban.Port,
			Host:    ban.Host,
			Set:     ban.Set,
			Strikes: ban.Strikes,
			Repeat:  ban.Repeat,
			Since:   mc.WallMillis(ban.Since),
			Until:   mc.WallMillis(ban.Until),
			Passes:  ban.Passes,
		})
	}
	return out
}

func (p *Pool) ClearBans(key string) int {
	if p == nil || p.state == nil || p.state.bans == nil {
		return 0
	}
	lifted := p.state.bans.lift(key)
	if p.state.destState != nil {
		for _, ban := range lifted {
			p.state.destState.Unblock(shortCacheKey(ban))
		}
	}
	p.publishBans()
	return len(lifted)
}

func (p *Pool) publishBans() {
	if p.isDiscovery() {
		return
	}
	metrics.GetMetricsCollector().UpdateBans(p.GetBans())
}

func (p *Pool) wireBans() {
	if p == nil || p.state == nil || p.state.bans == nil || len(p.Workers) == 0 {
		return
	}
	p.state.bans.changed = p.publishBans
	p.state.bans.settled = p.Workers[0].settleSilentHellos
	p.state.bans.gotAnswer = p.Workers[0].silentHelloGotAnswer
	p.state.bans.hostProbe = iphealth.ProbeTLSHost
	p.state.bans.prepare = p.Workers[0].hintBanRetest
}

type ipBlockView struct {
	dest *destStateTracker
	bans *ipBans
}

func (v ipBlockView) IsBlocked(dstIPPort string) bool {
	return v.dest.IsBlocked(dstIPPort) || v.bans.bannedKey(dstIPPort)
}

func (v ipBlockView) AddBlocked(dstIPPort string) {
	v.dest.AddBlocked(dstIPPort)
}
