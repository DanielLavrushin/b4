package nfq

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/florianl/go-nfqueue"
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
	hold     *helloHold
}

type pendingHelloCache struct {
	mu       sync.Mutex
	flows    map[string]*pendingHello
	bytes    int
	live     atomic.Int64
	holding  atomic.Int64
	heldOnce map[string]time.Time
	onceLen  atomic.Int64
	closed   bool
}

type feedResult struct {
	joined  []byte
	prefix  int
	ok      bool
	claimed *helloHold
	held    *helloHold
}

func newPendingHelloCache() *pendingHelloCache {
	return &pendingHelloCache{flows: make(map[string]*pendingHello)}
}

func startsClientHello(payload []byte) bool {
	if len(payload) < 6 || payload[0] != TLSHandshakeType || payload[5] != TLSClientHello {
		return false
	}
	if len(payload) > 9 && payload[9] != TLSVersionMajor {
		return false
	}
	return len(payload) <= 10 || payload[10] <= TLSMaxVersionMinor
}

func truncatedClientHello(payload []byte) bool {
	if !startsClientHello(payload) {
		return false
	}
	recLen := int(payload[3])<<8 | int(payload[4])
	if recLen <= 0 {
		return false
	}
	return 5+recLen > len(payload)
}

func (c *pendingHelloCache) Feed(connKey string, seq uint32, payload []byte) ([]byte, int, bool) {
	res := c.FeedHold(connKey, seq, payload, nil)
	if res.claimed != nil {
		res.claimed.settle(nfqueue.NfAccept, "released ahead of the next packet")
	}
	return res.joined, res.prefix, res.ok
}

func (c *pendingHelloCache) FeedHold(connKey string, seq uint32, payload []byte, hold *helloHold) feedResult {
	if c == nil || len(payload) == 0 {
		return feedResult{}
	}

	if c.live.Load() == 0 && !truncatedClientHello(payload) {
		return feedResult{}
	}

	c.mu.Lock()
	res, released := c.feedLocked(connKey, seq, payload, hold)
	c.syncLiveLocked()
	c.mu.Unlock()

	if released != nil {
		released.report("released, the next segment did not continue the record")
	}
	if res.held != nil {
		log.Tracef("Holding segment 1 of a split ClientHello %s until the rest of the record arrives", connKey)
	}
	return res
}

func (c *pendingHelloCache) feedLocked(connKey string, seq uint32, payload []byte, hold *helloHold) (feedResult, *helloHold) {
	var res feedResult
	var released *helloHold

	entry := c.flows[connKey]
	if entry != nil && time.Since(entry.storedAt) > pendingHelloTTL {
		released = c.dropLocked(connKey)
		entry = nil
	}

	if entry == nil {
		res.held = c.storeLocked(connKey, seq, payload, hold)
		return res, released
	}

	joined, prefix, related := joinSegment(entry, payload, seq)
	if !related {
		released = c.dropLocked(connKey)
		res.held = c.storeLocked(connKey, seq, payload, hold)
		return res, released
	}
	res.claimed = c.claimLocked(entry)
	if joined == nil {
		return res, released
	}

	startSeq := entry.startSeq
	c.dropLocked(connKey)
	if len(joined) <= maxPendingHelloRecord {
		c.storeLocked(connKey, startSeq, joined, nil)
	}

	res.joined, res.prefix, res.ok = joined, prefix, true
	return res, released
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
	released := c.dropLocked(connKey)
	c.syncLiveLocked()
	c.mu.Unlock()
	if released != nil {
		released.report("released with its pending record")
	}
}

func (c *pendingHelloCache) syncLiveLocked() {
	c.live.Store(int64(len(c.flows)))
}

func (c *pendingHelloCache) dropLocked(connKey string) *helloHold {
	entry, ok := c.flows[connKey]
	if !ok {
		return nil
	}
	released := c.releaseLocked(entry)
	c.bytes -= len(entry.data)
	delete(c.flows, connKey)
	return released
}

func (c *pendingHelloCache) storeLocked(connKey string, seq uint32, payload []byte, hold *helloHold) *helloHold {
	if len(payload) > maxPendingHelloRecord || !truncatedClientHello(payload) {
		return nil
	}

	c.evictLocked(len(payload))
	if len(c.flows) >= maxPendingHelloEntries || c.bytes+len(payload) > maxPendingHelloBytes {
		return nil
	}

	data := make([]byte, len(payload))
	copy(data, payload)
	entry := &pendingHello{
		startSeq: seq,
		endSeq:   seq + uint32(len(payload)),
		data:     data,
		storedAt: time.Now(),
	}
	c.flows[connKey] = entry
	c.bytes += len(data)

	if hold == nil || !c.canHoldLocked(connKey) {
		return nil
	}
	c.attachLocked(connKey, entry, hold)
	return hold
}

