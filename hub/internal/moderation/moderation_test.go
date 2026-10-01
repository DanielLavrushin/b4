package moderation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

var testNow = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

var console = Actor{Kind: store.ActorConsole, Ref: "abcd1234", IP: "127.0.0.1"}

const hiddenPending = "hidden-pending"

type buildLog struct {
	triggers []string
}

func (b *buildLog) Request(trigger string) {
	b.triggers = append(b.triggers, trigger)
}

type env struct {
	t       *testing.T
	ctx     context.Context
	st      *store.Store
	svc     *Service
	builds  *buildLog
	hub     *hubwire.Identity
	builtin *hubwire.Identity
	n       int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub, err := hubwire.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	builtin, err := hubwire.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, ctx: context.Background(), st: st, builds: &buildLog{}, hub: hub, builtin: builtin}
	e.svc = &Service{Store: st, Builds: e.builds, HubKeyID: hub.KeyID(), BuiltinKeys: []string{builtin.KeyID()}, Now: func() time.Time { return testNow }}
	return e
}

func keyOf(n int) string {
	return fmt.Sprintf("%064x", n)
}

func (e *env) set(author string, statuses ...string) string {
	e.t.Helper()
	id, err := hubdata.NewSetID(testNow)
	if err != nil {
		e.t.Fatal(err)
	}
	for i, status := range statuses {
		e.n++
		v := &store.Version{
			FP:           fmt.Sprintf("fp-%d", e.n),
			TargetsKey:   fmt.Sprintf("tk-%d", e.n),
			Title:        fmt.Sprintf("title %d", e.n),
			Projection:   map[string]interface{}{"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}}},
			Status:       hubwire.SetStatusPending,
			UploaderHMAC: author,
			CreatedAt:    testNow,
			UpdatedAt:    testNow,
		}
		if i == 0 {
			err = e.st.CreateSet(e.ctx, store.Set{ID: id, AuthorHMAC: author, CreatedAt: testNow, UpdatedAt: testNow}, v)
		} else {
			v.SetID = id
			err = e.st.AddVersion(e.ctx, v)
		}
		if err != nil {
			e.t.Fatal(err)
		}
		version := i + 1
		switch status {
		case hubwire.SetStatusPending:
		case hubwire.SetStatusActive:
			err = e.st.Approve(e.ctx, id, version, testNow)
		case hubwire.SetStatusRejected:
			err = e.st.Reject(e.ctx, id, version, "earlier", testNow)
		case hubwire.SetStatusHidden:
			if err = e.st.Approve(e.ctx, id, version, testNow); err == nil {
				err = e.st.Hide(e.ctx, id, version, "earlier", testNow)
			}
		case hiddenPending:
			err = e.st.Hide(e.ctx, id, version, "earlier", testNow)
		default:
			e.t.Fatalf("unknown status %q", status)
		}
		if err != nil {
			e.t.Fatal(err)
		}
	}
	return id
}

func (e *env) version(id string, version int) *store.Version {
	e.t.Helper()
	v, err := e.st.GetVersion(e.ctx, id, version)
	if err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *env) audit(q store.AuditQuery) []store.AuditEntry {
	e.t.Helper()
	q.Limit = 500
	entries, _, err := e.st.AuditLog(e.ctx, q)
	if err != nil {
		e.t.Fatal(err)
	}
	return entries
}

func (e *env) report(id string, version int, key, asn string) int64 {
	e.t.Helper()
	if err := e.st.InsertReport(e.ctx, store.Report{SetID: id, Version: version, KeyHMAC: key, ASNObserved: asn, Reason: "bad", ReceivedAt: testNow}); err != nil {
		e.t.Fatal(err)
	}
	reports, err := e.st.ReportsForVersion(e.ctx, id, version)
	if err != nil {
		e.t.Fatal(err)
	}
	return reports[0].ID
}

func (e *env) reportStates(id string, version int) map[string]int {
	e.t.Helper()
	reports, err := e.st.ReportsForVersion(e.ctx, id, version)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]int{}
	for _, r := range reports {
		out[r.State+"/"+r.Resolution]++
	}
	return out
}

func codeOf(t *testing.T, err error, want string) *Error {
	t.Helper()
	if CodeOf(err) != want {
		t.Fatalf("expected code %q, got %v", want, err)
	}
	var me *Error
	errors.As(err, &me)
	return me
}

func one(id string, version int) []Ref {
	return []Ref{{SetID: id, Version: version}}
}

