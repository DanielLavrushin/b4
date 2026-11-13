package nfq

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/sys/unix"
)

const (
	netnsWSLANLink   = "b4ws0"
	netnsWSLANPeer   = "b4ws0p"
	netnsWSLANRouter = "192.168.88.1"
	netnsWSLANClient = "192.168.88.2"
)

func netnsWSRule(t *testing.T, rule ...string) {
	t.Helper()
	netnsHoldRun(t, append([]string{"iptables", "-w", rule[0], rule[1], "-A"}, rule[2:]...)...)
	t.Cleanup(func() {
		_ = exec.Command("iptables", append([]string{"-w", rule[0], rule[1], "-D"}, rule[2:]...)...).Run()
	})
}

func netnsWSStartLANClient(t *testing.T) int {
	t.Helper()
	netnsHoldRun(t, "ip", "link", "set", "lo", "up")
	_ = exec.Command("ip", "link", "del", netnsWSLANLink).Run()
	netnsHoldRun(t, "ip", "link", "add", netnsWSLANLink, "type", "veth", "peer", "name", netnsWSLANPeer)
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", netnsWSLANLink).Run() })
	netnsHoldRun(t, "ip", "link", "set", netnsWSLANLink, "up")
	netnsHoldRun(t, "ip", "addr", "add", netnsWSLANRouter+"/24", "dev", netnsWSLANLink)

	cmd := exec.Command("unshare", "-n", "sleep", "600")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot open a third network namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	pid := cmd.Process.Pid
	self, _ := os.Readlink("/proc/thread-self/ns/net")
	deadline := time.Now().Add(3 * time.Second)
	for {
		other, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/net", pid))
		if err == nil && other != self {
			break
		}
		if time.Now().After(deadline) {
			t.Skip("the child never entered its own network namespace")
		}
		time.Sleep(20 * time.Millisecond)
	}

	netnsHoldRun(t, "ip", "link", "set", netnsWSLANPeer, "netns", strconv.Itoa(pid))
	in := func(args ...string) {
		netnsHoldRun(t, append([]string{"nsenter", "-t", strconv.Itoa(pid), "-n"}, args...)...)
	}
	in("ip", "link", "set", "lo", "up")
	in("ip", "link", "set", netnsWSLANPeer, "up")
	in("ip", "addr", "add", netnsWSLANClient+"/24", "dev", netnsWSLANPeer)
	in("ip", "route", "add", "default", "via", netnsWSLANRouter)
	if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0o644); err != nil {
		t.Skipf("cannot enable IPv4 forwarding: %v", err)
	}
	netnsWSRule(t, "-t", "nat", "POSTROUTING", "-o", netnsHoldLink, "-j", "MASQUERADE")
	return pid
}

func netnsWSStartWorkerBehindProductionRules(t *testing.T, set *config.SetConfig) *Worker {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
	cfg.Sets = []*config.SetConfig{set}
	queue, port := strconv.Itoa(netnsHoldQueue), strconv.Itoa(netnsHoldPort)
	window := fmt.Sprintf("0:%d", cfg.Queue.TCPConnBytesLimit)
	mark := fmt.Sprintf("0x%x/0x%x", cfg.Queue.Mark, cfg.Queue.Mark)
	netnsWSRule(t, "-t", "mangle", "POSTROUTING", "-p", "tcp", "--dport", port, "-m", "mark", "!", "--mark", mark,
		"-m", "connbytes", "--connbytes-dir", "original", "--connbytes-mode", "packets", "--connbytes", window, "-j", "NFQUEUE", "--queue-num", queue)
	netnsWSRule(t, "-t", "mangle", "PREROUTING", "-p", "tcp", "--sport", port,
		"-m", "connbytes", "--connbytes-dir", "reply", "--connbytes-mode", "packets", "--connbytes", window, "-j", "NFQUEUE", "--queue-num", queue)
	netnsWSRule(t, "-t", "mangle", "PREROUTING", "-p", "tcp", "--sport", port, "--tcp-flags", "SYN,ACK", "SYN,ACK", "-j", "NFQUEUE", "--queue-num", queue)
	netnsWSRule(t, "-t", "mangle", "PREROUTING", "-p", "tcp", "--sport", port, "--tcp-flags", "RST", "RST", "-j", "NFQUEUE", "--queue-num", queue)
	w := newTestWorker(t, &cfg)
	w.qnum = netnsHoldQueue
	if err := w.Start(); err != nil {
		t.Fatalf("bind queue %d: %v", netnsHoldQueue, err)
	}
	t.Cleanup(w.Stop)
	return w
}

