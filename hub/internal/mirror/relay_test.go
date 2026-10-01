package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

type gate struct {
	down *atomic.Bool
	next http.RoundTripper
}

func (g gate) RoundTrip(req *http.Request) (*http.Response, error) {
	if g.down.Load() {
		return nil, errors.New("connection refused")
	}
	return g.next.RoundTrip(req)
}

type upstreamLog struct {
	mu       sync.Mutex
	received []hubwire.Record
	agents   []string
	via      []string
}

func (u *upstreamLog) add(rec hubwire.Record, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.received = append(u.received, rec)
	u.agents = append(u.agents, r.UserAgent())
	u.via = append(u.via, r.Header.Get(HeaderVia))
}

func (u *upstreamLog) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.received)
}

func acceptingUpstream(t *testing.T, log *upstreamLog) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var rec hubwire.Record
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Errorf("upstream received a body that does not decode: %v", err)
		}
		log.add(rec, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{"id":%q,"kind":%q,"upstream":true}`, rec.ID(), rec.Kind)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func voteFor(t *testing.T, id *hubwire.Identity, now time.Time) []byte {
	t.Helper()
	return testkit.Sign(t, id, hubwire.RecordVote, hubwire.VoteBody{SetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Version: 1, FP: "fp", Kind: hubwire.VoteWorks}, now)
}

func decode(t *testing.T, a *Answer) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(a.Body, &body); err != nil {
		t.Fatalf("answer %d does not decode: %v", a.Status, err)
	}
	return body
}

func TestRelayForwardsAndQueuesWhileUpstreamIsDown(t *testing.T) {
	var down atomic.Bool
	log := &upstreamLog{}
	upstream := acceptingUpstream(t, log)
	c := &clock{now: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)}
	dir := filepath.Join(t.TempDir(), "relay")
	relay := &Relay{Upstream: upstream.URL, Dir: dir, Now: c.Now, Node: "node-a", Client: &http.Client{Transport: gate{down: &down, next: http.DefaultTransport}}}
	ctx := context.Background()
	voter := testkit.Identity(t)

	answer := relay.Handle(ctx, voteFor(t, voter, c.Now()), nil, "")
	if answer.Status != http.StatusAccepted || !strings.Contains(string(answer.Body), `"upstream":true`) {
		t.Fatalf("a reachable upstream must answer through the relay unchanged, got %d %s", answer.Status, answer.Body)
	}
	if log.agents[0] != ingest.RelayAgent || log.via[0] != "node-a" {
		t.Fatalf("a relayed record must say it comes from a mirror and name the hop, got %q %q", log.agents[0], log.via[0])
	}

	down.Store(true)
	queuedRaw := voteFor(t, voter, c.Now())
	var queuedRec hubwire.Record
	_ = json.Unmarshal(queuedRaw, &queuedRec)
	answer = relay.Handle(ctx, queuedRaw, nil, "")
	body := decode(t, answer)
	if answer.Status != http.StatusAccepted || body["queued"] != true || body["id"] != queuedRec.ID() || body["kind"] != hubwire.RecordVote {
		t.Fatalf("a vote must be queued while the upstream is down, got %d %v", answer.Status, body)
	}
	info, err := os.Stat(filepath.Join(dir, queuedRec.ID()+".json"))
	if err != nil {
		t.Fatalf("queued record file missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("queued records hold router keys and votes and must be private, got %v", info.Mode().Perm())
	}

	share := testkit.Sign(t, voter, hubwire.RecordShare, hubwire.ShareBody{}, c.Now())
	answer = relay.Handle(ctx, share, nil, "")
	if answer.Status != http.StatusBadGateway || !strings.Contains(string(answer.Body), CodeHubUnreachable) || answer.RetryAfter == "" {
		t.Fatalf("a share must be refused with a retry hint while the upstream is down, got %d %s %q", answer.Status, answer.Body, answer.RetryAfter)
	}
	if relay.Queued() != 1 {
		t.Fatalf("only the vote may be queued, got %d", relay.Queued())
	}
	if err := relay.Retry(ctx); err == nil {
		t.Fatal("retry must report the upstream as unreachable")
	}
	if relay.Queued() != 1 {
		t.Fatalf("a failed retry must keep the record, got %d queued", relay.Queued())
	}

	down.Store(false)
	c.Add(breakerMaxBackoff)
	if err := relay.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if relay.Queued() != 0 {
		t.Fatalf("delivered records must leave the queue, got %d", relay.Queued())
	}
	if log.count() != 2 || log.received[1].ID() != queuedRec.ID() {
		t.Fatalf("upstream must receive the queued vote verbatim, got %d records", log.count())
	}
}

func TestRelayRefusesBadRecordsWithoutForwardingThem(t *testing.T) {
	log := &upstreamLog{}
	upstream := acceptingUpstream(t, log)
	relay := &Relay{Upstream: upstream.URL, Dir: filepath.Join(t.TempDir(), "relay")}
	ctx := context.Background()
	voter := testkit.Identity(t)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	expectRefused := func(name string, raw []byte, status int, code string) {
		t.Helper()
		answer := relay.Handle(ctx, raw, nil, "")
		if answer.Status != status || decode(t, answer)["code"] != code {
			t.Errorf("%s: got %d %s, want %d %s", name, answer.Status, answer.Body, status, code)
		}
	}
	expectRefused("junk", []byte("{not json"), http.StatusBadRequest, ingest.CodeBadRecord)

	edit := func(raw []byte, change func(*hubwire.Record)) []byte {
		var rec hubwire.Record
		_ = json.Unmarshal(raw, &rec)
		change(&rec)
		out, _ := json.Marshal(rec)
		return out
	}
	vote := voteFor(t, voter, now)
	expectRefused("tampered", edit(vote, func(r *hubwire.Record) { r.TS++ }), http.StatusBadRequest, ingest.CodeBadSignature)
	expectRefused("newline in the signature", edit(vote, func(r *hubwire.Record) { r.Sig = r.Sig[:4] + "\n" + r.Sig[4:] }), http.StatusBadRequest, ingest.CodeBadSignature)

	expectRefused("no wire version", []byte(`{"kind":"vote"}`), http.StatusBadRequest, ingest.CodeBadRecord)

	if log.count() != 0 {
		t.Fatalf("refused records must never reach the upstream, %d did", log.count())
	}
	if answer := relay.Handle(ctx, vote, nil, ""); answer.Status != http.StatusAccepted {
		t.Fatalf("the untouched record still goes through, got %d", answer.Status)
	}
}

func TestRelayRefusesALoop(t *testing.T) {
	log := &upstreamLog{}
	upstream := acceptingUpstream(t, log)
	relay := &Relay{Upstream: upstream.URL, Dir: filepath.Join(t.TempDir(), "relay"), Node: "node-a"}
	ctx := context.Background()
	vote := voteFor(t, testkit.Identity(t), time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))

	for _, via := range []string{"node-a", "other, node-a", strings.Repeat("hop,", MaxHops)} {
		answer := relay.Handle(ctx, vote, nil, via)
		if answer.Status != http.StatusLoopDetected || decode(t, answer)["code"] != CodeRelayLoop {
			t.Errorf("via %q: got %d %s", via, answer.Status, answer.Body)
		}
	}
	if log.count() != 0 {
		t.Fatal("a looping record must not be forwarded")
	}
	if answer := relay.Handle(ctx, vote, nil, "node-b"); answer.Status != http.StatusAccepted || log.via[0] != "node-b,node-a" {
		t.Fatalf("a record from another mirror is passed on with this hop added, got %d via %v", answer.Status, log.via)
	}
}

func TestRelayAnswersAtOnceWhileTheHubIsDown(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)
	c := &clock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	relay := &Relay{Upstream: upstream.URL, Dir: filepath.Join(t.TempDir(), "relay"), Now: c.Now}
	ctx := context.Background()
	voter := testkit.Identity(t)

	for i := 0; i < breakerThreshold; i++ {
		if answer := relay.Handle(ctx, voteFor(t, voter, c.Now()), nil, ""); answer.Status != http.StatusAccepted {
			t.Fatalf("a vote is queued when the hub fails, got %d", answer.Status)
		}
	}
	if calls.Load() != breakerThreshold {
		t.Fatalf("each attempt before the breaker opens reaches the hub, got %d", calls.Load())
	}
	if answer := relay.Handle(ctx, voteFor(t, voter, c.Now()), nil, ""); answer.Status != http.StatusAccepted || decode(t, answer)["queued"] != true {
		t.Fatalf("with the breaker open a vote is queued at once, got %d %s", answer.Status, answer.Body)
	}
	share := testkit.Sign(t, voter, hubwire.RecordShare, hubwire.ShareBody{}, c.Now())
	if answer := relay.Handle(ctx, share, nil, ""); answer.Status != http.StatusBadGateway {
		t.Fatalf("with the breaker open a share is refused at once, got %d", answer.Status)
	}
	if calls.Load() != breakerThreshold {
		t.Fatalf("an open breaker must not wait on the hub, got %d calls", calls.Load())
	}
	if err := relay.Retry(ctx); err == nil || calls.Load() != breakerThreshold {
		t.Fatalf("the drain waits while the breaker is open, got %v after %d calls", err, calls.Load())
	}
	c.Add(breakerMinBackoff)
	relay.Handle(ctx, voteFor(t, voter, c.Now()), nil, "")
	if calls.Load() != breakerThreshold+1 {
		t.Fatalf("after the backoff one record probes the hub, got %d calls", calls.Load())
	}
	relay.Handle(ctx, voteFor(t, voter, c.Now()), nil, "")
	if calls.Load() != breakerThreshold+1 {
		t.Fatalf("a failed probe reopens the breaker, got %d calls", calls.Load())
	}
}

func TestARouterHangingUpIsNotAHubFailure(t *testing.T) {
	log := &upstreamLog{}
	upstream := acceptingUpstream(t, log)
	relay := &Relay{Upstream: upstream.URL, Dir: filepath.Join(t.TempDir(), "relay")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	answer := relay.Handle(ctx, voteFor(t, testkit.Identity(t), time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)), nil, "")
	if answer.Status != http.StatusAccepted || log.count() != 1 || relay.Queued() != 0 {
		t.Fatalf("the record must still reach the hub after the router hung up, got %d, %d forwarded, %d queued", answer.Status, log.count(), relay.Queued())
	}
	relay.breaker.mu.Lock()
	failures := relay.breaker.failures
	relay.breaker.mu.Unlock()
	if failures != 0 {
		t.Fatalf("a hang-up must not count against the hub, got %d failures", failures)
	}
}

func TestRelayLimitsEachClient(t *testing.T) {
	log := &upstreamLog{}
	upstream := acceptingUpstream(t, log)
	relay := &Relay{Upstream: upstream.URL, Dir: filepath.Join(t.TempDir(), "relay")}
	ctx := context.Background()
	vote := voteFor(t, testkit.Identity(t), time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	client := net.ParseIP("203.0.113.9")
	for i := 0; i < ClientRequestsPerHour; i++ {
		if answer := relay.Handle(ctx, vote, client, ""); answer.Status != http.StatusAccepted {
			t.Fatalf("request %d within the limit was refused: %d", i, answer.Status)
		}
	}
	answer := relay.Handle(ctx, vote, client, "")
	body := decode(t, answer)
	if answer.Status != http.StatusTooManyRequests || body["scope"] != "request" || answer.RetryAfter == "" {
		t.Fatalf("a client over its hourly budget must be told to retry, got %d %v %q", answer.Status, body, answer.RetryAfter)
	}
	if answer := relay.Handle(ctx, vote, net.ParseIP("198.51.100.20"), ""); answer.Status != http.StatusAccepted {
		t.Fatalf("other networks keep their own budget, got %d", answer.Status)
	}
	if answer := relay.Handle(ctx, vote, net.ParseIP("10.84.0.1"), ""); answer.Status != http.StatusAccepted {
		t.Fatalf("a private address is a misconfigured proxy, not a client to limit, got %d", answer.Status)
	}
}

func TestTheQueueIsCapped(t *testing.T) {
	var down atomic.Bool
	down.Store(true)
	relay := &Relay{Upstream: "http://hub.invalid", Dir: filepath.Join(t.TempDir(), "relay"), QueueLimit: 2, Client: &http.Client{Transport: gate{down: &down, next: http.DefaultTransport}}}
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if answer := relay.Handle(ctx, voteFor(t, testkit.Identity(t), now), nil, ""); answer.Status != http.StatusAccepted {
			t.Fatalf("record %d fits the queue, got %d", i, answer.Status)
		}
	}
	answer := relay.Handle(ctx, voteFor(t, testkit.Identity(t), now), nil, "")
	if answer.Status != http.StatusServiceUnavailable || decode(t, answer)["code"] != CodeQueueFull {
		t.Fatalf("a full queue must refuse so the router keeps its copy, got %d %s", answer.Status, answer.Body)
	}

	perKey := &Relay{Upstream: "http://hub.invalid", Dir: filepath.Join(t.TempDir(), "relay"), Client: relay.Client}
	voter := testkit.Identity(t)
	for i := 0; i < QueuedPerKeyPerDay; i++ {
		if answer := perKey.Handle(ctx, voteFor(t, voter, now), nil, ""); answer.Status != http.StatusAccepted {
			t.Fatalf("record %d of one key fits its daily share, got %d", i, answer.Status)
		}
	}
	if answer := perKey.Handle(ctx, voteFor(t, voter, now), nil, ""); answer.Status != http.StatusBadGateway {
		t.Fatalf("one key cannot fill the queue, got %d", answer.Status)
	}

	off := &Relay{Upstream: "http://hub.invalid", Dir: filepath.Join(t.TempDir(), "relay"), QueueLimit: -1, Client: relay.Client}
	if answer := off.Handle(ctx, voteFor(t, testkit.Identity(t), now), nil, ""); answer.Status != http.StatusBadGateway || off.Queued() != 0 {
		t.Fatalf("with the queue off nothing is held, got %d and %d queued", answer.Status, off.Queued())
	}
}

func TestTheDrainGoesOldestFirstAndSkipsWhatTheHubDefers(t *testing.T) {
	var mu sync.Mutex
	var order []string
	deferID := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var rec hubwire.Record
		_ = json.Unmarshal(raw, &rec)
		mu.Lock()
		order = append(order, rec.ID())
		skip := rec.ID() == deferID
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if skip {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"rate_limited","scope":"vote","retry_after":3600}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(upstream.Close)
	c := &clock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	dir := filepath.Join(t.TempDir(), "relay")
	relay := &Relay{Upstream: upstream.URL, Dir: dir, Now: c.Now}
	ids := make([]string, 3)
	for i := range ids {
		raw := voteFor(t, testkit.Identity(t), c.Now())
		var rec hubwire.Record
		_ = json.Unmarshal(raw, &rec)
		ids[i] = rec.ID()
		if err := relay.enqueue(ids[i], raw); err != nil {
			t.Fatal(err)
		}
		at := c.Now().Add(time.Duration(i-3) * time.Hour)
		if err := os.Chtimes(filepath.Join(dir, ids[i]+".json"), at, at); err != nil {
			t.Fatal(err)
		}
	}
	deferID = ids[0]

	if err := relay.Retry(context.Background()); err != nil {
		t.Fatalf("a per-key limit must not stop the drain: %v", err)
	}
	mu.Lock()
	got := append([]string{}, order...)
	mu.Unlock()
	if len(got) != 3 || got[0] != ids[0] || got[1] != ids[1] || got[2] != ids[2] {
		t.Fatalf("the drain must go oldest first, got %v want %v", got, ids)
	}
	if relay.Queued() != 1 {
		t.Fatalf("only the deferred record stays, got %d", relay.Queued())
	}
	if err := relay.Retry(context.Background()); err != nil || len(order) != 3 {
		t.Fatalf("a deferred record waits for its retry_after, got %v after %d sends", err, len(order))
	}
	c.Add(2 * time.Hour)
	mu.Lock()
	deferID = ""
	mu.Unlock()
	if err := relay.Retry(context.Background()); err != nil || relay.Queued() != 0 {
		t.Fatalf("the deferred record goes once its wait is over, got %v with %d queued", err, relay.Queued())
	}
}

func TestRetryKeepsRecordsTheHubDidNotAccept(t *testing.T) {
	var status atomic.Int32
	var body atomic.Value
	body.Store(`{"code":"test"}`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	defer upstream.Close()

	c := &clock{now: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)}
	dir := filepath.Join(t.TempDir(), "relay")
	relay := &Relay{Upstream: upstream.URL, Dir: dir, Now: c.Now}
	ctx := context.Background()
	raw := voteFor(t, testkit.Identity(t), c.Now())
	var rec hubwire.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if err := relay.enqueue(rec.ID(), raw); err != nil {
		t.Fatal(err)
	}

	body.Store(`{"code":"rate_limited","scope":"request","retry_after":60}`)
	for _, code := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		if err := relay.Retry(ctx); err == nil {
			t.Fatalf("a %d answer must pause the drain", code)
		}
		if relay.Queued() != 1 {
			t.Fatalf("a %d answer must keep the record, got %d queued", code, relay.Queued())
		}
		c.Add(breakerMaxBackoff)
	}

	body.Store(`{"code":"bad_record"}`)
	status.Store(http.StatusBadRequest)
	if err := relay.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if relay.Queued() != 0 {
		t.Fatalf("a refused record must leave the queue, got %d queued", relay.Queued())
	}
}

func TestAMirrorRefusesToRelayToItself(t *testing.T) {
	if _, err := New(Options{Upstream: "https://m1.hub.example/", PublicURL: "https://M1.hub.example", Layout: hubdata.Layout{Root: t.TempDir()}}); err == nil {
		t.Fatal("a mirror whose upstream is its own public url must not start")
	}

	svc, err := New(Options{Upstream: "http://placeholder.invalid", Layout: hubdata.Layout{Root: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	self := httptest.NewServer(svc.Router())
	t.Cleanup(self.Close)
	svc.opts.Upstream = self.URL
	svc.relay.Upstream = self.URL

	if err := svc.Refresh(context.Background()); !errors.Is(err, errSelfUpstream) {
		t.Fatalf("an upstream answering as this mirror must be named as such, got %v", err)
	}
	answer := svc.relay.Handle(context.Background(), voteFor(t, testkit.Identity(t), time.Now()), nil, "")
	if answer.Status != http.StatusLoopDetected {
		t.Fatalf("a record relayed back to the same mirror must stop at once, got %d %s", answer.Status, answer.Body)
	}
	if svc.relay.Queued() != 0 {
		t.Fatal("a loop must not fill the queue")
	}
}

func TestAnOversizedReportIsPassedOnButNeverQueued(t *testing.T) {
	var down atomic.Bool
	log := &upstreamLog{}
	upstream := acceptingUpstream(t, log)
	relay := &Relay{Upstream: upstream.URL, Dir: filepath.Join(t.TempDir(), "relay"), Client: &http.Client{Transport: gate{down: &down, next: http.DefaultTransport}}}
	ctx := context.Background()
	big := testkit.Sign(t, testkit.Identity(t), hubwire.RecordReport, hubwire.ReportBody{SetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Version: 1, Reason: strings.Repeat("x", MaxQueuedRecord)}, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	if answer := relay.Handle(ctx, big, nil, ""); answer.Status != http.StatusAccepted || log.count() != 1 {
		t.Fatalf("a large record goes to the hub, which decides, got %d after %d forwards", answer.Status, log.count())
	}
	down.Store(true)
	if answer := relay.Handle(ctx, big, nil, ""); answer.Status != http.StatusBadGateway || relay.Queued() != 0 {
		t.Fatalf("a record too large to queue is refused so the router keeps it, got %d with %d queued", answer.Status, relay.Queued())
	}
}

func TestConcurrentFailuresDoNotStretchTheBackoff(t *testing.T) {
	const inFlight = 8
	c := &clock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	var arrived sync.WaitGroup
	arrived.Add(inFlight)
	release := make(chan struct{})
	var once sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrived.Done()
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)
	relay := &Relay{Upstream: upstream.URL, Dir: filepath.Join(t.TempDir(), "relay"), Now: c.Now}
	var done sync.WaitGroup
	for i := 0; i < inFlight; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			share := testkit.Sign(t, testkit.Identity(t), hubwire.RecordShare, hubwire.ShareBody{}, c.Now())
			relay.Handle(context.Background(), share, nil, "")
		}()
	}
	arrived.Wait()
	c.Add(time.Second)
	once.Do(func() { close(release) })
	done.Wait()
	relay.breaker.mu.Lock()
	backoff := relay.breaker.backoff
	relay.breaker.mu.Unlock()
	if backoff != breakerMinBackoff {
		t.Fatalf("failures of requests already in flight when the breaker opened must not stretch it, got %s", backoff)
	}

	var b breaker
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	b.failure(t0, t0.Add(time.Second), false)
	b.failure(t0, t0.Add(time.Second), false)
	if ok, _ := b.allow(t0.Add(10 * time.Second)); ok {
		t.Fatal("two failures open the breaker")
	}
	ok, probe := b.allow(t0.Add(time.Second + breakerMinBackoff))
	if !ok || !probe {
		t.Fatal("after the backoff one request probes")
	}
	if again, _ := b.allow(t0.Add(time.Second + breakerMinBackoff)); again {
		t.Fatal("only one probe at a time")
	}
	b.failure(t0.Add(time.Second+breakerMinBackoff), t0.Add(2*time.Second+breakerMinBackoff), true)
	if b.backoff != 2*breakerMinBackoff {
		t.Fatalf("a failed probe doubles the backoff, got %s", b.backoff)
	}
}

func TestALoopThroughAnOlderMirrorStopsAtTheRepeatedRecord(t *testing.T) {
	var target string
	var hops atomic.Int32
	older := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops.Add(1)
		raw, _ := io.ReadAll(r.Body)
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, target+hubwire.PathMessage, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", ingest.RelayAgent)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
	}))
	t.Cleanup(older.Close)
	svc, err := New(Options{Upstream: older.URL, Layout: hubdata.Layout{Root: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	self := httptest.NewServer(svc.Router())
	t.Cleanup(self.Close)
	target = self.URL

	answer := svc.relay.Handle(context.Background(), voteFor(t, testkit.Identity(t), time.Now()), nil, "")
	if answer.Status != http.StatusLoopDetected || !strings.Contains(string(answer.Body), CodeRelayLoop) {
		t.Fatalf("a record that comes back while it is still on its way must stop, got %d %s", answer.Status, answer.Body)
	}
	if hops.Load() != 1 || svc.relay.Queued() != 0 {
		t.Fatalf("the loop must end after one round trip with nothing queued, got %d hops and %d queued", hops.Load(), svc.relay.Queued())
	}
}

func TestAFutureWireVersionIsPassedOnUnchecked(t *testing.T) {
	var down atomic.Bool
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded.Add(1)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(upstream.Close)
	relay := &Relay{Upstream: upstream.URL, Dir: filepath.Join(t.TempDir(), "relay"), Client: &http.Client{Transport: gate{down: &down, next: http.DefaultTransport}}}
	future := []byte(`{"v":2,"kind":"vote","key":"k","ts":"2026-10-01T12:00:00Z","nonce":"n","body":{},"sig":"s"}`)
	if answer := relay.Handle(context.Background(), future, nil, ""); answer.Status != http.StatusAccepted || forwarded.Load() != 1 {
		t.Fatalf("a record of a later wire version goes to the hub unchecked, got %d after %d forwards", answer.Status, forwarded.Load())
	}
	down.Store(true)
	if answer := relay.Handle(context.Background(), future, nil, ""); answer.Status != http.StatusBadGateway || relay.Queued() != 0 {
		t.Fatalf("an unchecked record is never queued, got %d with %d queued", answer.Status, relay.Queued())
	}
}

func TestTheHubsLimitOnTheMirrorQueuesVotesAndSendsSharesElsewhere(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"rate_limited","scope":"request","retry_after":600}`))
	}))
	t.Cleanup(upstream.Close)
	c := &clock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	relay := &Relay{Upstream: upstream.URL, Dir: filepath.Join(t.TempDir(), "relay"), Now: c.Now}
	ctx := context.Background()
	voter := testkit.Identity(t)

	if answer := relay.Handle(ctx, voteFor(t, voter, c.Now()), nil, ""); answer.Status != http.StatusAccepted || decode(t, answer)["queued"] != true {
		t.Fatalf("a vote waits in the queue while the hub refuses this mirror, got %d %s", answer.Status, answer.Body)
	}
	share := testkit.Sign(t, voter, hubwire.RecordShare, hubwire.ShareBody{}, c.Now())
	answer := relay.Handle(ctx, share, nil, "")
	if answer.Status != http.StatusServiceUnavailable || decode(t, answer)["code"] != CodeHubBusy || answer.RetryAfter != "600" {
		t.Fatalf("a share gets a 5xx so the router tries its next address, got %d %s %q", answer.Status, answer.Body, answer.RetryAfter)
	}
	before := calls.Load()
	if err := relay.Retry(ctx); err == nil || calls.Load() != before {
		t.Fatalf("the drain waits out the hub's limit on this mirror, got %v after %d calls", err, calls.Load()-before)
	}
}

