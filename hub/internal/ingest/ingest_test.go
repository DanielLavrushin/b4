package ingest

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/asn"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ratelimit"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

var (
	peerA = net.ParseIP("203.0.113.5")
	peerB = net.ParseIP("198.51.100.7")
	peerC = net.ParseIP("192.0.2.9")

	origins = map[string]testkit.Origin{
		"203.0.113.5":  {ASN: "64500", Country: "RU", Name: "EXAMPLE-A"},
		"198.51.100.7": {ASN: "64501", Country: "DE", Name: "EXAMPLE-B"},
		"192.0.2.9":    {ASN: "64502", Country: "NL", Name: "EXAMPLE-C"},
	}
)

type fixture struct {
	svc   *Service
	store *store.Store
	clock time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	layout := hubdata.Layout{Root: t.TempDir()}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(layout.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fixture{store: st, clock: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)}
	now := func() time.Time { return f.clock }
	f.svc = &Service{
		Store:   st,
		Blobs:   layout.Blobs(),
		Secret:  []byte("0123456789abcdef0123456789abcdef"),
		Limiter: ratelimit.New(now),
		ASN:     asn.New(testkit.CymruLookup(origins), st, now),
		Now:     now,
	}
	return f
}

func (f *fixture) post(t *testing.T, raw []byte, peer net.IP) Response {
	t.Helper()
	return f.svc.Handle(context.Background(), raw, peer)
}

func (f *fixture) share(t *testing.T, id *hubwire.Identity, set config.SetConfig, peer net.IP) Response {
	t.Helper()
	env := testkit.BuildEnvelope(t, &set)
	return f.post(t, testkit.SignShare(t, id, env, f.clock), peer)
}

func expect(t *testing.T, resp Response, status int, code string) {
	t.Helper()
	if resp.Status != status {
		t.Fatalf("expected %d, got %d %v", status, resp.Status, resp.Body)
	}
	if code != "" && resp.Body["code"] != code {
		t.Fatalf("expected code %q, got %v", code, resp.Body)
	}
}

