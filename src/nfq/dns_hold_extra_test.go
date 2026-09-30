package nfq

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
	"github.com/florianl/go-nfqueue"
	"github.com/mdlayher/netlink"
)

type unbindingQueue struct {
	unbound  atomic.Bool
	verdicts chan int
}

func (q *unbindingQueue) SetVerdict(id uint32, verdict int) error {
	if q.unbound.Load() {
		return errors.New("queue unbound")
	}
	q.verdicts <- verdict
	return nil
}

func TestHeldDNSAnswerIsAcceptedBeforeTheWorkerLetsGoOfItsQueue(t *testing.T) {
	w, _ := holdWorker(t, false)
	holdLimit(t, time.Minute)
	stubRouteAwait(t, make(chan struct{}))
	q := &unbindingQueue{verdicts: make(chan int, 8)}
	ctx, cancel := context.WithCancel(context.Background())
	w.ctx = ctx
	w.holdStop = make(chan struct{})
	w.cancel = func() {
		q.unbound.Store(true)
		cancel()
	}

	w.processDnsPacket(&verdictCtx{id: 21, q: q}, answerPacket(), 53, 40000, buildDNSResponse(21, "www.example.com", []net.IP{net.ParseIP("203.0.113.21")}))
	select {
	case v := <-q.verdicts:
		t.Fatalf("verdict %d before the worker stopped", v)
	case <-time.After(30 * time.Millisecond):
	}
	w.Stop()
	select {
	case v := <-q.verdicts:
		if v != nfqueue.NfAccept {
			t.Fatalf("verdict %d on stop, want accept", v)
		}
	default:
		t.Fatal("the held answer was not accepted before the worker let go of its queue")
	}
}

func TestNoDNSAnswerIsHeldOnceTheWorkerIsStopping(t *testing.T) {
	w, _ := holdWorker(t, false)
	stubRouteAwait(t, make(chan struct{}))
	w.holdStop = make(chan struct{})
	close(w.holdStop)
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 22, q: q}, answerPacket(), 53, 40000, buildDNSResponse(22, "www.example.com", []net.IP{net.ParseIP("203.0.113.22")}))
	if v := q.now(t); v != nfqueue.NfAccept {
		t.Fatalf("verdict %d while stopping, want an immediate accept", v)
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestVerdictForAPacketTheKernelDroppedIsNotAnError(t *testing.T) {
	buf := &syncBuffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	log.StartCapture(buf)
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w := &Worker{ctx: ctx, qnum: 537}

	if ret := w.handleNfqError(&netlink.OpError{Op: "receive", Err: syscall.ENOENT}); ret != 0 {
		t.Fatalf("returned %d, want 0 so the queue keeps running", ret)
	}
	log.Flush()
	if strings.Contains(buf.String(), "nfq:") {
		t.Fatalf("a verdict for a packet the kernel already dropped was logged as an error: %q", buf.String())
	}
}

type replyLog struct {
	mu     sync.Mutex
	events []string
	sent   chan []byte
}

func (r *replyLog) add(e string) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

func (r *replyLog) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func watchReplies(t *testing.T) *replyLog {
	t.Helper()
	r := &replyLog{sent: make(chan []byte, 8)}
	prev := dnsReplyObserver
	dnsReplyObserver = func(resp []byte) {
		r.add("send")
		r.sent <- append([]byte(nil), resp...)
	}
	t.Cleanup(func() { dnsReplyObserver = prev })
	return r
}

type orderedQueue struct {
	log      *replyLog
	failDrop bool
	verdicts chan int
}

func (q *orderedQueue) SetVerdict(id uint32, verdict int) error {
	if verdict == nfqueue.NfDrop {
		q.log.add("drop")
		if q.failDrop {
			return errors.New("queue gone")
		}
	} else {
		q.log.add("accept")
	}
	q.verdicts <- verdict
	return nil
}

func noReply(t *testing.T, r *replyLog, d time.Duration, why string) {
	t.Helper()
	select {
	case <-r.sent:
		t.Fatal(why)
	case <-time.After(d):
	}
}

func oneReply(t *testing.T, r *replyLog) []byte {
	t.Helper()
	select {
	case resp := <-r.sent:
		return resp
	case <-time.After(5 * time.Second):
		t.Fatal("the answer was never sent")
		return nil
	}
}

func onlyAddress(t *testing.T, resp []byte, want string) {
	t.Helper()
	ips := dns.ParseResponseIPs(resp)
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP(want)) {
		t.Fatalf("sent %v, want only %s", ips, want)
	}
}

func TestRewrittenDNSAnswerIsSentOnceAfterTheSet(t *testing.T) {
	w, _ := holdWorker(t, false)
	gate := make(chan struct{})
	stubRouteAwait(t, gate)
	r := watchReplies(t)
	q := &orderedQueue{log: r, verdicts: make(chan int, 8)}

	answer := buildTestMixedResponse("www.example.com", dnsTypeA, "203.0.113.11", "2001:db8::11")
	w.processDnsPacket(&verdictCtx{id: 23, q: q}, answerPacket(), 53, 40000, answer)
	noReply(t, r, 30*time.Millisecond, "the rewritten answer was sent before its addresses were in the set")
	close(gate)
	onlyAddress(t, oneReply(t, r), "203.0.113.11")
	holdsDone(t, w)
	noReply(t, r, 20*time.Millisecond, "the rewritten answer was sent twice")
	if got := r.list(); len(got) != 2 || got[0] != "drop" || got[1] != "send" {
		t.Fatalf("events %v, want the drop verdict before the rewritten answer", got)
	}
}

