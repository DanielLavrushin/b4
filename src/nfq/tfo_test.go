package nfq

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

var fastOpenSynOptions = []byte{
	0x02, 0x04, 0x05, 0xb4,
	0x04, 0x02,
	0x08, 0x0a, 0x11, 0x22, 0x33, 0x44, 0x00, 0x00, 0x00, 0x00,
	0x01,
	0x03, 0x03, 0x07,
	0x22, 0x0a, 0xc0, 0x0c, 0x1e, 0xc0, 0x0c, 0x1e, 0xc0, 0x0c,
	0x01, 0x01,
}

func fastOpenSyn(ver byte, payload []byte, isn uint32) []byte {
	return synWithOptions(ver, fastOpenSynOptions, payload, isn)
}

func synWithOptions(ver byte, opts, payload []byte, isn uint32) []byte {
	segment := append(append([]byte(nil), opts...), payload...)
	doff := byte((20+len(opts))/4) << 4
	if ver == IPv4 {
		pkt := makeV4TCPPacket(segment, isn)
		pkt[20+12] = doff
		pkt[20+13] = 0x02
		sock.FixIPv4Checksum(pkt[:20])
		sock.FixTCPChecksum(pkt)
		return pkt
	}
	pkt := makeV6TCPPacket(segment, isn)
	pkt[IPv6HeaderLen+12] = doff
	pkt[IPv6HeaderLen+13] = 0x02
	sock.FixTCPChecksumV6(pkt)
	return pkt
}

func newDualStackInjectingWorker(t *testing.T, sets ...*config.SetConfig) *Worker {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Sets = sets
	cfg.Queue.IPv6Enabled = true
	w := newTestWorker(t, &cfg)
	w.sock = &sock.Sender{}
	t.Cleanup(w.wg.Wait)
	return w
}

func ipv4HeaderChecksumValid(h []byte) bool {
	var sum uint32
	for i := 0; i+1 < len(h); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(h[i : i+2]))
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return sum == 0xffff
}

