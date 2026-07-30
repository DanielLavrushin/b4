package nfq

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxPendingHelloEntries = 2048
	maxPendingHelloBytes   = 1 << 20
	maxPendingHelloRecord  = 4096
	pendingHelloTTL        = 2 * time.Second
)

type pendingHello struct {
	startSeq uint32
	endSeq   uint32
	data     []byte
	storedAt time.Time
}

type pendingHelloCache struct {
	mu    sync.Mutex
	flows map[string]*pendingHello
	bytes int
	live  atomic.Int64
}

func newPendingHelloCache() *pendingHelloCache {
	return &pendingHelloCache{flows: make(map[string]*pendingHello)}
}

func truncatedClientHello(payload []byte) bool {
	if len(payload) < 6 || payload[0] != TLSHandshakeType || payload[5] != TLSClientHello {
		return false
	}
	recLen := int(payload[3])<<8 | int(payload[4])
	if recLen <= 0 {
		return false
	}
	return 5+recLen > len(payload)
}

func (c *pendingHelloCache) Feed(connKey string, seq uint32, payload []byte) ([]byte, int, bool) {
	if c == nil || len(payload) == 0 {
		return nil, 0, false
	}

	if c.live.Load() == 0 && !truncatedClientHello(payload) {
		return nil, 0, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.syncLiveLocked()

	entry := c.flows[connKey]
	if entry != nil && time.Since(entry.storedAt) > pendingHelloTTL {
		c.dropLocked(connKey)
		entry = nil
	}

	if entry == nil {
		c.storeLocked(connKey, seq, payload)
		return nil, 0, false
	}

	joined, prefix, related := joinSegment(entry, payload, seq)
	if !related {
		c.dropLocked(connKey)
		c.storeLocked(connKey, seq, payload)
		return nil, 0, false
	}
	if joined == nil {
		return nil, 0, false
	}

	startSeq := entry.startSeq
	c.dropLocked(connKey)
	if len(joined) <= maxPendingHelloRecord {
		c.storeLocked(connKey, startSeq, joined)
	}

	return joined, prefix, true
}

func joinSegment(entry *pendingHello, payload []byte, seq uint32) ([]byte, int, bool) {
	gap := int32(seq - entry.endSeq)
	if gap > 0 {
		return nil, 0, false
	}
	if int32(seq-entry.startSeq) < 0 {
		return nil, 0, false
	}

	overlap := int(-gap)
	if overlap >= len(payload) {
		return nil, 0, true
	}

	fresh := payload[overlap:]
	joined := make([]byte, 0, len(entry.data)+len(fresh))
	joined = append(joined, entry.data...)
	joined = append(joined, fresh...)
	return joined, len(entry.data), true
}

func (c *pendingHelloCache) Drop(connKey string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropLocked(connKey)
	c.syncLiveLocked()
}

func (c *pendingHelloCache) syncLiveLocked() {
	c.live.Store(int64(len(c.flows)))
}

func (c *pendingHelloCache) dropLocked(connKey string) {
	entry, ok := c.flows[connKey]
	if !ok {
		return
	}
	c.bytes -= len(entry.data)
	delete(c.flows, connKey)
}

func (c *pendingHelloCache) storeLocked(connKey string, seq uint32, payload []byte) {
	if len(payload) > maxPendingHelloRecord || !truncatedClientHello(payload) {
		return
	}

	c.evictLocked(len(payload))
	if len(c.flows) >= maxPendingHelloEntries || c.bytes+len(payload) > maxPendingHelloBytes {
		return
	}

	data := make([]byte, len(payload))
	copy(data, payload)
	c.flows[connKey] = &pendingHello{
		startSeq: seq,
		endSeq:   seq + uint32(len(payload)),
		data:     data,
		storedAt: time.Now(),
	}
	c.bytes += len(data)
}

func (c *pendingHelloCache) evictLocked(incoming int) {
	if len(c.flows) < maxPendingHelloEntries && c.bytes+incoming <= maxPendingHelloBytes {
		return
	}

	now := time.Now()
	for k, v := range c.flows {
		if now.Sub(v.storedAt) > pendingHelloTTL {
			c.bytes -= len(v.data)
			delete(c.flows, k)
		}
	}

	for len(c.flows) >= maxPendingHelloEntries || c.bytes+incoming > maxPendingHelloBytes {
		var oldestKey string
		var oldestAt time.Time
		for k, v := range c.flows {
			if oldestAt.IsZero() || v.storedAt.Before(oldestAt) {
				oldestKey = k
				oldestAt = v.storedAt
			}
		}
		if oldestKey == "" {
			return
		}
		c.dropLocked(oldestKey)
	}
}

func (c *pendingHelloCache) Cleanup() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, v := range c.flows {
		if now.Sub(v.storedAt) > pendingHelloTTL {
			c.bytes -= len(v.data)
			delete(c.flows, k)
		}
	}
	c.syncLiveLocked()
}

func (c *pendingHelloCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.flows)
}
