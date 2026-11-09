package nfq

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
)

func checksumHoleL3(ver byte) int {
	if ver == IPv4 {
		return 20
	}
	return IPv6HeaderLen
}

func checksumHolePacket(ver, proto byte, l4 []byte) []byte {
	l3 := checksumHoleL3(ver)
	pkt := make([]byte, l3+len(l4))
	if ver == IPv4 {
		pkt[0] = 0x45
		binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
		binary.BigEndian.PutUint16(pkt[4:6], 0x1234)
		pkt[8] = 64
		pkt[9] = proto
		copy(pkt[12:16], net.IPv4(192, 168, 1, 10).To4())
		copy(pkt[16:20], net.IPv4(203, 0, 113, 5).To4())
		sock.FixIPv4Checksum(pkt[:20])
	} else {
		pkt[0] = 0x60
		binary.BigEndian.PutUint16(pkt[4:6], uint16(len(l4)))
		pkt[6] = proto
		pkt[7] = 64
		copy(pkt[8:24], net.ParseIP("2001:db8::10"))
		copy(pkt[24:40], net.ParseIP("2001:db8::5"))
	}
	copy(pkt[l3:], l4)
	return pkt
}

func checksumHoleDst(pkt []byte) net.IP {
	if pkt[0]>>4 == IPv4 {
		return net.IP(pkt[16:20])
	}
	return net.IP(pkt[24:40])
}

func fixTCPChecksumFor(pkt []byte) {
	if pkt[0]>>4 == IPv4 {
		sock.FixTCPChecksum(pkt)
		return
	}
	sock.FixTCPChecksumV6(pkt)
}

func checksumHoleTCPPacket(ver byte, window uint16, payload []byte) []byte {
	tcp := make([]byte, 20+len(payload))
	binary.BigEndian.PutUint16(tcp[0:2], 51234)
	binary.BigEndian.PutUint16(tcp[2:4], 443)
	binary.BigEndian.PutUint32(tcp[4:8], 0x01020304)
	binary.BigEndian.PutUint32(tcp[8:12], 0x0a0b0c0d)
	tcp[12] = 0x50
	tcp[13] = 0x18
	binary.BigEndian.PutUint16(tcp[14:16], window)
	copy(tcp[20:], payload)
	pkt := checksumHolePacket(ver, ipProtoTCP, tcp)
	fixTCPChecksumFor(pkt)
	return pkt
}

func rfc1071UDPChecksumValid(pkt []byte) bool {
	l3 := checksumHoleL3(pkt[0] >> 4)
	var sum uint32
	words := func(b []byte) {
		for i := 0; i+1 < len(b); i += 2 {
			sum += uint32(b[i])<<8 | uint32(b[i+1])
		}
		if len(b)%2 == 1 {
			sum += uint32(b[len(b)-1]) << 8
		}
	}
	if pkt[0]>>4 == IPv4 {
		words(pkt[12:20])
	} else {
		words(pkt[8:40])
	}
	sum += ipProtoUDP + uint32(len(pkt)-l3)
	words(pkt[l3:])
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return sum == 0xffff
}

func TestCorruptTCPChecksumNeverLeavesTheZeroEquivalent(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		t.Run(fmt.Sprintf("v%d", ver), func(t *testing.T) {
			l3 := checksumHoleL3(ver)
			pkt := checksumHoleTCPPacket(ver, 65535, nil)
			copy(pkt[l3+18:l3+20], pkt[l3+16:l3+18])
			fixTCPChecksumFor(pkt)
			if pkt[l3+16] != 0 || pkt[l3+17] != 0 || !rfc1071TCPChecksumValid(pkt) {
				t.Fatalf("the urgent pointer did not tune the correct checksum to 0x0000, got %#04x", binary.BigEndian.Uint16(pkt[l3+16:l3+18]))
			}

			corruptTCPChecksum(pkt, l3)

			if rfc1071TCPChecksumValid(pkt) {
				t.Fatalf("a correct 0x0000 checksum was corrupted into %#04x, which a receiver accepts", binary.BigEndian.Uint16(pkt[l3+16:l3+18]))
			}
		})
	}
}

func sendOOBFakeFor(t *testing.T, strategy string, original []byte) []byte {
	t.Helper()
	set := config.NewSetConfig()
	set.Faking.Strategy = strategy
	set.Faking.TTL = 3
	set.Fragmentation.OOBPosition = 4
	set.Fragmentation.OOBPositionMax = 4
	set.Fragmentation.MiddleSNI = false
	set.Fragmentation.ReverseOrder = false
	set.TCP.Seg2Delay = 0
	set.TCP.Seg2DelayMax = 0
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)
	sent := captureInjected(t, w)

	ver := original[0] >> 4
	if ver == IPv4 {
		w.sendOOBFragments(&set, original, checksumHoleDst(original))
	} else {
		w.sendOOBFragmentsV6(&set, original, checksumHoleDst(original))
	}

	l3 := checksumHoleL3(ver)
	packets := sent()
	for _, p := range packets {
		if p[0]>>4 == ver && len(p) == l3+21 && p[l3+13]&0x20 != 0 {
			return p
		}
	}
	t.Fatalf("no OOB fake among %d sent packets", len(packets))
	return nil
}

