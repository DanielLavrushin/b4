package mtproto

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

// blackholeListener accepts TCP and then says nothing, which is what a filtered
// route looks like from the dialler: the handshake completes, the ClientHello
// goes unanswered, and the dial ends on the timeout rather than on an error.
func blackholeListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				<-done
				_ = c.Close()
			}(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split listener addr: %v", err)
	}
	return port
}

func withWSDialPort(t *testing.T, port string) {
	t.Helper()
	prev := wsDialPort
	wsDialPort = port
	t.Cleanup(func() { wsDialPort = prev })

	prevMark := selfDialMark
	selfDialMark = func() uint { return 0 }
	t.Cleanup(func() { selfDialMark = prevMark })
}

func TestDialBudgetBoundsWhatAClientWaits(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	withWSDialPort(t, blackholeListener(t))

	cfg := &config.MTProtoConfig{
		UpstreamMode:   "ws",
		WSEndpointHost: "127.0.0.1",
	}

	start := time.Now()
	_, _, err := dialObfuscatedDC(cfg, config.QueueConfig{}, 2, connectionTagAbridged, nil, "", dialTarget{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("dial against a blackhole succeeded")
	}
	// The budget is the contract: a client that waits longer than this has
	// already given up and reported the proxy as misconfigured, so anything
	// spent past it is spent for nobody.
	if limit := dialBudget + time.Second; elapsed > limit {
		t.Fatalf("dial took %v against two dead routes, want <= %v", elapsed, limit)
	}

	// The cooldown used to be recorded only once every transport had failed, so
	// an edge rescued by a later route stayed first in line, at full price, for
	// the next session as well.
	if !wsCooldownActive(2) {
		t.Fatal("a native edge that timed out was not put into cooldown")
	}
}

func TestDialBudgetFitsInsideWhatTheClientWaits(t *testing.T) {
	// Measured on a censored network: every session whose dial ran past five
	// seconds was closed by the client before the relay started. These bounds
	// are what keeps a single dead route from spending that window.
	const clientPatience = 5 * time.Second
	if dialBudget >= clientPatience {
		t.Fatalf("dialBudget %v must stay under the %v a client waits", dialBudget, clientPatience)
	}
	if wsDialTimeout > dialBudget {
		t.Fatalf("wsDialTimeout %v exceeds the whole dial budget %v", wsDialTimeout, dialBudget)
	}
	if tcpDialTimeout > dialBudget {
		t.Fatalf("tcpDialTimeout %v exceeds the whole dial budget %v", tcpDialTimeout, dialBudget)
	}
	// Two attempts have to fit, or a failover is not reachable at all.
	if wsDialTimeout+wsDialMinAttempt > dialBudget {
		t.Fatalf("no room for a second route: wsDialTimeout %v + minimum %v > budget %v",
			wsDialTimeout, wsDialMinAttempt, dialBudget)
	}
}

// idleWSConn is a wsConn whose reads block, which is what alive() has to see to
// call a pooled conn usable. It is backed by a real socket rather than a pipe so
// the TLS handshake write lands in the kernel buffer instead of deadlocking.
func idleWSConn(t *testing.T, port string) *wsConn {
	t.Helper()
	raw, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		t.Fatalf("dial blackhole: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	tlsConn := tls.Client(raw, &tls.Config{InsecureSkipVerify: true, ServerName: "example.invalid"})
	return &wsConn{tls: tlsConn, br: bufio.NewReader(tlsConn)}
}

func TestCooledEdgeIsSteppedOverWhenAnotherRouteExists(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	withWSDialPort(t, blackholeListener(t))

	// A route that exists and fails on the name lookup, so there is a fallback
	// to step to without the test leaving the machine.
	cfg := &config.MTProtoConfig{
		UpstreamMode:   "ws",
		WSEndpointHost: "127.0.0.1",
		WSCustomDomain: "invalid",
	}
	wsEndpointFailed("127.0.0.1", "kws2.web.telegram.org")
	wsEndpointFailed("127.0.0.1", "kws2-1.web.telegram.org")

	start := time.Now()
	if _, _, err := dialObfuscatedDC(cfg, config.QueueConfig{}, 2, connectionTagAbridged, nil, "", dialTarget{}); err == nil {
		t.Fatal("dial succeeded with every route dead")
	}
	elapsed := time.Since(start)

	// Both edge names resolve to the same blackholed address. Trying them costs
	// the whole budget and teaches nothing the cooldown had not already
	// recorded, so neither should have been dialled.
	if elapsed > 2*wsDialMinAttempt+time.Second {
		t.Fatalf("dial took %v, so a cooled edge was still tried", elapsed)
	}
}

func TestOneAttemptCannotOutlastItsTimeout(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	withWSDialPort(t, blackholeListener(t))

	// Connect, TLS and upgrade share one deadline. Given a timeout each, an
	// attempt could run to twice what it was handed, and the budget it came out
	// of is spent by then.
	const timeout = 1200 * time.Millisecond
	start := time.Now()
	if _, err := dialWS("127.0.0.1", "kws2.web.telegram.org", "", timeout, 0); err == nil {
		t.Fatal("dial against a blackhole succeeded")
	}
	if elapsed := time.Since(start); elapsed > timeout+400*time.Millisecond {
		t.Fatalf("one attempt took %v on a %v timeout", elapsed, timeout)
	}
}

func TestAnAddressIsOnlyBlamedWhenItHadTimeToAnswer(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	withWSDialPort(t, blackholeListener(t))

	const sni = "kws2.web.telegram.org"

	// Too short to break an address on. Resolution is paid out of the same
	// budget, so a slow lookup can leave exactly this much behind - and cooling
	// the address off for five minutes on it would retire a healthy route.
	if _, err := dialWS("127.0.0.1", sni, "", wsDialMinAttempt/2, 0); err == nil {
		t.Fatal("dial against a blackhole succeeded")
	}
	if wsEndpointCooling("127.0.0.1", sni) {
		t.Fatal("address cooled off after a slot too short to judge it by")
	}

	// A fair trial, and the same silence, is evidence.
	if _, err := dialWS("127.0.0.1", sni, "", wsDialMinAttempt+300*time.Millisecond, 0); err == nil {
		t.Fatal("dial against a blackhole succeeded")
	}
	if !wsEndpointCooling("127.0.0.1", sni) {
		t.Fatal("address that went silent on a full slot was not cooled off")
	}
}

func TestSkipNativeWhenCoolingAndAnotherRouteExists(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)

	native := transportPlan{kind: transportWS, dc: 2, sni: "kws2.web.telegram.org", native: true}
	cf := transportPlan{kind: transportWS, dc: 2, sni: "kws2.example.co.uk", cfBase: "example.co.uk"}

	if hasNonNativePlan([]transportPlan{native}) {
		t.Error("a list of only native plans reported a non-native route")
	}
	if !hasNonNativePlan([]transportPlan{native, cf}) {
		t.Error("a list carrying a CF route reported none")
	}
}

func TestEndpointCooldownMovesAFailedAddressToTheBack(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)

	const sni = "kws203.example.co.uk"
	if wsEndpointCooling("172.67.0.1", sni) {
		t.Fatal("a fresh address reported as cooling")
	}

	wsEndpointFailed("172.67.0.1", sni)
	if !wsEndpointCooling("172.67.0.1", sni) {
		t.Fatal("an address that timed out is not cooling")
	}
	// One Cloudflare address fronts many names and a censor filters on the name,
	// so the same address under a different name must stay usable.
	if wsEndpointCooling("172.67.0.1", "kws203.other.co.uk") {
		t.Fatal("cooldown leaked to another server name on the same address")
	}

	wsEndpointRecovered("172.67.0.1", sni)
	if wsEndpointCooling("172.67.0.1", sni) {
		t.Fatal("cooldown survived the address answering again")
	}
}

