package nfq

import (
	"bytes"
	"encoding/binary"
	"slices"
	"strconv"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sock"
)

func newFakingSet(fragment bool) config.SetConfig {
	set := newFragmentingYTImagesSet()
	set.Targets.DomainOnly = false
	if !fragment {
		set.Fragmentation.Strategy = config.ConfigNone
	}
	set.Faking.SNI = true
	set.Faking.SNISeqLength = 1
	set.Faking.Strategy = "timestamp"
	set.Faking.SNIType = config.FakePayloadDefault2
	return set
}

func fakeHelloSeqs(pkts [][]byte, fake []byte) []uint32 {
	var seqs []uint32
	for _, p := range pkts {
		if len(p) == 0 {
			continue
		}
		ihl := 40
		if p[0]>>4 == IPv4 {
			ihl = int(p[0]&0x0f) * 4
		}
		if len(p) < ihl+TCPHeaderMinLen {
			continue
		}
		tcp := p[ihl:]
		off := int(tcp[12]>>4) * 4
		if len(tcp) > off && bytes.Equal(tcp[off:], fake) {
			seqs = append(seqs, binary.BigEndian.Uint32(tcp[4:8]))
		}
	}
	slices.Sort(seqs)
	return seqs
}

func realSegments(pkts [][]byte, fake []byte) []sentSegment {
	var out []sentSegment
	for _, s := range sentData(pkts) {
		if !bytes.Equal(s.payload, fake) {
			out = append(out, s)
		}
	}
	return out
}

func tls12KeyExchange() []byte {
	rec := []byte{0x16, 0x03, 0x03, 0x00, 0x25, 0x10, 0x00, 0x00, 0x21, 0x20}
	rec = append(rec, bytes.Repeat([]byte{0x5a}, 32)...)
	rec = append(rec, 0x14, 0x03, 0x03, 0x00, 0x01, 0x01)
	rec = append(rec, 0x16, 0x03, 0x03, 0x00, 0x28)
	return append(rec, bytes.Repeat([]byte{0x77}, 40)...)
}

func changeCipherSpecAndFinished() []byte {
	return append([]byte{0x14, 0x03, 0x03, 0x00, 0x01, 0x01}, tlsAppData(53)...)
}

func TestAFakeClientHelloGoesOutOnlyWithTheClientHello(t *testing.T) {
	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	const base = uint32(1000)
	next := base + uint32(len(hello))
	type later struct {
		name    string
		payload []byte
		seq     uint32
	}
	tls13 := []later{
		{"change cipher spec and finished", changeCipherSpecAndFinished(), next},
		{"application data", tlsAppData(200), next + 64},
	}
	cases := []struct {
		name     string
		fragment bool
		setup    func(*config.SetConfig)
		before   func(*Worker, *config.SetConfig)
		later    []later
	}{
		{name: "learned address", fragment: true, later: tls13},
		{name: "domain-only", fragment: true, setup: func(s *config.SetConfig) { s.Targets.DomainOnly = true }, later: tls13},
		{name: "dns hint", fragment: true, before: func(w *Worker, s *config.SetConfig) {
			w.storeHostHint("10.0.0.1", "1.2.3.4", s, "i.ytimg.com", "dns", 0, 0)
		}, later: tls13},
		{name: "address set", fragment: true, setup: func(s *config.SetConfig) {
			s.Targets.DomainsToMatch = nil
			s.Targets.IpsToMatch = []string{"1.2.3.4"}
		}, later: tls13},
		{name: "dns hint, no fragmentation", before: func(w *Worker, s *config.SetConfig) {
			w.storeHostHint("10.0.0.1", "1.2.3.4", s, "i.ytimg.com", "dns", 0, 0)
		}, later: tls13},
		{name: "learned address, no fragmentation, TLS 1.2", later: []later{{"client key exchange", tls12KeyExchange(), next}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set := newFakingSet(c.fragment)
			if c.setup != nil {
				c.setup(&set)
			}
			cfg := config.NewConfig()
			cfg.Sets = []*config.SetConfig{&set}
			w := newTestWorker(t, &cfg)
			sent := captureInjected(t, w)
			fake := sock.GetPayload(&set.Faking)
			if c.before != nil {
				c.before(w, &set)
			}

			w.ProcessPacket(makeV4TCPPacket(hello, base))
			if got := fakeHelloSeqs(sent(), fake); !slices.Equal(got, []uint32{base}) {
				t.Fatalf("the ClientHello: fake hellos at %v, want one at %d", got, base)
			}
			for _, l := range c.later {
				if v := w.ProcessPacket(makeV4TCPPacket(l.payload, l.seq)); v != engine.VerdictDrop {
					t.Fatalf("%s after the ClientHello: the set keeps the packet, got %v", l.name, v)
				}
				pkts := sent()
				if got := fakeHelloSeqs(pkts, fake); len(got) != 0 {
					t.Errorf("%s after the ClientHello: fake hellos at %v, want none", l.name, got)
				}
				coversOnce(t, realSegments(pkts, fake), l.seq, l.payload)
			}
			w.ProcessPacket(makeV4TCPPacket(hello, base))
			if got := fakeHelloSeqs(sent(), fake); !slices.Equal(got, []uint32{base}) {
				t.Errorf("a resent ClientHello: fake hellos at %v, want one at %d", got, base)
			}
		})
	}
}

