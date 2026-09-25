package mtproto

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRoute struct {
	delay   time.Duration
	err     error
	hang    bool
	tlsHang bool
}

type raceConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *raceConn) Close() error {
	c.closed.Store(true)
	return nil
}

var errFake503 = &wsHandshakeError{statusCode: 503, statusLine: "503 Service Unavailable"}

type raceHarness struct {
	mu       sync.Mutex
	routes   map[string]fakeRoute
	inFlight int
	peak     int
	native   int
	peakNat  int
	started  []string
	failures []raceAttempt
	spares   chan raceAttempt
}

func newRaceHarness(routes map[string]fakeRoute) *raceHarness {
	return &raceHarness{routes: routes, spares: make(chan raceAttempt, 8)}
}

func (h *raceHarness) race(deadline time.Time, stagger time.Duration, maxInFlight int) *dialRace {
	return &dialRace{
		stagger:     stagger,
		maxInFlight: maxInFlight,
		minAttempt:  20 * time.Millisecond,
		deadline:    deadline,
		timeoutFor:  func(transportPlan) time.Duration { return time.Second },
		dial:        h.dial,
		started: func(p transportPlan) {
			h.mu.Lock()
			h.started = append(h.started, p.sni)
			h.mu.Unlock()
		},
		failed: func(a raceAttempt) {
			h.mu.Lock()
			h.failures = append(h.failures, a)
			h.mu.Unlock()
		},
		accept: func(raceAttempt) error { return nil },
		spare:  func(a raceAttempt) { h.spares <- a },
	}
}

func (h *raceHarness) dial(p transportPlan, timeout time.Duration, fresh bool) (net.Conn, bool, error) {
	h.mu.Lock()
	r := h.routes[p.sni]
	h.inFlight++
	if h.inFlight > h.peak {
		h.peak = h.inFlight
	}
	if p.native {
		h.native++
		if h.native > h.peakNat {
			h.peakNat = h.native
		}
	}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.inFlight--
		if p.native {
			h.native--
		}
		h.mu.Unlock()
	}()
	if r.hang {
		time.Sleep(timeout)
		return nil, false, &net.OpError{Op: "dial", Err: timeoutError{}}
	}
	if r.tlsHang {
		time.Sleep(timeout)
		return nil, false, &net.OpError{Op: "read", Err: timeoutError{}}
	}
	time.Sleep(r.delay)
	if r.err != nil {
		return nil, false, r.err
	}
	return &raceConn{}, false, nil
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func nativePlan(sni string) transportPlan {
	return transportPlan{kind: transportWS, dc: 2, sni: sni, dialHost: telegramWSEdgeIP, native: true}
}

func cfPlan(sni string) transportPlan {
	return transportPlan{kind: transportWS, dc: 2, sni: sni, cfBase: sni}
}

func TestRaceStartsTheNextRouteWhileASlowOneHangs(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"edge": {hang: true},
		"cf":   {delay: 30 * time.Millisecond},
	})
	start := time.Now()
	out := h.race(time.Now().Add(3*time.Second), 100*time.Millisecond, 3).run([]transportPlan{nativePlan("edge"), cfPlan("cf")})
	elapsed := time.Since(start)
	if out.winner == nil || out.winner.plan.sni != "cf" {
		t.Fatalf("winner %+v, want cf", out.winner)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("race took %v behind a hanging edge", elapsed)
	}
	reportedBy := time.Now().Add(3 * time.Second)
	for {
		h.mu.Lock()
		n := len(h.failures)
		h.mu.Unlock()
		if n > 0 || time.Now().After(reportedBy) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.failures) != 1 || h.failures[0].plan.sni != "edge" {
		t.Fatalf("late edge timeout not reported: %+v", h.failures)
	}
}

func TestRaceMovesOnAtOnceAfterAFastFailure(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"cf1": {delay: 10 * time.Millisecond, err: errFake503},
		"cf2": {delay: 10 * time.Millisecond, err: errFake503},
		"cf3": {delay: 10 * time.Millisecond},
	})
	start := time.Now()
	out := h.race(time.Now().Add(3*time.Second), 10*time.Second, 3).run([]transportPlan{cfPlan("cf1"), cfPlan("cf2"), cfPlan("cf3")})
	if out.winner == nil || out.winner.plan.sni != "cf3" {
		t.Fatalf("winner %+v, want cf3", out.winner)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("two 503s cost %v, the stagger was waited out", elapsed)
	}
	if len(out.attempts) != 2 {
		t.Fatalf("attempts %v, want the two 503s", out.attempts)
	}
}

