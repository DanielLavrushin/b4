package ingest

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ratelimit"
	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func mediaGroupBody(t *testing.T) hubwire.GroupBody {
	t.Helper()
	body := groupOf(t, "media", testkit.SampleSet("Video", "video.example"), testkit.SampleSet("Music", "music.example"))
	body.Members[0].Escalate = &hubwire.GroupEscalation{To: 1, RstThreshold: 3}
	return body
}

func (f *fixture) setGroupStatus(t *testing.T, id string, version int, status string) {
	t.Helper()
	ctx := context.Background()
	if err := f.store.Update(ctx, func(tx *store.Tx) error {
		return tx.SetGroupStatus(ctx, id, version, status, "", f.clock)
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) listedGroup(t *testing.T, author *hubwire.Identity, body hubwire.GroupBody) (string, []hubwire.GroupMemberResult) {
	t.Helper()
	resp := f.group(t, author, body, peerA)
	expect(t, resp, http.StatusAccepted, "")
	members := groupMembers(t, resp)
	for _, m := range members {
		if err := f.store.Approve(context.Background(), m.SetID, m.Version, f.clock); err != nil {
			t.Fatal(err)
		}
	}
	id := resp.Body["group_id"].(string)
	f.setGroupStatus(t, id, 1, hubwire.SetStatusActive)
	return id, members
}

func applied(members []hubwire.GroupMemberResult) []hubwire.GroupVoteMember {
	out := make([]hubwire.GroupVoteMember, len(members))
	for i, m := range members {
		out[i] = hubwire.GroupVoteMember{SetID: m.SetID, Version: m.Version}
	}
	return out
}

func (f *fixture) groupVote(t *testing.T, voter *hubwire.Identity, body hubwire.GroupVoteBody, peer net.IP) Response {
	t.Helper()
	return f.post(t, testkit.Sign(t, voter, hubwire.RecordGroupVote, body, f.clock), peer)
}

func (f *fixture) gfp(t *testing.T, members []hubwire.GroupVoteMember, escalations ...*hubwire.GroupEscalation) string {
	t.Helper()
	fps := make([]hubwire.GroupFingerprintMember, len(members))
	for i, m := range members {
		v, err := f.store.GetVersion(context.Background(), m.SetID, m.Version)
		if err != nil {
			t.Fatal(err)
		}
		fps[i].FP = v.FP
		if i < len(escalations) {
			fps[i].Escalate = escalations[i]
		}
	}
	return hubwire.GroupFingerprint(fps)
}

func (f *fixture) groupVotesOf(t *testing.T, id *hubwire.Identity, gfp string) []store.Vote {
	t.Helper()
	pool, err := f.store.ScoringGroupVotesByGFP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Vote
	for _, v := range pool[gfp] {
		if v.KeyHMAC == hubdata.KeyHMAC(f.svc.Secret, id.KeyID()) {
			out = append(out, v)
		}
	}
	return out
}

func TestAGroupVoteCountsUnderTheGroupFingerprint(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	author, voter := testkit.Identity(t), testkit.Identity(t)
	body := mediaGroupBody(t)
	id, members := f.listedGroup(t, author, body)
	named := applied(members)

	raw := testkit.Sign(t, voter, hubwire.RecordGroupVote, hubwire.GroupVoteBody{
		GroupID: id, Version: 1, Members: named, Kind: hubwire.VoteBroken, Domain: "www.music.example",
		ASNHint: "hint", CountryHint: "de", Engine: "tun", B4Version: "1.90.0",
	}, f.clock)
	expect(t, f.post(t, raw, peerB), http.StatusAccepted, "")

	gfp := f.gfp(t, named, body.Members[0].Escalate)
	if gfp == f.gfp(t, named) {
		t.Fatal("the escalation is part of the group fingerprint")
	}
	votes := f.groupVotesOf(t, voter, gfp)
	if len(votes) != 1 {
		t.Fatalf("one group vote under the group fingerprint, got %+v", votes)
	}
	v := votes[0]
	if v.SetID != id || v.Version != 1 || v.Kind != score.KindManualBroken || v.Weight != -1 || v.ASNObserved != "64501" || v.CountryObserved != "DE" ||
		!v.OriginVerified || v.Domain != "www.music.example" || v.CountryHint != "DE" || v.Engine != "tun" || v.B4Version != "1.90.0" || v.ByAuthor {
		t.Errorf("the group vote keeps the group, its version, the kind, the origin and a targeted domain, got %+v", v)
	}
	setVotes, err := f.store.AllVotes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, sv := range setVotes {
		if sv.KeyHMAC == v.KeyHMAC {
			t.Errorf("a group vote never counts for a member set, got %+v", sv)
		}
	}
	var rec hubwire.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	r, err := f.store.GetRecord(ctx, rec.ID())
	if err != nil || r.Kind != hubwire.RecordGroupVote || r.SetID != id || r.Version != 1 {
		t.Fatalf("the record names the group and its version, got %+v %v", r, err)
	}

	again := f.post(t, raw, peerB)
	expect(t, again, http.StatusOK, "")
	if again.Body["duplicate"] != true || again.Body["id"] != rec.ID() {
		t.Errorf("a replay answers as a duplicate, got %v", again.Body)
	}
	if votes := f.groupVotesOf(t, voter, gfp); len(votes) != 1 || votes[0].Kind != score.KindManualBroken {
		t.Errorf("a replay counts nothing again, got %+v", votes)
	}

	f.clock = f.clock.Add(time.Minute)
	expect(t, f.groupVote(t, voter, hubwire.GroupVoteBody{GroupID: id, Version: 1, Members: named, Kind: hubwire.VoteWorks, Domain: "unrelated.example"}, peerB), http.StatusAccepted, "")
	if votes := f.groupVotesOf(t, voter, gfp); len(votes) != 1 || votes[0].Kind != score.KindManualWorks || votes[0].Domain != "" {
		t.Errorf("the newest group vote in the bucket wins and an untargeted domain is dropped, got %+v", votes)
	}
	expect(t, f.groupVote(t, voter, hubwire.GroupVoteBody{GroupID: id, Version: 1, Members: named, Kind: "meh"}, peerB), http.StatusBadRequest, CodeBadRecord)
}

func TestTheAuthorsUploadVoteStaysTheAuthorsWhenTheyVoteThroughATwinGroup(t *testing.T) {
	f := newFixture(t)
	author, copier := testkit.Identity(t), testkit.Identity(t)
	body := mediaGroupBody(t)
	id, members := f.listedGroup(t, author, body)
	named := applied(members)
	twinBody := body
	twinBody.DerivedFrom = &hubwire.Origin{ID: id, Version: 1}
	resp := f.group(t, copier, twinBody, peerB)
	expect(t, resp, http.StatusAccepted, "")
	twin := resp.Body["group_id"].(string)
	if twin == id || !slices.Equal(applied(groupMembers(t, resp)), named) {
		t.Fatalf("another key files a twin of the group over the same sets, got %v", resp.Body)
	}
	f.setGroupStatus(t, twin, 1, hubwire.SetStatusActive)

	expect(t, f.groupVote(t, author, hubwire.GroupVoteBody{GroupID: twin, Version: 1, Members: named, Kind: hubwire.VoteWorks}, peerA), http.StatusAccepted, "")
	votes := f.groupVotesOf(t, author, f.gfp(t, named, body.Members[0].Escalate))
	if len(votes) != 1 || votes[0].SetID != twin || votes[0].Kind != score.KindManualWorks || !votes[0].ByAuthor {
		t.Fatalf("the author's vote through the twin replaces their upload vote in the bucket and stays the author's, got %+v", votes)
	}
}

func TestAGroupVoteNeedsAListedGroupVersion(t *testing.T) {
	f := newFixture(t)
	author, voter := testkit.Identity(t), testkit.Identity(t)
	resp := f.group(t, author, mediaGroupBody(t), peerA)
	expect(t, resp, http.StatusAccepted, "")
	id, members := resp.Body["group_id"].(string), groupMembers(t, resp)
	vote := func(groupID string, version int) Response {
		return f.groupVote(t, voter, hubwire.GroupVoteBody{GroupID: groupID, Version: version, Members: applied(members), Kind: hubwire.VoteWorks}, peerB)
	}

	expect(t, vote(id, 1), http.StatusBadRequest, CodeNotActive)
	for _, m := range members {
		if err := f.store.Approve(context.Background(), m.SetID, m.Version, f.clock); err != nil {
			t.Fatal(err)
		}
	}
	expect(t, vote(id, 1), http.StatusBadRequest, CodeNotActive)
	f.setGroupStatus(t, id, 1, hubwire.SetStatusActive)
	expect(t, vote(id, 1), http.StatusAccepted, "")
	expect(t, vote(id, 2), http.StatusBadRequest, CodeUnknownSet)
	expect(t, vote("01ARZ3NDEKTSV4RRFFQ69G5FAV", 1), http.StatusBadRequest, CodeUnknownSet)
	expect(t, vote("not-a-group", 1), http.StatusBadRequest, CodeBadRecord)
	f.setGroupStatus(t, id, 1, hubwire.SetStatusHidden)
	expect(t, vote(id, 1), http.StatusBadRequest, CodeNotActive)
}

func TestAGroupVoteNamesTheGroupsSetsInOrderAtActiveVersions(t *testing.T) {
	f := newFixture(t)
	author, voter := testkit.Identity(t), testkit.Identity(t)
	body := mediaGroupBody(t)
	id, members := f.listedGroup(t, author, body)
	named := applied(members)
	vote := func(members []hubwire.GroupVoteMember) Response {
		f.clock = f.clock.Add(time.Second)
		return f.groupVote(t, voter, hubwire.GroupVoteBody{GroupID: id, Version: 1, Members: members, Kind: hubwire.VoteWorks}, peerB)
	}

	swapped := vote([]hubwire.GroupVoteMember{named[1], named[0]})
	expect(t, swapped, http.StatusBadRequest, CodeMemberMismatch)
	if swapped.Body["member"] != 0 {
		t.Errorf("the refusal names the first member out of place, got %v", swapped.Body)
	}
	expect(t, vote(named[:1]), http.StatusBadRequest, CodeMemberMismatch)
	expect(t, vote(append(append([]hubwire.GroupVoteMember{}, named...), named[0])), http.StatusBadRequest, CodeMemberMismatch)
	unknown := vote([]hubwire.GroupVoteMember{named[0], {SetID: named[1].SetID, Version: 9}})
	expect(t, unknown, http.StatusBadRequest, CodeUnknownSet)
	if unknown.Body["member"] != 1 {
		t.Errorf("the refusal names the unknown member, got %v", unknown.Body)
	}

	next := testkit.SampleSet("Music", "music.example")
	next.Faking.TTL = 77
	next.Hub = &config.HubOrigin{ID: named[1].SetID, Version: 1}
	shared := f.share(t, author, next, peerA)
	expect(t, shared, http.StatusAccepted, "")
	if shared.Body["set_id"] != named[1].SetID || shared.Body["version"] != 2 {
		t.Fatalf("the author's new strategy is version 2 of the member set, got %v", shared.Body)
	}
	newer := []hubwire.GroupVoteMember{named[0], {SetID: named[1].SetID, Version: 2}}
	pending := vote(newer)
	expect(t, pending, http.StatusBadRequest, CodeNotActive)
	if pending.Body["member"] != 1 {
		t.Errorf("the refusal names the pending member version, got %v", pending.Body)
	}

	if err := f.store.Approve(context.Background(), named[1].SetID, 2, f.clock); err != nil {
		t.Fatal(err)
	}
	expect(t, vote(named), http.StatusAccepted, "")
	expect(t, vote(newer), http.StatusAccepted, "")
	older, current := f.gfp(t, named, body.Members[0].Escalate), f.gfp(t, newer, body.Members[0].Escalate)
	if older == current || len(f.groupVotesOf(t, voter, older)) != 1 || len(f.groupVotesOf(t, voter, current)) != 1 {
		t.Errorf("a vote on an older but still active member version counts under that version's group fingerprint")
	}
}

func TestGroupVotesSpendTheVoteLimitNotTheShareLimit(t *testing.T) {
	f := newFixture(t)
	author, voter := testkit.Identity(t), testkit.Identity(t)
	id, members := f.listedGroup(t, author, mediaGroupBody(t))
	body := hubwire.GroupVoteBody{GroupID: id, Version: 1, Members: applied(members), Kind: hubwire.VoteWorks}
	for i := 0; i < ratelimit.VotesPerDay; i++ {
		f.clock = f.clock.Add(time.Second)
		expect(t, f.groupVote(t, voter, body, peerB), http.StatusAccepted, "")
	}
	f.clock = f.clock.Add(time.Second)
	resp := f.groupVote(t, voter, body, peerB)
	expect(t, resp, http.StatusTooManyRequests, CodeRateLimited)
	if resp.Body["scope"] != ratelimit.ScopeVote || resp.Body["limit"] != ratelimit.VotesPerDay {
		t.Errorf("group votes share the vote limit, got %v", resp.Body)
	}
	expect(t, f.share(t, voter, testkit.SampleSet("own", "own.example"), peerB), http.StatusAccepted, "")
}

func TestAGroupShareCastsTheAuthorsUploadGroupVoteOnce(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	body := mediaGroupBody(t)
	raw := testkit.Sign(t, author, hubwire.RecordGroup, body, f.clock)
	resp := f.post(t, raw, peerA)
	expect(t, resp, http.StatusAccepted, "")
	id, members := resp.Body["group_id"].(string), groupMembers(t, resp)
	gfp := f.gfp(t, applied(members), body.Members[0].Escalate)

	check := func(when string) {
		t.Helper()
		pool, err := f.store.ScoringGroupVotesByGFP(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		votes := pool[gfp]
		if len(pool) != 1 || len(votes) != 1 {
			t.Fatalf("%s: one upload vote for the filed group, got %+v", when, pool)
		}
		v := votes[0]
		if v.Kind != score.KindUpload || v.Weight != 1 || !v.ByAuthor || v.SetID != id || v.Version != 1 || v.ASNObserved != "64500" || !v.OriginVerified ||
			v.KeyHMAC != hubdata.KeyHMAC(f.svc.Secret, author.KeyID()) || v.B4Version != "1.82.0" || v.Engine != "nfqueue" || v.CountryHint != "XX" {
			t.Errorf("%s: the author's upload vote for the group, got %+v", when, v)
		}
	}
	check("filed")
	expect(t, f.post(t, raw, peerA), http.StatusOK, "")
	check("replayed")
}
