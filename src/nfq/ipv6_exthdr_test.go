package nfq

import (
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
)

const (
	v6TestSrc = "2001:db8::1"
	v6TestDst = "2001:db8::2"
)

func makeV6ExtHeader(nextHeader byte, octets int) []byte {
	if octets < 8 || octets%8 != 0 {
		panic("extension headers are a multiple of 8 bytes")
	}
	ext := make([]byte, octets)
	ext[0] = nextHeader
	ext[1] = byte(octets/8 - 1)
	ext[2] = 1
	ext[3] = byte(octets - 4)
	return ext
}

func makeV6FragHeader(nextHeader byte) []byte {
	ext := make([]byte, 8)
	ext[0] = nextHeader
	binary.BigEndian.PutUint32(ext[4:8], 0xB4B4B4B4)
	return ext
}

func makeV6TCPPacketWithExt(payload []byte, seq uint32, extType byte, ext []byte) []byte {
	const tcpHL = 20
	pkt := make([]byte, IPv6HeaderLen+len(ext)+tcpHL+len(payload))

	pkt[0] = 0x60
	binary.BigEndian.PutUint16(pkt[4:6], uint16(len(pkt)-IPv6HeaderLen))
	if len(ext) > 0 {
		pkt[6] = extType
	} else {
		pkt[6] = 6
	}
	pkt[7] = 64
	copy(pkt[8:24], net.ParseIP(v6TestSrc).To16())
	copy(pkt[24:40], net.ParseIP(v6TestDst).To16())
	copy(pkt[IPv6HeaderLen:], ext)

	tcp := IPv6HeaderLen + len(ext)
	binary.BigEndian.PutUint16(pkt[tcp:tcp+2], 12345)
	binary.BigEndian.PutUint16(pkt[tcp+2:tcp+4], 443)
	binary.BigEndian.PutUint32(pkt[tcp+4:tcp+8], seq)
	pkt[tcp+12] = byte(tcpHL/4) << 4
	pkt[tcp+13] = 0x18
	binary.BigEndian.PutUint16(pkt[tcp+14:tcp+16], 65535)

	copy(pkt[tcp+tcpHL:], payload)
	return pkt
}

func makeV6UDPPacketWithExt(payload []byte, sport, dport uint16, extType byte, ext []byte) []byte {
	pkt := make([]byte, IPv6HeaderLen+len(ext)+UDPHeaderLen+len(payload))

	pkt[0] = 0x60
	binary.BigEndian.PutUint16(pkt[4:6], uint16(len(pkt)-IPv6HeaderLen))
	if len(ext) > 0 {
		pkt[6] = extType
	} else {
		pkt[6] = 17
	}
	pkt[7] = 64
	copy(pkt[8:24], net.ParseIP(v6TestSrc).To16())
	copy(pkt[24:40], net.ParseIP(v6TestDst).To16())
	copy(pkt[IPv6HeaderLen:], ext)

	udp := IPv6HeaderLen + len(ext)
	binary.BigEndian.PutUint16(pkt[udp:udp+2], sport)
	binary.BigEndian.PutUint16(pkt[udp+2:udp+4], dport)
	binary.BigEndian.PutUint16(pkt[udp+4:udp+6], uint16(UDPHeaderLen+len(payload)))
	copy(pkt[udp+UDPHeaderLen:], payload)
	return pkt
}

func makeTLSLikePayload(n int) []byte {
	payload := make([]byte, n)
	payload[0] = 0x16
	payload[1] = 0x03
	payload[2] = 0x01
	binary.BigEndian.PutUint16(payload[3:5], uint16(n-5))
	payload[5] = 0x01
	for i := 6; i < n; i++ {
		payload[i] = byte(i)
	}
	return payload
}

func newInjectWorker(t *testing.T) (*Worker, net.IP) {
	t.Helper()
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)
	w.sock = &sock.Sender{}
	return w, net.ParseIP(v6TestDst)
}

