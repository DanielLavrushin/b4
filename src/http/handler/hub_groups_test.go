package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hub/hubtest"
	"github.com/daniellavrushin/b4/hubwire"
)

func hubGroupLocalSet(id, name, group string, domains ...string) *config.SetConfig {
	set := hubStrategySet(name, domains...)
	set.Id = id
	set.Group = group
	return &set
}

func hubGroupStamp(set *config.SetConfig, hubID string, version int, groupID string, groupVersion int) *config.SetConfig {
	set.Hub = &config.HubOrigin{ID: hubID, Version: version, Hash: "stale", AppliedAt: "2026-01-01T00:00:00Z", GroupID: groupID, GroupVersion: groupVersion}
	return set
}

func (e *hubEnv) publishCatalogue(t *testing.T, seq int64, sets []hubwire.CatalogueSet, groups []hubwire.CatalogueGroup) {
	t.Helper()
	e.hub.Publish(t, &hubwire.Catalogue{Epoch: 1, Seq: seq, Sets: sets, Groups: groups}, time.Now().Add(time.Hour))
	if _, err := e.svc.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func hubGroupShareBody(t *testing.T, env *hubEnv) hubwire.GroupBody {
	t.Helper()
	records := env.hub.Records()
	if len(records) == 0 || records[len(records)-1].Kind != hubwire.RecordGroup {
		t.Fatalf("the hub must have received a group record, got %+v", records)
	}
	var body hubwire.GroupBody
	if err := json.Unmarshal(records[len(records)-1].Body, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestHubGroupSharePublishesTheGroupInListOrder(t *testing.T) {
	env := newHubEnv(t)
	a := hubGroupStamp(hubGroupLocalSet("a", "Video A", "video", "a.example"), "hub-a", 2, "g-video", 3)
	a.Escalate = config.EscalateConfig{To: "c2", RstThreshold: 4, RstWindowSec: 30}
	outside := hubGroupLocalSet("x", "Outside", "", "x.example")
	c1 := hubGroupStamp(hubGroupLocalSet("c1", "Moved one", "video", "c1.example"), "hub-c1", 1, "g-music", 9)
	c1.Escalate = config.EscalateConfig{To: "x", RstThreshold: 2}
	m1 := hubGroupStamp(hubGroupLocalSet("m1", "Music 1", "music", "m1.example"), "hub-m1", 1, "g-music", 9)
	c2 := hubGroupStamp(hubGroupLocalSet("c2", "Moved two", "video", "c2.example"), "hub-c2", 1, "g-music", 9)
	m2 := hubGroupStamp(hubGroupLocalSet("m2", "Music 2", "music", "m2.example"), "hub-m2", 1, "g-music", 9)
	m3 := hubGroupStamp(hubGroupLocalSet("m3", "Music 3", "music", "m3.example"), "hub-m3", 1, "g-music", 9)
	env.update(func(cfg *config.Config) {
		cfg.Sets = []*config.SetConfig{a, outside, c1, m1, c2, m2, m3}
	})

	rec := postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video", Description: "works here"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp HubGroupShareResponse
	decodeInto(t, rec, &resp)

	body := hubGroupShareBody(t, env)
	if body.Title != "video" || body.Description != "works here" || body.B4Version != Version || len(body.Members) != 3 {
		t.Fatalf("unexpected group body %+v", body)
	}
	for i, want := range []string{"Video A", "Moved one", "Moved two"} {
		if got := body.Members[i].Envelope.Title; got != want {
			t.Errorf("member %d must be %q in list order, got %q", i, want, got)
		}
		if _, ok := body.Members[i].Envelope.Set["escalate"]; ok {
			t.Errorf("member %d must not carry its local escalation in the envelope", i)
		}
	}
	if esc := body.Members[0].Escalate; esc == nil || esc.To != 2 || esc.RstThreshold != 4 || esc.RstWindowSec != 30 {
		t.Errorf("a link to another member travels as its position with its thresholds, got %+v", esc)
	}
	if esc := body.Members[1].Escalate; esc != nil {
		t.Errorf("a link to a set outside the group must be dropped, got %+v", esc)
	}
	if body.DerivedFrom == nil || body.DerivedFrom.ID != "g-video" || body.DerivedFrom.Version != 3 {
		t.Errorf("DerivedFrom must name the hub group this local group is the copy of, not the one two moved sets carry, got %+v", body.DerivedFrom)
	}

	if resp.GroupID == "" || resp.Version != 1 || resp.Status != hubwire.SetStatusPending || len(resp.Members) != 3 {
		t.Fatalf("unexpected reply %+v", resp)
	}
	for i, local := range []string{"a", "c1", "c2"} {
		m := resp.Members[i]
		if m.SetID != local || m.HubID == "" || m.Result != HubGroupResultNewSet {
			t.Errorf("member %d reply must name local %s and its hub set, got %+v", i, local, m)
		}
		set := env.localSet(local)
		if set.Hub == nil || set.Hub.ID != m.HubID || set.Hub.Version != 1 || set.Hub.Hash != body.Members[i].Envelope.Fingerprint || set.Hub.GroupID != resp.GroupID || set.Hub.GroupVersion != 1 {
			t.Errorf("member %d must be stamped from the hub answer, got %+v", i, set.Hub)
		}
		if set.Hub.AppliedAt != "2026-01-01T00:00:00Z" || set.Hub.Vote != "" {
			t.Errorf("stamping keeps applied_at only, got %+v", set.Hub)
		}
		if hubStateOf(env.api.getCfg(), set) != HubStateUnmodified {
			t.Errorf("member %d must read as unmodified after publishing", i)
		}
	}
	for _, id := range []string{"m1", "m2", "m3", "x"} {
		if set := env.localSet(id); (set.Hub != nil && set.Hub.GroupID != "g-music") || (id == "x" && set.Hub != nil) {
			t.Errorf("set %s is not in the group and must keep its stamp, got %+v", id, set.Hub)
		}
	}

	firstGroup := resp.GroupID
	env.hub.SetAnswer(func(rec *hubwire.Record) hubtest.Answer {
		var sent hubwire.GroupBody
		_ = json.Unmarshal(rec.Body, &sent)
		answer := hubtest.NewGroupAnswer(rec, sent)
		answer.Version = 2
		answer.Members[0] = hubwire.GroupMemberResult{SetID: "someone-else", Version: 4, Status: hubwire.SetStatusActive, Linked: true}
		answer.Members[1].Version = 2
		return hubtest.Answer{Status: http.StatusAccepted, Body: answer}
	})
	rec = postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	decodeInto(t, rec, &resp)
	if got := hubGroupShareBody(t, env).DerivedFrom; got == nil || got.ID != firstGroup || got.Version != 1 {
		t.Errorf("the next publish derives from the group just published, got %+v", got)
	}
	if resp.Members[0].Result != HubGroupResultLinked || resp.Members[1].Result != HubGroupResultNewVersion || resp.Members[2].Result != HubGroupResultNewSet {
		t.Errorf("each member must say whether it linked, made a new version or a new set: %+v", resp.Members)
	}
	if set := env.localSet("a"); set.Hub.ID != "someone-else" || set.Hub.Version != 4 || set.Hub.GroupVersion != 2 {
		t.Errorf("a linked member takes the linked hub set's id and version, got %+v", set.Hub)
	}
}

func TestHubGroupShareErrorsKeepTheConfig(t *testing.T) {
	env := newHubEnv(t)
	first := hubGroupLocalSet("first", "First", "video", "a.example")
	second := hubGroupLocalSet("second", "Second", "video", "b.example")
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{first, second} })
	unchanged := func(step string) {
		t.Helper()
		for _, id := range []string{"first", "second"} {
			if set := env.localSet(id); set.Hub != nil {
				t.Errorf("%s: set %s must stay unstamped, got %+v", step, id, set.Hub)
			}
		}
	}

	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "  "}), http.StatusBadRequest, "bad_request")
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "nothing"}), http.StatusNotFound, "not_found")

	env.hub.SetAnswer(func(*hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: http.StatusBadRequest, Body: map[string]interface{}{"code": "invalid_set", "error": "no targets", "member": 1}}
	})
	rec := postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video"})
	var memberErr HubMemberError
	decodeInto(t, rec, &memberErr)
	if rec.Code != http.StatusBadRequest || memberErr.Code != "invalid_set" || memberErr.Member != 1 || memberErr.SetID != "second" || memberErr.SetName != "Second" {
		t.Errorf("a member refusal must name the set, got %d %+v", rec.Code, memberErr)
	}
	unchanged("member refusal")

	env.hub.SetAnswer(func(*hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: http.StatusBadRequest, Body: map[string]interface{}{"code": "bad_record", "error": "unknown record kind"}}
	})
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video"}), http.StatusBadRequest, "hub_no_groups")
	unchanged("older hub")

	env.hub.SetAnswer(func(*hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: http.StatusConflict, Body: map[string]interface{}{"code": "duplicate_group", "error": "unchanged", "group_id": "g-1", "version": 2}}
	})
	rec = postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video"})
	var remote HubRemoteError
	decodeInto(t, rec, &remote)
	if rec.Code != http.StatusConflict || remote.Code != "duplicate_group" || remote.GroupID != "g-1" || remote.Version != 2 {
		t.Errorf("a duplicate group must pass the group id through, got %d %+v", rec.Code, remote)
	}
	unchanged("duplicate")

	env.hub.SetAnswer(func(*hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: http.StatusAccepted, Body: map[string]interface{}{"id": "r", "kind": "group", "group_id": "g-1", "version": 1, "members": []map[string]interface{}{{"set_id": "only-one", "version": 1}}}}
	})
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video"}), http.StatusBadGateway, "bad_hub_answer")
	unchanged("short answer")

	env.hub.SetAnswer(nil)
	env.hub.SetDown(true)
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video"}), http.StatusBadGateway, "hub_unreachable")
	if env.svc.OutboxCount() != 0 {
		t.Errorf("a group record is never queued")
	}
	unchanged("hub down")
	env.hub.SetDown(false)

	var big []*config.SetConfig
	for i := 0; i < 4; i++ {
		domains := make([]string, hubwire.MaxSNIDomains)
		for j := range domains {
			domains[j] = fmt.Sprintf("member-%d-domain-%04d.a-rather-long-example-domain.test", i, j)
		}
		big = append(big, hubGroupLocalSet(fmt.Sprintf("big-%d", i), fmt.Sprintf("Big %d", i), "big", domains...))
	}
	env.update(func(cfg *config.Config) { cfg.Sets = append(cfg.Sets, big...) })
	records := len(env.hub.Records())
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "big"}), http.StatusBadRequest, "group_too_large")
	if len(env.hub.Records()) != records {
		t.Errorf("a record over the hub limit must not be sent")
	}

	var many []*config.SetConfig
	for i := 0; i <= hubwire.MaxGroupMembers; i++ {
		many = append(many, hubGroupLocalSet(fmt.Sprintf("many-%d", i), fmt.Sprintf("Many %d", i), "many", fmt.Sprintf("many%d.example", i)))
	}
	env.update(func(cfg *config.Config) { cfg.Sets = append(cfg.Sets, many...) })
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "many"}), http.StatusBadRequest, "too_many_members")
	unchanged("too many")
}

