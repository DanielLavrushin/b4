package handler

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hub/hubtest"
	"github.com/daniellavrushin/b4/hubwire"
)

func hubGroupVoteEnv(t *testing.T) *hubEnv {
	t.Helper()
	env := newHubEnv(t)
	env.svc.SetNetwork("12389", "RU")
	s1 := hubCatalogueMember(t, "s1", 3, "alice", "a.example")
	s2 := hubCatalogueMember(t, "s2", 1, "alice", "b.example")
	g := hubwire.CatalogueGroup{ID: "G", Version: 2, Title: "video", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s2"}, {SetID: "s1"}}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{s1, s2}, []hubwire.CatalogueGroup{g})
	return env
}

func hubGroupVoteRecords(env *hubEnv) []hubwire.GroupVoteBody {
	var bodies []hubwire.GroupVoteBody
	for _, rec := range env.hub.Records() {
		var body hubwire.GroupVoteBody
		if rec.Kind == hubwire.RecordGroupVote && json.Unmarshal(rec.Body, &body) == nil {
			bodies = append(bodies, body)
		}
	}
	return bodies
}

func hubGroupCopyVotes(env *hubEnv, label string) []config.HubOrigin {
	var origins []config.HubOrigin
	for _, set := range env.api.getCfg().Sets {
		if set.Group == label && set.Hub != nil {
			origins = append(origins, *set.Hub)
		}
	}
	return origins
}

func TestHubGroupVoteNeedsTheListedVersionUnmodified(t *testing.T) {
	env := hubGroupVoteEnv(t)
	works := HubVoteRequest{Kind: hubwire.VoteWorks}

	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/missing/vote", works), http.StatusNotFound, "not_found")
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/G/vote", HubVoteRequest{Kind: "meh"}), http.StatusBadRequest, "bad_request")
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/G/vote", works), http.StatusConflict, "not_applied")

	hubApplyGroupOK(t, env, "G", nil)
	ttl := env.api.getCfg().Sets[0].Faking.TTL
	env.update(func(cfg *config.Config) { cfg.Sets[0].Faking.TTL = ttl + 1 })
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/G/vote", works), http.StatusConflict, "modified")
	env.update(func(cfg *config.Config) { cfg.Sets[0].Faking.TTL = ttl })

	env.update(func(cfg *config.Config) {
		for _, set := range cfg.Sets {
			set.Hub.GroupVersion = 3
		}
	})
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateApplied || got.Version != 3 {
		t.Fatalf("a copy ahead of the catalogue reads applied, got %+v", got)
	}
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/G/vote", works), http.StatusConflict, "other_version")
	env.update(func(cfg *config.Config) {
		for _, set := range cfg.Sets {
			set.Hub.GroupVersion = 2
		}
	})

	g3 := hubwire.CatalogueGroup{ID: "G", Version: 3, Title: "video", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s2"}, {SetID: "s1"}}}
	env.publishCatalogue(t, 2, []hubwire.CatalogueSet{hubCatalogueMember(t, "s1", 3, "alice", "a.example"), hubCatalogueMember(t, "s2", 1, "alice", "b.example")}, []hubwire.CatalogueGroup{g3})
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateUpdate {
		t.Fatalf("a copy behind the catalogue reads update, got %+v", got)
	}
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/G/vote", works), http.StatusConflict, "other_version")

	if got := hubGroupVoteRecords(env); len(got) != 0 {
		t.Errorf("a refused vote is never sent, got %+v", got)
	}
	for _, origin := range hubGroupCopyVotes(env, "video") {
		if origin.GroupVote != "" {
			t.Errorf("a refused vote is never remembered, got %+v", origin)
		}
	}
}

func TestHubGroupVoteSignsTheGroupAndItsMembersInOrder(t *testing.T) {
	env := hubGroupVoteEnv(t)
	hubApplyGroupOK(t, env, "G", nil)

	rec := postJSON(t, env.mux, "/api/hub/groups/G/vote", HubVoteRequest{Kind: " Works ", Domain: " Video.Example.COM. "})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	var voted HubVoteResponse
	decodeInto(t, rec, &voted)
	if !voted.Sent || voted.Queued {
		t.Errorf("the vote must be delivered while the hub answers: %+v", voted)
	}
	bodies := hubGroupVoteRecords(env)
	want := hubwire.GroupVoteBody{
		GroupID:     "G",
		Version:     2,
		Members:     []hubwire.GroupVoteMember{{SetID: "s2", Version: 1}, {SetID: "s1", Version: 3}},
		Kind:        hubwire.VoteWorks,
		Domain:      "video.example.com",
		ASNHint:     "12389",
		CountryHint: "RU",
		Engine:      env.api.getCfg().Queue.Mode,
		B4Version:   Version,
	}
	if len(bodies) != 1 || !reflect.DeepEqual(bodies[0], want) {
		t.Fatalf("the hub must receive one group vote\n got %+v\nwant %+v", bodies, want)
	}
	for _, rec := range env.hub.Records() {
		if rec.Kind == hubwire.RecordVote {
			t.Errorf("a group vote sends no set vote: %+v", rec)
		}
	}

	origins := hubGroupCopyVotes(env, "video")
	if len(origins) != 2 {
		t.Fatalf("the copy holds two sets, got %+v", origins)
	}
	for _, origin := range origins {
		if origin.GroupVote != hubwire.VoteWorks || origin.GroupVotedAt == "" || origin.Vote != "" || origin.VotedAt != "" {
			t.Errorf("every set of the copy remembers the group vote and no set vote, got %+v", origin)
		}
	}
	var view HubGroup
	decodeInto(t, getJSON(t, env.mux, "/api/hub/groups/G"), &view)
	if view.Applied.Vote != hubwire.VoteWorks || view.Applied.VotedAt != origins[0].GroupVotedAt {
		t.Errorf("the group reads back this device's vote, got %+v", view.Applied)
	}
	for _, m := range view.Members {
		if m.Set.Applied == nil || m.Set.Applied.Vote != "" || m.Set.Applied.GroupID != "G" {
			t.Errorf("a member set applied with the group names the group and keeps no vote of its own, got %+v", m.Set.Applied)
		}
	}

	env.hub.SetDown(true)
	rec = postJSON(t, env.mux, "/api/hub/groups/G/vote", HubVoteRequest{Kind: hubwire.VoteBroken})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	decodeInto(t, rec, &voted)
	if voted.Sent || !voted.Queued || env.svc.OutboxCount() != 1 {
		t.Errorf("a group vote must be queued while the hub is down: %+v outbox=%d", voted, env.svc.OutboxCount())
	}
	for _, origin := range hubGroupCopyVotes(env, "video") {
		if origin.GroupVote != hubwire.VoteBroken {
			t.Errorf("a queued vote is remembered, got %+v", origin)
		}
	}
	env.hub.SetDown(false)

	env.hub.SetAnswer(func(*hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: http.StatusForbidden, Body: map[string]string{"code": "banned", "error": "key banned"}}
	})
	rec = postJSON(t, env.mux, "/api/hub/groups/G/vote", HubVoteRequest{Kind: hubwire.VoteWorks})
	var remote HubRemoteError
	decodeInto(t, rec, &remote)
	if rec.Code != http.StatusForbidden || remote.Code != "banned" || remote.Message != "key banned" {
		t.Errorf("a hub refusal must pass through, got %d %+v", rec.Code, remote)
	}

	env.hub.SetAnswer(func(*hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: http.StatusBadRequest, Body: map[string]interface{}{"code": "bad_record", "error": "unknown record kind"}}
	})
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/G/vote", HubVoteRequest{Kind: hubwire.VoteWorks}), http.StatusBadRequest, "hub_no_group_votes")

	env.hub.SetAnswer(func(*hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: http.StatusBadRequest, Body: map[string]interface{}{"code": "bad_record", "error": "group vote body does not decode"}}
	})
	expectCode(t, postJSON(t, env.mux, "/api/hub/groups/G/vote", HubVoteRequest{Kind: hubwire.VoteWorks}), http.StatusBadRequest, "bad_record")

	for _, origin := range hubGroupCopyVotes(env, "video") {
		if origin.GroupVote != hubwire.VoteBroken {
			t.Errorf("a refused vote leaves the remembered one, got %+v", origin)
		}
	}
}

