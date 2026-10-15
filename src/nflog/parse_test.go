package nflog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdlayher/netlink"
	"github.com/mdlayher/netlink/nlenc"
	"golang.org/x/sys/unix"
)

type kernelCopy struct {
	family  byte
	hook    uint8
	prefix  []byte
	inDev   uint32
	outDev  uint32
	mark    uint32
	seq     uint32
	withSeq bool
	payload []byte
	unpad   bool
}

func (k kernelCopy) message(t *testing.T) []byte {
	t.Helper()
	ae := netlink.NewAttributeEncoder()
	ae.ByteOrder = binary.BigEndian
	ae.Bytes(nfulaPacketHdr, []byte{0x08, 0x00, k.hook, 0x00})
	if k.prefix != nil {
		ae.Bytes(nfulaPrefix, k.prefix)
	}
	if k.inDev != 0 {
		ae.Uint32(nfulaIfindexIndev, k.inDev)
	}
	if k.outDev != 0 {
		ae.Uint32(nfulaIfindexOutdev, k.outDev)
	}
	if k.mark != 0 {
		ae.Uint32(nfulaMark, k.mark)
	}
	ae.Bytes(3, make([]byte, 16))
	ae.Uint32(11, 0)
	ae.Uint32(14, 0)
	if k.withSeq {
		ae.Uint32(nfulaSeq, k.seq)
	}
	ae.Bytes(nfulaPayload, k.payload)
	attrs, err := ae.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if k.unpad {
		attrs = attrs[:len(attrs)-(align4(len(k.payload))-len(k.payload))]
	}
	body := joined([]byte{k.family, unix.NFNETLINK_V0, 0x02, 0x19}, attrs)
	msg := make([]byte, nlmsgHeaderLen, nlmsgHeaderLen+len(body))
	nlenc.PutUint32(msg[0:4], uint32(nlmsgHeaderLen+len(body)))
	nlenc.PutUint16(msg[4:6], nfulnlMsgPacket)
	return append(msg, body...)
}

func controlMessage(typ uint16) []byte {
	msg := make([]byte, 20)
	nlenc.PutUint32(msg[0:4], 20)
	nlenc.PutUint16(msg[4:6], typ)
	return msg
}

func padded(msg []byte) []byte {
	return append(msg, make([]byte, align4(len(msg))-len(msg))...)
}

func queryV4(n int) []byte {
	b := make([]byte, n)
	b[0] = 0x45
	for i := 1; i < n; i++ {
		b[i] = byte(i)
	}
	return b
}

func queryV6(n int) []byte {
	b := make([]byte, n)
	b[0] = 0x60
	for i := 1; i < n; i++ {
		b[i] = byte(255 - i)
	}
	return b
}

func collect(c *Conn, datagram []byte) []Packet {
	var got []Packet
	c.process(datagram, func(p Packet) {
		p.Payload = append([]byte(nil), p.Payload...)
		got = append(got, p)
	})
	return got
}