func TestRaceCapsConcurrentAttempts(t *testing.T) {
	routes := map[string]fakeRoute{}
	var plans []transportPlan
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		routes[n] = fakeRoute{hang: true}
		plans = append(plans, cfPlan(n))
	}
	h := newRaceHarness(routes)
	r := h.race(time.Now().Add(400*time.Millisecond), 10*time.Millisecond, 2)
	r.timeoutFor = func(transportPlan) time.Duration { return 150 * time.Millisecond }
	out := r.run(plans)
	if out.winner != nil {
		t.Fatal("a hanging route won")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.peak > 2 {
		t.Fatalf("%d attempts in flight, cap is 2", h.peak)
	}
}

func TestRaceNeverRunsTwoNativeNamesAtOnce(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"kws2-1": {hang: true},
		"kws2":   {delay: 10 * time.Millisecond},
		"cf":     {hang: true},
	})
	r := h.race(time.Now().Add(time.Second), 10*time.Millisecond, 3)
	r.timeoutFor = func(transportPlan) time.Duration { return 200 * time.Millisecond }
	out := r.run([]transportPlan{nativePlan("kws2-1"), nativePlan("kws2"), cfPlan("cf")})
	if out.winner != nil {
		t.Fatalf("winner %s, want none: the sibling name shares the dead address", out.winner.plan.sni)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.peakNat > 1 {
		t.Fatalf("%d native dials at once", h.peakNat)
	}
	for _, s := range h.started {
		if s == "kws2" {
			t.Fatal("second native name dialled after the first timed out")
		}
	}
	if out.untried != 1 {
		t.Fatalf("untried = %d, want 1", out.untried)
	}
}

func TestRaceTriesTheSiblingNameAfterARedirect(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"kws2-1": {delay: 5 * time.Millisecond, err: &wsHandshakeError{statusCode: 302, statusLine: "302 Found"}},
		"kws2":   {delay: 5 * time.Millisecond},
	})
	out := h.race(time.Now().Add(time.Second), time.Second, 3).run([]transportPlan{nativePlan("kws2-1"), nativePlan("kws2")})
	if out.winner == nil || out.winner.plan.sni != "kws2" {
		t.Fatalf("winner %+v, want kws2", out.winner)
	}
	if out.nativeTried != 1 || out.nativeRedirects != 1 {
		t.Fatalf("native tried=%d redirects=%d", out.nativeTried, out.nativeRedirects)
	}
}

func TestRaceStopsStartingWhenTheBudgetIsSpent(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{"a": {hang: true}, "b": {hang: true}, "c": {hang: true}})
	r := h.race(time.Now().Add(150*time.Millisecond), 80*time.Millisecond, 3)
	r.minAttempt = 100 * time.Millisecond
	out := r.run([]transportPlan{cfPlan("a"), cfPlan("b"), cfPlan("c")})
	h.mu.Lock()
	started := len(h.started)
	h.mu.Unlock()
	if started != 1 {
		t.Fatalf("%d routes started with no budget left for a second", started)
	}
	if out.untried != 2 {
		t.Fatalf("untried = %d, want 2", out.untried)
	}
}

func TestRaceHandsASlowerSuccessToSpare(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"slow": {delay: 150 * time.Millisecond},
		"fast": {delay: 10 * time.Millisecond},
	})
	out := h.race(time.Now().Add(time.Second), 20*time.Millisecond, 3).run([]transportPlan{cfPlan("slow"), cfPlan("fast")})
	if out.winner == nil || out.winner.plan.sni != "fast" {
		t.Fatalf("winner %+v, want fast", out.winner)
	}
	select {
	case a := <-h.spares:
		if a.plan.sni != "slow" {
			t.Fatalf("spare %s, want slow", a.plan.sni)
		}
	case <-time.After(time.Second):
		t.Fatal("the slower success was never handed on")
	}
}

func TestRaceSkipsAWinnerTheSessionRejects(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"first":  {delay: 5 * time.Millisecond},
		"second": {delay: 5 * time.Millisecond},
	})
	r := h.race(time.Now().Add(time.Second), time.Second, 3)
	rejected := false
	r.accept = func(a raceAttempt) error {
		if a.plan.sni == "first" {
			rejected = true
			return errors.New("send handshake: broken pipe")
		}
		return nil
	}
	out := r.run([]transportPlan{cfPlan("first"), cfPlan("second")})
	if !rejected || out.winner == nil || out.winner.plan.sni != "second" {
		t.Fatalf("winner %+v after rejecting the first", out.winner)
	}
}

