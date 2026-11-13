package nfq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/iphealth"
	"golang.org/x/sys/unix"
)

const netnsConntrackSysctls = "/proc/sys/net/netfilter/"

func netnsConntrackSysctl(t *testing.T, name, value string) {
	t.Helper()
	path := netnsConntrackSysctls + name
	prev, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("cannot read %s in this namespace: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Skipf("cannot set %s in this namespace: %v", name, err)
	}
	t.Cleanup(func() { _ = os.WriteFile(path, prev, 0o644) })
}

func netnsHelloServerIn(t *testing.T, pid, helloLen, answer int) {
	t.Helper()
	var ln net.Listener
	netnsHoldInside(t, pid, func() error {
		var err error
		ln, err = net.Listen("tcp4", net.JoinHostPort(netnsHoldServer, strconv.Itoa(netnsHoldPort)))
		return err
	})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		if answer > 0 {
			_, _ = io.ReadFull(c, make([]byte, helloLen))
			time.Sleep(300 * time.Millisecond)
			_, _ = c.Write(make([]byte, answer))
		}
		<-release
	}()
	t.Cleanup(func() {
		close(release)
		ln.Close()
		wg.Wait()
	})
}

type netnsFakedHello struct {
	w      *Worker
	pool   *Pool
	conn   net.Conn
	key    string
	tuple  ctnl.Tuple
	probes *atomic.Int32
}

