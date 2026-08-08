package nfq

import (
	"encoding/binary"
	"net"
	"testing"
)

func buildClientSynV4(srcIP, dstIP string, sport, dport uint16, seq uint32) []byte {
	pkt := make([]byte, 40)
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], 40)
	pkt[8] = 64
	pkt[9] = 6
	copy(pkt[12:16], net.ParseIP(srcIP).To4())
	copy(pkt[16:20], net.ParseIP(dstIP).To4())

	binary.BigEndian.PutUint16(pkt[20:22], sport)
	binary.BigEndian.PutUint16(pkt[22:24], dport)
	binary.BigEndian.PutUint32(pkt[24:28], seq)
	pkt[32] = 0x50
	pkt[33] = 0x02
	return pkt
}

func TestBuildSynRSTV4AcknowledgesTheSyn(t *testing.T) {
	client := net.ParseIP("192.168.1.100")
	server := net.ParseIP("185.199.110.133")
	syn := buildClientSynV4("192.168.1.100", "185.199.110.133", 51234, 443, 0xAABBCCDD)

	rst := buildSynRSTV4(syn, 20, client, server)
	if rst == nil {
		t.Fatal("buildSynRSTV4 returned nothing for a valid SYN")
	}

	if flags := rst[33]; flags != 0x14 {
		t.Errorf("flags = 0x%02x, want RST|ACK (0x14); a bare RST is ignored by a socket in SYN_SENT", flags)
	}
	if ack := binary.BigEndian.Uint32(rst[28:32]); ack != 0xAABBCCDE {
		t.Errorf("ack = 0x%08x, want the client ISN plus one", ack)
	}
	if !net.IP(rst[12:16]).Equal(server) {
		t.Errorf("source = %s, want the server address so the client accepts it", net.IP(rst[12:16]))
	}
	if !net.IP(rst[16:20]).Equal(client) {
		t.Errorf("destination = %s, want the client address", net.IP(rst[16:20]))
	}
	if sport := binary.BigEndian.Uint16(rst[20:22]); sport != 443 {
		t.Errorf("source port = %d, want 443", sport)
	}
	if dport := binary.BigEndian.Uint16(rst[22:24]); dport != 51234 {
		t.Errorf("destination port = %d, want 51234", dport)
	}
}

func TestBuildSynRSTV4RejectsShortPacket(t *testing.T) {
	if rst := buildSynRSTV4(make([]byte, 24), 20, net.ParseIP("1.1.1.1"), net.ParseIP("2.2.2.2")); rst != nil {
		t.Errorf("expected no reset for a truncated packet")
	}
}

func TestBuildSynRSTV6AcknowledgesTheSyn(t *testing.T) {
	client := net.ParseIP("2001:db8::100")
	server := net.ParseIP("2606:50c0:8000::154")

	syn := make([]byte, 60)
	syn[0] = 0x60
	binary.BigEndian.PutUint16(syn[4:6], 20)
	syn[6] = 6
	syn[7] = 64
	copy(syn[8:24], client.To16())
	copy(syn[24:40], server.To16())
	binary.BigEndian.PutUint16(syn[40:42], 51234)
	binary.BigEndian.PutUint16(syn[42:44], 443)
	binary.BigEndian.PutUint32(syn[44:48], 0x11223344)
	syn[52] = 0x50
	syn[53] = 0x02

	rst := buildSynRSTV6(syn, client, server)
	if rst == nil {
		t.Fatal("buildSynRSTV6 returned nothing for a valid SYN")
	}
	if flags := rst[53]; flags != 0x14 {
		t.Errorf("flags = 0x%02x, want RST|ACK (0x14)", flags)
	}
	if ack := binary.BigEndian.Uint32(rst[48:52]); ack != 0x11223345 {
		t.Errorf("ack = 0x%08x, want the client ISN plus one", ack)
	}
	if !net.IP(rst[8:24]).Equal(server) {
		t.Errorf("source = %s, want the server address", net.IP(rst[8:24]))
	}
}

func TestBuildSynRSTV6RejectsShortPacket(t *testing.T) {
	if rst := buildSynRSTV6(make([]byte, 48), net.ParseIP("2001:db8::1"), net.ParseIP("2001:db8::2")); rst != nil {
		t.Errorf("expected no reset for a truncated packet")
	}
}
