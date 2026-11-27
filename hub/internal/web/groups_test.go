package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func (f *fixture) group(author *hubwire.Identity, title, address string, sets ...config.SetConfig) (string, []hubwire.GroupMemberResult) {
	f.t.Helper()
	body := hubwire.GroupBody{Title: title}
	for i := range sets {
		body.Members = append(body.Members, hubwire.GroupMember{Envelope: *testkit.BuildEnvelope(f.t, &sets[i])})
	}
	resp := f.ingest.Handle(context.Background(), testkit.Sign(f.t, author, hubwire.RecordGroup, body, f.clock), parseIP(address))
	if resp.Status != http.StatusAccepted {
		f.t.Fatalf("group %s: %d %v", title, resp.Status, resp.Body)
	}
	return resp.Body["group_id"].(string), resp.Body["members"].([]hubwire.GroupMemberResult)
}

func (f *fixture) queueView() QueueView {
	f.t.Helper()
	var q QueueView
	f.admin(http.MethodGet, PathAPI+"/queue", nil).decode(f.t, &q)
	return q
}

func (f *fixture) badges() BadgesView {
	f.t.Helper()
	var b BadgesView
	f.admin(http.MethodGet, PathAPI+"/counts", nil).decode(f.t, &b)
	return b
}

func (f *fixture) groupAction(id, action string, req GroupActionRequest) response {
	return f.admin(http.MethodPost, PathAPI+"/groups/"+id+"/1/"+action, req)
}

type mediaGroup struct {
	group, linked, single string
	members               []hubwire.GroupMemberResult
}

func (f *fixture) mediaGroup() mediaGroup {
	f.t.Helper()
	stranger, author := testkit.Identity(f.t), testkit.Identity(f.t)
	linked := testkit.SampleSet("Linked", "linked.example")
	linkedID, _ := f.shareAs(stranger, linked, otherAddress)
	f.approve(linkedID)
	single, _ := f.shareAs(author, testkit.SampleSet("Single", "single.example"), authorAddress)
	id, members := f.group(author, "media", authorAddress, linked, testkit.SampleSet("Video", "video.example"), testkit.SampleSet("Music", "music.example"))
	if !members[0].Linked || members[0].SetID != linkedID || members[1].Linked || members[2].Linked {
		f.t.Fatalf("the first member links the stranger's set, the others are new: %+v", members)
	}
	return mediaGroup{group: id, linked: linkedID, single: single, members: members}
}

func queuedSets(q QueueView) map[string]bool {
	out := map[string]bool{}
	for _, e := range q.Items {
		out[e.SetID] = true
	}
	return out
}

func TestTheQueueShowsAGroupOnceAndCountsItOnce(t *testing.T) {
	f := newFixture(t, password)
	m := f.mediaGroup()
	video, music := m.members[1].SetID, m.members[2].SetID

	q := f.queueView()
	if sets := queuedSets(q); len(q.Items) != 1 || !sets[m.single] {
		t.Fatalf("the versions a pending group brought are not queued on their own: %+v", sets)
	}
	if len(q.Groups) != 1 || q.Groups[0].GroupID != m.group || q.Groups[0].Status != "pending" || len(q.Groups[0].Members) != 3 {
		t.Fatalf("one queue item per pending group version: %+v", q.Groups)
	}
	members := q.Groups[0].Members
	if members[0].Brought || members[0].Row == nil || members[0].Entry != nil || members[0].Row.SetID != m.linked || members[0].ListedVersion != 1 {
		t.Fatalf("a linked member is a brief row with its listed version: %+v", members[0])
	}
	if !members[1].Brought || members[1].Entry == nil || members[1].Row != nil || members[1].Entry.SetID != video || members[1].Entry.Lineage == nil || members[2].Entry.SetID != music {
		t.Fatalf("a brought member is a full entry: %+v", members[1])
	}
	if b := f.badges(); b.Pending != 2 || b.OldestPending == nil {
		t.Fatalf("the badge counts the group once next to the single share: %+v", b)
	}
	var overview OverviewView
	f.admin(http.MethodGet, PathAPI+"/overview", nil).decode(t, &overview)
	if overview.Counts.Pending != 2 {
		t.Fatalf("the overview counts the queue the same way: %d", overview.Counts.Pending)
	}

	f.expectStatus(f.groupAction(m.group, ActionHide, GroupActionRequest{Reason: "later"}), http.StatusOK)
	q = f.queueView()
	if sets := queuedSets(q); len(q.Groups) != 0 || len(q.Items) != 3 || !sets[video] || !sets[music] {
		t.Fatalf("a hidden group gives its pending members back to the queue: %+v", q)
	}
	if b := f.badges(); b.Pending != 3 {
		t.Fatalf("and to the badge: %+v", b)
	}
	f.expectStatus(f.groupAction(m.group, ActionRestore, GroupActionRequest{}), http.StatusOK)
	if q := f.queueView(); len(q.Groups) != 1 || len(q.Items) != 1 {
		t.Fatalf("restore folds them again: %+v", q)
	}
}

