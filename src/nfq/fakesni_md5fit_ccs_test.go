package nfq

import (
	"fmt"
	"testing"
)

func TestAnMD5FakeForASecondClientHelloBehindAChangeCipherSpecStillFitsItsSegment(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		for _, h := range md5FitHellos() {
			second := append(append([]byte(nil), changeCipherSpecRecord...), h.hello...)
			original := md5FitFullSegmentOriginal(ver, nil, nil, second)
			for _, set := range md5FitSets() {
				t.Run(fmt.Sprintf("v%d/%s/%s", ver, h.name, set.Name), func(t *testing.T) {
					w := newDualStackInjectingWorker(t, &set)
					sent := captureInjected(t, w)
					sendFakeSNIFor(w, &set, original)
					got := sent()
					if len(got) != 1 {
						t.Fatalf("want one fake, sent %d packets", len(got))
					}
					fake := got[0]
					if len(fake) > len(original) {
						t.Fatalf("a %d-byte fake for a %d-byte real segment that fills the path MTU: the interface cannot carry it", len(fake), len(original))
					}
					if set.Faking.Strategy == "md5sum" {
						if lengths := md5OptionLengths(fake); len(lengths) != 1 || lengths[0] != 18 {
							t.Fatalf("an MD5 Sum fake must still carry its option, got lengths %v", lengths)
						}
						md5FitHeadersConsistent(t, fake)
						return
					}
					if lengths := md5OptionLengths(fake); len(lengths) != 0 {
						t.Fatalf("md5_on_fake: the option has no room in the packet size, yet the fake carries it (%v)", lengths)
					}
				})
			}
		}
	}
}
