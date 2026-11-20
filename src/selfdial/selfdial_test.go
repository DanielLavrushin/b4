package selfdial

import (
	"net"
	"net/netip"
	"testing"
	"time"
)

func reset(t *testing.T) {
	t.Helper()
	mu.Lock()
	flows = make(map[flow]time.Time)
	live.Store(0)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		flows = make(map[flow]time.Time)
		live.Store(0)
		mu.Unlock()
	})
}

func TestAConnectionDialledWithTheControlIsKnownBeforeTheServerAcceptsIt(t *testing.T) {
	for _, c := range []struct {
		network, listen string
	}{
		{"tcp4", "127.0.0.1:0"},
		{"tcp6", "[::1]:0"},
	} {
		t.Run(c.network, func(t *testing.T) {
			reset(t)
			ln, err := net.Listen(c.network, c.listen)
			if err != nil {
				t.Skipf("no %s loopback: %v", c.network, err)
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
				atAccept <- Holds(from.Port(), server)
				peer <- from.Port()
			}()

			d := net.Dialer{Timeout: 3 * time.Second, Control: Control(0)}
			conn, err := d.Dial(c.network, ln.Addr().String())
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer conn.Close()
			local := netip.MustParseAddrPort(conn.LocalAddr().String())

			if !<-atAccept {
				t.Fatal("the connection was not known when the server accepted it, so the first packets of b4's own connection would pass unrecognised")
			}
			if got := <-peer; got != local.Port() {
				t.Fatalf("the server saw port %d, the dialer reports %d: the port noted before connecting is not the one the connection uses", got, local.Port())
			}
			if Holds(local.Port()+1, server) {
				t.Fatal("another port reads as b4's own connection")
			}
		})
	}
}

func TestAPlainDialIsNotKnown(t *testing.T) {
	reset(t)
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if conn, err := ln.Accept(); err == nil {
			conn.Close()
		}
	}()
	conn, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	local := netip.MustParseAddrPort(conn.LocalAddr().String())
	if Holds(local.Port(), netip.MustParseAddrPort(ln.Addr().String())) {
		t.Fatal("a connection dialled without the control reads as b4's own")
	}
}

func TestAKnownConnectionLastsWhileItsPacketsKeepComing(t *testing.T) {
	reset(t)
	remote := netip.MustParseAddrPort("149.154.167.51:443")
	start := time.Now()
	noteAt(40000, remote, start)
	if !holdsAt(40000, remote, start.Add(flowTTL-time.Second)) {
		t.Fatal("forgotten before its time")
	}
	if !holdsAt(40000, remote, start.Add(2*flowTTL-2*time.Second)) {
		t.Fatal("a packet seen within the window did not extend it, so a long connection is forgotten while it still sends")
	}
	if holdsAt(40000, remote, start.Add(4*flowTTL)) {
		t.Fatal("kept after a whole window without a packet")
	}
	if live.Load() != 0 {
		t.Fatalf("%d entries counted after the only one expired", live.Load())
	}
}

func TestAMappedRemoteMatchesItsPlainAddress(t *testing.T) {
	reset(t)
	noteAt(40001, netip.MustParseAddrPort("[::ffff:149.154.167.51]:443"), time.Now())
	if !Holds(40001, netip.MustParseAddrPort("149.154.167.51:443")) {
		t.Fatal("a connection noted with an IPv4-mapped address is not found by the packet's plain IPv4 address")
	}
}

func TestTheRegistryStaysBounded(t *testing.T) {
	reset(t)
	now := time.Now()
	for i := 0; i < flowsMax+50; i++ {
		noteAt(uint16(1+i%60000), netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), 443), now)
	}
	mu.Lock()
	n := len(flows)
	mu.Unlock()
	if n > flowsMax {
		t.Fatalf("%d entries, cap %d", n, flowsMax)
	}
}
