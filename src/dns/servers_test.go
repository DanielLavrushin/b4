package dns

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func startAnswering(t *testing.T, reply func([]byte) []byte, counter *int32) Server {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if counter != nil {
				atomic.AddInt32(counter, 1)
			}
			if resp := reply(buf[:n]); resp != nil {
				_, _ = conn.WriteToUDP(resp, from)
			}
		}
	}()
	t.Cleanup(func() {
		conn.Close()
		<-done
	})
	return Server{UDP: net.IPv4(127, 0, 0, 1), Port: conn.LocalAddr().(*net.UDPAddr).Port, Timeout: 3 * time.Second}
}

func answeringWith(ip net.IP) func([]byte) []byte {
	return func(query []byte) []byte { return BuildAnswerFromIPs(query, 60, []net.IP{ip}) }
}

func startDoH(t *testing.T, ip net.IP, delay time.Duration) Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, _ := io.ReadAll(r.Body)
		if delay > 0 {
			time.Sleep(delay)
		}
		if ip == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", DoHContentType)
		_, _ = w.Write(BuildAnswerFromIPs(query, 60, []net.IP{ip}))
	}))
	t.Cleanup(ts.Close)
	return Server{Source: ts.URL, DoHURL: ts.URL, Timeout: 3 * time.Second}
}

func TestTheSecondResolverIsAskedAfterTheHeadStart(t *testing.T) {
	resetHealth(t)
	first := startAnswering(t, func([]byte) []byte { return nil }, nil)
	first.Source = "first"
	second := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 20)), nil)
	second.Source = "second"

	start := time.Now()
	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{first, second}, true, false, "example.test", true, false)
	took := time.Since(start)

	if err != nil || len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 20)) {
		t.Fatalf("got %v %v, want the second resolver's answer", ips, err)
	}
	if took >= first.Timeout {
		t.Fatalf("the lookup took %s; a silent resolver must cost the head start, not its whole timeout of %s", took, first.Timeout)
	}
}

func TestAServfailMovesToTheNextResolver(t *testing.T) {
	resetHealth(t)
	first := startAnswering(t, BuildServfailResponse, nil)
	first.Source = "servfail"
	second := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 21)), nil)
	second.Source = "good"

	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{first, second}, true, false, "example.test", true, false)
	if err != nil || len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 21)) {
		t.Fatalf("got %v %v, want the answer of the resolver after the SERVFAIL", ips, err)
	}
	if SourceUnreachable("servfail") {
		t.Fatal("a resolver that answered SERVFAIL was counted as unreachable")
	}
}

func TestTheEncryptedAnswerWinsOverAPlainOne(t *testing.T) {
	resetHealth(t)
	plain := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 22)), nil)
	plain.Source = "plain"
	slow := startDoH(t, net.IPv4(203, 0, 113, 23), 120*time.Millisecond)

	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{plain, slow}, true, false, "example.test", true, false)
	if err != nil || len(ips) != 1 {
		t.Fatalf("got %v %v", ips, err)
	}
	if !ips[0].Equal(net.IPv4(203, 0, 113, 23)) {
		t.Fatalf("got %v, want the encrypted answer to win over the plain one", ips)
	}
}

func TestAPlainAnswerIsNotHeldLongerThanTheHeadStart(t *testing.T) {
	resetHealth(t)
	plain := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 24)), nil)
	plain.Source = "plain"
	silent := startDoH(t, nil, 0)
	silent.Timeout = 3 * time.Second

	start := time.Now()
	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{plain, silent}, true, false, "example.test", true, false)
	took := time.Since(start)

	if err != nil || len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 24)) {
		t.Fatalf("got %v %v, want the plain answer", ips, err)
	}
	if took >= silent.Timeout {
		t.Fatalf("the lookup waited %s for a failing DoH resolver instead of one head start", took)
	}
}

func TestOneResolverIsAskedExactlyOnce(t *testing.T) {
	resetHealth(t)
	var asked int32
	srv := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 25)), &asked)
	srv.Source = "only"

	if _, err := LookupWithFallback(context.Background(), LookupIPs, []Server{srv}, true, false, "example.test", true, false); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got := atomic.LoadInt32(&asked); got != 1 {
		t.Fatalf("the resolver was asked %d times, want 1: a set without the list must behave as before", got)
	}
}