func (f *fixture) expectStatus(resp response, status int) response {
	f.t.Helper()
	if resp.status != status {
		f.t.Fatalf("expected %d, got %d %s", status, resp.status, resp.body)
	}
	return resp
}

func TestGroupActionsOverTheAPI(t *testing.T) {
	f := newFixture(t, password)
	m := f.mediaGroup()
	video := m.members[1].SetID

	var preview GroupModerationView
	f.expectStatus(f.groupAction(m.group, ActionApprove, GroupActionRequest{DryRun: true}), http.StatusOK).decode(t, &preview)
	if preview.Code != "moderation.preview" || preview.Group.To != "active" || preview.Group.ListedAfter != 1 || len(preview.Members) != 3 {
		t.Fatalf("preview: %+v", preview)
	}
	if preview.Members[0].To != "" || preview.Members[1].To != "active" || !preview.Members[1].Brought || preview.Members[2].Position != 2 {
		t.Fatalf("preview members: %+v", preview.Members)
	}
	var detail GroupDetailView
	f.admin(http.MethodGet, PathAPI+"/groups/"+m.group, nil).decode(t, &detail)
	if len(detail.Versions) != 1 || detail.Versions[0].Status != "pending" || detail.PublishedVersion != 0 {
		t.Fatalf("a preview changes nothing: %+v", detail)
	}

	f.expectError(f.groupAction(m.group, ActionReject, GroupActionRequest{}), http.StatusBadRequest, "reason_required")
	f.expectError(f.groupAction(m.group, ActionApprove, GroupActionRequest{ExpectStatus: "active"}), http.StatusConflict, "stale")
	f.expectOK(f.admin(http.MethodPost, PathAPI+"/sets/"+video+"/withdraw", map[string]string{"reason": "author asked"}))
	f.expectStatus(f.groupAction(m.group, ActionApprove, GroupActionRequest{DryRun: true}), http.StatusOK).decode(t, &preview)
	if !preview.Group.OK || preview.Members[1].OK || preview.Members[1].Code != "set_withdrawn" {
		t.Fatalf("a preview answers with the refused member: %+v", preview)
	}
	refused := f.expectError(f.groupAction(m.group, ActionApprove, GroupActionRequest{}), http.StatusConflict, "set_withdrawn")
	if len(refused.Items) != 3 || refused.Items[1].OK || refused.Items[1].SetID != video {
		t.Fatalf("the refusal names the member: %+v", refused.Items)
	}
	f.expectOK(f.admin(http.MethodPost, PathAPI+"/sets/"+video+"/reinstate", map[string]string{}))

	var done GroupModerationView
	f.expectStatus(f.groupAction(m.group, ActionApprove, GroupActionRequest{ExpectStatus: "pending"}), http.StatusOK).decode(t, &done)
	if done.Code != "group.approved" || done.BatchID == "" || done.Params["members"] != float64(2) {
		t.Fatalf("approve: %+v", done)
	}
	f.admin(http.MethodGet, PathAPI+"/groups/"+m.group, nil).decode(t, &detail)
	if detail.Versions[0].Status != "active" || detail.PublishedVersion != 1 || detail.Versions[0].Members[1].Entry.Status != "active" {
		t.Fatalf("the approved group is published with its members: %+v", detail)
	}

	var audit AuditPageView
	f.admin(http.MethodGet, PathAPI+"/audit?batch="+done.BatchID, nil).decode(t, &audit)
	if len(audit.Items) != 3 {
		t.Fatalf("one batch for the group and its two brought members: %+v", audit.Items)
	}
	for _, e := range audit.Items {
		if e.TargetKind == "group" && (e.TargetLabel != "media" || e.Action != "group.approve") {
			t.Fatalf("the group row is labelled with its title: %+v", e)
		}
	}

	f.expectError(f.admin(http.MethodGet, PathAPI+"/groups/"+unknownSetID, nil), http.StatusNotFound, codeNotFound)
	f.expectError(f.groupAction(m.group, "delete", GroupActionRequest{}), http.StatusNotFound, codeNotFound)
	f.expectError(f.admin(http.MethodPost, PathAPI+"/groups/"+m.group+"/0/approve", GroupActionRequest{}), http.StatusNotFound, codeNotFound)
}

