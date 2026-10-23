package quic

import "testing"

func malformedInitialWithToken(tokenVarint []byte, tail int) []byte {
	pkt := []byte{0xC3, 0x00, 0x00, 0x00, 0x01, 8, 1, 2, 3, 4, 5, 6, 7, 8, 0}
	pkt = append(pkt, tokenVarint...)
	return append(pkt, make([]byte, tail)...)
}

func TestLocateSNIOffsetRejectsATokenLongerThanThePacket(t *testing.T) {
	for _, c := range []struct {
		name  string
		token []byte
	}{
		{"two byte varint", []byte{0x7f, 0xff}},
		{"four byte varint", []byte{0x80, 0x00, 0x40, 0x00}},
		{"four byte varint at its maximum", []byte{0xbf, 0xff, 0xff, 0xff}},
		{"eight byte varint", []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
	} {
		t.Run(c.name, func(t *testing.T) {
			pkt := malformedInitialWithToken(c.token, 64)
			if !IsInitial(pkt) {
				t.Fatal("the fixture must be an Initial, or it never reaches the parser")
			}
			off, length := LocateSNIOffset(pkt)
			if off != -1 || length != 0 {
				t.Fatalf("got offset %d length %d for a token that cannot fit in %d bytes", off, length, len(pkt))
			}
		})
	}
}

func TestLocateSNIOffsetSurvivesEveryTruncationOfAnInitial(t *testing.T) {
	full := malformedInitialWithToken([]byte{0x80, 0x00, 0x40, 0x00}, 1200)
	for i := 0; i <= len(full); i++ {
		off, length := LocateSNIOffset(full[:i])
		if off > 0 && off+length > i {
			t.Fatalf("truncated to %d bytes: offset %d length %d points past the packet", i, off, length)
		}
	}
}

func TestDecryptInitialRejectsATokenLongerThanThePacket(t *testing.T) {
	dcid := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	for _, token := range [][]byte{
		{0x7f, 0xff},
		{0x80, 0x00, 0x40, 0x00},
		{0xbf, 0xff, 0xff, 0xff},
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	} {
		pkt := malformedInitialWithToken(token, 64)
		if _, ok := DecryptInitial(dcid, pkt); ok {
			t.Fatalf("a token of %x in a %d byte packet decrypted", token, len(pkt))
		}
	}
}

func TestInitialPacketNumberParsersSurviveEveryTruncation(t *testing.T) {
	dcid := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	full := malformedInitialWithToken([]byte{0x80, 0x00, 0x40, 0x00}, 1200)
	for i := 0; i <= len(full); i++ {
		DecryptInitial(dcid, full[:i])
		IsInitial(full[:i])
		LooksLikeQUIC(full[:i])
		LooksLikeShortHeader(full[:i])
		ParseDCID(full[:i])
		ParseInitialCIDs(full[:i])
		CoalesceInitial(full[:i], 0)
	}
}
