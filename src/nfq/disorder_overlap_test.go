package nfq

import (
	"bytes"
	"fmt"
	"net"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const disorderTestSeq = uint32(1_000_000)

var disorderTestPattern = []byte{0x16, 0x03, 0x03, 0x00, 0x00}

type injectFamily struct {
	name     string
	dst      net.IP
	packet   func([]byte, uint32) []byte
	parse    func([]byte) (PacketInfo, bool)
	ttlAt    int
	disorder func(*Worker, *config.SetConfig, []byte, net.IP)
	inject   func(*Worker, *config.SetConfig, []byte, net.IP, bool)
}

func injectFamilies() []injectFamily {
	return []injectFamily{
		{"IPv4", net.IPv4(1, 2, 3, 4), makeV4TCPPacket, ExtractPacketInfoV4, 8, (*Worker).sendDisorderFragments, (*Worker).dropAndInjectTCP},
		{"IPv6", net.ParseIP(v6TestDst), makeV6TCPPacket, ExtractPacketInfoV6, 7, (*Worker).sendDisorderFragmentsV6, (*Worker).dropAndInjectTCPv6},
	}
}

type sentPiece struct {
	offset int
	data   []byte
}

func realPieces(t *testing.T, family injectFamily, sent [][]byte) []sentPiece {
	t.Helper()
	var pieces []sentPiece
	for _, p := range sent {
		pi, ok := family.parse(p)
		if !ok {
			t.Fatalf("an injected packet does not parse: %x", p)
		}
		if p[family.ttlAt] == 64 && pi.PayloadLen > 0 {
			pieces = append(pieces, sentPiece{int(int32(pi.Seq0 - disorderTestSeq)), bytes.Clone(pi.Payload)})
		}
	}
	slices.SortFunc(pieces, func(a, b sentPiece) int { return a.offset - b.offset })
	return pieces
}

func pieceOffsets(pieces []sentPiece) []int {
	offsets := make([]int, len(pieces))
	for i, p := range pieces {
		offsets[i] = p.offset
	}
	return offsets
}

func disorderOverlapSet(overlap, position int) config.SetConfig {
	set := config.NewSetConfig()
	set.Faking.SNI = false
	set.Fragmentation.Strategy = "disorder"
	set.Fragmentation.MiddleSNI = false
	set.Fragmentation.SeqOverlapLength = overlap
	set.Fragmentation.SeqOverlapBytes = disorderTestPattern
	set.Fragmentation.Disorder.SplitPosition = position
	set.Fragmentation.Disorder.ShuffleMode = "full"
	set.Fragmentation.Disorder.MinJitterUs = 1
	set.Fragmentation.Disorder.MaxJitterUs = 2
	return set
}

func disorderPieces(t *testing.T, family injectFamily, set *config.SetConfig, payload []byte) []sentPiece {
	t.Helper()
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)
	sent := captureInjected(t, w)
	family.disorder(w, set, family.packet(payload, disorderTestSeq), family.dst)
	return realPieces(t, family, sent())
}

func repeatedPattern(pattern []byte, length int) []byte {
	return bytes.Repeat(pattern, length/len(pattern)+1)[:length:length]
}

func rebuiltFrom(pieces []sentPiece) []byte {
	var joined []byte
	for _, p := range pieces {
		joined = append(joined, p.data[max(0, len(joined)-p.offset):]...)
	}
	return joined
}

func TestDisorderPutsTheOverlapOnThePieceThatStartsAtTheSplitPosition(t *testing.T) {
	hello := buildClientHello("overlap.example.org", 600, 0xAB)
	for _, v := range []struct{ overlap, position int }{{128, 256}, {256, 512}, {1, 2}, {255, 256}} {
		for _, family := range injectFamilies() {
			t.Run(fmt.Sprintf("overlap %d at %d over %s", v.overlap, v.position, family.name), func(t *testing.T) {
				set := disorderOverlapSet(v.overlap, v.position)
				for range 4 {
					pieces := disorderPieces(t, family, &set, hello)
					if got, want := pieceOffsets(pieces), []int{0, v.position - v.overlap}; !slices.Equal(got, want) {
						t.Fatalf("overlap %d with the split position %d: the pieces start at %v, want %v", v.overlap, v.position, got, want)
					}
					if !bytes.Equal(pieces[0].data, hello[:v.position]) {
						t.Fatalf("the first piece carries other bytes than the packet's first %d", v.position)
					}
					want := append(repeatedPattern(disorderTestPattern, v.overlap), hello[v.position:]...)
					if !bytes.Equal(pieces[1].data, want) {
						t.Fatalf("the second piece is not %d pattern bytes and then the packet from byte %d on:\n got %x\nwant %x", v.overlap, v.position, pieces[1].data, want)
					}
				}
			})
		}
	}
}

