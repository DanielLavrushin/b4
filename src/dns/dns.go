package dns

import (
	"encoding/binary"
	"net"
	"strings"
)

const (
	MaxLabelLen = 63
	MaxNameLen  = 255
)

func ParseQueryDomain(payload []byte) (string, bool) {
	if len(payload) < 12 {
		return "", false
	}
	if binary.BigEndian.Uint16(payload[4:6]) == 0 {
		return "", false
	}

	pos := 12
	encoded := 1
	var domain []byte

	for pos < len(payload) {
		length := int(payload[pos])
		if length == 0 {
			if len(domain) == 0 || pos+5 > len(payload) {
				return "", false
			}
			return string(domain), true
		}
		if length > MaxLabelLen || pos+1+length > len(payload) {
			return "", false
		}
		encoded += 1 + length
		if encoded > MaxNameLen {
			return "", false
		}
		if len(domain) > 0 {
			domain = append(domain, '.')
		}
		domain = append(domain, payload[pos+1:pos+1+length]...)
		pos += 1 + length
	}

	return "", false
}

const hexDigits = "0123456789abcdef"

func nameByteUnsafe(c byte) bool {
	return c < 0x20 || c == 0x7f
}

func SafeName(name string) string {
	unsafeAt := -1
	for i := 0; i < len(name); i++ {
		if nameByteUnsafe(name[i]) {
			unsafeAt = i
			break
		}
	}
	if unsafeAt < 0 {
		return name
	}

	var b strings.Builder
	b.Grow(len(name) + 16)
	b.WriteString(name[:unsafeAt])
	for i := unsafeAt; i < len(name); i++ {
		c := name[i]
		if nameByteUnsafe(c) {
			b.WriteString(`\x`)
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0x0f])
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func ParseTransactionID(payload []byte) (uint16, bool) {
	if len(payload) < 2 {
		return 0, false
	}
	return binary.BigEndian.Uint16(payload[:2]), true
}

const (
	RcodeNoError  uint8 = 0
	RcodeServFail uint8 = 2
	RcodeNXDomain uint8 = 3
	RcodeRefused  uint8 = 5
)

func ResponseRcode(payload []byte) (uint8, bool) {
	if len(payload) < 12 {
		return 0, false
	}
	return payload[3] & 0x0F, true
}

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

	resp[2] = 0x80 | (query[2] & 0x79)
	resp[3] = 0x83

	binary.BigEndian.PutUint16(resp[4:6], 1)
	binary.BigEndian.PutUint16(resp[6:8], 0)
	binary.BigEndian.PutUint16(resp[8:10], 0)
	binary.BigEndian.PutUint16(resp[10:12], 0)

	return resp
}

func BuildServfailResponse(query []byte) []byte {
	resp := BuildBlockResponse(query)
	if resp == nil {
		return nil
	}
	// Set only the RCODE nibble to SERVFAIL (2); preserve the upper flag bits.
	resp[3] = (resp[3] & 0xF0) | 0x02
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
