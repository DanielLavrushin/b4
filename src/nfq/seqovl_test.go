package nfq

import (
	"bytes"
	"encoding/binary"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
)

type comboFamily struct {
	name   string
	dst    net.IP
	packet func([]byte, uint32) []byte
	parse  func([]byte) (PacketInfo, bool)
	ttlAt  int
	combo  func(*Worker, *config.SetConfig, []byte, net.IP, bool)
}

func comboFamilies() []comboFamily {
	return []comboFamily{
		{"IPv4", net.IPv4(1, 2, 3, 4), makeV4TCPPacket, ExtractPacketInfoV4, 8, (*Worker).sendComboFragments},
		{"IPv6", net.ParseIP(v6TestDst), makeV6TCPPacket, ExtractPacketInfoV6, 7, (*Worker).sendComboFragmentsV6},
	}
}

func clientSplitHello(firstRecord int) []byte {
	hello := buildClientHello("overlap.example.org", 200, 0xAB)
	record := func(body []byte) []byte {
		return append([]byte{0x16, 0x03, 0x01, byte(len(body) >> 8), byte(len(body))}, body...)
	}
	return append(record(hello[5:5+firstRecord]), record(hello[5+firstRecord:])...)
}

func overlapTestSet(overlap int, pattern []byte) config.SetConfig {
	set := config.NewSetConfig()
	set.Faking.SNI = false
	set.Fragmentation.Strategy = "combo"
	set.Fragmentation.MiddleSNI = true
	set.Fragmentation.Combo.FirstByteSplit = true
	set.Fragmentation.Combo.ExtensionSplit = true
	set.Fragmentation.Combo.ShuffleMode = "full"
	set.Fragmentation.Combo.FirstDelayMs = 1
	set.Fragmentation.Combo.JitterMaxUs = 1
	set.Fragmentation.SeqOverlapLength = overlap
	set.Fragmentation.SeqOverlapBytes = pattern
	return set
}

func comboSyn(t *testing.T, packet func([]byte, uint32) []byte, parse func([]byte) (PacketInfo, bool), isn uint32) ([]byte, int) {
	t.Helper()
	syn := packet(nil, isn)
	pi, ok := parse(syn)
	if !ok {
		t.Fatalf("the SYN does not parse: %x", syn)
	}
	syn[pi.IPHdrLen+13] = 0x02
	return syn, pi.IPHdrLen
}

func openComboStream(t *testing.T, w *Worker, packet func([]byte, uint32) []byte, parse func([]byte) (PacketInfo, bool), start uint32) {
	t.Helper()
	syn, tcpAt := comboSyn(t, packet, parse, start-1)
	w.streams.Open(syn, tcpAt, time.Now())
}

func TestComboKeepsTheOverlapOnARequestWithMethodEOL(t *testing.T) {
	const seq, overlap, realTTL = uint32(1_000_000), 64, 64
	request := []byte("GET / HTTP/1.1\r\nHost: overlap.example.org\r\nUser-Agent: curl/8.5.0-test\r\n\r\n")
	for _, tc := range []struct {
		name   string
		dst    net.IP
		packet func([]byte, uint32) []byte
		parse  func([]byte) (PacketInfo, bool)
		ttlAt  int
		inject func(*Worker, *config.SetConfig, []byte, net.IP, bool)
	}{
		{"IPv4", net.IPv4(1, 2, 3, 4), makeV4TCPPacket, ExtractPacketInfoV4, 8, (*Worker).dropAndInjectTCP},
		{"IPv6", net.ParseIP(v6TestDst), makeV6TCPPacket, ExtractPacketInfoV6, 7, (*Worker).dropAndInjectTCPv6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.NewConfig()
			w := newTestWorker(t, &cfg)
			sent := captureInjected(t, w)
			set := overlapTestSet(overlap, []byte{0x16, 0x03, 0x03, 0x00, 0x00})
			set.TCP.HTTPMethodEOL = true
			openComboStream(t, w, tc.packet, tc.parse, seq)
			tc.inject(w, &set, tc.packet(request, seq), tc.dst, true)

			lowest := seq
			var joined []byte
			for _, p := range sent() {
				pi, ok := tc.parse(p)
				if !ok {
					t.Fatalf("an injected packet does not parse: %x", p)
				}
				if p[tc.ttlAt] != realTTL {
					continue
				}
				joined = append(joined, pi.Payload...)
				if pi.Seq0 < lowest {
					lowest = pi.Seq0
				}
			}
			if !bytes.Contains(joined, methodEOL) {
				t.Fatal("HTTP Method EOL did not run, so this test proves nothing")
			}
			if lowest != seq-overlap {
				t.Fatalf("the lowest piece starts at %d, want %d: the request lost its overlap once HTTP Method EOL rewrote it", lowest, seq-overlap)
			}
		})
	}
}

