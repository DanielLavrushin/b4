package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
)

func keyNamed(n int) string {
	return fmt.Sprintf("%064x", n)
}

func addSetWith(t *testing.T, st *Store, id, author string, statuses ...string) {
	t.Helper()
	ctx := context.Background()
	for i, status := range statuses {
		v := sampleVersion(fmt.Sprintf("fp-%s-%d", id, i+1), "targets-"+id)
		v.Title = fmt.Sprintf("%s v%d", id, i+1)
		v.UploaderHMAC = author
		if i == 0 {
			if err := st.CreateSet(ctx, Set{ID: id, AuthorHMAC: author, CreatedAt: testNow, UpdatedAt: testNow}, v); err != nil {
				t.Fatal(err)
			}
		} else {
			v.SetID = id
			if err := st.AddVersion(ctx, v); err != nil {
				t.Fatal(err)
			}
		}
		switch status {
		case hubwire.SetStatusPending:
		case hubwire.SetStatusActive:
			if err := st.Approve(ctx, id, i+1, testNow); err != nil {
				t.Fatal(err)
			}
		case hubwire.SetStatusRejected:
			if err := st.Reject(ctx, id, i+1, "no", testNow); err != nil {
				t.Fatal(err)
			}
		case hubwire.SetStatusHidden:
			if err := st.Approve(ctx, id, i+1, testNow); err != nil {
				t.Fatal(err)
			}
			if err := st.Hide(ctx, id, i+1, "hidden", testNow); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unknown status %q", status)
		}
	}
}

func setIDs(versions []Version) map[string]int {
	out := make(map[string]int, len(versions))
	for _, v := range versions {
		out[v.SetID] = v.Version
	}
	return out
}

