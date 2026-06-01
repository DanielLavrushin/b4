package sock

import (
	"encoding/binary"
	"net"
	"testing"
)

func onesSum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	return uint16(sum)
}

func TestBuildUDPPacketV4PayloadBounds(t *testing.T) {
	src := net.IPv4(8, 8, 8, 8)
	dst := net.IPv4(192, 168, 1, 50)

	if pkt := BuildUDPPacketV4(src, dst, 53, 40000, make([]byte, 0xffff-28)); pkt == nil {
		t.Error("expected non-nil at max payload size")
	}
	if pkt := BuildUDPPacketV4(src, dst, 53, 40000, make([]byte, 0xffff-27)); pkt != nil {
		t.Error("expected nil for oversized payload (would overflow IPv4 total length)")
	}
}

func TestBuildUDPPacketV4Checksums(t *testing.T) {
	src := net.IPv4(8, 8, 8, 8)
	dst := net.IPv4(192, 168, 1, 50)
	payload := []byte{0x12, 0x34, 0x81, 0x80, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x07}

	pkt := BuildUDPPacketV4(src, dst, 53, 40000, payload)
	if pkt == nil {
		t.Fatal("BuildUDPPacketV4 returned nil")
	}
	if len(pkt) != 20+8+len(payload) {
		t.Fatalf("unexpected length %d", len(pkt))
	}
	if pkt[9] != 17 {
		t.Fatalf("protocol = %d, want 17", pkt[9])
	}
	if !net.IP(pkt[12:16]).Equal(src.To4()) {
		t.Fatalf("src mismatch: %v", net.IP(pkt[12:16]))
	}
	if !net.IP(pkt[16:20]).Equal(dst.To4()) {
		t.Fatalf("dst mismatch: %v", net.IP(pkt[16:20]))
	}

	if got := onesSum(pkt[:20]); got != 0xffff {
		t.Fatalf("ip header checksum invalid: folded sum = %#x", got)
	}

	pseudo := make([]byte, 12)
	copy(pseudo[0:4], pkt[12:16])
	copy(pseudo[4:8], pkt[16:20])
	pseudo[9] = 17
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(8+len(payload)))
	if got := onesSum(append(pseudo, pkt[20:]...)); got != 0xffff {
		t.Fatalf("udp checksum invalid: folded sum = %#x", got)
	}
}

func buildMinimalIPv4UDPPacket(payloadSize int) []byte {
	ipHdrLen := 20
	udpHdrLen := 8
	totalLen := ipHdrLen + udpHdrLen + payloadSize

	pkt := make([]byte, totalLen)

	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	pkt[8] = 64
	pkt[9] = 17 // UDP
	copy(pkt[12:16], []byte{192, 168, 1, 1})
	copy(pkt[16:20], []byte{10, 0, 0, 1})

	binary.BigEndian.PutUint16(pkt[ipHdrLen:], 12345)
	binary.BigEndian.PutUint16(pkt[ipHdrLen+2:], 53)
	binary.BigEndian.PutUint16(pkt[ipHdrLen+4:], uint16(udpHdrLen+payloadSize))

	for i := 0; i < payloadSize; i++ {
		pkt[ipHdrLen+udpHdrLen+i] = byte(i % 256)
	}

	FixIPv4Checksum(pkt[:ipHdrLen])

	return pkt
}

func TestBuildFakeUDPFromOriginalV4_TooShort(t *testing.T) {
	_, ok := BuildFakeUDPFromOriginalV4(make([]byte, 10), 100, 3, nil)
	if ok {
		t.Error("expected false for short packet")
	}
}

func TestBuildFakeUDPFromOriginalV4_NotIPv4(t *testing.T) {
	pkt := make([]byte, 50)
	pkt[0] = 0x60
	_, ok := BuildFakeUDPFromOriginalV4(pkt, 100, 3, nil)
	if ok {
		t.Error("expected false for non-IPv4")
	}
}

func TestBuildFakeUDPFromOriginalV4_Valid(t *testing.T) {
	pkt := buildMinimalIPv4UDPPacket(20)
	result, ok := BuildFakeUDPFromOriginalV4(pkt, 50, 3, nil)
	if !ok {
		t.Fatal("expected success")
	}

	// Check TTL
	if result[8] != 3 {
		t.Errorf("TTL not set: expected 3, got %d", result[8])
	}

	// Check length
	expectedLen := 20 + 8 + 50
	if len(result) != expectedLen {
		t.Errorf("expected len %d, got %d", expectedLen, len(result))
	}
}

func TestIPv4FragmentUDP_TooShort(t *testing.T) {
	_, ok := IPv4FragmentUDP(make([]byte, 20), 8)
	if ok {
		t.Error("expected false")
	}
}

func TestIPv4FragmentUDP_Valid(t *testing.T) {
	pkt := buildMinimalIPv4UDPPacket(100)
	frags, ok := IPv4FragmentUDP(pkt, 20)
	if !ok {
		t.Fatal("expected success")
	}
	if len(frags) != 2 {
		t.Errorf("expected 2 fragments, got %d", len(frags))
	}
}
