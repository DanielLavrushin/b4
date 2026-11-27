package moderation

import (
	"errors"
	"testing"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

type member struct {
	setID   string
	version int
	brought bool
}

func (e *env) group(author string, members ...member) string {
	e.t.Helper()
	id, err := hubdata.NewSetID()
	if err != nil {
		e.t.Fatal(err)
	}
	v := &store.GroupVersion{Title: "youtube", Status: hubwire.SetStatusPending, UploaderHMAC: author, CreatedAt: testNow, UpdatedAt: testNow}
	for _, m := range members {
		v.Members = append(v.Members, store.GroupMember{SetID: m.setID, SetVersion: m.version, Brought: m.brought})
	}
	err = e.st.Update(e.ctx, func(t *store.Tx) error {
		return t.CreateGroup(e.ctx, store.Group{ID: id, AuthorHMAC: author, CreatedAt: testNow, UpdatedAt: testNow}, v)
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) groupStatus(id string, version int) string {
	e.t.Helper()
	v, err := e.st.GetGroupVersion(e.ctx, id, version)
	if err != nil {
		e.t.Fatal(err)
	}
	return v.Status
}

func (e *env) statuses(refs ...member) []string {
	e.t.Helper()
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, e.version(r.setID, r.version).Status)
	}
	return out
}

func sameStrings(a []string, b ...string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestApprovingAGroupApprovesWhatItBroughtAndLeavesLinkedSetsAlone(t *testing.T) {
	e := newEnv(t)
	author, other := keyOf(1), keyOf(2)
	fresh := member{e.set(author, hubwire.SetStatusPending), 1, true}
	own := member{e.set(author, hubwire.SetStatusActive, hubwire.SetStatusPending), 2, true}
	linked := member{e.set(other, hubwire.SetStatusActive), 1, false}
	queued := member{e.set(other, hubwire.SetStatusPending), 1, false}
	single := member{e.set(author, hubwire.SetStatusPending), 1, false}
	g := e.group(author, fresh, own, linked, queued)
	all := []member{fresh, own, linked, queued, single}

	preview, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: g, Version: 1, ExpectStatus: hubwire.SetStatusPending}, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Code != "moderation.preview" || e.groupStatus(g, 1) != hubwire.SetStatusPending || !sameStrings(e.statuses(all...), "pending", "pending", "active", "pending", "pending") {
		t.Fatalf("a preview changes nothing: %+v", preview)
	}
	if preview.Group.From != hubwire.SetStatusPending || preview.Group.To != hubwire.SetStatusActive || preview.Group.ListedAfter != 0 || len(preview.Members) != 4 {
		t.Fatalf("the group stays out while a linked member is pending: %+v", preview.Group)
	}
	if m := preview.Members[0]; !m.Brought || m.To != hubwire.SetStatusActive || m.ListedAfter != 1 {
		t.Fatalf("a brought member is approved: %+v", m)
	}
	if m := preview.Members[1]; m.To != hubwire.SetStatusActive || m.Listed != 1 || m.ListedAfter != 2 {
		t.Fatalf("a brought version of a listed set replaces it: %+v", m)
	}
	if m := preview.Members[2]; m.Brought || m.To != "" || m.From != hubwire.SetStatusActive || m.Listed != 1 {
		t.Fatalf("a linked member is untouched: %+v", m)
	}
	if len(e.audit(store.AuditQuery{})) != 0 {
		t.Fatal("a preview writes no audit row")
	}

	res, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: g, Version: 1}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != "group.approved" || res.BatchID == "" || res.Params["members"] != 2 {
		t.Fatalf("result: %+v", res)
	}
	if e.groupStatus(g, 1) != hubwire.SetStatusActive || !sameStrings(e.statuses(all...), "active", "active", "active", "pending", "pending") {
		t.Fatalf("approve covers the group and the versions it brought only: %v", e.statuses(all...))
	}
	entries := e.audit(store.AuditQuery{BatchID: res.BatchID})
	actions := map[string]int{}
	for _, entry := range entries {
		actions[entry.Action+" "+entry.TargetID]++
		if entry.Action == "group.approve" && (entry.TargetKind != store.TargetGroup || entry.Version != 1 || entry.After["members"] != float64(2)) {
			t.Fatalf("the group row: %+v", entry)
		}
	}
	if len(entries) != 3 || actions["group.approve "+g] != 1 || actions["set.approve "+fresh.setID] != 1 || actions["set.approve "+own.setID] != 1 {
		t.Fatalf("one batch holds the group row and a row per approved member: %+v", entries)
	}
	if len(e.builds.triggers) != 1 {
		t.Fatalf("one build request, got %v", e.builds.triggers)
	}
}

