package quic

import (
	"encoding/binary"
)

const (
	dummyPacketNumber = 20
	dummyPayloadLen   = 32
	maxCIDLen         = 20
)

func writeVar(dst []byte, v uint64) []byte {
	switch {
	case v < 1<<6:
		return append(dst, byte(v))
	case v < 1<<14:
		return append(dst, byte(v>>8)|0x40, byte(v))
	case v < 1<<30:
		return append(dst, byte(v>>24)|0x80, byte(v>>16), byte(v>>8), byte(v))
	default:
		out := make([]byte, 8)
		binary.BigEndian.PutUint64(out, v)
		out[0] |= 0xC0
		return append(dst, out...)
	}
}

func ParseInitialCIDs(b []byte) (dcid, scid []byte, version uint32, ok bool) {
	if !IsInitial(b) {
		return nil, nil, 0, false
	}
	version = binary.BigEndian.Uint32(b[1:5])
	off := 1 + 4
	if len(b) < off+1 {
		return nil, nil, 0, false
	}
	dlen := int(b[off])
	off++
	if dlen > maxCIDLen || len(b) < off+dlen+1 {
		return nil, nil, 0, false
	}
	dcid = b[off : off+dlen]
	off += dlen
	slen := int(b[off])
	off++
	if slen > maxCIDLen || len(b) < off+slen {
		return nil, nil, 0, false
	}
	scid = b[off : off+slen]
	return dcid, scid, version, true
}

func initialFirstByte(version uint32) (byte, bool) {
	switch version {
	case versionV1:
		return 0xC0 | 0x03, true
	case versionV2:
		return 0xC0 | 0x10 | 0x03, true
	}
	return 0, false
}

// BuildDummyInitial returns an Initial packet that decrypts cleanly under the keys
// derived from dcid and carries nothing but PADDING frames. Coalesced ahead of a real
// Initial in the same datagram it takes the first-packet slot, while the peer discards
// it and goes on to the packet behind it.
func BuildDummyInitial(dcid, scid []byte, version uint32, packetNumber uint32, payloadLen int) ([]byte, bool) {
	if len(dcid) == 0 || len(dcid) > maxCIDLen || len(scid) > maxCIDLen {
		return nil, false
	}
	if payloadLen < 4 {
		payloadLen = dummyPayloadLen
	}
	first, ok := initialFirstByte(version)
	if !ok {
		return nil, false
	}
	hp, aead, iv, err := deriveInitial(dcid, version)
	if err != nil {
		return nil, false
	}

	const pnLen = 4
	hdr := make([]byte, 0, 1+4+1+len(dcid)+1+len(scid)+1+4+pnLen)
	hdr = append(hdr, first)
	hdr = binary.BigEndian.AppendUint32(hdr, version)
	hdr = append(hdr, byte(len(dcid)))
	hdr = append(hdr, dcid...)
	hdr = append(hdr, byte(len(scid)))
	hdr = append(hdr, scid...)
	hdr = writeVar(hdr, 0)
	hdr = writeVar(hdr, uint64(pnLen+payloadLen+aead.Overhead()))

	pnOff := len(hdr)
	hdr = binary.BigEndian.AppendUint32(hdr, packetNumber)

	nonce := make([]byte, len(iv))
	copy(nonce, iv)
	for i := 0; i < pnLen; i++ {
		nonce[len(nonce)-pnLen+i] ^= hdr[pnOff+i]
	}

	pkt := aead.Seal(hdr, nonce, make([]byte, payloadLen), hdr)
	if pnOff+4+16 > len(pkt) {
		return nil, false
	}

	var mask [16]byte
	hp.Encrypt(mask[:], pkt[pnOff+4:pnOff+4+16])
	pkt[0] ^= mask[0] & 0x0f
	for i := 0; i < pnLen; i++ {
		pkt[pnOff+i] ^= mask[1+i]
	}
	return pkt, true
}

// CoalesceInitial prepends a padding-only Initial to a client Initial datagram. The
// returned datagram is what a DPI that only inspects the first packet of a datagram
// sees, and the ClientHello is no longer in that slot.
func CoalesceInitial(payload []byte, payloadLen int) ([]byte, bool) {
	dcid, scid, version, ok := ParseInitialCIDs(payload)
	if !ok {
		return nil, false
	}
	dummy, ok := BuildDummyInitial(dcid, scid, version, dummyPacketNumber, payloadLen)
	if !ok {
		return nil, false
	}
	out := make([]byte, 0, len(dummy)+len(payload))
	out = append(out, dummy...)
	out = append(out, payload...)
	return out, true
}
