package tproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"sync/atomic"
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

type handleCounter struct {
	calls atomic.Int32
}

func (b *handleCounter) Handle(net.Conn, net.IP, int) (bool, net.Conn) {
	b.calls.Add(1)
	return true, nil
}

func (b *handleCounter) FailOpenViaWorker(net.Conn, net.IP, int) bool { return false }

func (b *handleCounter) FailOpenOrder(net.IP, int) (bool, bool) { return false, false }

func (b *handleCounter) NoteFailOpenDirect(net.IP, bool, int64) {}

func (b *handleCounter) Track(net.Conn) func() { return func() {} }

func listenLoopback(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func bridgeBesideTelegramUpstream(t *testing.T, upstreamPort int, chosen, procRoot string) (*Listener, *handleCounter) {
	t.Helper()
	cfg := config.NewConfig()
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "tg", "Telegram", true
	set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeProxy
	set.Routing.Upstream.Host, set.Routing.Upstream.Port = "127.0.0.1", upstreamPort
	cfg.Sets = []*config.SetConfig{&set}
	cfg.System.MTProto.UpstreamSet = chosen

	m := NewManager(nil)
	t.Cleanup(m.cancel)
	peer := m.newListener(&cfg, &set, 0)
	peer.ctx = m.ctx
	if procRoot != "" {
		peer.guard.procRoot = procRoot
	}
	m.listeners[set.Id] = peer
	m.publishLocked()

	rec := &handleCounter{}
	bridge := &Listener{SetName: "Telegram over WebSocket", MTProtoWS: true, Bridge: rec, peers: m.running, ctx: m.ctx}
	return bridge, rec
}

func TestTheBridgeSendsTheConnectionsOfTheTelegramSetsUpstreamDirect(t *testing.T) {
	if _, err := os.Stat("/proc/net/tcp"); err != nil {
		t.Skip("no /proc/net/tcp")
	}
	t.Cleanup(func() { SetTelegramDial(nil) })
	var telegramDials atomic.Int32
	SetTelegramDial(func(context.Context, string) (net.Conn, bool, error) {
		telegramDials.Add(1)
		return nil, true, errors.New("through the Telegram set")
	})

	otherProcess := func(t *testing.T, upstreamPort int, client, dst string) string {
		root := t.TempDir()
		writeProcFixture(t, root,
			[]fixtureSocket{
				{local: "127.0.0.1:" + strconv.Itoa(upstreamPort), remote: "0.0.0.0:0", state: listenState, inode: 10},
				{local: client, remote: dst, state: establishedState, inode: 20},
			},
			nil,
			map[int][]int{100: {10}, 200: {20}},
			map[int]string{100: "xray", 200: "telegram-desktop"},
		)
		return root
	}

	for _, c := range []struct {
		name     string
		chosen   string
		procRoot func(t *testing.T, upstreamPort int, client, dst string) string
		direct   bool
	}{
		{name: "a connection of the upstream's process", chosen: "tg", direct: true},
		{name: "a proxy set that is not the Telegram set", chosen: ""},
		{name: "a connection of another process", chosen: "tg", procRoot: otherProcess},
	} {
		t.Run(c.name, func(t *testing.T) {
			telegramDials.Store(0)
			upstream := listenLoopback(t)
			telegram := listenLoopback(t)
			upstreamPort := upstream.Addr().(*net.TCPAddr).Port

			out, err := net.Dial("tcp4", telegram.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			captured, err := telegram.Accept()
			if err != nil {
				t.Fatal(err)
			}
			root := ""
			if c.procRoot != nil {
				root = c.procRoot(t, upstreamPort, out.LocalAddr().String(), telegram.Addr().String())
			}
			bridge, rec := bridgeBesideTelegramUpstream(t, upstreamPort, c.chosen, root)

			served := make(chan struct{})
			go func() {
				defer close(served)
				bridge.serve(captured, nil)
			}()

			if !c.direct {
				select {
				case <-served:
				case <-time.After(5 * time.Second):
					t.Fatal("the bridge never finished with the connection")
				}
				if rec.calls.Load() != 1 {
					t.Fatalf("the bridge handled the connection %d times, want once: only the Telegram set's upstream is sent direct", rec.calls.Load())
				}
				return
			}

			if _, err := out.Write([]byte("ping")); err != nil {
				t.Fatal(err)
			}
			_ = telegram.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
			direct, err := telegram.Accept()
			if err != nil {
				t.Fatalf("the upstream's connection never reached Telegram directly: %v", err)
			}
			defer direct.Close()
			got := make([]byte, 4)
			_ = direct.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, err := io.ReadFull(direct, got); err != nil || string(got) != "ping" {
				t.Fatalf("Telegram got %q (%v), want the upstream's bytes", got, err)
			}
			_ = out.Close()
			_ = direct.Close()
			select {
			case <-served:
			case <-time.After(5 * time.Second):
				t.Fatal("the direct relay never ended")
			}
			if n := rec.calls.Load(); n != 0 {
				t.Fatalf("the bridge handled the upstream's own connection %d times, so b4 would dial it through the Telegram set's upstream again: a loop", n)
			}
			if n := telegramDials.Load(); n != 0 {
				t.Fatalf("the direct relay went through the Telegram set's dial %d times, back into the upstream", n)
			}
		})
	}
}