func TestTheDrainSkipsNewKeysWhileTheHubRefusesThem(t *testing.T) {
	refuse := ""
	var mu sync.Mutex
	var sent []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var rec hubwire.Record
		_ = json.Unmarshal(raw, &rec)
		mu.Lock()
		sent = append(sent, rec.Key)
		refused := rec.Key == refuse
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if refused {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"rate_limited","scope":"newkey","retry_after":3600}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(upstream.Close)
	c := &clock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	dir := filepath.Join(t.TempDir(), "relay")
	relay := &Relay{Upstream: upstream.URL, Dir: dir, Now: c.Now}
	ctx := context.Background()
	fresh, other, regular := testkit.Identity(t), testkit.Identity(t), testkit.Identity(t)
	if answer := relay.Handle(ctx, voteFor(t, regular, c.Now()), nil, ""); answer.Status != http.StatusAccepted {
		t.Fatalf("a regular router's vote: %d", answer.Status)
	}
	refuse = fresh.KeyID()
	mu.Lock()
	sent = nil
	mu.Unlock()
	for i, id := range []*hubwire.Identity{fresh, other, regular} {
		raw := voteFor(t, id, c.Now())
		var rec hubwire.Record
		_ = json.Unmarshal(raw, &rec)
		if err := relay.enqueue(rec.ID(), raw); err != nil {
			t.Fatal(err)
		}
		at := c.Now().Add(time.Duration(i-3) * time.Hour)
		if err := os.Chtimes(filepath.Join(dir, rec.ID()+".json"), at, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := relay.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string{}, sent...)
	mu.Unlock()
	if len(got) != 2 || got[0] != fresh.KeyID() || got[1] != regular.KeyID() {
		t.Fatalf("after the hub refuses a new key, only keys it already accepted are sent, got %v", got)
	}
	if relay.Queued() != 2 {
		t.Fatalf("the two new keys stay queued, got %d", relay.Queued())
	}
}

func TestPrepareTightensAQueueLeftByAnOlderMirror(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "relay")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "0123.json")
	if err := os.WriteFile(old, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	(&Relay{Dir: dir}).Prepare()
	dirInfo, _ := os.Stat(dir)
	fileInfo, _ := os.Stat(old)
	if dirInfo.Mode().Perm() != 0o700 || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("an upgraded queue must be private, got dir %v file %v", dirInfo.Mode().Perm(), fileInfo.Mode().Perm())
	}
}

func TestSpecialPurposeAddressesAreNotClients(t *testing.T) {
	for addr, want := range map[string]bool{"203.0.113.9": true, "2001:db8::1": true, "100.64.1.1": false, "198.18.0.1": false, "10.84.0.1": false, "127.0.0.1": false} {
		if got := publicClient(net.ParseIP(addr)); got != want {
			t.Errorf("%s: got %v, want %v", addr, got, want)
		}
	}
}

func TestRelayedRecordsCarryTheMirrorSignature(t *testing.T) {
	var header atomic.Value
	var body atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		header.Store(r.Header.Get(hubdata.HeaderRelay))
		body.Store(raw)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(upstream.Close)
	mirror := testkit.Identity(t)
	relay := &Relay{Upstream: upstream.URL, Dir: filepath.Join(t.TempDir(), "relay"), Identity: mirror}
	relay.Handle(context.Background(), voteFor(t, testkit.Identity(t), time.Now()), nil, "")
	keyID, ok := hubdata.VerifyRelay(header.Load().(string), body.Load().([]byte), time.Now())
	if !ok || keyID != mirror.KeyID() {
		t.Fatalf("a mirror with an identity must sign what it passes on, got %q %v", keyID, ok)
	}
}
