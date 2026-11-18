package tproxy

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/socks5"
)

func TestTheBridgeDialsDirectThroughTheTelegramDialWhenItTakesTheDial(t *testing.T) {
	t.Cleanup(func() { SetTelegramDial(nil) })
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	dest := ln.Addr().String()

	var asked []string
	sentinel := errors.New("through the Telegram set")
	SetTelegramDial(func(_ context.Context, d string) (net.Conn, bool, error) {
		asked = append(asked, d)
		return nil, true, sentinel
	})

	bridge := &Listener{MTProtoWS: true, Upstream: socks5.ClientConfig{BypassMark: 0}}
	if _, err := bridge.dialDirectRelay(context.Background(), dest, time.Second); !errors.Is(err, sentinel) {
		t.Fatalf("bridge direct dial returned %v, want the Telegram dial's result", err)
	}
	if len(asked) != 1 || asked[0] != dest {
		t.Fatalf("Telegram dial asked for %v, want [%s]", asked, dest)
	}

	proxy := &Listener{Upstream: socks5.ClientConfig{BypassMark: 0}}
	conn, err := proxy.dialDirectRelay(context.Background(), dest, time.Second)
	if err != nil {
		t.Fatalf("a proxy set's direct dial must not go through the Telegram dial: %v", err)
	}
	_ = conn.Close()
	if len(asked) != 1 {
		t.Fatalf("Telegram dial asked %d times, want only the bridge's dial", len(asked))
	}

	SetTelegramDial(func(context.Context, string) (net.Conn, bool, error) { return nil, false, nil })
	conn, err = bridge.dialDirectRelay(context.Background(), dest, time.Second)
	if err != nil {
		t.Fatalf("without a Telegram set the bridge dials directly as before: %v", err)
	}
	_ = conn.Close()
}

func TestATargetlessProxySetGetsAListenerOnlyAsTheTelegramSet(t *testing.T) {
	cfg := config.NewConfig()
	proxy := config.NewSetConfig()
	proxy.Id, proxy.Enabled = "tg", true
	proxy.Routing.Enabled, proxy.Routing.Mode = true, config.RoutingModeProxy
	proxy.Routing.Upstream.Host, proxy.Routing.Upstream.Port = "127.0.0.1", 1080
	cfg.Sets = []*config.SetConfig{&proxy}

	if wantsListener(&cfg, &proxy) {
		t.Fatal("a proxy set with no targets has nothing to divert and needs no listener")
	}
	cfg.System.MTProto.UpstreamSet = proxy.Id
	if !wantsListener(&cfg, &proxy) {
		t.Fatal("chosen as the Telegram set, the proxy set needs a listener for b4's own Telegram dials")
	}
	proxy.Routing.Upstream.Port = 0
	if wantsListener(&cfg, &proxy) {
		t.Fatal("a proxy set without an upstream port is not usable as the Telegram set")
	}
}
