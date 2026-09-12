package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/asn"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/geo"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/ratelimit"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

var origins = map[string]testkit.Origin{
	"203.0.113.5":  {ASN: "64500", Country: "RU", Name: "EXAMPLE-A ISP A"},
	"198.51.100.7": {ASN: "64501", Country: "DE", Name: "EXAMPLE-B ISP B"},
}

type hub struct {
	t        *testing.T
	api      *Server
	server   *httptest.Server
	store    *store.Store
	builder  *catalogue.Builder
	identity *hubwire.Identity
	clock    time.Time
}

func startHub(t *testing.T) *hub {
	t.Helper()
	layout := hubdata.Layout{Root: t.TempDir()}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	hubID, err := hubwire.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.WriteIdentity(hubID, false); err != nil {
		t.Fatal(err)
	}
	secret, err := layout.LoadOrCreateSecret()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(layout.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := &hub{t: t, store: st, identity: hubID, clock: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)}
	now := func() time.Time { return h.clock }
	h.builder = &catalogue.Builder{
		Store:      st,
		Identity:   hubID,
		PublicDir:  layout.Public(),
		PublicURL:  "https://hub.example",
		GeoSources: []hubwire.GeoSource{{SiteURL: geo.DefaultGeoSiteURL, IPURL: geo.DefaultGeoIPURL}},
		Now:        now,
	}
	server := &Server{
		Store:     st,
		Blobs:     layout.Blobs(),
		PublicDir: layout.Public(),
		Ingest: &ingest.Service{
			Store:   st,
			Blobs:   layout.Blobs(),
			Secret:  secret,
			Limiter: ratelimit.New(now),
			ASN:     asn.New(testkit.CymruLookup(origins), st, now),
			Now:     now,
		},
		Catalogue:     h.builder,
		Geo:           geo.NewIndex(layout.Geo() + "/geosite.dat"),
		AdminPassword: "secret",
	}
	h.api = server
	h.server = httptest.NewServer(server.Router())
	t.Cleanup(h.server.Close)
	return h
}

func (h *hub) post(raw []byte, forwardedFor string) (int, map[string]interface{}) {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.server.URL+hubwire.PathMessage, bytes.NewReader(raw))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", forwardedFor)
	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		h.t.Fatalf("response %d does not decode: %v", resp.StatusCode, err)
	}
	return resp.StatusCode, body
}

func (h *hub) get(path string) (*http.Response, []byte) {
	h.t.Helper()
	resp, err := h.server.Client().Get(h.server.URL + path)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp, raw
}

