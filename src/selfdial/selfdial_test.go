package selfdial

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"
)

func reset(t *testing.T) {
	t.Helper()
	mu.Lock()
	flows = make(map[flow]entry)
	live.Store(0)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		flows = make(map[flow]entry)
		live.Store(0)
		mu.Unlock()
	})
}

func TestAConnectionDialledForTheTelegramSetIsKnownBeforeTheServerAcceptsIt(t *testing.T) {
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

			conn, err := Dial(context.Background(), c.network, ln.Addr().String(), 0, 3*time.Second)
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
		t.Fatal("a connection dialled without selfdial reads as b4's own")
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

func registered() int {
	mu.Lock()
	defer mu.Unlock()
	return len(flows)
}

func TestAFailedDialLeavesNoRegistrationBehind(t *testing.T) {
	reset(t)
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := ln.Addr().String()
	ln.Close()

	conn, err := Dial(context.Background(), "tcp4", refused, 0, 3*time.Second)
	if err == nil {
		conn.Close()
		t.Fatalf("a dial to the closed port %s connected", refused)
	}
	if n := registered(); n != 0 {
		t.Fatalf("%d registration(s) left after the dial failed: the kernel can hand the freed port to another connection to the same address, which would then read as b4's own", n)
	}
}

func TestASuccessfulDialKeepsOnlyItsOwnRegistration(t *testing.T) {
	reset(t)
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if conn, err := ln.Accept(); err == nil {
			defer conn.Close()
			time.Sleep(100 * time.Millisecond)
		}
	}()
	conn, err := Dial(context.Background(), "tcp4", ln.Addr().String(), 0, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	local := netip.MustParseAddrPort(conn.LocalAddr().String())
	if !Holds(local.Port(), netip.MustParseAddrPort(ln.Addr().String())) || registered() != 1 {
		t.Fatalf("after a successful dial %d registration(s), want exactly the connection's own", registered())
	}
}

func TestAnOlderDialsReleaseLeavesANewerRegistrationAlone(t *testing.T) {
	reset(t)
	remote := netip.MustParseAddrPort("149.154.167.51:443")
	now := time.Now()
	older := noteAt(40010, remote, now)
	newer := noteAt(40010, remote, now)
	release(keyOf(40010, remote), older)
	if !holdsAt(40010, remote, now) {
		t.Fatal("the release of a dial that failed removed the registration a later dial on the same port made")
	}
	release(keyOf(40010, remote), newer)
	if holdsAt(40010, remote, now) {
		t.Fatal("a dial's own release did not remove its registration")
	}
}

func TestAnAttemptThatRegistersAfterTheDialEndedIsReleased(t *testing.T) {
	reset(t)
	remote := netip.MustParseAddrPort("149.154.167.51:443")
	tr := &tracker{}
	tr.finish(nil)
	tr.record(keyOf(40011, remote), noteAt(40011, remote, time.Now()))
	if registered() != 0 {
		t.Fatal("an attempt that registered its port after the dial had already ended stayed registered")
	}
}

func TestANewConnectionOnARegisteredTupleEndsTheRegistration(t *testing.T) {
	reset(t)
	remote := netip.MustParseAddrPort("149.154.167.51:443")
	now := time.Now()
	noteAt(40012, remote, now)
	for _, c := range []struct {
		name string
		syn  bool
		seq  uint32
		want bool
	}{
		{"b4's SYN", true, 1000, true},
		{"b4's SYN sent again", true, 1000, true},
		{"a later segment of b4's connection", false, 1001, true},
		{"the SYN of another connection on the same port and address", true, 5000, false},
		{"a later segment of that connection", false, 5001, false},
	} {
		if got := segmentAt(40012, remote, c.syn, c.seq, now); got != c.want {
			t.Fatalf("%s: known %v, want %v", c.name, got, c.want)
		}
	}
	noteAt(40012, remote, now)
	if !segmentAt(40012, remote, true, 9000, now) {
		t.Fatal("a new dial from the same port is not known by its own SYN")
	}
}
