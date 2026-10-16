package nfq

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/florianl/go-nfqueue"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

type injectRouteCall struct {
	dst  string
	mark uint32
}

type injectRouteStub struct {
	mu     sync.Mutex
	calls  []injectRouteCall
	oif    uint32
	ok     bool
	answer func(dst net.IP, mark uint32) (uint32, bool)
}

func (s *injectRouteStub) got() []injectRouteCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]injectRouteCall(nil), s.calls...)
}

func (s *injectRouteStub) set(oif uint32, ok bool) {
	s.mu.Lock()
	s.oif, s.ok = oif, ok
	s.mu.Unlock()
}

func (s *injectRouteStub) answerBy(fn func(dst net.IP, mark uint32) (uint32, bool)) {
	s.mu.Lock()
	s.answer = fn
	s.mu.Unlock()
}

func waitInjectRouteIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		injectRouteMu.Lock()
		n := len(injectRouteInflight)
		injectRouteMu.Unlock()
		if n == 0 && len(injectRouteSlots) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d route lookups still running", n)
		}
		time.Sleep(time.Millisecond)
	}
}

func resetInjectRouteCache() {
	injectRouteMu.Lock()
	injectRouteCache = make(map[injectRouteKey]injectRouteEntry)
	injectRouteMu.Unlock()
}

func stubInjectRoute(t *testing.T, oif uint32, ok bool) *injectRouteStub {
	t.Helper()
	waitInjectRouteIdle(t)
	stub := &injectRouteStub{oif: oif, ok: ok}
	prev, prevCheck, prevWait := injectRouteLookup, rerouteCheckAll.Load(), injectRouteWait
	injectRouteLookup = func(dst net.IP, mark uint32) (uint32, bool) {
		stub.mu.Lock()
		stub.calls = append(stub.calls, injectRouteCall{dst: dst.String(), mark: mark})
		answer, oif, ok := stub.answer, stub.oif, stub.ok
		stub.mu.Unlock()
		if answer != nil {
			return answer(dst, mark)
		}
		return oif, ok
	}
	SetRerouteCheckAll(true)
	resetInjectRouteCache()
	t.Cleanup(func() {
		waitInjectRouteIdle(t)
		injectRouteLookup = prev
		injectRouteWait = prevWait
		SetRerouteCheckAll(prevCheck)
		resetInjectRouteCache()
	})
	return stub
}

func backdateInjectRoute(t *testing.T, dst net.IP, mark uint32, by time.Duration) {
	t.Helper()
	var key injectRouteKey
	copy(key.dst[:], dst.To16())
	key.mark = mark
	injectRouteMu.Lock()
	defer injectRouteMu.Unlock()
	e, had := injectRouteCache[key]
	if !had {
		t.Fatalf("no cached answer for %s with mark 0x%x", dst, mark)
	}
	e.at = e.at.Add(-by)
	injectRouteCache[key] = e
}

const keeneticHotspotMark = uint32(0x0ffffaaa)

func TestAConnectionAnotherServiceRoutesAwayByItsMarkIsHandedOff(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	hotspot := keeneticHotspotMark
	for _, c := range []struct {
		name     string
		mark     *uint32
		out      uint32
		injected uint32
		ok       bool
		want     int
		why      string
	}{
		{"marked, b4's packets would leave by the uplink", &hotspot, idx["ppp9"], idx["eth7"], true, nfqueue.NfAccept, "the packets b4 injects would leave by another interface than the connection"},
		{"marked, b4's packets would follow it", &hotspot, idx["ppp9"], idx["ppp9"], true, nfqueue.NfDrop, "b4's own packets take the same interface, so the strategy applies"},
		{"unmarked", nil, idx["ppp9"], idx["eth7"], true, nfqueue.NfDrop, "only a packet another service marked is checked"},
		{"marked, the route lookup fails", &hotspot, idx["ppp9"], 0, false, nfqueue.NfDrop, "without an answer from the kernel b4 behaves as before"},
		{"marked, b4's packets have no route", &hotspot, idx["ppp9"], 0, true, nfqueue.NfAccept, "the packets b4 would inject cannot leave at all, while the connection leaves by ppp9"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stub := stubInjectRoute(t, c.injected, c.ok)
			set := newFragmentingYTImagesSet()
			w := newInjectingWorker(t, &set)
			if got := queuedVerdict(t, w, ytimgHello(), c.mark, c.out, 0); got != c.want {
				t.Fatalf("verdict %d, want %d: %s", got, c.want, c.why)
			}
			calls := stub.got()
			if c.mark == nil {
				if len(calls) != 0 {
					t.Fatalf("an unmarked packet asked the kernel for a route: %v", calls)
				}
				return
			}
			if len(calls) != 1 || calls[0] != (injectRouteCall{dst: "1.2.3.4", mark: 0x8000}) {
				t.Fatalf("route lookups %v, want one for 1.2.3.4 with the queue mark 0x8000, the mark the packets b4 injects carry", calls)
			}
		})
	}
}

