package nfq

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discord"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/quic"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/stun"
	"github.com/daniellavrushin/b4/utils"
	"github.com/florianl/go-nfqueue"
)

type pktInfo struct {
	raw    []byte
	ver    uint8
	proto  uint8
	addr   [32]byte
	src    net.IP
	dst    net.IP
	srcStr string
	dstStr string
	srcMac string
	ihl    int
}

func selfInjectedMark(m, queueMark uint32, cfg *config.Config) bool {
	if queueMark == 0 {
		return false
	}
	if m == queueMark {
		return true
	}
	if cfg != nil {
		if m == uint32(cfg.DiscoveryFlowMark()) || m == uint32(cfg.DiscoveryInjectedMark()) {
			return false
		}
	}
	if m&queueMark != queueMark {
		return false
	}
	return m&^(queueMark|config.PerSetRouteMarkBits) == 0
}

func (w *Worker) handlePacket(q *nfqueue.Nfqueue, a nfqueue.Attribute, mark uint) int {
	if a.PacketID == nil || a.Payload == nil || len(*a.Payload) == 0 {
		if a.PacketID != nil && q != nil {
			if err := q.SetVerdict(*a.PacketID, nfqueue.NfAccept); err != nil {
				log.Tracef("failed to set verdict on invalid packet %d: %v", *a.PacketID, err)
			}
		}
		return 0
	}

	vc := &verdictCtx{id: *a.PacketID, q: q}

	if a.Mark != nil && selfInjectedMark(*a.Mark, uint32(mark), w.getConfig()) {
		return vc.accept()
	}

	if !w.matchesInterface(a) {
		return vc.accept()
	}

	select {
	case <-w.ctx.Done():
		return 0
	default:
	}

	return w.dispatch(vc, *a.Payload)
}

func (w *Worker) dispatch(vc *verdictCtx, raw []byte) int {
	cfg := w.getConfig()
	matcher := w.getMatcher()

	atomic.AddUint64(&w.packetsProcessed, 1)

	pkt, ok := w.parseIPHeaders(raw)
	if !ok {
		return vc.accept()
	}

	matched, st := matcher.MatchIPWithSource(pkt.dst, pkt.srcMac)
	var set *config.SetConfig
	if matched {
		set = st
	}

	switch pkt.proto {
	case 6:
		if len(pkt.raw) >= pkt.ihl+TCPHeaderMinLen {
			return w.handleTCPPacket(vc, pkt, cfg, matcher, matched, set, st)
		}
	case 17:
		if len(pkt.raw) >= pkt.ihl+UDPHeaderLen {
			return w.handleUDPPacket(vc, pkt, cfg, matcher, matched, set, st)
		}
	}

	return vc.accept()
}

const maxInjectInflight = 512

var (
	injectInflight   = make(chan struct{}, maxInjectInflight)
	injectOverloaded atomic.Uint64
	injectLastLog    atomic.Int64
)

func injectAcquire() bool {
	select {
	case injectInflight <- struct{}{}:
		return true
	default:
	}
	n := injectOverloaded.Add(1)
	now := time.Now().Unix()
	last := injectLastLog.Load()
	if now-last >= 10 && injectLastLog.CompareAndSwap(last, now) {
		log.Warnf("Packet injection backlog is full (%d in flight), %d packets have gone through unmodified so far", maxInjectInflight, n)
	}
	return false
}

func injectRelease() {
	<-injectInflight
}

func InjectInflight() int {
	return len(injectInflight)
}

func InjectOverloaded() uint64 {
	return injectOverloaded.Load()
}

func ResetInjectOverloaded() {
	injectOverloaded.Store(0)
}

func needsTCPInjection(set *config.SetConfig) bool {
	if set == nil {
		return false
	}

	return set.TCP.DropSACK ||
		set.Faking.SNI ||
		set.Faking.SNIMutation.Mode != config.ConfigOff ||
		set.TCP.Desync.Mode != config.ConfigOff ||
		set.TCP.Desync.PostDesync ||
		set.TCP.Win.Mode != config.ConfigOff ||
		set.Fragmentation.Strategy != config.ConfigNone ||
		len(set.Fragmentation.StrategyPool) > 0
}

