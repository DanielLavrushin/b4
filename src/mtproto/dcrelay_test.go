package mtproto

import (
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

const (
	planRelay    = "127.0.0.1:4443"
	planRelayDC4 = "127.0.0.1:4446"
)

func withCleanRelayState(t *testing.T) {
	t.Helper()
	reset := func() {
		relayResetState()
		tcpResetState()
		workerResetStall()
	}
	reset()
	t.Cleanup(reset)
}

func planNames(plans []transportPlan) []string {
	out := make([]string, 0, len(plans))
	for _, p := range plans {
		out = append(out, p.describe())
	}
	return out
}

func relayIndex(plans []transportPlan) int {
	for i, p := range plans {
		if p.relay {
			return i
		}
	}
	return -1
}

func bridgeRelayConfig() *config.MTProtoConfig {
	return &config.MTProtoConfig{
		UpstreamMode:   "auto",
		DCRelay:        planRelay,
		CFWorkerDomain: "w.user.workers.dev",
	}
}

func TestBridgeTriesTheRelayAfterEveryWebSocketRoute(t *testing.T) {
	withCleanRelayState(t)
	target := dialTarget{ip: "149.154.167.92", port: 443}
	plans, err := planTransports(bridgeRelayConfig(), config.QueueConfig{IPv4Enabled: true}, 4, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	at := relayIndex(plans)
	if at < 0 {
		t.Fatalf("a bridged session got no relay route: %v", planNames(plans))
	}
	if plans[at].kind != transportTCP || plans[at].addr != planRelayDC4 {
		t.Fatalf("relay route %q, want tcp://%s", plans[at].describe(), planRelayDC4)
	}
	sawWorker := false
	for i, p := range plans {
		if p.kind != transportWS {
			continue
		}
		if i > at {
			t.Fatalf("%q is listed behind the relay: %v", p.describe(), planNames(plans))
		}
		sawWorker = sawWorker || p.isWorker
	}
	if !sawWorker || len(wsSNIs(plans)) < 2 {
		t.Fatalf("the WebSocket routes and the Worker must stay ahead of the relay: %v", planNames(plans))
	}
	for i, p := range plans {
		if p.relay && i != at {
			t.Fatalf("more than one relay route: %v", planNames(plans))
		}
	}
}

func TestBridgeKeepsTheAddressTheClientDialledBehindTheRelay(t *testing.T) {
	withCleanRelayState(t)
	target := dialTarget{ip: "149.154.167.92", port: 443}
	plans, err := planTransports(bridgeRelayConfig(), config.QueueConfig{IPv4Enabled: true}, 4, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	at := relayIndex(plans)
	if at < 0 {
		t.Fatalf("no relay route: %v", planNames(plans))
	}
	rest := plans[at+1:]
	if len(rest) < 2 || rest[0].addr != "149.154.167.92:443" || rest[1].addr != "149.154.167.91:443" {
		t.Fatalf("direct TCP to the dialled address and the data centre must follow the relay: %v", planNames(plans))
	}
	for _, p := range rest {
		if p.relay {
			t.Fatalf("a direct route carries the relay mark: %v", planNames(plans))
		}
	}
}

func TestBridgeLeavesDC203OffTheRelay(t *testing.T) {
	withCleanRelayState(t)
	target := dialTarget{ip: "91.105.192.100", port: 443}
	for _, dc := range []int{203, -203} {
		plans, err := planTransports(bridgeRelayConfig(), config.QueueConfig{IPv4Enabled: true}, dc, target)
		if err != nil {
			t.Fatalf("DC %d: unexpected error: %v", dc, err)
		}
		if relayIndex(plans) >= 0 {
			t.Fatalf("DC %d was sent to the relay, which has no port for it: %v", dc, planNames(plans))
		}
		direct := false
		for _, p := range plans {
			direct = direct || (p.kind == transportTCP && p.addr == "91.105.192.100:443")
		}
		if !direct {
			t.Fatalf("DC %d lost its direct route: %v", dc, planNames(plans))
		}
	}
}

func TestProxyKeepsTheRelayFirstAndInPlaceOfDirectTCP(t *testing.T) {
	withCleanRelayState(t)
	plans, err := planTransports(bridgeRelayConfig(), config.QueueConfig{IPv4Enabled: true}, 4, dialTarget{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if relayIndex(plans) != 0 || plans[0].addr != planRelayDC4 {
		t.Fatalf("the proxy must still dial the relay first: %v", planNames(plans))
	}
	for _, p := range plans[1:] {
		if p.kind == transportTCP {
			t.Fatalf("the proxy gained a direct route beside its relay: %v", planNames(plans))
		}
	}
	tcpOnly, err := planTransports(&config.MTProtoConfig{UpstreamMode: "tcp", DCRelay: planRelay}, config.QueueConfig{IPv4Enabled: true}, 4, dialTarget{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tcpOnly) != 1 || !tcpOnly[0].relay {
		t.Fatalf("Direct TCP with a relay must be the relay alone: %v", planNames(tcpOnly))
	}
}

func TestBridgePutsALeadingRelayFirst(t *testing.T) {
	withCleanRelayState(t)
	target := dialTarget{ip: "149.154.167.92", port: 443}
	relayLeadNote(planRelayDC4)

	plans, err := planTransports(bridgeRelayConfig(), config.QueueConfig{IPv4Enabled: true}, 4, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if relayIndex(plans) != 0 {
		t.Fatalf("a relay that carried the last session must lead: %v", planNames(plans))
	}
	if len(wsSNIs(plans)) == 0 {
		t.Fatalf("the WebSocket routes must stay behind a leading relay: %v", planNames(plans))
	}
	last := plans[len(plans)-1]
	if last.kind != transportTCP || last.relay {
		t.Fatalf("direct TCP must stay last, got %q: %v", last.describe(), planNames(plans))
	}
	if plans[1].kind == transportTCP {
		t.Fatalf("direct TCP followed the relay to the front, where it would hold the WebSocket routes back: %v", planNames(plans))
	}

	other, err := planTransports(bridgeRelayConfig(), config.QueueConfig{IPv4Enabled: true}, 2, dialTarget{ip: "149.154.167.51", port: 443})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if relayIndex(other) == 0 {
		t.Fatalf("the relay leads for a data centre it was never needed for: %v", planNames(other))
	}

	proxy, err := planTransports(&config.MTProtoConfig{UpstreamMode: "ws", DCRelay: planRelay}, config.QueueConfig{IPv4Enabled: true}, 4, dialTarget{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasTCP(proxy) {
		t.Fatalf("WebSocket only gained a relay route: %v", planNames(proxy))
	}
}

func TestBridgeLeavesACoolingRelayOut(t *testing.T) {
	withCleanRelayState(t)
	target := dialTarget{ip: "149.154.167.92", port: 443}
	relayLeadNote(planRelayDC4)
	tcpRecordFailure(planRelayDC4)

	plans, err := planTransports(bridgeRelayConfig(), config.QueueConfig{IPv4Enabled: true}, 4, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if relayIndex(plans) >= 0 {
		t.Fatalf("a cooling relay is still listed: %v", planNames(plans))
	}
}

func TestRelayLeadLapsesAndClears(t *testing.T) {
	withCleanRelayState(t)
	const addr = "203.0.113.7:7008"
	if relayLeads(addr) {
		t.Fatal("a relay leads before it carried anything")
	}
	relayLeadNote(addr)
	if !relayLeads(addr) {
		t.Fatal("a relay that carried a session from behind does not lead")
	}
	relayLeadClear(addr)
	if relayLeads(addr) {
		t.Fatal("the lead survived being cleared")
	}

	relayLeadNote(addr)
	relayLeadMu.Lock()
	relayLeadUntil[addr] = time.Now().Add(-time.Second)
	relayLeadMu.Unlock()
	if relayLeads(addr) {
		t.Fatal("the lead did not lapse")
	}
	relayLeadMu.Lock()
	_, kept := relayLeadUntil[addr]
	relayLeadMu.Unlock()
	if kept {
		t.Fatal("a lapsed entry was kept")
	}
}

func relayedInfo(addr string, fromBehind bool) dialInfo {
	return dialInfo{
		transport:  "tcp://" + addr,
		plan:       transportPlan{kind: transportTCP, addr: addr, relay: true},
		fromBehind: fromBehind,
	}
}

func TestRelayLeadIsEarnedOnlyFromBehindAndOnlyOnTheRelay(t *testing.T) {
	withCleanRelayState(t)
	const addr = "203.0.113.7:7008"
	if relayAnswered(relayedInfo(addr, false)) != nil {
		t.Fatal("a relay that was already first in line has no lead to earn")
	}
	direct := dialInfo{plan: transportPlan{kind: transportTCP, addr: "149.154.167.91:443"}, fromBehind: true}
	if relayAnswered(direct) != nil || relayUnanswered(direct) != nil {
		t.Fatal("direct TCP was treated as the relay")
	}
	worker := dialInfo{isWorker: true, worker: "w.example.workers.dev", fromBehind: true}
	if relayAnswered(worker) != nil || relayUnanswered(worker) != nil {
		t.Fatal("a Worker was treated as the relay")
	}
	earn := relayAnswered(relayedInfo(addr, true))
	if earn == nil {
		t.Fatal("a relay that won from behind cannot earn the lead")
	}
	earn()
	if !relayLeads(addr) {
		t.Fatal("an answer through the relay did not give it the lead")
	}
	relayUnanswered(relayedInfo(addr, false))()
	if relayLeads(addr) {
		t.Fatal("a session the relay left unanswered did not end its lead")
	}
}

func TestOverlappingFallbackSessionsDoNotRenewTheLead(t *testing.T) {
	withCleanRelayState(t)
	const addr = "203.0.113.7:7008"
	first := relayAnswered(relayedInfo(addr, true))
	second := relayAnswered(relayedInfo(addr, true))
	first()

	until := time.Now().Add(10 * time.Second)
	relayLeadMu.Lock()
	relayLeadUntil[addr] = until
	relayLeadMu.Unlock()
	second()
	relayLeadMu.Lock()
	got := relayLeadUntil[addr]
	relayLeadMu.Unlock()
	if !got.Equal(until) {
		t.Fatalf("a second session that fell back before the lead began moved its end by %v, so the WebSocket routes are retried later than every %v", got.Sub(until).Round(time.Second), relayLeadFor)
	}

	relayLeadMu.Lock()
	relayLeadUntil[addr] = time.Now().Add(-time.Second)
	relayLeadMu.Unlock()
	second()
	if !relayLeads(addr) {
		t.Fatal("a lead that had lapsed could not be earned again")
	}
}

func relayHooks(t *testing.T, addr string) relayOpts {
	t.Helper()
	info := relayedInfo(addr, true)
	return relayOpts{label: "test", bufPool: relayPool(), onAnswer: relayAnswered(info), onUnanswered: relayUnanswered(info)}
}

func TestRelaySessionEarnsTheLeadOnTheFirstAnswer(t *testing.T) {
	withCleanRelayState(t)
	const addr = "203.0.113.7:7008"
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()

	leading := make(chan bool, 2)
	go func() {
		buf := make([]byte, 512)
		_, _ = dcA.Read(buf)
		leading <- relayLeads(addr)
		_, _ = dcA.Write([]byte("an answer"))
		_, _ = dcA.Write([]byte("more of it"))
	}()
	go func() {
		_, _ = clientA.Write([]byte("a request"))
		buf := make([]byte, 512)
		_, _ = clientA.Read(buf)
		leading <- relayLeads(addr)
		_, _ = clientA.Read(buf)
		_ = clientA.Close()
	}()

	relayConns(clientB, dcB, relayHooks(t, addr))
	_ = dcA.Close()

	if before := <-leading; before {
		t.Fatal("the relay led before it had answered anything")
	}
	if after := <-leading; !after {
		t.Fatal("the first answer through the relay did not give it the lead")
	}
	if !relayLeads(addr) {
		t.Fatal("an answered session ended the lead")
	}
}

func TestRelayThatClosesWithoutAnsweringLosesTheLead(t *testing.T) {
	withCleanRelayState(t)
	const addr = "203.0.113.7:7008"
	relayLeadNote(addr)
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer clientA.Close()

	go func() { _, _ = clientA.Write([]byte("a request nobody answers")) }()
	go func() {
		buf := make([]byte, 512)
		_, _ = dcA.Read(buf)
		_ = dcA.Close()
	}()

	relayConns(clientB, dcB, relayHooks(t, addr))
	if relayLeads(addr) {
		t.Fatal("a relay that took a request and closed without an answer still leads")
	}
}

func TestRelayThatStaysMuteLosesTheLead(t *testing.T) {
	withFastStall(t)
	withCleanRelayState(t)
	const addr = "203.0.113.7:7008"
	relayLeadNote(addr)
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer dcA.Close()

	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := dcA.Read(buf); err != nil {
				return
			}
		}
	}()
	go func() {
		_, _ = clientA.Write([]byte("a request nobody answers"))
		time.Sleep(relayAnswerDue + 100*time.Millisecond)
		_ = clientA.Close()
	}()

	relayConns(clientB, dcB, relayHooks(t, addr))
	if relayLeads(addr) {
		t.Fatal("a relay that stayed mute past the time an answer was due still leads")
	}
}

func TestClientThatLeavesBeforeAnAnswerIsDueKeepsTheLead(t *testing.T) {
	withCleanRelayState(t)
	const addr = "203.0.113.7:7008"
	relayLeadNote(addr)
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer dcA.Close()

	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := dcA.Read(buf); err != nil {
				return
			}
		}
	}()
	go func() {
		_, _ = clientA.Write([]byte("a request"))
		time.Sleep(50 * time.Millisecond)
		_ = clientA.Close()
	}()

	relayConns(clientB, dcB, relayHooks(t, addr))
	if !relayLeads(addr) {
		t.Fatal("a client that left before an answer was due cost the relay its lead")
	}
}

func muteUpstream(dc net.Conn) {
	buf := make([]byte, 512)
	for {
		if _, err := dc.Read(buf); err != nil {
			return
		}
	}
}

func TestClientThatLeavesRightAfterADelayedFirstRequestKeepsTheLead(t *testing.T) {
	withFastStall(t)
	withCleanRelayState(t)
	const addr = "203.0.113.7:7008"
	relayLeadNote(addr)
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer dcA.Close()

	go muteUpstream(dcA)
	go func() {
		time.Sleep(relayAnswerDue + 100*time.Millisecond)
		_, _ = clientA.Write([]byte("a first request after an idle start"))
		time.Sleep(20 * time.Millisecond)
		_ = clientA.Close()
	}()

	relayConns(clientB, dcB, relayHooks(t, addr))
	if !relayLeads(addr) {
		t.Fatal("the idle time before the first request was counted as time the relay had to answer it")
	}
}

func TestClientRetryingIntoAMuteRelayStillCostsItTheLead(t *testing.T) {
	withFastStall(t)
	withCleanRelayState(t)
	const addr = "203.0.113.7:7008"
	relayLeadNote(addr)
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer dcA.Close()

	go muteUpstream(dcA)
	go func() {
		_, _ = clientA.Write([]byte("a request nobody answers"))
		time.Sleep(relayAnswerDue - 30*time.Millisecond)
		_, _ = clientA.Write([]byte("the retry nobody answers either"))
		time.Sleep(80 * time.Millisecond)
		_ = clientA.Close()
	}()

	relayConns(clientB, dcB, relayHooks(t, addr))
	if relayLeads(addr) {
		t.Fatal("a retry moved the clock forward, so a relay that stayed mute past the time an answer was due kept its lead")
	}
}

func TestStallReporterLeavesTheRelayAlone(t *testing.T) {
	if stallReporter(relayedInfo("203.0.113.7:7008", true)) != nil {
		t.Fatal("a relay session gained the stall watchdog, which also cuts the proxy's relay sessions")
	}
}

func TestProxyStepsOverACoolingRelay(t *testing.T) {
	withCleanRelayState(t)
	tcpRecordFailure(planRelayDC4)
	plans, err := planTransports(&config.MTProtoConfig{UpstreamMode: "auto", DCRelay: planRelay}, config.QueueConfig{IPv4Enabled: true}, 4, dialTarget{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasTCP(plans) || len(wsSNIs(plans)) == 0 {
		t.Fatalf("a cooling relay must leave the proxy its WebSocket routes only: %v", planNames(plans))
	}
}

func TestRejectedRelaySessionEndsTheLead(t *testing.T) {
	withCleanRelayState(t)
	const addr = "203.0.113.7:7008"
	relayLeadNote(addr)
	demoteRejectedRoute(dialInfo{plan: transportPlan{kind: transportTCP, addr: addr, relay: true}}, 2)
	if relayLeads(addr) {
		t.Fatal("a relay that landed a session on the wrong data centre still leads")
	}
	if !tcpAddrInCooldown(addr) {
		t.Fatal("the rejected relay was not cooled")
	}
}

func TestRelayDialFailureEndsTheLead(t *testing.T) {
	withCleanRelayState(t)
	const addr = "203.0.113.7:7008"
	relayLeadNote(addr)
	refused := &net.OpError{Op: "dial", Err: errConnRefused{}}
	recordDialFailure(2, raceAttempt{plan: transportPlan{kind: transportTCP, addr: addr, relay: true}, err: refused})
	if relayLeads(addr) {
		t.Fatal("a relay that refused the dial still leads")
	}
}

type errConnRefused struct{}

func (errConnRefused) Error() string { return "connect: connection refused" }

func TestRelayTurnFitsInsideTheDialBudget(t *testing.T) {
	if dialRaceRelayAfter <= dialRaceWorkerAfter {
		t.Fatalf("the relay's turn at %v must come after the Worker's at %v", dialRaceRelayAfter, dialRaceWorkerAfter)
	}
	if dialRaceRelayAfter+wsDialMinAttempt > dialBudget {
		t.Fatalf("no room to dial the relay: turn at %v + minimum %v > budget %v", dialRaceRelayAfter, wsDialMinAttempt, dialBudget)
	}
}

func TestBridgeUpstreamKeepsTheRelay(t *testing.T) {
	cfg := &config.Config{}
	cfg.System.MTProto.UpstreamMode = "ws"
	cfg.System.MTProto.DCRelay = planRelay
	mt := bridgeUpstream(cfg)
	if mt.DCRelay != planRelay {
		t.Fatalf("the bridge dropped the relay: %q", mt.DCRelay)
	}
	if mt.UpstreamMode != "auto" {
		t.Fatalf("the bridge must dial in auto mode, got %q", mt.UpstreamMode)
	}
	if cfg.System.MTProto.UpstreamMode != "ws" {
		t.Fatal("the bridge changed the shared configuration")
	}
}

func TestBridgeDialFallsBackToTheRelayAndThenLeadsWithIt(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	withCleanRelayState(t)
	withWSDialPort(t, blackholeListener(t))

	relayPort, err := strconv.Atoi(blackholeListener(t))
	if err != nil {
		t.Fatalf("relay port: %v", err)
	}
	relayTo := net.JoinHostPort("127.0.0.1", strconv.Itoa(relayPort))
	cfg := &config.MTProtoConfig{
		UpstreamMode:   "auto",
		WSEndpointHost: "127.0.0.1",
		DCRelay:        net.JoinHostPort("127.0.0.1", strconv.Itoa(relayPort-1)),
	}
	target := dialTarget{ip: "127.0.0.1", port: 443}

	start := time.Now()
	conn, info, err := dialObfuscatedDC(cfg, config.QueueConfig{IPv4Enabled: true}, 2, connectionTagAbridged, nil, "", target)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("the relay did not carry the session behind a silent edge: %v", err)
	}
	_ = conn.Close()
	if !info.plan.relay || info.plan.addr != relayTo {
		t.Fatalf("connected via %q, want the relay at %s", info.transport, relayTo)
	}
	if elapsed < dialRaceRelayAfter {
		t.Fatalf("the relay won after %v, ahead of the %v the WebSocket routes are given", elapsed, dialRaceRelayAfter)
	}
	if elapsed > dialRaceRelayAfter+600*time.Millisecond {
		t.Fatalf("the relay waited %v behind a silent edge", elapsed)
	}
	if relayLeads(relayTo) {
		t.Fatal("the relay leads on a connect alone, which a relay that cannot reach Telegram passes too")
	}
	earn := relayAnswered(info)
	if earn == nil {
		t.Fatal("a relay that won from behind cannot earn the lead")
	}
	earn()

	start = time.Now()
	conn, info, err = dialObfuscatedDC(cfg, config.QueueConfig{IPv4Enabled: true}, 2, connectionTagAbridged, nil, "", target)
	if err != nil {
		t.Fatalf("second dial failed: %v", err)
	}
	_ = conn.Close()
	if !info.plan.relay {
		t.Fatalf("second dial connected via %q, want the relay", info.transport)
	}
	if relayAnswered(info) != nil {
		t.Fatal("a relay that led the dial would renew its own lead, so the WebSocket routes would never be tried again")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("a leading relay still waited %v", elapsed)
	}

	settled := time.Now().Add(wsDialTimeout + 2*time.Second)
	for !wsCooldownActive(2) && time.Now().Before(settled) {
		time.Sleep(50 * time.Millisecond)
	}
}