func TestRejectingAGroupNeedsAReasonAndRejectsOnlyItsPendingBroughtVersions(t *testing.T) {
	e := newEnv(t)
	author, other := keyOf(1), keyOf(2)
	fresh := member{e.set(author, hubwire.SetStatusPending), 1, true}
	early := member{e.set(author, hubwire.SetStatusActive), 1, true}
	linked := member{e.set(other, hubwire.SetStatusPending), 1, false}
	g := e.group(author, fresh, early, linked)
	if _, err := e.st.CreateReasonPreset(e.ctx, store.ReasonPreset{Scope: store.ScopeReject, Label: "spam", Text: "spam"}, testNow); err != nil {
		t.Fatal(err)
	}

	_, err := e.svc.ModerateGroup(e.ctx, console, ActionReject, GroupRef{GroupID: g, Version: 1}, Options{Reason: "  "})
	if CodeOf(err) != CodeReasonRequired || e.groupStatus(g, 1) != hubwire.SetStatusPending || !sameStrings(e.statuses(fresh, early, linked), "pending", "active", "pending") {
		t.Fatalf("a rejection without a reason changes nothing: %v", err)
	}

	if _, err := e.svc.ModerateGroup(e.ctx, console, ActionReject, GroupRef{GroupID: g, Version: 1}, Options{Reason: "spam"}); err != nil {
		t.Fatal(err)
	}
	if e.groupStatus(g, 1) != hubwire.SetStatusRejected || !sameStrings(e.statuses(fresh, early, linked), "rejected", "active", "pending") {
		t.Fatalf("reject covers the pending versions the group brought: %v", e.statuses(fresh, early, linked))
	}
	if v := e.version(fresh.setID, 1); v.StatusReason != "spam" {
		t.Fatalf("the member keeps the reason: %q", v.StatusReason)
	}
	presets, err := e.st.ReasonPresets(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 1 || presets[0].Uses != 1 {
		t.Fatalf("one decision uses the preset once: %+v", presets)
	}
}

func TestHideAndRestoreTouchOnlyTheGroupVersion(t *testing.T) {
	e := newEnv(t)
	author, other := keyOf(1), keyOf(2)
	fresh := member{e.set(author, hubwire.SetStatusPending), 1, true}
	linked := member{e.set(other, hubwire.SetStatusActive), 1, false}
	g := e.group(author, fresh, linked)

	res, err := e.svc.ModerateGroup(e.ctx, console, ActionHide, GroupRef{GroupID: g, Version: 1}, Options{Reason: "later"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BatchID != "" || e.groupStatus(g, 1) != hubwire.SetStatusHidden || !sameStrings(e.statuses(fresh, linked), "pending", "active") {
		t.Fatalf("hiding a pending group leaves its members: %+v %v", res, e.statuses(fresh, linked))
	}
	if _, err := e.svc.ModerateGroup(e.ctx, console, ActionRestore, GroupRef{GroupID: g, Version: 1}, Options{}); err != nil {
		t.Fatal(err)
	}
	if e.groupStatus(g, 1) != hubwire.SetStatusPending || !sameStrings(e.statuses(fresh, linked), "pending", "active") {
		t.Fatal("restore returns the group to pending")
	}

	approved, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: g, Version: 1}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Group.Listed != 0 || approved.Group.ListedAfter != 1 {
		t.Fatalf("the group is listed once every member is: %+v", approved.Group)
	}
	hidden, err := e.svc.ModerateGroup(e.ctx, console, ActionHide, GroupRef{GroupID: g, Version: 1}, Options{Reason: "broken"})
	if err != nil {
		t.Fatal(err)
	}
	if hidden.Group.Listed != 1 || hidden.Group.ListedAfter != 0 || !sameStrings(e.statuses(fresh, linked), "active", "active") {
		t.Fatalf("hiding a listed group leaves its members listed: %+v %v", hidden.Group, e.statuses(fresh, linked))
	}
	if _, err := e.svc.ModerateGroup(e.ctx, console, ActionRestore, GroupRef{GroupID: g, Version: 1}, Options{}); err != nil {
		t.Fatal(err)
	}
	if e.groupStatus(g, 1) != hubwire.SetStatusActive || !sameStrings(e.statuses(fresh, linked), "active", "active") {
		t.Fatal("restore returns the group to active")
	}
	for _, entry := range e.audit(store.AuditQuery{TargetKind: store.TargetGroup}) {
		if entry.Action == "group.hide" || entry.Action == "group.restore" {
			if entry.BatchID != "" {
				t.Fatalf("hide and restore write one row: %+v", entry)
			}
		}
	}
	if n := len(e.audit(store.AuditQuery{TargetKind: store.TargetSet})); n != 1 {
		t.Fatalf("only the approve touched a member, got %d set rows", n)
	}
}

func TestAStaleOrRefusedGroupChangesNothing(t *testing.T) {
	e := newEnv(t)
	author := keyOf(1)
	first := member{e.set(author, hubwire.SetStatusPending), 1, true}
	second := member{e.set(author, hubwire.SetStatusPending), 1, true}
	g := e.group(author, first, second)
	unchanged := func(label string) {
		t.Helper()
		if e.groupStatus(g, 1) != hubwire.SetStatusPending || !sameStrings(e.statuses(first, second), "pending", "pending") {
			t.Fatalf("%s: nothing may change: %v", label, e.statuses(first, second))
		}
		if entries := e.audit(store.AuditQuery{Action: "set.approve"}); len(entries) != 0 {
			t.Fatalf("%s: no member row: %+v", label, entries)
		}
	}

	_, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: g, Version: 1, ExpectStatus: hubwire.SetStatusActive}, Options{})
	if CodeOf(err) != CodeStale {
		t.Fatalf("stale: %v", err)
	}
	unchanged("stale")
	_, err = e.svc.ModerateGroup(e.ctx, console, ActionRestore, GroupRef{GroupID: g, Version: 1}, Options{})
	if CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("invalid transition: %v", err)
	}
	unchanged("invalid transition")
	_, err = e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: g, Version: 2}, Options{})
	if CodeOf(err) != CodeNotFound {
		t.Fatalf("missing version: %v", err)
	}

	if err := e.st.Update(e.ctx, func(t *store.Tx) error { return t.WithdrawSet(e.ctx, second.setID, "author asked", testNow) }); err != nil {
		t.Fatal(err)
	}
	preview, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: g, Version: 1}, Options{DryRun: true})
	if err != nil || !preview.Group.OK || len(preview.Members) != 2 || !preview.Members[0].OK || preview.Members[1].Code != CodeSetWithdrawn {
		t.Fatalf("a preview names the refused member: %v %+v", err, preview)
	}
	unchanged("preview")
	_, err = e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: g, Version: 1}, Options{})
	var refusal *Error
	if CodeOf(err) != CodeSetWithdrawn || !errors.As(err, &refusal) || len(refusal.Items) != 2 || !refusal.Items[0].OK || refusal.Items[1].OK || refusal.Items[1].SetID != second.setID {
		t.Fatalf("a refused member refuses the group and names itself: %v %+v", err, refusal)
	}
	unchanged("refused member")
	if entries := e.audit(store.AuditQuery{TargetKind: store.TargetGroup}); len(entries) != 0 {
		t.Fatalf("no group row either: %+v", entries)
	}

	if _, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: g, Version: 1}, Options{Force: true}); err != nil {
		t.Fatal(err)
	}
	if e.groupStatus(g, 1) != hubwire.SetStatusActive || !sameStrings(e.statuses(first, second), "active", "active") {
		t.Fatal("force approves the withdrawn member")
	}
}

