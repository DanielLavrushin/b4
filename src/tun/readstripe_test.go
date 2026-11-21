package tun

import (
	"encoding/binary"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/engine"
)

type scriptedTUN struct {
	pkts chan []byte
	quit chan struct{}
}

func (s *scriptedTUN) Read(buf []byte) (int, error) {
	select {
	case p := <-s.pkts:
		return copy(buf, p), nil
	case <-s.quit:
		return 0, io.EOF
	}
}

type parkingProcessor struct {
	mu      sync.Mutex
	seen    []string
	park    string
	parked  chan struct{}
	release chan struct{}
}

func (p *parkingProcessor) ProcessPacketDir(raw []byte, _ engine.Direction) engine.PacketVerdict {
	name := string(raw[40:])
	p.mu.Lock()
	p.seen = append(p.seen, name)
	p.mu.Unlock()
	if name == p.park {
		close(p.parked)
		<-p.release
	}
	return engine.VerdictDrop
}

func (p *parkingProcessor) list() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.seen...)
}

func (p *parkingProcessor) waitFor(t *testing.T, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !slices.Contains(p.list(), name) {
		if time.Now().After(deadline) {
			t.Fatalf("%q was never processed, got %v", name, p.list())
		}
		time.Sleep(time.Millisecond)
	}
}

func tcpPacketFrom(sport uint16, payload string) []byte {
	pkt := make([]byte, 40+len(payload))
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	pkt[8] = 64
	pkt[9] = 6
	copy(pkt[12:16], []byte{10, 0, 0, 1})
	copy(pkt[16:20], []byte{1, 2, 3, 4})
	binary.BigEndian.PutUint16(pkt[20:22], sport)
	binary.BigEndian.PutUint16(pkt[22:24], 443)
	pkt[32] = 5 << 4
	pkt[33] = 0x18
	copy(pkt[40:], payload)
	return pkt
}

func TestASecondReaderWaitsForTheFirstPacketOfItsConnection(t *testing.T) {
	seg1, seg2 := tcpPacketFrom(12345, "seg1"), tcpPacketFrom(12345, "seg2")
	stripe := func(p []byte) uint32 {
		h, ok := tcpFlowHash(p)
		if !ok {
			t.Fatal("a TCP packet must have a flow stripe")
		}
		return h % flowStripes
	}
	if stripe(seg1) != stripe(seg2) {
		t.Fatal("two packets of one connection must share a stripe")
	}
	port := uint16(23456)
	for stripe(tcpPacketFrom(port, "other")) == stripe(seg1) {
		port++
	}
	other := tcpPacketFrom(port, "other")

	e := &Engine{quit: make(chan struct{})}
	src := &scriptedTUN{pkts: make(chan []byte), quit: e.quit}
	proc := &parkingProcessor{park: "seg1", parked: make(chan struct{}), release: make(chan struct{})}
	var readers sync.WaitGroup
	t.Cleanup(func() {
		select {
		case <-proc.release:
		default:
			close(proc.release)
		}
		close(e.quit)
		readers.Wait()
	})
	for range 2 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			e.serve(src, proc, func([]byte, engine.Direction) {})
		}()
	}

	src.pkts <- seg1
	select {
	case <-proc.parked:
	case <-time.After(5 * time.Second):
		t.Fatal("segment 1 was never processed")
	}
	src.pkts <- other
	proc.waitFor(t, "other")
	src.pkts <- seg2
	time.Sleep(50 * time.Millisecond)
	if got := proc.list(); !slices.Equal(got, []string{"seg1", "other"}) {
		t.Fatalf("while segment 1 is being handled: processed %v, want [seg1 other]; segment 2 of the same connection must wait, another connection must not", got)
	}

	close(proc.release)
	proc.waitFor(t, "seg2")
	if got := proc.list(); !slices.Equal(got, []string{"seg1", "other", "seg2"}) {
		t.Fatalf("processed %v, want [seg1 other seg2]", got)
	}
}

func TestAnotherConnectionIsReadWhileASegmentWaitsForItsTurn(t *testing.T) {
	seg1, seg2 := tcpPacketFrom(12345, "seg1"), tcpPacketFrom(12345, "seg2")
	h1, _ := tcpFlowHash(seg1)
	port := uint16(23456)
	for {
		h, _ := tcpFlowHash(tcpPacketFrom(port, "other"))
		if h%flowStripes != h1%flowStripes {
			break
		}
		port++
	}
	other := tcpPacketFrom(port, "other")

	e := &Engine{quit: make(chan struct{})}
	src := &scriptedTUN{pkts: make(chan []byte), quit: e.quit}
	proc := &parkingProcessor{park: "seg1", parked: make(chan struct{}), release: make(chan struct{})}
	var readers sync.WaitGroup
	t.Cleanup(func() {
		select {
		case <-proc.release:
		default:
			close(proc.release)
		}
		close(e.quit)
		readers.Wait()
	})
	for range 3 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			e.serve(src, proc, func([]byte, engine.Direction) {})
		}()
	}

	src.pkts <- seg1
	select {
	case <-proc.parked:
	case <-time.After(5 * time.Second):
		t.Fatal("segment 1 was never processed")
	}
	src.pkts <- seg2
	delivered := make(chan struct{})
	go func() {
		select {
		case src.pkts <- other:
			close(delivered)
		case <-e.quit:
		}
	}()
	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatalf("while segment 2 waits for segment 1 of its connection, no reader takes a packet of another connection (processed %v)", proc.list())
	}
	proc.waitFor(t, "other")
	if got := proc.list(); !slices.Equal(got, []string{"seg1", "other"}) {
		t.Fatalf("while segment 1 is being handled: processed %v, want [seg1 other]", got)
	}

	close(proc.release)
	proc.waitFor(t, "seg2")
	if got := proc.list(); !slices.Equal(got, []string{"seg1", "other", "seg2"}) {
		t.Fatalf("processed %v, want [seg1 other seg2]", got)
	}
}

func TestOnlyTCPPacketsTakeAFlowStripe(t *testing.T) {
	udp := tcpPacketFrom(12345, "dns")
	udp[9] = 17
	frag := tcpPacketFrom(12345, "frag")
	binary.BigEndian.PutUint16(frag[6:8], 0x2000)
	for name, p := range map[string][]byte{"UDP": udp, "an IPv4 fragment": frag, "a short packet": {0x45, 0}} {
		if _, ok := tcpFlowHash(p); ok {
			t.Errorf("%s took a flow stripe, it must be read without one", name)
		}
	}
}