func netnsWSEchoServerIn(t *testing.T, pid int) *atomic.Value {
	t.Helper()
	var ln net.Listener
	netnsHoldInside(t, pid, func() error {
		var err error
		ln, err = net.Listen("tcp4", net.JoinHostPort(netnsHoldServer, strconv.Itoa(netnsHoldPort)))
		return err
	})
	peer := &atomic.Value{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		peer.Store(c.RemoteAddr().(*net.TCPAddr).IP.String())
		_ = c.SetDeadline(time.Now().Add(15 * time.Second))
		block := make([]byte, 100)
		for {
			if _, err := io.ReadFull(c, block); err != nil {
				return
			}
			if _, err := c.Write(block); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
	})
	return peer
}

func TestNetnsWSSizeRewritesTheRealOpening(t *testing.T) {
	netnsHoldRequire(t)
	srv := netnsHoldStartPeer(t)
	payload := []byte("first outgoing data")
	reads := netnsHoldServerIn(t, srv, len(payload))
	wire := netnsHoldCapture(t, srv)
	_, set := wsFixture()
	set.Targets.IpsToMatch = []string{netnsHoldServer}
	set.TCP.RSTProtection.Enabled = false
	netnsOverlapStart(t, srv, set)
	conn, err := netnsHoldDial()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err = conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-reads:
		if !bytes.Equal(got, payload) {
			t.Fatal("server did not receive original data")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server timed out")
	}
	time.Sleep(50 * time.Millisecond)
	port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
	syn, data := false, false
	for _, p := range wire() {
		if p.sport != port || p.dport != netnsHoldPort {
			continue
		}
		if p.flags&2 != 0 {
			syn = true
			if p.window != 1 || p.scale != 1 {
				t.Fatalf("real SYN window=%d scale=%d", p.window, p.scale)
			}
		}
		if p.payload > 0 {
			data = true
			if p.window != 1 {
				t.Fatalf("first data window=%d", p.window)
			}
		}
	}
	if !syn || !data {
		t.Fatal("opening not captured")
	}
}