type hubGroupFixture struct {
	sets   []hubwire.CatalogueSet
	groups []hubwire.CatalogueGroup
}

func hubCatalogueMember(t *testing.T, id string, version int, author string, domains ...string) hubwire.CatalogueSet {
	t.Helper()
	return hubCatalogueMemberWith(t, id, version, author, nil, domains...)
}

func hubCatalogueMemberWith(t *testing.T, id string, version int, author string, edit func(*config.SetConfig), domains ...string) hubwire.CatalogueSet {
	t.Helper()
	set := hubStrategySet(id, domains...)
	if edit != nil {
		edit(&set)
	}
	cs, _ := hubtest.CatalogueSet(t, id, version, &set, nil)
	cs.Author = author
	return cs
}

func hubGroupIDs(sets []*config.SetConfig) []string {
	ids := make([]string, 0, len(sets))
	for _, set := range sets {
		ids = append(ids, set.Id)
	}
	return ids
}

func countTableRefreshes(t *testing.T) *int {
	t.Helper()
	n := 0
	prev := tablesRefreshFunc
	tablesRefreshFunc = func() error { n++; return nil }
	t.Cleanup(func() { tablesRefreshFunc = prev })
	return &n
}

func TestHubGroupApplyMakesOneBlockAtTheTop(t *testing.T) {
	env := newHubEnv(t)
	s1 := hubCatalogueMemberWith(t, "s1", 2, "alice", func(set *config.SetConfig) { set.TCP.DPortFilter = "8443" }, "a.example", "shared.example")
	s2 := hubCatalogueMember(t, "s2", 1, "alice", "b.example", "shared.example")
	s3 := hubCatalogueMemberWith(t, "s3", 1, "bob", func(set *config.SetConfig) { set.TCP.DPortFilter = "9443" }, "c.example")
	group := hubwire.CatalogueGroup{ID: "g-video", Version: 3, Title: "video", Author: "alice", Members: []hubwire.CatalogueGroupMember{
		{SetID: "s1", Escalate: &hubwire.GroupEscalation{To: 2, RstThreshold: 5}},
		{SetID: "s2"},
		{SetID: "s3"},
	}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{s1, s2, s3}, []hubwire.CatalogueGroup{group})

	mine := hubGroupLocalSet("mine", "Mine", "Video", "mine.example")
	old := hubGroupLocalSet("old", "Old", "", "shared.example", "a.example", "kept.example")
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{mine, old} })
	refreshes := countTableRefreshes(t)

	rec := postJSON(t, env.mux, "/api/hub/groups/g-video/apply", map[string]interface{}{})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp HubGroupApplyResponse
	decodeInto(t, rec, &resp)
	if *refreshes != 1 {
		t.Errorf("a group apply restarts once, got %d", *refreshes)
	}
	if resp.Group != "video 2" {
		t.Errorf("the group takes its title, made unique against a local group that differs by case, got %q", resp.Group)
	}

	cfg := env.api.getCfg()
	if len(cfg.Sets) != 5 || len(resp.Sets) != 3 || len(resp.Created) != 3 || len(resp.Ungrouped) != 0 {
		t.Fatalf("unexpected result %+v, sets %v", resp, hubGroupIDs(cfg.Sets))
	}
	for i, hubID := range []string{"s1", "s2", "s3"} {
		set := cfg.Sets[i]
		if set.Id != resp.Sets[i].ID || set.Id != resp.Created[i] || resp.Sets[i].HubID != hubID || !resp.Sets[i].Created {
			t.Errorf("member %d must sit at position %d in member order: %v %+v", i, i, hubGroupIDs(cfg.Sets), resp.Sets[i])
		}
		if set.Group != "video 2" || set.Hub == nil || set.Hub.ID != hubID || set.Hub.GroupID != "g-video" || set.Hub.GroupVersion != 3 || set.Hub.AppliedAt == "" {
			t.Errorf("member %d must be stamped with the hub group: group %q hub %+v", i, set.Group, set.Hub)
		}
	}
	if cfg.Sets[3].Id != "mine" || cfg.Sets[3].Group != "Video" || cfg.Sets[4].Id != "old" {
		t.Errorf("the other sets follow the block untouched: %v", hubGroupIDs(cfg.Sets))
	}
	if esc := cfg.Sets[0].Escalate; esc.To != cfg.Sets[2].Id || esc.RstThreshold != 5 {
		t.Errorf("the escalation must point at the new local id of member 2, got %+v", esc)
	}
	if cfg.Sets[1].Escalate.To != "" || cfg.Sets[2].Escalate.To != "" {
		t.Errorf("members without a link have none")
	}
	for _, i := range []int{0, 1} {
		if !slices.Contains(cfg.Sets[i].Targets.SNIDomains, "shared.example") {
			t.Errorf("member %d must keep shared.example: members never take domains from each other, got %v", i, cfg.Sets[i].Targets.SNIDomains)
		}
	}
	if got := env.localSet("old").Targets.SNIDomains; len(got) != 1 || got[0] != "kept.example" {
		t.Errorf("a set outside the block gives up the members' domains, got %v", got)
	}
	if len(resp.Sets[0].Moved) != 2 || len(resp.Sets[1].Moved) != 0 {
		t.Errorf("moved lists each member's domains taken from sets outside the block: %+v / %+v", resp.Sets[0].Moved, resp.Sets[1].Moved)
	}
	for i, s := range resp.Sets {
		for _, c := range s.Covered {
			if slices.Contains(resp.Created, c.SetId) {
				t.Errorf("member %d must not report another member as covered: %+v", i, c)
			}
		}
	}

	view := getJSON(t, env.mux, "/api/hub/groups/g-video")
	var applied HubGroup
	decodeInto(t, view, &applied)
	if applied.Applied.State != HubGroupStateApplied || applied.Applied.Group != "video 2" || applied.Applied.Version != 3 || len(applied.Applied.SetIDs) != 3 {
		t.Errorf("the group must read as applied, got %+v", applied.Applied)
	}
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/missing/apply", map[string]interface{}{}), http.StatusNotFound, "not_found")
}

