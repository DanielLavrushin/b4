package dns

import (
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	FakeIPPoolLimit = 8192
	FakeIPLeaseTTL  = time.Hour
)

func FakeIPAddress(name string, network *net.IPNet) net.IP {
	base, size, ok := fakeIPBlock(network)
	if !ok {
		return nil
	}
	name = normalizeFakeName(name)
	if name == "" {
		return nil
	}
	offset := uint64(fnv1a32(name))%(size-2) + 1
	return fakeIPOffset(base, offset)
}

func InFakeIPNetwork(ip net.IP, network *net.IPNet) bool {
	if ip == nil || network == nil {
		return false
	}
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	return network.Contains(v4)
}

type fakeIPLease struct {
	ip      net.IP
	network string
	seen    time.Time
}

type FakeIPPool struct {
	mu     sync.Mutex
	byName map[string]fakeIPLease
	byAddr map[string]string
	now    func() time.Time
}

func NewFakeIPPool() *FakeIPPool {
	return &FakeIPPool{
		byName: map[string]fakeIPLease{},
		byAddr: map[string]string{},
		now:    time.Now,
	}
}

func (p *FakeIPPool) Address(name string, network *net.IPNet) net.IP {
	if p == nil {
		return FakeIPAddress(name, network)
	}
	base, size, ok := fakeIPBlock(network)
	if !ok {
		return nil
	}
	name = normalizeFakeName(name)
	if name == "" {
		return nil
	}
	block := network.String()
	now := p.now()

	p.mu.Lock()
	defer p.mu.Unlock()

	if lease, held := p.byName[name]; held && lease.network == block {
		lease.seen = now
		p.byName[name] = lease
		return lease.ip
	}

	start := uint64(fnv1a32(name)) % (size - 2)
	for probe := uint64(0); probe < size-2; probe++ {
		ip := fakeIPOffset(base, (start+probe)%(size-2)+1)
		key := ip.String()
		owner, taken := p.byAddr[key]
		if taken && owner != name {
			if lease, held := p.byName[owner]; held && now.Sub(lease.seen) <= FakeIPLeaseTTL {
				continue
			}
			delete(p.byAddr, key)
			delete(p.byName, owner)
		}
		p.byName[name] = fakeIPLease{ip: ip, network: block, seen: now}
		p.byAddr[key] = name
		if len(p.byName) > FakeIPPoolLimit {
			p.evictLocked(now)
		}
		return ip
	}
	return FakeIPAddress(name, network)
}

func (p *FakeIPPool) evictLocked(now time.Time) {
	var (
		oldestName string
		oldestSeen time.Time
	)
	for name, lease := range p.byName {
		if now.Sub(lease.seen) > FakeIPLeaseTTL {
			delete(p.byName, name)
			delete(p.byAddr, lease.ip.String())
			continue
		}
		if oldestName == "" || lease.seen.Before(oldestSeen) {
			oldestName, oldestSeen = name, lease.seen
		}
	}
	if oldestName != "" && len(p.byName) > FakeIPPoolLimit {
		lease := p.byName[oldestName]
		delete(p.byName, oldestName)
		delete(p.byAddr, lease.ip.String())
	}
}

func fakeIPBlock(network *net.IPNet) (uint32, uint64, bool) {
	if network == nil {
		return 0, 0, false
	}
	base := network.IP.To4()
	if base == nil {
		return 0, 0, false
	}
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return 0, 0, false
	}
	hostBits := 32 - ones
	if hostBits < 2 || hostBits > 30 {
		return 0, 0, false
	}
	size := uint64(1) << uint(hostBits)
	if size <= 2 {
		return 0, 0, false
	}
	return binary.BigEndian.Uint32(base), size, true
}

func fakeIPOffset(base uint32, offset uint64) net.IP {
	out := make(net.IP, 4)
	binary.BigEndian.PutUint32(out, uint32(uint64(base)+offset))
	return out
}

func normalizeFakeName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

func fnv1a32(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}
