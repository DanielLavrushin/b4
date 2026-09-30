package tun

import (
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	firstNMaxFlows    = 1 << 16
	firstNTCPIdle     = 30 * time.Minute
	firstNUDPIdle     = 2 * time.Minute
	firstNClosedIdle  = 10 * time.Second
	firstNSweepGap    = time.Second
	firstNEvictSample = 8
)

type flowKey struct {
	proto        uint8
	src, dst     [4]byte
	sport, dport uint16
}

type flowCount struct {
	packets uint32
	isn     uint32
	synSeen bool
	closing bool
	seen    time.Time
}

type firstNParams struct {
	tcpLimit uint32
	udpLimit uint32
	dupHosts map[[4]byte]struct{}
	dupNets  []*net.IPNet
}

type firstNLimiter struct {
	params atomic.Pointer[firstNParams]
	passed atomic.Uint64

	mu        sync.Mutex
	flows     map[flowKey]*flowCount
	lastSweep time.Time
}

func newFirstNLimiter(p captureParams) *firstNLimiter {
	l := &firstNLimiter{flows: make(map[flowKey]*flowCount)}
	l.setParams(p)
	return l
}

func (l *firstNLimiter) setParams(p captureParams) {
	np := &firstNParams{
		tcpLimit: uint32(max(p.tcpLimit, 1)),
		udpLimit: uint32(max(p.udpLimit, 1)),
		dupHosts: make(map[[4]byte]struct{}),
	}
	for _, s := range p.dupIPs {
		if _, ipnet, err := net.ParseCIDR(s); err == nil {
			if v4 := ipnet.IP.To4(); v4 != nil {
				if ones, _ := ipnet.Mask.Size(); ones == 32 {
					np.dupHosts[[4]byte(v4)] = struct{}{}
				} else {
					np.dupNets = append(np.dupNets, ipnet)
				}
			}
			continue
		}
		if ip := net.ParseIP(s).To4(); ip != nil {
			np.dupHosts[[4]byte(ip)] = struct{}{}
		}
	}
	l.params.Store(np)
}

func (p *firstNParams) duplicated(dst [4]byte) bool {
	if _, ok := p.dupHosts[dst]; ok {
		return true
	}
	for _, n := range p.dupNets {
		if n.Contains(net.IP(dst[:])) {
			return true
		}
	}
	return false
}

func (l *firstNLimiter) admit(raw []byte, now time.Time) bool {
	if len(raw) < 20 || raw[0]>>4 != 4 {
		return true
	}
	ihl := int(raw[0]&0x0f) * 4
	proto := raw[9]
	if ihl < 20 || (proto != 6 && proto != 17) || len(raw) < ihl+4 {
		return true
	}
	if (uint16(raw[6])<<8|uint16(raw[7]))&0x3fff != 0 {
		return true
	}
	k := flowKey{proto: proto, sport: uint16(raw[ihl])<<8 | uint16(raw[ihl+1]), dport: uint16(raw[ihl+2])<<8 | uint16(raw[ihl+3])}
	copy(k.src[:], raw[12:16])
	copy(k.dst[:], raw[16:20])

	p := l.params.Load()
	limit := p.udpLimit
	if proto == 17 {
		if k.sport == 53 || k.dport == 53 {
			return true
		}
	} else {
		if p.duplicated(k.dst) {
			return true
		}
		limit = p.tcpLimit
	}

	var syn, fin bool
	var seq uint32
	if proto == 6 && len(raw) >= ihl+14 {
		flags := raw[ihl+13]
		syn = flags&0x02 != 0 && flags&0x10 == 0
		fin = flags&0x05 != 0
		seq = binary.BigEndian.Uint32(raw[ihl+4 : ihl+8])
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.flows[k]
	if !ok {
		if len(l.flows) >= firstNMaxFlows {
			if now.Sub(l.lastSweep) >= firstNSweepGap {
				l.sweepLocked(now)
			}
			if len(l.flows) >= firstNMaxFlows {
				l.evictStalestLocked()
			}
		}
		f = &flowCount{}
		l.flows[k] = f
	}
	if syn && (!f.synSeen || f.isn != seq) {
		*f = flowCount{isn: seq, synSeen: true}
	}
	f.packets++
	f.seen = now
	if fin {
		f.closing = true
	}
	pass := f.packets <= limit
	if !pass {
		l.passed.Add(1)
	}
	return pass
}

func (l *firstNLimiter) sweep(now time.Time) {
	l.mu.Lock()
	l.sweepLocked(now)
	l.mu.Unlock()
}

func (l *firstNLimiter) evictStalestLocked() {
	var victim flowKey
	var oldest time.Time
	n := 0
	for k, f := range l.flows {
		if n == 0 || f.seen.Before(oldest) {
			victim, oldest = k, f.seen
		}
		n++
		if n == firstNEvictSample {
			break
		}
	}
	if n > 0 {
		delete(l.flows, victim)
	}
}

func (l *firstNLimiter) sweepLocked(now time.Time) {
	l.lastSweep = now
	for k, f := range l.flows {
		idle := firstNUDPIdle
		switch {
		case f.closing:
			idle = firstNClosedIdle
		case k.proto == 6:
			idle = firstNTCPIdle
		}
		if now.Sub(f.seen) > idle {
			delete(l.flows, k)
		}
	}
}
