package dns

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
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

func silent([]byte) []byte { return nil }

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

func lifeContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}

func coolDown(source string) {
	for i := 0; i < SourceFailuresToTrip; i++ {
		NoteSourceFailure(source)
	}
}

func asksInFlight(source string) int {
	sourceMu.Lock()
	defer sourceMu.Unlock()
	return sourceAsks[source]
}

func goodAnswer(ip net.IP) []byte {
	return BuildAnswerFromIPs(BuildQuery("example.test", 7, rrTypeA), 60, []net.IP{ip})
}

func TestTheSecondResolverIsAskedAfterTheHeadStart(t *testing.T) {
	resetHealth(t)
	first := startAnswering(t, silent, nil)
	first.Timeout = 3 * time.Second
	first.Source = "first"
	start := time.Now()
	var secondAskedAt atomic.Int64
	second := startAnswering(t, func(q []byte) []byte {
		secondAskedAt.CompareAndSwap(0, int64(time.Since(start)))
		return BuildAnswerFromIPs(q, 60, []net.IP{net.IPv4(203, 0, 113, 20)})
	}, nil)
	second.Source = "second"

	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{first, second}, Options{Life: lifeContext(t)}, true, "example.test", true, false)
	took := time.Since(start)

	if err != nil || len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 20)) {
		t.Fatalf("got %v %v, want the second resolver's answer", ips, err)
	}
	if took >= first.Timeout {
		t.Fatalf("the lookup took %s; a silent resolver must cost the head start, not its whole timeout of %s", took, first.Timeout)
	}
	if at := time.Duration(secondAskedAt.Load()); at < HeadStart-20*time.Millisecond {
		t.Fatalf("the second resolver was asked after %s, before the head start of %s ran out: the list is asked in order, not raced", at, HeadStart)
	}
}

func TestAServfailMovesOnAtOnce(t *testing.T) {
	resetHealth(t)
	var asked int32
	first := startAnswering(t, BuildServfailResponse, &asked)
	first.Source = "servfail"
	second := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 21)), nil)
	second.Source = "good"

	for i := 1; i <= SourceFailuresToTrip+1; i++ {
		start := time.Now()
		ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{first, second}, Options{Life: lifeContext(t)}, true, "example.test", true, false)
		took := time.Since(start)
		if err != nil || len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 21)) {
			t.Fatalf("lookup %d: got %v %v, want the answer of the resolver after the SERVFAIL", i, ips, err)
		}
		if took >= HeadStart {
			t.Fatalf("lookup %d took %s after a SERVFAIL; the next resolver must be asked at once", i, took)
		}
	}
	if SourceCooling("servfail") {
		t.Fatal("a resolver that answered SERVFAIL every time is cooling down: an rcode answer is not a failure")
	}
	if got := atomic.LoadInt32(&asked); got != SourceFailuresToTrip+1 {
		t.Fatalf("the SERVFAIL resolver was asked %d times in %d lookups, want every time", got, SourceFailuresToTrip+1)
	}
}

func TestTheFirstUsableAnswerEndsTheLookup(t *testing.T) {
	resetHealth(t)
	plain := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 22)), nil)
	plain.Source = "plain"
	var asked int32
	slow := startDoH(t, net.IPv4(203, 0, 113, 23), 50*time.Millisecond, &asked)

	start := time.Now()
	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{plain, slow}, Options{Life: lifeContext(t)}, true, "example.test", true, false)
	took := time.Since(start)

	if err != nil || len(ips) != 1 || !ips[0].Equal(net.IPv4(203, 0, 113, 22)) {
		t.Fatalf("got %v %v, want the first resolver's answer", ips, err)
	}
	if took >= HeadStart {
		t.Fatalf("the lookup took %s; the first usable answer must end it", took)
	}
	time.Sleep(HeadStart + 100*time.Millisecond)
	if got := atomic.LoadInt32(&asked); got != 0 {
		t.Fatalf("the second resolver was asked %d times after a usable answer, want 0", got)
	}
}

