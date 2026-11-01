package nfq

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"golang.org/x/sys/unix"
)

const (
	netnsHoldLink   = "b4hh0"
	netnsHoldPeer   = "b4hh0p"
	netnsHoldRouter = "203.0.113.1"
	netnsHoldServer = "203.0.113.2"
	netnsHoldPort   = 443
	netnsHoldQueue  = 541
	netnsHoldMSS    = 600
)

type netnsWireSegment struct {
	sport, dport uint16
	seq          uint32
	payload      int
}

func netnsHoldRequire(t *testing.T) {
	t.Helper()
	if os.Getenv("B4_NETNS_TEST") != "1" {
		t.Skip("set B4_NETNS_TEST=1 and run inside a network namespace (make test-netns)")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root inside the namespace")
	}
	self, err1 := os.Readlink("/proc/self/ns/net")
	init, err2 := os.Readlink("/proc/1/ns/net")
	if err1 == nil && err2 == nil && self == init {
		t.Fatal("refusing to run: this is the host network namespace, and the test writes firewall rules")
	}
	for _, bin := range []string{"ip", "iptables", "unshare", "nsenter"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is not installed", bin)
		}
	}
}

func netnsHoldRun(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

func netnsHoldStartPeer(t *testing.T) int {
	t.Helper()
	_ = exec.Command("ip", "link", "del", netnsHoldLink).Run()
	netnsHoldRun(t, "ip", "link", "add", netnsHoldLink, "type", "veth", "peer", "name", netnsHoldPeer)
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", netnsHoldLink).Run() })
	netnsHoldRun(t, "ip", "link", "set", netnsHoldLink, "up")
	netnsHoldRun(t, "ip", "addr", "add", netnsHoldRouter+"/24", "dev", netnsHoldLink)

	cmd := exec.Command("unshare", "-n", "sleep", "600")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot open a second network namespace: %v", err)
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

	netnsHoldRun(t, "ip", "link", "set", netnsHoldPeer, "netns", strconv.Itoa(pid))
	in := func(args ...string) {
		netnsHoldRun(t, append([]string{"nsenter", "-t", strconv.Itoa(pid), "-n"}, args...)...)
	}
	in("ip", "link", "set", "lo", "up")
	in("ip", "link", "set", netnsHoldPeer, "up")
	in("ip", "addr", "add", netnsHoldServer+"/24", "dev", netnsHoldPeer)
	in("ip", "route", "add", "default", "via", netnsHoldRouter)
	return pid
}

func netnsHoldInside(t *testing.T, pid int, fn func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		home, err := os.Open("/proc/thread-self/ns/net")
		if err != nil {
			done <- err
			return
		}
		defer home.Close()
		target, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", pid))
		if err != nil {
			done <- err
			return
		}
		defer target.Close()
		if err := unix.Setns(int(target.Fd()), unix.CLONE_NEWNET); err != nil {
			done <- err
			return
		}
		err = fn()
		if unix.Setns(int(home.Fd()), unix.CLONE_NEWNET) == nil {
			runtime.UnlockOSThread()
		}
		done <- err
	}()
	if err := <-done; err != nil {
		t.Fatalf("inside the network namespace of %d: %v", pid, err)
	}
}

func netnsHoldParse(pkt []byte) (netnsWireSegment, bool) {
	var s netnsWireSegment
	if len(pkt) < 20 || pkt[0]>>4 != 4 || pkt[9] != unix.IPPROTO_TCP {
		return s, false
	}
	ihl := int(pkt[0]&0x0f) * 4
	end := min(int(binary.BigEndian.Uint16(pkt[2:4])), len(pkt))
	if ihl < 20 || end < ihl+20 {
		return s, false
	}
	tcp := pkt[ihl:end]
	off := int(tcp[12]>>4) * 4
	if off < 20 || off > len(tcp) {
		return s, false
	}
	s.sport, s.dport = binary.BigEndian.Uint16(tcp[0:2]), binary.BigEndian.Uint16(tcp[2:4])
	s.seq, s.payload = binary.BigEndian.Uint32(tcp[4:8]), len(tcp)-off
	return s, true
}

