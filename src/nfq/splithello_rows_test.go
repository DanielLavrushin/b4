package nfq

import (
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sock"
)

const earlyCut = 600

func recordSends(w *Worker, order *orderLog, label func([]byte) string) {
	w.sock.Divert(&sock.Sender{}, func(pkt []byte) bool {
		order.add("send " + label(pkt))
		return false
	})
}

func sentGroup(pkt []byte, base uint32, cut int) string {
	segs := sentData([][]byte{pkt})
	if len(segs) != 1 {
		return "other"
	}
	if int(segs[0].seq-base) < cut {
		return "1"
	}
	return "2"
}

func orderedWorker(t *testing.T, sets ...*config.SetConfig) (*Worker, func() [][]byte) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Queue.IPv6Enabled = true
	cfg.Sets = sets
	w := newTestWorker(t, &cfg)
	sent := captureInjected(t, w)
	t.Cleanup(w.pendingHello.ReleaseHolds)
	return w, sent
}

func whileParked(t *testing.T, order *orderLog, want ...string) {
	t.Helper()
	time.Sleep(20 * time.Millisecond)
	if got := order.list(); !slices.Equal(got, want) {
		t.Fatalf("while segment 1 is on its way: %v, want %v; nothing of segment 2 may leave ahead of it", got, want)
	}
}

func TestEveryPathSendsASplitClientHelloInSequenceOrder(t *testing.T) {
	hello := helloWithEarlySNI("i.ytimg.com")
	seg1, seg2 := hello[:earlyCut], hello[earlyCut:]
	cases := []struct {
		name   string
		set    func() config.SetConfig
		before func(*Worker, *config.SetConfig, func() [][]byte)
		packet func([]byte, uint32) []byte
		want   []string
	}{
		{
			name:   "learned on this connection",
			set:    func() config.SetConfig { return newFakingSet(false) },
			packet: makeV4TCPPacket,
			want:   []string{"F@0", "1", "2"},
		},
		{
			name: "learned from an earlier connection",
			set:  func() config.SetConfig { return newFakingSet(false) },
			before: func(w *Worker, _ *config.SetConfig, sent func() [][]byte) {
				w.ProcessPacket(makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 100, 1111, 0x18))
				sent()
			},
			packet: makeV4TCPPacket,
			want:   []string{"F@0", "1", "2"},
		},
		{
			name: "the connection's decision, domain-only set",
			set: func() config.SetConfig {
				s := newFakingSet(false)
				s.Targets.DomainOnly = true
				return s
			},
			packet: makeV4TCPPacket,
			want:   []string{"F@0", "1", "2"},
		},
		{
			name: "a DNS host hint",
			set:  func() config.SetConfig { return newFakingSet(false) },
			before: func(w *Worker, s *config.SetConfig, _ func() [][]byte) {
				w.storeHostHint("10.0.0.1", "1.2.3.4", s, "i.ytimg.com", "dns", 0, 0)
			},
			packet: makeV4TCPPacket,
			want:   []string{"F@0", "1", "F@600", "2"},
		},
		{
			name: "an address target",
			set: func() config.SetConfig {
				s := newFakingSet(false)
				s.Targets.DomainsToMatch = nil
				s.Targets.IpsToMatch = []string{"1.2.3.4"}
				return s
			},
			packet: makeV4TCPPacket,
			want:   []string{"F@0", "1", "F@600", "2"},
		},
		{
			name:   "learned on this connection, IPv6",
			set:    func() config.SetConfig { return newFakingSet(false) },
			packet: makeV6TCPPacket,
			want:   []string{"F@0", "1", "2"},
		},
		{
			name: "a DNS host hint, IPv6",
			set:  func() config.SetConfig { return newFakingSet(false) },
			before: func(w *Worker, s *config.SetConfig, _ func() [][]byte) {
				w.storeHostHint("2001:db8::1", "2001:db8::2", s, "i.ytimg.com", "dns", 0, 0)
			},
			packet: makeV6TCPPacket,
			want:   []string{"F@0", "1", "F@600", "2"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set := c.set()
			w, sent := orderedWorker(t, &set)
			fake := sock.GetPayload(&set.Faking)
			if c.before != nil {
				c.before(w, &set, sent)
			}
			order := &orderLog{}
			parked, release := parkFirstSend(t, w, order, func(pkt []byte) string {
				return labelSent(pkt, splitSeq, fake, namedPiece{"1", seg1}, namedPiece{"2", seg2})
			})
			q := orderedVerdicts{order}

			queueTo(w, q, 1, c.packet(seg1, splitSeq))
			waitParked(t, parked)
			queueTo(w, q, 2, c.packet(seg2, splitSeq+earlyCut))
			whileParked(t, order, "drop 1", "drop 2")
			release()
			sent()
			if got := order.sends(); !slices.Equal(got, c.want) {
				t.Fatalf("sent %v, want %v", got, c.want)
			}
		})
	}
}

