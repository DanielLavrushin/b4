package nfq

import (
	"bytes"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/quic"
	"github.com/daniellavrushin/b4/sni"
)

const (
	maxQUICFlowEntries = 10000
	quicFlowIdle       = 60 * time.Second
)

type quicFlow struct {
	host     string
	odcid    []byte
	lastSeen time.Time
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
	if _, exists := c.flows[connKey]; !exists && len(c.flows) >= maxQUICFlowEntries {
		c.evictLocked(now)
	}
	c.flows[connKey] = &quicFlow{host: host, odcid: bytes.Clone(odcid), lastSeen: now}
}

func (c *quicFlowCache) Lookup(connKey string, payload []byte) string {
	if c == nil {
		return ""
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
	if flow == nil || idle > quicFlowIdle {
		return ""
	}

	if quic.IsInitial(payload) {
		dcid := quic.ParseDCID(payload)
		if !bytes.Equal(dcid, odcid) {
			if _, ok := quic.DecryptInitial(odcid, payload); !ok {
				if _, own := quic.DecryptInitial(dcid, payload); own {
					c.forgetFlow(connKey, flow)
				}
				return ""
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
	return host
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

func dropsQUICFlow(set *config.SetConfig) bool {
	if set.Routing.Enabled && config.RoutingIsBlock(set.Routing.Mode) {
		return true
	}
	return set.UDP.Mode == config.UDPModeDrop || set.UDP.Mode == config.UDPModeReject
}

func (w *Worker) quicFlowSet(cfg *config.Config, matcher *sni.SuffixSet, connKey string, pkt *pktInfo, dport uint16, payload []byte) (*config.SetConfig, string) {
	host := w.quicFlows.Lookup(connKey, payload)
	if host == "" {
		return nil, ""
	}
	matched, set := matcher.MatchSNIWithSourceTLS(host, pkt.srcMac, 0x0304, pkt.ver)
	if !matched || !set.MatchesUDPDPort(dport) {
		return nil, ""
	}
	if escSet := w.escalatedSetFor(cfg, host, pkt.srcMac); escSet != nil {
		set = escSet
	}
	if !dropsQUICFlow(set) {
		return nil, ""
	}
	return set, host
}
