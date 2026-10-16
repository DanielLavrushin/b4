package tun

import (
	"net"
	"net/netip"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/leaktest"
	"github.com/daniellavrushin/b4/nfq"
	"go.uber.org/goleak"
	"golang.org/x/sys/unix"
)

type fixedVerdict engine.PacketVerdict

func (v fixedVerdict) ProcessPacketDir([]byte, engine.Direction) engine.PacketVerdict {
	return engine.PacketVerdict(v)
}

type countingProcessor struct {
	verdict engine.PacketVerdict
	calls   int
}

func (p *countingProcessor) ProcessPacketDir([]byte, engine.Direction) engine.PacketVerdict {
	p.calls++
	return p.verdict
}

type deleteJob struct {
	tuple ctnl.Tuple
	dir   ctnl.Direction
}

type recordingDeleter struct {
	mu      sync.Mutex
	jobs    []deleteJob
	refuse  bool
	stopped int
}

func (d *recordingDeleter) Enqueue(t ctnl.Tuple, dir ctnl.Direction) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.refuse {
		return false
	}
	d.jobs = append(d.jobs, deleteJob{tuple: t, dir: dir})
	return true
}

func (d *recordingDeleter) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped++
}

func (d *recordingDeleter) snapshot() ([]deleteJob, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]deleteJob(nil), d.jobs...), d.stopped
}

func replyJob(t *testing.T, src, dst string) deleteJob {
	t.Helper()
	tuple, err := ctnl.NewTuple(unix.IPPROTO_UDP, netip.MustParseAddrPort(src), netip.MustParseAddrPort(dst))
	if err != nil {
		t.Fatal(err)
	}
	return deleteJob{tuple: tuple, dir: ctnl.Reply}
}

func portsModeEngine(d conntrackDeleter) *Engine {
	cfg := config.NewConfig()
	e := NewEngine(&cfg, nil)
	e.conntrack = d
	return e
}

func firstNEngine(udpLimit int) *Engine {
	cfg := config.NewConfig()
	cfg.Queue.UDPConnBytesLimit = udpLimit
	e := NewEngine(&cfg, nil)
	e.limiter = newFirstNLimiter(captureParamsFrom(&cfg))
	return e
}

func stubConntrack(t *testing.T) (*recordingDeleter, *int) {
	t.Helper()
	d := &recordingDeleter{}
	created := 0
	orig := newConntrackDeleter
	newConntrackDeleter = func() conntrackDeleter {
		created++
		return d
	}
	t.Cleanup(func() { newConntrackDeleter = orig })
	return d, &created
}

const learnedQUICServer = "203.0.113.10"

func learnedAddressPool(t *testing.T) *nfq.Pool {
	t.Helper()
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "learned-drop", "LearnedDrop", true
	set.Targets.SNIDomains = []string{"blocked.example"}
	set.UDP.Mode = config.UDPModeDrop
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Sets = []*config.SetConfig{&set}
	if _, _, err := cfg.GetTargetsForSet(&set); err != nil {
		t.Fatal(err)
	}
	cfg.BuildTCPPortMap()
	cfg.BuildSetPortRanges()
	pool := nfq.NewPool(&cfg)
	t.Cleanup(pool.Stop)
	pool.GetMatcher().LearnIPToDomain(net.ParseIP(learnedQUICServer), "www.blocked.example", cfg.Sets[0])
	return pool
}

func udpPacketWith(sport uint16, payload []byte) []byte {
	return append(testPacket(17, "198.51.100.1", learnedQUICServer, sport, 443, 0), payload...)
}

