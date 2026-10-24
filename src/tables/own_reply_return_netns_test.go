package tables

import (
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	netnsFirmwareCTMark   = "0xdea8"
	netnsDNSPort          = 53
	netnsCollidingTCPPort = 40444
	netnsCollidingUDPPort = 40453
	netnsOwnSNATPort      = 40443
	netnsOwnSNATAddr      = "203.0.113.3"
)

func netnsUDPEchoIn(t *testing.T, pid, port int) {
	t.Helper()
	var pc net.PacketConn
	netnsInside(t, pid, func() error {
		var err error
		pc, err = net.ListenPacket("udp4", net.JoinHostPort(netnsSrvIP, strconv.Itoa(port)))
		return err
	})
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(buf[:n], from)
		}
	}()
	t.Cleanup(func() { pc.Close() })
}

func netnsMarkedDialer(mark int, local net.Addr) *net.Dialer {
	return &net.Dialer{
		Timeout:   3 * time.Second,
		LocalAddr: local,
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				if mark != 0 {
					serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, mark)
				}
			}); err != nil {
				return err
			}
			return serr
		},
	}
}

func netnsUDPRoundTrip(t *testing.T, conn net.Conn, msg string) uint16 {
	t.Helper()
	defer conn.Close()
	port := uint16(conn.LocalAddr().(*net.UDPAddr).Port)
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("send %q: %v", msg, err)
	}
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != msg {
		t.Fatalf("the answer to %q never came back (%q, %v)", msg, buf[:n], err)
	}
	return port
}

func netnsTCPRoundTrip(t *testing.T, conn net.Conn, msg string) uint16 {
	t.Helper()
	defer conn.Close()
	port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("write %q: %v", msg, err)
	}
	if _, err := io.ReadFull(conn, make([]byte, len(msg))); err != nil {
		t.Fatalf("read the echo of %q: %v", msg, err)
	}
	return port
}

func netnsIptInsert(t *testing.T, table, op, chain string, spec ...string) {
	t.Helper()
	args := []string{"iptables", "-w", "-t", table, op, chain}
	if op == "-I" {
		args = append(args, "1")
	}
	netnsRun(t, append(args, spec...)...)
	t.Cleanup(func() { _, _ = run(append([]string{"iptables", "-w", "-t", table, "-D", chain}, spec...)...) })
}

func netnsIptPackets(t *testing.T, table, chain, contains string) int {
	t.Helper()
	for _, line := range strings.Split(netnsRun(t, "iptables", "-w", "-t", table, "-nvxL", chain), "\n") {
		if f := strings.Fields(line); len(f) > 2 && strings.Contains(line, contains) {
			if n, err := strconv.Atoi(f[0]); err == nil {
				return n
			}
		}
	}
	return -1
}