func TestHubGroupApplyNamesTheMemberThatFails(t *testing.T) {
	env := newHubEnv(t)
	s1 := hubCatalogueMember(t, "s1", 1, "alice", "a.example")
	s2 := hubCatalogueMember(t, "s2", 1, "alice")
	group := hubwire.CatalogueGroup{ID: "g", Version: 1, Title: "g", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s1"}, {SetID: "s2"}}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{s1, s2}, []hubwire.CatalogueGroup{group})
	before := len(env.api.getCfg().Sets)

	rec := postJSON(t, env.mux, "/api/hub/groups/g/apply", map[string]interface{}{})
	var e HubMemberError
	decodeInto(t, rec, &e)
	if rec.Code != http.StatusBadRequest || e.Code != "no_targets" || e.Member != 1 || e.HubID != "s2" {
		t.Fatalf("the member without targets must be named, got %d %+v", rec.Code, e)
	}
	if len(env.api.getCfg().Sets) != before {
		t.Errorf("nothing is saved when one member fails")
	}
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/g/apply", HubGroupApplyRequest{Replace: true}), http.StatusConflict, "not_applied")
}

func TestHubGroupUpdateReplacesTheCopyInPlace(t *testing.T) {
	env := newHubEnv(t)
	s1v1 := hubCatalogueMember(t, "s1", 1, "alice", "a.example")
	s2 := hubCatalogueMember(t, "s2", 1, "alice", "b.example")
	s4 := hubCatalogueMember(t, "s4", 1, "alice", "d.example")
	v1 := hubwire.CatalogueGroup{ID: "g-video", Version: 1, Title: "video", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s1"}, {SetID: "s2"}, {SetID: "s4"}}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{s1v1, s2, s4}, []hubwire.CatalogueGroup{v1})
	hubApplyGroupOK(t, env, "g-video", nil)

	top := hubGroupLocalSet("top", "Top", "", "top.example")
	other := hubGroupLocalSet("other", "Other", "", "other.example")
	moved := hubGroupStamp(hubGroupLocalSet("moved", "Moved", "elsewhere", "moved.example"), "s3", 1, "g-video", 1)
	mine := hubGroupLocalSet("mine", "Mine", "video", "mine.example")
	env.update(func(cfg *config.Config) {
		for _, set := range cfg.Sets {
			set.Group = "my video"
		}
		cfg.Sets[1].Enabled = false
		mine.Group = "my video"
		cfg.Sets = append(append([]*config.SetConfig{top}, cfg.Sets...), mine, other, moved)
	})
	before := env.api.getCfg().Sets
	l1, l2, l4 := before[1], before[2], before[3]

	s1v2 := hubCatalogueMember(t, "s1", 2, "alice", "a.example", "a2.example")
	s3 := hubCatalogueMember(t, "s3", 1, "bob", "c.example")
	v2 := hubwire.CatalogueGroup{ID: "g-video", Version: 2, Title: "video renamed", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s1"}, {SetID: "s3"}, {SetID: "s2", Escalate: &hubwire.GroupEscalation{To: 0}}}}
	env.publishCatalogue(t, 2, []hubwire.CatalogueSet{s1v2, s2, s3, s4}, []hubwire.CatalogueGroup{v2})

	var view HubGroup
	decodeInto(t, getJSON(t, env.mux, "/api/hub/groups/g-video"), &view)
	if view.Applied.State != HubGroupStateUpdate || view.Applied.Group != "my video" || view.Applied.Version != 1 {
		t.Fatalf("a newer group version must read as update on the renamed copy, got %+v", view.Applied)
	}

	resp := hubApplyGroupOK(t, env, "g-video", map[string]interface{}{"replace": true})
	cfg := env.api.getCfg()
	ids := hubGroupIDs(cfg.Sets)
	if len(resp.Created) != 1 || resp.Group != "my video" {
		t.Fatalf("only the new member is created and the local name is kept: %+v", resp)
	}
	want := []string{"top", l1.Id, resp.Created[0], l2.Id, "mine", l4.Id, "other", "moved"}
	if !slices.Equal(ids, want) {
		t.Fatalf("expected %v, got %v", want, ids)
	}
	for i, hubID := range []string{"s1", "s3", "s2"} {
		set := cfg.Sets[1+i]
		if set.Group != "my video" || set.Hub == nil || set.Hub.ID != hubID || set.Hub.GroupVersion != 2 {
			t.Errorf("member %s must be in the block with the new group version: %q %+v", hubID, set.Group, set.Hub)
		}
	}
	if cfg.Sets[1].Hub.Version != 2 || !slices.Contains(cfg.Sets[1].Targets.SNIDomains, "a2.example") {
		t.Errorf("s1 must be replaced by its new version: %+v %v", cfg.Sets[1].Hub, cfg.Sets[1].Targets.SNIDomains)
	}
	if cfg.Sets[3].Enabled {
		t.Errorf("a replaced member keeps its local enabled switch")
	}
	if cfg.Sets[3].Escalate.To != l1.Id {
		t.Errorf("the escalation of the new version points at the kept local id of member 0, got %q", cfg.Sets[3].Escalate.To)
	}
	if mineNow := env.localSet("mine"); mineNow.Group != "my video" {
		t.Errorf("a local set in the group stays in it, got %q", mineNow.Group)
	}
	if kept := env.localSet("moved"); kept.Group != "elsewhere" || kept.Hub.GroupID != "g-video" || kept.Hub.Version != 1 || resp.Created[0] == "moved" {
		t.Errorf("a set moved to another local group is not part of the copy and stays as it was: %q %+v", kept.Group, kept.Hub)
	}
	dropped := env.localSet(l4.Id)
	if dropped.Group != "" || dropped.Hub.GroupID != "" || dropped.Hub.GroupVersion != 0 || dropped.Hub.ID != "s4" || !slices.Equal(resp.Ungrouped, []string{l4.Id}) {
		t.Errorf("a member the group no longer lists leaves the group and stays a set: %q %+v %v", dropped.Group, dropped.Hub, resp.Ungrouped)
	}

	decodeInto(t, getJSON(t, env.mux, "/api/hub/groups/g-video"), &view)
	if view.Applied.State != HubGroupStateApplied || len(view.Applied.SetIDs) != 3 {
		t.Fatalf("after the update the group reads as applied, got %+v", view.Applied)
	}
	env.update(func(cfg *config.Config) {
		for _, set := range cfg.Sets {
			if set.Id == l1.Id {
				set.Faking.TTL = 3
			}
		}
	})
	decodeInto(t, getJSON(t, env.mux, "/api/hub/groups/g-video"), &view)
	if view.Applied.State != HubGroupStateModified {
		t.Errorf("a member edited locally makes the group read as modified, got %+v", view.Applied)
	}
}

func hubApplyGroupOK(t *testing.T, env *hubEnv, id string, body map[string]interface{}) HubGroupApplyResponse {
	t.Helper()
	if body == nil {
		body = map[string]interface{}{}
	}
	rec := postJSON(t, env.mux, "/api/hub/groups/"+id+"/apply", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp HubGroupApplyResponse
	decodeInto(t, rec, &resp)
	return resp
}

func TestHubGroupCopyIsTheLabelHoldingMostOfItsSets(t *testing.T) {
	stamped := func(id, group, groupID string) *config.SetConfig {
		return hubGroupStamp(hubGroupLocalSet(id, id, group, id+".example"), "hub-"+id, 1, groupID, 1)
	}
	sets := []*config.SetConfig{
		stamped("moved", "elsewhere", "g"),
		stamped("a", "video", "g"),
		stamped("b", "video", "g"),
		stamped("loose", "", "g"),
		stamped("other", "video", "h"),
	}
	found := hubGroupCopyOf(sets, "g")
	if found.label != "video" || !slices.Equal(hubGroupIDs(found.sets), []string{"a", "b"}) {
		t.Errorf("the copy is the label with most sets of the group, without moved or ungrouped ones: %q %v", found.label, hubGroupIDs(found.sets))
	}
	tie := hubGroupCopyOf([]*config.SetConfig{stamped("x", "first", "g"), stamped("y", "second", "g")}, "g")
	if tie.label != "first" {
		t.Errorf("a tie goes to the label whose set comes first, got %q", tie.label)
	}
	interleaved := hubGroupCopyOf([]*config.SetConfig{stamped("p", "first", "g"), stamped("q", "second", "g"), stamped("r", "second", "g"), stamped("s", "first", "g")}, "g")
	if interleaved.label != "first" || !slices.Equal(hubGroupIDs(interleaved.sets), []string{"p", "s"}) {
		t.Errorf("a tie goes to the label whose first set comes first, even when the labels interleave, got %q %v", interleaved.label, hubGroupIDs(interleaved.sets))
	}
}

func TestHubSetsFoldsTheGroupAuthorsMembers(t *testing.T) {
	env := newHubEnv(t)
	s1 := hubCatalogueMember(t, "s1", 1, "alice", "one.example")
	s2 := hubCatalogueMember(t, "s2", 1, "alice", "two.example")
	s3 := hubCatalogueMember(t, "s3", 1, "bob", "three.example")
	s4 := hubCatalogueMember(t, "s4", 1, "alice", "four.example")
	group := hubwire.CatalogueGroup{ID: "g", Version: 1, Title: "mix", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s1"}, {SetID: "s2"}, {SetID: "s3"}}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{s1, s2, s3, s4}, []hubwire.CatalogueGroup{group})

	var list HubSetsResponse
	decodeInto(t, getJSON(t, env.mux, "/api/hub/sets"), &list)
	listed := func() []string {
		ids := []string{}
		for _, s := range list.Sets {
			ids = append(ids, s.ID)
		}
		slices.Sort(ids)
		return ids
	}
	if len(list.Groups) != 1 || len(list.Groups[0].Members) != 3 || list.Groups[0].Applied.State != HubGroupStateNone {
		t.Fatalf("the group must be listed with its members, got %+v", list.Groups)
	}
	if got := listed(); !slices.Equal(got, []string{"s3", "s4"}) || list.Total != 2 {
		t.Errorf("the group author's members are folded into the group, another author's keeps its card: %v total %d", got, list.Total)
	}

	decodeInto(t, getJSON(t, env.mux, "/api/hub/sets?domain=www.two.example"), &list)
	if len(list.Groups) != 1 || len(list.Sets) != 0 || list.Total != 0 {
		t.Errorf("a domain search lists the group when a member matches and folds the member: %+v", list)
	}
	decodeInto(t, getJSON(t, env.mux, "/api/hub/sets?domain=four.example"), &list)
	if len(list.Groups) != 0 || len(list.Sets) != 1 || list.Sets[0].ID != "s4" {
		t.Errorf("no member matches, no group: %+v", list)
	}
	decodeInto(t, getJSON(t, env.mux, "/api/hub/sets?limit=1"), &list)
	if len(list.Sets) != 1 || list.Total != 2 {
		t.Errorf("the limit applies after the fold: %d sets, total %d", len(list.Sets), list.Total)
	}

	presets := env.api.communityPresets([]string{"two.example"}, false)
	if len(presets) != 1 || presets[0].Config.Hub == nil || presets[0].Config.Hub.ID != "s2" {
		t.Errorf("Discovery still tests a member the Community view folds, got %+v", presets)
	}

	var groups HubGroupsResponse
	decodeInto(t, getJSON(t, env.mux, "/api/hub/groups?set=s3"), &groups)
	if len(groups.Groups) != 1 || groups.Groups[0].ID != "g" || groups.Groups[0].Members[0].Set.ID != "s1" {
		t.Errorf("the groups holding s3 must be listed, got %+v", groups.Groups)
	}
	decodeInto(t, getJSON(t, env.mux, "/api/hub/groups?set=s4"), &groups)
	if groups.Groups == nil || len(groups.Groups) != 0 {
		t.Errorf("no group holds s4, got %+v", groups.Groups)
	}
	decodeInto(t, getJSON(t, env.mux, "/api/hub/groups"), &groups)
	if len(groups.Groups) != 1 {
		t.Errorf("all listed groups without a set, got %+v", groups.Groups)
	}
	expectCode(t, getJSON(t, env.mux, "/api/hub/groups/missing"), http.StatusNotFound, "not_found")
}

func TestHubSingleSetWritesKeepTheGroupStamp(t *testing.T) {
	env := newHubEnv(t)
	s1 := hubCatalogueMember(t, "s1", 2, "alice", "a.example")
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{s1}, nil)
	local := hubGroupStamp(hubGroupLocalSet("local", "Local", "video", "a.example"), "s1", 1, "g-video", 4)
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{local} })

	hubApplyOK(t, env, "s1", map[string]interface{}{"replace": "local"})
	if set := env.localSet("local"); set.Hub.Version != 2 || set.Hub.GroupID != "g-video" || set.Hub.GroupVersion != 4 || set.Group != "video" {
		t.Errorf("updating one member keeps it in its group copy, got %q %+v", set.Group, set.Hub)
	}

	rec := postJSON(t, env.mux, "/api/hub/share", HubShareRequest{SetID: "local"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	if set := env.localSet("local"); set.Hub.GroupID != "g-video" || set.Hub.GroupVersion != 4 {
		t.Errorf("sharing one member keeps its group stamp, got %+v", set.Hub)
	}
}

func hubGroupStateOf(t *testing.T, env *hubEnv, id string) HubGroupApplied {
	t.Helper()
	var view HubGroup
	decodeInto(t, getJSON(t, env.mux, "/api/hub/groups/"+id), &view)
	return view.Applied
}

func hubGroupAnswer(env *hubEnv, version int, members []hubwire.GroupMemberResult) {
	env.hub.SetAnswer(func(rec *hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: http.StatusAccepted, Body: map[string]interface{}{"id": rec.ID(), "kind": rec.Kind, "group_id": "G", "version": version, "status": hubwire.SetStatusPending, "members": members}}
	})
}

func TestHubGroupPublishedAheadOfTheCatalogueReadsApplied(t *testing.T) {
	env := newHubEnv(t)
	a := hubGroupLocalSet("a", "A", "video", "a.example")
	b := hubGroupLocalSet("b", "B", "video", "b.example")
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{a, b} })
	hubGroupAnswer(env, 1, []hubwire.GroupMemberResult{{SetID: "s1", Version: 1}, {SetID: "s2", Version: 1}})
	if rec := postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video"}); rec.Code != http.StatusAccepted {
		t.Fatalf("publish v1: %d %s", rec.Code, rec.Body.String())
	}
	g1 := hubwire.CatalogueGroup{ID: "G", Version: 1, Title: "video", Author: "me", Members: []hubwire.CatalogueGroupMember{{SetID: "s1"}, {SetID: "s2"}}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{hubCatalogueMember(t, "s1", 1, "me", "a.example"), hubCatalogueMember(t, "s2", 1, "me", "b.example")}, []hubwire.CatalogueGroup{g1})
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateApplied {
		t.Fatalf("the published group reads applied once listed, got %+v", got)
	}

	c := hubGroupLocalSet("c", "C", "video", "c.example")
	env.update(func(cfg *config.Config) {
		cfg.Sets[0].Targets.SNIDomains = append(cfg.Sets[0].Targets.SNIDomains, "a2.example")
		cfg.Sets = append(cfg.Sets, c)
	})
	hubGroupAnswer(env, 2, []hubwire.GroupMemberResult{{SetID: "s1", Version: 2}, {SetID: "s2", Version: 1, Linked: true}, {SetID: "t3", Version: 1}})
	if rec := postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video"}); rec.Code != http.StatusAccepted {
		t.Fatalf("publish v2: %d %s", rec.Code, rec.Body.String())
	}
	got := hubGroupStateOf(t, env, "G")
	if got.State != HubGroupStateApplied || got.Version != 2 {
		t.Fatalf("a copy published ahead of the listed version waits for moderation and reads applied, not update, got %+v", got)
	}

	env.update(func(cfg *config.Config) { cfg.Sets[1].Faking.TTL = 3 })
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateModified {
		t.Errorf("a member edited after publishing reads modified, got %+v", got)
	}
}

