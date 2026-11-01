package sock

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

func TestBuildICMPv4Reject_Structure(t *testing.T) {
	origPacket := make([]byte, 28)
	origPacket[0] = 0x45
	binary.BigEndian.PutUint16(origPacket[2:4], 28)
	origPacket[9] = 17
	copy(origPacket[12:16], net.IPv4(10, 0, 0, 1).To4())
	copy(origPacket[16:20], net.IPv4(93, 184, 216, 34).To4())
	binary.BigEndian.PutUint16(origPacket[20:22], 12345)
	binary.BigEndian.PutUint16(origPacket[22:24], 443)

	clientIP := net.IPv4(10, 0, 0, 1).To4()
	serverIP := net.IPv4(93, 184, 216, 34).To4()

	icmp := BuildICMPv4Reject(origPacket, clientIP, serverIP)

	if icmp[0] != 0x45 {
		t.Fatalf("expected IPv4 version+IHL 0x45, got 0x%02x", icmp[0])
	}

	totalLen := binary.BigEndian.Uint16(icmp[2:4])
	expectedLen := uint16(20 + 8 + 28)
	if totalLen != expectedLen {
		t.Fatalf("expected total length %d, got %d", expectedLen, totalLen)
	}

	if icmp[9] != 1 {
		t.Fatalf("expected protocol ICMP (1), got %d", icmp[9])
	}

	if !net.IP(icmp[12:16]).Equal(serverIP) {
		t.Fatalf("expected src IP %s, got %s", serverIP, net.IP(icmp[12:16]))
	}
	if !net.IP(icmp[16:20]).Equal(clientIP) {
		t.Fatalf("expected dst IP %s, got %s", clientIP, net.IP(icmp[16:20]))
	}

	if icmp[20] != 3 {
		t.Fatalf("expected ICMP type 3, got %d", icmp[20])
	}
	if icmp[21] != 3 {
		t.Fatalf("expected ICMP code 3, got %d", icmp[21])
	}

	quotedSrcPort := binary.BigEndian.Uint16(icmp[48:50])
	if quotedSrcPort != 12345 {
		t.Fatalf("expected quoted src port 12345, got %d", quotedSrcPort)
	}
}

func TestBuildICMPv4Reject_Checksum(t *testing.T) {
	origPacket := make([]byte, 28)
	origPacket[0] = 0x45
	binary.BigEndian.PutUint16(origPacket[2:4], 28)
	origPacket[9] = 17
	copy(origPacket[12:16], net.IPv4(10, 0, 0, 1).To4())
	copy(origPacket[16:20], net.IPv4(93, 184, 216, 34).To4())

	icmp := BuildICMPv4Reject(origPacket, net.IPv4(10, 0, 0, 1).To4(), net.IPv4(93, 184, 216, 34).To4())

	icmpData := icmp[20:]
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
	if uint16(sum) != 0xFFFF {
		t.Fatalf("ICMP checksum verification failed: got 0x%04x, expected 0xFFFF", uint16(sum))
	}
}

func TestBuildICMPv6Reject_Structure(t *testing.T) {
	origPacket := make([]byte, 48)
	origPacket[0] = 0x60
	binary.BigEndian.PutUint16(origPacket[4:6], 8)
	origPacket[6] = 17
	origPacket[7] = 64

	clientIP := net.ParseIP("2001:db8::1").To16()
	serverIP := net.ParseIP("2001:db8::2").To16()
	copy(origPacket[8:24], clientIP)
	copy(origPacket[24:40], serverIP)
	binary.BigEndian.PutUint16(origPacket[40:42], 12345)
	binary.BigEndian.PutUint16(origPacket[42:44], 443)

	icmp := BuildICMPv6Reject(origPacket, clientIP, serverIP)

	if icmp[0]>>4 != 6 {
		t.Fatalf("expected IPv6 version 6, got %d", icmp[0]>>4)
	}

	if icmp[6] != 58 {
		t.Fatalf("expected next header ICMPv6 (58), got %d", icmp[6])
	}

	if !net.IP(icmp[8:24]).Equal(serverIP) {
		t.Fatalf("expected src IP %s, got %s", serverIP, net.IP(icmp[8:24]))
	}
	if !net.IP(icmp[24:40]).Equal(clientIP) {
		t.Fatalf("expected dst IP %s, got %s", clientIP, net.IP(icmp[24:40]))
	}

	if icmp[40] != 1 {
		t.Fatalf("expected ICMPv6 type 1, got %d", icmp[40])
	}
	if icmp[41] != 4 {
		t.Fatalf("expected ICMPv6 code 4, got %d", icmp[41])
	}
}