func TestAGroupWithADeletedMemberStillShows(t *testing.T) {
	f := newFixture(t, password)
	m := f.mediaGroup()
	music := m.members[2].SetID
	f.expectOK(f.admin(http.MethodPost, PathAPI+"/sets/"+music+"/delete", map[string]string{"confirm": music}))

	q := f.queueView()
	if len(q.Groups) != 1 || !q.Groups[0].Members[2].Missing || q.Groups[0].Members[2].Entry != nil || q.Groups[0].Members[1].Entry == nil {
		t.Fatalf("a deleted member is marked missing: %+v", q.Groups)
	}
	var detail GroupDetailView
	f.expectStatus(f.admin(http.MethodGet, PathAPI+"/groups/"+m.group, nil), http.StatusOK).decode(t, &detail)
	if !detail.Versions[0].Members[2].Missing {
		t.Fatalf("detail: %+v", detail.Versions[0].Members)
	}
	var done GroupModerationView
	f.expectStatus(f.groupAction(m.group, ActionApprove, GroupActionRequest{}), http.StatusOK).decode(t, &done)
	if !done.Members[2].Missing || done.Members[1].To != "active" || done.Group.ListedAfter != 0 {
		t.Fatalf("the rest is approved and the group stays out of the catalogue: %+v", done)
	}
}

func TestAnOldGroupAgesTheQueue(t *testing.T) {
	f := newFixture(t, password)
	stranger, author := testkit.Identity(t), testkit.Identity(t)
	linked := testkit.SampleSet("Linked", "linked.example")
	linkedID, _ := f.shareAs(stranger, linked, otherAddress)
	f.approve(linkedID)
	published := f.clock
	f.group(author, "chat", authorAddress, linked)
	f.build()
	f.clock = f.clock.Add(80 * time.Hour)

	if b := f.badges(); b.Pending != 1 || b.OldestPending == nil || !b.OldestPending.Equal(published) {
		t.Fatalf("a group that links only listed sets is still one pending item: %+v", b)
	}
	var view HealthView
	f.admin(http.MethodGet, PathAPI+"/health", nil).decode(t, &view)
	if !hasIssue(view, IssueQueueOld) || view.Pending.Count != 1 || view.Pending.Oldest != nil {
		t.Fatalf("an old pending group is flagged: %+v", view)
	}
	if g := view.Pending.OldestGroup; g == nil || g.Title != "chat" || g.Version != 1 || view.Pending.Since == nil || !view.Pending.Since.Equal(published) {
		t.Fatalf("the card names the group as the oldest item: %+v", view.Pending)
	}

	f.shareAs(author, testkit.SampleSet("Later", "later.example"), authorAddress)
	var later HealthView
	f.admin(http.MethodGet, PathAPI+"/health", nil).decode(t, &later)
	if later.Pending.Count != 2 || later.Pending.Oldest != nil || later.Pending.OldestGroup == nil || !later.Pending.Since.Equal(published) {
		t.Fatalf("a newer set does not take the old group's place: %+v", later.Pending)
	}
}

