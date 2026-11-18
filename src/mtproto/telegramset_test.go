package mtproto

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func telegramSetTestConfig(enabled bool) *config.Config {
	cfg := config.NewConfig()
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "tg", "Telegram", enabled
	cfg.Sets = []*config.SetConfig{&set}
	cfg.System.MTProto.UpstreamSet = "tg"
	return &cfg
}

func useTelegramSet(t *testing.T, cfg *config.Config) {
	t.Helper()
	setTelegramSet(cfg)
	t.Cleanup(func() { setTelegramSet(nil) })
}

func everyRouteKind() map[string]transportPlan {
	return map[string]transportPlan{
		"direct TCP":         {kind: transportTCP, addr: "149.154.167.51:443"},
		"DC relay":           {kind: transportTCP, addr: "198.51.100.7:7008", relay: true},
		"Telegram's WS edge": {kind: transportWS, sni: "kws2.web.telegram.org", dialHost: "149.154.167.220", native: true},
		"CF reserve":         {kind: transportWS, sni: "kws2.example.co.uk", cfBase: "example.co.uk"},
		"custom WS domain":   {kind: transportWS, sni: "kws2.relay.example", cfBase: "relay.example"},
		"Worker":             {kind: transportWS, sni: "w.example.workers.dev", dialHost: "w.example.workers.dev", isWorker: true},
	}
}

func TestEveryTelegramRouteCarriesTheTelegramSetMark(t *testing.T) {
	useTelegramSet(t, telegramSetTestConfig(true))

	base := selfDialMark()
	if base != uint(config.TelegramSetDialMark) {
		t.Fatalf("self-dial mark 0x%x, want 0x%x while a Telegram set is chosen", base, config.TelegramSetDialMark)
	}
	for name, p := range everyRouteKind() {
		want := uint(config.TelegramSetDialMark)
		if p.isWorker {
			want = uint(config.SelfDialRelayMark)
		}
		if got := planDialMark(p, base); got != want {
			t.Errorf("%s dials with 0x%x, want 0x%x: every route but the Worker takes the Telegram set's strategy, the reserve included", name, got, want)
		}
	}
}

func TestTheWorkerTakesTheTelegramSetsStrategyOnlyWhenSetsMayProcessIt(t *testing.T) {
	cfg := telegramSetTestConfig(true)
	useTelegramSet(t, cfg)
	worker := everyRouteKind()["Worker"]

	setWorkerFollowsSets(cfg)
	t.Cleanup(func() { setWorkerFollowsSets(nil) })
	if got := planDialMark(worker, selfDialMark()); got != uint(config.SelfDialRelayMark) {
		t.Errorf("a Worker dial plans 0x%x, want 0x%x: a strategy aimed at Telegram's edge can break the Worker, so it skips DPI unless sets may process it", got, config.SelfDialRelayMark)
	}
	if got := workerDialMark(selfDialMark()); got != uint(config.SelfDialRelayMark) {
		t.Errorf("pool and fail-open Worker dials use 0x%x, want 0x%x", got, config.SelfDialRelayMark)
	}

	cfg.System.MTProto.CFWorkerDPI = true
	setWorkerFollowsSets(cfg)
	if got := planDialMark(worker, selfDialMark()); got != uint(config.TelegramSetDialMark) {
		t.Errorf("with sets allowed to process the Worker, its dial plans 0x%x, want 0x%x: the Telegram set's strategy", got, config.TelegramSetDialMark)
	}
	if got := workerDialMark(selfDialMark()); got != uint(config.TelegramSetDialMark) {
		t.Errorf("with sets allowed to process the Worker, pool dials use 0x%x, want 0x%x", got, config.TelegramSetDialMark)
	}
}

