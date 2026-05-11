package mtproto

import "encoding/binary"

type msgSplitter struct {
	proto    uint32
	buf      []byte
	disabled bool
}

func newMsgSplitter(proto uint32) *msgSplitter {
	switch proto {
	case connectionTagAbridged, connectionTagInter, connectionTagPadded:
		return &msgSplitter{proto: proto}
	}
	return &msgSplitter{proto: proto, disabled: true}
}

func (s *msgSplitter) split(chunk []byte) [][]byte {
	if s.disabled {
		if len(chunk) == 0 {
			return nil
		}
		return [][]byte{append([]byte(nil), chunk...)}
	}
	s.buf = append(s.buf, chunk...)
	var out [][]byte
	for {
		n, ok := s.nextLen()
		if !ok {
			break
		}
		if n <= 0 {
			out = append(out, append([]byte(nil), s.buf...))
			s.buf = s.buf[:0]
			s.disabled = true
			return out
		}
		if n > len(s.buf) {
			break
		}
		pkt := make([]byte, n)
		copy(pkt, s.buf[:n])
		out = append(out, pkt)
		s.buf = s.buf[n:]
	}
	return out
}

func (s *msgSplitter) flush() []byte {
	if len(s.buf) == 0 {
		return nil
	}
	tail := append([]byte(nil), s.buf...)
	s.buf = s.buf[:0]
	return tail
}

func (s *msgSplitter) nextLen() (int, bool) {
	switch s.proto {
	case connectionTagAbridged:
		if len(s.buf) < 1 {
			return 0, false
		}
		first := s.buf[0]
		if first == 0x7f || first == 0xff {
			if len(s.buf) < 4 {
				return 0, false
			}
			payloadLen := (int(s.buf[1]) | int(s.buf[2])<<8 | int(s.buf[3])<<16) * 4
			if payloadLen <= 0 {
				return -1, true
			}
			return 4 + payloadLen, true
		}
		payloadLen := int(first&0x7f) * 4
		if payloadLen <= 0 {
			return -1, true
		}
		return 1 + payloadLen, true
	case connectionTagInter, connectionTagPadded:
		if len(s.buf) < 4 {
			return 0, false
		}
		payloadLen := int(binary.LittleEndian.Uint32(s.buf[:4]) & 0x7fffffff)
		if payloadLen <= 0 {
			return -1, true
		}
		return 4 + payloadLen, true
	}
	return -1, true
}
