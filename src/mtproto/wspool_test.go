package mtproto

import (
	"testing"
	"time"
)

func TestRefillBackoffDoublesUpToTheCap(t *testing.T) {
	cases := map[int]time.Duration{
		1:  time.Second,
		2:  2 * time.Second,
		4:  8 * time.Second,
		20: wsRefillBackoffMax,
	}
	for fails, want := range cases {
		if got := wsRefillBackoff(fails); got != want {
			t.Errorf("wsRefillBackoff(%d) = %v, want %v", fails, got, want)
		}
	}
}

func TestEndpointCooldownGrowsWhileItKeepsFailing(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	const ip, sni = "149.154.167.220", "kws2.web.telegram.org"
	k := wsEndpointKey(ip, sni)

	expire := func() {
		wsStateMu.Lock()
		wsEndpointTo[k] = time.Now().Add(-time.Second)
		wsStateMu.Unlock()
	}
	until := func() time.Duration {
		wsStateMu.Lock()
		defer wsStateMu.Unlock()
		return time.Until(wsEndpointTo[k])
	}

	wsEndpointFailed(ip, sni)
	if d := until(); d > wsEndpointFailTTL || d < wsEndpointFailTTL-time.Second {
		t.Fatalf("first cooldown %v, want %v", d, wsEndpointFailTTL)
	}
	wsEndpointFailed(ip, sni)
	if d := until(); d > wsEndpointFailTTL {
		t.Fatalf("a failure during the cooldown extended it to %v", d)
	}
	expire()
	wsEndpointFailed(ip, sni)
	if d := until(); d < 2*wsEndpointFailTTL-time.Second {
		t.Fatalf("second cooldown %v, want %v", d, 2*wsEndpointFailTTL)
	}
	for i := 0; i < 6; i++ {
		expire()
		wsEndpointFailed(ip, sni)
	}
	if d := until(); d > wsEndpointFailMaxTTL {
		t.Fatalf("cooldown grew past the cap: %v", d)
	}

	wsEndpointRecovered(ip, sni)
	wsEndpointFailed(ip, sni)
	if d := until(); d > wsEndpointFailTTL {
		t.Fatalf("a recovered endpoint kept its escalation: %v", d)
	}
}

func TestAddressCooldownCoversEveryNameOnIt(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	const ip = "149.154.167.220"

	wsAddressFailed(ip)
	for _, sni := range []string{"kws2.web.telegram.org", "kws2-1.web.telegram.org", "kws4.web.telegram.org"} {
		if !wsEndpointCooling(ip, sni) {
			t.Errorf("%s is not cooling after its address stopped answering", sni)
		}
	}
	wsStateMu.Lock()
	addrTTL := time.Until(wsEndpointTo[wsAddressKey(ip)])
	wsStateMu.Unlock()
	if addrTTL > wsAddressFailTTL {
		t.Errorf("a first connect timeout cooled the address for %v, want %v", addrTTL, wsAddressFailTTL)
	}
	if wsEndpointCooling("149.154.167.99", "kws2.web.telegram.org") {
		t.Error("the cooldown leaked to another address")
	}
	wsEndpointRecovered(ip, "kws4.web.telegram.org")
	if wsEndpointCooling(ip, "kws2.web.telegram.org") {
		t.Error("an answer on one name left the address cooling for the others")
	}
}

func TestPoolFallsBackToSharedDomainsWhileTheEdgeIsDown(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	cfg := &MTProtoUpstream{CFProxyEnabled: true}
	p := newWSPool(*cfg, 0, wsPoolDefaultSize)
	defer p.close()

	for _, pl := range wsPlansForDC(2, cfg) {
		if !pl.native {
			t.Fatalf("healthy edge, yet the pool plans %s", pl.describe())
		}
	}
	if got := p.targetFor(wsKeyFromDC(2)); got != wsPoolDefaultSize {
		t.Fatalf("healthy edge target %d, want %d", got, wsPoolDefaultSize)
	}

	wsAddressFailed(telegramWSEdgeIP)
	plans := wsPlansForDC(-2, cfg)
	if len(plans) < 3 || !plans[0].native || plans[len(plans)-1].native {
		t.Fatalf("edge down, pool plans %v", plans)
	}
	if got := p.targetFor(wsKeyFromDC(-2)); got != wsPoolCFTarget {
		t.Fatalf("edge down target %d, want %d", got, wsPoolCFTarget)
	}
	for _, pl := range withoutNative(plans) {
		if pl.native {
			t.Fatalf("a spare slot dials the dead edge: %s", pl.describe())
		}
	}
	if got := onlyNative(plans); len(got) != 2 || got[0].sni != "kws2-1.web.telegram.org" {
		t.Fatalf("media probe plans %v, want kws2-1 then kws2", got)
	}
}