func TestEndToEnd(t *testing.T) {
	h := startHub(t)
	ctx := context.Background()

	resp, raw := h.get(hubwire.PathHealth)
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(raw)) != "ok" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("health: %d %q %s", resp.StatusCode, raw, resp.Header.Get("Content-Type"))
	}
	if resp, _ = h.get(hubwire.PathManifest); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("manifest before any build must be 404, got %d", resp.StatusCode)
	}

	author := testkit.Identity(t)
	set := testkit.SampleSet("YouTube", "youtube.com", "googlevideo.com")
	env := testkit.BuildEnvelope(t, &set)
	if len(env.Payloads) != 1 {
		t.Fatalf("the sample envelope must carry one TLS payload, got %d", len(env.Payloads))
	}
	status, body := h.post(testkit.SignShare(t, author, env, h.clock), "203.0.113.5")
	if status != http.StatusAccepted || body["status"] != hubwire.SetStatusPending || body["kind"] != hubwire.RecordShare {
		t.Fatalf("share: %d %v", status, body)
	}
	setID, _ := body["set_id"].(string)
	if !hubdata.ValidSetID(setID) || body["version"] != float64(1) {
		t.Fatalf("share answer lacks set identity: %v", body)
	}
	if body["id"] == nil || body["id"] == "" {
		t.Fatalf("share answer lacks a record id: %v", body)
	}

	if _, err := h.builder.Build(ctx); err != nil {
		t.Fatal(err)
	}
	hits, _ := h.search("youtube.com")
	if len(hits) != 0 {
		t.Fatalf("a pending set must not be listed, got %d", len(hits))
	}

	if err := h.store.Approve(ctx, setID, 1, h.clock); err != nil {
		t.Fatal(err)
	}
	h.clock = h.clock.Add(time.Minute)
	if _, err := h.builder.Build(ctx); err != nil {
		t.Fatal(err)
	}

	resp, raw = h.get(hubwire.PathManifest)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("manifest: %d %s", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	var m hubwire.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if err := hubwire.VerifyManifest(&m, []string{h.identity.KeyID()}); err != nil {
		t.Fatalf("manifest must verify with the hub key: %v", err)
	}
	if err := hubwire.VerifyManifest(&m, []string{author.KeyID()}); err == nil {
		t.Fatalf("manifest must not verify with a foreign key")
	}
	if m.Seq != 2 || m.Mirrors[0] != "https://hub.example" || len(m.DoHAllowlist) == 0 || m.GeoSources[0].SiteURL != geo.DefaultGeoSiteURL {
		t.Fatalf("unexpected manifest %+v", m)
	}

	resp, raw = h.get(hubwire.PathFiles + m.Catalogue.File)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/gzip" || !strings.Contains(resp.Header.Get("Cache-Control"), "max-age=31536000") {
		t.Fatalf("catalogue file: %d %s %s", resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"))
	}
	if int64(len(raw)) != m.Catalogue.Size || hubwire.BlobHash(raw) != m.Catalogue.SHA256 {
		t.Fatalf("catalogue bytes do not match the manifest")
	}
	cat, err := catalogue.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Epoch != m.Epoch || cat.Seq != m.Seq || len(cat.Sets) != 1 {
		t.Fatalf("unexpected catalogue %+v", cat)
	}
	cs := cat.Sets[0]
	if cs.ID != setID || cs.Version != 1 || cs.FP != env.Fingerprint || cs.Title != "YouTube" || cs.Status != hubwire.SetStatusActive {
		t.Fatalf("unexpected set %+v", cs)
	}
	if len(cs.Payloads) != 1 || cs.Payloads[0].SHA256 != env.Payloads[0].SHA256 || cs.Payloads[0].Domain != "www.google.com" || len(cat.Blobs) != 1 {
		t.Fatalf("payload reference missing: %+v %+v", cs.Payloads, cat.Blobs)
	}
	if ref, _ := cs.Set["faking"].(map[string]interface{})["payload_file"].(string); ref != hubwire.RefPrefix+cs.Payloads[0].SHA256 {
		t.Fatalf("projection must reference the blob, got %q", ref)
	}
	if cs.Scores.Global.Devices != 1 || cs.Scores.ASN["64500"].Devices != 1 || cat.ASNNames["64500"] != "EXAMPLE-A ISP A" {
		t.Fatalf("the upload vote must land in the author's asn cell: %+v %v", cs.Scores, cat.ASNNames)
	}
	if cs.B4Min != hubwire.BaselineVersion || cs.Engine != "nfqueue" || cs.Author == "" || len(cs.Author) > hubdata.AuthorLabelLength {
		t.Fatalf("unexpected metadata %+v", cs)
	}
	roundTrip := cs.ToEnvelope()
	if _, err := hubwire.Open(roundTrip, hubwire.OpenOptions{}); err != nil {
		t.Fatalf("a catalogue set must open as an envelope: %v", err)
	}

	resp, raw = h.get(hubwire.PathBlob + cs.Payloads[0].SHA256)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("blob: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if hubwire.BlobHash(raw) != cs.Payloads[0].SHA256 || len(raw) != cs.Payloads[0].Size {
		t.Fatalf("blob bytes do not match the reference")
	}
	if resp, _ = h.get(hubwire.PathBlob + strings.Repeat("0", 64)); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown blob must be 404, got %d", resp.StatusCode)
	}
	if resp, _ = h.get(hubwire.PathFiles + "catalogue-1-999.json.gz"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown catalogue must be 404, got %d", resp.StatusCode)
	}
	if resp, _ = h.get(hubwire.PathFiles + "../hub.key"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("path escapes must be 404, got %d", resp.StatusCode)
	}

	hits, total := h.search("www.youtube.com")
	if total != 1 || hits[0].ID != setID || hits[0].Match == nil || hits[0].Match.Entry != "youtube.com" || hits[0].Match.Relation != "covered" || hits[0].Match.Via != MatchViaDomain {
		t.Fatalf("search: %d %+v", total, hits)
	}
	if hits, total = h.search("example.org"); total != 0 || len(hits) != 0 {
		t.Fatalf("unrelated domain must not match, got %+v", hits)
	}
	if hits, total = h.search(""); total != 1 || hits[0].Match != nil {
		t.Fatalf("empty search lists everything without a match, got %+v", hits)
	}

	voter := testkit.Identity(t)
	vote := hubwire.VoteBody{SetID: setID, Version: 1, FP: cs.FP, Kind: hubwire.VoteWorks, Domain: "youtube.com", Engine: "nfqueue", B4Version: "1.82.0"}
	status, body = h.post(testkit.Sign(t, voter, hubwire.RecordVote, vote, h.clock), "198.51.100.7")
	if status != http.StatusAccepted || body["kind"] != hubwire.RecordVote || body["id"] == nil {
		t.Fatalf("vote: %d %v", status, body)
	}
	dirty, _ := h.store.Dirty(ctx)
	if !dirty {
		t.Fatalf("a vote must dirty the catalogue")
	}
	result, built, err := h.builder.BuildIfNeeded(ctx)
	if err != nil || !built {
		t.Fatalf("rebuild after vote: %v %v", built, err)
	}
	scored := result.ByID[setID]
	if scored.Scores.Global.Devices != 2 || scored.Scores.Global.N <= cs.Scores.Global.N || scored.Scores.ASN["64501"].Devices != 1 || scored.Scores.CC["DE"].Devices != 1 {
		t.Fatalf("vote not reflected in scores: %+v", scored.Scores)
	}
	if result.Catalogue.ASNNames["64501"] != "EXAMPLE-B ISP B" {
		t.Fatalf("asn names must follow the scored cells, got %v", result.Catalogue.ASNNames)
	}
	if _, _, err := h.builder.BuildIfNeeded(ctx); err != nil {
		t.Fatal(err)
	}
	if h.builder.Latest().Manifest.Seq != 3 {
		t.Fatalf("a clean store must not rebuild, seq %d", h.builder.Latest().Manifest.Seq)
	}

	status, body = h.post(testkit.SignShare(t, author, env, h.clock), "203.0.113.5")
	if status != http.StatusConflict || body["code"] != ingest.CodeDuplicateStrategy || body["set_id"] != setID || body["version"] != float64(1) {
		t.Fatalf("second share: %d %v", status, body)
	}
	replay := testkit.SignShare(t, author, env, h.clock)
	var replayed hubwire.Record
	if err := json.Unmarshal(replay, &replayed); err != nil {
		t.Fatal(err)
	}
	if status, body = h.post(replay, "203.0.113.5"); status != http.StatusConflict {
		t.Fatalf("replay setup: %d %v", status, body)
	}
	status, body = h.post(replay, "203.0.113.5")
	if status != http.StatusOK || body["duplicate"] != true || body["id"] != replayed.ID() || body["set_id"] != setID {
		t.Fatalf("replayed record: %d %v", status, body)
	}

	status, body = h.post([]byte(`{"v":1,"kind":"share","key":"x","ts":1,"nonce":"n","body":{},"sig":"bad"}`), "203.0.113.5")
	if status != http.StatusBadRequest || body["code"] != ingest.CodeBadSignature {
		t.Fatalf("bad key: %d %v", status, body)
	}
	oversized := bytes.Repeat([]byte("x"), ingest.MaxBodyBytes+1)
	if status, body = h.post(oversized, "203.0.113.5"); status != http.StatusRequestEntityTooLarge || body["code"] != ingest.CodeTooLarge {
		t.Fatalf("oversized: %d %v", status, body)
	}
}

func (h *hub) search(domain string) ([]Hit, int) {
	h.t.Helper()
	return h.api.Search(domain, 50)
}

func TestBasicAuthGuardsAdminHandlers(t *testing.T) {
	h := startHub(t)
	server := &Server{AdminPassword: "secret"}
	guarded := server.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
		t.Errorf("missing credentials must be refused, got %d", rec.Code)
	}
	req.SetBasicAuth("admin", "secret")
	rec = httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("valid credentials must pass, got %d", rec.Code)
	}
	unset := &Server{}
	rec = httptest.NewRecorder()
	unset.BasicAuth(guarded).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("an empty admin password must lock the admin area, got %d", rec.Code)
	}
	_ = h
}
