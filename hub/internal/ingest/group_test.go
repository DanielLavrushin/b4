package ingest

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ratelimit"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func groupOf(t *testing.T, title string, sets ...config.SetConfig) hubwire.GroupBody {
	t.Helper()
	body := hubwire.GroupBody{Title: title, ASNHint: "hint", CountryHint: "xx", Engine: "nfqueue", B4Version: "1.82.0"}
	for i := range sets {
		body.Members = append(body.Members, hubwire.GroupMember{Envelope: *testkit.BuildEnvelope(t, &sets[i])})
	}
	return body
}

func (f *fixture) group(t *testing.T, id *hubwire.Identity, body hubwire.GroupBody, peer net.IP) Response {
	t.Helper()
	return f.post(t, testkit.Sign(t, id, hubwire.RecordGroup, body, f.clock), peer)
}

func groupMembers(t *testing.T, resp Response) []hubwire.GroupMemberResult {
	t.Helper()
	members, ok := resp.Body["members"].([]hubwire.GroupMemberResult)
	if !ok {
		t.Fatalf("a group answer lists its members, got %v", resp.Body)
	}
	return members
}

func tuned(name string, ttl uint8, domains ...string) config.SetConfig {
	set := testkit.SampleSet(name, domains...)
	set.Faking.TTL = ttl
	return set
}

func quic(name string, domains ...string) config.SetConfig {
	set := testkit.SampleSet(name, domains...)
	set.UDP.FilterQUIC = "all"
	return set
}

func TestAGroupTakesApartTheTCPAndQUICVariantsTheHubFolded(t *testing.T) {
	for _, stamped := range []bool{false, true} {
		t.Run("stamped="+strconv.FormatBool(stamped), func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			author := testkit.Identity(t)
			tcp, udp := testkit.SampleSet("YouTube", "youtube.com"), quic("YouTube", "youtube.com")
			tcpEnv, udpEnv := testkit.BuildEnvelope(t, &tcp), testkit.BuildEnvelope(t, &udp)
			if tcpEnv.Fingerprint == udpEnv.Fingerprint {
				t.Fatalf("the QUIC variant must carry another strategy")
			}

			first := f.share(t, author, tcp, peerA)
			expect(t, first, http.StatusAccepted, "")
			folded := f.share(t, author, udp, peerA)
			expect(t, folded, http.StatusAccepted, "")
			s := first.Body["set_id"].(string)
			if folded.Body["set_id"] != s || folded.Body["version"] != 2 {
				t.Fatalf("the hub folds the QUIC share into the TCP set as version 2, got %v", folded.Body)
			}

			body := groupOf(t, "youtube", tcp, udp)
			if stamped {
				body.Members[0].Envelope.DerivedFrom = &hubwire.Origin{ID: s, Version: 1}
				body.Members[1].Envelope.DerivedFrom = &hubwire.Origin{ID: s, Version: 2}
			}
			resp := f.group(t, author, body, peerA)
			expect(t, resp, http.StatusAccepted, "")
			members := groupMembers(t, resp)
			if members[1] != (hubwire.GroupMemberResult{SetID: s, Version: 2, Status: hubwire.SetStatusPending, Linked: true}) {
				t.Errorf("the QUIC member links the folded set at its current version, got %+v", members[1])
			}
			tcpSet := members[0].SetID
			if tcpSet == s || members[0].Version != 1 || members[0].Linked || members[0].Status != hubwire.SetStatusPending {
				t.Fatalf("the TCP member becomes a set of its own, got %+v", members[0])
			}
			latest, err := f.store.LatestVersion(ctx, s)
			if err != nil || latest.Version != 2 || latest.FP != udpEnv.Fingerprint {
				t.Errorf("the folded set keeps listing QUIC as its newest version, got %+v %v", latest, err)
			}
			v, err := f.store.GetVersion(ctx, tcpSet, 1)
			if err != nil || v.FP != tcpEnv.Fingerprint || v.RecordID != resp.Body["id"] {
				t.Fatalf("the new set holds the TCP strategy under the group record, got %+v %v", v, err)
			}
			set, _, err := f.store.GetSet(ctx, tcpSet)
			if err != nil {
				t.Fatal(err)
			}
			if wantDerived := map[bool]string{true: s, false: ""}[stamped]; set.DerivedFromID != wantDerived {
				t.Errorf("derived_from %q, want %q", set.DerivedFromID, wantDerived)
			}
			_, versions, err := f.store.GetGroup(ctx, resp.Body["group_id"].(string))
			if err != nil || len(versions) != 1 {
				t.Fatalf("group: %v %v", versions, err)
			}
			got := versions[0].Members
			if len(got) != 2 || got[0].SetID != tcpSet || !got[0].Brought || got[1].SetID != s || got[1].Brought || got[1].SetVersion != 2 {
				t.Errorf("the group keeps its members in order with what it brought, got %+v", got)
			}
		})
	}
}