func TestBuildICMPv6Reject_Checksum(t *testing.T) {
	origPacket := make([]byte, 48)
	origPacket[0] = 0x60
	binary.BigEndian.PutUint16(origPacket[4:6], 8)
	origPacket[6] = 17
	origPacket[7] = 64

	clientIP := net.ParseIP("2001:db8::1").To16()
	serverIP := net.ParseIP("2001:db8::2").To16()
	copy(origPacket[8:24], clientIP)
	copy(origPacket[24:40], serverIP)

	icmp := BuildICMPv6Reject(origPacket, clientIP, serverIP)

	var sum uint32
	for i := 0; i < 16; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(icmp[8+i : 10+i]))
	}
	for i := 0; i < 16; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(icmp[24+i : 26+i]))
	}
	icmpLen := binary.BigEndian.Uint16(icmp[4:6])
	sum += uint32(icmpLen)
	sum += 58

	icmpData := icmp[40:]
	for i := 0; i < len(icmpData)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(icmpData[i : i+2]))
	}
	if len(icmpData)%2 == 1 {
		sum += uint32(icmpData[len(icmpData)-1]) << 8
	}
	for sum > 0xFFFF {
		sum = (sum >> 16) + (sum & 0xFFFF)
	}
	if uint16(sum) != 0xFFFF {
		t.Fatalf("ICMPv6 checksum verification failed: got 0x%04x, expected 0xFFFF", uint16(sum))
	}
}

func TestBuildICMPv4Reject_InvalidInput(t *testing.T) {
	if BuildICMPv4Reject(nil, net.IPv4(1, 1, 1, 1).To4(), net.IPv4(2, 2, 2, 2).To4()) != nil {
		t.Fatal("expected nil for nil packet")
	}
	if BuildICMPv4Reject(make([]byte, 10), net.IPv4(1, 1, 1, 1).To4(), net.IPv4(2, 2, 2, 2).To4()) != nil {
		t.Fatal("expected nil for too-short packet")
	}
	bad := make([]byte, 20)
	bad[0] = 0x43
	if BuildICMPv4Reject(bad, net.IPv4(1, 1, 1, 1).To4(), net.IPv4(2, 2, 2, 2).To4()) != nil {
		t.Fatal("expected nil for bad IHL")
	}
	if BuildICMPv6Reject(nil, make([]byte, 16), make([]byte, 16)) != nil {
		t.Fatal("expected nil for nil v6 packet")
	}
	if BuildICMPv6Reject(make([]byte, 20), make([]byte, 16), make([]byte, 16)) != nil {
		t.Fatal("expected nil for too-short v6 packet")
	}
}

func foldsToAllOnes(b []byte) bool {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum > 0xFFFF {
		sum = (sum >> 16) + (sum & 0xFFFF)
	}
	return uint16(sum) == 0xFFFF
}

func postSNATDatagram() []byte {
	p := make([]byte, 28)
	p[0] = 0x45
	binary.BigEndian.PutUint16(p[2:4], 28)
	p[8] = 64
	p[9] = 17
	copy(p[12:16], net.IPv4(94, 189, 76, 227).To4())
	copy(p[16:20], net.IPv4(1, 2, 3, 4).To4())
	FixIPv4Checksum(p[:20])
	binary.BigEndian.PutUint16(p[20:22], 44100)
	binary.BigEndian.PutUint16(p[22:24], 443)
	binary.BigEndian.PutUint16(p[24:26], 8)
	binary.BigEndian.PutUint16(p[26:28], 0x1234)
	return p
}

func TestBuildICMPv4RejectQuotesTheDeviceItIsSentTo(t *testing.T) {
	orig := postSNATDatagram()
	device := net.IPv4(192, 168, 1, 50).To4()

	icmp := BuildICMPv4Reject(orig, device, net.IPv4(1, 2, 3, 4).To4())

	quote := icmp[28:]
	if !net.IP(quote[12:16]).Equal(device) {
		t.Fatalf("the quote must name the device the reject is sent to, or its socket never takes it, got %s", net.IP(quote[12:16]))
	}
	if !bytes.Equal(quote[16:], orig[16:]) || !bytes.Equal(quote[:10], orig[:10]) {
		t.Fatal("only the quoted source address and its header checksum may change")
	}
	if !foldsToAllOnes(quote[:20]) {
		t.Fatal("the quoted IP header checksum must match the rewritten source address")
	}
	if !foldsToAllOnes(icmp[20:]) {
		t.Fatal("the ICMP checksum must cover the rewritten quote")
	}
}

func TestBuildICMPv4RejectQuotesAPacketFromTheDeviceUnchanged(t *testing.T) {
	orig := postSNATDatagram()

	icmp := BuildICMPv4Reject(orig, net.IPv4(94, 189, 76, 227).To4(), net.IPv4(1, 2, 3, 4).To4())

	if !bytes.Equal(icmp[28:], orig) {
		t.Fatal("a packet that already comes from the device is quoted byte for byte")
	}
}

func TestBuildICMPv4Reject_ShortPacket(t *testing.T) {
	origPacket := make([]byte, 20)
	origPacket[0] = 0x45
	binary.BigEndian.PutUint16(origPacket[2:4], 20)

	icmp := BuildICMPv4Reject(origPacket, net.IPv4(10, 0, 0, 1).To4(), net.IPv4(1, 1, 1, 1).To4())

	expectedLen := 20 + 8 + 20
	if len(icmp) != expectedLen {
		t.Fatalf("expected length %d for short packet, got %d", expectedLen, len(icmp))
	}
}
