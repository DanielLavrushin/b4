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

func TestRelayedRecordsCarryNoNetwork(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	author := testkit.Identity(t)
	setID, fp := approvedSet(t, f, author)
	vote := func(id *hubwire.Identity, src Source) Response {
		body := hubwire.VoteBody{SetID: setID, Version: 1, FP: fp, Kind: hubwire.VoteWorks}
		return f.svc.HandleFrom(ctx, testkit.Sign(t, id, hubwire.RecordVote, body, f.clock), src)
	}
	direct := testkit.Identity(t)
	relayed := testkit.Identity(t)
	expect(t, vote(direct, Source{IP: peerB}), http.StatusAccepted, "")
	expect(t, vote(relayed, Source{IP: peerC, Relayed: true}), http.StatusAccepted, "")

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
		t.Errorf("a relayed vote must not take the relay's network, got %+v", r)
	}
}

func TestRelayedReportsDoNotHideAVersion(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	setID, _ := approvedSet(t, f, author)
	report := func(peer net.IP, relayed bool) Response {
		body := hubwire.ReportBody{SetID: setID, Version: 1, Reason: "steals traffic"}
		raw := testkit.Sign(t, testkit.Identity(t), hubwire.RecordReport, body, f.clock)
		return f.svc.HandleFrom(context.Background(), raw, Source{IP: peer, Relayed: relayed})
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

func TestRelayedByMatchesTheMirrorAgent(t *testing.T) {
	for agent, want := range map[string]bool{"b4hub-mirror": true, "b4hub-mirror/1.3.0": true, "b4/1.83.1": false, "": false, "curl/8.5": false} {
		if got := RelayedBy(agent); got != want {
			t.Errorf("%q: got %v, want %v", agent, got, want)
		}
	}
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
