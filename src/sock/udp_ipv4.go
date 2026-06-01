package sock

import (
	"encoding/binary"
	"net"

	"github.com/daniellavrushin/b4/utils"
)

func BuildUDPPacketV4(srcIP, dstIP net.IP, srcPort, dstPort uint16, payload []byte) []byte {
	src := srcIP.To4()
	dst := dstIP.To4()
	if src == nil || dst == nil {
		return nil
	}
	if len(payload) > 0xffff-28 {
		return nil
	}

	total := 20 + 8 + len(payload)
	pkt := make([]byte, total)

	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(total))
	binary.BigEndian.PutUint16(pkt[4:6], utils.RandUint16())
	pkt[8] = 64
	pkt[9] = 17
	copy(pkt[12:16], src)
	copy(pkt[16:20], dst)

	binary.BigEndian.PutUint16(pkt[20:22], srcPort)
	binary.BigEndian.PutUint16(pkt[22:24], dstPort)
	binary.BigEndian.PutUint16(pkt[24:26], uint16(8+len(payload)))
	copy(pkt[28:], payload)

	FixIPv4Checksum(pkt[:20])
	FixUDPChecksum(pkt, 20)
	return pkt
}

func udpChecksumIPv4(pkt []byte) {
	ihl := int((pkt[0] & 0x0f) << 2)
	udpo := ihl
	ulen := int(binary.BigEndian.Uint16(pkt[udpo+4 : udpo+6]))
	pseudo := make([]byte, 12)
	copy(pseudo[0:4], pkt[12:16])
	copy(pseudo[4:8], pkt[16:20])
	pseudo[9] = 17
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(ulen))
	pkt[udpo+6], pkt[udpo+7] = 0, 0
	sum := uint32(0)
	for i := 0; i < len(pseudo); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(pseudo[i : i+2]))
	}
	udp := pkt[udpo : udpo+ulen]
	for i := 0; i+1 < len(udp); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(udp[i : i+2]))
	}
	if len(udp)%2 == 1 {
		sum += uint32(udp[len(udp)-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	c := ^uint16(sum)
	if c == 0 {
		c = 0xffff
	}
	binary.BigEndian.PutUint16(pkt[udpo+6:udpo+8], c)
}

func BuildFakeUDPFromOriginalV4(orig []byte, fakeLen int, ttl uint8, payload []byte) ([]byte, bool) {
	if len(orig) < 20 || orig[0]>>4 != 4 {
		return nil, false
	}
	ihl := int((orig[0] & 0x0f) << 2)
	if ihl < 20 || len(orig) < ihl+8 {
		return nil, false
	}
	out := make([]byte, 20+8+fakeLen)
	copy(out, orig[:20])
	out[0] = (out[0] & 0xF0) | 0x05
	out[8] = ttl
	id := binary.BigEndian.Uint16(out[4:6])
	binary.BigEndian.PutUint16(out[4:6], id+1)
	out[6], out[7] = 0, 0
	binary.BigEndian.PutUint16(out[2:4], uint16(20+8+fakeLen))
	copy(out[20:], orig[ihl:ihl+8])
	binary.BigEndian.PutUint16(out[20+4:20+6], uint16(8+fakeLen))
	if len(payload) > 0 {
		copy(out[28:28+fakeLen], payload)
	}
	FixIPv4Checksum(out[:20])
	udpChecksumIPv4(out)
	return out, true
}

func IPv4FragmentUDP(orig []byte, split int) ([][]byte, bool) {
	if len(orig) < 28 || orig[0]>>4 != 4 {
		return nil, false
	}
	ihl := int((orig[0] & 0x0f) << 2)
	if ihl < 20 || len(orig) < ihl+8 {
		return nil, false
	}
	total := int(binary.BigEndian.Uint16(orig[2:4]))
	if total > len(orig) {
		total = len(orig)
	}
	udp := orig[ihl:total]
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
	id := binary.BigEndian.Uint16(orig[4:6])
	ip1 := make([]byte, 20+firstDataAligned)
	copy(ip1, orig[:20])
	ip1[0] = (ip1[0] & 0xF0) | 0x05
	binary.BigEndian.PutUint16(ip1[4:6], id)
	ip1[6] = 0x20
	ip1[7] = 0x00
	binary.BigEndian.PutUint16(ip1[2:4], uint16(20+firstDataAligned))
	copy(ip1[20:], udp[:firstDataAligned])
	FixIPv4Checksum(ip1[:20])
	ip2Data := udp[firstDataAligned:]
	offsetUnits := firstDataAligned / 8
	ip2 := make([]byte, 20+len(ip2Data))
	copy(ip2, orig[:20])
	ip2[0] = (ip2[0] & 0xF0) | 0x05
	binary.BigEndian.PutUint16(ip2[4:6], id)
	ip2[6] = byte(offsetUnits>>8) & 0x1F
	ip2[7] = byte(offsetUnits)
	binary.BigEndian.PutUint16(ip2[2:4], uint16(20+len(ip2Data)))
	copy(ip2[20:], ip2Data)
	FixIPv4Checksum(ip2[:20])
	return [][]byte{ip2, ip1}, true
}
