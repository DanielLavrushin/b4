package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
)

var testNow = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hub.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func sampleVersion(fp, targetsKey string) *Version {
	return &Version{
		FP:         fp,
		TargetsKey: targetsKey,
		Title:      "sample",
		Projection: map[string]interface{}{"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}}},
		Status:     hubwire.SetStatusPending,
		B4Min:      hubwire.BaselineVersion,
		CreatedAt:  testNow,
		UpdatedAt:  testNow,
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := first.SchemaVersion(context.Background())
	if err != nil || v != len(migrations) {
		t.Fatalf("schema version after first open: %d %v", v, err)
	}
	first.Close()
	second, err := Open(path)
	if err != nil {
		t.Fatalf("reopening an existing database must not fail: %v", err)
	}
	defer second.Close()
	for _, table := range []string{"meta", "keys", "records", "sets", "set_versions", "votes", "reports", "asn_names"} {
		var n int
		if err := second.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s missing after migration: %d %v", table, n, err)
		}
	}
	var mode string
	if err := second.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal mode must be wal, got %q %v", mode, err)
	}
}

func TestEpochAndSeq(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	epoch, seq, err := st.NextSeq(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if epoch != testNow.Unix() || seq != 1 {
		t.Errorf("first build must start the epoch at now with seq 1, got %d %d", epoch, seq)
	}
	_, seq, _ = st.NextSeq(ctx, testNow)
	if seq != 2 {
		t.Errorf("seq must increment, got %d", seq)
	}
	fresh, err := st.NewEpoch(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if fresh <= epoch {
		t.Errorf("a new epoch must be newer than the old one: %d vs %d", fresh, epoch)
	}
	e, seq, _ := st.NextSeq(ctx, testNow)
	if e != fresh || seq != 1 {
		t.Errorf("seq must restart at 1 in a new epoch, got %d %d", e, seq)
	}
}

func TestDuplicateRuleNeedsSameFingerprintAndTargets(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if err := st.CreateSet(ctx, Set{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", AuthorHMAC: "author", CreatedAt: testNow, UpdatedAt: testNow}, sampleVersion("fp-a", "targets-a")); err != nil {
		t.Fatal(err)
	}
	if v, err := st.FindDuplicate(ctx, "fp-a", "targets-a"); err != nil || v.SetID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" || v.Version != 1 {
		t.Errorf("same fingerprint and targets must be a duplicate, got %+v %v", v, err)
	}
	if _, err := st.FindDuplicate(ctx, "fp-a", "targets-b"); !errors.Is(err, ErrNotFound) {
		t.Errorf("same fingerprint with other targets is a new set, got %v", err)
	}
	if _, err := st.FindDuplicate(ctx, "fp-b", "targets-a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("other fingerprint with the same targets is a new set, got %v", err)
	}
	if err := st.Reject(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "spam", testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindDuplicate(ctx, "fp-a", "targets-a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a rejected version must not block a re-upload, got %v", err)
	}
}

func TestPendingQueueAndListing(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	const id = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := st.CreateSet(ctx, Set{ID: id, AuthorHMAC: "author", CreatedAt: testNow, UpdatedAt: testNow}, sampleVersion("fp-a", "targets-a")); err != nil {
		t.Fatal(err)
	}
	pending, err := st.PendingVersions(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected one pending version, got %d %v", len(pending), err)
	}
	listed, _ := st.ListedVersions(ctx)
	if len(listed) != 0 {
		t.Errorf("a pending version must not be listed")
	}
	if err := st.Approve(ctx, id, 1, testNow); err != nil {
		t.Fatal(err)
	}
	listed, _ = st.ListedVersions(ctx)
	if len(listed) != 1 || listed[0].Status != hubwire.SetStatusActive {
		t.Fatalf("an approved version must be listed, got %+v", listed)
	}
	set, versions, err := st.GetSet(ctx, id)
	if err != nil || set.CurrentVersion != 1 || len(versions) != 1 {
		t.Errorf("approve must advance current_version: %+v %d %v", set, len(versions), err)
	}

	second := sampleVersion("fp-b", "targets-a")
	second.SetID = id
	if err := st.AddVersion(ctx, second); err != nil {
		t.Fatal(err)
	}
	if second.Version != 2 {
		t.Errorf("next version must be 2, got %d", second.Version)
	}
	listed, _ = st.ListedVersions(ctx)
	if len(listed) != 1 || listed[0].Version != 1 {
		t.Errorf("a pending later version must not replace the listed one, got %+v", listed)
	}
	if err := st.Approve(ctx, id, 2, testNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	listed, _ = st.ListedVersions(ctx)
	if len(listed) != 1 || listed[0].Version != 2 {
		t.Errorf("only the newest active version is listed, got %+v", listed)
	}
	if err := st.Hide(ctx, id, 2, "reports", testNow.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	listed, _ = st.ListedVersions(ctx)
	if len(listed) != 1 || listed[0].Version != 1 {
		t.Errorf("hiding the newest version falls back to the previous active one, got %+v", listed)
	}
	if err := st.Approve(ctx, "missing", 1, testNow); !errors.Is(err, ErrNotFound) {
		t.Errorf("approving an unknown version must fail with ErrNotFound, got %v", err)
	}
	dirty, _ := st.Dirty(ctx)
	if !dirty {
		t.Errorf("moderation must mark the store dirty")
	}
	if err := st.MarkBuilt(ctx, testNow); err != nil {
		t.Fatal(err)
	}
	if dirty, _ = st.Dirty(ctx); dirty {
		t.Errorf("a build must clear the dirty flag")
	}
}

func TestVoteNewestWinsPerBucket(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if _, _, err := st.TouchKey(ctx, "k1", testNow.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	base := Vote{SetID: "s", Version: 1, FP: "fp", KeyHMAC: "k1", Kind: "manual_works", Weight: 1, ASNObserved: "64500", OriginVerified: true, Bucket: 100, ReceivedAt: testNow}
	if err := st.UpsertVote(ctx, base); err != nil {
		t.Fatal(err)
	}
	older := base
	older.Kind = "manual_broken"
	older.Weight = -1
	older.ReceivedAt = testNow.Add(-time.Hour)
	if err := st.UpsertVote(ctx, older); err != nil {
		t.Fatal(err)
	}
	votes, _ := st.VotesForFP(ctx, "fp")
	if len(votes) != 1 || votes[0].Kind != "manual_works" {
		t.Fatalf("an older vote must not replace a newer one, got %+v", votes)
	}
	newer := older
	newer.ReceivedAt = testNow.Add(time.Hour)
	if err := st.UpsertVote(ctx, newer); err != nil {
		t.Fatal(err)
	}
	votes, _ = st.VotesForFP(ctx, "fp")
	if len(votes) != 1 || votes[0].Kind != "manual_broken" || votes[0].KeyFirstSeen.IsZero() {
		t.Fatalf("a newer vote must replace the older one and carry the key age, got %+v", votes)
	}
	otherBucket := base
	otherBucket.Bucket = 101
	if err := st.UpsertVote(ctx, otherBucket); err != nil {
		t.Fatal(err)
	}
	if votes, _ = st.VotesForFP(ctx, "fp"); len(votes) != 2 {
		t.Errorf("a different bucket is a separate vote, got %d", len(votes))
	}
}

func TestIndependentReportsCountDistinctKeysAndASNs(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	add := func(key, asn string) {
		if err := st.InsertReport(ctx, Report{SetID: "s", Version: 1, KeyHMAC: key, ASNObserved: asn, Reason: "x", ReceivedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	add("k1", "1")
	add("k2", "1")
	add("k1", "2")
	if n, _ := st.IndependentReports(ctx, "s", 1); n != 2 {
		t.Errorf("two keys over two asns give two independent reports, got %d", n)
	}
	add("k3", "")
	if n, _ := st.IndependentReports(ctx, "s", 1); n != 2 {
		t.Errorf("a report without an observed asn must not count, got %d", n)
	}
	add("k3", "3")
	if n, _ := st.IndependentReports(ctx, "s", 1); n != 3 {
		t.Errorf("three keys over three asns give three, got %d", n)
	}
}

func TestBanKeyWithoutPriorContact(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if err := st.BanKey(ctx, "k9", "abuse", testNow); err != nil {
		t.Fatal(err)
	}
	k, err := st.GetKey(ctx, "k9")
	if err != nil || !k.Banned || k.BanReason != "abuse" {
		t.Errorf("banning an unseen key must create it banned, got %+v %v", k, err)
	}
	if _, created, _ := st.TouchKey(ctx, "k9", testNow); created {
		t.Errorf("a banned key is already known")
	}
}

func TestMigrationBacksUpTheDatabaseFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	all := migrations
	migrations = all[:1]
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetMeta(context.Background(), "probe", "kept"); err != nil {
		t.Fatal(err)
	}
	first.Close()
	migrations = all
	if _, err := os.Stat(backupPath(path, 1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a fresh database must not be backed up: %v", err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if v, _ := second.SchemaVersion(context.Background()); v != len(all) {
		t.Fatalf("schema version %d after upgrade", v)
	}
	backup, err := Open(backupPath(path, 1))
	if err != nil {
		t.Fatalf("the backup must be a usable database: %v", err)
	}
	defer backup.Close()
	if v, _ := backup.Meta(context.Background(), "probe"); v != "kept" {
		t.Errorf("the backup must hold the pre-migration rows, got %q", v)
	}
}

func TestPruneBackupsKeepsTheNewest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	for _, v := range []int{1, 2, 3, 10} {
		if err := os.WriteFile(backupPath(path, v), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path+".vfoo.bak", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pruneBackups(path, keptBackups); err != nil {
		t.Fatal(err)
	}
	for v, want := range map[int]bool{1: false, 2: false, 3: true, 10: true} {
		_, err := os.Stat(backupPath(path, v))
		if got := err == nil; got != want {
			t.Errorf("backup v%d present %v, want %v", v, got, want)
		}
	}
	if _, err := os.Stat(path + ".vfoo.bak"); err != nil {
		t.Errorf("files that are not numbered backups are left alone: %v", err)
	}
}

func TestSettingsPersistAndDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	got, err := st.Settings(ctx)
	if err != nil || got != DefaultSettings() {
		t.Fatalf("fresh store must answer the defaults: %+v %v", got, err)
	}
	want := DefaultSettings()
	want.SharesPerDay = 42
	if err := st.SaveSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.Settings(ctx); got != want {
		t.Fatalf("saved settings not read back: %+v", got)
	}
	bad := want
	bad.VotesPerDay = 0
	if err := st.SaveSettings(ctx, bad); err == nil {
		t.Fatal("a zero limit must be refused")
	}
	st.Close()
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if got, _ = again.Settings(ctx); got != want {
		t.Fatalf("settings must survive reopening: %+v", got)
	}
}

func TestTrustKey(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if err := st.TrustKey(ctx, "k1", testNow); err != nil {
		t.Fatal(err)
	}
	k, err := st.GetKey(ctx, "k1")
	if err != nil || !k.Trusted || !k.TrustedAt.Equal(testNow) {
		t.Fatalf("trusting an unseen key must create it trusted, got %+v %v", k, err)
	}
	if err := st.UntrustKey(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
	if k, _ = st.GetKey(ctx, "k1"); k.Trusted || !k.TrustedAt.IsZero() {
		t.Fatalf("untrust must clear the flag, got %+v", k)
	}
	summaries, err := st.Keys(ctx)
	if err != nil || len(summaries) != 1 || summaries[0].Trusted {
		t.Fatalf("keys: %+v %v", summaries, err)
	}
}
