package nfq

import (
	"encoding/binary"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/selfdial"
	"github.com/daniellavrushin/b4/sock"
)

const (
	telegramFlowTTL  = time.Minute
	telegramFlowsMax = 4096
)

type telegramFlow struct {
	src, dst     netip.Addr
	sport, dport uint16
}

type telegramPair struct {
	src, dst netip.Addr
}

type telegramFlows struct {
	live  atomic.Int64
	mu    sync.Mutex
	flows map[telegramFlow]time.Time
	pairs map[telegramPair]time.Time
}

func newTelegramFlows() *telegramFlows {
	return &telegramFlows{
		flows: make(map[telegramFlow]time.Time),
		pairs: make(map[telegramPair]time.Time),
	}
}

func telegramInjectorMark(cfg *config.Config) uint {
	if cfg.Queue.IsDiscovery || cfg.Queue.Mark == 0 {
		return 0
	}
	return injectorMark(cfg) | uint(config.TelegramSetDialMark)
}

func (w *Worker) openTelegramInjector(cfg *config.Config) {
	mark := telegramInjectorMark(cfg)
	if mark == 0 || w.telegramFlows == nil {
		return
	}
	s, err := sock.NewSenderWithMark(int(mark))
	if err != nil {
		log.Warnf("NFQ queue %d: no raw socket with mark 0x%x (%v), so the packets b4 sends into the connections it opens itself carry the ordinary injector mark and follow a routing set that lists their address", w.qnum, mark, err)
		return
	}
	w.divertTelegramFlows(s)
}

func (w *Worker) divertTelegramFlows(telegram *sock.Sender) {
	w.telegramSock = telegram
	w.sock.Divert(telegram, w.telegramFlows.carries)
}

func (w *Worker) ownTelegramSet(pkt *pktInfo) *config.SetConfig {
	cfg := w.getConfig()
	if cfg == nil || cfg.Queue.IsDiscovery || cfg.System.MTProto.UpstreamSet == "" || pkt.proto != ipProtoTCP || len(pkt.raw) < pkt.ihl+4 {
		return nil
	}
	if w.srcResolver != nil && pkt.srcOrigin != tunSrcRouter && pkt.srcOrigin != tunSrcUnknown {
		return nil
	}
	dst, ok := netip.AddrFromSlice(pkt.dst)
	if !ok {
		return nil
	}
	dport := binary.BigEndian.Uint16(pkt.raw[pkt.ihl+2 : pkt.ihl+4])
	if !selfdial.Holds(pkt.clientPort(), netip.AddrPortFrom(dst.Unmap(), dport)) {
		return nil
	}
	return cfg.TelegramUpstreamSet()
}

func (w *Worker) openTUNTelegramInjector(reinject int) {
	if w.telegramFlows == nil {
		return
	}
	mark := reinject | int(config.TelegramSetDialMark)
	s, err := sock.NewSenderWithMark(mark)
	if err != nil {
		log.Warnf("TUN: no raw socket with mark 0x%x (%v), so the packets b4 sends into its own Telegram connections carry the ordinary re-send mark and follow a routing set that lists their address", mark, err)
		return
	}
	w.divertTelegramFlows(s)
}

func telegramFlowOf(packet []byte) (telegramFlow, bool, bool) {
	var flow telegramFlow
	if len(packet) < 1 {
		return flow, false, false
	}
	switch packet[0] >> 4 {
	case IPv4:
		if len(packet) < IPv4HeaderMinLen {
			return flow, false, false
		}
		ihl := int(packet[0]&0x0f) * 4
		if ihl < IPv4HeaderMinLen || len(packet) < ihl || packet[9] != ipProtoTCP {
			return flow, false, false
		}
		flow.src = netip.AddrFrom4([4]byte(packet[12:16]))
		flow.dst = netip.AddrFrom4([4]byte(packet[16:20]))
		if binary.BigEndian.Uint16(packet[6:8])&0x1fff != 0 {
			return flow, false, true
		}
		return telegramFlowPorts(flow, packet, ihl)
	case IPv6:
		if len(packet) < IPv6HeaderLen {
			return flow, false, false
		}
		flow.src = netip.AddrFrom16([16]byte(packet[8:24]))
		flow.dst = netip.AddrFrom16([16]byte(packet[24:40]))
		next, off := packet[6], IPv6HeaderLen
		for {
			switch next {
			case 0, 43, 60:
				if len(packet) < off+2 {
					return flow, false, false
				}
				next, off = packet[off], off+int(packet[off+1])*8+8
			case 44:
				if len(packet) < off+8 {
					return flow, false, false
				}
				next = packet[off]
				if binary.BigEndian.Uint16(packet[off+2:off+4])&^7 != 0 {
					return flow, false, next == ipProtoTCP
				}
				off += 8
			case ipProtoTCP:
				return telegramFlowPorts(flow, packet, off)
			default:
				return flow, false, false
			}
		}
	}
	return flow, false, false
}

