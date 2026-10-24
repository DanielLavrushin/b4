package tables

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/florianl/go-nfqueue"
	"golang.org/x/sys/unix"
)

const (
	netnsSrvLink   = "b4qs0"
	netnsSrvPeer   = "b4qs0p"
	netnsSrvRouter = "203.0.113.1"
	netnsSrvIP     = "203.0.113.2"
	netnsSrvPort   = 443

	netnsClampSize       = 600
	netnsTimestampOption = 12
	netnsTCPFlagSYN      = 0x02
	netnsTCPFlagACK      = 0x10
)

type netnsQueued struct {
	hook uint8
	pkt  []byte
}

type netnsSegment struct {
	proto        uint8
	id           uint16
	sport, dport uint16
	seq, ack     uint32
	flags        uint8
	mss          int
	payload      []byte
}

func netnsPacketQueueListener(t *testing.T, qnum uint16) (func() []netnsQueued, func()) {
	t.Helper()
	nf, err := nfqueue.Open(&nfqueue.Config{
		NfQueue:      qnum,
		MaxPacketLen: 0xffff,
		MaxQueueLen:  1024,
		Copymode:     nfqueue.NfQnlCopyPacket,
	})
	if err != nil {
		t.Fatalf("bind queue %d: %v", qnum, err)
	}
	var mu sync.Mutex
	var seen []netnsQueued
	ctx, cancel := context.WithCancel(context.Background())
	err = nf.RegisterWithErrorFunc(ctx,
		func(a nfqueue.Attribute) int {
			if a.Payload != nil && a.Hook != nil {
				mu.Lock()
				seen = append(seen, netnsQueued{hook: *a.Hook, pkt: append([]byte(nil), *a.Payload...)})
				mu.Unlock()
			}
			if a.PacketID != nil {
				_ = nf.SetVerdict(*a.PacketID, nfqueue.NfAccept)
			}
			return 0
		},
		func(error) int { return 0 },
	)
	if err != nil {
		cancel()
		nf.Close()
		t.Fatalf("register queue %d: %v", qnum, err)
	}
	snapshot := func() []netnsQueued {
		mu.Lock()
		defer mu.Unlock()
		return append([]netnsQueued(nil), seen...)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			nf.Close()
		})
	}
	t.Cleanup(stop)
	return snapshot, stop
}

func netnsParseIPv4(pkt []byte) (netnsSegment, bool) {
	s := netnsSegment{mss: -1}
	if len(pkt) < 20 || pkt[0]>>4 != 4 {
		return s, false
	}
	ihl := int(pkt[0]&0x0f) * 4
	end := min(int(binary.BigEndian.Uint16(pkt[2:4])), len(pkt))
	if ihl < 20 || ihl > end {
		return s, false
	}
	s.proto, s.id = pkt[9], binary.BigEndian.Uint16(pkt[4:6])
	l4 := pkt[ihl:end]
	switch s.proto {
	case unix.IPPROTO_TCP:
		if len(l4) < 20 {
			return s, false
		}
		off := int(l4[12]>>4) * 4
		if off < 20 || off > len(l4) {
			return s, false
		}
		s.sport, s.dport = binary.BigEndian.Uint16(l4[0:2]), binary.BigEndian.Uint16(l4[2:4])
		s.seq, s.ack, s.flags = binary.BigEndian.Uint32(l4[4:8]), binary.BigEndian.Uint32(l4[8:12]), l4[13]
		opts := l4[20:off]
		for i := 0; i < len(opts); {
			if opts[i] == 0 {
				break
			}
			if opts[i] == 1 {
				i++
				continue
			}
			if i+1 >= len(opts) || opts[i+1] < 2 {
				break
			}
			if opts[i] == 2 && opts[i+1] == 4 && i+4 <= len(opts) {
				s.mss = int(binary.BigEndian.Uint16(opts[i+2 : i+4]))
			}
			i += int(opts[i+1])
		}
		s.payload = l4[off:]
	case unix.IPPROTO_UDP:
		if len(l4) < 8 {
			return s, false
		}
		s.sport, s.dport = binary.BigEndian.Uint16(l4[0:2]), binary.BigEndian.Uint16(l4[2:4])
		s.payload = l4[8:]
	default:
		return s, false
	}
	return s, true
}

func netnsStartPeer(t *testing.T, link, peer, routerAddr, peerAddr string) int {
	t.Helper()
	for _, bin := range []string{"unshare", "nsenter"} {
		if !hasBinary(bin) {
			t.Skipf("%s is not installed", bin)
		}
	}
	_, _ = run("ip", "link", "del", link)
	netnsRun(t, "ip", "link", "add", link, "type", "veth", "peer", "name", peer)
	t.Cleanup(func() { _, _ = run("ip", "link", "del", link) })
	netnsRun(t, "ip", "link", "set", link, "up")
	netnsRun(t, "ip", "addr", "add", routerAddr+"/24", "dev", link)

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
			t.Skipf("the child never entered its own network namespace")
		}
		time.Sleep(20 * time.Millisecond)
	}

	netnsRun(t, "ip", "link", "set", peer, "netns", strconv.Itoa(pid))
	in := func(args ...string) {
		netnsRun(t, append([]string{"nsenter", "-t", strconv.Itoa(pid), "-n"}, args...)...)
	}
	in("ip", "link", "set", "lo", "up")
	in("ip", "link", "set", peer, "up")
	in("ip", "addr", "add", peerAddr+"/24", "dev", peer)
	in("ip", "route", "add", "default", "via", routerAddr)
	return pid
}

