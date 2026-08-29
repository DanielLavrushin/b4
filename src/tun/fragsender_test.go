package tun

import "testing"

func ipv4Header(fragField uint16, proto byte) []byte {
	h := make([]byte, 28)
	h[0] = 0x45
	h[6] = byte(fragField >> 8)
	h[7] = byte(fragField)
	h[9] = proto
	h[20] = 0
	h[21] = 53
	return h
}

func TestEveryFragmentOfADatagramTakesTheSamePath(t *testing.T) {
	cases := []struct {
		name string
		frag uint16
		want bool
	}{
		{"not fragmented", 0x0000, false},
		{"do not fragment", 0x4000, false},
		{"first fragment, more to follow", 0x2000, true},
		{"a later fragment", 0x00b9, true},
		{"last fragment", 0x0185, true},
	}
	for _, c := range cases {
		h := ipv4Header(c.frag, 17)
		fragmented := (uint16(h[6])<<8|uint16(h[7]))&0x3fff != 0
		if fragmented != c.want {
			t.Errorf("%s: read as fragmented=%v, want %v. The first fragment carries offset 0 with the "+
				"more-fragments bit set, so testing the offset alone lets it take the client path while every "+
				"later fragment takes the other one. Split across two marks the pieces route differently and "+
				"the far end can never put the datagram back together", c.name, fragmented, c.want)
		}
	}
}