func newExtHdrSet() config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "v6-ext"
	set.Name = "v6 ext"
	set.Enabled = true
	set.Fragmentation.StrategyPool = nil
	set.Fragmentation.SNIPosition = 75
	set.Fragmentation.SNIPositionMax = 0
	set.TCP.Seg2Delay = 0
	set.TCP.Seg2DelayMax = 0
	set.TCP.Desync.Mode = config.ConfigOff
	set.TCP.Desync.PostDesync = false
	set.TCP.Win.Mode = config.ConfigOff
	set.Faking.SNI = false
	set.Faking.SNIMutation.Mode = config.ConfigOff
	return set
}

var v6InjectStrategies = []string{"tcp", "ip", "oob", "tls", "disorder", "extsplit", "firstbyte", "combo", "hybrid", "none", "unknown-strategy"}

func TestDropAndInjectTCPv6SurvivesDestinationOptionsHeader(t *testing.T) {
	for _, payloadLen := range []int{52, 80} {
		pkt := makeV6TCPPacketWithExt(makeTLSLikePayload(payloadLen), 1000, 60, makeV6ExtHeader(6, 8))

		for _, strategy := range v6InjectStrategies {
			for _, allInjectors := range []bool{false, true} {
				name := fmt.Sprintf("%d/%s", payloadLen, strategy)
				if allInjectors {
					name += "/all-injectors"
				}
				t.Run(name, func(t *testing.T) {
					w, dst := newInjectWorker(t)
					set := newExtHdrSet()
					set.Fragmentation.Strategy = strategy
					if allInjectors {
						set.TCP.Desync.Mode = "combo"
						set.TCP.Desync.PostDesync = true
						set.TCP.Win.Mode = "oscillate"
						set.Faking.SNI = true
						set.Faking.SNISeqLength = 2
						set.Faking.SNIMutation.Mode = "full"
					}

					raw := make([]byte, len(pkt))
					copy(raw, pkt)

					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("dropAndInjectTCPv6 panicked on an IPv6 packet carrying a destination-options header: %v", r)
						}
					}()

					w.dropAndInjectTCPv6(&set, raw, dst)
				})
			}
		}
	}
}

func TestDropAndInjectQUICV6SurvivesDestinationOptionsHeader(t *testing.T) {
	quicPayload := makeTLSLikePayload(120)
	quicPayload[0] = 0xC0
	pkt := makeV6UDPPacketWithExt(quicPayload, 51000, 443, 60, makeV6ExtHeader(17, 8))

	w, dst := newInjectWorker(t)
	set := newExtHdrSet()
	set.UDP.Mode = "fake"
	set.UDP.FakeSeqLength = 2
	set.UDP.FakeLen = 64

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("dropAndInjectQUICV6 panicked on an IPv6 packet carrying a destination-options header: %v", r)
		}
	}()

	w.dropAndInjectQUICV6(&set, pkt, dst)
}

func TestDropAndInjectTCPv6KeepsStrategyPathForPlainPacket(t *testing.T) {
	const delayMs = 300
	payload := makeTLSLikePayload(80)

	set := newExtHdrSet()
	set.Fragmentation.Strategy = "tcp"
	set.TCP.Seg2Delay = delayMs

	w, dst := newInjectWorker(t)

	plain := makeV6TCPPacketWithExt(payload, 1000, 0, nil)
	start := time.Now()
	w.dropAndInjectTCPv6(&set, plain, dst)
	plainElapsed := time.Since(start)

	if plainElapsed < delayMs*time.Millisecond {
		t.Fatalf("a plain IPv6 TCP packet must still be split by the tcp strategy (took %s, the inter-segment delay alone is %dms)", plainElapsed, delayMs)
	}

	withExt := makeV6TCPPacketWithExt(payload, 1000, 60, makeV6ExtHeader(6, 8))
	start = time.Now()
	w.dropAndInjectTCPv6(&set, withExt, dst)
	extElapsed := time.Since(start)

	if extElapsed >= delayMs*time.Millisecond {
		t.Fatalf("an IPv6 packet with a destination-options header must be passed through untouched, not split (took %s)", extElapsed)
	}
}