func netnsInside(t *testing.T, pid int, fn func() error) {
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

func netnsTCPMaxSeg(c net.Conn) (int, error) {
	sc, ok := c.(syscall.Conn)
	if !ok {
		return 0, errors.New("the connection has no socket")
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var mss int
	var serr error
	if err := raw.Control(func(fd uintptr) {
		mss, serr = unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_MAXSEG)
	}); err != nil {
		return 0, err
	}
	return mss, serr
}

func netnsEchoServerIn(t *testing.T, pid int) func() map[string]int {
	t.Helper()
	var ln net.Listener
	netnsInside(t, pid, func() error {
		var err error
		ln, err = net.Listen("tcp4", net.JoinHostPort(netnsSrvIP, strconv.Itoa(netnsSrvPort)))
		return err
	})
	var mu sync.Mutex
	var wg sync.WaitGroup
	mss := map[string]int{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				if v, err := netnsTCPMaxSeg(c); err == nil {
					host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
					mu.Lock()
					mss[host] = v
					mu.Unlock()
				}
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				buf := make([]byte, 256)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				_, _ = c.Write(buf[:n])
			}(c)
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
	})
	return func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		out := make(map[string]int, len(mss))
		for k, v := range mss {
			out[k] = v
		}
		return out
	}
}

func netnsEchoRoundTrip(t *testing.T, dial func() (net.Conn, error), msg string) (port uint16, mss int) {
	t.Helper()
	conn, err := dial()
	if err != nil {
		t.Fatalf("dial %s:%d: %v", netnsSrvIP, netnsSrvPort, err)
	}
	defer conn.Close()
	port = uint16(conn.LocalAddr().(*net.TCPAddr).Port)
	if mss, err = netnsTCPMaxSeg(conn); err != nil {
		t.Fatalf("read TCP_MAXSEG: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read the echo: %v", err)
	}
	return port, mss
}

func netnsDialServer() (net.Conn, error) {
	d := net.Dialer{Timeout: 3 * time.Second}
	return d.Dial("tcp4", net.JoinHostPort(netnsSrvIP, strconv.Itoa(netnsSrvPort)))
}

func TestNetnsTheRouterOwnPacketsAreQueuedOnce(t *testing.T) {
	for _, engine := range []string{backendIPTables, backendNFTables} {
		for _, l := range captureLayouts {
			t.Run(engine+"/"+l.name, func(t *testing.T) {
				netnsRequire(t)
				if engine == backendNFTables && !hasBinary("nft") {
					t.Skip("nft is not installed")
				}
				srv := netnsStartPeer(t, netnsSrvLink, netnsSrvPeer, netnsSrvRouter, netnsSrvIP)
				netnsEchoServerIn(t, srv)

				routeEngine = nil
				defer func() { routeEngine = nil }()

				cfg := netnsConfig(engine)
				cfg.Sets = nil
				l.apply(cfg, "AA:BB:CC:DD:EE:01", "192.0.2.77")
				if err := AddRules(cfg); err != nil {
					t.Fatalf("AddRules: %v", err)
				}
				defer func() { _ = ClearRules(cfg) }()
				if !(&Monitor{}).checkRules(cfg) {
					t.Errorf("the monitor finds rules missing right after they were installed, so it rebuilds them on every check")
				}

				snapshot, stop := netnsPacketQueueListener(t, uint16(cfg.Queue.StartNum))
				clientPort, _ := netnsEchoRoundTrip(t, netnsDialServer, "b4-queued-once")
				udp, err := net.ListenUDP("udp4", &net.UDPAddr{})
				if err != nil {
					t.Fatalf("open udp: %v", err)
				}
				udpPort := uint16(udp.LocalAddr().(*net.UDPAddr).Port)
				for i := range 3 {
					if _, err := udp.WriteToUDP([]byte(fmt.Sprintf("b4-once-%d", i)), &net.UDPAddr{IP: net.ParseIP(netnsSrvIP), Port: netnsSrvPort}); err != nil {
						t.Fatalf("send udp: %v", err)
					}
				}
				udp.Close()
				time.Sleep(300 * time.Millisecond)
				stop()

				want := uint8(netnsHookPostrouting)
				if l.devices {
					want = netnsHookOutput
				}
				hooks := map[string][]uint8{}
				tcp, datagrams := 0, 0
				for _, q := range snapshot() {
					s, ok := netnsParseIPv4(q.pkt)
					if !ok || s.dport != netnsSrvPort {
						continue
					}
					var key string
					switch {
					case s.proto == unix.IPPROTO_TCP && s.sport == clientPort:
						key = fmt.Sprintf("TCP id %d seq %d ack %d flags %#02x len %d", s.id, s.seq, s.ack, s.flags, len(s.payload))
						if len(hooks[key]) == 0 {
							tcp++
						}
					case s.proto == unix.IPPROTO_UDP && s.sport == udpPort:
						key = "UDP " + string(s.payload)
						if len(hooks[key]) == 0 {
							datagrams++
						}
					default:
						continue
					}
					hooks[key] = append(hooks[key], q.hook)
				}
				for key, at := range hooks {
					if len(at) != 1 || at[0] != want {
						t.Errorf("%s from the router itself was queued at hooks %v; it must be queued once, at hook %d, or b4 processes, logs and counts it twice", key, at, want)
					}
				}
				if tcp < 3 || datagrams != 3 {
					t.Fatalf("%d TCP and %d UDP packets of the router's own connections reached the queue; the handshake, the data and all 3 datagrams must, or this test proves nothing", tcp, datagrams)
				}
				t.Logf("queued once each at hook %d: %d TCP packets, %d datagrams", want, tcp, datagrams)
			})
		}
	}
}

