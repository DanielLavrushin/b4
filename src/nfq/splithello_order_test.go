package nfq

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/sock"
)

type orderLog struct {
	mu  sync.Mutex
	got []string
}

func (l *orderLog) add(s string) {
	l.mu.Lock()
	l.got = append(l.got, s)
	l.mu.Unlock()
}

func (l *orderLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.got...)
}

func (l *orderLog) sends() []string {
	var out []string
	for _, s := range l.list() {
		if after, ok := strings.CutPrefix(s, "send "); ok {
			out = append(out, after)
		}
	}
	return out
}

type orderedVerdicts struct {
	log *orderLog
}

func (q orderedVerdicts) SetVerdict(id uint32, verdict int) error {
	q.log.add(heldVerdict{id: id, verdict: verdict}.String())
	return nil
}

type namedPiece struct {
	name string
	data []byte
}

func labelSent(pkt []byte, base uint32, fake []byte, pieces ...namedPiece) string {
	segs := sentData([][]byte{pkt})
	if len(segs) != 1 {
		return "other"
	}
	s := segs[0]
	if len(fake) > 0 && bytes.Equal(s.payload, fake) {
		return fmt.Sprintf("F@%d", s.seq-base)
	}
	for _, p := range pieces {
		if bytes.Equal(s.payload, p.data) {
			return p.name
		}
	}
	return fmt.Sprintf("piece@%d+%d", s.seq-base, len(s.payload))
}

func parkFirstSend(t *testing.T, w *Worker, order *orderLog, label func([]byte) string) (<-chan struct{}, func()) {
	t.Helper()
	parked := make(chan struct{})
	release := make(chan struct{})
	var first, released sync.Once
	free := func() { released.Do(func() { close(release) }) }
	w.sock.Divert(&sock.Sender{}, func(pkt []byte) bool {
		park := false
		first.Do(func() { park = true })
		if park {
			close(parked)
			<-release
		}
		order.add("send " + label(pkt))
		return false
	})
	t.Cleanup(free)
	return parked, free
}

func waitParked(t *testing.T, parked <-chan struct{}) {
	t.Helper()
	select {
	case <-parked:
	case <-time.After(5 * time.Second):
		t.Fatal("the injector never sent the first packet")
	}
}

func TestASplitClientHelloLeavesInSequenceOrder(t *testing.T) {
	for _, c := range []struct {
		name   string
		packet func([]byte, uint32) []byte
	}{
		{"IPv4", makeV4TCPPacket},
		{"IPv6", makeV6TCPPacket},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newFakingSet(false)
			w, sent := heldHelloWorker(t, &set)
			fake := sock.GetPayload(&set.Faking)
			hello := helloWithEarlySNI("i.ytimg.com")
			const cut = 600
			seg1, seg2 := hello[:cut], hello[cut:]
			order := &orderLog{}
			parked, release := parkFirstSend(t, w, order, func(pkt []byte) string {
				return labelSent(pkt, splitSeq, fake, namedPiece{"1", seg1}, namedPiece{"2", seg2})
			})
			q := orderedVerdicts{order}

			queueTo(w, q, 1, c.packet(seg1, splitSeq))
			waitParked(t, parked)
			queueTo(w, q, 2, c.packet(seg2, splitSeq+cut))
			if got := order.list(); !slices.Equal(got, []string{"drop 1", "drop 2"}) {
				t.Fatalf("while segment 1's fake is still on its way: %v, want [drop 1 drop 2]; segment 2 must wait for segment 1, not leave ahead of it", got)
			}

			release()
			sent()
			if got, want := order.sends(), []string{"F@0", "1", "2"}; !slices.Equal(got, want) {
				t.Fatalf("sent %v, want %v: the fake, segment 1, then segment 2 with no fake of its own", got, want)
			}
		})
	}
}
