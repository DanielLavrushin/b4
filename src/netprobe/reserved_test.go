package netprobe

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestRefuseAddrsStopsTheDial(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	refuseLoopback := func(a netip.Addr) bool { return a.IsLoopback() }
	d := RefuseAddrs(Dialer(0, time.Second, 0), refuseLoopback)
	_, err = d.DialContext(context.Background(), "tcp", ln.Addr().String())
	var refused *ReservedAddrError
	if !errors.As(err, &refused) || refused.Addr != "127.0.0.1" {
		t.Fatalf("a loopback destination must be refused before connecting, got %v", err)
	}

	allowed := RefuseAddrs(Dialer(0, time.Second, 0), func(netip.Addr) bool { return false })
	conn, err := allowed.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("an allowed destination connects: %v", err)
	}
	conn.Close()

	if d := RefuseAddrs(Dialer(1, time.Second, 0), nil); d.Control == nil {
		t.Error("a nil predicate keeps the mark control")
	}
}
