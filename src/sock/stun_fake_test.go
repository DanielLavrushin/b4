package sock

import (
	"bytes"
	"testing"

	"github.com/daniellavrushin/b4/stun"
)

func TestBuildSTUNRequestIsRecognisedAsSTUN(t *testing.T) {
	for _, size := range []int{20, 24, 64, 100, 1200} {
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
	for _, size := range []int{0, 19, 21, 23, 65, 101} {
		if pkt := BuildSTUNRequest(size); pkt != nil {
			t.Fatalf("size %d: built %d bytes, want nothing", size, len(pkt))
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