func assertNothingFiled(t *testing.T, f *fixture, author *hubwire.Identity) {
	t.Helper()
	ctx := context.Background()
	sets, err := f.store.Sets(ctx)
	if err != nil || len(sets) != 0 {
		t.Errorf("a refused group files no set, got %d %v", len(sets), err)
	}
	pending, err := f.store.PendingGroupVersions(ctx)
	if err != nil || len(pending) != 0 {
		t.Errorf("a refused group files no group, got %d %v", len(pending), err)
	}
	kinds, _, err := f.store.KeyActivity(ctx, hubdata.KeyHMAC(f.svc.Secret, author.KeyID()), f.clock.Add(-time.Hour))
	if err != nil || len(kinds) != 0 {
		t.Errorf("a refused group leaves no record, got %+v %v", kinds, err)
	}
}

func TestAGroupWithARefusedMemberFilesNothing(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	body := groupOf(t, "mixed", testkit.SampleSet("ok", "ok.example"), testkit.SampleSet("second", "second.example"))
	body.Members[1].Envelope.Set = map[string]interface{}{"targets": map[string]interface{}{}}
	resp := f.group(t, author, body, peerA)
	expect(t, resp, http.StatusBadRequest, CodeInvalidSet)
	if resp.Body["member"] != 1 {
		t.Errorf("the answer names the refused member, got %v", resp.Body)
	}
	assertNothingFiled(t, f, author)

	body = groupOf(t, "twins", testkit.SampleSet("a", "a.example"), testkit.SampleSet("a", "a.example"))
	resp = f.group(t, author, body, peerA)
	expect(t, resp, http.StatusBadRequest, CodeInvalidGroup)
	if resp.Body["member"] != 1 {
		t.Errorf("identical members are refused at the second, got %v", resp.Body)
	}
	assertNothingFiled(t, f, author)
}

func TestAGroupLinksAnotherAuthorsCurrentSet(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	stranger, author := testkit.Identity(t), testkit.Identity(t)
	theirs := testkit.SampleSet("Discord", "discord.com")
	shared := f.share(t, stranger, theirs, peerB)
	expect(t, shared, http.StatusAccepted, "")
	theirID := shared.Body["set_id"].(string)

	resp := f.group(t, author, groupOf(t, "chat", theirs, tuned("Telegram", 9, "t.me")), peerA)
	expect(t, resp, http.StatusAccepted, "")
	members := groupMembers(t, resp)
	if members[0] != (hubwire.GroupMemberResult{SetID: theirID, Version: 1, Status: hubwire.SetStatusPending, Linked: true}) {
		t.Errorf("an unchanged copy links the other author's set, got %+v", members[0])
	}
	if members[1].Linked || members[1].SetID == theirID {
		t.Errorf("a new member is filed as a set of the sender, got %+v", members[1])
	}
	if _, versions, _ := f.store.GetSet(ctx, theirID); len(versions) != 1 {
		t.Errorf("a link adds no version to the linked set, got %d", len(versions))
	}
	votes, err := f.store.VotesForVersion(ctx, theirID, 1)
	if err != nil || len(votes) != 2 {
		t.Errorf("the linked set gets the sender's upload vote next to its author's, got %d %v", len(votes), err)
	}
	mine, _, err := f.store.GetSet(ctx, members[1].SetID)
	if err != nil || mine.AuthorHMAC != hubdata.KeyHMAC(f.svc.Secret, author.KeyID()) {
		t.Errorf("the brought set belongs to the sender, got %+v %v", mine, err)
	}
}

