package nfq

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"github.com/florianl/go-nfqueue"
)

const (
	splitCut = 1396
	splitSeq = uint32(1000)
)

type heldVerdict struct {
	id      uint32
	verdict int
}

func (v heldVerdict) String() string {
	if v.verdict == nfqueue.NfDrop {
		return fmt.Sprintf("drop %d", v.id)
	}
	return fmt.Sprintf("accept %d", v.id)
}

func acceptOf(id uint32) heldVerdict { return heldVerdict{id: id, verdict: nfqueue.NfAccept} }
func dropOf(id uint32) heldVerdict   { return heldVerdict{id: id, verdict: nfqueue.NfDrop} }

type verdictLog struct {
	mu     sync.Mutex
	got    []heldVerdict
	closed bool
}

func (l *verdictLog) SetVerdict(id uint32, verdict int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return errors.New("use of closed file")
	}
	l.got = append(l.got, heldVerdict{id: id, verdict: verdict})
	return nil
}

func (l *verdictLog) list() []heldVerdict {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]heldVerdict(nil), l.got...)
}

func (l *verdictLog) waitFor(n int, d time.Duration) []heldVerdict {
	deadline := time.Now().Add(d)
	for {
		got := l.list()
		if len(got) >= n || time.Now().After(deadline) {
			return got
		}
		time.Sleep(time.Millisecond)
	}
}

func wantVerdicts(t *testing.T, got []heldVerdict, want ...heldVerdict) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("verdicts %v, want %v", got, want)
	}
}

func holdHellosFor(t *testing.T, d time.Duration) {
	t.Helper()
	prev := helloHoldMax
	helloHoldMax = d
	t.Cleanup(func() { helloHoldMax = prev })
}

func holdLocalHellosFor(t *testing.T, d time.Duration) {
	t.Helper()
	prev := helloHoldLocalMax
	helloHoldLocalMax = d
	t.Cleanup(func() { helloHoldLocalMax = prev })
}

func staggerReplays(t *testing.T, d time.Duration) {
	t.Helper()
	prev := helloReplayStagger
	helloReplayStagger = d
	t.Cleanup(func() { helloReplayStagger = prev })
}

func splitHello(host string) (full, seg1, seg2 []byte) {
	full = buildClientHello(host, 1400, 0xAB)
	return full, full[:splitCut], full[splitCut:]
}

func helloWithExtensions(exts ...[]byte) []byte {
	var all []byte
	for _, e := range exts {
		all = append(all, e...)
	}
	body := []byte{0x03, 0x03}
	body = append(body, bytes.Repeat([]byte{0x11}, 32)...)
	body = append(body, 32)
	body = append(body, bytes.Repeat([]byte{0x22}, 32)...)
	body = append(body, 0x00, 0x02, 0x13, 0x01, 0x01, 0x00)
	body = binary.BigEndian.AppendUint16(body, uint16(len(all)))
	body = append(body, all...)
	handshake := append([]byte{0x01, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}, body...)
	record := binary.BigEndian.AppendUint16([]byte{0x16, 0x03, 0x01}, uint16(len(handshake)))
	return append(record, handshake...)
}

func cacheHold(q verdictSetter, id uint32) *helloHold {
	return &helloHold{id: id, q: q}
}

func heldHelloWorker(t *testing.T, sets ...*config.SetConfig) (*Worker, func() [][]byte) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Queue.IPv6Enabled = true
	cfg.Sets = sets
	w := newTestWorker(t, &cfg)
	sent := captureInjected(t, w)
	t.Cleanup(w.pendingHello.ReleaseHolds)
	return w, sent
}

func queueTo(w *Worker, q verdictSetter, id uint32, raw []byte) {
	w.dispatch(&verdictCtx{id: id, q: q}, raw)
}

type sentSegment struct {
	seq     uint32
	payload []byte
	size    int
}

func sentData(pkts [][]byte) []sentSegment {
	var out []sentSegment
	for _, p := range pkts {
		if len(p) == 0 {
			continue
		}
		ihl := 40
		switch p[0] >> 4 {
		case IPv4:
			ihl = int(p[0]&0x0f) * 4
			if len(p) < ihl || p[9] != 6 {
				continue
			}
		case IPv6:
			if len(p) < ihl || p[6] != 6 {
				continue
			}
		default:
			continue
		}
		tcp := p[ihl:]
		if len(tcp) < TCPHeaderMinLen || binary.BigEndian.Uint16(tcp[2:4]) != 443 {
			continue
		}
		off := int(tcp[12]>>4) * 4
		if len(tcp) <= off {
			continue
		}
		out = append(out, sentSegment{seq: binary.BigEndian.Uint32(tcp[4:8]), payload: tcp[off:], size: len(p)})
	}
	return out
}

func coversOnce(t *testing.T, segs []sentSegment, base uint32, record []byte) {
	t.Helper()
	seen := make([]int, len(record))
	for _, s := range segs {
		off := int(s.seq - base)
		if off < 0 || off+len(s.payload) > len(record) {
			t.Fatalf("an injected segment at seq %d with %d bytes lies outside the record at %d", s.seq, len(s.payload), base)
		}
		if !bytes.Equal(record[off:off+len(s.payload)], s.payload) {
			t.Fatalf("the injected segment at seq %d does not carry the record's bytes", s.seq)
		}
		for i := range s.payload {
			seen[off+i]++
		}
	}
	for i, n := range seen {
		if n != 1 {
			t.Fatalf("record byte %d was sent %d times, want exactly once", i, n)
		}
	}
}

func inSegmentOne(s sentSegment, base uint32) bool {
	return s.seq-base < splitCut
}

func newDuplicatingYTImagesSet() config.SetConfig {
	set := newFragmentingYTImagesSet()
	set.Targets.DomainOnly = false
	set.TCP.Duplicate.Enabled = true
	set.TCP.Duplicate.Count = 2
	return set
}

