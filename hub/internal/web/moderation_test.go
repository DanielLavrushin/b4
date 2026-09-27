package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

const unknownSetID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func (f *fixture) shareAs(author *hubwire.Identity, set config.SetConfig, address string) (string, int) {
	f.t.Helper()
	env := testkit.BuildEnvelope(f.t, &set)
	resp := f.ingest.Handle(context.Background(), testkit.SignShare(f.t, author, env, f.clock), parseIP(address))
	if resp.Status != http.StatusAccepted {
		f.t.Fatalf("share %s: %d %v", set.Name, resp.Status, resp.Body)
	}
	return resp.Body["set_id"].(string), resp.Body["version"].(int)
}

func nextVersion(name, setID string, from int, ttl uint8, domains ...string) config.SetConfig {
	set := testkit.SampleSet(name, domains...)
	set.Faking.TTL = ttl
	set.Hub = &config.HubOrigin{ID: setID, Version: from}
	return set
}

func (f *fixture) voteAs(voter *hubwire.Identity, setID string, version int, fp, address string) {
	f.t.Helper()
	body := hubwire.VoteBody{SetID: setID, Version: version, FP: fp, Kind: hubwire.VoteWorks}
	resp := f.ingest.Handle(context.Background(), testkit.Sign(f.t, voter, hubwire.RecordVote, body, f.clock), parseIP(address))
	if resp.Status != http.StatusAccepted {
		f.t.Fatalf("vote: %d %v", resp.Status, resp.Body)
	}
}

func (f *fixture) reportAs(reporter *hubwire.Identity, setID string, version int, address string) {
	f.t.Helper()
	body := hubwire.ReportBody{SetID: setID, Version: version, Reason: "breaks sites"}
	resp := f.ingest.Handle(context.Background(), testkit.Sign(f.t, reporter, hubwire.RecordReport, body, f.clock), parseIP(address))
	if resp.Status != http.StatusAccepted {
		f.t.Fatalf("report: %d %v", resp.Status, resp.Body)
	}
}

func (f *fixture) keyOf(id *hubwire.Identity) string {
	return hubdata.KeyHMAC(f.web.Secret, id.KeyID())
}

func (f *fixture) versionOf(setID string, version int) *store.Version {
	f.t.Helper()
	v, err := f.store.GetVersion(context.Background(), setID, version)
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

func (f *fixture) published(setID string) *hubwire.CatalogueSet {
	latest := f.builder.Latest()
	if latest == nil {
		return nil
	}
	return latest.ByID[setID]
}

func (f *fixture) expectOK(resp response) ActionResult {
	f.t.Helper()
	if resp.status != http.StatusOK {
		f.t.Fatalf("expected 200, got %d %s", resp.status, resp.body)
	}
	var res ActionResult
	resp.decode(f.t, &res)
	return res
}

func (f *fixture) expectError(resp response, status int, code string) ErrorBody {
	f.t.Helper()
	if resp.status != status {
		f.t.Fatalf("expected %d %s, got %d %s", status, code, resp.status, resp.body)
	}
	var body ErrorBody
	resp.decode(f.t, &body)
	if body.Code != code {
		f.t.Fatalf("expected code %s, got %s", code, resp.body)
	}
	return body
}

func (f *fixture) setsView() SetsView {
	f.t.Helper()
	var sets SetsView
	f.admin(http.MethodGet, PathAPI+"/sets", nil).decode(f.t, &sets)
	return sets
}

func entryOf(entries []EntryView, setID string, version int) *EntryView {
	for i := range entries {
		if entries[i].SetID == setID && (version == 0 || entries[i].Version == version) {
			return &entries[i]
		}
	}
	return nil
}

func (f *fixture) auditLog(query string) AuditPageView {
	f.t.Helper()
	resp := f.admin(http.MethodGet, PathAPI+"/audit"+query, nil)
	if resp.status != http.StatusOK {
		f.t.Fatalf("audit: %d %s", resp.status, resp.body)
	}
	var page AuditPageView
	resp.decode(f.t, &page)
	return page
}

func (f *fixture) reportsPage(query string) ReportsPageView {
	f.t.Helper()
	resp := f.admin(http.MethodGet, PathAPI+"/reports"+query, nil)
	if resp.status != http.StatusOK {
		f.t.Fatalf("reports: %d %s", resp.status, resp.body)
	}
	var page ReportsPageView
	resp.decode(f.t, &page)
	return page
}

func (f *fixture) moderate(req ModerationRequest) (response, ModerationView) {
	f.t.Helper()
	resp := f.admin(http.MethodPost, PathAPI+"/moderation", req)
	var view ModerationView
	if resp.status == http.StatusOK {
		resp.decode(f.t, &view)
	}
	return resp, view
}

func altEncoding(t *testing.T, key string) string {
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

func TestVersionTransitionsOverTheAPI(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Transitions", authorAddress, "transitions.example")
	other, _ := f.share("Other", otherAddress, "other.example")

	res := f.expectOK(f.admin(http.MethodPost, setPath(id, 1, ActionHide), map[string]string{"reason": "suspicious"}))
	if res.Code != "set.hidden" || res.Params["listed_after"] != float64(0) {
		t.Fatalf("hide pending: %+v", res)
	}
	hidden := entryOf(f.setsView().Hidden, id, 1)
	if hidden == nil || hidden.HiddenFrom != hubwire.SetStatusPending || hidden.StatusReason != "suspicious" {
		t.Fatalf("a hidden pending version shows where it came from: %+v", hidden)
	}
	res = f.expectOK(f.admin(http.MethodPost, setPath(id, 1, ActionRestore), nil))
	if res.Code != "set.restored" {
		t.Fatalf("restore: %+v", res)
	}
	if v := f.versionOf(id, 1); v.Status != hubwire.SetStatusPending || v.HiddenFrom != "" {
		t.Fatalf("restore returns a pending version to the queue: %+v", v)
	}
	if entryOf(f.setsView().Pending, id, 1) == nil {
		t.Fatalf("the restored version is back in the queue")
	}

	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, ActionApprove), nil))
	body := f.expectError(f.admin(http.MethodPost, setPath(id, 1, ActionApprove), nil), http.StatusConflict, "invalid_transition")
	if body.Params["from"] != hubwire.SetStatusActive || body.Params["set_id"] != id {
		t.Fatalf("the refusal names the status: %+v", body)
	}
	f.expectError(f.admin(http.MethodPost, setPath(id, 1, ActionReject), map[string]string{"reason": "late"}), http.StatusConflict, "invalid_transition")
	f.expectError(f.admin(http.MethodPost, setPath(other, 1, ActionReject), map[string]string{}), http.StatusBadRequest, "reason_required")
	f.expectError(f.admin(http.MethodPost, setPath(other, 1, ActionReject), map[string]string{"reason": "   "}), http.StatusBadRequest, "reason_required")
	f.expectError(f.admin(http.MethodPost, setPath(id, 1, ActionHide), map[string]string{"reason": "x", "expect_status": "pending"}), http.StatusConflict, "stale")
	if v := f.versionOf(id, 1); v.Status != hubwire.SetStatusActive {
		t.Fatalf("a stale click changes nothing: %+v", v)
	}

	res = f.expectOK(f.admin(http.MethodPost, setPath(id, 1, ActionHide), map[string]string{"reason": "x", "expect_status": "active"}))
	if res.Params["listed_after"] != float64(0) || f.published(id) != nil {
		t.Fatalf("hiding the only listed version unlists the set: %+v", res)
	}
	hidden = entryOf(f.setsView().Hidden, id, 1)
	if hidden == nil || hidden.HiddenFrom != hubwire.SetStatusActive {
		t.Fatalf("a hidden active version shows where it came from: %+v", hidden)
	}
	res = f.expectOK(f.admin(http.MethodPost, setPath(id, 1, ActionRestore), nil))
	if res.Params["listed_after"] != float64(1) || f.published(id) == nil {
		t.Fatalf("restore relists the version: %+v", res)
	}
	if res.Build == nil || res.Build.State != catalogue.BuildIdle || res.Build.LastOK == nil {
		t.Fatalf("the answer carries the build state: %+v", res.Build)
	}
	f.expectError(f.admin(http.MethodPost, setPath(id, 1, ActionRestore), nil), http.StatusConflict, "invalid_transition")
	f.expectError(f.admin(http.MethodPost, setPath(unknownSetID, 1, ActionHide), map[string]string{"reason": "x"}), http.StatusNotFound, "not_found")
}

