package nfq

import (
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
)

const (
	maxVoiceFlowEntries = 10000
	voiceFlowIdle       = 60 * time.Second
)

type voiceFlow struct {
	setID    string
	lastSeen time.Time
}

type voiceFlowCache struct {
	mu    sync.RWMutex
	flows map[string]*voiceFlow
}

func newVoiceFlowCache() *voiceFlowCache {
	return &voiceFlowCache{flows: make(map[string]*voiceFlow)}
}

func (c *voiceFlowCache) Store(connKey, setID string) {
	if c == nil || setID == "" {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.flows[connKey]; !exists && len(c.flows) >= maxVoiceFlowEntries {
		c.evictLocked(now)
	}
	c.flows[connKey] = &voiceFlow{setID: setID, lastSeen: now}
}

func (c *voiceFlowCache) Lookup(connKey string) string {
	if c == nil {
		return ""
	}
	now := time.Now()
	c.mu.RLock()
	flow := c.flows[connKey]
	var setID string
	var idle time.Duration
	if flow != nil {
		setID, idle = flow.setID, now.Sub(flow.lastSeen)
	}
	c.mu.RUnlock()
	if flow == nil || idle > voiceFlowIdle {
		return ""
	}
	if idle > time.Second {
		c.mu.Lock()
		if current, ok := c.flows[connKey]; ok && current == flow {
			current.lastSeen = now
		}
		c.mu.Unlock()
	}
	return setID
}

func (c *voiceFlowCache) evictLocked(now time.Time) {
	var oldestKey string
	var oldestAt time.Time
	for k, v := range c.flows {
		if now.Sub(v.lastSeen) > voiceFlowIdle {
			delete(c.flows, k)
		} else if oldestKey == "" || v.lastSeen.Before(oldestAt) {
			oldestKey = k
			oldestAt = v.lastSeen
		}
	}
	if len(c.flows) >= maxVoiceFlowEntries && oldestKey != "" {
		delete(c.flows, oldestKey)
	}
}

func (c *voiceFlowCache) Cleanup() {
	if c == nil {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.flows {
		if now.Sub(v.lastSeen) > voiceFlowIdle {
			delete(c.flows, k)
		}
	}
}

func (w *Worker) voiceFlowSet(matcher *sni.SuffixSet, connKey string, pkt *pktInfo, dport uint16, keep sni.SetFilter) *config.SetConfig {
	if !matcher.SelectsUDPL7() {
		return nil
	}
	return matcher.UDPL7Owner(w.voiceFlows.Lookup(connKey), dport, pkt.srcMac, pkt.ver, keep)
}

func (w *Worker) passVoiceFlow(vc *verdictCtx, pkt *pktInfo, cfg *config.Config, set *config.SetConfig, sport, dport uint16) int {
	blocks := routingBlocks(set)
	reason := voiceExemptReason
	if blocks {
		reason = ""
	}
	if !cfg.Queue.IsDiscovery {
		log.LogConnection("UDP", "", "", pkt.srcStr, sport, set.Name, pkt.dstStr, dport, pkt.srcMac, "", reason)
	}
	if !blocks {
		return vc.accept()
	}
	observeFlow(cfg, pkt, sport, dport, set, "")
	return w.blockUDP(vc, pkt, cfg, set, sport, dport, "", "", set.Name, "", false)
}