type dropRefusingLog struct {
	verdictLog
	refuse uint32
}

func (l *dropRefusingLog) SetVerdict(id uint32, verdict int) error {
	if id == l.refuse && verdict == nfqueue.NfDrop {
		return errors.New("netlink: no buffer space available")
	}
	return l.verdictLog.SetVerdict(id, verdict)
}

func TestPendingHelloHoldsOnlyAFreshTruncatedHelloStart(t *testing.T) {
	holdHellosFor(t, time.Minute)
	c := newPendingHelloCache()
	t.Cleanup(c.ReleaseHolds)
	q := &verdictLog{}
	full, seg1, seg2 := splitHello("i.ytimg.com")

	for _, tc := range []struct {
		name    string
		key     string
		payload []byte
		hold    *helloHold
	}{
		{"a segment 1 without a candidate", "a", seg1, nil},
		{"a whole ClientHello", "b", full, cacheHold(q, 2)},
		{"a continuation segment", "c", seg2, cacheHold(q, 3)},
	} {
		if res := c.FeedHold(tc.key, splitSeq, tc.payload, tc.hold); res.held != nil {
			t.Errorf("%s was held", tc.name)
		}
	}
	if n := c.holding.Load(); n != 0 {
		t.Fatalf("%d holds attached, want none", n)
	}

	held := cacheHold(q, 4)
	if res := c.FeedHold("d", splitSeq, seg1, held); res.held != held {
		t.Fatal("a fresh segment 1 with a candidate must be held")
	}
	res := c.FeedHold("d", splitSeq, seg1, cacheHold(q, 5))
	if res.held != nil || res.claimed != held {
		t.Fatalf("a retransmitted segment 1 must claim the hold and not be held itself: held %v, claimed %v", res.held, res.claimed)
	}
	res.claimed.verdict(nfqueue.NfAccept)

	if res := c.FeedHold("e", splitSeq, seg1, cacheHold(q, 6)); res.held == nil {
		t.Fatal("a fresh segment 1 with a candidate must be held")
	}
	if res := c.FeedHold("e", splitSeq+50000, seg1, cacheHold(q, 7)); res.held != nil {
		t.Fatal("a second segment 1 on a connection that was already held once must not be held")
	}
	wantVerdicts(t, q.list(), acceptOf(4), acceptOf(6))
	if n := c.holding.Load(); n != 0 {
		t.Fatalf("%d holds attached, want none", n)
	}
}

func TestAHeldHelloIsClaimedOnce(t *testing.T) {
	holdHellosFor(t, time.Minute)
	c := newPendingHelloCache()
	t.Cleanup(c.ReleaseHolds)
	q := &verdictLog{}
	full, seg1, seg2 := splitHello("i.ytimg.com")

	held := cacheHold(q, 1)
	c.FeedHold("k", splitSeq, seg1, held)
	c.FeedHold("other", splitSeq, seg1, cacheHold(q, 2))
	if h := c.TakeHold("missing"); h != nil {
		t.Fatal("a connection without a hold must not claim another connection's hold")
	}

	res := c.FeedHold("k", splitSeq+splitCut, seg2, nil)
	if !res.ok || !bytes.Equal(res.joined, full) {
		t.Fatal("segment 2 must still join the held segment 1")
	}
	if res.claimed != held {
		t.Fatal("the join must claim the hold")
	}
	if h := c.TakeHold("k"); h != nil {
		t.Fatal("a claimed hold must not be claimed a second time")
	}
	if got := q.list(); len(got) != 0 {
		t.Fatalf("claiming a hold must leave its verdict to the claimant, got %v", got)
	}
	if n := c.holding.Load(); n != 1 {
		t.Fatalf("%d holds attached, want the other connection's only", n)
	}
	res.claimed.verdict(nfqueue.NfAccept)
}