func (f *fixture) republished() (string, []hubwire.GroupMemberResult) {
	f.t.Helper()
	author := testkit.Identity(f.t)
	g, first := f.group(author, "media", authorAddress, testkit.SampleSet("Video", "video.example"), testkit.SampleSet("Music", "music.example"))
	f.clock = f.clock.Add(time.Minute)
	video := testkit.SampleSet("Video", "video.example")
	video.Hub = &config.HubOrigin{ID: first[0].SetID, Version: first[0].Version}
	music := testkit.SampleSet("Music", "music.example")
	music.Faking.TTL = 9
	music.Hub = &config.HubOrigin{ID: first[1].SetID, Version: first[1].Version}
	body := hubwire.GroupBody{Title: "media", DerivedFrom: &hubwire.Origin{ID: g, Version: 1}}
	for _, set := range []config.SetConfig{video, music} {
		body.Members = append(body.Members, hubwire.GroupMember{Envelope: *testkit.BuildEnvelope(f.t, &set)})
	}
	resp := f.ingest.Handle(context.Background(), testkit.Sign(f.t, author, hubwire.RecordGroup, body, f.clock), parseIP(authorAddress))
	second, _ := resp.Body["members"].([]hubwire.GroupMemberResult)
	if resp.Status != http.StatusAccepted || resp.Body["version"] != 2 || len(second) != 2 {
		f.t.Fatalf("republish: %d %v", resp.Status, resp.Body)
	}
	if !second[0].Linked || second[0].SetID != first[0].SetID || second[0].Version != 1 || second[1].Linked || second[1].SetID != first[1].SetID || second[1].Version != 2 {
		f.t.Fatalf("v2 links the pending Video/1 and brings Music/2: %+v", second)
	}
	return g, first
}

func (f *fixture) queueCounts(items, groups int) QueueView {
	f.t.Helper()
	q := f.queueView()
	if b := f.badges(); len(q.Items) != items || len(q.Groups) != groups || b.Pending != items+groups {
		f.t.Fatalf("queue: want %d sets and %d groups, got %d and %d with badge %d", items, groups, len(q.Items), len(q.Groups), b.Pending)
	}
	return q
}

func (f *fixture) groupVersionAction(id string, version int, action string, req GroupActionRequest) response {
	return f.admin(http.MethodPost, PathAPI+"/groups/"+id+"/"+strconv.Itoa(version)+"/"+action, req)
}

