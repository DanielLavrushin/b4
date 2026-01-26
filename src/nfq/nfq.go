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

func (w *Worker) Start() error {
	cfg := w.getConfig()
	mark := cfg.Queue.Mark
	s, err := sock.NewSenderWithMark(int(mark))
	if err != nil {
		return err
	}
	w.sock = s

	c := nfqueue.Config{
		NfQueue:      w.qnum,
		MaxPacketLen: 0xffff,
		MaxQueueLen:  4096,
		Copymode:     nfqueue.NfQnlCopyPacket,
	}
	q, err := nfqueue.Open(&c)
	if err != nil {
		return err
	}
	w.q = q

	w.wg.Add(1)
	go w.gc(cfg)

	w.wg.Add(1)

	go func() {
		pid := os.Getpid()
		log.Tracef("NFQ bound pid=%d queue=%d", pid, w.qnum)
		defer w.wg.Done()
		_ = q.RegisterWithErrorFunc(w.ctx, func(a nfqueue.Attribute) int {
			cfg := w.getConfig()
			set := cfg.MainSet

			matcher := w.getMatcher()
			id := *a.PacketID

			if a.Mark != nil && *a.Mark == uint32(mark) {
				if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
					log.Tracef("failed to set verdict on packet %d: %v", id, err)
				}
				return 0
			}

			// Interface filtering
			if !w.matchesInterface(a) {
				if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
					log.Tracef("failed to set verdict on packet %d: %v", id, err)
				}
				return 0
			}

			select {
			case <-w.ctx.Done():
				return 0
			default:
			}

			atomic.AddUint64(&w.packetsProcessed, 1)

			if a.PacketID == nil || a.Payload == nil || len(*a.Payload) == 0 {
				if a.PacketID != nil && q != nil {
					if err := q.SetVerdict(*a.PacketID, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on invalid packet %d: %v", *a.PacketID, err)
					}
				}
				return 0
			}
			raw := *a.Payload

			v := raw[0] >> 4
			if v != IPv4 && v != IPv6 {
				if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
					log.Tracef("failed to set verdict on packet %d: %v", id, err)
				}
				return 0
			}
			var proto uint8
			var src, dst net.IP
			var ihl int
			if v == IPv4 {
				if len(raw) < 20 {
					if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}
				ihl = int(raw[0]&0x0f) * 4
				if len(raw) < ihl {
					if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}

				// Check for IP fragmentation
				fragOffset := binary.BigEndian.Uint16(raw[6:8]) & 0x1FFF
				moreFragments := (binary.BigEndian.Uint16(raw[6:8]) & 0x2000) != 0

				if fragOffset != 0 || moreFragments {
					if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to accept fragmented IPv4 packet %d: %v", id, err)
					}
					return 0
				}

				proto = raw[9]
				src = net.IP(raw[12:16])
				dst = net.IP(raw[16:20])

			} else {
				if len(raw) < IPv6HeaderLen {
					if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}
				ihl = IPv6HeaderLen
				nextHeader := raw[6]
				offset := 40

				for {
					switch nextHeader {
					case 0, 43, 60: // Hop-by-Hop, Routing, Destination Options
						if len(raw) < offset+2 {
							if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
								log.Tracef("failed to set verdict on packet %d: %v", id, err)
							}
							return 0
						}
						nextHeader = raw[offset]
						hdrLen := int(raw[offset+1])*8 + 8
						offset += hdrLen
					case 44:
						if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
							log.Tracef("failed to accept fragmented IPv6 packet %d: %v", id, err)
						}
						return 0
					default:
						goto done
					}
				}
			done:
				proto = nextHeader
				ihl = offset
				src = net.IP(raw[8:24])
				dst = net.IP(raw[24:40])
			}

			if src.IsLoopback() || dst.IsLoopback() {
				if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
					log.Tracef("failed to set verdict on packet %d: %v", id, err)
				}
				return 0
			}
			srcStr := src.String()
			dstStr := dst.String()

			srcMac := w.getMacByIp(srcStr)

			matched, st := matcher.MatchIP(dst)
			if matched {
				set = st
			}

			// TCP processing
			if proto == 6 && len(raw) >= ihl+TCPHeaderMinLen {
				tcp := raw[ihl:]
				if len(tcp) < TCPHeaderMinLen {
					if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}
				datOff := int((tcp[12]>>4)&0x0f) * 4
				if len(tcp) < datOff {
					if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}
				payload := tcp[datOff:]
				sport := binary.BigEndian.Uint16(tcp[0:2])
				dport := binary.BigEndian.Uint16(tcp[2:4])

				if sport == HTTPSPort {
					return w.HandleIncoming(q, id, v, raw, ihl, src, dstStr, dport, srcStr, sport, payload)
				}

				tcpFlags := tcp[13]
				isSyn := (tcpFlags & 0x02) != 0 // SYN flag
				isAck := (tcpFlags & 0x10) != 0 // ACK flag
				isRst := (tcpFlags & 0x04) != 0
				if isRst && dport == HTTPSPort {
					log.Tracef("RST received from %s:%d", dstStr, dport)
				}

				if isSyn && !isAck && dport == HTTPSPort && matched {
					log.Tracef("TCP SYN to %s:%d (set: %s)", dstStr, dport, set.Name)

					metrics := metrics.GetMetricsCollector()
					metrics.RecordConnection("TCP-SYN", "", srcStr, dstStr, true)

					if v == IPv4 {
						// Syndata - modify SYN packet to include payload
						modsyn := raw

						// SynFake - independent
						if set.TCP.SynFake {
							w.sendFakeSyn(set, raw, ihl, datOff)
						}

						if set.Fragmentation.Strategy != config.ConfigNone && set.Faking.TCPMD5 {
							w.sendFakeSynWithMD5(set, raw, ihl, dst)
						}

						_ = w.sock.SendIPv4(modsyn, dst)
					} else {
						if set.TCP.SynFake {
							w.sendFakeSynV6(set, raw, ihl, datOff)
						}

						if set.Fragmentation.Strategy != config.ConfigNone && set.Faking.TCPMD5 {
							w.sendFakeSynWithMD5V6(set, raw, dst)
						}

						_ = w.sock.SendIPv6(raw, dst)
					}

					if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
						log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
					}
					return 0
				}

				host := ""
				matchedIP := matched
				matchedSNI := false
				ipTarget := ""
				sniTarget := ""

				if dport == HTTPSPort && len(payload) > 0 {
					log.Tracef("TCP payload to %s: len=%d, first5=%x", dstStr, len(payload), payload[:min(5, len(payload))])
					if len(payload) >= 5 && payload[0] == 0x16 {
						log.Tracef("TLS record: type=%x ver=%x%x len=%d", payload[0], payload[1], payload[2],
							int(payload[3])<<8|int(payload[4]))
					}
					connKey := fmt.Sprintf("%s:%d->%s:%d", srcStr, sport, dstStr, dport)

					host, _ = sni.ParseTLSClientHelloSNI(payload)

					if captureManager := capture.GetManager(cfg); captureManager != nil {
						captureManager.CapturePayload(connKey, host, "tls", payload)
					}

					if host != "" {
						if mSNI, stSNI := matcher.MatchSNI(host); mSNI {
							matchedSNI = true
							matched = true
							set = stSNI
							// Learn IP-to-domain association for future UDP packets
							matcher.LearnIPToDomain(dst, host, stSNI)
						}
					}
				}

				if matchedIP {
					ipTarget = st.Name
				}
				if matchedSNI {
					sniTarget = set.Name
				}

				if !log.IsDiscoveryActive() {
					log.Infof(",TCP,%s,%s,%s:%d,%s,%s:%d,%s", sniTarget, host, srcStr, sport, ipTarget, dstStr, dport, srcMac)
				}

				if matched {
					metrics := metrics.GetMetricsCollector()
					metrics.RecordConnection("TCP", host, srcStr, dstStr, true)
					metrics.RecordPacket(uint64(len(raw)))

					if matched && set.TCP.Incoming.Mode != config.ConfigOff {
						connKey := fmt.Sprintf("%s:%d->%s:%d", srcStr, sport, dstStr, dport)
						connState.RegisterOutgoing(connKey, set)
					}

					packetCopy := make([]byte, len(raw))
					copy(packetCopy, raw)

					if set.TCP.DropSACK {
						if v == 4 {
							packetCopy = sock.StripSACKFromTCP(packetCopy)
						} else {
							packetCopy = sock.StripSACKFromTCPv6(packetCopy)
						}
					}

					dstCopy := make(net.IP, len(dst))
					copy(dstCopy, dst)
					setCopy := set

					if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
						log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
						return 0
					}

					w.wg.Add(1)
					go func(s *config.SetConfig, pkt []byte, d net.IP) {
						defer w.wg.Done()
						if v == 4 {
							w.dropAndInjectTCP(s, pkt, d)
						} else {
							w.dropAndInjectTCPv6(s, pkt, d)
						}
					}(setCopy, packetCopy, dstCopy)
					return 0
				}

				if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
					log.Tracef("failed to set verdict on packet %d: %v", id, err)
				}
				return 0
			}

			// UDP processing
			if proto == 17 && len(raw) >= ihl+8 {
				udp := raw[ihl:]
				if len(udp) < 8 {
					if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}

				payload := udp[8:]
				sport := binary.BigEndian.Uint16(udp[0:2])
				dport := binary.BigEndian.Uint16(udp[2:4])
				connKey := fmt.Sprintf("%s:%d->%s:%d", srcStr, sport, dstStr, dport)

				// Handle DNS packets
				if sport == 53 || dport == 53 {
					return w.processDnsPacket(v, sport, dport, payload, raw, ihl, id)
				}

				if utils.IsPrivateIP(dst) {
					if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}

				matchedIP := matched
				matchedQUIC := false
				isSTUN := false
				host := ""
				ipTarget := ""
				sniTarget := ""

				if matchedIP {
					ipTarget = st.Name
				}

				if !matchedIP {
					if mLearned, learnedSet, learnedDomain := matcher.MatchLearnedIP(dst); mLearned {
						matchedIP = true
						matched = true
						set = learnedSet
						host = learnedDomain
						sniTarget = learnedSet.Name
						ipTarget = learnedSet.Name
					}
				}

				isSTUN = stun.IsSTUNMessage(payload)

				if host == "" {
					if h, ok := sni.ParseQUICClientHelloSNI(payload); ok {
						host = h
					}
				}

				if host != "" {
					if mSNI, sniSet := matcher.MatchSNI(host); mSNI {
						matchedQUIC = true
						set = sniSet
						sniTarget = sniSet.Name
						matcher.LearnIPToDomain(dst, host, sniSet)
					}
				}

				if !matchedQUIC && matchedIP && set.UDP.FilterQUIC == "all" {
					if quic.IsInitial(payload) {
						matchedQUIC = true
					}
				}

				if captureManager := capture.GetManager(cfg); captureManager != nil {
					captureManager.CapturePayload(connKey, host, "quic", payload)
				}

				shouldHandle := (matchedIP || matchedQUIC) && !(isSTUN && set.UDP.FilterSTUN)

				matched = shouldHandle

				if !log.IsDiscoveryActive() {
					log.Infof(",UDP,%s,%s,%s:%d,%s,%s:%d,%s", sniTarget, host, srcStr, sport, ipTarget, dstStr, dport, srcMac)
				}

				if isSTUN && set.UDP.FilterSTUN {
					if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}

				if !shouldHandle {
					if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}

				metrics := metrics.GetMetricsCollector()
				metrics.RecordConnection("UDP", host, srcStr, dstStr, matched)
				metrics.RecordPacket(uint64(len(raw)))

				switch set.UDP.Mode {
				case "drop":
					if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
						log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
					}
					return 0

				case "fake":
					packetCopy := make([]byte, len(raw))
					copy(packetCopy, raw)
					dstCopy := make(net.IP, len(dst))
					copy(dstCopy, dst)
					setCopy := set

					if err := q.SetVerdict(id, nfqueue.NfDrop); err != nil {
						log.Tracef("failed to set drop verdict on UDP packet %d: %v", id, err)
						return 0
					}

					w.wg.Add(1)
					go func(s *config.SetConfig, pkt []byte, d net.IP) {
						defer w.wg.Done()
						if v == IPv4 {
							w.dropAndInjectQUIC(s, pkt, d)
						} else {
							w.dropAndInjectQUICV6(s, pkt, d)
						}
					}(setCopy, packetCopy, dstCopy)
					return 0

				default:
					if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}
			}

			if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
				log.Tracef("failed to set verdict on packet %d: %v", id, err)
			}
			return 0
		}, func(e error) int {
			if w.ctx.Err() != nil {

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
		})
	}()

	return nil
}