func TestShareAcceptedThenDuplicateStrategy(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	set := testkit.SampleSet("YouTube", "youtube.com", "googlevideo.com")

	resp := f.share(t, author, set, peerA)
	expect(t, resp, http.StatusAccepted, "")
	if resp.Body["status"] != hubwire.SetStatusPending || resp.Body["version"] != 1 {
		t.Fatalf("unexpected acceptance %v", resp.Body)
	}
	setID, _ := resp.Body["set_id"].(string)
	if !hubdata.ValidSetID(setID) {
		t.Fatalf("set id %q is malformed", setID)
	}
	v, err := f.store.GetVersion(context.Background(), setID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if v.ASNObserved != "64500" || v.CountryObserved != "RU" || v.ASNHint != "hint" || v.CountryHint != "XX" {
		t.Errorf("origin not recorded: %+v", v)
	}
	if len(v.Payloads) != 1 || v.Payloads[0].Domain != "www.google.com" || !f.svc.Blobs.Exists(v.Payloads[0].SHA256) {
		t.Errorf("payload blob not stored: %+v", v.Payloads)
	}
	if v.Family != "fake+frag-tls" {
		t.Errorf("unexpected family %q", v.Family)
	}
	if len(v.Flags) != 1 || v.Flags[0] != FlagNeedsPayload {
		t.Errorf("unexpected flags %v", v.Flags)
	}
	if ref, _ := v.Projection["faking"].(map[string]interface{})["payload_file"].(string); ref != hubwire.RefPrefix+v.Payloads[0].SHA256 {
		t.Errorf("projection must reference the blob, got %q", ref)
	}
	votes, _ := f.store.VotesForVersion(context.Background(), setID, 1)
	if len(votes) != 1 || votes[0].Kind != "upload" || !votes[0].OriginVerified {
		t.Errorf("an upload must record the author's vote, got %+v", votes)
	}

	other := testkit.Identity(t)
	resp = f.share(t, other, set, peerB)
	expect(t, resp, http.StatusConflict, CodeDuplicateStrategy)
	if resp.Body["set_id"] != setID || resp.Body["version"] != 1 {
		t.Errorf("duplicate must name the existing version, got %v", resp.Body)
	}
	votes, _ = f.store.VotesForVersion(context.Background(), setID, 1)
	if len(votes) != 2 {
		t.Errorf("a duplicate upload counts as a vote from the second key, got %d votes", len(votes))
	}

	retargeted := testkit.SampleSet("YouTube", "youtube.com")
	resp = f.share(t, other, retargeted, peerB)
	expect(t, resp, http.StatusAccepted, "")
	if resp.Body["set_id"] == setID {
		t.Errorf("same strategy with other targets must be a new set")
	}
}

func TestDerivedFromBySameAuthorBecomesNextVersion(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	first := f.share(t, author, testkit.SampleSet("A", "a.example"), peerA)
	expect(t, first, http.StatusAccepted, "")
	setID := first.Body["set_id"].(string)

	tuned := testkit.SampleSet("A2", "a.example")
	tuned.Faking.TTL = 9
	tuned.Hub = &config.HubOrigin{ID: setID, Version: 1}
	resp := f.share(t, author, tuned, peerA)
	expect(t, resp, http.StatusAccepted, "")
	if resp.Body["set_id"] != setID || resp.Body["version"] != 2 {
		t.Errorf("a derived share by the author must become version 2, got %v", resp.Body)
	}

	stranger := testkit.Identity(t)
	foreign := testkit.SampleSet("A3", "a.example")
	foreign.Faking.TTL = 11
	foreign.Hub = &config.HubOrigin{ID: setID, Version: 2}
	resp = f.share(t, stranger, foreign, peerB)
	expect(t, resp, http.StatusAccepted, "")
	if resp.Body["set_id"] == setID {
		t.Errorf("another key cannot add versions to a foreign set")
	}
	set, _, err := f.store.GetSet(context.Background(), resp.Body["set_id"].(string))
	if err != nil || set.DerivedFromID != setID || set.DerivedFromVersion != 2 {
		t.Errorf("lineage must be kept on the new set, got %+v %v", set, err)
	}
}

func TestRejectsBadSignatureBannedAndUnknownKinds(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	env := testkit.BuildEnvelope(t, &config.SetConfig{})
	raw := testkit.SignShare(t, author, env, f.clock)

	var rec hubwire.Record
	_ = json.Unmarshal(raw, &rec)
	rec.TS++
	tampered, _ := json.Marshal(rec)
	expect(t, f.post(t, tampered, peerA), http.StatusBadRequest, CodeBadSignature)

	expect(t, f.post(t, []byte("{not json"), peerA), http.StatusBadRequest, CodeBadRecord)

	set := testkit.SampleSet("x", "x.example")
	expect(t, f.share(t, author, set, peerA), http.StatusAccepted, "")
	if err := f.store.BanKey(context.Background(), hubdata.KeyHMAC(f.svc.Secret, author.KeyID()), "spam", f.clock); err != nil {
		t.Fatal(err)
	}
	expect(t, f.share(t, author, testkit.SampleSet("y", "y.example"), peerA), http.StatusForbidden, CodeBanned)
}

func TestSetWithoutTargetsAndBadPayloadRefused(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	empty := config.NewSetConfig()
	empty.Name = "empty"
	expect(t, f.share(t, author, empty, peerA), http.StatusBadRequest, CodeInvalidSet)

	env := testkit.BuildEnvelope(t, &config.SetConfig{})
	env.Set = map[string]interface{}{"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}}}
	env.Payloads = []hubwire.Payload{{SHA256: "00", Protocol: hubwire.ProtocolTLS, Data: []byte("garbage")}}
	expect(t, f.post(t, testkit.SignShare(t, author, env, f.clock), peerA), http.StatusBadRequest, CodePayloadInvalid)
}

func TestDuplicateRecordAnswers200(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	env := testkit.BuildEnvelope(t, ptr(testkit.SampleSet("x", "x.example")))
	raw := testkit.SignShare(t, author, env, f.clock)
	first := f.post(t, raw, peerA)
	expect(t, first, http.StatusAccepted, "")
	again := f.post(t, raw, peerA)
	expect(t, again, http.StatusOK, "")
	if again.Body["duplicate"] != true || again.Body["id"] != first.Body["id"] || again.Body["set_id"] != first.Body["set_id"] {
		t.Errorf("replay must answer the original ids, got %v", again.Body)
	}
}

func ptr(set config.SetConfig) *config.SetConfig {
	return &set
}

func TestSharesPerKeyPerDayAreLimited(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	for i := 0; i < ratelimit.SharesPerDay; i++ {
		set := testkit.SampleSet("x", "x.example")
		set.Faking.TTL = uint8(10 + i)
		expect(t, f.share(t, author, set, peerA), http.StatusAccepted, "")
	}
	set := testkit.SampleSet("x", "x.example")
	set.Faking.TTL = 99
	resp := f.share(t, author, set, peerA)
	expect(t, resp, http.StatusTooManyRequests, CodeRateLimited)
	if retry, _ := resp.Body["retry_after"].(int); retry <= 0 {
		t.Errorf("rate limited answers carry retry_after, got %v", resp.Body)
	}
	f.clock = f.clock.Add(ratelimit.Day)
	expect(t, f.share(t, author, set, peerA), http.StatusAccepted, "")
}

func TestNewKeysPerAddressAreLimited(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < ratelimit.NewKeysPerDay; i++ {
		set := testkit.SampleSet("x", "x.example")
		set.Faking.TTL = uint8(10 + i)
		expect(t, f.share(t, testkit.Identity(t), set, peerA), http.StatusAccepted, "")
	}
	set := testkit.SampleSet("x", "x.example")
	set.Faking.TTL = 99
	expect(t, f.share(t, testkit.Identity(t), set, peerA), http.StatusTooManyRequests, CodeRateLimited)
	expect(t, f.share(t, testkit.Identity(t), set, peerB), http.StatusAccepted, "")
}

func approvedSet(t *testing.T, f *fixture, author *hubwire.Identity) (string, string) {
	t.Helper()
	resp := f.share(t, author, testkit.SampleSet("v", "v.example"), peerA)
	expect(t, resp, http.StatusAccepted, "")
	setID := resp.Body["set_id"].(string)
	if err := f.store.Approve(context.Background(), setID, 1, f.clock); err != nil {
		t.Fatal(err)
	}
	v, err := f.store.GetVersion(context.Background(), setID, 1)
	if err != nil {
		t.Fatal(err)
	}
	return setID, v.FP
}

func TestVoteRules(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	voter := testkit.Identity(t)
	pending := f.share(t, author, testkit.SampleSet("p", "p.example"), peerA)
	expect(t, pending, http.StatusAccepted, "")
	pendingID := pending.Body["set_id"].(string)
	pendingVersion, _ := f.store.GetVersion(context.Background(), pendingID, 1)

	vote := func(setID string, version int, fp, kind, domain string, peer net.IP) Response {
		body := hubwire.VoteBody{SetID: setID, Version: version, FP: fp, Kind: kind, Domain: domain}
		return f.post(t, testkit.Sign(t, voter, hubwire.RecordVote, body, f.clock), peer)
	}
	expect(t, vote(pendingID, 1, pendingVersion.FP, hubwire.VoteWorks, "", peerB), http.StatusBadRequest, CodeNotActive)

	setID, fp := approvedSet(t, f, author)
	expect(t, vote(setID, 1, fp, "meh", "", peerB), http.StatusBadRequest, CodeBadRecord)
	expect(t, vote(setID, 1, "deadbeef", hubwire.VoteWorks, "", peerB), http.StatusBadRequest, CodeFingerprint)
	expect(t, vote("01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, fp, hubwire.VoteWorks, "", peerB), http.StatusBadRequest, CodeUnknownSet)
	expect(t, vote(setID, 1, fp, hubwire.VoteWorks, "www.v.example", peerB), http.StatusAccepted, "")
	f.clock = f.clock.Add(time.Minute)
	expect(t, vote(setID, 1, fp, hubwire.VoteBroken, "unrelated.example", peerB), http.StatusAccepted, "")

	votes, _ := f.store.VotesForFP(context.Background(), fp)
	var mine []store.Vote
	for _, v := range votes {
		if v.KeyHMAC == hubdata.KeyHMAC(f.svc.Secret, voter.KeyID()) {
			mine = append(mine, v)
		}
	}
	if len(mine) != 1 || mine[0].Kind != "manual_broken" || mine[0].Weight != -1 || mine[0].ASNObserved != "64501" || mine[0].Domain != "" {
		t.Errorf("newest vote in the bucket must win and an untargeted domain must be dropped, got %+v", mine)
	}
}

func TestThreeIndependentReportsHideAVersion(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	setID, _ := approvedSet(t, f, author)
	report := func(id *hubwire.Identity, peer net.IP) Response {
		body := hubwire.ReportBody{SetID: setID, Version: 1, Reason: "steals traffic"}
		return f.post(t, testkit.Sign(t, id, hubwire.RecordReport, body, f.clock), peer)
	}
	expect(t, report(testkit.Identity(t), peerA), http.StatusAccepted, "")
	expect(t, report(testkit.Identity(t), peerA), http.StatusAccepted, "")
	expect(t, report(testkit.Identity(t), peerB), http.StatusAccepted, "")
	v, _ := f.store.GetVersion(context.Background(), setID, 1)
	if v.Status != hubwire.SetStatusActive {
		t.Fatalf("two asns are not enough to hide, got %s", v.Status)
	}
	expect(t, report(testkit.Identity(t), peerC), http.StatusAccepted, "")
	v, _ = f.store.GetVersion(context.Background(), setID, 1)
	if v.Status != hubwire.SetStatusHidden || v.StatusReason != ReasonReports {
		t.Errorf("three keys in three asns must hide the version, got %s %q", v.Status, v.StatusReason)
	}
	expect(t, report(testkit.Identity(t), peerC), http.StatusAccepted, "")
}

func TestTargetsKeyIsCanonical(t *testing.T) {
	a := map[string]interface{}{"targets": map[string]interface{}{
		"sni_domains":        []interface{}{"B.example", "a.example."},
		"geosite_categories": []interface{}{"YouTube"},
	}}
	b := map[string]interface{}{"targets": map[string]interface{}{
		"sni_domains":        []interface{}{"a.example", "b.example", "b.example"},
		"geosite_categories": []interface{}{"youtube"},
	}}
	if TargetsKey(a) != TargetsKey(b) {
		t.Errorf("order, case, trailing dots and duplicates must not change the key")
	}
	c := map[string]interface{}{"targets": map[string]interface{}{"sni_domains": []interface{}{"a.example"}}}
	if TargetsKey(a) == TargetsKey(c) {
		t.Errorf("different targets must differ")
	}
}

func TestFlags(t *testing.T) {
	blanket := map[string]interface{}{
		"targets": map[string]interface{}{"geosite_categories": []interface{}{"category-ads-all"}},
		"routing": map[string]interface{}{"enabled": true, "mode": "block"},
	}
	got := Flags(blanket, nil)
	if len(got) != 2 || got[0] != FlagBlock || got[1] != FlagBlanket {
		t.Errorf("unexpected flags %v", got)
	}
	catchAll := map[string]interface{}{
		"targets": map[string]interface{}{"sni_domains": []interface{}{"regexp:.*"}},
		"dns":     map[string]interface{}{"pins": map[string]interface{}{"a.example": []interface{}{"1.2.3.4"}}},
	}
	got = Flags(catchAll, []hubwire.BlobRef{{SHA256: "x"}})
	if len(got) != 3 || got[0] != FlagNeedsPayload || got[1] != FlagHasPins || got[2] != FlagCatchAll {
		t.Errorf("unexpected flags %v", got)
	}
}

func TestBlobsDirIsUnderLayout(t *testing.T) {
	layout := hubdata.Layout{Root: t.TempDir()}
	if filepath.Dir(layout.Blobs().Dir) != layout.Root {
		t.Errorf("blobs must live under the data root")
	}
}
