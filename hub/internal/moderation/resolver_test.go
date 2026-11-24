package moderation

import (
	"fmt"
	"testing"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/store"
)

func plainProjection() map[string]interface{} {
	return map[string]interface{}{"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}}}
}

func personalProjection() map[string]interface{} {
	p := plainProjection()
	p["dns"] = map[string]interface{}{"enabled": true, "doh_url": "https://dns.nextdns.io/canaryid"}
	return p
}

func (e *env) setWith(projection map[string]interface{}, author string, statuses ...string) string {
	e.t.Helper()
	e.projection = projection
	defer func() { e.projection = plainProjection() }()
	return e.set(author, statuses...)
}

func (e *env) expectPersonalResolver(err error, id string, version int, status string) {
	e.t.Helper()
	me := codeOf(e.t, err, CodePersonalResolver)
	if me.Params["set_id"] != id || me.Params["version"] != version {
		e.t.Fatalf("the refusal must name the version: %+v", me.Params)
	}
	if v := e.version(id, version); v.Status != status {
		e.t.Fatalf("a refused action changes nothing, %s/%d is %s", id, version, v.Status)
	}
}

func TestAStoredPersonalResolverIsNotPublished(t *testing.T) {
	e := newEnv(t)
	author := keyOf(1)
	for _, status := range []string{hubwire.SetStatusPending, hubwire.SetStatusRejected} {
		id := e.setWith(personalProjection(), author, status)
		for _, opts := range []Options{{}, {Force: true}, {DryRun: true}} {
			res, err := e.svc.Moderate(e.ctx, console, ActionApprove, one(id, 1), opts)
			if opts.DryRun {
				if err != nil || len(res.Items) != 1 || res.Items[0].OK || res.Items[0].Code != CodePersonalResolver {
					t.Fatalf("the preview must name the refusal: %+v %v", res, err)
				}
				continue
			}
			e.expectPersonalResolver(err, id, 1, status)
		}
	}

	hidden := e.setWith(personalProjection(), author, hubwire.SetStatusHidden)
	_, err := e.svc.Moderate(e.ctx, console, ActionRestore, one(hidden, 1), Options{})
	e.expectPersonalResolver(err, hidden, 1, hubwire.SetStatusHidden)

	queued := e.setWith(personalProjection(), author, hiddenPending)
	if _, err := e.svc.Moderate(e.ctx, console, ActionRestore, one(queued, 1), Options{}); err != nil {
		t.Fatalf("a version restored to the queue is not published: %v", err)
	}
	_, err = e.svc.Moderate(e.ctx, console, ActionApprove, one(queued, 1), Options{})
	e.expectPersonalResolver(err, queued, 1, hubwire.SetStatusPending)

	for action, id := range map[string]string{
		ActionReject: e.setWith(personalProjection(), author, hubwire.SetStatusPending),
		ActionHide:   e.setWith(personalProjection(), author, hubwire.SetStatusActive),
	} {
		if _, err := e.svc.Moderate(e.ctx, console, action, one(id, 1), Options{Reason: "private address"}); err != nil {
			t.Errorf("%s keeps the address from the catalogue and must work: %v", action, err)
		}
	}
	if entries := e.audit(store.AuditQuery{Action: "set.approve"}); len(entries) != 0 {
		t.Fatalf("nothing was approved: %+v", entries)
	}
}

func (e *env) addVersion(id, author string, projection map[string]interface{}) int {
	e.t.Helper()
	e.n++
	v := &store.Version{SetID: id, FP: fmt.Sprintf("fp-%d", e.n), TargetsKey: fmt.Sprintf("tk-%d", e.n), Title: "added", Projection: projection,
		Status: hubwire.SetStatusPending, UploaderHMAC: author, CreatedAt: testNow, UpdatedAt: testNow}
	if err := e.st.AddVersion(e.ctx, v); err != nil {
		e.t.Fatal(err)
	}
	if err := e.st.Approve(e.ctx, id, v.Version, testNow); err != nil {
		e.t.Fatal(err)
	}
	return v.Version
}

func (e *env) listed(id string) int {
	e.t.Helper()
	versions, err := e.st.CatalogueVersions(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, v := range versions {
		if v.SetID == id {
			return v.Version
		}
	}
	return 0
}

func TestAnActivePersonalResolverIsNeverListed(t *testing.T) {
	e := newEnv(t)
	author := keyOf(1)
	id := e.set(author, hubwire.SetStatusActive)
	e.addVersion(id, author, personalProjection())
	e.addVersion(id, author, plainProjection())
	only := e.setWith(personalProjection(), author, hubwire.SetStatusActive)
	if e.listed(id) != 3 || e.listed(only) != 0 {
		t.Fatalf("listed %d and %d, want 3 and nothing", e.listed(id), e.listed(only))
	}

	res, err := e.svc.Moderate(e.ctx, console, ActionHide, one(id, 3), Options{Reason: "regressed", DryRun: true})
	if err != nil || res.Items[0].Listed != 3 || res.Items[0].ListedAfter != 1 {
		t.Fatalf("the preview must skip the version with a personal resolver: %+v %v", res, err)
	}
	if _, err := e.svc.Moderate(e.ctx, console, ActionHide, one(id, 3), Options{Reason: "regressed"}); err != nil {
		t.Fatal(err)
	}
	if e.listed(id) != 1 {
		t.Fatalf("hiding falls back to the older plain version, got %d", e.listed(id))
	}
	if hidden, err := e.st.AutoHide(e.ctx, id, 1, "reports", 3, testNow); err != nil || !hidden {
		t.Fatalf("%v %v", hidden, err)
	}
	if e.listed(id) != 0 {
		t.Fatalf("an automatic hide must not list the version with a personal resolver, got %d", e.listed(id))
	}
}

func TestAReinstatedOrUnbannedSetListsNoPersonalResolver(t *testing.T) {
	e := newEnv(t)
	author := keyOf(1)
	id := e.setWith(personalProjection(), author, hubwire.SetStatusActive)
	if _, err := e.svc.Withdraw(e.ctx, console, id, "author asked"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Reinstate(e.ctx, console, id); err != nil {
		t.Fatal(err)
	}
	entries := e.audit(store.AuditQuery{Action: "set.reinstate"})
	if len(entries) != 1 || fmt.Sprint(entries[0].After["listed"]) != "0" || e.listed(id) != 0 {
		t.Fatalf("the set comes back without the version: %+v, listed %d", entries, e.listed(id))
	}

	if _, err := e.svc.Key(e.ctx, console, author, ActionBan, KeyOptions{Reason: "spam", Create: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Key(e.ctx, console, author, ActionUnban, KeyOptions{}); err != nil {
		t.Fatal(err)
	}
	if e.listed(id) != 0 {
		t.Fatalf("lifting a ban must not list the version, got %d", e.listed(id))
	}
}

func TestAnEditThatDropsAPersonalResolverCanBeApproved(t *testing.T) {
	e := newEnv(t)
	id := e.setWith(personalProjection(), keyOf(1), hubwire.SetStatusPending)
	kept := store.VersionEdit{Title: "kept", Projection: personalProjection(), FP: "fp-kept", TargetsKey: "tk-kept"}
	_, err := e.svc.Edit(e.ctx, console, Ref{SetID: id, Version: 1}, kept, true)
	e.expectPersonalResolver(err, id, 1, hubwire.SetStatusPending)
	if v := e.version(id, 1); v.Title == "kept" {
		t.Fatalf("a refused edit-and-approve leaves the version alone: %+v", v)
	}

	dropped := store.VersionEdit{Title: "dropped", Projection: plainProjection(), FP: "fp-dropped", TargetsKey: "tk-dropped"}
	res, err := e.svc.Edit(e.ctx, console, Ref{SetID: id, Version: 1}, dropped, true)
	if err != nil || res.Code != "set.edited_approved" {
		t.Fatalf("an edit that drops the address is approved: %+v %v", res, err)
	}
	if v := e.version(id, 1); v.Status != hubwire.SetStatusActive {
		t.Fatalf("%+v", v)
	}
}

func TestABatchSkipsAPersonalResolver(t *testing.T) {
	e := newEnv(t)
	personal := e.setWith(personalProjection(), keyOf(1), hubwire.SetStatusPending)
	plain := e.set(keyOf(2), hubwire.SetStatusPending)
	refs := append(one(personal, 1), one(plain, 1)...)
	_, err := e.svc.Moderate(e.ctx, console, ActionApprove, refs, Options{})
	codeOf(t, err, CodeBatchInvalid)
	res, err := e.svc.Moderate(e.ctx, console, ActionApprove, refs, Options{Partial: true})
	if err != nil || fmt.Sprint(res.Params["applied"], res.Params["skipped"]) != "1 1" || res.Items[0].Code != CodePersonalResolver {
		t.Fatalf("the other version of the batch is approved: %+v %v", res, err)
	}
	if e.version(personal, 1).Status != hubwire.SetStatusPending || e.version(plain, 1).Status != hubwire.SetStatusActive {
		t.Fatal("only the version without a personal resolver becomes active")
	}
}
