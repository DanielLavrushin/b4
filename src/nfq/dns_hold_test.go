package nfq

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/engine"
	"github.com/florianl/go-nfqueue"
)

type fakeQueue struct {
	verdicts chan int
}

func newFakeQueue() *fakeQueue { return &fakeQueue{verdicts: make(chan int, 8)} }

func (f *fakeQueue) SetVerdict(id uint32, verdict int) error {
	f.verdicts <- verdict
	return nil
}

func (f *fakeQueue) now(t *testing.T) int {
	t.Helper()
	select {
	case v := <-f.verdicts:
		return v
	default:
		t.Fatal("no verdict was given while the packet was being handled")
		return -1
	}
}

func (f *fakeQueue) within(t *testing.T, d time.Duration) int {
	t.Helper()
	select {
	case v := <-f.verdicts:
		return v
	case <-time.After(d):
		t.Fatalf("no verdict within %s", d)
		return -1
	}
}

func (f *fakeQueue) none(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case v := <-f.verdicts:
		t.Fatalf("verdict %d was given while the set was still being updated", v)
	case <-time.After(d):
	}
}

func holdWorker(t *testing.T, ipv6 bool) (*Worker, *config.SetConfig) {
	t.Helper()
	set := config.NewSetConfig()
	set.Id = "hold"
	set.Name = "hold"
	set.Enabled = true
	set.Targets.DomainsToMatch = []string{"example.com"}
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeProxy
	cfg := config.NewConfig()
	cfg.Queue.IPv6Enabled = ipv6
	cfg.Sets = []*config.SetConfig{&set}
	return passiveDNSWorker(t, &cfg), &set
}

func stubRouteAwait(t *testing.T, waits ...<-chan struct{}) *atomic.Int32 {
	t.Helper()
	prev := RoutingHandleDNSAwaitFunc
	t.Cleanup(func() { RoutingHandleDNSAwaitFunc = prev })
	calls := &atomic.Int32{}
	RoutingHandleDNSAwaitFunc = func(*config.Config, *config.SetConfig, []net.IP) []<-chan struct{} {
		calls.Add(1)
		return waits
	}
	return calls
}

func holdLimit(t *testing.T, d time.Duration) {
	t.Helper()
	prev := dnsRouteHoldMax
	dnsRouteHoldMax = d
	t.Cleanup(func() { dnsRouteHoldMax = prev })
}

func answerPacket() *pktInfo {
	server := net.ParseIP("192.168.1.1").To4()
	client := net.ParseIP("192.168.1.100").To4()
	return &pktInfo{ver: IPv4, proto: 17, src: server, dst: client, srcStr: server.String(), dstStr: client.String()}
}

func queryPacket() *pktInfo {
	server := net.ParseIP("192.168.1.1").To4()
	client := net.ParseIP("192.168.1.100").To4()
	return &pktInfo{ver: IPv4, proto: 17, src: client, dst: server, srcStr: client.String(), dstStr: server.String()}
}

func holdsDone(t *testing.T, w *Worker) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a held answer was never released")
	}
}

func TestDNSAnswerWithNewAddressesWaitsUntilTheyAreInTheSet(t *testing.T) {
	w, _ := holdWorker(t, false)
	gate := make(chan struct{})
	calls := stubRouteAwait(t, gate)
	q := newFakeQueue()

	vc := &verdictCtx{id: 7, q: q}
	if ret := w.processDnsPacket(vc, answerPacket(), 53, 40000, buildDNSResponse(1, "www.example.com", []net.IP{net.ParseIP("203.0.113.10")})); ret != 0 {
		t.Fatalf("returned %d, want 0", ret)
	}
	if calls.Load() != 1 {
		t.Fatalf("the answer reached the set %d times, want once", calls.Load())
	}
	q.none(t, 50*time.Millisecond)
	close(gate)
	if v := q.within(t, 5*time.Second); v != nfqueue.NfAccept {
		t.Fatalf("verdict %d after the set was updated, want accept", v)
	}
	holdsDone(t, w)
}

func TestDNSAnswerWithKnownAddressesPassesAtOnce(t *testing.T) {
	w, _ := holdWorker(t, false)
	stubRouteAwait(t)
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 8, q: q}, answerPacket(), 53, 40000, buildDNSResponse(2, "www.example.com", []net.IP{net.ParseIP("203.0.113.10")}))
	if v := q.now(t); v != nfqueue.NfAccept {
		t.Fatalf("verdict %d, want accept", v)
	}
}

func TestDNSAnswerForAnUnmatchedNamePassesAtOnce(t *testing.T) {
	w, _ := holdWorker(t, false)
	gate := make(chan struct{})
	calls := stubRouteAwait(t, gate)
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 9, q: q}, answerPacket(), 53, 40000, buildDNSResponse(3, "www.other.org", []net.IP{net.ParseIP("203.0.113.10")}))
	if v := q.now(t); v != nfqueue.NfAccept {
		t.Fatalf("verdict %d, want accept", v)
	}
	if calls.Load() != 0 {
		t.Fatal("an answer that matches no set was routed")
	}
}

func TestDNSAnswerIsReleasedAtTheTimeLimit(t *testing.T) {
	w, _ := holdWorker(t, false)
	holdLimit(t, 40*time.Millisecond)
	stubRouteAwait(t, make(chan struct{}))
	q := newFakeQueue()

	start := time.Now()
	w.processDnsPacket(&verdictCtx{id: 10, q: q}, answerPacket(), 53, 40000, buildDNSResponse(4, "www.example.com", []net.IP{net.ParseIP("203.0.113.10")}))
	if v := q.within(t, 5*time.Second); v != nfqueue.NfAccept {
		t.Fatalf("verdict %d at the time limit, want accept", v)
	}
	if held := time.Since(start); held < 40*time.Millisecond {
		t.Fatalf("released after %s, before the %s limit", held, 40*time.Millisecond)
	}
	holdsDone(t, w)
}

