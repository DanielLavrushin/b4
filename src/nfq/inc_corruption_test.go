package nfq

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"
)

func TestIncomingBadChecksumFakesNeverCarryAValidChecksum(t *testing.T) {
	const iterations = 2_000_000
	w := &Worker{}
	for _, ver := range []byte{IPv4, IPv6} {
		for _, strategy := range []string{"badsum", "all"} {
			t.Run(fmt.Sprintf("v%d/%s", ver, strategy), func(t *testing.T) {
				l3 := checksumHoleL3(ver)
				base := checksumHoleTCPPacket(ver, 65535, nil)
				base[l3+13] = 0x10
				base[l3+16], base[l3+17] = 0, 0
				fake := make([]byte, len(base))
				rng := rand.New(rand.NewSource(int64(ver)))
				valid := 0
				for i := 0; i < iterations; i++ {
					copy(fake, base)
					binary.BigEndian.PutUint32(fake[l3+4:l3+8], rng.Uint32())
					if ver == IPv4 {
						w.applyCorruption(fake, l3, strategy)
					} else {
						w.applyCorruptionV6(fake, strategy)
					}
					if rfc1071TCPChecksumValid(fake) {
						valid++
					}
				}
				if valid != 0 {
					t.Fatalf("%d of %d incoming %s fakes went out with a valid TCP checksum", valid, iterations, strategy)
				}
			})
		}
	}
}