func TestHubGroupShowsTheGroupScoreNotItsMembers(t *testing.T) {
	env := newHubEnv(t)
	env.svc.SetNetwork("12389", "RU")
	s1 := hubCatalogueMember(t, "s1", 1, "alice", "a.example")
	scored := hubwire.CatalogueGroup{ID: "G", Version: 1, Title: "video", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s1"}}, Scores: hubwire.Scores{
		Global: hubwire.Score{Score: 0.5, N: 6, Devices: 4},
		ASN:    map[string]hubwire.Score{"12389": {Score: 0.8, N: 3, Devices: 2, Newest: "2026-11-27T10:00:00Z"}},
	}}
	plain := hubwire.CatalogueGroup{ID: "H", Version: 1, Title: "plain", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s1"}}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{s1}, []hubwire.CatalogueGroup{scored, plain})

	var view HubGroup
	decodeInto(t, getJSON(t, env.mux, "/api/hub/groups/G"), &view)
	if want := (hubwire.Displayed{Bucket: hubwire.BucketASN, Score: 0.8, N: 3, Devices: 2, Newest: "2026-11-27T10:00:00Z"}); view.Display != want {
		t.Errorf("the group shows its own score for this network, got %+v want %+v", view.Display, want)
	}
	if view.Members[0].Set.Display.Bucket != hubwire.BucketNone {
		t.Errorf("a member keeps its own score, got %+v", view.Members[0].Set.Display)
	}
	var groups HubGroupsResponse
	decodeInto(t, getJSON(t, env.mux, "/api/hub/groups"), &groups)
	for _, g := range groups.Groups {
		if g.ID == "H" && g.Display.Bucket != hubwire.BucketNone {
			t.Errorf("a group without votes shows no score, got %+v", g.Display)
		}
		if g.ID == "G" && g.Display.Bucket != hubwire.BucketASN {
			t.Errorf("the list carries the group score too, got %+v", g.Display)
		}
	}
}