func TestComboOverlapsOnlyAPacketThatStartsAMessage(t *testing.T) {
	const seq, overlap, realTTL = uint32(1_000_000), 64, 64
	pattern := []byte{0x16, 0x03, 0x03, 0x00, 0x00}
	continuation := bytes.Repeat([]byte{0xCD}, 400)
	for _, tc := range []struct {
		name     string
		payload  []byte
		overlaps bool
	}{
		{"a ClientHello", buildClientHello("overlap.example.org", 200, 0xAB), true},
		{"a ClientHello the client split after 1 byte", clientSplitHello(1), true},
		{"a ClientHello the client split after 4 bytes", clientSplitHello(4), true},
		{"a ClientHello the client split after 5 bytes", clientSplitHello(5), true},
		{"an HTTP request", []byte("GET / HTTP/1.1\r\nHost: overlap.example.org\r\n\r\n"), true},
		{"the rest of a ClientHello", continuation, false},
		{"the rest of a ClientHello that starts with the handshake type", append([]byte{0x16}, continuation...), false},
		{"application data", append([]byte{0x17, 0x03, 0x03, 0x01, 0x90}, continuation...), false},
		{"a later handshake record", append([]byte{0x16, 0x03, 0x03, 0x01, 0x90, 0x10}, continuation...), false},
	} {
		for _, family := range comboFamilies() {
			t.Run(tc.name+" over "+family.name, func(t *testing.T) {
				cfg := config.NewConfig()
				w := newTestWorker(t, &cfg)
				sent := captureInjected(t, w)
				set := overlapTestSet(overlap, pattern)
				openComboStream(t, w, family.packet, family.parse, seq)
				family.combo(w, &set, family.packet(tc.payload, seq), family.dst, true)

				type piece struct {
					seq  uint32
					data []byte
				}
				var pieces []piece
				for _, p := range sent() {
					pi, ok := family.parse(p)
					if !ok {
						t.Fatalf("an injected packet does not parse: %x", p)
					}
					if p[family.ttlAt] == realTTL {
						pieces = append(pieces, piece{pi.Seq0, pi.Payload})
					}
				}
				slices.SortFunc(pieces, func(a, b piece) int { return int(int64(a.seq) - int64(b.seq)) })
				if len(pieces) < 2 {
					t.Fatalf("combo sent %d pieces, so this test proves nothing", len(pieces))
				}
				want := tc.payload
				if tc.overlaps {
					if pieces[0].seq != seq-overlap {
						t.Fatalf("the first piece starts at %d, want %d: a packet that starts a message keeps its overlap", pieces[0].seq, seq-overlap)
					}
					if !bytes.HasPrefix(pieces[0].data, pattern) {
						t.Fatalf("the overlap carries %x, want the pattern %x", pieces[0].data[:len(pattern)], pattern)
					}
					want = append(bytes.Repeat(pattern, overlap/len(pattern)+1)[:overlap:overlap], tc.payload...)
				} else if pieces[0].seq != seq {
					t.Fatalf("the first piece starts at %d, before the packet's own %d, and covers bytes the server has not read yet", pieces[0].seq, seq)
				}
				var joined []byte
				for _, p := range pieces {
					joined = append(joined, p.data...)
				}
				if !bytes.Equal(joined, want) {
					t.Fatalf("the pieces rebuild other bytes than the packet held:\n got %x\nwant %x", joined, want)
				}
			})
		}
	}
}