func cfPenalized(domain string) bool {
	cfBalancerInst.mu.Lock()
	defer cfBalancerInst.mu.Unlock()
	t, ok := cfBalancerInst.cooldown[domain]
	return ok && time.Now().Before(t)
}

func clearCFPenalty(domain string) {
	cfBalancerInst.mu.Lock()
	delete(cfBalancerInst.cooldown, domain)
	cfBalancerInst.mu.Unlock()
}

func TestStarvedTimeoutDoesNotPenalizeTheDomain(t *testing.T) {
	const domain = "starved.example.co.uk"
	t.Cleanup(func() { clearCFPenalty(domain) })
	timeout := &net.OpError{Op: "read", Err: timeoutError{}}
	plan := transportPlan{kind: transportWS, dc: 2, sni: "kws2." + domain, cfBase: domain}

	recordDialFailure(2, raceAttempt{plan: plan, err: timeout, timeout: wsDialTimeout / 2})
	if cfPenalized(domain) {
		t.Fatal("a domain cut short by the budget was penalised")
	}
	recordDialFailure(2, raceAttempt{plan: plan, err: timeout, timeout: wsDialTimeout})
	if !cfPenalized(domain) {
		t.Fatal("a domain silent for the full timeout was not penalised")
	}
}

func TestConnectStageIsToldApartFromHandshake(t *testing.T) {
	connect := &net.OpError{Op: "dial", Err: timeoutError{}}
	if !isConnectStage(wrapTCPDial(connect)) {
		t.Fatal("a connect timeout was not recognised")
	}
	if isConnectStage(&net.OpError{Op: "read", Err: timeoutError{}}) {
		t.Fatal("a handshake read timeout was taken for a connect timeout")
	}
}

func wrapTCPDial(err error) error {
	return &wrappedErr{err}
}

type wrappedErr struct{ err error }

func (w *wrappedErr) Error() string { return "tcp dial 149.154.167.220: " + w.err.Error() }
func (w *wrappedErr) Unwrap() error { return w.err }

func tcpPlan(addr string) transportPlan {
	return transportPlan{kind: transportTCP, dc: 2, addr: addr}
}

func workerPlan(sni string) transportPlan {
	return transportPlan{kind: transportWS, dc: 2, sni: sni, dialHost: sni, isWorker: true}
}

func (h *raceHarness) startedNames() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.started...)
}

func TestRaceNeverLetsALowerTierOvertake(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"cf":     {delay: 400 * time.Millisecond},
		"worker": {delay: 5 * time.Millisecond},
		"":       {delay: 5 * time.Millisecond},
	})
	out := h.race(time.Now().Add(3*time.Second), 20*time.Millisecond, 3).run([]transportPlan{cfPlan("cf"), workerPlan("worker"), tcpPlan("149.154.167.51:443")})
	if out.winner == nil || out.winner.plan.sni != "cf" {
		t.Fatalf("winner %+v, want the WebSocket route", out.winner)
	}
	if got := h.startedNames(); len(got) != 1 {
		t.Fatalf("started %v while the first tier was still in flight", got)
	}
}

func TestRaceMovesToTheNextTierOnceOneIsSpent(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"cf": {delay: 5 * time.Millisecond, err: errFake503},
		"":   {delay: 5 * time.Millisecond},
	})
	out := h.race(time.Now().Add(3*time.Second), time.Second, 3).run([]transportPlan{cfPlan("cf"), tcpPlan("149.154.167.51:443")})
	if out.winner == nil || out.winner.plan.kind != transportTCP {
		t.Fatalf("winner %+v, want the TCP route after the WebSocket tier failed", out.winner)
	}
}

func TestRaceTriesTheSiblingAfterAHandshakeTimeout(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"kws2-1": {tlsHang: true},
		"kws2":   {delay: 5 * time.Millisecond},
		"cf":     {hang: true},
	})
	r := h.race(time.Now().Add(2*time.Second), 20*time.Millisecond, 3)
	r.timeoutFor = func(transportPlan) time.Duration { return 150 * time.Millisecond }
	out := r.run([]transportPlan{nativePlan("kws2-1"), nativePlan("kws2"), cfPlan("cf")})
	if out.winner == nil || out.winner.plan.sni != "kws2" {
		t.Fatalf("winner %+v, want kws2: a filtered name says nothing about its sibling", out.winner)
	}
}

