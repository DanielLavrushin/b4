package config

import "testing"

func telegramSetConfig() Config {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id, set.Name, set.Enabled = "tg", "Telegram", true
	cfg.Sets = []*SetConfig{&set}
	cfg.System.MTProto.UpstreamSet = "tg"
	return cfg
}

func TestTelegramUpstreamSetIsUsableOnlyWhenItCanApply(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*Config)
		usable bool
	}{
		{"an enabled set", func(*Config) {}, true},
		{"no set chosen", func(c *Config) { c.System.MTProto.UpstreamSet = "" }, false},
		{"the chosen set no longer exists", func(c *Config) { c.System.MTProto.UpstreamSet = "gone" }, false},
		{"the chosen set is disabled", func(c *Config) { c.Sets[0].Enabled = false }, false},
		{"a block set", func(c *Config) { c.Sets[0].Routing.Enabled, c.Sets[0].Routing.Mode = true, RoutingModeBlock }, false},
		{"a Telegram over WebSocket set, which would loop into b4's own bridge", func(c *Config) {
			c.Sets[0].Routing.Enabled, c.Sets[0].Routing.Mode = true, RoutingModeMTProtoWS
		}, false},
		{"a Telegram over WebSocket mode left over on a set with routing off", func(c *Config) {
			c.Sets[0].Routing.Mode = RoutingModeMTProtoWS
		}, true},
		{"an interface set", func(c *Config) {
			c.Sets[0].Routing.Enabled, c.Sets[0].Routing.Mode, c.Sets[0].Routing.EgressInterface = true, RoutingModeInterface, "wg0"
		}, true},
		{"an interface set with no interface chosen", func(c *Config) {
			c.Sets[0].Routing.Enabled, c.Sets[0].Routing.Mode = true, RoutingModeInterface
		}, false},
		{"a proxy set", func(c *Config) {
			c.Sets[0].Routing.Enabled, c.Sets[0].Routing.Mode, c.Sets[0].Routing.Upstream.Port = true, RoutingModeProxy, 1080
		}, true},
		{"a proxy set with no upstream port", func(c *Config) {
			c.Sets[0].Routing.Enabled, c.Sets[0].Routing.Mode = true, RoutingModeProxy
		}, false},
		{"TUN mode, whose engine knows b4's own connections by their addresses", func(c *Config) { c.Queue.Mode = "tun" }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := telegramSetConfig()
			c.change(&cfg)
			if got := cfg.TelegramUpstreamSet() != nil; got != c.usable {
				t.Fatalf("usable %v, want %v", got, c.usable)
			}
		})
	}
	var none *Config
	if none.TelegramUpstreamSet() != nil {
		t.Fatal("a nil config has no Telegram set")
	}
}

func TestTelegramSetMarkNeedsBothBits(t *testing.T) {
	for _, c := range []struct {
		mark uint32
		want bool
	}{
		{TelegramSetDialMark, true},
		{TelegramSetDialMark | 0x8000, true},
		{SelfDialMark, false},
		{TelegramSetBit, false},
		{SelfDialRelayMark, false},
	} {
		if got := IsTelegramSetMark(c.mark); got != c.want {
			t.Errorf("mark 0x%x: %v, want %v", c.mark, got, c.want)
		}
	}
	if IsSelfDialSkipMark(TelegramSetDialMark) {
		t.Error("the Telegram-set mark must not read as b4's no-DPI mark, or its connections would never be processed")
	}
}

func TestAMarkSharingTheTelegramSetBitsLeavesTheSetUnused(t *testing.T) {
	for _, mark := range []uint{uint(TelegramSetBit), uint(TelegramSetDialMark)} {
		cfg := telegramSetConfig()
		cfg.Queue.Mark = mark
		if err := cfg.Validate(); err != nil {
			t.Fatalf("queue mark 0x%x was valid before the Telegram set existed and must stay valid: %v", mark, err)
		}
		if cfg.TelegramUpstreamSet() != nil {
			t.Errorf("queue mark 0x%x: b4's Telegram connections would carry every bit of it and skip packet processing, so the set must not be used", mark)
		}
	}

	cfg := telegramSetConfig()
	cfg.System.Checker.DiscoveryFlowMark = uint(TelegramSetDialMark | 0x8000)
	cfg.System.Checker.DiscoveryInjectedMark = 0xBBBB
	if cfg.TelegramUpstreamSet() != nil {
		t.Error("a Discovery mark carrying the Telegram-set bits would give Discovery's packets that set's strategy, so the set must not be used")
	}

	cfg = telegramSetConfig()
	cfg.Queue.Mark = 0x4008000
	if cfg.TelegramUpstreamSet() == nil {
		t.Error("a queue mark with a bit outside 0x4040000 does not clash with the Telegram-set mark")
	}
}
