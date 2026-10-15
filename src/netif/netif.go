package netif

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Kind int

const (
	KindUnknown Kind = iota
	KindMissing
	KindUserspaceTunnel
	KindWireGuard
	KindOther
	KindAmneziaWG
)

const cacheTTL = 30 * time.Second

const routeTTL = 5 * time.Second

const flagUp = 0x1

const routeFlagReject = 0x0200

var Root = "/sys/class/net/"

var RouteFile = "/proc/net/route"

type entry struct {
	kind Kind
	up   bool
	at   time.Time
}

var (
	mu    sync.RWMutex
	cache = make(map[string]entry)
)

var (
	routeMu     sync.Mutex
	routeAt     time.Time
	routeIfaces map[string]bool
)

func ifaceUp(name string) bool {
	b, err := os.ReadFile(Root + name + "/flags")
	if err != nil {
		return true
	}
	raw := strings.TrimSpace(string(b))
	raw = strings.TrimPrefix(raw, "0x")
	v, err := strconv.ParseUint(raw, 16, 64)
	if err != nil {
		return true
	}
	return v&flagUp != 0
}

func classify(name string) (Kind, bool) {
	if _, err := os.Stat(Root + name); err != nil {
		return KindMissing, false
	}
	up := ifaceUp(name)
	if _, err := os.Stat(Root + name + "/tun_flags"); err == nil {
		return KindUserspaceTunnel, up
	}
	if b, err := os.ReadFile(Root + name + "/uevent"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			switch strings.TrimSpace(line) {
			case "DEVTYPE=wireguard":
				return KindWireGuard, up
			case "DEVTYPE=amneziawg":
				return KindAmneziaWG, up
			}
		}
	}
	return KindOther, up
}

func lookup(name string) entry {
	name = strings.TrimSpace(name)
	if name == "" {
		return entry{kind: KindUnknown}
	}

	now := time.Now()

	mu.RLock()
	prev, had := cache[name]
	mu.RUnlock()
	if had && now.Sub(prev.at) < cacheTTL {
		return prev
	}

	kind, up := classify(name)
	if kind == KindMissing && had && prev.kind != KindMissing && prev.kind != KindUnknown {
		kind = prev.kind
		up = false
	}

	e := entry{kind: kind, up: up, at: now}

	mu.Lock()
	cache[name] = e
	mu.Unlock()

	return e
}

func Of(name string) Kind {
	return lookup(name).kind
}

func IsUserspaceTunnel(name string) bool {
	return lookup(name).kind == KindUserspaceTunnel
}

func IsEncapsulated(name string) bool {
	switch lookup(name).kind {
	case KindUserspaceTunnel, KindWireGuard:
		return true
	}
	return false
}

func IsUp(name string) bool {
	e := lookup(name)
	switch e.kind {
	case KindUnknown, KindMissing:
		return false
	}
	return e.up
}

func EncapsulatedAndUp(name string) bool {
	e := lookup(name)
	if !e.up {
		return false
	}
	switch e.kind {
	case KindUserspaceTunnel, KindWireGuard:
		return true
	}
	return false
}

func TunnelAndUp(name string) bool {
	e := lookup(name)
	if !e.up {
		return false
	}
	switch e.kind {
	case KindUserspaceTunnel, KindWireGuard, KindAmneziaWG:
		return true
	}
	return false
}

func Describe(name string) string {
	switch lookup(name).kind {
	case KindUserspaceTunnel:
		return "a TUN/TAP device driven by a userspace program"
	case KindWireGuard:
		return "a WireGuard tunnel"
	case KindMissing:
		return "not present on this system"
	default:
		return "a network interface"
	}
}

func CarriesDefaultRoute(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	routeMu.Lock()
	ifaces, fresh := routeIfaces, routeIfaces != nil && time.Since(routeAt) < routeTTL
	routeMu.Unlock()
	if !fresh {
		ifaces = readDefaultRouteIfaces()
		routeMu.Lock()
		routeIfaces, routeAt = ifaces, time.Now()
		routeMu.Unlock()
	}
	return ifaces[name]
}

func readDefaultRouteIfaces() map[string]bool {
	out := make(map[string]bool)
	b, err := os.ReadFile(RouteFile)
	if err != nil {
		return out
	}
	best := int64(-1)
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.Contains(line, "\t00000000\t") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 8 || f[0] == "*" || f[1] != "00000000" || f[7] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(f[3], 16, 32)
		if err != nil || flags&routeFlagReject != 0 {
			continue
		}
		metric, err := strconv.ParseInt(f[6], 10, 64)
		if err != nil {
			continue
		}
		switch {
		case best < 0 || metric < best:
			best = metric
			out = map[string]bool{f[0]: true}
		case metric == best:
			out[f[0]] = true
		}
	}
	return out
}

func Forget() {
	mu.Lock()
	cache = make(map[string]entry)
	mu.Unlock()
	routeMu.Lock()
	routeIfaces = nil
	routeMu.Unlock()
}

func MarkDown(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	e, ok := cache[name]
	if !ok {
		return
	}
	e.up = false
	e.at = time.Now()
	cache[name] = e
}

func ForgetIface(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	mu.Lock()
	delete(cache, name)
	mu.Unlock()
}