func TestAMatchOnASupersededVersionIsNotALinkButOnTheListedOrHiddenOneIs(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	stranger, author := testkit.Identity(t), testkit.Identity(t)
	old := tuned("ntc", 9, "ntc.party")
	first := f.share(t, stranger, old, peerB)
	expect(t, first, http.StatusAccepted, "")
	superseded := first.Body["set_id"].(string)
	expect(t, f.share(t, stranger, tuned("ntc", 11, "ntc.party"), peerB), http.StatusAccepted, "")

	hiddenSet := testkit.SampleSet("Instagram", "instagram.com")
	h := f.share(t, stranger, hiddenSet, peerB)
	expect(t, h, http.StatusAccepted, "")
	hiddenID := h.Body["set_id"].(string)
	if err := f.store.Hide(ctx, hiddenID, 1, "reports", f.clock); err != nil {
		t.Fatal(err)
	}

	listedSet := tuned("Discord", 9, "discord.com")
	l := f.share(t, stranger, listedSet, peerB)
	expect(t, l, http.StatusAccepted, "")
	listedID := l.Body["set_id"].(string)
	if err := f.store.Approve(ctx, listedID, 1, f.clock); err != nil {
		t.Fatal(err)
	}
	next := f.share(t, stranger, tuned("Discord", 11, "discord.com"), peerB)
	if next.Body["set_id"] != listedID || next.Body["version"] != 2 {
		t.Fatalf("the second Discord share is version 2 of the listed set, got %v", next.Body)
	}

	other := testkit.Identity(t)
	signal := tuned("Signal", 9, "signal.org")
	s := f.share(t, other, signal, peerB)
	expect(t, s, http.StatusAccepted, "")
	signalID := s.Body["set_id"].(string)
	if err := f.store.Approve(ctx, signalID, 1, f.clock); err != nil {
		t.Fatal(err)
	}
	hiddenNext := tuned("Signal", 11, "signal.org")
	expect(t, f.share(t, other, hiddenNext, peerB), http.StatusAccepted, "")
	if err := f.store.Hide(ctx, signalID, 2, "reports", f.clock); err != nil {
		t.Fatal(err)
	}

	resp := f.group(t, author, groupOf(t, "mixed", old, hiddenSet, listedSet, hiddenNext), peerA)
	expect(t, resp, http.StatusAccepted, "")
	members := groupMembers(t, resp)
	if m := members[3]; m.Linked || m.SetID == signalID {
		t.Errorf("a member equal to a hidden version of a set that lists an older one becomes a new set, got %+v", m)
	}
	if m := members[0]; m.Linked || m.SetID == superseded || m.Version != 1 {
		t.Errorf("a member that matches only a version a newer pending one supersedes becomes a new set, got %+v", m)
	}
	if members[1] != (hubwire.GroupMemberResult{SetID: hiddenID, Version: 1, Status: hubwire.SetStatusHidden, Linked: true}) {
		t.Errorf("a member that matches a hidden set links it rather than filing it again, got %+v", members[1])
	}
	if members[2] != (hubwire.GroupMemberResult{SetID: listedID, Version: 1, Status: hubwire.SetStatusActive, Linked: true}) {
		t.Errorf("a member that matches the listed version links it while a newer version waits, got %+v", members[2])
	}
	for _, id := range []string{hiddenID, listedID} {
		if _, versions, _ := f.store.GetSet(ctx, id); len(versions) != map[string]int{hiddenID: 1, listedID: 2}[id] {
			t.Errorf("a link adds no version to %s, got %d", id, len(versions))
		}
	}
}

