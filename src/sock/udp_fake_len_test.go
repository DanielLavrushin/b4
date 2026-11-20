package sock

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

func buildUDPFakeWithoutPanic(t *testing.T, v6 bool, fakeLen int, payload []byte) (fake []byte, ok bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("fake_len=%d v6=%v: building the UDP fake panicked: %v", fakeLen, v6, r)
			fake, ok = nil, false
		}
	}()
	if v6 {
		return BuildFakeUDPFromOriginalV6(buildMinimalIPv6UDPPacket(20), fakeLen, 5, payload)
	}
	return BuildFakeUDPFromOriginalV4(buildMinimalIPv4UDPPacket(20), fakeLen, 5, payload)
}

func TestAUDPFakeWithANegativeLengthIsNotBuilt(t *testing.T) {
	for _, fakeLen := range []int{-1, -100} {
		for _, v6 := range []bool{false, true} {
			for _, payload := range [][]byte{nil, bytes.Repeat([]byte{0x40}, 64)} {
				t.Run(fmt.Sprintf("fake_len=%d/v6=%v/payload=%d", fakeLen, v6, len(payload)), func(t *testing.T) {
					fake, ok := buildUDPFakeWithoutPanic(t, v6, fakeLen, payload)
					if ok || fake != nil {
						t.Fatalf("built a %d-byte fake (ok=%v) for a negative length", len(fake), ok)
					}
				})
			}
		}
	}
}

func udpFakeLengthFieldsAgree(fake []byte, v6 bool) error {
	l3 := 20
	if v6 {
		l3 = 40
		if got := int(binary.BigEndian.Uint16(fake[4:6])); got != len(fake)-l3 {
			return fmt.Errorf("IPv6 payload length %d for a %d-byte packet", got, len(fake))
		}
	} else if got := int(binary.BigEndian.Uint16(fake[2:4])); got != len(fake) {
		return fmt.Errorf("IPv4 total length %d for a %d-byte packet", got, len(fake))
	}
	if got := int(binary.BigEndian.Uint16(fake[l3+4 : l3+6])); got != len(fake)-l3 {
		return fmt.Errorf("UDP length %d for %d bytes after the IP header", got, len(fake)-l3)
	}
	return nil
}

func TestAUDPFakeAtTheLargestOfferedLengthFitsA1500BytePacket(t *testing.T) {
	payload := bytes.Repeat([]byte{0x40}, 1600)
	for _, c := range []struct {
		v6      bool
		fakeLen int
	}{{false, 1500}, {true, 1500}, {true, 1460}} {
		fake, ok := buildUDPFakeWithoutPanic(t, c.v6, c.fakeLen, payload)
		if !ok {
			t.Fatalf("v6=%v fake_len=%d: no fake built; a too long fake must be cut to what one packet carries, not dropped", c.v6, c.fakeLen)
		}
		if len(fake) > 1500 {
			t.Errorf("v6=%v fake_len=%d: the fake is %d bytes, more than a 1500-byte MTU carries, so the kernel refuses it", c.v6, c.fakeLen, len(fake))
		}
		if err := udpFakeLengthFieldsAgree(fake, c.v6); err != nil {
			t.Errorf("v6=%v fake_len=%d: %v", c.v6, c.fakeLen, err)
		}
	}
	for _, v6 := range []bool{false, true} {
		fake, ok := buildUDPFakeWithoutPanic(t, v6, 1200, payload)
		l3 := 20
		if v6 {
			l3 = 40
		}
		if !ok || len(fake) != l3+8+1200 || !bytes.Equal(fake[l3+8:], payload[:1200]) {
			t.Errorf("v6=%v fake_len=1200: got %d bytes (ok=%v), want %d carrying the payload", v6, len(fake), ok, l3+8+1200)
		}
	}
}