func TestHubAppliedNamesTheGroupOnlyForASetInALocalGroup(t *testing.T) {
	env := hubGroupVoteEnv(t)
	hubApplyGroupOK(t, env, "G", nil)
	env.update(func(cfg *config.Config) { cfg.Sets[0].Group = "" })

	var view HubGroup
	decodeInto(t, getJSON(t, env.mux, "/api/hub/groups/G"), &view)
	byHubID := map[string]*HubApplied{}
	for _, m := range view.Members {
		byHubID[m.Set.ID] = m.Set.Applied
	}
	if got := byHubID["s2"]; got == nil || got.GroupID != "" {
		t.Errorf("a set taken out of its local group is rated on its own, got %+v", got)
	}
	if got := byHubID["s1"]; got == nil || got.GroupID != "G" {
		t.Errorf("a set in the local copy names the hub group, got %+v", got)
	}
}

func TestHubGroupApplyKeepsTheGroupVoteOnlyForTheSameVersion(t *testing.T) {
	env := hubGroupVoteEnv(t)
	hubApplyGroupOK(t, env, "G", nil)
	if rec := postJSON(t, env.mux, "/api/hub/groups/G/vote", HubVoteRequest{Kind: hubwire.VoteWorks}); rec.Code != http.StatusAccepted {
		t.Fatalf("vote: %d %s", rec.Code, rec.Body.String())
	}

	env.update(func(cfg *config.Config) { cfg.Sets[0].Faking.TTL++ })
	hubApplyGroupOK(t, env, "G", map[string]interface{}{"replace": true})
	for _, origin := range hubGroupCopyVotes(env, "video") {
		if origin.GroupVote != hubwire.VoteWorks || origin.GroupVersion != 2 {
			t.Errorf("reapplying the version the vote was cast on keeps the vote, got %+v", origin)
		}
	}

	g3 := hubwire.CatalogueGroup{ID: "G", Version: 3, Title: "video", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s2"}}}
	env.publishCatalogue(t, 2, []hubwire.CatalogueSet{hubCatalogueMember(t, "s1", 3, "alice", "a.example"), hubCatalogueMember(t, "s2", 1, "alice", "b.example")}, []hubwire.CatalogueGroup{g3})
	resp := hubApplyGroupOK(t, env, "G", map[string]interface{}{"replace": true})
	if len(resp.Ungrouped) != 1 {
		t.Fatalf("the member the group no longer lists leaves it: %+v", resp)
	}
	for _, set := range env.api.getCfg().Sets {
		switch {
		case set.Id == resp.Ungrouped[0]:
			if set.Hub.GroupID != "" || set.Hub.GroupVote != "" || set.Hub.GroupVotedAt != "" {
				t.Errorf("a set taken out of the group drops the group vote with the stamp, got %+v", set.Hub)
			}
		case set.Hub.ID == "s2":
			if set.Hub.GroupVersion != 3 || set.Hub.GroupVote != "" || set.Hub.GroupVotedAt != "" {
				t.Errorf("updating to a new group version drops the vote cast on the old one, got %+v", set.Hub)
			}
		}
	}
	if got := hubGroupStateOf(t, env, "G"); got.Vote != "" || got.VotedAt != "" {
		t.Errorf("the updated group reads no vote, got %+v", got)
	}
}

func hubGroupLocalOf(env *hubEnv, hubID string) *config.SetConfig {
	for _, set := range env.api.getCfg().Sets {
		if set.Hub != nil && set.Hub.ID == hubID {
			return set
		}
	}
	return nil
}

func TestHubGroupEscalationsOfTheCopyMustMatchTheListing(t *testing.T) {
	env := newHubEnv(t)
	s1 := hubCatalogueMember(t, "s1", 1, "alice", "a.example")
	s2 := hubCatalogueMember(t, "s2", 1, "alice", "b.example")
	s3 := hubCatalogueMember(t, "s3", 1, "alice", "c.example")
	g := hubwire.CatalogueGroup{ID: "G", Version: 1, Title: "video", Author: "alice", Members: []hubwire.CatalogueGroupMember{
		{SetID: "s1", Escalate: &hubwire.GroupEscalation{To: 1, RstThreshold: 5}},
		{SetID: "s2"},
		{SetID: "s3"},
	}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{s1, s2, s3}, []hubwire.CatalogueGroup{g})
	fallback := hubGroupLocalSet("fallback", "Fallback", "", "z.example")
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{fallback} })
	countTableRefreshes(t)
	hubApplyGroupOK(t, env, "G", nil)
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateApplied {
		t.Fatalf("a freshly applied copy reads applied, got %+v", got)
	}
	first, second, third := hubGroupLocalOf(env, "s1").Id, hubGroupLocalOf(env, "s2").Id, hubGroupLocalOf(env, "s3").Id
	listed := hubGroupLocalOf(env, "s1").Escalate
	works := HubVoteRequest{Kind: hubwire.VoteWorks}

	edits := []struct {
		name string
		edit func(map[string]*config.SetConfig)
	}{
		{"the listed link removed", func(sets map[string]*config.SetConfig) { sets[first].Escalate = config.DefaultSetConfig.Escalate }},
		{"a threshold of the listed link changed", func(sets map[string]*config.SetConfig) { sets[first].Escalate.RstThreshold = 6 }},
		{"the listed link pointed at another member", func(sets map[string]*config.SetConfig) { sets[first].Escalate.To = third }},
		{"the listed link pointed outside the copy", func(sets map[string]*config.SetConfig) { sets[first].Escalate.To = "fallback" }},
		{"a link the group does not have added between members", func(sets map[string]*config.SetConfig) { sets[second].Escalate.To = third }},
	}
	for _, e := range edits {
		t.Run(e.name, func(t *testing.T) {
			t.Cleanup(func() {
				env.update(func(cfg *config.Config) {
					for _, set := range cfg.Sets {
						switch set.Id {
						case first:
							set.Escalate = listed
						case second:
							set.Escalate = config.DefaultSetConfig.Escalate
						}
					}
				})
			})
			env.update(func(cfg *config.Config) {
				byID := map[string]*config.SetConfig{}
				for _, set := range cfg.Sets {
					byID[set.Id] = set
				}
				e.edit(byID)
			})
			if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateModified {
				t.Errorf("reads modified, got %+v", got)
			}
			expectCode(t, postJSON(t, env.mux, "/api/hub/groups/G/vote", works), http.StatusConflict, "modified")
		})
	}
	if got := hubGroupVoteRecords(env); len(got) != 0 {
		t.Fatalf("a vote on a copy with other links is never sent, got %+v", got)
	}

	env.update(func(cfg *config.Config) {
		for _, set := range cfg.Sets {
			if set.Id == third {
				set.Escalate = config.EscalateConfig{To: "fallback", RstThreshold: 7}
			}
		}
	})
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateApplied {
		t.Errorf("a member's own link to a set outside the copy keeps the copy applied, got %+v", got)
	}
	if rec := postJSON(t, env.mux, "/api/hub/groups/G/vote", works); rec.Code != http.StatusAccepted {
		t.Errorf("a vote on that copy is accepted, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHubGroupPublisherReadsAppliedWithAClampedEscalation(t *testing.T) {
	env := newHubEnv(t)
	a := hubGroupLocalSet("a", "A", "video", "a.example")
	a.Escalate = config.EscalateConfig{To: "b", RstThreshold: 50, StallTimeoutMs: 99999}
	b := hubGroupLocalSet("b", "B", "video", "b.example")
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{a, b} })
	hubGroupAnswer(env, 1, []hubwire.GroupMemberResult{{SetID: "s1", Version: 1}, {SetID: "s2", Version: 1}})
	if rec := postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video"}); rec.Code != http.StatusAccepted {
		t.Fatalf("publish v1: %d %s", rec.Code, rec.Body.String())
	}
	esc := hubGroupShareBody(t, env).Members[0].Escalate
	if esc == nil || esc.To != 1 || esc.RstThreshold != 20 || esc.StallTimeoutMs != 15000 {
		t.Fatalf("the published link is clamped, got %+v", esc)
	}
	g1 := hubwire.CatalogueGroup{ID: "G", Version: 1, Title: "video", Author: "me", Members: []hubwire.CatalogueGroupMember{{SetID: "s1", Escalate: esc}, {SetID: "s2"}}}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{hubCatalogueMember(t, "s1", 1, "me", "a.example"), hubCatalogueMember(t, "s2", 1, "me", "b.example")}, []hubwire.CatalogueGroup{g1})
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateApplied {
		t.Fatalf("the publisher's copy with thresholds above the limits reads applied once listed, got %+v", got)
	}

	env.update(func(cfg *config.Config) { cfg.Sets[0].Escalate = config.DefaultSetConfig.Escalate })
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateModified {
		t.Fatalf("the publisher's copy without the listed link reads modified, got %+v", got)
	}
	hubGroupAnswer(env, 2, []hubwire.GroupMemberResult{{SetID: "s1", Version: 1, Linked: true}, {SetID: "s2", Version: 1, Linked: true}})
	if rec := postJSON(t, env.mux, "/api/hub/groups/share", HubGroupShareRequest{Group: "video"}); rec.Code != http.StatusAccepted {
		t.Fatalf("publish v2: %d %s", rec.Code, rec.Body.String())
	}
	if got := hubGroupStateOf(t, env, "G"); got.State != HubGroupStateApplied || got.Version != 2 {
		t.Errorf("a copy published ahead of the listing is not held to the listed links, got %+v", got)
	}
}

