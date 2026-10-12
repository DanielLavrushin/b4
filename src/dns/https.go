package dns

import (
	"encoding/binary"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/crypto/cryptobyte"
)

const (
	TypeSVCB  uint16 = 64
	TypeHTTPS uint16 = 65

	svcParamMandatory = 0
	svcParamECH       = 5

	echConfigVersion = 0xfe0d
	echKEMX25519     = 0x0020
	echX25519KeySize = 32
	echKDFSHA256     = 0x0001
	echMandatoryExt  = 0x8000

	rrTypeNS    = 2
	rrTypeSOA   = 6
	rrTypePTR   = 12
	rrTypeMX    = 15
	rrTypeRRSIG = 46

	headerFlagAD = 0x20
)

var echAEADs = map[uint16]bool{0x0001: true, 0x0002: true, 0x0003: true}

type echConfig struct {
	kem        uint16
	publicKey  []byte
	suiteOK    bool
	publicName string
	mandatory  bool
}

func ParseResponseECH(payload []byte) []byte {
	var best []byte
	var bestPriority uint16
	eachServiceBindingECH(payload, false, func(priority uint16, ech []byte) {
		if priority == 0 {
			return
		}
		if best == nil || priority < bestPriority {
			best, bestPriority = ech, priority
		}
	})
	return best
}

func ResponseECHPublicNames(payload []byte) []string {
	var names []string
	eachServiceBindingECH(payload, true, func(_ uint16, ech []byte) {
		configs, ok := parseECHConfigs(ech)
		if !ok {
			return
		}
		for _, c := range configs {
			if name := strings.ToLower(c.publicName); name != "" && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	})
	return names
}

func eachServiceBindingECH(payload []byte, allSections bool, fn func(priority uint16, ech []byte)) {
	if len(payload) < 12 {
		return
	}
	qdCount := int(binary.BigEndian.Uint16(payload[4:6]))
	records := int(binary.BigEndian.Uint16(payload[6:8]))
	if allSections {
		records += int(binary.BigEndian.Uint16(payload[8:10])) + int(binary.BigEndian.Uint16(payload[10:12]))
	}

	offset := 12
	for i := 0; i < qdCount; i++ {
		next, ok := skipDNSName(payload, offset)
		if !ok || next+4 > len(payload) {
			return
		}
		offset = next + 4
	}

	for i := 0; i < records; i++ {
		next, ok := skipDNSName(payload, offset)
		if !ok || next+10 > len(payload) {
			return
		}
		typ := binary.BigEndian.Uint16(payload[next : next+2])
		rdLen := int(binary.BigEndian.Uint16(payload[next+8 : next+10]))
		start := next + 10
		if start+rdLen > len(payload) {
			return
		}
		offset = start + rdLen
		if typ != TypeHTTPS && typ != TypeSVCB {
			continue
		}
		if priority, ech := svcbECH(payload, start, offset); len(ech) > 0 {
			fn(priority, ech)
		}
	}
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
	configs, ok := parseECHConfigs(configList)
	if !ok {
		return ""
	}
	for _, c := range configs {
		if c.kem != echKEMX25519 || !c.suiteOK || c.mandatory || !validECHPublicName(c.publicName) {
			continue
		}
		if len(c.publicKey) != echX25519KeySize {
			return ""
		}
		return strings.ToLower(c.publicName)
	}
	return ""
}

func parseECHConfigs(list []byte) ([]echConfig, bool) {
	s := cryptobyte.String(list)
	var body cryptobyte.String
	if !s.ReadUint16LengthPrefixed(&body) || !s.Empty() {
		return nil, false
	}
	var configs []echConfig
	for !body.Empty() {
		var version uint16
		var contents cryptobyte.String
		if !body.ReadUint16(&version) || !body.ReadUint16LengthPrefixed(&contents) {
			return nil, false
		}
		if version != echConfigVersion {
			continue
		}
		c, ok := parseECHConfig(contents)
		if !ok {
			return nil, false
		}
		configs = append(configs, c)
	}
	return configs, true
}

func parseECHConfig(s cryptobyte.String) (echConfig, bool) {
	var c echConfig
	var publicKey, suites, name, extensions cryptobyte.String
	if !s.Skip(1) || !s.ReadUint16(&c.kem) || !s.ReadUint16LengthPrefixed(&publicKey) || len(publicKey) == 0 ||
		!s.ReadUint16LengthPrefixed(&suites) || len(suites) == 0 || len(suites)%4 != 0 {
		return echConfig{}, false
	}
	for !suites.Empty() {
		var kdf, aead uint16
		if !suites.ReadUint16(&kdf) || !suites.ReadUint16(&aead) {
			return echConfig{}, false
		}
		if kdf == echKDFSHA256 && echAEADs[aead] {
			c.suiteOK = true
		}
	}
	if !s.Skip(1) || !s.ReadUint8LengthPrefixed(&name) || len(name) == 0 || !s.ReadUint16LengthPrefixed(&extensions) || !s.Empty() {
		return echConfig{}, false
	}
	for !extensions.Empty() {
		var typ uint16
		var data cryptobyte.String
		if !extensions.ReadUint16(&typ) || !extensions.ReadUint16LengthPrefixed(&data) {
			return echConfig{}, false
		}
		if typ&echMandatoryExt != 0 {
			c.mandatory = true
		}
	}
	c.publicKey = publicKey
	c.publicName = string(name)
	return c, true
}

func validECHPublicName(name string) bool {
	if len(name) > 253 {
		return false
	}
	labels := strings.Split(name, ".")
	if len(labels) <= 1 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '-' {
				return false
			}
		}
	}
	return !ipv4Literal(labels)
}

func ipv4Literal(labels []string) bool {
	if len(labels) > 4 {
		return false
	}
	numbers := make([]uint64, len(labels))
	for i, label := range labels {
		n, ok := ipv4Number(label)
		if !ok {
			return false
		}
		numbers[i] = n
	}
	last := len(numbers) - 1
	for _, n := range numbers[:last] {
		if n > 255 {
			return false
		}
	}
	return last == 0 || numbers[last] < 1<<(8*(4-last))
}

func ipv4Number(label string) (uint64, bool) {
	base := 10
	switch {
	case len(label) >= 2 && (label[:2] == "0x" || label[:2] == "0X"):
		base, label = 16, label[2:]
		if label == "" {
			return 0, true
		}
	case len(label) >= 2 && label[0] == '0':
		base, label = 8, label[1:]
	}
	n, err := strconv.ParseUint(label, base, 32)
	return n, err == nil
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
	changed := map[rrsetKey]bool{}
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
					changed[rrsetKey{owner: strings.ToLower(rr.name), class: rr.class, typ: rr.typ}] = true
				}
			}
			if rr.typ == rrTypeOPT && int(rr.class) > ednsUDPSize {
				ednsUDPSize = int(rr.class)
			}
			sections[s] = append(sections[s], rr)
		}
	}
	if len(changed) == 0 {
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
			if signsChangedRRset(rr, changed) {
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

type rrsetKey struct {
	owner string
	class uint16
	typ   uint16
}

func signsChangedRRset(rr answerRR, changed map[rrsetKey]bool) bool {
	if rr.typ != rrTypeRRSIG || len(rr.rdata) < 2 {
		return false
	}
	return changed[rrsetKey{owner: strings.ToLower(rr.name), class: rr.class, typ: binary.BigEndian.Uint16(rr.rdata[0:2])}]
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