func netnsStartFakedHello(t *testing.T, desync config.DesyncConfig, answer int, closeTimeout string) *netnsFakedHello {
	t.Helper()
	netnsHoldRequire(t)
	hello := buildClientHello(netnsStallHost, 16, 0xAB)
	srv := netnsHoldStartPeer(t)
	netnsHoldRun(t, "ip", "link", "set", "lo", "up")
	netnsHelloServerIn(t, srv, len(hello), answer)

	set := netnsStallSet()
	set.TCP.Desync = desync
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.CacheBlockedIPs = true
	cfg := config.NewConfig()
	cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
	cfg.Sets = []*config.SetConfig{&set}

	rule := []string{"POSTROUTING", "-o", netnsHoldLink, "-p", "tcp", "--dport", strconv.Itoa(netnsHoldPort),
		"-m", "connbytes", "--connbytes-dir", "original", "--connbytes-mode", "packets", "--connbytes", "0:19",
		"-m", "conntrack", "--ctdir", "ORIGINAL",
		"-m", "mark", "!", "--mark", fmt.Sprintf("0x%x/0x%x", cfg.Queue.Mark, cfg.Queue.Mark),
		"-j", "NFQUEUE", "--queue-num", strconv.Itoa(netnsHoldQueue)}
	netnsHoldRun(t, append([]string{"iptables", "-w", "-t", "mangle", "-A"}, rule...)...)
	t.Cleanup(func() {
		_ = exec.Command("iptables", append([]string{"-w", "-t", "mangle", "-D"}, rule...)...).Run()
	})
	netnsConntrackSysctl(t, "nf_conntrack_acct", "1")
	netnsConntrackSysctl(t, "nf_conntrack_tcp_be_liberal", "1")
	netnsConntrackSysctl(t, "nf_conntrack_tcp_timeout_close", closeTimeout)

	w := newTestWorker(t, &cfg)
	w.qnum = netnsHoldQueue
	probes := &atomic.Int32{}
	w.bans = newTestIPBans(t, func(context.Context, net.IP, int, int) iphealth.TLSResult {
		probes.Add(1)
		return iphealth.TLSSilent
	})
	pool := &Pool{Workers: []*Worker{w}, state: &runtimeState{bans: w.bans, destState: w.destState}}
	pool.wireBans()
	silentOnceBefore(w.bans, net.JoinHostPort(netnsHoldServer, strconv.Itoa(netnsHoldPort)))
	if err := w.Start(); err != nil {
		t.Fatalf("bind queue %d: %v", netnsHoldQueue, err)
	}
	t.Cleanup(w.Stop)

	conn, err := netnsHoldDial()
	if err != nil {
		t.Fatalf("dial %s:%d: %v", netnsHoldServer, netnsHoldPort, err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	local := conn.LocalAddr().(*net.TCPAddr).AddrPort()
	remote := conn.RemoteAddr().(*net.TCPAddr).AddrPort()
	tuple, err := ctnl.NewTuple(unix.IPPROTO_TCP, netip.AddrPortFrom(local.Addr().Unmap(), local.Port()), netip.AddrPortFrom(remote.Addr().Unmap(), remote.Port()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(hello); err != nil {
		t.Fatalf("write the ClientHello: %v", err)
	}
	h := &netnsFakedHello{
		w:      w,
		pool:   pool,
		conn:   conn,
		key:    fmt.Sprintf(connKeyFormat, netnsHoldRouter, local.Port(), netnsHoldServer, netnsHoldPort),
		tuple:  tuple,
		probes: probes,
	}
	deadline := time.Now().Add(3 * time.Second)
	for !w.destState.Watching() {
		if time.Now().After(deadline) {
			t.Fatal("the ClientHello was never queued, so this test proves nothing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return h
}

func (h *netnsFakedHello) flow(t *testing.T) (ctnl.Flow, bool) {
	t.Helper()
	f, found, err := ctnl.Lookup(h.tuple, ctnl.Original)
	if err != nil {
		t.Fatalf("conntrack lookup: %v", err)
	}
	return f, found
}

func (h *netnsFakedHello) waitForEntry(t *testing.T, within time.Duration, ready func(ctnl.Flow, bool) bool) (ctnl.Flow, bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		f, found := h.flow(t)
		if ready(f, found) || time.Now().After(deadline) {
			return f, found
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (h *netnsFakedHello) clientStillOpen(t *testing.T) bool {
	t.Helper()
	raw, err := h.conn.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var info *unix.TCPInfo
	if cerr := raw.Control(func(fd uintptr) {
		info, err = unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO)
	}); cerr != nil || err != nil {
		t.Fatalf("read the client's socket state: %v, %v", cerr, err)
	}
	return info.State == unix.BPF_TCP_ESTABLISHED
}

func (h *netnsFakedHello) sweepAndExpectReset(t *testing.T) {
	t.Helper()
	h.pool.sweepSilentHellos(time.Now().Add(time.Minute))
	h.w.bans.wg.Wait()
	if n := h.probes.Load(); n != 1 {
		t.Fatalf("the silent server must be checked once with neutral names, checked %d times", n)
	}
	if !h.w.bans.reg.Banned(net.ParseIP(netnsHoldServer), netnsHoldPort) {
		t.Fatal("a silent neutral-name check must ban the address")
	}
	if _, err := h.conn.Read(make([]byte, 1)); !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("the client waiting on the banned address must be reset, its read ended with: %v", err)
	}
	if h.w.destState.Watching() {
		t.Error("a settled connection must not stay watched")
	}
}

func TestNetnsAConnectionItsOwnFakesClosedIsCheckedAndReset(t *testing.T) {
	for _, c := range []struct {
		name   string
		desync config.DesyncConfig
		closes bool
	}{
		{"rst", config.DesyncConfig{Mode: "rst", TTL: 3, Count: 3}, true},
		{"fin", config.DesyncConfig{Mode: "fin", TTL: 3, Count: 3}, true},
		{"combo", config.DesyncConfig{Mode: "combo", TTL: 3, Count: 3}, true},
		{"full", config.DesyncConfig{Mode: "full", TTL: 3, Count: 3}, true},
		{"post-desync", config.DesyncConfig{Mode: config.ConfigOff, PostDesync: true, TTL: 3}, true},
		{"ack", config.DesyncConfig{Mode: "ack", TTL: 3, Count: 3}, false},
		{"no fakes", config.DesyncConfig{Mode: config.ConfigOff}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := netnsStartFakedHello(t, c.desync, 0, "10")
			f, found := h.waitForEntry(t, 600*time.Millisecond, func(f ctnl.Flow, found bool) bool {
				return !found || f.TCPState != ctnl.TCPEstablished
			})
			if !found || !f.HasState || !f.HasCounters {
				t.Fatalf("the connection must have an entry with a state and counters, or this test proves nothing: %+v, found %v", f, found)
			}
			if closed := f.TCPState != ctnl.TCPEstablished; closed != c.closes {
				t.Fatalf("conntrack state %d: closed by the strategy = %v, want %v, so this test no longer proves what it says", f.TCPState, closed, c.closes)
			}
			if !h.clientStillOpen(t) {
				t.Fatal("the client must still be waiting for the server, or there is nothing to free")
			}
			h.sweepAndExpectReset(t)
		})
	}
}

func TestNetnsAFakeClosedEntryThatExpiredIsStillChecked(t *testing.T) {
	h := netnsStartFakedHello(t, config.DesyncConfig{Mode: "rst", TTL: 3, Count: 3}, 0, "1")
	if _, found := h.waitForEntry(t, 5*time.Second, func(_ ctnl.Flow, found bool) bool { return !found }); found {
		t.Fatal("the closed entry never expired, so this test proves nothing")
	}
	if !h.clientStillOpen(t) {
		t.Fatal("the client must still be waiting for the server, or there is nothing to free")
	}
	h.sweepAndExpectReset(t)
}

func TestNetnsAServerThatAnsweredAfterTheFakesClosedTheEntryReadsAsAnswered(t *testing.T) {
	for _, mode := range []string{"rst", "fin"} {
		t.Run(mode, func(t *testing.T) {
			const answer = 3000
			h := netnsStartFakedHello(t, config.DesyncConfig{Mode: mode, TTL: 3, Count: 3}, answer, "10")
			h.w.destState.watchMu.RLock()
			watched, ok := h.w.destState.watches[h.key]
			var watch helloWatch
			if ok {
				watch = *watched
			}
			h.w.destState.watchMu.RUnlock()
			if !ok {
				t.Fatal("the connection must be watched")
			}
			if _, err := io.ReadFull(h.conn, make([]byte, answer)); err != nil {
				t.Fatalf("the server's answer must reach the client: %v", err)
			}
			f, found := h.waitForEntry(t, time.Second, func(f ctnl.Flow, found bool) bool {
				return found && f.ReplyBytes > f.ReplyPackets*80
			})
			if !found || f.TCPState == ctnl.TCPEstablished {
				t.Fatalf("the fakes must have closed the entry, or this test proves nothing: %+v, found %v", f, found)
			}
			got, err := checkServerSilence(watch)
			if err != nil {
				t.Fatal(err)
			}
			if got != silenceAnswered {
				t.Fatalf("a closed entry still counts the server's bytes, so the answer must be seen: read %d, flow %+v", got, f)
			}
		})
	}
}
