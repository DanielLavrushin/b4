package dns

import (
	"context"
	"errors"
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
	return Server{UDP: net.IPv4(127, 0, 0, 1), Port: conn.LocalAddr().(*net.UDPAddr).Port, Timeout: time.Second}
}

func answeringWith(ip net.IP) func([]byte) []byte {
	return func(query []byte) []byte { return BuildAnswerFromIPs(query, 60, []net.IP{ip}) }
}

func startDoH(t *testing.T, ip net.IP, delay time.Duration, counter *int32) Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, _ := io.ReadAll(r.Body)
		if counter != nil {
			atomic.AddInt32(counter, 1)
		}
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
	return Server{Source: ts.URL, DoHURL: ts.URL, Timeout: time.Second}
}

func TestTheSecondResolverIsAskedAfterTheHeadStart(t *testing.T) {
	resetHealth(t)
	first := startAnswering(t, func([]byte) []byte { return nil }, nil)
	first.Timeout = 3 * time.Second
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

func TestAServfailMovesOnAtOnce(t *testing.T) {
	resetHealth(t)
	first := startAnswering(t, BuildServfailResponse, nil)
	first.Source = "servfail"
	second := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 21)), nil)
	second.Source = "good"

	start := time.Now()
	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{first, second}, true, false, "example.test", true, false)
	took := time.Since(start)

	if err != nil || len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 21)) {
		t.Fatalf("got %v %v, want the answer of the resolver after the SERVFAIL", ips, err)
	}
	if took >= HeadStart {
		t.Fatalf("the lookup took %s after a SERVFAIL; the next resolver must be asked at once", took)
	}
	if SourceUnreachable("servfail") {
		t.Fatal("a resolver that answered SERVFAIL was counted as unreachable")
	}
}

func TestTheFirstUsableAnswerEndsTheLookup(t *testing.T) {
	resetHealth(t)
	plain := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 22)), nil)
	plain.Source = "plain"
	var asked int32
	slow := startDoH(t, net.IPv4(203, 0, 113, 23), 50*time.Millisecond, &asked)

	start := time.Now()
	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{plain, slow}, true, false, "example.test", true, false)
	took := time.Since(start)

	if err != nil || len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 22)) {
		t.Fatalf("got %v %v, want the first resolver's answer", ips, err)
	}
	if took >= HeadStart {
		t.Fatalf("the lookup took %s; the first usable answer must end it", took)
	}
	if got := atomic.LoadInt32(&asked); got != 0 {
		t.Fatalf("the second resolver was asked %d times after a usable answer, want 0", got)
	}
}

func TestABlockedResolverCoolsDownAfterThreeFailures(t *testing.T) {
	resetHealth(t)
	t.Cleanup(func() { ResetSourceHealth() })
	blocked := startAnswering(t, func([]byte) []byte { return nil }, nil)
	blocked.Timeout = 60 * time.Millisecond
	blocked.Source = "blocked"
	backup := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 26)), nil)
	backup.Source = "backup"

	for i := 1; i <= SourceFailuresToTrip; i++ {
		if _, err := LookupWithFallback(context.Background(), LookupIPs, []Server{blocked, backup}, true, false, "example.test", true, false); err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
	}
	if !SourceUnreachable("blocked") {
		t.Fatalf("a resolver that failed %d times never cooled down", SourceFailuresToTrip)
	}
}

func TestAMultiResolverEmptyAnswerIsNodata(t *testing.T) {
	resetHealth(t)
	first := startAnswering(t, BuildEmptyAnswer, nil)
	first.Source = "empty"
	second := startAnswering(t, BuildEmptyAnswer, nil)
	second.Source = "empty2"

	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{first, second}, true, false, "example.test", true, false)
	if !errors.Is(err, ErrNoAddress) || len(ips) != 0 {
		t.Fatalf("got %v %v, want an empty answer reported as no address", ips, err)
	}
}

func TestABudgetWithoutAListKeepsMainsValue(t *testing.T) {
	if got, want := ServersBudget(nil, 5*time.Second), 2*5*time.Second+FallbackBudget; got != want {
		t.Fatalf("budget without a list is %s, want main's %s", got, want)
	}
	two := []Server{{Timeout: 5 * time.Second}, {Timeout: 5 * time.Second}}
	if got, want := ServersBudget(two, 5*time.Second), 2*5*time.Second+FallbackBudget+HeadStart; got != want {
		t.Fatalf("budget with two resolvers is %s, want %s", got, want)
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

func TestRepeatedLookupsLeaveNoGoroutines(t *testing.T) {
	resetHealth(t)
	good := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 27)), nil)
	good.Source = "fast"
	silent := startAnswering(t, func([]byte) []byte { return nil }, nil)
	silent.Timeout = time.Second
	silent.Source = "silent"

	for i := 0; i < 50; i++ {
		if _, err := LookupWithFallback(context.Background(), LookupIPs, []Server{good, silent}, true, false, "example.test", true, false); err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
	}
}
