package catalogue

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
)

var scoredAt = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

func castGroupVote(t *testing.T, st *store.Store, groupID, gfp, key, kind, asn, cc string) {
	t.Helper()
	at := scoredAt.Add(-time.Hour)
	if err := st.UpsertGroupVote(context.Background(), store.Vote{
		RecordID: key + "-" + gfp, SetID: groupID, Version: 1, FP: gfp, KeyHMAC: key, Kind: kind, Weight: score.Weights[kind],
		ASNObserved: asn, CountryObserved: cc, OriginVerified: true, Bucket: score.BucketOf(at), ReceivedAt: at,
	}); err != nil {
		t.Fatal(err)
	}
}

func groupByID(t *testing.T, groups []hubwire.CatalogueGroup, id string) hubwire.CatalogueGroup {
	t.Helper()
	for _, g := range groups {
		if g.ID == id {
			return g
		}
	}
	t.Fatalf("group %s is not listed: %+v", id, groups)
	return hubwire.CatalogueGroup{}
}

func TestAListedGroupCarriesTheScoreOfItsGroupVotes(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, memberA, "fp-a")
	addActiveSet(t, st, memberB, "fp-b")
	escalate := &hubwire.GroupEscalation{To: 1, RstThreshold: 3}
	addGroup(t, st, groupOne, "author-group", hubwire.SetStatusActive, store.GroupMember{SetID: memberA, SetVersion: 1, Escalate: escalate}, store.GroupMember{SetID: memberB, SetVersion: 1})
	addGroup(t, st, groupTwo, "author-group", hubwire.SetStatusActive, store.GroupMember{SetID: memberB, SetVersion: 1})
	gfp := hubwire.GroupFingerprint([]hubwire.GroupFingerprintMember{{FP: "fp-a", Escalate: escalate}, {FP: "fp-b"}})

	castGroupVote(t, st, groupOne, gfp, "voter-1", score.KindManualWorks, "64500", "RU")
	castGroupVote(t, st, groupOne, gfp, "voter-2", score.KindManualWorks, "64500", "RU")
	castGroupVote(t, st, groupOne, gfp, "voter-3", score.KindManualBroken, "64501", "DE")
	castGroupVote(t, st, groupOne, gfp, "voter-4", score.KindManualWorks, "64502", "NL")
	castGroupVote(t, st, groupOne, gfp, "author-group", score.KindUpload, "64502", "NL")
	castGroupVote(t, st, groupOne, gfp, "banned", score.KindManualBroken, "64500", "RU")
	if _, _, err := st.TouchKey(ctx, "banned", scoredAt.AddDate(0, -1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := st.BanKey(ctx, "banned", "spam", scoredAt); err != nil {
		t.Fatal(err)
	}
	for asn, name := range map[string]string{"64500": "EXAMPLE-A", "64501": "EXAMPLE-B", "64502": "EXAMPLE-C"} {
		if err := st.UpsertASNName(ctx, asn, name, "", scoredAt); err != nil {
			t.Fatal(err)
		}
	}

	result, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scores := groupByID(t, result.Catalogue.Groups, groupOne).Scores
	if scores.Global.Devices != 5 {
		t.Errorf("every voter but the banned key counts worldwide, the author included, got %+v", scores.Global)
	}
	if len(scores.ASN) != 1 || scores.ASN["64500"].Devices != 2 {
		t.Errorf("only the network with two independent devices is listed; a single device and the author stay out, got %+v", scores.ASN)
	}
	if len(scores.CC) != 1 || scores.CC["RU"].Devices != 2 {
		t.Errorf("the same holds per country, got %+v", scores.CC)
	}
	if !reflect.DeepEqual(result.Catalogue.ASNNames, map[string]string{"64500": "EXAMPLE-A"}) {
		t.Errorf("asn_names covers the networks the group scores name, got %v", result.Catalogue.ASNNames)
	}
	if unvoted := groupByID(t, result.Catalogue.Groups, groupTwo).Scores; !reflect.DeepEqual(unvoted, result.Catalogue.Sets[0].Scores) {
		t.Errorf("a group without votes carries the empty score of a set without votes, got %+v and %+v", unvoted, result.Catalogue.Sets[0].Scores)
	}
	for _, cs := range result.Catalogue.Sets {
		if cs.Scores.Global.Devices != 0 {
			t.Errorf("group votes never reach a member set's score, got %s %+v", cs.ID, cs.Scores.Global)
		}
	}
}

func TestTheAuthorsVoteThroughATwinGroupStaysOutOfTheNetworkCells(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, memberA, "fp-a")
	addActiveSet(t, st, memberB, "fp-b")
	gfp := hubwire.GroupFingerprint([]hubwire.GroupFingerprintMember{{FP: "fp-a"}, {FP: "fp-b"}})
	for id, author := range map[string]string{groupOne: "author-group", groupTwo: "copier"} {
		if err := st.Update(ctx, func(tx *store.Tx) error {
			v := &store.GroupVersion{Title: "group " + id, Status: hubwire.SetStatusPending, UploaderHMAC: author, GFP: gfp, CreatedAt: scoredAt, UpdatedAt: scoredAt,
				Members: []store.GroupMember{{SetID: memberA, SetVersion: 1}, {SetID: memberB, SetVersion: 1}}}
			if err := tx.CreateGroup(ctx, store.Group{ID: id, AuthorHMAC: author, CreatedAt: scoredAt, UpdatedAt: scoredAt}, v); err != nil {
				return err
			}
			return tx.SetGroupStatus(ctx, id, 1, hubwire.SetStatusActive, "", scoredAt)
		}); err != nil {
			t.Fatal(err)
		}
	}
	castGroupVote(t, st, groupOne, gfp, "voter-1", score.KindManualWorks, "64500", "RU")
	castGroupVote(t, st, groupTwo, gfp, "author-group", score.KindManualWorks, "64500", "RU")

	groups := listedGroups(t, b)
	for _, id := range []string{groupOne, groupTwo} {
		scores := groupByID(t, groups, id).Scores
		if scores.Global.Devices != 2 || len(scores.ASN) != 0 || len(scores.CC) != 0 {
			t.Errorf("%s: the author's vote through a twin counts worldwide only and fills no network or country cell, got %+v", id, scores)
		}
	}
}