func TestWithoutIPTablesOnlyAMarkWithTheQueueMarkIsCheckedForAnotherRoute(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	magi, hotspot := uint32(0x4d616769), keeneticHotspotMark
	for _, c := range []struct {
		name    string
		mark    *uint32
		want    int
		lookups int
		why     string
	}{
		{"MagiTrickle's mark", &magi, nfqueue.NfDrop, 0, "with nftables another service's output chain can give b4's own packets its mark, so the strategy applies as before"},
		{"a mark with every queue-mark bit", &hotspot, nfqueue.NfAccept, 1, "b4 left such a packet alone before, so handing it off is never worse"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stub := stubInjectRoute(t, idx["eth7"], true)
			SetRerouteCheckAll(false)
			set := newFragmentingYTImagesSet()
			w := newInjectingWorker(t, &set)
			if got := queuedVerdict(t, w, ytimgHello(), c.mark, idx["ppp9"], 0); got != c.want {
				t.Fatalf("verdict %d, want %d: %s", got, c.want, c.why)
			}
			if calls := stub.got(); len(calls) != c.lookups {
				t.Fatalf("route lookups %v, want %d", calls, c.lookups)
			}
		})
	}
}

func TestAHandedOffConnectionAsksTheKernelOnce(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stub := stubInjectRoute(t, idx["eth7"], true)
	hotspot := keeneticHotspotMark
	for i := 0; i < 19; i++ {
		set := newFragmentingYTImagesSet()
		if got := queuedVerdict(t, newInjectingWorker(t, &set), ytimgHello(), &hotspot, idx["ppp9"], 0); got != nfqueue.NfAccept {
			t.Fatalf("packet %d: verdict %d, want the connection handed off", i+1, got)
		}
	}
	if calls := stub.got(); len(calls) != 1 {
		t.Fatalf("route lookups %v: a handed-off connection asks the kernel once, not for every queued packet", calls)
	}
}

func TestACachedRouteIsAskedAgainBeforeAHandOff(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stub := stubInjectRoute(t, 99, true)
	hotspot := keeneticHotspotMark
	verdict := func() int {
		set := newFragmentingYTImagesSet()
		return queuedVerdict(t, newInjectingWorker(t, &set), ytimgHello(), &hotspot, idx["ppp9"], 0)
	}

	if got := verdict(); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: b4's packets would leave by interface 99, so the connection is handed off", got)
	}
	stub.set(idx["ppp9"], true)
	backdateInjectRoute(t, net.ParseIP("1.2.3.4"), 0x8000, injectRouteRecheck)
	if got := verdict(); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: the route now leads to ppp9, as after a PPP reconnect, and an answer older than the recheck must not hand the connection off", got)
	}
	if calls := stub.got(); len(calls) != 2 {
		t.Fatalf("route lookups %v, want a second one because the cached answer disagreed with the packet", calls)
	}
	if got := verdict(); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d, want the strategy applied", got)
	}
	if calls := stub.got(); len(calls) != 2 {
		t.Fatalf("route lookups %v: a cached answer that matches the packet needs no new lookup", calls)
	}
}

