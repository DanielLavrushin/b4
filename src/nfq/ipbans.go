package nfq

import (
	"context"
	"fmt"
	"net"
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
)

type banProber func(ctx context.Context, ip net.IP, port int, mark int) iphealth.TLSResult

type ipBans struct {
	reg         *iphealth.BanRegistry
	good        *iphealth.KnownGood
	probe       banProber
	enabled     atomic.Bool
	flushFailed atomic.Bool
	mu          sync.Mutex
	inflight    map[string]struct{}
	sem         chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	changed     func()
}

func newIPBans(reg *iphealth.BanRegistry, good *iphealth.KnownGood, probe banProber) *ipBans {
	if probe == nil {
		probe = iphealth.ProbeTLS
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &ipBans{
		reg:      reg,
		good:     good,
		probe:    probe,
		inflight: make(map[string]struct{}),
		sem:      make(chan struct{}, banProbeParallel),
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (b *ipBans) applyConfig(cfg *config.Config) {
	if b == nil {
		return
	}
	b.enabled.Store(cfg != nil && !cfg.Queue.IsDiscovery && cfg.System.IPHealth.BansEnabled())
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

func (b *ipBans) confirm(cfg *config.Config, ip net.IP, port uint16, host, setName string) {
	if !b.on() || cfg == nil || ip == nil {
		return
	}
	addr := append(net.IP(nil), ip...)
	first, repeat := cfg.System.IPHealth.BanTTL(), cfg.System.IPHealth.RepeatBanTTL()
	key := iphealth.BanKey(addr, int(port))
	b.run(key, addr, int(port), int(cfg.MainInjectedMark()), func(res iphealth.TLSResult) {
		b.settleStrike(key, addr, int(port), host, setName, first, repeat, res)
	})
}

func (b *ipBans) settleStrike(key string, ip net.IP, port int, host, setName string, first, repeat time.Duration, res iphealth.TLSResult) {
	switch res {
	case iphealth.TLSReplied:
		log.Infof("IP bans: %s answers TLS for other names, so the block is on the name and the address is not banned (host: %s, set: %s)", key, host, setName)
		return
	case iphealth.TLSUnreachable:
		log.Tracef("IP bans: %s did not take a TCP connection from the router, not banning it (host: %s, set: %s)", key, host, setName)
		return
	}
	if !b.on() {
		return
	}
	until, isRepeat, ok := b.reg.Strike(ip, port, host, setName, first, repeat)
	if !ok {
		return
	}
	b.good.Forget(host, ip)
	log.Infof("IP bans: %s does not answer TLS for neutral names either, banned until %s (host: %s, set: %s, repeat: %t)", key, until.Format("2006-01-02 15:04:05"), host, setName, isRepeat)
	b.notify()
}

func (b *ipBans) notify() {
	if b.changed != nil {
		b.changed()
	}
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
		key := ban.Key()
		b.run(key, ip, ban.Port, mark, func(res iphealth.TLSResult) {
			b.settleRetest(key, res)
		})
	}
}

func (b *ipBans) settleRetest(key string, res iphealth.TLSResult) {
	if b.reg.RecordCheck(key, res == iphealth.TLSReplied) {
		log.Infof("IP bans: %s answered TLS for neutral names in several re-tests in a row, the ban is lifted", key)
		b.notify()
		return
	}
	log.Tracef("IP bans: re-test of %s: %s", key, res)
}

func (b *ipBans) run(key string, ip net.IP, port, mark int, done func(iphealth.TLSResult)) bool {
	b.mu.Lock()
	if b.ctx.Err() != nil {
		b.mu.Unlock()
		return false
	}
	if _, busy := b.inflight[key]; busy || len(b.inflight) >= banPendingMax {
		b.mu.Unlock()
		return false
	}
	b.inflight[key] = struct{}{}
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
		res := b.probe(b.ctx, ip, port, mark)
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
		lifted = b.reg.List()
		b.reg.Clear()
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