func TestTUNHoldsQUICButNoOtherUDPToAnAddressLearnedForADroppedName(t *testing.T) {
	worker := learnedAddressPool(t).Workers[0]
	wireguard := make([]byte, 148)
	wireguard[0] = 0x01
	dtls := append([]byte{0x16, 0xfe, 0xfd, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x30}, make([]byte, 48)...)
	srtp := make([]byte, 60)
	srtp[0], srtp[1] = 0x80, 0x60
	longHeader := append([]byte{0xc3, 0, 0, 0, 1, 8, 1, 2, 3, 4, 5, 6, 7, 8, 0}, make([]byte, 40)...)

	for _, c := range []struct {
		name    string
		payload []byte
		held    bool
	}{
		{"QUIC", longHeader, true},
		{"WireGuard", wireguard, false},
		{"DTLS", dtls, false},
		{"SRTP", srtp, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			raw := udpPacketWith(40001, c.payload)

			d := &recordingDeleter{}
			if forward, _ := portsModeEngine(d).processPacket(worker, raw); forward {
				t.Fatal("the set drops every datagram to the learned address")
			}
			if jobs, _ := d.snapshot(); (len(jobs) == 1) != c.held || len(jobs) > 1 {
				t.Fatalf("kernel window: %d conntrack deletes, want a delete only for QUIC (held=%v)", len(jobs), c.held)
			}

			counting := firstNEngine(2)
			forwarded := 0
			for i := 0; i < 5; i++ {
				if forward, _ := counting.processPacket(worker, raw); forward {
					forwarded++
				}
			}
			held := counting.QUICDropPersistence().Held
			if c.held && (forwarded != 0 || held != 5) {
				t.Fatalf("b4 counts: QUIC must stay dropped, got %d of 5 forwarded and %d held", forwarded, held)
			}
			if !c.held && (forwarded != 3 || held != 0) {
				t.Fatalf("b4 counts: past the limit of 2 the flow must pass as before, got %d of 5 forwarded and %d held", forwarded, held)
			}
		})
	}
}

func TestTUNForwardsOnlyWhatB4Accepts(t *testing.T) {
	quic := testPacket(17, "198.51.100.1", "203.0.113.10", 40001, 443, 0)
	verdicts := []struct {
		verdict engine.PacketVerdict
		forward bool
	}{
		{engine.VerdictAccept, true},
		{engine.VerdictDrop, false},
		{engine.VerdictDropFlow, false},
	}
	for name, e := range map[string]*Engine{"kernel window": portsModeEngine(&recordingDeleter{}), "b4 counts": firstNEngine(8)} {
		for _, v := range verdicts {
			if got, _ := e.processPacket(fixedVerdict(v.verdict), quic); got != v.forward {
				t.Errorf("%s, verdict %d: forwarded %v, want %v", name, v.verdict, got, v.forward)
			}
		}
	}
}

func TestTheKernelWindowDeletesTheEntryByTheReplyTupleOfTheHeaderReadFromTheTUN(t *testing.T) {
	d := &recordingDeleter{}
	e := portsModeEngine(d)

	natted := testPacket(17, "198.51.100.1", "203.0.113.10", 40001, 443, 0)
	remapped := testPacket(17, "198.51.100.1", "203.0.113.10", 61000, 443, 0)
	routerOwn := testPacket(17, "10.0.2.1", "203.0.113.10", 40005, 443, 0)
	for _, raw := range [][]byte{natted, natted, remapped, routerOwn} {
		if forward, _ := e.processPacket(fixedVerdict(engine.VerdictDropFlow), raw); forward {
			t.Fatal("a datagram b4 drops for the whole connection must not be forwarded")
		}
	}

	jobs, _ := d.snapshot()
	want := []deleteJob{
		replyJob(t, "203.0.113.10:443", "198.51.100.1:40001"),
		replyJob(t, "203.0.113.10:443", "198.51.100.1:40001"),
		replyJob(t, "203.0.113.10:443", "198.51.100.1:61000"),
		replyJob(t, "203.0.113.10:443", "10.0.2.1:40005"),
	}
	if !slices.Equal(jobs, want) {
		t.Fatalf("deletes %v, want %v: the reply tuple is the reverse of the header the TUN read, which carries the source and port SNAT gave the connection, not the LAN client b4 resolves for its own view, and every decision drop deletes again because each later datagram opens a new entry", jobs, want)
	}
	st := e.QUICDropPersistence()
	if st.FirstN || !st.Conntrack || st.ConntrackQueued != 4 || st.ConntrackRefused != 0 || st.Unheld != 0 {
		t.Fatalf("counters %+v, want 4 deletes queued and nothing else", st)
	}
}

func TestTheKernelWindowLeavesEveryOtherVerdictAlone(t *testing.T) {
	d := &recordingDeleter{}
	e := portsModeEngine(d)
	quic := testPacket(17, "198.51.100.1", "203.0.113.10", 40001, 443, 0)
	for _, v := range []engine.PacketVerdict{engine.VerdictAccept, engine.VerdictDrop} {
		e.processPacket(fixedVerdict(v), quic)
	}
	if jobs, _ := d.snapshot(); len(jobs) != 0 {
		t.Fatalf("an accepted datagram or a plain drop deleted %v; only a decision drop of QUIC holds the connection", jobs)
	}
}