func TestANegativeAnswerEndsTheLookupToo(t *testing.T) {
	resetHealth(t)
	first := startAnswering(t, BuildEmptyAnswer, nil)
	first.Source = "empty"
	var asked int32
	second := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 24)), &asked)
	second.Source = "positive"

	ips, err := LookupWithFallback(context.Background(), LookupIPs, []Server{first, second}, Options{Life: lifeContext(t)}, true, "example.test", true, false)
	if !errors.Is(err, ErrNoAddress) || errors.Is(err, ErrTransientAnswer) || len(ips) != 0 {
		t.Fatalf("got %v %v, want the first resolver's empty answer reported as no address", ips, err)
	}
	time.Sleep(HeadStart + 100*time.Millisecond)
	if got := atomic.LoadInt32(&asked); got != 0 {
		t.Fatalf("the next resolver was asked %d times after a negative answer, want 0: a later entry never corrects an earlier one", got)
	}
}

func TestABlockedResolverCoolsDownAfterThreeFailures(t *testing.T) {
	resetHealth(t)
	var asked int32
	blocked := startAnswering(t, silent, &asked)
	blocked.Timeout = time.Second
	blocked.Source = "blocked"
	backup := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 26)), nil)
	backup.Source = "backup"
	servers := []Server{blocked, backup}

	for i := 1; i <= SourceFailuresToTrip; i++ {
		if _, err := LookupWithFallback(context.Background(), LookupIPs, servers, Options{Life: lifeContext(t)}, true, "example.test", true, false); err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for !SourceCooling("blocked") {
		if time.Now().After(deadline) {
			t.Fatalf("a resolver that timed out after the backup won never cooled down, its timeout is %s", blocked.Timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}

	start := time.Now()
	if _, err := LookupWithFallback(context.Background(), LookupIPs, servers, Options{Life: lifeContext(t)}, true, "example.test", true, false); err != nil {
		t.Fatalf("lookup after the cooldown began: %v", err)
	}
	if took := time.Since(start); took >= HeadStart {
		t.Fatalf("the lookup took %s with the blocked resolver cooling down, want no head start for it", took)
	}
	if got := atomic.LoadInt32(&asked); got != SourceFailuresToTrip {
		t.Fatalf("the blocked resolver was asked %d times, want %d: it is left out while it cools down", got, SourceFailuresToTrip)
	}
}

func TestARealFailureBeatsARetryableAnswer(t *testing.T) {
	resetHealth(t)
	silentSrv := startAnswering(t, silent, nil)
	silentSrv.Timeout = 400 * time.Millisecond
	silentSrv.Source = "silent"
	refusing := startAnswering(t, BuildServfailResponse, nil)
	refusing.Source = "refusing"

	_, err := LookupWithFallback(context.Background(), LookupIPs, []Server{silentSrv, refusing}, Options{Life: lifeContext(t), Strict: true}, true, "example.test", true, false)
	if err == nil {
		t.Fatal("got no error; one entry timed out and the other refused, the failure must win")
	}
	if errors.Is(err, ErrTransientAnswer) {
		t.Fatalf("got %v; a backup's SERVFAIL must not hide the primary's timeout", err)
	}
}

func TestARefusingBackupDoesNotHideACoolingPrimary(t *testing.T) {
	resetHealth(t)
	servfail := BuildServfailResponse(BuildQuery("example.test", 7, rrTypeA))
	servers := []Server{{Source: "dead", Timeout: time.Second}, {Source: "refusing", Timeout: time.Second}}
	ask := func(_ context.Context, srv Server) ([]byte, error) {
		if srv.Source == "dead" {
			return nil, errors.New("no reply")
		}
		return servfail, nil
	}

	for i := 1; i <= SourceFailuresToTrip+2; i++ {
		resp, _, err := AskServers(context.Background(), servers, Options{Life: lifeContext(t)}, ask)
		if err == nil {
			t.Fatalf("lookup %d handed over the backup's SERVFAIL (%d bytes) with no error: once the dead primary cools down the fallback must still run", i, len(resp))
		}
	}
	if !SourceCooling("dead") {
		t.Fatal("the dead primary never cooled down, so the test did not reach the lookups it is about")
	}

	resp, source, err := AskServers(context.Background(), servers, Options{Life: lifeContext(t), Strict: true}, ask)
	if err == nil {
		t.Fatalf("a strict set got %d bytes from %q with no error, want the primary's failure", len(resp), source)
	}
}

func TestAShortReplyIsNotAnAnswer(t *testing.T) {
	resetHealth(t)
	short := func(context.Context, Server) ([]byte, error) { return []byte{1, 2, 3}, nil }
	for _, servers := range [][]Server{
		{{Source: "one"}},
		{{Source: "one"}, {Source: "two"}},
	} {
		resp, _, err := AskServers(context.Background(), servers, Options{}, short)
		if err == nil || len(resp) != 0 {
			t.Fatalf("servers %d: got %d bytes and %v, want a failure", len(servers), len(resp), err)
		}
	}

	ResetSourceHealth()
	good := goodAnswer(net.IPv4(203, 0, 113, 29))
	resp, source, err := AskServers(context.Background(), []Server{{Source: "short"}, {Source: "good"}}, Options{},
		func(_ context.Context, srv Server) ([]byte, error) {
			if srv.Source == "short" {
				return []byte{1, 2, 3}, nil
			}
			return good, nil
		})
	if err != nil || source != "good" || len(resp) != len(good) {
		t.Fatalf("got %d bytes from %q and %v, want the next resolver's answer after a short reply", len(resp), source, err)
	}
	sourceMu.Lock()
	state := sourceHealth["short"]
	sourceMu.Unlock()
	if state == nil || state.failures != 1 {
		t.Fatalf("the short reply was recorded as %+v, want one failure", state)
	}
}

func TestBudgets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		servers []Server
		def     time.Duration
		want    time.Duration
	}{
		{"no set, as on main", nil, 5 * time.Second, 2*5*time.Second + FallbackBudget},
		{"no set and no timeout given", nil, 0, 2*defaultLookupTimeout + FallbackBudget},
		{"one resolver, as on main", []Server{{Timeout: 2 * time.Second}}, 2 * time.Second, 2*2*time.Second + FallbackBudget},
		{"two resolvers add one head start", []Server{{Timeout: 5 * time.Second}, {Timeout: 5 * time.Second}}, 5 * time.Second, 2*5*time.Second + FallbackBudget + HeadStart},
		{"eight resolvers add seven", make([]Server, 8), 5 * time.Second, 2*5*time.Second + FallbackBudget + 7*HeadStart},
	} {
		if got := ServersBudget(tc.servers, tc.def); got != tc.want {
			t.Errorf("%s: budget is %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestOneResolverIsAskedExactlyOnce(t *testing.T) {
	resetHealth(t)
	var asked int32
	srv := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 25)), &asked)
	srv.Source = "only"

	if _, err := LookupWithFallback(context.Background(), LookupIPs, []Server{srv}, Options{Life: lifeContext(t)}, true, "example.test", true, false); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got := atomic.LoadInt32(&asked); got != 1 {
		t.Fatalf("the resolver was asked %d times, want 1: a set without the list must behave as before", got)
	}
}

