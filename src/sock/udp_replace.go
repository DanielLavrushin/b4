package sock

import "encoding/binary"

func ReplaceUDPPayload(raw []byte, l4off int, payload []byte) []byte {
	if len(raw) == 0 || l4off < 0 || len(raw) < l4off+8 {
		return nil
	}
	total := l4off + 8 + len(payload)
	if total > 0xffff {
		return nil
	}
	switch raw[0] >> 4 {
	case 4:
		if l4off < 20 || l4off != int(raw[0]&0x0f)*4 || raw[9] != 17 {
			return nil
		}
		if binary.BigEndian.Uint16(raw[6:8])&0x3fff != 0 {
			return nil
		}
		out := make([]byte, total)
		copy(out, raw[:l4off+8])
		copy(out[l4off+8:], payload)
		if total > int(binary.BigEndian.Uint16(raw[2:4])) {
			out[6] &^= 0x40
		}
		binary.BigEndian.PutUint16(out[2:4], uint16(total))
		binary.BigEndian.PutUint16(out[l4off+4:l4off+6], uint16(8+len(payload)))
		FixIPv4Checksum(out[:l4off])
		FixUDPChecksum(out, l4off)
		return out
	case 6:
		if l4off != 40 || raw[6] != 17 {
			return nil
		}
		out := make([]byte, total)
		copy(out, raw[:48])
		copy(out[48:], payload)
		binary.BigEndian.PutUint16(out[4:6], uint16(8+len(payload)))
		binary.BigEndian.PutUint16(out[44:46], uint16(8+len(payload)))
		FixUDPChecksumV6(out)
		return out
	}
	return nil
}