func TestAHeldHelloIsReleasedAtTheTimeLimit(t *testing.T) {
	holdHellosFor(t, 20*time.Millisecond)
	c := newPendingHelloCache()
	q := &verdictLog{}
	_, seg1, seg2 := splitHello("i.ytimg.com")

	start := time.Now()
	c.FeedHold("k", splitSeq, seg1, cacheHold(q, 1))
	wantVerdicts(t, q.waitFor(1, 5*time.Second), acceptOf(1))
	if held := time.Since(start); held < 20*time.Millisecond {
		t.Fatalf("released after %s, before the %s limit", held, 20*time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	wantVerdicts(t, q.list(), acceptOf(1))
	if n := c.holding.Load(); n != 0 {
		t.Fatalf("%d holds attached after the release, want none", n)
	}

	joined, _, ok := c.Feed("k", splitSeq+splitCut, seg2)
	if !ok {
		t.Fatal("segment 2 must still join after the time limit released segment 1")
	}
	if host, _, _ := sni.ParseTLSClientHelloSNI(joined); host != "i.ytimg.com" {
		t.Fatalf("recovered SNI %q, want i.ytimg.com", host)
	}
}

func TestTheTimerAndAClaimSettleAHeldHelloOnce(t *testing.T) {
	holdHellosFor(t, time.Millisecond)
	c := newPendingHelloCache()
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")

	const rounds = 1000
	claimed := 0
	for i := 0; i < rounds; i++ {
		key := fmt.Sprintf("flow-%d", i)
		c.FeedHold(key, splitSeq, seg1, cacheHold(q, uint32(i)))
		time.Sleep(time.Duration(i%5) * 400 * time.Microsecond)
		if h := c.TakeHold(key); h != nil {
			h.verdict(nfqueue.NfDrop)
			claimed++
		}
	}

	got := q.waitFor(rounds, 5*time.Second)
	counts := make(map[uint32]int, rounds)
	drops := 0
	for _, v := range got {
		counts[v.id]++
		if v.verdict == nfqueue.NfDrop {
			drops++
		}
	}
	for i := 0; i < rounds; i++ {
		if counts[uint32(i)] != 1 {
			t.Fatalf("packet %d got %d verdicts, want exactly one", i, counts[uint32(i)])
		}
	}
	if drops != claimed {
		t.Fatalf("%d claimed holds but %d drop verdicts", claimed, drops)
	}
	if n := c.holding.Load(); n != 0 {
		t.Fatalf("%d holds still counted after every hold was settled", n)
	}
}

func TestEvictionNeverRemovesAHeldHello(t *testing.T) {
	holdHellosFor(t, time.Minute)
	c := newPendingHelloCache()
	t.Cleanup(c.ReleaseHolds)
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")

	held := cacheHold(q, 1)
	c.FeedHold("held", splitSeq, seg1, held)
	for i := 0; i < maxPendingHelloEntries+64; i++ {
		c.FeedHold(fmt.Sprintf("flow-%d", i), uint32(i), seg1, nil)
	}
	c.mu.Lock()
	entry := c.flows["held"]
	c.mu.Unlock()
	if entry == nil || entry.hold != held {
		t.Fatal("eviction removed the held segment 1, the oldest entry")
	}
	if got := q.list(); len(got) != 0 {
		t.Fatalf("eviction gave the held segment 1 a verdict: %v", got)
	}

	full := newPendingHelloCache()
	full.mu.Lock()
	for i := 0; i < maxPendingHelloEntries; i++ {
		full.flows[fmt.Sprintf("held-%d", i)] = &pendingHello{data: seg1[:8], storedAt: time.Now().Add(-time.Hour), hold: cacheHold(q, 2)}
	}
	full.mu.Unlock()
	res := full.FeedHold("new", splitSeq, seg1, cacheHold(q, 3))
	full.mu.Lock()
	_, stored := full.flows["new"]
	left := len(full.flows)
	full.flows = make(map[string]*pendingHello)
	full.mu.Unlock()
	if res.held != nil || stored {
		t.Fatal("with every slot held, a new segment 1 must be neither stored nor held")
	}
	if left != maxPendingHelloEntries {
		t.Fatalf("%d entries left, want all %d held ones", left, maxPendingHelloEntries)
	}
}

func TestHeldHellosAreCapped(t *testing.T) {
	holdHellosFor(t, time.Minute)
	c := newPendingHelloCache()
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")

	for i := 0; i < maxHeldHellos; i++ {
		if res := c.FeedHold(fmt.Sprintf("flow-%d", i), splitSeq, seg1, cacheHold(q, uint32(i))); res.held == nil {
			t.Fatalf("hold %d of %d was refused", i+1, maxHeldHellos)
		}
	}
	if res := c.FeedHold("one-more", splitSeq, seg1, cacheHold(q, maxHeldHellos)); res.held != nil {
		t.Fatalf("hold %d was attached above the cap of %d", maxHeldHellos+1, maxHeldHellos)
	}
	c.ReleaseHolds()
	got := q.list()
	if len(got) != maxHeldHellos {
		t.Fatalf("%d verdicts at shutdown, want %d", len(got), maxHeldHellos)
	}
	for _, v := range got {
		if v.verdict != nfqueue.NfAccept || v.id >= maxHeldHellos {
			t.Fatalf("unexpected verdict at shutdown: %v", v)
		}
	}
}

func TestASegmentOneIsHeldOncePerConnection(t *testing.T) {
	holdHellosFor(t, time.Minute)
	c := newPendingHelloCache()
	t.Cleanup(c.ReleaseHolds)
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")

	c.FeedHold("k", splitSeq, seg1, cacheHold(q, 1))
	c.TakeHold("k").verdict(nfqueue.NfAccept)
	c.Drop("k")
	if res := c.FeedHold("k", splitSeq, seg1, cacheHold(q, 2)); res.held != nil {
		t.Fatal("a connection was held a second time within the held-once window")
	}

	c.Drop("k")
	c.ForgetHeld("k")
	if res := c.FeedHold("k", splitSeq, seg1, cacheHold(q, 3)); res.held == nil {
		t.Fatal("a new SYN on the tuple must allow one hold again")
	}
	c.TakeHold("k").verdict(nfqueue.NfAccept)

	c.mu.Lock()
	c.heldOnce["k"] = time.Now().Add(-heldOnceTTL - time.Second)
	c.mu.Unlock()
	c.Cleanup()
	if n := c.onceLen.Load(); n != 0 {
		t.Fatalf("%d held-once marks left after Cleanup, want the stale one pruned", n)
	}

	crowded := newPendingHelloCache()
	crowded.mu.Lock()
	crowded.heldOnce = make(map[string]time.Time, maxHeldOnce)
	for i := 0; i < maxHeldOnce; i++ {
		crowded.heldOnce[fmt.Sprintf("old-%d", i)] = time.Now()
	}
	crowded.onceLen.Store(maxHeldOnce)
	crowded.mu.Unlock()
	if res := crowded.FeedHold("new", splitSeq, seg1, cacheHold(q, 4)); res.held != nil {
		t.Fatalf("a full table of %d held-once marks must skip the hold", maxHeldOnce)
	}
}

func TestForgetHeldSkipsTheLockWhileNothingWasHeld(t *testing.T) {
	c := newPendingHelloCache()
	c.mu.Lock()
	done := make(chan struct{})
	go func() {
		c.ForgetHeld("10.0.0.1:12345->1.2.3.4:443")
		close(done)
	}()
	select {
	case <-done:
		c.mu.Unlock()
	case <-time.After(time.Second):
		c.mu.Unlock()
		<-done
		t.Fatal("a SYN waited for the cache lock while no connection had ever been held")
	}
}

func TestReleaseHoldsAcceptsEveryHeldHelloOnce(t *testing.T) {
	holdHellosFor(t, time.Minute)
	c := newPendingHelloCache()
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")

	for i := uint32(1); i <= 3; i++ {
		c.FeedHold(fmt.Sprintf("flow-%d", i), splitSeq, seg1, cacheHold(q, i))
	}
	c.ReleaseHolds()
	got := q.list()
	slices.SortFunc(got, func(a, b heldVerdict) int { return int(a.id) - int(b.id) })
	wantVerdicts(t, got, acceptOf(1), acceptOf(2), acceptOf(3))

	if res := c.FeedHold("late", splitSeq, seg1, cacheHold(q, 4)); res.held != nil {
		t.Fatal("a hold attached after shutdown started")
	}
	c.ReleaseHolds()
	if n := len(q.list()); n != 3 {
		t.Fatalf("a second release gave %d more verdicts", n-3)
	}

	var none *pendingHelloCache
	none.ReleaseHolds()
	none.ForgetHeld("k")
	if none.TakeHold("k") != nil || none.withdraw("k", cacheHold(q, 5)) {
		t.Fatal("a missing cache holds nothing")
	}
}

func TestWithdrawDetachesOnlyItsOwnHold(t *testing.T) {
	holdHellosFor(t, time.Minute)
	c := newPendingHelloCache()
	t.Cleanup(c.ReleaseHolds)
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")

	held := cacheHold(q, 1)
	c.FeedHold("k", splitSeq, seg1, held)
	if c.withdraw("k", cacheHold(q, 2)) {
		t.Fatal("withdrew a hold that is not the attached one")
	}
	if c.withdraw("other", held) {
		t.Fatal("withdrew a hold from the wrong connection")
	}
	if !c.withdraw("k", held) {
		t.Fatal("the attached hold could not be withdrawn")
	}
	if c.withdraw("k", held) {
		t.Fatal("a hold was withdrawn twice")
	}
	if n := c.holding.Load(); n != 0 {
		t.Fatalf("%d holds attached after the withdrawal, want none", n)
	}
	if got := q.list(); len(got) != 0 {
		t.Fatalf("withdrawing a hold must not give it a verdict, got %v", got)
	}
}

func TestFeedReleasesAClaimedHoldAheadOfTheNextSegment(t *testing.T) {
	holdHellosFor(t, time.Minute)
	c := newPendingHelloCache()
	q := &verdictLog{}
	full, seg1, seg2 := splitHello("i.ytimg.com")

	c.FeedHold("k", splitSeq, seg1, cacheHold(q, 1))
	joined, _, ok := c.Feed("k", splitSeq+splitCut, seg2)
	if !ok || !bytes.Equal(joined, full) {
		t.Fatal("segment 2 must join the held segment 1")
	}
	wantVerdicts(t, q.list(), acceptOf(1))
}

func TestAHeldHelloOnAClosedQueueExpiresQuietly(t *testing.T) {
	holdHellosFor(t, 5*time.Millisecond)
	buf := &syncBuffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	log.StartCapture(buf)
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
	})
	c := newPendingHelloCache()
	_, seg1, _ := splitHello("i.ytimg.com")

	c.FeedHold("k", splitSeq, seg1, cacheHold(&verdictLog{closed: true}, 1))
	deadline := time.Now().Add(5 * time.Second)
	for c.holding.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the hold was never released")
		}
		time.Sleep(time.Millisecond)
	}
	log.Flush()
	if out := buf.String(); strings.Contains(out, "held") || strings.Contains(out, "verdict") {
		t.Fatalf("a verdict refused by a closed queue must stay at trace level, logged %q", out)
	}
}