func TestHubGroupMembershipDifferenceReadsModified(t *testing.T) {
	env := newHubEnv(t)
	s1 := hubCatalogueMember(t, "s1", 1, "alice", "a.example")
	s2 := hubCatalogueMember(t, "s2", 1, "alice", "b.example")
	s9 := hubCatalogueMember(t, "s9", 1, "bob", "a.example", "z.example")
	g1 := hubwire.CatalogueGroup{ID: "G", Version: 1, Title: "video", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s1"}, {SetID: "s2"}}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{s1, s2, s9}, []hubwire.CatalogueGroup{g1})
	hubApplyGroupOK(t, env, "G", nil)
	first := env.api.getCfg().Sets[0]

	env.update(func(cfg *config.Config) {
		dup := *cfg.Sets[0]
		hub := *dup.Hub
		dup.Hub, dup.Id, dup.Name = &hub, "dup", dup.Name+" (copy)"
		cfg.Sets = append([]*config.SetConfig{cfg.Sets[0], &dup}, cfg.Sets[1:]...)
	})
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateApplied {
		t.Errorf("a second local set with a member's hub id is ignored, got %+v", got)
	}
	env.update(func(cfg *config.Config) {
		cfg.Sets = slices.DeleteFunc(cfg.Sets, func(s *config.SetConfig) bool { return s.Id == "dup" })
	})

	hubApplyOK(t, env, "s9", map[string]interface{}{"replace": first.Id})
	if set := env.localSet(first.Id); set.Hub.ID != "s9" || set.Hub.GroupID != "" || set.Hub.GroupVersion != 0 || set.Group != first.Group {
		t.Errorf("a member replaced by another hub set keeps its place in the local group but not the hub group stamp, got %q %+v", set.Group, set.Hub)
	}
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateModified {
		t.Errorf("a member missing from the copy at the listed version reads modified, got %+v", got)
	}

	env.update(func(cfg *config.Config) {
		for _, set := range cfg.Sets {
			if set.Id == first.Id {
				set.Hub.GroupID, set.Hub.GroupVersion = "G", 1
			}
		}
	})
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateModified {
		t.Errorf("a set stamped with the group whose hub set is no member reads modified, got %+v", got)
	}

	s2v2 := hubCatalogueMember(t, "s2", 2, "alice", "b.example", "b2.example")
	env.publishCatalogue(t, 2, []hubwire.CatalogueSet{s1, s2v2, s9}, []hubwire.CatalogueGroup{g1})
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateUpdate {
		t.Errorf("a newer version of a member set reads update, got %+v", got)
	}
	g2 := g1
	g2.Version = 2
	env.publishCatalogue(t, 3, []hubwire.CatalogueSet{s1, s2, s9}, []hubwire.CatalogueGroup{g2})
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateUpdate {
		t.Errorf("a newer listed group version reads update, got %+v", got)
	}
}

func TestHubGroupApplyRefusedInstallsNoPayload(t *testing.T) {
	env := newHubEnv(t)
	captured := hubStrategySet("Shared video", "youtube.com")
	captured.Faking.SNIType = config.FakePayloadCapture
	captured.Faking.PayloadFile = "captures/tls_www_google_com.bin"
	hello := slices.Clone(config.FakeSNI1)
	hello[20] ^= 0x5a
	s1, payloads := hubtest.CatalogueSet(t, "s1", 1, &captured, func(string) ([]byte, error) { return hello, nil })
	for _, p := range payloads {
		env.hub.AddBlob(p)
	}
	s1.Author = "alice"
	empty := hubCatalogueMember(t, "s2", 1, "alice")
	refused := hubwire.CatalogueGroup{ID: "g", Version: 1, Title: "g", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s1"}, {SetID: "s2"}}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{s1, empty}, []hubwire.CatalogueGroup{refused})
	dir := filepath.Join(filepath.Dir(env.api.getCfg().ConfigPath), "captures")
	files := func() []string {
		entries, _ := os.ReadDir(dir)
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}
	metadata := func() string {
		raw, _ := os.ReadFile(filepath.Join(dir, "payloads.json"))
		return string(raw)
	}
	before, listed := files(), metadata()

	rec := postJSON(t, env.mux, "/api/hub/groups/g/apply", map[string]interface{}{})
	var e HubMemberError
	decodeInto(t, rec, &e)
	if rec.Code != http.StatusBadRequest || e.Code != "no_targets" || e.Member != 1 {
		t.Fatalf("the second member must refuse the group, got %d %+v", rec.Code, e)
	}
	if got := files(); !slices.Equal(got, before) || metadata() != listed {
		t.Errorf("a refused group apply must install no payload of an earlier member: captures %v, before %v", got, before)
	}

	accepted := hubwire.CatalogueGroup{ID: "g", Version: 2, Title: "g", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s1"}, {SetID: "s3"}}}
	env.publishCatalogue(t, 2, []hubwire.CatalogueSet{s1, hubCatalogueMember(t, "s3", 1, "alice", "b.example")}, []hubwire.CatalogueGroup{accepted})
	resp := hubApplyGroupOK(t, env, "g", nil)
	if len(resp.Sets[0].Payloads) != 1 || !slices.Contains(files(), filepath.Base(resp.Sets[0].Payloads[0].File)) {
		t.Errorf("an accepted group apply installs its members' payloads, got %+v in %v", resp.Sets[0].Payloads, files())
	}
	if set := env.localSet(resp.Sets[0].ID); set.Faking.PayloadFile != resp.Sets[0].Payloads[0].File {
		t.Errorf("the member points at its installed payload, got %q", set.Faking.PayloadFile)
	}
}

func TestHubGroupApplyReportsASetIdleUnderTheWholeBlock(t *testing.T) {
	env := newHubEnv(t)
	pa := hubCatalogueMemberWith(t, "pa", 1, "alice", func(set *config.SetConfig) {
		set.Targets.ASNs = []string{"13335"}
		set.Targets.IPs = []string{"10.1.0.0/24"}
	})
	pb := hubCatalogueMemberWith(t, "pb", 1, "alice", func(set *config.SetConfig) {
		set.Targets.ASNs = []string{"15169"}
		set.Targets.IPs = []string{"10.2.0.0/24"}
	})
	group := hubwire.CatalogueGroup{ID: "g-asn", Version: 1, Title: "asn", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "pa"}, {SetID: "pb"}}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{pa, pb}, []hubwire.CatalogueGroup{group})

	local := func(id string, asns, ips []string) *config.SetConfig {
		set := hubGroupLocalSet(id, id, "")
		set.Targets.ASNs, set.Targets.IPs, set.Targets.IpsToMatch = asns, ips, ips
		return set
	}
	both := local("both", []string{"13335", "15169"}, nil)
	solo := local("solo", []string{"13335"}, nil)
	partial := local("partial", []string{"13335", "3356"}, nil)
	split := local("split", nil, []string{"10.1.0.0/24", "10.2.0.0/24"})
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{both, solo, partial, split} })
	countTableRefreshes(t)

	resp := hubApplyGroupOK(t, env, "g-asn", nil)
	type entry struct {
		asns []string
		idle bool
	}
	seen := map[string][]entry{}
	for _, member := range resp.Sets {
		for _, c := range member.Covered {
			seen[c.SetId] = append(seen[c.SetId], entry{c.ASNs, c.Idle})
		}
	}
	if got := seen["both"]; len(got) != 2 || !got[0].idle || !got[1].idle || !slices.Equal(got[0].asns, []string{"13335"}) || !slices.Equal(got[1].asns, []string{"15169"}) {
		t.Errorf("a set whose ASNs the members cover together is idle, each member listing its own ASN, got %+v", got)
	}
	if got := seen["solo"]; len(got) != 1 || !got[0].idle {
		t.Errorf("a set one member covers alone stays idle, got %+v", got)
	}
	if got := seen["partial"]; len(got) != 1 || got[0].idle {
		t.Errorf("a set with an ASN outside the block is not idle, got %+v", got)
	}
	if got := seen["split"]; len(got) != 1 || !got[0].idle || len(got[0].asns) != 0 {
		t.Errorf("a set no member covers alone but the block covers together is listed once as idle, got %+v", got)
	}
	if !slices.ContainsFunc(hubGroupAppliedCovered(resp.Sets[0]), func(id string) bool { return id == "split" }) {
		t.Errorf("the set idle only under the whole block is reported with the first member, got %+v", resp.Sets)
	}
}

