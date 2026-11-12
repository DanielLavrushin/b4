package dns

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAStrictSetAsksItsOnlyResolverWhileItCools(t *testing.T) {
	resetHealth(t)
	var asked int32
	srv := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 40)), &asked)
	srv.Source = "only"
	coolDown("only")

	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{srv}, Options{Life: lifeContext(t), Strict: true}, true, "example.test", true, false)
	if err != nil || len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 40)) {
		t.Fatalf("got %v %v, want the answer: a strict set has nothing else to ask, so it asks a cooling resolver every time", ips, err)
	}
	if got := atomic.LoadInt32(&asked); got != 1 {
		t.Fatalf("the cooling resolver was asked %d times, want 1", got)
	}

	coolDown("only")
	query := BuildQuery("example.test", 7, rrTypeA)
	resp, source, err := AskServers(context.Background(), []Server{srv}, Options{Life: lifeContext(t), Strict: true},
		func(ctx context.Context, s Server) ([]byte, error) {
			return ResolveUpstreamContext(ctx, query, s.UDP, ForwardOptions{Port: s.Port, Timeout: s.Timeout})
		})
	if err != nil || source != "only" || len(ParseResponseIPs(resp)) != 1 {
		t.Fatalf("the device path got %d bytes from %q and %v, want the cooling resolver's answer", len(resp), source, err)
	}
}

func TestAStrictSetLeavesACoolingEntryOutWhileAnotherAnswers(t *testing.T) {
	resetHealth(t)
	var asked int32
	silentSrv := startAnswering(t, silent, &asked)
	silentSrv.Source = "silent"
	backup := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 41)), nil)
	backup.Source = "backup"
	coolDown("silent")

	start := time.Now()
	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{silentSrv, backup}, Options{Life: lifeContext(t), Strict: true}, true, "example.test", true, false)
	took := time.Since(start)
	if err != nil || len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 41)) {
		t.Fatalf("got %v %v, want the backup's answer", ips, err)
	}
	if took >= HeadStart {
		t.Fatalf("the lookup took %s; a cooling entry must not cost a strict set the head start", took)
	}
	if got := atomic.LoadInt32(&asked); got != 0 {
		t.Fatalf("the cooling entry was asked %d times while another entry answered, want 0", got)
	}
}

func TestAStrictSetAsksItsCoolingEntriesBeforeGivingUp(t *testing.T) {
	resetHealth(t)
	dead := startAnswering(t, silent, nil)
	dead.Timeout = 100 * time.Millisecond
	dead.Source = "dead"
	var asked int32
	recovered := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 42)), &asked)
	recovered.Source = "recovered"
	coolDown("recovered")

	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{recovered, dead}, Options{Life: lifeContext(t), Strict: true}, true, "example.test", true, false)
	if err != nil || len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 42)) {
		t.Fatalf("got %v %v, want the cooling entry's answer once the other entry failed", ips, err)
	}
	if got := atomic.LoadInt32(&asked); got != 1 {
		t.Fatalf("the cooling entry was asked %d times, want 1", got)
	}
}

func TestEveryResolverCoolingIsNotANegativeAnswer(t *testing.T) {
	resetHealth(t)
	coolDown("one")
	coolDown("two")
	servers := []Server{{Source: "one"}, {Source: "two"}}
	asked := false
	_, source, err := AskServers(context.Background(), servers, Options{Life: lifeContext(t)},
		func(context.Context, Server) ([]byte, error) {
			asked = true
			return nil, errors.New("down")
		})
	if !errors.Is(err, ErrSourceCooling) || errors.Is(err, ErrNoAddress) {
		t.Fatalf("got %v, want the cooling error: a lookup that asked nobody must not read as a name without an address", err)
	}
	if asked || source != "one" {
		t.Fatalf("asked=%v source=%q, want nobody asked and the first resolver named", asked, source)
	}
}

