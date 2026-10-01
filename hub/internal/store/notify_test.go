package store

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
)

var notifyClock = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

func openNotifyStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func notifyShare(t *testing.T, st *Store, n int, uploader, status string) string {
	t.Helper()
	id := "01ARZ3NDEKTSV4RRFFQ69G5F" + strconv.Itoa(10+n)
	v := &Version{
		FP:              "fp" + strconv.Itoa(n),
		TargetsKey:      "tk" + strconv.Itoa(n),
		Title:           "Set " + strconv.Itoa(n),
		Projection:      map[string]interface{}{},
		Status:          status,
		UploaderHMAC:    uploader,
		ASNObserved:     "64500",
		CountryObserved: "RU",
		CreatedAt:       notifyClock,
		UpdatedAt:       notifyClock,
	}
	if err := st.CreateSet(context.Background(), Set{ID: id, AuthorHMAC: uploader, CreatedAt: notifyClock, UpdatedAt: notifyClock}, v); err != nil {
		t.Fatal(err)
	}
	return id
}

func tagKey(t *testing.T, st *Store, key, tag string) {
	t.Helper()
	ctx := context.Background()
	if _, _, err := st.TouchKey(ctx, key, notifyClock); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE keys SET tag = ? WHERE key_hmac = ?`, tag, key); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyEventsFilterEachSource(t *testing.T) {
	st := openNotifyStore(t)
	ctx := context.Background()
	tagKey(t, st, "testkey", TagTest)

	pending := notifyShare(t, st, 1, "author", hubwire.SetStatusPending)
	notifyShare(t, st, 2, "testkey", hubwire.SetStatusPending)
	active := notifyShare(t, st, 3, "author", hubwire.SetStatusActive)

	for _, key := range []string{"reporter", "testkey"} {
		if err := st.InsertReport(ctx, Report{SetID: active, Version: 1, KeyHMAC: key, ASNObserved: "64501", Reason: "broken", ReceivedAt: notifyClock}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.AutoHide(ctx, active, 1, "reports", 3, notifyClock); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Audit(ctx, AuditEntry{At: notifyClock, Actor: ActorConsole, Action: "set.hide", TargetKind: TargetSet, TargetID: pending, Version: 1}); err != nil {
		t.Fatal(err)
	}

	okBuild, _ := st.StartBuild(ctx, "manual", notifyClock)
	if err := st.FinishBuild(ctx, BuildRun{ID: okBuild, OK: true, FinishedAt: notifyClock}); err != nil {
		t.Fatal(err)
	}
	failed, _ := st.StartBuild(ctx, "auto", notifyClock)
	if err := st.FinishBuild(ctx, BuildRun{ID: failed, OK: false, Error: "disk full", FinishedAt: notifyClock}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartBuild(ctx, "auto", notifyClock); err != nil {
		t.Fatal(err)
	}

	approved, err := st.AnnounceMirror(ctx, "https://a.example", "k1", "1.0", notifyClock)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMirrorStatus(ctx, approved.ID, MirrorApproved, "", notifyClock); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AnnounceMirror(ctx, "https://b.example", "k2", "1.1", notifyClock); err != nil {
		t.Fatal(err)
	}

	want := map[string]int{NotifyShare: 1, NotifyReport: 1, NotifyAutoHide: 1, NotifyBuildFailed: 1, NotifyMirror: 1}
	for kind, n := range want {
		b, err := st.NotifyEvents(ctx, kind, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		if b.Count != n || len(b.Events) != n {
			t.Fatalf("%s: expected %d events, got %+v", kind, n, b)
		}
		e := b.Events[0]
		if b.MaxID != e.ID || e.At.IsZero() {
			t.Errorf("%s: max id and time must be carried, got %+v", kind, b)
		}
		switch kind {
		case NotifyShare:
			if e.SetID != pending || e.Title != "Set 1" || e.KeyHMAC != "author" || e.ASN != "64500" || e.Country != "RU" {
				t.Errorf("share event: %+v", e)
			}
		case NotifyReport:
			if e.SetID != active || e.Title != "Set 3" || e.KeyHMAC != "reporter" || e.Reason != "broken" {
				t.Errorf("report event: %+v", e)
			}
		case NotifyAutoHide:
			if e.SetID != active || e.Reports != 3 || e.Title != "Set 3" {
				t.Errorf("auto-hide event: %+v", e)
			}
		case NotifyBuildFailed:
			if e.ID != failed || e.Reason != "disk full" || e.Detail != "auto" {
				t.Errorf("build event: %+v", e)
			}
		case NotifyMirror:
			if e.URL != "https://b.example" || e.Detail != "1.1" {
				t.Errorf("mirror event: %+v", e)
			}
		}
	}
	if _, err := st.NotifyEvents(ctx, "vote", 0, 10); err == nil {
		t.Error("an unknown source must be refused")
	}
}

func TestNotifyEventsCountBeyondLimitAndAfterCursor(t *testing.T) {
	st := openNotifyStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		notifyShare(t, st, i, "author", hubwire.SetStatusPending)
	}
	b, err := st.NotifyEvents(ctx, NotifyShare, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if b.Count != 5 || len(b.Events) != 2 || b.MaxID != 5 || b.Events[0].ID != 1 {
		t.Fatalf("the count and max id cover every row, the list only the first ones: %+v", b)
	}
	b, _ = st.NotifyEvents(ctx, NotifyShare, 3, 10)
	if b.Count != 2 || b.Events[0].ID != 4 {
		t.Fatalf("rows at or below the cursor must be skipped: %+v", b)
	}
	heads, err := st.NotifyMaxIDs(ctx)
	if err != nil || heads[NotifyShare] != 5 || heads[NotifyReport] != 0 || len(heads) != len(NotifySources) {
		t.Fatalf("max ids: %v %v", heads, err)
	}
}

func TestNotifyCursorsOnlyMoveForward(t *testing.T) {
	st := openNotifyStore(t)
	ctx := context.Background()
	if got, err := st.NotifyCursors(ctx, "telegram"); err != nil || len(got) != 0 {
		t.Fatalf("no cursors yet: %v %v", got, err)
	}
	if err := st.AdvanceNotifyCursors(ctx, "telegram", map[string]int64{NotifyShare: 7, NotifyReport: 3}); err != nil {
		t.Fatal(err)
	}
	if err := st.AdvanceNotifyCursors(ctx, "telegram", map[string]int64{NotifyShare: 5, NotifyReport: 12}); err != nil {
		t.Fatal(err)
	}
	if err := st.AdvanceNotifyCursors(ctx, "webhook", map[string]int64{NotifyShare: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := st.NotifyCursors(ctx, "telegram")
	if err != nil || len(got) != 2 || got[NotifyShare] != 7 || got[NotifyReport] != 12 {
		t.Fatalf("cursors must never move back and stay per channel: %v %v", got, err)
	}
	if got, _ := st.NotifyCursors(ctx, "webhook"); len(got) != 1 || got[NotifyShare] != 1 {
		t.Fatalf("webhook cursors: %v", got)
	}
}
