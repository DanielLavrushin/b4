package dns

import (
	"encoding/binary"
	"net"
)

func WithoutPinnedAlias(payload []byte, pinned func(name string) bool, ttl uint32) []byte {
	if len(payload) < 12 || pinned == nil || payload[2]&0x80 == 0 || (payload[2]>>3)&0x0F != 0 {
		return nil
	}
	if rcode := payload[3] & 0x0F; rcode != 0 && rcode != 3 {
		return nil
	}
	if binary.BigEndian.Uint16(payload[4:6]) != 1 {
		return nil
	}
	anCount := int(binary.BigEndian.Uint16(payload[6:8]))
	if anCount == 0 {
		return nil
	}

	qName, offset, ok := readName(payload, 12)
	if !ok || offset+4 > len(payload) || !pinned(qName) {
		return nil
	}
	qtype := binary.BigEndian.Uint16(payload[offset : offset+2])
	questionEnd := offset + 4

	aliased := false
	var ips []net.IP
	offset = questionEnd
	for range anCount {
		owner, next, ok := readName(payload, offset)
		if !ok || next+10 > len(payload) {
			return nil
		}
		typ := binary.BigEndian.Uint16(payload[next : next+2])
		start := next + 10
		end := start + int(binary.BigEndian.Uint16(payload[next+8:next+10]))
		if end > len(payload) {
			return nil
		}
		switch {
		case (typ == rrTypeCNAME || typ == rrTypeDNAME) && pinned(owner):
			aliased = true
		case typ == qtype && typ == rrTypeA && end-start == net.IPv4len,
			typ == qtype && typ == rrTypeAAAA && end-start == net.IPv6len:
			ips = append(ips, net.IP(append([]byte(nil), payload[start:end]...)))
		}
		offset = end
	}
	if !aliased {
		return nil
	}

	var out []byte
	if qtype == rrTypeA || qtype == rrTypeAAAA {
		if out = BuildAnswerFromIPs(payload, ttl, ips); out == nil {
			return nil
		}
	} else {
		out = make([]byte, questionEnd)
		copy(out, payload[:questionEnd])
		out[3] &^= 0x0F
		binary.BigEndian.PutUint16(out[6:8], 0)
		binary.BigEndian.PutUint16(out[8:10], 0)
		binary.BigEndian.PutUint16(out[10:12], 0)
	}
	out[2] &^= 0x02
	out[3] &^= headerFlagAD
	return out
}
