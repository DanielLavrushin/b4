package dns

import (
	"encoding/binary"
	"net"
)

func ParseQueryDomain(payload []byte) (string, bool) {
	// DNS header is 12 bytes
	if len(payload) < 12 {
		return "", false
	}

	pos := 12
	var domain []byte

	for pos < len(payload) {
		length := int(payload[pos])
		if length == 0 {
			break
		}
		if pos+1+length > len(payload) {
			return "", false
		}
		if len(domain) > 0 {
			domain = append(domain, '.')
		}
		domain = append(domain, payload[pos+1:pos+1+length]...)
		pos += 1 + length
	}

	if len(domain) == 0 {
		return "", false
	}
	return string(domain), true
}

func ParseTransactionID(payload []byte) (uint16, bool) {
	if len(payload) < 2 {
		return 0, false
	}
	return binary.BigEndian.Uint16(payload[:2]), true
}

// BuildBlockResponse builds an NXDOMAIN DNS response for the given query,
// echoing the original question. This sinkholes the domain: the client gets
// "no such host" and never obtains an IP, which blocks the domain regardless
// of TLS SNI (defeating HTTP/2 connection coalescing and Encrypted ClientHello).
// Returns nil if the query is too short or malformed to parse.
func BuildBlockResponse(query []byte) []byte {
	if len(query) < 12 {
		return nil
	}
	qend, ok := skipDNSName(query, 12)
	if !ok || qend+4 > len(query) {
		return nil
	}
	questionEnd := qend + 4 // QTYPE + QCLASS

	resp := make([]byte, questionEnd)
	copy(resp, query[:questionEnd])

	// Flags: QR=1, keep Opcode+RD, clear AA/TC, RA=1, RCODE=3 (NXDOMAIN).
	resp[2] = 0x80 | (query[2] & 0x79)
	resp[3] = 0x83

	// QDCOUNT=1, ANCOUNT=0, NSCOUNT=0, ARCOUNT=0.
	binary.BigEndian.PutUint16(resp[4:6], 1)
	binary.BigEndian.PutUint16(resp[6:8], 0)
	binary.BigEndian.PutUint16(resp[8:10], 0)
	binary.BigEndian.PutUint16(resp[10:12], 0)

	return resp
}

func ParseResponseIPs(payload []byte) []net.IP {
	if len(payload) < 12 {
		return nil
	}
	qdCount := int(binary.BigEndian.Uint16(payload[4:6]))
	anCount := int(binary.BigEndian.Uint16(payload[6:8]))
	if anCount == 0 {
		return nil
	}

	offset := 12
	for i := 0; i < qdCount; i++ {
		next, ok := skipDNSName(payload, offset)
		if !ok || next+4 > len(payload) {
			return nil
		}
		offset = next + 4 // QTYPE + QCLASS
	}

	ips := make([]net.IP, 0, anCount)
	for i := 0; i < anCount; i++ {
		next, ok := skipDNSName(payload, offset)
		if !ok || next+10 > len(payload) {
			break
		}
		offset = next

		typ := binary.BigEndian.Uint16(payload[offset : offset+2])
		offset += 2 // TYPE
		offset += 2 // CLASS
		offset += 4 // TTL

		rdLen := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
		offset += 2
		if offset+rdLen > len(payload) {
			break
		}

		switch typ {
		case 1: // A
			if rdLen == 4 {
				ip := make(net.IP, 4)
				copy(ip, payload[offset:offset+4])
				ips = append(ips, ip)
			}
		case 28: // AAAA
			if rdLen == 16 {
				ip := make(net.IP, 16)
				copy(ip, payload[offset:offset+16])
				ips = append(ips, ip)
			}
		}

		offset += rdLen
	}

	return ips
}

func skipDNSName(payload []byte, start int) (int, bool) {
	if start >= len(payload) {
		return 0, false
	}
	pos := start
	jumps := 0
	jumped := false
	next := start

	for {
		if pos >= len(payload) {
			return 0, false
		}
		l := payload[pos]
		if l == 0 {
			if !jumped {
				next = pos + 1
			}
			return next, true
		}
		// compressed pointer
		if l&0xC0 == 0xC0 {
			if pos+1 >= len(payload) {
				return 0, false
			}
			ptr := int(binary.BigEndian.Uint16(payload[pos:pos+2]) & 0x3FFF)
			if ptr >= len(payload) {
				return 0, false
			}
			if !jumped {
				next = pos + 2
			}
			pos = ptr
			jumped = true
			jumps++
			if jumps > 16 {
				return 0, false
			}
			continue
		}

		pos++
		if pos+int(l) > len(payload) {
			return 0, false
		}
		pos += int(l)
		if !jumped {
			next = pos
		}
	}
}
