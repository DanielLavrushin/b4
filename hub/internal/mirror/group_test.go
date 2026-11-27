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
