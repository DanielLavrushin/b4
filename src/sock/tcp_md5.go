package sock

import (
	"crypto/rand"
	"encoding/binary"
)

func AddTCPMD5Option(packet []byte, isIPv6 bool) []byte {
	var ipHdrLen int
	if isIPv6 {
		ipHdrLen = 40
	} else {
		ipHdrLen = int((packet[0] & 0x0F) * 4)
	}

	if len(packet) < ipHdrLen+20 {
		return packet
	}

	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen

	md5Opt := make([]byte, 20)
	md5Opt[0] = 1           // NOP
	md5Opt[1] = 1           // NOP
	md5Opt[2] = 19          // MD5 kind
	md5Opt[3] = 18          // MD5 length
	rand.Read(md5Opt[4:20]) // 16 bytes fake MD5

	newTCPHdrLen := tcpHdrLen + 20
	if newTCPHdrLen > 60 { // Max TCP header is 60 bytes
		return packet
	}

	newPacket := make([]byte, len(packet)+20)

	copy(newPacket[:ipHdrLen], packet[:ipHdrLen])
	copy(newPacket[ipHdrLen:ipHdrLen+20], packet[ipHdrLen:ipHdrLen+20])

	if tcpHdrLen > 20 {
		copy(newPacket[ipHdrLen+20:ipHdrLen+tcpHdrLen], packet[ipHdrLen+20:ipHdrLen+tcpHdrLen])
	}

	copy(newPacket[ipHdrLen+tcpHdrLen:ipHdrLen+tcpHdrLen+20], md5Opt)

	if payloadStart < len(packet) {
		copy(newPacket[ipHdrLen+newTCPHdrLen:], packet[payloadStart:])
	}

	newPacket[ipHdrLen+12] = byte((newTCPHdrLen/4)<<4) | (newPacket[ipHdrLen+12] & 0x0F)

	if isIPv6 {
		binary.BigEndian.PutUint16(newPacket[4:6], uint16(len(newPacket)-40))
		FixTCPChecksumV6(newPacket)
	} else {
		binary.BigEndian.PutUint16(newPacket[2:4], uint16(len(newPacket)))
		FixIPv4Checksum(newPacket[:ipHdrLen])
		FixTCPChecksum(newPacket)
	}

	return newPacket
}