func TestARoutedTelegramSetCarriesTheWorkerOnItsRoute(t *testing.T) {
	useTelegramSet(t, routedTelegramConfig(config.RoutingModeInterface))
	useTelegramRouting(t, &TelegramRoute{Mode: config.RoutingModeInterface, Mark: 0x2a, IPv4: true}, nil)
	setWorkerFollowsSets(nil)

	want := uint(config.SelfDialRelayMark | 0x2a)
	if got := planDialMark(everyRouteKind()["Worker"], selfDialMark()); got != want {
		t.Fatalf("a Worker dial plans 0x%x, want 0x%x: the exemption covers DPI only, never the set's route", got, want)
	}
}

func TestWithoutAUsableTelegramSetTheReserveAndWorkersStayUnprocessed(t *testing.T) {
	useTelegramSet(t, telegramSetTestConfig(false))

	base := selfDialMark()
	if base != uint(config.SelfDialMark) {
		t.Fatalf("self-dial mark 0x%x, want 0x%x with the chosen set disabled", base, config.SelfDialMark)
	}
	want := map[string]uint{
		"direct TCP":         uint(config.SelfDialMark),
		"DC relay":           uint(config.SelfDialMark),
		"Telegram's WS edge": uint(config.SelfDialMark),
		"CF reserve":         uint(config.SelfDialRelayMark),
		"custom WS domain":   uint(config.SelfDialRelayMark),
		"Worker":             uint(config.SelfDialRelayMark),
	}
	for name, p := range everyRouteKind() {
		if got := planDialMark(p, base); got != want[name] {
			t.Errorf("%s dials with 0x%x, want 0x%x as before the setting existed", name, got, want[name])
		}
	}
}

func TestTheCoverPathNeverCarriesTheTelegramSetMark(t *testing.T) {
	useTelegramSet(t, telegramSetTestConfig(true))
	if got := coverDialMark(); got != uint(config.SelfDialMark) {
		t.Fatalf("cover path mark 0x%x, want 0x%x: the fake SNI site has to be reached the way a local prober sees it", got, config.SelfDialMark)
	}
}

func TestTheProxyRestartsWhenTheTelegramSetTurnsOnOrOff(t *testing.T) {
	off, on := telegramSetTestConfig(false), telegramSetTestConfig(true)
	if !mtprotoNeedsRestart(off, on) || !mtprotoNeedsRestart(on, off) {
		t.Fatal("the pools keep the mark they were built with, so the proxy has to restart when the Telegram set turns on or off")
	}
	tuned := telegramSetTestConfig(true)
	tuned.Sets[0].Fragmentation.SNIPosition = 3
	if mtprotoNeedsRestart(on, tuned) {
		t.Fatal("a change to the set's strategy is read per packet and must not restart the proxy")
	}
}

func TestTheBridgeRebuildsItsWorkerPoolForTheTelegramSet(t *testing.T) {
	off := telegramSetTestConfig(false)
	off.System.MTProto.CFWorkerDomain = "w.example.workers.dev"
	b := NewTransparentBridge(off)
	t.Cleanup(func() {
		b.Close()
		setTelegramSet(nil)
	})

	before := b.getPool()
	if before == nil || before.mark != uint(config.SelfDialMark) {
		t.Fatalf("pool %+v, want one built with 0x%x", before, config.SelfDialMark)
	}

	on := telegramSetTestConfig(true)
	on.System.MTProto.CFWorkerDomain = off.System.MTProto.CFWorkerDomain
	b.UpdateConfig(on)
	after := b.getPool()
	if after == nil || after == before || after.mark != uint(config.TelegramSetDialMark) {
		t.Fatalf("pool %+v after the Telegram set was chosen, want a new one built with 0x%x", after, config.TelegramSetDialMark)
	}
}

