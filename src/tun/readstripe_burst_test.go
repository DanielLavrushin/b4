package tun

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/engine"
)

func serveReaders(t *testing.T, readers int, proc *parkingProcessor) *scriptedTUN {
	t.Helper()
	e := &Engine{quit: make(chan struct{})}
	src := &scriptedTUN{pkts: make(chan []byte), quit: e.quit}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		select {
		case <-proc.release:
		default:
			close(proc.release)
		}
		close(e.quit)
		wg.Wait()
	})
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.serve(src, proc, func([]byte, engine.Direction) {})
		}()
	}
	return src
}

func feed(t *testing.T, src *scriptedTUN, p []byte) {
	t.Helper()
	select {
	case src.pkts <- p:
	case <-time.After(2 * time.Second):
		t.Fatalf("no reader took %q", p[40:])
	}
}

func segmentsOf(sport uint16, n int) ([][]byte, []string) {
	pkts, names := make([][]byte, n), make([]string, n)
	for i := range n {
		names[i] = fmt.Sprintf("seg%d", i+1)
		pkts[i] = tcpPacketFrom(sport, names[i])
	}
	return pkts, names
}

func TestABurstOfOneConnectionLeavesTheOtherReadersFree(t *testing.T) {
	const readers = 4
	segs, names := segmentsOf(12345, readers)
	h1, _ := tcpFlowHash(segs[0])
	port := uint16(23456)
	for {
		h, _ := tcpFlowHash(tcpPacketFrom(port, "other"))
		if h%flowStripes != h1%flowStripes {
			break
		}
		port++
	}
	proc := &parkingProcessor{park: "seg1", parked: make(chan struct{}), release: make(chan struct{})}
	src := serveReaders(t, readers, proc)

	feed(t, src, segs[0])
	select {
	case <-proc.parked:
	case <-time.After(5 * time.Second):
		t.Fatal("segment 1 was never processed")
	}
	for _, p := range segs[1:] {
		feed(t, src, p)
	}
	feed(t, src, tcpPacketFrom(port, "other"))
	proc.waitFor(t, "other")
	if got := proc.list(); !slices.Equal(got, []string{"seg1", "other"}) {
		t.Fatalf("while segment 1 is being handled and %d later segments of its connection wait: processed %v, want [seg1 other]", readers-1, got)
	}

	close(proc.release)
	proc.waitFor(t, names[readers-1])
	want := slices.Insert(slices.Clone(names), 1, "other")
	if got := proc.list(); !slices.Equal(got, want) {
		t.Fatalf("processed %v, want %v", got, want)
	}
}

func TestAPacketPastAFullStripeBacklogIsProcessedAtOnce(t *testing.T) {
	segs, names := segmentsOf(12345, stripeBacklog+2)
	proc := &parkingProcessor{park: "seg1", parked: make(chan struct{}), release: make(chan struct{})}
	src := serveReaders(t, 2, proc)

	feed(t, src, segs[0])
	select {
	case <-proc.parked:
	case <-time.After(5 * time.Second):
		t.Fatal("segment 1 was never processed")
	}
	for _, p := range segs[1:] {
		feed(t, src, p)
	}
	last := names[len(names)-1]
	proc.waitFor(t, last)
	if got := proc.list(); !slices.Equal(got, []string{"seg1", last}) {
		t.Fatalf("with %d segments waiting behind segment 1: processed %v, want [seg1 %s]", stripeBacklog, got, last)
	}

	close(proc.release)
	proc.waitFor(t, names[len(names)-2])
	want := append([]string{"seg1", last}, names[1:len(names)-1]...)
	if got := proc.list(); !slices.Equal(got, want) {
		t.Fatalf("processed %v, want %v", got, want)
	}
}
