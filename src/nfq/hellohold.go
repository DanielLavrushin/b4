package nfq

import (
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/sock"
)

var (
	helloHoldMax       = 100 * time.Millisecond
	helloHoldLocalMax  = 5 * time.Millisecond
	helloReplayStagger = time.Millisecond
)

const (
	maxHeldHellos = 256
	maxHeldOnce   = 4096
	heldOnceTTL   = 30 * time.Second
)

type helloHold struct {
	id    uint32
	q     verdictSetter
	raw   []byte
	dst   net.IP
	ver   uint8
	key   string
	at    time.Time
	timer *time.Timer
	local bool
}

func (h *helloHold) verdict(v int) {
	if err := h.q.SetVerdict(h.id, v); err != nil {
		log.Tracef("failed to set verdict on held packet %d: %v", h.id, err)
	}
}

func (h *helloHold) report(how string) {
	log.Tracef("Split ClientHello %s: segment 1 held %s, %s", h.key, time.Since(h.at).Round(10*time.Microsecond), how)
}

func (h *helloHold) settle(v int, how string) {
	h.verdict(v)
	h.report(how)
}

func actsOnTCPHello(set *config.SetConfig) bool {
	switch {
	case set.Routing.Enabled && config.RoutingIsBlock(set.Routing.Mode):
		return true
	case set.Escalate.Active():
		return true
	case set.RoutingDivertsPackets() && config.RoutingUsesTProxy(set.Routing.Mode):
		return false
	}
	return set.TCP.IPBlockDetect.Enabled || needsTCPInjection(set)
}

func (w *Worker) helloHoldCandidate(vc *verdictCtx, pkt *pktInfo, matcher *sni.SuffixSet, connKey string, tcp, payload []byte, dport uint16, matched bool) *helloHold {
	if matched || vc.q == nil || vc.copy != nil || tcp[13]&0x07 != 0 || !truncatedClientHello(payload) {
		return nil
	}
	record := 5 + (int(payload[3])<<8 | int(payload[4]))
	if record > maxPendingHelloRecord || record > 2*len(payload) {
		return nil
	}
	if !matcher.NameSetCoversTCPPort(dport) || w.holdsReleased() {
		return nil
	}
	if _, _, known := w.tlsCache.Lookup(connKey); known {
		return nil
	}
	return &helloHold{
		id:    vc.id,
		q:     vc.q,
		raw:   append([]byte(nil), pkt.raw...),
		dst:   append(net.IP(nil), pkt.dst...),
		ver:   pkt.ver,
		local: vc.routerOriginated(),
	}
}

func injectAcquireN(n int) bool {
	for i := 0; i < n; i++ {
		if !injectAcquire() {
			injectReleaseN(i)
			return false
		}
	}
	return true
}

func injectReleaseN(n int) {
	for i := 0; i < n; i++ {
		injectRelease()
	}
}

func stripSACK(raw []byte, ver uint8) []byte {
	if ver == IPv4 {
		return sock.StripSACKFromTCP(raw)
	}
	return sock.StripSACKFromTCPv6(raw)
}

func (w *Worker) sendCopies(raw []byte, dst net.IP, ver uint8, n int) {
	for i := 0; i < n; i++ {
		if ver == IPv4 {
			_ = w.sock.SendIPv4(raw, dst)
		} else {
			_ = w.sock.SendIPv6(raw, dst)
		}
	}
}

func (w *Worker) injectHeldThen(set *config.SetConfig, held *helloHold, cur []byte, dst net.IP, ver uint8) {
	inject := w.dropAndInjectTCP
	if ver != IPv4 {
		inject = w.dropAndInjectTCPv6
	}
	if held == nil {
		w.wg.Add(1)
		go func() {
			defer func() {
				injectRelease()
				w.wg.Done()
			}()
			inject(set, cur, dst)
		}()
		return
	}
	started := make(chan struct{})
	stagger := helloReplayStagger
	w.wg.Add(2)
	go func() {
		defer func() {
			injectRelease()
			w.wg.Done()
		}()
		close(started)
		inject(set, held.raw, held.dst)
	}()
	go func() {
		defer func() {
			injectRelease()
			w.wg.Done()
		}()
		<-started
		time.Sleep(stagger)
		inject(set, cur, dst)
	}()
}