func TestTelegramSetProblemNamesWhatIsMissing(t *testing.T) {
	for _, c := range []struct {
		name    string
		change  func(*config.Config)
		problem bool
	}{
		{"a usable set", func(*config.Config) {}, false},
		{"no set chosen", func(c *config.Config) { c.System.MTProto.UpstreamSet = "" }, false},
		{"a missing set", func(c *config.Config) { c.System.MTProto.UpstreamSet = "gone" }, true},
		{"a disabled set", func(c *config.Config) { c.Sets[0].Enabled = false }, true},
		{"a block set", func(c *config.Config) {
			c.Sets[0].Routing.Enabled, c.Sets[0].Routing.Mode = true, config.RoutingModeBlock
		}, true},
		{"TUN mode", func(c *config.Config) { c.Queue.Mode = "tun" }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := telegramSetTestConfig(true)
			c.change(cfg)
			if got := telegramSetProblem(cfg) != ""; got != c.problem {
				t.Fatalf("problem %q, want one: %v", telegramSetProblem(cfg), c.problem)
			}
			if c.problem && cfg.TelegramUpstreamSet() != nil {
				t.Fatal("a set with a problem must not be used for dials")
			}
		})
	}
}

func TestTheConnectionTestAppliesOnlyTheSavedTelegramSet(t *testing.T) {
	setTelegramSet(nil)
	draft, err := PlanTransportProbe(telegramSetTestConfig(true), 2)
	if err != nil {
		t.Fatal(err)
	}
	if draft.mark != uint(config.SelfDialMark) || draft.TelegramSet() != "" {
		t.Fatalf("probe mark 0x%x set %q for a choice that is not saved, want no set: the packet engine and the routes follow the saved config only", draft.mark, draft.TelegramSet())
	}

	useTelegramSet(t, telegramSetTestConfig(true))
	saved, err := PlanTransportProbe(telegramSetTestConfig(true), 2)
	if err != nil {
		t.Fatal(err)
	}
	if saved.mark != uint(config.TelegramSetDialMark) || saved.TelegramSet() != "Telegram" {
		t.Fatalf("probe mark 0x%x set %q for the saved choice, want 0x%x and Telegram", saved.mark, saved.TelegramSet(), config.TelegramSetDialMark)
	}

	cleared := telegramSetTestConfig(true)
	cleared.System.MTProto.UpstreamSet = ""
	none, err := PlanTransportProbe(cleared, 2)
	if err != nil {
		t.Fatal(err)
	}
	if none.mark != uint(config.SelfDialMark) || none.TelegramSet() != "" {
		t.Fatalf("probe mark 0x%x set %q with the choice cleared on the page, want the path without a set", none.mark, none.TelegramSet())
	}
}

type fakeSetDialer struct {
	mu      sync.Mutex
	listens bool
	block   chan struct{}
	asked   []string
	by      []time.Time
	served  []net.Conn
}

func (f *fakeSetDialer) CanDialViaSet(string) bool { return f.listens }

func (f *fakeSetDialer) DialViaSetBy(setID, host string, port int, by time.Time) (net.Conn, bool, error) {
	f.mu.Lock()
	f.asked = append(f.asked, fmt.Sprintf("%s %s:%d", setID, host, port))
	f.by = append(f.by, by)
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	client, server := net.Pipe()
	f.mu.Lock()
	f.served = append(f.served, server)
	f.mu.Unlock()
	return client, true, nil
}

func (f *fakeSetDialer) dials() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

func routedTelegramConfig(mode string) *config.Config {
	cfg := telegramSetTestConfig(true)
	set := cfg.Sets[0]
	set.Routing.Enabled, set.Routing.Mode = true, mode
	set.Routing.EgressInterface = "wg0"
	set.Routing.Upstream.Host, set.Routing.Upstream.Port = "127.0.0.1", 1080
	return cfg
}

func useTelegramRouting(t *testing.T, route *TelegramRoute, via TelegramSetDialer) {
	t.Helper()
	routeOf := func(string) (TelegramRoute, bool) {
		if route == nil {
			return TelegramRoute{}, false
		}
		return *route, true
	}
	SetTelegramRouting(routeOf, via)
	t.Cleanup(func() { telegramHooks.Store(nil) })
}

