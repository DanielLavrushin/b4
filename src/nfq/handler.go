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
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/quic"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/stun"
	"github.com/daniellavrushin/b4/utils"
	"github.com/florianl/go-nfqueue"
)

// pktInfo holds parsed IP-layer information for a packet being processed.
type pktInfo struct {
	raw    []byte
	ver    uint8
	proto  uint8
	src    net.IP
	dst    net.IP
	srcStr string
	dstStr string
	srcMac string
	ihl    int
}

// accept sets an NfAccept verdict on a packet.
func accept(q *nfqueue.Nfqueue, id uint32) int {
	if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
		log.Tracef("failed to set verdict on packet %d: %v", id, err)
	}
	return 0
}

// handlePacket is the main NFQueue packet handler callback.
func (w *Worker) handlePacket(q *nfqueue.Nfqueue, a nfqueue.Attribute, mark uint) int {
	cfg := w.getConfig()
	matcher := w.getMatcher()

	if a.PacketID == nil || a.Payload == nil || len(*a.Payload) == 0 {
		if a.PacketID != nil && q != nil {
			if err := q.SetVerdict(*a.PacketID, nfqueue.NfAccept); err != nil {
				log.Tracef("failed to set verdict on invalid packet %d: %v", *a.PacketID, err)
			}
		}
		return 0
	}

	id := *a.PacketID

	if a.Mark != nil && *a.Mark == uint32(mark) {
		return accept(q, id)
	}

	if !w.matchesInterface(a) {
		return accept(q, id)
	}

	select {
	case <-w.ctx.Done():
		return 0
	default:
	}

	atomic.AddUint64(&w.packetsProcessed, 1)

	pkt, ok := w.parseIPHeaders(*a.Payload)
	if !ok {
		return accept(q, id)
	}

	matched, st := matcher.MatchIPWithSource(pkt.dst, pkt.srcMac)
	var set *config.SetConfig
	if matched {
		set = st
	}

	switch pkt.proto {
	case 6: // TCP
		if len(pkt.raw) >= pkt.ihl+TCPHeaderMinLen {
			return w.handleTCPPacket(q, id, pkt, cfg, matcher, matched, set, st)
		}
	case 17: // UDP
		if len(pkt.raw) >= pkt.ihl+UDPHeaderLen {
			return w.handleUDPPacket(q, id, pkt, cfg, matcher, matched, set, st)
		}
	}

	return accept(q, id)
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

// parseIPHeaders parses IP version, protocol, addresses and header length.
// Returns nil, false if the packet should be accepted without processing.
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
		p.src = net.IP(raw[12:16])
		p.dst = net.IP(raw[16:20])
		p.ihl = ihl
	} else {
		if len(raw) < IPv6HeaderLen {
			return nil, false
		}
		nextHeader := raw[6]
		offset := 40

		for {
			switch nextHeader {
			case 0, 43, 60:
				if len(raw) < offset+2 {
					return nil, false
				}
				nextHeader = raw[offset]
				hdrLen := int(raw[offset+1])*8 + 8
				offset += hdrLen
			case 44:
				return nil, false
			default:
				goto done
			}
		}
	done:
		p.proto = nextHeader
		p.ihl = offset
		p.src = net.IP(raw[8:24])
		p.dst = net.IP(raw[24:40])
	}

	if p.src.IsLoopback() || p.dst.IsLoopback() {
		return nil, false
	}

	p.srcStr = p.src.String()
	p.dstStr = p.dst.String()
	p.srcMac = w.getMacByIp(p.srcStr)

	return p, true
}

