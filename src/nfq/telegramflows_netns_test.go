package nfq

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"golang.org/x/sys/unix"
)

func netnsTelegramCountMark(t *testing.T, mark uint) {
	t.Helper()
	rule := []string{"-t", "mangle", "OUTPUT", "-o", netnsHoldLink, "-d", netnsHoldServer, "-p", "tcp", "-m", "mark", "--mark", fmt.Sprintf("%#x", mark), "-j", "ACCEPT"}
	netnsHoldRun(t, append([]string{"iptables", "-w", rule[0], rule[1], "-A"}, rule[2:]...)...)
	t.Cleanup(func() {
		_ = exec.Command("iptables", append([]string{"-w", rule[0], rule[1], "-D"}, rule[2:]...)...).Run()
	})
}

func netnsTelegramMarkCounts(t *testing.T) map[string]int {
	t.Helper()
	out, err := exec.Command("iptables", "-w", "-t", "mangle", "-L", "OUTPUT", "-v", "-x", "-n").CombinedOutput()
	if err != nil {
		t.Fatalf("list mangle OUTPUT: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	counts := map[string]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[len(f)-2] != "match" {
			continue
		}
		n, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		counts[f[len(f)-1]] = n
	}
	return counts
}

func TestNetnsAStartedWorkerSendsIntoItsTelegramConnectionsWithTheirDialMark(t *testing.T) {
	netnsHoldRequire(t)
	netnsHoldStartPeer(t)
	netnsHoldRun(t, "ip", "link", "set", "lo", "up")

	cfg := config.NewConfig()
	cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
	telegramMark, injector := telegramInjectorMark(&cfg), injectorMark(&cfg)
	if telegramMark == 0 || telegramMark == injector {
		t.Fatalf("the default config has no separate Telegram injector mark (%#x, injector %#x)", telegramMark, injector)
	}
	netnsTelegramCountMark(t, telegramMark)
	netnsTelegramCountMark(t, injector)

	w := newTestWorker(t, &cfg)
	w.qnum = netnsHoldQueue
	if err := w.Start(); err != nil {
		t.Fatalf("bind queue %d: %v", netnsHoldQueue, err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			w.Stop()
		}
	})

	server := net.ParseIP(netnsHoldServer)
	telegram := telegramFlowPacketV4(netnsHoldRouter, netnsHoldServer, 41031, netnsHoldPort, ipProtoTCP, 0)
	other := telegramFlowPacketV4(netnsHoldRouter, netnsHoldServer, 41032, netnsHoldPort, ipProtoTCP, 0)
	w.telegramFlows.note(telegram, time.Now())
	if err := w.sock.SendIPv4(telegram, server); err != nil {
		t.Fatalf("send into the Telegram connection: %v", err)
	}
	if err := w.sock.SendIPv4(other, server); err != nil {
		t.Fatalf("send into another connection: %v", err)
	}

	counts := netnsTelegramMarkCounts(t)
	if got := counts[fmt.Sprintf("%#x", telegramMark)]; got != 1 {
		t.Errorf("%d packet(s) left with the Telegram injector mark %#x, want the one sent into the noted connection: without the Telegram injector a routing set that lists the Telegram address takes them away from the connection (counters %v)", got, telegramMark, counts)
	}
	if got := counts[fmt.Sprintf("%#x", injector)]; got != 1 {
		t.Errorf("%d packet(s) left with the injector mark %#x, want the one sent into the connection nobody noted (counters %v)", got, injector, counts)
	}

	telegramSock := w.telegramSock
	if telegramSock == nil {
		t.Fatal("Start opened no Telegram injector")
	}
	w.Stop()
	stopped = true
	if err := telegramSock.SendIPv4(telegram, server); err == nil {
		t.Error("the Telegram injector still sends after Stop, so every restart leaks its raw sockets")
	}
}