func TestHideFallsBackToTheListedPredecessor(t *testing.T) {
	f := newFixture(t, password)
	ctx := context.Background()
	author := testkit.Identity(t)
	id, _ := f.shareAs(author, testkit.SampleSet("Fallback", "fallback.example"), authorAddress)
	f.approve(id)
	if _, v := f.shareAs(author, nextVersion("Fallback", id, 1, 8, "fallback.example"), authorAddress); v != 2 {
		t.Fatalf("a derived share by the author is version 2, got %d", v)
	}
	if err := f.store.Approve(ctx, id, 2, f.clock); err != nil {
		t.Fatal(err)
	}
	f.build()
	if cs := f.published(id); cs == nil || cs.Version != 2 {
		t.Fatalf("the newest active version is listed: %+v", cs)
	}

	gen, _ := f.store.DirtyGeneration(ctx)
	rebuilds := f.rebuilds
	item := []ModerationItemRequest{{SetID: id, Version: 2}}
	resp, view := f.moderate(ModerationRequest{Action: ActionHide, Reason: "regressed", Items: item, DryRun: true})
	if resp.status != http.StatusOK || view.Code != "moderation.preview" || len(view.Items) != 1 {
		t.Fatalf("dry run: %d %s", resp.status, resp.body)
	}
	if it := view.Items[0]; !it.OK || it.From != hubwire.SetStatusActive || it.To != hubwire.SetStatusHidden || it.Listed != 2 || it.ListedAfter != 1 || it.Withheld != "" {
		t.Fatalf("the preview names the fallback: %+v", it)
	}
	_, view = f.moderate(ModerationRequest{Action: ActionHide, Reason: "regressed", Items: item, DryRun: true, Withdraw: true})
	if it := view.Items[0]; it.ListedAfter != 0 || it.Withheld != store.WithheldWithdrawn {
		t.Fatalf("the preview of a withdrawal empties the set: %+v", it)
	}
	if v := f.versionOf(id, 2); v.Status != hubwire.SetStatusActive {
		t.Fatalf("a dry run changes nothing: %+v", v)
	}
	if set, _, _ := f.store.GetSet(ctx, id); !set.WithdrawnAt.IsZero() {
		t.Fatalf("a dry run withdraws nothing")
	}
	if after, _ := f.store.DirtyGeneration(ctx); after != gen || f.rebuilds != rebuilds || len(f.auditLog("").Items) != 0 {
		t.Fatalf("a dry run writes nothing, builds nothing and audits nothing")
	}

	res := f.expectOK(f.admin(http.MethodPost, setPath(id, 2, ActionHide), map[string]string{"reason": "regressed"}))
	if res.Params["listed_after"] != float64(1) {
		t.Fatalf("hiding v2 falls back to v1: %+v", res)
	}
	if cs := f.published(id); cs == nil || cs.Version != 1 {
		t.Fatalf("the catalogue lists v1 after the hide: %+v", cs)
	}
	f.expectOK(f.admin(http.MethodPost, setPath(id, 2, ActionRestore), nil))
	if cs := f.published(id); cs == nil || cs.Version != 2 {
		t.Fatalf("restoring v2 lists it again: %+v", cs)
	}

	res = f.expectOK(f.admin(http.MethodPost, setPath(id, 2, ActionHide), map[string]interface{}{"reason": "abandoned", "withdraw": true}))
	if res.Params["listed_after"] != float64(0) {
		t.Fatalf("hide with withdraw lists nothing: %+v", res)
	}
	if f.published(id) != nil {
		t.Fatalf("hide with withdraw removes the whole set from the next catalogue")
	}
	withheld := entryOf(f.setsView().Withheld, id, 0)
	if withheld == nil || withheld.Withheld != store.WithheldWithdrawn || withheld.Version != 1 {
		t.Fatalf("the set is withheld with its listed version: %+v", withheld)
	}
	var detail SetDetailView
	f.admin(http.MethodGet, PathAPI+"/sets/"+id, nil).decode(t, &detail)
	if detail.WithdrawnAt == nil || detail.WithdrawReason != "abandoned" || detail.Withheld != store.WithheldWithdrawn || detail.ListedVersion != 0 {
		t.Fatalf("detail of a withdrawn set: %+v", detail)
	}
}

