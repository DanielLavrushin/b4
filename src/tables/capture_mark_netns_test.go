package tables

import (
	"context"
	"net"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func netnsSendMarkedUDP(t *testing.T, srcPort, dstPort int, mark int) {
	t.Helper()
	d := net.Dialer{
		LocalAddr: &net.UDPAddr{IP: net.ParseIP(dnsqIP), Port: srcPort},
		Control: func(network, address string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, mark)
			}); err != nil {
				return err
			}
			return serr
		},
	}
	conn, err := d.DialContext(context.Background(), "udp4", net.JoinHostPort(dnsqPeerIP, strconv.Itoa(dstPort)))
	if err != nil {
		t.Fatalf("dial marked udp to port %d: %v", dstPort, err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("b4-capture-mark-probe")); err != nil {
		t.Fatalf("send marked udp to port %d: %v", dstPort, err)
	}
}

func TestNetnsAnotherServicesMarkWithTheQueueBitIsQueued(t *testing.T) {
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			netnsRequire(t)
			if engine == backendNFTables && !hasBinary("nft") {
				t.Skip("nft is not installed")
			}
			netnsDNSQueryLinks(t)

			routeEngine = nil
			defer func() { routeEngine = nil }()

			cfg := netnsConfig(engine)
			cfg.Sets = nil
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			defer func() { _ = ClearRules(cfg) }()

			queueMark := int(cfg.Queue.Mark)
			snapshot, stop := netnsConntrackQueueListener(t, uint16(cfg.Queue.StartNum))
			netnsSendMarkedUDP(t, 41451, 443, 0x0ffffaaa)
			netnsSendMarkedUDP(t, 41452, 443, queueMark|0x2bd8)
			netnsSendMarkedUDP(t, 41453, 443, queueMark|0x20000000)
			time.Sleep(300 * time.Millisecond)
			stop()

			hotspot, own := 0, map[int]int{}
			for _, p := range snapshot() {
				switch {
				case p.sport == 41451 && p.dport == 443:
					hotspot++
				case (p.sport == 41452 || p.sport == 41453) && p.dport == 443:
					own[int(p.sport)]++
				}
			}
			if hotspot == 0 {
				t.Errorf("a datagram marked 0x0ffffaaa, as KeeneticOS marks the devices of its hotspot, never reached the queue; it has the queue mark's bit, but it is not one of b4's own packets")
			}
			for sport, n := range own {
				t.Errorf("source port %d: a datagram b4 sends itself was queued %d time(s)", sport, n)
			}
		})
	}
}

func TestNetnsPacketsB4InjectsAreNotQueuedAgain(t *testing.T) {
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			netnsRequire(t)
			if engine == backendNFTables && !hasBinary("nft") {
				t.Skip("nft is not installed")
			}
			netnsDNSQueryLinks(t)

			routeEngine = nil
			defer func() { routeEngine = nil }()

			cfg := netnsConfig(engine)
			cfg.Sets = nil
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			defer func() { _ = ClearRules(cfg) }()

			snapshot, stop := netnsConntrackQueueListener(t, uint16(cfg.Queue.StartNum))
			netnsSendUDP(t, 41443, 443)
			netnsSendMarkedUDP(t, 41444, 443, int(cfg.Queue.Mark))
			netnsSendMarkedUDP(t, 41445, 443, int(netnsInjectorMark(cfg.Queue.Mark)))
			time.Sleep(300 * time.Millisecond)
			stop()

			plain, marked := 0, map[uint8]int{}
			for _, p := range snapshot() {
				switch {
				case p.sport == 41443 && p.dport == 443:
					plain++
				case (p.sport == 41444 || p.sport == 41445) && p.dport == 443:
					marked[p.hook]++
				}
			}
			if plain == 0 {
				t.Fatalf("an unmarked UDP 443 datagram never reached the queue, so this test proves nothing")
			}
			for hook, n := range marked {
				t.Errorf("hook %d: a datagram carrying the queue mark was queued %d time(s); packets b4 injects itself must not wait in its queue again, where on kernels before 4.18 they clash in conntrack with the real packet", hook, n)
			}
		})
	}
}