func TestRaceKeepsTheSiblingWhenNothingElseIsLeft(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"kws2-1": {hang: true},
		"kws2":   {delay: 5 * time.Millisecond},
	})
	r := h.race(time.Now().Add(2*time.Second), 20*time.Millisecond, 3)
	r.timeoutFor = func(transportPlan) time.Duration { return 150 * time.Millisecond }
	out := r.run([]transportPlan{nativePlan("kws2-1"), nativePlan("kws2")})
	if out.winner == nil || out.winner.plan.sni != "kws2" {
		t.Fatalf("winner %+v, want kws2 when it is the only route left", out.winner)
	}
}

func TestRaceGivesTheWorkerATurnWhileTheSharedDomainsHang(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"cf1":    {hang: true},
		"cf2":    {hang: true},
		"cf3":    {hang: true},
		"cf4":    {hang: true},
		"worker": {delay: 5 * time.Millisecond},
	})
	r := h.race(time.Now().Add(3*time.Second), 20*time.Millisecond, 3)
	r.workerAfter = 200 * time.Millisecond
	r.timeoutFor = func(transportPlan) time.Duration { return time.Second }
	t.Cleanup(h.waitIdle)
	start := time.Now()
	out := r.run([]transportPlan{cfPlan("cf1"), cfPlan("cf2"), cfPlan("cf3"), cfPlan("cf4"), workerPlan("worker")})
	if out.winner == nil || !out.winner.plan.isWorker {
		t.Fatalf("winner %+v, want the Worker once the shared domains had their head start", out.winner)
	}
	if elapsed := time.Since(start); elapsed > 600*time.Millisecond {
		t.Fatalf("the Worker waited %v behind hanging domains", elapsed)
	}
}

func (h *raceHarness) waitIdle() {
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		h.mu.Lock()
		n := h.inFlight
		h.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRaceRedialsAPooledConnThatDiedAtTheHandshake(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{"worker": {delay: 5 * time.Millisecond}})
	r := h.race(time.Now().Add(2*time.Second), time.Second, 3)
	var dials []bool
	var mu sync.Mutex
	r.dial = func(p transportPlan, timeout time.Duration, fresh bool) (net.Conn, bool, error) {
		mu.Lock()
		dials = append(dials, fresh)
		mu.Unlock()
		return &raceConn{}, !fresh, nil
	}
	r.accept = func(a raceAttempt) error {
		if a.pooled {
			return errors.New("send handshake: broken pipe")
		}
		return nil
	}
	out := r.run([]transportPlan{workerPlan("worker")})
	if out.winner == nil || out.winner.pooled {
		t.Fatalf("winner %+v, want a fresh dial after the pooled conn died", out.winner)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dials) != 2 || dials[0] || !dials[1] {
		t.Fatalf("dials %v, want one pooled then one fresh", dials)
	}
}

func TestRaceKeepsACooledWorkerBehindTheSharedDomains(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"cf1":    {hang: true},
		"worker": {delay: 5 * time.Millisecond},
	})
	r := h.race(time.Now().Add(2*time.Second), 20*time.Millisecond, 3)
	r.workerAfter = 100 * time.Millisecond
	r.earlyOK = func(p transportPlan) bool { return p.sni != "worker" }
	r.timeoutFor = func(transportPlan) time.Duration { return 400 * time.Millisecond }
	t.Cleanup(h.waitIdle)
	start := time.Now()
	out := r.run([]transportPlan{cfPlan("cf1"), workerPlan("worker")})
	if out.winner == nil || !out.winner.plan.isWorker {
		t.Fatalf("winner %+v, want the Worker once the shared domain failed", out.winner)
	}
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
		t.Fatalf("a cooled Worker started after %v, ahead of the shared domain it is ranked behind", elapsed)
	}
}

func TestRaceTimesTheWorkerFromTheTierAheadOfIt(t *testing.T) {
	h := newRaceHarness(map[string]fakeRoute{
		"":       {hang: true},
		"cf1":    {delay: 100 * time.Millisecond},
		"worker": {delay: 5 * time.Millisecond},
	})
	r := h.race(time.Now().Add(3*time.Second), 20*time.Millisecond, 3)
	r.workerAfter = 200 * time.Millisecond
	r.timeoutFor = func(transportPlan) time.Duration { return 300 * time.Millisecond }
	t.Cleanup(h.waitIdle)
	out := r.run([]transportPlan{tcpPlan("203.0.113.9:443"), cfPlan("cf1"), workerPlan("worker")})
	if out.winner == nil || out.winner.plan.sni != "cf1" {
		t.Fatalf("winner %+v, want the WebSocket route: the Worker's wait starts when that tier does", out.winner)
	}
}