func tcpOptionKinds(pkt []byte) []byte {
	l3 := int(pkt[0]&0x0f) * 4
	if pkt[0]>>4 == IPv6 {
		l3 = IPv6HeaderLen
	}
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

func assertSynWithoutPayload(t *testing.T, got, orig []byte) {
	t.Helper()
	v4 := orig[0]>>4 == IPv4
	l3 := IPv6HeaderLen
	if v4 {
		l3 = int(orig[0]&0x0f) * 4
	}
	tcpHL := int(orig[l3+12]>>4) * 4
	if len(got) != l3+tcpHL {
		t.Fatalf("the SYN must keep its headers and lose its payload: %d bytes, want %d", len(got), l3+tcpHL)
	}
	for i := range got {
		lengthField := (v4 && (i == 2 || i == 3)) || (!v4 && (i == 4 || i == 5))
		checksum := (v4 && (i == 10 || i == 11)) || i == l3+16 || i == l3+17
		if !lengthField && !checksum && got[i] != orig[i] {
			t.Fatalf("header byte %d changed: %#x, want %#x", i, got[i], orig[i])
		}
	}
	if v4 {
		if n := int(binary.BigEndian.Uint16(got[2:4])); n != len(got) {
			t.Errorf("IPv4 total length %d, want %d", n, len(got))
		}
		if !ipv4HeaderChecksumValid(got[:l3]) || !sock.TCPChecksumValid(got) {
			t.Error("the stripped SYN must carry valid checksums")
		}
		return
	}
	if n := int(binary.BigEndian.Uint16(got[4:6])); n != tcpHL {
		t.Errorf("IPv6 payload length %d, want %d", n, tcpHL)
	}
	if !sock.TCPChecksumValidV6(got) {
		t.Error("the stripped SYN must carry a valid checksum")
	}
}

func TestEveryStrategySendsAFastOpenSynWithoutItsPayload(t *testing.T) {
	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	for _, strategy := range []string{"tcp", "ip", "oob", "tls", "disorder", "extsplit", "firstbyte", "combo", "hybrid"} {
		for _, ver := range []byte{IPv4, IPv6} {
			name, l3 := strategy+"/v4", 20
			if ver == IPv6 {
				name, l3 = strategy+"/v6", IPv6HeaderLen
			}
			t.Run(name, func(t *testing.T) {
				set := newFragmentingYTImagesSet()
				set.Fragmentation.Strategy = strategy

				data := fastOpenSyn(ver, hello, 1000)
				data[l3+13] = 0x18
				control := newDualStackInjectingWorker(t, &set)
				controlSent := captureInjected(t, control)
				if v := control.ProcessPacket(data); v != engine.VerdictDrop || len(controlSent()) == 0 {
					t.Fatalf("the ClientHello as a data segment must get the set's strategy, got %v", v)
				}

				w := newDualStackInjectingWorker(t, &set)
				sent := captureInjected(t, w)
				syn := fastOpenSyn(ver, hello, 1000)
				if v := w.ProcessPacket(syn); v != engine.VerdictDrop {
					t.Fatalf("a fast-open SYN of a set that processes the hello must be taken over, got %v", v)
				}
				got := sent()
				if len(got) != 1 {
					t.Fatalf("a fast-open SYN must go out as one SYN, got %d packets", len(got))
				}
				assertSynWithoutPayload(t, got[0], syn)
			})
		}
	}
}

func TestTheHelloResentAfterAStrippedSynGetsTheSetsStrategy(t *testing.T) {
	set := newFragmentingYTImagesSet()
	set.TCP.IPBlockDetect.Enabled = true
	w := newDualStackInjectingWorker(t, &set)
	sent := captureInjected(t, w)
	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	const isn = uint32(1000)
	syn := fastOpenSyn(IPv4, hello, isn)
	w.ProcessPacket(syn)
	if got := loggedSet(t, ch); got != set.Name {
		t.Fatalf("the fast-open SYN row: want set %q, got %q", set.Name, got)
	}
	if got := sent(); len(got) != 1 {
		t.Fatalf("the fast-open SYN must go out as one SYN, got %d packets", len(got))
	} else {
		assertSynWithoutPayload(t, got[0], syn)
	}

	if v := w.ProcessPacket(makeV4TCPPacketFlags(hello, isn+1, 0x18)); v != engine.VerdictDrop {
		t.Fatalf("the ClientHello the client resends after the handshake must get the set's strategy, got %v", v)
	}
	if got := loggedSet(t, ch); got != set.Name {
		t.Errorf("the resent ClientHello row: want set %q, got %q", set.Name, got)
	}
	pieces := sent()
	if len(pieces) != 2 {
		t.Fatalf("the tcp strategy must split the resent ClientHello in two, got %d packets", len(pieces))
	}
	for _, p := range pieces {
		if p[20+13]&0x02 != 0 {
			t.Error("a piece of the resent ClientHello must not be a SYN")
		}
	}
	if e := w.destState.conns["10.0.0.1:12345->1.2.3.4:443"]; e == nil || e.retransmits != 1 {
		t.Errorf("the ClientHello the SYN carried never reached the server and must not count as a retransmission, got %+v", e)
	}
}

func TestAFastOpenSynKeepsItsPayloadWhenTheSetLeavesTheHelloAlone(t *testing.T) {
	set := newPassiveSet()
	w := newDualStackInjectingWorker(t, &set)
	sent := captureInjected(t, w)
	if v := w.ProcessPacket(fastOpenSyn(IPv4, buildClientHello("i.ytimg.com", 16, 0xAB), 1000)); v != engine.VerdictAccept {
		t.Errorf("a set that leaves the ClientHello alone must leave the fast-open SYN alone, got %v", v)
	}
	if got := sent(); len(got) != 0 {
		t.Errorf("nothing may be injected for it, got %d packets", len(got))
	}
}

func TestAFastOpenSynBehindIPv6ExtensionHeadersGoesOutUnchanged(t *testing.T) {
	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	ext := makeV6ExtHeader(6, 8)
	set := newFragmentingYTImagesSet()

	control := newDualStackInjectingWorker(t, &set)
	controlSent := captureInjected(t, control)
	if v := control.ProcessPacket(makeV6TCPPacketWithExt(hello, 1000, 60, ext)); v != engine.VerdictDrop || len(controlSent()) != 1 {
		t.Fatalf("the ClientHello as data behind an extension header must reach the injector, got %v", v)
	}

	syn := makeV6TCPPacketWithExt(hello, 1000, 60, ext)
	syn[IPv6HeaderLen+len(ext)+13] = 0x02
	w := newDualStackInjectingWorker(t, &set)
	sent := captureInjected(t, w)
	if v := w.ProcessPacket(syn); v != engine.VerdictAccept {
		t.Errorf("a fast-open SYN whose payload cannot be cut off must go out as the client sent it, got %v", v)
	}
	if got := sent(); len(got) != 0 {
		t.Errorf("nothing may be injected for it, got %d packets", len(got))
	}
}

func TestDropSACKTreatsAFastOpenSynLikeABareSyn(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		set := newStaticAddressSet(true)
		set.Targets.IpsToMatch = []string{"1.2.3.4", "2001:db8::2"}
		set.TCP.DropSACK = true

		var out [][]byte
		for _, payload := range [][]byte{nil, buildClientHello("i.ytimg.com", 16, 0xAB)} {
			w := newDualStackInjectingWorker(t, &set)
			sent := captureInjected(t, w)
			if v := w.ProcessPacket(fastOpenSyn(ver, payload, 1000)); v != engine.VerdictDrop {
				t.Fatalf("v%d payload %d: a DropSACK set rewrites the SYN, got %v", ver, len(payload), v)
			}
			got := sent()
			if len(got) != 1 {
				t.Fatalf("v%d payload %d: want one SYN, got %d packets", ver, len(payload), len(got))
			}
			out = append(out, got[0])
		}
		if bytes.IndexByte(tcpOptionKinds(out[0]), 4) >= 0 {
			t.Errorf("v%d: DropSACK must take SACK-permitted off a bare SYN", ver)
		}
		if !bytes.Equal(out[0], out[1]) {
			t.Errorf("v%d: a fast-open SYN must leave as the same SYN a bare one does:\n bare %x\n tfo  %x", ver, out[0], out[1])
		}
	}
}

