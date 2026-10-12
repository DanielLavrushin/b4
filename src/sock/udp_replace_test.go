package sock

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func dnsReplyV4(options []byte, df bool, payload []byte) []byte {
	ihl := 20 + len(options)
	pkt := make([]byte, ihl+8+len(payload))
	pkt[0] = 0x40 | byte(ihl/4)
	pkt[1] = 0xb8
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	binary.BigEndian.PutUint16(pkt[4:6], 0xbeef)
	if df {
		pkt[6] = 0x40
	}
	pkt[8] = 57
	pkt[9] = 17
	copy(pkt[12:16], []byte{192, 168, 1, 1})
	copy(pkt[16:20], []byte{192, 168, 1, 100})
	copy(pkt[20:ihl], options)
	binary.BigEndian.PutUint16(pkt[ihl:], 53)
	binary.BigEndian.PutUint16(pkt[ihl+2:], 40000)
	binary.BigEndian.PutUint16(pkt[ihl+4:], uint16(8+len(payload)))
	copy(pkt[ihl+8:], payload)
	FixIPv4Checksum(pkt[:ihl])
	FixUDPChecksum(pkt, ihl)
	return pkt
}

func dnsReplyV6(payload []byte) []byte {
	pkt := make([]byte, 48+len(payload))
	pkt[0], pkt[1], pkt[2], pkt[3] = 0x6b, 0x81, 0x23, 0x45
	binary.BigEndian.PutUint16(pkt[4:6], uint16(8+len(payload)))
	pkt[6] = 17
	pkt[7] = 61
	pkt[8], pkt[9], pkt[23] = 0xfd, 0x00, 0x01
	pkt[24], pkt[25], pkt[39] = 0xfd, 0x00, 0x64
	binary.BigEndian.PutUint16(pkt[40:42], 53)
	binary.BigEndian.PutUint16(pkt[42:44], 40000)
	binary.BigEndian.PutUint16(pkt[44:46], uint16(8+len(payload)))
	copy(pkt[48:], payload)
	FixUDPChecksumV6(pkt)
	return pkt
}

func dnsBytes(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = seed + byte(i*7)
	}
	return b
}

func udpV4ChecksumValid(pkt []byte, ihl int) bool {
	pseudo := make([]byte, 12)
	copy(pseudo[0:8], pkt[12:20])
	pseudo[9] = 17
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(pkt)-ihl))
	return onesSum(append(pseudo, pkt[ihl:]...)) == 0xffff
}

func udpV6ChecksumValid(pkt []byte) bool {
	pseudo := make([]byte, 40)
	copy(pseudo[0:32], pkt[8:40])
	binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(pkt)-40))
	pseudo[39] = 17
	return onesSum(append(pseudo, pkt[40:]...)) == 0xffff
}

