package nfq

import "testing"

func TestTheHelloRangeHoldsAcrossTheWholeSequenceSpace(t *testing.T) {
	for _, c := range []struct {
		name      string
		start     uint32
		length    uint32
		seq       uint32
		pastHello bool
	}{
		{"the first byte of the ClientHello", 1000, 500, 1000, false},
		{"its last byte", 1000, 500, 1499, false},
		{"the first byte after it", 1000, 500, 1500, true},
		{"2 GiB after it", 1000, 500, 1500 + 1<<31, true},
		{"the last byte before it comes round again", 1000, 500, 999, true},
		{"inside a ClientHello across the wrap", 0xFFFFFF00, 0x200, 0xFFFFFFF0, false},
		{"after the wrap, still inside it", 0xFFFFFF00, 0x200, 0x50, false},
		{"after a ClientHello across the wrap", 0xFFFFFF00, 0x200, 0x100, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			cache := &tlsInfoCache{conns: make(map[string]*tlsInfo)}
			cache.Store("conn", "i.ytimg.com", 0x0303, c.start, 1, c.length)
			if got := cache.PastHello("conn", c.seq); got != c.pastHello {
				t.Fatalf("PastHello(%#x) for a ClientHello at %#x+%d = %v, want %v", c.seq, c.start, c.length, got, c.pastHello)
			}
		})
	}
}