func TestDialEndpointsPrefersAddressesThatHaveNotTimedOut(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)

	// An IP literal has nothing to resolve and no ordering to make.
	if got := wsDialEndpoints(context.Background(), "127.0.0.1", "kws2.example.co.uk"); len(got) != 1 || got[0] != "127.0.0.1" {
		t.Fatalf("literal address rewritten: %v", got)
	}
}

func TestWSPoolServesDataCentersWithNoTelegramEdge(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)

	port := blackholeListener(t)

	// No route configured, so the refill this get() schedules finds nothing to
	// dial and the test stays off the network. The gate under test is in get().
	p := newWSPool(MTProtoUpstream{}, 0, wsPoolDefaultSize)
	defer p.close()

	p.mu.Lock()
	p.idle[wsKeyFromDC(203)] = []wsPoolEntry{{conn: idleWSConn(t, port), created: time.Now()}}
	p.mu.Unlock()

	// DC 203 is where Russian accounts live and Telegram's edge does not serve
	// it, so the pool used to refuse it outright and every session there paid a
	// cold dial at the full timeout.
	if got := p.get(203); got == nil {
		t.Fatal("pool refused to serve DC 203")
	}
}

func TestWSPlansCoverDataCentersWithNoTelegramEdge(t *testing.T) {
	cfg := &MTProtoUpstream{CFProxyEnabled: true}

	plans := wsPlansForDC(203, cfg)
	if len(plans) == 0 {
		t.Fatal("DC 203 has no pool route, so it can never be warmed")
	}
	for _, p := range plans {
		if p.cfBase == "" {
			t.Errorf("DC 203 plan %s carries no CF domain to score", p.describe())
		}
		if p.native {
			t.Errorf("DC 203 plan %s claims to be Telegram's own edge", p.describe())
		}
	}

	// Turning the shared pool off must leave nothing behind to dial.
	if plans := wsPlansForDC(203, &MTProtoUpstream{}); len(plans) != 0 {
		t.Fatalf("DC 203 built %d plan(s) with every route disabled", len(plans))
	}

	// DC 2 keeps Telegram's own edge, and those plans have to be marked native
	// or the cooldown and the shortened timeout never apply to them.
	edge := wsPlansForDC(2, cfg)
	if len(edge) < 2 {
		t.Fatalf("DC 2 built %d plan(s), want both kws2 names", len(edge))
	}
	for _, p := range edge[:2] {
		if !p.native {
			t.Errorf("DC 2 edge plan %s is not marked native", p.describe())
		}
	}
}

