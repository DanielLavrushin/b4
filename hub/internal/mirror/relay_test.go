package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func TestRelayForwardsAndQueuesWhileUpstreamIsDown(t *testing.T) {
	var down atomic.Bool
	var mu sync.Mutex
	var received []hubwire.Record
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var rec hubwire.Record
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Errorf("upstream received a body that does not decode: %v", err)
		}
		mu.Lock()
		received = append(received, rec)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{"id":%q,"kind":%q,"upstream":true}`, rec.ID(), rec.Kind)
	}))
	defer upstream.Close()

	dir := filepath.Join(t.TempDir(), "relay")
	relay := &Relay{Upstream: upstream.URL, Dir: dir, Client: &http.Client{Transport: gate{down: &down, next: http.DefaultTransport}}}
	ctx := context.Background()
	voter := testkit.Identity(t)
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	vote := func() []byte {
		return testkit.Sign(t, voter, hubwire.RecordVote, hubwire.VoteBody{SetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Version: 1, FP: "fp", Kind: hubwire.VoteWorks}, now)
	}

	answer := relay.Handle(ctx, vote())
	if answer.Status != http.StatusAccepted || !strings.Contains(string(answer.Body), `"upstream":true`) {
		t.Fatalf("a reachable upstream must answer through the relay unchanged, got %d %s", answer.Status, answer.Body)
	}

	down.Store(true)
	queuedRaw := vote()
	var queuedRec hubwire.Record
	_ = json.Unmarshal(queuedRaw, &queuedRec)
	answer = relay.Handle(ctx, queuedRaw)
	var body map[string]interface{}
	if err := json.Unmarshal(answer.Body, &body); err != nil {
		t.Fatal(err)
	}
	if answer.Status != http.StatusAccepted || body["queued"] != true || body["id"] != queuedRec.ID() || body["kind"] != hubwire.RecordVote {
		t.Fatalf("a vote must be queued while the upstream is down, got %d %v", answer.Status, body)
	}
	if _, err := os.Stat(filepath.Join(dir, queuedRec.ID()+".json")); err != nil {
		t.Fatalf("queued record file missing: %v", err)
	}

	share := testkit.Sign(t, voter, hubwire.RecordShare, hubwire.ShareBody{}, now)
	answer = relay.Handle(ctx, share)
	if answer.Status != http.StatusBadGateway || !strings.Contains(string(answer.Body), CodeHubUnreachable) {
		t.Fatalf("a share must be refused while the upstream is down, got %d %s", answer.Status, answer.Body)
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
	if err := relay.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if relay.Queued() != 0 {
		t.Fatalf("delivered records must leave the queue, got %d", relay.Queued())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 || received[1].ID() != queuedRec.ID() {
		t.Fatalf("upstream must receive the queued vote verbatim, got %d records", len(received))
	}
}

func TestRetryKeepsRecordsTheHubDidNotAccept(t *testing.T) {
	var status atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(`{"code":"test"}`))
	}))
	defer upstream.Close()

	dir := filepath.Join(t.TempDir(), "relay")
	relay := &Relay{Upstream: upstream.URL, Dir: dir}
	ctx := context.Background()
	voter := testkit.Identity(t)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	raw := testkit.Sign(t, voter, hubwire.RecordVote, hubwire.VoteBody{SetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Version: 1, FP: "fp", Kind: hubwire.VoteWorks}, now)
	var rec hubwire.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if err := relay.enqueue(rec.ID(), raw); err != nil {
		t.Fatal(err)
	}

	for _, code := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		if err := relay.Retry(ctx); err == nil {
			t.Fatalf("a %d answer must pause the drain", code)
		}
		if relay.Queued() != 1 {
			t.Fatalf("a %d answer must keep the record, got %d queued", code, relay.Queued())
		}
	}

	status.Store(http.StatusBadRequest)
	if err := relay.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if relay.Queued() != 0 {
		t.Fatalf("a refused record must leave the queue, got %d queued", relay.Queued())
	}
}