func TestARepublishedGroupIsModeratedWithoutLosingAMember(t *testing.T) {
	t.Run("approve the newer version", func(t *testing.T) {
		f := newFixture(t, password)
		g, first := f.republished()
		f.queueCounts(0, 2)

		var done GroupModerationView
		f.expectStatus(f.groupVersionAction(g, 2, ActionApprove, GroupActionRequest{ExpectStatus: "pending"}), http.StatusOK).decode(t, &done)
		if done.Members[0].To != "active" || done.Group.ListedAfter != 2 || done.Params["members"] != float64(2) {
			t.Fatalf("approving v2 approves the Video/1 it links: %+v", done)
		}
		if q := f.queueCounts(0, 1); q.Groups[0].Version != 1 {
			t.Fatalf("v1 stays queued with Music/1: %+v", q.Groups)
		}
		f.build()
		var detail GroupDetailView
		f.admin(http.MethodGet, PathAPI+"/groups/"+g, nil).decode(t, &detail)
		if detail.PublishedVersion != 2 || detail.Versions[1].Members[0].Row == nil || detail.Versions[1].Members[0].Row.SetID != first[0].SetID || detail.Versions[1].Members[0].Row.Status != "active" {
			t.Fatalf("the group is listed with Video/1: %+v", detail)
		}
	})

	t.Run("reject the older version", func(t *testing.T) {
		f := newFixture(t, password)
		g, first := f.republished()

		var preview GroupModerationView
		f.expectStatus(f.groupVersionAction(g, 1, ActionReject, GroupActionRequest{Reason: "superseded by v2", DryRun: true}), http.StatusOK).decode(t, &preview)
		if m := preview.Members[0]; !m.OK || m.To != "" || m.Code != "kept_newer_group_version" || m.Params["group_version"] != float64(2) || preview.Members[1].To != "rejected" {
			t.Fatalf("the preview keeps the member v2 still includes: %+v", preview.Members)
		}
		f.expectStatus(f.groupVersionAction(g, 1, ActionReject, GroupActionRequest{Reason: "superseded by v2", ExpectStatus: "pending"}), http.StatusOK)
		var video SetDetailView
		f.admin(http.MethodGet, PathAPI+"/sets/"+first[0].SetID, nil).decode(t, &video)
		if video.Versions[0].Status != "pending" {
			t.Fatalf("Video/1 waits for v2: %+v", video.Versions[0])
		}
		if q := f.queueCounts(0, 1); q.Groups[0].Version != 2 {
			t.Fatalf("v2 holds Video/1 and Music/2: %+v", q.Groups)
		}

		f.expectStatus(f.groupVersionAction(g, 2, ActionHide, GroupActionRequest{Reason: "later"}), http.StatusOK)
		if sets := queuedSets(f.queueCounts(2, 0)); !sets[first[0].SetID] || !sets[first[1].SetID] {
			t.Fatalf("with v2 hidden both pending members are queued on their own: %v", sets)
		}
	})
}

func TestAGroupConfirmIsStaleWhenAMemberChangedAfterItsPreview(t *testing.T) {
	for _, change := range []string{ActionReject, ActionHide} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t, password)
			id, members := f.group(testkit.Identity(t), "media", authorAddress, testkit.SampleSet("Video", "video.example"), testkit.SampleSet("Music", "music.example"))
			var preview GroupModerationView
			f.expectStatus(f.groupAction(id, ActionApprove, GroupActionRequest{DryRun: true}), http.StatusOK).decode(t, &preview)
			expect := make([]ModerationItemRequest, 0, len(preview.Members))
			for _, m := range preview.Members {
				expect = append(expect, ModerationItemRequest{SetID: m.SetID, Version: m.Version, ExpectStatus: m.From})
			}
			f.expectOK(f.admin(http.MethodPost, PathAPI+"/sets/"+members[0].SetID+"/1/"+change, actionRequest{Reason: "another tab", ExpectStatus: "pending"}))

			refused := f.expectError(f.groupAction(id, ActionApprove, GroupActionRequest{ExpectStatus: "pending", Members: expect}), http.StatusConflict, "stale")
			if len(refused.Items) != 2 || refused.Items[0].OK || refused.Items[0].SetID != members[0].SetID {
				t.Fatalf("the refusal names the member: %+v", refused.Items)
			}
			var detail GroupDetailView
			f.admin(http.MethodGet, PathAPI+"/groups/"+id, nil).decode(t, &detail)
			if v := detail.Versions[0]; v.Status != "pending" || v.Members[0].Entry.Status == "active" || v.Members[1].Entry.Status != "pending" {
				t.Fatalf("a stale confirm changes nothing: %+v", v)
			}
		})
	}

	t.Run("delete", func(t *testing.T) {
		f := newFixture(t, password)
		id, members := f.group(testkit.Identity(t), "media", authorAddress, testkit.SampleSet("Video", "video.example"), testkit.SampleSet("Music", "music.example"))
		expect := []ModerationItemRequest{{SetID: members[0].SetID, Version: 1, ExpectStatus: "pending"}, {SetID: members[1].SetID, Version: 1, ExpectStatus: "pending"}}
		f.expectOK(f.admin(http.MethodPost, PathAPI+"/sets/"+members[0].SetID+"/delete", map[string]string{"confirm": members[0].SetID}))
		f.expectError(f.groupAction(id, ActionApprove, GroupActionRequest{ExpectStatus: "pending", Members: expect}), http.StatusConflict, "stale")
		var detail GroupDetailView
		f.admin(http.MethodGet, PathAPI+"/groups/"+id, nil).decode(t, &detail)
		if v := detail.Versions[0]; v.Status != "pending" || v.Members[1].Entry.Status != "pending" {
			t.Fatalf("a member deleted after the preview makes the confirm stale: %+v", v)
		}
	})
}

