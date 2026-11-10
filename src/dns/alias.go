package dns

import (
	"encoding/binary"
	"strings"
)

func WithoutPinnedAlias(payload []byte, pinned func(name string) bool, ttl uint32, maxSize int) []byte {
	if len(payload) < 12 || pinned == nil || payload[2]&0x80 == 0 || (payload[2]>>3)&0x0F != 0 {
		return nil
	}
	if rcode := payload[3] & 0x0F; rcode != 0 && rcode != 3 {
		return nil
	}
	if binary.BigEndian.Uint16(payload[4:6]) != 1 || binary.BigEndian.Uint16(payload[6:8]) == 0 {
		return nil
	}

	qName, offset, ok := readName(payload, 12)
	if !ok || offset+4 > len(payload) || !pinned(qName) {
		return nil
	}
	qtype := binary.BigEndian.Uint16(payload[offset : offset+2])
	qclass := binary.BigEndian.Uint16(payload[offset+2 : offset+4])
	questionEnd := offset + 4

	var sections [3][]answerRR
	unportable := map[int]bool{}
	offset = questionEnd
	for s := range sections {
		count := int(binary.BigEndian.Uint16(payload[6+2*s : 8+2*s]))
		for range count {
			rr, next, ok := readRawRR(payload, offset)
			if !ok || rr.typ == rrTypeTSIG {
				return nil
			}
			if s == 0 && (rr.typ == rrTypeCNAME || rr.typ == rrTypeDNAME || rr.typ == qtype) {
				if portable, _, ok := readPortableRR(payload, offset); ok {
					rr = portable
				} else if rr.typ != qtype || qtype == rrTypeCNAME || qtype == rrTypeDNAME {
					return nil
				} else {
					unportable[len(sections[0])] = true
				}
			}
			offset = next
			sections[s] = append(sections[s], rr)
		}
	}

	chain := map[string]bool{strings.ToLower(qName): true}
	for grew := true; grew; {
		grew = false
		for _, rr := range sections[0] {
			target := strings.ToLower(rr.target)
			if rr.typ == rrTypeCNAME && chain[strings.ToLower(rr.name)] && !chain[target] {
				chain[target] = true
				grew = true
			}
		}
	}

	aliased := false
	for _, rr := range sections[0] {
		if rr.typ != rrTypeCNAME && rr.typ != rrTypeDNAME {
			continue
		}
		if pinned(rr.name) {
			aliased = true
		}
		if chain[strings.ToLower(rr.name)] {
			ttl = min(ttl, rr.ttl)
		}
	}
	if !aliased {
		return nil
	}

	var kept []answerRR
	for i, rr := range sections[0] {
		if rr.typ != qtype || rr.class != qclass || rr.typ == rrTypeCNAME || rr.typ == rrTypeDNAME || !chain[strings.ToLower(rr.name)] {
			continue
		}
		if unportable[i] {
			return nil
		}
		rr.name = qName
		rr.ttl = min(rr.ttl, ttl)
		kept = append(kept, rr)
	}
	if len(kept) == 0 && (qtype == rrTypeA || qtype == rrTypeAAAA) {
		return nil
	}

	var opts []answerRR
	ednsUDPSize := 0
	for _, rr := range sections[2] {
		if rr.typ == rrTypeOPT {
			opts = append(opts, rr)
			ednsUDPSize = max(ednsUDPSize, int(rr.class))
		}
	}

	out := make([]byte, 12, len(payload))
	copy(out, payload[:12])
	out[3] &^= headerFlagAD | 0x0F
	binary.BigEndian.PutUint16(out[6:8], uint16(len(kept)))
	binary.BigEndian.PutUint16(out[8:10], 0)
	binary.BigEndian.PutUint16(out[10:12], uint16(len(opts)))
	out = append(out, payload[12:questionEnd]...)
	names := map[string]int{}
	for _, rr := range kept {
		out = appendRR(out, rr, qName, names)
	}
	for _, rr := range opts {
		out = appendRR(out, rr, qName, names)
	}

	if len(out) > len(payload) && len(out) > responseSizeLimit(maxSize, ednsUDPSize) {
		return nil
	}
	return out
}

func readRawRR(payload []byte, offset int) (answerRR, int, bool) {
	name, next, ok := readName(payload, offset)
	if !ok || next+10 > len(payload) {
		return answerRR{}, 0, false
	}
	start := next + 10
	end := start + int(binary.BigEndian.Uint16(payload[next+8:next+10]))
	if end > len(payload) {
		return answerRR{}, 0, false
	}
	return answerRR{
		name:  name,
		typ:   binary.BigEndian.Uint16(payload[next : next+2]),
		class: binary.BigEndian.Uint16(payload[next+2 : next+4]),
		ttl:   binary.BigEndian.Uint32(payload[next+4 : next+8]),
		rdata: payload[start:end],
	}, end, true
}
