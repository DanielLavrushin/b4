package sock

import (
	"bytes"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/stun"
)

func TestBuildSTUNRequestIsRecognisedAsSTUN(t *testing.T) {
	for _, size := range []int{20, 24, 64, 100, 1200, 65552} {
		pkt := BuildSTUNRequest(size)
		if len(pkt) != size {
			t.Fatalf("size %d: built %d bytes", size, len(pkt))
		}
		if !stun.IsSTUNMessage(pkt) {
			t.Fatalf("size %d: b4's own detector does not recognise the fake", size)
		}
	}
}

func TestBuildSTUNRequestRefusesALengthItCannotEncode(t *testing.T) {
	for _, size := range []int{0, 19, 21, 23, 65, 101, 65556, 65560} {
		if pkt := BuildSTUNRequest(size); pkt != nil {
			t.Fatalf("size %d: built %d bytes, want nothing", size, len(pkt))
		}
	}
}

func TestEveryNormalisedSTUNFakeLengthBuildsASendablePacket(t *testing.T) {
	for _, size := range []int{0, 21, 101, 65504, 65507, 65556, 1 << 20} {
		n := config.NormalizeSTUNFakeLen(size)
		pkt := BuildSTUNRequest(n)
		if pkt == nil || !stun.IsSTUNMessage(pkt) {
			t.Fatalf("size %d normalised to %d builds no STUN request", size, n)
		}
		if 20+8+len(pkt) > 0xffff {
			t.Fatalf("size %d normalised to %d does not fit an IPv4 UDP datagram", size, n)
		}
	}
}

func TestEverySTUNRequestCarriesItsOwnTransactionID(t *testing.T) {
	first := BuildSTUNRequest(64)
	second := BuildSTUNRequest(64)
	if first == nil || second == nil {
		t.Fatal("the generator returned nothing")
	}
	if bytes.Equal(first[8:20], second[8:20]) {
		t.Fatal("two fakes share a transaction ID, so every device would send the same bytes")
	}
}