func TestWithdrawAndReinstateOverTheAPI(t *testing.T) {
	f := newFixture(t, password)
	author := testkit.Identity(t)
	id, _ := f.shareAs(author, testkit.SampleSet("Withdrawn", "withdrawn.example"), authorAddress)
	f.approve(id)
	f.build()
	if f.published(id) == nil {
		t.Fatalf("the set starts listed")
	}
	f.expectError(f.admin(http.MethodPost, PathAPI+"/sets/"+id+"/reinstate", nil), http.StatusConflict, "not_withdrawn")

	res := f.expectOK(f.admin(http.MethodPost, PathAPI+"/sets/"+id+"/withdraw", map[string]string{"reason": "author asked"}))
	if res.Code != "set.withdrawn" || res.Params["set_id"] != id {
		t.Fatalf("withdraw: %+v", res)
	}
	if f.published(id) != nil {
		t.Fatalf("a withdrawn set leaves the catalogue")
	}
	sets := f.setsView()
	if entryOf(sets.Listed, id, 0) != nil {
		t.Fatalf("a withdrawn set is not listed: %+v", sets.Listed)
	}
	if w := entryOf(sets.Withheld, id, 0); w == nil || w.Withheld != "set_withdrawn" {
		t.Fatalf("a withdrawn set is withheld: %+v", sets.Withheld)
	}
	f.expectError(f.admin(http.MethodPost, PathAPI+"/sets/"+id+"/withdraw", map[string]string{"reason": "again"}), http.StatusConflict, "set_withdrawn")

	_, v2 := f.shareAs(author, nextVersion("Withdrawn", id, 1, 8, "withdrawn.example"), authorAddress)
	f.expectError(f.admin(http.MethodPost, setPath(id, v2, ActionApprove), nil), http.StatusConflict, "set_withdrawn")
	if v := f.versionOf(id, v2); v.Status != hubwire.SetStatusPending {
		t.Fatalf("a refused approval changes nothing: %+v", v)
	}
	f.expectOK(f.admin(http.MethodPost, setPath(id, v2, ActionApprove), map[string]bool{"force": true}))
	if f.published(id) != nil {
		t.Fatalf("a forced approval does not reinstate the set")
	}

	res = f.expectOK(f.admin(http.MethodPost, PathAPI+"/sets/"+id+"/reinstate", nil))
	if res.Code != "set.reinstated" {
		t.Fatalf("reinstate: %+v", res)
	}
	if cs := f.published(id); cs == nil || cs.Version != v2 {
		t.Fatalf("a reinstated set is listed with its newest active version: %+v", cs)
	}
	if len(f.setsView().Withheld) != 0 {
		t.Fatalf("nothing is withheld after the reinstatement")
	}
	f.expectError(f.admin(http.MethodPost, PathAPI+"/sets/"+unknownSetID+"/withdraw", nil), http.StatusNotFound, "not_found")
	f.expectError(f.admin(http.MethodPost, PathAPI+"/sets/nope/withdraw", nil), http.StatusNotFound, "not_found")
}

func TestBanQuarantinesAuthorAndVotes(t *testing.T) {
	f := newFixture(t, password)
	author := testkit.Identity(t)
	listedID, _ := f.shareAs(author, testkit.SampleSet("Quarantine", "quarantine.example"), authorAddress)
	f.approve(listedID)
	listed := f.versionOf(listedID, 1)
	loyal := testkit.Identity(t)
	fickle := testkit.Identity(t)
	f.voteAs(loyal, listedID, 1, listed.FP, voterAddress)
	f.voteAs(fickle, listedID, 1, listed.FP, otherAddress)
	second := testkit.SampleSet("Second strategy", "second.example")
	second.Faking.TTL = 9
	pendingID, _ := f.shareAs(author, second, authorAddress)
	f.build()
	before := f.published(listedID).Scores.Global
	if before.Devices != 3 {
		t.Fatalf("the author and two voters: %+v", before)
	}

	authorKey := f.keyOf(author)
	var impact KeyImpactView
	resp := f.admin(http.MethodGet, PathAPI+"/keys/"+authorKey+"/impact", nil)
	if resp.status != http.StatusOK {
		t.Fatalf("impact: %d %s", resp.status, resp.body)
	}
	resp.decode(t, &impact)
	if len(impact.Listed) != 1 || impact.Listed[0].SetID != listedID || impact.Listed[0].Version != 1 || impact.Listed[0].Title != "Quarantine" {
		t.Fatalf("impact lists the listed set: %+v", impact.Listed)
	}
	if len(impact.Pending) != 1 || impact.Pending[0].SetID != pendingID || impact.Pending[0].Title != "Second strategy" {
		t.Fatalf("impact lists the pending share: %+v", impact.Pending)
	}
	if impact.Votes != 2 || impact.VotedSets != 2 || impact.Reports != 0 || impact.Mirrors == nil || len(impact.Mirrors) != 0 {
		t.Fatalf("impact counts: %+v", impact)
	}
	f.expectError(f.admin(http.MethodGet, PathAPI+"/keys/nope/impact", nil), http.StatusBadRequest, "bad_key")

	f.expectOK(f.admin(http.MethodPost, PathAPI+"/keys/"+f.keyOf(fickle)+"/ban", map[string]string{"reason": "vote ring"}))
	after := f.published(listedID).Scores.Global
	if after.Devices != before.Devices-1 || after.N >= before.N {
		t.Fatalf("a banned voter's vote leaves the score at the next build: before %+v after %+v", before, after)
	}

	f.expectOK(f.admin(http.MethodPost, PathAPI+"/keys/"+authorKey+"/ban", map[string]string{"reason": "spam"}))
	if f.published(listedID) != nil {
		t.Fatalf("a banned author's set leaves the next catalogue")
	}
	sets := f.setsView()
	if w := entryOf(sets.Withheld, listedID, 1); w == nil || w.Withheld != "author_banned" || !w.AuthorBanned {
		t.Fatalf("the set is withheld for the ban: %+v", sets.Withheld)
	}
	if entryOf(sets.Listed, listedID, 0) != nil {
		t.Fatalf("a quarantined set is not listed")
	}
	if p := entryOf(sets.Pending, pendingID, 1); p == nil || !p.AuthorBanned {
		t.Fatalf("the queue flags the banned author: %+v", p)
	}
	f.expectError(f.admin(http.MethodPost, setPath(pendingID, 1, ActionApprove), nil), http.StatusConflict, "author_banned")
	f.expectOK(f.admin(http.MethodPost, setPath(pendingID, 1, ActionApprove), map[string]bool{"force": true}))
	if f.published(pendingID) != nil {
		t.Fatalf("a forced approval of a banned author stays unlisted")
	}

	f.expectOK(f.admin(http.MethodPost, PathAPI+"/keys/"+authorKey+"/unban", nil))
	if f.published(listedID) == nil || f.published(pendingID) == nil {
		t.Fatalf("unbanning restores both sets at the next build")
	}
	unknown := fmt.Sprintf("%064x", 99)
	f.expectError(f.admin(http.MethodPost, PathAPI+"/keys/"+unknown+"/unban", nil), http.StatusNotFound, "unknown_key")
	f.expectOK(f.admin(http.MethodPost, PathAPI+"/keys/"+unknown+"/ban", map[string]string{"reason": "pre-emptive"}))
	if k, err := f.store.GetKey(context.Background(), unknown); err != nil || !k.Banned {
		t.Fatalf("banning an unseen key from the console creates it banned: %+v %v", k, err)
	}
}

