package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

const (
	contractAuthorIP      = "203.0.113.5"
	contractVoterIP       = "198.51.100.7"
	contractSigningDomain = "b4hub/1\n"
	contractManifestLimit = 256 << 10
)

var contractCatalogueFile = regexp.MustCompile(`^catalogue-([0-9]+)-([0-9]+)\.json\.gz$`)

var (
	contractManifestTags = []string{
		"v", "key_id", "epoch", "seq", "generated_at", "expires_at", "catalogue",
		"mirrors,omitempty", "geo_sources,omitempty", "doh_allowlist,omitempty", "revoked_keys,omitempty", "sig,omitempty",
	}
	contractFileRefTags   = []string{"file", "sha256", "size"}
	contractGeoSourceTags = []string{"site_url,omitempty", "ip_url,omitempty"}

	contractCatalogueTags = []string{"epoch", "seq", "generated_at", "sets", "blobs,omitempty", "asn_names,omitempty"}
	contractSetTags       = []string{
		"id", "version", "fp", "title", "description,omitempty", "author", "b4_min", "b4_version,omitempty",
		"engine,omitempty", "family,omitempty", "flags,omitempty", "status", "created_at", "updated_at",
		"geo,omitempty", "set", "payloads,omitempty", "scores", "derived_from,omitempty",
	}
	contractBlobRefTags = []string{"sha256", "protocol", "domain,omitempty", "size"}
	contractScoresTags  = []string{"global", "asn,omitempty", "cc,omitempty"}
	contractScoreTags   = []string{"score", "n", "devices", "newest,omitempty"}
	contractOriginTags  = []string{"id", "version,omitempty"}

	contractMessageKeys = []string{
		"id", "kind", "set_id", "version", "status", "duplicate", "queued",
		"code", "error", "retry_after", "scope", "limit", "window",
	}
)

type routerFileRef struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type routerGeoSource struct {
	SiteURL string `json:"site_url,omitempty"`
	IPURL   string `json:"ip_url,omitempty"`
}

type routerManifest struct {
	V            int               `json:"v"`
	KeyID        string            `json:"key_id"`
	Epoch        int64             `json:"epoch"`
	Seq          int64             `json:"seq"`
	GeneratedAt  string            `json:"generated_at"`
	ExpiresAt    string            `json:"expires_at"`
	Catalogue    routerFileRef     `json:"catalogue"`
	Mirrors      []string          `json:"mirrors,omitempty"`
	GeoSources   []routerGeoSource `json:"geo_sources,omitempty"`
	DoHAllowlist []string          `json:"doh_allowlist,omitempty"`
	RevokedKeys  []string          `json:"revoked_keys,omitempty"`
	Sig          string            `json:"sig,omitempty"`
}

type contractFixture struct {
	h         *hub
	active    string
	activeFP  string
	pending   string
	pendingFP string
	hidden    string
}

func startContractHub(t *testing.T) *contractFixture {
	t.Helper()
	ctx := context.Background()
	f := &contractFixture{h: startHub(t)}
	f.active, f.activeFP = f.share(t, "YouTube", "youtube.com", "googlevideo.com")
	f.pending, f.pendingFP = f.share(t, "Discord", "discord.com")
	f.hidden, _ = f.share(t, "Instagram", "instagram.com")
	for _, id := range []string{f.active, f.hidden} {
		if err := f.h.store.Approve(ctx, id, 1, f.h.clock); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.h.store.Hide(ctx, f.hidden, 1, "reports", f.h.clock); err != nil {
		t.Fatal(err)
	}
	if err := f.h.store.RevokeKey(ctx, testkit.Identity(t).KeyID()); err != nil {
		t.Fatal(err)
	}
	f.h.clock = f.h.clock.Add(time.Minute)
	return f
}

func (f *contractFixture) share(t *testing.T, title string, domains ...string) (string, string) {
	t.Helper()
	set := testkit.SampleSet(title, domains...)
	env := testkit.BuildEnvelope(t, &set)
	status, body := f.h.post(testkit.SignShare(t, testkit.Identity(t), env, f.h.clock), contractAuthorIP)
	if status != http.StatusAccepted {
		t.Fatalf("share %s: %d %v", title, status, body)
	}
	assertSubset(t, "share answer", body, contractMessageKeys)
	for _, key := range []string{"id", "kind", "set_id", "version", "status"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("a router stamps its local set from %q, the share answer lacks it: %v", key, body)
		}
	}
	id, _ := body["set_id"].(string)
	if !hubdata.ValidSetID(id) || body["version"] != float64(1) || body["status"] != "pending" || body["kind"] != "share" {
		t.Fatalf("unexpected share answer %v", body)
	}
	return id, env.Fingerprint
}

