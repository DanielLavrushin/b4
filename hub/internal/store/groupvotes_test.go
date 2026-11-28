package store

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
)

func groupVoteAt(key, recordID, asn string, at time.Time) Vote {
	return Vote{RecordID: recordID, SetID: "group", Version: 1, FP: "gfp", KeyHMAC: key, Kind: "manual_works", Weight: 1,
		ASNObserved: asn, CountryObserved: "RU", OriginVerified: true, B4Version: "1.90.0", Engine: "tun", Bucket: at.Unix() / int64(7*24*3600), ReceivedAt: at}
}

func TestPruneVotesAlsoDropsOldGroupVotesAndUnlinksTheirRecords(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	keep := 365 * 24 * time.Hour
	old := testNow.Add(-keep - time.Second)
	for _, v := range []Vote{groupVoteAt("key-old", "group-vote-old", "64500", old), groupVoteAt("key-new", "group-vote-new", "64500", testNow)} {
		if err := st.UpsertGroupVote(ctx, v); err != nil {
			t.Fatal(err)
		}
		if err := st.InsertRecord(ctx, Record{ID: v.RecordID, Kind: hubwire.RecordGroupVote, KeyHMAC: v.KeyHMAC, SetID: "group", Version: 1, ReceivedAt: v.ReceivedAt}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.UpsertVote(ctx, Vote{RecordID: "vote-old", SetID: "set", Version: 1, FP: "fp", KeyHMAC: "key-old", Kind: "manual_works", Weight: 1, Bucket: 1, ReceivedAt: old}); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkBuilt(ctx, testNow); err != nil {
		t.Fatal(err)
	}

	removed, err := st.PruneVotes(ctx, testNow, keep)
	if err != nil || removed != 2 {
		t.Fatalf("the old set vote and the old group vote go, got %d %v", removed, err)
	}
	pool, err := st.ScoringGroupVotesByGFP(ctx)
	if err != nil || len(pool["gfp"]) != 1 || pool["gfp"][0].RecordID != "group-vote-new" {
		t.Fatalf("only the young group vote stays, got %+v %v", pool, err)
	}
	for id, group := range map[string]string{"group-vote-old": "", "group-vote-new": "group"} {
		r, err := st.GetRecord(ctx, id)
		if err != nil || r.SetID != group || (group == "") != (r.Version == 0) {
			t.Errorf("%s: want group %q, got %+v %v", id, group, r, err)
		}
	}
	if dirty, _ := st.Dirty(ctx); dirty {
		t.Error("pruning group votes forces no build either")
	}
}

func TestAGroupVoteIsTheAuthorsWhenItsKeyFiledThatGroupFingerprintOrWroteTheNamedGroup(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	for _, g := range []struct{ id, author, gfp, status string }{
		{"group", "author", "gfp", hubwire.SetStatusActive},
		{"twin", "copier", "gfp", hubwire.SetStatusActive},
		{"spurned", "spurner", "spurned-gfp", hubwire.SetStatusRejected},
	} {
		if err := st.Update(ctx, func(tx *Tx) error {
			v := &GroupVersion{Title: g.id, Status: hubwire.SetStatusPending, GFP: g.gfp, CreatedAt: testNow, UpdatedAt: testNow}
			if err := tx.CreateGroup(ctx, Group{ID: g.id, AuthorHMAC: g.author, CreatedAt: testNow, UpdatedAt: testNow}, v); err != nil {
				return err
			}
			return tx.SetGroupStatus(ctx, g.id, 1, g.status, "", testNow)
		}); err != nil {
			t.Fatal(err)
		}
	}
	vote := func(key, recordID, groupID, gfp string) Vote {
		v := groupVoteAt(key, recordID, "64500", testNow)
		v.SetID, v.FP = groupID, gfp
		return v
	}
	for _, v := range []Vote{
		vote("author", "upload", "group", "gfp"),
		vote("author", "through-twin", "twin", "gfp"),
		vote("copier", "copier-on-original", "group", "gfp"),
		vote("voter", "independent", "group", "gfp"),
		vote("author", "moved-members", "group", "listed-gfp"),
		vote("spurner", "rejected-gfp", "group", "spurned-gfp"),
		vote("author", "elsewhere", "unknown-group", "other-gfp"),
	} {
		if err := st.UpsertGroupVote(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	pool, err := st.ScoringGroupVotesByGFP(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, votes := range pool {
		for _, v := range votes {
			got[v.RecordID] = v.ByAuthor
			if v.RecordID == "through-twin" && v.SetID != "twin" {
				t.Errorf("the newer vote in the bucket names the twin, got %+v", v)
			}
		}
	}
	want := map[string]bool{
		"through-twin":       true,
		"copier-on-original": true,
		"independent":        false,
		"moved-members":      true,
		"rejected-gfp":       false,
		"elsewhere":          false,
	}
	if !maps.Equal(got, want) {
		t.Fatalf("a group vote is the author's when its key filed a group version with that fingerprint or wrote the group it names, got %v want %v", got, want)
	}
}

func TestKeyCountersIncludeGroupVotes(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if _, _, err := st.TouchKey(ctx, "voter", testNow); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertVote(ctx, Vote{RecordID: "vote", SetID: "set", Version: 1, FP: "fp", KeyHMAC: "voter", Kind: "manual_works", Weight: 1, ASNObserved: "64500", Bucket: 1, ReceivedAt: testNow.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertGroupVote(ctx, groupVoteAt("voter", "group-vote", "64501", testNow)); err != nil {
		t.Fatal(err)
	}
	impact, err := st.KeyImpact(ctx, "voter")
	if err != nil || impact.Votes != 2 || impact.VotedSets != 2 {
		t.Fatalf("the ban preview counts the group vote, got %+v %v", impact, err)
	}
	row, err := st.KeyRowOf(ctx, "voter")
	if err != nil || row.Votes != 2 || row.ManualVotes != 2 || row.LastASN != "64501" || !row.LastSeen.Equal(testNow) {
		t.Fatalf("the key list counts the group vote and its network, got %+v %v", row, err)
	}
	origins, err := st.KeyOrigins(ctx, "voter")
	if err != nil || len(origins) != 2 || origins[0].ASN != "64501" || !slices.Equal(origins[0].Sources, []string{hubwire.RecordGroupVote}) {
		t.Fatalf("the networks list names the group vote, got %+v %v", origins, err)
	}
	clients, err := st.KeyClients(ctx, "voter")
	if err != nil || len(clients) != 1 || clients[0].B4Version != "1.90.0" || clients[0].Count != 1 {
		t.Fatalf("the b4 versions list holds the group vote's client, got %+v %v", clients, err)
	}
}