func TestReportsLifecycleOverTheAPI(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Reported", authorAddress, "reported.example")
	f.approve(id)
	f.build()
	f.reportAs(testkit.Identity(t), id, 1, authorAddress)
	f.reportAs(testkit.Identity(t), id, 1, otherAddress)
	if v := f.versionOf(id, 1); v.Status != hubwire.SetStatusActive {
		t.Fatalf("two asns are not enough: %+v", v)
	}
	f.reportAs(testkit.Identity(t), id, 1, thirdAddress)
	if v := f.versionOf(id, 1); v.Status != hubwire.SetStatusHidden || v.StatusReason != "reports" || v.HiddenFrom != hubwire.SetStatusActive {
		t.Fatalf("three independent reports hide the version: %+v", v)
	}
	audit := f.auditLog("?target_kind=set&target_id=" + id)
	if len(audit.Items) != 1 || audit.Items[0].Actor != store.ActorSystem || audit.Items[0].Action != "set.hide" || audit.Items[0].TargetLabel != "Reported" ||
		audit.Items[0].After["status"] != "hidden" || audit.Items[0].After["independent_reports"] != float64(3) {
		t.Fatalf("the auto-hide is audited as the system: %+v", audit.Items)
	}

	page := f.reportsPage("")
	if page.Total != 3 || len(page.Items) != 3 || page.Counts[store.ReportOpen] != 3 || page.Counts[store.ReportDismissed] != 0 || page.Counts[store.ReportResolved] != 0 {
		t.Fatalf("open reports: %+v", page)
	}
	for _, r := range page.Items {
		if r.Title != "Reported" || r.SetStatus != hubwire.SetStatusHidden || r.State != store.ReportOpen || !r.Counts || r.ASNObserved == "" {
			t.Fatalf("a report carries its set: %+v", r)
		}
	}
	if resp := f.admin(http.MethodGet, PathAPI+"/reports?state=bogus", nil); resp.status != http.StatusBadRequest {
		t.Fatalf("an unknown state is refused: %d", resp.status)
	}
	first := page.Items[0].ID

	res := f.expectOK(f.admin(http.MethodPost, PathAPI+"/reports/"+fmt.Sprint(first)+"/dismiss", map[string]string{"note": "duplicate"}))
	if res.Code != "reports.updated" || res.Params["count"] != float64(1) {
		t.Fatalf("dismiss: %+v", res)
	}
	var detail SetDetailView
	f.admin(http.MethodGet, PathAPI+"/sets/"+id, nil).decode(t, &detail)
	if detail.Versions[0].Independent != 2 || detail.Versions[0].OpenReports != 2 {
		t.Fatalf("a dismissed report lowers the count below three: %+v", detail.Versions[0])
	}
	f.expectOK(f.admin(http.MethodPost, PathAPI+"/reports/"+fmt.Sprint(first)+"/reopen", nil))
	f.admin(http.MethodGet, PathAPI+"/sets/"+id, nil).decode(t, &detail)
	if detail.Versions[0].Independent != 3 {
		t.Fatalf("a reopened report counts again: %+v", detail.Versions[0])
	}

	res = f.expectOK(f.admin(http.MethodPost, setPath(id, 1, ActionRestore), nil))
	if res.Params["listed_after"] != float64(1) || f.versionOf(id, 1).Status != hubwire.SetStatusActive {
		t.Fatalf("restore: %+v", res)
	}
	if page = f.reportsPage(""); page.Total != 0 || page.Counts[store.ReportDismissed] != 3 {
		t.Fatalf("the restore dismisses the reports that hid it: %+v", page)
	}
	for _, r := range f.reportsPage("?state=dismissed").Items {
		if r.Resolution != store.ResolutionRestored || r.ResolvedAt == nil || r.Counts {
			t.Fatalf("dismissed by the restore: %+v", r)
		}
	}

	f.reportAs(testkit.Identity(t), id, 1, thirdAddress)
	if v := f.versionOf(id, 1); v.Status != hubwire.SetStatusActive {
		t.Fatalf("one further report does not hide a restored version: %+v", v)
	}
	page = f.reportsPage("")
	if page.Total != 1 || page.Items[0].SetStatus != hubwire.SetStatusActive {
		t.Fatalf("the fresh report is the only open one: %+v", page)
	}
	fresh := page.Items[0].ID
	res = f.expectOK(f.admin(http.MethodPost, PathAPI+"/reports/bulk", ReportsActionRequest{IDs: []int64{fresh}, Action: ActionResolve, Note: "handled"}))
	if res.Params["count"] != float64(1) || res.Params["state"] != store.ReportResolved {
		t.Fatalf("bulk resolve: %+v", res)
	}
	f.expectError(f.admin(http.MethodPost, PathAPI+"/reports/bulk", ReportsActionRequest{IDs: []int64{fresh, 9999}, Action: ActionDismiss}), http.StatusNotFound, "not_found")
	f.expectError(f.admin(http.MethodPost, PathAPI+"/reports/bulk", ReportsActionRequest{IDs: []int64{fresh}, Action: "explode"}), http.StatusBadRequest, "unknown_action")
	f.expectError(f.admin(http.MethodPost, PathAPI+"/reports/"+fmt.Sprint(fresh)+"/explode", nil), http.StatusBadRequest, "unknown_action")
	f.expectError(f.admin(http.MethodPost, PathAPI+"/reports/abc/dismiss", nil), http.StatusNotFound, "not_found")
	triage := f.auditLog("?target_kind=report")
	if len(triage.Items) != 3 || triage.Items[0].Action != "report.resolve" || triage.Items[1].Action != "report.reopen" || triage.Items[2].Action != "report.dismiss" ||
		triage.Items[2].TargetID != fmt.Sprint(first) || triage.Items[2].Reason != "duplicate" {
		t.Fatalf("each triage step is audited: %+v", triage.Items)
	}

	brigaded, _ := f.share("Brigaded", otherAddress, "brigaded.example")
	f.approve(brigaded)
	brigade := testkit.Identity(t)
	f.reportAs(brigade, brigaded, 1, authorAddress)
	f.reportAs(testkit.Identity(t), brigaded, 1, otherAddress)
	f.expectOK(f.admin(http.MethodPost, PathAPI+"/keys/"+f.keyOf(brigade)+"/ban", map[string]string{"reason": "brigade"}))
	f.reportAs(testkit.Identity(t), brigaded, 1, thirdAddress)
	if v := f.versionOf(brigaded, 1); v.Status != hubwire.SetStatusActive {
		t.Fatalf("a report from a key banned afterwards stops counting: %+v", v)
	}
	page = f.reportsPage("?state=all&set=" + brigaded)
	flagged := 0
	for _, r := range page.Items {
		if r.KeyBanned {
			flagged++
			if r.Counts || r.KeyHMAC != f.keyOf(brigade) {
				t.Fatalf("the banned reporter's report does not count: %+v", r)
			}
		}
	}
	if page.Total != 3 || flagged != 1 {
		t.Fatalf("the set's reports: %+v", page)
	}
}

