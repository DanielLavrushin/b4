package ingest

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ratelimit"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

const base64URLAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

func reencode(t *testing.T, raw []byte, edit func(*hubwire.Record)) []byte {
	t.Helper()
	var rec hubwire.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	edit(&rec)
	out, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRecordsMustUseTheCanonicalEncoding(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	voter := testkit.Identity(t)
	setID, fp := approvedSet(t, f, author)
	signed := func() []byte {
		body := hubwire.VoteBody{SetID: setID, Version: 1, FP: fp, Kind: hubwire.VoteWorks}
		return testkit.Sign(t, voter, hubwire.RecordVote, body, f.clock)
	}

	variants := map[string]func(*hubwire.Record){
		"newline in the signature": func(r *hubwire.Record) { r.Sig = r.Sig[:10] + "\n" + r.Sig[10:] },
		"spare bits in the signature": func(r *hubwire.Record) {
			last := strings.IndexByte(base64URLAlphabet, r.Sig[len(r.Sig)-1])
			r.Sig = r.Sig[:len(r.Sig)-1] + string(base64URLAlphabet[last^1])
		},
	}
	for name, edit := range variants {
		raw := signed()
		variant := reencode(t, raw, edit)
		var rec hubwire.Record
		_ = json.Unmarshal(variant, &rec)
		if _, err := hubwire.VerifyRecord(&rec); err != nil {
			t.Fatalf("%s: the variant must still verify for this test to mean anything: %v", name, err)
		}
		expect(t, f.post(t, variant, peerB), http.StatusBadRequest, CodeBadSignature)
		expect(t, f.post(t, raw, peerB), http.StatusAccepted, "")
	}

	raw := signed()
	expect(t, f.post(t, raw, peerB), http.StatusAccepted, "")
	expect(t, f.post(t, raw, peerB), http.StatusOK, "")
}

func TestCanonicalEncodingRejectsPaddedKeys(t *testing.T) {
	id := testkit.Identity(t)
	rec, err := hubwire.SignRecord(id, hubwire.RecordVote, hubwire.VoteBody{SetID: "x", Version: 1, FP: "f", Kind: hubwire.VoteWorks}, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !CanonicalEncoding(rec) {
		t.Fatal("a freshly signed record must be canonical")
	}
	padded := *rec
	padded.Key = " " + padded.Key
	if CanonicalEncoding(&padded) {
		t.Error("a key with surrounding space is not canonical")
	}
}

func approvedMirror(t *testing.T, f *fixture) *hubwire.Identity {
	t.Helper()
	ctx := context.Background()
	id := testkit.Identity(t)
	m, err := f.store.AnnounceMirror(ctx, "https://relay.example", hubdata.KeyHMAC(f.svc.Secret, id.KeyID()), "1.3.0", f.clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetMirrorStatus(ctx, m.ID, store.MirrorApproved, "", f.clock); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fixture) relay(mirror *hubwire.Identity, raw []byte, peer net.IP) Response {
	return f.svc.HandleFrom(context.Background(), raw, Source{IP: peer, Relay: hubdata.SignRelay(mirror, f.clock, raw)})
}

func TestRelayedRecordsCarryNoNetwork(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	author := testkit.Identity(t)
	setID, fp := approvedSet(t, f, author)
	mirror := approvedMirror(t, f)
	vote := func(id *hubwire.Identity) []byte {
		body := hubwire.VoteBody{SetID: setID, Version: 1, FP: fp, Kind: hubwire.VoteWorks}
		return testkit.Sign(t, id, hubwire.RecordVote, body, f.clock)
	}
	direct := testkit.Identity(t)
	relayed := testkit.Identity(t)
	expect(t, f.post(t, vote(direct), peerB), http.StatusAccepted, "")
	expect(t, f.relay(mirror, vote(relayed), peerC), http.StatusAccepted, "")

	votes, err := f.store.VotesForFP(ctx, fp)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]store.Vote{}
	for _, v := range votes {
		byKey[v.KeyHMAC] = v
	}
	d := byKey[hubdata.KeyHMAC(f.svc.Secret, direct.KeyID())]
	if d.ASNObserved != "64501" || d.CountryObserved != "DE" || !d.OriginVerified {
		t.Errorf("a direct vote keeps the network it came from, got %+v", d)
	}
	r := byKey[hubdata.KeyHMAC(f.svc.Secret, relayed.KeyID())]
	if r.ASNObserved != "" || r.CountryObserved != "" || r.OriginVerified {
		t.Errorf("a vote passed on by an approved mirror must not take the mirror's network, got %+v", r)
	}
}

func TestOnlyASignatureFromAnApprovedMirrorMarksARecordRelayed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	author := testkit.Identity(t)
	setID, fp := approvedSet(t, f, author)
	mirror := approvedMirror(t, f)
	stranger := testkit.Identity(t)
	voter := testkit.Identity(t)
	vote := func() []byte {
		body := hubwire.VoteBody{SetID: setID, Version: 1, FP: fp, Kind: hubwire.VoteWorks}
		return testkit.Sign(t, voter, hubwire.RecordVote, body, f.clock)
	}
	cases := map[string]func(raw []byte) string{
		"no header":             func([]byte) string { return "" },
		"unapproved key":        func(raw []byte) string { return hubdata.SignRelay(stranger, f.clock, raw) },
		"another body":          func([]byte) string { return hubdata.SignRelay(mirror, f.clock, []byte("{}")) },
		"stale signature":       func(raw []byte) string { return hubdata.SignRelay(mirror, f.clock.Add(-2*hubdata.RelayWindow), raw) },
		"garbage":               func([]byte) string { return "1 nonsense 0 x" },
		"user agent claim only": func([]byte) string { return "" },
	}
	for name, header := range cases {
		raw := vote()
		expect(t, f.svc.HandleFrom(ctx, raw, Source{IP: peerB, Relay: header(raw)}), http.StatusAccepted, "")
		var rec hubwire.Record
		_ = json.Unmarshal(raw, &rec)
		votes, _ := f.store.VotesForFP(ctx, fp)
		found := false
		for _, v := range votes {
			if v.KeyHMAC == hubdata.KeyHMAC(f.svc.Secret, rec.Key) {
				found = true
				if v.ASNObserved != "64501" || !v.OriginVerified {
					t.Errorf("%s: a record without a valid approved-mirror signature keeps its sender's network, got %+v", name, v)
				}
			}
		}
		if !found {
			t.Errorf("%s: the vote was not stored", name)
		}
	}
}

func TestRelayedReportsDoNotHideAVersion(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	setID, _ := approvedSet(t, f, author)
	mirror := approvedMirror(t, f)
	report := func(peer net.IP, relayed bool) Response {
		body := hubwire.ReportBody{SetID: setID, Version: 1, Reason: "steals traffic"}
		raw := testkit.Sign(t, testkit.Identity(t), hubwire.RecordReport, body, f.clock)
		if relayed {
			return f.relay(mirror, raw, peer)
		}
		return f.post(t, raw, peer)
	}
	expect(t, report(peerA, false), http.StatusAccepted, "")
	expect(t, report(peerB, true), http.StatusAccepted, "")
	expect(t, report(peerC, true), http.StatusAccepted, "")
	v, err := f.store.GetVersion(context.Background(), setID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != hubwire.SetStatusActive {
		t.Fatalf("reports passed on by mirrors must not count as independent networks, got %s", v.Status)
	}
}

func TestAnApprovedMirrorHasALargerNewKeyBudget(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	setID, fp := approvedSet(t, f, author)
	mirror := approvedMirror(t, f)
	vote := func() []byte {
		body := hubwire.VoteBody{SetID: setID, Version: 1, FP: fp, Kind: hubwire.VoteWorks}
		return testkit.Sign(t, testkit.Identity(t), hubwire.RecordVote, body, f.clock)
	}
	for i := 0; i < ratelimit.NewKeysPerDay*RelayNewKeyFactor; i++ {
		expect(t, f.relay(mirror, vote(), peerC), http.StatusAccepted, "")
	}
	expect(t, f.relay(mirror, vote(), peerC), http.StatusTooManyRequests, CodeRateLimited)

	for i := 0; i < ratelimit.NewKeysPerDay; i++ {
		expect(t, f.post(t, vote(), peerB), http.StatusAccepted, "")
	}
	expect(t, f.post(t, vote(), peerB), http.StatusTooManyRequests, CodeRateLimited)
}

func TestAnApprovedMirrorKeepsItsKey(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := testkit.Identity(t)
	squatter := testkit.Identity(t)
	announce := func(id *hubwire.Identity) Response {
		return f.post(t, testkit.Sign(t, id, hubwire.RecordMirror, hubwire.MirrorBody{URL: "https://mirror.example", Version: "1.3.0"}, f.clock), peerA)
	}
	expect(t, announce(squatter), http.StatusAccepted, "")
	expect(t, announce(owner), http.StatusAccepted, "")
	m, err := f.store.MirrorByURL(ctx, "https://mirror.example")
	if err != nil {
		t.Fatal(err)
	}
	if m.KeyHMAC != hubdata.KeyHMAC(f.svc.Secret, owner.KeyID()) {
		t.Fatalf("a pending mirror follows its latest announcement, got %+v", m)
	}
	if err := f.store.SetMirrorStatus(ctx, m.ID, store.MirrorApproved, "", f.clock); err != nil {
		t.Fatal(err)
	}

	f.clock = f.clock.Add(time.Hour)
	expect(t, announce(squatter), http.StatusConflict, CodeMirrorKey)
	m, _ = f.store.MirrorByURL(ctx, "https://mirror.example")
	if m.KeyHMAC != hubdata.KeyHMAC(f.svc.Secret, owner.KeyID()) || m.Status != store.MirrorApproved || m.LastSeen.Equal(f.clock) {
		t.Fatalf("another key must not take over an approved mirror, got %+v", m)
	}
	resp := announce(owner)
	expect(t, resp, http.StatusAccepted, "")
	if resp.Body["status"] != store.MirrorApproved {
		t.Fatalf("the owner keeps announcing as before, got %v", resp.Body)
	}
	m, _ = f.store.MirrorByURL(ctx, "https://mirror.example")
	if !m.LastSeen.Equal(f.clock) {
		t.Fatalf("the owner's announcement refreshes last_seen, got %+v", m)
	}
}

func TestTheApprovedMirrorListIsCachedForAMinute(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	author := testkit.Identity(t)
	setID, fp := approvedSet(t, f, author)
	mirror := approvedMirror(t, f)
	relayedVote := func() bool {
		voter := testkit.Identity(t)
		body := hubwire.VoteBody{SetID: setID, Version: 1, FP: fp, Kind: hubwire.VoteWorks}
		expect(t, f.relay(mirror, testkit.Sign(t, voter, hubwire.RecordVote, body, f.clock), peerC), http.StatusAccepted, "")
		votes, _ := f.store.VotesForFP(ctx, fp)
		for _, v := range votes {
			if v.KeyHMAC == hubdata.KeyHMAC(f.svc.Secret, voter.KeyID()) {
				return v.ASNObserved == ""
			}
		}
		t.Fatal("vote not stored")
		return false
	}
	if !relayedVote() {
		t.Fatal("an approved mirror's signed record is relayed")
	}
	m, err := f.store.MirrorByURL(ctx, "https://relay.example")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetMirrorStatus(ctx, m.ID, store.MirrorRejected, "gone", f.clock); err != nil {
		t.Fatal(err)
	}
	if !relayedVote() {
		t.Fatal("within a minute the cached approval still holds")
	}
	f.clock = f.clock.Add(MirrorKeysTTL)
	if relayedVote() {
		t.Fatal("after a minute a rejected mirror's records keep their sender's network")
	}
}