func TestAnInterfaceTelegramSetCarriesEveryRouteOnItsMarkWithoutDPI(t *testing.T) {
	useTelegramSet(t, routedTelegramConfig(config.RoutingModeInterface))
	useTelegramRouting(t, &TelegramRoute{Mode: config.RoutingModeInterface, Mark: 0x2a, IPv4: true}, nil)

	want := uint(config.SelfDialRelayMark | 0x2a)
	for name, p := range everyRouteKind() {
		if got := planDialMark(p, selfDialMark()); got != want {
			t.Errorf("%s dials with 0x%x, want 0x%x: the set's route mark picks its table, and the no-DPI bit keeps reinjected packets off the main route", name, got, want)
		}
	}
	if got, _ := resolveTelegramMark(selfDialMark()); got != want {
		t.Errorf("downloads and direct dials resolve to 0x%x, want 0x%x", got, want)
	}
	if got := coverDialMark(); got != uint(config.SelfDialMark) {
		t.Errorf("the cover path dials with 0x%x, want 0x%x outside the set's route", got, config.SelfDialMark)
	}
}

func TestAnInterfaceTelegramSetKeepsIPv6OffWhenItsRouteHasNone(t *testing.T) {
	prev := dialIPv6Probe
	dialIPv6Probe = func() bool { return true }
	t.Cleanup(func() { dialIPv6Probe = prev })
	useTelegramSet(t, routedTelegramConfig(config.RoutingModeInterface))

	useTelegramRouting(t, &TelegramRoute{Mode: config.RoutingModeInterface, Mark: 0x2a, IPv4: true}, nil)
	if dialNetwork() != "tcp4" || dialFamilyAllows(net.ParseIP("2001:67c:4e8:f004::a")) {
		t.Fatal("a set routing IPv4 only must keep IPv6 dials off, or they leave by the main table outside the set's route")
	}
	useTelegramRouting(t, &TelegramRoute{Mode: config.RoutingModeInterface, Mark: 0x2a, IPv4: true, IPv6: true}, nil)
	if dialNetwork() != "tcp" || !dialFamilyAllows(net.ParseIP("2001:67c:4e8:f004::a")) {
		t.Fatal("a set routing IPv6 too must allow IPv6 dials")
	}
}

func TestTheDownloadsBehindAnIPv4OnlyInterfaceSetNeverDialIPv6(t *testing.T) {
	prev := dialIPv6Probe
	dialIPv6Probe = func() bool { return true }
	t.Cleanup(func() { dialIPv6Probe = prev })
	useTelegramSet(t, routedTelegramConfig(config.RoutingModeInterface))

	refused := func() bool {
		tr, ok := telegramHTTPClient(200 * time.Millisecond).Transport.(*http.Transport)
		if !ok || tr.DialContext == nil {
			t.Fatal("the download client has no dial hook")
		}
		_, err := tr.DialContext(context.Background(), "tcp", "[2001:db8::1]:443")
		var addrErr *net.AddrError
		return errors.As(err, &addrErr)
	}

	useTelegramRouting(t, &TelegramRoute{Mode: config.RoutingModeInterface, Mark: 0x2a, IPv4: true}, nil)
	if !refused() {
		t.Fatal("a download dialled an IPv6 address behind a set that routes IPv4 only, so it would leave by the main table outside the set's route and kill switch")
	}
	useTelegramRouting(t, &TelegramRoute{Mode: config.RoutingModeInterface, Mark: 0x2a, IPv4: true, IPv6: true}, nil)
	if refused() {
		t.Fatal("a set routing IPv6 too must let the downloads dial IPv6")
	}
}

