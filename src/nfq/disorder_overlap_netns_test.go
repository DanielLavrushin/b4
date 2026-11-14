package nfq

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func netnsDisorderOverlapSet(overlap, position int, shuffle string) config.SetConfig {
	set := netnsStallSet()
	set.Targets.DomainsToMatch = []string{netnsOverlapHost}
	set.Targets.IpsToMatch = []string{netnsHoldServer}
	set.Fragmentation = config.DefaultSetConfig.Fragmentation
	set.Fragmentation.Strategy = "disorder"
	set.Fragmentation.MiddleSNI = false
	set.Fragmentation.Disorder.SplitPosition = position
	set.Fragmentation.SeqOverlapLength = overlap
	set.Fragmentation.SeqOverlapBytes = []byte{0x16, 0x03, 0x03, 0x00, 0x00}
	set.Fragmentation.Disorder.ShuffleMode = shuffle
	return set
}

func netnsDisorderRealStarts(all []netnsWireSegment, port uint16) (start uint32, seqs map[uint32]bool, ok bool) {
	seqs = map[uint32]bool{}
	for _, s := range all {
		if s.sport != port || s.dport != netnsHoldPort {
			continue
		}
		if !ok {
			start, ok = s.seq+1, true
		}
		if s.payload > 0 && s.ttl >= 30 {
			seqs[s.seq] = true
		}
	}
	return start, seqs, ok
}

func TestNetnsADisorderOverlapLeavesTheServersBytesAlone(t *testing.T) {
	record := func(fill byte, length int) []byte {
		return append([]byte{0x17, 0x03, 0x03, byte(length >> 8), byte(length)}, bytes.Repeat([]byte{fill}, length)...)
	}
	for _, v := range []struct{ overlap, position int }{{128, 256}, {256, 512}} {
		for _, shuffle := range []string{"reverse", "full"} {
			for _, layout := range []struct {
				name string
				pad  int
			}{
				{"a ClientHello in one segment", 420},
				{"a ClientHello in two segments", 1000},
			} {
				t.Run(fmt.Sprintf("overlap %d at %d, %s, %s", v.overlap, v.position, shuffle, layout.name), func(t *testing.T) {
					netnsHoldRequire(t)
					srv := netnsHoldStartPeer(t)
					hello := buildClientHello(netnsOverlapHost, layout.pad, 0xAB)
					if first := min(len(hello), netnsOverlapSeg1); first <= v.position {
						t.Fatalf("the first segment holds %d bytes, not more than the position %d, so this test proves nothing", first, v.position)
					}
					short, long := record(0x41, 25), record(0x42, v.position+140)
					reads := netnsOverlapServerIn(t, srv, len(hello), len(short)+len(long))
					wire := netnsHoldCapture(t, srv)
					set := netnsDisorderOverlapSet(v.overlap, v.position, shuffle)
					netnsOverlapStart(t, srv, &set)

					var ports []uint16
					for range netnsOverlapConns {
						ports = append(ports, netnsOverlapExchange(t, reads, hello, short, long))
					}
					time.Sleep(50 * time.Millisecond)

					seen := wire()
					for _, port := range ports {
						start, seqs, ok := netnsDisorderRealStarts(seen, port)
						if !ok {
							t.Fatalf("nothing of the connection from port %d on the wire", port)
						}
						if !seqs[start+uint32(v.position)-uint32(v.overlap)] {
							t.Fatalf("no piece starts %d bytes before byte %d of the ClientHello in %v: the overlap never left", v.overlap, v.position, seqs)
						}
						later := start + uint32(len(hello)+len(short))
						if !seqs[later+uint32(v.position)-uint32(v.overlap)] {
							t.Fatalf("no piece starts %d bytes before byte %d of the record after the ClientHello in %v, so this test never put an overlap on a later packet", v.overlap, v.position, seqs)
						}
					}
				})
			}
		}
	}
}