func (w *Worker) dropAndInjectQUIC(cfg *config.SetConfig, raw []byte, dst net.IP) {
	udpCfg := &cfg.UDP
	seg2d := udpCfg.Seg2Delay
	if udpCfg.Mode != "fake" {
		return
	}
	if udpCfg.FakeSeqLength > 0 {
		for i := 0; i < udpCfg.FakeSeqLength; i++ {
			fake, ok := sock.BuildFakeUDPFromOriginalV4(raw, udpCfg.FakeLen, cfg.Faking.TTL)
			if ok {
				if udpCfg.FakingStrategy == "checksum" {
					ipHdrLen := int((fake[0] & 0x0F) * 4)
					if len(fake) >= ipHdrLen+8 {
						fake[ipHdrLen+6] ^= 0xFF
						fake[ipHdrLen+7] ^= 0xFF
					}
				}
				_ = w.sock.SendIPv4(fake, dst)
				if seg2d > 0 {
					time.Sleep(time.Duration(seg2d) * time.Millisecond)
				}
			}
		}
	}

	// Try to locate SNI within encrypted QUIC payload
	splitPos := 24 // fallback
	ipHdrLen := int((raw[0] & 0x0F) * 4)
	if len(raw) >= ipHdrLen+8 {
		quicPayload := raw[ipHdrLen+8:] // skip IP + UDP headers
		sniOff, sniLen := quic.LocateSNIOffset(quicPayload)
		if sniOff > 0 && sniLen > 0 {
			// Split in middle of SNI
			splitPos = sniOff + sniLen/2
		}
	}

	frags, ok := sock.IPv4FragmentUDP(raw, splitPos)
	if !ok {
		_ = w.sock.SendIPv4(raw, dst)
		return
	}

	w.SendTwoSegmentsV4(frags[0], frags[1], dst, seg2d, cfg.Fragmentation.ReverseOrder)
}

