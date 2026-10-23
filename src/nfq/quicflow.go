package nfq

import (
	"bytes"
	"encoding/binary"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/quic"
	"github.com/daniellavrushin/b4/sni"
)

const (
	maxQUICFlowEntries = 10000
	quicFlowIdle       = 60 * time.Second
	quicFlowShared     = 5 * time.Second
	maxCoalescedHellos = 2
)

type quicFlow struct {
	host      string
	odcid     []byte
	coalesced int
	lastSeen  time.Time
}

type quicFlowCache struct {
	mu    sync.RWMutex
	flows map[string]*quicFlow
}

func newQUICFlowCache() *quicFlowCache {
	return &quicFlowCache{flows: make(map[string]*quicFlow)}
}

func (c *quicFlowCache) Store(connKey, host string, odcid []byte) {
	if c == nil || host == "" || len(odcid) == 0 {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	carried := 0
	if existing, exists := c.flows[connKey]; exists {
		if bytes.Equal(existing.odcid, odcid) {
			existing.host = host
			existing.lastSeen = now
			return
		}
		if now.Sub(existing.lastSeen) <= quicFlowShared {
			carried = existing.coalesced
		}
	} else if len(c.flows) >= maxQUICFlowEntries {
		c.evictLocked(now)
	}
	c.flows[connKey] = &quicFlow{host: host, odcid: bytes.Clone(odcid), coalesced: carried, lastSeen: now}
}

func (c *quicFlowCache) NoteQUIC(connKey string, odcid []byte) {
	if c == nil || len(odcid) == 0 {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, exists := c.flows[connKey]; exists {
		if now.Sub(existing.lastSeen) <= quicFlowIdle {
			return
		}
	} else if len(c.flows) >= maxQUICFlowEntries {
		c.evictLocked(now)
	}
	c.flows[connKey] = &quicFlow{odcid: bytes.Clone(odcid), lastSeen: now}
}

func (c *quicFlowCache) NoteCoalesced(connKey string) int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	flow := c.flows[connKey]
	if flow == nil {
		return 0
	}
	flow.coalesced++
	return flow.coalesced
}

func (c *quicFlowCache) Lookup(connKey string, payload []byte) (string, bool) {
	if c == nil {
		return "", false
	}
	now := time.Now()
	c.mu.RLock()
	flow := c.flows[connKey]
	var host string
	var odcid []byte
	var idle time.Duration
	if flow != nil {
		host, odcid, idle = flow.host, flow.odcid, now.Sub(flow.lastSeen)
	}
	c.mu.RUnlock()
	if flow == nil {
		return "", false
	}
	if idle > quicFlowIdle {
		c.forgetFlow(connKey, flow)
		return "", false
	}

	if quic.IsInitial(payload) {
		dcid := quic.ParseDCID(payload)
		if !bytes.Equal(dcid, odcid) {
			if _, ok := quic.DecryptInitial(odcid, payload); !ok {
				if _, own := quic.DecryptInitial(dcid, payload); own {
					c.takeOverFlow(connKey, flow, dcid)
				}
				return "", false
			}
		}
	}

	if idle > time.Second {
		c.mu.Lock()
		if current, ok := c.flows[connKey]; ok && current == flow {
			current.lastSeen = now
		}
		c.mu.Unlock()
	}
	return host, true
}

func (c *quicFlowCache) Forget(connKey string) {
	if c == nil {
		return
	}
	c.mu.RLock()
	_, exists := c.flows[connKey]
	c.mu.RUnlock()
	if !exists {
		return
	}
	c.mu.Lock()
	delete(c.flows, connKey)
	c.mu.Unlock()
}

func (c *quicFlowCache) forgetFlow(connKey string, flow *quicFlow) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if current, ok := c.flows[connKey]; ok && current == flow {
		delete(c.flows, connKey)
	}
}

func (c *quicFlowCache) takeOverFlow(connKey string, flow *quicFlow, odcid []byte) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if current, ok := c.flows[connKey]; !ok || current != flow {
		return
	}
	carried := 0
	if now.Sub(flow.lastSeen) <= quicFlowShared {
		carried = flow.coalesced
	}
	c.flows[connKey] = &quicFlow{odcid: bytes.Clone(odcid), coalesced: carried, lastSeen: now}
}

func (c *quicFlowCache) evictLocked(now time.Time) {
	var oldestKey string
	var oldestAt time.Time
	for k, v := range c.flows {
		if now.Sub(v.lastSeen) > quicFlowIdle {
			delete(c.flows, k)
		} else if oldestKey == "" || v.lastSeen.Before(oldestAt) {
			oldestKey = k
			oldestAt = v.lastSeen
		}
	}
	if len(c.flows) >= maxQUICFlowEntries && oldestKey != "" {
		delete(c.flows, oldestKey)
	}
}

func (c *quicFlowCache) Cleanup() {
	if c == nil {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.flows {
		if now.Sub(v.lastSeen) > quicFlowIdle {
			delete(c.flows, k)
		}
	}
}

func looksLikeWireGuard(payload []byte) bool {
	if len(payload) < 4 || payload[1] != 0 || payload[2] != 0 || payload[3] != 0 {
		return false
	}
	switch payload[0] {
	case 1:
		return len(payload) == 148
	case 2:
		return len(payload) == 92
	case 3:
		return len(payload) == 64
	case 4:
		return len(payload) >= 32
	}
	return false
}

func looksLikeDTLS(payload []byte) bool {
	if len(payload) < 13 || payload[0] < 0x14 || payload[0] > 0x17 || payload[1] != 0xfe {
		return false
	}
	if payload[2] != 0xfd && payload[2] != 0xff {
		return false
	}
	return int(binary.BigEndian.Uint16(payload[11:13]))+13 <= len(payload)
}

func mayBelongToAQUICFlow(payload []byte, isVoiceMedia bool) bool {
	if isVoiceMedia || !quic.LooksLikeShortHeader(payload) {
		return false
	}
	return !looksLikeWireGuard(payload) && !looksLikeDTLS(payload)
}

func dropsQUICFlow(set *config.SetConfig) bool {
	if set.Routing.Enabled && config.RoutingIsBlock(set.Routing.Mode) {
		return true
	}
	return set.UDP.Mode == config.UDPModeDrop || set.UDP.Mode == config.UDPModeReject
}

func (w *Worker) quicFlowSet(cfg *config.Config, matcher *sni.SuffixSet, connKey string, pkt *pktInfo, dport uint16, payload []byte, keep sni.SetFilter) (*config.SetConfig, string) {
	host, _ := w.quicFlows.Lookup(connKey, payload)
	if host == "" {
		return nil, ""
	}
	matched, set := matcher.MatchSNIWithSourceTLS(host, pkt.srcMac, 0x0304, pkt.ver, keep)
	if !matched || !set.MatchesUDPDPort(dport) {
		return nil, ""
	}
	if escSet := w.escalatedSetFor(cfg, set, host, pkt.srcMac, keep); escSet != nil {
		set = escSet
	}
	if !dropsQUICFlow(set) {
		return nil, ""
	}
	return set, host
}