func TestNetnsWSSizeZeroLeavesTheKernelsOwnValue(t *testing.T) {
	const closedPort = netnsHoldPort + 1
	for _, numbers := range []config.WSSizeConfig{{Window: 0, Scale: 2}, {Window: 4096, Scale: 0}} {
		t.Run(fmt.Sprintf("window %d scale %d", numbers.Window, numbers.Scale), func(t *testing.T) {
			netnsHoldRequire(t)
			srv := netnsHoldStartPeer(t)
			payload := []byte("first outgoing data")
			reads := netnsHoldServerIn(t, srv, len(payload))
			wire := netnsHoldCapture(t, srv)
			_, set := wsFixture()
			set.Targets.IpsToMatch = []string{netnsHoldServer}
			set.TCP.RSTProtection.Enabled = false
			set.TCP.WSSize.Window, set.TCP.WSSize.Scale = numbers.Window, numbers.Scale
			netnsOverlapStart(t, srv, set)
			untouched := net.Dialer{Timeout: 3 * time.Second, Control: func(_, _ string, c syscall.RawConn) error {
				var serr error
				if err := c.Control(func(fd uintptr) {
					serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_MAXSEG, netnsHoldMSS)
				}); err != nil {
					return err
				}
				return serr
			}}
			if refused, err := untouched.Dial("tcp4", net.JoinHostPort(netnsHoldServer, strconv.Itoa(closedPort))); err == nil {
				refused.Close()
				t.Fatalf("port %d on the server was expected to be closed", closedPort)
			}
			conn, err := netnsHoldDial()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err = conn.Write(payload); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-reads:
				if !bytes.Equal(got, payload) {
					t.Fatal("the server did not receive the original data")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the server timed out")
			}
			port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
			conn.Close()
			time.Sleep(50 * time.Millisecond)
			var own, syn, data *netnsWireSegment
			seen := wire()
			for i := range seen {
				p := &seen[i]
				switch {
				case p.dport == closedPort && p.flags&2 != 0 && own == nil:
					own = p
				case p.sport == port && p.dport == netnsHoldPort && p.flags&2 != 0 && syn == nil:
					syn = p
				case p.sport == port && p.dport == netnsHoldPort && p.payload > 0 && data == nil:
					data = p
				}
			}
			if own == nil || syn == nil || data == nil {
				t.Fatalf("the capture missed a packet: untouched SYN %v, rewritten SYN %v, first data %v", own != nil, syn != nil, data != nil)
			}
			if own.window == 0 || own.scale <= 2 {
				t.Skipf("the kernel's own SYN has window %d and scale %d, the case needs a window above 0 and a scale above 2", own.window, own.scale)
			}
			if numbers.Window == 0 && (syn.window != own.window || data.window == 0) {
				t.Fatalf("window 0 leaves the window alone, but the SYN reached the server with window %d instead of the kernel's %d and the first data packet with window %d", syn.window, own.window, data.window)
			}
			if numbers.Window > 0 && (int(syn.window) != numbers.Window || int(data.window) != numbers.Window) {
				t.Fatalf("the SYN reached the server with window %d and the first data packet with window %d, want %d", syn.window, data.window, numbers.Window)
			}
			if numbers.Scale == 0 && syn.scale != own.scale {
				t.Fatalf("scale 0 leaves the Window Scale option alone, but the SYN reached the server with scale %d instead of the kernel's %d", syn.scale, own.scale)
			}
			if numbers.Scale > 0 && syn.scale != numbers.Scale {
				t.Fatalf("the SYN reached the server with scale %d, want %d", syn.scale, numbers.Scale)
			}
		})
	}
}

