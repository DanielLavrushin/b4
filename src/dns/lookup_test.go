package dns

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type fakeResolver struct {
	conn    *net.UDPConn
	answers map[uint16][]net.IP
	mu      sync.Mutex
	asked   []string
	badID   bool
}

func startFakeResolver(t *testing.T, answers map[uint16][]net.IP) *fakeResolver {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeResolver{conn: conn, answers: answers}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = conn.WriteToUDP(f.answer(buf[:n]), from)
		}
	}()
	t.Cleanup(func() {
		conn.Close()
		<-done
	})
	return f
}

func (f *fakeResolver) answer(query []byte) []byte {
	name, _ := ParseQueryDomain(query)
	qtype, _ := QuestionType(query)
	f.mu.Lock()
	f.asked = append(f.asked, name)
	bad := f.badID
	f.mu.Unlock()
	resp := BuildAnswerFromIPs(query, 60, f.answers[qtype])
	if resp == nil {
		resp = BuildEmptyAnswer(query)
	}
	if bad {
		resp[0] ^= 0xFF
	}
	return resp
}

func (f *fakeResolver) server() Server {
	return Server{UDP: net.IPv4(127, 0, 0, 1), Port: f.conn.LocalAddr().(*net.UDPAddr).Port, Timeout: time.Second}
}

func TestSetServerFollowsTheSetsDNSSettings(t *testing.T) {
	if _, ok := SetServer(false, "9.9.9.9", ""); ok {
		t.Error("a set with DNS off has no server")
	}
	if srv, ok := SetServer(true, "9.9.9.9", "https://dns.example/dns-query"); !ok || srv.DoHURL == "" || srv.UDP != nil {
		t.Errorf("the DoH URL takes precedence over the server address, got %+v", srv)
	}
	if srv, ok := SetServer(true, "9.9.9.9", ""); !ok || !srv.UDP.Equal(net.IPv4(9, 9, 9, 9)) {
		t.Errorf("got %+v", srv)
	}
	if _, ok := SetServer(true, "not-an-ip", ""); ok {
		t.Error("a target that is not an address is not a server")
	}
}

func TestLookupIPsAsksTheSetsServerForEachFamily(t *testing.T) {
	f := startFakeResolver(t, map[uint16][]net.IP{
		rrTypeA:    {net.IPv4(198, 51, 100, 7)},
		rrTypeAAAA: {net.ParseIP("2001:db8::7")},
	})

	ips, err := LookupIPs(context.Background(), f.server(), "blocked.example", true, true)
	if err != nil {
		t.Fatalf("LookupIPs: %v", err)
	}
	if len(ips) != 2 || !ips[0].Equal(net.IPv4(198, 51, 100, 7)) || !ips[1].Equal(net.ParseIP("2001:db8::7")) {
		t.Fatalf("got %v", ips)
	}

	ips, err = LookupIPs(context.Background(), f.server(), "blocked.example", true, false)
	if err != nil || len(ips) != 1 || ips[0].To4() == nil {
		t.Fatalf("IPv4 only: %v %v", ips, err)
	}
}

func TestLookupIPsFailsWithoutAnAddress(t *testing.T) {
	f := startFakeResolver(t, nil)
	if _, err := LookupIPs(context.Background(), f.server(), "empty.example", true, true); err == nil {
		t.Fatal("an answer with no address must be an error, so the caller can fall back")
	}
}

func TestLookupIPsRejectsAnAnswerToAnotherQuery(t *testing.T) {
	f := startFakeResolver(t, map[uint16][]net.IP{rrTypeA: {net.IPv4(198, 51, 100, 7)}})
	f.mu.Lock()
	f.badID = true
	f.mu.Unlock()
	if _, err := LookupIPs(context.Background(), f.server(), "blocked.example", true, false); err == nil {
		t.Fatal("an answer carrying another transaction id was accepted")
	}
}

func TestLookupIPsOverDoH(t *testing.T) {
	var got []string
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, _ := io.ReadAll(r.Body)
		name, _ := ParseQueryDomain(query)
		mu.Lock()
		got = append(got, r.Header.Get("Content-Type")+" "+name)
		mu.Unlock()
		w.Header().Set("Content-Type", DoHContentType)
		_, _ = w.Write(BuildAnswerFromIPs(query, 60, []net.IP{net.IPv4(203, 0, 113, 9)}))
	}))
	t.Cleanup(ts.Close)
	srv := Server{DoHURL: ts.URL + "/dns-query", Timeout: 2 * time.Second}
	t.Cleanup(func() { lookupDoHClient(srv.Mark, srv.Timeout).CloseIdleConnections() })

	ips, err := LookupIPs(context.Background(), srv, "blocked.example", true, false)
	if err != nil {
		t.Fatalf("LookupIPs: %v", err)
	}
	if len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 9)) {
		t.Fatalf("got %v", ips)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != DoHContentType+" blocked.example" {
		t.Fatalf("DoH server saw %v", got)
	}
}

func TestLookupIPsHonoursTheCallersDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx, cancelDeadline := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancelDeadline()
	if _, err := LookupIPs(ctx, Server{UDP: net.IPv4(127, 0, 0, 1), Port: 9}, "late.example", true, false); err == nil {
		t.Fatal("a lookup past its deadline must fail at once")
	}
}