func TestAPacketOnAnotherInterfaceIsAskedAgainAtOnce(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stub := stubInjectRoute(t, idx["eth7"], true)
	hotspot := keeneticHotspotMark
	verdict := func(out uint32) int {
		set := newFragmentingYTImagesSet()
		return queuedVerdict(t, newInjectingWorker(t, &set), ytimgHello(), &hotspot, out, 0)
	}

	if got := verdict(idx["ppp9"]); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: b4's packets would leave by eth7, so the connection is handed off", got)
	}
	stub.set(idx["eth8"], true)
	if got := verdict(idx["eth8"]); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: the connection now leaves by eth8, where the route of b4's own packets leads too", got)
	}
	if calls := stub.got(); len(calls) != 2 {
		t.Fatalf("route lookups %v: an answer cached for another interface must be asked again at once", calls)
	}
}

func TestARerouteHandOffIsLoggedWithItsInterface(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stubInjectRoute(t, idx["eth7"], true)
	set := newFragmentingYTImagesSet()
	w := newInjectingWorker(t, &set)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	hotspot := keeneticHotspotMark
	if got := queuedVerdict(t, w, ytimgHello(), &hotspot, idx["ppp9"], 0); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d, want the ClientHello accepted unchanged", got)
	}
	f := nextConnectionLine(t, ch)
	if flags := f[len(f)-1]; flags != "reroute->ppp9" {
		t.Fatalf("connection line flags %q, want %q", flags, "reroute->ppp9")
	}
}

func stubRouteMarks(t *testing.T, marks map[string][]uint32) {
	t.Helper()
	prev := RouteMarksFunc
	RouteMarksFunc = func(iface string) []uint32 { return marks[iface] }
	t.Cleanup(func() { RouteMarksFunc = prev })
}

func TestOnAnInterfaceASetRoutesThroughOnlyTheSetsOwnConnectionsKeepTheStrategy(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	const setMark, mwan3Mark = uint32(0x2bd8), uint32(0x200)
	for _, c := range []struct {
		name   string
		mark   uint32
		lookup uint32
		want   int
		why    string
	}{
		{"another service's mark", keeneticHotspotMark, 0x8000, nfqueue.NfAccept, "the mark takes the connection to ppp9, where b4's own packets would not follow"},
		{"a mark another service routes to the set's interface by its own rule", mwan3Mark, 0x8000, nfqueue.NfAccept, "b4's own packets get the set's mark only for the set's addresses, so they stay on the main route"},
		{"the routed set's own mark", setMark, 0x8000 | setMark, nfqueue.NfDrop, "b4's own packets get the set's mark and follow the connection to ppp9"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stub := stubInjectRoute(t, idx["eth7"], true)
			stub.answerBy(func(_ net.IP, mark uint32) (uint32, bool) {
				if mark&config.PerSetRouteMarkBits == setMark || mark&0x3f00 == mwan3Mark {
					return idx["ppp9"], true
				}
				return idx["eth7"], true
			})
			stubRouteMarks(t, map[string][]uint32{"ppp9": {setMark}})
			set := newFragmentingYTImagesSet()
			w := newInjectingWorker(t, &set)

			m := c.mark
			if got := queuedVerdict(t, w, ytimgHello(), &m, idx["ppp9"], 0); got != c.want {
				t.Fatalf("verdict %d, want %d: %s", got, c.want, c.why)
			}
			if calls := stub.got(); len(calls) != 1 || calls[0].mark != c.lookup {
				t.Fatalf("route lookups %v, want one with mark 0x%x", calls, c.lookup)
			}
		})
	}
}