func hubGroupAppliedCovered(member HubGroupAppliedSet) []string {
	ids := []string{}
	for _, c := range member.Covered {
		ids = append(ids, c.SetId)
	}
	return ids
}

func TestHubGroupUndoGivesTheMovedDomainsBack(t *testing.T) {
	env := newHubEnv(t)
	env.api.mux.HandleFunc("/api/sets/batch-delete", env.api.handleBatchDeleteSets)
	s1 := hubCatalogueMember(t, "s1", 1, "alice", "a.example", "shared.example")
	s2 := hubCatalogueMember(t, "s2", 1, "alice", "b.example", "shared.example")
	group := hubwire.CatalogueGroup{ID: "g", Version: 1, Title: "video", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s1"}, {SetID: "s2"}}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{s1, s2}, []hubwire.CatalogueGroup{group})
	old := hubGroupLocalSet("old", "Old", "", "shared.example", "a.example", "kept.example")
	other := hubGroupLocalSet("other", "Other", "", "b.example")
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{old, other} })
	countTableRefreshes(t)

	resp := hubApplyGroupOK(t, env, "g", nil)
	var moved []DomainReassignment
	for _, member := range resp.Sets {
		moved = append(moved, member.Moved...)
	}
	if got := env.localSet("old").Targets.SNIDomains; !slices.Equal(got, []string{"kept.example"}) {
		t.Fatalf("the apply must take the members' domains from Old, got %v", got)
	}
	later := hubGroupLocalSet("later", "Later", "", "a.example")
	env.update(func(cfg *config.Config) { cfg.Sets = append(cfg.Sets, later) })

	rec := postJSON(t, env.mux, "/api/sets/batch-delete", map[string]interface{}{"ids": resp.Created, "restore": append(moved, DomainReassignment{Domain: "gone.example", SetId: resp.Created[0]})})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	cfg := env.api.getCfg()
	if ids := hubGroupIDs(cfg.Sets); !slices.Equal(ids, []string{"old", "other", "later"}) {
		t.Fatalf("undo deletes the group's sets, got %v", ids)
	}
	if got := env.localSet("old").Targets.SNIDomains; !slices.Equal(got, []string{"kept.example", "shared.example", "a.example"}) {
		t.Errorf("undo gives the moved domains back to the set they came from, got %v", got)
	}
	if got := env.localSet("other").Targets.SNIDomains; !slices.Equal(got, []string{"b.example"}) {
		t.Errorf("undo gives b.example back once, got %v", got)
	}
	if got := env.localSet("later").Targets.SNIDomains; !slices.Equal(got, []string{"a.example"}) {
		t.Errorf("giving a domain back takes it from no other set, got %v", got)
	}
}

