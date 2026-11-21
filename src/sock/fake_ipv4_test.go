package sock

import (
	"encoding/binary"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestBuildFakeSNIPacketV4_TooShort(t *testing.T) {
	result := BuildFakeSNIPacketV4(make([]byte, 30), &config.SetConfig{})
	if result != nil {
		t.Error("expected nil for packet < 40 bytes")
	}
}

func TestBuildFakeSNIPacketV4_NotIPv4(t *testing.T) {
	pkt := make([]byte, 60)
	pkt[0] = 0x60 // IPv6 version
	result := BuildFakeSNIPacketV4(pkt, &config.SetConfig{})
	if result != nil {
		t.Error("expected nil for non-IPv4 packet")
	}
}

func TestBuildFakeSNIPacketV4_DefaultPayload1(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(100)
	cfg := &config.SetConfig{}
	cfg.Faking.SNIType = config.FakePayloadDefault1

	result := BuildFakeSNIPacketV4(pkt, cfg)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should contain FakeSNI payload
	if len(result) < 40+len(config.FakeSNI1) {
		t.Errorf("result too short: %d", len(result))
	}
}

func TestBuildFakeSNIPacketV4_DefaultPayload2(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(100)
	cfg := &config.SetConfig{}
	cfg.Faking.SNIType = config.FakePayloadDefault2

	result := BuildFakeSNIPacketV4(pkt, cfg)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should contain FakeSNI payload
	if len(result) < 40+len(config.FakeSNI2) {
		t.Errorf("result too short: %d", len(result))
	}
}

func TestBuildFakeSNIPacketV4_RandomPayload(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(100)
	cfg := &config.SetConfig{}
	cfg.Faking.SNIType = config.FakePayloadRandom

	result := BuildFakeSNIPacketV4(pkt, cfg)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Random payload is 1200 bytes
	expectedLen := 20 + 20 + 1200
	if len(result) != expectedLen {
		t.Errorf("expected len %d, got %d", expectedLen, len(result))
	}
}

func TestBuildFakeSNIPacketV4_CustomPayload(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(100)
	cfg := &config.SetConfig{}
	cfg.Faking.SNIType = config.FakePayloadCustom
	cfg.Faking.CustomPayload = "test-payload"

	result := BuildFakeSNIPacketV4(pkt, cfg)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	expectedLen := 20 + 20 + len("test-payload")
	if len(result) != expectedLen {
		t.Errorf("expected len %d, got %d", expectedLen, len(result))
	}
}

func TestBuildFakeSNIPacketV4_TTLStrategy(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(100)
	cfg := &config.SetConfig{}
	cfg.Faking.Strategy = "ttl"
	cfg.Faking.TTL = 3

	result := BuildFakeSNIPacketV4(pkt, cfg)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result[8] != 3 {
		t.Errorf("TTL not set: expected 3, got %d", result[8])
	}
}

func TestBuildFakeSNIPacketV4_PastSeqStrategy(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(100)
	origSeq := binary.BigEndian.Uint32(pkt[24:28])

	cfg := &config.SetConfig{}
	cfg.Faking.Strategy = "pastseq"
	cfg.Faking.SeqOffset = 5000

	result := BuildFakeSNIPacketV4(pkt, cfg)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	newSeq := binary.BigEndian.Uint32(result[24:28])
	if newSeq != origSeq-5000 {
		t.Errorf("seq not adjusted: expected %d, got %d", origSeq-5000, newSeq)
	}
}

func TestBuildFakeSNIPacketV4_PastSeqDefaultOffset(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(100)
	origSeq := binary.BigEndian.Uint32(pkt[24:28])

	cfg := &config.SetConfig{}
	cfg.Faking.Strategy = "pastseq"
	cfg.Faking.SeqOffset = 0 // Default should be 8192

	result := BuildFakeSNIPacketV4(pkt, cfg)
	newSeq := binary.BigEndian.Uint32(result[24:28])
	if newSeq != origSeq-8192 {
		t.Errorf("default seq offset not applied: expected %d, got %d", origSeq-8192, newSeq)
	}
}

func TestBuildFakeSNIPacketV4_RandSeqWithOffset(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(50)

	cfg := &config.SetConfig{}
	cfg.Faking.Strategy = "randseq"
	cfg.Faking.SeqOffset = 500

	result := BuildFakeSNIPacketV4(pkt, cfg)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestBuildFakeSNIPacketV4_RandSeqNoOffset(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(50)

	cfg := &config.SetConfig{}
	cfg.Faking.Strategy = "randseq"
	cfg.Faking.SeqOffset = 0

	result := BuildFakeSNIPacketV4(pkt, cfg)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestBuildFakeSNIPacketV4_TCPCheckStrategy(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(100)
	cfg := &config.SetConfig{}
	cfg.Faking.Strategy = "tcp_check"

	result := BuildFakeSNIPacketV4(pkt, cfg)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// TCP checksum should be corrupted (XOR'd with 0xFF)
	// Just verify packet is returned
}

func TestFixIPv4Checksum_TooShort(t *testing.T) {
	pkt := make([]byte, 10)
	FixIPv4Checksum(pkt) // Should not panic
}

func TestFixIPv4Checksum_ValidPacket(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(10)

	// Corrupt checksum
	pkt[10], pkt[11] = 0xFF, 0xFF

	FixIPv4Checksum(pkt[:20])

	// Verify by recalculating
	var sum uint32
	for i := 0; i < 20; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(pkt[i : i+2]))
	}
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	if uint16(sum) != 0xffff {
		t.Error("checksum invalid after fix")
	}
}

func TestFixTCPChecksum(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(20)

	// Corrupt checksum
	pkt[36], pkt[37] = 0xFF, 0xFF

	FixTCPChecksum(pkt)

	// Should not panic and packet should be valid length
	if len(pkt) < 40 {
		t.Error("packet corrupted")
	}
}