func TestOnlyTheRouteMarkOfB4sOwnSetCountsOnItsInterface(t *testing.T) {
	stubRouteMarks(t, map[string][]uint32{"ppp9": {0x2bd8, 0x5aef}})
	for _, c := range []struct {
		iface string
		bits  uint32
		want  bool
	}{
		{"ppp9", 0x2bd8, true},
		{"ppp9", 0x5aef, true},
		{"ppp9", 0x200, false},
		{"ppp9", 0, false},
		{"eth7", 0x2bd8, false},
	} {
		if got := setRoutesWith(c.iface, c.bits); got != c.want {
			t.Errorf("%s with route bits 0x%x: %v, want %v", c.iface, c.bits, got, c.want)
		}
	}
	RouteMarksFunc = nil
	if setRoutesWith("ppp9", 0x2bd8) {
		t.Error("without routing state no route bits count")
	}
}

func TestAnArrivingPacketIsNotCheckedForAnotherServicesRoute(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stub := stubInjectRoute(t, idx["eth7"], true)
	set := newFragmentingYTImagesSet()
	w := newInjectingWorker(t, &set)

	hotspot := keeneticHotspotMark
	queuedVerdict(t, w, ytimgHello(), &hotspot, 0, idx["ppp9"])
	if calls := stub.got(); len(calls) != 0 {
		t.Fatalf("a packet with no egress interface asked the kernel for a route: %v", calls)
	}
}

func TestDiscoveryIsNotCheckedForAnotherServicesRoute(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stub := stubInjectRoute(t, idx["eth7"], true)
	set := newFragmentingYTImagesSet()
	w := newInjectingWorker(t, &set)
	cfg := w.getConfig()
	cfg.Queue.IsDiscovery = true

	hotspot := keeneticHotspotMark
	queuedVerdict(t, w, ytimgHello(), &hotspot, idx["ppp9"], 0)
	if calls := stub.got(); len(calls) != 0 {
		t.Fatalf("a Discovery run asked the kernel for a route: %v", calls)
	}
}

func TestAQUICInitialAnotherServiceRoutesAwayIsHandedOff(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stubInjectRoute(t, idx["eth7"], true)
	set := newDomainUDPSet(config.QUICFilterSNI)
	cfg := newUDPGateConfig(t, &set)
	w := newTestWorker(t, &cfg)

	hotspot := keeneticHotspotMark
	initial := buildQUICInitialWithSNI(t, []byte{6, 6, 6, 6, 9, 9, 9, 9}, "rr1.googlevideo.com")
	pkt := makeV4UDPPacket(initial, net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4"), 51000, 443)
	if got := queuedVerdict(t, w, pkt, &hotspot, idx["ppp9"], 0); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: udp.mode=drop must not apply to QUIC that another service routes away from b4's own packets", got)
	}
}

func TestAnAgreeingRouteIsAskedAgainOnceItExpires(t *testing.T) {
	stub := stubInjectRoute(t, 7, true)
	dst := net.ParseIP("1.2.3.4")
	injectRouteOIF(dst, 0x8000, 7)
	injectRouteOIF(dst, 0x8000, 7)
	if calls := stub.got(); len(calls) != 1 {
		t.Fatalf("route lookups %v: a fresh agreeing answer needs no new lookup", calls)
	}
	backdateInjectRoute(t, dst, 0x8000, injectRouteTTL)
	injectRouteOIF(dst, 0x8000, 7)
	if calls := stub.got(); len(calls) != 2 {
		t.Fatalf("route lookups %v: an answer older than the TTL must be asked again, as after an uplink failover", calls)
	}
}

func TestAFailedRouteLookupIsCachedUntilItExpires(t *testing.T) {
	stub := stubInjectRoute(t, 0, false)
	dst := net.ParseIP("1.2.3.4")
	for i := 0; i < 2; i++ {
		if _, ok := injectRouteOIF(dst, 0x8000, 7); ok {
			t.Fatal("a failed lookup reported an answer")
		}
	}
	if calls := stub.got(); len(calls) != 1 {
		t.Fatalf("route lookups %v: a failed lookup is not asked again for every packet", calls)
	}
	backdateInjectRoute(t, dst, 0x8000, injectRouteRecheck)
	injectRouteOIF(dst, 0x8000, 7)
	if calls := stub.got(); len(calls) != 1 {
		t.Fatalf("route lookups %v: a failed lookup stays cached for the whole TTL, not only the recheck", calls)
	}
	backdateInjectRoute(t, dst, 0x8000, injectRouteTTL)
	injectRouteOIF(dst, 0x8000, 7)
	if calls := stub.got(); len(calls) != 2 {
		t.Fatalf("route lookups %v: a failed lookup older than the TTL must be asked again", calls)
	}
}