func comboRealPieceStarts(t *testing.T, family comboFamily, sent [][]byte, realTTL byte) []uint32 {
	t.Helper()
	var starts []uint32
	for _, p := range sent {
		pi, ok := family.parse(p)
		if !ok {
			t.Fatalf("an injected packet does not parse: %x", p)
		}
		if p[family.ttlAt] == realTTL && pi.PayloadLen > 0 {
			starts = append(starts, pi.Seq0)
		}
	}
	slices.SortFunc(starts, func(a, b uint32) int { return int(int32(a - b)) })
	return starts
}

func TestComboOverlapsOnlyTheFirstMessageOfAConnection(t *testing.T) {
	const isn, overlap, realTTL = uint32(5_000_000), 64, 64
	pattern := []byte{0x16, 0x03, 0x03, 0x00, 0x00}
	first := []byte("GET /one HTTP/1.1\r\nHost: overlap.example.org\r\nUser-Agent: curl/8.5.0-test\r\n\r\n")
	second := []byte("GET /two HTTP/1.1\r\nHost: overlap.example.org\r\nUser-Agent: curl/8.5.0-test\r\n\r\n")
	hello := buildClientHello("overlap.example.org", 200, 0xAB)
	for _, family := range comboFamilies() {
		for _, c := range []struct {
			name          string
			first, second []byte
			syn           bool
		}{
			{"a second HTTP request", first, second, true},
			{"a ClientHello after CONNECT", []byte("CONNECT overlap.example.org:443 HTTP/1.1\r\nHost: overlap.example.org:443\r\n\r\n"), hello, true},
			{"a connection whose SYN b4 never saw", first, second, false},
		} {
			t.Run(c.name+" over "+family.name, func(t *testing.T) {
				set := overlapTestSet(overlap, pattern)
				set.Id, set.Name, set.Enabled = "overlap", "Overlap", true
				set.Targets.IpsToMatch = []string{"1.2.3.4", "2001:db8::2"}
				w := newDualStackInjectingWorker(t, &set)
				sent := captureInjected(t, w)

				if c.syn {
					syn, _ := comboSyn(t, family.packet, family.parse, isn)
					if v := w.ProcessPacket(syn); v != engine.VerdictAccept {
						t.Fatalf("the SYN goes out as the client sent it, got %v", v)
					}
				}
				send := func(payload []byte, seq uint32) []uint32 {
					t.Helper()
					if v := w.ProcessPacket(family.packet(payload, seq)); v != engine.VerdictDrop {
						t.Fatalf("combo takes the packet over, got %v", v)
					}
					starts := comboRealPieceStarts(t, family, sent(), realTTL)
					if len(starts) < 2 {
						t.Fatalf("combo sent %d pieces, so this test proves nothing", len(starts))
					}
					return starts
				}

				start, next := isn+1, isn+1+uint32(len(c.first))
				firstPieces := send(c.first, start)
				want := start
				if c.syn {
					want = start - overlap
				}
				if firstPieces[0] != want {
					t.Fatalf("the first message starts at %d, want %d", firstPieces[0], want)
				}
				if c.syn {
					if resent := send(c.first, start); resent[0] != start-overlap {
						t.Fatalf("a resend of the first message starts at %d, want %d: it keeps the overlap", resent[0], start-overlap)
					}
				}
				if secondPieces := send(c.second, next); secondPieces[0] != next {
					t.Fatalf("the second message starts at %d, before its own %d, over bytes of the first that may still be on their way", secondPieces[0], next)
				}
			})
		}
	}
}

