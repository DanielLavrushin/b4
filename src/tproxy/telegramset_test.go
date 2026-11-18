package tproxy

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/socks5"
)

func TestTelegramOverWebSocketDialsDirectWithTheTelegramSetMark(t *testing.T) {
	cfg := config.NewConfig()
	telegram := config.NewSetConfig()
	telegram.Id, telegram.Enabled = "tg", true
	bridge := config.NewSetConfig()
	bridge.Id, bridge.Enabled = "bridge", true
	bridge.Routing.Enabled, bridge.Routing.Mode = true, config.RoutingModeMTProtoWS
	proxy := config.NewSetConfig()
	proxy.Id, proxy.Enabled = "proxy", true
	proxy.Routing.Enabled, proxy.Routing.Mode = true, "proxy"
	cfg.Sets = []*config.SetConfig{&telegram, &bridge, &proxy}

	if got := telegramDirectMark(&cfg, &bridge); got != 0 {
		t.Fatalf("mark 0x%x with no Telegram set chosen, want the listener's own", got)
	}

	cfg.System.MTProto.UpstreamSet = telegram.Id
	if got := telegramDirectMark(&cfg, &bridge); got != config.TelegramSetDialMark {
		t.Fatalf("mark 0x%x for a Telegram over WebSocket set, want 0x%x", got, config.TelegramSetDialMark)
	}
	if got := telegramDirectMark(&cfg, &proxy); got != 0 {
		t.Fatalf("mark 0x%x for a proxy set, whose direct dials are not Telegram's", got)
	}
	telegram.Enabled = false
	if got := telegramDirectMark(&cfg, &bridge); got != 0 {
		t.Fatalf("mark 0x%x with the Telegram set disabled, want the listener's own", got)
	}

	l := &Listener{Upstream: socks5.ClientConfig{BypassMark: config.SelfDialMark}}
	if got := l.directDialMark(); got != config.SelfDialMark {
		t.Fatalf("direct dial mark 0x%x, want the bypass mark 0x%x", got, config.SelfDialMark)
	}
	l.directMark.Store(config.TelegramSetDialMark)
	if got := l.directDialMark(); got != config.TelegramSetDialMark {
		t.Fatalf("direct dial mark 0x%x, want 0x%x", got, config.TelegramSetDialMark)
	}
}
