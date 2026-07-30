package nfq

import (
	"encoding/binary"
	"net"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
)

// resolveTLSSplit picks the byte offset inside the TCP payload to split at.
// TLSRecordPosition is measured from the end of the 5-byte record header, which
// is what makes position 1 land right after it. When MiddleSNI is set the split
// is placed inside the SNI itself, the equivalent of byedpi's "+s" offset flag.
func resolveTLSSplit(cfg *config.SetConfig, payload []byte, payloadLen int) int {
	split := 0

	if cfg.Fragmentation.MiddleSNI {
		if s, e, ok := locateSNI(payload); ok && e > s {
			split = s + (e-s)/2
		}
	}

	if split <= 0 {
		pos := config.ResolveRange(cfg.Fragmentation.TLSRecordPosition, cfg.Fragmentation.TLSRecordPositionMax)
		if pos <= 0 {
			pos = 1
		}
		split = 5 + pos
	}

	if split >= payloadLen {
		split = payloadLen / 2
	}
	if split < 6 {
		split = 6
	}
	if split >= payloadLen {
		split = payloadLen - 1
	}

	return split
}

// sendTLSFragments splits the TCP segment at an offset measured from the end of
// the TLS record header, or at the SNI when MiddleSNI is set. This is a TCP-level
// split at a record-relative position, not a rewrite into two TLS records: b4
// operates on a live kernel flow, and inserting a second 5-byte record header
// would shift every following sequence number in the connection.
func (w *Worker) sendTLSFragments(cfg *config.SetConfig, packet []byte, dst net.IP) {
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen
	payload := packet[payloadStart:]
	payloadLen := len(payload)

	if payloadLen < 5 || payload[0] != 0x16 {
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	absoluteSplit := resolveTLSSplit(cfg, payload, payloadLen)

	seg1Len := payloadStart + absoluteSplit
	seg1 := make([]byte, seg1Len)
	copy(seg1, packet[:seg1Len])

	seg2Len := payloadStart + (payloadLen - absoluteSplit)
	seg2 := make([]byte, seg2Len)
	copy(seg2[:payloadStart], packet[:payloadStart])
	copy(seg2[payloadStart:], packet[payloadStart+absoluteSplit:])

	binary.BigEndian.PutUint16(seg1[2:4], uint16(seg1Len))
	sock.FixIPv4Checksum(seg1[:ipHdrLen])
	sock.FixTCPChecksum(seg1)

	seq := binary.BigEndian.Uint32(seg2[ipHdrLen+4 : ipHdrLen+8])
	binary.BigEndian.PutUint32(seg2[ipHdrLen+4:ipHdrLen+8], seq+uint32(absoluteSplit))

	id := binary.BigEndian.Uint16(seg1[4:6])
	binary.BigEndian.PutUint16(seg2[4:6], id+1)
	binary.BigEndian.PutUint16(seg2[2:4], uint16(seg2Len))
	sock.FixIPv4Checksum(seg2[:ipHdrLen])
	sock.FixTCPChecksum(seg2)

	seg2d := config.ResolveSeg2Delay(cfg.TCP.Seg2Delay, cfg.TCP.Seg2DelayMax)
	w.SendTwoSegmentsV4(seg1, seg2, dst, seg2d, cfg.Fragmentation.ReverseOrder)
}

func (w *Worker) sendTLSFragmentsV6(cfg *config.SetConfig, packet []byte, dst net.IP) {
	ipv6HdrLen := 40
	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
	payloadStart := ipv6HdrLen + tcpHdrLen
	payload := packet[payloadStart:]
	payloadLen := len(payload)

	if payloadLen < 5 || payload[0] != 0x16 {
		_ = w.sock.SendIPv6(packet, dst)
		return
	}

	absoluteSplit := resolveTLSSplit(cfg, payload, payloadLen)

	seg1Len := payloadStart + absoluteSplit
	seg1 := make([]byte, seg1Len)
	copy(seg1, packet[:seg1Len])

	seg2Len := payloadStart + (payloadLen - absoluteSplit)
	seg2 := make([]byte, seg2Len)
	copy(seg2[:payloadStart], packet[:payloadStart])
	copy(seg2[payloadStart:], packet[payloadStart+absoluteSplit:])

	binary.BigEndian.PutUint16(seg1[4:6], uint16(seg1Len-ipv6HdrLen))
	sock.FixTCPChecksumV6(seg1)

	seq := binary.BigEndian.Uint32(seg2[ipv6HdrLen+4 : ipv6HdrLen+8])
	binary.BigEndian.PutUint32(seg2[ipv6HdrLen+4:ipv6HdrLen+8], seq+uint32(absoluteSplit))

	binary.BigEndian.PutUint16(seg2[4:6], uint16(seg2Len-ipv6HdrLen))
	sock.FixTCPChecksumV6(seg2)

	seg2d := config.ResolveSeg2Delay(cfg.TCP.Seg2Delay, cfg.TCP.Seg2DelayMax)
	w.SendTwoSegmentsV6(seg1, seg2, dst, seg2d, cfg.Fragmentation.ReverseOrder)
}