func (f *contractFixture) build(t *testing.T) {
	t.Helper()
	if _, err := f.h.builder.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (f *contractFixture) manifest(t *testing.T) ([]byte, hubwire.Manifest) {
	t.Helper()
	resp, raw := f.h.get(hubwire.PathManifest)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest: %d %s", resp.StatusCode, raw)
	}
	var m hubwire.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest does not decode as hubwire.Manifest: %v", err)
	}
	return raw, m
}

func (f *contractFixture) catalogueFile(t *testing.T, name string) []byte {
	t.Helper()
	resp, raw := f.h.get(hubwire.PathFiles + name)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("catalogue file %s: %d %s", name, resp.StatusCode, raw)
	}
	return raw
}

func tagName(tag string) (string, bool) {
	name, opts, _ := strings.Cut(tag, ",")
	return name, slices.Contains(strings.Split(opts, ","), "omitempty")
}

func tagNames(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		name, _ := tagName(tag)
		out = append(out, name)
	}
	return out
}

func assertSubset(t *testing.T, what string, obj map[string]interface{}, allowed []string) {
	t.Helper()
	for key := range obj {
		if !slices.Contains(allowed, key) {
			t.Errorf("%s carries %q, which 1.82/1.83 routers do not know; frozen keys are %v", what, key, allowed)
		}
	}
}

func assertKeys(t *testing.T, what string, obj map[string]interface{}, frozen []string) {
	t.Helper()
	assertSubset(t, what, obj, tagNames(frozen))
	for _, tag := range frozen {
		name, optional := tagName(tag)
		if _, ok := obj[name]; !ok && !optional {
			t.Errorf("%s lacks the required key %q", what, name)
		}
	}
}

func assertExactKeys(t *testing.T, what string, obj map[string]interface{}, want []string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for key := range obj {
		got = append(got, key)
	}
	sort.Strings(got)
	sorted := append([]string{}, want...)
	sort.Strings(sorted)
	if !slices.Equal(got, sorted) {
		t.Errorf("%s keys are %v, want exactly %v", what, got, sorted)
	}
}

func object(t *testing.T, what string, v interface{}) map[string]interface{} {
	t.Helper()
	obj, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("%s is %T, want a JSON object", what, v)
	}
	return obj
}

func objects(t *testing.T, what string, v interface{}) []map[string]interface{} {
	t.Helper()
	list, ok := v.([]interface{})
	if !ok {
		t.Fatalf("%s is %T, want a JSON array", what, v)
	}
	out := make([]map[string]interface{}, 0, len(list))
	for i, item := range list {
		out = append(out, object(t, what+"["+strconv.Itoa(i)+"]", item))
	}
	return out
}

func decodeObject(t *testing.T, what string, raw []byte) map[string]interface{} {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]interface{}
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("%s does not decode: %v", what, err)
	}
	return obj
}

func gunzip(t *testing.T, gz []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatalf("catalogue is not gzip: %v", err)
	}
	defer zr.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(zr); err != nil {
		t.Fatalf("catalogue does not decompress: %v", err)
	}
	return buf.Bytes()
}

func routerVerify(t *testing.T, raw []byte, hubKey ed25519.PublicKey) routerManifest {
	t.Helper()
	var m routerManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("a 1.82/1.83 router cannot decode the manifest: %v", err)
	}
	if m.V != 1 {
		t.Fatalf("a 1.82/1.83 router accepts wire version 1 only, got %d", m.V)
	}
	if m.KeyID != base64.RawURLEncoding.EncodeToString(hubKey) {
		t.Fatalf("manifest key_id %q is not the hub key", m.KeyID)
	}
	sig, err := base64.RawURLEncoding.DecodeString(m.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		t.Fatalf("manifest sig is not a raw-url base64 ed25519 signature: %q", m.Sig)
	}
	unsigned := m
	unsigned.Sig = ""
	first, err := json.Marshal(&unsigned)
	if err != nil {
		t.Fatal(err)
	}
	var generic interface{}
	if err := json.Unmarshal(first, &generic); err != nil {
		t.Fatal(err)
	}
	canon, err := json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	digest := append([]byte(contractSigningDomain), canon...)
	if !ed25519.Verify(hubKey, digest, sig) {
		t.Fatalf("a 1.82/1.83 router rejects this manifest: its signature covers fields the frozen v1 manifest does not have")
	}
	return m
}

