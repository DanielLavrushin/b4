package nfq

import (
	"encoding/binary"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/log"
	"golang.org/x/sys/unix"
)

const (
	flowDropRecentTTL  = 5 * time.Second
	maxRecentFlowDrops = 4096
)

type udpDropWriters struct {
	v4 func(src, dst netip.AddrPort) error
	v6 func(src, dst netip.AddrPort) error
}

type conntrackDeleter interface {
	Enqueue(t ctnl.Tuple, dir ctnl.Direction) bool
	EnqueueBestEffort(t ctnl.Tuple, dir ctnl.Direction) bool
}

type conntrackDeleterRef struct {
	d conntrackDeleter
}

type flowDropKey struct {
	src netip.AddrPort
	dst netip.AddrPort
}

type flowDropPersistence struct {
	writers   atomic.Pointer[udpDropWriters]
	deleter   atomic.Pointer[conntrackDeleterRef]
	mu        sync.Mutex
	recent    map[flowDropKey]time.Time
	nftWarned atomic.Bool

	nftWritten  atomic.Uint64
	nftRecent   atomic.Uint64
	nftFailed   atomic.Uint64
	ctQueued    atomic.Uint64
	ctRefused   atomic.Uint64
	ctBehindSet atomic.Uint64
	unpersisted atomic.Uint64
}

type QUICDropPersistence struct {
	NftV4              bool   `json:"nft_v4"`
	NftV6              bool   `json:"nft_v6"`
	Conntrack          bool   `json:"conntrack"`
	NftWritten         uint64 `json:"nft_written"`
	NftRecent          uint64 `json:"nft_recent"`
	NftFailed          uint64 `json:"nft_failed"`
	ConntrackQueued    uint64 `json:"conntrack_queued"`
	ConntrackRefused   uint64 `json:"conntrack_refused"`
	ConntrackBehindSet uint64 `json:"conntrack_behind_set"`
	Unpersisted        uint64 `json:"unpersisted"`
}

var flowDropCurrent atomic.Pointer[flowDropPersistence]

func QUICDropPersistenceState() QUICDropPersistence {
	f := flowDropCurrent.Load()
	if f == nil {
		return QUICDropPersistence{}
	}
	return f.snapshot()
}

func newFlowDropPersistence() *flowDropPersistence {
	return &flowDropPersistence{recent: make(map[flowDropKey]time.Time)}
}

func (f *flowDropPersistence) snapshot() QUICDropPersistence {
	s := QUICDropPersistence{
		Conntrack:          f.deleter.Load() != nil,
		NftWritten:         f.nftWritten.Load(),
		NftRecent:          f.nftRecent.Load(),
		NftFailed:          f.nftFailed.Load(),
		ConntrackQueued:    f.ctQueued.Load(),
		ConntrackRefused:   f.ctRefused.Load(),
		ConntrackBehindSet: f.ctBehindSet.Load(),
		Unpersisted:        f.unpersisted.Load(),
	}
	if w := f.writers.Load(); w != nil {
		s.NftV4, s.NftV6 = w.v4 != nil, w.v6 != nil
	}
	return s
}

func (f *flowDropPersistence) setWriters(v4, v6 func(src, dst netip.AddrPort) error) {
	if v4 == nil && v6 == nil {
		f.writers.Store(nil)
	} else {
		f.writers.Store(&udpDropWriters{v4: v4, v6: v6})
	}
	f.mu.Lock()
	clear(f.recent)
	f.mu.Unlock()
}

func (f *flowDropPersistence) setDeleter(d conntrackDeleter) {
	if d == nil {
		f.deleter.Store(nil)
		return
	}
	f.deleter.Store(&conntrackDeleterRef{d: d})
}

func (f *flowDropPersistence) writer(v6 bool) func(src, dst netip.AddrPort) error {
	w := f.writers.Load()
	if w == nil {
		return nil
	}
	if v6 {
		return w.v6
	}
	return w.v4
}

