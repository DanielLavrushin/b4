package sock

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func captureFakeSet(size int) *config.SetConfig {
	set := config.NewSetConfig()
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadData = bytes.Repeat([]byte{0x16}, size)
	set.Faking.Strategy = "tcp_check"
	set.Faking.ApplyTTL = false
	set.Faking.TLSMod = nil
	set.Faking.FakeLenMode = ""
	return &set
}

func TestAFakeTooLongForItsLengthFieldIsNotBuilt(t *testing.T) {
	cases := []struct {
		v6   bool
		size int
		fits bool
	}{
		{false, 60000, true},
		{false, 65483, true},
		{false, 65484, false},
		{false, 65490, false},
		{false, 65503, false},
		{false, 70000, false},
		{true, 65515, true},
		{true, 65516, false},
		{true, 65520, false},
		{true, 70000, false},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s/payload=%d", familyName(c.v6), c.size), func(t *testing.T) {
			original, _ := helloPacket(t, c.v6)
			var fake []byte
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("building the TCP fake panicked: %v", r)
					}
				}()
				if c.v6 {
					fake = BuildFakeSNIPacketV6(original, captureFakeSet(c.size))
				} else {
					fake = BuildFakeSNIPacketV4(original, captureFakeSet(c.size))
				}
			}()
			if fake == nil {
				if c.fits {
					t.Fatal("no fake built, though the packet fits its length field")
				}
				return
			}
			if c.v6 {
				if got := int(binary.BigEndian.Uint16(fake[4:6])); got != len(fake)-40 {
					t.Fatalf("IPv6 payload length %d for a %d-byte packet", got, len(fake))
				}
			} else if got := int(binary.BigEndian.Uint16(fake[2:4])); got != len(fake) {
				t.Fatalf("IPv4 total length %d for a %d-byte packet", got, len(fake))
			}
		})
	}
}
