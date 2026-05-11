package mtproto

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMsgSplitter_AbridgedShort(t *testing.T) {
	s := newMsgSplitter(connectionTagAbridged)
	pkt := append([]byte{0x02}, bytes.Repeat([]byte{0xAA}, 8)...)
	out := s.split(pkt)
	if len(out) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(out))
	}
	if !bytes.Equal(out[0], pkt) {
		t.Fatalf("packet mismatch")
	}
	if len(s.buf) != 0 {
		t.Fatalf("buffer should be empty, got %d bytes", len(s.buf))
	}
}

func TestMsgSplitter_AbridgedExtended(t *testing.T) {
	s := newMsgSplitter(connectionTagAbridged)
	payload := bytes.Repeat([]byte{0xBB}, 256)
	hdr := []byte{0x7F, 0x40, 0x00, 0x00}
	pkt := append(hdr, payload...)
	out := s.split(pkt)
	if len(out) != 1 || len(out[0]) != 4+256 {
		t.Fatalf("expected 1 packet of 260 bytes, got %d packets, first len=%d", len(out), len(out[0]))
	}
}

func TestMsgSplitter_AbridgedExtendedFF(t *testing.T) {
	s := newMsgSplitter(connectionTagAbridged)
	hdr := []byte{0xFF, 0x02, 0x00, 0x00}
	payload := bytes.Repeat([]byte{0xCC}, 8)
	pkt := append(hdr, payload...)
	out := s.split(pkt)
	if len(out) != 1 || len(out[0]) != 12 {
		t.Fatalf("expected 1 packet of 12 bytes, got %d packets, first len=%d", len(out), len(out[0]))
	}
}

func TestMsgSplitter_Intermediate(t *testing.T) {
	s := newMsgSplitter(connectionTagInter)
	payload := bytes.Repeat([]byte{0xDD}, 8)
	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint32(hdr, 8)
	pkt := append(hdr, payload...)
	out := s.split(pkt)
	if len(out) != 1 || len(out[0]) != 12 {
		t.Fatalf("expected 1 packet of 12 bytes, got %d, first len=%d", len(out), len(out[0]))
	}
}

func TestMsgSplitter_IntermediateQuickAckFlag(t *testing.T) {
	s := newMsgSplitter(connectionTagInter)
	payload := bytes.Repeat([]byte{0xEE}, 4)
	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint32(hdr, 4|0x80000000)
	pkt := append(hdr, payload...)
	out := s.split(pkt)
	if len(out) != 1 || len(out[0]) != 8 {
		t.Fatalf("quick-ack: expected 1 packet of 8 bytes, got %d packets, first len=%d", len(out), len(out[0]))
	}
}

func TestMsgSplitter_PaddedIntermediate(t *testing.T) {
	s := newMsgSplitter(connectionTagPadded)
	payload := bytes.Repeat([]byte{0xFE}, 16)
	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint32(hdr, 16)
	pkt := append(hdr, payload...)
	out := s.split(pkt)
	if len(out) != 1 || len(out[0]) != 20 {
		t.Fatalf("padded: expected 1 packet of 20 bytes, got %d packets, first len=%d", len(out), len(out[0]))
	}
}

func TestMsgSplitter_MultiplePacketsOneChunk(t *testing.T) {
	s := newMsgSplitter(connectionTagAbridged)
	pkt1 := append([]byte{0x02}, bytes.Repeat([]byte{0x01}, 8)...)
	pkt2 := append([]byte{0x03}, bytes.Repeat([]byte{0x02}, 12)...)
	combined := append(pkt1, pkt2...)
	out := s.split(combined)
	if len(out) != 2 {
		t.Fatalf("expected 2 packets, got %d", len(out))
	}
	if !bytes.Equal(out[0], pkt1) || !bytes.Equal(out[1], pkt2) {
		t.Fatalf("packet boundaries wrong")
	}
}

func TestMsgSplitter_PartialBuffer(t *testing.T) {
	s := newMsgSplitter(connectionTagAbridged)
	pkt := append([]byte{0x02}, bytes.Repeat([]byte{0x01}, 8)...)

	out := s.split(pkt[:5])
	if len(out) != 0 {
		t.Fatalf("partial: expected 0 packets, got %d", len(out))
	}
	if len(s.buf) != 5 {
		t.Fatalf("partial: expected 5 bytes buffered, got %d", len(s.buf))
	}

	out = s.split(pkt[5:])
	if len(out) != 1 || !bytes.Equal(out[0], pkt) {
		t.Fatalf("partial+remainder: expected single full packet")
	}
	if len(s.buf) != 0 {
		t.Fatalf("partial: buffer should be empty after completion")
	}
}

func TestMsgSplitter_PartialHeader(t *testing.T) {
	s := newMsgSplitter(connectionTagInter)
	payload := bytes.Repeat([]byte{0xAB}, 8)
	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint32(hdr, 8)
	pkt := append(hdr, payload...)

	out := s.split(pkt[:2])
	if len(out) != 0 {
		t.Fatalf("partial header: expected 0 packets, got %d", len(out))
	}
	out = s.split(pkt[2:])
	if len(out) != 1 || len(out[0]) != 12 {
		t.Fatalf("partial header completion failed")
	}
}

func TestMsgSplitter_UnknownProtoFallthrough(t *testing.T) {
	s := newMsgSplitter(0xDEADBEEF)
	chunk := []byte{1, 2, 3, 4, 5}
	out := s.split(chunk)
	if len(out) != 1 || !bytes.Equal(out[0], chunk) {
		t.Fatalf("unknown proto should pass-through, got %d packets", len(out))
	}
	out = s.split([]byte{6, 7, 8})
	if len(out) != 1 || !bytes.Equal(out[0], []byte{6, 7, 8}) {
		t.Fatalf("unknown proto subsequent chunk should pass-through")
	}
}

func TestMsgSplitter_ZeroLengthDisables(t *testing.T) {
	s := newMsgSplitter(connectionTagInter)
	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint32(hdr, 0)
	tail := []byte{0xAA, 0xBB}
	out := s.split(append(hdr, tail...))
	if len(out) != 1 {
		t.Fatalf("zero-len header: expected 1 emit, got %d", len(out))
	}
	if !s.disabled {
		t.Fatalf("zero-len header should disable splitter")
	}
}

func TestMsgSplitter_FlushReturnsTail(t *testing.T) {
	s := newMsgSplitter(connectionTagAbridged)
	s.split([]byte{0x02, 0xAA, 0xBB})
	tail := s.flush()
	if len(tail) != 3 {
		t.Fatalf("flush: expected 3 bytes, got %d", len(tail))
	}
	if len(s.buf) != 0 {
		t.Fatalf("flush should empty buffer")
	}
	if s.flush() != nil {
		t.Fatalf("flush on empty buffer should return nil")
	}
}
