package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
)

func withOvershootingExtLength(pkt []byte) []byte {
	out := make([]byte, len(pkt))
	copy(out, pkt)
	out[IPv6HeaderLen+1] = 0xFF
	return out
}

func TestParseIPHeadersRejectsIPv6ExtensionHeaderPastEndOfPacket(t *testing.T) {
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)
	payload := makeTLSLikePayload(80)

	for _, tc := range []struct {
		name string
		pkt  []byte
	}{
		{name: "tcp", pkt: withOvershootingExtLength(makeV6TCPPacketWithExt(payload, 1000, 60, makeV6ExtHeader(6, 8)))},
		{name: "udp", pkt: withOvershootingExtLength(makeV6UDPPacketWithExt(payload, 51000, 443, 60, makeV6ExtHeader(17, 8)))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := w.parseIPHeaders(tc.pkt)
			if ok {
				t.Fatalf("a destination-options length pointing past the end must be rejected, got ihl %d on a %d byte packet", p.ihl, len(tc.pkt))
			}

			if v := w.ProcessPacket(tc.pkt); v != engine.VerdictAccept {
				t.Fatalf("an unparsable IPv6 packet must be accepted untouched, got verdict %v", v)
			}
		})
	}
}

func TestParseIPHeadersRejectsChainedIPv6ExtensionHeaderPastEndOfPacket(t *testing.T) {
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)
	payload := makeTLSLikePayload(80)

	ext := append(makeV6ExtHeader(60, 8), makeV6ExtHeader(6, 8)...)
	pkt := makeV6TCPPacketWithExt(payload, 1000, 60, ext)
	pkt[IPv6HeaderLen+9] = 0xFF

	if p, ok := w.parseIPHeaders(pkt); ok {
		t.Fatalf("the second header of the chain overshoots the packet and must be rejected, got ihl %d on a %d byte packet", p.ihl, len(pkt))
	}
	if v := w.ProcessPacket(pkt); v != engine.VerdictAccept {
		t.Fatalf("an unparsable IPv6 packet must be accepted untouched, got verdict %v", v)
	}
}