// handleTCPPacket processes TCP packets: matching, logging, metrics, and dispatch.
func (w *Worker) handleTCPPacket(q *nfqueue.Nfqueue, id uint32, pkt *pktInfo, cfg *config.Config, matcher *sni.SuffixSet, matched bool, set *config.SetConfig, st *config.SetConfig) int {
	tcp := pkt.raw[pkt.ihl:]
	if len(tcp) < TCPHeaderMinLen {
		return accept(q, id)
	}
	datOff := int((tcp[12]>>4)&0x0f) * 4
	if len(tcp) < datOff {
		return accept(q, id)
	}
	payload := tcp[datOff:]
	sport := binary.BigEndian.Uint16(tcp[0:2])
	dport := binary.BigEndian.Uint16(tcp[2:4])

	if cfg.IsTCPPort(sport) {
		return w.HandleIncoming(q, id, pkt.ver, pkt.raw, pkt.ihl, pkt.src, pkt.dstStr, dport, pkt.srcStr, sport, payload)
	}

	// If IP matched but set has a port filter, verify port matches (AND logic)
	if matched && !set.MatchesTCPDPort(dport) {
		matched = false
		set = nil
	}

	matchedLearned := false
	if mLearned, learnedSet, _ := matcher.MatchLearnedIPWithSource(pkt.dst, pkt.srcMac); mLearned {
		if learnedSet.MatchesTCPDPort(dport) {
			matched = true
			set = learnedSet
			st = learnedSet
			matchedLearned = true
		}
	}

	if !matched && cfg.IsTCPPort(dport) {
		if portMatched, portSet := matcher.MatchTCPPort(dport); portMatched {
			matched = true
			set = portSet
		}
	}

	if matched && cfg.IsTCPPort(dport) && set.TCP.Duplicate.Enabled && set.TCP.Duplicate.Count > 0 {
		log.Tracef("TCP duplicate to %s:%d (%d copies, set: %s)", pkt.dstStr, dport, set.TCP.Duplicate.Count, set.Name)

		dupConnKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)
		dupHost, dupTLS, _ := w.tlsCache.Lookup(dupConnKey)

		m := metrics.GetMetricsCollector()
		m.RecordConnection("TCP-DUP", dupHost, pkt.srcStr, pkt.dstStr, true, pkt.srcMac, set.Name, config.TLSVersionString(dupTLS))
		m.RecordPacket(uint64(len(pkt.raw)))

		if !cfg.Queue.IsDiscovery {
			log.LogConnection("TCP", "", dupHost, pkt.srcStr, sport, set.Name, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(dupTLS), "tcp-dup")
		}

		if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
			log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
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
		log.Tracef("RST received from %s:%d", pkt.dstStr, dport)
	}

	if isSyn && !isAck && cfg.IsTCPPort(dport) && matched && !set.TCP.Duplicate.Enabled && needsTCPSynInjection(set) {
		log.Tracef("TCP SYN to %s:%d (set: %s)", pkt.dstStr, dport, set.Name)

		m := metrics.GetMetricsCollector()
		m.RecordConnection("TCP-SYN", "", pkt.srcStr, pkt.dstStr, true, pkt.srcMac, set.Name, "")

		if pkt.ver == IPv4 {
			if set.TCP.SynFake {
				w.sendFakeSyn(set, pkt.raw, pkt.ihl, datOff)
			}
			if set.Fragmentation.Strategy != config.ConfigNone && set.Faking.TCPMD5 {
				w.sendFakeSynWithMD5(set, pkt.raw, pkt.ihl, pkt.dst)
			}
			_ = w.sock.SendIPv4(pkt.raw, pkt.dst)
		} else {
			if set.TCP.SynFake {
				w.sendFakeSynV6(set, pkt.raw, pkt.ihl, datOff)
			}
			if set.Fragmentation.Strategy != config.ConfigNone && set.Faking.TCPMD5 {
				w.sendFakeSynWithMD5V6(set, pkt.raw, pkt.dst)
			}
			_ = w.sock.SendIPv6(pkt.raw, pkt.dst)
		}

		if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
			log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
		}
		return 0
	}

	host := ""
	isClientHello := false
	var tlsVersion uint16
	matchedIP := st != nil
	matchedSNI := false
	ipTarget := ""
	sniTarget := ""

	// Show port-matched set name in log
	if !matchedIP && matched && set != nil {
		ipTarget = set.Name
	}

	if cfg.IsTCPPort(dport) && len(payload) > 0 {
		log.Tracef("TCP payload to %s: len=%d, first5=%x", pkt.dstStr, len(payload), payload[:min(5, len(payload))])
		if len(payload) >= 5 && payload[0] == 0x16 {
			log.Tracef("TLS record: type=%x ver=%x%x len=%d", payload[0], payload[1], payload[2],
				int(payload[3])<<8|int(payload[4]))
		}
		connKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)

		host, tlsVersion, _ = sni.ParseTLSClientHelloSNI(payload)
		isClientHello = host != ""

		if host != "" && tlsVersion != 0 {
			w.tlsCache.Store(connKey, host, tlsVersion)
		}

		if captureManager := capture.GetManager(cfg); captureManager != nil {
			captureManager.CapturePayload(connKey, host, "tls", payload)
		}

		if host != "" {
			if mSNI, stSNI := matcher.MatchSNIWithSourceTLS(host, pkt.srcMac, tlsVersion); mSNI {
				// If SNI-matched set has a port filter, verify port matches (AND logic)
				if stSNI.MatchesTCPDPort(dport) {
					matchedSNI = true
					matched = true
					set = stSNI
					matcher.LearnIPToDomain(pkt.dst, host, stSNI)
				}
			}
		}

		// If IP-matched set has a TLS version filter that doesn't match, clear it
		if matched && !matchedSNI && set != nil && !set.MatchesTLSVersion(tlsVersion) {
			matched = false
			set = nil
		}

		if matchedLearned && !matchedSNI && !(len(payload) >= 1 && payload[0] == 0x16) {
			if set != nil && set.Fragmentation.Strategy == config.ConfigNone && len(set.Fragmentation.StrategyPool) == 0 && set.TCP.Desync.Mode == config.ConfigOff {
				matched = false
				set = nil
			}
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

	if matchedSNI {
		sniTarget = set.Name
	} else if matchedIP {
		ipTarget = st.Name
	}

	if matched && isClientHello && host != "" && cfg.IsTCPPort(dport) {
		if escId, _, ok := w.destState.GetEscalation(host); ok {
			if escSet := cfg.GetSetById(escId); escSet != nil && escSet.Enabled {
				log.Tracef("escalation hit for %s: %s -> %s", host, set.Name, escSet.Name)
				set = escSet
				if sniTarget != "" {
					sniTarget = set.Name
				}
				if ipTarget != "" {
					ipTarget = set.Name
				}
			} else {
				w.destState.ClearEscalation(host)
			}
		}
	}

	if matched && isClientHello && set.TCP.IPBlockDetect.Enabled && host != "" && cfg.IsTCPPort(dport) {
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
			if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
				log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
			}
			return 0
		}
	}

	if !cfg.Queue.IsDiscovery {
		log.LogConnection("TCP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(tlsVersion), "")
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

	if matched && set != nil && set.Routing.Enabled && config.RoutingUsesTProxy(set.Routing.Mode) {
		return accept(q, id)
	}

	if matched {
		ibdOn := set.TCP.IPBlockDetect.Enabled
		canEscalate := set.Escalate.To != ""
		if isClientHello && (ibdOn || canEscalate) && host != "" && cfg.IsTCPPort(dport) {
			ibd := &set.TCP.IPBlockDetect
			dstIPPort := fmt.Sprintf("%s:%d", pkt.dstStr, dport)
			ibConnKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)

			count, firstSeen := w.destState.RecordClientHello(ibConnKey, host)
			threshold := ibd.RetransmitThreshold
			if threshold <= 0 {
				threshold = 3
			}
			timeout := time.Duration(ibd.TimeoutMs) * time.Millisecond
			if timeout <= 0 {
				timeout = 3000 * time.Millisecond
			}

			if count >= threshold || (count > 1 && time.Since(firstSeen) > timeout) {
				if canEscalate {
					if next := cfg.GetSetById(set.Escalate.To); next != nil && next.Enabled {
						ttl := time.Duration(set.Escalate.TtlSec) * time.Second
						if w.destState.SetEscalation(host, next.Id, ttl) {
							metrics.GetMetricsCollector().RecordEscalation()
							registerEscalatedRoute(cfg, next, pkt.dst)
							if !cfg.Queue.IsDiscovery {
								log.LogConnection("TCP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, config.TLSVersionString(tlsVersion), "ipblock-escalate->"+next.Name)
							}
							if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
								log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
							}
							return 0
						}
						log.Warnf("escalation hop cap reached for %s (chain stopped at %s)", host, set.Name)
					}
				}
				if ibdOn {
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
					if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
						log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
					}
					return 0
				}
			}
		}

		if set.TCP.Incoming.Mode != config.ConfigOff || set.TCP.RSTProtection.Enabled || set.Escalate.To != "" {
			connKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)
			w.connTracker.RegisterOutgoing(connKey, set)
		}

		if !needsTCPInjection(set) {
			return accept(q, id)
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

		if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
			log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
			return 0
		}

		v := pkt.ver
		w.wg.Add(1)
		go func(s *config.SetConfig, pktData []byte, d net.IP) {
			defer w.wg.Done()
			if v == 4 {
				w.dropAndInjectTCP(s, pktData, d)
			} else {
				w.dropAndInjectTCPv6(s, pktData, d)
			}
		}(setCopy, packetCopy, dstCopy)
		return 0
	}

	return accept(q, id)
}

