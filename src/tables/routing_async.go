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

	routeAsyncSeenMu sync.Mutex
	routeAsyncSeen   = make(map[string]time.Time)
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
	refresh := routeAsyncRefresh(set)
	now := time.Now()

	routeAsyncSeenMu.Lock()
	defer routeAsyncSeenMu.Unlock()

	if len(routeAsyncSeen) > routeAsyncSeenMax {
		for k, t := range routeAsyncSeen {
			if now.Sub(t) >= refresh {
				delete(routeAsyncSeen, k)
			}
		}
	}

	fresh := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		key := set.Id + "|" + ip.String()
		if last, seen := routeAsyncSeen[key]; seen && now.Sub(last) < refresh {
			continue
		}
		routeAsyncSeen[key] = now
		fresh = append(fresh, ip)
	}
	return fresh
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
	routeAsyncSeenMu.Unlock()
}

func routeAsyncForgetAll() {
	routeAsyncSeenMu.Lock()
	defer routeAsyncSeenMu.Unlock()
	routeAsyncSeen = make(map[string]time.Time)
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

func RoutingHandleDNSAsync(cfg *config.Config, set *config.SetConfig, ips []net.IP) {
	if cfg == nil || set == nil || !set.Routing.Enabled || len(ips) == 0 || set.Targets.DomainOnly {
		return
	}
	fresh := routeAsyncClaim(set, ips)
	if len(fresh) == 0 {
		return
	}
	routeAsyncSubmit(
		func() { RoutingHandleDNS(cfg, set, fresh) },
		func() { routeAsyncRelease(set, fresh) },
	)
}

func RoutingLearnIPAsync(cfg *config.Config, set *config.SetConfig, ip net.IP) {
	if cfg == nil || set == nil || ip == nil || !set.Routing.Enabled {
		return
	}
	if config.RoutingIsBlock(set.Routing.Mode) {
		return
	}
	fresh := routeAsyncClaim(set, []net.IP{ip})
	if len(fresh) == 0 {
		return
	}
	routeAsyncSubmit(
		func() { RoutingLearnIP(cfg, set, fresh[0]) },
		func() { routeAsyncRelease(set, fresh) },
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
