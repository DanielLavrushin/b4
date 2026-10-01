package catalogue

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	setA = "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	setB = "01ARZ3NDEKTSV4RRFFQ69G5FA2"
)

func runs(t *testing.T, st *store.Store, q store.BuildQuery) []store.BuildRun {
	t.Helper()
	out, _, err := st.BuildRuns(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBuildRecordsHistoryAndChanges(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, setA, "fp-a")
	first, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	status := b.Status()
	if status.State != BuildIdle || status.LastOK == nil || status.LastError != nil {
		t.Fatalf("status after a good build: %+v", status)
	}
	last := status.LastOK
	if !last.OK || last.Seq != first.Manifest.Seq || last.Trigger != TriggerManual || last.Sets != 1 || last.Blobs != 1 || last.File != first.Manifest.Catalogue.File {
		t.Fatalf("the build run describes the publish: %+v", last)
	}
	if len(last.Changes.Added) != 1 || last.Changes.Added[0].SetID != setA || last.Changes.Added[0].Title != "set "+setA || !last.Changes.ContentChanged() {
		t.Fatalf("the first build adds the set: %+v", last.Changes)
	}
	history := runs(t, st, store.BuildQuery{})
	if len(history) != 1 || history[0].ID != last.ID || len(history[0].Changes.Added) != 1 {
		t.Fatalf("the run is stored: %+v", history)
	}

	if err := st.Hide(ctx, setA, 1, "abuse", b.Now()); err != nil {
		t.Fatal(err)
	}
	addActiveSet(t, st, setB, "fp-b")
	second, err := b.BuildFor(ctx, TriggerSchedule)
	if err != nil {
		t.Fatal(err)
	}
	if second.ByID[setA] != nil || second.ByID[setB] == nil {
		t.Fatalf("hidden set out, new set in: %v", second.ByID)
	}
	changes := b.Status().LastOK.Changes
	if len(changes.Removed) != 1 || changes.Removed[0].SetID != setA || changes.Removed[0].Version != 1 || len(changes.Added) != 1 || changes.Added[0].SetID != setB {
		t.Fatalf("the second build records what changed: %+v", changes)
	}
	if b.Status().LastOK.Trigger != TriggerSchedule {
		t.Fatalf("the trigger is recorded: %+v", b.Status().LastOK)
	}

	if _, err := b.Build(ctx); err != nil {
		t.Fatal(err)
	}
	if b.Status().LastOK.Changes.ContentChanged() {
		t.Fatalf("a rebuild without edits changes nothing: %+v", b.Status().LastOK.Changes)
	}
	if got := runs(t, st, store.BuildQuery{}); len(got) != 3 {
		t.Fatalf("every build is recorded: %d", len(got))
	}
	if got := runs(t, st, store.BuildQuery{OnlyChanges: true}); len(got) != 2 {
		t.Fatalf("only the builds that changed content are listed as changes: %d", len(got))
	}
}

func TestFailedBuildIsRecordedAndReported(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, setA, "fp-a")
	blocker := filepath.Join(b.PublicDir, ManifestFile)
	if err := os.MkdirAll(filepath.Join(blocker, "occupied"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(ctx); err == nil {
		t.Fatalf("a manifest that cannot be written must fail the build")
	}
	status := b.Status()
	if status.State != BuildIdle || status.LastOK != nil || status.LastError == nil {
		t.Fatalf("status after a failed build: %+v", status)
	}
	if status.LastError.OK || status.LastError.Error == "" || status.LastError.FinishedAt.IsZero() {
		t.Fatalf("the failure is described: %+v", status.LastError)
	}
	failed := runs(t, st, store.BuildQuery{OnlyFailed: true})
	if len(failed) != 1 || failed[0].Error != status.LastError.Error || failed[0].OK {
		t.Fatalf("the failure is stored: %+v", failed)
	}
	if dirty, _ := st.Dirty(ctx); !dirty {
		t.Fatalf("a failed build must leave the store dirty")
	}
	if needed, _ := b.NeedsBuild(ctx); !needed {
		t.Fatalf("a failed build must be retried")
	}
	if b.Latest() != nil {
		t.Fatalf("a failed build publishes nothing")
	}

	fresh := &Builder{Store: st, Identity: b.Identity, PublicDir: b.PublicDir, Now: b.Now}
	fresh.LoadHistory(ctx)
	if loaded := fresh.Status(); loaded.LastError == nil || loaded.LastError.Error != status.LastError.Error || loaded.LastOK != nil {
		t.Fatalf("a restarted builder loads the last failure: %+v", loaded)
	}

	if err := os.RemoveAll(blocker); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(ctx); err != nil {
		t.Fatal(err)
	}
	status = b.Status()
	if status.LastOK == nil || status.LastError != nil {
		t.Fatalf("a later success clears the reported error: %+v", status)
	}
	if dirty, _ := st.Dirty(ctx); dirty {
		t.Fatalf("a good build clears the dirty flag")
	}
}

func TestWriteDuringBuildKeepsTheStoreDirty(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, setA, "fp-a")
	armed := false
	b.Mirrors = &MirrorHealth{Store: st, Now: func() time.Time {
		if armed {
			armed = false
			if err := st.Hide(context.Background(), setA, 1, "landed mid-build", b.Now()); err != nil {
				t.Error(err)
			}
		}
		return b.Now()
	}}
	armed = true
	result, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if armed {
		t.Fatalf("the hook must run inside the build")
	}
	if result.ByID[setA] == nil {
		t.Fatalf("the build read the catalogue before the write landed")
	}
	if dirty, _ := st.Dirty(ctx); !dirty {
		t.Fatalf("a write that lands during a build must keep the store dirty")
	}
	if needed, _ := b.NeedsBuild(ctx); !needed {
		t.Fatalf("the write must be picked up by the next build")
	}
	result, err = b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.ByID[setA] != nil {
		t.Fatalf("the next build publishes the write")
	}
	if needed, _ := b.NeedsBuild(ctx); needed {
		t.Fatalf("a quiet build leaves nothing to do")
	}
}

func TestBuildWithholdsSetsAndBannedVotes(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, setA, "fp-a")
	addActiveSet(t, st, setB, "fp-b")
	now := b.Now()
	for i, key := range []string{"voter-1", "voter-2"} {
		if _, _, err := st.TouchKey(ctx, key, now.Add(-30*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertVote(ctx, store.Vote{SetID: setA, Version: 1, FP: "fp-a", KeyHMAC: key, Kind: "manual_works", Weight: 1,
			ASNObserved: []string{"64500", "64501"}[i], OriginVerified: true, Bucket: 1, ReceivedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := result.ByID[setA].Scores.Global
	if before.Devices != 2 {
		t.Fatalf("two voters: %+v", before)
	}
	if err := st.BanKey(ctx, "voter-2", "vote ring", now); err != nil {
		t.Fatal(err)
	}
	result, err = b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	after := result.ByID[setA].Scores.Global
	if after.Devices != 1 || after.N >= before.N {
		t.Fatalf("a banned voter's vote must leave the score: before %+v after %+v", before, after)
	}

	if err := st.Update(ctx, func(tx *store.Tx) error { return tx.WithdrawSet(ctx, setB, "author asked", now) }); err != nil {
		t.Fatal(err)
	}
	result, err = b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.ByID[setB] != nil || result.ByID[setA] == nil {
		t.Fatalf("a withdrawn set leaves the catalogue: %v", result.ByID)
	}
	if removed := b.Status().LastOK.Changes.Removed; len(removed) != 1 || removed[0].SetID != setB {
		t.Fatalf("the withdrawal is recorded as a removal: %+v", removed)
	}
	if err := st.BanKey(ctx, "author-"+setA, "spam", now); err != nil {
		t.Fatal(err)
	}
	if result, err = b.Build(ctx); err != nil {
		t.Fatal(err)
	}
	if len(result.Catalogue.Sets) != 0 || len(result.Catalogue.Blobs) != 0 {
		t.Fatalf("a banned author's set and its payloads leave the catalogue: %+v", result.Catalogue)
	}
	if err := st.UnbanKey(ctx, "author-"+setA); err != nil {
		t.Fatal(err)
	}
	if result, err = b.Build(ctx); err != nil {
		t.Fatal(err)
	}
	if result.ByID[setA] == nil {
		t.Fatalf("unbanning the author lists the set again")
	}
}

func TestDiffClassifiesChanges(t *testing.T) {
	set := func(id string, version int, title string, score float64, devices int) hubwire.CatalogueSet {
		return hubwire.CatalogueSet{ID: id, Version: version, Title: title, Scores: hubwire.Scores{Global: hubwire.Score{Score: score, Devices: devices}}}
	}
	result := func(mirrors, revoked []string, sets ...hubwire.CatalogueSet) *Result {
		return indexResult(&hubwire.Manifest{Mirrors: mirrors, RevokedKeys: revoked}, &hubwire.Catalogue{Sets: sets})
	}
	prev := result([]string{"https://hub.example", "https://old.example"}, []string{"k1"},
		set("kept", 1, "Kept", 0.5, 3),
		set("bumped", 1, "Bumped", 0.5, 3),
		set("renamed", 2, "Old name", 0.5, 3),
		set("rescored", 1, "Rescored", 0.5, 3),
		set("wobbled", 1, "Wobbled", 0.5, 3),
		set("gone", 4, "Gone", 0.5, 3),
	)
	next := result([]string{"https://hub.example", "https://new.example"}, []string{"k1", "k2"},
		set("kept", 1, "Kept", 0.5, 3),
		set("bumped", 2, "Bumped", 0.5, 3),
		set("renamed", 2, "New name", 0.5, 3),
		set("rescored", 1, "Rescored", 0.6, 3),
		set("wobbled", 1, "Wobbled", 0.51, 3),
		set("fresh", 1, "Fresh", 0, 0),
	)
	c := Diff(prev, next)
	if len(c.Added) != 1 || c.Added[0].SetID != "fresh" {
		t.Errorf("added: %+v", c.Added)
	}
	if len(c.Removed) != 1 || c.Removed[0].SetID != "gone" || c.Removed[0].Version != 4 || c.Removed[0].Title != "Gone" {
		t.Errorf("removed: %+v", c.Removed)
	}
	if len(c.Updated) != 1 || c.Updated[0].SetID != "bumped" || c.Updated[0].From != 1 || c.Updated[0].To != 2 {
		t.Errorf("updated: %+v", c.Updated)
	}
	if len(c.Edited) != 1 || c.Edited[0].SetID != "renamed" || c.Edited[0].Title != "New name" {
		t.Errorf("edited: %+v", c.Edited)
	}
	if c.Rescored != 1 {
		t.Errorf("a score move below the threshold is not a rescore: %d", c.Rescored)
	}
	if len(c.MirrorsAdded) != 1 || c.MirrorsAdded[0] != "https://new.example" || len(c.MirrorsRemoved) != 1 || c.MirrorsRemoved[0] != "https://old.example" {
		t.Errorf("mirrors: %+v %+v", c.MirrorsAdded, c.MirrorsRemoved)
	}
	if len(c.RevokedAdded) != 1 || c.RevokedAdded[0] != "k2" {
		t.Errorf("revoked: %+v", c.RevokedAdded)
	}

	quiet := Diff(prev, result(prev.Manifest.Mirrors, prev.Manifest.RevokedKeys,
		set("kept", 1, "Kept", 0.5, 4), set("bumped", 1, "Bumped", 0.5, 3), set("renamed", 2, "Old name", 0.5, 3),
		set("rescored", 1, "Rescored", 0.5, 3), set("wobbled", 1, "Wobbled", 0.5, 3), set("gone", 4, "Gone", 0.5, 3)))
	if quiet.Rescored != 1 || quiet.ContentChanged() {
		t.Errorf("a device count change is a rescore and not a content change: %+v", quiet)
	}
	if first := Diff(nil, next); len(first.Added) != 6 || len(first.MirrorsAdded) != 2 || len(first.RevokedAdded) != 2 {
		t.Errorf("the first build adds everything: %+v", first)
	}
	if empty := Diff(prev, nil); empty.ContentChanged() {
		t.Errorf("no result, no changes: %+v", empty)
	}
}

func TestMirrorsDriftFollowsAnnounceableMirrors(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	clock := b.Now()
	b.Mirrors = &MirrorHealth{Store: st, Now: func() time.Time { return clock }}
	if drift, err := b.MirrorsDrift(ctx); err != nil || drift {
		t.Fatalf("no mirrors and no build means no drift: %v %v", drift, err)
	}
	m, err := st.AnnounceMirror(ctx, "https://mirror.example", "k", "1.0.0", clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMirrorStatus(ctx, m.ID, store.MirrorApproved, "", clock); err != nil {
		t.Fatal(err)
	}
	if drift, _ := b.MirrorsDrift(ctx); drift {
		t.Fatalf("an approved mirror without a passing check is not announceable")
	}
	if err := st.RecordMirrorCheck(ctx, m.ID, store.MirrorCheck{At: clock, OK: true, Epoch: 1, Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if drift, _ := b.MirrorsDrift(ctx); !drift {
		t.Fatalf("a newly healthy mirror drifts from an unpublished hub")
	}
	result, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Manifest.Mirrors) != 2 || result.Manifest.Mirrors[0] != "https://hub.example" || result.Manifest.Mirrors[1] != m.URL {
		t.Fatalf("the manifest lists the hub first and then the mirror: %v", result.Manifest.Mirrors)
	}
	if changes := b.Status().LastOK.Changes; len(changes.MirrorsAdded) != 2 {
		t.Fatalf("the first build announces both addresses: %+v", changes)
	}
	if drift, _ := b.MirrorsDrift(ctx); drift {
		t.Fatalf("a manifest that lists the mirror has no drift")
	}
	clock = clock.Add(DefaultMirrorWindow + time.Minute)
	if drift, _ := b.MirrorsDrift(ctx); drift {
		t.Fatalf("while no mirror passes its check the published list is kept, so there is no drift")
	}
	if result, err = b.Build(ctx); err != nil {
		t.Fatal(err)
	}
	if len(result.Manifest.Mirrors) != 2 || result.Manifest.Mirrors[1] != m.URL {
		t.Fatalf("a build while no mirror passes keeps the approved mirrors it listed: %v", result.Manifest.Mirrors)
	}

	other, err := st.AnnounceMirror(ctx, "https://other.example", "k2", "1.0.0", clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMirrorStatus(ctx, other.ID, store.MirrorApproved, "", clock); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordMirrorCheck(ctx, other.ID, store.MirrorCheck{At: clock, OK: true, Epoch: 1, Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if drift, _ := b.MirrorsDrift(ctx); !drift {
		t.Fatalf("once another mirror passes, the one past its window must drift out")
	}
	if result, err = b.Build(ctx); err != nil {
		t.Fatal(err)
	}
	if len(result.Manifest.Mirrors) != 2 || result.Manifest.Mirrors[1] != other.URL {
		t.Fatalf("the stale mirror is replaced by the passing one: %v", result.Manifest.Mirrors)
	}
	if changes := b.Status().LastOK.Changes; len(changes.MirrorsRemoved) != 1 || changes.MirrorsRemoved[0] != m.URL {
		t.Fatalf("the drop is recorded: %+v", changes)
	}

	clock = clock.Add(DefaultMirrorWindow + time.Minute)
	if err := st.SetMirrorStatus(ctx, other.ID, store.MirrorRejected, "gone", clock); err != nil {
		t.Fatal(err)
	}
	if drift, _ := b.MirrorsDrift(ctx); !drift {
		t.Fatalf("a mirror a moderator rejected must leave even when nothing else passes")
	}
	if result, err = b.Build(ctx); err != nil {
		t.Fatal(err)
	}
	if len(result.Manifest.Mirrors) != 1 {
		t.Fatalf("only the hub stays listed: %v", result.Manifest.Mirrors)
	}
}

func TestRunBuildsOnRequestAfterTheDebounce(t *testing.T) {
	b, st := testBuilder(t)
	addActiveSet(t, st, setA, "fp-a")
	b.Debounce = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.Run(ctx, time.Hour)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	waitFor := func(what string, cond func(BuildStatus) bool) BuildStatus {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if st := b.Status(); cond(st) {
				return st
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s: %+v", what, b.Status())
		return BuildStatus{}
	}
	waitFor("the startup build", func(s BuildStatus) bool {
		return s.State == BuildIdle && s.LastOK != nil && s.LastOK.Trigger == TriggerStartup
	})

	if err := st.MarkDirty(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.Request("moderation")
	if s := b.Status(); s.State == BuildIdle {
		t.Fatalf("a request is visible as queued until it runs: %+v", s)
	}
	waitFor("the requested build", func(s BuildStatus) bool {
		return s.State == BuildIdle && s.LastOK != nil && s.LastOK.Trigger == "moderation"
	})

	before := len(runs(t, st, store.BuildQuery{}))
	b.Request("moderation")
	waitFor("the idle request", func(s BuildStatus) bool { return s.State == BuildIdle })
	if s := b.Status(); s.Trigger != "" || !s.QueuedAt.IsZero() {
		t.Fatalf("an idle builder reports no trigger: %+v", s)
	}
	if got := len(runs(t, st, store.BuildQuery{})); got != before {
		t.Fatalf("a request on a clean store builds nothing: %d -> %d", before, got)
	}

	b.RequestForced(TriggerManual)
	waitFor("the forced build", func(s BuildStatus) bool {
		return s.State == BuildIdle && s.LastOK != nil && s.LastOK.Trigger == TriggerManual
	})
	if got := len(runs(t, st, store.BuildQuery{})); got != before+1 {
		t.Fatalf("a forced request builds a clean store: %d -> %d", before, got)
	}
}

func TestRequestDuringABuildStaysQueuedWithItsTrigger(t *testing.T) {
	b, _ := testBuilder(t)
	b.markBuilding(TriggerSchedule, b.Now())
	b.Request(TriggerMirrors)
	if st := b.Status(); st.State != BuildBuilding {
		t.Fatalf("a request during a build must not hide the running build: %+v", st)
	}
	b.markFinished(store.BuildRun{ID: 1, OK: true})
	st := b.Status()
	if st.State != BuildQueued || st.Trigger != TriggerMirrors || st.QueuedAt.IsZero() {
		t.Fatalf("the request made during the build must stay queued with its trigger: %+v", st)
	}
	trigger, _ := b.takeRequest()
	if trigger != TriggerMirrors {
		t.Fatalf("the queued build must run with the requested trigger, got %q", trigger)
	}
}

func TestTheHubsOwnAddressAsAMirrorNeverDrifts(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	clock := b.Now()
	b.Mirrors = &MirrorHealth{Store: st, Now: func() time.Time { return clock }}
	m, err := st.AnnounceMirror(ctx, b.HubBase(), "k", "1.0.0", clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMirrorStatus(ctx, m.ID, store.MirrorApproved, "", clock); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordMirrorCheck(ctx, m.ID, store.MirrorCheck{At: clock, OK: true, Epoch: 1, Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(ctx); err != nil {
		t.Fatal(err)
	}
	if drift, _ := b.MirrorsDrift(ctx); drift {
		t.Fatalf("the hub's own address approved as a mirror must not force a rebuild every round")
	}
}

func TestABuildThatCannotLockIsRecordedAsFailed(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.PublicDir = filepath.Join(blocker, "public")
	if _, err := b.Build(ctx); err == nil {
		t.Fatal("a build without a writable public directory must fail")
	}
	status := b.Status()
	if status.State != BuildIdle || status.LastError == nil || status.LastError.Error == "" {
		t.Fatalf("the failure must be visible in the status: %+v", status)
	}
	runs, _, err := st.BuildRuns(ctx, store.BuildQuery{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].OK || runs[0].Error == "" {
		t.Fatalf("the failure must be in the build history: %+v", runs)
	}
}
