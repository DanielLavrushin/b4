package main

import (
	"context"
	"testing"
	"time"

	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

func TestCleanUpDeletesVotesOlderThanAYear(t *testing.T) {
	ctx := context.Background()
	layout := hubdata.Layout{Root: t.TempDir()}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(layout.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2027, 11, 24, 12, 0, 0, 0, time.UTC)
	for key, age := range map[string]time.Duration{"young": 364 * 24 * time.Hour, "old": 366 * 24 * time.Hour} {
		if err := st.UpsertVote(ctx, store.Vote{RecordID: key, SetID: "set", Version: 1, FP: "fp", KeyHMAC: key, Kind: "manual_works", Weight: 1, ReceivedAt: now.Add(-age)}); err != nil {
			t.Fatal(err)
		}
	}
	cleanUp(ctx, &services{layout: layout, store: st}, now)
	votes, err := st.AllVotes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(votes) != 1 || votes[0].RecordID != "young" {
		t.Fatalf("only the vote of the last year must be left, got %+v", votes)
	}
}