func TestStoppingAWorkerReleasesItsHeldHellos(t *testing.T) {
	holdHellosFor(t, time.Minute)
	set := newFragmentingYTImagesSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}
	w := newTestWorker(t, &cfg)
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must be held", got)
	}
	w.Stop()
	wantVerdicts(t, q.list(), acceptOf(1))

	queueTo(w, q, 2, makeV4TCPPacketFromPort(seg1, 5000, 23456, 0x18))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
}

func TestReleasingThePoolsHoldsReleasesHeldHellos(t *testing.T) {
	holdHellosFor(t, time.Minute)
	set := newFragmentingYTImagesSet()
	w, _ := heldHelloWorker(t, &set)
	p := &Pool{Workers: []*Worker{w}, state: &runtimeState{pendingHello: w.pendingHello}}
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	p.ReleaseHolds()
	wantVerdicts(t, q.list(), acceptOf(1))
}

func TestSplitHelloOfADomainOnlySetIsReplayedFromSegmentOne(t *testing.T) {
	staggerReplays(t, 100*time.Millisecond)
	set := newFragmentingYTImagesSet()
	w, sent := heldHelloWorker(t, &set)
	q := &verdictLog{}
	full, seg1, seg2 := splitHello("i.ytimg.com")

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
	}
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), dropOf(2), dropOf(1))

	segs := sentData(sent())
	coversOnce(t, segs, splitSeq, full)
	lastOne, firstTwo := -1, len(segs)
	for i, s := range segs {
		limit := 40 + len(seg2)
		if inSegmentOne(s, splitSeq) {
			limit = 40 + len(seg1)
			lastOne = i
			if s.seq-splitSeq+uint32(len(s.payload)) > splitCut {
				t.Fatalf("an injected piece at seq %d crosses the client's segment boundary", s.seq)
			}
		} else if i < firstTwo {
			firstTwo = i
		}
		if s.size > limit {
			t.Fatalf("an injected packet of %d bytes is longer than the %d-byte segment it came from", s.size, limit)
		}
	}
	if lastOne < 0 || lastOne > firstTwo {
		t.Fatalf("segment 1's pieces must leave before segment 2's: last of segment 1 at %d, first of segment 2 at %d", lastOne, firstTwo)
	}
	if n := w.pendingHello.holding.Load(); n != 0 {
		t.Fatalf("%d holds left, want none", n)
	}
}

