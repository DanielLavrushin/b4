package sock

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

func TestAddTCPMD5OptionRefusesAPacketItsLengthFieldCannotCarry(t *testing.T) {
	for _, c := range []struct {
		v6   bool
		opts int
		l4   int
		fits bool
	}{
		{false, 0, 65495, true},
		{false, 0, 65496, false},
		{false, 0, 65515, false},
		{false, 12, 65495, true},
		{false, 12, 65496, false},
		{false, 12, 65515, false},
		{true, 0, 65515, true},
		{true, 0, 65516, false},
		{true, 0, 65535, false},
		{true, 12, 65515, true},
		{true, 12, 65535, false},
	} {
		t.Run(fmt.Sprintf("%s/options=%d/l4=%d", familyName(c.v6), c.opts, c.l4), func(t *testing.T) {
			var l3 int
			var base []byte
			if c.v6 {
				l3, base = 40, buildMinimalIPv6TCPPacket(c.l4-20-c.opts)
			} else {
				l3, base = 20, buildMinimalIPv4TCPPacket(c.l4-20-c.opts)
			}
			pkt := withTCPOptions(base, l3, bytes.Repeat([]byte{0x01}, c.opts))
			var got []byte
			var ok bool
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("AddTCPMD5Option panicked: %v", r)
					}
				}()
				got, ok = AddTCPMD5Option(pkt, c.v6)
			}()
			if ok != c.fits {
				t.Fatalf("ok=%v, want %v for a %d-byte packet", ok, c.fits, len(pkt))
			}
			if !ok {
				if !bytes.Equal(got, pkt) {
					t.Fatal("a packet refused the option must come back unchanged")
				}
				return
			}
			if c.v6 {
				if n := int(binary.BigEndian.Uint16(got[4:6])); n != len(got)-40 || !TCPChecksumValidV6(got) {
					t.Fatalf("IPv6 payload length %d for a %d-byte packet, or a wrong checksum", n, len(got))
				}
			} else if n := int(binary.BigEndian.Uint16(got[2:4])); n != len(got) || !TCPChecksumValid(got) {
				t.Fatalf("IPv4 total length %d for a %d-byte packet, or a wrong checksum", n, len(got))
			}
		})
	}
}