func TestASetDetailNamesTheGroupsThatHoldIt(t *testing.T) {
	f := newFixture(t, password)
	m := f.mediaGroup()
	detail := func(id string) SetDetailView {
		t.Helper()
		var d SetDetailView
		f.admin(http.MethodGet, PathAPI+"/sets/"+id, nil).decode(t, &d)
		return d
	}
	if g := detail(m.linked).Groups; len(g) != 1 || g[0].GroupID != m.group || g[0].Version != 1 || g[0].Title != "media" || g[0].Status != "pending" || g[0].Brought || g[0].SetVersion != 1 {
		t.Fatalf("a linked set names the group: %+v", g)
	}
	if g := detail(m.members[1].SetID).Groups; len(g) != 1 || !g[0].Brought {
		t.Fatalf("a brought set names the group: %+v", g)
	}
	if resp := f.admin(http.MethodGet, PathAPI+"/sets/"+m.single, nil); !strings.Contains(resp.body, `"groups":[]`) {
		t.Fatalf("a set in no group has an empty list: %s", resp.body)
	}
	f.expectStatus(f.groupAction(m.group, ActionHide, GroupActionRequest{Reason: "later"}), http.StatusOK)
	if g := detail(m.linked).Groups; len(g) != 1 || g[0].Status != "hidden" {
		t.Fatalf("a hidden group is still named: %+v", g)
	}
}

func previewExpectations(preview GroupModerationView, withKept bool) []ModerationItemRequest {
	out := make([]ModerationItemRequest, 0, len(preview.Members))
	for _, m := range preview.Members {
		item := ModerationItemRequest{SetID: m.SetID, Version: m.Version, ExpectStatus: m.From}
		if withKept {
			kept := m.Code == "kept_newer_group_version" || m.Code == "kept_other_group"
			item.ExpectKept = &kept
		}
		out = append(out, item)
	}
	return out
}

func (f *fixture) firstVersionStatuses(g string) []string {
	f.t.Helper()
	var detail GroupDetailView
	f.admin(http.MethodGet, PathAPI+"/groups/"+g, nil).decode(f.t, &detail)
	v := detail.Versions[0]
	return []string{v.Status, v.Members[0].Entry.Status, v.Members[1].Entry.Status}
}