func TestAnIPv6OnlyInterfaceTelegramSetNeverDialsIPv4(t *testing.T) {
	prev := dialIPv6Probe
	dialIPv6Probe = func() bool { return false }
	t.Cleanup(func() { dialIPv6Probe = prev })
	useTelegramSet(t, routedTelegramConfig(config.RoutingModeInterface))
	v4, v6 := net.ParseIP("149.154.167.51"), net.ParseIP("2001:67c:4e8:f004::a")

	useTelegramRouting(t, &TelegramRoute{Mode: config.RoutingModeInterface, Mark: 0x2a, IPv6: true}, nil)
	if got := dialNetwork(); got != "tcp6" || dialFamilyAllows(v4) || !dialFamilyAllows(v6) {
		t.Fatalf("a set routing IPv6 only dials over %q, so IPv4 dials leave by the main table outside the set's route and kill switch", got)
	}
	tr, ok := telegramHTTPClient(200 * time.Millisecond).Transport.(*http.Transport)
	if !ok || tr.DialContext == nil {
		t.Fatal("the download client has no dial hook")
	}
	_, err := tr.DialContext(context.Background(), "tcp", "192.0.2.1:443")
	var addrErr *net.AddrError
	if !errors.As(err, &addrErr) {
		t.Fatalf("a download behind a set routing IPv6 only dialled an IPv4 address: %v", err)
	}

	useTelegramRouting(t, &TelegramRoute{Mode: config.RoutingModeInterface, Mark: 0x2a, IPv4: true, IPv6: true}, nil)
	if got := dialNetwork(); got != "tcp4" || !dialFamilyAllows(v4) {
		t.Fatalf("a set routing both families dials over %q on a router without IPv6, want tcp4", got)
	}
}

func TestAQueueFamilyChangeRebuildsThePoolsOfAnInterfaceTelegramSet(t *testing.T) {
	families := func(mode string, v4, v6 bool) *config.Config {
		cfg := routedTelegramConfig(mode)
		if mode == "" {
			cfg = telegramSetTestConfig(true)
		}
		cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled = v4, v6
		return cfg
	}
	routed, next := families(config.RoutingModeInterface, true, true), families(config.RoutingModeInterface, false, true)
	if !mtprotoNeedsRestart(routed, next) {
		t.Fatal("the proxy kept its pools after IPv4 left the set's route, so pre-opened IPv4 connections leave by the main table")
	}
	if mtprotoNeedsRestart(families("", true, true), families("", false, true)) {
		t.Fatal("a Telegram set without routing restarted the proxy on a queue family change it does not depend on")
	}

	b := NewTransparentBridge(routed)
	t.Cleanup(func() {
		b.Close()
		setTelegramSet(nil)
	})
	before := b.getWSPool()
	b.UpdateConfig(next)
	if after := b.getWSPool(); after == before {
		t.Fatal("the bridge kept its WebSocket pool after IPv4 left the set's route")
	}
}

func TestAMissingRouteLeavesTheTelegramConnectionsAsTheyWereWithoutASet(t *testing.T) {
	for _, mode := range []string{config.RoutingModeInterface, config.RoutingModeProxy} {
		t.Run(mode, func(t *testing.T) {
			useTelegramSet(t, routedTelegramConfig(mode))
			useTelegramRouting(t, nil, &fakeSetDialer{})
			want := map[string]uint{
				"direct TCP":         uint(config.SelfDialMark),
				"DC relay":           uint(config.SelfDialMark),
				"Telegram's WS edge": uint(config.SelfDialMark),
				"CF reserve":         uint(config.SelfDialRelayMark),
				"custom WS domain":   uint(config.SelfDialRelayMark),
				"Worker":             uint(config.SelfDialRelayMark),
			}
			for name, p := range everyRouteKind() {
				if got := planDialMark(p, selfDialMark()); got != want[name] {
					t.Errorf("%s dials with 0x%x, want 0x%x while the set's route is not in place", name, got, want[name])
				}
			}
			if conn, handled, _ := DialTelegramDirect(context.Background(), "149.154.167.51:443"); handled || conn != nil {
				t.Error("the bridge's direct dial must fall back to its own mark while the route is missing")
			}
		})
	}
}

