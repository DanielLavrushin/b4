package nfq

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/sock"
)

func dataPacketWithOptions(ver byte, opts []byte) []byte {
	segment := append(append([]byte(nil), opts...), tlsAppData(64)...)
	doff := byte((20+len(opts))/4) << 4
	if ver == IPv4 {
		pkt := makeV4TCPPacket(segment, 5000)
		pkt[20+12] = doff
		sock.FixIPv4Checksum(pkt[:20])
		sock.FixTCPChecksum(pkt)
		return pkt
	}
	pkt := makeV6TCPPacket(segment, 5000)
	pkt[IPv6HeaderLen+12] = doff
	sock.FixTCPChecksumV6(pkt)
	return pkt
}

func TestWindowFakesCarryTheWholeTCPHeader(t *testing.T) {
	timestamps := []byte{0x01, 0x01, 0x08, 0x0a, 0x00, 0x00, 0x10, 0x01, 0x00, 0x00, 0x20, 0x02}
	for _, mode := range []string{"oscillate", "random", "escalate", "zero"} {
		for _, ver := range []byte{IPv4, IPv6} {
			for _, opts := range [][]byte{nil, timestamps} {
				t.Run(fmt.Sprintf("%s/v%d/options=%d", mode, ver, len(opts)), func(t *testing.T) {
					set := newFragmentingYTImagesSet()
					set.TCP.Win.Mode = mode
					w := newDualStackInjectingWorker(t, &set)
					sent := captureInjected(t, w)

					pkt := dataPacketWithOptions(ver, opts)
					l3 := 20
					if ver == IPv4 {
						w.ManipulateWindowIPv4(&set, pkt, net.IP{1, 2, 3, 4})
					} else {
						l3 = IPv6HeaderLen
						w.ManipulateWindowIPv6(&set, pkt, net.ParseIP("2001:db8::2"))
					}
					tcpHL := 20 + len(opts)
					want := l3 + tcpHL
					if mode == "zero" {
						want = len(pkt)
					}

					fakes := sent()
					if len(fakes) == 0 {
						t.Fatal("window manipulation sent nothing")
					}
					for i, f := range fakes {
						if len(f) != want {
							t.Fatalf("fake %d: %d bytes, want %d", i, len(f), want)
						}
						if hl := int(f[l3+12]>>4) * 4; hl != tcpHL {
							t.Fatalf("fake %d: TCP header length %d, want %d", i, hl, tcpHL)
						}
						if !bytes.Equal(f[l3+20:l3+tcpHL], pkt[l3+20:l3+tcpHL]) {
							t.Errorf("fake %d: the TCP options must be the original's", i)
						}
						if ver == IPv4 {
							if n := int(binary.BigEndian.Uint16(f[2:4])); n != len(f) || !ipv4HeaderChecksumValid(f[:l3]) || !sock.TCPChecksumValid(f) {
								t.Errorf("fake %d: total length %d of %d bytes, or a bad checksum", i, n, len(f))
							}
						} else if n := int(binary.BigEndian.Uint16(f[4:6])); n != len(f)-IPv6HeaderLen || !sock.TCPChecksumValidV6(f) {
							t.Errorf("fake %d: payload length %d of %d bytes, or a bad checksum", i, n, len(f))
						}
					}
				})
			}
		}
	}
}