func TestParseDatagram(t *testing.T) {
	prefix := []byte("b4dns\x00")
	v4 := kernelCopy{family: unix.AF_INET, hook: 3, prefix: prefix, outDev: 2, mark: 0x8000, seq: 7, withSeq: true, payload: queryV4(61)}
	v6 := kernelCopy{family: unix.AF_INET6, hook: 2, prefix: prefix, inDev: 3, outDev: 4, seq: 8, withSeq: true, payload: queryV6(80)}
	wantV4 := Packet{Payload: queryV4(61), Prefix: "b4dns", Mark: 0x8000, OutDev: 2, Hook: 3, Seq: 7, HasSeq: true}
	wantV6 := Packet{Payload: queryV6(80), Prefix: "b4dns", InDev: 3, OutDev: 4, Hook: 2, Seq: 8, HasSeq: true}
	odd := kernelCopy{family: unix.AF_INET, hook: 1, prefix: prefix, inDev: 5, seq: 9, withSeq: true, payload: queryV4(57)}
	wantOdd := Packet{Payload: queryV4(57), Prefix: "b4dns", InDev: 5, Hook: 1, Seq: 9, HasSeq: true}
	oddUnpadded := odd
	oddUnpadded.unpad = true

	for _, tc := range []struct {
		name     string
		datagram func(t *testing.T) []byte
		want     []Packet
	}{
		{"an IPv4 query copied in OUTPUT with its mark", func(t *testing.T) []byte { return v4.message(t) }, []Packet{wantV4}},
		{"an IPv6 query copied in FORWARD without a mark", func(t *testing.T) []byte { return v6.message(t) }, []Packet{wantV6}},
		{"an odd payload length padded by the kernel", func(t *testing.T) []byte { return odd.message(t) }, []Packet{wantOdd}},
		{"an odd payload length at the end of an unpadded datagram", func(t *testing.T) []byte { return oddUnpadded.message(t) }, []Packet{wantOdd}},
		{"two messages in one datagram", func(t *testing.T) []byte { return joined(v4.message(t), v6.message(t)) }, []Packet{wantV4, wantV6}},
		{"a message after an unpadded one starts at the aligned offset", func(t *testing.T) []byte {
			return joined(padded(oddUnpadded.message(t)), v6.message(t))
		}, []Packet{wantOdd, wantV6}},
		{"a truncated trailing message is ignored", func(t *testing.T) []byte {
			return joined(v4.message(t), v6.message(t)[:30])
		}, []Packet{wantV4}},
		{"a trailing header that claims more than the datagram holds is ignored", func(t *testing.T) []byte {
			return joined(v4.message(t), v6.message(t)[:nlmsgHeaderLen])
		}, []Packet{wantV4}},
		{"done and error messages are ignored", func(t *testing.T) []byte {
			return joined(controlMessage(uint16(netlink.Done)), v4.message(t), controlMessage(uint16(netlink.Error)))
		}, []Packet{wantV4}},
		{"a copy without a sequence number", func(t *testing.T) []byte {
			k := v4
			k.withSeq = false
			return k.message(t)
		}, []Packet{{Payload: queryV4(61), Prefix: "b4dns", Mark: 0x8000, OutDev: 2, Hook: 3}}},
		{"a prefix ends at its first NUL", func(t *testing.T) []byte {
			k := v4
			k.prefix = []byte("b4dns\x00junk\x00")
			return k.message(t)
		}, []Packet{wantV4}},
		{"a copy without a prefix", func(t *testing.T) []byte {
			k := v4
			k.prefix = nil
			return k.message(t)
		}, []Packet{{Payload: queryV4(61), Mark: 0x8000, OutDev: 2, Hook: 3, Seq: 7, HasSeq: true}}},
		{"a copy with a malformed mark is dropped", func(t *testing.T) []byte {
			msg := v4.message(t)
			bad := bytes.Index(msg, nativeAttr(nfulaMark, 0x00, 0x00, 0x80, 0x00))
			if bad < 0 {
				t.Fatal("the mark attribute is missing from the test message")
			}
			nlenc.PutUint16(msg[bad:bad+2], 6)
			return joined(msg, v6.message(t))
		}, []Packet{wantV6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := collect(&Conn{}, tc.datagram(t))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parsed\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func copiesWithSeq(t *testing.T, seqs ...uint32) []byte {
	t.Helper()
	var datagram []byte
	for _, seq := range seqs {
		datagram = append(datagram, kernelCopy{family: unix.AF_INET, hook: 1, prefix: []byte("b4dns\x00"), seq: seq, withSeq: true, payload: queryV4(40)}.message(t)...)
	}
	return datagram
}

func TestSeqGapsCountLost(t *testing.T) {
	for _, tc := range []struct {
		name      string
		datagrams [][]uint32
		lost      uint64
	}{
		{"copies in order", [][]uint32{{0, 1, 2}, {3}}, 0},
		{"a gap inside one datagram", [][]uint32{{5, 6, 9}}, 2},
		{"a gap between datagrams", [][]uint32{{5}, {6}, {10, 11}}, 3},
		{"the first copy seen sets the start", [][]uint32{{1000}, {1001}}, 0},
		{"the sequence wraps without loss", [][]uint32{{0xfffffffe, 0xffffffff}, {0, 1}}, 0},
		{"copies lost across the wrap", [][]uint32{{0xfffffffe}, {1}}, 2},
		{"a step back is not counted as loss", [][]uint32{{100}, {3, 4}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Conn{}
			for _, seqs := range tc.datagrams {
				c.process(copiesWithSeq(t, seqs...), func(Packet) {})
			}
			if got := c.Lost(); got != tc.lost {
				t.Fatalf("Lost() = %d, want %d", got, tc.lost)
			}
		})
	}

	t.Run("copies without a sequence number count nothing", func(t *testing.T) {
		c := &Conn{}
		k := kernelCopy{family: unix.AF_INET, hook: 1, payload: queryV4(40)}
		c.process(joined(copiesWithSeq(t, 1), k.message(t), copiesWithSeq(t, 2)), func(Packet) {})
		if got := c.Lost(); got != 0 {
			t.Fatalf("Lost() = %d, want 0", got)
		}
	})
}

func socketpairConn(t *testing.T) (*Conn, int) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	peer := fds[1]
	t.Cleanup(func() { _ = unix.Close(peer) })
	f := os.NewFile(uintptr(fds[0]), "nflog-test")
	raw, err := f.SyscallConn()
	if err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	c := newConn(f, raw)
	t.Cleanup(func() { _ = c.Close() })
	return c, peer
}

func spin(d time.Duration) {
	for end := time.Now().Add(d); time.Now().Before(end); {
	}
}

func TestDrainReturnsWhileTheReaderIsBehind(t *testing.T) {
	c, peer := socketpairConn(t)
	var decided atomic.Uint64
	decide := func(Packet) {
		decided.Add(1)
		spin(100 * time.Microsecond)
	}
	done := make(chan error, 1)
	go func() { done <- c.Run(decide) }()

	floodEnd := time.Now().Add(700 * time.Millisecond)
	flooded := make(chan struct{})
	go func() {
		defer close(flooded)
		seq := uint32(0)
		for time.Now().Before(floodEnd) {
			if _, err := unix.Write(peer, copiesWithSeq(t, seq, seq+1, seq+2, seq+3)); err == nil {
				seq += 4
			} else {
				time.Sleep(20 * time.Microsecond)
			}
		}
	}()

	var worst time.Duration
	for time.Now().Before(floodEnd) {
		start := time.Now()
		if err := c.Drain(decide); err != nil {
			t.Fatalf("Drain: %v", err)
		}
		worst = max(worst, time.Since(start))
		time.Sleep(5 * time.Millisecond)
	}
	<-flooded
	if worst > 200*time.Millisecond {
		t.Errorf("a Drain held its caller for %s while copies kept arriving faster than they were decided; each NFQUEUE worker that matches a DNS answer waits on it", worst)
	}
	if decided.Load() == 0 {
		t.Fatal("no copy was decided, so the check proves nothing")
	}
	_ = c.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept running after Close")
	}
}

func TestDrainDecidesAQueuedBurstInOrder(t *testing.T) {
	prev := readBatch
	readBatch = 2
	t.Cleanup(func() { readBatch = prev })
	c, peer := socketpairConn(t)
	want := uint32(0)
	for range 8 {
		seqs := make([]uint32, 25)
		for i := range seqs {
			seqs[i] = want
			want++
		}
		if _, err := unix.Write(peer, copiesWithSeq(t, seqs...)); err != nil {
			t.Fatal(err)
		}
	}
	next := uint32(0)
	if err := c.Drain(func(p Packet) {
		if p.Seq != next {
			t.Errorf("copy %d was delivered where %d was due", p.Seq, next)
		}
		next = p.Seq + 1
	}); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if next != want {
		t.Fatalf("one Drain delivered %d of the %d copies queued before it; the answer gate relies on every earlier copy being decided", next, want)
	}
	if c.backlogged() {
		t.Fatal("a drain that reached the end of the socket left it marked as backlogged")
	}
}

func TestRunAndDrainDeliverEveryCopyOnce(t *testing.T) {
	c, peer := socketpairConn(t)

	send := func(seqs ...uint32) {
		t.Helper()
		if _, err := unix.Write(peer, copiesWithSeq(t, seqs...)); err != nil {
			t.Fatal(err)
		}
	}
	seqsOf := func(drain func(func(Packet)) error) []uint32 {
		t.Helper()
		var got []uint32
		if err := drain(func(p Packet) { got = append(got, p.Seq) }); err != nil {
			t.Fatalf("Drain: %v", err)
		}
		return got
	}

	send(1, 2)
	send(3)
	if got := seqsOf(c.Drain); fmt.Sprint(got) != "[1 2 3]" {
		t.Fatalf("Drain delivered %v, want [1 2 3]", got)
	}
	if got := seqsOf(c.Drain); len(got) != 0 {
		t.Fatalf("a second Drain delivered %v again", got)
	}

	seen := make(chan uint32, 8)
	done := make(chan error, 1)
	go func() { done <- c.Run(func(p Packet) { seen <- p.Seq }) }()
	expect := func(want uint32) {
		t.Helper()
		select {
		case got := <-seen:
			if got != want {
				t.Fatalf("Run delivered copy %d, want %d", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Run never delivered copy %d", want)
		}
	}
	send(4)
	expect(4)
	send(6)
	expect(6)
	if got := c.Lost(); got != 1 {
		t.Fatalf("Lost() = %d, want 1 for the missing copy 5", got)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v after Close, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept running after Close")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("a second Close returned %v", err)
	}
	if err := c.Drain(func(Packet) { t.Fatal("Drain delivered a copy after Close") }); err != nil {
		t.Fatalf("Drain after Close returned %v, want nil", err)
	}
}