func TestDisorderCutsOnceAtTheSplitPosition(t *testing.T) {
	const position = 256
	hello := buildClientHello("overlap.example.org", 600, 0xAB)
	for _, c := range []struct {
		name    string
		overlap int
		pattern []byte
	}{
		{"no overlap length", 0, disorderTestPattern},
		{"no pattern", 128, nil},
		{"an overlap as long as the first piece", position, disorderTestPattern},
		{"an overlap longer than the first piece", position + 100, disorderTestPattern},
	} {
		for _, family := range injectFamilies() {
			t.Run(c.name+" over "+family.name, func(t *testing.T) {
				set := disorderOverlapSet(c.overlap, position)
				set.Fragmentation.SeqOverlapBytes = c.pattern
				pieces := disorderPieces(t, family, &set, hello)
				if got, want := pieceOffsets(pieces), []int{0, position}; !slices.Equal(got, want) {
					t.Fatalf("the pieces start at %v, want %v: one cut and no overlap", got, want)
				}
				if !bytes.Equal(rebuiltFrom(pieces), hello) {
					t.Fatal("the pieces rebuild other bytes than the packet held")
				}
			})
		}
	}
}

func TestDisorderKeepsItsOwnCutsWithoutASplitPosition(t *testing.T) {
	long := buildClientHello("overlap.example.org", 600, 0xAB)
	short := buildClientHello("overlap.example.org", 20, 0xAB)
	nameStart, nameEnd, ok := locateSNI(long)
	if !ok || nameStart <= 128 {
		t.Fatalf("the long ClientHello's name starts at %d (found %v), want it past the overlap", nameStart, ok)
	}
	nameMiddle := nameStart + (nameEnd-nameStart)/2
	builtIn := func(payload []byte) []int {
		return []int{0, 1, len(payload) / 2, len(payload) * 3 / 4}
	}
	for _, c := range []struct {
		name    string
		payload []byte
		change  func(*config.SetConfig)
		want    []int
	}{
		{"no split position", long, func(s *config.SetConfig) { s.Fragmentation.Disorder.SplitPosition = 0 }, builtIn(long)},
		{"a negative split position", long, func(s *config.SetConfig) { s.Fragmentation.Disorder.SplitPosition = -5 }, builtIn(long)},
		{"a set saved before the split position existed", long, func(s *config.SetConfig) {
			s.Fragmentation.Disorder.SplitPosition = 0
			s.Fragmentation.SNIPosition = 256
		}, builtIn(long)},
		{"a packet shorter than the split position", short, func(*config.SetConfig) {}, builtIn(short)},
		{"SNI-based splitting", long, func(s *config.SetConfig) { s.Fragmentation.MiddleSNI = true }, []int{0, nameStart - 128, nameMiddle, nameEnd}},
	} {
		for _, family := range injectFamilies() {
			t.Run(c.name+" over "+family.name, func(t *testing.T) {
				set := disorderOverlapSet(128, 256)
				c.change(&set)
				pieces := disorderPieces(t, family, &set, c.payload)
				if got := pieceOffsets(pieces); !slices.Equal(got, c.want) {
					t.Fatalf("the pieces start at %v, want %v", got, c.want)
				}
				if !bytes.Equal(rebuiltFrom(pieces), c.payload) {
					t.Fatal("the pieces rebuild other bytes than the packet held")
				}
			})
		}
	}
}

func TestDisorderWithSNISplittingCutsAPacketWithoutANameAsBefore(t *testing.T) {
	rest := bytes.Repeat([]byte{0xCD}, 700)
	for _, family := range injectFamilies() {
		t.Run(family.name, func(t *testing.T) {
			set := disorderOverlapSet(128, 256)
			set.Fragmentation.MiddleSNI = true
			pieces := disorderPieces(t, family, &set, rest)
			if got, want := pieceOffsets(pieces), []int{0, 1, len(rest) / 2, len(rest) * 3 / 4}; !slices.Equal(got, want) {
				t.Fatalf("the pieces start at %v, want %v: the split position waits for SNI-based splitting to be off", got, want)
			}
		})
	}
}
