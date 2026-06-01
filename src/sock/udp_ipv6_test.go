package sock

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestBuildUDPPacketV6PayloadBounds(t *testing.T) {
	src := net.ParseIP("2001:db8::1")
	dst := net.ParseIP("2001:db8::2")

	if pkt := BuildUDPPacketV6(src, dst, 53, 40000, make([]byte, 0xffff-8)); pkt == nil {
		t.Error("expected non-nil at max payload size")
	}
	if pkt := BuildUDPPacketV6(src, dst, 53, 40000, make([]byte, 0xffff-7)); pkt != nil {
		t.Error("expected nil for oversized payload (would overflow UDP/payload length)")
	}
}

func TestBuildUDPPacketV6Checksums(t *testing.T) {
	src := net.ParseIP("2001:db8::1")
	dst := net.ParseIP("2001:db8::2")
	payload := []byte{0xab, 0xcd, 0x01, 0x00, 0x00, 0x01}

	pkt := BuildUDPPacketV6(src, dst, 53, 40000, payload)
	if pkt == nil {
		t.Fatal("BuildUDPPacketV6 returned nil")
	}
	if len(pkt) != 40+8+len(payload) {
		t.Fatalf("unexpected length %d", len(pkt))
	}
	if pkt[6] != 17 {
		t.Fatalf("next header = %d, want 17", pkt[6])
	}

	pseudo := make([]byte, 40)
	copy(pseudo[0:16], pkt[8:24])
	copy(pseudo[16:32], pkt[24:40])
	binary.BigEndian.PutUint32(pseudo[32:36], uint32(8+len(payload)))
	pseudo[39] = 17
	if got := onesSum(append(pseudo, pkt[40:]...)); got != 0xffff {
		t.Fatalf("udp6 checksum invalid: folded sum = %#x", got)
	}
}

func TestBuildUDPPacketV6RejectsV4(t *testing.T) {
	if pkt := BuildUDPPacketV6(net.IPv4(1, 1, 1, 1), net.ParseIP("2001:db8::2"), 53, 40000, nil); pkt != nil {
		t.Fatal("expected nil for v4 source")
	}
}

func buildMinimalIPv6UDPPacket(payloadSize int) []byte {
	ipv6HdrLen := 40
	udpHdrLen := 8
	totalLen := ipv6HdrLen + udpHdrLen + payloadSize

	pkt := make([]byte, totalLen)

	pkt[0] = 0x60
	binary.BigEndian.PutUint16(pkt[4:6], uint16(udpHdrLen+payloadSize))
	pkt[6] = 17 // UDP
	pkt[7] = 64

	pkt[23] = 1
	pkt[39] = 1

	binary.BigEndian.PutUint16(pkt[ipv6HdrLen:], 12345)
	binary.BigEndian.PutUint16(pkt[ipv6HdrLen+2:], 53)
	binary.BigEndian.PutUint16(pkt[ipv6HdrLen+4:], uint16(udpHdrLen+payloadSize))

	for i := 0; i < payloadSize; i++ {
		pkt[ipv6HdrLen+udpHdrLen+i] = byte(i % 256)
	}

	udpChecksumIPv6(pkt)

	return pkt
}

func TestUdpChecksumIPv6_TooShort(t *testing.T) {
	pkt := make([]byte, 40)
	udpChecksumIPv6(pkt) // Should not panic
}

func TestBuildFakeUDPFromOriginalV6_TooShort(t *testing.T) {
	_, ok := BuildFakeUDPFromOriginalV6(make([]byte, 40), 100, 3, nil)
	if ok {
		t.Error("expected false")
	}
}

func TestBuildFakeUDPFromOriginalV6_NotIPv6(t *testing.T) {
	pkt := make([]byte, 60)
	pkt[0] = 0x45
	_, ok := BuildFakeUDPFromOriginalV6(pkt, 100, 3, nil)
	if ok {
		t.Error("expected false")
	}
}

func TestBuildFakeUDPFromOriginalV6_Valid(t *testing.T) {
	pkt := buildMinimalIPv6UDPPacket(20)
	result, ok := BuildFakeUDPFromOriginalV6(pkt, 50, 5, nil)
	if !ok {
		t.Fatal("expected success")
	}

	if result[7] != 5 {
		t.Errorf("hop limit not set: expected 5, got %d", result[7])
	}

	expectedLen := 40 + 8 + 50
	if len(result) != expectedLen {
		t.Errorf("expected len %d, got %d", expectedLen, len(result))
	}
}

func TestIPv6FragmentUDP_TooShort(t *testing.T) {
	_, ok := IPv6FragmentUDP(make([]byte, 40), 8)
	if ok {
		t.Error("expected false")
	}
}

func TestIPv6FragmentUDP_Valid(t *testing.T) {
	pkt := buildMinimalIPv6UDPPacket(100)
	frags, ok := IPv6FragmentUDP(pkt, 20)
	if !ok {
		t.Fatal("expected success")
	}
	if len(frags) != 2 {
		t.Errorf("expected 2 fragments, got %d", len(frags))
	}

	// Both fragments should have next header = 44 (Fragment)
	for i, frag := range frags {
		if frag[6] != 44 {
			t.Errorf("fragment %d: expected next header 44, got %d", i, frag[6])
		}
	}
}
