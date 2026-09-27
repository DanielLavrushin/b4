package ingest

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/moderation"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func listedSet(t *testing.T, f *fixture, name, domain string) string {
	t.Helper()
	resp := f.share(t, testkit.Identity(t), testkit.SampleSet(name, domain), peerA)
	expect(t, resp, http.StatusAccepted, "")
	setID := resp.Body["set_id"].(string)
	if err := f.store.Approve(context.Background(), setID, 1, f.clock); err != nil {
		t.Fatal(err)
	}
	return setID
}

func fileReport(t *testing.T, f *fixture, reporter *hubwire.Identity, setID string, peer net.IP) {
	t.Helper()
	body := hubwire.ReportBody{SetID: setID, Version: 1, Reason: "breaks the site"}
	expect(t, f.post(t, testkit.Sign(t, reporter, hubwire.RecordReport, body, f.clock), peer), http.StatusAccepted, "")
}

func statusOf(t *testing.T, f *fixture, setID string) string {
	t.Helper()
	v, err := f.store.GetVersion(context.Background(), setID, 1)
	if err != nil {
		t.Fatal(err)
	}
	return v.Status
}

func (f *fixture) mod() *moderation.Service {
	return &moderation.Service{Store: f.store, Now: func() time.Time { return f.clock }}
}

func TestAutoHideIsAuditedAsSystem(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	setID := listedSet(t, f, "Audited", "audited.example")
	for _, peer := range []net.IP{peerA, peerB, peerC} {
		fileReport(t, f, testkit.Identity(t), setID, peer)
	}
	if got := statusOf(t, f, setID); got != hubwire.SetStatusHidden {
		t.Fatalf("three independent reports hide the version, got %s", got)
	}
	entries, _, err := f.store.AuditLog(ctx, store.AuditQuery{TargetKind: store.TargetSet, TargetID: setID})
	if err != nil || len(entries) != 1 {
		t.Fatalf("the auto-hide writes one audit row: %+v %v", entries, err)
	}
	e := entries[0]
	if e.Actor != store.ActorSystem || e.ActorRef != ReasonReports || e.Action != "set.hide" || e.Reason != ReasonReports || e.Version != 1 {
		t.Errorf("audit row: %+v", e)
	}
	if e.Before["status"] != hubwire.SetStatusActive || e.After["status"] != hubwire.SetStatusHidden || e.After["independent_reports"] != float64(3) {
		t.Errorf("audit snapshots: %v %v", e.Before, e.After)
	}
	fileReport(t, f, testkit.Identity(t), setID, peerC)
	if entries, _, _ = f.store.AuditLog(ctx, store.AuditQuery{}); len(entries) != 1 {
		t.Fatalf("a report on a hidden version is stored without a second hide: %+v", entries)
	}
	if reports, _ := f.store.ReportsForVersion(ctx, setID, 1); len(reports) != 4 {
		t.Fatalf("every report is kept: %d", len(reports))
	}
}

func TestRestoreNeedsFreshReportsToHideAgain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	setID := listedSet(t, f, "Restored", "restored.example")
	for _, peer := range []net.IP{peerA, peerB, peerC} {
		fileReport(t, f, testkit.Identity(t), setID, peer)
	}
	if got := statusOf(t, f, setID); got != hubwire.SetStatusHidden {
		t.Fatalf("hidden by reports expected, got %s", got)
	}
	actor := moderation.Actor{Kind: store.ActorCLI}
	if _, err := f.mod().Moderate(ctx, actor, moderation.ActionRestore, []moderation.Ref{{SetID: setID, Version: 1}}, moderation.Options{}); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, f, setID); got != hubwire.SetStatusActive {
		t.Fatalf("restore returns the version to active, got %s", got)
	}
	reports, _ := f.store.ReportsForVersion(ctx, setID, 1)
	for _, r := range reports {
		if r.State != store.ReportDismissed || r.Resolution != store.ResolutionRestored {
			t.Fatalf("the restore dismisses the reports that hid it: %+v", r)
		}
	}

	fileReport(t, f, testkit.Identity(t), setID, peerC)
	if got := statusOf(t, f, setID); got != hubwire.SetStatusActive {
		t.Fatalf("one fresh report must not hide a restored version, got %s", got)
	}
	if n, _ := f.store.IndependentReports(ctx, setID, 1); n != 1 {
		t.Fatalf("only the fresh report counts, got %d", n)
	}
	fileReport(t, f, testkit.Identity(t), setID, peerA)
	fileReport(t, f, testkit.Identity(t), setID, peerB)
	if got := statusOf(t, f, setID); got != hubwire.SetStatusHidden {
		t.Fatalf("three fresh independent reports hide it again, got %s", got)
	}
}

func TestDismissedAndBannedReportsStopCounting(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	actor := moderation.Actor{Kind: store.ActorCLI}

	dismissed := listedSet(t, f, "Dismissed", "dismissed.example")
	fileReport(t, f, testkit.Identity(t), dismissed, peerA)
	fileReport(t, f, testkit.Identity(t), dismissed, peerB)
	reports, _ := f.store.ReportsForVersion(ctx, dismissed, 1)
	if _, err := f.mod().Reports(ctx, actor, []int64{reports[0].ID}, moderation.ActionDismiss, "noise"); err != nil {
		t.Fatal(err)
	}
	fileReport(t, f, testkit.Identity(t), dismissed, peerC)
	if got := statusOf(t, f, dismissed); got != hubwire.SetStatusActive {
		t.Fatalf("a dismissed report must not count toward the auto-hide, got %s", got)
	}
	if n, _ := f.store.IndependentReports(ctx, dismissed, 1); n != 2 {
		t.Fatalf("two open reports remain, got %d", n)
	}

	banned := listedSet(t, f, "Banned reporter", "banned.example")
	brigade := testkit.Identity(t)
	fileReport(t, f, brigade, banned, peerA)
	fileReport(t, f, testkit.Identity(t), banned, peerB)
	if err := f.store.BanKey(ctx, hubdata.KeyHMAC(f.svc.Secret, brigade.KeyID()), "brigade", f.clock); err != nil {
		t.Fatal(err)
	}
	fileReport(t, f, testkit.Identity(t), banned, peerC)
	if got := statusOf(t, f, banned); got != hubwire.SetStatusActive {
		t.Fatalf("a report from a key banned afterwards must not count, got %s", got)
	}
	if n, _ := f.store.IndependentReports(ctx, banned, 1); n != 2 {
		t.Fatalf("two unbanned reporters remain, got %d", n)
	}
}

func TestReportsOnAPendingVersionDoNotHideIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	resp := f.share(t, testkit.Identity(t), testkit.SampleSet("Queued", "queued.example"), peerA)
	expect(t, resp, http.StatusAccepted, "")
	setID := resp.Body["set_id"].(string)
	for _, peer := range []net.IP{peerA, peerB, peerC} {
		fileReport(t, f, testkit.Identity(t), setID, peer)
	}
	if got := statusOf(t, f, setID); got != hubwire.SetStatusPending {
		t.Fatalf("reports on a pending version are recorded for the moderator, got %s", got)
	}
	if n, _ := f.store.IndependentReports(ctx, setID, 1); n != 3 {
		t.Fatalf("the reports are stored and count, got %d", n)
	}
	if entries, _, _ := f.store.AuditLog(ctx, store.AuditQuery{}); len(entries) != 0 {
		t.Fatalf("nothing was hidden, nothing is audited: %+v", entries)
	}
}
