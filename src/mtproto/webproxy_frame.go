package mtproto

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	webFrameHeaderSize = 8
	webMaxFramePayload = 1 << 20
	webMaxBatchFrames  = 4096
	webInitialWindow   = 4 << 20
	webMaxDataChunk    = 64 << 10
	webMaxStreamID     = 0x00FFFFFF
)

type webFrameType byte

const (
	webFrameOpen     webFrameType = 0x01
	webFrameData     webFrameType = 0x02
	webFrameClose    webFrameType = 0x03
	webFrameWindow   webFrameType = 0x04
	webFramePing     webFrameType = 0x05
	webFramePong     webFrameType = 0x06
	webFrameHello    webFrameType = 0x10
	webFrameWelcome  webFrameType = 0x11
	webFrameAuthChal webFrameType = 0x12
	webFrameAuthResp webFrameType = 0x13
	webFrameBye      webFrameType = 0x1f
)

func (t webFrameType) String() string {
	switch t {
	case webFrameOpen:
		return "OPEN"
	case webFrameData:
		return "DATA"
	case webFrameClose:
		return "CLOSE"
	case webFrameWindow:
		return "WINDOW"
	case webFramePing:
		return "PING"
	case webFramePong:
		return "PONG"
	case webFrameHello:
		return "HELLO"
	case webFrameWelcome:
		return "WELCOME"
	case webFrameAuthChal:
		return "AUTH_CHAL"
	case webFrameAuthResp:
		return "AUTH_RESP"
	case webFrameBye:
		return "BYE"
	}
	return fmt.Sprintf("0x%02x", byte(t))
}

func webKnownFrameType(v byte) bool {
	switch webFrameType(v) {
	case webFrameOpen, webFrameData, webFrameClose, webFrameWindow,
		webFramePing, webFramePong, webFrameHello, webFrameWelcome,
		webFrameAuthChal, webFrameAuthResp, webFrameBye:
		return true
	}
	return false
}

func webFrameIsSession(t webFrameType) bool {
	switch t {
	case webFramePing, webFramePong, webFrameHello, webFrameWelcome,
		webFrameAuthChal, webFrameAuthResp, webFrameBye:
		return true
	}
	return false
}

type webFrame struct {
	typ     webFrameType
	stream  uint32
	payload []byte
}

var errWebFrameProtocol = errors.New("web proxy frame protocol error")

var errWebStreamRefused = errors.New("web proxy stream refused")

func appendWebFrame(dst []byte, typ webFrameType, stream uint32, payload []byte) []byte {
	var hdr [webFrameHeaderSize]byte
	hdr[0] = byte(typ)
	hdr[1] = byte(stream >> 16)
	hdr[2] = byte(stream >> 8)
	hdr[3] = byte(stream)
	binary.BigEndian.PutUint32(hdr[4:8], uint32(len(payload)))
	dst = append(dst, hdr[:]...)
	return append(dst, payload...)
}

func webFrameBytes(typ webFrameType, stream uint32, payload []byte) []byte {
	return appendWebFrame(make([]byte, 0, webFrameHeaderSize+len(payload)), typ, stream, payload)
}

func parseWebFrames(msg []byte) ([]webFrame, error) {
	if len(msg) == 0 {
		return nil, fmt.Errorf("%w: empty message", errWebFrameProtocol)
	}
	var out []webFrame
	for off := 0; off < len(msg); {
		if len(msg)-off < webFrameHeaderSize {
			return nil, fmt.Errorf("%w: truncated header", errWebFrameProtocol)
		}
		if !webKnownFrameType(msg[off]) {
			return nil, fmt.Errorf("%w: unknown type 0x%02x", errWebFrameProtocol, msg[off])
		}
		stream := uint32(msg[off+1])<<16 | uint32(msg[off+2])<<8 | uint32(msg[off+3])
		size := binary.BigEndian.Uint32(msg[off+4 : off+8])
		if size > webMaxFramePayload {
			return nil, fmt.Errorf("%w: payload %d over cap", errWebFrameProtocol, size)
		}
		end := off + webFrameHeaderSize + int(size)
		if end > len(msg) {
			return nil, fmt.Errorf("%w: truncated payload", errWebFrameProtocol)
		}
		if len(out) >= webMaxBatchFrames {
			return nil, fmt.Errorf("%w: over %d frames per message", errWebFrameProtocol, webMaxBatchFrames)
		}
		out = append(out, webFrame{
			typ:     webFrameType(msg[off]),
			stream:  stream,
			payload: msg[off+webFrameHeaderSize : end],
		})
		off = end
	}
	return out, nil
}
