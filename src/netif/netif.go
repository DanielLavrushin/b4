package netif

import (
	"os"
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
)

const cacheTTL = 30 * time.Second

var Root = "/sys/class/net/"

type entry struct {
	kind Kind
	at   time.Time
}

var (
	mu    sync.Mutex
	cache = make(map[string]entry)
)

func classify(name string) Kind {
	if _, err := os.Stat(Root + name); err != nil {
		return KindMissing
	}
	if _, err := os.Stat(Root + name + "/tun_flags"); err == nil {
		return KindUserspaceTunnel
	}
	if b, err := os.ReadFile(Root + name + "/uevent"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(line) == "DEVTYPE=wireguard" {
				return KindWireGuard
			}
		}
	}
	return KindOther
}

func Of(name string) Kind {
	name = strings.TrimSpace(name)
	if name == "" {
		return KindUnknown
	}

	now := time.Now()

	mu.Lock()
	if e, ok := cache[name]; ok && now.Sub(e.at) < cacheTTL {
		mu.Unlock()
		return e.kind
	}
	mu.Unlock()

	k := classify(name)

	mu.Lock()
	cache[name] = entry{kind: k, at: now}
	mu.Unlock()

	return k
}

func IsUserspaceTunnel(name string) bool {
	return Of(name) == KindUserspaceTunnel
}

func IsEncapsulated(name string) bool {
	switch Of(name) {
	case KindUserspaceTunnel, KindWireGuard:
		return true
	}
	return false
}

func Describe(name string) string {
	switch Of(name) {
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

func Forget() {
	mu.Lock()
	cache = make(map[string]entry)
	mu.Unlock()
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