func (w *Worker) dropAndInjectTCP(cfg *config.SetConfig, raw []byte, dst net.IP) {

	if len(raw) < 40 {
		_ = w.sock.SendIPv4(raw, dst)
		return
	}

	ipHdrLen := int((raw[0] & 0x0F) * 4)
	tcpHdrLen := int((raw[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen
	payloadLen := len(raw) - payloadStart

	if payloadLen <= 0 {
		_ = w.sock.SendIPv4(raw, dst)
		return
	}

	if cfg.Faking.SNIMutation.Mode != config.ConfigOff {
		raw = w.MutateClientHello(cfg, raw, dst)
	}

	if cfg.TCP.Desync.Mode != config.ConfigOff {
		w.ExecuteDesyncIPv4(cfg, raw, dst)
		time.Sleep(time.Duration(cfg.TCP.Seg2Delay) * time.Millisecond)
	}

	if cfg.TCP.Win.Mode != config.ConfigOff {
		w.ManipulateWindowIPv4(cfg, raw, dst)
	}

	if cfg.Faking.SNI && cfg.Faking.SNISeqLength > 0 {
		w.sendFakeSNISequence(cfg, raw, dst)
	}

	switch cfg.Fragmentation.Strategy {
	case "tcp":
		w.sendTCPFragments(cfg, raw, dst)
	case "ip":
		w.sendIPFragments(cfg, raw, dst)
	case "oob":
		w.sendOOBFragments(cfg, raw, dst)
	case "tls":
		w.sendTLSFragments(cfg, raw, dst)
	case "disorder":
		w.sendDisorderFragments(cfg, raw, dst)
	case "extsplit":
		w.sendExtSplitFragments(cfg, raw, dst)
	case "firstbyte":
		w.sendFirstByteDesync(cfg, raw, dst)
	case "combo":
		w.sendComboFragments(cfg, raw, dst)
	case "hybrid":
		w.sendHybridFragments(cfg, raw, dst)
	case config.ConfigNone:
		_ = w.sock.SendIPv4(raw, dst)
	default:
		w.sendComboFragments(cfg, raw, dst)
	}

	if cfg.TCP.Desync.PostDesync {
		time.Sleep(50 * time.Millisecond)
		w.sendPostDesyncRST(cfg, raw, ipHdrLen, dst)
	}
}

func (w *Worker) sendTCPFragments(cfg *config.SetConfig, packet []byte, dst net.IP) {

	seg2d := cfg.TCP.Seg2Delay
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	totalLen := len(packet)
	payloadStart := ipHdrLen + tcpHdrLen
	payloadLen := totalLen - payloadStart
	if payloadLen <= 0 {
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	payload := packet[payloadStart:]
	p1 := cfg.Fragmentation.SNIPosition
	validP1 := p1 > 0 && p1 < payloadLen

	p2 := -1
	if cfg.Fragmentation.MiddleSNI {
		if s, e, ok := locateSNI(payload); ok && e-s >= 4 {
			sniLen := e - s
			if sniLen > 30 {
				p2 = e - 12
			} else {
				p2 = s + sniLen/2
			}
		}
	}

	// Ensure p2 is within bounds
	if p2 >= payloadLen {
		p2 = payloadLen - 1
	}

	validP2 := p2 > 0 && p2 < payloadLen && (!validP1 || p2 != p1)

	if !validP1 && !validP2 {
		p1 = 1
		validP1 = p1 < payloadLen
	}

	if validP1 && validP2 && p2 < p1 {
		p1, p2 = p2, p1
	}

	if validP1 && validP2 {
		seg1Len := payloadStart + p1
		seg2Len := payloadStart + (p2 - p1)
		seg3Len := payloadStart + (payloadLen - p2)

		seg1 := make([]byte, seg1Len)
		copy(seg1, packet[:seg1Len])

		seg2 := make([]byte, seg2Len)
		copy(seg2[:payloadStart], packet[:payloadStart])
		copy(seg2[payloadStart:], payload[p1:p2])

		seg3 := make([]byte, seg3Len)
		copy(seg3[:payloadStart], packet[:payloadStart])
		copy(seg3[payloadStart:], payload[p2:])

		binary.BigEndian.PutUint16(seg1[2:4], uint16(seg1Len))
		sock.FixIPv4Checksum(seg1[:ipHdrLen])
		sock.FixTCPChecksum(seg1)

		seq0 := binary.BigEndian.Uint32(packet[ipHdrLen+4 : ipHdrLen+8])
		id0 := binary.BigEndian.Uint16(packet[4:6])

		binary.BigEndian.PutUint32(seg2[ipHdrLen+4:ipHdrLen+8], seq0+uint32(p1))
		binary.BigEndian.PutUint16(seg2[4:6], id0+1)
		binary.BigEndian.PutUint16(seg2[2:4], uint16(seg2Len))
		sock.FixIPv4Checksum(seg2[:ipHdrLen])
		sock.FixTCPChecksum(seg2)

		binary.BigEndian.PutUint32(seg3[ipHdrLen+4:ipHdrLen+8], seq0+uint32(p2))
		binary.BigEndian.PutUint16(seg3[4:6], id0+2)
		binary.BigEndian.PutUint16(seg3[2:4], uint16(seg3Len))
		sock.FixIPv4Checksum(seg3[:ipHdrLen])
		sock.FixTCPChecksum(seg3)

		if cfg.Fragmentation.ReverseOrder {
			_ = w.sock.SendIPv4(seg2, dst)
			if seg2d > 0 {
				time.Sleep(time.Duration(seg2d) * time.Millisecond)
			}
			_ = w.sock.SendIPv4(seg1, dst)
			if seg2d > 0 {
				time.Sleep(time.Duration(seg2d) * time.Millisecond)
			}
			_ = w.sock.SendIPv4(seg3, dst)
		} else {
			_ = w.sock.SendIPv4(seg1, dst)
			if seg2d > 0 {
				time.Sleep(time.Duration(seg2d) * time.Millisecond)
			}
			_ = w.sock.SendIPv4(seg2, dst)
			if seg2d > 0 {
				time.Sleep(time.Duration(seg2d) * time.Millisecond)
			}
			_ = w.sock.SendIPv4(seg3, dst)
		}
		return
	}

	splitPos := p1
	if !validP1 {
		splitPos = p2
	}
	seg1Len := payloadStart + splitPos
	seg1 := make([]byte, seg1Len)
	copy(seg1, packet[:seg1Len])

	seg2Len := payloadStart + (payloadLen - splitPos)
	seg2 := make([]byte, seg2Len)
	copy(seg2[:payloadStart], packet[:payloadStart])
	copy(seg2[payloadStart:], packet[payloadStart+splitPos:])

	binary.BigEndian.PutUint16(seg1[2:4], uint16(seg1Len))
	sock.FixIPv4Checksum(seg1[:ipHdrLen])
	sock.FixTCPChecksum(seg1)

	seq := binary.BigEndian.Uint32(seg2[ipHdrLen+4 : ipHdrLen+8])
	binary.BigEndian.PutUint32(seg2[ipHdrLen+4:ipHdrLen+8], seq+uint32(splitPos))
	id := binary.BigEndian.Uint16(seg1[4:6])
	binary.BigEndian.PutUint16(seg2[4:6], id+1)
	binary.BigEndian.PutUint16(seg2[2:4], uint16(seg2Len))
	sock.FixIPv4Checksum(seg2[:ipHdrLen])
	sock.FixTCPChecksum(seg2)

	w.SendTwoSegmentsV4(seg1, seg2, dst, seg2d, cfg.Fragmentation.ReverseOrder)
}

func (w *Worker) sendIPFragments(cfg *config.SetConfig, packet []byte, dst net.IP) {
	seg2d := cfg.TCP.Seg2Delay
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen
	payloadLen := len(packet) - payloadStart

	if payloadLen <= 0 {
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	payload := packet[payloadStart:]

	// Determine split position (relative to payload start)
	splitPos := cfg.Fragmentation.SNIPosition

	// Override with middle_sni if enabled and SNI found
	if cfg.Fragmentation.MiddleSNI {
		if s, e, ok := locateSNI(payload); ok && e-s >= 4 {
			sniLen := e - s
			if sniLen > 30 {
				splitPos = e - 12
			} else {
				splitPos = s + sniLen/2
			}
		}
	}

	if splitPos <= 0 || splitPos >= payloadLen {
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	// Convert to absolute position
	splitPos = payloadStart + splitPos

	// Align to 8-byte boundary (IP fragmentation requirement)
	dataLen := splitPos - ipHdrLen
	dataLen = (dataLen + 7) &^ 7
	splitPos = ipHdrLen + dataLen

	minSplitPos := ipHdrLen + 8
	if splitPos < minSplitPos {
		splitPos = minSplitPos
	}

	if splitPos >= len(packet) {
		splitPos = len(packet) - 8
		dataLen := splitPos - ipHdrLen
		dataLen = dataLen &^ 7
		splitPos = ipHdrLen + dataLen
		if splitPos < minSplitPos {
			_ = w.sock.SendIPv4(packet, dst)
			return
		}
	}

	frag1 := make([]byte, splitPos)
	copy(frag1, packet[:splitPos])
	frag1[6] |= 0x20
	binary.BigEndian.PutUint16(frag1[2:4], uint16(splitPos))
	sock.FixIPv4Checksum(frag1[:ipHdrLen])

	frag2Len := ipHdrLen + len(packet) - splitPos
	frag2 := make([]byte, frag2Len)
	copy(frag2, packet[:ipHdrLen])
	copy(frag2[ipHdrLen:], packet[splitPos:])
	fragOff := uint16(splitPos-ipHdrLen) / 8
	binary.BigEndian.PutUint16(frag2[6:8], fragOff)
	binary.BigEndian.PutUint16(frag2[2:4], uint16(frag2Len))
	sock.FixIPv4Checksum(frag2[:ipHdrLen])

	w.SendTwoSegmentsV4(frag1, frag2, dst, seg2d, cfg.Fragmentation.ReverseOrder)
}

func (w *Worker) sendFakeSNISequence(cfg *config.SetConfig, original []byte, dst net.IP) {
	fk := &cfg.Faking
	if !fk.SNI || fk.SNISeqLength <= 0 {
		return
	}

	fake := sock.BuildFakeSNIPacketV4(original, cfg)
	ipHdrLen := int((fake[0] & 0x0F) * 4)
	tcpHdrLen := int((fake[ipHdrLen+12] >> 4) * 4)

	for i := 0; i < fk.SNISeqLength; i++ {
		_ = w.sock.SendIPv4(fake, dst)

		// Update for next iteration
		if i+1 < fk.SNISeqLength {
			// Increment IP ID
			id := binary.BigEndian.Uint16(fake[4:6])
			binary.BigEndian.PutUint16(fake[4:6], id+1)

			// Adjust sequence number for non-past/rand strategies
			if fk.Strategy != "pastseq" && fk.Strategy != "randseq" {
				payloadLen := len(fake) - (ipHdrLen + tcpHdrLen)
				seq := binary.BigEndian.Uint32(fake[ipHdrLen+4 : ipHdrLen+8])
				binary.BigEndian.PutUint32(fake[ipHdrLen+4:ipHdrLen+8], seq+uint32(payloadLen))
				sock.FixIPv4Checksum(fake[:ipHdrLen])
				sock.FixTCPChecksum(fake)
			}
		}
	}
}

func (w *Worker) getMacByIp(ip string) string {

	if ipToMac := w.ipToMac.Load(); ipToMac != nil {
		return ipToMac.(map[string]string)[ip]
	}
	return ""
}

func (w *Worker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	if w.q != nil {
		_ = w.q.Close()
	}
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	if w.sock != nil {
		w.sock.Close()
	}
}

func (w *Worker) gc(cfg *config.Config) {
	defer w.wg.Done()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-t.C:
			connState.Cleanup()

			if cfg.System.WebServer.IsEnabled {
				mtcs := metrics.GetMetricsCollector()
				workerID := int(w.qnum - uint16(cfg.Queue.StartNum))
				processed := atomic.LoadUint64(&w.packetsProcessed)
				mtcs.UpdateSingleWorker(workerID, "active", processed)
			}
		}
	}
}

func (w *Worker) GetStats() (uint64, string) {
	return atomic.LoadUint64(&w.packetsProcessed), "active"
}
