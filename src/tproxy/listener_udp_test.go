package tproxy

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/socks5"
)

type fakeUDPRelay struct {
	mu    sync.Mutex
	wrote []string
	done  chan struct{}
	once  sync.Once
}

func newFakeUDPRelay() *fakeUDPRelay { return &fakeUDPRelay{done: make(chan struct{})} }

func (r *fakeUDPRelay) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wrote = append(r.wrote, string(p))
	return len(p), nil
}

func (r *fakeUDPRelay) Read([]byte) (int, error) {
	<-r.done
	return 0, net.ErrClosed
}

func (r *fakeUDPRelay) SetReadDeadline(time.Time) error { return nil }

func (r *fakeUDPRelay) Close() error {
	r.once.Do(func() { close(r.done) })
	return nil
}

func (r *fakeUDPRelay) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.wrote)
}

func (r *fakeUDPRelay) closed() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

type fakeUDPReply struct{ shut atomic.Bool }

func (r *fakeUDPReply) WriteToUDP(b []byte, _ *net.UDPAddr) (int, error) { return len(b), nil }
func (r *fakeUDPReply) Close() error                                     { r.shut.Store(true); return nil }

type udpTestRig struct {
	l        *Listener
	mu       sync.Mutex
	relays   map[string]*fakeUDPRelay
	replies  []*fakeUDPReply
	upstream atomic.Int64
	direct   atomic.Int64
	dial     func(ctx context.Context, dst *net.UDPAddr) error
}

func (r *udpTestRig) relay(dst *net.UDPAddr) *fakeUDPRelay {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.relays[dst.String()]
}

func (r *udpTestRig) keep(dst *net.UDPAddr) *fakeUDPRelay {
	relay := newFakeUDPRelay()
	r.mu.Lock()
	r.relays[dst.String()] = relay
	r.mu.Unlock()
	return relay
}

func newUDPTestRig(t *testing.T, limit int64) *udpTestRig {
	t.Helper()
	rig := &udpTestRig{relays: map[string]*fakeUDPRelay{}}
	l := &Listener{
		SetID:    "udp-test",
		SetName:  "udp-test",
		Upstream: socks5.ClientConfig{Host: "192.0.2.1", Port: 1080},
		guard:    newLoopGuard("192.0.2.1", 1080),
	}
	l.ctx, l.cancel = context.WithCancel(context.Background())
	l.udpSessions = make(map[string]*udpSession)
	rig.l = l

	openReply, dialUpstream, dialDirect := udpOpenReply, udpDialUpstream, udpDialDirect
	before := udpSessionsOpen.Load()
	udpSessionLimit.Store(limit)
	udpOpenReply = func(context.Context, *net.UDPAddr, bool, uint32) (udpReply, error) {
		reply := &fakeUDPReply{}
		rig.mu.Lock()
		rig.replies = append(rig.replies, reply)
		rig.mu.Unlock()
		return reply, nil
	}
	udpDialUpstream = func(ctx context.Context, _ socks5.ClientConfig, dst *net.UDPAddr) (udpRelay, error) {
		rig.upstream.Add(1)
		if rig.dial != nil {
			if err := rig.dial(ctx, dst); err != nil {
				return nil, err
			}
		}
		return rig.keep(dst), nil
	}
	udpDialDirect = func(_ context.Context, dst *net.UDPAddr, _ uint32) (udpRelay, error) {
		rig.direct.Add(1)
		return rig.keep(dst), nil
	}
	t.Cleanup(func() {
		l.cancel()
		l.stopUDP()
		udpWaitSetups(t, l)
		udpOpenReply, udpDialUpstream, udpDialDirect = openReply, dialUpstream, dialDirect
		udpSessionLimit.Store(0)
		udpWaitFor(t, "every UDP session to leave the shared count", func() bool { return udpSessionsOpen.Load() == before })
	})
	return rig
}

func udpWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func udpWaitSetups(t *testing.T, l *Listener) {
	t.Helper()
	udpWaitFor(t, "the UDP session setups in flight to finish", func() bool {
		l.udpMu.Lock()
		defer l.udpMu.Unlock()
		return l.udpSetups == 0
	})
}

func udpTestAddr(ip string, port int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.ParseIP(ip), Port: port}
}

func (r *udpTestRig) send(t *testing.T, src, dst *net.UDPAddr, msg string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		r.l.dispatchUDP(src, dst, []byte(msg), false)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("dispatching %q to %s blocked the read loop", msg, dst)
	}
}

func (r *udpTestRig) wrote(t *testing.T, dst *net.UDPAddr, want ...string) {
	t.Helper()
	udpWaitFor(t, "the relay to "+dst.String()+" to carry "+want[len(want)-1], func() bool {
		relay := r.relay(dst)
		return relay != nil && slices.Equal(relay.got(), want)
	})
}