func netnsDialWithMark(mark int) (net.Conn, error) {
	d := net.Dialer{
		Timeout: 3 * time.Second,
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				if mark != 0 {
					serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, mark)
				}
				if serr == nil {
					serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_NODELAY, 1)
				}
			}); err != nil {
				return err
			}
			return serr
		},
	}
	return d.Dial("tcp4", net.JoinHostPort(netnsHoldServer, strconv.Itoa(netnsHoldPort)))
}

func TestNetnsAStartedWorkerSendsIntoAConnectionItOpensItselfWithItsDialMark(t *testing.T) {
	for _, c := range []struct {
		name     string
		mark     int
		dialMark bool
	}{
		{"a connection b4 opens itself", int(config.SelfDialMark), true},
		{"another connection of the router", 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			netnsHoldRequire(t)
			srv := netnsHoldStartPeer(t)
			netnsHoldRun(t, "ip", "link", "set", "lo", "up")
			hello := buildClientHello(netnsStallHost, 300, 0xAB)
			got := netnsHoldServerIn(t, srv, len(hello))

			cfg := config.NewConfig()
			cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
			set := newFragmentingYTImagesSet()
			cfg.Sets = []*config.SetConfig{&set}
			dialMark, injector := telegramInjectorMark(&cfg), injectorMark(&cfg)
			if dialMark == 0 || dialMark == injector {
				t.Fatalf("the default config has no separate dial-mark injector (%#x, injector %#x)", dialMark, injector)
			}

			rule := []string{"-t", "mangle", "OUTPUT", "-o", netnsHoldLink, "-p", "tcp", "--dport", strconv.Itoa(netnsHoldPort),
				"-m", "mark", "!", "--mark", fmt.Sprintf("%#x/%#x", engine.InjectedMarkBit, engine.InjectedMarkBit),
				"-j", "NFQUEUE", "--queue-num", strconv.Itoa(netnsHoldQueue)}
			netnsHoldRun(t, append([]string{"iptables", "-w", rule[0], rule[1], "-A"}, rule[2:]...)...)
			t.Cleanup(func() {
				_ = exec.Command("iptables", append([]string{"-w", rule[0], rule[1], "-D"}, rule[2:]...)...).Run()
			})
			netnsTelegramCountMark(t, dialMark)
			netnsTelegramCountMark(t, injector)

			w := newTestWorker(t, &cfg)
			w.qnum = netnsHoldQueue
			if err := w.Start(); err != nil {
				t.Fatalf("bind queue %d: %v", netnsHoldQueue, err)
			}
			t.Cleanup(w.Stop)

			conn, err := netnsDialWithMark(c.mark)
			if err != nil {
				t.Fatalf("dial %s:%d with mark %#x: %v", netnsHoldServer, netnsHoldPort, c.mark, err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			if _, err := conn.Write(hello); err != nil {
				t.Fatalf("write the ClientHello: %v", err)
			}
			select {
			case b := <-got:
				if !bytes.Equal(b, hello) {
					t.Fatalf("the server read %d bytes that are not the ClientHello", len(b))
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the server never read the whole ClientHello")
			}

			counts := netnsTelegramMarkCounts(t)
			marked, plain := counts[fmt.Sprintf("%#x", dialMark)], counts[fmt.Sprintf("%#x", injector)]
			want, wrong := plain, marked
			if c.dialMark {
				want, wrong = marked, plain
			}
			if want < 2 || wrong != 0 {
				t.Fatalf("%d packet(s) left with the dial-mark injector's %#x and %d with the injector's %#x, want the split segments by the %s one: %s (counters %v)",
					marked, dialMark, plain, injector, map[bool]string{true: "first", false: "second"}[c.dialMark],
					map[bool]string{true: "the kernel reports the socket owner of a connection b4 opens with 0x40000, and without the dial mark a routing set that lists the address takes its segments away from it", false: "a router connection without the dial mark keeps the ordinary injector"}[c.dialMark], counts)
			}
		})
	}
}