func TestAFragmentingSetSendsAllOfSegmentOneBeforeSegmentTwo(t *testing.T) {
	for _, c := range []struct {
		name   string
		packet func([]byte, uint32) []byte
	}{
		{"IPv4", makeV4TCPPacket},
		{"IPv6", makeV6TCPPacket},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newFakingSet(true)
			set.Fragmentation.Strategy = "combo"
			set.Fragmentation.SeqOverlapBytes = nil
			set.Fragmentation.Combo.FirstDelayMs = 1
			set.Fragmentation.Combo.FirstDelayMsMax = 0
			w, sent := orderedWorker(t, &set)
			fake := sock.GetPayload(&set.Faking)
			hello := helloWithEarlySNI("i.ytimg.com")
			order := &orderLog{}
			parked, release := parkFirstSend(t, w, order, func(pkt []byte) string {
				return sentGroup(pkt, splitSeq, earlyCut)
			})
			q := orderedVerdicts{order}

			queueTo(w, q, 1, c.packet(hello[:earlyCut], splitSeq))
			waitParked(t, parked)
			queueTo(w, q, 2, c.packet(hello[earlyCut:], splitSeq+earlyCut))
			whileParked(t, order, "drop 1", "drop 2")
			release()
			pkts := sent()

			groups := order.sends()
			if !slices.IsSorted(groups) || !slices.Contains(groups, "1") || !slices.Contains(groups, "2") {
				t.Fatalf("sent pieces of segments %v, want every packet of segment 1 before any packet of segment 2", groups)
			}
			if got := fakeHelloSeqs(pkts, fake); !slices.Equal(got, []uint32{splitSeq, splitSeq + earlyCut}) {
				t.Fatalf("fake hellos at %v, want one on each segment as before", got)
			}
			coversOnce(t, realSegments(pkts, fake), splitSeq, hello)
		})
	}
}

