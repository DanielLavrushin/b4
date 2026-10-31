package iphealth

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

func newTestTracker(reachable func(ip string) bool) *Tracker {
	return NewTracker(func(ip string, _ uint16, _ int) bool { return reachable(ip) })
}

func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

func TestTrackerNeedsThresholdBeforeProbing(t *testing.T) {
	probed := false
	var mu sync.Mutex
	s := newTestTracker(func(string) bool {
		mu.Lock()
		probed = true
		mu.Unlock()
		return false
	})
	defer s.Stop()

	s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)

	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if probed {
		t.Errorf("probed after 2 SYNs, threshold is 3")
	}
	if s.IsDead("1.2.3.4") {
		t.Errorf("destination marked dead before the probe ran")
	}
}

func TestTrackerMarksDeadWhenProbeFails(t *testing.T) {
	s := newTestTracker(func(string) bool { return false })
	defer s.Stop()

	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}

	if !waitFor(t, func() bool { return s.IsDead("1.2.3.4") }) {
		t.Fatal("destination never became dead after an unanswered probe")
	}
	if got := s.DeadIPs(); len(got) != 1 || got[0] != "1.2.3.4" {
		t.Errorf("DeadIPs = %v, want [1.2.3.4]", got)
	}
}

func TestTrackerProbeRescuesFalsePositive(t *testing.T) {
	s := newTestTracker(func(string) bool { return true })
	defer s.Stop()

	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}

	if !waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		e := s.entries["1.2.3.4"]
		return e != nil && !e.probing
	}) {
		t.Fatal("probe never completed")
	}
	if s.IsDead("1.2.3.4") {
		t.Errorf("destination marked dead even though the router reached it")
	}
}

func TestTrackerDeadStateExpiresAndReprobes(t *testing.T) {
	var mu sync.Mutex
	reachable := false
	s := newTestTracker(func(string) bool {
		mu.Lock()
		defer mu.Unlock()
		return reachable
	})
	defer s.Stop()

	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}
	if !waitFor(t, func() bool { return s.IsDead("1.2.3.4") }) {
		t.Fatal("destination never became dead")
	}

	mu.Lock()
	reachable = true
	mu.Unlock()

	s.Cleanup(time.Nanosecond)

	if !waitFor(t, func() bool { return !s.IsDead("1.2.3.4") }) {
		t.Fatal("dead state never expired, so an unbanned address would stay blocked forever")
	}
}

func TestTrackerDeadStateSurvivesWhenStillUnreachable(t *testing.T) {
	s := newTestTracker(func(string) bool { return false })
	defer s.Stop()

	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}
	if !waitFor(t, func() bool { return s.IsDead("1.2.3.4") }) {
		t.Fatal("destination never became dead")
	}

	s.Cleanup(time.Nanosecond)

	if !waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		e := s.entries["1.2.3.4"]
		return e != nil && !e.probing && e.state == stateDead
	}) {
		t.Fatal("re-probe did not restore the dead state for a still unreachable address")
	}
}

func TestTrackerBurstSuspendsDetection(t *testing.T) {
	s := newTestTracker(func(string) bool { return false })
	defer s.Stop()

	ips := make([]string, 0, deadBurst)
	for i := 0; i < deadBurst; i++ {
		ip := "10.0.0." + strconv.Itoa(i+1)
		ips = append(ips, ip)
		for j := 0; j < 3; j++ {
			s.RecordSyn(ip, 443, 0, 3, time.Hour)
		}
	}

	if !waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.suspendedLocked(time.Now())
	}) {
		t.Fatal("a burst of unreachable destinations did not suspend detection")
	}

	for _, ip := range ips {
		if s.IsDead(ip) {
			t.Fatalf("%s still reported dead while detection is suspended", ip)
		}
	}
	if got := s.DeadIPs(); len(got) != 0 {
		t.Errorf("DeadIPs = %v, want none while suspended", got)
	}
}

func TestTrackerStopIsIdempotent(t *testing.T) {
	s := newTestTracker(func(string) bool { return false })
	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}
	s.Stop()
	s.Stop()

	s.RecordSyn("5.6.7.8", 443, 0, 1, time.Hour)
	if s.IsDead("5.6.7.8") {
		t.Errorf("store kept condemning destinations after Stop")
	}
}

func TestTrackerRecordResponseCannotResurrectDead(t *testing.T) {
	s := newTestTracker(func(string) bool { return false })
	defer s.Stop()

	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}
	if !waitFor(t, func() bool { return s.IsDead("1.2.3.4") }) {
		t.Fatal("destination never became dead")
	}

	s.RecordResponse("1.2.3.4")

	if !s.IsDead("1.2.3.4") {
		t.Error("a bare inbound packet cleared the dead verdict; b4's own injected reset carries the destination as its source and loops back when the client is the router itself, so this resurrects the address on every reset")
	}
}

