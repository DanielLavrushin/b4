package quic

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWriteVarRoundTrips(t *testing.T) {
	for _, v := range []uint64{0, 1, 63, 64, 16383, 16384, 1073741823} {
		got, n := readVar(writeVar(nil, v))
		if n == 0 || got != v {
			t.Fatalf("varint %d round-tripped as %d (n=%d)", v, got, n)
		}
	}
}

func TestBuildDummyInitialDecryptsUnderItsOwnDCID(t *testing.T) {
	dcid := []byte{0x83, 0x94, 0xc8, 0xf0, 0x3e, 0x51, 0x57, 0x08}
	scid := []byte{1, 2, 3, 4}

	for _, version := range []uint32{versionV1, versionV2} {
		pkt, ok := BuildDummyInitial(dcid, scid, version, dummyPacketNumber, dummyPayloadLen)
		if !ok {
			t.Fatalf("version %#x: BuildDummyInitial failed", version)
		}
		if !IsInitial(pkt) {
			t.Fatalf("version %#x: dummy is not recognised as an Initial", version)
		}
		if got := ParseDCID(pkt); !bytes.Equal(got, dcid) {
			t.Fatalf("version %#x: dcid %x, want %x", version, got, dcid)
		}
		plain, ok := DecryptInitial(dcid, pkt)
		if !ok {
			t.Fatalf("version %#x: dummy does not decrypt under its own keys", version)
		}
		if len(plain) != dummyPayloadLen {
			t.Fatalf("version %#x: payload %d bytes, want %d", version, len(plain), dummyPayloadLen)
		}
		for i, b := range plain {
			if b != 0 {
				t.Fatalf("version %#x: payload byte %d is %#x, want PADDING", version, i, b)
			}
		}
	}
}

func TestBuildDummyInitialRejectsBadInput(t *testing.T) {
	if _, ok := BuildDummyInitial(nil, nil, versionV1, 1, 32); ok {
		t.Fatal("empty dcid accepted")
	}
	if _, ok := BuildDummyInitial(make([]byte, 21), nil, versionV1, 1, 32); ok {
		t.Fatal("oversized dcid accepted")
	}
	if _, ok := BuildDummyInitial([]byte{1, 2, 3, 4}, nil, 0xdeadbeef, 1, 32); ok {
		t.Fatal("unknown version accepted")
	}
}

func TestCoalesceInitialPutsDummyFirstAndKeepsOriginal(t *testing.T) {
	dcid := []byte{0x83, 0x94, 0xc8, 0xf0, 0x3e, 0x51, 0x57, 0x08}
	original := buildClientInitialFixture(t, dcid)

	out, ok := CoalesceInitial(original, dummyPayloadLen)
	if !ok {
		t.Fatal("CoalesceInitial failed")
	}
	if !bytes.HasSuffix(out, original) {
		t.Fatal("original datagram is not preserved at the tail")
	}
	if bytes.HasPrefix(out, original) {
		t.Fatal("original is still the first packet in the datagram")
	}

	head := out[:len(out)-len(original)]
	if _, ok := DecryptInitial(dcid, head); !ok {
		t.Fatal("prepended packet does not decrypt, peer would drop the datagram")
	}
	if got := ParseDCID(head); !bytes.Equal(got, dcid) {
		t.Fatalf("prepended packet dcid %x, want %x", got, dcid)
	}
}

func TestCoalesceInitialIgnoresNonInitial(t *testing.T) {
	if _, ok := CoalesceInitial([]byte{0x40, 1, 2, 3}, 32); ok {
		t.Fatal("short-header packet accepted")
	}
	if _, ok := CoalesceInitial(nil, 32); ok {
		t.Fatal("empty payload accepted")
	}
}

func buildClientInitialFixture(t *testing.T, dcid []byte) []byte {
	t.Helper()
	hp, aead, iv, err := deriveInitial(dcid, versionV1)
	if err != nil {
		t.Fatalf("deriveInitial: %v", err)
	}
	const pnLen = 4
	hdr := []byte{0xC0 | 0x03}
	hdr = binary.BigEndian.AppendUint32(hdr, versionV1)
	hdr = append(hdr, byte(len(dcid)))
	hdr = append(hdr, dcid...)
	hdr = append(hdr, 0)
	hdr = writeVar(hdr, 0)
	hdr = writeVar(hdr, uint64(pnLen+64+aead.Overhead()))
	pnOff := len(hdr)
	hdr = binary.BigEndian.AppendUint32(hdr, 0)

	nonce := make([]byte, len(iv))
	copy(nonce, iv)
	for i := 0; i < pnLen; i++ {
		nonce[len(nonce)-pnLen+i] ^= hdr[pnOff+i]
	}
	pkt := aead.Seal(hdr, nonce, make([]byte, 64), hdr)
	var mask [16]byte
	hp.Encrypt(mask[:], pkt[pnOff+4:pnOff+4+16])
	pkt[0] ^= mask[0] & 0x0f
	for i := 0; i < pnLen; i++ {
		pkt[pnOff+i] ^= mask[1+i]
	}
	return pkt
}
