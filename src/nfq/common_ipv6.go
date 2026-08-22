package nfq

import (
	"encoding/binary"
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
)

const (
	ipProtoTCP = 6
	ipProtoUDP = 17
)

func upperLayerOffsetV6(packet []byte) (int, byte, bool) {
	if len(packet) < IPv6HeaderLen || packet[0]>>4 != IPv6 {
		return 0, 0, false
	}

	nextHeader := packet[6]
	offset := IPv6HeaderLen

	for {
		switch nextHeader {
		case 0, 43, 60:
			if len(packet) < offset+2 {
				return 0, 0, false
			}
			nextHeader = packet[offset]
			offset += int(packet[offset+1])*8 + 8
		case 44:
			return 0, 0, false
		default:
			if offset > len(packet) {
				return 0, 0, false
			}
			return offset, nextHeader, true
		}
	}
}

func hasPlainIPv6Header(packet []byte, proto byte) bool {
	offset, upper, ok := upperLayerOffsetV6(packet)
	return ok && offset == IPv6HeaderLen && upper == proto
}

func ExtractPacketInfoV6(packet []byte) (PacketInfo, bool) {
	if len(packet) < 60 {
		return PacketInfo{}, false
	}

	offset, proto, ok := upperLayerOffsetV6(packet)
	if !ok || proto != ipProtoTCP {
		return PacketInfo{}, false
	}

	if len(packet) < offset+20 {
		return PacketInfo{}, false
	}

	tcpHdrLen := int((packet[offset+12] >> 4) * 4)
	payloadStart := offset + tcpHdrLen
	if payloadStart > len(packet) {
		return PacketInfo{}, false
	}
	payloadLen := len(packet) - payloadStart

	return PacketInfo{
		IPHdrLen:     offset,
		TCPHdrLen:    tcpHdrLen,
		PayloadStart: payloadStart,
		PayloadLen:   payloadLen,
		Payload:      packet[payloadStart:],
		Seq0:         binary.BigEndian.Uint32(packet[offset+4 : offset+8]),
		IsIPv6:       true,
	}, true
}

func (w *Worker) SendSegmentsV6(segs [][]byte, dst net.IP, cfg *config.SetConfig) {
	delay := config.ResolveSeg2Delay(cfg.TCP.Seg2Delay, cfg.TCP.Seg2DelayMax)
	if cfg.Fragmentation.ReverseOrder {
		for i := len(segs) - 1; i >= 0; i-- {
			_ = w.sock.SendIPv6(segs[i], dst)
			if i > 0 && delay > 0 {
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
		}
	} else {
		for i, seg := range segs {
			_ = w.sock.SendIPv6(seg, dst)
			if i < len(segs)-1 && delay > 0 {
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
		}
	}
}

func BuildSegmentV6(packet []byte, pi PacketInfo, payloadSlice []byte, seqOffset uint32) []byte {
	segLen := pi.PayloadStart + len(payloadSlice)
	seg := make([]byte, segLen)
	copy(seg[:pi.PayloadStart], packet[:pi.PayloadStart])
	copy(seg[pi.PayloadStart:], payloadSlice)

	binary.BigEndian.PutUint32(seg[pi.IPHdrLen+4:pi.IPHdrLen+8], pi.Seq0+seqOffset)
	binary.BigEndian.PutUint16(seg[4:6], uint16(segLen-40))

	sock.FixTCPChecksumV6(seg)
	return seg
}

func (w *Worker) SendTwoSegmentsV6(seg1, seg2 []byte, dst net.IP, delay int, reverse bool) {
	if reverse {
		_ = w.sock.SendIPv6(seg2, dst)
		if delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		_ = w.sock.SendIPv6(seg1, dst)
	} else {
		_ = w.sock.SendIPv6(seg1, dst)
		if delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		_ = w.sock.SendIPv6(seg2, dst)
	}
}

func BuildSeqOverlapSegmentV6(packet []byte, pi PacketInfo, origPayload []byte, origStartOffset int, seqovlLen int, pattern []byte) []byte {
	if seqovlLen <= 0 || len(pattern) == 0 {
		return BuildSegmentV6(packet, pi, origPayload, uint32(origStartOffset))
	}
	ext := make([]byte, seqovlLen+len(origPayload))
	for i := 0; i < seqovlLen; i++ {
		ext[i] = pattern[i%len(pattern)]
	}
	copy(ext[seqovlLen:], origPayload)
	seqOffset := uint32(origStartOffset) - uint32(seqovlLen)
	return BuildSegmentV6(packet, pi, ext, seqOffset)
}

func BuildFakeOverlapSegmentV6(packet []byte, pi PacketInfo, payloadLen int, seqOffset uint32, fakePattern []byte, fakeHopLimit uint8) []byte {
	if payloadLen <= 0 {
		return nil
	}

	segLen := pi.PayloadStart + payloadLen
	seg := make([]byte, segLen)
	copy(seg[:pi.PayloadStart], packet[:pi.PayloadStart])

	patLen := len(fakePattern)
	if patLen == 0 {
		for i := 0; i < payloadLen; i++ {
			seg[pi.PayloadStart+i] = byte((i * 7) & 0xFF)
		}
	} else {
		for i := 0; i < payloadLen; i++ {
			seg[pi.PayloadStart+i] = fakePattern[i%patLen]
		}
	}

	binary.BigEndian.PutUint32(seg[pi.IPHdrLen+4:pi.IPHdrLen+8], pi.Seq0+seqOffset)
	binary.BigEndian.PutUint16(seg[4:6], uint16(segLen-40))

	seg[7] = dynamicTTL(packet, true, fakeHopLimit)

	seg[pi.IPHdrLen+13] &^= 0x08

	sock.FixTCPChecksumV6(seg)
	corruptTCPChecksum(seg, pi.IPHdrLen)

	return seg
}