func telegramFlowPorts(flow telegramFlow, packet []byte, off int) (telegramFlow, bool, bool) {
	if len(packet) < off+4 {
		return flow, false, false
	}
	flow.sport = binary.BigEndian.Uint16(packet[off : off+2])
	flow.dport = binary.BigEndian.Uint16(packet[off+2 : off+4])
	return flow, true, true
}

func (f *telegramFlows) note(packet []byte, now time.Time) {
	if f == nil {
		return
	}
	flow, whole, ok := telegramFlowOf(packet)
	if !ok || !whole {
		return
	}
	f.noteFlow(flow, now)
}

func (f *telegramFlows) noteReply(packet []byte, now time.Time) {
	if f == nil {
		return
	}
	reply, whole, ok := telegramFlowOf(packet)
	if !ok || !whole {
		return
	}
	f.noteFlow(telegramFlow{src: reply.dst, dst: reply.src, sport: reply.dport, dport: reply.sport}, now)
}

func (f *telegramFlows) noteFlow(flow telegramFlow, now time.Time) {
	until := now.Add(telegramFlowTTL)
	pair := telegramPair{src: flow.src, dst: flow.dst}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, had := f.flows[flow]; !had && len(f.flows) >= telegramFlowsMax {
		f.sweepLocked(now)
		trimTelegramFlows(f.flows)
	}
	if _, had := f.pairs[pair]; !had && len(f.pairs) >= telegramFlowsMax {
		f.sweepLocked(now)
		trimTelegramFlows(f.pairs)
	}
	f.flows[flow] = until
	f.pairs[pair] = until
	f.countLocked()
}

func trimTelegramFlows[K comparable](m map[K]time.Time) {
	for k := range m {
		if len(m) < telegramFlowsMax {
			return
		}
		delete(m, k)
	}
}

func (f *telegramFlows) carries(packet []byte) bool {
	if f == nil || f.live.Load() == 0 {
		return false
	}
	return f.carriesAt(packet, time.Now())
}

func (f *telegramFlows) carriesAt(packet []byte, now time.Time) bool {
	return f.lookupAt(packet, now, false)
}

func (f *telegramFlows) carriesOn(packet []byte) bool {
	if f == nil || f.live.Load() == 0 {
		return false
	}
	return f.carriesOnAt(packet, time.Now())
}

func (f *telegramFlows) carriesOnAt(packet []byte, now time.Time) bool {
	return f.lookupAt(packet, now, true)
}

func (f *telegramFlows) lookupAt(packet []byte, now time.Time, extend bool) bool {
	if f == nil || f.live.Load() == 0 {
		return false
	}
	flow, whole, ok := telegramFlowOf(packet)
	if !ok {
		return false
	}
	pair := telegramPair{src: flow.src, dst: flow.dst}
	f.mu.Lock()
	defer f.mu.Unlock()
	var held bool
	if whole {
		held = telegramFlowHeld(f.flows, flow, now)
	} else {
		held = telegramFlowHeld(f.pairs, pair, now)
	}
	if held && extend {
		until := now.Add(telegramFlowTTL)
		if whole {
			f.flows[flow] = until
		}
		if _, had := f.pairs[pair]; had {
			f.pairs[pair] = until
		}
	}
	f.countLocked()
	return held
}

func telegramFlowHeld[K comparable](m map[K]time.Time, k K, now time.Time) bool {
	until, had := m[k]
	if !had {
		return false
	}
	if now.Before(until) {
		return true
	}
	delete(m, k)
	return false
}

func (f *telegramFlows) sweep(now time.Time) {
	if f == nil || f.live.Load() == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweepLocked(now)
	f.countLocked()
}

func (f *telegramFlows) sweepLocked(now time.Time) {
	for k, until := range f.flows {
		if !now.Before(until) {
			delete(f.flows, k)
		}
	}
	for k, until := range f.pairs {
		if !now.Before(until) {
			delete(f.pairs, k)
		}
	}
}

func (f *telegramFlows) countLocked() {
	f.live.Store(int64(len(f.flows) + len(f.pairs)))
}
