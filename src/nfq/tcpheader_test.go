package nfq

import (
	"fmt"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sock"
)

func mustNotPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", what, r)
		}
	}()
	f()
}

func packetWithDataOffset(ver byte, words, flags byte) []byte {
	if ver == IPv4 {
		pkt := makeV4TCPPacket(make([]byte, 64), 1000)
		pkt[20+12] = words << 4
		pkt[20+13] = flags
		return pkt
	}
	pkt := makeV6TCPPacket(make([]byte, 64), 1000)
	pkt[IPv6HeaderLen+12] = words << 4
	pkt[IPv6HeaderLen+13] = flags
	return pkt
}

func TestAPacketWithATCPHeaderShorterThan20BytesPassesUntouched(t *testing.T) {
	type scenario struct {
		name  string
		flags byte
		apply func(*config.SetConfig)
	}
	scenarios := []scenario{
		{"dropsack syn", 0x02, func(s *config.SetConfig) { s.TCP.DropSACK = true }},
		{"syn fake", 0x02, func(s *config.SetConfig) { s.TCP.SynFake = true }},
		{"md5 syn", 0x02, func(s *config.SetConfig) { s.Faking.TCPMD5 = true }},
		{"window", 0x18, func(s *config.SetConfig) { s.TCP.Win.Mode = "oscillate" }},
	}
	for _, strategy := range []string{"tcp", "ip", "oob", "tls", "disorder", "extsplit", "firstbyte", "combo", "hybrid"} {
		scenarios = append(scenarios, scenario{"strategy " + strategy, 0x18, func(s *config.SetConfig) { s.Fragmentation.Strategy = strategy }})
	}
	for words := byte(0); words < 5; words++ {
		for _, ver := range []byte{IPv4, IPv6} {
			for _, sc := range scenarios {
				t.Run(fmt.Sprintf("v%d/offset%d/%s", ver, words, sc.name), func(t *testing.T) {
					set := newStaticAddressSet(true)
					set.Targets.IpsToMatch = []string{"1.2.3.4", "2001:db8::2"}
					sc.apply(&set)
					w := newDualStackInjectingWorker(t, &set)
					sent := captureInjected(t, w)
					mustNotPanic(t, "ProcessPacket", func() {
						if v := w.ProcessPacket(packetWithDataOffset(ver, words, sc.flags)); v != engine.VerdictAccept {
							t.Errorf("a packet with a %d-byte TCP header must pass untouched, got %v", int(words)*4, v)
						}
					})
					if got := sent(); len(got) != 0 {
						t.Errorf("nothing may be injected for a malformed header, sent %d packets", len(got))
					}
				})
			}
		}
	}
}

func TestRewritersRefuseATCPHeaderShorterThan20Bytes(t *testing.T) {
	for words := byte(0); words < 5; words++ {
		for _, ver := range []byte{IPv4, IPv6} {
			t.Run(fmt.Sprintf("v%d/offset%d", ver, words), func(t *testing.T) {
				l3 := 20
				if ver == IPv6 {
					l3 = IPv6HeaderLen
				}

				syn := packetWithDataOffset(ver, words, 0x02)
				mustNotPanic(t, "synWithoutPayload", func() {
					if _, ok := synWithoutPayload(syn, ver, l3, int(words)*4); ok {
						t.Error("synWithoutPayload must refuse a TCP header shorter than 20 bytes")
					}
				})

				mustNotPanic(t, "AddTCPMD5Option", func() {
					if _, ok := sock.AddTCPMD5Option(syn, ver == IPv6); ok {
						t.Error("AddTCPMD5Option must refuse a TCP header shorter than 20 bytes")
					}
				})

				set := newFragmentingYTImagesSet()
				w := newDualStackInjectingWorker(t, &set)
				sent := captureInjected(t, w)
				data := packetWithDataOffset(ver, words, 0x18)
				for _, mode := range []string{"oscillate", "random", "escalate", "zero"} {
					set.TCP.Win.Mode = mode
					mustNotPanic(t, "window manipulation "+mode, func() {
						if ver == IPv4 {
							w.ManipulateWindowIPv4(&set, data, net.IP{1, 2, 3, 4})
						} else {
							w.ManipulateWindowIPv6(&set, data, net.ParseIP("2001:db8::2"))
						}
					})
				}
				if got := sent(); len(got) != 0 {
					t.Errorf("window manipulation must send nothing for a malformed header, sent %d packets", len(got))
				}
			})
		}
	}
}
