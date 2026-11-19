package nfq

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
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