func TestTheKernelWindowCountsWhatItCannotHold(t *testing.T) {
	quic := testPacket(17, "198.51.100.1", "203.0.113.10", 40001, 443, 0)
	frag := testPacket(17, "198.51.100.1", "203.0.113.10", 40001, 443, 0)
	frag[6] = 0x20
	v6 := make([]byte, 48)
	v6[0], v6[6] = 0x60, 17

	full := portsModeEngine(&recordingDeleter{refuse: true})
	for _, raw := range [][]byte{quic, frag, v6} {
		full.processPacket(fixedVerdict(engine.VerdictDropFlow), raw)
	}
	if st := full.QUICDropPersistence(); st.ConntrackRefused != 1 || st.Unheld != 1 || st.ConntrackQueued != 0 {
		t.Fatalf("counters %+v, want the refused delete and the fragment without ports counted once each, and IPv6, which the TUN never forwards nor windows, not at all", st)
	}

	none := portsModeEngine(nil)
	none.processPacket(fixedVerdict(engine.VerdictDropFlow), quic)
	if st := none.QUICDropPersistence(); st.Conntrack || st.Unheld != 1 {
		t.Fatalf("counters %+v, want a drop without a conntrack worker counted as not held", st)
	}
}

func TestB4CountingProcessesEveryLaterPacketOfADroppedFlow(t *testing.T) {
	e := firstNEngine(2)
	unused := &recordingDeleter{}
	e.conntrack = unused
	dropped := testPacket(17, "192.168.0.10", "203.0.113.10", 50000, 443, 0)
	accepted := testPacket(17, "192.168.0.10", "203.0.113.10", 50001, 443, 0)

	drops := &countingProcessor{verdict: engine.VerdictDropFlow}
	forwarded := 0
	for i := 0; i < 30; i++ {
		if forward, _ := e.processPacket(drops, dropped); forward {
			forwarded++
		}
	}
	if drops.calls != 30 || forwarded != 0 {
		t.Fatalf("a dropped flow: b4 decided %d of 30 datagrams and %d were forwarded, want every one decided and none forwarded", drops.calls, forwarded)
	}

	accepts := &countingProcessor{verdict: engine.VerdictAccept}
	for i := 0; i < 30; i++ {
		e.processPacket(accepts, accepted)
	}
	if accepts.calls != 2 {
		t.Fatalf("an accepted flow keeps its limit of 2, yet b4 decided %d of its 30 datagrams", accepts.calls)
	}
	if got := e.limiter.passed.Load(); got != 28 {
		t.Fatalf("%d datagrams went past b4, want only the 28 of the accepted flow", got)
	}
	st := e.QUICDropPersistence()
	if !st.FirstN || st.Held != 30 || st.Unheld != 0 || st.ConntrackQueued != 0 {
		t.Fatalf("counters %+v, want 30 drops held by b4's own count", st)
	}
	if jobs, _ := unused.snapshot(); len(jobs) != 0 {
		t.Fatalf("b4's own count already sees every packet of the flow, so no conntrack entry is deleted, yet %d deletes were queued", len(jobs))
	}
}

func TestASettingsChangeEndsAHoldWithAFreshCount(t *testing.T) {
	e := firstNEngine(2)
	cfg := e.config()
	released := testPacket(17, "192.168.0.10", "203.0.113.10", 50000, 443, 0)
	kept := testPacket(17, "192.168.0.10", "203.0.113.10", 50001, 443, 0)

	drops := &countingProcessor{verdict: engine.VerdictDropFlow}
	for i := 0; i < 5; i++ {
		e.processPacket(drops, released)
		e.processPacket(drops, kept)
	}
	if drops.calls != 10 {
		t.Fatalf("b4 decided %d of 10 datagrams of two dropped flows, want all", drops.calls)
	}

	e.UpdateConfig(cfg)

	accepts := &countingProcessor{verdict: engine.VerdictAccept}
	for i := 0; i < 5; i++ {
		e.processPacket(accepts, released)
	}
	if accepts.calls != 2 {
		t.Fatalf("after a settings change a flow b4 now accepts is decided for a fresh count of 2 and then passes, yet b4 decided %d of 5", accepts.calls)
	}

	drops.calls = 0
	for i := 0; i < 5; i++ {
		e.processPacket(drops, kept)
	}
	if drops.calls != 5 {
		t.Fatalf("a flow b4 still drops after a settings change must stay held, yet b4 decided only %d of 5 datagrams", drops.calls)
	}
}

