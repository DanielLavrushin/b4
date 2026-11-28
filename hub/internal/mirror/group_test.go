package mirror

import (
	"context"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func TestAGroupRecordIsForwardedAndNeverQueued(t *testing.T) {
	var down atomic.Bool
	log := &upstreamLog{}
	upstream := acceptingUpstream(t, log)
	relay := &Relay{Upstream: upstream.URL, Dir: filepath.Join(t.TempDir(), "relay"), Client: &http.Client{Transport: gate{down: &down, next: http.DefaultTransport}}}
	ctx := context.Background()
	author := testkit.Identity(t)
	set := testkit.SampleSet("YouTube", "youtube.com")
	body := hubwire.GroupBody{Title: "youtube", Members: []hubwire.GroupMember{{Envelope: *testkit.BuildEnvelope(t, &set)}}}

	if answer := relay.Handle(ctx, testkit.Sign(t, author, hubwire.RecordGroup, body, time.Now()), nil, ""); answer.Status != http.StatusAccepted || log.count() != 1 || log.received[0].Kind != hubwire.RecordGroup {
		t.Fatalf("a group record goes to the hub, got %d after %d forwards", answer.Status, log.count())
	}
	down.Store(true)
	if answer := relay.Handle(ctx, testkit.Sign(t, author, hubwire.RecordGroup, body, time.Now()), nil, ""); answer.Status != http.StatusBadGateway || relay.Queued() != 0 {
		t.Fatalf("a group record is never queued while the hub is down, got %d with %d queued", answer.Status, relay.Queued())
	}
}

func TestAGroupVoteIsQueuedWhileTheHubIsDownAndDeliveredLater(t *testing.T) {
	var down atomic.Bool
	down.Store(true)
	log := &upstreamLog{}
	upstream := acceptingUpstream(t, log)
	c := &clock{now: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)}
	relay := &Relay{Upstream: upstream.URL, Dir: filepath.Join(t.TempDir(), "relay"), Now: c.Now, Client: &http.Client{Transport: gate{down: &down, next: http.DefaultTransport}}}
	ctx := context.Background()
	body := hubwire.GroupVoteBody{GroupID: "01ARZ3NDEKTSV4RRFFQ69G5GA1", Version: 1, Members: []hubwire.GroupVoteMember{{SetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Version: 1}}, Kind: hubwire.VoteWorks}

	answer := relay.Handle(ctx, testkit.Sign(t, testkit.Identity(t), hubwire.RecordGroupVote, body, c.Now()), nil, "")
	if got := decode(t, answer); answer.Status != http.StatusAccepted || got["queued"] != true || got["kind"] != hubwire.RecordGroupVote || relay.Queued() != 1 {
		t.Fatalf("a group vote is queued like a vote while the hub is down, got %d %v with %d queued", answer.Status, got, relay.Queued())
	}
	down.Store(false)
	c.Add(breakerMaxBackoff)
	if err := relay.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if relay.Queued() != 0 || log.count() != 1 || log.received[0].Kind != hubwire.RecordGroupVote {
		t.Fatalf("the queued group vote reaches the hub, got %d queued and %d delivered", relay.Queued(), log.count())
	}
}