func TestSplitHelloOfADomainOnlySetIsReplayedOverIPv6(t *testing.T) {
	set := newFragmentingYTImagesSet()
	w, sent := heldHelloWorker(t, &set)
	q := &verdictLog{}
	full, seg1, seg2 := splitHello("i.ytimg.com")

	queueTo(w, q, 1, makeV6TCPPacket(seg1, splitSeq))
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
	}
	queueTo(w, q, 2, makeV6TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), dropOf(2), dropOf(1))
	coversOnce(t, sentData(sent()), splitSeq, full)
}

func TestSplitHelloThatMatchesNoSetLeavesInOrder(t *testing.T) {
	set := newFragmentingYTImagesSet()
	w, sent := heldHelloWorker(t, &set)
	q := &verdictLog{}
	_, seg1, seg2 := splitHello("www.example.org")

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
	}
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
	if segs := sentData(sent()); len(segs) != 0 {
		t.Fatalf("%d segments injected for a hello no set matches", len(segs))
	}
}

func TestSplitHelloWithoutANameIsReleasedByItsSecondSegment(t *testing.T) {
	set := newFragmentingYTImagesSet()
	w, _ := heldHelloWorker(t, &set)
	q := &verdictLog{}
	full := helloWithExtensions(buildExtension(21, bytes.Repeat([]byte{0xAB}, 1400)), buildExtension(43, []byte{0x02, 0x03, 0x04}))

	queueTo(w, q, 1, makeV4TCPPacket(full[:splitCut], splitSeq))
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
	}
	queueTo(w, q, 2, makeV4TCPPacket(full[splitCut:], splitSeq+splitCut))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
}

func TestHeldHelloIsReleasedAheadOfTheConnectionsNextPacket(t *testing.T) {
	for _, tc := range []struct {
		name string
		next func(seg1 []byte) []byte
	}{
		{"a retransmitted segment 1", func(seg1 []byte) []byte { return makeV4TCPPacket(seg1, splitSeq) }},
		{"a FIN", func([]byte) []byte { return makeV4TCPPacketFlags(nil, splitSeq+splitCut, 0x11) }},
		{"a bare ACK", func([]byte) []byte { return makeV4TCPPacketFlags(nil, splitSeq+splitCut, 0x10) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := newFragmentingYTImagesSet()
			w, _ := heldHelloWorker(t, &set)
			q := &verdictLog{}
			_, seg1, _ := splitHello("i.ytimg.com")

			queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
			queueTo(w, q, 2, tc.next(seg1))
			wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
			if n := w.pendingHello.holding.Load(); n != 0 {
				t.Fatalf("%d holds left, want none", n)
			}
		})
	}
}

func TestANewSYNOnTheTupleReleasesAStaleHoldAndAllowsANewOne(t *testing.T) {
	set := newFragmentingYTImagesSet()
	w, _ := heldHelloWorker(t, &set)
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	queueTo(w, q, 2, makeV4TCPPacketFlags(nil, 90000, 0x02))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))

	queueTo(w, q, 3, makeV4TCPPacket(seg1, 90001))
	if got := q.list()[2:]; len(got) != 0 {
		t.Fatalf("segment 1 of the new connection on the tuple got %v at once, it must be held", got)
	}
}

func TestAPacketThatIsItselfHeldGetsExactlyOneVerdict(t *testing.T) {
	holdHellosFor(t, time.Minute)
	c := newPendingHelloCache()
	t.Cleanup(c.ReleaseHolds)
	q := &verdictLog{}
	_, seg1, _ := splitHello("i.ytimg.com")

	held := cacheHold(q, 1)
	c.FeedHold("k", splitSeq, seg1, held)
	vc := &verdictCtx{id: 1, q: q, hellos: c, helloKey: "k", own: held}
	vc.accept()
	wantVerdicts(t, q.list(), acceptOf(1))
	if n := c.holding.Load(); n != 0 {
		t.Fatalf("%d holds attached after the packet took its own verdict, want none", n)
	}

	c.Drop("k")
	c.ForgetHeld("k")
	again := cacheHold(q, 2)
	c.FeedHold("k", splitSeq, seg1, again)
	c.ReleaseHolds()
	late := &verdictCtx{id: 2, q: q, hellos: c, helloKey: "k", own: again}
	if late.drop() {
		t.Fatal("a packet the release already accepted cannot be dropped")
	}
	late.accept()
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
}

func TestHeldHelloIsDroppedWithAHelloTheSetRefuses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		set   func() config.SetConfig
		setup func(w *Worker)
	}{
		{"a block set", func() config.SetConfig {
			set := newPassiveSet()
			set.Routing.Enabled = true
			set.Routing.Mode = config.RoutingModeBlock
			return set
		}, func(*Worker) {}},
		{"an address cached as blocked", func() config.SetConfig {
			set := newFragmentingYTImagesSet()
			set.TCP.IPBlockDetect.Enabled = true
			set.TCP.IPBlockDetect.CacheBlockedIPs = true
			return set
		}, func(w *Worker) { w.destState.AddBlocked("1.2.3.4:443") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := tc.set()
			w, _ := heldHelloWorker(t, &set)
			tc.setup(w)
			q := &verdictLog{}
			_, seg1, seg2 := splitHello("i.ytimg.com")

			queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
			if got := q.list(); len(got) != 0 {
				t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
			}
			queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
			wantVerdicts(t, q.list(), dropOf(1), dropOf(2))
		})
	}
}

func TestHeldHelloIsDroppedWhenItsDestinationStallsPastTheThreshold(t *testing.T) {
	set := newFragmentingYTImagesSet()
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.RetransmitThreshold = 3
	w, sent := heldHelloWorker(t, &set)
	q := &verdictLog{}
	_, seg1, seg2 := splitHello("i.ytimg.com")
	key := fmt.Sprintf(connKeyFormat, "10.0.0.1", 12345, "1.2.3.4", 443)
	for i := 1; i < set.TCP.IPBlockDetect.RetransmitThreshold; i++ {
		w.destState.RecordClientHello(key, "i.ytimg.com")
	}

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
	}
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), dropOf(1), dropOf(2))
	if segs := sentData(sent()); len(segs) != 0 {
		t.Fatalf("%d segments sent toward the server at the stall threshold", len(segs))
	}
	if !w.destState.HasRSTSent(key) {
		t.Fatal("the stall threshold did not reset the client, so this did not take the stall exit")
	}
}

