package nfq

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func injectedPayloads(sent [][]byte, minLen int) [][]byte {
	var out [][]byte
	for _, p := range sent {
		l3 := int(p[0]&0x0f) * 4
		if p[0]>>4 == IPv6 {
			l3 = IPv6HeaderLen
		}
		off := l3 + int(p[l3+12]>>4)*4
		if len(p)-off >= minLen {
			out = append(out, p[off:])
		}
	}
	return out
}

func sendTwoDecoys(t *testing.T, ver byte, set *config.SetConfig) [][]byte {
	t.Helper()
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)
	sent := captureInjected(t, w)
	hello := buildClientHello("www.example.com", 0, 0)
	for range 2 {
		if ver == IPv6 {
			pkt := makeV6TCPPacket(hello, 1000)
			pi, ok := ExtractPacketInfoV6(pkt)
			if !ok {
				t.Fatal("IPv6 packet info")
			}
			w.sendDecoyPacketV6(set, pkt, pi, net.IP(pkt[24:40]))
			continue
		}
		pkt := buildHTTPPacketV4(hello)
		pkt[23] = 187
		pi, ok := ExtractPacketInfoV4(pkt)
		if !ok {
			t.Fatal("IPv4 packet info")
		}
		w.sendDecoyPacket(set, pkt, pi, net.IP(pkt[16:20]))
	}
	bodies := injectedPayloads(sent(), 100)
	if len(bodies) != 2 {
		t.Fatalf("want 2 decoy bodies, got %d", len(bodies))
	}
	return bodies
}

func TestADecoyCarriesTheTLSModOfItsSet(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		t.Run(fmt.Sprintf("v%d", ver), func(t *testing.T) {
			set := config.NewSetConfig()
			set.Faking.TLSMod = []string{"rnd"}
			bodies := sendTwoDecoys(t, ver, &set)
			if bytes.Equal(bodies[0][9:41], bodies[1][9:41]) || bytes.Equal(bodies[0][9:41], config.FakeSNI1[11:43]) {
				t.Fatalf("decoys with tls_mod rnd carry the built-in Random % x", bodies[0][9:17])
			}
		})
	}
}

func TestACutDecoyDeclaresTheRecordLengthItCarries(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		t.Run(fmt.Sprintf("v%d", ver), func(t *testing.T) {
			set := config.NewSetConfig()
			body := sendTwoDecoys(t, ver, &set)[0]
			size := 2 + len(body)
			if len(config.FakeSNI1) <= size {
				t.Fatalf("the built-in payload is %d bytes and the decoy %d, so it was not cut and this test proves nothing", len(config.FakeSNI1), size)
			}
			if rec := int(binary.BigEndian.Uint16(body[1:3])); rec != size-5 {
				t.Fatalf("a %d-byte decoy declares a %d-byte record, want %d", size, rec, size-5)
			}
		})
	}
}

func TestACutDecoyDeclaresTheHandshakeLengthItCarries(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		t.Run(fmt.Sprintf("v%d", ver), func(t *testing.T) {
			set := config.NewSetConfig()
			body := sendTwoDecoys(t, ver, &set)[0]
			size := 2 + len(body)
			if hs := int(body[4])<<16 | int(body[5])<<8 | int(body[6]); hs != size-5-4 {
				t.Fatalf("a %d-byte decoy declares a %d-byte handshake, want %d", size, hs, size-5-4)
			}
		})
	}
}

func TestAFakeSynCarriesTheTLSModOfItsSet(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		t.Run(fmt.Sprintf("v%d", ver), func(t *testing.T) {
			cfg := config.NewConfig()
			w := newTestWorker(t, &cfg)
			sent := captureInjected(t, w)
			set := config.NewSetConfig()
			set.Faking.TLSMod = []string{"rnd"}
			set.TCP.SynFakeLen = 100

			for range 2 {
				if ver == IPv6 {
					syn := makeV6TCPPacket(nil, 0xAABBCCDD)
					syn[IPv6HeaderLen+13] = 0x02
					w.sendFakeSynV6(&set, syn, IPv6HeaderLen, 20)
					continue
				}
				syn := buildClientSynV4("192.168.1.100", "185.199.110.133", 51234, 443, 0xAABBCCDD)
				w.sendFakeSyn(&set, syn, 20, 20)
			}

			bodies := injectedPayloads(sent(), 100)
			if len(bodies) != 2 {
				t.Fatalf("want 2 fake SYN payloads, got %d", len(bodies))
			}
			if bytes.Equal(bodies[0][11:43], bodies[1][11:43]) || bytes.Equal(bodies[0][11:43], config.FakeSNI1[11:43]) {
				t.Fatalf("fake SYNs with tls_mod rnd carry the built-in Random % x", bodies[0][11:19])
			}
		})
	}
}
