package sock

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func helloPacket(t *testing.T, v6 bool) (pkt []byte, l3 int) {
	t.Helper()
	hello := realHello(t, "www.example.com")
	if v6 {
		pkt = buildMinimalIPv6TCPPacket(len(hello))
		copy(pkt[60:], hello)
		FixTCPChecksumV6(pkt)
		return pkt, 40
	}
	pkt = packetWithTimestamp(len(hello))
	copy(pkt[52:], hello)
	FixIPv4Checksum(pkt[:20])
	FixTCPChecksum(pkt)
	return pkt, 20
}

func seqFakeSet(strategy string, offset int32, payload []byte) *config.SetConfig {
	set := config.NewSetConfig()
	set.Faking.SNIType = config.FakePayloadDefault1
	set.Faking.PayloadData = nil
	if payload != nil {
		set.Faking.SNIType = config.FakePayloadCapture
		set.Faking.PayloadData = payload
	}
	set.Faking.Strategy = strategy
	set.Faking.SeqOffset = offset
	set.Faking.ApplyTTL = false
	set.Faking.TLSMod = nil
	set.Faking.FakeLenMode = ""
	return &set
}

func buildSeqFake(t *testing.T, original []byte, v6 bool, set *config.SetConfig) []byte {
	t.Helper()
	var fake []byte
	if v6 {
		fake = BuildFakeSNIPacketV6(original, set)
	} else {
		fake = BuildFakeSNIPacketV4(original, set)
	}
	if fake == nil {
		t.Fatal("fake not built")
	}
	return fake
}

func tcpSeqAndPayload(pkt []byte, l3 int) (seq uint32, payload int) {
	thl := int(pkt[l3+12]>>4) * 4
	return binary.BigEndian.Uint32(pkt[l3+4 : l3+8]), len(pkt) - l3 - thl
}

func TestAPastSeqOrRandSeqFakeEndsBeforeTheRealData(t *testing.T) {
	big := make([]byte, 9000)
	big[0] = 0x17
	cases := []struct {
		strategy string
		offset   int32
		payload  []byte
	}{
		{"pastseq", 2, nil},
		{"pastseq", 1000, nil},
		{"pastseq", -2, nil},
		{"pastseq", 0, big},
		{"randseq", 100, nil},
		{"randseq", -100, nil},
	}
	for _, v6 := range []bool{false, true} {
		original, l3 := helloPacket(t, v6)
		realSeq, _ := tcpSeqAndPayload(original, l3)
		for _, c := range cases {
			t.Run(fmt.Sprintf("%s/%s seq_offset=%d/payload=%d", familyName(v6), c.strategy, c.offset, len(c.payload)), func(t *testing.T) {
				fake := buildSeqFake(t, original, v6, seqFakeSet(c.strategy, c.offset, c.payload))
				seq, n := tcpSeqAndPayload(fake, l3)
				if overlap := int32(seq + uint32(n) - realSeq); overlap > 0 {
					t.Fatalf("the %d-byte fake (seq %d) ends %d bytes past the real data's first byte (seq %d), so a server takes those bytes as the start of the stream",
						n, seq, overlap, realSeq)
				}
			})
		}
	}
}

func TestAPastSeqFakeKeepsAnOffsetLargerThanItself(t *testing.T) {
	cases := []struct {
		strategy string
		offset   int32
		back     func(dataLen int) uint32
	}{
		{"pastseq", 10000, func(int) uint32 { return 10000 }},
		{"pastseq", 0, func(int) uint32 { return 8192 }},
		{"pastseq", 1000000, func(int) uint32 { return 1000000 }},
		{"randseq", 10000, func(n int) uint32 { return 10000 + uint32(n) }},
	}
	for _, v6 := range []bool{false, true} {
		original, l3 := helloPacket(t, v6)
		realSeq, dataLen := tcpSeqAndPayload(original, l3)
		for _, c := range cases {
			fake := buildSeqFake(t, original, v6, seqFakeSet(c.strategy, c.offset, nil))
			seq, _ := tcpSeqAndPayload(fake, l3)
			if want := realSeq - c.back(dataLen); seq != want {
				t.Errorf("%s %s seq_offset=%d: fake seq %d, want %d", familyName(v6), c.strategy, c.offset, seq, want)
			}
		}
	}
}
