package nfq

import (
	"encoding/binary"
	"net/netip"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
)

const (
	maxStreamStarts = 20000
	streamStartTTL  = 60 * time.Second
)

type streamKey struct {
	client, server netip.AddrPort
}

type streamStartCache struct {
	mu        sync.Mutex
	cur, prev map[streamKey]uint32
	rotated   time.Time
}

func newStreamStartCache() *streamStartCache {
	return &streamStartCache{cur: make(map[streamKey]uint32), prev: make(map[streamKey]uint32), rotated: time.Now()}
}

type streamGate struct {
	cfg *config.Config
	on  bool
}

func usesSequenceOverlap(cfg *config.Config) bool {
	for _, set := range cfg.Sets {
		if set != nil && set.Fragmentation.SeqOverlapLength > 0 && len(set.Fragmentation.SeqOverlapBytes) > 0 {
			return true
		}
	}
	return false
}

func (w *Worker) recordsStreamStarts(cfg *config.Config) bool {
	if g := w.streamsGate.Load(); g != nil && g.cfg == cfg {
		return g.on
	}
	on := usesSequenceOverlap(cfg)
	w.streamsGate.Store(&streamGate{cfg: cfg, on: on})
	return on
}

func streamKeyOf(packet []byte, tcpAt int) (streamKey, bool) {
	if len(packet) < tcpAt+8 {
		return streamKey{}, false
	}
	var client, server netip.Addr
	switch packet[0] >> 4 {
	case 4:
		if tcpAt < IPv4HeaderMinLen {
			return streamKey{}, false
		}
		client = netip.AddrFrom4([4]byte(packet[12:16]))
		server = netip.AddrFrom4([4]byte(packet[16:20]))
	case 6:
		if tcpAt < IPv6HeaderLen {
			return streamKey{}, false
		}
		client = netip.AddrFrom16([16]byte(packet[8:24]))
		server = netip.AddrFrom16([16]byte(packet[24:40]))
	default:
		return streamKey{}, false
	}
	tcp := packet[tcpAt:]
	return streamKey{
		client: netip.AddrPortFrom(client, binary.BigEndian.Uint16(tcp[0:2])),
		server: netip.AddrPortFrom(server, binary.BigEndian.Uint16(tcp[2:4])),
	}, true
}

func (c *streamStartCache) Open(syn []byte, tcpAt int, now time.Time) {
	if c == nil {
		return
	}
	key, ok := streamKeyOf(syn, tcpAt)
	if !ok {
		return
	}
	start := binary.BigEndian.Uint32(syn[tcpAt+4:]) + 1

	c.mu.Lock()
	defer c.mu.Unlock()
	c.cur[key] = start
	if len(c.cur) >= maxStreamStarts/2 {
		c.rotateLocked(now)
	}
}

func (c *streamStartCache) Starts(packet []byte, tcpAt int, seq uint32) bool {
	if c == nil {
		return false
	}
	key, ok := streamKeyOf(packet, tcpAt)
	if !ok {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if start, found := c.cur[key]; found {
		return start == seq
	}
	start, found := c.prev[key]
	return found && start == seq
}

func (c *streamStartCache) Cleanup(now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Sub(c.rotated) >= streamStartTTL {
		c.rotateLocked(now)
	}
}

func (c *streamStartCache) rotateLocked(now time.Time) {
	c.prev, c.cur = c.cur, make(map[streamKey]uint32)
	c.rotated = now
}
