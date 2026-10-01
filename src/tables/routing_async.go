package tables

import (
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	routeAsyncQueueSize = 1024
	routeAsyncSeenMax   = 8192
)

var (
	routeAsyncOnce    sync.Once
	routeAsyncCh      chan func()
	routeAsyncDropped atomic.Uint64
	routeAsyncLastLog atomic.Int64

	routeAsyncSeenMu  sync.Mutex
	routeAsyncSeen    = make(map[string]time.Time)
	routeAsyncPending = make(map[string]chan struct{})
	routeAsyncOpen    = make(map[string]*routeAsyncBatch)
	routeAsyncGen     uint64
	routeAsyncSetGen  = make(map[string]uint64)

	routeInstallFailedAt    sync.Map
	routeInstallFailureMemo = time.Minute
)

func routeAsyncStart() {
	routeAsyncOnce.Do(func() {
		routeAsyncCh = make(chan func(), routeAsyncQueueSize)
		go func() {
			for job := range routeAsyncCh {
				routeAsyncRun(job)
			}
		}()
	})
}

func routeAsyncRun(job func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Routing: async firewall update panicked: %v", r)
		}
	}()
	job()
}

func routeAsyncSubmit(job func(), onDrop func()) {
	routeAsyncStart()
	select {
	case routeAsyncCh <- job:
	default:
		if onDrop != nil {
			onDrop()
		}
		n := routeAsyncDropped.Add(1)
		now := time.Now().Unix()
		last := routeAsyncLastLog.Load()
		if now-last >= 10 && routeAsyncLastLog.CompareAndSwap(last, now) {
			log.Warnf("Routing: firewall update backlog is full, %d updates skipped so far", n)
		}
	}
}

func routeAsyncRefresh(set *config.SetConfig) time.Duration {
	ttl := set.Routing.IPTTLSeconds
	if ttl <= 0 {
		ttl = 3600
	}
	return time.Duration(ttl) * time.Second / 2
}

func routeAsyncClaim(set *config.SetConfig, ips []net.IP) []net.IP {
	routeAsyncSeenMu.Lock()
	defer routeAsyncSeenMu.Unlock()
	fresh, _ := routeAsyncClaimLocked(set, ips, false)
	return fresh
}

func routeAsyncClaimLocked(set *config.SetConfig, ips []net.IP, joinPending bool) ([]net.IP, []<-chan struct{}) {
	refresh := routeAsyncRefresh(set)
	now := time.Now()

	routePruneStamps(routeAsyncSeen, routeAsyncSeenMax, now, refresh)

	fresh := make([]net.IP, 0, len(ips))
	var waits []<-chan struct{}
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		key := set.Id + "|" + ip.String()
		if joinPending {
			if pending, ok := routeAsyncPending[key]; ok {
				waits = routeAsyncJoin(waits, pending)
				continue
			}
		}
		if last, seen := routeAsyncSeen[key]; seen && now.Sub(last) < refresh {
			continue
		}
		routeAsyncSeen[key] = now
		fresh = append(fresh, ip)
	}
	return fresh, waits
}

func routeAsyncJoin(waits []<-chan struct{}, pending chan struct{}) []<-chan struct{} {
	for _, w := range waits {
		if w == pending {
			return waits
		}
	}
	return append(waits, pending)
}

func routeAsyncTrackLocked(setID string, ips []net.IP, done chan struct{}) {
	for _, ip := range ips {
		routeAsyncPending[setID+"|"+ip.String()] = done
	}
}

func routeAsyncSettle(setID string, ips []net.IP, done chan struct{}) {
	routeAsyncSeenMu.Lock()
	for _, ip := range ips {
		key := setID + "|" + ip.String()
		if routeAsyncPending[key] == done {
			delete(routeAsyncPending, key)
		}
	}
	routeAsyncSeenMu.Unlock()
	close(done)
}

