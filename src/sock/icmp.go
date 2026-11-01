package sock

import (
	"bytes"
	"encoding/binary"
)

func BuildICMPv4Reject(originalPacket []byte, clientIP, serverIP []byte) []byte {
	if len(originalPacket) < 20 || len(clientIP) < 4 || len(serverIP) < 4 || originalPacket[0]>>4 != 4 {
		return nil
	}
	ihl := int(originalPacket[0]&0x0F) * 4
	if ihl < 20 || ihl > len(originalPacket) {
		return nil
	}
	quotedLen := ihl + 8
	if quotedLen > len(originalPacket) {
		quotedLen = len(originalPacket)
	}

	icmpLen := 8 + quotedLen
	totalLen := 20 + icmpLen

	pkt := make([]byte, totalLen)

	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	pkt[8] = 64
	pkt[9] = 1
	copy(pkt[12:16], serverIP[:4])
	copy(pkt[16:20], clientIP[:4])

	ipEnd := 20
	pkt[ipEnd] = 3
	pkt[ipEnd+1] = 3

	copy(pkt[ipEnd+8:], originalPacket[:quotedLen])
	if quoted := pkt[ipEnd+8 : ipEnd+8+ihl]; !bytes.Equal(quoted[12:16], clientIP[:4]) {
		copy(quoted[12:16], clientIP[:4])
		FixIPv4Checksum(quoted)
	}

	icmpData := pkt[ipEnd:]
	var sum uint32
	for i := 0; i < len(icmpData)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(icmpData[i : i+2]))
	}
	if len(icmpData)%2 == 1 {
		sum += uint32(icmpData[len(icmpData)-1]) << 8
	}
	for sum > 0xFFFF {
		sum = (sum >> 16) + (sum & 0xFFFF)
	}
	binary.BigEndian.PutUint16(pkt[ipEnd+2:ipEnd+4], ^uint16(sum))

	FixIPv4Checksum(pkt[:20])

	return pkt
}

func BuildICMPv6Reject(originalPacket []byte, clientIP, serverIP []byte) []byte {
	if len(originalPacket) < 40 || len(clientIP) < 16 || len(serverIP) < 16 || originalPacket[0]>>4 != 6 {
		return nil
	}
	quotedLen := len(originalPacket)
	if quotedLen > 1232 {
		quotedLen = 1232
	}

	icmpLen := 8 + quotedLen
	totalLen := 40 + icmpLen

	pkt := make([]byte, totalLen)

	pkt[0] = 0x60
	binary.BigEndian.PutUint16(pkt[4:6], uint16(icmpLen))
	pkt[6] = 58
	pkt[7] = 64
	copy(pkt[8:24], serverIP[:16])
	copy(pkt[24:40], clientIP[:16])

	ipEnd := 40
	pkt[ipEnd] = 1
	pkt[ipEnd+1] = 4

	copy(pkt[ipEnd+8:], originalPacket[:quotedLen])

	var sum uint32
	for i := 0; i < 16; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(serverIP[i : i+2]))
	}
	for i := 0; i < 16; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(clientIP[i : i+2]))
	}
	sum += uint32(icmpLen)
	sum += 58

	icmpData := pkt[ipEnd:]
	for i := 0; i < len(icmpData)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(icmpData[i : i+2]))
	}
	if len(icmpData)%2 == 1 {
		sum += uint32(icmpData[len(icmpData)-1]) << 8
	}
	for sum > 0xFFFF {
		sum = (sum >> 16) + (sum & 0xFFFF)
	}
	binary.BigEndian.PutUint16(pkt[ipEnd+2:ipEnd+4], ^uint16(sum))

	return pkt
}