func TestTransitionTable(t *testing.T) {
	want := map[string]map[string]string{
		ActionApprove: {"pending": "active", "active": "", "rejected": "active", "hidden": "", hiddenPending: ""},
		ActionReject:  {"pending": "rejected", "active": "", "rejected": "", "hidden": "", hiddenPending: ""},
		ActionHide:    {"pending": "hidden", "active": "hidden", "rejected": "", "hidden": "", hiddenPending: ""},
		ActionRestore: {"pending": "", "active": "", "rejected": "", "hidden": "active", hiddenPending: "pending"},
	}
	done := map[string]string{ActionApprove: "set.approved", ActionReject: "set.rejected", ActionHide: "set.hidden", ActionRestore: "set.restored"}
	for action, cases := range want {
		for from, to := range cases {
			t.Run(action+"/"+from, func(t *testing.T) {
				e := newEnv(t)
				id := e.set(keyOf(1), from)
				before := e.version(id, 1)
				res, err := e.svc.Moderate(e.ctx, console, action, one(id, 1), Options{Reason: "because"})
				after := e.version(id, 1)
				if to == "" {
					me := codeOf(t, err, CodeInvalidTransition)
					if me.Params["from"] != before.Status {
						t.Errorf("the refusal names the current status: %v", me.Params)
					}
					if after.Status != before.Status || !after.UpdatedAt.Equal(before.UpdatedAt) {
						t.Errorf("a refused transition must change nothing: %+v", after)
					}
					if len(e.builds.triggers) != 0 || len(e.audit(store.AuditQuery{})) != 0 {
						t.Errorf("a refused transition requests no build and writes no audit row")
					}
					return
				}
				if err != nil {
					t.Fatalf("%s from %s must be allowed: %v", action, from, err)
				}
				if after.Status != to || res.Code != done[action] {
					t.Fatalf("%s from %s: status %s code %s, want %s", action, from, after.Status, res.Code, to)
				}
				entries := e.audit(store.AuditQuery{})
				if len(entries) != 1 || entries[0].Action != "set."+action || entries[0].Before["status"] != before.Status || entries[0].After["status"] != to {
					t.Errorf("one audit row with the transition: %+v", entries)
				}
				if len(e.builds.triggers) != 1 || e.builds.triggers[0] != "moderation" {
					t.Errorf("an applied transition requests one build: %v", e.builds.triggers)
				}
			})
		}
	}
}

func TestRejectNeedsAReason(t *testing.T) {
	e := newEnv(t)
	id := e.set(keyOf(1), hubwire.SetStatusPending)
	_, err := e.svc.Moderate(e.ctx, console, ActionReject, one(id, 1), Options{Reason: "  \t "})
	codeOf(t, err, CodeReasonRequired)
	if v := e.version(id, 1); v.Status != hubwire.SetStatusPending {
		t.Fatalf("a refused rejection changes nothing: %+v", v)
	}
	long := strings.Repeat("я", MaxReasonRunes+20)
	if _, err := e.svc.Moderate(e.ctx, console, ActionReject, one(id, 1), Options{Reason: "  " + long}); err != nil {
		t.Fatal(err)
	}
	v := e.version(id, 1)
	if got := []rune(v.StatusReason); len(got) != MaxReasonRunes {
		t.Fatalf("the reason is clipped to %d runes, got %d", MaxReasonRunes, len(got))
	}
	entries := e.audit(store.AuditQuery{})
	if len(entries) != 1 || entries[0].Reason != v.StatusReason || entries[0].After["status_reason"] != v.StatusReason {
		t.Fatalf("the audit row carries the stored reason: %+v", entries)
	}
}

func TestRequestShapeErrors(t *testing.T) {
	e := newEnv(t)
	id := e.set(keyOf(1), hubwire.SetStatusPending)
	_, err := e.svc.Moderate(e.ctx, console, "explode", one(id, 1), Options{})
	codeOf(t, err, CodeUnknownAction)
	_, err = e.svc.Moderate(e.ctx, console, ActionApprove, nil, Options{})
	codeOf(t, err, CodeNothingToDo)
	many := make([]Ref, MaxBatch+1)
	for i := range many {
		many[i] = Ref{SetID: id, Version: 1}
	}
	_, err = e.svc.Moderate(e.ctx, console, ActionApprove, many, Options{})
	codeOf(t, err, CodeTooMany)
	_, err = e.svc.Moderate(e.ctx, console, ActionApprove, one(id, 9), Options{})
	codeOf(t, err, CodeNotFound)
	if v := e.version(id, 1); v.Status != hubwire.SetStatusPending {
		t.Fatalf("nothing may change: %+v", v)
	}
}

func TestExpectStatusRefusesStaleClicks(t *testing.T) {
	e := newEnv(t)
	id := e.set(keyOf(1), hubwire.SetStatusActive)
	refs := []Ref{{SetID: id, Version: 1, ExpectStatus: hubwire.SetStatusPending}}
	_, err := e.svc.Moderate(e.ctx, console, ActionHide, refs, Options{Reason: "x"})
	me := codeOf(t, err, CodeStale)
	if me.Params["from"] != hubwire.SetStatusActive {
		t.Errorf("stale names the status found: %v", me.Params)
	}
	if v := e.version(id, 1); v.Status != hubwire.SetStatusActive {
		t.Fatalf("a stale click changes nothing: %+v", v)
	}
	refs[0].ExpectStatus = hubwire.SetStatusActive
	if _, err := e.svc.Moderate(e.ctx, console, ActionHide, refs, Options{Reason: "x"}); err != nil {
		t.Fatalf("a matching expectation passes: %v", err)
	}
}

