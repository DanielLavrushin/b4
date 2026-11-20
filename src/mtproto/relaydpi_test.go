package mtproto

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

type relayEnv struct {
	name  string
	path  string
	setUp func(t *testing.T) *config.Config
}

func relayEnvs() []relayEnv {
	tun := func(cfg *config.Config) *config.Config {
		cfg.Queue.Mode = "tun"
		return cfg
	}
	return []relayEnv{
		{"NFQUEUE without a Telegram set", "none", func(*testing.T) *config.Config {
			cfg := config.NewConfig()
			return &cfg
		}},
		{"NFQUEUE with the chosen set disabled", "none", func(*testing.T) *config.Config {
			return telegramSetTestConfig(false)
		}},
		{"NFQUEUE with a Telegram set without routing", "strategy", func(*testing.T) *config.Config {
			return telegramSetTestConfig(true)
		}},
		{"NFQUEUE with an interface Telegram set", "interface", func(t *testing.T) *config.Config {
			useTelegramRouting(t, &TelegramRoute{Mode: config.RoutingModeInterface, Mark: 0x2a, IPv4: true}, nil)
			return routedTelegramConfig(config.RoutingModeInterface)
		}},
		{"NFQUEUE with a proxy Telegram set", "proxy", func(t *testing.T) *config.Config {
			useTelegramRouting(t, nil, &fakeSetDialer{listens: true})
			return routedTelegramConfig(config.RoutingModeProxy)
		}},
		{"NFQUEUE with an interface Telegram set whose route is not in place", "none", func(t *testing.T) *config.Config {
			useTelegramRouting(t, nil, &fakeSetDialer{})
			return routedTelegramConfig(config.RoutingModeInterface)
		}},
		{"NFQUEUE with a proxy Telegram set whose listener is down", "none", func(t *testing.T) *config.Config {
			useTelegramRouting(t, nil, &fakeSetDialer{})
			return routedTelegramConfig(config.RoutingModeProxy)
		}},
		{"TUN with a Telegram set chosen", "none", func(*testing.T) *config.Config {
			return tun(telegramSetTestConfig(true))
		}},
		{"TUN without a Telegram set", "none", func(*testing.T) *config.Config {
			cfg := config.NewConfig()
			return tun(&cfg)
		}},
	}
}

func wantRelayMark(path string, p transportPlan, workerDPI, cfProxyDPI bool) uint {
	switch path {
	case "strategy":
		if p.isWorker && !workerDPI {
			return uint(config.SelfDialRelayMark)
		}
		return uint(config.TelegramSetDialMark)
	case "interface":
		return uint(config.SelfDialRelayMark | 0x2a)
	case "proxy":
		return uint(config.TelegramSetDialMark)
	}
	if (p.isWorker && !workerDPI) || (p.cfBase != "" && !cfProxyDPI) {
		return uint(config.SelfDialRelayMark)
	}
	return uint(config.SelfDialMark)
}

func useRelaySwitches(t *testing.T, cfg *config.Config) {
	t.Helper()
	setWorkerFollowsSets(cfg)
	setCFProxyFollowsSets(cfg)
	t.Cleanup(func() {
		setWorkerFollowsSets(nil)
		setCFProxyFollowsSets(nil)
	})
}

func TestEveryRelayDialsWithTheMarkOfItsModeAndSwitches(t *testing.T) {
	for _, env := range relayEnvs() {
		for _, workerDPI := range []bool{false, true} {
			for _, cfProxyDPI := range []bool{false, true} {
				name := env.name
				if workerDPI {
					name += ", sets may process the Worker"
				}
				if cfProxyDPI {
					name += ", sets may process the CF proxy domains"
				}
				t.Run(name, func(t *testing.T) {
					cfg := env.setUp(t)
					cfg.System.MTProto.CFWorkerDPI = workerDPI
					cfg.System.MTProto.CFProxyDPI = cfProxyDPI
					useTelegramSet(t, cfg)
					useRelaySwitches(t, cfg)

					base := selfDialMark()
					for route, p := range everyRouteKind() {
						want := wantRelayMark(env.path, p, workerDPI, cfProxyDPI)
						if got := planDialMark(p, base); got != want {
							t.Errorf("%s dials with 0x%x, want 0x%x", route, got, want)
						}
					}
					worker := wantRelayMark(env.path, everyRouteKind()["Worker"], workerDPI, cfProxyDPI)
					if got := workerDialMark(base); got != worker {
						t.Errorf("the Worker pool and fail-open dials use 0x%x, want 0x%x", got, worker)
					}
				})
			}
		}
	}
}

func TestTheBridgeAppliesTheCFProxySwitchOnEveryConfig(t *testing.T) {
	on := config.NewConfig()
	on.System.MTProto.CFProxyDPI = true
	b := NewTransparentBridge(&on)
	t.Cleanup(func() {
		b.Close()
		setTelegramSet(nil)
		setCFProxyFollowsSets(nil)
	})
	reserve := everyRouteKind()["CF reserve"]
	if got := planDialMark(reserve, selfDialMark()); got != uint(config.SelfDialMark) {
		t.Fatalf("with the switch on at start the CF proxy domains dial with 0x%x, want 0x%x", got, config.SelfDialMark)
	}

	off := config.NewConfig()
	b.UpdateConfig(&off)
	if got := planDialMark(reserve, selfDialMark()); got != uint(config.SelfDialRelayMark) {
		t.Fatalf("after the switch was turned off they dial with 0x%x, want 0x%x", got, config.SelfDialRelayMark)
	}
	b.UpdateConfig(&on)
	if got := planDialMark(reserve, selfDialMark()); got != uint(config.SelfDialMark) {
		t.Fatalf("after the switch was turned on again they dial with 0x%x, want 0x%x", got, config.SelfDialMark)
	}
}

func TestPooledCFProxySparesFollowTheSwitch(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	port := blackholeListener(t)
	cfg := config.NewConfig()
	useTelegramSet(t, &cfg)
	useRelaySwitches(t, &cfg)

	ws := newWSPool(MTProtoUpstream{}, selfDialMark(), wsPoolDefaultSize)
	t.Cleanup(ws.close)
	for _, route := range []string{"CF reserve", "custom WS domain"} {
		plan := everyRouteKind()[route]
		plan.dc = 203

		skipped := idleWSConn(t, port)
		skipped.mark = planDialMark(plan, ws.mark)
		if skipped.mark != uint(config.SelfDialRelayMark) {
			t.Fatalf("%s: a spare dialled with the switch off carries 0x%x, want 0x%x", route, skipped.mark, config.SelfDialRelayMark)
		}
		if !ws.offer(203, skipped, plan) {
			t.Fatalf("%s: the pool refused a spare", route)
		}

		cfg.System.MTProto.CFProxyDPI = true
		setCFProxyFollowsSets(&cfg)
		if got := ws.get(203); got != nil {
			t.Fatalf("%s: the pool handed out a spare dialled without DPI after sets were allowed to process it", route)
		}

		processed := idleWSConn(t, port)
		processed.mark = planDialMark(plan, ws.mark)
		if !ws.offer(203, processed, plan) {
			t.Fatalf("%s: the pool refused a spare dialled for sets", route)
		}
		cfg.System.MTProto.CFProxyDPI = false
		setCFProxyFollowsSets(&cfg)
		if got := ws.get(203); got != nil {
			t.Fatalf("%s: the pool handed out a spare dialled for sets after the switch was turned off", route)
		}
	}
}