func TestDNSAnswerIsReleasedWhenTheWorkerStops(t *testing.T) {
	w, _ := holdWorker(t, false)
	holdLimit(t, time.Minute)
	stubRouteAwait(t, make(chan struct{}))
	ctx, cancel := context.WithCancel(context.Background())
	w.ctx = ctx
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 11, q: q}, answerPacket(), 53, 40000, buildDNSResponse(5, "www.example.com", []net.IP{net.ParseIP("203.0.113.10")}))
	q.none(t, 30*time.Millisecond)
	cancel()
	if v := q.within(t, 5*time.Second); v != nfqueue.NfAccept {
		t.Fatalf("verdict %d on shutdown, want accept", v)
	}
	holdsDone(t, w)
}

func TestDNSAnswerIsNotHeldWithoutAQueue(t *testing.T) {
	w, _ := holdWorker(t, false)
	calls := stubRouteAwait(t, make(chan struct{}))

	vc := &verdictCtx{verdict: engine.VerdictDrop}
	w.processDnsPacket(vc, answerPacket(), 53, 40000, buildDNSResponse(6, "www.example.com", []net.IP{net.ParseIP("203.0.113.10")}))
	if vc.verdict != engine.VerdictAccept {
		t.Fatalf("TUN mode must keep answering in line, got verdict %v", vc.verdict)
	}
	if calls.Load() != 1 {
		t.Fatal("TUN mode must still add the addresses to the set")
	}
	if n := len(dnsRouteHoldSlots); n != 0 {
		t.Fatalf("%d answers are held in TUN mode", n)
	}
}

func TestDNSAnswerIsNotHeldWhenEveryHoldSlotIsTaken(t *testing.T) {
	w, _ := holdWorker(t, false)
	stubRouteAwait(t, make(chan struct{}))
	taken := 0
	for full := false; !full; {
		select {
		case dnsRouteHoldSlots <- struct{}{}:
			taken++
		default:
			full = true
		}
	}
	t.Cleanup(func() {
		for ; taken > 0; taken-- {
			<-dnsRouteHoldSlots
		}
	})
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 12, q: q}, answerPacket(), 53, 40000, buildDNSResponse(7, "www.example.com", []net.IP{net.ParseIP("203.0.113.10")}))
	if v := q.now(t); v != nfqueue.NfAccept {
		t.Fatalf("verdict %d with every hold slot taken, want an immediate accept", v)
	}
}

func TestRewrittenDNSAnswerIsDroppedAtOnceAndSentAfterTheSet(t *testing.T) {
	w, _ := holdWorker(t, false)
	gate := make(chan struct{})
	calls := stubRouteAwait(t, gate)
	q := newFakeQueue()

	answer := buildTestMixedResponse("www.example.com", dnsTypeA, "203.0.113.11", "2001:db8::11")
	w.processDnsPacket(&verdictCtx{id: 13, q: q}, answerPacket(), 53, 40000, answer)
	if v := q.now(t); v != nfqueue.NfDrop {
		t.Fatalf("verdict %d for an answer b4 rewrites, want drop", v)
	}
	if calls.Load() != 1 {
		t.Fatal("the rewritten answer's addresses did not reach the set")
	}
	if n := len(dnsRouteHoldSlots); n != 1 {
		t.Fatalf("%d rewritten answers are waiting to be sent, want 1", n)
	}
	close(gate)
	holdsDone(t, w)
	q.none(t, 20*time.Millisecond)
}

func TestPinnedDNSAnswerWaitsForItsAddresses(t *testing.T) {
	w, set := holdWorker(t, false)
	set.DNS.Pins = map[string][]string{"pin.example.com": {"203.0.113.12"}}
	gate := make(chan struct{})
	calls := stubRouteAwait(t, gate)
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 14, q: q}, queryPacket(), 40000, 53, dns.BuildQuery("pin.example.com", 15, dnsTypeA))
	if v := q.now(t); v != nfqueue.NfDrop {
		t.Fatalf("verdict %d for a pinned query, want drop", v)
	}
	if calls.Load() != 1 {
		t.Fatal("the pinned addresses did not reach the set")
	}
	if n := len(dnsRouteHoldSlots); n != 1 {
		t.Fatalf("%d pinned answers are waiting to be sent, want 1", n)
	}
	close(gate)
	holdsDone(t, w)
}

func TestRoutingHandleDNSAwaitFallsBackToTheAsyncHook(t *testing.T) {
	prevAwait, prevAsync, prevSync := RoutingHandleDNSAwaitFunc, RoutingHandleDNSAsyncFunc, RoutingHandleDNSFunc
	t.Cleanup(func() {
		RoutingHandleDNSAwaitFunc, RoutingHandleDNSAsyncFunc, RoutingHandleDNSFunc = prevAwait, prevAsync, prevSync
	})
	var async int
	RoutingHandleDNSAwaitFunc = nil
	RoutingHandleDNSAsyncFunc = func(*config.Config, *config.SetConfig, []net.IP) { async++ }
	RoutingHandleDNSFunc = nil

	if !routingHandleDNSAvailable() {
		t.Fatal("the async hook alone must count as available")
	}
	if waits := routingHandleDNSAwait(&config.Config{}, nil, nil); waits != nil || async != 1 {
		t.Fatalf("without the await hook the async hook must run and nothing be awaited: waits %v, async %d", waits, async)
	}
}