func netnsHoldCapture(t *testing.T, pid int) func() []netnsWireSegment {
	t.Helper()
	proto := binary.NativeEndian.Uint16(binary.BigEndian.AppendUint16(nil, unix.ETH_P_IP))
	fd := -1
	netnsHoldInside(t, pid, func() error {
		iface, err := net.InterfaceByName(netnsHoldPeer)
		if err != nil {
			return err
		}
		if fd, err = unix.Socket(unix.AF_PACKET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, int(proto)); err != nil {
			return err
		}
		if err := unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: proto, Ifindex: iface.Index}); err != nil {
			return err
		}
		return unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &unix.Timeval{Usec: 50000})
	})
	var mu sync.Mutex
	var seen []netnsWireSegment
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65536)
		for {
			select {
			case <-stop:
				return
			default:
			}
			n, from, err := unix.Recvfrom(fd, buf, 0)
			if err != nil {
				continue
			}
			if ll, ok := from.(*unix.SockaddrLinklayer); ok && ll.Pkttype == unix.PACKET_OUTGOING {
				continue
			}
			if s, ok := netnsHoldParse(buf[:n]); ok {
				mu.Lock()
				seen = append(seen, s)
				mu.Unlock()
			}
		}
	}()
	var once sync.Once
	collect := func() []netnsWireSegment {
		once.Do(func() {
			close(stop)
			<-done
			_ = unix.Close(fd)
		})
		mu.Lock()
		defer mu.Unlock()
		return append([]netnsWireSegment(nil), seen...)
	}
	t.Cleanup(func() { collect() })
	return collect
}

func netnsHoldServerIn(t *testing.T, pid int, want int) <-chan []byte {
	t.Helper()
	var ln net.Listener
	netnsHoldInside(t, pid, func() error {
		var err error
		ln, err = net.Listen("tcp4", net.JoinHostPort(netnsHoldServer, strconv.Itoa(netnsHoldPort)))
		return err
	})
	got := make(chan []byte, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, err := ln.Accept()
		if err != nil {
			got <- nil
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(10 * time.Second))
		buf := make([]byte, want)
		if _, err := io.ReadFull(c, buf); err != nil {
			got <- nil
			return
		}
		got <- buf
	}()
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
	})
	return got
}

func netnsHoldDial() (net.Conn, error) {
	d := net.Dialer{
		Timeout: 3 * time.Second,
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_MAXSEG, netnsHoldMSS)
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

func TestNetnsAHeldHelloThatMatchesNoSetLeavesInOrder(t *testing.T) {
	netnsHoldRequire(t)
	srv := netnsHoldStartPeer(t)

	buf := &syncBuffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelTrace, true)
	log.StartCapture(buf)
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
	})

	hello := buildClientHello("allowed.example", 700, 0xAB)
	nameAt := bytes.Index(hello, []byte("allowed.example"))
	received := netnsHoldServerIn(t, srv, len(hello))
	wire := netnsHoldCapture(t, srv)

	rule := []string{"-t", "mangle", "OUTPUT", "-o", netnsHoldLink, "-p", "tcp", "--dport", strconv.Itoa(netnsHoldPort), "-j", "NFQUEUE", "--queue-num", strconv.Itoa(netnsHoldQueue)}
	netnsHoldRun(t, append([]string{"iptables", "-w", rule[0], rule[1], "-A"}, rule[2:]...)...)
	t.Cleanup(func() {
		_ = exec.Command("iptables", append([]string{"-w", rule[0], rule[1], "-D"}, rule[2:]...)...).Run()
	})

	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "held", "held", true
	set.Targets.DomainsToMatch = []string{"blocked.example"}
	cfg := config.NewConfig()
	cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
	cfg.Sets = []*config.SetConfig{&set}
	w := newTestWorker(t, &cfg)
	w.qnum = netnsHoldQueue
	if err := w.Start(); err != nil {
		t.Fatalf("bind queue %d: %v", netnsHoldQueue, err)
	}
	t.Cleanup(w.Stop)

	conn, err := netnsHoldDial()
	if err != nil {
		t.Fatalf("dial %s:%d: %v", netnsHoldServer, netnsHoldPort, err)
	}
	defer conn.Close()
	port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(hello); err != nil {
		t.Fatalf("write the ClientHello: %v", err)
	}
	select {
	case got := <-received:
		if !bytes.Equal(got, hello) {
			t.Fatalf("the server read %d bytes that differ from the %d-byte ClientHello", len(got), len(hello))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server never read the whole ClientHello")
	}
	time.Sleep(200 * time.Millisecond)

	var data []netnsWireSegment
	for _, s := range wire() {
		if s.sport == port && s.dport == netnsHoldPort && s.payload > 0 {
			data = append(data, s)
		}
	}
	if len(data) < 2 || data[0].payload >= nameAt {
		t.Fatalf("the ClientHello reached the server as %v; it must leave in two segments with the name in the second, or nothing was held and this test proves nothing", data)
	}
	for i := 1; i < len(data); i++ {
		if int32(data[i].seq-data[i-1].seq) <= 0 {
			t.Fatalf("segments reached the server out of order or twice: %v", data)
		}
	}

	key := fmt.Sprintf(connKeyFormat, netnsHoldRouter, port, netnsHoldServer, netnsHoldPort)
	w.pendingHello.mu.Lock()
	_, wasHeld := w.pendingHello.heldOnce[key]
	w.pendingHello.mu.Unlock()
	if !wasHeld {
		t.Fatalf("segment 1 of %s was never held, so this test proves nothing", key)
	}
	log.Flush()
	settled := regexp.MustCompile(regexp.QuoteMeta("Split ClientHello "+key+": segment 1 held ") + `(\S+), (.+)`).FindStringSubmatch(buf.String())
	if settled == nil {
		t.Fatalf("no settle line for %s in the trace", key)
	}
	if settled[2] != "released ahead of the next packet" {
		t.Fatalf("segment 1 was settled by %q, want the release ahead of segment 2", settled[2])
	}
	t.Logf("segment 1 held %s; on the wire: %v", settled[1], data)
}