// handleUDPPacket processes UDP packets: DNS, QUIC, STUN filtering, and dispatch.
func (w *Worker) handleUDPPacket(q *nfqueue.Nfqueue, id uint32, pkt *pktInfo, cfg *config.Config, matcher *sni.SuffixSet, matched bool, set *config.SetConfig, st *config.SetConfig) int {
	udp := pkt.raw[pkt.ihl:]
	if len(udp) < UDPHeaderLen {
		return accept(q, id)
	}

	payload := udp[8:]
	sport := binary.BigEndian.Uint16(udp[0:2])
	dport := binary.BigEndian.Uint16(udp[2:4])
	connKey := fmt.Sprintf(connKeyFormat, pkt.srcStr, sport, pkt.dstStr, dport)

	if sport == 53 || dport == 53 {
		return w.processDnsPacket(pkt.ver, sport, dport, payload, pkt.raw, pkt.ihl, id, pkt.srcMac)
	}

	if utils.IsPrivateIP(pkt.dst) {
		return accept(q, id)
	}

	matchedIP := st != nil
	matchedQUIC := false
	isSTUN := false
	host := ""
	ipTarget := ""
	sniTarget := ""

	// If IP matched but set has a port filter, verify port matches (AND logic)
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
				matched = true
				set = learnedSet
				host = learnedDomain
				sniTarget = learnedSet.Name
				ipTarget = learnedSet.Name
			}
		}
	}

	// If IP matching didn't find a set, try UDP port-based set matching
	matchedPort := false
	if !matched {
		if portMatched, portSet := matcher.MatchUDPPort(dport); portMatched {
			matchedPort = true
			matched = true
			set = portSet
			ipTarget = portSet.Name
		}
	}

	isSTUN = stun.IsSTUNMessage(payload)

	isQUIC := quic.LooksLikeQUIC(payload)

	if host == "" && isQUIC {
		if h, ok := sni.ParseQUICClientHelloSNI(payload); ok {
			host = h
		}
	}

	if host != "" {
		if mSNI, sniSet := matcher.MatchSNIWithSourceTLS(host, pkt.srcMac, 0x0304); mSNI { // QUIC is always TLS 1.3
			// If SNI-matched set has a port filter, verify port matches (AND logic)
			if sniSet.MatchesUDPDPort(dport) {
				matchedQUIC = true
				set = sniSet
				sniTarget = sniSet.Name
				matcher.LearnIPToDomain(pkt.dst, host, sniSet)
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

	shouldHandle := (matchedIP || matchedQUIC || matchedPort) && !(isSTUN && set.UDP.FilterSTUN)

	matched = shouldHandle

	udpTLS := ""
	if matchedQUIC || isQUIC {
		udpTLS = "1.3" // QUIC is always TLS 1.3
	}

	if shouldHandle && set != nil && host != "" {
		if escId, _, ok := w.destState.GetEscalation(host); ok {
			if escSet := cfg.GetSetById(escId); escSet != nil && escSet.Enabled {
				log.Tracef("UDP escalation hit for %s: %s -> %s", host, set.Name, escSet.Name)
				set = escSet
				if sniTarget != "" {
					sniTarget = set.Name
				}
				if ipTarget != "" {
					ipTarget = set.Name
				}
			} else {
				w.destState.ClearEscalation(host)
			}
		}
	}

	if !cfg.Queue.IsDiscovery {
		log.LogConnection("UDP", sniTarget, host, pkt.srcStr, sport, ipTarget, pkt.dstStr, dport, pkt.srcMac, udpTLS, "")
	}

	if isSTUN && set != nil && set.UDP.FilterSTUN {
		return accept(q, id)
	}

	if !shouldHandle {
		m := metrics.GetMetricsCollector()
		m.RecordConnection("UDP", host, pkt.srcStr, pkt.dstStr, false, pkt.srcMac, "", udpTLS)
		m.RecordPacket(uint64(len(pkt.raw)))
		return accept(q, id)
	}

	m := metrics.GetMetricsCollector()
	setName := ""
	if matched {
		setName = set.Name
	}
	m.RecordConnection("UDP", host, pkt.srcStr, pkt.dstStr, matched, pkt.srcMac, setName, udpTLS)
	m.RecordPacket(uint64(len(pkt.raw)))

	switch set.UDP.Mode {
	case "drop":
		if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
			log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
		}
		return 0

	case "reject":
		if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
			log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
			return 0
		}
		if pkt.ver == IPv4 {
			if icmp := sock.BuildICMPv4Reject(pkt.raw, pkt.src.To4(), pkt.dst.To4()); icmp != nil {
				_ = w.sock.SendIPv4(icmp, pkt.src)
			}
		} else {
			if icmp := sock.BuildICMPv6Reject(pkt.raw, pkt.src.To16(), pkt.dst.To16()); icmp != nil {
				_ = w.sock.SendIPv6(icmp, pkt.src)
			}
		}
		return 0

	case "fake":
		packetCopy := make([]byte, len(pkt.raw))
		copy(packetCopy, pkt.raw)
		dstCopy := make(net.IP, len(pkt.dst))
		copy(dstCopy, pkt.dst)
		setCopy := set

		if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
			log.Tracef("failed to set drop verdict on UDP packet %d: %v", id, err)
			return 0
		}

		v := pkt.ver
		w.wg.Add(1)
		go func(s *config.SetConfig, p []byte, d net.IP) {
			defer w.wg.Done()
			if v == IPv4 {
				w.dropAndInjectQUIC(s, p, d)
			} else {
				w.dropAndInjectQUICV6(s, p, d)
			}
		}(setCopy, packetCopy, dstCopy)
		return 0

	default:
		return accept(q, id)
	}
}

// handleNfqError handles errors from the NFQueue subsystem.
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
