package mtproto

import (
	"encoding/binary"
	"errors"
)

// errUpstreamRejected ends a relay whose upstream refused the session for a
// reason the client must not be handed, so the close line says why rather than
// reading as an ordinary EOF.
var errUpstreamRejected = errors.New("upstream rejected the session")

// transportErrFrame rebuilds an error packet the scanner held back, for the codes
// that belong to the client rather than to b4's choice of route.
func transportErrFrame(proto uint32, code int32) []byte {
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, uint32(code))
	switch proto {
	case connectionTagAbridged:
		return append([]byte{0x01}, body...)
	default:
		return append([]byte{0x04, 0x00, 0x00, 0x00}, body...)
	}
}

// Telegram answers a transport-level fault with a packet whose whole payload is
// one signed little-endian int32, framed by the transport in use. Any real
// MTProto message is at least 20 bytes of payload, so a payload this short is
// always an error and never traffic.
const (
	tgErrAuthKeyNotFound = -404
	tgErrFlood           = -429
	tgErrInvalidDC       = -444

	// transportErrMinPayload is the smallest payload that can be an error code,
	// and the code reads four bytes out of the frame, so anything shorter is not
	// a short frame but a length that cannot be right at all. Intermediate and
	// padded take their length straight off the wire, so one to three bytes is
	// reachable from a desynced or hostile upstream.
	transportErrMinPayload = 4
	// transportErrMaxPayload is the largest payload still read as an error code.
	// Padded intermediate appends up to three bytes, and Telegram Desktop applies
	// the same rule: a frame carrying fewer than three int32s is an error.
	transportErrMaxPayload = 7
	// transportErrFloor bounds what counts as an error code at all. A quick ack
	// is a four-byte value with the top bit set, so it reads as a large negative
	// number; every documented error code is a small one.
	transportErrFloor = -1000
)

func transportErrName(code int32) string {
	switch code {
	case tgErrAuthKeyNotFound:
		return "auth key not found"
	case tgErrFlood:
		return "transport flood"
	case tgErrInvalidDC:
		return "invalid DC"
	}
	return "unknown"
}

// dcFrameScanner walks the plaintext MTProto stream coming back from the data
// center and reports the transport error codes in it.
//
// It holds back only frames short enough to be an error code, which are at most
// eleven bytes and cannot be traffic, so an error can be dropped before it
// reaches the client rather than being noticed after the fact. Everything else
// is passed straight through, and the scanner switches itself off for the rest
// of the session the moment the stream stops making sense, so a framing it does
// not understand costs the session nothing.
type dcFrameScanner struct {
	proto    uint32
	disabled bool

	hdr     []byte
	holding []byte
	need    int
	bodyLen int
	pass    int
}

func newDCFrameScanner(proto uint32) *dcFrameScanner {
	switch proto {
	case connectionTagAbridged, connectionTagInter, connectionTagPadded:
		return &dcFrameScanner{proto: proto}
	}
	return &dcFrameScanner{proto: proto, disabled: true}
}

// feed returns the bytes to forward to the client, and the error code of the
// first short frame it completed. When found is true, out is everything that
// preceded the error frame, the error frame itself has been consumed, and rest
// is whatever followed it in this chunk - a caller that decides to pass the
// error on has to forward rest behind it or those bytes are lost.
func (s *dcFrameScanner) feed(chunk []byte) (out, rest []byte, code int32, found bool) {
	if s == nil || s.disabled {
		return chunk, nil, 0, false
	}
	if len(s.hdr) == 0 && len(s.holding) == 0 && s.need == 0 && s.pass >= len(chunk) {
		s.pass -= len(chunk)
		return chunk, nil, 0, false
	}

	out = make([]byte, 0, len(chunk)+len(s.holding))
	for len(chunk) > 0 {
		switch {
		case s.pass > 0:
			n := s.pass
			if n > len(chunk) {
				n = len(chunk)
			}
			out = append(out, chunk[:n]...)
			chunk = chunk[n:]
			s.pass -= n

		case s.need > 0:
			n := s.need
			if n > len(chunk) {
				n = len(chunk)
			}
			s.holding = append(s.holding, chunk[:n]...)
			chunk = chunk[n:]
			s.need -= n
			if s.need > 0 {
				continue
			}
			body := s.holding[len(s.holding)-s.bodyLen:]
			c := int32(binary.LittleEndian.Uint32(body[:4]))
			if c < 0 && c > transportErrFloor {
				s.disabled = true
				return out, chunk, c, true
			}
			out = append(out, s.holding...)
			s.holding = s.holding[:0]

		default:
			s.hdr = append(s.hdr, chunk[0])
			chunk = chunk[1:]
			n, ready := s.headerLen()
			if !ready {
				continue
			}
			if n < transportErrMinPayload {
				s.disabled = true
				out = append(out, s.hdr...)
				s.hdr = s.hdr[:0]
				return append(out, chunk...), nil, 0, false
			}
			if n <= transportErrMaxPayload {
				s.holding = append(s.holding[:0], s.hdr...)
				s.bodyLen = n
				s.need = n
			} else {
				out = append(out, s.hdr...)
				s.pass = n
			}
			s.hdr = s.hdr[:0]
		}
	}
	return out, nil, 0, false
}

// headerLen reports the payload length of the frame whose header bytes have been
// collected so far. ready is false while the header is still incomplete, and a
// length below transportErrMinPayload means the header cannot be a frame at all.
func (s *dcFrameScanner) headerLen() (int, bool) {
	switch s.proto {
	case connectionTagAbridged:
		first := s.hdr[0]
		if first == 0x7f || first == 0xff {
			if len(s.hdr) < 4 {
				return 0, false
			}
			return (int(s.hdr[1]) | int(s.hdr[2])<<8 | int(s.hdr[3])<<16) * 4, true
		}
		return int(first&0x7f) * 4, true
	case connectionTagInter, connectionTagPadded:
		if len(s.hdr) < 4 {
			return 0, false
		}
		return int(binary.LittleEndian.Uint32(s.hdr[:4]) & 0x7fffffff), true
	}
	return -1, true
}
