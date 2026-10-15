package sock

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func withTCPOptions(pkt []byte, l3 int, opts []byte) []byte {
	out := append(append(append([]byte(nil), pkt[:l3+20]...), opts...), pkt[l3+20:]...)
	out[l3+12] = byte((20+len(opts))/4) << 4
	if l3 == 40 {
		binary.BigEndian.PutUint16(out[4:6], uint16(len(out)-40))
		FixTCPChecksumV6(out)
	} else {
		binary.BigEndian.PutUint16(out[2:4], uint16(len(out)))
		FixIPv4Checksum(out[:20])
		FixTCPChecksum(out)
	}
	return out
}

func TestAddTCPMD5OptionAddsTheOptionOrSaysItCannot(t *testing.T) {
	cases := []struct {
		name string
		v6   bool
		opts int
		fits bool
	}{
		{"v4 without options", false, 0, true},
		{"v4 with 20 bytes of options", false, 20, true},
		{"v4 with 24 bytes of options", false, 24, false},
		{"v6 without options", true, 0, true},
		{"v6 with 24 bytes of options", true, 24, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, l3 := buildMinimalIPv4TCPPacket(30), 20
			if tc.v6 {
				base, l3 = buildMinimalIPv6TCPPacket(30), 40
			}
			pkt := withTCPOptions(base, l3, bytes.Repeat([]byte{0x01}, tc.opts))
			got, ok := AddTCPMD5Option(pkt, tc.v6)
			if ok != tc.fits {
				t.Fatalf("ok=%v, want %v", ok, tc.fits)
			}
			if !tc.fits {
				if !bytes.Equal(got, pkt) {
					t.Error("a packet without room for the option must come back unchanged")
				}
				return
			}
			hdr := l3 + 20 + tc.opts
			if len(got) != len(pkt)+20 || int(got[l3+12]>>4)*4 != 20+tc.opts+20 {
				t.Fatalf("want 20 more option bytes, got %d bytes with a %d-byte TCP header", len(got), int(got[l3+12]>>4)*4)
			}
			if !bytes.Equal(got[hdr:hdr+4], []byte{1, 1, 19, 18}) {
				t.Errorf("the MD5 option is missing: %x", got[hdr:hdr+4])
			}
			if !bytes.Equal(got[hdr+20:], pkt[hdr:]) {
				t.Error("the payload must follow the option unchanged")
			}
			if tc.v6 {
				if int(binary.BigEndian.Uint16(got[4:6])) != len(got)-40 || !TCPChecksumValidV6(got) {
					t.Error("payload length or checksum is wrong")
				}
			} else if int(binary.BigEndian.Uint16(got[2:4])) != len(got) || !TCPChecksumValid(got) {
				t.Error("total length or checksum is wrong")
			}
		})
	}
}

func optionKindsAReceiverReads(pkt []byte, l3 int) []byte {
	opts := pkt[l3+20 : l3+int(pkt[l3+12]>>4)*4]
	var kinds []byte
	for i := 0; i < len(opts) && opts[i] != 0; {
		if opts[i] == 1 {
			i++
			continue
		}
		kinds = append(kinds, opts[i])
		if i+1 >= len(opts) || opts[i+1] < 2 {
			break
		}
		i += int(opts[i+1])
	}
	return kinds
}

func TestAddTCPMD5OptionStaysReadableAfterAnEndOfOptionList(t *testing.T) {
	for _, v6 := range []bool{false, true} {
		base, l3 := buildMinimalIPv4TCPPacket(30), 20
		if v6 {
			base, l3 = buildMinimalIPv6TCPPacket(30), 40
		}
		pkt := withTCPOptions(base, l3, []byte{0x02, 0x04, 0x05, 0xb4, 0x00, 0x00, 0x00, 0x00})
		got, ok := AddTCPMD5Option(pkt, v6)
		if !ok {
			t.Fatalf("v6=%v: an 8-byte option list has room for the MD5 option", v6)
		}
		if kinds := optionKindsAReceiverReads(got, l3); !bytes.Equal(kinds, []byte{2, 19}) {
			t.Errorf("v6=%v: a receiver reads options %v, want MSS and MD5", v6, kinds)
		}
		if !bytes.Equal(got[l3+20:l3+24], pkt[l3+20:l3+24]) || !bytes.Equal(got[l3+48:], pkt[l3+28:]) {
			t.Errorf("v6=%v: the MSS option and the payload must be kept", v6)
		}
	}
}

func TestAddTCPMD5OptionRefusesMalformedOptions(t *testing.T) {
	for _, opts := range [][]byte{{0x02, 0x00, 0x01, 0x01}, {0x02, 0x08, 0x05, 0xb4}} {
		pkt := withTCPOptions(buildMinimalIPv4TCPPacket(30), 20, opts)
		if got, ok := AddTCPMD5Option(pkt, false); ok || !bytes.Equal(got, pkt) {
			t.Errorf("options %x: a malformed option length must be refused and the packet returned unchanged", opts)
		}
	}
}

func TestAddTCPMD5OptionRefusesAHeaderLongerThanThePacket(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(0)
	pkt[20+12] = 0xa0
	if _, ok := AddTCPMD5Option(pkt, false); ok {
		t.Error("a TCP header that claims more bytes than the packet has must be refused")
	}
}

func TestAddTCPMD5OptionRefusesAHeaderShorterThan20Bytes(t *testing.T) {
	for words := byte(0); words < 5; words++ {
		pkt := buildMinimalIPv4TCPPacket(64)
		pkt[20+12] = words << 4
		if got, ok := AddTCPMD5Option(pkt, false); ok || !bytes.Equal(got, pkt) {
			t.Errorf("a %d-byte TCP header must be refused and returned unchanged", int(words)*4)
		}
	}
}