func TestHeldHelloIsAcceptedFirstWhenItsSetLeavesTheHelloAlone(t *testing.T) {
	passive := newPassiveSet()
	other := newFragmentingYTImagesSet()
	other.Id, other.Name = "other", "Other"
	other.Targets.DomainsToMatch = []string{"other.example"}
	w, sent := heldHelloWorker(t, &passive, &other)
	lines := connectionLines(t)
	q := &verdictLog{}
	_, seg1, seg2 := splitHello("i.ytimg.com")

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
	}
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
	if segs := sentData(sent()); len(segs) != 0 {
		t.Fatalf("%d segments injected for a set that does not inject", len(segs))
	}
	if seen := drainLines(lines); rowsFor(seen, passive.Name) != 1 {
		t.Fatalf("segment 2 must match %q by its name and leave through that set's accept, got %v", passive.Name, seen)
	}
}

func TestDuplicatesOfAHeldHelloLeaveFirst(t *testing.T) {
	set := newDuplicatingYTImagesSet()
	w, sent := heldHelloWorker(t, &set)
	q := &verdictLog{}
	_, seg1, seg2 := splitHello("i.ytimg.com")

	first := makeV4TCPPacket(seg1, splitSeq)
	queueTo(w, q, 1, first)
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
	}
	w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "i.ytimg.com", &set)
	second := makeV4TCPPacket(seg2, splitSeq+splitCut)
	queueTo(w, q, 2, second)
	wantVerdicts(t, q.list(), dropOf(2), dropOf(1))

	got := sent()
	want := [][]byte{first, first, second, second}
	if len(got) != len(want) {
		t.Fatalf("%d packets sent, want %d copies", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("copy %d is not the expected segment: the copies of segment 1 must leave before those of segment 2", i)
		}
	}
}

func TestHeldHelloLeavesUnchangedWhenTheInjectionBacklogIsFull(t *testing.T) {
	set := newFragmentingYTImagesSet()
	w, sent := heldHelloWorker(t, &set)
	q := &verdictLog{}
	_, seg1, seg2 := splitHello("i.ytimg.com")

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
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
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
	if segs := sentData(sent()); len(segs) != 0 {
		t.Fatalf("%d segments injected with a full backlog", len(segs))
	}
}

func TestAPartialInjectionAcquisitionGivesItsSlotBack(t *testing.T) {
	taken := 0
	for len(injectInflight) < cap(injectInflight)-1 {
		injectInflight <- struct{}{}
		taken++
	}
	t.Cleanup(func() {
		for ; taken > 0; taken-- {
			<-injectInflight
		}
	})
	if injectAcquireN(2) {
		injectReleaseN(2)
		t.Fatal("two inject slots were acquired while only one was free")
	}
	if n, want := len(injectInflight), cap(injectInflight)-1; n != want {
		t.Fatalf("%d inject slots taken after the failed acquisition, want %d: the slot it took must be given back", n, want)
	}
}

func TestHeldHelloIsAcceptedWhenSegmentTwoCannotBeDropped(t *testing.T) {
	set := newFragmentingYTImagesSet()
	w, sent := heldHelloWorker(t, &set)
	q := &dropRefusingLog{refuse: 2}
	_, seg1, seg2 := splitHello("i.ytimg.com")
	slots := len(injectInflight)

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
	}
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), acceptOf(1))
	if segs := sentData(sent()); len(segs) != 0 {
		t.Fatalf("%d segments injected although segment 2 could not be dropped", len(segs))
	}
	if n := len(injectInflight); n != slots {
		t.Fatalf("%d inject slots taken after the refused drop, want %d", n, slots)
	}
	if n := w.pendingHello.holding.Load(); n != 0 {
		t.Fatalf("%d holds left, want none", n)
	}
}

func TestHeldHelloIsAcceptedWhenADuplicatedSegmentTwoCannotBeDropped(t *testing.T) {
	set := newDuplicatingYTImagesSet()
	w, sent := heldHelloWorker(t, &set)
	q := &dropRefusingLog{refuse: 2}
	_, seg1, seg2 := splitHello("i.ytimg.com")

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
	}
	w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "i.ytimg.com", &set)
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), acceptOf(1))
	if got := sent(); len(got) != 0 {
		t.Fatalf("%d copies sent although segment 2 could not be dropped", len(got))
	}
	if n := w.pendingHello.holding.Load(); n != 0 {
		t.Fatalf("%d holds left, want none", n)
	}
}

func TestSegmentOneThatIsNotHeldGetsItsVerdictAtOnce(t *testing.T) {
	sniFirst := helloWithExtensions(buildSNIExtension("i.ytimg.com"), buildExtension(21, bytes.Repeat([]byte{0xAB}, 1400)), buildExtension(43, []byte{0x02, 0x03, 0x04}))
	_, seg1, _ := splitHello("i.ytimg.com")
	threeSegments := buildClientHello("i.ytimg.com", 2900, 0xCD)
	for _, tc := range []struct {
		name   string
		mutate func(set *config.SetConfig)
		raw    []byte
	}{
		{"the name in segment 1", nil, makeV4TCPPacket(sniFirst[:splitCut], splitSeq)},
		{"a hello that needs three segments", nil, makeV4TCPPacket(threeSegments[:splitCut], splitSeq)},
		{"a TCP Fast Open SYN", nil, makeV4TCPPacketFlags(seg1, splitSeq, 0x02)},
		{"an address target", func(set *config.SetConfig) { set.Targets.IpsToMatch = []string{"1.2.3.4"} }, makeV4TCPPacket(seg1, splitSeq)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := newFragmentingYTImagesSet()
			if tc.mutate != nil {
				tc.mutate(&set)
			}
			w, _ := heldHelloWorker(t, &set)
			q := &verdictLog{}
			queueTo(w, q, 1, tc.raw)
			if got := q.list(); len(got) != 1 || got[0].id != 1 {
				t.Fatalf("verdicts %v, want one for packet 1 while it was handled", got)
			}
			if n := w.pendingHello.holding.Load(); n != 0 {
				t.Fatalf("%d holds attached, want none", n)
			}
			if n := w.pendingHello.onceLen.Load(); n != 0 {
				t.Fatalf("segment 1 was attached as a hold %d times, want never", n)
			}
		})
	}
}