func TestHubAppliedNamesOnlyAGroupTheHubLists(t *testing.T) {
	env := hubGroupVoteEnv(t)
	hubApplyGroupOK(t, env, "G", nil)
	groupOf := func() string {
		t.Helper()
		var set HubSet
		decodeInto(t, getJSON(t, env.mux, "/api/hub/sets/s1"), &set)
		if set.Applied == nil {
			t.Fatalf("s1 is applied, got %+v", set)
		}
		return set.Applied.GroupID
	}
	if got := groupOf(); got != "G" {
		t.Errorf("a set of a listed group names the group, got %q", got)
	}
	s1 := hubCatalogueMember(t, "s1", 3, "alice", "a.example")
	s2 := hubCatalogueMember(t, "s2", 1, "alice", "b.example")
	env.publishCatalogue(t, 2, []hubwire.CatalogueSet{s1, s2}, nil)
	if got := groupOf(); got != "" {
		t.Errorf("a set of a group the hub no longer lists is rated on its own, got %q", got)
	}
	broken := hubwire.CatalogueGroup{ID: "G", Version: 2, Title: "video", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "s2"}, {SetID: "s1"}, {SetID: "gone"}}}
	env.publishCatalogue(t, 3, []hubwire.CatalogueSet{s1, s2}, []hubwire.CatalogueGroup{broken})
	if got := groupOf(); got != "" {
		t.Errorf("a set of a group that does not resolve is rated on its own, got %q", got)
	}
	var list HubSetsResponse
	decodeInto(t, getJSON(t, env.mux, "/api/hub/sets"), &list)
	if len(list.Sets) != 2 {
		t.Fatalf("both sets are listed on their own, got %+v", list.Sets)
	}
	for _, set := range list.Sets {
		if set.Applied == nil || set.Applied.GroupID != "" {
			t.Errorf("the list names no unlisted group, got %+v", set.Applied)
		}
	}
}

