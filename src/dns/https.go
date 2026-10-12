package dns

import (
	"encoding/binary"
	"strings"
)

const (
	TypeSVCB  uint16 = 64
	TypeHTTPS uint16 = 65

	svcParamMandatory = 0
	svcParamECH       = 5
	echConfigVersion  = 0xfe0d

	rrTypeNS    = 2
	rrTypeSOA   = 6
	rrTypePTR   = 12
	rrTypeMX    = 15
	rrTypeRRSIG = 46

	headerFlagAD = 0x20
)

func ParseResponseECH(payload []byte) []byte {
	if len(payload) < 12 {
		return nil
	}
	qdCount := int(binary.BigEndian.Uint16(payload[4:6]))
	anCount := int(binary.BigEndian.Uint16(payload[6:8]))

	offset := 12
	for i := 0; i < qdCount; i++ {
		next, ok := skipDNSName(payload, offset)
		if !ok || next+4 > len(payload) {
			return nil
		}
		offset = next + 4
	}

	var best []byte
	var bestPriority uint16
	for i := 0; i < anCount; i++ {
		next, ok := skipDNSName(payload, offset)
		if !ok || next+10 > len(payload) {
			break
		}
		typ := binary.BigEndian.Uint16(payload[next : next+2])
		rdLen := int(binary.BigEndian.Uint16(payload[next+8 : next+10]))
		start := next + 10
		if start+rdLen > len(payload) {
			break
		}
		offset = start + rdLen
		if typ != TypeHTTPS && typ != TypeSVCB {
			continue
		}
		priority, ech := svcbECH(payload, start, offset)
		if priority == 0 || len(ech) == 0 {
			continue
		}
		if best == nil || priority < bestPriority {
			best, bestPriority = ech, priority
		}
	}
	return best
}

func svcbECH(payload []byte, start, end int) (uint16, []byte) {
	if start+2 > end {
		return 0, nil
	}
	priority := binary.BigEndian.Uint16(payload[start : start+2])
	pos, ok := skipDNSName(payload, start+2)
	if !ok || pos > end {
		return 0, nil
	}
	for pos+4 <= end {
		key := binary.BigEndian.Uint16(payload[pos : pos+2])
		size := int(binary.BigEndian.Uint16(payload[pos+2 : pos+4]))
		pos += 4
		if pos+size > end {
			return 0, nil
		}
		if key == svcParamECH {
			return priority, append([]byte(nil), payload[pos:pos+size]...)
		}
		pos += size
	}
	return priority, nil
}

func ECHPublicName(configList []byte) string {
	if len(configList) < 2 {
		return ""
	}
	end := 2 + int(binary.BigEndian.Uint16(configList[0:2]))
	if end > len(configList) {
		return ""
	}
	for pos := 2; pos+4 <= end; {
		version := binary.BigEndian.Uint16(configList[pos : pos+2])
		size := int(binary.BigEndian.Uint16(configList[pos+2 : pos+4]))
		pos += 4
		if pos+size > end {
			return ""
		}
		if version == echConfigVersion {
			if name := echPublicName(configList[pos : pos+size]); name != "" {
				return name
			}
		}
		pos += size
	}
	return ""
}

func echPublicName(contents []byte) string {
	pos := 3
	for range 2 {
		if pos+2 > len(contents) {
			return ""
		}
		pos += 2 + int(binary.BigEndian.Uint16(contents[pos:pos+2]))
	}
	pos++
	if pos >= len(contents) {
		return ""
	}
	size := int(contents[pos])
	pos++
	if size == 0 || pos+size > len(contents) {
		return ""
	}
	return strings.ToLower(string(contents[pos : pos+size]))
}

func StripECH(payload []byte, maxSize int) ([]byte, int) {
	if len(payload) < 12 || payload[2]&0x80 == 0 || payload[2]&0x02 != 0 || payload[3]&0x0F != 0 {
		return nil, FilterUnchanged
	}
	if binary.BigEndian.Uint16(payload[4:6]) != 1 {
		return nil, FilterUnchanged
	}
	qName, offset, ok := readName(payload, 12)
	if !ok || offset+4 > len(payload) {
		return nil, FilterUnchanged
	}
	questionEnd := offset + 4

	var sections [3][]answerRR
	offset = questionEnd
	stripped := false
	ednsUDPSize := 0
	for s := range sections {
		count := int(binary.BigEndian.Uint16(payload[6+2*s : 8+2*s]))
		for range count {
			rr, next, ok := readPortableRR(payload, offset)
			if !ok {
				return nil, FilterUnchanged
			}
			offset = next
			if rr.typ == TypeHTTPS || rr.typ == TypeSVCB {
				if rdata, ok := svcbWithoutECH(rr.rdata); ok {
					rr.rdata = rdata
					stripped = true
				}
			}
			if rr.typ == rrTypeOPT && int(rr.class) > ednsUDPSize {
				ednsUDPSize = int(rr.class)
			}
			sections[s] = append(sections[s], rr)
		}
	}
	if !stripped {
		return nil, FilterUnchanged
	}

	out := make([]byte, 12, len(payload))
	copy(out, payload[:12])
	out[3] &^= headerFlagAD
	out = append(out, payload[12:questionEnd]...)
	names := map[string]int{}
	for s, rrs := range sections {
		kept := 0
		for _, rr := range rrs {
			if signsServiceBinding(rr) {
				continue
			}
			out = appendRR(out, rr, qName, names)
			kept++
		}
		binary.BigEndian.PutUint16(out[6+2*s:8+2*s], uint16(kept))
	}

	if len(out) > len(payload) && len(out) > responseSizeLimit(maxSize, ednsUDPSize) {
		return nil, FilterTooLarge
	}
	return out, FilterRewritten
}