func TestAProxyTelegramSetDialsThroughItsUpstream(t *testing.T) {
	useTelegramSet(t, routedTelegramConfig(config.RoutingModeProxy))
	via := &fakeSetDialer{listens: true}
	useTelegramRouting(t, nil, via)

	base := selfDialMark()
	for name, p := range everyRouteKind() {
		if got := planDialMark(p, base); got != uint(config.TelegramSetDialMark) {
			t.Errorf("%s plans mark 0x%x, want 0x%x so the raw dial takes the set's upstream", name, got, config.TelegramSetDialMark)
		}
	}
	if !telegramViaProxy(base) {
		t.Fatal("a WebSocket dial must leave name resolution to the proxy set")
	}

	conn, err := dialOneTCP(transportPlan{kind: transportTCP, addr: "149.154.167.51:443"}, planDialMark(transportPlan{kind: transportTCP}, base), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	direct, handled, err := DialTelegramDirect(context.Background(), "149.154.175.50:443")
	if err != nil || !handled {
		t.Fatalf("the bridge's direct dial: handled %v, %v, want it through the set", handled, err)
	}
	_ = direct.Close()
	cli := telegramHTTPClient(time.Second)
	_, _ = cli.Get("http://core.telegram.org/getProxyConfig")

	got := via.dials()
	want := []string{"tg 149.154.167.51:443", "tg 149.154.175.50:443", "tg core.telegram.org:80"}
	if len(got) != len(want) {
		t.Fatalf("dials through the set %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dial %d through the set: %s, want %s", i, got[i], want[i])
		}
	}
}

func putTelegramRoute(t *testing.T, mode string, inPlace bool) {
	t.Helper()
	switch {
	case !inPlace:
		useTelegramRouting(t, nil, &fakeSetDialer{})
	case mode == config.RoutingModeInterface:
		useTelegramRouting(t, &TelegramRoute{Mode: mode, Mark: 0x2a, IPv4: true}, nil)
	default:
		useTelegramRouting(t, nil, &fakeSetDialer{listens: true})
	}
}

func TestPoolSparesDialledBeforeTheTelegramRouteWasInPlaceAreNotHandedOut(t *testing.T) {
	for _, mode := range []string{config.RoutingModeInterface, config.RoutingModeProxy} {
		t.Run(mode, func(t *testing.T) {
			wsResetState()
			t.Cleanup(wsResetState)
			port := blackholeListener(t)
			useTelegramSet(t, routedTelegramConfig(mode))
			putTelegramRoute(t, mode, false)

			ws := newWSPool(MTProtoUpstream{}, selfDialMark(), wsPoolDefaultSize)
			t.Cleanup(ws.close)
			workers := newCFWorkerPool(selfDialMark())
			t.Cleanup(workers.close)
			cf := transportPlan{kind: transportWS, dc: 203, sni: "kws203.example.invalid", cfBase: "example.invalid"}
			worker := transportPlan{kind: transportWS, dc: 2, sni: "w.example.workers.dev", dialHost: "w.example.workers.dev", isWorker: true}
			park := func(c *wsConn) {
				workers.mu.Lock()
				workers.idle[planWorkerKey(worker)] = []cfWorkerEntry{{conn: c, created: time.Now()}}
				workers.mu.Unlock()
			}

			offRoute := idleWSConn(t, port)
			offRoute.mark = planDialMark(cf, ws.mark)
			if !ws.offer(203, offRoute, cf) {
				t.Fatal("the pool refused a spare")
			}
			offRouteWorker := idleWSConn(t, port)
			offRouteWorker.mark = workerDialMark(workers.mark)
			park(offRouteWorker)

			putTelegramRoute(t, mode, true)

			if got := ws.get(203); got != nil {
				t.Fatal("the WebSocket pool handed out a spare dialled before the set's route was in place")
			}
			if got := workers.get(worker); got != nil {
				t.Fatal("the Worker pool handed out a spare dialled before the set's route was in place")
			}

			onRoute := idleWSConn(t, port)
			onRoute.mark = planDialMark(cf, ws.mark)
			if !ws.offer(203, onRoute, cf) {
				t.Fatal("the pool refused a spare dialled on the set's route")
			}
			if got := ws.get(203); got == nil || got.conn != onRoute {
				t.Fatal("the WebSocket pool dropped a spare dialled on the set's route")
			}
			onRouteWorker := idleWSConn(t, port)
			onRouteWorker.mark = workerDialMark(workers.mark)
			park(onRouteWorker)
			if got := workers.get(worker); got != onRouteWorker {
				t.Fatal("the Worker pool dropped a spare dialled on the set's route")
			}
		})
	}
}