func makeV4TCPPacket(payload []byte, seq uint32) []byte {
	const ipHL = 20
	const tcpHL = 20
	pkt := make([]byte, ipHL+tcpHL+len(payload))

	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	binary.BigEndian.PutUint16(pkt[4:6], 0x1234)
	pkt[8] = 64
	pkt[9] = 6
	copy(pkt[12:16], []byte{10, 0, 0, 1})
	copy(pkt[16:20], []byte{1, 2, 3, 4})

	binary.BigEndian.PutUint16(pkt[ipHL:ipHL+2], 12345)
	binary.BigEndian.PutUint16(pkt[ipHL+2:ipHL+4], 443)
	binary.BigEndian.PutUint32(pkt[ipHL+4:ipHL+8], seq)
	pkt[ipHL+12] = byte(tcpHL/4) << 4
	pkt[ipHL+13] = 0x18

	copy(pkt[ipHL+tcpHL:], payload)
	return pkt
}

func TestBuildSeqOverlapSegmentV4_PrependsPatternAndShiftsSeq(t *testing.T) {
	origPayload := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22}
	startOff := 100
	pattern := []byte{0x16, 0x03, 0x03, 0x00, 0x00}
	seqovlLen := 7
	const baseSeq = uint32(1000)

	pkt := makeV4TCPPacket(origPayload, baseSeq)
	pi, ok := ExtractPacketInfoV4(pkt)
	if !ok {
		t.Fatal("ExtractPacketInfoV4 failed")
	}

	out := BuildSeqOverlapSegmentV4(pkt, pi, origPayload, startOff, seqovlLen, pattern, 0)

	wantLen := pi.PayloadStart + seqovlLen + len(origPayload)
	if len(out) != wantLen {
		t.Fatalf("segment length: want %d, got %d", wantLen, len(out))
	}

	gotIPLen := binary.BigEndian.Uint16(out[2:4])
	if int(gotIPLen) != wantLen {
		t.Fatalf("IP total length: want %d, got %d", wantLen, gotIPLen)
	}

	gotSeq := binary.BigEndian.Uint32(out[pi.IPHdrLen+4 : pi.IPHdrLen+8])
	wantSeq := pi.Seq0 + uint32(startOff) - uint32(seqovlLen)
	if gotSeq != wantSeq {
		t.Fatalf("seq: want %d, got %d", wantSeq, gotSeq)
	}

	body := out[pi.PayloadStart:]
	for i := 0; i < seqovlLen; i++ {
		if body[i] != pattern[i%len(pattern)] {
			t.Fatalf("prefix byte %d: want 0x%02X, got 0x%02X", i, pattern[i%len(pattern)], body[i])
		}
	}
	for i, b := range origPayload {
		if body[seqovlLen+i] != b {
			t.Fatalf("payload byte %d: want 0x%02X, got 0x%02X", i, b, body[seqovlLen+i])
		}
	}
}

func TestBuildSeqOverlapSegmentV4_ZeroLengthFallsBackToPlainSegment(t *testing.T) {
	origPayload := []byte{0xAA, 0xBB, 0xCC}
	pkt := makeV4TCPPacket(origPayload, 1000)
	pi, _ := ExtractPacketInfoV4(pkt)

	out := BuildSeqOverlapSegmentV4(pkt, pi, origPayload, 0, 0, []byte{0xFF}, 0)

	if len(out) != pi.PayloadStart+len(origPayload) {
		t.Fatalf("zero-length seqovl should not extend payload (got %d, want %d)",
			len(out), pi.PayloadStart+len(origPayload))
	}
	gotSeq := binary.BigEndian.Uint32(out[pi.IPHdrLen+4 : pi.IPHdrLen+8])
	if gotSeq != pi.Seq0 {
		t.Fatalf("zero-length seqovl should not shift seq (got %d, want %d)", gotSeq, pi.Seq0)
	}
}

func TestBuildSeqOverlapSegmentV4_EmptyPatternFallsBackToPlainSegment(t *testing.T) {
	origPayload := []byte{0xAA, 0xBB, 0xCC}
	pkt := makeV4TCPPacket(origPayload, 1000)
	pi, _ := ExtractPacketInfoV4(pkt)

	out := BuildSeqOverlapSegmentV4(pkt, pi, origPayload, 0, 5, nil, 0)

	if len(out) != pi.PayloadStart+len(origPayload) {
		t.Fatalf("empty pattern should not extend payload (got %d, want %d)",
			len(out), pi.PayloadStart+len(origPayload))
	}
}