func TestAMemberLinksTheSetItCameFromAmongEqualMatches(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	stranger := testkit.Identity(t)
	set := testkit.SampleSet("Discord", "discord.com")
	shared := f.share(t, stranger, set, peerB)
	expect(t, shared, http.StatusAccepted, "")
	first := shared.Body["set_id"].(string)
	v, err := f.store.GetVersion(ctx, first, 1)
	if err != nil {
		t.Fatal(err)
	}
	twin, err := hubdata.NewSetID()
	if err != nil {
		t.Fatal(err)
	}
	author := hubdata.KeyHMAC(f.svc.Secret, stranger.KeyID())
	if err := f.store.CreateSet(ctx, store.Set{ID: twin, AuthorHMAC: author, CreatedAt: f.clock, UpdatedAt: f.clock}, v); err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{first, twin} {
		member := set
		member.Hub = &config.HubOrigin{ID: from, Version: 1}
		resp := f.group(t, testkit.Identity(t), groupOf(t, "chat", member), peerA)
		expect(t, resp, http.StatusAccepted, "")
		if got := groupMembers(t, resp)[0]; got.SetID != from || !got.Linked {
			t.Errorf("a member that came from %s links that set among equal ones, got %+v", from, got)
		}
	}
}

func TestAnUnchangedGroupIsADuplicate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	author := testkit.Identity(t)
	body := groupOf(t, "youtube", testkit.SampleSet("YouTube", "youtube.com"), testkit.SampleSet("Video", "googlevideo.com"))
	body.Description = "both"
	body.Members[0].Escalate = &hubwire.GroupEscalation{To: 1, RstThreshold: 3}
	raw := testkit.Sign(t, author, hubwire.RecordGroup, body, f.clock)
	first := f.post(t, raw, peerA)
	expect(t, first, http.StatusAccepted, "")
	groupID := first.Body["group_id"].(string)
	if !hubdata.ValidSetID(groupID) || first.Body["version"] != 1 || first.Body["status"] != hubwire.SetStatusPending || first.Body["kind"] != hubwire.RecordGroup {
		t.Fatalf("unexpected group answer %v", first.Body)
	}

	replay := f.post(t, raw, peerA)
	expect(t, replay, http.StatusOK, "")
	if replay.Body["duplicate"] != true || replay.Body["group_id"] != groupID || replay.Body["version"] != 1 || replay.Body["status"] != hubwire.SetStatusPending || replay.Body["set_id"] != nil {
		t.Errorf("a replayed group record names its group, got %v", replay.Body)
	}
	if got, want := groupMembers(t, replay), groupMembers(t, first); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("a replay answers the members the router stamps from, got %+v want %+v", got, want)
	}

	again := body
	again.DerivedFrom = &hubwire.Origin{ID: groupID, Version: 1}
	resp := f.group(t, author, again, peerA)
	expect(t, resp, http.StatusConflict, CodeDuplicateGroup)
	if resp.Body["group_id"] != groupID || resp.Body["version"] != 1 {
		t.Errorf("the duplicate names the group version it repeats, got %v", resp.Body)
	}
	if _, versions, _ := f.store.GetGroup(ctx, groupID); len(versions) != 1 {
		t.Errorf("a duplicate adds no group version, got %d", len(versions))
	}

	escalated := again
	escalated.Members = append([]hubwire.GroupMember{}, again.Members...)
	escalated.Members[0].Escalate = &hubwire.GroupEscalation{To: 1, RstThreshold: 4}
	resp = f.group(t, author, escalated, peerA)
	expect(t, resp, http.StatusAccepted, "")
	if resp.Body["group_id"] != groupID || resp.Body["version"] != 2 {
		t.Errorf("a changed escalation makes a new version of the group, got %v", resp.Body)
	}
}