func routeAsyncAbandon(setID string, ips []net.IP, done chan struct{}) {
	routeAsyncSeenMu.Lock()
	for _, ip := range ips {
		key := setID + "|" + ip.String()
		delete(routeAsyncSeen, key)
		if routeAsyncPending[key] == done {
			delete(routeAsyncPending, key)
		}
	}
	routeAsyncSeenMu.Unlock()
	close(done)
}

func routeAsyncStampExcept(set *config.SetConfig, ips []net.IP, failed []string) {
	skip := make(map[string]struct{}, len(failed))
	for _, entry := range failed {
		skip[entry] = struct{}{}
	}
	now := time.Now()
	routeAsyncSeenMu.Lock()
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		s := ip.String()
		if _, bad := skip[s]; bad {
			continue
		}
		routeAsyncSeen[set.Id+"|"+s] = now
	}
	routeAsyncSeenMu.Unlock()
}

func routeAsyncUsable(cfg *config.Config, ips []net.IP) []net.IP {
	usable := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		switch {
		case ip.To4() != nil:
			if cfg.Queue.IPv4Enabled {
				usable = append(usable, ip)
			}
		case ip.To16() != nil:
			if cfg.Queue.IPv6Enabled {
				usable = append(usable, ip)
			}
		}
	}
	return usable
}

func routeAsyncForgetSet(setID string) {
	if setID == "" {
		return
	}
	prefix := setID + "|"
	routeAsyncSeenMu.Lock()
	for k := range routeAsyncSeen {
		if strings.HasPrefix(k, prefix) {
			delete(routeAsyncSeen, k)
		}
	}
	for k := range routeAsyncPending {
		if strings.HasPrefix(k, prefix) {
			delete(routeAsyncPending, k)
		}
	}
	routeAsyncSetGen[setID]++
	delete(routeAsyncOpen, setID)
	routeAsyncSeenMu.Unlock()
}

func routeAsyncForgetKeys(keys []string) {
	routeAsyncSeenMu.Lock()
	for _, k := range keys {
		delete(routeAsyncSeen, k)
		delete(routeAsyncPending, k)
	}
	routeAsyncSeenMu.Unlock()
}

func routeAsyncForgetAll() {
	routeAsyncSeenMu.Lock()
	defer routeAsyncSeenMu.Unlock()
	routeAsyncSeen = make(map[string]time.Time)
	routeAsyncPending = make(map[string]chan struct{})
	routeAsyncOpen = make(map[string]*routeAsyncBatch)
	routeAsyncGen++
	routeAsyncSetGen = make(map[string]uint64)
}

func routeAsyncRelease(set *config.SetConfig, ips []net.IP) {
	routeAsyncSeenMu.Lock()
	defer routeAsyncSeenMu.Unlock()
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		delete(routeAsyncSeen, set.Id+"|"+ip.String())
	}
}

type routeAsyncBatch struct {
	cfg    *config.Config
	set    *config.SetConfig
	ips    []net.IP
	done   chan struct{}
	gen    uint64
	setGen uint64
}

func routeAsyncBatchStale(setID string, batch *routeAsyncBatch) bool {
	routeAsyncSeenMu.Lock()
	defer routeAsyncSeenMu.Unlock()
	return batch.gen != routeAsyncGen || batch.setGen != routeAsyncSetGen[setID]
}

func routeAsyncSeal(setID string, batch *routeAsyncBatch) (*config.Config, *config.SetConfig, []net.IP) {
	routeAsyncSeenMu.Lock()
	defer routeAsyncSeenMu.Unlock()
	if routeAsyncOpen[setID] == batch {
		delete(routeAsyncOpen, setID)
	}
	return batch.cfg, batch.set, batch.ips
}

func routeAsyncFlush(setID string, batch *routeAsyncBatch) {
	cfg, set, ips := routeAsyncSeal(setID, batch)
	defer routeAsyncSettle(setID, ips, batch.done)
	routeHandleDNS(cfg, set, ips, func() bool { return routeAsyncBatchStale(setID, batch) })
}