func TestANewUDPFlowWaitingForTheUpstreamDoesNotHoldEstablishedFlows(t *testing.T) {
	rig := newUDPTestRig(t, 100)
	client := udpTestAddr("10.0.0.5", 40000)
	live, slow := udpTestAddr("203.0.113.1", 50000), udpTestAddr("203.0.113.2", 50001)
	gate := make(chan struct{})
	rig.dial = func(ctx context.Context, dst *net.UDPAddr) error {
		if dst.String() != slow.String() {
			return nil
		}
		select {
		case <-gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	rig.send(t, client, live, "a1")
	rig.wrote(t, live, "a1")

	rig.send(t, client, slow, "b1")
	rig.send(t, client, live, "a2")
	rig.wrote(t, live, "a1", "a2")
	rig.send(t, client, slow, "b2")
	rig.send(t, client, slow, "b3")
	if relay := rig.relay(slow); relay != nil {
		t.Fatalf("the slow flow has a relay before its upstream answered: %v", relay.got())
	}

	close(gate)
	rig.wrote(t, slow, "b1", "b2", "b3")
}

func TestADatagramBurstDuringSetupKeepsOnlyABoundedQueue(t *testing.T) {
	rig := newUDPTestRig(t, 100)
	client, dst := udpTestAddr("10.0.0.5", 40000), udpTestAddr("203.0.113.2", 50001)
	gate := make(chan struct{})
	rig.dial = func(ctx context.Context, _ *net.UDPAddr) error {
		select {
		case <-gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for i := 0; i < udpPendingQueue*3; i++ {
		rig.send(t, client, dst, "p")
	}
	close(gate)
	udpWaitFor(t, "the relay to open", func() bool { return rig.relay(dst) != nil })
	udpWaitFor(t, "the queued datagrams", func() bool { return len(rig.relay(dst).got()) == udpPendingQueue })
	time.Sleep(50 * time.Millisecond)
	if n := len(rig.relay(dst).got()); n != udpPendingQueue {
		t.Errorf("%d datagrams reached the relay, want the first %d: the rest arrived while the session was not open yet", n, udpPendingQueue)
	}
}

func TestTheUDPSessionLimitDropsNewFlowsAndNeverSendsThemDirect(t *testing.T) {
	rig := newUDPTestRig(t, 2)
	rig.l.FailOpen = true
	client := udpTestAddr("10.0.0.5", 40000)
	first, second, third := udpTestAddr("203.0.113.1", 50000), udpTestAddr("203.0.113.2", 50000), udpTestAddr("203.0.113.3", 50000)

	rig.send(t, client, first, "a")
	rig.send(t, client, second, "b")
	rig.wrote(t, first, "a")
	rig.wrote(t, second, "b")

	rig.send(t, client, third, "c")
	time.Sleep(50 * time.Millisecond)
	if rig.relay(third) != nil || rig.upstream.Load() != 2 || rig.direct.Load() != 0 {
		t.Fatalf("at the session limit a new flow must be dropped: upstream dials %d, direct dials %d, relay %v; a direct dial would leak it around the upstream",
			rig.upstream.Load(), rig.direct.Load(), rig.relay(third) != nil)
	}
	rig.send(t, client, first, "a2")
	rig.wrote(t, first, "a", "a2")

	rig.relay(second).Close()
	udpWaitFor(t, "the closed flow to leave the table", func() bool { return udpSessionsOpen.Load() == 1 })
	rig.send(t, client, third, "c2")
	rig.wrote(t, third, "c2")
}

func TestAnUpstreamThatRefusedUDPIsNotDialedForEveryNewFlow(t *testing.T) {
	refused := errors.New("no UDP ASSOCIATE")
	for _, failOpen := range []bool{false, true} {
		name := "fail closed"
		if failOpen {
			name = "fail open"
		}
		t.Run(name, func(t *testing.T) {
			rig := newUDPTestRig(t, 100)
			rig.l.FailOpen = failOpen
			var down atomic.Bool
			down.Store(true)
			rig.dial = func(context.Context, *net.UDPAddr) error {
				if down.Load() {
					return refused
				}
				return nil
			}
			client := udpTestAddr("10.0.0.5", 40000)
			rig.send(t, client, udpTestAddr("203.0.113.1", 50000), "a")
			udpWaitFor(t, "the first dial to fail", func() bool { return rig.l.udpDownUntil.Load() != 0 })

			for i := 0; i < 20; i++ {
				rig.send(t, client, udpTestAddr("203.0.113.9", 50000+i), "x")
			}
			time.Sleep(50 * time.Millisecond)
			if n := rig.upstream.Load(); n != 1 {
				t.Errorf("%d upstream dials for 21 new flows, want 1: each new flow must not wait out another dial while the upstream is down", n)
			}
			if failOpen {
				udpWaitFor(t, "the flows to go direct", func() bool { return rig.direct.Load() == 21 })
			} else if n := rig.direct.Load(); n != 0 {
				t.Errorf("%d direct dials with fail-open off, want none", n)
			}

			down.Store(false)
			rig.l.udpDownUntil.Store(0)
			back := udpTestAddr("203.0.113.50", 50000)
			rig.send(t, client, back, "again")
			rig.wrote(t, back, "again")
			if n := rig.upstream.Load(); n != 2 {
				t.Errorf("%d upstream dials once the wait was over, want the next flow to try the upstream again", n)
			}
		})
	}
}

func TestStoppingTheListenerClosesAUDPSessionStillBeingSetUp(t *testing.T) {
	rig := newUDPTestRig(t, 100)
	client, dst := udpTestAddr("10.0.0.5", 40000), udpTestAddr("203.0.113.2", 50001)
	gate := make(chan struct{})
	rig.dial = func(context.Context, *net.UDPAddr) error {
		<-gate
		return nil
	}
	rig.send(t, client, dst, "a")
	udpWaitFor(t, "the upstream dial to start", func() bool { return rig.upstream.Load() == 1 })

	rig.l.cancel()
	rig.l.stopUDP()
	close(gate)

	udpWaitFor(t, "the late relay to be closed", func() bool {
		relay := rig.relay(dst)
		return relay != nil && relay.closed()
	})
	rig.mu.Lock()
	defer rig.mu.Unlock()
	for _, reply := range rig.replies {
		if !reply.shut.Load() {
			t.Error("the reply socket of a session that opened after the listener stopped stayed open")
		}
	}
}

func TestTheUDPSessionLimitFollowsTheOpenFileAllowance(t *testing.T) {
	for openFiles, want := range map[uint64]int64{0: 64, 256: 64, 1024: 128, 4096: 512, 524288: 1024} {
		if got := udpSessionLimitFor(openFiles); got != want {
			t.Errorf("with %d open files allowed the session limit is %d, want %d", openFiles, got, want)
		}
	}
}

func TestTwoProxySetsNeverOpenMoreUDPSessionsThanTheSharedLimit(t *testing.T) {
	rig := newUDPTestRig(t, 1)
	other := &Listener{
		SetID:    "udp-test-other",
		SetName:  "udp-test-other",
		Upstream: socks5.ClientConfig{Host: "192.0.2.1", Port: 1080},
		guard:    newLoopGuard("192.0.2.1", 1080),
	}
	other.ctx, other.cancel = context.WithCancel(context.Background())
	other.udpSessions = make(map[string]*udpSession)
	defer func() {
		other.cancel()
		other.stopUDP()
		udpWaitSetups(t, other)
	}()

	before := udpSessionsOpen.Load()
	for round := 0; round < 20000; round++ {
		start := make(chan struct{})
		var wg sync.WaitGroup
		for n, l := range []*Listener{rig.l, other} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				l.dispatchUDP(udpTestAddr("10.0.0.5", 20000+round), udpTestAddr("203.0.113.1", 50000+n), []byte("x"), false)
			}()
		}
		close(start)
		wg.Wait()
		if open := udpSessionsOpen.Load() - before; open > 1 {
			t.Fatalf("round %d: %d UDP sessions are open across two proxy sets that share a limit of 1; each set checked the limit before the other had counted its session", round, open)
		}
		rig.l.stopUDP()
		other.stopUDP()
	}
}

type stuckUDPRelay struct {
	*fakeUDPRelay
	entered chan struct{}
	once    sync.Once
}

func (r *stuckUDPRelay) Write([]byte) (int, error) {
	r.once.Do(func() { close(r.entered) })
	<-r.done
	return 0, net.ErrClosed
}

func TestAStuckFirstWriteHoldsNeitherTheNextDatagramNorTheStop(t *testing.T) {
	rig := newUDPTestRig(t, 100)
	relay := &stuckUDPRelay{fakeUDPRelay: newFakeUDPRelay(), entered: make(chan struct{})}
	defer relay.Close()
	udpDialUpstream = func(context.Context, socks5.ClientConfig, *net.UDPAddr) (udpRelay, error) {
		return relay, nil
	}
	client, dst := udpTestAddr("10.0.0.5", 40000), udpTestAddr("203.0.113.2", 50001)

	rig.send(t, client, dst, "a")
	<-relay.entered
	rig.send(t, client, dst, "b")

	stopped := make(chan struct{})
	go func() {
		rig.l.stopUDP()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stopping the listener waited for a queued datagram stuck in the relay's write; closing the session must end that write")
	}
	if !relay.closed() {
		t.Error("the relay of the stopped session stayed open")
	}
}