func TestABannedAuthorsGroupNeedsForce(t *testing.T) {
	e := newEnv(t)
	author := keyOf(1)
	fresh := member{e.set(author, hubwire.SetStatusPending), 1, true}
	g := e.group(author, fresh)
	linkedOnly := e.group(author, member{e.set(keyOf(2), hubwire.SetStatusActive), 1, false})
	if _, _, err := e.st.TouchKey(e.ctx, author, testNow); err != nil {
		t.Fatal(err)
	}
	if err := e.st.BanKey(e.ctx, author, "spam", testNow); err != nil {
		t.Fatal(err)
	}

	preview, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: g, Version: 1}, Options{DryRun: true})
	if err != nil || preview.Group.OK || preview.Group.Code != CodeAuthorBanned || len(preview.Members) != 1 || preview.Members[0].Code != CodeAuthorBanned {
		t.Fatalf("a preview carries the refusal so the moderator can force it: %v %+v", err, preview)
	}
	_, err = e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: g, Version: 1}, Options{})
	if CodeOf(err) != CodeAuthorBanned || e.groupStatus(g, 1) != hubwire.SetStatusPending || e.version(fresh.setID, 1).Status != hubwire.SetStatusPending {
		t.Fatalf("a banned author's group waits for force: %v", err)
	}
	if _, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: linkedOnly, Version: 1}, Options{}); CodeOf(err) != CodeAuthorBanned {
		t.Fatalf("so does one that only links other authors' sets: %v", err)
	}
	res, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: g, Version: 1}, Options{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if e.groupStatus(g, 1) != hubwire.SetStatusActive || e.version(fresh.setID, 1).Status != hubwire.SetStatusActive {
		t.Fatal("force approves the group and its member")
	}
	if res.Group.Withheld != store.WithheldAuthorBanned || res.Group.ListedAfter != 0 {
		t.Fatalf("the group stays withheld while its author is banned: %+v", res.Group)
	}
	entries := e.audit(store.AuditQuery{TargetKind: store.TargetGroup})
	if len(entries) != 1 || entries[0].After["forced"] != true {
		t.Fatalf("the group row records the force: %+v", entries)
	}
}

