package sni

import "encoding/binary"

const tlsRecordHandshake = 0x16

func LocateServerName(payload []byte) (start, end int, ok bool) {
	if s, e, found := LocateServerNameInRecord(payload); found {
		return s, e, true
	}
	return ScanServerName(payload)
}

func ScanServerName(payload []byte) (start, end int, ok bool) {
	if len(payload) < 10 {
		return 0, 0, false
	}
	switch payload[0] {
	case 20, 21, 23, 24:
		return 0, 0, false
	}

	for i := 0; i+9 <= len(payload); i++ {
		if payload[i] != 0x00 || payload[i+1] != 0x00 {
			continue
		}
		extLen := int(binary.BigEndian.Uint16(payload[i+2 : i+4]))
		listLen := int(binary.BigEndian.Uint16(payload[i+4 : i+6]))
		if listLen < 4 || extLen != listLen+2 {
			continue
		}
		if payload[i+6] != 0x00 {
			continue
		}
		nameLen := int(binary.BigEndian.Uint16(payload[i+7 : i+9]))
		if nameLen != listLen-3 || nameLen > MaxSNINameLen {
			continue
		}
		s := i + 9
		e := s + nameLen
		if e > len(payload) {
			continue
		}
		if !IsValidSNI(payload[s:e]) {
			continue
		}
		return s, e, true
	}
	return 0, 0, false
}

func LocateServerNameInRecord(payload []byte) (start, end int, ok bool) {
	if len(payload) < 5 || payload[0] != tlsRecordHandshake {
		return 0, 0, false
	}

	p := 5

	if p+4 > len(payload) || payload[p] != tlsHandshakeClientHello {
		return 0, 0, false
	}

	p += 4

	if p+2+32 > len(payload) {
		return 0, 0, false
	}
	p += 2 + 32

	if p >= len(payload) {
		return 0, 0, false
	}
	sidLen := int(payload[p])
	p++
	if p+sidLen > len(payload) {
		return 0, 0, false
	}
	p += sidLen

	if p+2 > len(payload) {
		return 0, 0, false
	}
	csLen := int(binary.BigEndian.Uint16(payload[p : p+2]))
	p += 2
	if p+csLen > len(payload) {
		return 0, 0, false
	}
	p += csLen

	if p >= len(payload) {
		return 0, 0, false
	}
	cmLen := int(payload[p])
	p++
	if p+cmLen > len(payload) {
		return 0, 0, false
	}
	p += cmLen

	if p+2 > len(payload) {
		return 0, 0, false
	}
	extLen := int(binary.BigEndian.Uint16(payload[p : p+2]))
	p += 2
	if p+extLen > len(payload) {
		extLen = len(payload) - p
	}
	e := p
	ee := p + extLen

	for e+4 <= ee {
		extType := binary.BigEndian.Uint16(payload[e : e+2])
		extDataLen := int(binary.BigEndian.Uint16(payload[e+2 : e+4]))
		e += 4
		if e+extDataLen > ee {
			break
		}

		if extType == 0 && extDataLen >= 5 {
			q := e
			if q+2 > e+extDataLen {
				break
			}
			listLen := int(binary.BigEndian.Uint16(payload[q : q+2]))
			q += 2
			if q+listLen > e+extDataLen {
				break
			}
			if q+3 > e+extDataLen {
				break
			}
			nameType := payload[q]
			q++
			if nameType != 0 {
				break
			}
			nameLen := int(binary.BigEndian.Uint16(payload[q : q+2]))
			q += 2
			if nameLen == 0 || q+nameLen > e+extDataLen {
				break
			}
			return q, q + nameLen, true
		}

		e += extDataLen
	}
	return 0, 0, false
}
