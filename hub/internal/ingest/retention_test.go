package ingest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func TestAnExpiredVoteCannotBeReplayed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	author := testkit.Identity(t)
	setID, fp := approvedSet(t, f, author)
	keep := 365 * 24 * time.Hour
	raw := testkit.Sign(t, testkit.Identity(t), hubwire.RecordVote, hubwire.VoteBody{SetID: setID, Version: 1, FP: fp, Kind: hubwire.VoteBroken}, f.clock)
	expect(t, f.post(t, raw, peerB), http.StatusAccepted, "")

	f.clock = f.clock.Add(keep + time.Hour)
	if removed, err := f.store.PruneVotes(ctx, f.clock, keep); err != nil || removed != 2 {
		t.Fatalf("the upload vote and the report must expire, got %d %v", removed, err)
	}
	if n, err := f.store.CountVotes(ctx); err != nil || n != 0 {
		t.Fatalf("no vote must be left, got %d %v", n, err)
	}
	resp := f.post(t, raw, peerB)
	expect(t, resp, http.StatusOK, "")
	if resp.Body["duplicate"] != true {
		t.Errorf("a replayed record must be answered as a duplicate, got %v", resp.Body)
	}
	if n, err := f.store.CountVotes(ctx); err != nil || n != 0 {
		t.Errorf("a replayed record must not bring its vote back, got %d %v", n, err)
	}
}