func TestAHeldSegmentOneLeavesBeforeTheSegmentWithTheName(t *testing.T) {
	staggerReplays(t, 0)
	for _, c := range []struct {
		name   string
		packet func([]byte, uint32) []byte
	}{
		{"IPv4", makeV4TCPPacket},
		{"IPv6", makeV6TCPPacket},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newFakingSet(false)
			w, sent := orderedWorker(t, &set)
			fake := sock.GetPayload(&set.Faking)
			_, seg1, seg2 := splitHello("i.ytimg.com")
			order := &orderLog{}
			recordSends(w, order, func(pkt []byte) string {
				return labelSent(pkt, splitSeq, fake, namedPiece{"1", seg1}, namedPiece{"2", seg2})
			})
			q := orderedVerdicts{order}

			queueTo(w, q, 1, c.packet(seg1, splitSeq))
			queueTo(w, q, 2, c.packet(seg2, splitSeq+splitCut))
			sent()
			want := []string{"drop 2", "drop 1", "send F@0", "send 1", "send F@1396", "send 2"}
			if got := order.list(); !slices.Equal(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

func TestTheHeldSegmentTwoStillWaitsTheReplayStagger(t *testing.T) {
	const stagger = 60 * time.Millisecond
	staggerReplays(t, stagger)
	set := newFakingSet(false)
	w, sent := orderedWorker(t, &set)
	_, seg1, seg2 := splitHello("i.ytimg.com")
	var at []time.Time
	w.sock.Divert(&sock.Sender{}, func(pkt []byte) bool {
		at = append(at, time.Now())
		return false
	})
	q := &verdictLog{}

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	sent()
	if len(at) != 4 {
		t.Fatalf("%d packets sent, want a fake and a segment for each of the two segments", len(at))
	}
	if gap := at[2].Sub(at[1]); gap < stagger {
		t.Fatalf("segment 2's fake left %v after segment 1, want at least the %v replay stagger", gap, stagger)
	}
}

func TestATUNSplitClientHelloLeavesInOrderFromACopy(t *testing.T) {
	set := newFakingSet(false)
	w, sent := orderedWorker(t, &set)
	w.getConfig().Queue.Mode = "tun"
	fake := sock.GetPayload(&set.Faking)
	hello := helloWithEarlySNI("i.ytimg.com")
	seg1, seg2 := hello[:earlyCut], hello[earlyCut:]
	order := &orderLog{}
	parked, release := parkFirstSend(t, w, order, func(pkt []byte) string {
		return labelSent(pkt, splitSeq, fake, namedPiece{"1", seg1}, namedPiece{"2", seg2})
	})

	if v := w.ProcessPacket(makeV4TCPPacket(seg1, splitSeq)); v != engine.VerdictDrop {
		t.Fatalf("segment 1: got %v, want the set to take it", v)
	}
	waitParked(t, parked)
	buf := makeV4TCPPacket(seg2, splitSeq+earlyCut)
	if v := w.ProcessPacket(buf); v != engine.VerdictDrop {
		t.Fatalf("segment 2 while segment 1 is on its way: got %v, want it held back for the injector", v)
	}
	for i := range buf {
		buf[i] = 0
	}
	release()
	sent()
	if got := order.sends(); !slices.Equal(got, []string{"F@0", "1", "2"}) {
		t.Fatalf("sent %v, want [F@0 1 2] with segment 2's bytes as they were read", got)
	}
}

func TestEarlyDataDoesNotOvertakeAQueuedSegmentTwo(t *testing.T) {
	set := newFakingSet(false)
	w, sent := orderedWorker(t, &set)
	fake := sock.GetPayload(&set.Faking)
	hello := helloWithEarlySNI("i.ytimg.com")
	seg1, seg2 := hello[:earlyCut], hello[earlyCut:]
	early := changeCipherSpecAndFinished()
	order := &orderLog{}
	parked, release := parkFirstSend(t, w, order, func(pkt []byte) string {
		return labelSent(pkt, splitSeq, fake, namedPiece{"1", seg1}, namedPiece{"2", seg2}, namedPiece{"data", early})
	})
	q := orderedVerdicts{order}

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	waitParked(t, parked)
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+earlyCut))
	queueTo(w, q, 3, makeV4TCPPacket(early, splitSeq+uint32(len(hello))))
	whileParked(t, order, "drop 1", "drop 2", "drop 3")
	release()
	sent()
	if got, want := order.sends(), []string{"F@0", "1", "2", "data"}; !slices.Equal(got, want) {
		t.Fatalf("sent %v, want %v", got, want)
	}
}

func TestSegmentTwoOfANonInjectingSetFollowsTheInjectedSegmentOne(t *testing.T) {
	images := newFakingSet(false)
	images.Targets.DomainOnly = true
	port := newPassiveSet()
	port.Id = "port"
	port.Name = "Port"
	port.Targets.DomainOnly = false
	port.Targets.DomainsToMatch = nil
	port.TCP.DPortFilter = "443"
	w, sent := orderedWorker(t, &images, &port)
	fake := sock.GetPayload(&images.Faking)
	hello := helloWithEarlySNI("i.ytimg.com")
	seg1, seg2 := hello[:earlyCut], hello[earlyCut:]
	order := &orderLog{}
	parked, release := parkFirstSend(t, w, order, func(pkt []byte) string {
		return labelSent(pkt, splitSeq, fake, namedPiece{"1", seg1}, namedPiece{"2", seg2})
	})
	q := orderedVerdicts{order}

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	waitParked(t, parked)
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+earlyCut))
	whileParked(t, order, "drop 1", "drop 2")
	release()
	sent()
	if got, want := order.sends(), []string{"F@0", "1", "2"}; !slices.Equal(got, want) {
		t.Fatalf("sent %v, want %v: segment 2 matches the port set, which injects nothing, and must still follow segment 1", got, want)
	}
}