func TestTrackerHandshakeClearsDead(t *testing.T) {
	s := newTestTracker(func(string) bool { return false })
	defer s.Stop()

	for i := 0; i < 3; i++ {
		s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	}
	if !waitFor(t, func() bool { return s.IsDead("1.2.3.4") }) {
		t.Fatal("destination never became dead")
	}

	s.RecordHandshake("1.2.3.4")

	if s.IsDead("1.2.3.4") {
		t.Error("a completed handshake must clear the dead verdict, otherwise a recovered address stays blocked")
	}
}

func TestTrackerResponseStillRefreshesLiveEntry(t *testing.T) {
	s := newTestTracker(func(string) bool { return false })
	defer s.Stop()

	s.RecordSyn("1.2.3.4", 443, 0, 3, time.Hour)
	s.RecordResponse("1.2.3.4")

	s.mu.Lock()
	e := s.entries["1.2.3.4"]
	syns, st := e.syns, e.state
	s.mu.Unlock()

	if syns != 0 || st != stateLive {
		t.Errorf("syns=%d state=%v, want a reply to reset the SYN counter on a live entry", syns, st)
	}
}

const (
	testProbeConnect   = time.Second
	testProbeHandshake = 150 * time.Millisecond
)

func probeAddr(t *testing.T, addr net.Addr) (net.IP, int) {
	t.Helper()
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("not a TCP address: %v", addr)
	}
	return tcp.IP, tcp.Port
}

func serveRaw(t *testing.T, ln net.Listener, handle func(net.Conn)) {
	t.Helper()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var conns []net.Conn
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				handle(c)
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
}

func listenLocal(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln
}

func TestProbeTLSRepliedWhenTheHandshakeCompletes(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	ip, port := probeAddr(t, srv.Listener.Addr())
	if got := probeTLS(context.Background(), ip, port, 0, testProbeConnect, testProbeHandshake*10); got != TLSReplied {
		t.Fatalf("a server that completes the handshake: %s, want replied", got)
	}
}

func TestProbeTLSRepliedWhenTheServerSendsAnAlert(t *testing.T) {
	ln := tls.NewListener(listenLocal(t), &tls.Config{
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return nil, errors.New("no certificate for this name")
		},
	})
	serveRaw(t, ln, func(c net.Conn) {
		_ = c.(*tls.Conn).Handshake()
		c.Close()
	})

	ip, port := probeAddr(t, ln.Addr())
	if got := probeTLS(context.Background(), ip, port, 0, testProbeConnect, testProbeHandshake*10); got != TLSReplied {
		t.Fatalf("a TLS alert is an answer: %s, want replied", got)
	}
}

func TestProbeTLSRepliedWhenTheServerSendsAnythingElse(t *testing.T) {
	ln := listenLocal(t)
	serveRaw(t, ln, func(c net.Conn) {
		_, _ = c.Write([]byte("HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n"))
		c.Close()
	})

	ip, port := probeAddr(t, ln.Addr())
	if got := probeTLS(context.Background(), ip, port, 0, testProbeConnect, testProbeHandshake*10); got != TLSReplied {
		t.Fatalf("bytes that are not TLS are still an answer: %s, want replied", got)
	}
}

func TestProbeTLSSilentWhenTheServerNeverWrites(t *testing.T) {
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
	if got := probeTLS(context.Background(), ip, port, 0, testProbeConnect, testProbeHandshake); got != TLSSilent {
		t.Fatalf("a connected server that never answers: %s, want silent", got)
	}
	if took := time.Since(start); took > time.Duration(len(neutralTLSNames))*testProbeHandshake+time.Second {
		t.Errorf("the handshake timeout must bound the probe, took %s", took)
	}
}

func TestProbeTLSRepliedWhenTheServerClosesWithoutAByte(t *testing.T) {
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
			start := time.Now()
			if got := probeTLS(context.Background(), ip, port, 0, testProbeConnect, testProbeHandshake*10); got != TLSReplied {
				t.Fatalf("a server that ends the connection without a byte is alive, like a front end refusing an unknown name: %s, want replied", got)
			}
			if took := time.Since(start); took >= testProbeHandshake*10 {
				t.Errorf("an active %s must not wait for the handshake timeout, took %s", name, took)
			}
		})
	}
}

func TestProbeTLSUnreachableWhenTheConnectFails(t *testing.T) {
	ln := listenLocal(t)
	ip, port := probeAddr(t, ln.Addr())
	ln.Close()

	if got := probeTLS(context.Background(), ip, port, 0, testProbeConnect, testProbeHandshake); got != TLSUnreachable {
		t.Fatalf("a closed port: %s, want unreachable", got)
	}
	if got := ProbeTLS(context.Background(), nil, 443, 0); got != TLSUnreachable {
		t.Fatalf("no address: %s, want unreachable", got)
	}
}

func TestProbeTLSIsInconclusiveWhenCancelled(t *testing.T) {
	ln := listenLocal(t)
	serveRaw(t, ln, func(c net.Conn) {
		buf := make([]byte, 4096)
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	ip, port := probeAddr(t, ln.Addr())
	if got := probeTLS(ctx, ip, port, 0, testProbeConnect, 10*time.Second); got != TLSUnreachable {
		t.Fatalf("a probe cut short by shutdown must never read as silent: %s", got)
	}
}