func TestUpperLayerOffsetV6MatchesParseIPHeaders(t *testing.T) {
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)
	payload := makeTLSLikePayload(80)

	for _, tc := range []struct {
		name    string
		extType byte
		ext     []byte
		wantIHL int
		wantOK  bool
	}{
		{name: "no extension header", extType: 0, ext: nil, wantIHL: 40, wantOK: true},
		{name: "hop-by-hop", extType: 0, ext: makeV6ExtHeader(6, 8), wantIHL: 48, wantOK: true},
		{name: "routing", extType: 43, ext: makeV6ExtHeader(6, 16), wantIHL: 56, wantOK: true},
		{name: "destination options", extType: 60, ext: makeV6ExtHeader(6, 8), wantIHL: 48, wantOK: true},
		{name: "chained", extType: 60, ext: append(makeV6ExtHeader(43, 8), makeV6ExtHeader(6, 8)...), wantIHL: 56, wantOK: true},
		{name: "fragment", extType: 44, ext: makeV6FragHeader(6), wantIHL: 0, wantOK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkt := makeV6TCPPacketWithExt(payload, 1000, tc.extType, tc.ext)

			p, parsed := w.parseIPHeaders(pkt)
			if parsed != tc.wantOK {
				t.Fatalf("parseIPHeaders ok = %v, want %v", parsed, tc.wantOK)
			}

			off, proto, walked := upperLayerOffsetV6(pkt)
			if walked != tc.wantOK {
				t.Fatalf("upperLayerOffsetV6 ok = %v, want %v", walked, tc.wantOK)
			}

			pi, extracted := ExtractPacketInfoV6(pkt)
			if extracted != tc.wantOK {
				t.Fatalf("ExtractPacketInfoV6 ok = %v, want %v", extracted, tc.wantOK)
			}

			if !tc.wantOK {
				return
			}

			if p.ihl != tc.wantIHL {
				t.Fatalf("parseIPHeaders ihl = %d, want %d", p.ihl, tc.wantIHL)
			}
			if off != p.ihl {
				t.Fatalf("upperLayerOffsetV6 offset = %d, parseIPHeaders ihl = %d", off, p.ihl)
			}
			if pi.IPHdrLen != p.ihl {
				t.Fatalf("ExtractPacketInfoV6 IPHdrLen = %d, parseIPHeaders ihl = %d", pi.IPHdrLen, p.ihl)
			}
			if proto != p.proto {
				t.Fatalf("upperLayerOffsetV6 proto = %d, parseIPHeaders proto = %d", proto, p.proto)
			}
		})
	}
}

func TestHasPlainIPv6HeaderOnlyAcceptsA40ByteHeader(t *testing.T) {
	payload := makeTLSLikePayload(80)

	tcpPlain := makeV6TCPPacketWithExt(payload, 1000, 0, nil)
	if !hasPlainIPv6Header(tcpPlain, ipProtoTCP) {
		t.Error("a plain IPv6 TCP packet must be accepted by the TCP injectors")
	}
	if hasPlainIPv6Header(tcpPlain, ipProtoUDP) {
		t.Error("a TCP packet must not be accepted by the UDP path")
	}

	tcpExt := makeV6TCPPacketWithExt(payload, 1000, 60, makeV6ExtHeader(6, 8))
	if hasPlainIPv6Header(tcpExt, ipProtoTCP) {
		t.Error("an extension header moves the TCP header off the hardcoded offset 40 and must be rejected")
	}

	udpPlain := makeV6UDPPacketWithExt(payload, 51000, 443, 0, nil)
	if !hasPlainIPv6Header(udpPlain, ipProtoUDP) {
		t.Error("a plain IPv6 UDP packet must be accepted by the QUIC path")
	}

	udpExt := makeV6UDPPacketWithExt(payload, 51000, 443, 60, makeV6ExtHeader(17, 8))
	if hasPlainIPv6Header(udpExt, ipProtoUDP) {
		t.Error("an extension header moves the UDP header off the hardcoded offset 40 and must be rejected")
	}

	if hasPlainIPv6Header(tcpPlain[:39], ipProtoTCP) {
		t.Error("a truncated IPv6 header must be rejected")
	}
	if hasPlainIPv6Header(makeV4UDPPacket(payload, net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4"), 1, 2), ipProtoUDP) {
		t.Error("an IPv4 packet must be rejected by the IPv6 guard")
	}
}
