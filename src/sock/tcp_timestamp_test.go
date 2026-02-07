package sock

import (
	"encoding/binary"
	"testing"
)

// buildTestTCPPacketWithTimestamp creates a test TCP packet with a timestamp option
func buildTestTCPPacketWithTimestamp(isIPv6 bool, tsval uint32) []byte {
	var packet []byte
	var ipHdrLen int

	if isIPv6 {
		ipHdrLen = 40
		// Minimal IPv6 header
		packet = make([]byte, ipHdrLen+32) // 40 IP + 32 TCP (20 base + 12 options with NOPs)
		packet[0] = 0x60 // Version 6
		binary.BigEndian.PutUint16(packet[4:6], 32) // Payload length
		packet[6] = 6 // Next header: TCP
		packet[7] = 64 // Hop limit
	} else {
		ipHdrLen = 20
		// Minimal IPv4 header
		packet = make([]byte, ipHdrLen+32) // 20 IP + 32 TCP (20 base + 12 options with NOPs)
		packet[0] = 0x45 // Version 4, IHL 5
		binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet))) // Total length
		packet[9] = 6 // Protocol: TCP
	}

	// TCP header starts after IP header
	tcpStart := ipHdrLen

	// Set TCP data offset to 8 (32 bytes header)
	packet[tcpStart+12] = 0x80 // Data offset = 8 (8 * 4 = 32 bytes)

	// Add TCP timestamp option
	// Format: NOP (1) | NOP (1) | Kind=8 (1) | Len=10 (1) | TSval (4) | TSecr (4)
	optStart := tcpStart + 20
	packet[optStart] = TCPOptionNOP        // NOP
	packet[optStart+1] = TCPOptionNOP      // NOP
	packet[optStart+2] = TCPOptionTimestamp // Kind = 8
	packet[optStart+3] = TCPTimestampLength // Length = 10
	binary.BigEndian.PutUint32(packet[optStart+4:optStart+8], tsval) // TSval
	binary.BigEndian.PutUint32(packet[optStart+8:optStart+12], 0)    // TSecr

	return packet
}

func TestDecreaseTCPTimestamp_IPv4(t *testing.T) {
	originalTSval := uint32(1000000)
	decrease := uint32(600000)
	expected := originalTSval - decrease

	packet := buildTestTCPPacketWithTimestamp(false, originalTSval)

	result := DecreaseTCPTimestamp(packet, decrease, false)
	if !result {
		t.Fatal("DecreaseTCPTimestamp returned false, expected true")
	}

	// Verify the timestamp was decreased
	ipHdrLen := 20
	tcpStart := ipHdrLen
	optStart := tcpStart + 20
	newTSval := binary.BigEndian.Uint32(packet[optStart+4 : optStart+8])

	if newTSval != expected {
		t.Errorf("TSval not decreased correctly. Expected %d, got %d", expected, newTSval)
	}
}

func TestDecreaseTCPTimestamp_IPv6(t *testing.T) {
	originalTSval := uint32(2000000)
	decrease := uint32(600000)
	expected := originalTSval - decrease

	packet := buildTestTCPPacketWithTimestamp(true, originalTSval)

	result := DecreaseTCPTimestamp(packet, decrease, true)
	if !result {
		t.Fatal("DecreaseTCPTimestamp returned false, expected true")
	}

	// Verify the timestamp was decreased
	ipHdrLen := 40
	tcpStart := ipHdrLen
	optStart := tcpStart + 20
	newTSval := binary.BigEndian.Uint32(packet[optStart+4 : optStart+8])

	if newTSval != expected {
		t.Errorf("TSval not decreased correctly. Expected %d, got %d", expected, newTSval)
	}
}

func TestDecreaseTCPTimestamp_NoTimestamp(t *testing.T) {
	// Create a packet without timestamp option
	packet := make([]byte, 40) // 20 IP + 20 TCP (no options)
	packet[0] = 0x45           // Version 4, IHL 5
	packet[9] = 6              // Protocol: TCP
	packet[20+12] = 0x50       // Data offset = 5 (20 bytes, no options)

	result := DecreaseTCPTimestamp(packet, 600000, false)
	if result {
		t.Error("DecreaseTCPTimestamp should return false for packet without timestamp")
	}
}

func TestDecreaseTCPTimestamp_Underflow(t *testing.T) {
	// Test with TSval smaller than decrease amount (should underflow)
	originalTSval := uint32(100000)
	decrease := uint32(600000)

	packet := buildTestTCPPacketWithTimestamp(false, originalTSval)

	result := DecreaseTCPTimestamp(packet, decrease, false)
	if !result {
		t.Fatal("DecreaseTCPTimestamp returned false, expected true")
	}

	// Verify underflow behavior (wraps around)
	ipHdrLen := 20
	tcpStart := ipHdrLen
	optStart := tcpStart + 20
	newTSval := binary.BigEndian.Uint32(packet[optStart+4 : optStart+8])

	// With uint32, this should wrap around
	expected := originalTSval - decrease // Will wrap around due to uint32
	if newTSval != expected {
		t.Errorf("TSval underflow not handled correctly. Expected %d, got %d", expected, newTSval)
	}
}