func TestLookupsThatLeaveALoserBehindLeaveNoGoroutines(t *testing.T) {
	resetHealth(t)
	var started int32
	slow := startAnswering(t, silent, &started)
	slow.Timeout = 500 * time.Millisecond
	slow.Source = "slow"
	good := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 27)), nil)
	good.Source = "fast"
	life := lifeContext(t)

	const lookups = 20
	var wg sync.WaitGroup
	errs := make(chan error, lookups)
	for i := 0; i < lookups; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := LookupWithFallback(context.Background(), LookupIPs, []Server{slow, good}, Options{Life: life}, true, "example.test", true, false)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("lookup: %v", err)
		}
	}
	if got := atomic.LoadInt32(&started); got != lookups {
		t.Fatalf("the slow resolver was asked %d times, want %d: every lookup must leave a loser behind for the leak guard to judge", got, lookups)
	}
}

func TestACallerThatLeavesEndsItsOnlyResolversQuery(t *testing.T) {
	resetHealth(t)
	NoteSourceFailure("only")
	NoteSourceFailure("only")
	callCtx, cancelCall := context.WithCancel(context.Background())
	entered := make(chan struct{})
	ended := make(chan struct{})
	returned := make(chan error, 1)
	go func() {
		_, _, err := AskServers(callCtx, []Server{{Source: "only", Timeout: 5 * time.Second}}, Options{Life: lifeContext(t)},
			func(sctx context.Context, _ Server) ([]byte, error) {
				close(entered)
				<-sctx.Done()
				close(ended)
				return nil, sctx.Err()
			})
		returned <- err
	}()

	<-entered
	cancelCall()
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("the query to a set's only resolver outlived the caller that left: without a list nothing else is waiting on it")
	}
	if err := <-returned; !errors.Is(err, context.Canceled) {
		t.Fatalf("the caller got %v, want its own cancellation", err)
	}
	if left := asksLeft("only", time.Second); left != 0 {
		t.Fatalf("%d asks are still counted after the query ended", left)
	}
	if SourceCooling("only") {
		t.Fatal("a query the caller cut was counted as the resolver's third failure")
	}
}