func jsonShape(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Slice:
		return "[]" + jsonShape(t.Elem())
	case reflect.Pointer:
		return "*" + jsonShape(t.Elem())
	case reflect.Map:
		return "map[" + jsonShape(t.Key()) + "]" + jsonShape(t.Elem())
	case reflect.Struct:
		fields := make([]string, 0, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			fields = append(fields, f.Tag.Get("json")+" "+jsonShape(f.Type))
		}
		sort.Strings(fields)
		return "{" + strings.Join(fields, "; ") + "}"
	}
	return t.Kind().String()
}

func structTags(t reflect.Type) []string {
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		out = append(out, t.Field(i).Tag.Get("json"))
	}
	sort.Strings(out)
	return out
}

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

func TestContractManifestKeysFrozen(t *testing.T) {
	f := startContractHub(t)
	f.build(t)
	raw, _ := f.manifest(t)
	if len(raw) > contractManifestLimit {
		t.Fatalf("manifest is %d bytes, 1.82/1.83 routers read at most %d", len(raw), contractManifestLimit)
	}
	m := decodeObject(t, "manifest", raw)
	assertKeys(t, "manifest", m, contractManifestTags)
	for _, key := range []string{"sig", "mirrors", "geo_sources", "doh_allowlist", "revoked_keys"} {
		if _, ok := m[key]; !ok {
			t.Errorf("the fixture populates %q, the served manifest lacks it", key)
		}
	}
	assertExactKeys(t, "manifest", m, tagNames(contractManifestTags))
	if v, _ := m["v"].(json.Number); v.String() != "1" {
		t.Errorf("manifest v is %v, 1.82/1.83 routers accept 1 only", m["v"])
	}
	assertExactKeys(t, "manifest catalogue", object(t, "manifest catalogue", m["catalogue"]), contractFileRefTags)
	for _, geo := range objects(t, "manifest geo_sources", m["geo_sources"]) {
		assertKeys(t, "manifest geo_sources entry", geo, contractGeoSourceTags)
	}
	for _, field := range []string{"generated_at", "expires_at"} {
		stamp, _ := m[field].(string)
		if _, err := time.Parse(time.RFC3339, stamp); err != nil {
			t.Errorf("manifest %s %q is not RFC 3339: %v", field, stamp, err)
		}
	}
}

func TestContractManifestSignatureOldRouters(t *testing.T) {
	if got, want := jsonShape(reflect.TypeOf(hubwire.Manifest{})), jsonShape(reflect.TypeOf(routerManifest{})); got != want {
		t.Fatalf("hubwire.Manifest no longer matches the manifest 1.82/1.83 routers sign over:\n got %s\nwant %s", got, want)
	}
	if got := structTags(reflect.TypeOf(routerManifest{})); !slices.Equal(got, sortedCopy(contractManifestTags)) {
		t.Fatalf("routerManifest tags %v drifted from the frozen list %v", got, contractManifestTags)
	}
	if got := structTags(reflect.TypeOf(routerFileRef{})); !slices.Equal(got, sortedCopy(contractFileRefTags)) {
		t.Fatalf("routerFileRef tags %v drifted from the frozen list %v", got, contractFileRefTags)
	}
	if got := structTags(reflect.TypeOf(routerGeoSource{})); !slices.Equal(got, sortedCopy(contractGeoSourceTags)) {
		t.Fatalf("routerGeoSource tags %v drifted from the frozen list %v", got, contractGeoSourceTags)
	}

	f := startContractHub(t)
	f.build(t)
	raw, m := f.manifest(t)
	old := routerVerify(t, raw, f.h.identity.Public())
	if err := hubwire.VerifyManifest(&m, []string{f.h.identity.KeyID()}); err != nil {
		t.Fatalf("hubwire.VerifyManifest rejects the served manifest: %v", err)
	}
	if old.Epoch != m.Epoch || old.Seq != m.Seq || old.Catalogue.File != m.Catalogue.File || len(old.RevokedKeys) != 1 {
		t.Fatalf("the frozen decode disagrees with hubwire: %+v vs %+v", old, m)
	}

	tampered := decodeObject(t, "manifest", raw)
	tampered["seq"] = json.Number(strconv.FormatInt(m.Seq+1, 10))
	changed, err := json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	var bad hubwire.Manifest
	if err := json.Unmarshal(changed, &bad); err != nil {
		t.Fatal(err)
	}
	if err := hubwire.VerifyManifest(&bad, []string{f.h.identity.KeyID()}); err == nil {
		t.Fatalf("a manifest with a changed seq must not verify")
	}
}