func TestAFakeClientHelloGoesOutOnlyWithTheClientHelloOverIPv6(t *testing.T) {
	set := newFakingSet(true)
	w, sent := heldHelloWorker(t, &set)
	fake := sock.GetPayload(&set.Faking)
	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	const base = uint32(1000)

	w.ProcessPacket(makeV6TCPPacket(hello, base))
	if got := fakeHelloSeqs(sent(), fake); !slices.Equal(got, []uint32{base}) {
		t.Fatalf("the ClientHello: fake hellos at %v, want one at %d", got, base)
	}
	data := changeCipherSpecAndFinished()
	if v := w.ProcessPacket(makeV6TCPPacket(data, base+uint32(len(hello)))); v != engine.VerdictDrop {
		t.Fatalf("a fragmenting set keeps the later packet, got %v", v)
	}
	pkts := sent()
	if got := fakeHelloSeqs(pkts, fake); len(got) != 0 {
		t.Errorf("finished after the ClientHello: fake hellos at %v, want none", got)
	}
	coversOnce(t, realSegments(pkts, fake), base+uint32(len(hello)), data)
}

func TestASplitClientHelloKeepsAFakeOnEachSegment(t *testing.T) {
	full, seg1, seg2 := splitHello("i.ytimg.com")
	both := []uint32{splitSeq, splitSeq + splitCut}
	for _, c := range []struct {
		name   string
		queued bool
		warm   bool
		want   []uint32
	}{
		{name: "held", queued: true, want: both},
		{name: "learned address", queued: true, warm: true, want: both},
		{name: "no queue to hold in", want: []uint32{splitSeq + splitCut}},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newFakingSet(true)
			w, sent := heldHelloWorker(t, &set)
			fake := sock.GetPayload(&set.Faking)
			if c.warm {
				w.ProcessPacket(makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 100, 1111, 0x18))
				sent()
			}
			q := &verdictLog{}
			deliver := func(id uint32, raw []byte) {
				if c.queued {
					queueTo(w, q, id, raw)
					return
				}
				w.ProcessPacket(raw)
			}
			deliver(1, makeV4TCPPacket(seg1, splitSeq))
			deliver(2, makeV4TCPPacket(seg2, splitSeq+splitCut))
			if got := fakeHelloSeqs(sent(), fake); !slices.Equal(got, c.want) {
				t.Fatalf("split ClientHello: fake hellos at %v, want %v", got, c.want)
			}
			deliver(3, makeV4TCPPacket(changeCipherSpecAndFinished(), splitSeq+uint32(len(full))))
			if got := fakeHelloSeqs(sent(), fake); len(got) != 0 {
				t.Errorf("finished after the split ClientHello: fake hellos at %v, want none", got)
			}
		})
	}
}

func TestPlainHTTPAndOtherPortsKeepTheirFakes(t *testing.T) {
	get := func(path string) []byte {
		return []byte("GET " + path + " HTTP/1.1\r\nHost: example.org\r\nUser-Agent: test-agent\r\n\r\n")
	}
	for _, c := range []struct {
		name          string
		port          uint16
		first, second []byte
	}{
		{"plain HTTP", 80, get("/"), get("/next")},
		{"another protocol", 5222, bytes.Repeat([]byte{0xC3}, 64), bytes.Repeat([]byte{0x3C}, 120)},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newFakingSet(true)
			set.Targets.DomainsToMatch = nil
			set.TCP.DPortFilter = strconv.Itoa(int(c.port))
			cfg := config.NewConfig()
			cfg.Sets = []*config.SetConfig{&set}
			cfg.BuildTCPPortMap()
			cfg.BuildSetPortRanges()
			w := newTestWorker(t, &cfg)
			sent := captureInjected(t, w)
			fake := sock.GetPayload(&set.Faking)
			const base = uint32(5000)
			onPort := func(payload []byte, seq uint32) []byte {
				pkt := makeV4TCPPacket(payload, seq)
				binary.BigEndian.PutUint16(pkt[22:24], c.port)
				return pkt
			}
			w.ProcessPacket(onPort(c.first, base))
			if got := fakeHelloSeqs(sent(), fake); !slices.Equal(got, []uint32{base}) {
				t.Errorf("the first message: fakes at %v, want one at %d", got, base)
			}
			at := base + uint32(len(c.first))
			w.ProcessPacket(onPort(c.second, at))
			if got := fakeHelloSeqs(sent(), fake); !slices.Equal(got, []uint32{at}) {
				t.Errorf("a later message on a connection without a ClientHello: fakes at %v, want one at %d as before", got, at)
			}
		})
	}
}

func helloWithEarlySNI(host string) []byte {
	return helloWithExtensions(
		buildSNIExtension(host),
		buildExtension(21, bytes.Repeat([]byte{0xAB}, 1000)),
		buildExtension(43, []byte{0x02, 0x03, 0x04}),
	)
}