func TestBulkModerationOverTheAPI(t *testing.T) {
	f := newFixture(t, password)
	a, _ := f.share("Bulk A", authorAddress, "a.example")
	b, _ := f.share("Bulk B", otherAddress, "b.example")
	rebuilds := f.rebuilds
	resp, view := f.moderate(ModerationRequest{Action: ActionApprove, Items: []ModerationItemRequest{{SetID: a, Version: 1}, {SetID: b, Version: 1}}})
	if resp.status != http.StatusOK || view.Code != "moderation.batch" || view.BatchID == "" || view.Params["applied"] != float64(2) || view.Params["skipped"] != float64(0) {
		t.Fatalf("bulk approve: %d %s", resp.status, resp.body)
	}
	if len(view.Items) != 2 || !view.Items[0].OK || !view.Items[1].OK {
		t.Fatalf("items: %+v", view.Items)
	}
	if f.rebuilds != rebuilds+1 {
		t.Fatalf("a batch requests one build, got %d", f.rebuilds-rebuilds)
	}
	approvals := f.auditLog("?action=set.approve")
	if len(approvals.Items) != 2 || approvals.Items[0].BatchID != view.BatchID || approvals.Items[1].BatchID != view.BatchID {
		t.Fatalf("both audit rows share the batch id %s: %+v", view.BatchID, approvals.Items)
	}
	if batch := f.auditLog("?batch=" + view.BatchID); len(batch.Items) != 2 {
		t.Fatalf("the batch filter returns the batch: %+v", batch.Items)
	}
	if none := f.auditLog("?batch=unknown"); len(none.Items) != 0 {
		t.Fatalf("an unknown batch matches nothing: %+v", none.Items)
	}
	if f.published(a) == nil || f.published(b) == nil {
		t.Fatalf("both sets are published")
	}

	c, _ := f.share("Bulk C", thirdAddress, "c.example")
	rebuilds = f.rebuilds
	mixed := []ModerationItemRequest{{SetID: c, Version: 1}, {SetID: a, Version: 1}}
	resp, _ = f.moderate(ModerationRequest{Action: ActionApprove, Items: mixed})
	body := f.expectError(resp, http.StatusConflict, "batch_invalid")
	if len(body.Items) != 2 || !body.Items[0].OK || body.Items[1].OK || body.Items[1].Code != "invalid_transition" || body.Items[1].SetID != a {
		t.Fatalf("the refusal carries per-item codes: %+v", body.Items)
	}
	if v := f.versionOf(c, 1); v.Status != hubwire.SetStatusPending || f.rebuilds != rebuilds || len(f.auditLog("?action=set.approve").Items) != 2 {
		t.Fatalf("an invalid batch applies nothing: %+v", v)
	}
	resp, view = f.moderate(ModerationRequest{Action: ActionApprove, Items: mixed, Partial: true})
	if resp.status != http.StatusOK || view.Params["applied"] != float64(1) || view.Params["skipped"] != float64(1) || view.Items[1].Code != "invalid_transition" {
		t.Fatalf("partial: %d %s", resp.status, resp.body)
	}
	if v := f.versionOf(c, 1); v.Status != hubwire.SetStatusActive {
		t.Fatalf("the valid item applies: %+v", v)
	}

	resp, _ = f.moderate(ModerationRequest{Action: "explode", Items: mixed})
	f.expectError(resp, http.StatusBadRequest, "unknown_action")
	resp, _ = f.moderate(ModerationRequest{Action: ActionApprove})
	f.expectError(resp, http.StatusBadRequest, "nothing_to_do")
	f.expectError(f.admin(http.MethodPost, PathAPI+"/moderation", map[string]string{"action": ActionApprove, "bogus": "x"}), http.StatusBadRequest, "bad_request")
}

type auditStep struct {
	name   string
	path   string
	body   interface{}
	action string
	before map[string]interface{}
	after  map[string]interface{}
}

