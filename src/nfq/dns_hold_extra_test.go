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
	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)

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

func TestPinnedDNSAnswerOverTCPIsWrittenWhenItsServerStops(t *testing.T) {
	w, set := holdWorker(t, false)
	holdLimit(t, time.Minute)
	set.DNS.Pins = map[string][]string{"pin.example.com": {"203.0.113.20"}}
	stubRouteAwait(t, make(chan struct{}))
	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)

	answered := make(chan []byte, 1)
	go func() {
		resp, _ := s.answerVia(set, w.getConfig(), dns.BuildQuery("pin.example.com", 20, dnsTypeA), "pin.example.com", net.ParseIP("192.168.1.100"))
		answered <- resp
	}()
	select {
	case <-answered:
		t.Fatal("the pinned TCP answer did not wait for its addresses")
	case <-time.After(30 * time.Millisecond):
	}
	s.cancel()
	select {
	case resp := <-answered:
		onlyAddress(t, resp, "203.0.113.20")
	case <-time.After(time.Second):
		t.Fatal("the pinned TCP answer kept its connection waiting after the server stopped")
	}
}

func TestPinnedAnswerOnATCPConnectionWaitsForItsAddresses(t *testing.T) {
	w, set := holdWorker(t, false)
	set.DNS.Pins = map[string][]string{"pin.example.com": {"203.0.113.41"}}
	gate := make(chan struct{})
	stubRouteAwait(t, gate)
	var srv *dnsTCPServer
	for port := 45470; port < 45500; port++ {
		srv = newDNSTCPServer(w, port)
		if srv.Start() == nil {
			break
		}
		srv = nil
	}
	if srv == nil {
		t.Skip("no free port for dns tcp listener")
	}
	t.Cleanup(srv.Stop)

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(srv.port)), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := writeDNSTCPMessage(conn, dns.BuildQuery("pin.example.com", 41, dnsTypeA), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if resp, err := readDNSTCPMessage(conn); err == nil {
		t.Fatalf("the pinned answer (%d bytes) was written before its addresses were in the set", len(resp))
	}
	close(gate)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := readDNSTCPMessage(conn)
	if err != nil {
		t.Fatalf("the pinned answer never arrived: %v", err)
	}
	onlyAddress(t, resp, "203.0.113.41")
}

func TestEscalatedPinnedAnswerWaitsForItsAddresses(t *testing.T) {
	cfg, _, backup := passiveDNSPair(t, 1)
	backup.DNS.Pins = map[string][]string{"youtube.com": {"142.250.74.14"}}
	backup.Routing.Enabled = true
	backup.Routing.Mode = config.RoutingModeProxy
	w := passiveDNSWorker(t, cfg)
	gate := make(chan struct{})
	calls := stubRouteAwait(t, gate)
	r := watchReplies(t)
	q := &orderedQueue{log: r, verdicts: make(chan int, 8)}

	nx := dns.BuildBlockResponse(dns.BuildQuery("youtube.com", 0x1235, dnsTypeA))
	w.processDnsPacket(&verdictCtx{id: 42, q: q}, answerPacket(), 53, 40000, nx)
	select {
	case v := <-q.verdicts:
		if v != nfqueue.NfDrop {
			t.Fatalf("verdict %d for the failed answer, want drop", v)
		}
	default:
		t.Fatal("no verdict was given for the failed answer while it was being handled")
	}
	if calls.Load() != 1 {
		t.Fatalf("the escalated set was asked %d times, want once", calls.Load())
	}
	noReply(t, r, 30*time.Millisecond, "the escalated set's pinned answer was sent before its addresses were in the set")
	close(gate)
	onlyAddress(t, oneReply(t, r), "142.250.74.14")
	holdsDone(t, w)
}

func TestEscalatedPinnedAnswerAfterARedirectWaitsForItsAddresses(t *testing.T) {
	w, set := holdWorker(t, false)
	set.DNS.Pins = map[string][]string{"pin.example.com": {"203.0.113.43"}}
	gate := make(chan struct{})
	stubRouteAwait(t, gate)
	r := watchReplies(t)

	answered := make(chan bool, 1)
	go func() {
		answered <- w.answerViaSetInline(IPv4, w.getConfig(), set, "pin.example.com", dns.BuildQuery("pin.example.com", 43, dnsTypeA), net.ParseIP("192.168.1.100"), 40000, net.ParseIP("192.168.1.1"))
	}()
	noReply(t, r, 30*time.Millisecond, "the pinned answer was sent before its addresses were in the set")
	close(gate)
	onlyAddress(t, oneReply(t, r), "203.0.113.43")
	if !<-answered {
		t.Fatal("the set with a pin did not answer")
	}
}

func TestPoolReleasesTheHeldAnswersOfEveryWorker(t *testing.T) {
	holdLimit(t, time.Minute)
	stubRouteAwait(t, make(chan struct{}))
	var none *Pool
	none.ReleaseHolds()

	pool := &Pool{}
	var queues []*fakeQueue
	for i := 0; i < 2; i++ {
		w, _ := holdWorker(t, false)
		w.holdStop = make(chan struct{})
		q := newFakeQueue()
		w.processDnsPacket(&verdictCtx{id: uint32(44 + i), q: q}, answerPacket(), 53, 40000, buildDNSResponse(uint16(44+i), "www.example.com", []net.IP{net.ParseIP("203.0.113.44")}))
		pool.Workers = append(pool.Workers, w)
		queues = append(queues, q)
	}
	q := queues[0]
	q.none(t, 30*time.Millisecond)

	pool.ReleaseHolds()
	for i, q := range queues {
		select {
		case v := <-q.verdicts:
			if v != nfqueue.NfAccept {
				t.Fatalf("worker %d: verdict %d, want accept", i, v)
			}
		default:
			t.Fatalf("worker %d still held its answer after the pool released them", i)
		}
	}
}

func TestWorkerFromTheConstructorAcceptsHeldAnswersBeforeLettingGoOfItsQueue(t *testing.T) {
	w, _ := holdWorker(t, false)
	holdLimit(t, time.Minute)
	stubRouteAwait(t, make(chan struct{}))
	built := NewWorkerWithQueue(w.getConfig(), 0)
	q := &unbindingQueue{verdicts: make(chan int, 8)}
	w.ctx, w.holdStop = built.ctx, built.holdStop
	w.cancel = func() {
		q.unbound.Store(true)
		built.cancel()
	}

	w.processDnsPacket(&verdictCtx{id: 46, q: q}, answerPacket(), 53, 40000, buildDNSResponse(46, "www.example.com", []net.IP{net.ParseIP("203.0.113.46")}))
	w.Stop()
	select {
	case v := <-q.verdicts:
		if v != nfqueue.NfAccept {
			t.Fatalf("verdict %d on stop, want accept", v)
		}
	default:
		t.Fatal("a worker built by NewWorkerWithQueue let go of its queue before it accepted the held answer")
	}
}

func TestNoHoldStartsOnceTheHoldsAreReleased(t *testing.T) {
	w, _ := holdWorker(t, false)
	w.holdStop = make(chan struct{})
	w.releaseHolds()

	if w.holdForRoutes(&verdictCtx{id: 47, q: newFakeQueue()}, []<-chan struct{}{make(chan struct{})}, "www.example.com", func() {}) {
		holdsDone(t, w)
		t.Fatal("a hold started after the worker released its holds, so its verdict would come after the queue is gone")
	}
}