func (e *env) groupVersion(id, author string, members ...member) int {
	e.t.Helper()
	v := &store.GroupVersion{GroupID: id, Title: "youtube", Status: hubwire.SetStatusPending, UploaderHMAC: author, CreatedAt: testNow, UpdatedAt: testNow}
	for _, m := range members {
		v.Members = append(v.Members, store.GroupMember{SetID: m.setID, SetVersion: m.version, Brought: m.brought})
	}
	if err := e.st.Update(e.ctx, func(t *store.Tx) error { return t.AddGroupVersion(e.ctx, v) }); err != nil {
		e.t.Fatal(err)
	}
	return v.Version
}

func (e *env) pendingBadge() int {
	e.t.Helper()
	b, err := e.st.Badges(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return b.Pending
}

type republished struct {
	group                string
	video, music, music2 member
}

func (e *env) republished(author string) republished {
	e.t.Helper()
	video := e.set(author, hubwire.SetStatusPending)
	music := e.set(author, hubwire.SetStatusPending, hubwire.SetStatusPending)
	r := republished{video: member{video, 1, true}, music: member{music, 1, true}, music2: member{music, 2, true}}
	r.group = e.group(author, r.video, r.music)
	if v := e.groupVersion(r.group, author, member{video, 1, false}, r.music2); v != 2 {
		e.t.Fatalf("the republish is version 2, got %d", v)
	}
	if n := e.pendingBadge(); n != 2 {
		e.t.Fatalf("two pending group versions fold every member, got %d", n)
	}
	return r
}

func TestApprovingARepublishedGroupApprovesTheMemberAnEarlierVersionBrought(t *testing.T) {
	e := newEnv(t)
	author := keyOf(1)
	r := e.republished(author)

	preview, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: r.group, Version: 2}, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if m := preview.Members[0]; m.Brought || m.To != hubwire.SetStatusActive || m.ListedAfter != 1 || preview.Group.ListedAfter != 2 {
		t.Fatalf("the linked member the first version brought is approved with the group: %+v %+v", m, preview.Group)
	}
	res, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: r.group, Version: 2}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Params["members"] != 2 || res.Group.ListedAfter != 2 || e.groupStatus(r.group, 2) != hubwire.SetStatusActive {
		t.Fatalf("approve lists the group: %+v", res)
	}
	if !sameStrings(e.statuses(r.video, r.music, r.music2), "active", "pending", "active") {
		t.Fatalf("approve covers what v2 brought and the version v1 brought that v2 links: %v", e.statuses(r.video, r.music, r.music2))
	}
	if n := len(e.audit(store.AuditQuery{BatchID: res.BatchID})); n != 3 {
		t.Fatalf("one batch for the group and both members, got %d rows", n)
	}
	if n := e.pendingBadge(); n != 1 {
		t.Fatalf("v1 stays queued with the member only it holds, got %d", n)
	}

	foreign := member{e.set(keyOf(2), hubwire.SetStatusPending), 1, true}
	e.group(keyOf(2), foreign)
	mine := e.group(author, member{foreign.setID, 1, false})
	if _, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: mine, Version: 1}, Options{}); err != nil {
		t.Fatal(err)
	}
	if e.version(foreign.setID, 1).Status != hubwire.SetStatusPending || e.pendingBadge() != 2 {
		t.Fatal("a pending set another group brought is not approved by a group that links it")
	}
}

