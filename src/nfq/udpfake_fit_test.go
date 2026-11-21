package nfq

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func sentUDPFakePayloads(pkts [][]byte, real []byte) [][]byte {
	var out [][]byte
	for _, p := range pkts {
		l3 := IPv6HeaderLen
		if p[0]>>4 == IPv4 {
			l3 = int(p[0]&0x0f) * 4
		}
		if len(p) < l3+UDPHeaderLen {
			continue
		}
		payload := p[l3+UDPHeaderLen:]
		if string(payload) == string(real) {
			continue
		}
		out = append(out, payload)
	}
	return out
}

func TestAUDPFakeAtTheLargestFakeLenKeepsItsInnerLengths(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		for _, kind := range []string{config.FakePayloadAutoQUIC, config.FakePayloadAutoSTUN} {
			for _, fakeLen := range []int{1460, 1472} {
				t.Run(fmt.Sprintf("v%d/%s/%d", ver, kind, fakeLen), func(t *testing.T) {
					set := udpFakeModeSet("udp-fake-fit", fakeLen)
					set.UDP.FakePayloadFile = kind
					w := newDualStackInjectingWorker(t, &set)
					sent := captureInjected(t, w)
					raw, dst := udpFakeTestPacket(ver)

					injectUDPFake(w, &set, ver, raw, dst)

					l3 := IPv6HeaderLen
					if ver == IPv4 {
						l3 = int(raw[0]&0x0f) * 4
					}
					pkts := sent()
					fakes := sentUDPFakePayloads(pkts, raw[l3+UDPHeaderLen:])
					if len(fakes) != 1 {
						t.Fatalf("%d fakes sent, want one", len(fakes))
					}
					fake := fakes[0]
					for _, p := range pkts {
						if len(p) > 1500 {
							t.Fatalf("a %d-byte packet left, more than one 1500-byte packet carries", len(p))
						}
					}
					switch kind {
					case config.FakePayloadAutoQUIC:
						if n := int(binary.BigEndian.Uint16(fake[24:26]) & 0x3FFF); n != len(fake)-26 {
							t.Fatalf("the %d-byte QUIC fake declares %d bytes after its length field, %d follow", len(fake), n, len(fake)-26)
						}
					case config.FakePayloadAutoSTUN:
						if n := int(binary.BigEndian.Uint16(fake[2:4])); n != len(fake)-20 {
							t.Fatalf("the %d-byte STUN fake declares a %d-byte body, %d follow", len(fake), n, len(fake)-20)
						}
						if n := int(binary.BigEndian.Uint16(fake[22:24])); n != len(fake)-24 {
							t.Fatalf("the %d-byte STUN fake's padding declares %d bytes, %d follow", len(fake), n, len(fake)-24)
						}
					}
				})
			}
		}
	}
}

func fakeClientHelloWarnings(buf *syncBuffer, setName string) []string {
	var lines []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "[WARN]") && strings.Contains(line, "Fake ClientHello") && strings.Contains(line, fmt.Sprintf("%q", setName)) {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestATCPFakeWarningDoesNotHideAUDPOne(t *testing.T) {
	for _, first := range []string{"TCP", "UDP"} {
		t.Run(first+" first", func(t *testing.T) {
			buf := captureLog(t)
			set := udpFakeModeSet("both-fakes-refused", 64)
			w := newDualStackInjectingWorker(t, &set)
			w.sock = refusingSender(t)
			raw, dst := udpFakeTestPacket(IPv4)
			tcp := func() { w.warnFakeSend(&set, 600, net.IPv4(1, 2, 3, 4), errors.New("refused")) }
			udp := func() { injectUDPFake(w, &set, IPv4, raw, dst) }
			if first == "TCP" {
				tcp()
				udp()
			} else {
				udp()
				tcp()
			}

			log.Flush()
			if lines := fakeClientHelloWarnings(buf, set.Name); len(lines) != 1 {
				t.Errorf("want one warning for the refused fake ClientHello, got %d:\n%s", len(lines), buf.String())
			}
			if lines := udpFakeWarnings(buf, set.Name); len(lines) != 1 {
				t.Errorf("want one warning for the refused UDP fake, got %d:\n%s", len(lines), buf.String())
			}
		})
	}
}