func TestPinnedDNSAnswerIsSentOnceAfterItsAddresses(t *testing.T) {
	w, set := holdWorker(t, false)
	set.DNS.Pins = map[string][]string{"pin.example.com": {"203.0.113.12"}}
	gate := make(chan struct{})
	stubRouteAwait(t, gate)
	r := watchReplies(t)
	q := &orderedQueue{log: r, verdicts: make(chan int, 8)}

	w.processDnsPacket(&verdictCtx{id: 24, q: q}, queryPacket(), 40000, 53, dns.BuildQuery("pin.example.com", 16, dnsTypeA))
	noReply(t, r, 30*time.Millisecond, "the pinned answer was sent before its addresses were in the set")
	close(gate)
	onlyAddress(t, oneReply(t, r), "203.0.113.12")
	holdsDone(t, w)
	noReply(t, r, 20*time.Millisecond, "the pinned answer was sent twice")
}

func TestRewrittenDNSAnswerIsNotSentWhenTheDropFails(t *testing.T) {
	w, _ := holdWorker(t, false)
	gate := make(chan struct{})
	close(gate)
	stubRouteAwait(t, gate)
	r := watchReplies(t)
	q := &orderedQueue{log: r, failDrop: true, verdicts: make(chan int, 8)}

	answer := buildTestMixedResponse("www.example.com", dnsTypeA, "203.0.113.14", "2001:db8::14")
	w.processDnsPacket(&verdictCtx{id: 25, q: q}, answerPacket(), 53, 40000, answer)
	holdsDone(t, w)
	noReply(t, r, 50*time.Millisecond, "the rewritten answer went out although the original could not be dropped")
}

func TestDNSAnswerWaitsForEveryNewAddress(t *testing.T) {
	w, _ := holdWorker(t, false)
	first, second := make(chan struct{}), make(chan struct{})
	stubRouteAwait(t, first, second)
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 26, q: q}, answerPacket(), 53, 40000, buildDNSResponse(8, "www.example.com", []net.IP{net.ParseIP("203.0.113.15"), net.ParseIP("203.0.113.16")}))
	close(first)
	q.none(t, 50*time.Millisecond)
	close(second)
	if v := q.within(t, 5*time.Second); v != nfqueue.NfAccept {
		t.Fatalf("verdict %d, want accept", v)
	}
	holdsDone(t, w)
}

func TestHeldDNSAnswerKeepsTheWorkerBusy(t *testing.T) {
	w, _ := holdWorker(t, false)
	gate := make(chan struct{})
	stubRouteAwait(t, gate)
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 27, q: q}, answerPacket(), 53, 40000, buildDNSResponse(9, "www.example.com", []net.IP{net.ParseIP("203.0.113.17")}))
	idle := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(idle)
	}()
	select {
	case <-idle:
		t.Fatal("the worker could stop without waiting for a held answer")
	case <-time.After(30 * time.Millisecond):
	}
	close(gate)
	q.within(t, 5*time.Second)
	holdsDone(t, w)
}

func TestEscalatedDNSAnswerWaitsForItsAddresses(t *testing.T) {
	t.Cleanup(stopDNSRouteCleanup)
	plain := config.NewSetConfig()
	plain.Id, plain.Name, plain.Enabled = "plain", "plain", true
	plain.Targets.DomainsToMatch = []string{"example.com"}
	routed := config.NewSetConfig()
	routed.Id, routed.Name, routed.Enabled = "routed", "routed", true
	routed.Targets.DomainsToMatch = []string{"other.org"}
	routed.Routing.Enabled = true
	routed.Routing.Mode = config.RoutingModeProxy
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&plain, &routed}
	w := passiveDNSWorker(t, &cfg)
	w.destState.SetEscalation("www.example.com", routed.Id, "test", 0)
	gate := make(chan struct{})
	calls := stubRouteAwait(t, gate)
	q := newFakeQueue()

	w.processDnsPacket(&verdictCtx{id: 28, q: q}, queryPacket(), 40000, 53, dns.BuildQuery("www.example.com", 17, dnsTypeA))
	if v := q.now(t); v != nfqueue.NfAccept {
		t.Fatalf("verdict %d for the query, want accept", v)
	}
	w.processDnsPacket(&verdictCtx{id: 29, q: q}, answerPacket(), 53, 40000, buildDNSResponse(17, "www.example.com", []net.IP{net.ParseIP("203.0.113.18")}))
	if calls.Load() != 1 {
		t.Fatalf("the escalated set was asked %d times, want once", calls.Load())
	}
	q.none(t, 50*time.Millisecond)
	close(gate)
	if v := q.within(t, 5*time.Second); v != nfqueue.NfAccept {
		t.Fatalf("verdict %d, want accept", v)
	}
	holdsDone(t, w)
}

func TestPinnedDNSAnswerOverTCPWaitsForItsAddresses(t *testing.T) {
	w, set := holdWorker(t, false)
	set.DNS.Pins = map[string][]string{"pin.example.com": {"203.0.113.19"}}
	gate := make(chan struct{})
	stubRouteAwait(t, gate)
	s := &dnsTCPServer{worker: w}

	answered := make(chan []byte, 1)
	go func() {
		resp, _ := s.answerVia(set, w.getConfig(), dns.BuildQuery("pin.example.com", 18, dnsTypeA), "pin.example.com", net.ParseIP("192.168.1.100"))
		answered <- resp
	}()
	select {
	case <-answered:
		t.Fatal("the pinned TCP answer was written before its addresses were in the set")
	case <-time.After(50 * time.Millisecond):
	}
	close(gate)
	select {
	case resp := <-answered:
		onlyAddress(t, resp, "203.0.113.19")
	case <-time.After(5 * time.Second):
		t.Fatal("the pinned TCP answer was never written")
	}
}
