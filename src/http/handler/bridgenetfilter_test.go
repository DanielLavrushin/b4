package handler

import (
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tables"
)

func TestBridgeNetfilterStatus(t *testing.T) {
	newCfg := func(mode string) *config.Config {
		cfg := config.DefaultConfig
		cfg.Queue.IPv4Enabled = true
		cfg.Queue.IPv6Enabled = false
		cfg.Sets = nil
		if mode != "" {
			set := config.NewSetConfig()
			set.Id = "s1"
			set.Enabled = true
			set.Routing.Enabled = true
			set.Routing.Mode = mode
			cfg.Sets = []*config.SetConfig{&set}
		}
		return &cfg
	}
	on := tables.BridgeNetfilter{GlobalV4: true, BridgesV4: []string{"br-lan", "docker0"}}

	t.Run("off is safe", func(t *testing.T) {
		bridges, safe := bridgeNetfilterStatus(tables.BridgeNetfilter{}, newCfg(config.RoutingModeProxy))
		if len(bridges) != 0 || !safe {
			t.Fatalf("got %v safe=%v, want none and safe", bridges, safe)
		}
	})

	t.Run("on without transparent-proxy routing is safe", func(t *testing.T) {
		bridges, safe := bridgeNetfilterStatus(on, newCfg(config.RoutingModeInterface))
		if !slices.Equal(bridges, []string{"br-lan", "docker0"}) || !safe {
			t.Fatalf("got %v safe=%v, want both bridges and safe", bridges, safe)
		}
	})

	t.Run("on with a proxy set is unsafe", func(t *testing.T) {
		if _, safe := bridgeNetfilterStatus(on, newCfg(config.RoutingModeProxy)); safe {
			t.Fatal("expected a proxy set behind bridge netfilter to be unsafe")
		}
	})

	t.Run("on with an mtproto-ws set is unsafe", func(t *testing.T) {
		if _, safe := bridgeNetfilterStatus(on, newCfg(config.RoutingModeMTProtoWS)); safe {
			t.Fatal("expected an mtproto-ws set behind bridge netfilter to be unsafe")
		}
	})

	t.Run("on with the Telegram bridge is unsafe", func(t *testing.T) {
		cfg := newCfg("")
		cfg.System.MTProto.Bridge.Enabled = true
		if _, safe := bridgeNetfilterStatus(on, cfg); safe {
			t.Fatal("expected Telegram over WebSocket behind bridge netfilter to be unsafe")
		}
	})

	t.Run("a disabled proxy set does not count", func(t *testing.T) {
		cfg := newCfg(config.RoutingModeProxy)
		cfg.Sets[0].Enabled = false
		if _, safe := bridgeNetfilterStatus(on, cfg); !safe {
			t.Fatal("expected a disabled set to leave bridge netfilter safe")
		}
	})

	t.Run("IPv6 bridges matter only with IPv6 on", func(t *testing.T) {
		v6 := tables.BridgeNetfilter{GlobalV6: true, BridgesV6: []string{"br-lan"}}
		cfg := newCfg(config.RoutingModeProxy)
		if bridges, safe := bridgeNetfilterStatus(v6, cfg); len(bridges) != 0 || !safe {
			t.Fatalf("IPv6 off: got %v safe=%v, want none and safe", bridges, safe)
		}
		cfg.Queue.IPv6Enabled = true
		if bridges, safe := bridgeNetfilterStatus(v6, cfg); !slices.Equal(bridges, []string{"br-lan"}) || safe {
			t.Fatalf("IPv6 on: got %v safe=%v, want br-lan and unsafe", bridges, safe)
		}
	})

	t.Run("no config", func(t *testing.T) {
		bridges, safe := bridgeNetfilterStatus(on, nil)
		if !slices.Equal(bridges, []string{"br-lan", "docker0"}) || !safe {
			t.Fatalf("got %v safe=%v, want both bridges and safe", bridges, safe)
		}
	})
}
