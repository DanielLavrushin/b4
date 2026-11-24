package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestADeletedVoteLeavesTheDatabaseFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hub.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	marker := "marker-of-a-voted-domain.example"
	if err := st.UpsertVote(ctx, Vote{RecordID: "r", SetID: "set", Version: 1, FP: "fp", KeyHMAC: "key", Kind: "manual_works", Weight: 1, Domain: marker, ReceivedAt: testNow.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	holds := func() bool {
		t.Helper()
		if err := st.Checkpoint(ctx); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return bytes.Contains(raw, []byte(marker))
	}
	if !holds() {
		t.Fatal("the vote must be in the file before it is pruned")
	}
	if removed, err := st.PruneVotes(ctx, testNow, time.Minute); err != nil || removed != 1 {
		t.Fatalf("the vote must go, got %d %v", removed, err)
	}
	if holds() {
		t.Error("the deleted vote must not stay readable in the database file")
	}
	st.Close()
}

func TestCheckpointGivesUpSoonOnABusyDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hub.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	blocked := func(ctx context.Context, name string) {
		t.Helper()
		start := time.Now()
		if err := st.Checkpoint(ctx); err == nil {
			t.Errorf("%s: a checkpoint that another process blocks must not pass for done", name)
		}
		if waited := time.Since(start); waited > time.Second {
			t.Errorf("%s: the only connection must not wait for another process, it waited %v", name, waited)
		}
		var wait int
		if err := st.db.QueryRowContext(context.Background(), `PRAGMA busy_timeout`).Scan(&wait); err != nil {
			t.Fatal(err)
		}
		if wait != 5000 {
			t.Errorf("%s: everything else waits 5 seconds for a lock, got %d ms", name, wait)
		}
	}
	for _, begin := range []string{"BEGIN IMMEDIATE", "BEGIN"} {
		for _, statement := range []string{begin, "SELECT COUNT(*) FROM meta"} {
			if _, err := other.db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
		blocked(ctx, begin)
		short, cancel := context.WithTimeout(ctx, checkpointWait/4)
		blocked(short, begin+" and a stop of the hub")
		cancel()
		if _, err := other.db.ExecContext(ctx, "ROLLBACK"); err != nil {
			t.Fatal(err)
		}
		if err := st.Checkpoint(ctx); err != nil {
			t.Errorf("%s: the next checkpoint must go through, got %v", begin, err)
		}
		if err := st.MarkDirty(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPruneVotesDropsOldVotesAndKeepsRecordIDs(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	keep := 180 * 24 * time.Hour
	old := testNow.Add(-keep - time.Second)
	edge := testNow.Add(-keep)
	put := func(id, key, kind string, at time.Time) {
		t.Helper()
		if _, _, err := st.TouchKey(ctx, key, at); err != nil {
			t.Fatal(err)
		}
		if kind == "vote" {
			if err := st.UpsertVote(ctx, Vote{RecordID: id, SetID: "set", Version: 1, FP: "fp", KeyHMAC: key, Kind: "manual_works", Weight: 1, ASNObserved: "64500", CountryObserved: "RU", OriginVerified: true, Domain: "example.com", Bucket: at.Unix() / int64(7*24*3600), ReceivedAt: at}); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.InsertRecord(ctx, Record{ID: id, Kind: kind, KeyHMAC: key, SetID: "set", Version: 1, ReceivedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	put("vote-old", "key-old", "vote", old)
	put("vote-edge", "key-edge", "vote", edge)
	put("vote-new", "key-new", "vote", testNow)
	put("vote-again", "key-old", "vote", testNow)
	if err := st.CreateSet(ctx, Set{ID: "set", AuthorHMAC: "key-old", CreatedAt: old, UpdatedAt: old}, func() *Version {
		v := sampleVersion("fp", "targets")
		v.RecordID = "share-old"
		return v
	}()); err != nil {
		t.Fatal(err)
	}
	put("share-old", "key-old", "share", old)
	put("duplicate-old", "key-other", "share", old)
	put("report-old", "key-old", "report", old)
	if err := st.MarkBuilt(ctx, testNow); err != nil {
		t.Fatal(err)
	}

	removed, err := st.PruneVotes(ctx, testNow, keep)
	if err != nil || removed != 1 {
		t.Fatalf("exactly the vote past the age must go, got %d %v", removed, err)
	}
	votes, err := st.AllVotes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	left := map[string]bool{}
	for _, v := range votes {
		left[v.RecordID] = true
	}
	if len(left) != 3 || !left["vote-edge"] || !left["vote-new"] || !left["vote-again"] {
		t.Fatalf("younger votes must stay, got %v", left)
	}
	for id, set := range map[string]string{"vote-old": "", "duplicate-old": "", "vote-edge": "set", "vote-new": "set", "share-old": "set", "report-old": "set"} {
		r, err := st.GetRecord(ctx, id)
		if err != nil {
			t.Fatalf("%s: a record id must outlive its vote, got %v", id, err)
		}
		if r.SetID != set || (set == "") != (r.Version == 0) || r.KeyHMAC == "" || r.ReceivedAt.IsZero() {
			t.Errorf("%s: want set %q, got %+v", id, set, r)
		}
	}
	if dirty, _ := st.Dirty(ctx); dirty {
		t.Error("the daily build picks up dropped votes, no build is forced")
	}
	if removed, err := st.PruneVotes(ctx, testNow, keep); err != nil || removed != 0 {
		t.Errorf("a second run has nothing to do, got %d %v", removed, err)
	}
}

func TestCheckpointLeavesTheWaitOfOtherRequestsAlone(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	type seen struct{ requests, shortest int }
	done, result := make(chan struct{}), make(chan seen)
	go func() {
		s := seen{shortest: 5000}
		for {
			select {
			case <-done:
				result <- s
				return
			default:
			}
			var wait int
			if err := st.db.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&wait); err != nil {
				t.Error(err)
			}
			s.requests++
			s.shortest = min(s.shortest, wait)
		}
	}()
	for range 200 {
		if err := st.Checkpoint(ctx); err != nil {
			t.Error(err)
		}
	}
	close(done)
	s := <-result
	if s.requests == 0 {
		t.Fatal("no request ran next to the checkpoints")
	}
	if s.shortest != 5000 {
		t.Errorf("a request next to a checkpoint waits 5 seconds for a lock, got %d ms", s.shortest)
	}
}