func foreignPieces(segs []sentSegment, base uint32, record, fake []byte) []uint32 {
	var seqs []uint32
	for _, s := range segs {
		off := int(s.seq - base)
		if off >= 0 && off+len(s.payload) <= len(record) && bytes.Equal(record[off:off+len(s.payload)], s.payload) {
			continue
		}
		if bytes.Contains(fake, s.payload) {
			seqs = append(seqs, s.seq)
		}
	}
	slices.Sort(seqs)
	return seqs
}

func TestTheComboDecoyGoesOutOnlyWithTheClientHello(t *testing.T) {
	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	const base = uint32(1000)
	for _, c := range []struct {
		name   string
		packet func([]byte, uint32) []byte
	}{
		{"IPv4", makeV4TCPPacket},
		{"IPv6", makeV6TCPPacket},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newFakingSet(true)
			set.Faking.SNI = false
			set.Fragmentation.Strategy = "combo"
			set.Fragmentation.SeqOverlapBytes = nil
			set.Fragmentation.Combo.DecoyEnabled = true
			set.Fragmentation.Combo.FirstDelayMs = 1
			set.Fragmentation.Combo.FirstDelayMsMax = 0
			w, sent := heldHelloWorker(t, &set)
			fake := sock.GetPayload(&set.Faking)

			w.ProcessPacket(c.packet(hello, base))
			if got := foreignPieces(sentData(sent()), base, hello, fake); !slices.Contains(got, base+2) {
				t.Fatalf("the ClientHello: fake pieces at %v, want the decoy at %d", got, base+2)
			}

			data := tlsAppData(200)
			at := base + uint32(len(hello))
			if v := w.ProcessPacket(c.packet(data, at)); v != engine.VerdictDrop {
				t.Fatalf("application data after the ClientHello: the set keeps the packet, got %v", v)
			}
			segs := sentData(sent())
			if got := foreignPieces(segs, at, data, fake); len(got) != 0 {
				t.Errorf("application data after the ClientHello: pieces of the fake ClientHello at %v, want none", got)
			}
			coversOnce(t, segs, at, data)
		})
	}
}

func TestASplitSecondClientHelloKeepsAFakeOnItsSecondSegment(t *testing.T) {
	set := newFakingSet(true)
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}
	w := newTestWorker(t, &cfg)
	sent := captureInjected(t, w)
	fake := sock.GetPayload(&set.Faking)
	const base = uint32(1000)
	const cut = 600

	first := buildClientHello("i.ytimg.com", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacket(first, base))
	if got := fakeHelloSeqs(sent(), fake); !slices.Equal(got, []uint32{base}) {
		t.Fatalf("the first ClientHello: fake hellos at %v, want one at %d", got, base)
	}

	flight := append([]byte{0x14, 0x03, 0x03, 0x00, 0x01, 0x01}, helloWithEarlySNI("i.ytimg.com")...)
	at := base + uint32(len(first))
	for _, seg := range []struct {
		name    string
		payload []byte
		seq     uint32
	}{
		{"change cipher spec and the start of the second ClientHello", flight[:cut], at},
		{"the rest of the second ClientHello", flight[cut:], at + cut},
	} {
		w.ProcessPacket(makeV4TCPPacket(seg.payload, seg.seq))
		if got := fakeHelloSeqs(sent(), fake); !slices.Equal(got, []uint32{seg.seq}) {
			t.Errorf("%s: fake hellos at %v, want one at %d", seg.name, got, seg.seq)
		}
	}

	w.ProcessPacket(makeV4TCPPacket(tlsAppData(200), at+uint32(len(flight))))
	if got := fakeHelloSeqs(sent(), fake); len(got) != 0 {
		t.Errorf("application data after the second ClientHello: fake hellos at %v, want none", got)
	}
}

func TestAFakeOnlySetLeavesSegmentTwoOfAHelloWithAnEarlySNIAlone(t *testing.T) {
	set := newFakingSet(false)
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}
	w := newTestWorker(t, &cfg)
	sent := captureInjected(t, w)
	fake := sock.GetPayload(&set.Faking)
	hello := helloWithEarlySNI("i.ytimg.com")
	const base = uint32(1000)
	const cut = 600

	if v := w.ProcessPacket(makeV4TCPPacket(hello[:cut], base)); v != engine.VerdictDrop {
		t.Fatalf("segment 1 with the SNI: got %v, want the set to take it", v)
	}
	if got := fakeHelloSeqs(sent(), fake); !slices.Equal(got, []uint32{base}) {
		t.Fatalf("segment 1 with the SNI: fake hellos at %v, want one at %d", got, base)
	}

	if v := w.ProcessPacket(makeV4TCPPacket(hello[cut:], base+cut)); v != engine.VerdictAccept {
		t.Errorf("segment 2 without the SNI: got %v, want it accepted untouched", v)
	}
	if pkts := sent(); len(pkts) != 0 {
		t.Errorf("segment 2 without the SNI: %d packets injected, want none", len(pkts))
	}
}
