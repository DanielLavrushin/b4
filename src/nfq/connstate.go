package nfq

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type connInfo struct {
	bytesIn   uint64
	threshold uint64
	set       *config.SetConfig
	lastSeen  time.Time
}

type connStateTracker struct {
	mu    sync.RWMutex
	conns map[string]*connInfo
}

var connState = &connStateTracker{
	conns: make(map[string]*connInfo),
}

func (t *connStateTracker) RegisterOutgoing(connKey string, set *config.SetConfig) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.conns[connKey] = &connInfo{
		set:      set,
		lastSeen: time.Now(),
	}
}

func (t *connStateTracker) GetSetForIncoming(clientIP string, clientPort uint16, serverIP string, serverPort uint16) *config.SetConfig {
	outKey := fmt.Sprintf("%s:%d->%s:%d", clientIP, clientPort, serverIP, serverPort)

	t.mu.Lock()
	defer t.mu.Unlock()

	info, exists := t.conns[outKey]
	if !exists || info.set == nil {
		return nil
	}

	info.lastSeen = time.Now()
	return info.set
}

func (t *connStateTracker) TrackIncomingBytes(clientIP string, clientPort uint16, serverIP string, serverPort uint16, bytes uint64, inc *config.IncomingConfig) bool {
	outKey := fmt.Sprintf("%s:%d->%s:%d", clientIP, clientPort, serverIP, serverPort)

	t.mu.Lock()
	defer t.mu.Unlock()

	info, exists := t.conns[outKey]
	if !exists {
		return false
	}

	if info.threshold == 0 {
		minKB := inc.Min
		maxKB := inc.Max
		if maxKB == 0 || maxKB < minKB {
			maxKB = minKB
		}
		if minKB <= 0 {
			minKB = 14
			maxKB = 14
		}

		if minKB == maxKB {
			info.threshold = uint64(minKB * 1024)
		} else {
			info.threshold = uint64((minKB + rand.Intn(maxKB-minKB+1)) * 1024)
		}
	}

	prevBytes := info.bytesIn
	info.bytesIn += bytes
	info.lastSeen = time.Now()

	if prevBytes < info.threshold && info.bytesIn >= info.threshold {
		info.bytesIn = 0
		info.threshold = 0
		return true
	}

	return false
}

func (t *connStateTracker) Cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for k, v := range t.conns {
		if now.Sub(v.lastSeen) > 120*time.Second {
			delete(t.conns, k)
		}
	}
}