func TestDirtyGenerationCountsEveryWrite(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	gen, err := st.DirtyGeneration(ctx)
	if err != nil || gen != "0" {
		t.Fatalf("a fresh store is at generation 0, got %q %v", gen, err)
	}
	if dirty, _ := st.Dirty(ctx); dirty {
		t.Fatalf("a fresh store is clean")
	}
	if err := st.MarkDirty(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkDirty(ctx); err != nil {
		t.Fatal(err)
	}
	if gen, _ = st.DirtyGeneration(ctx); gen != "2" {
		t.Fatalf("two writes make generation 2, got %q", gen)
	}

	if err := st.MarkPublished(ctx, testNow, "1"); err != nil {
		t.Fatal(err)
	}
	if dirty, _ := st.Dirty(ctx); !dirty {
		t.Fatalf("publishing an older generation must leave the store dirty")
	}
	if gen, _ = st.DirtyGeneration(ctx); gen != "2" {
		t.Fatalf("a stale publish must not touch the generation, got %q", gen)
	}
	if built, _ := st.BuiltAt(ctx); !built.Equal(testNow) {
		t.Fatalf("a publish records when it happened even when stale, got %v", built)
	}

	if err := st.MarkPublished(ctx, testNow.Add(time.Minute), "2"); err != nil {
		t.Fatal(err)
	}
	if dirty, _ := st.Dirty(ctx); dirty {
		t.Fatalf("publishing the current generation must clear the flag")
	}
	if gen, _ = st.DirtyGeneration(ctx); gen != "0" {
		t.Fatalf("a clean store is back at generation 0, got %q", gen)
	}

	if err := st.Update(ctx, func(tx *Tx) error { return tx.MarkDirty(ctx) }); err != nil {
		t.Fatal(err)
	}
	addSetWith(t, st, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "a", hubwire.SetStatusActive)
	if gen, _ = st.DirtyGeneration(ctx); gen != "3" {
		t.Fatalf("a transaction mark, a create and an approve count three writes, got %q", gen)
	}

	seen, err := st.DirtyGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Hide(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "mid-build", testNow); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkPublished(ctx, testNow, seen); err != nil {
		t.Fatal(err)
	}
	if dirty, _ := st.Dirty(ctx); !dirty {
		t.Fatalf("a write that lands while a build runs must survive the build's publish")
	}
}

func TestBuildRequestIsTakenOnce(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if taken, err := st.TakeBuildRequest(ctx); err != nil || taken {
		t.Fatalf("nothing requested yet, got %v %v", taken, err)
	}
	if err := st.RequestBuild(ctx, testNow); err != nil {
		t.Fatal(err)
	}
	if err := st.RequestBuild(ctx, testNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if taken, _ := st.TakeBuildRequest(ctx); !taken {
		t.Fatalf("a requested build must be taken")
	}
	if taken, _ := st.TakeBuildRequest(ctx); taken {
		t.Fatalf("a request is taken only once")
	}
}

func TestHideRemembersWhereTheVersionCameFrom(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	pending := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	active := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	addSetWith(t, st, pending, "a", hubwire.SetStatusPending)
	addSetWith(t, st, active, "a", hubwire.SetStatusActive)

	restore := func(id string) string {
		t.Helper()
		var target string
		err := st.Update(ctx, func(tx *Tx) error {
			var err error
			target, err = tx.Restore(ctx, id, 1, testNow)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return target
	}

	if err := st.Hide(ctx, pending, 1, "first", testNow); err != nil {
		t.Fatal(err)
	}
	if err := st.Hide(ctx, pending, 1, "second", testNow); err != nil {
		t.Fatal(err)
	}
	v, _ := st.GetVersion(ctx, pending, 1)
	if v.Status != hubwire.SetStatusHidden || v.HiddenFrom != hubwire.SetStatusPending || v.StatusReason != "second" {
		t.Fatalf("hiding twice must keep the original state: %+v", v)
	}
	if target := restore(pending); target != hubwire.SetStatusPending {
		t.Fatalf("a hidden pending version restores to pending, got %q", target)
	}
	v, _ = st.GetVersion(ctx, pending, 1)
	if v.Status != hubwire.SetStatusPending || v.HiddenFrom != "" || v.StatusReason != "" {
		t.Fatalf("restored pending version: %+v", v)
	}
	if set, _, _ := st.GetSet(ctx, pending); set.CurrentVersion != 0 {
		t.Fatalf("restoring to pending must not make the version current: %+v", set)
	}

	if err := st.Hide(ctx, active, 1, "abuse", testNow); err != nil {
		t.Fatal(err)
	}
	v, _ = st.GetVersion(ctx, active, 1)
	if v.HiddenFrom != hubwire.SetStatusActive {
		t.Fatalf("a hidden active version remembers it: %+v", v)
	}
	if target := restore(active); target != hubwire.SetStatusActive {
		t.Fatalf("a hidden active version restores to active, got %q", target)
	}
	if v, _ = st.GetVersion(ctx, active, 1); v.Status != hubwire.SetStatusActive || v.HiddenFrom != "" {
		t.Fatalf("restored active version: %+v", v)
	}

	if err := st.Hide(ctx, active, 1, "again", testNow); err != nil {
		t.Fatal(err)
	}
	if err := st.Approve(ctx, active, 1, testNow); err != nil {
		t.Fatal(err)
	}
	if v, _ = st.GetVersion(ctx, active, 1); v.HiddenFrom != "" {
		t.Fatalf("leaving the hidden state clears hidden_from: %+v", v)
	}

	err := st.Update(ctx, func(tx *Tx) error {
		_, err := tx.Restore(ctx, pending, 7, testNow)
		return err
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("restoring an unknown version must be ErrNotFound, got %v", err)
	}
}

func TestCatalogueVersionsWithholdWithdrawnSetsAndBannedAuthors(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	banned := keyNamed(3)
	plain := "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	withdrawn := "01ARZ3NDEKTSV4RRFFQ69G5FA2"
	quarantined := "01ARZ3NDEKTSV4RRFFQ69G5FA3"
	both := "01ARZ3NDEKTSV4RRFFQ69G5FA4"
	pendingOnly := "01ARZ3NDEKTSV4RRFFQ69G5FA5"
	addSetWith(t, st, plain, keyNamed(1), hubwire.SetStatusActive, hubwire.SetStatusActive)
	addSetWith(t, st, withdrawn, keyNamed(2), hubwire.SetStatusActive, hubwire.SetStatusPending)
	addSetWith(t, st, quarantined, banned, hubwire.SetStatusActive)
	addSetWith(t, st, both, banned, hubwire.SetStatusActive)
	addSetWith(t, st, pendingOnly, banned, hubwire.SetStatusPending)

	err := st.Update(ctx, func(tx *Tx) error {
		if err := tx.WithdrawSet(ctx, withdrawn, "author asked", testNow); err != nil {
			return err
		}
		return tx.WithdrawSet(ctx, both, "duplicate", testNow)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.BanKey(ctx, banned, "spam", testNow); err != nil {
		t.Fatal(err)
	}

	listed, err := st.ListedVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := setIDs(listed); len(got) != 4 || got[plain] != 2 || got[withdrawn] != 1 {
		t.Fatalf("listed versions ignore withholding and pick the newest active: %v", got)
	}
	cat, err := st.CatalogueVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := setIDs(cat); len(got) != 1 || got[plain] != 2 {
		t.Fatalf("only the unwithheld set may be published: %v", got)
	}

	withheld, err := st.Withheld(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{withdrawn: WithheldWithdrawn, quarantined: WithheldAuthorBanned, both: WithheldWithdrawn, pendingOnly: WithheldAuthorBanned}
	if len(withheld) != len(want) {
		t.Fatalf("withheld: %v", withheld)
	}
	for id, reason := range want {
		if withheld[id] != reason {
			t.Errorf("withheld reason of %s: got %q want %q", id, withheld[id], reason)
		}
	}
	versions, err := st.WithheldVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := setIDs(versions); len(got) != 3 || got[withdrawn] != 1 || got[quarantined] != 1 || got[both] != 1 {
		t.Fatalf("a withheld set is represented by its listed version and a set with nothing active is not withheld: %v", got)
	}
	counts, err := st.VersionCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Withheld != 3 || counts.Listed != 1 {
		t.Fatalf("version counts must agree with the groups: %+v", counts)
	}

	if err := st.Update(ctx, func(tx *Tx) error { return tx.ReinstateSet(ctx, withdrawn) }); err != nil {
		t.Fatal(err)
	}
	if err := st.UnbanKey(ctx, banned); err != nil {
		t.Fatal(err)
	}
	cat, _ = st.CatalogueVersions(ctx)
	if got := setIDs(cat); len(got) != 3 || got[withdrawn] != 1 || got[quarantined] != 1 || got[both] != 0 {
		t.Fatalf("reinstating and unbanning must bring the sets back, the other withdrawal stays: %v", got)
	}
	set, _, _ := st.GetSet(ctx, withdrawn)
	if !set.WithdrawnAt.IsZero() || set.WithdrawReason != "" {
		t.Fatalf("reinstating clears the withdrawal: %+v", set)
	}

	err = st.Update(ctx, func(tx *Tx) error { return tx.WithdrawSet(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FA9", "x", testNow) })
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdrawing an unknown set must be ErrNotFound, got %v", err)
	}
	err = st.Update(ctx, func(tx *Tx) error { return tx.ReinstateSet(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FA9") })
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("reinstating an unknown set must be ErrNotFound, got %v", err)
	}
}

func TestUnknownKeysCannotBeUnbannedOrUntrusted(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if err := st.UnbanKey(ctx, keyNamed(9)); !errors.Is(err, ErrNotFound) {
		t.Errorf("unbanning an unknown key must be ErrNotFound, got %v", err)
	}
	if err := st.UntrustKey(ctx, keyNamed(9)); !errors.Is(err, ErrNotFound) {
		t.Errorf("untrusting an unknown key must be ErrNotFound, got %v", err)
	}
	if _, err := st.GetKey(ctx, keyNamed(9)); !errors.Is(err, ErrNotFound) {
		t.Errorf("a failed unban or untrust must not create the key, got %v", err)
	}
}

func TestKeyImpactNamesWhatABanWouldTouch(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	key := keyNamed(1)
	listed := "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	pending := "01ARZ3NDEKTSV4RRFFQ69G5FA2"
	foreign := "01ARZ3NDEKTSV4RRFFQ69G5FA3"
	addSetWith(t, st, listed, key, hubwire.SetStatusActive, hubwire.SetStatusActive, hubwire.SetStatusPending)
	addSetWith(t, st, pending, key, hubwire.SetStatusPending)
	addSetWith(t, st, foreign, keyNamed(2), hubwire.SetStatusActive)
	for i, fp := range []string{"fp-a", "fp-a", "fp-b"} {
		if err := st.UpsertVote(ctx, Vote{SetID: foreign, Version: 1, FP: fp, KeyHMAC: key, Kind: "manual_works", Weight: 1, ASNObserved: fmt.Sprint(64500 + i), Bucket: 1, ReceivedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []Report{
		{SetID: foreign, Version: 1, KeyHMAC: key, ASNObserved: "64500", ReceivedAt: testNow},
		{SetID: foreign, Version: 1, KeyHMAC: key, ASNObserved: "64501", ReceivedAt: testNow},
	} {
		if err := st.InsertReport(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	reports, _ := st.ReportsForVersion(ctx, foreign, 1)
	if err := st.Update(ctx, func(tx *Tx) error {
		return tx.SetReportState(ctx, reports[0].ID, ReportDismissed, "", "", testNow)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AnnounceMirror(ctx, "https://mirror.example", key, "1.0.0", testNow); err != nil {
		t.Fatal(err)
	}

	impact, err := st.KeyImpact(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.Listed) != 1 || impact.Listed[0].SetID != listed || impact.Listed[0].Version != 2 || impact.Listed[0].Title != listed+" v2" {
		t.Errorf("listed: %+v", impact.Listed)
	}
	pendingRefs := map[string]int{}
	for _, ref := range impact.Pending {
		pendingRefs[ref.SetID] = ref.Version
	}
	if len(impact.Pending) != 2 || pendingRefs[listed] != 3 || pendingRefs[pending] != 1 {
		t.Errorf("pending: %+v", impact.Pending)
	}
	if impact.Votes != 3 || impact.VotedSets != 2 || impact.Reports != 1 || len(impact.Mirrors) != 1 {
		t.Errorf("counts: %+v", impact)
	}
	empty, err := st.KeyImpact(ctx, keyNamed(9))
	if err != nil || len(empty.Listed) != 0 || len(empty.Pending) != 0 || empty.Votes != 0 || len(empty.Mirrors) != 0 {
		t.Errorf("an unknown key touches nothing: %+v %v", empty, err)
	}
}

func TestScoringVotesLeaveOutBannedKeys(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	for i, key := range []string{keyNamed(1), keyNamed(2)} {
		if err := st.UpsertVote(ctx, Vote{SetID: "s", Version: 1, FP: "fp", KeyHMAC: key, Kind: "manual_works", Weight: 1, ASNObserved: fmt.Sprint(64500 + i), Bucket: 1, ReceivedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.BanKey(ctx, keyNamed(2), "vote ring", testNow); err != nil {
		t.Fatal(err)
	}
	scoring, err := st.ScoringVotesByFP(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(scoring["fp"]) != 1 || scoring["fp"][0].KeyHMAC != keyNamed(1) {
		t.Fatalf("a banned key's votes must not score: %+v", scoring["fp"])
	}
	all, _ := st.VotesByFP(ctx)
	if len(all["fp"]) != 2 {
		t.Fatalf("the votes stay stored for the console: %+v", all["fp"])
	}
	if err := st.UnbanKey(ctx, keyNamed(2)); err != nil {
		t.Fatal(err)
	}
	if scoring, _ = st.ScoringVotesByFP(ctx); len(scoring["fp"]) != 2 {
		t.Fatalf("unbanning restores the votes: %+v", scoring["fp"])
	}
}

func TestIndependentReportsCountOnlyOpenReportsFromUnbannedKeys(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	for i, key := range []string{keyNamed(1), keyNamed(2), keyNamed(3)} {
		if err := st.InsertReport(ctx, Report{SetID: "s", Version: 1, KeyHMAC: key, ASNObserved: fmt.Sprint(64500 + i), ReceivedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := st.IndependentReports(ctx, "s", 1); n != 3 {
		t.Fatalf("three keys over three asns, got %d", n)
	}
	reports, _ := st.ReportsForVersion(ctx, "s", 1)
	byKey := map[string]int64{}
	for _, r := range reports {
		byKey[r.KeyHMAC] = r.ID
		if !Counts(r) {
			t.Errorf("an open report from an unbanned key with an asn counts: %+v", r)
		}
	}
	if err := st.Update(ctx, func(tx *Tx) error {
		return tx.SetReportState(ctx, byKey[keyNamed(1)], ReportDismissed, "", "noise", testNow)
	}); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.IndependentReports(ctx, "s", 1); n != 2 {
		t.Fatalf("a dismissed report must not count, got %d", n)
	}
	if err := st.BanKey(ctx, keyNamed(2), "brigade", testNow); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.IndependentReports(ctx, "s", 1); n != 1 {
		t.Fatalf("a banned key's report must not count, got %d", n)
	}
	reports, _ = st.ReportsForVersion(ctx, "s", 1)
	for _, r := range reports {
		if r.KeyHMAC == keyNamed(2) && (!r.KeyBanned || Counts(r)) {
			t.Errorf("the banned reporter must be flagged: %+v", r)
		}
	}
	if err := st.Update(ctx, func(tx *Tx) error {
		return tx.SetReportState(ctx, byKey[keyNamed(1)], ReportOpen, "restored", "reopened", testNow)
	}); err != nil {
		t.Fatal(err)
	}
	reports, _ = st.ReportsForVersion(ctx, "s", 1)
	for _, r := range reports {
		if r.ID == byKey[keyNamed(1)] && (r.State != ReportOpen || r.Resolution != "" || !r.ResolvedAt.IsZero() || r.Note != "reopened") {
			t.Errorf("reopening clears the resolution and keeps the note: %+v", r)
		}
	}
	if n, _ := st.IndependentReports(ctx, "s", 1); n != 2 {
		t.Fatalf("a reopened report counts again, got %d", n)
	}
	err := st.Update(ctx, func(tx *Tx) error { return tx.SetReportState(ctx, 999, ReportDismissed, "", "", testNow) })
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown report must be ErrNotFound, got %v", err)
	}
}

func TestResolveVersionReportsTouchesOnlyOpenOnes(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := st.InsertReport(ctx, Report{SetID: "s", Version: 1, KeyHMAC: keyNamed(i), ASNObserved: "64500", ReceivedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.InsertReport(ctx, Report{SetID: "s", Version: 2, KeyHMAC: keyNamed(1), ASNObserved: "64500", ReceivedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	reports, _ := st.ReportsForVersion(ctx, "s", 1)
	var n int
	err := st.Update(ctx, func(tx *Tx) error {
		if err := tx.SetReportState(ctx, reports[0].ID, ReportDismissed, "", "", testNow); err != nil {
			return err
		}
		var err error
		n, err = tx.ResolveVersionReports(ctx, "s", 1, ReportResolved, ResolutionHidden, "", testNow)
		return err
	})
	if err != nil || n != 2 {
		t.Fatalf("two open reports on the version, got %d %v", n, err)
	}
	counts, _ := st.ReportCounts(ctx)
	if counts[ReportOpen] != 1 || counts[ReportDismissed] != 1 || counts[ReportResolved] != 2 {
		t.Fatalf("counts after resolving: %v", counts)
	}
	err = st.Update(ctx, func(tx *Tx) error {
		var err error
		n, err = tx.ResolveSetReports(ctx, "s", ReportResolved, ResolutionWithdrawn, "", testNow)
		return err
	})
	if err != nil || n != 1 {
		t.Fatalf("withdrawing the set resolves the remaining open report of any version, got %d %v", n, err)
	}
}

func TestQueryReportsPagesWithACursor(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	offsets := []time.Duration{0, time.Minute, 2 * time.Minute, 3 * time.Minute, 3 * time.Minute, 3 * time.Minute}
	for i, off := range offsets {
		r := Report{SetID: "s", Version: 1 + i%2, KeyHMAC: keyNamed(i), ASNObserved: fmt.Sprint(64500 + i%3), Reason: fmt.Sprint("r", i+1), ReceivedAt: testNow.Add(off)}
		if err := st.InsertReport(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	var order []string
	cursor := ""
	for page := 0; page < 5; page++ {
		items, total, next, err := st.QueryReports(ctx, ReportFilter{Before: cursor, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if total != 6 {
			t.Fatalf("total counts the whole filter, got %d", total)
		}
		for _, it := range items {
			order = append(order, it.Reason)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if got := fmt.Sprint(order); got != "[r6 r5 r4 r3 r2 r1]" {
		t.Fatalf("pages must walk newest first and break ties by id without repeats: %s", got)
	}

	all, _, _, _ := st.QueryReports(ctx, ReportFilter{})
	if err := st.Update(ctx, func(tx *Tx) error {
		return tx.SetReportState(ctx, all[0].ID, ReportResolved, ResolutionHidden, "", testNow)
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		f     ReportFilter
		total int
	}{
		{"open", ReportFilter{State: ReportOpen}, 5},
		{"resolved", ReportFilter{State: ReportResolved}, 1},
		{"version", ReportFilter{SetID: "s", Version: 2}, 3},
		{"key", ReportFilter{KeyHMAC: keyNamed(0)}, 1},
		{"asn", ReportFilter{ASN: "64500"}, 2},
		{"since", ReportFilter{Since: testNow.Add(2 * time.Minute)}, 4},
		{"until", ReportFilter{Until: testNow.Add(2 * time.Minute)}, 2},
		{"other set", ReportFilter{SetID: "t"}, 0},
	} {
		items, total, _, err := st.QueryReports(ctx, c.f)
		if err != nil || total != c.total || len(items) != c.total {
			t.Errorf("%s: total %d items %d %v, want %d", c.name, total, len(items), err, c.total)
		}
	}
	if _, _, _, err := st.QueryReports(ctx, ReportFilter{Before: "garbage"}); err != nil {
		t.Errorf("a malformed cursor is ignored, got %v", err)
	}
}

func TestAutoHideAuditsOnceAsSystem(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	active := "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	pending := "01ARZ3NDEKTSV4RRFFQ69G5FA2"
	addSetWith(t, st, active, "a", hubwire.SetStatusActive)
	addSetWith(t, st, pending, "a", hubwire.SetStatusPending)

	hidden, err := st.AutoHide(ctx, active, 1, "reports", 3, testNow)
	if err != nil || !hidden {
		t.Fatalf("an active version is hidden, got %v %v", hidden, err)
	}
	v, _ := st.GetVersion(ctx, active, 1)
	if v.Status != hubwire.SetStatusHidden || v.StatusReason != "reports" || v.HiddenFrom != hubwire.SetStatusActive {
		t.Fatalf("auto-hidden version: %+v", v)
	}
	entries, _, err := st.AuditLog(ctx, AuditQuery{TargetKind: TargetSet, TargetID: active})
	if err != nil || len(entries) != 1 {
		t.Fatalf("one audit row expected: %+v %v", entries, err)
	}
	e := entries[0]
	if e.Actor != ActorSystem || e.ActorRef != "reports" || e.Action != "set.hide" || e.Version != 1 || e.Reason != "reports" {
		t.Errorf("audit row: %+v", e)
	}
	if e.Before["status"] != "active" || e.After["status"] != "hidden" || e.After["independent_reports"] != float64(3) {
		t.Errorf("audit snapshots: %v %v", e.Before, e.After)
	}

	if hidden, err = st.AutoHide(ctx, active, 1, "reports", 4, testNow); err != nil || hidden {
		t.Fatalf("a hidden version is not hidden again, got %v %v", hidden, err)
	}
	if hidden, err = st.AutoHide(ctx, pending, 1, "reports", 3, testNow); err != nil || hidden {
		t.Fatalf("a pending version is not auto-hidden, got %v %v", hidden, err)
	}
	if v, _ = st.GetVersion(ctx, pending, 1); v.Status != hubwire.SetStatusPending {
		t.Fatalf("pending version must stay pending: %+v", v)
	}
	if entries, _, _ = st.AuditLog(ctx, AuditQuery{Actor: ActorSystem}); len(entries) != 1 {
		t.Fatalf("no-op auto-hides must not be audited: %+v", entries)
	}
	if _, err := st.AutoHide(ctx, active, 9, "reports", 3, testNow); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown version must be ErrNotFound, got %v", err)
	}
}

func TestAuditLogFiltersAndPages(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	add := func(at time.Duration, actor, action, kind, id string) int64 {
		t.Helper()
		n, err := st.Audit(ctx, AuditEntry{At: testNow.Add(at), Actor: actor, Action: action, TargetKind: kind, TargetID: id,
			Before: map[string]interface{}{"status": "pending"}, After: map[string]interface{}{"status": "active", "listed": 1}})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	first := add(0, ActorConsole, "set.approve", TargetSet, "a")
	add(time.Minute, ActorBasic, "set.hide", TargetSet, "a")
	add(2*time.Minute, ActorCLI, "key.ban", TargetKey, "k")
	add(3*time.Minute, ActorConsole, "set.restore", TargetSet, "a")
	add(4*time.Minute, ActorSystem, "set.hide", TargetSet, "b")
	if _, err := st.Audit(ctx, AuditEntry{At: testNow, Actor: ActorCLI, Action: "catalogue.epoch", TargetKind: TargetCatalogue}); err != nil {
		t.Fatal(err)
	}

	var ids []int64
	var before int64
	for page := 0; page < 5; page++ {
		items, next, err := st.AuditLog(ctx, AuditQuery{TargetKind: TargetSet, TargetID: "a", Limit: 2, Before: before})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range items {
			ids = append(ids, e.ID)
		}
		if next == 0 {
			break
		}
		before = next
	}
	if len(ids) != 3 || ids[2] != first || ids[0] <= ids[1] {
		t.Fatalf("paging over one target must list its three rows newest first: %v", ids)
	}

	for _, c := range []struct {
		name string
		q    AuditQuery
		want int
	}{
		{"exact action", AuditQuery{Action: "set.hide"}, 2},
		{"action prefix", AuditQuery{Action: "set."}, 4},
		{"actor", AuditQuery{Actor: ActorConsole}, 2},
		{"kind", AuditQuery{TargetKind: TargetKey}, 1},
		{"since", AuditQuery{Since: testNow.Add(3 * time.Minute)}, 2},
		{"until", AuditQuery{Until: testNow.Add(time.Minute)}, 2},
		{"all", AuditQuery{}, 6},
	} {
		items, _, err := st.AuditLog(ctx, c.q)
		if err != nil || len(items) != c.want {
			t.Errorf("%s: %d rows %v, want %d", c.name, len(items), err, c.want)
		}
	}
	items, _, _ := st.AuditLog(ctx, AuditQuery{Action: "set.approve"})
	if items[0].Before["status"] != "pending" || items[0].After["listed"] != float64(1) || !items[0].At.Equal(testNow) {
		t.Errorf("snapshots must round-trip: %+v", items[0])
	}
	items, _, _ = st.AuditLog(ctx, AuditQuery{Action: "catalogue.epoch"})
	if items[0].Before != nil || items[0].After != nil {
		t.Errorf("an entry without snapshots reads back without them: %+v", items[0])
	}
}

func TestBuildHistoryRecordsRuns(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	finish := func(at time.Duration, ok bool, changes BuildChanges) int64 {
		t.Helper()
		id, err := st.StartBuild(ctx, "manual", testNow.Add(at))
		if err != nil {
			t.Fatal(err)
		}
		run := BuildRun{ID: id, OK: ok, StartedAt: testNow.Add(at), FinishedAt: testNow.Add(at + time.Second), Changes: changes, Seq: id}
		if !ok {
			run.Error = "disk full"
		}
		if err := st.FinishBuild(ctx, run); err != nil {
			t.Fatal(err)
		}
		return id
	}
	added := BuildChanges{Added: []BuildSetRef{{SetID: "s", Version: 1, Title: "S"}}}
	old := finish(-40*24*time.Hour, true, added)
	quiet := finish(-3*24*time.Hour, true, BuildChanges{Rescored: 2})
	failed := finish(-time.Hour, false, BuildChanges{})
	latest := finish(0, true, added)
	if _, err := st.StartBuild(ctx, "schedule", testNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	runs, next, err := st.BuildRuns(ctx, BuildQuery{})
	if err != nil || len(runs) != 4 || next != 0 {
		t.Fatalf("an unfinished build is not listed: %d %d %v", len(runs), next, err)
	}
	if runs[0].ID != latest || !runs[0].OK || len(runs[0].Changes.Added) != 1 || runs[0].Changes.Added[0].SetID != "s" {
		t.Fatalf("newest run: %+v", runs[0])
	}
	if runs, _, _ = st.BuildRuns(ctx, BuildQuery{OnlyChanges: true}); len(runs) != 3 {
		t.Fatalf("changes include failures and exclude rescore-only runs: %+v", runs)
	}
	if runs, _, _ = st.BuildRuns(ctx, BuildQuery{OnlyFailed: true}); len(runs) != 1 || runs[0].ID != failed || runs[0].Error != "disk full" {
		t.Fatalf("failed runs: %+v", runs)
	}
	if runs, next, _ = st.BuildRuns(ctx, BuildQuery{Limit: 1}); len(runs) != 1 || next != latest {
		t.Fatalf("paging: %+v %d", runs, next)
	}
	if runs, _, _ = st.BuildRuns(ctx, BuildQuery{Before: next}); len(runs) != 3 || runs[0].ID != failed {
		t.Fatalf("second page: %+v", runs)
	}
	if last, _ := st.LastBuild(ctx, true); last == nil || last.ID != latest {
		t.Fatalf("last good build: %+v", last)
	}
	if last, _ := st.LastBuild(ctx, false); last == nil || last.ID != failed {
		t.Fatalf("last failed build: %+v", last)
	}

	if err := st.PruneBuilds(ctx, testNow, 2*24*time.Hour, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	runs, _, _ = st.BuildRuns(ctx, BuildQuery{})
	kept := map[int64]bool{}
	for _, r := range runs {
		kept[r.ID] = true
	}
	if kept[old] || kept[quiet] || !kept[failed] || !kept[latest] {
		t.Fatalf("pruning drops old unchanged runs and anything past the max age: %v", kept)
	}
}

func TestMirrorChecksDecideAnnouncement(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	approved, err := st.AnnounceMirror(ctx, "https://a.example", "k", "1.0.0", testNow)
	if err != nil {
		t.Fatal(err)
	}
	rejected, _ := st.AnnounceMirror(ctx, "https://b.example", "k", "1.0.0", testNow)
	pending, _ := st.AnnounceMirror(ctx, "https://c.example", "k", "1.0.0", testNow)
	if err := st.SetMirrorStatus(ctx, approved.ID, MirrorApproved, "", testNow); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMirrorStatus(ctx, rejected.ID, MirrorRejected, "spam", testNow); err != nil {
		t.Fatal(err)
	}
	window := 24 * time.Hour
	if urls, _ := st.AnnounceableMirrors(ctx, testNow, window); len(urls) != 0 {
		t.Fatalf("an approved mirror without a passing check is not announced: %v", urls)
	}
	pass := MirrorCheck{At: testNow, OK: true, Millis: 12, Epoch: 5, Seq: 9, GeneratedAt: "2026-09-12T12:00:00Z"}
	for _, m := range []*Mirror{approved, rejected, pending} {
		if err := st.RecordMirrorCheck(ctx, m.ID, pass); err != nil {
			t.Fatal(err)
		}
	}
	urls, _ := st.AnnounceableMirrors(ctx, testNow, window)
	if len(urls) != 1 || urls[0] != approved.URL {
		t.Fatalf("only the approved mirror with a passing check is announced: %v", urls)
	}
	m, _ := st.GetMirror(ctx, approved.ID)
	if !m.Healthy() || m.ServedEpoch != 5 || m.ServedSeq != 9 || m.CheckMillis != 12 || m.ServedGeneratedAt != pass.GeneratedAt {
		t.Fatalf("a passing check records what the mirror serves: %+v", m)
	}

	later := testNow.Add(time.Hour)
	fail := MirrorCheck{At: later, Code: "health", Error: "returned 503", Millis: 40}
	if err := st.RecordMirrorCheck(ctx, approved.ID, fail); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordMirrorCheck(ctx, rejected.ID, fail); err != nil {
		t.Fatal(err)
	}
	m, _ = st.GetMirror(ctx, approved.ID)
	if m.Healthy() || m.CheckCode != "health" || m.CheckError != "returned 503" || !m.LastOK.Equal(testNow) || m.ServedSeq != 9 || m.Reason != "" {
		t.Fatalf("a failed check keeps the last good state: %+v", m)
	}
	if urls, _ = st.AnnounceableMirrors(ctx, later, window); len(urls) != 1 {
		t.Fatalf("a mirror stays announced within the window after a failed check: %v", urls)
	}
	if urls, _ = st.AnnounceableMirrors(ctx, testNow.Add(window+time.Second), window); len(urls) != 0 {
		t.Fatalf("a mirror drops once its last pass is older than the window: %v", urls)
	}
	if r, _ := st.GetMirror(ctx, rejected.ID); r.Reason != "spam" || r.Status != MirrorRejected || r.CheckError != "returned 503" {
		t.Fatalf("a check must never overwrite the moderation reason: %+v", r)
	}
	if err := st.SetMirrorStatus(ctx, approved.ID, "bogus", "", testNow); err == nil {
		t.Fatalf("an unknown mirror status must be refused")
	}
}

func TestMigrationSevenBackfillsReportStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	all := migrations
	t.Cleanup(func() { migrations = all })
	migrations = all[:6]
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	stamp := func(hours int) string { return formatTime(testNow.Add(time.Duration(hours) * time.Hour)) }
	exec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := old.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	versions := []struct {
		set, status, reason string
	}{
		{"A", "active", ""},
		{"B", "rejected", "private domain"},
		{"C", "hidden", "reports"},
		{"D", "hidden", "breaks video"},
		{"E", "pending", ""},
	}
	for _, v := range versions {
		exec(`INSERT INTO sets(id, author_hmac, current_version, created_at, updated_at) VALUES(?, 'author', 1, ?, ?)`, v.set, stamp(0), stamp(2))
		exec(`INSERT INTO set_versions(set_id, version, fp, targets_key, title, projection_json, status, status_reason, created_at, updated_at)
			VALUES(?, 1, ?, ?, ?, '{}', ?, ?, ?, ?)`, v.set, "fp-"+v.set, "tk-"+v.set, "title "+v.set, v.status, v.reason, stamp(0), stamp(2))
	}
	reports := []struct {
		label, set string
		hours      int
	}{
		{"active-before", "A", 1},
		{"active-same", "A", 2},
		{"active-after", "A", 3},
		{"rejected-after", "B", 3},
		{"by-reports", "C", 1},
		{"hidden-before", "D", 1},
		{"hidden-after", "D", 3},
		{"pending", "E", 1},
	}
	for _, r := range reports {
		exec(`INSERT INTO reports(set_id, version, key_hmac, asn_observed, reason, received_at) VALUES(?, 1, 'k', '64500', ?, ?)`, r.set, r.label, stamp(r.hours))
	}
	exec(`INSERT INTO mirrors(url, key_hmac, first_seen, last_seen, status, reason) VALUES('https://up.example', 'k', ?, ?, 'approved', 'timeout')`, stamp(0), stamp(0))
	exec(`INSERT INTO mirrors(url, key_hmac, first_seen, last_seen, status, reason) VALUES('https://spam.example', 'k', ?, ?, 'rejected', 'spam')`, stamp(0), stamp(0))
	exec(`INSERT INTO mirrors(url, key_hmac, first_seen, last_seen, status, reason) VALUES('https://new.example', 'k', ?, ?, 'pending', '')`, stamp(0), stamp(0))
	if v, _ := old.SchemaVersion(ctx); v != 6 {
		t.Fatalf("the fixture database must be at schema 6, got %d", v)
	}
	old.Close()

	migrations = all
	st, err := Open(path)
	if err != nil {
		t.Fatalf("migration 7 must apply to a populated database: %v", err)
	}
	defer st.Close()
	if v, _ := st.SchemaVersion(ctx); v != len(all) {
		t.Fatalf("schema after upgrade: %d", v)
	}
	if _, err := os.Stat(backupPath(path, 6)); err != nil {
		t.Fatalf("the v6 database must be backed up first: %v", err)
	}

	got, err := st.AllReports(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byLabel := map[string]Report{}
	for _, r := range got {
		byLabel[r.Reason] = r
	}
	want := map[string][2]string{
		"active-before":  {ReportDismissed, ResolutionRestored},
		"active-same":    {ReportDismissed, ResolutionRestored},
		"active-after":   {ReportOpen, ""},
		"rejected-after": {ReportResolved, ResolutionRejected},
		"by-reports":     {ReportOpen, ""},
		"hidden-before":  {ReportResolved, ResolutionHidden},
		"hidden-after":   {ReportOpen, ""},
		"pending":        {ReportOpen, ""},
	}
	for label, w := range want {
		r, ok := byLabel[label]
		if !ok {
			t.Errorf("report %s lost in the migration", label)
			continue
		}
		if r.State != w[0] || r.Resolution != w[1] {
			t.Errorf("report %s: state %q resolution %q, want %q %q", label, r.State, r.Resolution, w[0], w[1])
		}
		closed := r.State != ReportOpen
		if closed && !r.ResolvedAt.Equal(testNow.Add(2*time.Hour)) {
			t.Errorf("report %s must be resolved at the version's last change, got %v", label, r.ResolvedAt)
		}
		if !closed && !r.ResolvedAt.IsZero() {
			t.Errorf("open report %s must carry no resolution time, got %v", label, r.ResolvedAt)
		}
	}
	if n, _ := st.IndependentReports(ctx, "A", 1); n != 1 {
		t.Errorf("only the report newer than the restore still counts, got %d", n)
	}

	mirrors, err := st.Mirrors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byURL := map[string]Mirror{}
	for _, m := range mirrors {
		byURL[m.URL] = m
	}
	if m := byURL["https://up.example"]; m.Reason != "" || m.CheckError != "timeout" || m.CheckCode != "unknown" {
		t.Errorf("a health failure stored as the reason moves to the check columns: %+v", m)
	}
	if m := byURL["https://spam.example"]; m.Reason != "spam" || m.CheckError != "" || m.CheckCode != "" {
		t.Errorf("a moderation reason stays the reason: %+v", m)
	}
	if m := byURL["https://new.example"]; m.Reason != "" || m.CheckError != "" || m.CheckCode != "" {
		t.Errorf("an unchecked mirror stays blank: %+v", m)
	}

	d, err := st.GetVersion(ctx, "D", 1)
	if err != nil || d.HiddenFrom != "" {
		t.Fatalf("versions hidden before the migration carry no origin: %+v %v", d, err)
	}
	var target string
	if err := st.Update(ctx, func(tx *Tx) error {
		var err error
		target, err = tx.Restore(ctx, "D", 1, testNow)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if target != hubwire.SetStatusActive {
		t.Errorf("a version hidden before the migration restores to active, got %q", target)
	}
	set, _, err := st.GetSet(ctx, "A")
	if err != nil || !set.WithdrawnAt.IsZero() || set.WithdrawReason != "" {
		t.Errorf("existing sets are not withdrawn: %+v %v", set, err)
	}
	if entries, _, err := st.AuditLog(ctx, AuditQuery{}); err != nil || len(entries) != 0 {
		t.Errorf("the audit log starts empty: %v %v", entries, err)
	}
	if runs, _, err := st.BuildRuns(ctx, BuildQuery{}); err != nil || len(runs) != 0 {
		t.Errorf("the build history starts empty: %v %v", runs, err)
	}
}

func TestLegacyHiddenVersionsRestoreToWhereTheyCameFrom(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	all := migrations
	t.Cleanup(func() { migrations = all })
	migrations = all[:6]
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	exec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := old.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	stamp := formatTime(testNow)
	exec(`INSERT INTO sets(id, author_hmac, current_version, created_at, updated_at) VALUES('L', 'author', 1, ?, ?)`, stamp, stamp)
	exec(`INSERT INTO sets(id, author_hmac, current_version, created_at, updated_at) VALUES('N', 'author', 0, ?, ?)`, stamp, stamp)
	for _, v := range []struct {
		set     string
		version int
	}{{"L", 1}, {"L", 2}, {"N", 1}} {
		exec(`INSERT INTO set_versions(set_id, version, fp, targets_key, title, projection_json, status, status_reason, created_at, updated_at)
			VALUES(?, ?, ?, ?, 'legacy', '{}', 'hidden', 'old', ?, ?)`, v.set, v.version, fmt.Sprintf("fp-%s-%d", v.set, v.version), fmt.Sprintf("tk-%s-%d", v.set, v.version), stamp, stamp)
	}
	old.Close()

	migrations = all
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, c := range []struct {
		set     string
		version int
		from    string
	}{{"L", 1, ""}, {"L", 2, "pending"}, {"N", 1, "pending"}} {
		v, err := st.GetVersion(ctx, c.set, c.version)
		if err != nil {
			t.Fatal(err)
		}
		if v.HiddenFrom != c.from {
			t.Fatalf("%s/%d: hidden_from %q, want %q", c.set, c.version, v.HiddenFrom, c.from)
		}
	}
}

func TestReportsFromTestKeysDoNotCount(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	for i, key := range []string{"k1", "k2", "k3"} {
		if _, err := st.db.ExecContext(ctx, `INSERT INTO keys(key_hmac, first_seen) VALUES(?, ?)`, key, formatTime(testNow)); err != nil {
			t.Fatal(err)
		}
		if err := st.InsertReport(ctx, Report{SetID: "S", Version: 1, KeyHMAC: key, ASNObserved: fmt.Sprintf("6450%d", i), Reason: "bad", ReceivedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := st.IndependentReports(ctx, "S", 1); n != 3 {
		t.Fatalf("three independent reports: %d", n)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE keys SET tag = 'test' WHERE key_hmac = 'k3'`); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.IndependentReports(ctx, "S", 1); n != 2 {
		t.Fatalf("a test key's report must not count toward the automatic hide: %d", n)
	}
}

func TestAutoHideReportedRecountsInsideItsTransaction(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	active := "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	addSetWith(t, st, active, "a", hubwire.SetStatusActive)
	for i, asn := range []string{"64500", "64501", "64502"} {
		if err := st.InsertReport(ctx, Report{RecordID: "record-" + asn, SetID: active, Version: 1, KeyHMAC: "reporter-" + asn, ASNObserved: asn, Reason: "steals traffic", ReceivedAt: testNow.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := st.IndependentReports(ctx, active, 1); err != nil || n != 3 {
		t.Fatalf("three independent reports, got %d %v", n, err)
	}
	if err := st.BanKey(ctx, "reporter-64502", "spam", testNow); err != nil {
		t.Fatal(err)
	}
	hidden, err := st.AutoHideReported(ctx, active, 1, "reports", 3, testNow)
	if err != nil || hidden {
		t.Fatalf("a count that fell below the threshold before the hide must not hide, got %v %v", hidden, err)
	}
	if v, _ := st.GetVersion(ctx, active, 1); v.Status != hubwire.SetStatusActive {
		t.Fatalf("the version stays listed: %+v", v)
	}
	if err := st.UnbanKey(ctx, "reporter-64502"); err != nil {
		t.Fatal(err)
	}
	if hidden, err = st.AutoHideReported(ctx, active, 1, "reports", 3, testNow); err != nil || !hidden {
		t.Fatalf("three counting reports hide it, got %v %v", hidden, err)
	}
	entries, _, err := st.AuditLog(ctx, AuditQuery{TargetKind: TargetSet, TargetID: active})
	if err != nil || len(entries) != 1 || entries[0].After["independent_reports"] != float64(3) {
		t.Fatalf("the hide is audited with the count it was decided on: %+v %v", entries, err)
	}
}