func TestTheHoldBelongsToTheSettingsGeneration(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 2, udpLimit: 2})
	now := time.Now()
	quic := testPacket(17, "192.168.0.10", "203.0.113.10", 50000, 443, 0)

	admitted(l, quic, 2, now)
	if !l.hold(quic) {
		t.Fatal("a counted flow must be held")
	}
	if got := admitted(l, quic, 10, now); got != 10 {
		t.Fatalf("a held flow is processed past its limit, got %d of 10", got)
	}

	l.setParams(captureParams{tcpLimit: 2, udpLimit: 2})
	if got := admitted(l, quic, 5, now); got != 2 {
		t.Fatalf("new settings, even equal ones, end the hold and the flow starts a fresh count of 2, got %d of 5 processed", got)
	}
	if !l.hold(quic) {
		t.Fatal("the flow must be held again under the new settings")
	}
	if got := admitted(l, quic, 5, now); got != 5 {
		t.Fatalf("held again, got %d of 5 processed", got)
	}
}

func TestOnlyAFlowB4CountsCanBeHeld(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 1, udpLimit: 1})
	now := time.Now()
	if l.hold(testPacket(17, "192.168.0.10", "203.0.113.10", 50000, 443, 0)) {
		t.Fatal("a flow b4 has not counted cannot be held")
	}
	frag := testPacket(17, "192.168.0.10", "203.0.113.10", 50000, 443, 0)
	frag[6] = 0x20
	uncounted := map[string][]byte{
		"dns":      testPacket(17, "192.168.0.10", "192.168.0.1", 50000, 53, 0),
		"fragment": frag,
		"icmp":     testPacket(1, "192.168.0.10", "203.0.113.10", 0, 0, 0),
		"ipv6":     {0x60, 0, 0, 0},
	}
	for name, raw := range uncounted {
		l.admit(raw, now)
		if l.hold(raw) {
			t.Errorf("%s: b4 does not count it, so there is nothing to hold", name)
		}
	}
	if len(l.flows) != 0 {
		t.Fatalf("holding must not create flows, %d exist", len(l.flows))
	}
}

func TestHoldingAFlowRacesNothing(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 2, udpLimit: 2})
	quic := testPacket(17, "192.168.0.10", "203.0.113.10", 50000, 443, 0)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				l.admit(quic, time.Now())
				l.hold(quic)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			l.setParams(captureParams{tcpLimit: 2, udpLimit: 2})
		}
	}()
	wg.Wait()

	l.hold(quic)
	if got := admitted(l, quic, 10, time.Now()); got != 10 {
		t.Fatalf("held after the race, got %d of 10 processed", got)
	}
}

func TestTheConntrackWorkerRunsOnlyWithTheKernelWindow(t *testing.T) {
	d, created := stubConntrack(t)
	untested := !ctnl.CurrentStatus().Tested

	counting := firstNEngine(8)
	counting.startFlowHold()
	if *created != 0 || counting.QUICDropPersistence().Conntrack {
		t.Fatalf("with b4 counting packets itself no conntrack worker is needed, got %d workers", *created)
	}

	window := portsModeEngine(nil)
	window.startFlowHold()
	window.startFlowHold()
	if *created != 1 || !window.QUICDropPersistence().Conntrack {
		t.Fatalf("with the kernel window the engine starts one worker, got %d workers", *created)
	}
	if untested && ctnl.CurrentStatus().Tested {
		t.Fatal("starting the engine ran the conntrack netlink self-test; the worker runs it at its first delete, so a kernel without conntrack netlink warns only once a set drops QUIC")
	}
	window.Stop()
	window.Stop()
	if _, stopped := d.snapshot(); stopped != 1 {
		t.Fatalf("Stop must stop the conntrack worker once, stopped %d times", stopped)
	}
}

func TestStopJoinsTheConntrackWorker(t *testing.T) {
	e := portsModeEngine(nil)
	e.startFlowHold()
	if _, ok := e.conntrack.(*ctnl.Worker); !ok {
		t.Fatalf("the TUN engine deletes through the ctnl worker, got %T", e.conntrack)
	}
	e.Stop()
	goleak.VerifyNone(t, leaktest.Options()...)
}