func TestReplaceUDPPayload(t *testing.T) {
	routerAlert := []byte{0x94, 0x04, 0x00, 0x00}
	for _, tc := range []struct {
		name    string
		options []byte
		df      bool
		from    int
		to      int
		wantDF  bool
	}{
		{"IPv4 shrinks and keeps DF", nil, true, 120, 45, true},
		{"IPv4 keeps its length and DF", nil, true, 64, 64, true},
		{"IPv4 grows and clears DF", nil, true, 45, 333, false},
		{"IPv4 grows without DF", nil, false, 45, 333, false},
		{"IPv4 with options shrinks", routerAlert, true, 77, 31, true},
		{"IPv4 with options grows", routerAlert, true, 31, 77, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := dnsReplyV4(tc.options, tc.df, dnsBytes(tc.from, 1))
			before := append([]byte(nil), raw...)
			ihl := 20 + len(tc.options)
			payload := dnsBytes(tc.to, 9)

			out := ReplaceUDPPayload(raw, ihl, payload)
			if out == nil {
				t.Fatal("ReplaceUDPPayload returned nil")
			}
			if !bytes.Equal(raw, before) {
				t.Fatal("the original packet was modified")
			}
			if len(out) != ihl+8+tc.to || int(binary.BigEndian.Uint16(out[2:4])) != len(out) {
				t.Fatalf("length %d, total length field %d, want %d", len(out), binary.BigEndian.Uint16(out[2:4]), ihl+8+tc.to)
			}
			if got := int(binary.BigEndian.Uint16(out[ihl+4 : ihl+6])); got != 8+tc.to {
				t.Fatalf("UDP length %d, want %d", got, 8+tc.to)
			}
			if !bytes.Equal(out[ihl+8:], payload) {
				t.Fatal("the payload is not the replacement")
			}
			if onesSum(out[:ihl]) != 0xffff {
				t.Fatal("the IPv4 header checksum does not verify")
			}
			if !udpV4ChecksumValid(out, ihl) {
				t.Fatal("the UDP checksum does not verify")
			}
			if out[1] != raw[1] || out[8] != raw[8] || !bytes.Equal(out[4:6], raw[4:6]) {
				t.Fatalf("TOS %#x TTL %d ID %x, want %#x %d %x", out[1], out[8], out[4:6], raw[1], raw[8], raw[4:6])
			}
			if !bytes.Equal(out[20:ihl], raw[20:ihl]) || !bytes.Equal(out[12:20], raw[12:20]) || !bytes.Equal(out[ihl:ihl+4], raw[ihl:ihl+4]) {
				t.Fatal("options, addresses or ports changed")
			}
			if gotDF := out[6]&0x40 != 0; gotDF != tc.wantDF {
				t.Fatalf("DF %v, want %v", gotDF, tc.wantDF)
			}
		})
	}

	for _, tc := range []struct {
		name     string
		from, to int
	}{
		{"IPv6 shrinks", 150, 61},
		{"IPv6 grows", 61, 410},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := dnsReplyV6(dnsBytes(tc.from, 3))
			before := append([]byte(nil), raw...)
			payload := dnsBytes(tc.to, 5)

			out := ReplaceUDPPayload(raw, 40, payload)
			if out == nil {
				t.Fatal("ReplaceUDPPayload returned nil")
			}
			if !bytes.Equal(raw, before) {
				t.Fatal("the original packet was modified")
			}
			if len(out) != 48+tc.to || int(binary.BigEndian.Uint16(out[4:6])) != 8+tc.to || int(binary.BigEndian.Uint16(out[44:46])) != 8+tc.to {
				t.Fatalf("length %d, payload length %d, UDP length %d, want %d", len(out), binary.BigEndian.Uint16(out[4:6]), binary.BigEndian.Uint16(out[44:46]), 8+tc.to)
			}
			if !bytes.Equal(out[48:], payload) {
				t.Fatal("the payload is not the replacement")
			}
			if !udpV6ChecksumValid(out) {
				t.Fatal("the UDP checksum does not verify")
			}
			if !bytes.Equal(out[0:4], raw[0:4]) || out[7] != raw[7] || !bytes.Equal(out[8:44], raw[8:44]) {
				t.Fatal("traffic class, flow label, hop limit, addresses or ports changed")
			}
		})
	}

	t.Run("packets it cannot rewrite", func(t *testing.T) {
		v4 := dnsReplyV4(nil, true, dnsBytes(40, 1))
		fragment := append([]byte(nil), v4...)
		fragment[6] |= 0x20
		tcp := append([]byte(nil), v4...)
		tcp[9] = 6
		hopByHop := make([]byte, 56+20)
		hopByHop[0], hopByHop[6], hopByHop[40] = 0x60, 0, 17
		notUDP := dnsReplyV6(dnsBytes(20, 1))
		notUDP[6] = 6
		for _, c := range []struct {
			name  string
			raw   []byte
			l4off int
			size  int
		}{
			{"IPv6 with an extension header", hopByHop, 48, 20},
			{"IPv6 that is not UDP", notUDP, 40, 20},
			{"an IPv4 fragment", fragment, 20, 20},
			{"IPv4 that is not UDP", tcp, 20, 20},
			{"an offset that is not the IPv4 header length", v4, 24, 20},
			{"a packet shorter than its UDP header", v4[:24], 20, 20},
			{"an IPv4 result over 65535 bytes", v4, 20, 0xffff - 27},
			{"an IPv6 result over 65535 bytes", dnsReplyV6(dnsBytes(20, 1)), 40, 0xffff - 47},
			{"an empty packet", nil, 0, 20},
		} {
			if out := ReplaceUDPPayload(c.raw, c.l4off, dnsBytes(c.size, 2)); out != nil {
				t.Errorf("%s: got a %d byte packet, want nil", c.name, len(out))
			}
		}
		if out := ReplaceUDPPayload(v4, 20, dnsBytes(0xffff-28, 2)); len(out) != 0xffff {
			t.Fatalf("a result of exactly 65535 bytes was refused (%d bytes)", len(out))
		}
	})
}