func TestAuditTrailOverTheAPI(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Audited", authorAddress, "audited.example")
	doomed, _ := f.share("Doomed", otherAddress, "doomed.example")
	authorKey := f.versionOf(id, 1).UploaderHMAC
	other := testkit.Identity(t)
	steps := []auditStep{
		{"approve", setPath(id, 1, ActionApprove), nil, "set.approve", map[string]interface{}{"status": "pending"}, map[string]interface{}{"status": "active"}},
		{"hide", setPath(id, 1, ActionHide), map[string]string{"reason": "checking"}, "set.hide", map[string]interface{}{"status": "active"}, map[string]interface{}{"status": "hidden"}},
		{"restore", setPath(id, 1, ActionRestore), nil, "set.restore", map[string]interface{}{"status": "hidden"}, map[string]interface{}{"status": "active"}},
		{"reject", setPath(doomed, 1, ActionReject), map[string]string{"reason": "spam"}, "set.reject", map[string]interface{}{"status": "pending"}, map[string]interface{}{"status": "rejected", "status_reason": "spam"}},
		{"withdraw", PathAPI + "/sets/" + id + "/withdraw", map[string]string{"reason": "asked"}, "set.withdraw", map[string]interface{}{"withdrawn": false}, map[string]interface{}{"withdrawn": true}},
		{"reinstate", PathAPI + "/sets/" + id + "/reinstate", nil, "set.reinstate", map[string]interface{}{"withdrawn": true}, map[string]interface{}{"withdrawn": false}},
		{"ban", PathAPI + "/keys/" + authorKey + "/ban", map[string]string{"reason": "spam"}, "key.ban", map[string]interface{}{"banned": false}, map[string]interface{}{"banned": true}},
		{"unban", PathAPI + "/keys/" + authorKey + "/unban", nil, "key.unban", map[string]interface{}{"banned": true}, map[string]interface{}{"banned": false}},
		{"trust", PathAPI + "/keys/" + authorKey + "/trust", nil, "key.trust", map[string]interface{}{"trusted": false}, map[string]interface{}{"trusted": true}},
		{"revoke", PathAPI + "/catalogue/revoke", map[string]string{"key_id": other.KeyID(), "confirm": other.KeyID()}, "catalogue.revoke", nil, map[string]interface{}{"revoked": other.KeyID()}},
		{"epoch", PathAPI + "/catalogue/epoch", nil, "catalogue.epoch", nil, nil},
		{"delete", PathAPI + "/sets/" + doomed + "/delete", map[string]string{"confirm": doomed}, "set.delete", map[string]interface{}{"title": "Doomed", "versions": float64(1)}, nil},
	}
	for _, step := range steps {
		count := len(f.auditLog("?limit=200").Items)
		f.clock = f.clock.Add(time.Second)
		f.expectOK(f.admin(http.MethodPost, step.path, step.body))
		items := f.auditLog("?limit=200").Items
		if len(items) != count+1 {
			t.Fatalf("%s must write exactly one audit row, wrote %d", step.name, len(items)-count)
		}
		e := items[0]
		if e.Action != step.action || e.Actor != store.ActorBasic || e.ActorRef != "" || e.ActorIP != "127.0.0.1" {
			t.Fatalf("%s: audit row %+v", step.name, e)
		}
		for k, want := range step.before {
			if e.Before[k] != want {
				t.Errorf("%s: before[%s] = %v, want %v", step.name, k, e.Before[k], want)
			}
		}
		for k, want := range step.after {
			if e.After[k] != want {
				t.Errorf("%s: after[%s] = %v, want %v", step.name, k, e.After[k], want)
			}
		}
	}
	deleted := f.auditLog("?action=set.delete").Items
	if len(deleted) != 1 || deleted[0].TargetLabel != "Doomed" {
		t.Fatalf("a deleted set keeps its label from the snapshot: %+v", deleted)
	}
	keyRows := f.auditLog("?action=key.")
	if len(keyRows.Items) != 3 || keyRows.Items[0].TargetLabel != hubdata.AuthorLabel(authorKey) {
		t.Fatalf("the action prefix filters key rows: %+v", keyRows.Items)
	}

	signIn := f.signIn(password)
	if signIn.status != http.StatusOK {
		t.Fatalf("sign-in: %d %s", signIn.status, signIn.body)
	}
	cookie := sessionCookieOf(t, signIn)
	resp := f.request(http.MethodPost, setPath(id, 1, ActionHide), map[string]string{"reason": "console"}, withCookie(cookie))
	f.expectOK(resp)
	sum := sha256.Sum256([]byte(cookie.Value))
	consoleRows := f.auditLog("?actor=console")
	if len(consoleRows.Items) != 1 {
		t.Fatalf("one console row: %+v", consoleRows.Items)
	}
	if e := consoleRows.Items[0]; e.Action != "set.hide" || e.ActorRef != hex.EncodeToString(sum[:4]) || e.ActorIP != "127.0.0.1" {
		t.Fatalf("a cookie session is recorded as the console with a session ref: %+v", e)
	}

	var ids []int64
	before := ""
	for page := 0; page < 10; page++ {
		got := f.auditLog("?target_kind=set&target_id=" + id + "&limit=2" + before)
		if len(got.Items) > 2 {
			t.Fatalf("a page holds at most the limit: %d", len(got.Items))
		}
		for _, e := range got.Items {
			if e.TargetID != id || e.TargetKind != store.TargetSet || e.TargetLabel != "Audited" {
				t.Fatalf("the filter leaks: %+v", e)
			}
			ids = append(ids, e.ID)
		}
		if got.Next == 0 {
			break
		}
		before = fmt.Sprintf("&before=%d", got.Next)
	}
	if len(ids) != 6 {
		t.Fatalf("approve, hide, restore, withdraw, reinstate and the console hide: %v", ids)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] >= ids[i-1] {
			t.Fatalf("pages walk newest first without repeats: %v", ids)
		}
	}
	for _, bad := range []string{"?limit=zero", "?before=abc", "?since=yesterday"} {
		if resp := f.admin(http.MethodGet, PathAPI+"/audit"+bad, nil); resp.status != http.StatusBadRequest {
			t.Errorf("%s must be refused, got %d", bad, resp.status)
		}
	}
}

func TestBuildHistoryOverTheAPI(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Built", authorAddress, "built.example")
	f.approve(id)
	resp := f.admin(http.MethodPost, PathAPI+"/catalogue/build", map[string]bool{"wait": true})
	if resp.status != http.StatusOK {
		t.Fatalf("build: %d %s", resp.status, resp.body)
	}
	builds := func(query string) BuildsPageView {
		t.Helper()
		resp := f.admin(http.MethodGet, PathAPI+"/catalogue/builds"+query, nil)
		if resp.status != http.StatusOK {
			t.Fatalf("builds: %d %s", resp.status, resp.body)
		}
		var page BuildsPageView
		resp.decode(t, &page)
		return page
	}
	status := func() BuildStateView {
		t.Helper()
		var view BuildStateView
		resp := f.admin(http.MethodGet, PathAPI+"/catalogue/status", nil)
		if resp.status != http.StatusOK {
			t.Fatalf("status: %d %s", resp.status, resp.body)
		}
		resp.decode(t, &view)
		return view
	}
	page := builds("")
	if len(page.Items) != 1 {
		t.Fatalf("one build: %+v", page)
	}
	first := page.Items[0]
	if !first.OK || first.Seq != 1 || first.Trigger != catalogue.TriggerManual || first.Sets != 1 || !first.Changed || first.FinishedAt == nil ||
		len(first.Changes.Added) != 1 || first.Changes.Added[0].SetID != id || first.Changes.Added[0].Title != "Built" {
		t.Fatalf("the first build adds the set: %+v", first)
	}
	if requests := f.auditLog("?action=catalogue.build").Items; len(requests) != 1 || requests[0].Actor != store.ActorBasic {
		t.Fatalf("a console build request is audited: %+v", requests)
	}

	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, ActionHide), map[string]string{"reason": "abuse"}))
	page = builds("")
	if len(page.Items) != 2 || len(page.Items[0].Changes.Removed) != 1 || page.Items[0].Changes.Removed[0].SetID != id || page.Items[0].Trigger != catalogue.TriggerSchedule {
		t.Fatalf("the hide rebuild records the removal: %+v", page.Items)
	}
	st := status()
	if st.State != catalogue.BuildIdle || st.Dirty || st.LastOK == nil || st.LastOK.Seq != 2 || st.LastError != nil {
		t.Fatalf("status after two builds: %+v", st)
	}

	manifest := filepath.Join(f.builder.PublicDir, catalogue.ManifestFile)
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(manifest, "occupied"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.expectError(f.admin(http.MethodPost, PathAPI+"/catalogue/build", map[string]bool{"wait": true}), http.StatusInternalServerError, "build_failed")
	st = status()
	if st.LastError == nil || st.LastError.OK || st.LastError.Error == "" || st.LastOK == nil || st.LastOK.Seq != 2 {
		t.Fatalf("status reports the failure next to the last good build: %+v", st)
	}
	if failed := builds("?only=failed"); len(failed.Items) != 1 || failed.Items[0].OK {
		t.Fatalf("failed builds: %+v", failed)
	}
	if changes := builds("?only=changes"); len(changes.Items) != 3 {
		t.Fatalf("changes list both content changes and the failure: %+v", changes)
	}
	if limited := builds("?limit=1"); len(limited.Items) != 1 || limited.Next == 0 {
		t.Fatalf("paging: %+v", limited)
	}
	if resp := f.admin(http.MethodGet, PathAPI+"/catalogue/builds?limit=x", nil); resp.status != http.StatusBadRequest {
		t.Fatalf("a bad limit is refused: %d", resp.status)
	}

	if err := os.RemoveAll(manifest); err != nil {
		t.Fatal(err)
	}
	f.expectOK(f.admin(http.MethodPost, PathAPI+"/catalogue/build", map[string]bool{"wait": true}))
	if st = status(); st.LastError != nil || st.LastOK == nil || st.Dirty {
		t.Fatalf("a good build clears the reported failure: %+v", st)
	}
}