func TestAConnectionWhoseNameIsKnownIsNotHeld(t *testing.T) {
	set := newFragmentingYTImagesSet()
	w, _ := heldHelloWorker(t, &set)
	q := &verdictLog{}
	whole := buildClientHello("www.example.org", 16, 0xAB)
	_, seg1, _ := splitHello("www.example.org")

	queueTo(w, q, 1, makeV4TCPPacket(whole, splitSeq))
	queueTo(w, q, 2, makeV4TCPPacket(seg1, splitSeq+uint32(len(whole))))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
	if n := w.pendingHello.onceLen.Load(); n != 0 {
		t.Fatalf("segment 1 of a second ClientHello was attached as a hold %d times, want never", n)
	}
}

func TestOnlyASetThatActsOnTheHelloHoldsSegmentOne(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(set *config.SetConfig)
		held   bool
	}{
		{"a name set that injects", func(*config.SetConfig) {}, true},
		{"a block set", func(set *config.SetConfig) {
			*set = newPassiveSet()
			set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeBlock
		}, true},
		{"a name set that detects IP blocks", func(set *config.SetConfig) {
			*set = newPassiveSet()
			set.TCP.IPBlockDetect.Enabled = true
		}, true},
		{"a name set that only duplicates", func(set *config.SetConfig) {
			*set = newPassiveSet()
			set.TCP.Duplicate.Enabled = true
		}, false},
		{"a proxied name set that escalates", func(set *config.SetConfig) {
			set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeProxy
			set.Escalate.To = "next"
		}, true},
		{"a name set that does nothing to TCP", func(set *config.SetConfig) { *set = newPassiveSet() }, false},
		{"a proxied name set", func(set *config.SetConfig) {
			set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeProxy
		}, false},
		{"a name set on another TCP port", func(set *config.SetConfig) {
			set.TCP.DPortFilter = "8443"
			set.TCPPortRanges = config.ParsePortRanges("8443")
		}, false},
		{"an address-only set", func(set *config.SetConfig) {
			set.Targets.DomainsToMatch = nil
			set.Targets.IpsToMatch = []string{"9.9.9.9"}
		}, false},
		{"a disabled name set", func(set *config.SetConfig) { set.Enabled = false }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := newFragmentingYTImagesSet()
			tc.mutate(&set)
			w, _ := heldHelloWorker(t, &set)
			q := &verdictLog{}
			_, seg1, _ := splitHello("www.example.org")

			queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
			if held := len(q.list()) == 0; held != tc.held {
				t.Fatalf("segment 1 held: %v, want %v", held, tc.held)
			}
		})
	}
}

func TestADomainOnlySetHoldsEverySplitHelloToTheSameAddress(t *testing.T) {
	for _, tc := range []struct {
		name       string
		domainOnly bool
		heldAgain  bool
	}{
		{"domain only", true, true},
		{"learning", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := newFragmentingYTImagesSet()
			set.Targets.DomainOnly = tc.domainOnly
			w, _ := heldHelloWorker(t, &set)
			q := &verdictLog{}
			_, seg1, seg2 := splitHello("i.ytimg.com")

			queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
			queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
			wantVerdicts(t, q.list(), dropOf(2), dropOf(1))

			const otherSeq = uint32(7000)
			queueTo(w, q, 3, makeV4TCPPacketFromPort(seg1, otherSeq, 23456, 0x18))
			if !tc.heldAgain {
				wantVerdicts(t, q.list()[2:], dropOf(3))
				return
			}
			if got := q.list()[2:]; len(got) != 0 {
				t.Fatalf("a second connection to the same address got %v at once; a domain_only set never learns the address, so its segment 1 must be held again", got)
			}
			queueTo(w, q, 4, makeV4TCPPacketFromPort(seg2, otherSeq+splitCut, 23456, 0x18))
			wantVerdicts(t, q.list()[2:], dropOf(4), dropOf(3))
		})
	}
}

func TestHeldHelloIsReleasedAtTheTimeLimitAndSegmentTwoIsProcessedAlone(t *testing.T) {
	holdHellosFor(t, 30*time.Millisecond)
	set := newFragmentingYTImagesSet()
	w, sent := heldHelloWorker(t, &set)
	q := &verdictLog{}
	_, seg1, seg2 := splitHello("i.ytimg.com")

	start := time.Now()
	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	wantVerdicts(t, q.waitFor(1, 5*time.Second), acceptOf(1))
	if held := time.Since(start); held < 30*time.Millisecond {
		t.Fatalf("released after %s, before the %s limit", held, 30*time.Millisecond)
	}

	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), acceptOf(1), dropOf(2))
	coversOnce(t, sentData(sent()), splitSeq+splitCut, seg2)
}