func TestAGroupRejectConfirmIsStaleWhenWhatThePreviewKeptChanged(t *testing.T) {
	const reason = "superseded by v2"
	confirm := func(f *fixture, g string, members []ModerationItemRequest) response {
		return f.groupVersionAction(g, 1, ActionReject, GroupActionRequest{Reason: reason, ExpectStatus: "pending", Members: members})
	}
	preview := func(f *fixture, g string) GroupModerationView {
		var p GroupModerationView
		f.expectStatus(f.groupVersionAction(g, 1, ActionReject, GroupActionRequest{Reason: reason, DryRun: true}), http.StatusOK).decode(t, &p)
		return p
	}
	stale := func(f *fixture, g string, first []hubwire.GroupMemberResult, members []ModerationItemRequest) {
		t.Helper()
		refused := f.expectError(confirm(f, g, members), http.StatusConflict, "stale")
		if len(refused.Items) != 2 || refused.Items[0].OK || refused.Items[0].SetID != first[0].SetID || !refused.Items[1].OK {
			t.Fatalf("the refusal names Video/1: %+v", refused.Items)
		}
		if got := f.firstVersionStatuses(g); got[0] != "pending" || got[1] != "pending" || got[2] != "pending" {
			t.Fatalf("a stale confirm changes nothing: %v", got)
		}
	}

	t.Run("nothing changed after the preview", func(t *testing.T) {
		f := newFixture(t, password)
		g, _ := f.republished()
		f.expectStatus(confirm(f, g, previewExpectations(preview(f, g), true)), http.StatusOK)
		if got := f.firstVersionStatuses(g); got[0] != "rejected" || got[1] != "pending" || got[2] != "rejected" {
			t.Fatalf("a confirm that matches its preview keeps Video/1: %v", got)
		}
		var approve GroupModerationView
		f.expectStatus(f.groupVersionAction(g, 2, ActionApprove, GroupActionRequest{DryRun: true}), http.StatusOK).decode(t, &approve)
		f.expectStatus(f.groupVersionAction(g, 2, ActionApprove, GroupActionRequest{ExpectStatus: "pending", Members: previewExpectations(approve, true)}), http.StatusOK)
		var detail GroupDetailView
		f.admin(http.MethodGet, PathAPI+"/groups/"+g, nil).decode(t, &detail)
		if v := detail.Versions[1]; v.Status != "active" || v.Members[0].Row == nil || v.Members[0].Row.Status != "active" || v.Members[1].Entry.Status != "active" {
			t.Fatalf("an approve confirm with kept expectations goes through: %+v", v)
		}
	})

	t.Run("the newer version is rejected after the preview", func(t *testing.T) {
		f := newFixture(t, password)
		g, first := f.republished()
		p := preview(f, g)
		if p.Members[0].Code != "kept_newer_group_version" || p.Members[1].To != "rejected" {
			t.Fatalf("the preview keeps Video/1: %+v", p.Members)
		}
		f.expectStatus(f.groupVersionAction(g, 2, ActionReject, GroupActionRequest{Reason: "another tab", ExpectStatus: "pending"}), http.StatusOK)

		stale(f, g, first, previewExpectations(p, true))

		f.expectStatus(confirm(f, g, previewExpectations(p, false)), http.StatusOK)
		if got := f.firstVersionStatuses(g); got[0] != "rejected" || got[1] != "rejected" || got[2] != "rejected" {
			t.Fatalf("a confirm without kept expectations rejects what nothing keeps now: %v", got)
		}
	})

	t.Run("another group that links the set arrives after the preview", func(t *testing.T) {
		f := newFixture(t, password)
		g, first := f.republished()
		f.expectStatus(f.groupVersionAction(g, 2, ActionReject, GroupActionRequest{Reason: "another tab", ExpectStatus: "pending"}), http.StatusOK)
		p := preview(f, g)
		if p.Members[0].Code != "" || p.Members[0].To != "rejected" {
			t.Fatalf("with v2 rejected the preview rejects Video/1: %+v", p.Members)
		}
		other, members := f.group(testkit.Identity(t), "video", authorAddress, testkit.SampleSet("Video", "video.example"))
		if !members[0].Linked || members[0].SetID != first[0].SetID {
			t.Fatalf("the other group links Video/1: %+v", members)
		}

		stale(f, g, first, previewExpectations(p, true))

		var res GroupModerationView
		f.expectStatus(confirm(f, g, nil), http.StatusOK).decode(t, &res)
		if got := f.firstVersionStatuses(g); got[0] != "rejected" || got[1] != "pending" || got[2] != "rejected" || res.Members[0].Code != "kept_other_group" || res.Members[0].Params["group_id"] != other {
			t.Fatalf("a confirm without expectations keeps what the other group includes: %v %+v", got, res.Members[0])
		}
	})
}