func TestContractCatalogueEntries(t *testing.T) {
	f := startContractHub(t)
	f.build(t)
	_, m := f.manifest(t)

	match := contractCatalogueFile.FindStringSubmatch(m.Catalogue.File)
	if match == nil {
		t.Fatalf("catalogue file %q is not catalogue-<epoch>-<seq>.json.gz", m.Catalogue.File)
	}
	if match[1] != strconv.FormatInt(m.Epoch, 10) || match[2] != strconv.FormatInt(m.Seq, 10) {
		t.Fatalf("catalogue file %q does not carry the manifest epoch %d and seq %d", m.Catalogue.File, m.Epoch, m.Seq)
	}
	gz := f.catalogueFile(t, m.Catalogue.File)
	if int64(len(gz)) != m.Catalogue.Size || hubwire.BlobHash(gz) != m.Catalogue.SHA256 {
		t.Fatalf("catalogue bytes do not match the manifest size and sha256")
	}

	cat := decodeObject(t, "catalogue", gunzip(t, gz))
	assertKeys(t, "catalogue", cat, contractCatalogueTags)
	if epoch, _ := cat["epoch"].(json.Number); epoch.String() != strconv.FormatInt(m.Epoch, 10) {
		t.Errorf("catalogue epoch %v differs from the manifest %d", cat["epoch"], m.Epoch)
	}
	if seq, _ := cat["seq"].(json.Number); seq.String() != strconv.FormatInt(m.Seq, 10) {
		t.Errorf("catalogue seq %v differs from the manifest %d", cat["seq"], m.Seq)
	}

	sets := objects(t, "catalogue sets", cat["sets"])
	if len(sets) != 1 || sets[0]["id"] != f.active {
		ids := make([]interface{}, 0, len(sets))
		for _, s := range sets {
			ids = append(ids, s["id"])
		}
		t.Fatalf("the catalogue must list only the active set %s, got %v", f.active, ids)
	}
	for i, entry := range sets {
		what := "catalogue set " + strconv.Itoa(i)
		assertKeys(t, what, entry, contractSetTags)
		if entry["status"] != "active" {
			t.Errorf("%s has status %v, routers list only active entries", what, entry["status"])
		}
		object(t, what+" set", entry["set"])
		scores := object(t, what+" scores", entry["scores"])
		assertKeys(t, what+" scores", scores, contractScoresTags)
		assertKeys(t, what+" scores global", object(t, what+" scores global", scores["global"]), contractScoreTags)
		for _, bucket := range []string{"asn", "cc"} {
			if cells, ok := scores[bucket]; ok {
				for key, cell := range object(t, what+" scores "+bucket, cells) {
					assertKeys(t, what+" scores "+bucket+" "+key, object(t, what+" scores "+bucket+" "+key, cell), contractScoreTags)
				}
			}
		}
		if payloads, ok := entry["payloads"]; ok {
			for _, ref := range objects(t, what+" payloads", payloads) {
				assertKeys(t, what+" payload", ref, contractBlobRefTags)
			}
		}
		if geo, ok := entry["geo"]; ok {
			assertKeys(t, what+" geo", object(t, what+" geo", geo), contractGeoSourceTags)
		}
		if origin, ok := entry["derived_from"]; ok {
			assertKeys(t, what+" derived_from", object(t, what+" derived_from", origin), contractOriginTags)
		}
	}
	if blobs, ok := cat["blobs"]; ok {
		for _, ref := range objects(t, "catalogue blobs", blobs) {
			assertKeys(t, "catalogue blob", ref, contractBlobRefTags)
		}
	}
}

func TestContractMonotonicPublishing(t *testing.T) {
	f := startContractHub(t)
	f.build(t)
	firstRaw, first := f.manifest(t)
	routerVerify(t, firstRaw, f.h.identity.Public())
	firstGz := f.catalogueFile(t, first.Catalogue.File)

	f.h.clock = f.h.clock.Add(time.Minute)
	f.build(t)
	secondRaw, second := f.manifest(t)

	if second.Epoch < first.Epoch || (second.Epoch == first.Epoch && second.Seq <= first.Seq) {
		t.Fatalf("(epoch, seq) must strictly increase: %d-%d then %d-%d", first.Epoch, first.Seq, second.Epoch, second.Seq)
	}
	if !second.Newer(&first) || first.Newer(&second) {
		t.Fatalf("routers must see %d-%d as newer than %d-%d", second.Epoch, second.Seq, first.Epoch, first.Seq)
	}
	if second.Catalogue.File == first.Catalogue.File {
		t.Fatalf("a new build must publish a new catalogue file, both are %s", first.Catalogue.File)
	}
	if err := hubwire.VerifyManifest(&second, []string{f.h.identity.KeyID()}); err != nil {
		t.Fatalf("the rebuilt manifest must verify: %v", err)
	}
	routerVerify(t, secondRaw, f.h.identity.Public())

	again := f.catalogueFile(t, first.Catalogue.File)
	if !bytes.Equal(again, firstGz) || hubwire.BlobHash(again) != first.Catalogue.SHA256 {
		t.Fatalf("%s must still be served unchanged after the next build, a router holding the older manifest downloads it", first.Catalogue.File)
	}
	secondGz := f.catalogueFile(t, second.Catalogue.File)
	if hubwire.BlobHash(secondGz) != second.Catalogue.SHA256 || int64(len(secondGz)) != second.Catalogue.Size {
		t.Fatalf("the rebuilt catalogue does not match its manifest")
	}
}