func TestTheRouteCacheStaysBounded(t *testing.T) {
	stubInjectRoute(t, 7, true)
	for i := 0; i <= injectRouteCacheMax; i++ {
		injectRouteOIF(net.IPv4(10, byte(i>>16), byte(i>>8), byte(i)), 0x8000, 7)
	}
	waitInjectRouteIdle(t)
	injectRouteMu.Lock()
	n := len(injectRouteCache)
	injectRouteMu.Unlock()
	if n > injectRouteCacheMax {
		t.Fatalf("%d cached answers, want at most %d", n, injectRouteCacheMax)
	}
}

func TestASlowRouteLookupHoldsAPacketOnlyBriefly(t *testing.T) {
	stub := stubInjectRoute(t, 7, true)
	gate := make(chan struct{})
	stub.answerBy(func(net.IP, uint32) (uint32, bool) {
		<-gate
		return 7, true
	})
	injectRouteWait = 20 * time.Millisecond
	dst := net.ParseIP("1.2.3.4")
	if _, ok := injectRouteOIF(dst, 0x8000, 7); ok {
		t.Fatal("a lookup still waiting on the kernel gave an answer")
	}

	injectRouteWait = 5 * time.Second
	begin := time.Now()
	if _, ok := injectRouteOIF(dst, 0x8000, 7); ok {
		t.Fatal("a lookup still waiting on the kernel gave an answer")
	}
	if waited := time.Since(begin); waited > 2*time.Second {
		t.Fatalf("a packet waited %s on a lookup another packet had already given up on", waited)
	}

	close(gate)
	waitInjectRouteIdle(t)
	if oif, ok := injectRouteOIF(dst, 0x8000, 7); !ok || oif != 7 {
		t.Fatalf("got %d, %v: the answer that came after the packets stopped waiting must fill the cache", oif, ok)
	}
	if calls := stub.got(); len(calls) != 1 {
		t.Fatalf("route lookups %v: packets that met a running lookup must not start another", calls)
	}
}

func TestBusyRouteLookupsLetAPacketThroughAtOnce(t *testing.T) {
	stub := stubInjectRoute(t, 7, true)
	gate := make(chan struct{})
	started := make(chan struct{}, injectRouteSlotsMax)
	stub.answerBy(func(net.IP, uint32) (uint32, bool) {
		started <- struct{}{}
		<-gate
		return 7, true
	})
	defer close(gate)
	injectRouteWait = 10 * time.Millisecond
	for i := 0; i < injectRouteSlotsMax; i++ {
		injectRouteOIF(net.IPv4(10, 0, 0, byte(i+1)), 0x8000, 7)
		<-started
	}

	injectRouteWait = 5 * time.Second
	begin := time.Now()
	if _, ok := injectRouteOIF(net.IPv4(10, 0, 0, 99), 0x8000, 7); ok {
		t.Fatal("a packet got an answer while every lookup slot was taken")
	}
	if waited := time.Since(begin); waited > 2*time.Second {
		t.Fatalf("the packet waited %s although no lookup could start", waited)
	}
	if calls := stub.got(); len(calls) != injectRouteSlotsMax {
		t.Fatalf("route lookups %v: no lookup may start while every slot is taken", calls)
	}
}

type fakeRouteSocket struct {
	mu      sync.Mutex
	last    netlink.Message
	errno   unix.Errno
	recvErr error
	closed  bool
}

func (f *fakeRouteSocket) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeRouteSocket) Send(m netlink.Message) error {
	f.mu.Lock()
	f.last = m
	f.mu.Unlock()
	return nil
}

