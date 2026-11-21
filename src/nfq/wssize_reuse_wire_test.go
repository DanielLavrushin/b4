package nfq

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/sock"
)

func TestTheReuseCaptureReadsEitherZeroChecksumAsValid(t *testing.T) {
	served, err := hex.DecodeString("45000036ceb540004006f407cb007102cb00710101bbf618494a8d5ca79d41c480180040ffff00000101080a6ba4e20c1ebbc4221603")
	if err != nil {
		t.Fatal(err)
	}
	recomputed := append([]byte(nil), served...)
	sock.FixTCPChecksum(recomputed)
	if binary.BigEndian.Uint16(recomputed[36:38]) != 0 {
		t.Fatalf("the captured server segment does not sum to zero: a recomputed checksum is %#04x", binary.BigEndian.Uint16(recomputed[36:38]))
	}
	if !reuseTCPChecksumValid(served) {
		t.Error("a server segment whose checksum is 0xffff, the other form of a zero sum, which the router's TCP accepted and acknowledged, reads as a bad checksum")
	}
	if !reuseTCPChecksumValid(recomputed) {
		t.Error("the same server segment with the checksum 0x0000 reads as a bad checksum")
	}
	broken := append([]byte(nil), served...)
	broken[len(broken)-1] ^= 1
	if reuseTCPChecksumValid(broken) {
		t.Error("a segment whose payload no longer matches its checksum reads as valid")
	}
	info, ok := ExtractPacketInfoV4(served)
	if !ok {
		t.Fatal("the captured server segment does not parse")
	}
	if decoy := BuildFakeOverlapSegmentV4(served, info, info.PayloadLen, 0, 1, []byte{0}, 7); decoy == nil || reuseTCPChecksumValid(decoy) {
		t.Error("an overlap decoy with its deliberately wrong checksum reads as valid, so the capture never holds it to the decoy rules")
	}

	v6 := make([]byte, 62)
	v6[0], v6[6], v6[7] = 0x60, 6, 64
	binary.BigEndian.PutUint16(v6[4:6], 22)
	copy(v6[8:24], net.ParseIP("2001:db8::2"))
	copy(v6[24:40], net.ParseIP("2001:db8::1"))
	binary.BigEndian.PutUint16(v6[40:42], 443)
	binary.BigEndian.PutUint16(v6[42:44], 63000)
	binary.BigEndian.PutUint32(v6[44:48], 1229622620)
	binary.BigEndian.PutUint32(v6[48:52], 2812101060)
	v6[52], v6[53] = 0x50, 0x18
	binary.BigEndian.PutUint16(v6[54:56], 64)
	v6[60], v6[61] = 0x16, 0x03
	sock.FixTCPChecksumV6(v6)
	copy(v6[58:60], v6[56:58])
	sock.FixTCPChecksumV6(v6)
	if binary.BigEndian.Uint16(v6[56:58]) != 0 {
		t.Fatalf("the IPv6 segment does not sum to zero: its checksum is %#04x", binary.BigEndian.Uint16(v6[56:58]))
	}
	if !reuseTCPChecksumValid(v6) {
		t.Error("an IPv6 segment with the checksum 0x0000 reads as a bad checksum")
	}
	binary.BigEndian.PutUint16(v6[56:58], 0xffff)
	if !reuseTCPChecksumValid(v6) {
		t.Error("an IPv6 segment whose checksum is 0xffff, the other form of a zero sum, reads as a bad checksum")
	}
	info6, ok := ExtractPacketInfoV6(v6)
	if !ok {
		t.Fatal("the IPv6 segment does not parse")
	}
	if decoy := BuildFakeOverlapSegmentV6(v6, info6, info6.PayloadLen, 0, []byte{0}, 7); decoy == nil || reuseTCPChecksumValid(decoy) {
		t.Error("an IPv6 overlap decoy with its deliberately wrong checksum reads as valid, so the capture never holds it to the decoy rules")
	}
}