func TestRejectingAnOlderGroupVersionKeepsWhatANewerVersionStillIncludes(t *testing.T) {
	e := newEnv(t)
	author := keyOf(1)
	r := e.republished(author)

	preview, err := e.svc.ModerateGroup(e.ctx, console, ActionReject, GroupRef{GroupID: r.group, Version: 1}, Options{Reason: "superseded", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if m := preview.Members[0]; !m.OK || m.To != "" || m.Code != CodeKeptNewerGroupVersion || m.Params["group_version"] != 2 || m.From != hubwire.SetStatusPending {
		t.Fatalf("the preview shows the member v2 links as kept: %+v", m)
	}
	if m := preview.Members[1]; m.To != hubwire.SetStatusRejected {
		t.Fatalf("the member only v1 holds is rejected: %+v", m)
	}
	res, err := e.svc.ModerateGroup(e.ctx, console, ActionReject, GroupRef{GroupID: r.group, Version: 1}, Options{Reason: "superseded"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Members[0].Code != CodeKeptNewerGroupVersion || res.Params["members"] != 1 {
		t.Fatalf("the result shows the kept member: %+v", res)
	}
	if !sameStrings(e.statuses(r.video, r.music, r.music2), "pending", "rejected", "pending") || e.groupStatus(r.group, 2) != hubwire.SetStatusPending {
		t.Fatalf("v2 keeps its members: %v", e.statuses(r.video, r.music, r.music2))
	}
	folded, err := e.st.FoldedVersions(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !folded[r.video.setID][1] || !folded[r.music.setID][2] || e.pendingBadge() != 1 {
		t.Fatalf("v2 holds both pending members and is one queue item: %v badge %d", folded, e.pendingBadge())
	}

	if _, err := e.svc.ModerateGroup(e.ctx, console, ActionHide, GroupRef{GroupID: r.group, Version: 2}, Options{Reason: "later"}); err != nil {
		t.Fatal(err)
	}
	if folded, _ := e.st.FoldedVersions(e.ctx); len(folded) != 0 || e.pendingBadge() != 2 {
		t.Fatalf("with no pending group version the members are queued on their own: %v badge %d", folded, e.pendingBadge())
	}
	if _, err := e.svc.ModerateGroup(e.ctx, console, ActionRestore, GroupRef{GroupID: r.group, Version: 2}, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: r.group, Version: 2}, Options{}); err != nil {
		t.Fatal(err)
	}
	if !sameStrings(e.statuses(r.video, r.music2), "active", "active") || e.pendingBadge() != 0 {
		t.Fatalf("v2 is approved with the kept member: %v", e.statuses(r.video, r.music2))
	}
}

func TestAGroupConfirmIsStaleWhenAMemberChangedAfterThePreview(t *testing.T) {
	for _, change := range []string{ActionReject, ActionHide} {
		t.Run(change, func(t *testing.T) {
			e := newEnv(t)
			r := e.republished(keyOf(1))
			expect := []Ref{{SetID: r.video.setID, Version: 1, ExpectStatus: hubwire.SetStatusPending}, {SetID: r.music2.setID, Version: 2, ExpectStatus: hubwire.SetStatusPending}}
			if _, err := e.svc.Moderate(e.ctx, console, change, []Ref{{SetID: r.video.setID, Version: 1}}, Options{Reason: "another tab"}); err != nil {
				t.Fatal(err)
			}
			before := e.statuses(r.video, r.music2)

			_, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: r.group, Version: 2, ExpectStatus: hubwire.SetStatusPending, Members: expect}, Options{})
			var refusal *Error
			if CodeOf(err) != CodeStale || !errors.As(err, &refusal) || refusal.Items[0].OK || refusal.Items[0].Code != CodeStale || !refusal.Items[1].OK {
				t.Fatalf("a linked member the group owns changed after the preview: %v %+v", err, refusal)
			}
			if e.groupStatus(r.group, 2) != hubwire.SetStatusPending || !sameStrings(e.statuses(r.video, r.music2), before...) {
				t.Fatalf("a stale confirm changes nothing: %v", e.statuses(r.video, r.music2))
			}

			expect[0].ExpectStatus = before[0]
			if _, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: r.group, Version: 2, ExpectStatus: hubwire.SetStatusPending, Members: expect}, Options{}); err != nil {
				t.Fatalf("a confirm with the statuses of a fresh preview goes through: %v", err)
			}
		})
	}

	e := newEnv(t)
	author := keyOf(1)
	first := member{e.set(author, hubwire.SetStatusPending), 1, true}
	g := e.group(author, first)
	if _, err := e.svc.Moderate(e.ctx, console, ActionHide, []Ref{{SetID: first.setID, Version: 1}}, Options{Reason: "another tab"}); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.ModerateGroup(e.ctx, console, ActionApprove, GroupRef{GroupID: g, Version: 1, Members: []Ref{{SetID: first.setID, Version: 1, ExpectStatus: hubwire.SetStatusPending}}}, Options{})
	if CodeOf(err) != CodeStale || e.groupStatus(g, 1) != hubwire.SetStatusPending {
		t.Fatalf("a brought member hidden after the preview makes the confirm stale: %v", err)
	}
}