func readPortableRR(payload []byte, offset int) (answerRR, int, bool) {
	name, next, ok := readName(payload, offset)
	if !ok || next+10 > len(payload) {
		return answerRR{}, 0, false
	}
	rr := answerRR{
		name:  name,
		typ:   binary.BigEndian.Uint16(payload[next : next+2]),
		class: binary.BigEndian.Uint16(payload[next+2 : next+4]),
		ttl:   binary.BigEndian.Uint32(payload[next+4 : next+8]),
	}
	start := next + 10
	end := start + int(binary.BigEndian.Uint16(payload[next+8:next+10]))
	if end > len(payload) {
		return answerRR{}, 0, false
	}
	rdata, target, ok := portableRData(payload, rr.typ, start, end)
	if !ok {
		return answerRR{}, 0, false
	}
	rr.rdata, rr.target = rdata, target
	return rr, end, true
}

func portableRData(payload []byte, typ uint16, start, end int) ([]byte, string, bool) {
	switch {
	case typ == rrTypeNS || typ == rrTypeCNAME || typ == rrTypePTR:
		name, next, ok := readName(payload, start)
		if !ok || next != end {
			return nil, "", false
		}
		return encodeName(name), name, true
	case typ == rrTypeMX:
		rdata, ok := expandRData(payload, start, end, 2, 1, 0)
		return rdata, "", ok
	case typ == rrTypeSOA:
		rdata, ok := expandRData(payload, start, end, 0, 2, 20)
		return rdata, "", ok
	case legacyCompressedType(typ):
		return nil, "", false
	}
	return payload[start:end], "", true
}

func legacyCompressedType(typ uint16) bool {
	switch typ {
	case 3, 4, 7, 8, 9, 14, 17, 18, 21, 24, 26, 30, 35:
		return true
	}
	return false
}

func expandRData(payload []byte, start, end, prefix, names, suffix int) ([]byte, bool) {
	if start+prefix > end {
		return nil, false
	}
	out := append([]byte(nil), payload[start:start+prefix]...)
	pos := start + prefix
	for range names {
		name, next, ok := readName(payload, pos)
		if !ok || next > end {
			return nil, false
		}
		out = append(out, encodeName(name)...)
		pos = next
	}
	if pos+suffix != end {
		return nil, false
	}
	return append(out, payload[pos:end]...), true
}

func svcbWithoutECH(rdata []byte) ([]byte, bool) {
	if len(rdata) < 3 {
		return nil, false
	}
	pos, ok := skipPlainName(rdata, 2)
	if !ok {
		return nil, false
	}
	out := append([]byte(nil), rdata[:pos]...)
	found := false
	for pos < len(rdata) {
		if pos+4 > len(rdata) {
			return nil, false
		}
		key := binary.BigEndian.Uint16(rdata[pos : pos+2])
		end := pos + 4 + int(binary.BigEndian.Uint16(rdata[pos+2:pos+4]))
		if end > len(rdata) {
			return nil, false
		}
		switch key {
		case svcParamECH:
			found = true
		case svcParamMandatory:
			if (end-pos-4)%2 != 0 {
				return nil, false
			}
			out = appendMandatoryWithoutECH(out, rdata[pos+4:end])
		default:
			out = append(out, rdata[pos:end]...)
		}
		pos = end
	}
	if !found {
		return nil, false
	}
	return out, true
}

func appendMandatoryWithoutECH(out, keys []byte) []byte {
	kept := make([]byte, 0, len(keys))
	for i := 0; i+2 <= len(keys); i += 2 {
		if binary.BigEndian.Uint16(keys[i:i+2]) != svcParamECH {
			kept = append(kept, keys[i:i+2]...)
		}
	}
	if len(kept) == 0 {
		return out
	}
	out = binary.BigEndian.AppendUint16(out, svcParamMandatory)
	out = binary.BigEndian.AppendUint16(out, uint16(len(kept)))
	return append(out, kept...)
}

func skipPlainName(b []byte, pos int) (int, bool) {
	for pos < len(b) {
		l := int(b[pos])
		if l == 0 {
			return pos + 1, true
		}
		if l&0xC0 != 0 {
			return 0, false
		}
		pos += 1 + l
	}
	return 0, false
}

func signsServiceBinding(rr answerRR) bool {
	if rr.typ != rrTypeRRSIG || len(rr.rdata) < 2 {
		return false
	}
	covered := binary.BigEndian.Uint16(rr.rdata[0:2])
	return covered == TypeHTTPS || covered == TypeSVCB
}

func appendRR(out []byte, rr answerRR, qName string, names map[string]int) []byte {
	out = appendOwnerName(out, rr.name, qName, names)
	var fixed [10]byte
	binary.BigEndian.PutUint16(fixed[0:2], rr.typ)
	binary.BigEndian.PutUint16(fixed[2:4], rr.class)
	binary.BigEndian.PutUint32(fixed[4:8], rr.ttl)
	binary.BigEndian.PutUint16(fixed[8:10], uint16(len(rr.rdata)))
	out = append(out, fixed[:]...)
	if key := strings.ToLower(rr.target); key != "" && len(out) < maxCompressionOffset {
		if _, seen := names[key]; !seen {
			names[key] = len(out)
		}
	}
	return append(out, rr.rdata...)
}

func appendOwnerName(out []byte, name, qName string, names map[string]int) []byte {
	if name == "" {
		return append(out, 0)
	}
	if strings.EqualFold(name, qName) {
		return append(out, 0xC0, 0x0C)
	}
	key := strings.ToLower(name)
	if off, seen := names[key]; seen {
		return append(out, byte(0xC0|off>>8), byte(off))
	}
	if len(out) < maxCompressionOffset {
		names[key] = len(out)
	}
	return append(out, encodeName(name)...)
}