func needsTCPSynInjection(set *config.SetConfig) bool {
	if set == nil {
		return false
	}

	hasActiveStrategy := set.Fragmentation.Strategy != config.ConfigNone || len(set.Fragmentation.StrategyPool) > 0
	return set.TCP.SynFake || (hasActiveStrategy && set.Faking.TCPMD5)
}

func needsPayloadlessInjection(set *config.SetConfig) bool {
	if set == nil {
		return false
	}

	return set.TCP.DropSACK
}

func (w *Worker) parseIPHeaders(raw []byte) (*pktInfo, bool) {
	v := raw[0] >> 4
	if v != IPv4 && v != IPv6 {
		return nil, false
	}

	p := &pktInfo{raw: raw, ver: v}

	if v == IPv4 {
		if len(raw) < IPv4HeaderMinLen {
			return nil, false
		}
		ihl := int(raw[0]&0x0f) * 4
		if len(raw) < ihl {
			return nil, false
		}

		fragOffset := binary.BigEndian.Uint16(raw[6:8]) & 0x1FFF
		moreFragments := (binary.BigEndian.Uint16(raw[6:8]) & 0x2000) != 0
		if fragOffset != 0 || moreFragments {
			return nil, false
		}

		p.proto = raw[9]
		copy(p.addr[0:4], raw[12:16])
		copy(p.addr[16:20], raw[16:20])
		p.src = net.IP(p.addr[0:4])
		p.dst = net.IP(p.addr[16:20])
		p.ihl = ihl
	} else {
		offset, nextHeader, ok := upperLayerOffsetV6(raw)
		if !ok {
			return nil, false
		}
		p.proto = nextHeader
		p.ihl = offset
		copy(p.addr[0:16], raw[8:24])
		copy(p.addr[16:32], raw[24:40])
		p.src = net.IP(p.addr[0:16])
		p.dst = net.IP(p.addr[16:32])
	}

	if p.src.IsLoopback() || p.dst.IsLoopback() {
		return nil, false
	}

	if w.srcResolver != nil && v == IPv4 && (p.proto == 6 || p.proto == 17) && len(raw) >= p.ihl+4 {
		sport := uint16(raw[p.ihl])<<8 | uint16(raw[p.ihl+1])
		dport := uint16(raw[p.ihl+2])<<8 | uint16(raw[p.ihl+3])
		if lan, ok := w.srcResolver.resolve(p.proto, p.src, sport, p.dst, dport); ok {
			if v4 := lan.To4(); v4 != nil {
				copy(p.addr[0:4], v4)
			}
		}
	}

	p.srcStr = p.src.String()
	p.dstStr = p.dst.String()
	p.srcMac = w.getMacByIp(p.srcStr)

	return p, true
}