func TestASetMovedOutOfTheGroupsCopyIsRatedOnItsOwn(t *testing.T) {
	env := hubGroupVoteEnv(t)
	hubApplyGroupOK(t, env, "G", nil)
	ids := map[string]string{}
	for _, set := range env.api.getCfg().Sets {
		if set.Hub != nil && set.Hub.GroupID == "G" {
			ids[set.Hub.ID] = set.Id
		}
	}
	if len(ids) < 2 {
		t.Fatalf("the group copy has its sets: %v", ids)
	}
	env.update(func(cfg *config.Config) {
		for _, set := range cfg.Sets {
			if set.Id == ids["s1"] {
				set.Group = "elsewhere"
			}
		}
	})
	copied := map[string]bool{}
	for _, set := range hubGroupCopyOf(env.api.getCfg().Sets, "G").sets {
		copied[set.Hub.ID] = true
	}
	var list HubSetsResponse
	decodeInto(t, getJSON(t, env.mux, "/api/hub/sets?limit=50"), &list)
	for hubID := range ids {
		var set HubSet
		decodeInto(t, getJSON(t, env.mux, "/api/hub/sets/"+hubID), &set)
		if set.Applied == nil {
			t.Fatalf("%s is applied, got %+v", hubID, set)
		}
		if rated := set.Applied.GroupID == "G"; rated != copied[hubID] {
			t.Errorf("%s: rated through the group %v, but in the group's local copy %v; a set outside the copy gets no group vote, so it must keep its own", hubID, rated, copied[hubID])
		}
	}
}