func routeAsyncDropBatch(setID string, batch *routeAsyncBatch) {
	_, _, ips := routeAsyncSeal(setID, batch)
	routeAsyncAbandon(setID, ips, batch.done)
}

func routeLacksEgress(set *config.SetConfig) bool {
	mode := set.Routing.Mode
	return (mode == "" || mode == config.RoutingModeInterface) && set.Routing.EgressInterface == ""
}

func routeNoteInstallFailed(setID string) {
	routeInstallFailedAt.Store(setID, time.Now())
}

func routeNoteInstalled(setID string) {
	routeInstallFailedAt.Delete(setID)
}

func routeInstallFailedRecently(setID string) bool {
	v, ok := routeInstallFailedAt.Load(setID)
	if !ok {
		return false
	}
	at, _ := v.(time.Time)
	return time.Since(at) < routeInstallFailureMemo
}

func RoutingHandleDNSAsync(cfg *config.Config, set *config.SetConfig, ips []net.IP) {
	RoutingHandleDNSAwait(cfg, set, ips)
}

func RoutingHandleDNSAwait(cfg *config.Config, set *config.SetConfig, ips []net.IP) []<-chan struct{} {
	if cfg == nil || set == nil || !set.Routing.Enabled || len(ips) == 0 || set.Targets.DomainOnly || routeLacksEgress(set) {
		return nil
	}
	usable := routeAsyncUsable(cfg, ips)
	if len(usable) == 0 {
		return nil
	}

	routeAsyncSeenMu.Lock()
	fresh, waits := routeAsyncClaimLocked(set, usable, true)
	var queued *routeAsyncBatch
	if len(fresh) > 0 {
		batch := routeAsyncOpen[set.Id]
		if batch == nil {
			batch = &routeAsyncBatch{done: make(chan struct{}), gen: routeAsyncGen, setGen: routeAsyncSetGen[set.Id]}
			routeAsyncOpen[set.Id] = batch
			queued = batch
		}
		batch.cfg, batch.set = cfg, set
		batch.ips = append(batch.ips, fresh...)
		routeAsyncTrackLocked(set.Id, fresh, batch.done)
		waits = routeAsyncJoin(waits, batch.done)
		if len(batch.ips) >= routeNftChunkSize && routeAsyncOpen[set.Id] == batch {
			delete(routeAsyncOpen, set.Id)
		}
	}
	routeAsyncSeenMu.Unlock()

	if queued != nil {
		setID := set.Id
		routeAsyncSubmit(
			func() { routeAsyncFlush(setID, queued) },
			func() { routeAsyncDropBatch(setID, queued) },
		)
	}
	if routeInstallFailedRecently(set.Id) {
		return nil
	}
	return waits
}

func RoutingLearnIPAsync(cfg *config.Config, set *config.SetConfig, ip net.IP) {
	if cfg == nil || set == nil || ip == nil || !set.Routing.Enabled {
		return
	}
	if config.RoutingIsBlock(set.Routing.Mode) {
		return
	}
	routeAsyncSeenMu.Lock()
	fresh, _ := routeAsyncClaimLocked(set, []net.IP{ip}, true)
	var done chan struct{}
	if len(fresh) > 0 {
		done = make(chan struct{})
		routeAsyncTrackLocked(set.Id, fresh, done)
	}
	routeAsyncSeenMu.Unlock()
	if len(fresh) == 0 {
		return
	}
	setID := set.Id
	routeAsyncSubmit(
		func() {
			defer routeAsyncSettle(setID, fresh, done)
			RoutingLearnIP(cfg, set, fresh[0])
		},
		func() { routeAsyncAbandon(setID, fresh, done) },
	)
}

func RoutingLearnHostAsync(cfg *config.Config, set *config.SetConfig, host string) {
	if cfg == nil || set == nil || host == "" || !set.Routing.Enabled {
		return
	}
	if config.RoutingIsBlock(set.Routing.Mode) || set.Targets.DomainOnly {
		return
	}
	routeAsyncSubmit(func() { RoutingLearnHost(cfg, set, host) }, nil)
}