func TestLookupsThatMeetAtTheEndOfACooldownNeverReportNoAddress(t *testing.T) {
	resetHealth(t)
	servers := []Server{{Source: "only", Timeout: time.Second}}
	good := goodAnswer(net.IPv4(203, 0, 113, 44))

	const rounds, lookups = 200, 8
	var asked, cooling, other int32
	for round := 0; round < rounds; round++ {
		ResetSourceHealth()
		coolDown("only")
		expireCooldown("only")
		var wg sync.WaitGroup
		begin := make(chan struct{})
		for i := 0; i < lookups; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-begin
				_, _, err := AskServers(context.Background(), servers, Options{},
					func(context.Context, Server) ([]byte, error) { return good, nil })
				switch {
				case err == nil:
					atomic.AddInt32(&asked, 1)
				case errors.Is(err, ErrSourceCooling):
					atomic.AddInt32(&cooling, 1)
				default:
					atomic.AddInt32(&other, 1)
				}
			}()
		}
		close(begin)
		wg.Wait()
	}
	if other != 0 {
		t.Fatalf("%d lookups that asked nobody ended with another error than the cooling one (%d asked, %d cooling)", other, asked, cooling)
	}
	if asked < rounds {
		t.Fatalf("%d of %d rounds let the recovery attempt through", asked, rounds)
	}
}

func TestAQuietAskLeavesResolverHealthAlone(t *testing.T) {
	resetHealth(t)
	coolDown("only")
	servers := []Server{{Source: "only", Timeout: time.Second}}
	good := goodAnswer(net.IPv4(203, 0, 113, 43))

	asked := 0
	resp, _, err := AskServers(context.Background(), servers, Options{Life: lifeContext(t), Quiet: true},
		func(context.Context, Server) ([]byte, error) {
			asked++
			return good, nil
		})
	if err != nil || len(resp) == 0 || asked != 1 {
		t.Fatalf("got %d bytes, %v, asked %d times; a quiet ask must not skip a cooling resolver", len(resp), err, asked)
	}
	if !SourceCooling("only") {
		t.Fatal("a quiet answer cleared the resolver's cooldown")
	}

	expireCooldown("only")
	_, _, _ = AskServers(context.Background(), servers, Options{Life: lifeContext(t), Quiet: true},
		func(context.Context, Server) ([]byte, error) { return nil, errors.New("down") })
	if SourceCooling("only") {
		t.Fatal("a quiet ask spent the resolver's recovery attempt")
	}

	ResetSourceHealth()
	for i := 0; i <= SourceFailuresToTrip; i++ {
		_, _, _ = AskServers(context.Background(), servers, Options{Life: lifeContext(t), Quiet: true},
			func(context.Context, Server) ([]byte, error) { return nil, errors.New("down") })
	}
	if SourceCooling("only") {
		t.Fatal("failed quiet asks cooled the resolver down for everyone else")
	}
}

func TestOneRecoveryAttemptAtATime(t *testing.T) {
	resetHealth(t)
	coolDown("only")
	expireCooldown("only")
	servers := []Server{{Source: "only", Timeout: time.Second}}

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	first := make(chan error, 1)
	go func() {
		_, _, err := AskServers(context.Background(), servers, Options{},
			func(context.Context, Server) ([]byte, error) {
				entered <- struct{}{}
				<-release
				return goodAnswer(net.IPv4(203, 0, 113, 45)), nil
			})
		first <- err
	}()
	<-entered

	_, _, err := AskServers(context.Background(), servers, Options{},
		func(context.Context, Server) ([]byte, error) {
			t.Error("a second lookup asked the resolver while its recovery attempt was still out")
			return nil, errors.New("down")
		})
	if !errors.Is(err, ErrSourceCooling) {
		t.Fatalf("the second lookup got %v, want the cooling error", err)
	}

	close(release)
	if err := <-first; err != nil {
		t.Fatalf("the recovery attempt: %v", err)
	}
	if SourceCooling("only") {
		t.Fatal("the resolver answered its recovery attempt and is still cooling down")
	}
}

func TestALosersLateAnswerClearsItsFailures(t *testing.T) {
	resetHealth(t)
	NoteSourceFailure("slow")
	NoteSourceFailure("slow")
	release := make(chan struct{})
	servers := []Server{{Source: "slow", Timeout: 5 * time.Second}, {Source: "fast", Timeout: time.Second}}
	good := goodAnswer(net.IPv4(203, 0, 113, 46))

	_, source, err := AskServers(context.Background(), servers, Options{Life: lifeContext(t)},
		func(_ context.Context, srv Server) ([]byte, error) {
			if srv.Source == "slow" {
				<-release
			}
			return good, nil
		})
	if err != nil || source != "fast" {
		t.Fatalf("answered by %q with %v, want the second resolver after the head start", source, err)
	}
	close(release)
	failures := func() int {
		sourceMu.Lock()
		defer sourceMu.Unlock()
		if state := sourceHealth["slow"]; state != nil {
			return state.failures
		}
		return 0
	}
	deadline := time.Now().Add(time.Second)
	for failures() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := failures(); got != 0 {
		t.Fatalf("the slow resolver still has %d failures after its late answer, want them cleared", got)
	}
	if left := asksLeft("slow", time.Second); left != 0 {
		t.Fatalf("%d asks to the slow resolver are still counted after it answered", left)
	}
}

