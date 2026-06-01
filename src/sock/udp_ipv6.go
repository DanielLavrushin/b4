package sock

import (
	"encoding/binary"
	"net"

	"github.com/daniellavrushin/b4/utils"
)

func BuildUDPPacketV6(srcIP, dstIP net.IP, srcPort, dstPort uint16, payload []byte) []byte {
	src := srcIP.To16()
	dst := dstIP.To16()
	if src == nil || dst == nil || srcIP.To4() != nil || dstIP.To4() != nil {
		return nil
	}
	if len(payload) > 0xffff-8 {
		return nil
	}

	pkt := make([]byte, 40+8+len(payload))

	pkt[0] = 0x60
	binary.BigEndian.PutUint16(pkt[4:6], uint16(8+len(payload)))
	pkt[6] = 17
	pkt[7] = 64
	copy(pkt[8:24], src)
	copy(pkt[24:40], dst)

	binary.BigEndian.PutUint16(pkt[40:42], srcPort)
	binary.BigEndian.PutUint16(pkt[42:44], dstPort)
	binary.BigEndian.PutUint16(pkt[44:46], uint16(8+len(payload)))
	copy(pkt[48:], payload)

	FixUDPChecksumV6(pkt)
	return pkt
}

func udpChecksumIPv6(pkt []byte) {
	if len(pkt) < 48 {
		return
	}

	ipv6HdrLen := 40
	udpOffset := ipv6HdrLen
	udpLen := int(binary.BigEndian.Uint16(pkt[udpOffset+4 : udpOffset+6]))

	pseudo := make([]byte, 40)
	copy(pseudo[0:16], pkt[8:24])
	copy(pseudo[16:32], pkt[24:40])
	binary.BigEndian.PutUint32(pseudo[32:36], uint32(udpLen))
	pseudo[39] = 17

	pkt[udpOffset+6], pkt[udpOffset+7] = 0, 0

	var sum uint32

	for i := 0; i < len(pseudo); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(pseudo[i : i+2]))
	}

	udp := pkt[udpOffset : udpOffset+udpLen]
	for i := 0; i+1 < len(udp); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(udp[i : i+2]))
	}
	if len(udp)%2 == 1 {
		sum += uint32(udp[len(udp)-1]) << 8
	}

	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}

	checksum := ^uint16(sum)
	if checksum == 0 {
		checksum = 0xffff
	}
	binary.BigEndian.PutUint16(pkt[udpOffset+6:udpOffset+8], checksum)
}

func BuildFakeUDPFromOriginalV6(orig []byte, fakeLen int, hopLimit uint8, payload []byte) ([]byte, bool) {
	if len(orig) < 48 || orig[0]>>4 != 6 {
		return nil, false
	}

	ipv6HdrLen := 40
	if len(orig) < ipv6HdrLen+8 || orig[6] != 17 {
		return nil, false
	}

	out := make([]byte, ipv6HdrLen+8+fakeLen)

	copy(out, orig[:ipv6HdrLen])

	out[7] = hopLimit

	binary.BigEndian.PutUint16(out[4:6], uint16(8+fakeLen))

	copy(out[ipv6HdrLen:], orig[ipv6HdrLen:ipv6HdrLen+8])

	binary.BigEndian.PutUint16(out[ipv6HdrLen+4:ipv6HdrLen+6], uint16(8+fakeLen))

	if len(payload) > 0 {
		copy(out[ipv6HdrLen+8:ipv6HdrLen+8+fakeLen], payload)
	}

	udpChecksumIPv6(out)

	return out, true
}

func IPv6FragmentUDP(orig []byte, split int) ([][]byte, bool) {
	if len(orig) < 48 || orig[0]>>4 != 6 {
		return nil, false
	}

	ipv6HdrLen := 40
	if len(orig) < ipv6HdrLen+8 || orig[6] != 17 {
		return nil, false
	}

	payloadLen := int(binary.BigEndian.Uint16(orig[4:6]))
	if payloadLen < 8 || ipv6HdrLen+payloadLen > len(orig) {
		return nil, false
	}

	udp := orig[ipv6HdrLen : ipv6HdrLen+payloadLen]
	if len(udp) < 8 {
		return nil, false
	}

	payload := udp[8:]
	if split < 1 || split >= len(payload) {
		split = 8
	}

	firstData := 8 + split
	firstDataAligned := firstData - (firstData % 8)
	if firstDataAligned < 8 {
		firstDataAligned = 8
	}
	if firstDataAligned >= len(udp) {
		return nil, false
	}

	fragHdrLen := 8

	var identification uint32 = utils.RandUint32()

	frag1Len := ipv6HdrLen + fragHdrLen + firstDataAligned
	frag1 := make([]byte, frag1Len)

	copy(frag1, orig[:ipv6HdrLen])
	frag1[6] = 44
	binary.BigEndian.PutUint16(frag1[4:6], uint16(fragHdrLen+firstDataAligned))

	fragHdr1 := frag1[ipv6HdrLen : ipv6HdrLen+fragHdrLen]
	fragHdr1[0] = 17
	fragHdr1[1] = 0
	binary.BigEndian.PutUint16(fragHdr1[2:4], 0|0x0001)
	binary.BigEndian.PutUint32(fragHdr1[4:8], identification)

	copy(frag1[ipv6HdrLen+fragHdrLen:], udp[:firstDataAligned])

	remainingData := udp[firstDataAligned:]
	frag2Len := ipv6HdrLen + fragHdrLen + len(remainingData)
	frag2 := make([]byte, frag2Len)

	copy(frag2, orig[:ipv6HdrLen])
	frag2[6] = 44
	binary.BigEndian.PutUint16(frag2[4:6], uint16(fragHdrLen+len(remainingData)))

	fragHdr2 := frag2[ipv6HdrLen : ipv6HdrLen+fragHdrLen]
	fragHdr2[0] = 17
	fragHdr2[1] = 0
	offsetUnits := uint16(firstDataAligned / 8)
	binary.BigEndian.PutUint16(fragHdr2[2:4], (offsetUnits << 3))
	binary.BigEndian.PutUint32(fragHdr2[4:8], identification)

	copy(frag2[ipv6HdrLen+fragHdrLen:], remainingData)

	return [][]byte{frag2, frag1}, true
}
