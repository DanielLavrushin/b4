package nfq

import (
	"encoding/binary"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/quic"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
	"github.com/mdlayher/netlink"
)

// Support kernels < 3.8 by performing NFQUEUE PF_UNBIND/PF_BIND for the given family.
func pfBind(con *netlink.Conn, family uint8) error {
	const (
		nfnlSubSysQueue      = 0x03
		nfQnlMsgConfig       = 2
		nfQaCfgCmd           = 1
		nfUlnlCfgCmdPfUnbind = 4
		nfUlnlCfgCmdPfBind   = 3
		nfnetlinkV0          = 0
	)

	for _, cmd := range []byte{nfUlnlCfgCmdPfUnbind, nfUlnlCfgCmdPfBind} {
		attrs, err := netlink.MarshalAttributes([]netlink.Attribute{
			{Type: nfQaCfgCmd, Data: []byte{cmd, 0x0, 0x0, family}},
		})
		if err != nil {
			return err
		}
		hdr := make([]byte, 2)
		binary.BigEndian.PutUint16(hdr, 0)
		data := append([]byte{syscall.AF_UNSPEC, nfnetlinkV0}, hdr...)
		data = append(data, attrs...)

		req := netlink.Message{
			Header: netlink.Header{
				Type:  netlink.HeaderType((nfnlSubSysQueue << 8) | nfQnlMsgConfig),
				Flags: netlink.Request | netlink.Acknowledge,
			},
			Data: data,
		}
		reply, err := con.Execute(req)
		if err != nil {
			return err
		}
		if err := netlink.Validate(req, reply); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) Start() error {
	cfg := w.getConfig()
	mark := cfg.Queue.Mark
	s, err := sock.NewSenderWithMark(int(mark))
	if err != nil {
		return err
	}
	w.sock = s
	cs, err := sock.NewSenderWithMark(int(mark) | engine.ClientMark)
	if err != nil {
		s.Close()
		return err
	}
	w.clientSock = cs

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

	if cfg.Queue.IPv4Enabled {
		if err := pfBind(q.Con, syscall.AF_INET); err != nil {
			log.Warnf("nfqueue PF_BIND AF_INET: %v", err)
		}
	}
	if cfg.Queue.IPv6Enabled {
		if err := pfBind(q.Con, syscall.AF_INET6); err != nil {
			log.Warnf("nfqueue PF_BIND AF_INET6: %v", err)
		}
	}

	w.wg.Add(1)
	go w.gc(cfg)

	w.wg.Add(1)
	go func() {
		pid := os.Getpid()
		log.Tracef("NFQ bound pid=%d queue=%d", pid, w.qnum)
		defer w.wg.Done()
		_ = q.RegisterWithErrorFunc(w.ctx,
			func(a nfqueue.Attribute) int {
				return w.handlePacket(q, a, mark)
			},
			func(e error) int {
				return w.handleNfqError(e)
			},
		)
	}()

	return nil
}

func (w *Worker) dropAndInjectQUIC(cfg *config.SetConfig, raw []byte, dst net.IP) {
	udpCfg := &cfg.UDP
	seg2d := config.ResolveSeg2Delay(udpCfg.Seg2Delay, udpCfg.Seg2DelayMax)
	if udpCfg.Mode != "fake" {
		return
	}
	if udpCfg.FakeSeqLength > 0 {
		for i := 0; i < udpCfg.FakeSeqLength; i++ {
			payload := udpCfg.FakePayloadData
			if udpCfg.FakePayloadFile == config.FakePayloadAutoQUIC {
				payload = sock.BuildQUICInitial(udpCfg.FakeLen)
			}
			fake, ok := sock.BuildFakeUDPFromOriginalV4(raw, udpCfg.FakeLen, cfg.Faking.TTL, payload)
			if ok {
				if udpCfg.FakingStrategy == "checksum" {
					ipHdrLen := int((fake[0] & 0x0F) * 4)
					if len(fake) >= ipHdrLen+8 {
						fake[ipHdrLen+6] ^= 0xFF
						fake[ipHdrLen+7] ^= 0xFF
					}
				}
				log.Tracef("Sending fake UDP packet %d/%d to %s", i+1, udpCfg.FakeSeqLength, dst.String())
				_ = w.sock.SendIPv4(fake, dst)
				if seg2d > 0 {
					time.Sleep(time.Duration(seg2d) * time.Millisecond)
				}
			}
		}
	}

	ipHdrLen := int((raw[0] & 0x0F) * 4)
	var realPayload []byte
	if len(raw) >= ipHdrLen+8 {
		realPayload = raw[ipHdrLen+8:]
	}
	if !quic.LooksLikeQUIC(realPayload) {
		log.Tracef("Packet does not look like QUIC for %s, sending whole packet", dst.String())
		_ = w.sock.SendIPv4(raw, dst)
		return
	}

	splitPos := 24
	if sniOff, sniLen := quic.LocateSNIOffset(realPayload); sniOff > 0 && sniLen > 0 {
		splitPos = sniOff + sniLen/2
	}

	frags, ok := sock.IPv4FragmentUDP(raw, splitPos)
	if !ok {
		log.Warnf("Failed to fragment QUIC packet for %s, sending whole packet", dst.String())
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
		time.Sleep(time.Duration(config.ResolveSeg2Delay(cfg.TCP.Seg2Delay, cfg.TCP.Seg2DelayMax)) * time.Millisecond)
	}

	if cfg.TCP.Win.Mode != config.ConfigOff {
		w.ManipulateWindowIPv4(cfg, raw, dst)
	}

	if cfg.Faking.SNI && cfg.Faking.SNISeqLength > 0 {
		w.sendFakeSNISequence(cfg, raw, dst)
	}

	strategy := config.ResolveStrategyPool(cfg.Fragmentation.StrategyPool, cfg.Fragmentation.Strategy)

	log.Tracef("Sending TCP packet with fragmentation strategy: %s", strategy)
	switch strategy {
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

	seg2d := config.ResolveSeg2Delay(cfg.TCP.Seg2Delay, cfg.TCP.Seg2DelayMax)
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
	p1 := config.ResolveRange(cfg.Fragmentation.SNIPosition, cfg.Fragmentation.SNIPositionMax)
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
			log.Tracef("Sending TCP fragments in reverse order to %s: seg3=%d, seg2=%d, seg1=%d", dst.String(), seg3Len, seg2Len, seg1Len)
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
			log.Tracef("Sending TCP fragments to %s: seg1=%d, seg2=%d, seg3=%d", dst.String(), seg1Len, seg2Len, seg3Len)
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
	seg2d := config.ResolveSeg2Delay(cfg.TCP.Seg2Delay, cfg.TCP.Seg2DelayMax)
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen
	payloadLen := len(packet) - payloadStart

	if payloadLen <= 0 {
		log.Tracef("No payload to fragment for %s, sending whole packet", dst.String())
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	payload := packet[payloadStart:]

	splitPos := config.ResolveRange(cfg.Fragmentation.SNIPosition, cfg.Fragmentation.SNIPositionMax)

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
		log.Warnf("Invalid split position for %s: %d", dst.String(), splitPos)
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	// Adjust splitPos to be relative to IP payload (not TCP payload)
	adjustedSplit := tcpHdrLen + splitPos

	fragments, ok := sock.IPv4FragmentPacket(packet, adjustedSplit)
	if !ok {
		log.Warnf("Failed to fragment packet for %s, sending whole packet", dst.String())
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	w.SendTwoSegmentsV4(fragments[0], fragments[1], dst, seg2d, cfg.Fragmentation.ReverseOrder)
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
		log.Tracef("Sending fake SNI packet %d/%d to %s", i+1, fk.SNISeqLength, dst.String())
		_ = w.sock.SendIPv4(fake, dst)

		if i+1 < fk.SNISeqLength {
			id := binary.BigEndian.Uint16(fake[4:6])
			binary.BigEndian.PutUint16(fake[4:6], id+1)

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
	if w.clientSock != nil && w.clientSock != w.sock {
		w.clientSock.Close()
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
			if w.connTracker != nil {
				w.connTracker.Cleanup()
			}
			_ = cleanupDNSPendingRoutes(time.Now())

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
