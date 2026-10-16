package tun

import (
	"encoding/binary"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/engine"
)

type packetProcessor interface {
	ProcessPacketDir(raw []byte, dir engine.Direction) engine.PacketVerdict
}

type conntrackDeleter interface {
	Enqueue(t ctnl.Tuple, dir ctnl.Direction) bool
	Stop()
}

var (
	newConntrackDeleter = func() conntrackDeleter { return ctnl.NewWorker() }
	conntrackSelfTest   = ctnl.Available
)

type flowHoldCounters struct {
	firstN    atomic.Uint64
	ctQueued  atomic.Uint64
	ctRefused atomic.Uint64
	unheld    atomic.Uint64
}

type QUICDropPersistence struct {
	FirstN           bool
	Conntrack        bool
	Held             uint64
	ConntrackQueued  uint64
	ConntrackRefused uint64
	Unheld           uint64
}

func (e *Engine) QUICDropPersistence() QUICDropPersistence {
	return QUICDropPersistence{
		FirstN:           e.limiter != nil,
		Conntrack:        e.conntrack != nil,
		Held:             e.holds.firstN.Load(),
		ConntrackQueued:  e.holds.ctQueued.Load(),
		ConntrackRefused: e.holds.ctRefused.Load(),
		Unheld:           e.holds.unheld.Load(),
	}
}

func (e *Engine) startFlowHold() {
	if e.limiter != nil || e.conntrack != nil {
		return
	}
	e.conntrack = newConntrackDeleter()
	conntrackSelfTest()
}

func (e *Engine) stopFlowHold() {
	if e.conntrack != nil {
		e.conntrack.Stop()
	}
}

func (e *Engine) processPacket(worker packetProcessor, raw []byte) (bool, engine.Direction) {
	pass, flow := true, engine.DirUnknown
	if l := e.limiter; l != nil {
		pass, flow = l.admit(raw, time.Now())
	}
	dir := e.packetDirection(raw, flow)
	if !pass {
		return true, dir
	}
	switch worker.ProcessPacketDir(raw, dir) {
	case engine.VerdictAccept:
		return true, dir
	case engine.VerdictDropFlow:
		e.holdFlow(raw)
	}
	return false, dir
}

func (e *Engine) holdFlow(raw []byte) {
	if len(raw) == 0 || raw[0]>>4 != 4 {
		return
	}
	if l := e.limiter; l != nil {
		if l.hold(raw) {
			e.holds.firstN.Add(1)
		} else {
			e.holds.unheld.Add(1)
		}
		return
	}
	t, ok := replyTuple(raw)
	if !ok || e.conntrack == nil {
		e.holds.unheld.Add(1)
		return
	}
	if e.conntrack.Enqueue(t, ctnl.Reply) {
		e.holds.ctQueued.Add(1)
	} else {
		e.holds.ctRefused.Add(1)
	}
}

func replyTuple(raw []byte) (ctnl.Tuple, bool) {
	if len(raw) < 20 || raw[0]>>4 != 4 {
		return ctnl.Tuple{}, false
	}
	ihl := int(raw[0]&0x0f) * 4
	if ihl < 20 || len(raw) < ihl+4 || binary.BigEndian.Uint16(raw[6:8])&0x3fff != 0 {
		return ctnl.Tuple{}, false
	}
	src := netip.AddrPortFrom(netip.AddrFrom4([4]byte(raw[12:16])), binary.BigEndian.Uint16(raw[ihl:ihl+2]))
	dst := netip.AddrPortFrom(netip.AddrFrom4([4]byte(raw[16:20])), binary.BigEndian.Uint16(raw[ihl+2:ihl+4]))
	t, err := ctnl.NewTuple(raw[9], dst, src)
	return t, err == nil
}
