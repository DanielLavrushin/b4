package warmtuple

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

func acceptedPeers(t *testing.T, ln net.Listener) <-chan netip.AddrPort {
	t.Helper()
	peers := make(chan netip.AddrPort, 4)
	go func() {
		defer close(peers)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			peers <- c.RemoteAddr().(*net.TCPAddr).AddrPort()
			c.Close()
		}
	}()
	return peers
}

func TestDirectDialRegistersTupleBeforeConnect(t *testing.T) {
	for _, family := range []struct{ network, address string }{{"tcp4", "127.0.0.1"}, {"tcp6", "::1"}} {
		t.Run(family.network, func(t *testing.T) {
			ln, err := net.Listen(family.network, net.JoinHostPort(family.address, "0"))
			if err != nil {
				t.Skipf("no %s loopback: %v", family.network, err)
			}
			peers := acceptedPeers(t, ln)
			target := ln.Addr().(*net.TCPAddr).AddrPort()
			source := netip.MustParseAddr(family.address)
			var registered netip.AddrPort
			releases := 0
			c, release, err := dialDirect(context.Background(), source, target, 0, func(local netip.AddrPort) (func(), error) {
				select {
				case peer := <-peers:
					t.Errorf("the listener accepted %s before the tuple was registered", peer)
				default:
				}
				registered = local
				return func() { releases++ }, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := c.LocalAddr().(*net.TCPAddr).AddrPort(); got != registered || directPortReserved(got.Port()) {
				t.Fatalf("registered %s, connected from %s", registered, got)
			}
			select {
			case peer := <-peers:
				if peer != registered {
					t.Fatalf("registered %s, the server saw %s", registered, peer)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("the server saw no connection")
			}
			c.Close()
			release()
			if releases != 1 {
				t.Fatalf("%d releases", releases)
			}
			ln.Close()
			for range peers {
			}
		})
	}
}

func TestDirectDialRefusesWithoutRegistration(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	peers := acceptedPeers(t, ln)
	refused := errors.New("warm flow is already registered or registry is full")
	c, release, err := dialDirect(context.Background(), netip.MustParseAddr("127.0.0.1"), ln.Addr().(*net.TCPAddr).AddrPort(), 0, func(netip.AddrPort) (func(), error) {
		return nil, refused
	})
	if !errors.Is(err, refused) || c != nil || release != nil {
		t.Fatalf("a refused registration gave %v, %v", c, err)
	}
	select {
	case peer := <-peers:
		t.Fatalf("the server accepted %s although the registration was refused", peer)
	case <-time.After(200 * time.Millisecond):
	}
	ln.Close()
	for range peers {
	}
	releases := 0
	c, release, err = dialDirect(context.Background(), netip.MustParseAddr("127.0.0.1"), netip.MustParseAddrPort("127.0.0.1:1"), 0, func(netip.AddrPort) (func(), error) {
		return func() { releases++ }, nil
	})
	if err == nil || c != nil || release != nil || releases != 1 {
		t.Fatalf("a refused connect gave %v, %v with %d releases, want an error and exactly one release", c, err, releases)
	}
}

func TestDirectDialSkipsWarmTuplePorts(t *testing.T) {
	if !directPortReserved(firstPort) || !directPortReserved(lastPort) || directPortReserved(firstPort-1) || directPortReserved(lastPort+1) {
		t.Fatal("the fallback does not keep away from exactly the warm-tuple ports")
	}
	saved := directPortReserved
	t.Cleanup(func() { directPortReserved = saved })
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	peers := acceptedPeers(t, ln)
	defer func() {
		ln.Close()
		for range peers {
		}
	}()
	target := ln.Addr().(*net.TCPAddr).AddrPort()
	var picked []uint16
	directPortReserved = func(port uint16) bool {
		picked = append(picked, port)
		return len(picked) <= 3
	}
	var registered []netip.AddrPort
	register := func(local netip.AddrPort) (func(), error) {
		registered = append(registered, local)
		return func() {}, nil
	}
	c, release, err := dialDirect(context.Background(), netip.MustParseAddr("127.0.0.1"), target, 0, register)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	release()
	if len(picked) != 4 || len(registered) != 1 || registered[0].Port() != picked[3] {
		t.Fatalf("picked %v and registered %v, want three reserved ports skipped and only the fourth registered", picked, registered)
	}
	picked, registered = nil, nil
	directPortReserved = func(port uint16) bool {
		picked = append(picked, port)
		return true
	}
	if c, _, err := dialDirect(context.Background(), netip.MustParseAddr("127.0.0.1"), target, 0, register); !errors.Is(err, errWarmRangePort) || c != nil {
		t.Fatalf("with only reserved ports the dial gave %v, %v", c, err)
	}
	if len(picked) != directBindTries || len(registered) != 0 {
		t.Fatalf("%d binds and %d registrations, want %d binds and none registered", len(picked), len(registered), directBindTries)
	}
}