func TestStartedEntriesOfAListRunOnTheServiceLifetime(t *testing.T) {
	resetHealth(t)
	NoteSourceFailure("one")
	NoteSourceFailure("one")
	life, stopService := context.WithCancel(context.Background())
	defer stopService()
	callCtx, cancelCall := context.WithCancel(context.Background())

	entered := make(chan struct{})
	ended := make(chan error, 1)
	returned := make(chan error, 1)
	go func() {
		servers := []Server{{Source: "one", Timeout: 5 * time.Second}, {Source: "two", Timeout: 5 * time.Second}}
		_, _, err := AskServers(callCtx, servers, Options{Life: life},
			func(sctx context.Context, srv Server) ([]byte, error) {
				if srv.Source == "one" {
					close(entered)
				}
				<-sctx.Done()
				if srv.Source == "one" {
					ended <- sctx.Err()
				}
				return nil, sctx.Err()
			})
		returned <- err
	}()

	<-entered
	cancelCall()
	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the caller got %v, want its own cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("the caller's cancellation did not end its wait")
	}
	select {
	case err := <-ended:
		t.Fatalf("the started entry ended with %v when the caller left; it must run on the service lifetime", err)
	case <-time.After(150 * time.Millisecond):
	}

	stopService()
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("the started entry outlived the service")
	}
	if SourceCooling("one") || asksLeft("one", time.Second) != 0 {
		t.Fatal("an entry the service cancelled was counted against the resolver or left in flight")
	}
}

func asksLeft(source string, wait time.Duration) int {
	deadline := time.Now().Add(wait)
	for asksInFlight(source) != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return asksInFlight(source)
}

func TestACallerThatCancelsRightAfterTheCallStillCoolsTheResolver(t *testing.T) {
	resetHealth(t)
	life := lifeContext(t)
	silentSrv := startAnswering(t, silent, nil)
	silentSrv.Timeout = time.Second
	silentSrv.Source = "silent"
	backup := startAnswering(t, answeringWith(net.IPv4(203, 0, 113, 28)), nil)
	backup.Source = "backup"

	for i := 0; i < SourceFailuresToTrip; i++ {
		callCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := LookupWithFallback(callCtx, LookupIPs, []Server{silentSrv, backup}, Options{Life: life}, true, "example.test", true, false)
		cancel()
		if err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for !SourceCooling("silent") {
		if time.Now().After(deadline) {
			t.Fatal("a caller that cancels right after the call left the silent resolver uncooled")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAnEntryThatIsNotAskedKeepsItsRecoveryAttempt(t *testing.T) {
	resetHealth(t)
	life := lifeContext(t)
	servers := []Server{{Source: "primary", Timeout: time.Second}, {Source: "backup", Timeout: time.Second}}
	fromPrimary, fromBackup := goodAnswer(net.IPv4(203, 0, 113, 30)), goodAnswer(net.IPv4(203, 0, 113, 31))
	primaryUp := true
	ask := func(_ context.Context, srv Server) ([]byte, error) {
		switch {
		case srv.Source == "backup":
			return fromBackup, nil
		case primaryUp:
			return fromPrimary, nil
		}
		return nil, errors.New("no reply")
	}

	coolDown("backup")
	expireCooldown("backup")

	if _, source, err := AskServers(context.Background(), servers, Options{Life: life}, ask); err != nil || source != "primary" {
		t.Fatalf("first lookup: answered by %q with %v, want the primary", source, err)
	}

	primaryUp = false
	if _, source, err := AskServers(context.Background(), servers, Options{Life: life}, ask); err != nil || source != "backup" {
		t.Fatalf("second lookup: answered by %q with %v, want the recovered backup: a lookup that never asked it must not spend its recovery attempt", source, err)
	}
}
