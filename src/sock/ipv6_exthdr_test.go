package sock

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func buildIPv6TCPPacketWithDestOpts(payloadSize int, seq uint32) []byte {
	const ipv6HdrLen = 40
	const extLen = 8
	const tcpHdrLen = 20

	pkt := make([]byte, ipv6HdrLen+extLen+tcpHdrLen+payloadSize)

	pkt[0] = 0x60
	binary.BigEndian.PutUint16(pkt[4:6], uint16(len(pkt)-ipv6HdrLen))
	pkt[6] = 60
	pkt[7] = 64
	pkt[23] = 1
	pkt[39] = 1

	pkt[ipv6HdrLen] = 6
	pkt[ipv6HdrLen+1] = 0
	pkt[ipv6HdrLen+2] = 1
	pkt[ipv6HdrLen+3] = 4

	tcp := ipv6HdrLen + extLen
	binary.BigEndian.PutUint16(pkt[tcp:], 12345)
	binary.BigEndian.PutUint16(pkt[tcp+2:], 443)
	binary.BigEndian.PutUint32(pkt[tcp+4:], seq)
	pkt[tcp+12] = 0x50
	pkt[tcp+13] = 0x18

	for i := 0; i < payloadSize; i++ {
		pkt[tcp+tcpHdrLen+i] = byte(i % 256)
	}

	return pkt
}

func buildIPv6PacketWithSACK() []byte {
	const ipv6HdrLen = 40
	const tcpHdrLen = 40

	pkt := make([]byte, ipv6HdrLen+tcpHdrLen+10)

	pkt[0] = 0x60
	binary.BigEndian.PutUint16(pkt[4:6], uint16(len(pkt)-ipv6HdrLen))
	pkt[6] = 6
	pkt[7] = 64
	pkt[23] = 1
	pkt[39] = 1

	binary.BigEndian.PutUint16(pkt[ipv6HdrLen:], 12345)
	binary.BigEndian.PutUint16(pkt[ipv6HdrLen+2:], 443)
	pkt[ipv6HdrLen+12] = byte((tcpHdrLen / 4) << 4)
	pkt[ipv6HdrLen+13] = 0x18

	opts := pkt[ipv6HdrLen+20:]
	opts[0] = 2
	opts[1] = 4
	binary.BigEndian.PutUint16(opts[2:4], 1440)
	opts[4] = 4
	opts[5] = 2
	opts[6] = 1
	opts[7] = 1
	opts[8] = 5
	opts[9] = 10
	binary.BigEndian.PutUint32(opts[10:14], 1000)
	binary.BigEndian.PutUint32(opts[14:18], 2000)

	FixTCPChecksumV6(pkt)
	return pkt
}

func TestStripSACKFromTCPv6_LeavesExtensionHeaderPacketAlone(t *testing.T) {
	pkt := buildIPv6TCPPacketWithDestOpts(20, 0xF0000001)
	orig := append([]byte(nil), pkt...)

	result := StripSACKFromTCPv6(pkt)

	if !bytes.Equal(result, orig) {
		t.Error("a packet whose TCP header sits behind an extension header must be returned untouched")
	}
}

func TestStripSACKFromTCPv6_StillStripsPlainPacket(t *testing.T) {
	pkt := buildIPv6PacketWithSACK()

	result := StripSACKFromTCPv6(pkt)

	newTCPHdrLen := int((result[52] >> 4) * 4)
	if newTCPHdrLen != 28 {
		t.Fatalf("TCP header length = %d, want 28 (MSS plus two NOPs, padded)", newTCPHdrLen)
	}

	wantOpts := []byte{2, 4, 0x05, 0xA0, 1, 1, 0, 0}
	if got := result[60 : 40+newTCPHdrLen]; !bytes.Equal(got, wantOpts) {
		t.Fatalf("options after stripping = %v, want %v", got, wantOpts)
	}
	if len(result) != 78 {
		t.Fatalf("packet length = %d, want 78 (10 bytes of SACK options removed)", len(result))
	}
}

func TestBuildFakeSNIPacketV6_RejectsExtensionHeaderPacket(t *testing.T) {
	pkt := buildIPv6TCPPacketWithDestOpts(20, 0xF0000001)

	if fake := BuildFakeSNIPacketV6(pkt, &config.SetConfig{}); fake != nil {
		t.Error("a packet whose TCP header sits behind an extension header must not be turned into a fake SNI packet")
	}
}