func TestAMemberOnlyChangeMakesANewGroupVersion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	author := testkit.Identity(t)
	first := f.group(t, author, groupOf(t, "youtube", testkit.SampleSet("YouTube", "youtube.com"), tuned("Video", 9, "googlevideo.com")), peerA)
	expect(t, first, http.StatusAccepted, "")
	groupID := first.Body["group_id"].(string)
	before := groupMembers(t, first)

	body := groupOf(t, "youtube", testkit.SampleSet("YouTube", "youtube.com"), tuned("Video", 11, "googlevideo.com"))
	body.DerivedFrom = &hubwire.Origin{ID: groupID, Version: 1}
	body.Members[1].Envelope.DerivedFrom = &hubwire.Origin{ID: before[1].SetID, Version: 1}
	resp := f.group(t, author, body, peerA)
	expect(t, resp, http.StatusAccepted, "")
	if resp.Body["group_id"] != groupID || resp.Body["version"] != 2 || resp.Body["status"] != hubwire.SetStatusPending {
		t.Fatalf("a changed member makes version 2 of the group, got %v", resp.Body)
	}
	members := groupMembers(t, resp)
	if members[0] != (hubwire.GroupMemberResult{SetID: before[0].SetID, Version: 1, Status: hubwire.SetStatusPending, Linked: true}) {
		t.Errorf("the unchanged member links its set, got %+v", members[0])
	}
	if members[1].SetID != before[1].SetID || members[1].Version != 2 || members[1].Linked {
		t.Errorf("the changed member becomes version 2 of its set, got %+v", members[1])
	}
	brought, err := f.store.BroughtVersions(ctx, groupID, 2)
	if err != nil || len(brought) != 1 || brought[0].SetID != before[1].SetID || brought[0].Version != 2 {
		t.Errorf("version 2 of the group brought only the changed member, got %+v %v", brought, err)
	}
	pending, err := f.store.PendingGroupVersions(ctx)
	if err != nil || len(pending) != 2 || pending[1].Version != 2 || len(pending[1].Members) != 2 {
		t.Errorf("both group versions wait for a moderator, got %+v %v", pending, err)
	}
}

func TestAGroupDerivedFromAnotherAuthorsGroupIsANewGroup(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	stranger, author := testkit.Identity(t), testkit.Identity(t)
	theirs := f.group(t, stranger, groupOf(t, "chat", testkit.SampleSet("Discord", "discord.com")), peerB)
	expect(t, theirs, http.StatusAccepted, "")
	theirID := theirs.Body["group_id"].(string)

	body := groupOf(t, "chat", testkit.SampleSet("Discord", "discord.com"), testkit.SampleSet("Telegram", "t.me"))
	body.DerivedFrom = &hubwire.Origin{ID: theirID, Version: 1}
	resp := f.group(t, author, body, peerA)
	expect(t, resp, http.StatusAccepted, "")
	mineID := resp.Body["group_id"].(string)
	if mineID == theirID || resp.Body["version"] != 1 {
		t.Fatalf("a group derived from another author's group is a new group, got %v", resp.Body)
	}
	g, _, err := f.store.GetGroup(ctx, mineID)
	if err != nil || g.DerivedFromID != theirID || g.DerivedFromVersion != 1 || g.AuthorHMAC != hubdata.KeyHMAC(f.svc.Secret, author.KeyID()) {
		t.Errorf("the new group records where it came from, got %+v %v", g, err)
	}
	if _, versions, _ := f.store.GetGroup(ctx, theirID); len(versions) != 1 {
		t.Errorf("the other author's group keeps one version, got %d", len(versions))
	}
}