func TestNetnsRepliesReachTheQueueWhenAnotherProgramMarksTheConnectionWithTheQueueBit(t *testing.T) {
	for i, engineName := range []string{backendIPTables, backendNFTables} {
		t.Run(engineName, func(t *testing.T) {
			netnsRequire(t)
			if engineName == backendNFTables && !hasBinary("nft") {
				t.Skip("nft is not installed")
			}
			t.Cleanup(setDSCPKeepSysctl("net/ipv4/ip_forward"))
			if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0o644); err != nil {
				t.Skipf("cannot turn forwarding on in this namespace: %v", err)
			}
			collidingTCP, collidingUDP, ownSNAT := netnsCollidingTCPPort+10*i, netnsCollidingUDPPort+10*i, netnsOwnSNATPort+10*i
			dev := netnsStartDevice(t)
			srv := netnsStartPeer(t, netnsSrvLink, netnsSrvPeer, netnsSrvRouter, netnsSrvIP)
			netnsEchoServerIn(t, srv)
			netnsUDPEchoIn(t, srv, netnsDNSPort)
			netnsRun(t, "ip", "addr", "add", netnsOwnSNATAddr+"/24", "dev", netnsSrvLink)
			netnsIptInsert(t, "nat", "-A", "POSTROUTING", "-s", netnsDevIP, "-o", netnsSrvLink, "-j", "MASQUERADE")
			netnsIptInsert(t, "nat", "-A", "POSTROUTING", "-o", netnsSrvLink, "-p", "tcp", "--sport", strconv.Itoa(ownSNAT), "-j", "SNAT", "--to-source", netnsOwnSNATAddr)

			routeEngine = nil
			defer func() { routeEngine = nil }()
			cfg := netnsConfig(engineName)
			cfg.Sets = nil
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			defer func() { _ = ClearRules(cfg) }()

			netnsIptInsert(t, "mangle", "-I", "PREROUTING", "-m", "conntrack", "--ctorigsrc", netnsDevIP, "--ctdir", "REPLY", "-m", "connmark", "--mark", "0x8000/0x8000")
			netnsIptInsert(t, "mangle", "-I", "PREROUTING", "-s", netnsDevIP, "-j", "CONNMARK", "--set-xmark", netnsFirmwareCTMark+"/0xffffffff")

			tcpListener, err := net.Listen("tcp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(collidingTCP)))
			if err != nil {
				t.Fatalf("listen on every address: %v", err)
			}
			defer tcpListener.Close()
			udpSocket, err := net.ListenPacket("udp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(collidingUDP)))
			if err != nil {
				t.Fatalf("bind a UDP socket to every address: %v", err)
			}
			defer udpSocket.Close()

			snapshot, stop := netnsPacketQueueListener(t, uint16(cfg.Queue.StartNum))
			server := net.JoinHostPort(netnsSrvIP, strconv.Itoa(netnsSrvPort))
			resolver := net.JoinHostPort(netnsSrvIP, strconv.Itoa(netnsDNSPort))
			var devConns [4]net.Conn
			netnsInside(t, dev.pid, func() error {
				var err error
				for i, d := range []struct {
					network, addr string
					local         net.Addr
				}{
					{"tcp4", server, nil},
					{"tcp4", server, &net.TCPAddr{Port: collidingTCP}},
					{"udp4", resolver, nil},
					{"udp4", resolver, &net.UDPAddr{Port: collidingUDP}},
				} {
					if devConns[i], err = netnsMarkedDialer(0, d.local).Dial(d.network, d.addr); err != nil {
						return err
					}
				}
				return nil
			})
			devTCP := netnsTCPRoundTrip(t, devConns[0], "b4-device-tcp")
			devTCPColliding := netnsTCPRoundTrip(t, devConns[1], "b4-device-tcp-colliding")
			devUDP := netnsUDPRoundTrip(t, devConns[2], "b4-device-dns")
			devUDPColliding := netnsUDPRoundTrip(t, devConns[3], "b4-device-dns-colliding")

			mark := int(cfg.Queue.Mark)
			dial := func(network, addr string, local net.Addr) net.Conn {
				t.Helper()
				c, err := netnsMarkedDialer(mark, local).Dial(network, addr)
				if err != nil {
					t.Fatalf("dial %s %s with the queue mark: %v", network, addr, err)
				}
				return c
			}
			ownTCP := netnsTCPRoundTrip(t, dial("tcp4", server, nil), "b4-own-tcp")
			ownTCPSNAT := netnsTCPRoundTrip(t, dial("tcp4", server, &net.TCPAddr{IP: net.ParseIP(netnsSrvRouter), Port: ownSNAT}), "b4-own-tcp-snat")
			ownUDP := netnsUDPRoundTrip(t, dial("udp4", resolver, nil), "b4-own-dns")
			time.Sleep(300 * time.Millisecond)
			stop()

			if n := netnsIptPackets(t, "mangle", "PREROUTING", "ctorigsrc "+netnsDevIP); n <= 0 {
				t.Fatalf("no reply toward the device carried the queue bit in its connection mark (%d), so this test proves nothing", n)
			}
			if n := netnsIptPackets(t, "nat", "POSTROUTING", "to:"+netnsOwnSNATAddr); n <= 0 {
				t.Fatalf("the source of b4's own connection on port %d was never rewritten (%d), so this test proves nothing about the address lookup after NAT", ownSNAT, n)
			}

			synacks, answers := map[uint16]int{}, map[uint16]int{}
			for _, q := range snapshot() {
				s, ok := netnsParseIPv4(q.pkt)
				if !ok || q.hook != netnsHookPrerouting {
					continue
				}
				switch {
				case s.proto == unix.IPPROTO_TCP && s.sport == netnsSrvPort && s.flags&(netnsTCPFlagSYN|netnsTCPFlagACK) == netnsTCPFlagSYN|netnsTCPFlagACK:
					synacks[s.dport]++
				case s.proto == unix.IPPROTO_UDP && s.sport == netnsDNSPort:
					answers[s.dport]++
				}
			}
			for _, c := range []struct {
				what   string
				got    int
				queued bool
			}{
				{"the SYN-ACK of the device's connection", synacks[devTCP], true},
				{"the SYN-ACK of the device's connection from a port the router listens on for every address", synacks[devTCPColliding], true},
				{"the DNS answer to the device", answers[devUDP], true},
				{"the DNS answer to the device on a port a router socket binds for every address", answers[devUDPColliding], true},
				{"the SYN-ACK of b4's own connection", synacks[ownTCP], false},
				{"the SYN-ACK of b4's own connection whose source NAT rewrote", synacks[ownTCPSNAT], false},
				{"the DNS answer to b4's own lookup", answers[ownUDP], false},
			} {
				switch {
				case c.queued && c.got == 0:
					t.Errorf("%s never reached the queue: a connection mark another program writes with the queue bit hides the replies of device connections from b4", c.what)
				case !c.queued && c.got != 0:
					t.Errorf("%s reached the queue %d time(s): the replies to b4's own lookups and probes must skip it", c.what, c.got)
				}
			}
			t.Logf("queued SYN-ACKs %v, DNS answers %v (device %d, %d, %d, %d; own %d, %d, %d)", synacks, answers, devTCP, devTCPColliding, devUDP, devUDPColliding, ownTCP, ownTCPSNAT, ownUDP)
		})
	}
}