func TestLineageAgainstTheListedVersion(t *testing.T) {
	f := newFixture(t, password)
	ctx := context.Background()
	author := testkit.Identity(t)
	id, _ := f.shareAs(author, testkit.SampleSet("Lineage", "lineage.example"), authorAddress)
	f.shareAs(author, nextVersion("Lineage", id, 1, 8, "lineage.example"), authorAddress)
	f.shareAs(author, nextVersion("Lineage", id, 2, 9, "lineage.example"), authorAddress)
	if err := f.store.Approve(ctx, id, 3, f.clock); err != nil {
		t.Fatal(err)
	}
	if _, v := f.shareAs(author, nextVersion("Lineage", id, 3, 10, "lineage.example"), authorAddress); v != 4 {
		t.Fatalf("fourth version expected, got %d", v)
	}

	other := testkit.Identity(t)
	relisted, _ := f.shareAs(other, testkit.SampleSet("Relist", "relist.example"), otherAddress)
	if err := f.store.Reject(ctx, relisted, 1, "no", f.clock); err != nil {
		t.Fatal(err)
	}
	f.shareAs(other, nextVersion("Relist", relisted, 1, 8, "relist.example"), otherAddress)
	fresh, _ := f.share("Fresh", thirdAddress, "fresh.example")

	pending := f.setsView().Pending
	for _, c := range []struct {
		setID   string
		version int
		kind    string
		current int
	}{
		{id, 1, LineageOlder, 3},
		{id, 2, LineageOlder, 3},
		{id, 4, LineageReplaces, 3},
		{relisted, 2, LineageRelists, 0},
		{fresh, 1, LineageFirst, 0},
	} {
		e := entryOf(pending, c.setID, c.version)
		if e == nil || e.Lineage == nil {
			t.Fatalf("%s/%d must be pending with a lineage: %+v", c.setID, c.version, e)
		}
		if e.Lineage.Kind != c.kind || e.Lineage.CurrentVersion != c.current {
			t.Errorf("%s/%d lineage %+v, want %s against %d", c.setID, c.version, e.Lineage, c.kind, c.current)
		}
	}
}

func TestRevokeCanonicalisationOverTheAPI(t *testing.T) {
	f := newFixture(t, password)
	revoke := func(body map[string]interface{}) response {
		return f.admin(http.MethodPost, PathAPI+"/catalogue/revoke", body)
	}
	other := testkit.Identity(t)
	canonical := other.KeyID()
	alt := altEncoding(t, canonical)
	builtin := hubwire.BuiltinHubKeys[0]

	f.expectError(revoke(map[string]interface{}{"key_id": canonical, "confirm": builtin}), http.StatusBadRequest, "confirm_mismatch")
	f.expectError(revoke(map[string]interface{}{"key_id": altEncoding(t, f.web.KeyID), "confirm": f.web.KeyID}), http.StatusBadRequest, "own_key")
	f.expectError(revoke(map[string]interface{}{"key_id": altEncoding(t, builtin), "confirm": builtin}), http.StatusBadRequest, "builtin_key")
	f.expectError(revoke(map[string]interface{}{"key_id": canonical[:10], "confirm": canonical[:10]}), http.StatusBadRequest, "bad_key")

	res := f.expectOK(revoke(map[string]interface{}{"key_id": alt, "confirm": " " + canonical + " "}))
	if res.Code != "catalogue.revoked" || res.Params["key_id"] != canonical {
		t.Fatalf("a non-canonical encoding is revoked under its canonical form: %+v", res)
	}
	res = f.expectOK(revoke(map[string]interface{}{"key_id": canonical, "confirm": alt}))
	if res.Code != "catalogue.revoke_known" {
		t.Fatalf("the canonical form is recognised as already revoked: %+v", res)
	}
	res = f.expectOK(revoke(map[string]interface{}{"key_id": builtin, "confirm": builtin, "allow_builtin": true}))
	if res.Code != "catalogue.revoked" {
		t.Fatalf("the override revokes a builtin key: %+v", res)
	}
	var overview OverviewView
	f.admin(http.MethodGet, PathAPI+"/overview", nil).decode(t, &overview)
	if got := strings.Join(overview.Catalogue.RevokedKeys, ","); !strings.Contains(got, canonical) || !strings.Contains(got, builtin) || strings.Contains(got, alt) || len(overview.Catalogue.RevokedKeys) != 2 {
		t.Fatalf("revoked keys hold the canonical forms only: %v", overview.Catalogue.RevokedKeys)
	}
	if latest := f.builder.Latest(); latest == nil || len(latest.Manifest.RevokedKeys) != 2 {
		t.Fatalf("the manifest carries both revocations: %+v", latest)
	}
	if rows := f.auditLog("?action=catalogue.revoke").Items; len(rows) != 2 {
		t.Fatalf("one audit row per new revocation: %+v", rows)
	}
}

