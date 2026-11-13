package iphealth

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const testBannedHost = "rr2---sn-ab5l6nr6.googlevideo.com"

func TestProbeTLSHostAsksForTheBannedName(t *testing.T) {
	var asked atomic.Value
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.TLS = &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			asked.Store(hello.ServerName)
			return nil, nil
		},
	}
	srv.StartTLS()
	defer srv.Close()

	ip, port := probeAddr(t, srv.Listener.Addr())
	if got := probeTLSHost(context.Background(), ip, port, testBannedHost, testProbeConnect, testProbeHandshake*10, nil); got != TLSReplied {
		t.Fatalf("a server that completes the handshake for the banned name: %s, want replied", got)
	}
	if name, _ := asked.Load().(string); name != testBannedHost {
		t.Errorf("the ClientHello named %q, want the name the ban was made for", name)
	}
}

func TestProbeTLSHostReportsItsSourcePortBeforeItConnects(t *testing.T) {
	ln := listenLocal(t)
	arrived := make(chan int, 1)
	var reported atomic.Int32
	serveRaw(t, ln, func(c net.Conn) {
		known := int(reported.Load())
		if from, ok := c.RemoteAddr().(*net.TCPAddr); ok && known == from.Port {
			arrived <- from.Port
		} else {
			arrived <- -1
		}
		c.Close()
	})

	ip, port := probeAddr(t, ln.Addr())
	probeTLSHost(context.Background(), ip, port, testBannedHost, testProbeConnect, testProbeHandshake*10, func(local int) {
		reported.Store(int32(local))
	})
	select {
	case from := <-arrived:
		if from <= 0 {
			t.Fatalf("the port the probe reported (%d) must be known before its connection arrives and must be the one it arrives from", reported.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the probe never connected")
	}
}

func TestProbeTLSHostRepliedOnTheFirstServerBytes(t *testing.T) {
	ln := listenLocal(t)
	serveRaw(t, ln, func(c net.Conn) {
		buf := make([]byte, 4096)
		_, _ = c.Read(buf)
		_, _ = c.Write([]byte{0x16, 0x03})
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	})

	ip, port := probeAddr(t, ln.Addr())
	if got := probeTLSHost(context.Background(), ip, port, testBannedHost, testProbeConnect, testProbeHandshake, nil); got != TLSReplied {
		t.Fatalf("a server that starts its answer and then stalls has answered: %s, want replied", got)
	}
}

func TestProbeTLSHostSilentWhenTheServerNeverWrites(t *testing.T) {
	ln := listenLocal(t)
	serveRaw(t, ln, func(c net.Conn) {
		buf := make([]byte, 4096)
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	})

	ip, port := probeAddr(t, ln.Addr())
	start := time.Now()
	if got := probeTLSHost(context.Background(), ip, port, testBannedHost, testProbeConnect, testProbeHandshake, nil); got != TLSSilent {
		t.Fatalf("a connected server that never answers: %s, want silent", got)
	}
	if took := time.Since(start); took > testProbeHandshake+time.Second {
		t.Errorf("the handshake timeout must bound the probe, took %s", took)
	}
}

func TestProbeTLSHostDoesNotCountAConnectionThatIsOnlyEnded(t *testing.T) {
	for name, reset := range map[string]bool{"close": false, "reset": true} {
		t.Run(name, func(t *testing.T) {
			ln := listenLocal(t)
			serveRaw(t, ln, func(c net.Conn) {
				buf := make([]byte, 4096)
				_, _ = c.Read(buf)
				if reset {
					_ = c.(*net.TCPConn).SetLinger(0)
				}
				c.Close()
			})

			ip, port := probeAddr(t, ln.Addr())
			if got := probeTLSHost(context.Background(), ip, port, testBannedHost, testProbeConnect, testProbeHandshake*10, nil); got != TLSSilent {
				t.Fatalf("a %s without a byte may come from a filter on the path or from b4 itself, so it must not count as the server's answer: %s, want silent", name, got)
			}
		})
	}
}

func TestProbeTLSHostUnreachableWithoutAConnection(t *testing.T) {
	ln := listenLocal(t)
	ip, port := probeAddr(t, ln.Addr())
	ln.Close()

	if got := probeTLSHost(context.Background(), ip, port, testBannedHost, testProbeConnect, testProbeHandshake, nil); got != TLSUnreachable {
		t.Fatalf("a closed port: %s, want unreachable", got)
	}
	if got := ProbeTLSHost(context.Background(), nil, 443, testBannedHost, nil); got != TLSUnreachable {
		t.Fatalf("no address: %s, want unreachable", got)
	}
	if got := ProbeTLSHost(context.Background(), net.ParseIP("127.0.0.1"), 443, "", nil); got != TLSUnreachable {
		t.Fatalf("no name: %s, want unreachable", got)
	}
}