func startBlackHole(t *testing.T) (Server, func() int) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	count := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		for {
			if _, _, err := conn.ReadFromUDP(buf); err != nil {
				return
			}
			mu.Lock()
			count++
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		conn.Close()
		<-done
	})
	asked := func() int {
		mu.Lock()
		defer mu.Unlock()
		return count
	}
	return Server{Source: "blackhole", UDP: net.IPv4(127, 0, 0, 1), Port: conn.LocalAddr().(*net.UDPAddr).Port, Timeout: 300 * time.Millisecond}, asked
}

func resetHealth(t *testing.T) {
	t.Helper()
	ResetSourceHealth()
	t.Cleanup(ResetSourceHealth)
}

func TestAnEmptyAnswerFromTheSetsResolverIsFinal(t *testing.T) {
	resetHealth(t)
	f := startFakeResolver(t, nil)
	srv := f.server()
	srv.Source = "empty"

	ips, err := LookupWithFallback(context.Background(), LookupIPs, srv, true, false, "localhost", true, true)
	if !errors.Is(err, ErrNoAddress) || len(ips) != 0 {
		t.Fatalf("got %v %v; an answer with no address must not send the name to the router's resolver", ips, err)
	}
}

func TestAFailingResolverFallsBackInTime(t *testing.T) {
	resetHealth(t)
	srv, _ := startBlackHole(t)
	ctx, cancel := context.WithTimeout(context.Background(), srv.Budget())
	defer cancel()

	start := time.Now()
	ips, err := LookupWithFallback(ctx, LookupIPs, srv, true, false, "localhost", true, true)
	if err != nil || len(ips) == 0 {
		t.Fatalf("a silent set resolver left no time for the router's resolver: %v %v", ips, err)
	}
	if took := time.Since(start); took > 2*srv.Timeout {
		t.Errorf("the lookup took %s; A and AAAA must share one timeout of %s", took, srv.Timeout)
	}
}

func TestStrictNeverFallsBack(t *testing.T) {
	resetHealth(t)
	srv, _ := startBlackHole(t)
	if ips, err := LookupWithFallback(context.Background(), LookupIPs, srv, true, true, "localhost", true, false); err == nil {
		t.Fatalf("strict fell back to %v", ips)
	}
}

func TestADeadResolverIsSkippedAfterRepeatedFailures(t *testing.T) {
	resetHealth(t)
	srv, asked := startBlackHole(t)
	for i := 0; i < SourceFailuresToTrip+3; i++ {
		if _, err := LookupWithFallback(context.Background(), LookupIPs, srv, true, false, "localhost", true, false); err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if n := asked(); n != SourceFailuresToTrip {
		t.Fatalf("the dead resolver was asked %d times, want %d before the cooldown", n, SourceFailuresToTrip)
	}
	if !SourceUnreachable(srv.Source) {
		t.Fatal("the resolver is not cooling down")
	}
}

func TestSourceBreakerLetsOneProbeThroughAfterTheCooldown(t *testing.T) {
	resetHealth(t)
	const source = "9.9.9.9"
	for i := 0; i < SourceFailuresToTrip; i++ {
		NoteSourceFailure(source)
	}
	if !SourceUnreachable(source) {
		t.Fatal("breaker did not trip")
	}
	sourceMu.Lock()
	sourceHealth[source].retryAt = time.Now().Add(-time.Second)
	sourceMu.Unlock()
	if SourceUnreachable(source) {
		t.Fatal("the cooldown expired, one probe must be let through")
	}
	if !SourceUnreachable(source) {
		t.Fatal("the probe was let through, the breaker must close again until the next cooldown")
	}
}

func TestServfailFromTheSetsResolverIsFinal(t *testing.T) {
	resetHealth(t)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
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
			_, _ = conn.WriteToUDP(BuildServfailResponse(buf[:n]), from)
		}
	}()
	t.Cleanup(func() {
		conn.Close()
		<-done
	})
	srv := Server{Source: "servfail", UDP: net.IPv4(127, 0, 0, 1), Port: conn.LocalAddr().(*net.UDPAddr).Port, Timeout: time.Second}
	ips, err := LookupWithFallback(context.Background(), LookupIPs, srv, true, false, "localhost", true, false)
	if !errors.Is(err, ErrNoAddress) || len(ips) != 0 {
		t.Fatalf("got %v %v; a SERVFAIL is the resolver's answer, as the DNS redirect hands it to clients", ips, err)
	}
	if SourceUnreachable(srv.Source) || NoteSourceSuccess(srv.Source) {
		t.Fatal("a resolver that answered was counted as failing")
	}
}

func TestDoHClientsAreKeyedOnTheConfiguredTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", DoHContentType)
		_, _ = w.Write(BuildAnswerFromIPs(query, 60, []net.IP{net.IPv4(203, 0, 113, 9)}))
	}))
	t.Cleanup(ts.Close)
	srv := Server{DoHURL: ts.URL, Timeout: 4 * time.Second}
	t.Cleanup(func() { lookupDoHClient(srv.Mark, srv.Timeout).CloseIdleConnections() })

	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(i+1)*time.Second)
		if _, err := LookupIPs(ctx, srv, "blocked.example", true, false); err != nil {
			cancel()
			t.Fatalf("LookupIPs: %v", err)
		}
		cancel()
	}
	lookupClientMu.Lock()
	n := 0
	for k := range lookupClients {
		if k == [2]int64{0, int64(4 * time.Second)} {
			n++
		}
	}
	lookupClientMu.Unlock()
	if n != 1 {
		t.Fatalf("%d DoH clients for one resolver, want 1", n)
	}
}
