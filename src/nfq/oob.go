package nfq

import (
	"encoding/binary"
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

func (w *Worker) sendOOBFragments(cfg *config.SetConfig, packet []byte, dst net.IP) {
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	if len(packet) < ipHdrLen+20 {
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen
	payloadLen := len(packet) - payloadStart

	if payloadLen <= 0 {
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	// Determine split position
	oobPos := config.ResolveRange(cfg.Fragmentation.OOBPosition, cfg.Fragmentation.OOBPositionMax)
	if oobPos <= 0 {
		oobPos = 1
	}

	// Handle middle SNI positioning
	if cfg.Fragmentation.MiddleSNI {
		if sniStart, sniEnd, ok := locateSNI(packet[payloadStart:]); ok && sniEnd > sniStart {
			oobPos = sniStart + (sniEnd-sniStart)/2
			log.Tracef("OOB: SNI at %d-%d, injecting at %d", sniStart, sniEnd, oobPos)
		}
	}

	// Clamp to valid range
	if oobPos >= payloadLen {
		oobPos = payloadLen / 2
	}
	if oobPos <= 0 {
		oobPos = 1
	}

	oobChar := cfg.Fragmentation.OOBChar
	if oobChar == 0 {
		oobChar = 'x'
	}

	seq := binary.BigEndian.Uint32(packet[ipHdrLen+4 : ipHdrLen+8])
	id := binary.BigEndian.Uint16(packet[4:6])
	payload := packet[payloadStart:]
	seg2delay := config.ResolveSeg2Delay(cfg.TCP.Seg2Delay, cfg.TCP.Seg2DelayMax)

	log.Tracef("OOB: Injecting fake 0x%02x at pos %d of %d bytes", oobChar, oobPos, payloadLen)

	// ===== Segment 1: payload[0:oobPos] - CLEAN, no OOB =====
	seg1Len := payloadStart + oobPos
	seg1 := make([]byte, seg1Len)
	copy(seg1[:payloadStart], packet[:payloadStart])
	copy(seg1[payloadStart:], payload[:oobPos])

	binary.BigEndian.PutUint16(seg1[2:4], uint16(seg1Len))
	sock.FixIPv4Checksum(seg1[:ipHdrLen])
	sock.FixTCPChecksum(seg1)

	// ===== Fake OOB packet: single byte with URG, LOW TTL =====
	fakeLen := payloadStart + 1
	fake := make([]byte, fakeLen)
	copy(fake[:payloadStart], packet[:payloadStart])
	fake[payloadStart] = oobChar

	// Set sequence to where this byte would be
	binary.BigEndian.PutUint32(fake[ipHdrLen+4:ipHdrLen+8], seq+uint32(oobPos))

	// Set URG flag and urgent pointer
	fake[ipHdrLen+13] |= 0x20                                    // URG flag
	binary.BigEndian.PutUint16(fake[ipHdrLen+18:ipHdrLen+20], 1) // Urgent pointer = 1

	// LOW TTL so it doesn't reach server, only DPI sees it
	fake[8] = cfg.Faking.TTL
	if fake[8] == 0 {
		fake[8] = 3
	}

	// Update IP ID and length
	binary.BigEndian.PutUint16(fake[4:6], id+1)
	binary.BigEndian.PutUint16(fake[2:4], uint16(fakeLen))

	sock.FixIPv4Checksum(fake[:ipHdrLen])
	sock.FixTCPChecksum(fake)

	// Optionally corrupt checksum based on faking strategy
	switch cfg.Faking.Strategy {
	case "tcp_check":
		corruptTCPChecksum(fake, ipHdrLen)
	case "md5sum":
		corruptTCPChecksum(fake, ipHdrLen)
		fake[10] ^= 0xFF
	}

	// ===== Segment 2: payload[oobPos:] - CLEAN =====
	seg2DataLen := payloadLen - oobPos
	seg2Len := payloadStart + seg2DataLen
	seg2 := make([]byte, seg2Len)
	copy(seg2[:payloadStart], packet[:payloadStart])
	copy(seg2[payloadStart:], payload[oobPos:])

	// Sequence continues from where seg1 ended (no gap for fake)
	binary.BigEndian.PutUint32(seg2[ipHdrLen+4:ipHdrLen+8], seq+uint32(oobPos))
	binary.BigEndian.PutUint16(seg2[4:6], id+2)
	binary.BigEndian.PutUint16(seg2[2:4], uint16(seg2Len))

	// Clear any URG flag that might have been copied
	seg2[ipHdrLen+13] &^= 0x20
	binary.BigEndian.PutUint16(seg2[ipHdrLen+18:ipHdrLen+20], 0)

	sock.FixIPv4Checksum(seg2[:ipHdrLen])
	sock.FixTCPChecksum(seg2)

	// ===== Send order =====
	if cfg.Fragmentation.ReverseOrder {
		// Reverse: seg2, fake, seg1
		_ = w.sock.SendIPv4(seg2, dst)
		if seg2delay > 0 {
			time.Sleep(time.Duration(seg2delay) * time.Millisecond)
		}
		_ = w.sock.SendIPv4(fake, dst)
		if seg2delay > 0 {
			time.Sleep(time.Duration(seg2delay) * time.Millisecond)
		}
		_ = w.sock.SendIPv4(seg1, dst)
	} else {
		// Normal: seg1, fake, seg2
		_ = w.sock.SendIPv4(seg1, dst)
		if seg2delay > 0 {
			time.Sleep(time.Duration(seg2delay) * time.Millisecond)
		}
		_ = w.sock.SendIPv4(fake, dst)
		if seg2delay > 0 {
			time.Sleep(time.Duration(seg2delay) * time.Millisecond)
		}
		_ = w.sock.SendIPv4(seg2, dst)
	}

	log.Tracef("OOB: Sent seg1=%d, fake=%d (TTL=%d), seg2=%d bytes", seg1Len, fakeLen, fake[8], seg2Len)
}

// sendOOBFragmentsV6 is the IPv6 version of OOB injection
func (w *Worker) sendOOBFragmentsV6(cfg *config.SetConfig, packet []byte, dst net.IP) {
	const ipv6HdrLen = 40

	if len(packet) < ipv6HdrLen+20 {
		_ = w.sock.SendIPv6(packet, dst)
		return
	}

	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
	payloadStart := ipv6HdrLen + tcpHdrLen
	payloadLen := len(packet) - payloadStart

	if payloadLen <= 0 {
		_ = w.sock.SendIPv6(packet, dst)
		return
	}

	oobPos := config.ResolveRange(cfg.Fragmentation.OOBPosition, cfg.Fragmentation.OOBPositionMax)
	if oobPos <= 0 {
		oobPos = 1
	}

	if cfg.Fragmentation.MiddleSNI {
		if sniStart, sniEnd, ok := locateSNI(packet[payloadStart:]); ok && sniEnd > sniStart {
			oobPos = sniStart + (sniEnd-sniStart)/2
			log.Tracef("OOB v6: SNI at %d-%d, injecting at %d", sniStart, sniEnd, oobPos)
		}
	}

	if oobPos >= payloadLen {
		oobPos = payloadLen / 2
	}
	if oobPos <= 0 {
		oobPos = 1
	}

	oobChar := cfg.Fragmentation.OOBChar
	if oobChar == 0 {
		oobChar = 'x'
	}

	seq := binary.BigEndian.Uint32(packet[ipv6HdrLen+4 : ipv6HdrLen+8])
	payload := packet[payloadStart:]
	seg2delay := config.ResolveSeg2Delay(cfg.TCP.Seg2Delay, cfg.TCP.Seg2DelayMax)

	log.Tracef("OOB v6: Injecting fake 0x%02x at pos %d of %d bytes", oobChar, oobPos, payloadLen)

	// ===== Segment 1: payload[0:oobPos] - CLEAN =====
	seg1Len := payloadStart + oobPos
	seg1 := make([]byte, seg1Len)
	copy(seg1[:payloadStart], packet[:payloadStart])
	copy(seg1[payloadStart:], payload[:oobPos])

	binary.BigEndian.PutUint16(seg1[4:6], uint16(seg1Len-ipv6HdrLen))
	sock.FixTCPChecksumV6(seg1)

	// ===== Fake OOB packet: single byte with URG, LOW HOP LIMIT =====
	fakeLen := payloadStart + 1
	fake := make([]byte, fakeLen)
	copy(fake[:payloadStart], packet[:payloadStart])
	fake[payloadStart] = oobChar

	binary.BigEndian.PutUint32(fake[ipv6HdrLen+4:ipv6HdrLen+8], seq+uint32(oobPos))
	fake[ipv6HdrLen+13] |= 0x20
	binary.BigEndian.PutUint16(fake[ipv6HdrLen+18:ipv6HdrLen+20], 1)

	// Low hop limit (IPv6 equivalent of TTL)
	fake[7] = cfg.Faking.TTL
	if fake[7] == 0 {
		fake[7] = 3
	}

	binary.BigEndian.PutUint16(fake[4:6], uint16(fakeLen-ipv6HdrLen))
	sock.FixTCPChecksumV6(fake)

	switch cfg.Faking.Strategy {
	case "tcp_check", "md5sum":
		corruptTCPChecksum(fake, ipv6HdrLen)
	}

	// ===== Segment 2: payload[oobPos:] - CLEAN =====
	seg2DataLen := payloadLen - oobPos
	seg2Len := payloadStart + seg2DataLen
	seg2 := make([]byte, seg2Len)
	copy(seg2[:payloadStart], packet[:payloadStart])
	copy(seg2[payloadStart:], payload[oobPos:])

	binary.BigEndian.PutUint32(seg2[ipv6HdrLen+4:ipv6HdrLen+8], seq+uint32(oobPos))
	binary.BigEndian.PutUint16(seg2[4:6], uint16(seg2Len-ipv6HdrLen))
	seg2[ipv6HdrLen+13] &^= 0x20
	binary.BigEndian.PutUint16(seg2[ipv6HdrLen+18:ipv6HdrLen+20], 0)

	sock.FixTCPChecksumV6(seg2)

	// ===== Send =====
	if cfg.Fragmentation.ReverseOrder {
		_ = w.sock.SendIPv6(seg2, dst)
		if seg2delay > 0 {
			time.Sleep(time.Duration(seg2delay) * time.Millisecond)
		}
		_ = w.sock.SendIPv6(fake, dst)
		if seg2delay > 0 {
			time.Sleep(time.Duration(seg2delay) * time.Millisecond)
		}
		_ = w.sock.SendIPv6(seg1, dst)
	} else {
		_ = w.sock.SendIPv6(seg1, dst)
		if seg2delay > 0 {
			time.Sleep(time.Duration(seg2delay) * time.Millisecond)
		}
		_ = w.sock.SendIPv6(fake, dst)
		if seg2delay > 0 {
			time.Sleep(time.Duration(seg2delay) * time.Millisecond)
		}
		_ = w.sock.SendIPv6(seg2, dst)
	}

	log.Tracef("OOB v6: Sent seg1=%d, fake=%d (hop=%d), seg2=%d bytes", seg1Len, fakeLen, fake[7], seg2Len)
}