func TestAGroupKeepsItsScoreUntilAMemberStrategyChanges(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, memberA, "fp-a")
	addActiveSet(t, st, memberB, "fp-b")
	members := []store.GroupMember{{SetID: memberA, SetVersion: 1}, {SetID: memberB, SetVersion: 1}}
	addGroup(t, st, groupOne, "author-group", hubwire.SetStatusActive, members...)
	gfp := hubwire.GroupFingerprint([]hubwire.GroupFingerprintMember{{FP: "fp-a"}, {FP: "fp-b"}})
	castGroupVote(t, st, groupOne, gfp, "voter-1", score.KindManualWorks, "64500", "RU")
	castGroupVote(t, st, groupOne, gfp, "voter-2", score.KindManualWorks, "64501", "DE")
	if g := groupByID(t, listedGroups(t, b), groupOne); g.Version != 1 || g.Scores.Global.Devices != 2 {
		t.Fatalf("the first version carries both votes, got %+v", g)
	}

	if err := st.Update(ctx, func(tx *store.Tx) error {
		v := &store.GroupVersion{GroupID: groupOne, Title: "group " + groupOne, Description: "new words", Status: hubwire.SetStatusPending, UploaderHMAC: "author-group", CreatedAt: scoredAt, UpdatedAt: scoredAt, Members: members}
		if err := tx.AddGroupVersion(ctx, v); err != nil {
			return err
		}
		return tx.SetGroupStatus(ctx, groupOne, v.Version, hubwire.SetStatusActive, "", scoredAt)
	}); err != nil {
		t.Fatal(err)
	}
	if g := groupByID(t, listedGroups(t, b), groupOne); g.Version != 2 || g.Description != "new words" || g.Scores.Global.Devices != 2 {
		t.Fatalf("a version that only rewords the group keeps its score, got %+v", g)
	}

	next := &store.Version{SetID: memberA, FP: "fp-a2", TargetsKey: "targets-" + memberA, Title: "set " + memberA, Projection: map[string]interface{}{}, Status: hubwire.SetStatusPending, CreatedAt: scoredAt, UpdatedAt: scoredAt}
	if err := st.AddVersion(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err := st.Approve(ctx, memberA, next.Version, scoredAt); err != nil {
		t.Fatal(err)
	}
	if g := groupByID(t, listedGroups(t, b), groupOne); g.Scores.Global.Devices != 0 || g.Scores.Global.N != 0 {
		t.Fatalf("a changed member strategy starts a new score, got %+v", g.Scores)
	}
}

func TestListedGroupFPFollowsTheMemberOrderAndEscalation(t *testing.T) {
	fps := map[string]string{memberA: "FP-A", memberB: "fp-b"}
	escalate := &hubwire.GroupEscalation{To: 1}
	got := ListedGroupFP([]hubwire.CatalogueGroupMember{{SetID: memberA, Escalate: escalate}, {SetID: memberB}}, fps)
	if got != hubwire.GroupFingerprint([]hubwire.GroupFingerprintMember{{FP: "fp-a", Escalate: escalate}, {FP: "fp-b"}}) {
		t.Error("the listed group fingerprint is the wire fingerprint of the listed member strategies")
	}
	if got == ListedGroupFP([]hubwire.CatalogueGroupMember{{SetID: memberB}, {SetID: memberA, Escalate: escalate}}, fps) ||
		got == ListedGroupFP([]hubwire.CatalogueGroupMember{{SetID: memberA}, {SetID: memberB}}, fps) {
		t.Error("the member order and the escalation are part of it")
	}
}