func TestContractMessageRefusals(t *testing.T) {
	f := startContractHub(t)
	ctx := context.Background()
	h := f.h
	voter := testkit.Identity(t)

	unknownID, err := hubdata.NewSetID(h.clock)
	if err != nil {
		t.Fatal(err)
	}
	banned := testkit.Identity(t)
	if err := h.store.BanKey(ctx, hubdata.KeyHMAC(h.api.Ingest.Secret, banned.KeyID()), "contract", h.clock); err != nil {
		t.Fatal(err)
	}
	works := func(setID, fp string) hubwire.VoteBody {
		return hubwire.VoteBody{SetID: setID, Version: 1, FP: fp, Kind: hubwire.VoteWorks, Domain: "youtube.com", Engine: "nfqueue", B4Version: "1.82.0"}
	}
	shareSet := testkit.SampleSet("Telegram", "telegram.org")

	refusals := []struct {
		name string
		raw  []byte
		code string
	}{
		{"vote for an unknown set", testkit.Sign(t, voter, hubwire.RecordVote, works(unknownID, f.activeFP), h.clock), "unknown_set"},
		{"vote for a pending version", testkit.Sign(t, voter, hubwire.RecordVote, works(f.pending, f.pendingFP), h.clock), "not_active"},
		{"vote with a foreign fingerprint", testkit.Sign(t, voter, hubwire.RecordVote, works(f.active, strings.Repeat("0", 64)), h.clock), "fp_mismatch"},
		{"report for an unknown set", testkit.Sign(t, voter, hubwire.RecordReport, hubwire.ReportBody{SetID: unknownID, Version: 1, Reason: "gone"}, h.clock), "unknown_set"},
		{"vote from a banned key", testkit.Sign(t, banned, hubwire.RecordVote, works(f.active, f.activeFP), h.clock), "banned"},
		{"report from a banned key", testkit.Sign(t, banned, hubwire.RecordReport, hubwire.ReportBody{SetID: f.active, Version: 1, Reason: "spam"}, h.clock), "banned"},
		{"share from a banned key", testkit.SignShare(t, banned, testkit.BuildEnvelope(t, &shareSet), h.clock), "banned"},
	}
	for _, r := range refusals {
		status, body := h.post(r.raw, contractVoterIP)
		if status < 400 || status >= 500 || status == http.StatusTooManyRequests {
			t.Errorf("%s: status %d, routers drop only a 4xx that is not 429, anything else is queued or retried", r.name, status)
		}
		assertSubset(t, r.name+" answer", body, contractMessageKeys)
		if body["code"] != r.code {
			t.Errorf("%s: code %v, want %q", r.name, body["code"], r.code)
		}
		if msg, _ := body["error"].(string); msg == "" {
			t.Errorf("%s: routers show the error text, the answer has none: %v", r.name, body)
		}
	}

	status, body := h.post(testkit.Sign(t, voter, hubwire.RecordVote, works(f.active, f.activeFP), h.clock), contractVoterIP)
	if status != http.StatusAccepted || body["kind"] != "vote" {
		t.Fatalf("accepted vote: %d %v", status, body)
	}
	assertExactKeys(t, "accepted vote", body, []string{"id", "kind"})
	assertSubset(t, "accepted vote", body, contractMessageKeys)
	if id, _ := body["id"].(string); id == "" {
		t.Errorf("accepted vote lacks a record id: %v", body)
	}

	status, body = h.post(testkit.Sign(t, voter, hubwire.RecordReport, hubwire.ReportBody{SetID: f.active, Version: 1, Reason: "stopped working"}, h.clock), contractVoterIP)
	if status != http.StatusAccepted || body["kind"] != "report" {
		t.Fatalf("accepted report: %d %v", status, body)
	}
	assertExactKeys(t, "accepted report", body, []string{"id", "kind"})
	assertSubset(t, "accepted report", body, contractMessageKeys)
	if id, _ := body["id"].(string); id == "" {
		t.Errorf("accepted report lacks a record id: %v", body)
	}
}