func (e *env) rejectPreview(r republished) []Ref {
	e.t.Helper()
	preview, err := e.svc.ModerateGroup(e.ctx, console, ActionReject, GroupRef{GroupID: r.group, Version: 1}, Options{Reason: "superseded", DryRun: true})
	if err != nil {
		e.t.Fatal(err)
	}
	expect := make([]Ref, 0, len(preview.Members))
	for _, m := range preview.Members {
		kept := m.Code == CodeKeptNewerGroupVersion
		expect = append(expect, Ref{SetID: m.SetID, Version: m.Version, ExpectStatus: m.From, ExpectKept: &kept})
	}
	return expect
}

func (e *env) rejectFirst(r republished, expect []Ref) (GroupResult, error) {
	return e.svc.ModerateGroup(e.ctx, console, ActionReject, GroupRef{GroupID: r.group, Version: 1, ExpectStatus: hubwire.SetStatusPending, Members: expect}, Options{Reason: "superseded"})
}

func (e *env) staleUnchanged(r republished, err error) {
	e.t.Helper()
	var refusal *Error
	if CodeOf(err) != CodeStale || !errors.As(err, &refusal) || refusal.Items[0].OK || refusal.Items[0].Code != CodeStale || !refusal.Items[1].OK {
		e.t.Fatalf("the member the preview judged differently makes the confirm stale: %v %+v", err, refusal)
	}
	if e.groupStatus(r.group, 1) != hubwire.SetStatusPending || !sameStrings(e.statuses(r.video, r.music), "pending", "pending") {
		e.t.Fatalf("a stale confirm changes nothing: %v", e.statuses(r.video, r.music))
	}
}