func TestMirrorsViewAndChecks(t *testing.T) {
	f := newFixture(t, password)
	ctx := context.Background()
	f.expectError(f.admin(http.MethodPost, PathAPI+"/mirrors/1/check", nil), http.StatusServiceUnavailable, "not_built")
	f.builder.Mirrors = &catalogue.MirrorHealth{Store: f.store, KeyID: f.web.KeyID, Now: func() time.Time { return f.clock }}

	var mu sync.Mutex
	var served []byte
	serve := func(raw []byte) {
		mu.Lock()
		served = raw
		mu.Unlock()
	}
	mux := http.NewServeMux()
	mux.HandleFunc(hubwire.PathHealth, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc(hubwire.PathManifest, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		raw := served
		mu.Unlock()
		_, _ = w.Write(raw)
	})
	healthy := httptest.NewServer(mux)
	t.Cleanup(healthy.Close)
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(broken.Close)

	good, err := f.store.AnnounceMirror(ctx, healthy.URL, "mirror-key", "1.0.0", f.clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetMirrorStatus(ctx, good.ID, store.MirrorApproved, "", f.clock); err != nil {
		t.Fatal(err)
	}
	mirrors := func() MirrorsView {
		t.Helper()
		resp := f.admin(http.MethodGet, PathAPI+"/mirrors", nil)
		if resp.status != http.StatusOK {
			t.Fatalf("mirrors: %d %s", resp.status, resp.body)
		}
		var view MirrorsView
		resp.decode(t, &view)
		return view
	}
	view := mirrors()
	if view.Manifest.Published || view.Manifest.Listed == nil || len(view.Manifest.Listed) != 0 || view.Manifest.HubURL != "https://hub.example" ||
		view.Orphans == nil || len(view.Orphans) != 0 || view.CheckInterval != 600 || view.Window != 86400 {
		t.Fatalf("an unpublished hub: %+v", view)
	}
	if len(view.Mirrors) != 1 || view.Mirrors[0].Announced || view.Mirrors[0].AnnounceNext || view.Mirrors[0].Healthy || view.Mirrors[0].Lag != LagUnknown || view.Mirrors[0].DropsAt != nil {
		t.Fatalf("an unchecked mirror is neither announced nor due: %+v", view.Mirrors)
	}

	signed := hubwire.Manifest{Epoch: 5, Seq: 3, GeneratedAt: f.clock.Format(time.RFC3339), ExpiresAt: f.clock.Add(time.Hour).Format(time.RFC3339)}
	if err := hubwire.SignManifest(&signed, f.builder.Identity); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(signed)
	serve(raw)
	rebuilds := f.rebuilds
	resp := f.admin(http.MethodPost, PathAPI+"/mirrors/"+fmt.Sprint(good.ID)+"/check", nil)
	if resp.status != http.StatusOK {
		t.Fatalf("check: %d %s", resp.status, resp.body)
	}
	var checked MirrorCheckResult
	resp.decode(t, &checked)
	m := checked.Mirror
	if checked.Code != "mirror.checked" || !m.Healthy || m.ServedEpoch != 5 || m.ServedSeq != 3 || m.ServedGeneratedAt != signed.GeneratedAt || m.CheckCode != "" {
		t.Fatalf("a passing check records what the mirror serves: %+v", checked)
	}
	if !m.AnnounceNext || m.DropsAt == nil || !m.DropsAt.Equal(f.clock.Add(catalogue.DefaultMirrorWindow)) {
		t.Fatalf("a passing check makes the mirror due for announcement: %+v", m)
	}
	if f.rebuilds != rebuilds+1 || !m.Announced || m.Lag != LagStale {
		t.Fatalf("the drift triggers a rebuild that announces the mirror; its old epoch is stale: rebuilds %d %+v", f.rebuilds-rebuilds, m)
	}
	view = mirrors()
	if !view.Manifest.Published || len(view.Manifest.Listed) != 2 || view.Manifest.Listed[1] != healthy.URL || !view.Mirrors[0].Announced {
		t.Fatalf("the published manifest lists the mirror: %+v", view)
	}
	if rows := f.auditLog("?action=mirror.check").Items; len(rows) != 1 || rows[0].After["ok"] != true || rows[0].TargetID != fmt.Sprint(good.ID) {
		t.Fatalf("a console check is audited: %+v", rows)
	}

	current, err := os.ReadFile(filepath.Join(f.builder.PublicDir, catalogue.ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	serve(current)
	rebuilds = f.rebuilds
	resp = f.admin(http.MethodPost, PathAPI+"/mirrors/"+fmt.Sprint(good.ID)+"/check", nil)
	resp.decode(t, &checked)
	if checked.Mirror.Lag != LagCurrent || checked.Mirror.ServedSeq != f.builder.Latest().Manifest.Seq || f.rebuilds != rebuilds {
		t.Fatalf("a mirror serving our manifest is current and needs no rebuild: %+v", checked.Mirror)
	}
	resp = f.admin(http.MethodPost, PathAPI+"/mirrors/check", nil)
	res := f.expectOK(resp)
	if res.Code != "mirrors.checked" || res.Params["checked"] != float64(1) || res.Params["failed"] != float64(0) {
		t.Fatalf("checking every approved mirror: %+v", res)
	}

	bad, err := f.store.AnnounceMirror(ctx, broken.URL, "mirror-key", "1.0.0", f.clock)
	if err != nil {
		t.Fatal(err)
	}
	f.expectOK(f.admin(http.MethodPost, PathAPI+"/mirrors/"+fmt.Sprint(bad.ID)+"/reject", map[string]string{"reason": "spam"}))
	resp = f.admin(http.MethodPost, PathAPI+"/mirrors/"+fmt.Sprint(bad.ID)+"/check", nil)
	if resp.status != http.StatusOK {
		t.Fatalf("check of a broken mirror: %d %s", resp.status, resp.body)
	}
	resp.decode(t, &checked)
	if checked.Code != "mirror.check_failed" || checked.Params["stage"] != catalogue.CheckHealth || checked.Mirror.CheckCode != catalogue.CheckHealth ||
		!strings.Contains(checked.Mirror.CheckError, "503") || checked.Mirror.Reason != "spam" || checked.Mirror.Status != store.MirrorRejected {
		t.Fatalf("a failing check records its stage and keeps the moderation reason: %+v", checked)
	}
	if stored, _ := f.store.GetMirror(ctx, bad.ID); stored.Reason != "spam" {
		t.Fatalf("the rejection reason survives the check: %+v", stored)
	}

	if err := f.store.DeleteMirror(ctx, good.ID); err != nil {
		t.Fatal(err)
	}
	view = mirrors()
	if len(view.Orphans) != 1 || view.Orphans[0] != healthy.URL || len(view.Mirrors) != 1 {
		t.Fatalf("a manifest entry without a mirror row is an orphan: %+v", view)
	}
	f.expectError(f.admin(http.MethodPost, PathAPI+"/mirrors/abc/check", nil), http.StatusNotFound, "not_found")
	f.expectError(f.admin(http.MethodPost, PathAPI+"/mirrors/999/check", nil), http.StatusNotFound, "not_found")
}

func TestUnknownConsoleEndpointIsJSON404(t *testing.T) {
	f := newFixture(t, password)
	resp := f.admin(http.MethodGet, PathAPI+"/no/such/thing", nil)
	body := f.expectError(resp, http.StatusNotFound, "not_found")
	if !strings.HasPrefix(resp.headers.Get("Content-Type"), "application/json") || body.Error == "" {
		t.Fatalf("an unknown console endpoint answers JSON: %v %+v", resp.headers, body)
	}
	if resp := f.get(PathAPI + "/no/such/thing"); resp.status != http.StatusUnauthorized {
		t.Fatalf("the fallback is guarded like every endpoint: %d", resp.status)
	}
}