func TestADuplicatingSetSendsItsCopiesAfterTheQueuedSegmentOne(t *testing.T) {
	set := newFakingSet(false)
	set.Targets.DomainOnly = true
	set.TCP.Duplicate.Enabled = true
	set.TCP.Duplicate.Count = 2
	w, sent := orderedWorker(t, &set)
	fake := sock.GetPayload(&set.Faking)
	hello := helloWithEarlySNI("i.ytimg.com")
	seg1, seg2 := hello[:earlyCut], hello[earlyCut:]
	order := &orderLog{}
	parked, release := parkFirstSend(t, w, order, func(pkt []byte) string {
		return labelSent(pkt, splitSeq, fake, namedPiece{"1", seg1}, namedPiece{"2", seg2})
	})
	q := orderedVerdicts{order}

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	waitParked(t, parked)
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+earlyCut))
	whileParked(t, order, "drop 1", "drop 2")
	release()
	sent()
	if got, want := order.sends(), []string{"F@0", "1", "2", "2"}; !slices.Equal(got, want) {
		t.Fatalf("sent %v, want %v", got, want)
	}
}

func TestSegmentTwoLeavesBeforeThePostDesyncBurst(t *testing.T) {
	set := newFakingSet(false)
	set.TCP.Desync.PostDesync = true
	w, sent := orderedWorker(t, &set)
	fake := sock.GetPayload(&set.Faking)
	hello := helloWithEarlySNI("i.ytimg.com")
	seg1, seg2 := hello[:earlyCut], hello[earlyCut:]
	order := &orderLog{}
	parked, release := parkFirstSend(t, w, order, func(pkt []byte) string {
		if len(pkt) > 33 && pkt[33]&0x05 != 0 {
			return "burst"
		}
		return labelSent(pkt, splitSeq, fake, namedPiece{"1", seg1}, namedPiece{"2", seg2})
	})
	q := orderedVerdicts{order}

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	waitParked(t, parked)
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+earlyCut))
	release()
	sent()
	got := order.sends()
	if len(got) < 4 || !slices.Equal(got[:3], []string{"F@0", "1", "2"}) {
		t.Fatalf("sent %v, want F@0, 1 and 2 ahead of the post-desync packets", got)
	}
	for _, s := range got[3:] {
		if s != "burst" {
			t.Fatalf("sent %v, want only post-desync packets after segment 2", got)
		}
	}
}

func TestDataAfterAHelloThatHasLeftIsAcceptedInPlace(t *testing.T) {
	set := newFakingSet(false)
	w, sent := orderedWorker(t, &set)
	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	q := &verdictLog{}

	queueTo(w, q, 1, makeV4TCPPacket(hello, splitSeq))
	sent()
	queueTo(w, q, 2, makeV4TCPPacket(tlsAppData(64), splitSeq+uint32(len(hello))))
	wantVerdicts(t, q.list(), dropOf(1), acceptOf(2))
	if pkts := sent(); len(pkts) != 0 {
		t.Fatalf("%d packets sent for data after a hello that has left, want none", len(pkts))
	}
}

func TestAConnectionWithoutAStoredHelloIsAcceptedInPlace(t *testing.T) {
	set := newFakingSet(false)
	w, sent := orderedWorker(t, &set)
	w.ProcessPacket(makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 100, 1111, 0x18))
	sent()
	q := &verdictLog{}

	queueTo(w, q, 1, makeV4TCPPacket(tlsAppData(64), splitSeq))
	wantVerdicts(t, q.list(), acceptOf(1))
	if pkts := sent(); len(pkts) != 0 {
		t.Fatalf("%d packets sent for a connection with no hello, want none", len(pkts))
	}
}

func fillInjectBacklog(t *testing.T) {
	t.Helper()
	taken := 0
	for full := false; !full; {
		select {
		case injectInflight <- struct{}{}:
			taken++
		default:
			full = true
		}
	}
	t.Cleanup(func() {
		for ; taken > 0; taken-- {
			<-injectInflight
		}
	})
}