func (f *fakeRouteSocket) SendMessages(ms []netlink.Message) error {
	if len(ms) > 0 {
		return f.Send(ms[len(ms)-1])
	}
	return nil
}

func (f *fakeRouteSocket) Receive() ([]netlink.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recvErr != nil {
		return nil, f.recvErr
	}
	data := binary.NativeEndian.AppendUint32(nil, uint32(-int32(f.errno)))
	h := netlink.Header{Type: netlink.Error, Sequence: f.last.Header.Sequence, PID: f.last.Header.PID}
	return []netlink.Message{{Header: h, Data: data}}, nil
}

func (f *fakeRouteSocket) SetDeadline(time.Time) error      { return nil }
func (f *fakeRouteSocket) SetReadDeadline(time.Time) error  { return nil }
func (f *fakeRouteSocket) SetWriteDeadline(time.Time) error { return nil }

func (f *fakeRouteSocket) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func useRouteSocket(t *testing.T, f *fakeRouteSocket) *netlink.Conn {
	t.Helper()
	c := netlink.NewConn(f, 1)
	injectRouteConnMu.Lock()
	prev := injectRouteConn
	injectRouteConn = c
	injectRouteConnMu.Unlock()
	t.Cleanup(func() {
		injectRouteConnMu.Lock()
		injectRouteConn = prev
		injectRouteConnMu.Unlock()
	})
	return c
}

func routeConnInUse() *netlink.Conn {
	injectRouteConnMu.Lock()
	defer injectRouteConnMu.Unlock()
	return injectRouteConn
}

func TestALostRouteReplyDropsTheSocket(t *testing.T) {
	f := &fakeRouteSocket{recvErr: os.ErrDeadlineExceeded}
	useRouteSocket(t, f)
	if _, ok := netlinkRouteOIF(net.ParseIP("192.0.2.1"), 0x8000); ok {
		t.Fatal("a lookup whose reply never came reported an answer")
	}
	if !f.isClosed() {
		t.Fatal("the socket that missed its reply was not closed, so its late reply would answer the next lookup")
	}
	if routeConnInUse() != nil {
		t.Fatal("the socket that missed its reply is still in use")
	}
}

func TestAKernelRefusalKeepsTheSocket(t *testing.T) {
	for _, c := range []struct {
		name  string
		dst   string
		errno unix.Errno
		ok    bool
		why   string
	}{
		{"IPv4, no route", "192.0.2.1", unix.ENETUNREACH, true, "b4's own packets cannot leave, which is an answer"},
		{"IPv4, unreachable route", "192.0.2.1", unix.EHOSTUNREACH, true, "b4's own packets cannot leave, which is an answer"},
		{"IPv4, prohibit route", "192.0.2.1", unix.EACCES, true, "b4's own packets cannot leave, which is an answer"},
		{"IPv4, blackhole route", "192.0.2.1", unix.EINVAL, true, "b4's own packets cannot leave, which is an answer"},
		{"IPv4, another refusal", "192.0.2.1", unix.EOPNOTSUPP, false, "only a missing or refused route counts as an answer"},
		{"IPv6, no route", "2001:db8::1", unix.ENETUNREACH, false, "with source-specific IPv6 routes the kernel finds a route once b4's send picks a source"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeRouteSocket{errno: c.errno}
			conn := useRouteSocket(t, f)
			oif, ok := netlinkRouteOIF(net.ParseIP(c.dst), 0x8000)
			if oif != 0 || ok != c.ok {
				t.Fatalf("got %d, %v, want 0, %v: %s", oif, ok, c.ok, c.why)
			}
			if f.isClosed() || routeConnInUse() != conn {
				t.Fatal("a refusal from the kernel ends the exchange, so the socket must stay in use")
			}
		})
	}
}