func TestSweepDropsAgedSparesAndForgetsIdleKeys(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	port := blackholeListener(t)
	p := newWSPool(MTProtoUpstream{}, 0, 1)
	defer p.close()

	aged := idleWSConn(t, port)
	fresh := idleWSConn(t, port)
	now := time.Now()
	k2, k4 := wsKeyFromDC(2), wsKeyFromDC(4)
	p.mu.Lock()
	p.idle[k2] = []wsPoolEntry{{conn: aged, created: now.Add(-2 * p.maxAge)}, {conn: fresh, created: now}}
	p.lastUsed[k2] = now
	p.lastUsed[k4] = now.Add(-wsPoolKeepWarm - time.Minute)
	p.mu.Unlock()

	p.sweep(now)

	closedBy := time.Now().Add(time.Second)
	for !aged.closed.Load() && time.Now().Before(closedBy) {
		time.Sleep(10 * time.Millisecond)
	}
	if !aged.closed.Load() {
		t.Error("an aged spare was kept open")
	}
	if fresh.closed.Load() || p.idleCount(k2) != 1 {
		t.Error("a fresh spare was dropped")
	}
	p.mu.Lock()
	_, stillWarm := p.lastUsed[k4]
	_, inUse := p.lastUsed[k2]
	p.mu.Unlock()
	if stillWarm {
		t.Error("a key nobody used for longer than the keep-warm window is still kept warm")
	}
	if !inUse {
		t.Error("a key in use was forgotten")
	}
}

func TestOfferKeepsASpareOnlyWhileThereIsRoom(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	port := blackholeListener(t)
	p := newWSPool(MTProtoUpstream{}, 0, 2)
	defer p.close()

	plan := transportPlan{kind: transportWS, dc: 2, sni: "kws2.web.telegram.org", native: true}
	if !p.offer(2, idleWSConn(t, port), plan) || !p.offer(2, idleWSConn(t, port), plan) {
		t.Fatal("the pool refused a spare it had room for")
	}
	if p.offer(2, idleWSConn(t, port), plan) {
		t.Fatal("the pool took a spare past its target")
	}
	worker := transportPlan{kind: transportWS, dc: 4, sni: "w.workers.dev", isWorker: true}
	if p.offer(4, idleWSConn(t, port), worker) {
		t.Fatal("the WS pool took a Worker conn")
	}
}

func TestScheduleRefillWaitsOutTheBackoff(t *testing.T) {
	withWSDialPort(t, blackholeListener(t))
	p := newWSPool(MTProtoUpstream{WSEndpointHost: "127.0.0.1"}, 0, wsPoolDefaultSize)
	defer p.close()
	k := wsKeyFromDC(2)
	p.mu.Lock()
	p.refillAfter[k] = time.Now().Add(time.Hour)
	p.mu.Unlock()

	p.scheduleRefill(2)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.refilling[k] {
		t.Fatal("a refill started inside its backoff")
	}
}

func TestOfferKeepsSharedDomainsOutOfAHealthyEdgeKey(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	port := blackholeListener(t)
	p := newWSPool(MTProtoUpstream{CFProxyEnabled: true}, 0, wsPoolDefaultSize)
	defer p.close()

	cf := transportPlan{kind: transportWS, dc: 2, sni: "kws2.example.co.uk", cfBase: "example.co.uk"}
	if p.offer(2, idleWSConn(t, port), cf) {
		t.Fatal("a shared-domain spare took a slot the working edge should fill")
	}
	wsAddressFailed(telegramWSEdgeIP)
	if !p.offer(2, idleWSConn(t, port), cf) {
		t.Fatal("with the edge down the shared-domain spare was refused")
	}
}

func TestClientSuccessClearsTheRefillBackoff(t *testing.T) {
	p := newWSPool(MTProtoUpstream{}, 0, wsPoolDefaultSize)
	defer p.close()
	k := wsKeyFromDC(-4)
	p.mu.Lock()
	p.refillFails[k] = 7
	p.refillAfter[k] = time.Now().Add(time.Hour)
	p.mu.Unlock()

	p.noteSuccess(-4)

	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.refillAfter[k]; ok || p.refillFails[k] != 0 {
		t.Fatal("a working route left the pool waiting out its backoff")
	}
}

func TestEdgeProbesAreThrottled(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	if !wsProbeAllowed(telegramWSEdgeIP) {
		t.Fatal("the first probe was refused")
	}
	if wsProbeAllowed(telegramWSEdgeIP) {
		t.Fatal("a second probe of the same edge ran inside the throttle")
	}
	if !wsProbeAllowed("149.154.167.99") {
		t.Fatal("the throttle leaked to another address")
	}
}