func TestHubGroupJointOwnerCountsOnlyWhatEveryMemberMatches(t *testing.T) {
	member := func(id, tls string, enabled bool, asn string) *config.SetConfig {
		set := hubGroupLocalSet(id, id, "")
		set.Enabled, set.Targets.TLSVersion, set.Targets.ASNs = enabled, tls, []string{asn}
		return set
	}
	below := hubGroupLocalSet("below", "below", "")
	ids := map[string]bool{"a": true, "b": true, "off": true}
	sets := []*config.SetConfig{member("a", "", true, "13335"), member("off", "", false, "3356"), member("b", "1.3", true, "15169"), below}
	out, ok := hubGroupJointOwner(sets, ids, "video")
	if !ok || len(out) != 2 || out[0].Id != hubGroupJointID || out[1] != below {
		t.Fatalf("the block collapses into one owner in its place, got %v %v", ok, hubGroupIDs(out))
	}
	if joint := out[0].Targets; joint.TLSVersion != "1.3" || !slices.Equal(joint.ASNs, []string{"13335", "15169"}) {
		t.Errorf("the joint owner takes the members' targets, the narrower TLS filter and nothing of a disabled member, got %+v", joint)
	}
	if _, ok := hubGroupJointOwner(append(sets, member("c", "1.2", true, "1")), map[string]bool{"a": true, "b": true, "c": true}, "video"); ok {
		t.Errorf("members with different TLS filters have no joint owner")
	}
}

