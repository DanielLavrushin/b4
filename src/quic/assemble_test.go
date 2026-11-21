package quic

import (
	"runtime"
	"testing"
)

func oneByteCryptoFrames(step, budget int) []byte {
	var plain []byte
	for off := 0; ; off += step {
		frame := append([]byte{0x06}, testVarint(off)...)
		frame = append(frame, 0x01, 0x41)
		if len(plain)+len(frame) > budget {
			return plain
		}
		plain = append(plain, frame...)
	}
}

func TestReassemblingManyOneByteCryptoFramesAllocatesLittle(t *testing.T) {
	const largestPayloadFile = 64 << 10
	const allowed = 4 << 20
	dcid := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	for _, step := range []int{1, 2} {
		packet := testSealInitial(t, dcid, oneByteCryptoFrames(step, largestPayloadFile-64))
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		plain, decrypted := DecryptInitial(dcid, packet)
		crypto, assembled := AssembleCrypto(dcid, plain)
		runtime.ReadMemStats(&after)
		ClearDCID(dcid)
		if !decrypted || !assembled || len(crypto) == 0 {
			t.Fatalf("fixture: the %d-byte Initial with CRYPTO frames %d apart must decrypt and reassemble, got decrypted %t, assembled %t, %d bytes", len(packet), step, decrypted, assembled, len(crypto))
		}
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > allowed {
			t.Errorf("reassembling a %d-byte Initial of one-byte CRYPTO frames %d apart allocated %.1f MB, want at most %d MB",
				len(packet), step, float64(allocated)/(1<<20), allowed>>20)
		}
	}
}

func TestReassemblyKeepsTheContiguousPrefix(t *testing.T) {
	dcid := []byte{9, 8, 7, 6, 5, 4, 3, 2}
	defer ClearDCID(dcid)
	frame := func(off int, data string) []byte {
		f := append([]byte{0x06}, testVarint(off)...)
		f = append(f, testVarint(len(data))...)
		return append(f, data...)
	}
	steps := []struct {
		plain []byte
		want  string
	}{
		{frame(0, "abc"), "abc"},
		{frame(5000, "far"), "abc"},
		{frame(3, "de"), "abcde"},
		{append(frame(10, "k"), frame(5, "fghij")...), "abcdefghijk"},
		{frame(40000, "beyond the buffer"), "abcdefghijk"},
	}
	for _, s := range steps {
		got, ok := AssembleCrypto(dcid, s.plain)
		if !ok || string(got) != s.want {
			t.Fatalf("after % x the reassembled prefix is %q (%t), want %q", s.plain, got, ok, s.want)
		}
	}
}
