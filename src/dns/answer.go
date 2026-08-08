package dns

import (
	"encoding/binary"
	"net"
	"strings"
)

const (
	rrTypeA     = 1
	rrTypeCNAME = 5
	rrTypeAAAA  = 28
	rrTypeOPT   = 41

	ednsDNSSECOK = 0x8000

	maxUnexpandedUDPResponse = 512
)

const (
	FilterUnchanged = iota
	FilterRewritten
	FilterAllDropped
)

type answerRR struct {
	name  string
	typ   uint16
	class uint16
	ttl   uint32
	rdata []byte
	ip    net.IP
}

func readName(payload []byte, offset int) (string, int, bool) {
	next, ok := skipDNSName(payload, offset)
	if !ok {
		return "", 0, false
	}

	var labels []string
	pos := offset
	jumps := 0
	for {
		if pos >= len(payload) {
			return "", 0, false
		}
		l := payload[pos]
		if l == 0 {
			break
		}
		if l&0xC0 == 0xC0 {
			if pos+1 >= len(payload) {
				return "", 0, false
			}
			ptr := int(binary.BigEndian.Uint16(payload[pos:pos+2]) & 0x3FFF)
			if ptr >= len(payload) {
				return "", 0, false
			}
			jumps++
			if jumps > 16 {
				return "", 0, false
			}
			pos = ptr
			continue
		}
		pos++
		if pos+int(l) > len(payload) {
			return "", 0, false
		}
		labels = append(labels, string(payload[pos:pos+int(l)]))
		pos += int(l)
	}

	return strings.Join(labels, "."), next, true
}

func encodeName(name string) []byte {
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		return []byte{0}
	}
	buf := make([]byte, 0, len(name)+2)
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			continue
		}
		buf = append(buf, byte(len(label)))
		buf = append(buf, label...)
	}
	return append(buf, 0)
}

func QuestionType(payload []byte) (uint16, bool) {
	if len(payload) < 12 || binary.BigEndian.Uint16(payload[4:6]) != 1 {
		return 0, false
	}
	end, ok := skipDNSName(payload, 12)
	if !ok || end+4 > len(payload) {
		return 0, false
	}
	return binary.BigEndian.Uint16(payload[end : end+2]), true
}

