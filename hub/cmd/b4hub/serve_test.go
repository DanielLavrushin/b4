package main

import (
	"bytes"
	"context"
	"os"
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

func TestCleanUpLeavesNothingDeletedInTheDatabaseFiles(t *testing.T) {
	ctx := context.Background()
	layout := hubdata.Layout{Root: t.TempDir()}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	open := func() *store.Store {
		t.Helper()
		st, err := store.Open(layout.DBPath())
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	now := time.Date(2027, 11, 24, 12, 0, 0, 0, time.UTC)
	put := func(st *store.Store, key, setID, domain string, at time.Time) {
		t.Helper()
		if err := st.UpsertVote(ctx, store.Vote{RecordID: key, SetID: setID, Version: 1, FP: "fp", KeyHMAC: key, Kind: "manual_works", Weight: 1, Domain: domain, ReceivedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	held := func(domain string) bool {
		t.Helper()
		for _, suffix := range []string{"", "-wal"} {
			raw, err := os.ReadFile(layout.DBPath() + suffix)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte(domain)) {
				return true
			}
		}
		return false
	}
	const expired, logged, purged = "expired-vote.example", "logged-vote.example", "purged-vote.example"

	st := open()
	if err := st.CreateSet(ctx, store.Set{ID: "set", AuthorHMAC: "author", CreatedAt: now, UpdatedAt: now}, &store.Version{FP: "fp", TargetsKey: "targets", Title: "Title", Status: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	put(st, "old", "other", expired, now.Add(-366*24*time.Hour))
	put(st, "young", "set", purged, now)
	st.Close()

	st = open()
	defer st.Close()
	put(st, "late", "other", logged, now.Add(-366*24*time.Hour))
	if !held(expired) || !held(logged) || !held(purged) {
		t.Fatal("the votes must be in the files before the clean-up")
	}
	cleanUp(ctx, &services{layout: layout, store: st}, now)
	if held(expired) || held(logged) {
		t.Error("an expired vote must not stay readable in the database files")
	}
	cli := open()
	if _, err := cli.DeleteSet(ctx, "set"); err != nil {
		t.Fatal(err)
	}
	cli.Close()
	cleanUp(ctx, &services{layout: layout, store: st}, now)
	if held(purged) {
		t.Error("a vote deleted with its set by another process must not stay readable in the database files")
	}
}
