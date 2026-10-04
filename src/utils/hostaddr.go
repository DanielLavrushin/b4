package utils

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

const hostAddrsTTL = 5 * time.Second

var hostAddrs struct {
	mu    sync.Mutex
	at    time.Time
	addrs map[netip.Addr]struct{}
}

var interfaceAddrs = net.InterfaceAddrs

func IsHostAddr(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("")
	switch {
	case addr.IsLoopback():
		return true
	case !addr.IsValid(), addr.IsUnspecified():
		return false
	}
	hostAddrs.mu.Lock()
	defer hostAddrs.mu.Unlock()
	if hostAddrs.addrs == nil || time.Since(hostAddrs.at) > hostAddrsTTL {
		hostAddrs.addrs = loadHostAddrs()
		hostAddrs.at = time.Now()
	}
	_, ok := hostAddrs.addrs[addr]
	return ok
}

func IsHostIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	return ok && IsHostAddr(addr)
}

func loadHostAddrs() map[netip.Addr]struct{} {
	out := map[netip.Addr]struct{}{}
	list, err := interfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range list {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if addr, ok := netip.AddrFromSlice(n.IP); ok {
			out[addr.Unmap()] = struct{}{}
		}
	}
	return out
}