func TestASegmentOneTheRouterSendsItselfIsHeldOnlyBriefly(t *testing.T) {
	holdHellosFor(t, time.Minute)
	holdLocalHellosFor(t, 20*time.Millisecond)
	for _, tc := range []struct {
		name  string
		vc    verdictCtx
		local bool
	}{
		{"a packet from a socket on the router", verdictCtx{localSocket: true, hook: nfHookPostRouting, hooked: true}, true},
		{"a packet in the router's output hook", verdictCtx{hook: nfHookLocalOut, hooked: true}, true},
		{"a forwarded packet", verdictCtx{hook: nfHookPostRouting, hooked: true, inDev: 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := newFragmentingYTImagesSet()
			w, _ := heldHelloWorker(t, &set)
			q := &verdictLog{}
			_, seg1, _ := splitHello("i.ytimg.com")

			vc := tc.vc
			vc.id, vc.q = 1, q
			start := time.Now()
			w.dispatch(&vc, makeV4TCPPacket(seg1, splitSeq))
			if got := q.list(); len(got) != 0 {
				t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
			}
			if !tc.local {
				time.Sleep(3 * helloHoldLocalMax)
				if got := q.list(); len(got) != 0 {
					t.Fatalf("a forwarded segment 1 got %v after %s, it must wait up to %s for segment 2", got, 3*helloHoldLocalMax, helloHoldMax)
				}
				return
			}
			wantVerdicts(t, q.waitFor(1, 5*time.Second), acceptOf(1))
			if held := time.Since(start); held < helloHoldLocalMax {
				t.Fatalf("released after %s, before the %s limit", held, helloHoldLocalMax)
			}
		})
	}
}

func TestTUNNeverHoldsASplitHello(t *testing.T) {
	set := newFragmentingYTImagesSet()
	w, _ := heldHelloWorker(t, &set)
	_, seg1, _ := splitHello("i.ytimg.com")

	if v := w.ProcessPacket(makeV4TCPPacket(seg1, splitSeq)); v != engine.VerdictAccept {
		t.Fatalf("segment 1 without a queue: verdict %v, want accept", v)
	}
	if n := w.pendingHello.holding.Load(); n != 0 {
		t.Fatalf("%d holds attached without a queue, want none", n)
	}
}

func injectingSilenceSet() config.SetConfig {
	set := newFragmentingYTImagesSet()
	set.TCP.RSTProtection.Enabled = false
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.CacheBlockedIPs = true
	return set
}

func resetSeqs(sent [][]byte) []uint32 {
	var seqs []uint32
	for _, p := range sent {
		if len(p) >= 40 && p[0]>>4 == IPv4 && p[33]&0x04 != 0 {
			seqs = append(seqs, binary.BigEndian.Uint32(p[24:28]))
		}
	}
	return seqs
}

func TestAHeldSplitHelloIsWatchedForASilentServerLikeAWholeOne(t *testing.T) {
	holdHellosFor(t, time.Minute)
	for _, tc := range []struct {
		name    string
		set     func() config.SetConfig
		replays bool
	}{
		{"a set that leaves the hello alone", silenceSet, false},
		{"a set that injects", injectingSilenceSet, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := tc.set()
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)
			t.Cleanup(r.w.pendingHello.ReleaseHolds)
			q := &verdictLog{}
			full, seg1, seg2 := splitHello("i.ytimg.com")

			queueTo(r.w, q, 1, clientPacket(12345, splitSeq, 0x18, seg1))
			if got := q.list(); len(got) != 0 {
				t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
			}
			queueTo(r.w, q, 2, clientPacket(12345, splitSeq+splitCut, 0x18, seg2))
			if tc.replays {
				wantVerdicts(t, q.list(), dropOf(2), dropOf(1))
			} else {
				wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
			}
			if !r.w.destState.Watching() {
				t.Fatal("a split ClientHello whose segment 1 was held must be watched for a silent server like a whole one")
			}

			r.sweepLater()
			if keys := r.probed(); !slices.Equal(keys, []string{"1.2.3.4:443"}) {
				t.Fatalf("the address a held split ClientHello got no answer from must be checked with neutral names once, probed %v", keys)
			}
			if !r.w.bans.reg.Banned(net.ParseIP("1.2.3.4"), 443) {
				t.Fatal("a silent neutral-name check must ban the address of a held split ClientHello")
			}
			sent := r.sent()
			if tc.replays {
				coversOnce(t, sentData(sent), splitSeq, full)
			} else if segs := sentData(sent); len(segs) != 0 {
				t.Fatalf("%d segments injected for a set that does not inject", len(segs))
			}
			if ports := resetsTo(sent); !slices.Equal(ports, []uint16{12345}) {
				t.Fatalf("the waiting connection must get one reset once the ban is in place, got resets to %v", ports)
			}
			if seqs := resetSeqs(sent); !slices.Equal(seqs, []uint32{silentServerAck}) {
				t.Fatalf("the reset must carry the sequence number the client expects from the server, got %#x want %#x", seqs, silentServerAck)
			}
			if r.w.destState.Watching() {
				t.Error("a settled connection must not stay watched")
			}
		})
	}
}

func TestAHeldSplitHelloToABannedAddressIsDroppedWithTheHello(t *testing.T) {
	holdHellosFor(t, time.Minute)
	set := injectingSilenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)
	t.Cleanup(r.w.pendingHello.ReleaseHolds)
	banAddress(t, r.w.bans, "1.2.3.4", 443)
	q := &verdictLog{}
	_, seg1, seg2 := splitHello("i.ytimg.com")

	queueTo(r.w, q, 1, clientPacket(12345, splitSeq, 0x18, seg1))
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
	}
	queueTo(r.w, q, 2, clientPacket(12345, splitSeq+splitCut, 0x18, seg2))
	wantVerdicts(t, q.list(), dropOf(1), dropOf(2))

	sent := r.sent()
	if segs := sentData(sent); len(segs) != 0 {
		t.Fatalf("%d segments sent toward a banned address", len(segs))
	}
	if ports := resetsTo(sent); !slices.Equal(ports, []uint16{12345}) {
		t.Fatalf("the client must get one reset for a ClientHello to a banned address, got resets to %v", ports)
	}
	if r.w.destState.Watching() {
		t.Error("a ClientHello refused for a ban must not be watched")
	}
	if keys := r.probed(); len(keys) != 0 {
		t.Errorf("an address already banned is not checked again, probed %v", keys)
	}
}