func TestOOBBadChecksumFakeNeverCarriesTheZeroEquivalent(t *testing.T) {
	payload := []byte("GET / HTTP/1.1\r\nHost: a\r\n\r\n")
	for _, ver := range []byte{IPv4, IPv6} {
		for _, strategy := range []string{"tcp_check", "md5sum"} {
			t.Run(fmt.Sprintf("v%d/%s", ver, strategy), func(t *testing.T) {
				l3 := checksumHoleL3(ver)
				probe := sendOOBFakeFor(t, "ttl", checksumHoleTCPPacket(ver, 0, payload))
				original := checksumHoleTCPPacket(ver, binary.BigEndian.Uint16(probe[l3+16:l3+18]), payload)
				clean := sendOOBFakeFor(t, "ttl", original)
				if clean[l3+16] != 0 || clean[l3+17] != 0 || !rfc1071TCPChecksumValid(clean) {
					t.Fatalf("the window did not tune the OOB fake's correct checksum to 0x0000, got %#04x", binary.BigEndian.Uint16(clean[l3+16:l3+18]))
				}

				fake := sendOOBFakeFor(t, strategy, original)

				if rfc1071TCPChecksumValid(fake) {
					t.Fatalf("the %s OOB fake went out with checksum %#04x, which a receiver accepts", strategy, binary.BigEndian.Uint16(fake[l3+16:l3+18]))
				}
			})
		}
	}
}

func tunedUDPFakePayload(t *testing.T, original []byte, size int, want uint16) []byte {
	t.Helper()
	ver := original[0] >> 4
	l3 := checksumHoleL3(ver)
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(0x40 + i)
	}
	for x := 0; x <= 0xffff; x++ {
		binary.BigEndian.PutUint16(payload[size-2:], uint16(x))
		var fake []byte
		if ver == IPv4 {
			fake, _ = sock.BuildFakeUDPFromOriginalV4(original, size, 3, payload)
		} else {
			fake, _ = sock.BuildFakeUDPFromOriginalV6(original, size, 3, payload)
		}
		if binary.BigEndian.Uint16(fake[l3+6:l3+8]) == want {
			return payload
		}
	}
	t.Fatalf("no payload gives the fake UDP checksum %#04x", want)
	return nil
}

func TestUDPChecksumFakeNeverCarriesTheZeroEquivalent(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		for _, correct := range []uint16{0xffff, 0xff00} {
			t.Run(fmt.Sprintf("v%d/%#04x", ver, correct), func(t *testing.T) {
				l3 := checksumHoleL3(ver)
				udp := make([]byte, 8+100)
				binary.BigEndian.PutUint16(udp[0:2], 51234)
				binary.BigEndian.PutUint16(udp[2:4], 443)
				binary.BigEndian.PutUint16(udp[4:6], uint16(len(udp)))
				original := checksumHolePacket(ver, ipProtoUDP, udp)
				payload := tunedUDPFakePayload(t, original, 16, correct)

				set := config.NewSetConfig()
				set.Faking.TTL = 3
				set.UDP.Mode = "fake"
				set.UDP.FakeSeqLength = 1
				set.UDP.FakeLen = len(payload)
				set.UDP.FakePayloadFile = ""
				set.UDP.FakePayloadData = payload
				set.UDP.FakingStrategy = "checksum"
				set.UDP.Seg2Delay = 0
				set.UDP.Seg2DelayMax = 0
				cfg := config.NewConfig()
				w := newTestWorker(t, &cfg)
				sent := captureInjected(t, w)

				if ver == IPv4 {
					w.dropAndInjectQUIC(&set, original, checksumHoleDst(original))
				} else {
					w.dropAndInjectQUICV6(&set, original, checksumHoleDst(original))
				}

				var fake []byte
				for _, p := range sent() {
					if len(p) == l3+8+len(payload) && bytes.Equal(p[l3+8:], payload) {
						fake = p
					}
				}
				if fake == nil {
					t.Fatal("the checksum fake was not sent")
				}
				field := binary.BigEndian.Uint16(fake[l3+6 : l3+8])
				if field == 0 || rfc1071UDPChecksumValid(fake) {
					t.Fatalf("a correct UDP checksum %#04x went out as %#04x, which a receiver accepts", correct, field)
				}
			})
		}
	}
}
