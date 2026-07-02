package log

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const connectionTimestampLayout = "2006/01/02 15:04:05.000000"

const connectionRingSize = 500

type ConnectionHub struct {
	mu        sync.RWMutex
	listeners []chan string
	ring      [connectionRingSize]string
	ringHead  int
	ringFull  bool
}

var (
	connectionHub     *ConnectionHub
	connectionHubOnce sync.Once
)

func GetConnectionHub() *ConnectionHub {
	connectionHubOnce.Do(func() {
		connectionHub = &ConnectionHub{}
	})
	return connectionHub
}

func (h *ConnectionHub) Subscribe() (chan string, []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan string, 256)
	h.listeners = append(h.listeners, ch)

	var snap []string
	if h.ringFull {
		snap = make([]string, 0, connectionRingSize)
		snap = append(snap, h.ring[h.ringHead:]...)
		snap = append(snap, h.ring[:h.ringHead]...)
	} else {
		snap = make([]string, h.ringHead)
		copy(snap, h.ring[:h.ringHead])
	}
	return ch, snap
}

func (h *ConnectionHub) Unsubscribe(ch chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, l := range h.listeners {
		if l == ch {
			h.listeners = append(h.listeners[:i], h.listeners[i+1:]...)
			return
		}
	}
}

func (h *ConnectionHub) Broadcast(msg string) {
	h.mu.Lock()
	h.ring[h.ringHead] = msg
	h.ringHead++
	if h.ringHead >= connectionRingSize {
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

func LogConnection(protocol, sniSet, domain, srcIP string, srcPort uint16, ipSet, dstIP string, dstPort uint16, srcMac, tlsVersion, metadata string) {
	source := net.JoinHostPort(srcIP, strconv.Itoa(int(srcPort)))
	destination := net.JoinHostPort(dstIP, strconv.Itoa(int(dstPort)))
	emitConnection(protocol, sniSet, domain, source, ipSet, destination, srcMac, tlsVersion, metadata)
}

func LogConnectionStr(protocol, sniSet, domain, source, ipSet, destination, srcMac, tlsVersion, metadata string) {
	emitConnection(protocol, sniSet, domain, source, ipSet, destination, srcMac, tlsVersion, metadata)
}

func formatConnectionCSV(protocol, sniSet, domain, source, ipSet, destination, srcMac, tlsVersion, metadata string) string {
	if metadata != "" {
		return fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s,%s",
			protocol, sniSet, domain, source, ipSet, destination, srcMac, tlsVersion, metadata)
	}
	return fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s",
		protocol, sniSet, domain, source, ipSet, destination, srcMac, tlsVersion)
}

func formatConnectionHuman(protocol, sniSet, domain, source, ipSet, destination, srcMac, tlsVersion, metadata string) string {
	var b strings.Builder
	b.Grow(96)
	if protocol == "" {
		protocol = "?"
	}
	b.WriteString(protocol)
	b.WriteByte(' ')
	b.WriteString(source)
	b.WriteString(" → ")
	b.WriteString(destination)
	if domain != "" {
		b.WriteByte(' ')
		b.WriteString(domain)
	}
	if tlsVersion != "" {
		b.WriteString(" tls=")
		b.WriteString(tlsVersion)
	}
	if sniSet != "" {
		b.WriteString(" sni-set=")
		b.WriteString(sniSet)
	}
	if ipSet != "" {
		b.WriteString(" ip-set=")
		b.WriteString(ipSet)
	}
	if srcMac != "" {
		b.WriteString(" mac=")
		b.WriteString(srcMac)
	}
	if metadata != "" {
		b.WriteString(" [")
		b.WriteString(metadata)
		b.WriteByte(']')
	}
	return b.String()
}

func sanitizeConnField(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ',' || r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

func emitConnection(protocol, sniSet, domain, source, ipSet, destination, srcMac, tlsVersion, metadata string) {
	sniSet = sanitizeConnField(sniSet)
	domain = sanitizeConnField(domain)
	ipSet = sanitizeConnField(ipSet)
	srcMac = sanitizeConnField(srcMac)
	metadata = sanitizeConnField(metadata)
	csv := formatConnectionCSV(protocol, sniSet, domain, source, ipSet, destination, srcMac, tlsVersion, metadata)
	GetConnectionHub().Broadcast(time.Now().Format(connectionTimestampLayout) + "," + csv)

	if Level(CurLevel.Load()) >= LevelInfo {
		Infof("%s", formatConnectionHuman(protocol, sniSet, domain, source, ipSet, destination, srcMac, tlsVersion, metadata))
	}
}

func FormatHostPort(ip string, port uint16) string {
	return net.JoinHostPort(ip, strconv.Itoa(int(port)))
}
