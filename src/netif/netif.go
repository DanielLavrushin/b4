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
)

const cacheTTL = 30 * time.Second

const flagUp = 0x1

var Root = "/sys/class/net/"

type entry struct {
	kind Kind
	up   bool
	at   time.Time
}

var (
	mu    sync.RWMutex
	cache = make(map[string]entry)
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
			if strings.TrimSpace(line) == "DEVTYPE=wireguard" {
				return KindWireGuard, up
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

func Forget() {
	mu.Lock()
	cache = make(map[string]entry)
	mu.Unlock()
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
