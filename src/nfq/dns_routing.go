package nfq

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

// RoutingHandleDNSFunc is called when DNS-resolved IPs are available for routing.
// Set from main.go to break the tables ↔ nfq import cycle.
var RoutingHandleDNSFunc func(cfg *config.Config, set *config.SetConfig, ips []net.IP)

// RoutingLearnIPFunc adds a destination IP observed at SNI-match time to an existing
// routing set's ipset (cheap, hot-path safe). Set from main.go.
var RoutingLearnIPFunc func(cfg *config.Config, set *config.SetConfig, ip net.IP)

func registerEscalatedRoute(cfg *config.Config, escSet *config.SetConfig, dst net.IP) {
	if cfg == nil || escSet == nil || dst == nil || !escSet.Routing.Enabled || RoutingHandleDNSFunc == nil {
		return
	}
	if cfg.Queue.IsDiscovery {
		return
	}
	log.Tracef("registerEscalatedRoute: adding %s to %s ipset (mode=%s)", dst, escSet.Name, escSet.Routing.Mode)
	RoutingHandleDNSFunc(cfg, escSet, []net.IP{dst})
}

// registerLearnedRoute steers future connections to dst by adding it to the matched
// routing set's ipset, using the IP seen at the TLS/QUIC ClientHello. No-op for
// non-routing and block-mode sets (filtered inside RoutingLearnIPFunc).
func registerLearnedRoute(cfg *config.Config, set *config.SetConfig, dst net.IP) {
	if cfg == nil || set == nil || dst == nil || !set.Routing.Enabled || RoutingLearnIPFunc == nil {
		return
	}
	if cfg.Queue.IsDiscovery {
		return
	}
	RoutingLearnIPFunc(cfg, set, dst)
}

type pendingDNSRoute struct {
	setID   string
	expires time.Time
}

var (
	dnsRoutePending   sync.Map
	dnsRouteCleanMu   sync.Mutex
	dnsRouteCleanStop chan struct{}
)

func dnsRouteKeyRequest(
	ipVersion byte,
	clientIP net.IP,
	clientPort uint16,
	dnsServerIP net.IP,
	dnsServerPort uint16,
	txid uint16,
	domain string,
) string {
	return fmt.Sprintf(
		"%d|%s|%d|%s|%d|%d|%s",
		ipVersion,
		clientIP.String(),
		clientPort,
		dnsServerIP.String(),
		dnsServerPort,
		txid,
		domain,
	)
}

func dnsRouteKeyResponse(
	ipVersion byte,
	clientIP net.IP,
	clientPort uint16,
	dnsServerIP net.IP,
	dnsServerPort uint16,
	txid uint16,
	domain string,
) string {
	return dnsRouteKeyRequest(ipVersion, clientIP, clientPort, dnsServerIP, dnsServerPort, txid, domain)
}

func startDNSRouteCleanup() {
	dnsRouteCleanMu.Lock()
	defer dnsRouteCleanMu.Unlock()
	if dnsRouteCleanStop != nil {
		return
	}
	stopCh := make(chan struct{})
	dnsRouteCleanStop = stopCh
	go func(ch <-chan struct{}) {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cleanupDNSPendingRoutes(time.Now())
			case <-ch:
				return
			}
		}
	}(stopCh)
}

func stopDNSRouteCleanup() {
	dnsRouteCleanMu.Lock()
	defer dnsRouteCleanMu.Unlock()
	if dnsRouteCleanStop != nil {
		close(dnsRouteCleanStop)
		dnsRouteCleanStop = nil
	}
}

// ShutdownDNSRouteRuntime stops global DNS-route cleanup goroutine.
// Call once on process shutdown.
func ShutdownDNSRouteRuntime() {
	stopDNSRouteCleanup()
}

func storeDNSPendingRoute(key string, setID string) {
	startDNSRouteCleanup()
	dnsRoutePending.Store(key, pendingDNSRoute{setID: setID, expires: time.Now().Add(2 * time.Minute)})
}

func consumeDNSPendingRoute(key string) (string, bool) {
	v, ok := dnsRoutePending.LoadAndDelete(key)
	if !ok {
		return "", false
	}
	r, ok2 := v.(pendingDNSRoute)
	if !ok2 {
		return "", false
	}
	if time.Now().After(r.expires) {
		return "", false
	}
	return r.setID, true
}

func cleanupDNSPendingRoutes(now time.Time) int {
	removed := 0
	dnsRoutePending.Range(func(key, value any) bool {
		r, ok := value.(pendingDNSRoute)
		if !ok {
			dnsRoutePending.Delete(key)
			removed++
			return true
		}
		if now.After(r.expires) {
			dnsRoutePending.Delete(key)
			removed++
		}
		return true
	})
	return removed
}
