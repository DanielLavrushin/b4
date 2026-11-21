package mtproto

import (
	"context"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/selfdial"
)

func telegramNetnsRequire(t *testing.T) {
	t.Helper()
	if os.Getenv("B4_NETNS_TEST") != "1" {
		t.Skip("set B4_NETNS_TEST=1 and run inside a network namespace (make test-netns)")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root inside the namespace to set SO_MARK")
	}
	self, err1 := os.Readlink("/proc/self/ns/net")
	init, err2 := os.Readlink("/proc/1/ns/net")
	if err1 == nil && err2 == nil && self == init {
		t.Fatal("refusing to run: this is the host network namespace")
	}
	if out, err := exec.Command("ip", "link", "set", "lo", "up").CombinedOutput(); err != nil {
		t.Skipf("cannot bring lo up: %v (%s)", err, out)
	}
}

func TestNetnsATelegramSetDialIsKnownToTheTUNEngineBeforeTheServerAcceptsIt(t *testing.T) {
	telegramNetnsRequire(t)
	for _, c := range []struct {
		name     string
		cfg      *config.Config
		recorded bool
		why      string
	}{
		{"a dial for the Telegram set's strategy", telegramSetTestConfig(true), true, "the TUN engine sees no socket mark, so it knows b4's own Telegram connection only by the port b4 dialled from, and it must know it from the first packet"},
		{"a dial without a Telegram set", func() *config.Config { c := config.NewConfig(); return &c }(), false, "without a Telegram set no set may claim b4's own connection"},
	} {
		t.Run(c.name, func(t *testing.T) {
			useTelegramSet(t, c.cfg)
			ln, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			server := netip.MustParseAddrPort(ln.Addr().String())
			atAccept := make(chan bool, 1)
			peer := make(chan uint16, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					atAccept <- false
					peer <- 0
					return
				}
				defer conn.Close()
				from := netip.MustParseAddrPort(conn.RemoteAddr().String())
				atAccept <- selfdial.Holds(from.Port(), server)
				peer <- from.Port()
			}()

			mark, _ := resolveTelegramMark(selfDialMark())
			conn, err := dialTelegramTCP(context.Background(), "tcp4", ln.Addr().String(), mark, 3*time.Second)
			if err != nil {
				t.Fatalf("dial with mark 0x%x: %v", mark, err)
			}
			defer conn.Close()
			local := netip.MustParseAddrPort(conn.LocalAddr().String())

			if got := <-atAccept; got != c.recorded {
				t.Fatalf("known when the server accepted it: %v, want %v (mark 0x%x): %s", got, c.recorded, mark, c.why)
			}
			if got := <-peer; got != local.Port() {
				t.Fatalf("the server saw port %d, the dialer reports %d", got, local.Port())
			}
		})
	}
}
