package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func (f *fixture) groupVoteAs(voter *hubwire.Identity, groupID string, members []hubwire.GroupMemberResult, address string) {
	f.t.Helper()
	body := hubwire.GroupVoteBody{GroupID: groupID, Version: 1, Kind: hubwire.VoteWorks}
	for _, m := range members {
		body.Members = append(body.Members, hubwire.GroupVoteMember{SetID: m.SetID, Version: m.Version})
	}
	resp := f.ingest.Handle(context.Background(), testkit.Sign(f.t, voter, hubwire.RecordGroupVote, body, f.clock), parseIP(address))
	if resp.Status != http.StatusAccepted {
		f.t.Fatalf("group vote: %d %v", resp.Status, resp.Body)
	}
}

func (f *fixture) groupDetail(id string) GroupDetailView {
	f.t.Helper()
	var detail GroupDetailView
	f.admin(http.MethodGet, PathAPI+"/groups/"+id, nil).decode(f.t, &detail)
	return detail
}

func TestAGroupDetailCarriesThePublishedAndTheLiveGroupScore(t *testing.T) {
	f := newFixture(t, password)
	author := testkit.Identity(t)
	id, members := f.group(author, "media", authorAddress, testkit.SampleSet("Video", "video.example"), testkit.SampleSet("Music", "music.example"))
	if d := f.groupDetail(id); d.Published != nil || d.Live.Devices != 0 || d.Evidence.Author.Works != 0 {
		t.Fatalf("an unlisted group has no score yet: %+v", d)
	}
	f.expectStatus(f.groupAction(id, ActionApprove, GroupActionRequest{ExpectStatus: "pending"}), http.StatusOK)
	first, second := testkit.Identity(t), testkit.Identity(t)
	f.groupVoteAs(first, id, members, voterAddress)
	f.groupVoteAs(second, id, members, otherAddress)
	f.build()

	d := f.groupDetail(id)
	if d.PublishedVersion != 1 || d.Published == nil || d.Published.Devices != 3 || d.Live.Devices != 3 || d.Published.Score != d.Live.Score {
		t.Fatalf("the published score counts the author's upload and both votes, the live one agrees: %+v %+v", d.Published, d.Live)
	}
	if ev := d.Evidence; ev.Author.Works != 1 || ev.Author.Devices != 1 || ev.Independent.Works != 2 || ev.Independent.Devices != 2 || ev.Independent.Broken != 0 || ev.Pooled != 0 {
		t.Fatalf("the evidence splits the author from the independent voters: %+v", ev)
	}

	f.groupVoteAs(testkit.Identity(t), id, members, thirdAddress)
	if d := f.groupDetail(id); d.Published.Devices != 3 || d.Live.Devices != 4 || d.Evidence.Independent.Devices != 3 {
		t.Fatalf("a vote after the build moves the live score only: %+v %+v %+v", d.Published, d.Live, d.Evidence)
	}

	var key KeyDetailView
	f.admin(http.MethodGet, PathAPI+"/keys/"+f.keyOf(first), nil).decode(t, &key)
	if len(key.Days) != 1 || key.Days[0].Vote != 1 || len(key.Kinds) != 1 || key.Kinds[0].Kind != hubwire.RecordGroupVote {
		t.Fatalf("the key's activity counts its group vote as a vote: %+v %+v", key.Days, key.Kinds)
	}
}

func TestAGroupDrawerSplitsTheAuthorFromTheVotersByTheFlagTheScoresUse(t *testing.T) {
	f := newFixture(t, password)
	ctx := context.Background()
	author, copier := testkit.Identity(t), testkit.Identity(t)
	sets := func() []config.SetConfig {
		return []config.SetConfig{testkit.SampleSet("Video", "video.example"), testkit.SampleSet("Music", "music.example")}
	}
	id, members := f.group(author, "media", authorAddress, sets()...)
	f.expectStatus(f.groupAction(id, ActionApprove, GroupActionRequest{ExpectStatus: "pending"}), http.StatusOK)
	twin, _ := f.group(copier, "media", otherAddress, sets()...)
	f.expectStatus(f.groupAction(twin, ActionApprove, GroupActionRequest{ExpectStatus: "pending"}), http.StatusOK)

	music := testkit.SampleSet("Music", "music.example")
	music.Faking.TTL = 9
	music.Hub = &config.HubOrigin{ID: members[1].SetID, Version: 1}
	if resp := f.ingest.Handle(ctx, testkit.SignShare(t, author, testkit.BuildEnvelope(t, &music), f.clock), parseIP(authorAddress)); resp.Status != http.StatusAccepted || resp.Body["version"] != 2 {
		t.Fatalf("the author re-versions a member outside the group: %d %v", resp.Status, resp.Body)
	}
	if err := f.store.Approve(ctx, members[1].SetID, 2, f.clock); err != nil {
		t.Fatal(err)
	}
	moved := []hubwire.GroupMemberResult{members[0], {SetID: members[1].SetID, Version: 2}}
	f.groupVoteAs(author, twin, moved, authorAddress)
	f.groupVoteAs(testkit.Identity(t), id, moved, voterAddress)
	f.build()

	gfp := hubwire.GroupFingerprint([]hubwire.GroupFingerprintMember{{FP: f.versionOf(moved[0].SetID, 1).FP}, {FP: f.versionOf(moved[1].SetID, 2).FP}})
	pool, err := f.store.ScoringGroupVotesByGFP(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var flagged, independent int
	ownKey := false
	for _, v := range pool[gfp] {
		if v.ByAuthor {
			flagged++
		} else {
			independent++
		}
		ownKey = ownKey || v.KeyHMAC == f.keyOf(author)
	}
	if !ownKey || flagged+independent != 2 {
		t.Fatalf("the listed fingerprint holds the group author's vote through the twin and one other vote, got %+v", pool[gfp])
	}
	if ev := f.groupDetail(id).Evidence; ev.Author.Works != flagged || ev.Independent.Works != independent {
		t.Fatalf("the drawer puts on the author's side exactly the votes the scores keep out of the network cells, want %d and %d, got %+v", flagged, independent, ev)
	}
}
