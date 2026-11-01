package nfq

import (
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

	srcUnattributed bool
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
	return m&^selfInjectedBits(queueMark) == 0
}

func localQueueMarked(a nfqueue.Attribute, queueMark uint32, cfg *config.Config) bool {
	if a.UID == nil || a.Mark == nil || queueMark == 0 || *a.Mark&queueMark != queueMark {
		return false
	}
	if _, role := packetIfaceIndex(a); role != ifaceLeaving {
		return false
	}
	m := *a.Mark
	return cfg == nil || (m != uint32(cfg.DiscoveryFlowMark()) && m != uint32(cfg.DiscoveryInjectedMark()))
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

	vc := &verdictCtx{id: *a.PacketID}
	if q != nil {
		vc.q = q
	}
	return w.handleQueuedThenPersist(vc, a, mark)
}

func (w *Worker) handleQueuedThenPersist(vc *verdictCtx, a nfqueue.Attribute, mark uint) int {
	gen := w.flowDropGeneration()
	ret := w.handleQueued(vc, a, mark)
	if vc.dropsFlow() {
		w.persistFlowDrop(*a.Payload, gen)
	}
	return ret
}

func (w *Worker) handleQueued(vc *verdictCtx, a nfqueue.Attribute, mark uint) int {
	if a.Mark != nil && selfInjectedMark(*a.Mark, uint32(mark), w.getConfig()) {
		return vc.accept()
	}

	if localQueueMarked(a, uint32(mark), w.getConfig()) {
		return vc.accept()
	}

	if a.Mark != nil && (config.IsTelegramBridgeMark(*a.Mark) || config.IsSelfDialSkipMark(*a.Mark)) {
		return vc.accept()
	}

	if !w.matchesInterface(a) {
		if !w.dnsCopyMayClaim(*a.Payload) {
			return vc.accept()
		}
		vc.ifaceFiltered = true
	}
	vc.tunnel = tunnelEgress(a)
	if a.Mark != nil && *a.Mark != 0 {
		if idx, role := packetIfaceIndex(a); role == ifaceLeaving {
			vc.foreignMark, vc.outDev = *a.Mark, idx
		}
	}

	select {
	case <-w.ctx.Done():
		return 0
	default:
	}

	if a.Hook != nil {
		vc.hook, vc.hooked = *a.Hook, true
	}
	if a.InDev != nil {
		vc.inDev = *a.InDev
	}
	vc.localSocket = a.UID != nil

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
	vc.dir = w.packetDirection(cfg, vc, pkt)
	if !cfg.Queue.IsDiscovery && cfg.Queue.Mode != "tun" && !cfg.IPFamilyEnabled(pkt.ver == IPv6) {
		return w.handleECHOnlyPacket(vc, pkt, cfg)
	}

	matched, st, stPrefix := matcher.MatchIPWithSourceDetail(pkt.dst, pkt.srcMac, w.blockScope(vc, pkt, matcher))
	var set *config.SetConfig
	if matched {
		set = st
	}

	switch pkt.proto {
	case 6:
		if len(pkt.raw) >= pkt.ihl+TCPHeaderMinLen {
			return w.handleTCPPacket(vc, pkt, cfg, matcher, matched, set, st, stPrefix)
		}
	case 17:
		if len(pkt.raw) >= pkt.ihl+UDPHeaderLen {
			return w.handleUDPPacket(vc, pkt, cfg, matcher, matched, set, st, stPrefix)
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
		set.TCP.HTTPMethodEOL ||
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
		seen := tunSrcPacket{ttl: raw[8], answer: p.proto == 17 && sport == 53 && dnsAnswerDatagram(raw[p.ihl:])}
		switch lan, origin := w.srcResolver.attributePacket(seen, p.proto, p.src, sport, p.dst, dport); origin {
		case tunSrcLAN, tunSrcRouter:
			if v4 := lan.To4(); v4 != nil {
				copy(p.addr[0:4], v4)
			}
		case tunSrcUnknown:
			p.srcUnattributed = true
		}
	}

	p.srcStr = p.src.String()
	p.dstStr = p.dst.String()
	p.srcMac = w.getMacByIp(p.srcStr)

	return p, true
}

type addressHold struct {
	set    *config.SetConfig
	prefix int
}

func (h addressHold) on() bool {
	return h.set != nil && h.prefix > 0 && h.set.IPPriorityApplies()
}

func (h addressHold) contests(name *config.SetConfig) bool {
	return name != nil && h.on() && name.Id != h.set.Id
}

func (h addressHold) fits(ipVersion uint8) bool {
	return h.set.MatchesIPVersion(ipVersion)
}

func (h addressHold) outranks(matcher *sni.SuffixSet, name *config.SetConfig, srcMac string) bool {
	return h.contests(name) && matcher.AddressHolds(h.set, h.prefix, name, srcMac)
}

func (h addressHold) against(matcher *sni.SuffixSet, name *config.SetConfig, srcMac string, ipVersion uint8) bool {
	return h.outranks(matcher, name, srcMac) && h.fits(ipVersion)
}

func (h addressHold) keeps(set *config.SetConfig) bool {
	return set != nil && h.on() && set.Id == h.set.Id
}

func (h addressHold) yieldsTo(cfg *config.Config, matcher *sni.SuffixSet, escSet *config.SetConfig, srcMac string) bool {
	return escalationReaches(cfg, h.set, escSet.Id) || !h.outranks(matcher, escSet, srcMac)
}

func (w *Worker) connectionSet(matcher *sni.SuffixSet, connKey, srcMac string, ipVersion uint8, dport uint16, payload []byte, seq uint32, keep sni.SetFilter, hold addressHold) (*config.SetConfig, string, bool) {
	decision, ok := w.tlsCache.Decision(connKey)
	if !ok || (startsClientHello(payload) && decision.helloSeq-seq > maxPendingHelloRecord) {
		return nil, "", false
	}
	set, held := decision.set, decision.heldBy != ""
	if decision.decidedBy != matcher || decision.decidedMac != srcMac {
		stillHeld := decision.heldBy != "" && hold.on() && decision.heldBy == hold.set.Id && hold.set.MatchesIPVersion(ipVersion)
		set, held = nil, false
		var named *config.SetConfig
		if matched, candidate := matcher.MatchSNIWithSourceTLS(decision.host, srcMac, decision.tlsVersion, ipVersion, keep); matched && candidate.MatchesTCPDPort(dport) {
			named = candidate
		}
		switch {
		case stillHeld && (named == nil || hold.outranks(matcher, named, srcMac)):
			set, held = hold.set, true
		case named != nil:
			set = named
			if hold.against(matcher, named, srcMac, ipVersion) {
				set, held = hold.set, true
			}
		}
		w.tlsCache.DecideHeld(connKey, decision.gen, matcher, srcMac, set, held)
	}
	if set == nil {
		return nil, "", false
	}
	return set, decision.host, held && hold.keeps(set)
}

func fragmentsOrDesyncs(set *config.SetConfig) bool {
	return set.Fragmentation.Strategy != config.ConfigNone || len(set.Fragmentation.StrategyPool) > 0 || set.TCP.Desync.Mode != config.ConfigOff
}

func duplicatesPackets(set *config.SetConfig) bool {
	return set.TCP.Duplicate.Enabled && set.TCP.Duplicate.Count > 0
}

func (w *Worker) handleTCPPacket(vc *verdictCtx, pkt *pktInfo, cfg *config.Config, matcher *sni.SuffixSet, matched bool, set *config.SetConfig, st *config.SetConfig, stPrefix int) int {
	tcp := pkt.raw[pkt.ihl:]
	if len(tcp) < TCPHeaderMinLen {
		return vc.accept()
	}
	datOff := int((tcp[12]>>4)&0x0f) * 4
	if datOff < TCPHeaderMinLen || len(tcp) < datOff {
		return vc.accept()
	}
	payload := tcp[datOff:]
	sport := binary.BigEndian.Uint16(tcp[0:2])
	dport := binary.BigEndian.Uint16(tcp[2:4])

	if vc.tcpIncoming(cfg, sport) {
		return w.HandleIncoming(vc, pkt.ver, pkt.raw, pkt.ihl, pkt.src, pkt.dstStr, dport, pkt.srcStr, sport, payload)
	}

	keep := w.blockScope(vc, pkt, matcher)

	if matched && !set.MatchesTCPDPort(dport) {
		matched = false
		set = nil
		st = nil
	}

	if utils.IsPrivateIP(pkt.dst) && !matcher.ListsPrivateTCPTarget(pkt.dst, pkt.srcMac, dport) {
		return vc.accept()
	}

	addressMatched := st != nil
	hold := addressHold{set: st, prefix: stPrefix}

	connKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)
	w.noteClientProgress(connKey, tcp)
	if cfg.IsTCPPort(dport) && tcp[13]&0x12 == 0x02 {
		if w.tlsCache.DropOtherConnection(connKey, binary.BigEndian.Uint32(tcp[4:8])+1) {
			w.destState.ForgetConnection(connKey)
		}
	}

	var connSet *config.SetConfig
	connHost := ""
	connHeld := false
	if cfg.IsTCPPort(dport) && tcp[13]&0x02 == 0 {
		connSet, connHost, connHeld = w.connectionSet(matcher, connKey, pkt.srcMac, pkt.ver, dport, payload, binary.BigEndian.Uint32(tcp[4:8]), keep, hold)
	}

	matchedHint := false
	hintHost := ""
	var hintTLS uint16
	var hintIP uint8
	if !matched && cfg.IsTCPPort(dport) {
		if hintSet, hinted, hintedTLS, hintedIP := w.lookupHostHint(cfg, pkt.srcStr, pkt.dstStr, pkt.srcMac, keep); hintSet != nil {
			if hintSet.MatchesTCPDPort(dport) {
				matched = true
				set = hintSet
				matchedHint = true
				hintHost = hinted
				hintTLS, hintIP = hintedTLS, hintedIP
			}
		}
	}

	matchedLearned := false
	learnedHost := ""
	heldLearned := false
	var learnedRival *config.SetConfig
	if !matchedHint {
		if mLearned, learnedSet, learnedDomain := matcher.MatchLearnedIPWithSource(pkt.dst, pkt.srcMac, keep); mLearned {
			if learnedSet.MatchesTCPDPort(dport) {
				if hold.contests(learnedSet) && (connHeld || hold.outranks(matcher, learnedSet, pkt.srcMac)) {
					learnedRival = learnedSet
				}
				if (learnedRival != nil && hold.fits(pkt.ver)) || (connHeld && hold.keeps(learnedSet)) {
					heldLearned = true
				} else {
					matched = true
					set = learnedSet
					st = learnedSet
					matchedLearned = true
					learnedHost = learnedDomain
				}
			}
		}
	}

	matchedPort := false
	if !matched && cfg.IsTCPPort(dport) {
		if portMatched, portSet := matcher.MatchTCPPort(dport, pkt.srcMac, keep); portMatched {
			matched = true
			set = portSet
			matchedPort = true
		}
	}

	matchedConn := false
	takeOver := connSet != nil && (matchedHint || matchedLearned || matchedPort) && set != nil && set.Id != connSet.Id && !addressMatched &&
		(fragmentsOrDesyncs(connSet) || duplicatesPackets(connSet)) && (needsPayloadlessInjection(connSet) || !needsPayloadlessInjection(set))
	if connSet != nil && (!matched || takeOver) {
		matched = true
		set = connSet
		st = nil
		matchedConn = true
		matchedHint = false
		hintHost = ""
		matchedLearned = false
		learnedHost = ""
	}

	earlyHost := hintHost
	if earlyHost == "" {
		earlyHost = learnedHost
	}
	if connHeld || (earlyHost == "" && matchedConn) {
		earlyHost = connHost
	}
	if matched && earlyHost != "" && cfg.IsTCPPort(dport) {
		if escSet := w.escalatedSetFor(cfg, set, earlyHost, pkt.srcMac, keep); escSet != nil && escSet != set && (!connHeld || !hold.keeps(set) || hold.yieldsTo(cfg, matcher, escSet, pkt.srcMac)) {
			log.Tracef("escalation hit for %s before the ClientHello: %s -> %s", earlyHost, set.Name, escSet.Name)
			set = escSet
			if st != nil {
				st = escSet
			}
		}
	}

	routeTProxy := matched && set != nil && set.RoutingDivertsPackets() && config.RoutingUsesTProxy(set.Routing.Mode)
	routeIfaceHandoff := matched && set != nil && !routeTProxy && set.RoutingHandsOffPackets()
	foreignEgress := matched && set != nil && w.handsOffEgress(vc, pkt, cfg, set)
	routeHandsOff := routeTProxy || routeIfaceHandoff || foreignEgress

	if matched && !routeHandsOff && cfg.IsTCPPort(dport) && duplicatesPackets(set) {
		log.Tracef("TCP duplicate to %s:%d (%d copies, set: %s)", pkt.dstStr, dport, set.TCP.Duplicate.Count, set.Name)

		dupHost, dupTLS, _ := w.tlsCache.Lookup(connKey)

		observeFlow(cfg, pkt, sport, dport, set, dupHost)

		if !cfg.Queue.IsDiscovery {
			dupSNISet, dupIPSet := "", set.Name
			if matchedConn {
				dupSNISet, dupIPSet = set.Name, ""
			}
			log.LogConnection("TCP", dupSNISet, dupHost, pkt.srcStr, sport, dupIPSet, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(dupTLS), "tcp-dup")
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
		if matched && set != nil && set.TCP.RSTProtection.Enabled && !foreignEgress {
			if w.connTracker.ShouldDropOutboundRST(connKey) {
				log.Warnf("RST protection: dropped outbound RST to %s:%d — connection not established", pkt.dstStr, dport)
				recordRSTDrop(cfg)
				vc.drop()
				return 0
			}
		}
	}

	if isAck && !isSyn && !isRst && cfg.IsTCPPort(dport) && matched && set != nil && set.TCP.RSTProtection.Enabled {
		w.connTracker.MarkEstablished(connKey)
	}

	if isSyn && !isAck && !routeHandsOff && cfg.IsTCPPort(dport) && matched {
		if w.handleSynBan(vc, pkt, cfg, set, sport, dport) {
			return 0
		}
		if w.handleSynHealth(vc, pkt, cfg, set, sport, dport) {
			return 0
		}
	}

	if isSyn && !isAck && !routeHandsOff && cfg.IsTCPPort(dport) && matched && !set.TCP.Duplicate.Enabled && needsTCPSynInjection(set) {
		log.Tracef("TCP SYN to %s:%d (set: %s)", pkt.dstStr, dport, set.Name)

		observeFlow(cfg, pkt, sport, dport, set, "")

		syn := pkt.raw
		if len(payload) > 0 && needsTCPInjection(set) {
			if stripped, ok := synWithoutPayload(pkt.raw, pkt.ver, pkt.ihl, datOff); ok {
				syn = stripped
			}
		}

		if pkt.ver == IPv4 {
			if set.TCP.SynFake {
				w.sendFakeSyn(set, syn, pkt.ihl, datOff)
			}
			if set.Faking.TCPMD5 {
				w.sendFakeSynWithMD5(set, syn, pkt.ihl, pkt.dst)
			}
			_ = w.sock.SendIPv4(syn, pkt.dst)
		} else {
			if set.TCP.SynFake {
				w.sendFakeSynV6(set, syn, pkt.ihl, datOff)
			}
			if set.Faking.TCPMD5 {
				w.sendFakeSynWithMD5V6(set, syn, pkt.dst)
			}
			_ = w.sock.SendIPv6(syn, pkt.dst)
		}

		if set.TCP.Incoming.Mode != config.ConfigOff || set.TCP.RSTProtection.Enabled || set.Escalate.Active() {
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
	heldHello := false
	ipTarget := ""
	sniTarget := ""
	classifyReason := ""

	if !matchedIP && !matchedConn && matched && set != nil {
		ipTarget = set.Name
	}

	clearMatch := func() {
		matched = false
		set = nil
		st = nil
		matchedIP = false
		matchedConn = false
		ipTarget = ""
	}

	if cfg.IsTCPPort(dport) && len(payload) > 0 {
		log.Tracef("TCP payload to %s: len=%d, first5=%x", pkt.dstStr, len(payload), payload[:min(5, len(payload))])
		if len(payload) >= 5 && payload[0] == 0x16 {
			log.Tracef("TLS record: type=%x ver=%x%x len=%d", payload[0], payload[1], payload[2],
				int(payload[3])<<8|int(payload[4]))
		}

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

		var helloGen uint64
		if host != "" && tlsVersion != 0 {
			helloSeq := binary.BigEndian.Uint32(tcp[4:8])
			if tcp[13]&0x02 != 0 {
				helloSeq++
			}
			helloGen = w.tlsCache.Store(connKey, host, tlsVersion, helloSeq)
		}

		if captureManager := capture.GetManager(cfg); captureManager != nil {
			captureManager.CapturePayload(connKey, host, "tls", payload)
		}

		if host != "" {
			var decided *config.SetConfig
			if mSNI, stSNI := matcher.MatchSNIWithSourceTLS(host, pkt.srcMac, tlsVersion, pkt.ver, keep); mSNI {
				if stSNI.MatchesTCPDPort(dport) {
					if hold.against(matcher, stSNI, pkt.srcMac, pkt.ver) {
						log.Tracef("ip priority: %s keeps %s:%d, the ClientHello for %s does not move it to %s", hold.set.Name, pkt.dstStr, dport, host, stSNI.Name)
						heldHello = true
					} else {
						decided = stSNI
						matchedSNI = true
						matched = true
						set = stSNI
						matcher.LearnIPToDomain(pkt.dst, host, stSNI)
						if vc.scope.learnsTLS(host, tlsVersion, pkt.ver) {
							registerLearnedRoute(cfg, stSNI, pkt.dst, host)
						}
					}
				}
			}
			if !matchedSNI && !heldHello && learnedRival != nil && hold.fits(pkt.ver) {
				heldHello = true
			}
			if heldHello {
				decided = hold.set
				matched = true
				set = hold.set
				st = hold.set
				matchedIP = true
				matchedLearned = false
				learnedHost = ""
			}
			w.tlsCache.DecideHeld(connKey, helloGen, matcher, pkt.srcMac, decided, heldHello)
		}

		if matched && !matchedSNI && set != nil && !set.MatchesTLSVersion(tlsVersion) {
			clearMatch()
		}

		if (matchedLearned || (matchedConn && !takeOver)) && !matchedSNI && !(len(payload) >= 1 && payload[0] == 0x16) {
			if set != nil && !fragmentsOrDesyncs(set) {
				clearMatch()
			}
		}

		if matchedHint && !matchedSNI && isClientHello && host != "" {
			log.Tracef("host hint for %s dropped: %s carries a clear SNI that matches no set", pkt.dstStr, host)
			clearMatch()
			matchedHint = false
			hintHost = ""
			if learnedSet, learnedDomain := learnedMatchAfterHintDrop(matcher, pkt.dst, pkt.srcMac, dport, tlsVersion, payload, keep); learnedSet != nil {
				log.Tracef("host hint for %s dropped, learned address match took over: %s (set: %s)", pkt.dstStr, learnedDomain, learnedSet.Name)
				matched = true
				set = learnedSet
				st = learnedSet
				matchedIP = true
				matchedLearned = true
				learnedHost = learnedDomain
				if escSet := w.escalatedSetFor(cfg, set, learnedHost, pkt.srcMac, keep); escSet != nil && escSet != set {
					log.Tracef("escalation hit for %s after the hint drop: %s -> %s", learnedHost, set.Name, escSet.Name)
					set = escSet
					st = escSet
				}
			}
		}
	}

	if host == "" || tlsVersion == 0 {
		if cachedHost, cachedTLS, found := w.tlsCache.Lookup(connKey); found {
			if host == "" {
				host = cachedHost
			}
			if tlsVersion == 0 {
				tlsVersion = cachedTLS
			}
		}
	}

	hostTLS, hostIP := tlsVersion, pkt.ver
	if host == "" && hintHost != "" {
		host = hintHost
		hostTLS, hostIP = hintTLS, hintIP
	}
	if matchedHint && !matchedSNI && classifyReason == "" {
		classifyReason = "dns-hint"
	}

	if matchedSNI || matchedConn {
		sniTarget = set.Name
	} else if matchedIP {
		ipTarget = st.Name
	}

	addressHolds := (heldHello || heldLearned || connHeld) && !matchedSNI

	if matched && host != "" && cfg.IsTCPPort(dport) {
		if escSet := w.escalatedSetFor(cfg, set, host, pkt.srcMac, keep); escSet != nil && (!addressHolds || hold.yieldsTo(cfg, matcher, escSet, pkt.srcMac)) {
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
			if vc.scope.learnsTLS(host, hostTLS, hostIP) {
				w.refreshEscalatedRoute(cfg, escSet, host, pkt.dst)
			}
		}
	}

	routeTProxy = matched && set != nil && set.RoutingDivertsPackets() && config.RoutingUsesTProxy(set.Routing.Mode)
	routeIfaceHandoff = matched && set != nil && !routeTProxy && set.RoutingHandsOffPackets()
	foreignEgress = matched && set != nil && w.handsOffEgress(vc, pkt, cfg, set)
	routeHandsOff = routeTProxy || routeIfaceHandoff || foreignEgress

	if routeIfaceHandoff && classifyReason == "" {
		classifyReason = "routed->" + set.Routing.EgressInterface
	}
	if foreignEgress && classifyReason == "" {
		classifyReason = vc.handOffReason()
	}

	if matched && isClientHello && !routeHandsOff && set.TCP.IPBlockDetect.Enabled && host != "" && cfg.IsTCPPort(dport) {
		ibd := &set.TCP.IPBlockDetect
		dstIPPort := fmt.Sprintf("%s:%d", pkt.dstStr, dport)

		cachedReason := ""
		if ibd.CacheBlockedIPs {
			if w.bans.banned(pkt.dst, dport) {
				cachedReason = "ipblock-ban"
			} else if w.destState.IsBlocked(dstIPPort) {
				cachedReason = "ipblock-cached"
			}
		}
		if cachedReason != "" {
			if !cfg.Queue.IsDiscovery {
				log.LogConnection("TCP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(tlsVersion), cachedReason)
			}
			w.destState.Unwatch(connKey)
			if pkt.ver == IPv4 {
				w.sendRSTToClientV4(pkt.raw, pkt.ihl, pkt.src, pkt.dst)
			} else {
				w.sendRSTToClientV6(pkt.raw, pkt.src, pkt.dst)
			}

			observeFlow(cfg, pkt, sport, dport, set, host)
			vc.drop()
			log.Tracef("IPBlockDetect: dropped packet to %s:%d (%s)", pkt.dstStr, dport, cachedReason)

			return 0
		}
	}

	if !cfg.Queue.IsDiscovery {
		log.LogConnection("TCP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(tlsVersion), classifyReason)
	}

	if !routeTProxy {
		var flowSet *config.SetConfig
		if matched {
			flowSet = set
		}
		observeFlow(cfg, pkt, sport, dport, flowSet, host)
	}

	var strippedSyn []byte
	if isSyn && len(payload) > 0 && matched && set != nil && !routeHandsOff && needsTCPInjection(set) {
		strippedSyn, _ = synWithoutPayload(pkt.raw, pkt.ver, pkt.ihl, datOff)
	}

	stallCount := 0
	stallFirstSeen := time.Time{}
	stallTracked := false
	if matched && isClientHello && strippedSyn == nil && host != "" && cfg.IsTCPPort(dport) && !foreignEgress &&
		(set.TCP.IPBlockDetect.Enabled || set.Escalate.Active()) {
		stallCount, stallFirstSeen = w.destState.RecordClientHello(connKey, host)
		stallTracked = true
	}

	if stallTracked && escalateOnStall(&set.Escalate, stallCount, stallFirstSeen) {
		if next := w.tryEscalate(cfg, set, host, pkt.srcMac, pkt.dst, escalateReasonStall, hostTLS, hostIP, vc.scope); next != nil {
			w.destState.ClearClientHellos(connKey)
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
				recordBlockedFlow(cfg, pkt, sport, dport, set, blockedTarget)
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

			threshold := ibd.RetransmitThreshold
			if threshold <= 0 {
				threshold = 3
			}
			timeout := time.Duration(ibd.TimeoutMs) * time.Millisecond
			if timeout <= 0 {
				timeout = 3000 * time.Millisecond
			}

			if stallCount >= threshold || (stallCount > 1 && time.Since(stallFirstSeen) > timeout) {
				if !w.destState.HasRSTSent(connKey) {
					w.destState.MarkRSTSent(connKey)
					w.destState.Unwatch(connKey)
					if pkt.ver == IPv4 {
						w.sendRSTToClientV4(pkt.raw, pkt.ihl, pkt.src, pkt.dst)
					} else {
						w.sendRSTToClientV6(pkt.raw, pkt.src, pkt.dst)
					}
					if ibd.CacheBlockedIPs {
						w.destState.AddBlocked(dstIPPort)
						w.bans.confirm(cfg, pkt.dst, dport, host, set.Name)
					}
					if !cfg.Queue.IsDiscovery {
						log.LogConnection("TCP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(tlsVersion), "ipblock")
					}
				}
				vc.drop()
				return 0
			}
			if stallCount == 1 {
				w.watchHelloSilence(cfg, set, pkt, vc.scope, connKey, host, sniTarget, ipTarget, sport, dport, tlsVersion, stallFirstSeen.Add(timeout))
			}
		}

		if !foreignEgress && (set.TCP.Incoming.Mode != config.ConfigOff || set.TCP.RSTProtection.Enabled || set.Escalate.Active() || addressHolds) {
			w.connTracker.RegisterOutgoing(connKey, set)
			if isAck && !isSyn && !isRst && cfg.IsTCPPort(dport) && set.TCP.RSTProtection.Enabled {
				w.connTracker.MarkEstablished(connKey)
			}
		}

		if routeHandsOff || !needsTCPInjection(set) {
			return vc.accept()
		}

		if len(payload) == 0 && !needsPayloadlessInjection(set) {
			return vc.accept()
		}

		if isSyn && len(payload) > 0 {
			if strippedSyn == nil {
				return vc.accept()
			}
			if set.TCP.DropSACK {
				if pkt.ver == 4 {
					strippedSyn = sock.StripSACKFromTCP(strippedSyn)
				} else {
					strippedSyn = sock.StripSACKFromTCPv6(strippedSyn)
				}
			}
			if !vc.drop() {
				return 0
			}
			if pkt.ver == 4 {
				_ = w.sock.SendIPv4(strippedSyn, pkt.dst)
			} else {
				_ = w.sock.SendIPv6(strippedSyn, pkt.dst)
			}
			return 0
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

func (w *Worker) handleUDPPacket(vc *verdictCtx, pkt *pktInfo, cfg *config.Config, matcher *sni.SuffixSet, matched bool, set *config.SetConfig, st *config.SetConfig, stPrefix int) int {
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

	keep := w.blockScope(vc, pkt, matcher)
	voiceSet := w.voiceFlowSet(matcher, connKey, pkt, dport, keep)
	if voiceSet != nil && !voiceSet.SelectsL7Class(udpL7Class(payload)) {
		return w.passVoiceFlow(vc, pkt, cfg, voiceSet, sport, dport)
	}

	matchedIP := st != nil
	matchedQUIC := false
	matchedLearned := false
	isVoiceMedia := false
	host := ""
	learnedHost := ""
	ipTarget := ""
	sniTarget := ""

	if matchedIP && !st.MatchesUDPDPort(dport) {
		matchedIP = false
		matched = false
		set = nil
	}

	var hold addressHold
	if matchedIP {
		hold = addressHold{set: st, prefix: stPrefix}
		ipTarget = st.Name
	}

	if !matchedIP {
		if mLearned, learnedSet, learnedDomain := matcher.MatchLearnedIPWithSource(pkt.dst, pkt.srcMac, keep); mLearned {
			if learnedSet.MatchesUDPDPort(dport) {
				matchedIP = true
				matchedLearned = true
				matched = true
				set = learnedSet
				learnedHost = learnedDomain
				sniTarget = learnedSet.Name
				ipTarget = learnedSet.Name
			}
		}
	}

	matchedPort := false
	if !matched {
		if portMatched, portSet := matcher.MatchUDPPort(dport, pkt.srcMac, keep); portMatched {
			matchedPort = true
			matched = true
			set = portSet
			ipTarget = portSet.Name
		}
	}

	isVoiceMedia = stun.IsSTUNMessage(payload) || discord.IsVoicePacket(payload)

	isQUIC := quic.LooksLikeQUIC(payload)

	namedByHello := false
	helloDatagram := false
	if isQUIC {
		if h, ok := sni.ParseQUICClientHelloSNI(payload); ok {
			helloDatagram = true
			host = h
			namedByHello = true
		}
	}

	flowKnown := false
	flowHost := ""
	if host == "" && (isQUIC || mayBelongToAQUICFlow(payload, isVoiceMedia)) {
		flowHost, flowKnown = w.quicFlows.Lookup(connKey, payload)
	}
	quicTraffic := isQUIC || flowKnown
	if host == "" && !quicTraffic {
		host = learnedHost
	}

	quicHeld := false
	if host != "" {
		if mSNI, sniSet := matcher.MatchSNIWithSourceTLS(host, pkt.srcMac, 0x0304, pkt.ver, keep); mSNI {
			if sniSet.MatchesUDPDPort(dport) {
				if hold.against(matcher, sniSet, pkt.srcMac, pkt.ver) {
					log.Tracef("ip priority: %s keeps %s:%d, the QUIC Initial for %s does not move it to %s", hold.set.Name, pkt.dstStr, dport, host, sniSet.Name)
					quicHeld = true
				} else {
					matchedQUIC = true
					set = sniSet
					sniTarget = sniSet.Name
					matcher.LearnIPToDomain(pkt.dst, host, sniSet)
					if vc.scope.learnsTLS(host, 0x0304, pkt.ver) {
						registerLearnedRoute(cfg, sniSet, pkt.dst, host)
					}
					w.storeHostHint(pkt.srcStr, pkt.dstStr, sniSet, host, "quic", 0x0304, pkt.ver)
				}
			}
		}
	}

	if namedByHello {
		w.quicFlows.Store(connKey, host, quic.ParseDCID(payload))
	} else if isQUIC && quic.IsInitial(payload) {
		w.quicFlows.NoteQUIC(connKey, quic.ParseDCID(payload))
	}

	if !matchedQUIC && (matchedIP || matchedPort) && set.UDP.FilterQUIC == "all" {
		if quicTraffic {
			matchedQUIC = true
		}
	}

	if matchedLearned && !matchedQUIC && quicTraffic {
		matchedIP = false
		matchedLearned = false
		matched = false
		set = nil
		learnedHost = ""
		sniTarget = ""
		ipTarget = ""
		if portMatched, portSet := matcher.MatchUDPPort(dport, pkt.srcMac, keep); portMatched {
			matchedPort = true
			matched = true
			set = portSet
			ipTarget = portSet.Name
			matchedQUIC = portSet.UDP.FilterQUIC == config.QUICFilterAll
		}
	}

	if host == "" {
		host = learnedHost
	}

	if captureManager := capture.GetManager(cfg); captureManager != nil {
		captureManager.CapturePayload(connKey, host, "quic", payload)
	}

	matchedFlow := false
	if matchedLearned || (host == "" && !matchedIP) {
		if flowSet, flowHost := w.quicFlowSet(cfg, matcher, connKey, pkt, dport, payload, keep); flowSet != nil {
			matchedFlow = true
			matchedLearned = false
			set = flowSet
			host = flowHost
			sniTarget = flowSet.Name
			ipTarget = ""
		}
	} else if host == "" && flowHost != "" {
		if flowSet := w.quicFlowYieldedBy(cfg, matcher, hold, flowHost, pkt, dport, keep); flowSet != nil {
			matchedFlow = true
			matchedIP = false
			matchedQUIC = false
			set = flowSet
			host = flowHost
			sniTarget = flowSet.Name
			ipTarget = ""
		} else if escSet := w.heldQUICFlowEscalation(cfg, matcher, hold, flowHost, pkt, dport, keep); escSet != nil && hold.keeps(set) {
			log.Tracef("UDP escalation hit for the held QUIC flow of %s: %s -> %s", flowHost, set.Name, escSet.Name)
			set = escSet
			ipTarget = escSet.Name
		}
	}

	matchedL7 := false
	l7Set := voiceSet
	if l7Set == nil {
		if class := udpL7Class(payload); class != "" {
			if l7Matched, selected := matcher.MatchUDPL7(dport, pkt.srcMac, class, pkt.ver, keep); l7Matched {
				l7Set = selected
				w.voiceFlows.Store(connKey, selected.Id)
			}
		}
	}
	if l7Set != nil {
		matchedL7 = true
		matchedIP = false
		matchedQUIC = false
		matchedFlow = false
		matchedPort = false
		matchedLearned = false
		set = l7Set
		sniTarget = ""
		ipTarget = l7Set.Name
	}

	anyMatch := matchedIP || matchedQUIC || matchedFlow || matchedPort || matchedL7
	shouldHandle := anyMatch && !(isVoiceMedia && !matchedL7 && set.UDP.FilterSTUN)

	matched = shouldHandle

	udpTLS := ""
	if matchedQUIC || matchedFlow || isQUIC {
		udpTLS = "1.3"
	}

	addressHolds := quicHeld && hold.keeps(set)

	if shouldHandle && set != nil && host != "" && !matchedFlow && !matchedL7 {
		if escSet := w.escalatedSetFor(cfg, set, host, pkt.srcMac, keep); escSet != nil && (!addressHolds || hold.yieldsTo(cfg, matcher, escSet, pkt.srcMac)) {
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
			if vc.scope.learnsTLS(host, 0x0304, pkt.ver) {
				w.refreshEscalatedRoute(cfg, escSet, host, pkt.dst)
			}
		}
	}

	voiceExempt := anyMatch && isVoiceMedia && !matchedL7 && set != nil && set.UDP.FilterSTUN
	udpForeignEgress := shouldHandle && !voiceExempt && set != nil && w.handsOffEgress(vc, pkt, cfg, set)
	bannedQUIC := shouldHandle && !voiceExempt && !udpForeignEgress && (isQUIC || matchedFlow) && !isVoiceMedia && w.refusesBannedQUIC(cfg, set, pkt.dst, dport)
	udpReason := ""
	if udpForeignEgress {
		udpReason = vc.handOffReason()
	} else if voiceExempt {
		udpReason = voiceExemptReason
	} else if bannedQUIC {
		udpReason = "ipblock-ban"
	}
	if !cfg.Queue.IsDiscovery {
		log.LogConnection("UDP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, udpTLS, udpReason)
	}

	if isVoiceMedia && !matchedL7 && set != nil && set.UDP.FilterSTUN {
		return vc.accept()
	}

	if !shouldHandle {
		observeFlow(cfg, pkt, sport, dport, nil, host)
		return vc.accept()
	}

	if !udpViaTProxy(set) {
		observeFlow(cfg, pkt, sport, dport, set, host)
	}

	quicFlow := (isQUIC || matchedFlow) && !isVoiceMedia

	if set.Routing.Enabled && config.RoutingIsBlock(set.Routing.Mode) {
		if matchedQUIC || matchedFlow || (matchedIP && !matchedLearned) || matchedL7 {
			return w.blockUDP(vc, pkt, cfg, set, sport, dport, sniTarget, host, ipTarget, udpTLS, quicFlow)
		}
		return vc.accept()
	}

	if udpForeignEgress {
		return vc.accept()
	}

	if bannedQUIC {
		return w.rejectDatagram(vc, pkt, quicFlow)
	}

	switch set.UDP.Mode {
	case config.ConfigOff:
		return vc.accept()

	case "drop":
		vc.drop()
		if quicFlow {
			vc.dropFlow()
		}
		return 0

	case "reject":
		return w.rejectDatagram(vc, pkt, quicFlow)

	case config.UDPModeCoalesce:
		if !config.RoutingUsesTProxy(set.RoutingModeOrDefault()) && set.RoutingHandsOffPackets() {
			return vc.accept()
		}
		if !helloDatagram {
			return vc.accept()
		}
		if w.quicFlows.NoteCoalesced(connKey) > maxCoalescedHellos {
			log.Tracef("QUIC ClientHello to %s repeated past %d coalesced datagrams, sending it unchanged", pkt.dstStr, maxCoalescedHellos)
			return vc.accept()
		}
		coalesced, ok := quic.CoalesceInitial(payload, 0)
		if !ok {
			return vc.accept()
		}
		var out []byte
		if pkt.ver == IPv4 {
			out = sock.BuildUDPPacketV4(pkt.src, pkt.dst, sport, dport, coalesced)
		} else {
			out = sock.BuildUDPPacketV6(pkt.src, pkt.dst, sport, dport, coalesced)
		}
		if out == nil {
			return vc.accept()
		}
		if !vc.drop() {
			return 0
		}
		log.Tracef("Coalescing QUIC Initial to %s behind a %d byte padding packet", pkt.dstStr, len(coalesced)-len(payload))
		if pkt.ver == IPv4 {
			_ = w.sock.SendIPv4(out, pkt.dst)
		} else {
			_ = w.sock.SendIPv6(out, pkt.dst)
		}
		return 0

	case "fake":
		if voiceSet != nil {
			return vc.accept()
		}
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

func (w *Worker) rejectDatagram(vc *verdictCtx, pkt *pktInfo, quicFlow bool) int {
	if !vc.drop() {
		return 0
	}
	if pkt.ver == IPv4 {
		if icmp := sock.BuildICMPv4Reject(pkt.raw, pkt.src.To4(), pkt.dst.To4()); icmp != nil {
			_ = w.clientSender().SendIPv4(icmp, pkt.src)
		}
	} else {
		if icmp := sock.BuildICMPv6Reject(pkt.raw, pkt.src.To16(), pkt.dst.To16()); icmp != nil {
			_ = w.clientSender().SendIPv6(icmp, pkt.src)
		}
	}
	if quicFlow {
		vc.dropFlow()
	}
	return 0
}

func (w *Worker) blockUDP(vc *verdictCtx, pkt *pktInfo, cfg *config.Config, set *config.SetConfig, sport, dport uint16, sniTarget, host, ipTarget, udpTLS string, quicFlow bool) int {
	if config.NormalizeBlockAction(set.Routing.BlockAction) != config.BlockActionDrop {
		if pkt.ver == IPv4 {
			if icmp := sock.BuildICMPv4Reject(pkt.raw, pkt.src.To4(), pkt.dst.To4()); icmp != nil {
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
		recordBlockedFlow(cfg, pkt, sport, dport, set, blockedTarget)
	}
	vc.drop()
	if quicFlow {
		vc.dropFlow()
	}
	return 0
}

func (w *Worker) handleNfqError(e error) int {
	if errors.Is(e, syscall.ENOBUFS) {
		if countsTraffic(w.getConfig()) {
			queueOverflows.Add(1)
		}
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
	if errors.Is(e, syscall.ENOENT) {
		log.Tracef("nfq queue %d: a verdict arrived for a packet the kernel had already dropped: %v", w.qnum, e)
		return 0
	}
	log.Errorf("nfq: %v", e)
	return 0
}