func (f *flowDropPersistence) persist(raw []byte, now time.Time) {
	src, dst, ok := udpFlowEndpoints(raw)
	if !ok {
		f.unpersisted.Add(1)
		return
	}
	if write := f.writer(src.Addr().Is6()); write != nil {
		if f.seenRecently(src, dst, now) {
			f.nftRecent.Add(1)
			return
		}
		err := write(src, dst)
		if err == nil {
			f.nftWritten.Add(1)
			f.restartWindow(src, dst)
			return
		}
		f.forget(src, dst)
		f.nftFailed.Add(1)
		if f.nftWarned.CompareAndSwap(false, true) {
			log.Warnf("Adding the dropped QUIC connection %s -> %s to b4's nftables drop set failed: %v. b4 deletes its conntrack entry instead where it can; later failures are counted in the diagnostics, not logged", src, dst, err)
		}
	}
	if ref := f.deleter.Load(); ref != nil {
		if t, err := ctnl.NewTuple(unix.IPPROTO_UDP, src, dst); err == nil {
			if ref.d.Enqueue(t, ctnl.Original) {
				f.ctQueued.Add(1)
			} else {
				f.ctRefused.Add(1)
			}
			return
		}
	}
	f.unpersisted.Add(1)
}

func (f *flowDropPersistence) restartWindow(src, dst netip.AddrPort) {
	ref := f.deleter.Load()
	if ref == nil {
		return
	}
	if t, err := ctnl.NewTuple(unix.IPPROTO_UDP, src, dst); err == nil && ref.d.EnqueueBestEffort(t, ctnl.Original) {
		f.ctBehindSet.Add(1)
	}
}

func (f *flowDropPersistence) seenRecently(src, dst netip.AddrPort, now time.Time) bool {
	key := flowDropKey{src: src, dst: dst}
	f.mu.Lock()
	defer f.mu.Unlock()
	if at, ok := f.recent[key]; ok && now.Sub(at) < flowDropRecentTTL {
		return true
	}
	if len(f.recent) >= maxRecentFlowDrops {
		for k, at := range f.recent {
			if now.Sub(at) >= flowDropRecentTTL {
				delete(f.recent, k)
			}
		}
		if len(f.recent) >= maxRecentFlowDrops {
			clear(f.recent)
		}
	}
	f.recent[key] = now
	return false
}

func (f *flowDropPersistence) forget(src, dst netip.AddrPort) {
	f.mu.Lock()
	delete(f.recent, flowDropKey{src: src, dst: dst})
	f.mu.Unlock()
}

func udpFlowEndpoints(raw []byte) (netip.AddrPort, netip.AddrPort, bool) {
	if len(raw) == 0 {
		return netip.AddrPort{}, netip.AddrPort{}, false
	}
	var src, dst netip.Addr
	var l4 int
	switch raw[0] >> 4 {
	case IPv4:
		if len(raw) < IPv4HeaderMinLen || raw[9] != unix.IPPROTO_UDP {
			return netip.AddrPort{}, netip.AddrPort{}, false
		}
		l4 = int(raw[0]&0x0f) * 4
		if l4 < IPv4HeaderMinLen {
			return netip.AddrPort{}, netip.AddrPort{}, false
		}
		src = netip.AddrFrom4([4]byte(raw[12:16]))
		dst = netip.AddrFrom4([4]byte(raw[16:20]))
	case IPv6:
		off, next, ok := upperLayerOffsetV6(raw)
		if !ok || next != unix.IPPROTO_UDP {
			return netip.AddrPort{}, netip.AddrPort{}, false
		}
		l4 = off
		src = netip.AddrFrom16([16]byte(raw[8:24]))
		dst = netip.AddrFrom16([16]byte(raw[24:40]))
	default:
		return netip.AddrPort{}, netip.AddrPort{}, false
	}
	if len(raw) < l4+UDPHeaderLen {
		return netip.AddrPort{}, netip.AddrPort{}, false
	}
	sport := binary.BigEndian.Uint16(raw[l4 : l4+2])
	dport := binary.BigEndian.Uint16(raw[l4+2 : l4+4])
	return netip.AddrPortFrom(src, sport), netip.AddrPortFrom(dst, dport), true
}

func (w *Worker) persistFlowDrop(raw []byte) {
	if w.flowDrops != nil {
		w.flowDrops.persist(raw, time.Now())
	}
}

func (p *Pool) SetUDPDropWriters(v4, v6 func(src, dst netip.AddrPort) error) {
	if p == nil || p.state == nil {
		return
	}
	p.state.flowDrops.setWriters(v4, v6)
}