func TestAGroupIsRefusedBeforeAnyWrite(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	sets := make([]config.SetConfig, hubwire.MaxGroupMembers+1)
	for i := range sets {
		sets[i] = testkit.SampleSet("s"+strconv.Itoa(i), "s"+strconv.Itoa(i)+".example")
	}
	expect(t, f.group(t, author, groupOf(t, "big", sets...), peerA), http.StatusBadRequest, CodeTooManyMembers)
	expect(t, f.group(t, author, hubwire.GroupBody{Title: "empty"}, peerA), http.StatusBadRequest, CodeInvalidGroup)
	expect(t, f.group(t, author, groupOf(t, "  ", sets[0]), peerA), http.StatusBadRequest, CodeInvalidGroup)

	for _, escalate := range []*hubwire.GroupEscalation{{To: 0}, {To: 2}, {To: -1}} {
		body := groupOf(t, "chain", sets[0], sets[1])
		body.Members[0].Escalate = escalate
		resp := f.group(t, author, body, peerA)
		expect(t, resp, http.StatusBadRequest, CodeInvalidGroup)
		if resp.Body["member"] != 0 {
			t.Errorf("an escalation to %d names its member, got %v", escalate.To, resp.Body)
		}
	}
	raw := testkit.Sign(t, author, hubwire.RecordGroup, json.RawMessage(`{"title":"x","members":"no"}`), f.clock)
	expect(t, f.post(t, raw, peerA), http.StatusBadRequest, CodeBadRecord)
	assertNothingFiled(t, f, author)

	body := groupOf(t, "chain", sets[0], sets[1])
	body.Members[0].Escalate = &hubwire.GroupEscalation{To: 1, StallThreshold: 2}
	resp := f.group(t, author, body, peerA)
	expect(t, resp, http.StatusAccepted, "")
	_, versions, err := f.store.GetGroup(context.Background(), resp.Body["group_id"].(string))
	if err != nil || versions[0].Members[0].Escalate == nil || *versions[0].Members[0].Escalate != (hubwire.GroupEscalation{To: 1, StallThreshold: 2}) || versions[0].Members[1].Escalate != nil {
		t.Errorf("a valid escalation is kept by member position, got %+v %v", versions, err)
	}
}

func TestAGroupEscalationThresholdOutsideTheSetEditorRangeIsRefused(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	a, b := testkit.SampleSet("a", "a.example"), testkit.SampleSet("b", "b.example")
	for _, escalate := range []hubwire.GroupEscalation{
		{To: 0, RstThreshold: 3000000000},
		{To: 0, TtlSec: 1 << 32},
		{To: 0, TtlSec: 86401},
		{To: 0, StallTimeoutMs: 15001},
		{To: 0, DNSThreshold: -1},
	} {
		body := groupOf(t, "chain", a, b)
		body.Members[1].Escalate = &escalate
		resp := f.group(t, author, body, peerA)
		expect(t, resp, http.StatusBadRequest, CodeInvalidGroup)
		if resp.Body["member"] != 1 {
			t.Errorf("an escalation %+v outside the range names its member, got %v", escalate, resp.Body)
		}
	}
	assertNothingFiled(t, f, author)

	body := groupOf(t, "chain", a, b)
	limits := hubwire.GroupEscalation{To: 0, RstThreshold: 20, RstWindowSec: 600, TtlSec: 86400, StallThreshold: 20, StallTimeoutMs: 15000, DNSThreshold: 10}
	body.Members[1].Escalate = &limits
	expect(t, f.group(t, author, body, peerA), http.StatusAccepted, "")
}