func TestPoolHoldsFewerSparesOnSharedDomains(t *testing.T) {
	p := newWSPool(MTProtoUpstream{CFProxyEnabled: true}, 0, wsPoolDefaultSize)
	defer p.close()

	if got := p.targetFor(wsKeyFromDC(2)); got != wsPoolDefaultSize {
		t.Errorf("DC 2 target = %d, want %d", got, wsPoolDefaultSize)
	}
	if got := p.targetFor(wsKeyFromDC(203)); got != wsPoolCFTarget {
		t.Errorf("DC 203 target = %d, want %d (shared public domains)", got, wsPoolCFTarget)
	}
}

func TestWarmupCoversDC203OnlyWhenARouteExists(t *testing.T) {
	if got := wsWarmupDCs(&config.MTProtoConfig{}); containsInt(got, 203) {
		t.Errorf("warmed DC 203 with no route to it: %v", got)
	}
	got := wsWarmupDCs(&config.MTProtoConfig{CFProxyEnabled: true})
	for _, want := range []int{2, 4, 203} {
		if !containsInt(got, want) {
			t.Errorf("warmup %v is missing DC %d", got, want)
		}
	}
	if got := wsWarmupDCs(&config.MTProtoConfig{WSCustomDomain: "example.co.uk"}); !containsInt(got, 203) {
		t.Errorf("a custom domain is a route to DC 203, but warmup was %v", got)
	}
}

func containsInt(xs []int, want int) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