func (w *Worker) handleTCPPacket(vc *verdictCtx, pkt *pktInfo, cfg *config.Config, matcher *sni.SuffixSet, matched bool, set *config.SetConfig, st *config.SetConfig) int {
	tcp := pkt.raw[pkt.ihl:]
	if len(tcp) < TCPHeaderMinLen {
		return vc.accept()
	}
	datOff := int((tcp[12]>>4)&0x0f) * 4
	if len(tcp) < datOff {
		return vc.accept()
	}
	payload := tcp[datOff:]
	sport := binary.BigEndian.Uint16(tcp[0:2])
	dport := binary.BigEndian.Uint16(tcp[2:4])

	if cfg.IsTCPPort(sport) {
		return w.HandleIncoming(vc, pkt.ver, pkt.raw, pkt.ihl, pkt.src, pkt.dstStr, dport, pkt.srcStr, sport, payload)
	}

	if matched && !set.MatchesTCPDPort(dport) {
		matched = false
		set = nil
		st = nil
	}

	matchedHint := false
	hintHost := ""
	if !matched && cfg.IsTCPPort(dport) {
		if hintSet, hinted := w.lookupHostHint(cfg, pkt.srcStr, pkt.dstStr, pkt.srcMac); hintSet != nil {
			if hintSet.MatchesTCPDPort(dport) {
				matched = true
				set = hintSet
				matchedHint = true
				hintHost = hinted
			}
		}
	}

	matchedLearned := false
	learnedHost := ""
	if !matchedHint {
		if mLearned, learnedSet, learnedDomain := matcher.MatchLearnedIPWithSource(pkt.dst, pkt.srcMac); mLearned {
			if learnedSet.MatchesTCPDPort(dport) {
				matched = true
				set = learnedSet
				st = learnedSet
				matchedLearned = true
				learnedHost = learnedDomain
			}
		}
	}

	if !matched && cfg.IsTCPPort(dport) {
		if portMatched, portSet := matcher.MatchTCPPort(dport, pkt.srcMac); portMatched {
			matched = true
			set = portSet
		}
	}

	earlyHost := hintHost
	if earlyHost == "" {
		earlyHost = learnedHost
	}
	if matched && earlyHost != "" && cfg.IsTCPPort(dport) {
		if escSet := w.escalatedSetFor(cfg, earlyHost, pkt.srcMac); escSet != nil && escSet != set {
			log.Tracef("escalation hit for %s before the ClientHello: %s -> %s", earlyHost, set.Name, escSet.Name)
			set = escSet
			if st != nil {
				st = escSet
			}
		}
	}

	routeTProxy := matched && set != nil && set.RoutingDivertsPackets() && config.RoutingUsesTProxy(set.Routing.Mode)
	routeIfaceHandoff := matched && set != nil && !routeTProxy && set.RoutingHandsOffPackets()
	routeHandsOff := routeTProxy || routeIfaceHandoff

	if matched && !routeHandsOff && cfg.IsTCPPort(dport) && set.TCP.Duplicate.Enabled && set.TCP.Duplicate.Count > 0 {
		log.Tracef("TCP duplicate to %s:%d (%d copies, set: %s)", pkt.dstStr, dport, set.TCP.Duplicate.Count, set.Name)

		dupConnKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)
		dupHost, dupTLS, _ := w.tlsCache.Lookup(dupConnKey)

		m := metrics.GetMetricsCollector()
		m.RecordConnection("TCP-DUP", dupHost, pkt.srcStr, pkt.dstStr, true, pkt.srcMac, set.Name, config.TLSVersionString(dupTLS))
		m.RecordPacket(uint64(len(pkt.raw)))

		if !cfg.Queue.IsDiscovery {
			log.LogConnection("TCP", "", dupHost, pkt.srcStr, sport, set.Name, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(dupTLS), "tcp-dup")
		}

		if !vc.drop() {
			return 0
		}

		for i := 0; i < set.TCP.Duplicate.Count; i++ {
			if pkt.ver == IPv4 {
				_ = w.sock.SendIPv4(pkt.raw, pkt.dst)
			} else {
				_ = w.sock.SendIPv6(pkt.raw, pkt.dst)
			}
		}
		return 0
	}

	tcpFlags := tcp[13]
	isSyn := (tcpFlags & 0x02) != 0
	isAck := (tcpFlags & 0x10) != 0
	isRst := (tcpFlags & 0x04) != 0
	if isRst && cfg.IsTCPPort(dport) {
		log.Tracef("RST to %s:%d", pkt.dstStr, dport)
		if matched && set != nil && set.TCP.RSTProtection.Enabled {
			connKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)
			if w.connTracker.ShouldDropOutboundRST(connKey) {
				log.Warnf("RST protection: dropped outbound RST to %s:%d — connection not established", pkt.dstStr, dport)
				metrics.GetMetricsCollector().RecordRSTDrop()
				vc.drop()
				return 0
			}
		}
	}

	if isAck && !isSyn && !isRst && cfg.IsTCPPort(dport) && matched && set != nil && set.TCP.RSTProtection.Enabled {
		connKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)
		w.connTracker.MarkEstablished(connKey)
	}

	if isSyn && !isAck && !routeHandsOff && cfg.IsTCPPort(dport) && matched {
		if w.handleSynHealth(vc, pkt, cfg, set, sport, dport) {
			return 0
		}
	}

	if isSyn && !isAck && !routeHandsOff && cfg.IsTCPPort(dport) && matched && !set.TCP.Duplicate.Enabled && needsTCPSynInjection(set) {
		log.Tracef("TCP SYN to %s:%d (set: %s)", pkt.dstStr, dport, set.Name)

		m := metrics.GetMetricsCollector()
		m.RecordConnection("TCP-SYN", "", pkt.srcStr, pkt.dstStr, true, pkt.srcMac, set.Name, "")

		if pkt.ver == IPv4 {
			if set.TCP.SynFake {
				w.sendFakeSyn(set, pkt.raw, pkt.ihl, datOff)
			}
			if set.Faking.TCPMD5 {
				w.sendFakeSynWithMD5(set, pkt.raw, pkt.ihl, pkt.dst)
			}
			_ = w.sock.SendIPv4(pkt.raw, pkt.dst)
		} else {
			if set.TCP.SynFake {
				w.sendFakeSynV6(set, pkt.raw, pkt.ihl, datOff)
			}
			if set.Faking.TCPMD5 {
				w.sendFakeSynWithMD5V6(set, pkt.raw, pkt.dst)
			}
			_ = w.sock.SendIPv6(pkt.raw, pkt.dst)
		}

		if set.TCP.Incoming.Mode != config.ConfigOff || set.TCP.RSTProtection.Enabled || set.Escalate.Active() {
			connKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)
			w.connTracker.RegisterOutgoing(connKey, set)
		}

		vc.drop()
		return 0
	}

	host := ""
	isClientHello := false
	var tlsVersion uint16
	matchedIP := st != nil
	matchedSNI := false
	ipTarget := ""
	sniTarget := ""
	classifyReason := ""

	if !matchedIP && matched && set != nil {
		ipTarget = set.Name
	}

	clearMatch := func() {
		matched = false
		set = nil
		st = nil
		matchedIP = false
		ipTarget = ""
	}

	if cfg.IsTCPPort(dport) && len(payload) > 0 {
		log.Tracef("TCP payload to %s: len=%d, first5=%x", pkt.dstStr, len(payload), payload[:min(5, len(payload))])
		if len(payload) >= 5 && payload[0] == 0x16 {
			log.Tracef("TLS record: type=%x ver=%x%x len=%d", payload[0], payload[1], payload[2],
				int(payload[3])<<8|int(payload[4]))
		}
		connKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)

		host, tlsVersion, _ = sni.ParseTLSClientHelloSNI(payload)

		if host == "" {
			seq := binary.BigEndian.Uint32(tcp[4:8])
			if joined, prefix, ok := w.pendingHello.Feed(connKey, seq, payload); ok {
				if joinedHost, joinedTLS, _ := sni.ParseTLSClientHelloSNI(joined); joinedHost != "" {
					host = joinedHost
					tlsVersion = joinedTLS
					classifyReason = "split-hello"
					w.pendingHello.Drop(connKey)
					log.Tracef("recovered SNI %q for %s:%d from split ClientHello (%d buffered + %d bytes)",
						host, pkt.dstStr, dport, prefix, len(payload))
				}
			}
		}

		isClientHello = host != ""

		if host != "" && tlsVersion != 0 {
			w.tlsCache.Store(connKey, host, tlsVersion)
		}

		if captureManager := capture.GetManager(cfg); captureManager != nil {
			captureManager.CapturePayload(connKey, host, "tls", payload)
		}

		if host != "" {
			if mSNI, stSNI := matcher.MatchSNIWithSourceTLS(host, pkt.srcMac, tlsVersion, pkt.ver); mSNI {
				if stSNI.MatchesTCPDPort(dport) {
					matchedSNI = true
					matched = true
					set = stSNI
					matcher.LearnIPToDomain(pkt.dst, host, stSNI)
					registerLearnedRoute(cfg, stSNI, pkt.dst, host)
				}
			}
		}

		if matched && !matchedSNI && set != nil && !set.MatchesTLSVersion(tlsVersion) {
			clearMatch()
		}

		if matchedLearned && !matchedSNI && !(len(payload) >= 1 && payload[0] == 0x16) {
			if set != nil && set.Fragmentation.Strategy == config.ConfigNone && len(set.Fragmentation.StrategyPool) == 0 && set.TCP.Desync.Mode == config.ConfigOff {
				clearMatch()
			}
		}

		if matchedHint && !matchedSNI && isClientHello && host != "" {
			log.Tracef("host hint for %s dropped: %s carries a clear SNI that matches no set", pkt.dstStr, host)
			clearMatch()
			matchedHint = false
			hintHost = ""
		}
	}

	if host == "" || tlsVersion == 0 {
		connKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)
		if cachedHost, cachedTLS, found := w.tlsCache.Lookup(connKey); found {
			if host == "" {
				host = cachedHost
			}
			if tlsVersion == 0 {
				tlsVersion = cachedTLS
			}
		}
	}

	if host == "" && hintHost != "" {
		host = hintHost
	}
	if matchedHint && !matchedSNI && classifyReason == "" {
		classifyReason = "dns-hint"
	}

	if matchedSNI {
		sniTarget = set.Name
	} else if matchedIP {
		ipTarget = st.Name
	}

	if matched && host != "" && cfg.IsTCPPort(dport) {
		if escSet := w.escalatedSetFor(cfg, host, pkt.srcMac); escSet != nil {
			if escSet != set {
				log.Tracef("escalation hit for %s: %s -> %s", host, set.Name, escSet.Name)
				set = escSet
			}
			if sniTarget != "" {
				sniTarget = set.Name
			}
			if ipTarget != "" {
				ipTarget = set.Name
			}
			w.refreshEscalatedRoute(cfg, escSet, host, pkt.dst)
		}
	}

	routeTProxy = matched && set != nil && set.RoutingDivertsPackets() && config.RoutingUsesTProxy(set.Routing.Mode)
	routeIfaceHandoff = matched && set != nil && !routeTProxy && set.RoutingHandsOffPackets()
	routeHandsOff = routeTProxy || routeIfaceHandoff

	if routeIfaceHandoff && classifyReason == "" {
		classifyReason = "routed->" + set.Routing.EgressInterface
	}

	if matched && isClientHello && !routeHandsOff && set.TCP.IPBlockDetect.Enabled && host != "" && cfg.IsTCPPort(dport) {
		ibd := &set.TCP.IPBlockDetect
		dstIPPort := fmt.Sprintf("%s:%d", pkt.dstStr, dport)

		if ibd.CacheBlockedIPs && w.destState.IsBlocked(dstIPPort) {
			if !cfg.Queue.IsDiscovery {
				log.LogConnection("TCP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(tlsVersion), "ipblock-cached")
			}
			if pkt.ver == IPv4 {
				w.sendRSTToClientV4(pkt.raw, pkt.ihl, pkt.src, pkt.dst)
			} else {
				w.sendRSTToClientV6(pkt.raw, pkt.src, pkt.dst)
			}

			m := metrics.GetMetricsCollector()
			m.RecordConnection("TCP", host, pkt.srcStr, pkt.dstStr, true, pkt.srcMac, set.Name, config.TLSVersionString(tlsVersion))
			m.RecordPacket(uint64(len(pkt.raw)))
			vc.drop()
			log.Tracef("IPBlockDetect: dropped packet to %s:%d (cached)", pkt.dstStr, dport)

			return 0
		}
	}

	if !cfg.Queue.IsDiscovery {
		log.LogConnection("TCP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(tlsVersion), classifyReason)
	}

	{
		m := metrics.GetMetricsCollector()
		setName := ""
		if matched {
			setName = set.Name
		}
		m.RecordConnection("TCP", host, pkt.srcStr, pkt.dstStr, matched, pkt.srcMac, setName, config.TLSVersionString(tlsVersion))
		m.RecordPacket(uint64(len(pkt.raw)))
	}

	stallCount := 0
	stallFirstSeen := time.Time{}
	stallTracked := false
	if matched && isClientHello && host != "" && cfg.IsTCPPort(dport) &&
		(set.TCP.IPBlockDetect.Enabled || set.Escalate.Active()) {
		stallKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)
		stallCount, stallFirstSeen = w.destState.RecordClientHello(stallKey, host)
		stallTracked = true
	}

	if stallTracked && escalateOnStall(&set.Escalate, stallCount, stallFirstSeen) {
		if next := w.tryEscalate(cfg, set, host, pkt.srcMac, pkt.dst, escalateReasonStall); next != nil {
			if !cfg.Queue.IsDiscovery {
				log.LogConnection("TCP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(tlsVersion), "escalate->"+next.Name)
			}
			vc.drop()
			return 0
		}
	}

	if matched && set != nil && set.Routing.Enabled && config.RoutingIsBlock(set.Routing.Mode) {
		if matchedSNI || (matchedIP && !matchedLearned) {
			if config.NormalizeBlockAction(set.Routing.BlockAction) != config.BlockActionDrop {
				if pkt.ver == IPv4 {
					w.sendRSTToClientV4(pkt.raw, pkt.ihl, pkt.src, pkt.dst)
				} else {
					w.sendRSTToClientV6(pkt.raw, pkt.src, pkt.dst)
				}
				log.Tracef("BLACKHOLE: sent RST to %s:%d (set: %s)", pkt.dstStr, dport, set.Name)
			}
			if !cfg.Queue.IsDiscovery {
				log.LogConnection("TCP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(tlsVersion), "block")
				blockedTarget := host
				if blockedTarget == "" {
					blockedTarget = pkt.dstStr
				}
				metrics.GetMetricsCollector().RecordBlock(blockedTarget, pkt.srcMac)
			}
			vc.drop()
			return 0
		}
		return vc.accept()
	}

	if matched && set != nil && set.RoutingDivertsPackets() && config.RoutingUsesTProxy(set.Routing.Mode) {
		return vc.accept()
	}

	if matched {
		if stallTracked && set.TCP.IPBlockDetect.Enabled && !routeHandsOff {
			ibd := &set.TCP.IPBlockDetect
			dstIPPort := fmt.Sprintf("%s:%d", pkt.dstStr, dport)
			ibConnKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)

			threshold := ibd.RetransmitThreshold
			if threshold <= 0 {
				threshold = 3
			}
			timeout := time.Duration(ibd.TimeoutMs) * time.Millisecond
			if timeout <= 0 {
				timeout = 3000 * time.Millisecond
			}

			if stallCount >= threshold || (stallCount > 1 && time.Since(stallFirstSeen) > timeout) {
				if !w.destState.HasRSTSent(ibConnKey) {
					w.destState.MarkRSTSent(ibConnKey)
					if pkt.ver == IPv4 {
						w.sendRSTToClientV4(pkt.raw, pkt.ihl, pkt.src, pkt.dst)
					} else {
						w.sendRSTToClientV6(pkt.raw, pkt.src, pkt.dst)
					}
					if ibd.CacheBlockedIPs {
						w.destState.AddBlocked(dstIPPort)
					}
					if !cfg.Queue.IsDiscovery {
						log.LogConnection("TCP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(tlsVersion), "ipblock")
					}
					m := metrics.GetMetricsCollector()
					m.RecordConnection("TCP", host, pkt.srcStr, pkt.dstStr, true, pkt.srcMac, set.Name, config.TLSVersionString(tlsVersion))
				}
				vc.drop()
				return 0
			}
		}

		if set.TCP.Incoming.Mode != config.ConfigOff || set.TCP.RSTProtection.Enabled || set.Escalate.Active() {
			connKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)
			w.connTracker.RegisterOutgoing(connKey, set)
		}

		if routeHandsOff || !needsTCPInjection(set) {
			return vc.accept()
		}

		if len(payload) == 0 && !needsPayloadlessInjection(set) {
			return vc.accept()
		}

		if !injectAcquire() {
			return vc.accept()
		}

		packetCopy := make([]byte, len(pkt.raw))
		copy(packetCopy, pkt.raw)

		if set.TCP.DropSACK {
			if pkt.ver == 4 {
				packetCopy = sock.StripSACKFromTCP(packetCopy)
			} else {
				packetCopy = sock.StripSACKFromTCPv6(packetCopy)
			}
		}

		dstCopy := make(net.IP, len(pkt.dst))
		copy(dstCopy, pkt.dst)
		setCopy := set

		if !vc.drop() {
			injectRelease()
			return 0
		}

		v := pkt.ver
		w.wg.Add(1)
		go func(s *config.SetConfig, pktData []byte, d net.IP) {
			defer func() {
				injectRelease()
				w.wg.Done()
			}()
			if v == 4 {
				w.dropAndInjectTCP(s, pktData, d)
			} else {
				w.dropAndInjectTCPv6(s, pktData, d)
			}
		}(setCopy, packetCopy, dstCopy)
		return 0
	}

	return vc.accept()
}

func (w *Worker) handleUDPPacket(vc *verdictCtx, pkt *pktInfo, cfg *config.Config, matcher *sni.SuffixSet, matched bool, set *config.SetConfig, st *config.SetConfig) int {
	udp := pkt.raw[pkt.ihl:]
	if len(udp) < UDPHeaderLen {
		return vc.accept()
	}

	payload := udp[8:]
	sport := binary.BigEndian.Uint16(udp[0:2])
	dport := binary.BigEndian.Uint16(udp[2:4])
	connKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)

	if sport == 53 || dport == 53 {
		return w.processDnsPacket(vc, pkt, sport, dport, payload)
	}

	if utils.IsPrivateIP(pkt.dst) {
		return vc.accept()
	}

	matchedIP := st != nil
	matchedQUIC := false
	matchedLearned := false
	isVoiceMedia := false
	host := ""
	ipTarget := ""
	sniTarget := ""

	if matchedIP && !st.MatchesUDPDPort(dport) {
		matchedIP = false
		matched = false
		set = nil
	}

	if matchedIP {
		ipTarget = st.Name
	}

	if !matchedIP {
		if mLearned, learnedSet, learnedDomain := matcher.MatchLearnedIPWithSource(pkt.dst, pkt.srcMac); mLearned {
			if learnedSet.MatchesUDPDPort(dport) {
				matchedIP = true
				matchedLearned = true
				matched = true
				set = learnedSet
				host = learnedDomain
				sniTarget = learnedSet.Name
				ipTarget = learnedSet.Name
			}
		}
	}

	matchedPort := false
	if !matched {
		if portMatched, portSet := matcher.MatchUDPPort(dport, pkt.srcMac); portMatched {
			matchedPort = true
			matched = true
			set = portSet
			ipTarget = portSet.Name
		}
	}

	isVoiceMedia = stun.IsSTUNMessage(payload) || discord.IsVoicePacket(payload)

	isQUIC := quic.LooksLikeQUIC(payload)

	if host == "" && isQUIC {
		if h, ok := sni.ParseQUICClientHelloSNI(payload); ok {
			host = h
		}
	}

	if host != "" {
		if mSNI, sniSet := matcher.MatchSNIWithSourceTLS(host, pkt.srcMac, 0x0304, pkt.ver); mSNI {
			if sniSet.MatchesUDPDPort(dport) {
				matchedQUIC = true
				set = sniSet
				sniTarget = sniSet.Name
				matcher.LearnIPToDomain(pkt.dst, host, sniSet)
				registerLearnedRoute(cfg, sniSet, pkt.dst, host)
				w.storeHostHint(pkt.srcStr, pkt.dstStr, sniSet, host, "quic")
			}
		}
	}

	if !matchedQUIC && (matchedIP || matchedPort) && set.UDP.FilterQUIC == "all" {
		if isQUIC {
			matchedQUIC = true
		}
	}

	if captureManager := capture.GetManager(cfg); captureManager != nil {
		captureManager.CapturePayload(connKey, host, "quic", payload)
	}

	shouldHandle := (matchedIP || matchedQUIC || matchedPort) && !(isVoiceMedia && set.UDP.FilterSTUN)

	matched = shouldHandle

	udpTLS := ""
	if matchedQUIC || isQUIC {
		udpTLS = "1.3"
	}

	if shouldHandle && set != nil && host != "" {
		if escSet := w.escalatedSetFor(cfg, host, pkt.srcMac); escSet != nil {
			if escSet != set {
				log.Tracef("UDP escalation hit for %s: %s -> %s", host, set.Name, escSet.Name)
				set = escSet
			}
			if sniTarget != "" {
				sniTarget = set.Name
			}
			if ipTarget != "" {
				ipTarget = set.Name
			}
			w.refreshEscalatedRoute(cfg, escSet, host, pkt.dst)
		}
	}

	if !cfg.Queue.IsDiscovery {
		log.LogConnection("UDP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, udpTLS, "")
	}

	if isVoiceMedia && set != nil && set.UDP.FilterSTUN {
		return vc.accept()
	}

	if !shouldHandle {
		m := metrics.GetMetricsCollector()
		m.RecordConnection("UDP", host, pkt.srcStr, pkt.dstStr, false, pkt.srcMac, "", udpTLS)
		m.RecordPacket(uint64(len(pkt.raw)))
		return vc.accept()
	}

	m := metrics.GetMetricsCollector()
	setName := ""
	if matched {
		setName = set.Name
	}
	m.RecordConnection("UDP", host, pkt.srcStr, pkt.dstStr, matched, pkt.srcMac, setName, udpTLS)
	m.RecordPacket(uint64(len(pkt.raw)))

	if set.Routing.Enabled && config.RoutingIsBlock(set.Routing.Mode) {
		if matchedQUIC || (matchedIP && !matchedLearned) {
			if config.NormalizeBlockAction(set.Routing.BlockAction) != config.BlockActionDrop {
				if pkt.ver == IPv4 {
					if icmp := sock.BuildICMPv4Reject(w.rejectQuote(pkt), pkt.src.To4(), pkt.dst.To4()); icmp != nil {
						_ = w.clientSender().SendIPv4(icmp, pkt.src)
					}
				} else {
					if icmp := sock.BuildICMPv6Reject(pkt.raw, pkt.src.To16(), pkt.dst.To16()); icmp != nil {
						_ = w.clientSender().SendIPv6(icmp, pkt.src)
					}
				}
			}
			if !cfg.Queue.IsDiscovery {
				log.LogConnection("UDP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, udpTLS, "block")
				blockedTarget := host
				if blockedTarget == "" {
					blockedTarget = pkt.dstStr
				}
				metrics.GetMetricsCollector().RecordBlock(blockedTarget, pkt.srcMac)
			}
			vc.drop()
			return 0
		}
		return vc.accept()
	}

	switch set.UDP.Mode {
	case config.ConfigOff:
		return vc.accept()

	case "drop":
		vc.drop()
		return 0

	case "reject":
		if !vc.drop() {
			return 0
		}
		if pkt.ver == IPv4 {
			if icmp := sock.BuildICMPv4Reject(w.rejectQuote(pkt), pkt.src.To4(), pkt.dst.To4()); icmp != nil {
				_ = w.clientSender().SendIPv4(icmp, pkt.src)
			}
		} else {
			if icmp := sock.BuildICMPv6Reject(pkt.raw, pkt.src.To16(), pkt.dst.To16()); icmp != nil {
				_ = w.clientSender().SendIPv6(icmp, pkt.src)
			}
		}
		return 0

	case "fake":
		if !config.RoutingUsesTProxy(set.RoutingModeOrDefault()) && set.RoutingHandsOffPackets() {
			return vc.accept()
		}

		if !injectAcquire() {
			return vc.accept()
		}

		packetCopy := make([]byte, len(pkt.raw))
		copy(packetCopy, pkt.raw)
		dstCopy := make(net.IP, len(pkt.dst))
		copy(dstCopy, pkt.dst)
		setCopy := set

		if !vc.drop() {
			injectRelease()
			return 0
		}

		v := pkt.ver
		w.wg.Add(1)
		go func(s *config.SetConfig, p []byte, d net.IP) {
			defer func() {
				injectRelease()
				w.wg.Done()
			}()
			if v == IPv4 {
				w.dropAndInjectQUIC(s, p, d)
			} else {
				w.dropAndInjectQUICV6(s, p, d)
			}
		}(setCopy, packetCopy, dstCopy)
		return 0

	default:
		return vc.accept()
	}
}

func (w *Worker) handleNfqError(e error) int {
	if errors.Is(e, syscall.ENOBUFS) {
		now := time.Now().Unix()
		last := atomic.LoadInt64(&w.lastOverflowLog)
		if now-last >= 5 {
			if atomic.CompareAndSwapInt64(&w.lastOverflowLog, last, now) {
				log.Warnf("nfq queue %d overflow - packets dropped", w.qnum)
			}
		}
		return 0
	}
	if w.ctx.Err() != nil {
		return 0
	}
	if errors.Is(e, os.ErrClosed) || errors.Is(e, net.ErrClosed) || errors.Is(e, syscall.EBADF) {
		return 0
	}
	if ne, ok := e.(net.Error); ok && ne.Timeout() {
		return 0
	}
	msg := e.Error()
	if strings.Contains(msg, "use of closed file") || strings.Contains(msg, "file descriptor") {
		return 0
	}
	log.Errorf("nfq: %v", e)
	return 0
}

func (w *Worker) rejectQuote(pkt *pktInfo) []byte {
	if pkt == nil || pkt.ver != IPv4 || len(pkt.raw) < 20 {
		return nil
	}
	v4 := pkt.src.To4()
	if v4 == nil || bytes.Equal(pkt.raw[12:16], v4) {
		return pkt.raw
	}
	if !w.tunSNAT {
		return pkt.raw
	}
	quoted := make([]byte, len(pkt.raw))
	copy(quoted, pkt.raw)
	copy(quoted[12:16], v4)
	sock.FixIPv4Checksum(quoted)
	return quoted
}