func TestHubGroupRepublishKeepsTheVoteOfAnUnchangedMember(t *testing.T) {
	env := newHubEnv(t)
	a := hubGroupLocalSet("a", "A", "video", "a.example")
	b := hubGroupLocalSet("b", "B", "video", "b.example")
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{a, b} })
	hubGroupAnswer(env, 1, []hubwire.GroupMemberResult{{SetID: "s1", Version: 1}, {SetID: "s2", Version: 1}})
	if rec := postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video"}); rec.Code != http.StatusAccepted {
		t.Fatalf("publish v1: %d %s", rec.Code, rec.Body.String())
	}
	env.update(func(cfg *config.Config) {
		for _, set := range cfg.Sets {
			set.Hub.Vote, set.Hub.VotedAt = hubwire.VoteWorks, "2026-11-20T10:00:00Z"
		}
		cfg.Sets[0].Faking.TTL = 3
	})

	hubGroupAnswer(env, 2, []hubwire.GroupMemberResult{{SetID: "s1", Version: 2}, {SetID: "s2", Version: 1, Linked: true}})
	if rec := postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video"}); rec.Code != http.StatusAccepted {
		t.Fatalf("publish v2: %d %s", rec.Code, rec.Body.String())
	}
	if set := env.localSet("b"); set.Hub.Vote != hubwire.VoteWorks || set.Hub.VotedAt != "2026-11-20T10:00:00Z" || set.Hub.GroupVersion != 2 {
		t.Errorf("a member linked to the hub set and version it already had keeps its vote, got %+v", set.Hub)
	}
	if set := env.localSet("a"); set.Hub.Vote != "" || set.Hub.VotedAt != "" || set.Hub.Version != 2 {
		t.Errorf("a member published as a new version drops the vote of the old one, got %+v", set.Hub)
	}
}