func TestHubGroupVoteStampsOnlyTheCopyItSent(t *testing.T) {
	env := hubGroupVoteEnv(t)
	hubApplyGroupOK(t, env, "G", nil)
	one, two := hubGroupLocalOf(env, "s1").Id, hubGroupLocalOf(env, "s2").Id
	var during func(set *config.SetConfig)
	env.hub.SetAnswer(func(rec *hubwire.Record) hubtest.Answer {
		if rec.Kind == hubwire.RecordGroupVote && during != nil {
			env.update(func(cfg *config.Config) {
				for _, set := range cfg.Sets {
					during(set)
				}
			})
		}
		return hubtest.Answer{Status: http.StatusAccepted, Body: map[string]interface{}{"id": rec.ID(), "kind": rec.Kind}}
	})

	during = func(set *config.SetConfig) {
		if set.Id == two {
			set.Hub.Version = 5
		}
	}
	if rec := postJSON(t, env.mux, "/api/hub/groups/G/vote", HubVoteRequest{Kind: hubwire.VoteWorks}); rec.Code != http.StatusAccepted {
		t.Fatalf("vote: %d %s", rec.Code, rec.Body.String())
	}
	if got := env.localSet(two).Hub; got.GroupVote != "" || got.GroupVotedAt != "" {
		t.Errorf("a set moved to another version of its hub set during the send keeps no vote, got %+v", got)
	}
	if got := env.localSet(one).Hub; got.GroupVote != hubwire.VoteWorks {
		t.Errorf("a set the vote named is stamped, got %+v", got)
	}

	during = nil
	env.update(func(cfg *config.Config) {
		for _, set := range cfg.Sets {
			if set.Id == two {
				set.Hub.Version = 1
			}
		}
	})
	during = func(set *config.SetConfig) {
		if set.Id == one {
			set.Hub.GroupVersion = 3
		}
	}
	if rec := postJSON(t, env.mux, "/api/hub/groups/G/vote", HubVoteRequest{Kind: hubwire.VoteBroken}); rec.Code != http.StatusAccepted {
		t.Fatalf("vote: %d %s", rec.Code, rec.Body.String())
	}
	if got := env.localSet(one).Hub; got.GroupVote != hubwire.VoteWorks {
		t.Errorf("a set moved to another group version during the send keeps its earlier vote, got %+v", got)
	}
	if got := env.localSet(two).Hub; got.GroupVote != hubwire.VoteBroken {
		t.Errorf("a set the vote named is stamped, got %+v", got)
	}
}