func TestADerivedFromVersionOutsideTheIntRangeIsRecordedAsZero(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	stranger, author := testkit.Identity(t), testkit.Identity(t)
	theirs := f.share(t, stranger, testkit.SampleSet("Discord", "discord.com"), peerB)
	expect(t, theirs, http.StatusAccepted, "")
	theirSet := theirs.Body["set_id"].(string)
	theirGroup := f.group(t, stranger, groupOf(t, "chat", testkit.SampleSet("Telegram", "t.me")), peerB)
	expect(t, theirGroup, http.StatusAccepted, "")
	theirGroupID := theirGroup.Body["group_id"].(string)

	env := testkit.BuildEnvelope(t, ptr(testkit.SampleSet("Discord2", "discord.gg")))
	env.DerivedFrom = &hubwire.Origin{ID: theirSet, Version: 1 << 33}
	shared := f.post(t, testkit.SignShare(t, author, env, f.clock), peerA)
	expect(t, shared, http.StatusAccepted, "")
	if set, _, err := f.store.GetSet(ctx, shared.Body["set_id"].(string)); err != nil || set.DerivedFromID != theirSet || set.DerivedFromVersion != 0 {
		t.Errorf("a shared set keeps its parent but not an out-of-range version, got %+v %v", set, err)
	}

	body := groupOf(t, "chat", testkit.SampleSet("Discord3", "discord.media"), testkit.SampleSet("Signal", "signal.org"))
	body.Members[0].Envelope.DerivedFrom = &hubwire.Origin{ID: theirSet, Version: 1 << 33}
	body.DerivedFrom = &hubwire.Origin{ID: theirGroupID, Version: 1 << 33}
	resp := f.group(t, author, body, peerA)
	expect(t, resp, http.StatusAccepted, "")
	member := groupMembers(t, resp)[0]
	if set, _, err := f.store.GetSet(ctx, member.SetID); err != nil || member.SetID == theirSet || set.DerivedFromID != theirSet || set.DerivedFromVersion != 0 {
		t.Errorf("a group member keeps its parent but not an out-of-range version, got %+v %v", set, err)
	}
	if g, _, err := f.store.GetGroup(ctx, resp.Body["group_id"].(string)); err != nil || g.DerivedFromID != theirGroupID || g.DerivedFromVersion != 0 {
		t.Errorf("a group keeps its parent group but not an out-of-range version, got %+v %v", g, err)
	}
}

func TestGroupsHaveTheirOwnDailyLimit(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	big := make([]config.SetConfig, 0, ratelimit.SharesPerDay+1)
	for i := 0; i <= ratelimit.SharesPerDay; i++ {
		big = append(big, tuned("g", uint8(20+i), "g"+strconv.Itoa(i)+".example"))
	}
	expect(t, f.group(t, author, groupOf(t, "big", big...), peerA), http.StatusAccepted, "")
	for i := 1; i < ratelimit.GroupsPerDay; i++ {
		expect(t, f.group(t, author, groupOf(t, "g"+strconv.Itoa(i), tuned("x", uint8(40+i), "x.example")), peerA), http.StatusAccepted, "")
	}
	resp := f.group(t, author, groupOf(t, "one more", tuned("x", 60, "x.example")), peerA)
	expect(t, resp, http.StatusTooManyRequests, CodeRateLimited)
	if resp.Body["scope"] != ratelimit.ScopeGroup || resp.Body["limit"] != ratelimit.GroupsPerDay {
		t.Errorf("the group limit names its own scope, got %v", resp.Body)
	}
	for i := 0; i < ratelimit.SharesPerDay; i++ {
		expect(t, f.share(t, author, tuned("y", uint8(70+i), "y.example"), peerA), http.StatusAccepted, "")
	}

	limits := store.DefaultSettings()
	limits.GroupsPerDay = ratelimit.GroupsPerDay + 1
	if err := f.store.SaveSettings(context.Background(), limits); err != nil {
		t.Fatal(err)
	}
	expect(t, f.group(t, author, groupOf(t, "allowed", tuned("x", 61, "x.example")), peerA), http.StatusAccepted, "")
}