func TestHideFallsBackAndWithdrawEmptiesTheSet(t *testing.T) {
	e := newEnv(t)
	id := e.set(keyOf(1), hubwire.SetStatusActive, hubwire.SetStatusActive)
	res, err := e.svc.Moderate(e.ctx, console, ActionHide, one(id, 2), Options{Reason: "regressed"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Params["listed_after"] != 1 || res.Items[0].Listed != 2 || res.Items[0].ListedAfter != 1 || res.Items[0].Withheld != "" {
		t.Fatalf("hiding the newest version lists the previous one: %+v %+v", res.Params, res.Items)
	}
	entries := e.audit(store.AuditQuery{})
	if entries[0].Before["listed"] != float64(2) || entries[0].After["listed"] != float64(1) || entries[0].After["status_reason"] != "regressed" {
		t.Fatalf("audit snapshots: %v %v", entries[0].Before, entries[0].After)
	}

	e.report(id, 2, keyOf(9), "64500")
	res, err = e.svc.Moderate(e.ctx, console, ActionHide, one(id, 1), Options{Reason: "gone for good", Withdraw: true})
	if err != nil {
		t.Fatal(err)
	}
	if states := e.reportStates(id, 2); states[store.ReportResolved+"/"+store.ResolutionWithdrawn] != 1 {
		t.Fatalf("withdrawing through a hide settles the reports of every version, as a withdrawal does: %v", states)
	}
	if entries := e.audit(store.AuditQuery{Action: "set.withdraw"}); len(entries) != 1 || entries[0].After["reports_resolved"] != float64(1) {
		t.Fatalf("the withdrawal is audited as one: %+v", entries)
	}
	if res.Params["listed_after"] != 0 || res.Items[0].Withheld != store.WithheldWithdrawn {
		t.Fatalf("withdrawing leaves nothing listed: %+v %+v", res.Params, res.Items)
	}
	set, _, _ := e.st.GetSet(e.ctx, id)
	if set.WithdrawnAt.IsZero() || set.WithdrawReason != "gone for good" {
		t.Fatalf("the set must be withdrawn with the reason: %+v", set)
	}
	if entries = e.audit(store.AuditQuery{}); entries[0].After["withdrawn"] != true {
		t.Fatalf("the audit row records the withdrawal: %v", entries[0].After)
	}

	other := e.set(keyOf(1), hubwire.SetStatusActive, hubwire.SetStatusPending)
	res, err = e.svc.Moderate(e.ctx, console, ActionReject, one(other, 2), Options{Reason: "spam", Withdraw: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Items[0].Listed != 1 || res.Items[0].ListedAfter != 0 {
		t.Fatalf("a rejection with withdraw takes the listed version down too: %+v", res.Items)
	}
	if set, _, _ = e.st.GetSet(e.ctx, other); set.WithdrawnAt.IsZero() {
		t.Fatalf("reject with withdraw must withdraw the set")
	}
	if v := e.version(other, 1); v.Status != hubwire.SetStatusActive {
		t.Fatalf("the listed version keeps its status: %+v", v)
	}

	third := e.set(keyOf(1), hubwire.SetStatusPending)
	if _, err := e.svc.Moderate(e.ctx, console, ActionApprove, one(third, 1), Options{Withdraw: true}); err != nil {
		t.Fatal(err)
	}
	if set, _, _ = e.st.GetSet(e.ctx, third); !set.WithdrawnAt.IsZero() {
		t.Fatalf("withdraw only applies to hide and reject")
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	e := newEnv(t)
	id := e.set(keyOf(1), hubwire.SetStatusActive, hubwire.SetStatusActive)
	active := e.set(keyOf(1), hubwire.SetStatusActive)
	gen, _ := e.st.DirtyGeneration(e.ctx)
	res, err := e.svc.Moderate(e.ctx, console, ActionHide, []Ref{{SetID: id, Version: 2}, {SetID: active, Version: 7}}, Options{Reason: "x", Withdraw: true, DryRun: true})
	if err != nil {
		t.Fatalf("a dry run reports invalid items instead of failing: %v", err)
	}
	if res.Code != "moderation.preview" || len(res.Items) != 2 {
		t.Fatalf("preview: %+v", res)
	}
	if it := res.Items[0]; !it.OK || it.From != hubwire.SetStatusActive || it.To != hubwire.SetStatusHidden || it.Listed != 2 || it.ListedAfter != 0 || it.Withheld != store.WithheldWithdrawn {
		t.Fatalf("preview of the valid item: %+v", it)
	}
	if it := res.Items[1]; it.OK || it.Code != CodeNotFound {
		t.Fatalf("preview of the invalid item: %+v", it)
	}
	if v := e.version(id, 2); v.Status != hubwire.SetStatusActive {
		t.Fatalf("a dry run must not hide: %+v", v)
	}
	if set, _, _ := e.st.GetSet(e.ctx, id); !set.WithdrawnAt.IsZero() {
		t.Fatalf("a dry run must not withdraw")
	}
	if after, _ := e.st.DirtyGeneration(e.ctx); after != gen {
		t.Fatalf("a dry run must not mark the store dirty: %s -> %s", gen, after)
	}
	if len(e.builds.triggers) != 0 || len(e.audit(store.AuditQuery{})) != 0 {
		t.Fatalf("a dry run requests no build and writes no audit")
	}
}

func TestBatchIsAllOrNothingUnlessPartial(t *testing.T) {
	e := newEnv(t)
	a := e.set(keyOf(1), hubwire.SetStatusPending)
	b := e.set(keyOf(2), hubwire.SetStatusPending)
	c := e.set(keyOf(3), hubwire.SetStatusActive)
	refs := []Ref{{SetID: a, Version: 1}, {SetID: b, Version: 1}, {SetID: c, Version: 1}}

	_, err := e.svc.Moderate(e.ctx, console, ActionApprove, refs, Options{})
	me := codeOf(t, err, CodeBatchInvalid)
	if len(me.Items) != 3 || !me.Items[0].OK || !me.Items[1].OK || me.Items[2].OK || me.Items[2].Code != CodeInvalidTransition {
		t.Fatalf("the refusal lists every item: %+v", me.Items)
	}
	for _, id := range []string{a, b} {
		if v := e.version(id, 1); v.Status != hubwire.SetStatusPending {
			t.Fatalf("an invalid batch applies nothing: %+v", v)
		}
	}
	if len(e.builds.triggers) != 0 || len(e.audit(store.AuditQuery{})) != 0 {
		t.Fatalf("an invalid batch requests no build and writes no audit")
	}

	res, err := e.svc.Moderate(e.ctx, console, ActionApprove, refs, Options{Partial: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != "moderation.batch" || res.Params["applied"] != 2 || res.Params["skipped"] != 1 || res.BatchID == "" {
		t.Fatalf("partial batch: %+v", res)
	}
	entries := e.audit(store.AuditQuery{})
	if len(entries) != 2 || entries[0].BatchID != res.BatchID || entries[1].BatchID != res.BatchID {
		t.Fatalf("both rows share the batch id: %+v", entries)
	}
	if len(e.builds.triggers) != 1 {
		t.Fatalf("a batch requests one build, got %v", e.builds.triggers)
	}
	res, err = e.svc.Moderate(e.ctx, console, ActionApprove, one(c, 1), Options{Partial: true})
	if err != nil || res.Params["applied"] != 0 || len(e.builds.triggers) != 1 {
		t.Fatalf("a partial batch that applies nothing requests no build: %+v %v %v", res, err, e.builds.triggers)
	}
	if res, err = e.svc.Moderate(e.ctx, console, ActionHide, one(a, 1), Options{Reason: "x"}); err != nil || res.BatchID != "" {
		t.Fatalf("a single decision carries no batch id: %+v %v", res, err)
	}
}

func TestApproveRespectsWithdrawalAndBans(t *testing.T) {
	e := newEnv(t)
	withdrawn := e.set(keyOf(1), hubwire.SetStatusActive, hubwire.SetStatusPending)
	if _, err := e.svc.Withdraw(e.ctx, console, withdrawn, "author asked"); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.Moderate(e.ctx, console, ActionApprove, one(withdrawn, 2), Options{})
	codeOf(t, err, CodeSetWithdrawn)
	res, err := e.svc.Moderate(e.ctx, console, ActionApprove, one(withdrawn, 2), Options{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Items[0].ListedAfter != 0 || res.Items[0].Withheld != store.WithheldWithdrawn {
		t.Fatalf("a forced approval of a withdrawn set stays unlisted: %+v", res.Items[0])
	}
	if set, _, _ := e.st.GetSet(e.ctx, withdrawn); set.WithdrawnAt.IsZero() {
		t.Fatalf("forcing an approval does not reinstate the set")
	}
	entries := e.audit(store.AuditQuery{Action: "set.approve"})
	if len(entries) != 1 || entries[0].After["forced"] != true {
		t.Fatalf("the forced approval is audited as forced: %+v", entries)
	}

	banned := keyOf(2)
	quarantined := e.set(banned, hubwire.SetStatusPending)
	if _, err := e.svc.Key(e.ctx, console, banned, ActionBan, KeyOptions{Reason: "spam", Create: true}); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Moderate(e.ctx, console, ActionApprove, one(quarantined, 1), Options{})
	codeOf(t, err, CodeAuthorBanned)
	if v := e.version(quarantined, 1); v.Status != hubwire.SetStatusPending {
		t.Fatalf("a refused approval changes nothing: %+v", v)
	}
	res, err = e.svc.Moderate(e.ctx, console, ActionApprove, one(quarantined, 1), Options{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Items[0].ListedAfter != 0 || res.Items[0].Withheld != store.WithheldAuthorBanned {
		t.Fatalf("a banned author's version stays unlisted after a forced approval: %+v", res.Items[0])
	}
	if _, err := e.svc.Moderate(e.ctx, console, ActionHide, one(quarantined, 1), Options{Reason: "x"}); err != nil {
		t.Fatalf("hiding needs no force for a banned author: %v", err)
	}
}

func TestReportsFollowTheDecision(t *testing.T) {
	e := newEnv(t)
	id := e.set(keyOf(1), hubwire.SetStatusActive)
	e.report(id, 1, keyOf(10), "64500")
	e.report(id, 1, keyOf(11), "64501")

	res, err := e.svc.Moderate(e.ctx, console, ActionHide, one(id, 1), Options{Reason: "checking", KeepReports: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.reportStates(id, 1); got["open/"] != 2 || res.Items[0].Reports != 0 {
		t.Fatalf("keep_reports leaves the reports open: %v %+v", got, res.Items[0])
	}
	res, err = e.svc.Moderate(e.ctx, console, ActionRestore, one(id, 1), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.reportStates(id, 1); got["dismissed/restored"] != 2 || res.Items[0].Reports != 2 {
		t.Fatalf("a restore dismisses the open reports: %v %+v", got, res.Items[0])
	}
	if entries := e.audit(store.AuditQuery{Action: "set.restore"}); entries[0].After["reports_dismissed"] != float64(2) {
		t.Fatalf("the restore records how many reports it dismissed: %v", entries[0].After)
	}

	e.report(id, 1, keyOf(12), "64502")
	if _, err := e.svc.Moderate(e.ctx, console, ActionApprove, one(id, 1), Options{}); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("approve on active: %v", err)
	}
	res, err = e.svc.Moderate(e.ctx, console, ActionHide, one(id, 1), Options{Reason: "confirmed"})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.reportStates(id, 1); got["resolved/hidden"] != 1 || got["dismissed/restored"] != 2 || res.Items[0].Reports != 1 {
		t.Fatalf("a hide resolves the open reports only: %v %+v", got, res.Items[0])
	}
	if entries := e.audit(store.AuditQuery{Action: "set.hide"}); entries[0].After["reports_resolved"] != float64(1) {
		t.Fatalf("the hide records the resolved reports: %v", entries[0].After)
	}

	pending := e.set(keyOf(1), hubwire.SetStatusPending)
	e.report(pending, 1, keyOf(13), "64500")
	if _, err := e.svc.Moderate(e.ctx, console, ActionReject, one(pending, 1), Options{Reason: "spam"}); err != nil {
		t.Fatal(err)
	}
	if got := e.reportStates(pending, 1); got["resolved/rejected"] != 1 {
		t.Fatalf("a rejection resolves the reports: %v", got)
	}
}

func TestWithdrawAndReinstate(t *testing.T) {
	e := newEnv(t)
	id := e.set(keyOf(1), hubwire.SetStatusActive, hubwire.SetStatusPending)
	e.report(id, 1, keyOf(10), "64500")
	e.report(id, 2, keyOf(11), "64501")
	res, err := e.svc.Withdraw(e.ctx, console, id, " author asked ")
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != "set.withdrawn" || res.Params["set_id"] != id || res.Params["title"] != e.version(id, 2).Title {
		t.Fatalf("withdraw result: %+v", res)
	}
	if got := e.reportStates(id, 1); got["resolved/withdrawn"] != 1 {
		t.Fatalf("withdrawing resolves the reports of every version: %v", got)
	}
	if got := e.reportStates(id, 2); got["resolved/withdrawn"] != 1 {
		t.Fatalf("withdrawing resolves the reports of every version: %v", got)
	}
	entries := e.audit(store.AuditQuery{Action: "set.withdraw"})
	if len(entries) != 1 || entries[0].Reason != "author asked" || entries[0].Before["withdrawn"] != false || entries[0].Before["listed"] != float64(1) ||
		entries[0].After["withdrawn"] != true || entries[0].After["listed"] != float64(0) || entries[0].After["reports_resolved"] != float64(2) {
		t.Fatalf("withdraw audit: %+v", entries)
	}
	_, err = e.svc.Withdraw(e.ctx, console, id, "again")
	codeOf(t, err, CodeSetWithdrawn)

	res, err = e.svc.Reinstate(e.ctx, console, id)
	if err != nil || res.Code != "set.reinstated" {
		t.Fatalf("reinstate: %+v %v", res, err)
	}
	entries = e.audit(store.AuditQuery{Action: "set.reinstate"})
	if len(entries) != 1 || entries[0].Before["withdraw_reason"] != "author asked" || entries[0].After["listed"] != float64(1) {
		t.Fatalf("reinstate audit: %+v", entries)
	}
	_, err = e.svc.Reinstate(e.ctx, console, id)
	codeOf(t, err, CodeNotWithdrawn)
	if got := strings.Join(e.builds.triggers, ","); got != "moderation,moderation" {
		t.Fatalf("withdraw and reinstate each request a build: %s", got)
	}

	unknown, _ := hubdata.NewSetID(testNow)
	_, err = e.svc.Withdraw(e.ctx, console, unknown, "x")
	codeOf(t, err, CodeNotFound)
	_, err = e.svc.Reinstate(e.ctx, console, unknown)
	codeOf(t, err, CodeNotFound)
}

func TestDeleteNeedsTheTypedID(t *testing.T) {
	e := newEnv(t)
	id := e.set(keyOf(1), hubwire.SetStatusActive, hubwire.SetStatusPending)
	_, err := e.svc.Delete(e.ctx, console, "not-a-set", "not-a-set")
	codeOf(t, err, CodeNotFound)
	_, err = e.svc.Delete(e.ctx, console, id, "yes")
	codeOf(t, err, CodeConfirmMismatch)
	if _, _, err := e.st.GetSet(e.ctx, id); err != nil {
		t.Fatalf("a mismatched confirmation keeps the set: %v", err)
	}
	title := e.version(id, 2).Title
	res, err := e.svc.Delete(e.ctx, console, id, " "+id+" ")
	if err != nil || res.Code != "set.deleted" || res.Params["title"] != title {
		t.Fatalf("delete: %+v %v", res, err)
	}
	if _, _, err := e.st.GetSet(e.ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the set must be gone: %v", err)
	}
	entries := e.audit(store.AuditQuery{Action: "set.delete"})
	if len(entries) != 1 || entries[0].Before["title"] != title || entries[0].Before["versions"] != float64(2) || entries[0].Before["listed"] != float64(1) || entries[0].Before["author"] != hubdata.AuthorLabel(keyOf(1)) {
		t.Fatalf("delete audit: %+v", entries)
	}
	_, err = e.svc.Delete(e.ctx, console, id, id)
	codeOf(t, err, CodeNotFound)
}

func TestKeyActions(t *testing.T) {
	e := newEnv(t)
	key := keyOf(7)
	_, err := e.svc.Key(e.ctx, console, "zz", ActionBan, KeyOptions{})
	codeOf(t, err, CodeBadKey)
	_, err = e.svc.Key(e.ctx, console, key, "explode", KeyOptions{})
	codeOf(t, err, CodeUnknownAction)
	for _, action := range []string{ActionUnban, ActionUntrust, ActionBan, ActionTrust} {
		_, err = e.svc.Key(e.ctx, console, key, action, KeyOptions{})
		me := codeOf(t, err, CodeUnknownKey)
		if me.Params["key"] != hubdata.AuthorLabel(key) {
			t.Errorf("unknown key names the label: %v", me.Params)
		}
	}
	_, err = e.svc.Key(e.ctx, console, key, ActionUnban, KeyOptions{Create: true})
	codeOf(t, err, CodeUnknownKey)
	if len(e.audit(store.AuditQuery{})) != 0 || len(e.builds.triggers) != 0 {
		t.Fatalf("refused key actions leave no trace")
	}

	res, err := e.svc.Key(e.ctx, console, "  "+strings.ToUpper(key)+" ", ActionBan, KeyOptions{Reason: "spam", Create: true})
	if err != nil || res.Code != "key.banned" {
		t.Fatalf("ban: %+v %v", res, err)
	}
	k, err := e.st.GetKey(e.ctx, key)
	if err != nil || !k.Banned || k.BanReason != "spam" {
		t.Fatalf("the key must be banned under its canonical name: %+v %v", k, err)
	}
	if _, err := e.svc.Key(e.ctx, console, key, ActionTrust, KeyOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Key(e.ctx, console, key, ActionUnban, KeyOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Key(e.ctx, console, key, ActionUntrust, KeyOptions{}); err != nil {
		t.Fatal(err)
	}
	entries := e.audit(store.AuditQuery{TargetKind: store.TargetKey, TargetID: key})
	if len(entries) != 4 {
		t.Fatalf("one audit row per key action: %+v", entries)
	}
	byAction := map[string]store.AuditEntry{}
	for _, entry := range entries {
		byAction[entry.Action] = entry
	}
	if ban := byAction["key.ban"]; ban.Reason != "spam" || ban.Before["banned"] != false || ban.After["banned"] != true || ban.Actor != store.ActorConsole || ban.ActorRef != console.Ref || ban.ActorIP != console.IP {
		t.Errorf("ban audit: %+v", ban)
	}
	if unban := byAction["key.unban"]; unban.Before["banned"] != true || unban.Before["ban_reason"] != "spam" || unban.After["banned"] != false || unban.After["trusted"] != true {
		t.Errorf("unban audit: %+v", unban)
	}
	if trust := byAction["key.trust"]; trust.Before["trusted"] != false || trust.After["trusted"] != true {
		t.Errorf("trust audit: %+v", trust)
	}
	if untrust := byAction["key.untrust"]; untrust.After["trusted"] != false {
		t.Errorf("untrust audit: %+v", untrust)
	}
	if got := strings.Join(e.builds.triggers, ","); got != "moderation,moderation" {
		t.Fatalf("only ban and unban touch the catalogue: %s", got)
	}

	if _, err := e.svc.KeyImpact(e.ctx, "nope"); CodeOf(err) != CodeBadKey {
		t.Fatalf("impact of a malformed key: %v", err)
	}
	author := keyOf(8)
	e.set(author, hubwire.SetStatusActive)
	e.set(author, hubwire.SetStatusPending)
	impact, err := e.svc.KeyImpact(e.ctx, strings.ToUpper(author))
	if err != nil || len(impact.Listed) != 1 || len(impact.Pending) != 1 {
		t.Fatalf("impact: %+v %v", impact, err)
	}
}

func nonCanonical(t *testing.T, key string) string {
	t.Helper()
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	idx := strings.IndexByte(alphabet, key[len(key)-1])
	if idx < 0 {
		t.Skipf("key %s does not end in a base64url character", key)
	}
	alt := key[:len(key)-1] + string(alphabet[idx|1])
	pub, err := hubwire.DecodeKey(alt)
	if alt == key || err != nil || hubwire.EncodeKey(pub) != key {
		t.Skipf("no non-canonical encoding of %s decodes to the same key", key)
	}
	return alt
}

func TestRevokeCanonicalisesAndGuards(t *testing.T) {
	e := newEnv(t)
	other, _ := hubwire.NewIdentity()
	canonical := other.KeyID()
	alt := nonCanonical(t, canonical)

	_, err := e.svc.Revoke(e.ctx, console, "not-a-key", RevokeOptions{Confirm: "not-a-key"})
	codeOf(t, err, CodeBadKey)
	_, err = e.svc.Revoke(e.ctx, console, canonical, RevokeOptions{})
	codeOf(t, err, CodeConfirmMismatch)
	_, err = e.svc.Revoke(e.ctx, console, canonical, RevokeOptions{Confirm: e.builtin.KeyID()})
	codeOf(t, err, CodeConfirmMismatch)
	_, err = e.svc.Revoke(e.ctx, console, nonCanonical(t, e.hub.KeyID()), RevokeOptions{Confirm: e.hub.KeyID()})
	codeOf(t, err, CodeOwnKey)
	_, err = e.svc.Revoke(e.ctx, console, " "+e.builtin.KeyID(), RevokeOptions{Confirm: nonCanonical(t, e.builtin.KeyID())})
	codeOf(t, err, CodeBuiltinKey)
	if revoked, _ := e.st.RevokedKeys(e.ctx); len(revoked) != 0 {
		t.Fatalf("refusals revoke nothing: %v", revoked)
	}

	res, err := e.svc.Revoke(e.ctx, console, alt, RevokeOptions{Confirm: canonical})
	if err != nil || res.Code != "catalogue.revoked" || res.Params["key_id"] != canonical {
		t.Fatalf("revoke: %+v %v", res, err)
	}
	if revoked, _ := e.st.RevokedKeys(e.ctx); len(revoked) != 1 || revoked[0] != canonical {
		t.Fatalf("the canonical encoding is stored: %v", revoked)
	}
	res, err = e.svc.Revoke(e.ctx, console, canonical, RevokeOptions{Confirm: alt})
	if err != nil || res.Code != "catalogue.revoke_known" {
		t.Fatalf("revoking again: %+v %v", res, err)
	}
	if _, err := e.svc.Revoke(e.ctx, console, e.builtin.KeyID(), RevokeOptions{Confirm: e.builtin.KeyID(), AllowBuiltin: true}); err != nil {
		t.Fatalf("the override allows a builtin key: %v", err)
	}
	entries := e.audit(store.AuditQuery{Action: "catalogue.revoke"})
	if len(entries) != 2 || entries[1].TargetID != canonical || entries[1].After["revoked"] != canonical || entries[0].TargetID != e.builtin.KeyID() {
		t.Fatalf("one audit row per new revocation: %+v", entries)
	}
	if got := strings.Join(e.builds.triggers, ","); got != "revoke,revoke" {
		t.Fatalf("a repeated revocation requests no build: %s", got)
	}
}

func TestReportActions(t *testing.T) {
	e := newEnv(t)
	id := e.set(keyOf(1), hubwire.SetStatusActive)
	r1 := e.report(id, 1, keyOf(10), "64500")
	r2 := e.report(id, 1, keyOf(11), "64501")
	e.report(id, 1, keyOf(12), "64502")

	_, err := e.svc.Reports(e.ctx, console, []int64{r1}, "explode", "")
	codeOf(t, err, CodeUnknownAction)
	_, err = e.svc.Reports(e.ctx, console, nil, ActionDismiss, "")
	codeOf(t, err, CodeNothingToDo)
	_, err = e.svc.Reports(e.ctx, console, make([]int64, MaxBatch+1), ActionDismiss, "")
	codeOf(t, err, CodeTooMany)
	_, err = e.svc.Reports(e.ctx, console, []int64{r1, 999}, ActionDismiss, "")
	codeOf(t, err, CodeNotFound)

	if got := e.reportStates(id, 1); got["open/"] != 3 {
		t.Fatalf("a batch naming an unknown report changes nothing: %v", got)
	}

	res, err := e.svc.Reports(e.ctx, console, []int64{r1}, ActionDismiss, "duplicate")
	if err != nil || res.Params["count"] != 1 || res.Params["state"] != store.ReportDismissed {
		t.Fatalf("dismiss: %+v %v", res, err)
	}
	if n, _ := e.st.IndependentReports(e.ctx, id, 1); n != 2 {
		t.Fatalf("a dismissed report stops counting, got %d", n)
	}
	if res, _ = e.svc.Reports(e.ctx, console, []int64{r1}, ActionDismiss, ""); res.Params["count"] != 0 {
		t.Fatalf("dismissing twice changes nothing: %+v", res)
	}
	entries := e.audit(store.AuditQuery{Action: "report.dismiss"})
	if len(entries) != 1 || entries[0].TargetKind != store.TargetReport || entries[0].TargetID != fmt.Sprint(r1) || entries[0].Version != 1 ||
		entries[0].Reason != "duplicate" || entries[0].Before["state"] != store.ReportOpen || entries[0].Before["set_id"] != id || entries[0].After["state"] != store.ReportDismissed {
		t.Fatalf("dismiss audit: %+v", entries)
	}

	res, err = e.svc.Reports(e.ctx, console, []int64{r1, r2}, ActionResolve, "handled")
	if err != nil || res.Params["count"] != 2 {
		t.Fatalf("resolve: %+v %v", res, err)
	}
	entries = e.audit(store.AuditQuery{Action: "report.resolve"})
	if len(entries) != 2 || entries[0].BatchID == "" || entries[0].BatchID != entries[1].BatchID {
		t.Fatalf("a multi-report action shares a batch id: %+v", entries)
	}
	if _, err := e.svc.Reports(e.ctx, console, []int64{r1}, ActionReopen, ""); err != nil {
		t.Fatal(err)
	}
	if got := e.reportStates(id, 1); got["open/"] != 2 || got["resolved/"] != 1 {
		t.Fatalf("reopen: %v", got)
	}
	if len(e.builds.triggers) != 0 {
		t.Fatalf("report triage does not touch the catalogue: %v", e.builds.triggers)
	}

	_, err = e.svc.VersionReports(e.ctx, console, id, 1, ActionReopen, "")
	codeOf(t, err, CodeUnknownAction)
	_, err = e.svc.VersionReports(e.ctx, console, id, 9, ActionDismiss, "")
	codeOf(t, err, CodeNotFound)
	res, err = e.svc.VersionReports(e.ctx, console, id, 1, ActionDismiss, "noise")
	if err != nil || res.Params["count"] != 2 {
		t.Fatalf("version dismiss: %+v %v", res, err)
	}
	if res, _ = e.svc.VersionReports(e.ctx, console, id, 1, ActionDismiss, ""); res.Params["count"] != 0 {
		t.Fatalf("nothing left to dismiss: %+v", res)
	}
	entries = e.audit(store.AuditQuery{Action: "set.reports_dismiss"})
	if len(entries) != 1 || entries[0].After["count"] != float64(2) || entries[0].TargetID != id {
		t.Fatalf("one audit row for the version dismissal: %+v", entries)
	}
}

func TestEpochAndMirrorActions(t *testing.T) {
	e := newEnv(t)
	res, err := e.svc.NewEpoch(e.ctx, console)
	if err != nil || res.Code != "catalogue.epoch" {
		t.Fatalf("epoch: %+v %v", res, err)
	}
	epoch, _ := e.st.Epoch(e.ctx)
	entries := e.audit(store.AuditQuery{Action: "catalogue.epoch"})
	if len(entries) != 1 || entries[0].After["epoch"] != float64(epoch) || entries[0].TargetID != fmt.Sprint(epoch) {
		t.Fatalf("epoch audit: %+v", entries)
	}

	m, err := e.st.AnnounceMirror(e.ctx, "https://mirror.example", keyOf(3), "1.0.0", testNow)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = e.svc.Mirror(e.ctx, console, m.ID, ActionReject, " ")
	codeOf(t, err, CodeReasonRequired)
	_, _, err = e.svc.Mirror(e.ctx, console, m.ID, "explode", "")
	codeOf(t, err, CodeUnknownAction)
	_, _, err = e.svc.Mirror(e.ctx, console, 999, ActionApprove, "")
	codeOf(t, err, CodeNotFound)
	if _, _, err := e.svc.Mirror(e.ctx, console, m.ID, ActionApprove, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.Mirror(e.ctx, console, m.ID, ActionReject, "serves stale files"); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.GetMirror(e.ctx, m.ID); got.Status != store.MirrorRejected || got.Reason != "serves stale files" {
		t.Fatalf("rejected mirror: %+v", got)
	}
	if _, _, err := e.svc.Mirror(e.ctx, console, m.ID, ActionRemove, ""); err != nil {
		t.Fatal(err)
	}
	entries = e.audit(store.AuditQuery{TargetKind: store.TargetMirror})
	if len(entries) != 3 || entries[2].Action != "mirror.approve" || entries[1].Action != "mirror.reject" || entries[1].Before["status"] != store.MirrorApproved || entries[0].Action != "mirror.remove" {
		t.Fatalf("mirror audit: %+v", entries)
	}
	if got := strings.Join(e.builds.triggers, ","); got != "epoch,mirrors,mirrors,mirrors" {
		t.Fatalf("build requests: %s", got)
	}
}

func TestEditAndApproveRespectsQuarantine(t *testing.T) {
	e := newEnv(t)
	banned := keyOf(4)
	id := e.set(banned, hubwire.SetStatusPending)
	if _, err := e.svc.Key(e.ctx, console, banned, ActionBan, KeyOptions{Create: true}); err != nil {
		t.Fatal(err)
	}
	e.builds.triggers = nil
	v := e.version(id, 1)
	edit := store.VersionEdit{Title: "edited", Projection: v.Projection, FP: v.FP, TargetsKey: v.TargetsKey}
	_, err := e.svc.Edit(e.ctx, console, Ref{SetID: id, Version: 1}, edit, true)
	codeOf(t, err, CodeAuthorBanned)
	if got := e.version(id, 1); got.Title != v.Title || got.Status != hubwire.SetStatusPending {
		t.Fatalf("a refused edit-and-approve leaves the version alone: %+v", got)
	}
	res, err := e.svc.Edit(e.ctx, console, Ref{SetID: id, Version: 1}, edit, false)
	if err != nil || res.Code != "set.edited" || len(e.builds.triggers) != 0 {
		t.Fatalf("an edit alone is allowed and needs no build: %+v %v %v", res, err, e.builds.triggers)
	}

	free := e.set(keyOf(5), hubwire.SetStatusPending)
	fv := e.version(free, 1)
	res, err = e.svc.Edit(e.ctx, console, Ref{SetID: free, Version: 1}, store.VersionEdit{Title: "tidy", Projection: fv.Projection, FP: fv.FP, TargetsKey: fv.TargetsKey}, true)
	if err != nil || res.Code != "set.edited_approved" {
		t.Fatalf("edit and approve: %+v %v", res, err)
	}
	entries := e.audit(store.AuditQuery{TargetKind: store.TargetSet, TargetID: free})
	if len(entries) != 2 || entries[0].Action != "set.approve" || entries[1].Action != "set.edit" || entries[1].After["title"] != "tidy" {
		t.Fatalf("an edit and approve writes both rows: %+v", entries)
	}
	_, err = e.svc.Edit(e.ctx, console, Ref{SetID: free, Version: 1}, store.VersionEdit{Title: "again", Projection: fv.Projection, FP: fv.FP, TargetsKey: fv.TargetsKey}, false)
	codeOf(t, err, CodeNotPending)
	_, err = e.svc.Edit(e.ctx, console, Ref{SetID: free, Version: 5}, store.VersionEdit{}, false)
	codeOf(t, err, CodeNotFound)
}

func TestReportNamedTwiceChangesOnce(t *testing.T) {
	e := newEnv(t)
	id := e.set(keyOf(1), hubwire.SetStatusActive)
	r := e.report(id, 1, keyOf(10), "64500")
	res, err := e.svc.Reports(e.ctx, console, []int64{r, r}, ActionDismiss, "")
	if err != nil || res.Params["count"] != 1 {
		t.Fatalf("naming a report twice must change it once: %+v %v", res, err)
	}
	if got := e.audit(store.AuditQuery{Action: "report.dismiss"}); len(got) != 1 {
		t.Fatalf("one audit row per changed report: %+v", got)
	}
}

func TestDismissingAVersionsReportsCountsTheSavedReason(t *testing.T) {
	e := newEnv(t)
	id := e.set(keyOf(1), hubwire.SetStatusActive)
	e.report(id, 1, keyOf(10), "64500")
	e.report(id, 1, keyOf(11), "64501")
	if _, err := e.st.CreateReasonPreset(e.ctx, store.ReasonPreset{Scope: store.ScopeReportDismiss, Text: "noise"}, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	uses := func() (int, bool) {
		presets, err := e.st.ReasonPresets(e.ctx)
		if err != nil || len(presets) != 1 {
			t.Fatalf("presets: %+v %v", presets, err)
		}
		return presets[0].Uses, !presets[0].LastUsed.IsZero()
	}
	if _, err := e.svc.VersionReports(e.ctx, console, id, 1, ActionDismiss, "noise"); err != nil {
		t.Fatal(err)
	}
	if n, used := uses(); n != 1 || !used {
		t.Fatalf("dismissing a version's reports with a saved reason counts one use, got %d %v", n, used)
	}
	if _, err := e.svc.VersionReports(e.ctx, console, id, 1, ActionDismiss, "noise"); err != nil {
		t.Fatal(err)
	}
	if n, _ := uses(); n != 1 {
		t.Fatalf("a dismissal that changes nothing does not count, got %d", n)
	}
}
