package dns

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
)

func encodeDNSName(name string) []byte {
	if name == "" {
		return []byte{0}
	}
	parts := strings.Split(name, ".")
	out := make([]byte, 0, len(name)+2)
	for _, p := range parts {
		out = append(out, byte(len(p)))
		out = append(out, []byte(p)...)
	}
	out = append(out, 0)
	return out
}

func buildDNSResponse(qdCount, anCount uint16, body []byte) []byte {
	msg := make([]byte, 12, 12+len(body))
	binary.BigEndian.PutUint16(msg[0:2], 0x1234)  // ID
	binary.BigEndian.PutUint16(msg[2:4], 0x8180)  // standard response, no error
	binary.BigEndian.PutUint16(msg[4:6], qdCount) // QDCOUNT
	binary.BigEndian.PutUint16(msg[6:8], anCount) // ANCOUNT
	msg = append(msg, body...)
	return msg
}

func buildDNSQuery(txid uint16, name string, qtype uint16) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], txid)
	binary.BigEndian.PutUint16(msg[2:4], 0x0100) // RD set, standard query
	binary.BigEndian.PutUint16(msg[4:6], 1)      // QDCOUNT
	msg = append(msg, encodeDNSName(name)...)
	q := make([]byte, 4)
	binary.BigEndian.PutUint16(q[0:2], qtype)
	binary.BigEndian.PutUint16(q[2:4], 1) // IN
	return append(msg, q...)
}

func TestBuildBlockResponse(t *testing.T) {
	t.Run("nxdomain echoes question and sets response flags", func(t *testing.T) {
		query := buildDNSQuery(0xABCD, "ads.example.com", 1)
		resp := BuildBlockResponse(query)
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if binary.BigEndian.Uint16(resp[0:2]) != 0xABCD {
			t.Errorf("txid not preserved: got 0x%x", binary.BigEndian.Uint16(resp[0:2]))
		}
		if resp[2]&0x80 == 0 {
			t.Error("QR bit not set (should be a response)")
		}
		if resp[2]&0x01 == 0 {
			t.Error("RD bit not preserved from query")
		}
		if resp[3]&0x0f != 3 {
			t.Errorf("RCODE should be 3 (NXDOMAIN), got %d", resp[3]&0x0f)
		}
		if binary.BigEndian.Uint16(resp[4:6]) != 1 {
			t.Error("QDCOUNT should be 1")
		}
		if binary.BigEndian.Uint16(resp[6:8]) != 0 {
			t.Error("ANCOUNT should be 0")
		}
		domain, ok := ParseQueryDomain(resp)
		if !ok || domain != "ads.example.com" {
			t.Errorf("question not echoed: got %q ok=%v", domain, ok)
		}
	})

	t.Run("short payload returns nil", func(t *testing.T) {
		if BuildBlockResponse([]byte{0x00, 0x01}) != nil {
			t.Error("expected nil for short payload")
		}
	})
}

func TestParseTransactionID(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		got, ok := ParseTransactionID([]byte{0x12, 0x34, 0x00})
		if !ok {
			t.Fatalf("expected ok=true")
		}
		if got != 0x1234 {
			t.Fatalf("expected txid 0x1234, got 0x%x", got)
		}
	})

	t.Run("short payload", func(t *testing.T) {
		_, ok := ParseTransactionID([]byte{0x12})
		if ok {
			t.Fatalf("expected ok=false for short payload")
		}
	})
}

func TestSkipDNSName(t *testing.T) {
	t.Run("plain name", func(t *testing.T) {
		msg := append(encodeDNSName("ifconfig.me"), 0x00)
		next, ok := skipDNSName(msg, 0)
		if !ok {
			t.Fatalf("expected ok=true")
		}
		if next != len(encodeDNSName("ifconfig.me")) {
			t.Fatalf("unexpected next offset: %d", next)
		}
	})

	t.Run("compressed pointer", func(t *testing.T) {
		base := encodeDNSName("ifconfig.me")
		// name at offset 0, then pointer to 0
		msg := append([]byte{}, base...)
		msg = append(msg, 0xC0, 0x00)

		next, ok := skipDNSName(msg, len(base))
		if !ok {
			t.Fatalf("expected ok=true for compressed name")
		}
		if next != len(base)+2 {
			t.Fatalf("unexpected next offset: %d", next)
		}
	})

	t.Run("pointer loop", func(t *testing.T) {
		// pointer to itself
		msg := []byte{0xC0, 0x00}
		_, ok := skipDNSName(msg, 0)
		if ok {
			t.Fatalf("expected ok=false for cyclic pointer")
		}
	})
}

