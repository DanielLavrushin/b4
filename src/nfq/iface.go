package nfq

import (
	"net"
	"sync"
	"sync/atomic"

	"github.com/florianl/go-nfqueue"
)

var (
	ifaceCache sync.Map
	ifaceSeen  sync.Map
)

func getIfaceName(idx uint32) string {
	if idx == 0 {
		return ""
	}

	if v, ok := ifaceCache.Load(idx); ok {
		return v.(string)
	}

	iface, err := net.InterfaceByIndex(int(idx))
	if err != nil {
		return ""
	}

	actual, _ := ifaceCache.LoadOrStore(idx, iface.Name)
	return actual.(string)
}

type ifaceRole int

const (
	ifaceArriving ifaceRole = iota
	ifaceLeaving
)

type IfaceCounts struct {
	Leaving  uint64 `json:"leaving"`
	Arriving uint64 `json:"arriving"`
}

func ifaceSeenKey(idx uint32, role ifaceRole) uint64 {
	return uint64(idx)<<1 | uint64(role)
}

func recordIfaceTraffic(idx uint32, role ifaceRole) {
	if idx == 0 {
		return
	}
	key := ifaceSeenKey(idx, role)
	if v, ok := ifaceSeen.Load(key); ok {
		v.(*atomic.Uint64).Add(1)
		return
	}
	actual, _ := ifaceSeen.LoadOrStore(key, &atomic.Uint64{})
	actual.(*atomic.Uint64).Add(1)
}

func IfaceTraffic() map[string]IfaceCounts {
	out := make(map[string]IfaceCounts)
	ifaceSeen.Range(func(k, v any) bool {
		key := k.(uint64)
		name := getIfaceName(uint32(key >> 1))
		if name == "" {
			return true
		}
		c := out[name]
		n := v.(*atomic.Uint64).Load()
		if ifaceRole(key&1) == ifaceLeaving {
			c.Leaving += n
		} else {
			c.Arriving += n
		}
		out[name] = c
		return true
	})
	return out
}

func ResetIfaceTraffic() {
	ifaceSeen.Range(func(k, _ any) bool {
		ifaceSeen.Delete(k)
		return true
	})
}

func packetIfaceIndex(a nfqueue.Attribute) (uint32, ifaceRole) {
	if a.OutDev != nil && *a.OutDev != 0 {
		return *a.OutDev, ifaceLeaving
	}
	if a.InDev != nil && *a.InDev != 0 {
		return *a.InDev, ifaceArriving
	}
	return 0, ifaceArriving
}

func (w *Worker) matchesInterface(a nfqueue.Attribute) bool {
	idx, role := packetIfaceIndex(a)
	recordIfaceTraffic(idx, role)

	cfg := w.getConfig()
	ifaces := cfg.Queue.Interfaces
	if len(ifaces) == 0 {
		return true // no filter = all interfaces
	}

	if idx == 0 {
		return true // can't determine, allow
	}

	name := getIfaceName(idx)
	for _, allowed := range ifaces {
		if name == allowed {
			return true
		}
	}
	return false
}