func TestNetnsTheMSSClampReachesTheHandshakeBeforeTheQueue(t *testing.T) {
	for _, engine := range []string{backendIPTables, backendNFTables} {
		for _, l := range captureLayouts {
			t.Run(engine+"/"+l.name, func(t *testing.T) {
				netnsRequire(t)
				if engine == backendNFTables && !hasBinary("nft") {
					t.Skip("nft is not installed")
				}
				t.Cleanup(setDSCPKeepSysctl("net/ipv4/ip_forward"))
				if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0o644); err != nil {
					t.Skipf("cannot turn forwarding on in this namespace: %v", err)
				}
				dev := netnsStartDevice(t)
				srv := netnsStartPeer(t, netnsSrvLink, netnsSrvPeer, netnsSrvRouter, netnsSrvIP)
				serverMSS := netnsEchoServerIn(t, srv)

				routeEngine = nil
				defer func() { routeEngine = nil }()

				cfg := netnsConfig(engine)
				cfg.Sets = nil
				cfg.Queue.MSSClamp.Enabled, cfg.Queue.MSSClamp.Size = true, netnsClampSize
				l.apply(cfg, dev.mac, netnsDevIP)
				if err := AddRules(cfg); err != nil {
					t.Fatalf("AddRules: %v", err)
				}
				defer func() { _ = ClearRules(cfg) }()

				snapshot, stop := netnsPacketQueueListener(t, uint16(cfg.Queue.StartNum))
				var forwarded net.Conn
				netnsInside(t, dev.pid, func() error {
					var err error
					forwarded, err = netnsDialServer()
					return err
				})
				forwardedPort, forwardedMSS := netnsEchoRoundTrip(t, func() (net.Conn, error) { return forwarded, nil }, "b4-forwarded")
				localPort, localMSS := netnsEchoRoundTrip(t, netnsDialServer, "b4-router")
				time.Sleep(300 * time.Millisecond)
				stop()

				limit := netnsClampSize - netnsTimestampOption
				atServer := serverMSS()
				for _, c := range []struct {
					what string
					mss  int
					ok   bool
				}{
					{"the forwarded client's MSS, set by the SYN-ACK", forwardedMSS, true},
					{"the server's MSS toward the forwarded client, set by the SYN", atServer[netnsDevIP], atServer[netnsDevIP] > 0},
					{"the router's own MSS, set by the SYN-ACK", localMSS, true},
					{"the server's MSS toward the router, set by the SYN", atServer[netnsSrvRouter], atServer[netnsSrvRouter] > 0},
				} {
					switch {
					case !c.ok:
						t.Errorf("%s: the server never accepted that connection, so it has no value", c.what)
					case c.mss > limit:
						t.Errorf("%s is %d with a global MSS clamp of %d, want at most %d: the clamp missed that handshake packet", c.what, c.mss, netnsClampSize, limit)
					}
				}

				synacks := 0
				for _, q := range snapshot() {
					s, ok := netnsParseIPv4(q.pkt)
					if !ok || s.proto != unix.IPPROTO_TCP || s.flags&netnsTCPFlagSYN == 0 {
						continue
					}
					flow := (s.dport == netnsSrvPort && (s.sport == forwardedPort || s.sport == localPort)) ||
						(s.sport == netnsSrvPort && (s.dport == forwardedPort || s.dport == localPort))
					if !flow {
						continue
					}
					if s.flags&netnsTCPFlagACK != 0 {
						synacks++
					}
					if s.mss < 0 || s.mss > netnsClampSize {
						t.Errorf("hook %d: b4 was handed a handshake packet %d -> %d with MSS %d, above the clamp of %d: the queue rule took it before the clamp did", q.hook, s.sport, s.dport, s.mss, netnsClampSize)
					}
				}
				if synacks == 0 {
					t.Fatalf("no SYN-ACK of the two connections reached the queue, so this test proves nothing about the order of the clamp and the queue rules")
				}
				t.Logf("MSS: forwarded client %d, router %d, server %v; %d SYN-ACKs queued", forwardedMSS, localMSS, atServer, synacks)
			})
		}
	}
}