func FilterAnswerIPs(payload []byte, ttlCap uint32, drop func(net.IP) bool) ([]byte, int) {
	if len(payload) < 12 || drop == nil {
		return nil, FilterUnchanged
	}
	if payload[2]&0x80 == 0 || payload[2]&0x02 != 0 || payload[3]&0x0F != 0 {
		return nil, FilterUnchanged
	}

	qdCount := int(binary.BigEndian.Uint16(payload[4:6]))
	anCount := int(binary.BigEndian.Uint16(payload[6:8]))
	nsCount := int(binary.BigEndian.Uint16(payload[8:10]))
	arCount := int(binary.BigEndian.Uint16(payload[10:12]))
	if qdCount != 1 || anCount == 0 || nsCount != 0 {
		return nil, FilterUnchanged
	}

	qName, offset, ok := readName(payload, 12)
	if !ok || offset+4 > len(payload) {
		return nil, FilterUnchanged
	}
	questionEnd := offset + 4
	question := payload[12:questionEnd]

	answers := make([]answerRR, 0, anCount)
	offset = questionEnd
	for i := 0; i < anCount; i++ {
		name, next, nameOK := readName(payload, offset)
		if !nameOK || next+10 > len(payload) {
			return nil, FilterUnchanged
		}
		offset = next
		typ := binary.BigEndian.Uint16(payload[offset : offset+2])
		class := binary.BigEndian.Uint16(payload[offset+2 : offset+4])
		ttl := binary.BigEndian.Uint32(payload[offset+4 : offset+8])
		rdLen := int(binary.BigEndian.Uint16(payload[offset+8 : offset+10]))
		offset += 10
		if offset+rdLen > len(payload) {
			return nil, FilterUnchanged
		}
		rdata := payload[offset : offset+rdLen]
		offset += rdLen

		rr := answerRR{name: name, typ: typ, class: class, ttl: ttl, rdata: rdata}
		switch typ {
		case rrTypeA:
			if rdLen != 4 {
				return nil, FilterUnchanged
			}
			rr.ip = net.IP(rdata).To4()
		case rrTypeAAAA:
			if rdLen != 16 {
				return nil, FilterUnchanged
			}
			rr.ip = net.IP(rdata)
		case rrTypeCNAME:
			target, _, targetOK := readName(payload, offset-rdLen)
			if !targetOK {
				return nil, FilterUnchanged
			}
			rr.rdata = encodeName(target)
		default:
			return nil, FilterUnchanged
		}
		answers = append(answers, rr)
	}

	additionalStart := offset
	for i := 0; i < arCount; i++ {
		if offset >= len(payload) || payload[offset] != 0 || offset+11 > len(payload) {
			return nil, FilterUnchanged
		}
		typ := binary.BigEndian.Uint16(payload[offset+1 : offset+3])
		if typ != rrTypeOPT {
			return nil, FilterUnchanged
		}
		if binary.BigEndian.Uint16(payload[offset+7:offset+9])&ednsDNSSECOK != 0 {
			return nil, FilterUnchanged
		}
		rdLen := int(binary.BigEndian.Uint16(payload[offset+9 : offset+11]))
		offset += 11 + rdLen
		if offset > len(payload) {
			return nil, FilterUnchanged
		}
	}
	additional := payload[additionalStart:offset]

	kept := make([]answerRR, 0, len(answers))
	dropped := 0
	addresses := 0
	for _, rr := range answers {
		if rr.ip != nil {
			addresses++
			if drop(rr.ip) {
				dropped++
				continue
			}
		}
		kept = append(kept, rr)
	}

	if dropped == 0 {
		return nil, FilterUnchanged
	}
	if dropped == addresses {
		return nil, FilterAllDropped
	}

	out := make([]byte, 0, len(payload)+64)
	header := make([]byte, 12)
	copy(header, payload[:12])
	binary.BigEndian.PutUint16(header[6:8], uint16(len(kept)))
	out = append(out, header...)
	out = append(out, question...)

	for _, rr := range kept {
		if strings.EqualFold(rr.name, qName) {
			out = append(out, 0xC0, 0x0C)
		} else {
			out = append(out, encodeName(rr.name)...)
		}
		ttl := rr.ttl
		if ttlCap > 0 && ttl > ttlCap {
			ttl = ttlCap
		}
		var fixed [10]byte
		binary.BigEndian.PutUint16(fixed[0:2], rr.typ)
		binary.BigEndian.PutUint16(fixed[2:4], rr.class)
		binary.BigEndian.PutUint32(fixed[4:8], ttl)
		binary.BigEndian.PutUint16(fixed[8:10], uint16(len(rr.rdata)))
		out = append(out, fixed[:]...)
		out = append(out, rr.rdata...)
	}

	out = append(out, additional...)

	if len(out) > len(payload) && len(out) > maxUnexpandedUDPResponse {
		return nil, FilterUnchanged
	}
	return out, FilterRewritten
}

func BuildAnswerFromIPs(payload []byte, ttl uint32, ips []net.IP) []byte {
	if len(payload) < 12 || len(ips) == 0 {
		return nil
	}
	if binary.BigEndian.Uint16(payload[4:6]) != 1 {
		return nil
	}
	end, ok := skipDNSName(payload, 12)
	if !ok || end+4 > len(payload) {
		return nil
	}
	questionEnd := end + 4
	qtype := binary.BigEndian.Uint16(payload[end : end+2])
	if qtype != rrTypeA && qtype != rrTypeAAAA {
		return nil
	}

	out := make([]byte, questionEnd, questionEnd+len(ips)*32)
	copy(out, payload[:questionEnd])
	out[2] |= 0x80
	out[3] &= 0xF0
	binary.BigEndian.PutUint16(out[6:8], 0)
	binary.BigEndian.PutUint16(out[8:10], 0)
	binary.BigEndian.PutUint16(out[10:12], 0)

	count := 0
	for _, ip := range ips {
		var rdata []byte
		if v4 := ip.To4(); v4 != nil {
			if qtype != rrTypeA {
				continue
			}
			rdata = v4
		} else if v6 := ip.To16(); v6 != nil {
			if qtype != rrTypeAAAA {
				continue
			}
			rdata = v6
		} else {
			continue
		}

		out = append(out, 0xC0, 0x0C)
		var fixed [10]byte
		binary.BigEndian.PutUint16(fixed[0:2], qtype)
		binary.BigEndian.PutUint16(fixed[2:4], 1)
		binary.BigEndian.PutUint32(fixed[4:8], ttl)
		binary.BigEndian.PutUint16(fixed[8:10], uint16(len(rdata)))
		out = append(out, fixed[:]...)
		out = append(out, rdata...)
		count++
	}

	if count == 0 {
		return nil
	}
	binary.BigEndian.PutUint16(out[6:8], uint16(count))
	return out
}