func TestAQueuedGroupShowsInFullTheOwnedPendingSetItLinks(t *testing.T) {
	f := newFixture(t, password)
	stranger, author := testkit.Identity(t), testkit.Identity(t)
	foreignID, _ := f.shareAs(stranger, testkit.SampleSet("Linked", "linked.example"), otherAddress)
	f.approve(foreignID)
	g, first := f.group(author, "media", authorAddress, testkit.SampleSet("Video", "video.example"), testkit.SampleSet("Music", "music.example"))
	f.approve(first[1].SetID)

	f.clock = f.clock.Add(time.Minute)
	body := hubwire.GroupBody{Title: "media", DerivedFrom: &hubwire.Origin{ID: g, Version: 1}}
	for _, set := range []config.SetConfig{testkit.SampleSet("Video", "video.example"), testkit.SampleSet("Music", "music.example"), testkit.SampleSet("Linked", "linked.example")} {
		body.Members = append(body.Members, hubwire.GroupMember{Envelope: *testkit.BuildEnvelope(t, &set)})
	}
	resp := f.ingest.Handle(context.Background(), testkit.Sign(t, author, hubwire.RecordGroup, body, f.clock), parseIP(authorAddress))
	second, _ := resp.Body["members"].([]hubwire.GroupMemberResult)
	if resp.Status != http.StatusAccepted || resp.Body["version"] != 2 || len(second) != 3 || !second[0].Linked || !second[1].Linked || !second[2].Linked || second[2].SetID != foreignID {
		t.Fatalf("v2 links Video/1, the approved Music/1 and the stranger's set: %d %v", resp.Status, resp.Body)
	}

	check := func(where string, v GroupVersionView) {
		t.Helper()
		owned, active, foreign := v.Members[0], v.Members[1], v.Members[2]
		if owned.Brought || owned.BroughtIn != 1 || owned.Row != nil || owned.Entry == nil || owned.Entry.SetID != first[0].SetID || owned.Entry.Lineage == nil {
			t.Fatalf("%s: the pending set v1 brought is a full entry: %+v", where, owned)
		}
		if active.BroughtIn != 1 || active.Entry != nil || active.Row == nil || active.Row.Status != "active" {
			t.Fatalf("%s: an owned set that is not pending is a row: %+v", where, active)
		}
		if foreign.BroughtIn != 0 || foreign.Entry != nil || foreign.Row == nil || foreign.Row.SetID != foreignID {
			t.Fatalf("%s: a foreign linked set is a row: %+v", where, foreign)
		}
	}
	q := f.queueCounts(0, 2)
	if q.Groups[0].Version != 1 || q.Groups[1].Version != 2 {
		t.Fatalf("both versions are queued, oldest first: %+v", q.Groups)
	}
	check("queue", q.Groups[1])
	var detail GroupDetailView
	f.admin(http.MethodGet, PathAPI+"/groups/"+g, nil).decode(t, &detail)
	check("detail", detail.Versions[1])

	f.expectStatus(f.groupVersionAction(g, 1, ActionReject, GroupActionRequest{Reason: "superseded by v2"}), http.StatusOK)
	q = f.queueCounts(0, 1)
	if q.Groups[0].Version != 2 {
		t.Fatalf("v2 is queued alone: %+v", q.Groups)
	}
	check("queue with v1 rejected", q.Groups[0])
}