func TestNetnsWSSizeOriginalHelloAndRealFragments(t *testing.T) {
	for _, strategy := range []string{"none", "combo"} {
		t.Run(strategy, func(t *testing.T) {
			netnsHoldRequire(t)
			srv := netnsHoldStartPeer(t)
			hello := wsHelloRecords(buildClientHello(netnsStallHost, 1400, 0xab), 100)
			later := []byte("later application data")
			reads := netnsOverlapServerIn(t, srv, len(hello), len(later))
			wire := netnsHoldCapture(t, srv)
			_, set := wsFixture()
			set.Targets.IpsToMatch = []string{netnsHoldServer}
			set.TCP.RSTProtection.Enabled = false
			set.TCP.WSSize.Cutoff = wsHello
			set.Fragmentation.Strategy = strategy
			if strategy == "combo" {
				set.Fragmentation = config.DefaultSetConfig.Fragmentation
				set.Fragmentation.Strategy = strategy
				set.Fragmentation.Combo.FirstDelayMs = 1
				set.Fragmentation.Combo.JitterMaxUs = 1
				set.Fragmentation.Combo.FirstByteSplit = true
				set.Fragmentation.Combo.ExtensionSplit = false
			}
			netnsOverlapStart(t, srv, set)
			conn, err := netnsHoldDial()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(5 * time.Second))
			for _, part := range [][]byte{hello[:32], hello[32:]} {
				if _, err := conn.Write(part); err != nil {
					t.Fatal(err)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if _, err := conn.Read(make([]byte, 1)); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Write(later); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-reads:
				if got.err != nil || !bytes.Equal(got.got, append(hello, later...)) {
					t.Fatalf("original application bytes changed: %v", got.err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("receiver timed out")
			}
			time.Sleep(50 * time.Millisecond)
			port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
			var start uint32
			parts, after := 0, 0
			tinyFirst := false
			for _, p := range wire() {
				if p.sport != port || p.dport != netnsHoldPort {
					continue
				}
				if p.flags&2 != 0 {
					start = p.seq + 1
					if p.window != 1 || p.scale != 1 {
						t.Fatal("opening window/scale missing")
					}
				}
				if p.payload == 0 {
					continue
				}
				if int32(p.seq-start) < int32(len(hello)) {
					parts++
					if p.seq == start && p.payload == 1 {
						tinyFirst = true
					}
					if p.window != 1 {
						t.Fatalf("original ClientHello part lost window: %+v", p)
					}
				} else {
					after++
					if p.window == 1 {
						t.Fatalf("later data still rewritten: %+v", p)
					}
				}
			}
			if parts < 3 || after == 0 {
				t.Fatalf("capture did not exercise multiple original packets and cutoff: %d/%d", parts, after)
			}
			if strategy == "combo" && !tinyFirst {
				t.Fatal("Combo did not produce its real one-byte first fragment")
			}
		})
	}
}

func TestNetnsWSSizeForwardedClientBehindMasquerade(t *testing.T) {
	const exchanges = 40
	for _, cutoff := range wsCutoffs {
		t.Run(wsCutoffName(cutoff), func(t *testing.T) {
			netnsHoldRequire(t)
			srv := netnsHoldStartPeer(t)
			lan := netnsWSStartLANClient(t)
			peer := netnsWSEchoServerIn(t, srv)
			wire := netnsHoldCapture(t, srv)
			_, set := wsFixture()
			set.Targets.IpsToMatch = []string{netnsHoldServer}
			set.TCP.RSTProtection.Enabled = false
			set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 4096, Scale: 2, Cutoff: cutoff}
			w := netnsWSStartWorkerBehindProductionRules(t, set)
			queued := atomic.LoadUint64(&w.packetsProcessed)

			var conn net.Conn
			netnsHoldInside(t, lan, func() error {
				var err error
				conn, err = net.DialTimeout("tcp4", net.JoinHostPort(netnsHoldServer, strconv.Itoa(netnsHoldPort)), 3*time.Second)
				return err
			})
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(10 * time.Second))
			block := make([]byte, 100)
			for i := 0; i < exchanges; i++ {
				if _, err := conn.Write(block); err != nil {
					t.Fatalf("exchange %d: %v", i, err)
				}
				if _, err := io.ReadFull(conn, block); err != nil {
					t.Fatalf("exchange %d: %v", i, err)
				}
			}
			w.wssize.mu.Lock()
			held := len(w.wssize.flows)
			w.wssize.mu.Unlock()
			queued = atomic.LoadUint64(&w.packetsProcessed) - queued
			port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
			conn.Close()
			time.Sleep(100 * time.Millisecond)

			if got := peer.Load(); got != netnsHoldRouter {
				t.Fatalf("the server saw the client as %v, want the router's address %s: the connection was not masqueraded", got, netnsHoldRouter)
			}
			if queued >= 2*exchanges {
				t.Fatalf("%d packets went through the queue, the capture window was not in effect", queued)
			}
			var start uint32
			syn, first, later := false, false, 0
			for _, p := range wire() {
				if p.sport != port || p.dport != netnsHoldPort {
					continue
				}
				switch {
				case p.flags&2 != 0:
					syn, start = true, p.seq+1
					if p.window != 4096 || p.scale != 2 {
						t.Fatalf("the SYN reached the server with window %d and scale %d", p.window, p.scale)
					}
				case p.payload == 0:
				case p.seq == start:
					first = true
					if p.window != 4096 {
						t.Fatalf("the first data packet reached the server with window %d", p.window)
					}
				default:
					later++
					if p.window == 4096 {
						t.Fatalf("data after the cutoff is still rewritten: %+v", p)
					}
				}
			}
			if !syn || !first || later < exchanges/2 {
				t.Fatalf("the capture missed the opening or the later data: syn %v, first data %v, %d later data packets", syn, first, later)
			}
			if held != 0 {
				t.Fatalf("%d record(s) held while the connection was still open, long after its cutoff", held)
			}
		})
	}
}