func (c *pendingHelloCache) canHoldLocked(connKey string) bool {
	if c.closed || c.holding.Load() >= maxHeldHellos {
		return false
	}
	if at, seen := c.heldOnce[connKey]; seen {
		return time.Since(at) >= heldOnceTTL
	}
	return len(c.heldOnce) < maxHeldOnce
}

func (c *pendingHelloCache) attachLocked(connKey string, entry *pendingHello, h *helloHold) {
	now := time.Now()
	h.key, h.at = connKey, now
	entry.hold = h
	c.holding.Add(1)
	if c.heldOnce == nil {
		c.heldOnce = make(map[string]time.Time)
	}
	c.heldOnce[connKey] = now
	c.onceLen.Store(int64(len(c.heldOnce)))
	limit := helloHoldMax
	if h.local {
		limit = helloHoldLocalMax
	}
	h.timer = time.AfterFunc(limit, func() { c.expire(connKey, h) })
}

func (c *pendingHelloCache) detachLocked(entry *pendingHello) *helloHold {
	h := entry.hold
	if h == nil {
		return nil
	}
	entry.hold = nil
	if h.timer != nil {
		h.timer.Stop()
	}
	return h
}

func (c *pendingHelloCache) claimLocked(entry *pendingHello) *helloHold {
	h := c.detachLocked(entry)
	if h != nil {
		c.holding.Add(-1)
	}
	return h
}

func (c *pendingHelloCache) releaseLocked(entry *pendingHello) *helloHold {
	h := c.detachLocked(entry)
	if h != nil {
		h.verdict(nfqueue.NfAccept)
		c.holding.Add(-1)
	}
	return h
}

func (c *pendingHelloCache) TakeHold(connKey string) *helloHold {
	if c == nil || c.holding.Load() == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.flows[connKey]
	if entry == nil {
		return nil
	}
	return c.claimLocked(entry)
}

func (c *pendingHelloCache) withdraw(connKey string, h *helloHold) bool {
	if c == nil || h == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.flows[connKey]
	if entry == nil || entry.hold != h {
		return false
	}
	c.claimLocked(entry)
	return true
}

func (c *pendingHelloCache) expire(connKey string, h *helloHold) {
	c.mu.Lock()
	entry := c.flows[connKey]
	released := entry != nil && entry.hold == h
	if released {
		c.releaseLocked(entry)
	}
	c.mu.Unlock()
	if released {
		h.report("released, the rest of the record did not arrive")
	}
}

func (c *pendingHelloCache) ReleaseHolds() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.closed = true
	var released []*helloHold
	for _, entry := range c.flows {
		if h := c.releaseLocked(entry); h != nil {
			released = append(released, h)
		}
	}
	c.mu.Unlock()
	for _, h := range released {
		h.report("released at shutdown")
	}
}

func (c *pendingHelloCache) ForgetHeld(connKey string) {
	if c == nil || c.onceLen.Load() == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.heldOnce, connKey)
	c.onceLen.Store(int64(len(c.heldOnce)))
}

func (c *pendingHelloCache) evictLocked(incoming int) {
	if len(c.flows) < maxPendingHelloEntries && c.bytes+incoming <= maxPendingHelloBytes {
		return
	}

	now := time.Now()
	for k, v := range c.flows {
		if v.hold == nil && now.Sub(v.storedAt) > pendingHelloTTL {
			c.bytes -= len(v.data)
			delete(c.flows, k)
		}
	}

	for len(c.flows) >= maxPendingHelloEntries || c.bytes+incoming > maxPendingHelloBytes {
		var oldestKey string
		var oldestAt time.Time
		for k, v := range c.flows {
			if v.hold == nil && (oldestAt.IsZero() || v.storedAt.Before(oldestAt)) {
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
		if v.hold == nil && now.Sub(v.storedAt) > pendingHelloTTL {
			c.bytes -= len(v.data)
			delete(c.flows, k)
		}
	}
	for k, at := range c.heldOnce {
		if now.Sub(at) >= heldOnceTTL {
			delete(c.heldOnce, k)
		}
	}
	if len(c.heldOnce) == 0 {
		c.heldOnce = nil
	}
	c.onceLen.Store(int64(len(c.heldOnce)))
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