func TestTheRouteRequestCarriesTheAddressAndTheMark(t *testing.T) {
	for _, c := range []struct {
		name   string
		dst    net.IP
		family uint8
		bits   uint8
		addr   []byte
	}{
		{"IPv4", net.IPv4(1, 2, 3, 4), unix.AF_INET, 32, []byte{1, 2, 3, 4}},
		{"IPv4, four bytes", net.IP{1, 2, 3, 4}, unix.AF_INET, 32, []byte{1, 2, 3, 4}},
		{"IPv6", net.ParseIP("2001:db8::1"), unix.AF_INET6, 128, net.ParseIP("2001:db8::1").To16()},
	} {
		t.Run(c.name, func(t *testing.T) {
			req, ok := injectRouteRequest(c.dst, 0x8000)
			if !ok || len(req) < unix.SizeofRtMsg {
				t.Fatalf("no request built for %s", c.dst)
			}
			if req[0] != c.family || req[1] != c.bits {
				t.Fatalf("family %d, dst_len %d, want %d and %d", req[0], req[1], c.family, c.bits)
			}
			attrs, err := netlink.UnmarshalAttributes(req[unix.SizeofRtMsg:])
			if err != nil {
				t.Fatalf("attributes: %v", err)
			}
			got := map[uint16][]byte{}
			for _, a := range attrs {
				got[a.Type] = a.Data
			}
			if !bytes.Equal(got[unix.RTA_DST], c.addr) {
				t.Fatalf("RTA_DST %x, want %x", got[unix.RTA_DST], c.addr)
			}
			if want := binary.NativeEndian.AppendUint32(nil, 0x8000); !bytes.Equal(got[unix.RTA_MARK], want) {
				t.Fatalf("RTA_MARK %x, want %x: the kernel reads the mark in host byte order", got[unix.RTA_MARK], want)
			}
		})
	}
	if _, ok := injectRouteRequest(nil, 0x8000); ok {
		t.Fatal("a request was built without an address")
	}
}

func TestARouteReplyWithoutAnInterfaceIsNoAnswer(t *testing.T) {
	reply := func(attrs ...netlink.Attribute) netlink.Message {
		b, err := netlink.MarshalAttributes(attrs)
		if err != nil {
			t.Fatal(err)
		}
		return netlink.Message{Data: append(make([]byte, unix.SizeofRtMsg), b...)}
	}
	oif := func(i uint32) netlink.Attribute {
		return netlink.Attribute{Type: unix.RTA_OIF, Data: binary.NativeEndian.AppendUint32(nil, i)}
	}
	dst := netlink.Attribute{Type: unix.RTA_DST, Data: []byte{1, 2, 3, 4}}
	truncated := reply(dst, oif(7))
	truncated.Data = append(truncated.Data, 8, 0, unix.RTA_PREFSRC, 0, 7)

	for _, c := range []struct {
		name string
		msgs []netlink.Message
		oif  uint32
		ok   bool
	}{
		{"an interface", []netlink.Message{reply(dst, oif(7))}, 7, true},
		{"interface zero", []netlink.Message{reply(dst, oif(0))}, 0, false},
		{"no interface", []netlink.Message{reply(dst)}, 0, false},
		{"a short message", []netlink.Message{{Data: []byte{2, 32}}}, 0, false},
		{"a truncated attribute", []netlink.Message{truncated}, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			oif, ok := routeReplyOIF(c.msgs)
			if oif != c.oif || ok != c.ok {
				t.Fatalf("got %d, %v, want %d, %v", oif, ok, c.oif, c.ok)
			}
		})
	}
}

func TestTheKernelNamesTheInterfaceForAnAddress(t *testing.T) {
	lo, err := net.InterfaceByName("lo")
	if err != nil {
		t.Skipf("no loopback interface: %v", err)
	}
	for i := 0; i < 2; i++ {
		oif, ok := netlinkRouteOIF(net.ParseIP("127.0.0.1"), 0x8000)
		if !ok {
			t.Skip("the kernel did not answer a route lookup here")
		}
		if oif != uint32(lo.Index) {
			t.Fatalf("route to 127.0.0.1 leaves by ifindex %d, want lo (%d)", oif, lo.Index)
		}
	}
}