func TestNetnsAHelloTheRouterWritesInTwoPartsIsHeldOnlyBriefly(t *testing.T) {
	netnsHoldRequire(t)
	srv := netnsHoldStartPeer(t)
	if err := os.WriteFile("/proc/sys/net/ipv4/tcp_autocorking", []byte("1"), 0o644); err != nil {
		t.Skipf("cannot turn on tcp_autocorking in the namespace: %v", err)
	}

	buf := &syncBuffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelTrace, true)
	log.StartCapture(buf)
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
	})

	hello := buildClientHello("allowed.example", 700, 0xAB)
	received := netnsHoldServerIn(t, srv, len(hello))

	rule := []string{"-t", "mangle", "POSTROUTING", "-o", netnsHoldLink, "-p", "tcp", "--dport", strconv.Itoa(netnsHoldPort), "-j", "NFQUEUE", "--queue-num", strconv.Itoa(netnsHoldQueue)}
	netnsHoldRun(t, append([]string{"iptables", "-w", rule[0], rule[1], "-A"}, rule[2:]...)...)
	t.Cleanup(func() {
		_ = exec.Command("iptables", append([]string{"-w", rule[0], rule[1], "-D"}, rule[2:]...)...).Run()
	})

	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "held", "held", true
	set.Targets.DomainsToMatch = []string{"blocked.example"}
	cfg := config.NewConfig()
	cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
	cfg.Sets = []*config.SetConfig{&set}
	w := newTestWorker(t, &cfg)
	w.qnum = netnsHoldQueue
	if err := w.Start(); err != nil {
		t.Fatalf("bind queue %d: %v", netnsHoldQueue, err)
	}
	t.Cleanup(w.Stop)

	conn, err := netnsHoldDial()
	if err != nil {
		t.Fatalf("dial %s:%d: %v", netnsHoldServer, netnsHoldPort, err)
	}
	defer conn.Close()
	port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	first := netnsHoldMSS - 12
	start := time.Now()
	if _, err := conn.Write(hello[:first]); err != nil {
		t.Fatalf("write the first %d bytes of the ClientHello: %v", first, err)
	}
	if _, err := conn.Write(hello[first:]); err != nil {
		t.Fatalf("write the rest of the ClientHello: %v", err)
	}
	select {
	case got := <-received:
		if !bytes.Equal(got, hello) {
			t.Fatalf("the server read %d bytes that differ from the %d-byte ClientHello", len(got), len(hello))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server never read the whole ClientHello")
	}
	took := time.Since(start)

	key := fmt.Sprintf(connKeyFormat, netnsHoldRouter, port, netnsHoldServer, netnsHoldPort)
	log.Flush()
	settled := regexp.MustCompile(regexp.QuoteMeta("Split ClientHello "+key+": segment 1 held ") + `(\S+), (.+)`).FindStringSubmatch(buf.String())
	if settled == nil {
		t.Fatalf("segment 1 of %s was never held, so this test proves nothing", key)
	}
	if settled[2] != "released, the rest of the record did not arrive" {
		t.Fatalf("segment 1 was settled by %q; the second write must stay corked behind the held segment, or this test proves nothing", settled[2])
	}
	if took >= helloHoldMax/2 {
		t.Fatalf("the server had the ClientHello after %s; a hold on a packet the router sends itself must end well before %s, since the second write waits for the held segment", took, helloHoldMax)
	}
	t.Logf("segment 1 held %s; the server had the ClientHello after %s", settled[1], took)
}
