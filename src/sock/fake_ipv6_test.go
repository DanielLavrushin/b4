package sock

import (
	"encoding/binary"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestBuildFakeSNIPacketV6_TooShort(t *testing.T) {
	result := BuildFakeSNIPacketV6(make([]byte, 50), &config.SetConfig{})
	if result != nil {
		t.Error("expected nil for packet < 60 bytes")
	}
}

func TestBuildFakeSNIPacketV6_NotIPv6(t *testing.T) {
	pkt := make([]byte, 80)
	pkt[0] = 0x45 // IPv4
	result := BuildFakeSNIPacketV6(pkt, &config.SetConfig{})
	if result != nil {
		t.Error("expected nil for non-IPv6 packet")
	}
}

func TestBuildFakeSNIPacketV6_DefaultPayload(t *testing.T) {
	pkt := buildMinimalIPv6TCPPacket(100)
	cfg := &config.SetConfig{}

	result := BuildFakeSNIPacketV6(pkt, cfg)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestBuildFakeSNIPacketV6_TTLStrategy(t *testing.T) {
	pkt := buildMinimalIPv6TCPPacket(100)
	cfg := &config.SetConfig{}
	cfg.Faking.Strategy = "ttl"
	cfg.Faking.TTL = 5

	result := BuildFakeSNIPacketV6(pkt, cfg)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result[7] != 5 {
		t.Errorf("hop limit not set: expected 5, got %d", result[7])
	}
}

func TestBuildFakeSNIPacketV6_PastSeqStrategy(t *testing.T) {
	pkt := buildMinimalIPv6TCPPacket(100)
	origSeq := binary.BigEndian.Uint32(pkt[44:48])

	cfg := &config.SetConfig{}
	cfg.Faking.Strategy = "pastseq"
	cfg.Faking.SeqOffset = 5000

	result := BuildFakeSNIPacketV6(pkt, cfg)
	newSeq := binary.BigEndian.Uint32(result[44:48])
	if newSeq != origSeq-5000 {
		t.Errorf("seq not adjusted")
	}
}

func TestFixTCPChecksumV6_TooShort(t *testing.T) {
	pkt := make([]byte, 30)
	FixTCPChecksumV6(pkt) // Should not panic
}

func TestFixTCPChecksumV6_ValidPacket(t *testing.T) {
	pkt := buildMinimalIPv6TCPPacket(20)

	// Corrupt and fix
	pkt[56], pkt[57] = 0xFF, 0xFF
	FixTCPChecksumV6(pkt)

	// Should complete without panic
}