func TestSegmentTwoIsAcceptedInPlaceWhenTheBacklogIsFull(t *testing.T) {
	set := newFakingSet(false)
	w, sent := orderedWorker(t, &set)
	fake := sock.GetPayload(&set.Faking)
	hello := helloWithEarlySNI("i.ytimg.com")
	order := &orderLog{}
	parked, release := parkFirstSend(t, w, order, func(pkt []byte) string {
		return labelSent(pkt, splitSeq, fake, namedPiece{"1", hello[:earlyCut]})
	})
	q := orderedVerdicts{order}

	queueTo(w, q, 1, makeV4TCPPacket(hello[:earlyCut], splitSeq))
	waitParked(t, parked)
	fillInjectBacklog(t)
	queueTo(w, q, 2, makeV4TCPPacket(hello[earlyCut:], splitSeq+earlyCut))
	if got := order.list(); !slices.Equal(got, []string{"drop 1", "accept 2"}) {
		t.Fatalf("with the backlog full: %v, want segment 2 accepted in place as before", got)
	}
	release()
	sent()
}

func TestPostHelloPacketsOfAFragmentingSetDoNotWaitForEachOther(t *testing.T) {
	set := newFakingSet(true)
	set.Fragmentation.Strategy = "combo"
	set.Fragmentation.SeqOverlapBytes = nil
	set.Fragmentation.Combo.FirstDelayMs = 100
	set.Fragmentation.Combo.FirstDelayMsMax = 0
	w, sent := orderedWorker(t, &set)
	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	a, b := tlsAppData(200), tlsAppData(300)
	atA := splitSeq + uint32(len(hello))
	atB := atA + uint32(len(a))
	order := &orderLog{}
	recordSends(w, order, func(pkt []byte) string {
		segs := sentData([][]byte{pkt})
		if len(segs) != 1 {
			return "other"
		}
		switch seq := segs[0].seq; {
		case seq-atA < uint32(len(a)):
			return "a"
		case seq-atB < uint32(len(b)):
			return "b"
		}
		return "hello"
	})
	w.ProcessPacket(makeV4TCPPacket(hello, splitSeq))
	sent()

	for _, p := range []struct {
		data []byte
		at   uint32
	}{{a, atA}, {b, atB}} {
		if v := w.ProcessPacket(makeV4TCPPacket(p.data, p.at)); v != engine.VerdictDrop {
			t.Fatalf("data after the hello: got %v, want the set to take it", v)
		}
	}
	sent()
	got := slices.DeleteFunc(order.sends(), func(s string) bool { return s == "hello" })
	lastIndex := func(name string) int {
		for i := len(got) - 1; i >= 0; i-- {
			if got[i] == name {
				return i
			}
		}
		return -1
	}
	firstA, lastA := slices.Index(got, "a"), lastIndex("a")
	firstB, lastB := slices.Index(got, "b"), lastIndex("b")
	if firstA < 0 || firstB < 0 || firstB > lastA || firstA > lastB {
		t.Fatalf("pieces sent %v: each post-hello packet must start before the other's first delay ends, not wait for it", got)
	}
}

func TestAShortRetransmissionOfSegmentOneQueuesBehindIt(t *testing.T) {
	set := newFakingSet(false)
	w, sent := orderedWorker(t, &set)
	fake := sock.GetPayload(&set.Faking)
	hello := helloWithEarlySNI("i.ytimg.com")
	seg1, short := hello[:earlyCut], hello[:8]
	order := &orderLog{}
	parked, release := parkFirstSend(t, w, order, func(pkt []byte) string {
		return labelSent(pkt, splitSeq, fake, namedPiece{"1", seg1}, namedPiece{"short", short})
	})
	q := orderedVerdicts{order}

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	waitParked(t, parked)
	queueTo(w, q, 2, makeV4TCPPacket(short, splitSeq))
	whileParked(t, order, "drop 1", "drop 2")
	release()
	sent()
	got := order.sends()
	if len(got) < 3 || !slices.Equal(got[:2], []string{"F@0", "1"}) || got[len(got)-1] != "short" {
		t.Fatalf("sent %v, want segment 1 with its fake, then the short retransmission", got)
	}
}