func TestASilentEntryOfAListHoldsABoundedNumberOfAsks(t *testing.T) {
	resetHealth(t)
	life, stop := context.WithCancel(context.Background())
	defer stop()
	good := goodAnswer(net.IPv4(203, 0, 113, 47))
	var started int32
	ask := func(ctx context.Context, srv Server) ([]byte, error) {
		if srv.Source == "silent" {
			atomic.AddInt32(&started, 1)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return good, nil
	}
	list := []Server{{Source: "silent", Timeout: time.Minute}, {Source: "backup", Timeout: time.Second}}

	const lookups = maxSourceAsks + 40
	var wg sync.WaitGroup
	var failed int32
	for i := 0; i < lookups; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, source, err := AskServers(context.Background(), list, Options{Life: life}, ask); err != nil || source != "backup" {
				atomic.AddInt32(&failed, 1)
			}
		}()
	}
	wg.Wait()
	if failed != 0 {
		t.Fatalf("%d of %d lookups did not get the backup's answer", failed, lookups)
	}
	if got := atomic.LoadInt32(&started); got != maxSourceAsks {
		t.Fatalf("%d asks to the silent entry were started by %d lookups, want the cap of %d", got, lookups, maxSourceAsks)
	}

	start := time.Now()
	if _, _, err := AskServers(context.Background(), list, Options{Life: life}, ask); err != nil {
		t.Fatalf("a lookup at the cap: %v", err)
	}
	if took := time.Since(start); took >= HeadStart {
		t.Fatalf("a lookup at the cap took %s, want the busy entry left out with no head start", took)
	}

	alone := []Server{{Source: "silent", Timeout: time.Minute}}
	done := make(chan struct{})
	go func() {
		_, _, _ = AskServers(context.Background(), alone, Options{Life: life}, ask)
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&started) != maxSourceAsks+1 {
		if time.Now().After(deadline) {
			t.Fatal("a set with one resolver was held back by the cap: without a list nothing else can answer")
		}
		time.Sleep(5 * time.Millisecond)
	}

	stop()
	<-done
	if left := asksLeft("silent", time.Second); left != 0 {
		t.Fatalf("%d asks are still counted after the service stopped", left)
	}
}

func TestStartedEntriesAreTrackedUntilTheyReturn(t *testing.T) {
	resetHealth(t)
	var open, begun int32
	track := func() func() {
		atomic.AddInt32(&begun, 1)
		atomic.AddInt32(&open, 1)
		return func() { atomic.AddInt32(&open, -1) }
	}
	release := make(chan struct{})
	servers := []Server{{Source: "slow", Timeout: 5 * time.Second}, {Source: "fast", Timeout: time.Second}, {Source: "spare", Timeout: time.Second}}
	good := goodAnswer(net.IPv4(203, 0, 113, 48))

	_, source, err := AskServers(context.Background(), servers, Options{Life: lifeContext(t), Track: track},
		func(_ context.Context, srv Server) ([]byte, error) {
			if srv.Source == "slow" {
				<-release
			}
			return good, nil
		})
	if err != nil || source != "fast" {
		t.Fatalf("answered by %q with %v, want the second resolver", source, err)
	}
	if got := atomic.LoadInt32(&begun); got != 2 {
		t.Fatalf("%d entries were tracked, want the two that started", got)
	}
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&open) != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&open); got != 1 {
		t.Fatalf("%d entries are held open after the answer, want the one still being asked", got)
	}
	close(release)
	deadline = time.Now().Add(time.Second)
	for atomic.LoadInt32(&open) != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&open); got != 0 {
		t.Fatalf("%d entries are still held open after every ask returned", got)
	}
}
