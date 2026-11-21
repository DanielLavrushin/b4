package tun

import (
	"encoding/binary"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/engine"
)

type orderProcessor struct {
	mu   sync.Mutex
	seq  map[uint16][]uint16
	done chan struct{}
	left int
}

func (p *orderProcessor) ProcessPacketDir(raw []byte, _ engine.Direction) engine.PacketVerdict {
	sport := binary.BigEndian.Uint16(raw[20:22])
	n := binary.BigEndian.Uint16(raw[40:42])
	if rand.Intn(4) == 0 {
		time.Sleep(time.Duration(rand.Intn(300)) * time.Microsecond)
	}
	p.mu.Lock()
	p.seq[sport] = append(p.seq[sport], n)
	p.left--
	if p.left == 0 {
		close(p.done)
	}
	p.mu.Unlock()
	return engine.VerdictDrop
}

func TestPacketsOfEachConnectionAreProcessedInOrderByConcurrentReaders(t *testing.T) {
	const readers, flows, per = 4, 24, 14
	var ports []uint16
	used := map[uint32]bool{}
	for port := uint16(20000); len(ports) < flows; port++ {
		h, _ := tcpFlowHash(tcpPacketFrom(port, "xx"))
		if !used[h%flowStripes] {
			used[h%flowStripes] = true
			ports = append(ports, port)
		}
	}
	var pkts [][]byte
	next := make([]int, flows)
	for len(pkts) < flows*per {
		i := rand.Intn(flows)
		if next[i] == per {
			continue
		}
		p := tcpPacketFrom(ports[i], "xx")
		binary.BigEndian.PutUint16(p[40:42], uint16(next[i]))
		next[i]++
		pkts = append(pkts, p)
	}
	e := &Engine{quit: make(chan struct{})}
	src := &scriptedTUN{pkts: make(chan []byte, len(pkts)), quit: e.quit}
	proc := &orderProcessor{seq: map[uint16][]uint16{}, done: make(chan struct{}), left: len(pkts)}
	for _, p := range pkts {
		src.pkts <- p
	}
	var wg sync.WaitGroup
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.serve(src, proc, func([]byte, engine.Direction) {})
		}()
	}
	select {
	case <-proc.done:
	case <-time.After(10 * time.Second):
		t.Error("not every packet was processed")
	}
	close(e.quit)
	wg.Wait()
	for port, s := range proc.seq {
		for i, n := range s {
			if int(n) != i {
				t.Fatalf("flow %d processed out of order: %v", port, s)
			}
		}
	}
	for i := range e.stripes {
		if e.stripes[i].busy || len(e.stripes[i].pending) != 0 {
			t.Fatalf("stripe %d left busy=%v pending=%d", i, e.stripes[i].busy, len(e.stripes[i].pending))
		}
	}
}