func TestTheSynFakePathSendsAFastOpenSynWithoutItsPayload(t *testing.T) {
	set := newStaticAddressSet(true)
	set.TCP.SynFake = true
	w := newDualStackInjectingWorker(t, &set)
	sent := captureInjected(t, w)

	syn := fastOpenSyn(IPv4, buildClientHello("i.ytimg.com", 16, 0xAB), 1000)
	if v := w.ProcessPacket(syn); v != engine.VerdictDrop {
		t.Fatalf("the SYN fake path takes the SYN over, got %v", v)
	}
	got := sent()
	if len(got) != 2 {
		t.Fatalf("want the fake SYN and the real one, got %d packets", len(got))
	}
	if sock.TCPChecksumValid(got[0]) || len(got[0]) != 20+20+len(fastOpenSynOptions) {
		t.Error("the fake SYN keeps its broken checksum and carries no ClientHello")
	}
	assertSynWithoutPayload(t, got[1], syn)
}

func TestTheMD5FakeSynAlwaysCarriesTheMD5Option(t *testing.T) {
	roomy := []byte{0x02, 0x04, 0x05, 0xb4, 0x04, 0x02, 0x08, 0x0a, 0, 0, 0, 1, 0, 0, 0, 0, 0x01, 0x03, 0x03, 0x07}
	tight := []byte{0x02, 0x04, 0x05, 0xb4, 0x01, 0x03, 0x03, 0x06, 0x01, 0x01, 0x08, 0x0a, 0, 0, 0, 1, 0, 0, 0, 0, 0x04, 0x02, 0x00, 0x00}
	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	cases := []struct {
		name          string
		opts, payload []byte
		keepsOptions  bool
	}{
		{"options that leave room", roomy, nil, true},
		{"24 bytes of options", tight, nil, false},
		{"fast open", fastOpenSynOptions, hello, false},
	}
	for _, ver := range []byte{IPv4, IPv6} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("v%d/%s", ver, tc.name), func(t *testing.T) {
				set := newStaticAddressSet(true)
				set.Targets.IpsToMatch = []string{"1.2.3.4", "2001:db8::2"}
				set.Faking.TCPMD5 = true
				w := newDualStackInjectingWorker(t, &set)
				sent := captureInjected(t, w)

				const isn = uint32(50000)
				syn := synWithOptions(ver, tc.opts, tc.payload, isn)
				if v := w.ProcessPacket(syn); v != engine.VerdictDrop {
					t.Fatalf("the MD5 SYN path takes the SYN over, got %v", v)
				}
				got := sent()
				if len(got) != 2 {
					t.Fatalf("want the MD5 fake and the real SYN, got %d packets", len(got))
				}
				fake, real := got[0], got[1]
				l3, ttl := 20, fake[8]
				if ver == IPv6 {
					l3, ttl = IPv6HeaderLen, fake[7]
				}

				if seq := binary.BigEndian.Uint32(fake[l3+4 : l3+8]); seq != isn-10000 || ttl >= 64 {
					t.Errorf("the fake SYN: seq %d ttl %d, want seq %d and a low TTL", seq, ttl, isn-10000)
				}
				want := []byte{19}
				if tc.keepsOptions {
					want = append(tcpOptionKinds(syn), 19)
				}
				if kinds := tcpOptionKinds(fake); !bytes.Equal(kinds, want) {
					t.Errorf("the fake SYN's options %v, want %v", kinds, want)
				}
				if len(fake) != l3+int(fake[l3+12]>>4)*4 {
					t.Error("the fake SYN must not carry the ClientHello")
				}
				if ver == IPv4 {
					if int(binary.BigEndian.Uint16(fake[2:4])) != len(fake) || !ipv4HeaderChecksumValid(fake[:l3]) || !sock.TCPChecksumValid(fake) {
						t.Error("the fake SYN's length or checksums are wrong")
					}
				} else if int(binary.BigEndian.Uint16(fake[4:6])) != len(fake)-IPv6HeaderLen || !sock.TCPChecksumValidV6(fake) {
					t.Error("the fake SYN's length or checksum is wrong")
				}

				if len(tc.payload) > 0 {
					assertSynWithoutPayload(t, real, syn)
				} else if !bytes.Equal(real, syn) {
					t.Error("a SYN without payload must go out unchanged")
				}
			})
		}
	}
}