func TestParseResponseIPs(t *testing.T) {
	t.Run("extracts A and AAAA with compressed answer names", func(t *testing.T) {
		qname := encodeDNSName("ifconfig.me")
		body := make([]byte, 0, 128)

		// Question
		body = append(body, qname...)
		body = append(body, 0x00, 0x01) // QTYPE A
		body = append(body, 0x00, 0x01) // QCLASS IN

		// Answer #1: A, name pointer to offset 12 (question name)
		body = append(body, 0xC0, 0x0C)             // NAME ptr
		body = append(body, 0x00, 0x01)             // TYPE A
		body = append(body, 0x00, 0x01)             // CLASS IN
		body = append(body, 0x00, 0x00, 0x00, 0x3C) // TTL
		body = append(body, 0x00, 0x04)             // RDLENGTH
		body = append(body, 1, 2, 3, 4)             // RDATA

		// Answer #2: AAAA
		ipv6 := net.ParseIP("2001:db8::1").To16()
		body = append(body, 0xC0, 0x0C)             // NAME ptr
		body = append(body, 0x00, 0x1C)             // TYPE AAAA
		body = append(body, 0x00, 0x01)             // CLASS IN
		body = append(body, 0x00, 0x00, 0x00, 0x3C) // TTL
		body = append(body, 0x00, 0x10)             // RDLENGTH
		body = append(body, ipv6...)

		msg := buildDNSResponse(1, 2, body)
		ips := ParseResponseIPs(msg)
		if len(ips) != 2 {
			t.Fatalf("expected 2 IPs, got %d", len(ips))
		}
		if !ips[0].Equal(net.IPv4(1, 2, 3, 4)) {
			t.Fatalf("unexpected first IP: %v", ips[0])
		}
		if !ips[1].Equal(ipv6) {
			t.Fatalf("unexpected second IP: %v", ips[1])
		}
	})

	t.Run("handles multi-question packet", func(t *testing.T) {
		q1 := encodeDNSName("ifconfig.me")
		q2 := encodeDNSName("example.org")

		body := make([]byte, 0, 128)
		// Q1
		body = append(body, q1...)
		body = append(body, 0x00, 0x01, 0x00, 0x01)
		// Q2
		body = append(body, q2...)
		body = append(body, 0x00, 0x01, 0x00, 0x01)

		// Answer for Q1
		body = append(body, 0xC0, 0x0C) // ptr to first name
		body = append(body, 0x00, 0x01) // A
		body = append(body, 0x00, 0x01) // IN
		body = append(body, 0x00, 0x00, 0x00, 0x1E)
		body = append(body, 0x00, 0x04)
		body = append(body, 9, 9, 9, 9)

		msg := buildDNSResponse(2, 1, body)
		ips := ParseResponseIPs(msg)
		if len(ips) != 1 {
			t.Fatalf("expected 1 IP, got %d", len(ips))
		}
		if !ips[0].Equal(net.IPv4(9, 9, 9, 9)) {
			t.Fatalf("unexpected IP: %v", ips[0])
		}
	})

	t.Run("returns empty on malformed answer name pointer", func(t *testing.T) {
		qname := encodeDNSName("ifconfig.me")
		body := make([]byte, 0, 64)

		body = append(body, qname...)
		body = append(body, 0x00, 0x01, 0x00, 0x01) // question

		// invalid answer name pointer out of packet bounds
		body = append(body, 0xC0, 0xFF)
		body = append(body, 0x00, 0x01, 0x00, 0x01)
		body = append(body, 0x00, 0x00, 0x00, 0x3C)
		body = append(body, 0x00, 0x04)
		body = append(body, 8, 8, 8, 8)

		msg := buildDNSResponse(1, 1, body)
		ips := ParseResponseIPs(msg)
		if len(ips) != 0 {
			t.Fatalf("expected no IPs for malformed payload, got %d", len(ips))
		}
	})
}
