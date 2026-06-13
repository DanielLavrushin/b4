package log

import (
	"fmt"
	"sync"
)

const discoveryRingSize = 1000

var (
	discoveryHub     *DiscoveryLogHub
	discoveryHubOnce sync.Once
)

type DiscoveryLogHub struct {
	mu        sync.RWMutex
	listeners []chan string
	ring      [discoveryRingSize]string
	ringHead  int
	ringFull  bool
}

func GetDiscoveryHub() *DiscoveryLogHub {
	discoveryHubOnce.Do(func() {
		discoveryHub = &DiscoveryLogHub{
			listeners: make([]chan string, 0),
		}
	})
	return discoveryHub
}

func (h *DiscoveryLogHub) Subscribe() (chan string, []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan string, 256)
	h.listeners = append(h.listeners, ch)

	var snap []string
	if h.ringFull {
		snap = make([]string, 0, discoveryRingSize)
		snap = append(snap, h.ring[h.ringHead:]...)
		snap = append(snap, h.ring[:h.ringHead]...)
	} else {
		snap = make([]string, h.ringHead)
		copy(snap, h.ring[:h.ringHead])
	}
	return ch, snap
}

func (h *DiscoveryLogHub) Unsubscribe(ch chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, l := range h.listeners {
		if l == ch {
			h.listeners = append(h.listeners[:i], h.listeners[i+1:]...)
			return
		}
	}
}

func (h *DiscoveryLogHub) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ring = [discoveryRingSize]string{}
	h.ringHead = 0
	h.ringFull = false
}

func (h *DiscoveryLogHub) Broadcast(msg string) {
	h.mu.Lock()
	h.ring[h.ringHead] = msg
	h.ringHead++
	if h.ringHead >= discoveryRingSize {
		h.ringHead = 0
		h.ringFull = true
	}
	listeners := make([]chan string, len(h.listeners))
	copy(listeners, h.listeners)
	h.mu.Unlock()

	for _, ch := range listeners {
		select {
		case ch <- msg:
		default:
		}
	}
}

func DiscoveryLogf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	GetDiscoveryHub().Broadcast(msg)
	Infof("[DISCOVERY] %s", msg)
}
