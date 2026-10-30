package mtproto

import (
	"crypto/tls"
	"errors"
	"net"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

type helloCounter struct {
	net.Conn
	read atomic.Int64
}

func (c *helloCounter) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.read.Add(int64(n))
	return n, err
}

type seenHello struct {
	size   int64
	curves []tls.CurveID
}

func captureWSHello(t *testing.T) seenHello {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan seenHello, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		hc := &helloCounter{Conn: c}
		srv := tls.Server(hc, &tls.Config{GetConfigForClient: func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
			got <- seenHello{size: hc.read.Load(), curves: append([]tls.CurveID(nil), chi.SupportedCurves...)}
			return nil, errors.New("hello captured")
		}})
		_ = srv.SetDeadline(time.Now().Add(3 * time.Second))
		_ = srv.Handshake()
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	prev := wsDialPort
	wsDialPort = port
	t.Cleanup(func() { wsDialPort = prev })
	if c, err := dialWSEndpoint("127.0.0.1", "kws2.example.co.uk", "kws2.example.co.uk", "/apiws", 3*time.Second, 0); err == nil {
		c.Close()
	}
	<-done
	select {
	case h := <-got:
		return h
	default:
		t.Fatal("the dial sent no ClientHello")
		return seenHello{}
	}
}

func TestTheWebSocketClientHelloFitsInOneSegment(t *testing.T) {
	h := captureWSHello(t)
	if h.size <= 0 || h.size > 1200 {
		t.Fatalf("the ClientHello took %d bytes, want it inside one TCP segment even at the IPv6 minimum MTU", h.size)
	}
	if slices.Contains(h.curves, tls.X25519MLKEM768) {
		t.Fatalf("the ClientHello offers X25519MLKEM768 (groups %v), whose key share pushes it into a second segment", h.curves)
	}
	if !slices.Contains(h.curves, tls.X25519) {
		t.Fatalf("the ClientHello offers groups %v, want X25519 among them", h.curves)
	}
}