func TestADialThroughTheProxySetIsBoundByTheRaceDeadline(t *testing.T) {
	via := &fakeSetDialer{listens: true, block: make(chan struct{})}
	start := time.Now()
	conn, handled, err := dialViaWithin(via, "tg", "149.154.167.51", 443, 50*time.Millisecond)
	if !handled || conn != nil || !errors.Is(err, errTelegramViaTimeout) {
		t.Fatalf("got %v, %v, %v, want a timeout handled by the set", conn, handled, err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the dial took %v past a 50 ms deadline", took)
	}
	close(via.block)
	deadline := time.Now().Add(2 * time.Second)
	for {
		via.mu.Lock()
		served := len(via.served)
		via.mu.Unlock()
		if served == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the late dial never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
	via.mu.Lock()
	server := via.served[0]
	via.mu.Unlock()
	_ = server.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := server.Read(make([]byte, 1)); err == nil {
		t.Fatal("the connection that arrived after the deadline must be closed, not leaked")
	}
}

func TestADialThroughTheProxySetHandsTheSetItsDeadline(t *testing.T) {
	via := &fakeSetDialer{listens: true}
	start := time.Now()
	conn, handled, err := dialViaWithin(via, "tg", "149.154.167.51", 443, 3*time.Second)
	if err != nil || !handled {
		t.Fatalf("got %v, %v", handled, err)
	}
	_ = conn.Close()
	via.mu.Lock()
	defer via.mu.Unlock()
	if len(via.by) != 1 || via.by[0].Before(start.Add(3*time.Second)) || via.by[0].After(time.Now().Add(3*time.Second)) {
		t.Fatalf("the set was given the deadline %v, want the dial's own 3 s so fail_open can fall back inside it", via.by)
	}
}

func TestADialThatTimesOutThroughTheProxySetCoolsItsRouteLikeADirectOne(t *testing.T) {
	tcpResetState()
	wsResetState()
	t.Cleanup(tcpResetState)
	t.Cleanup(wsResetState)
	via := &fakeSetDialer{listens: true, block: make(chan struct{})}
	t.Cleanup(func() { close(via.block) })
	_, _, err := dialViaWithin(via, "tg", "149.154.167.51", 443, 20*time.Millisecond)
	ws := fmt.Errorf("tcp dial %s: %w", "149.154.167.220", err)
	if !isDialTimeout(err) || !isDialTimeout(ws) {
		t.Fatalf("a dial through the set that ran out of time is not taken for a timeout: %v", err)
	}

	addr := "149.154.167.51:443"
	recordDialFailure(2, raceAttempt{plan: transportPlan{kind: transportTCP, addr: addr}, err: err, timeout: tcpDialTimeout})
	if !tcpAddrInCooldown(addr) {
		t.Error("a TCP route that timed out through the set was not cooled, so the next session spends its budget on it again")
	}
	recordDialFailure(4, raceAttempt{plan: transportPlan{kind: transportWS, native: true, dialHost: "149.154.167.220"}, err: ws, timeout: wsDialTimeout})
	if !wsCooldownActive(4) {
		t.Error("Telegram's edge timed out through the set but was not stepped over on the next session")
	}
}

func TestChangingTheTelegramSetRouteRestartsTheProxy(t *testing.T) {
	plain := telegramSetTestConfig(true)
	routed := routedTelegramConfig(config.RoutingModeInterface)
	if !mtprotoNeedsRestart(plain, routed) {
		t.Fatal("pre-opened connections went out on the old path, so turning the set's routing on must rebuild the pools")
	}
	other := routedTelegramConfig(config.RoutingModeInterface)
	other.Sets[0].Routing.EgressInterface = "wg1"
	if !mtprotoNeedsRestart(routed, other) {
		t.Fatal("a new output interface is a new path")
	}
}

func TestAMissingRouteIsReportedOnlyAfterTheGrace(t *testing.T) {
	sel := &telegramSelection{id: "tg", name: "Telegram", mode: config.RoutingModeInterface, key: "k"}
	telegramMissing.Lock()
	telegramMissing.key, telegramMissing.warned = "", false
	telegramMissing.Unlock()
	t.Cleanup(func() {
		telegramMissing.Lock()
		telegramMissing.key, telegramMissing.warned = "", false
		telegramMissing.Unlock()
	})

	noteTelegramRoute(sel, false)
	noteTelegramRoute(sel, false)
	telegramMissing.Lock()
	warned := telegramMissing.warned
	telegramMissing.since = time.Now().Add(-telegramRouteGrace - time.Second)
	telegramMissing.Unlock()
	if warned {
		t.Fatal("a route missing right after a save is still being installed and must not be reported")
	}
	noteTelegramRoute(sel, false)
	telegramMissing.Lock()
	warned = telegramMissing.warned
	telegramMissing.Unlock()
	if !warned {
		t.Fatal("a route still missing after the grace must be reported")
	}
	noteTelegramRoute(sel, true)
	telegramMissing.Lock()
	warned = telegramMissing.warned
	telegramMissing.Unlock()
	if warned {
		t.Fatal("a route in place again must clear the report")
	}
}

func TestTheBridgeRebuildsItsWebSocketPoolWhenTheTelegramSetsRouteChanges(t *testing.T) {
	strategy := telegramSetTestConfig(true)
	b := NewTransparentBridge(strategy)
	t.Cleanup(func() {
		b.Close()
		setTelegramSet(nil)
	})

	before := b.getWSPool()
	routed := routedTelegramConfig(config.RoutingModeInterface)
	b.UpdateConfig(routed)
	if after := b.getWSPool(); after == before {
		t.Fatal("the pool kept connections opened over the uplink after the set started routing through an interface; its base mark did not change, so the route has to be part of the pool's key")
	}
}

func TestAnExplicitRouteMarkOrTableChangeRebuildsTheTelegramPools(t *testing.T) {
	routed := routedTelegramConfig(config.RoutingModeInterface)
	routed.Sets[0].Routing.FWMark, routed.Sets[0].Routing.Table = 0x2a, 120
	for name, change := range map[string]func(*config.RoutingConfig){
		"mark":  func(r *config.RoutingConfig) { r.FWMark = 0x2b },
		"table": func(r *config.RoutingConfig) { r.Table = 121 },
	} {
		t.Run(name, func(t *testing.T) {
			next := routedTelegramConfig(config.RoutingModeInterface)
			next.Sets[0].Routing.FWMark, next.Sets[0].Routing.Table = 0x2a, 120
			change(&next.Sets[0].Routing)
			if !mtprotoNeedsRestart(routed, next) {
				t.Fatalf("the proxy kept its pools after the set's %s changed, so pre-opened connections stay on the old route", name)
			}

			b := NewTransparentBridge(routed)
			t.Cleanup(func() {
				b.Close()
				setTelegramSet(nil)
			})
			before := b.getWSPool()
			b.UpdateConfig(next)
			if after := b.getWSPool(); after == before {
				t.Fatalf("the bridge kept its WebSocket pool after the set's %s changed", name)
			}
		})
	}
}
