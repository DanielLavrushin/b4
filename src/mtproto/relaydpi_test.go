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
		{"TUN with a Telegram set without routing", "strategy", func(*testing.T) *config.Config {
			return tun(telegramSetTestConfig(true))
		}},
		{"TUN with an interface Telegram set", "interface", func(t *testing.T) *config.Config {
			useTelegramRouting(t, &TelegramRoute{Mode: config.RoutingModeInterface, Mark: 0x2a, IPv4: true}, nil)
			return tun(routedTelegramConfig(config.RoutingModeInterface))
		}},
		{"TUN without a Telegram set", "none", func(*testing.T) *config.Config {
			cfg := config.NewConfig()
			return tun(&cfg)
		}},
	}
}

func wantRelayMark(path string, p transportPlan) uint {
	switch path {
	case "strategy", "proxy":
		return uint(config.TelegramSetDialMark)
	case "interface":
		return uint(config.SelfDialRelayMark | 0x2a)
	}
	if p.isWorker || p.cfBase != "" {
		return uint(config.SelfDialRelayMark)
	}
	return uint(config.SelfDialMark)
}

func TestEveryRouteDialsWithTheMarkOfItsTelegramSet(t *testing.T) {
	for _, env := range relayEnvs() {
		t.Run(env.name, func(t *testing.T) {
			cfg := env.setUp(t)
			useTelegramSet(t, cfg)

			base := selfDialMark()
			for route, p := range everyRouteKind() {
				want := wantRelayMark(env.path, p)
				if got := planDialMark(p, base); got != want {
					t.Errorf("%s dials with 0x%x, want 0x%x", route, got, want)
				}
			}
			worker := wantRelayMark(env.path, everyRouteKind()["Worker"])
			if got := relayDialMark(base); got != worker {
				t.Errorf("the Worker pool and fail-open dials use 0x%x, want 0x%x", got, worker)
			}
		})
	}
}

func TestTheBridgeAppliesTheTelegramSetToTheRelaysOnEveryConfig(t *testing.T) {
	with := telegramSetTestConfig(true)
	b := NewTransparentBridge(with)
	t.Cleanup(func() {
		b.Close()
		setTelegramSet(nil)
	})
	for _, route := range []string{"CF reserve", "custom WS domain", "Worker"} {
		if got := planDialMark(everyRouteKind()[route], selfDialMark()); got != uint(config.TelegramSetDialMark) {
			t.Fatalf("%s with a Telegram set at start dials with 0x%x, want 0x%x", route, got, config.TelegramSetDialMark)
		}
	}

	without := config.NewConfig()
	b.UpdateConfig(&without)
	for _, route := range []string{"CF reserve", "custom WS domain", "Worker"} {
		if got := planDialMark(everyRouteKind()[route], selfDialMark()); got != uint(config.SelfDialRelayMark) {
			t.Fatalf("%s after the Telegram set was cleared dials with 0x%x, want 0x%x: without a set no other set may process b4's relays", route, got, config.SelfDialRelayMark)
		}
	}

	tun := telegramSetTestConfig(true)
	tun.Queue.Mode = "tun"
	b.UpdateConfig(tun)
	for _, route := range []string{"CF reserve", "custom WS domain", "Worker"} {
		if got := planDialMark(everyRouteKind()[route], selfDialMark()); got != uint(config.TelegramSetDialMark) {
			t.Fatalf("%s with a Telegram set in TUN mode dials with 0x%x, want 0x%x", route, got, config.TelegramSetDialMark)
		}
	}
}