func TestAGroupRejectIsStaleWhenWhatThePreviewKeptChanged(t *testing.T) {
	t.Run("nothing changed after the preview", func(t *testing.T) {
		e := newEnv(t)
		r := e.republished(keyOf(1))
		res, err := e.rejectFirst(r, e.rejectPreview(r))
		if err != nil || res.Members[0].Code != CodeKeptNewerGroupVersion || !sameStrings(e.statuses(r.video, r.music), "pending", "rejected") {
			t.Fatalf("a confirm that matches its preview keeps Video/1: %v %+v %v", err, res, e.statuses(r.video, r.music))
		}
	})

	t.Run("the newer version is rejected after the preview", func(t *testing.T) {
		e := newEnv(t)
		r := e.republished(keyOf(1))
		expect := e.rejectPreview(r)
		if !*expect[0].ExpectKept || *expect[1].ExpectKept {
			t.Fatalf("the preview keeps Video/1 and rejects Music/1: %+v", expect)
		}
		if _, err := e.svc.ModerateGroup(e.ctx, console, ActionReject, GroupRef{GroupID: r.group, Version: 2}, Options{Reason: "another tab"}); err != nil {
			t.Fatal(err)
		}

		_, err := e.rejectFirst(r, expect)
		e.staleUnchanged(r, err)

		for i := range expect {
			expect[i].ExpectKept = nil
		}
		res, err := e.rejectFirst(r, expect)
		if err != nil || res.Params["members"] != 2 || !sameStrings(e.statuses(r.video, r.music), "rejected", "rejected") {
			t.Fatalf("a confirm without kept expectations rejects what nothing keeps now: %v %+v %v", err, res, e.statuses(r.video, r.music))
		}
	})

	t.Run("the newer version is restored after the preview", func(t *testing.T) {
		e := newEnv(t)
		r := e.republished(keyOf(1))
		if _, err := e.svc.ModerateGroup(e.ctx, console, ActionHide, GroupRef{GroupID: r.group, Version: 2}, Options{Reason: "later"}); err != nil {
			t.Fatal(err)
		}
		expect := e.rejectPreview(r)
		if *expect[0].ExpectKept || *expect[1].ExpectKept {
			t.Fatalf("with v2 hidden the preview rejects both: %+v", expect)
		}
		if _, err := e.svc.ModerateGroup(e.ctx, console, ActionRestore, GroupRef{GroupID: r.group, Version: 2}, Options{}); err != nil {
			t.Fatal(err)
		}

		_, err := e.rejectFirst(r, expect)
		e.staleUnchanged(r, err)

		res, err := e.rejectFirst(r, nil)
		if err != nil || res.Members[0].Code != CodeKeptNewerGroupVersion || res.Params["members"] != 1 || !sameStrings(e.statuses(r.video, r.music), "pending", "rejected") {
			t.Fatalf("a confirm without expectations keeps what v2 includes: %v %+v %v", err, res, e.statuses(r.video, r.music))
		}
	})
}
