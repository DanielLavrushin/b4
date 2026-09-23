package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/hub"
	"github.com/daniellavrushin/b4/hub/hubtest"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
)

var hubTestBase struct {
	once    sync.Once
	scratch string
	dir     string
}

func TestMain(m *testing.M) {
	code := m.Run()
	if hubTestBase.scratch != "" {
		os.RemoveAll(hubTestBase.scratch)
	}
	os.Exit(code)
}

func hubTestConfigPath(t *testing.T) string {
	t.Helper()
	hubTestBase.once.Do(func() {
		scratch, err := os.MkdirTemp("", "b4-hub-handler-")
		if err != nil {
			panic(err)
		}
		hubTestBase.scratch = scratch
		seed := config.NewConfig()
		seed.ConfigPath = filepath.Join(scratch, "config.json")
		hubTestBase.dir = filepath.Dir(capture.GetManager(&seed).GetOutputPath())
	})
	base := hubTestBase.dir
	if err := os.MkdirAll(filepath.Join(base, "captures"), 0755); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(base, hub.DirName))
	os.Remove(filepath.Join(base, "config.json"))
	return filepath.Join(base, "config.json")
}

func hubAPI(t *testing.T) (*http.ServeMux, *config.Config) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = hubTestConfigPath(t)
	api := &API{cfgPtr: testCfgPtr(&cfg), geodataManager: geodat.NewGeodataManager("", "")}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterHubApi()
	return mux, &cfg
}

type hubEnv struct {
	api *API
	mux *http.ServeMux
	hub *hubtest.FakeHub
	svc *hub.Service
}

func newHubEnv(t *testing.T) *hubEnv {
	t.Helper()
	f := hubtest.New(t)
	cfg := f.Config(t, hubTestConfigPath(t))
	api := &API{cfgPtr: testCfgPtr(cfg), geodataManager: geodat.NewGeodataManager("", "")}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterHubApi()
	svc := hub.New(api.getCfg, hub.Options{Version: "1.83.0", HTTPClient: f.Client(), BuiltinBases: []string{}})
	previous := globalHubService
	SetHubService(svc)
	t.Cleanup(func() { SetHubService(previous) })
	return &hubEnv{api: api, mux: mux, hub: f, svc: svc}
}

func (e *hubEnv) update(fn func(cfg *config.Config)) {
	clone := e.api.getCfg().Clone()
	fn(clone)
	e.api.cfgPtr.Store(clone)
}

func (e *hubEnv) publish(t *testing.T, sets ...hubwire.CatalogueSet) {
	t.Helper()
	e.hub.Publish(t, &hubwire.Catalogue{Epoch: 1, Seq: 1, Sets: sets}, time.Now().Add(time.Hour))
	if _, err := e.svc.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func (e *hubEnv) localSet(id string) *config.SetConfig {
	for _, set := range e.api.getCfg().Sets {
		if set.Id == id {
			return set
		}
	}
	return nil
}

func postJSON(t *testing.T, mux *http.ServeMux, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func getJSON(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, out interface{}) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var ae APIError
	decodeInto(t, rec, &ae)
	return ae.Code
}

func expectCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("expected %d, got %d (%s)", status, rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != code {
		t.Fatalf("expected code %q, got %q (%s)", code, got, rec.Body.String())
	}
}

func hubStrategySet(name string, domains ...string) config.SetConfig {
	set := config.NewSetConfig()
	set.Name = name
	set.Targets.SNIDomains = domains
	set.Fragmentation.Strategy = "tls"
	set.Faking.SNI = true
	set.Faking.TTL = 7
	return set
}

func TestHubEnvelopeStripsPrivateFields(t *testing.T) {
	mux, _ := hubAPI(t)
	set := config.NewSetConfig()
	set.Id = "abc"
	set.Name = "Proxy set"
	set.Targets.SNIDomains = []string{"example.com"}
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeProxy
	set.Routing.Upstream.Host = "10.0.0.5"
	set.Routing.Upstream.Password = "secret"

	rec := postJSON(t, mux, "/api/hub/envelope", set)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Fatalf("credentials leaked into the envelope: %s", rec.Body.String())
	}
	var resp HubEnvelopeResponse
	decodeInto(t, rec, &resp)
	if resp.Envelope.Format != hubwire.Format || resp.Envelope.Title != "Proxy set" {
		t.Errorf("unexpected envelope header %+v", resp.Envelope)
	}
	if _, ok := resp.Envelope.Set["routing"]; ok {
		t.Errorf("routing must not cross")
	}
	found := false
	for _, s := range resp.Report.Stripped {
		if s.Path == "routing" && s.Reason == "routing_not_shareable" {
			found = true
		}
	}
	if !found {
		t.Errorf("report must name the stripped routing block: %+v", resp.Report)
	}
}

func TestHubImportInstallsPayloadAndStampsProvenance(t *testing.T) {
	mux, cfg := hubAPI(t)
	set := config.NewSetConfig()
	set.Name = "Shared"
	set.Targets.SNIDomains = []string{"example.com"}
	set.Faking.SNI = true
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = "captures/tls_www_google_com.bin"
	env, _, err := hubwire.Build(&set, hubwire.BuildOptions{ReadPayload: func(string) ([]byte, error) { return config.FakeSNI1, nil }})
	if err != nil {
		t.Fatal(err)
	}

	rec := postJSON(t, mux, "/api/hub/import", env)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp HubImportResponse
	decodeInto(t, rec, &resp)
	if resp.Set == nil || resp.Set.Hub == nil || resp.Set.Hub.Hash != env.Fingerprint {
		t.Fatalf("provenance not stamped: %+v", resp.Set)
	}
	if resp.Set.Id != "" || resp.Set.Name != "Shared" {
		t.Errorf("imported set must be unsaved and keep its title: %q %q", resp.Set.Id, resp.Set.Name)
	}
	if len(resp.Payloads) != 1 || !strings.HasPrefix(resp.Set.Faking.PayloadFile, "captures/") {
		t.Fatalf("payload not installed: %+v %q", resp.Payloads, resp.Set.Faking.PayloadFile)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(cfg.ConfigPath), resp.Set.Faking.PayloadFile))
	if err != nil {
		t.Fatalf("installed payload is not on disk: %v", err)
	}
	if !bytes.Equal(data, config.FakeSNI1) {
		t.Errorf("installed payload differs from the shared one")
	}
	if got, err := cfg.ReadCapturePayload(resp.Set.Faking.PayloadFile); err != nil || !bytes.Equal(got, config.FakeSNI1) {
		t.Errorf("the set's payload reference must resolve through the config: %v", err)
	}
}

func TestHubImportRejectsOtherFormats(t *testing.T) {
	mux, _ := hubAPI(t)
	rec := postJSON(t, mux, "/api/hub/import", map[string]interface{}{"format": 7, "set": map[string]interface{}{}})
	expectCode(t, rec, http.StatusBadRequest, "unsupported_format")
}

func TestHubEndpointsAreGated(t *testing.T) {
	mux, cfg := hubAPI(t)
	previous := globalHubService
	SetHubService(nil)
	t.Cleanup(func() { SetHubService(previous) })
	builtin := hubwire.BuiltinHubKeys
	hubwire.BuiltinHubKeys = nil
	t.Cleanup(func() { hubwire.BuiltinHubKeys = builtin })

	expectCode(t, getJSON(t, mux, "/api/hub/status"), http.StatusConflict, "hub_disabled")
	expectCode(t, postJSON(t, mux, "/api/hub/share", HubShareRequest{SetID: "x"}), http.StatusConflict, "hub_disabled")
	expectCode(t, getJSON(t, mux, "/api/hub/identity"), http.StatusConflict, "hub_disabled")

	cfg.System.Hub.Enabled = true
	expectCode(t, getJSON(t, mux, "/api/hub/status"), http.StatusServiceUnavailable, "hub_unavailable")

	svc := hub.New(func() *config.Config { return cfg }, hub.Options{BuiltinBases: []string{}})
	SetHubService(svc)
	expectCode(t, getJSON(t, mux, "/api/hub/status"), http.StatusConflict, "hub_not_configured")
	expectCode(t, getJSON(t, mux, "/api/hub/sets?domain=x"), http.StatusConflict, "hub_not_configured")
	expectCode(t, postJSON(t, mux, "/api/hub/sets/x/vote", HubVoteRequest{Kind: "works"}), http.StatusConflict, "hub_not_configured")

	signer, _ := hubwire.NewIdentity()
	cfg.System.Hub.PublicKey = signer.KeyID()
	rec := getJSON(t, mux, "/api/hub/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var st hub.Status
	decodeInto(t, rec, &st)
	if !st.Enabled || !st.Configured || st.KeyID == "" || st.Catalogue != nil || st.Outbox != 0 {
		t.Errorf("unexpected status %+v", st)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/hub/sync", strings.NewReader("{}"))
	req.Header.Set("Origin", "https://another.example")
	foreign := httptest.NewRecorder()
	mux.ServeHTTP(foreign, req)
	if foreign.Code == http.StatusForbidden {
		t.Fatalf("a foreign Origin must not be refused, the UI is often served from another host: %s", foreign.Body.String())
	}
}

func TestHubStatusNamesTheSetsThatMatchItsAddresses(t *testing.T) {
	env := newHubEnv(t)
	env.update(func(cfg *config.Config) {
		catchAll := config.NewSetConfig()
		catchAll.Name = "catch-all"
		catchAll.Targets.SNIDomains = []string{"regexp:.*"}
		catchAll.Enabled = true
		off := config.NewSetConfig()
		off.Name = "disabled"
		off.Targets.SNIDomains = []string{"b4core.app"}
		off.Enabled = false
		cfg.Sets = append(cfg.Sets, &catchAll, &off)
		cfg.System.Hub.URLs = []string{"https://mirror.example/base", env.hub.URL()}
	})
	rec := getJSON(t, env.mux, "/api/hub/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var st hubStatusResponse
	decodeInto(t, rec, &st)
	hosts := map[string]string{}
	for _, m := range st.SetMatches {
		hosts[m.Domain] = m.SetName
	}
	if hosts["mirror.example"] != "catch-all" || hosts["127.0.0.1"] != "catch-all" {
		t.Errorf("the enabled catch-all set must be reported for every hub host, got %+v", st.SetMatches)
	}
	for _, m := range st.SetMatches {
		if m.SetName == "disabled" {
			t.Errorf("a disabled set must not be reported: %+v", m)
		}
	}
}

func TestHubSyncEndpointReportsTheCatalogue(t *testing.T) {
	env := newHubEnv(t)
	set := hubStrategySet("Video", "youtube.com")
	cs, _ := hubtest.CatalogueSet(t, "yt-1", 1, &set, nil)
	env.hub.Publish(t, &hubwire.Catalogue{Epoch: 1, Seq: 1, Sets: []hubwire.CatalogueSet{cs}}, time.Now().Add(time.Hour))

	rec := postJSON(t, env.mux, "/api/hub/sync", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var st hub.Status
	decodeInto(t, rec, &st)
	if st.Catalogue == nil || st.Catalogue.Sets != 1 || st.LastSync == "" {
		t.Fatalf("sync must report the loaded catalogue: %+v", st)
	}

	env.hub.SetDown(true)
	expectCode(t, postJSON(t, env.mux, "/api/hub/sync", map[string]interface{}{}), http.StatusBadGateway, "sync_failed")
}

func TestHubApplyRefusesASetWhosePayloadCannotBeStored(t *testing.T) {
	env := newHubEnv(t)
	shared := hubStrategySet("Needs payload", "example.com")
	shared.Faking.SNIType = config.FakePayloadCapture
	shared.Faking.PayloadFile = "captures/tls_www_google_com.bin"
	cs, payloads := hubtest.CatalogueSet(t, "p-2", 1, &shared, func(string) ([]byte, error) { return config.FakeSNI1, nil })
	for _, p := range payloads {
		env.hub.AddBlob(p)
	}
	env.publish(t, cs)

	captures := capture.GetManager(env.api.getCfg()).GetOutputPath()
	sum := sha256.Sum256(payloads[0].Data)
	clash := []string{
		filepath.Join(captures, "tls_www_google_com.bin"),
		filepath.Join(captures, "tls_www_google_com-"+hex.EncodeToString(sum[:4])+".bin"),
	}
	saved := map[string][]byte{}
	for _, path := range clash {
		if old, err := os.ReadFile(path); err == nil {
			saved[path] = old
		}
		if err := os.WriteFile(path, []byte("not the shared payload"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, path := range clash {
			if old, ok := saved[path]; ok {
				os.WriteFile(path, old, 0644)
			} else {
				os.Remove(path)
			}
		}
	})

	rec := postJSON(t, env.mux, "/api/hub/sets/p-2/apply", map[string]interface{}{})
	expectCode(t, rec, http.StatusInternalServerError, "payload_install_failed")
	if !strings.Contains(rec.Body.String(), `"path":"faking.payload_file"`) {
		t.Errorf("the error must name the payload slot, got %s", rec.Body.String())
	}
	if len(env.api.getCfg().Sets) != 0 {
		t.Errorf("no set may be created when the payload cannot be stored")
	}

	body := map[string]interface{}{}
	raw, _ := json.Marshal(cs.ToEnvelope())
	json.Unmarshal(raw, &body)
	body["payloads"] = []interface{}{map[string]interface{}{"sha256": payloads[0].SHA256, "protocol": payloads[0].Protocol, "domain": payloads[0].Domain, "size": payloads[0].Size, "data": payloads[0].Data}}
	expectCode(t, postJSON(t, env.mux, "/api/hub/import", body), http.StatusInternalServerError, "payload_install_failed")
}

func TestHubApplyCreatesSetWithPayloadAndStamp(t *testing.T) {
	env := newHubEnv(t)
	shared := hubStrategySet("Shared video", "youtube.com", "googlevideo.com")
	shared.Targets.GeoSiteCategories = []string{"youtube"}
	shared.Faking.SNIType = config.FakePayloadCapture
	shared.Faking.PayloadFile = "captures/tls_www_google_com.bin"
	cs, payloads := hubtest.CatalogueSet(t, "yt-1", 2, &shared, func(string) ([]byte, error) { return config.FakeSNI1, nil })
	if len(payloads) != 1 {
		t.Fatalf("the fixture must carry one payload, got %d", len(payloads))
	}
	for _, p := range payloads {
		env.hub.AddBlob(p)
	}
	env.publish(t, cs)

	old := hubStrategySet("Old", "youtube.com", "example.org")
	old.Id = "old-1"
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{&old} })

	rec := postJSON(t, env.mux, "/api/hub/sets/yt-1/apply", map[string]interface{}{})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp HubApplyResponse
	decodeInto(t, rec, &resp)
	if resp.ID == "" || resp.Name != "Shared video" {
		t.Fatalf("unexpected reply %+v", resp)
	}
	if len(resp.Moved) != 1 || resp.Moved[0].Domain != "youtube.com" || resp.Moved[0].SetId != "old-1" {
		t.Errorf("youtube.com must be released from the older set: %+v", resp.Moved)
	}
	if len(resp.Payloads) != 1 || !strings.HasPrefix(resp.Payloads[0].File, "captures/") {
		t.Errorf("payload must be installed: %+v", resp.Payloads)
	}
	geoWarned := false
	for _, w := range resp.Warnings {
		if w.Code == "geosite_missing" {
			geoWarned = true
		}
	}
	if !geoWarned {
		t.Errorf("dropping the geosite category must be reported: %+v", resp.Warnings)
	}

	cfg := env.api.getCfg()
	if len(cfg.Sets) != 2 || cfg.Sets[0].Id != resp.ID {
		t.Fatalf("the applied set must be saved in front: %+v", cfg.Sets)
	}
	applied := cfg.Sets[0]
	if applied.Hub == nil || applied.Hub.ID != "yt-1" || applied.Hub.Version != 2 || applied.Hub.Hash != cs.FP || applied.Hub.AppliedAt == "" {
		t.Fatalf("hub stamp missing or wrong: %+v (want fp %s)", applied.Hub, cs.FP)
	}
	if !applied.Enabled || applied.Faking.TTL != 7 || applied.Fragmentation.Strategy != "tls" {
		t.Errorf("strategy lost on apply: %+v", applied.Faking)
	}
	if len(applied.Targets.GeoSiteCategories) != 0 {
		t.Errorf("geosite categories must be dropped without a database: %v", applied.Targets.GeoSiteCategories)
	}
	if len(applied.Targets.DomainsToMatch) != 2 {
		t.Errorf("targets must be expanded for matching: %v", applied.Targets.DomainsToMatch)
	}
	data, err := cfg.ReadCapturePayload(applied.Faking.PayloadFile)
	if err != nil || !bytes.Equal(data, config.FakeSNI1) {
		t.Fatalf("installed payload must resolve through the config: %q %v", applied.Faking.PayloadFile, err)
	}
	if state := hubStateOf(cfg, applied); state != HubStateUnmodified {
		t.Errorf("a freshly applied set must read as unmodified, got %q", state)
	}
	if kept := env.localSet("old-1"); kept == nil || len(kept.Targets.SNIDomains) != 1 || kept.Targets.SNIDomains[0] != "example.org" {
		t.Errorf("older set must keep only its other domain: %+v", kept)
	}
	if _, err := os.Stat(cfg.ConfigPath); err != nil {
		t.Errorf("config must be saved to disk: %v", err)
	}

	rec = getJSON(t, env.mux, "/api/hub/sets?domain=www.youtube.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var list HubSetsResponse
	decodeInto(t, rec, &list)
	if list.Total != 1 || len(list.Sets) != 1 {
		t.Fatalf("expected one match, got %+v", list)
	}
	view := list.Sets[0]
	if view.ID != "yt-1" || view.Match == nil || view.Match.Via != hub.MatchViaDomain || view.Match.Entry != "youtube.com" {
		t.Errorf("unexpected match %+v", view.Match)
	}
	if view.Applied == nil || view.Applied.SetID != resp.ID || view.Applied.HubState != HubStateUnmodified {
		t.Errorf("the applied local set must be reported: %+v", view.Applied)
	}
	if len(view.Targets.Domains) != 2 || len(view.Targets.GeoSite) != 1 || view.Targets.IPCount != 0 || len(view.Payloads) != 1 || view.Flags == nil {
		t.Errorf("unexpected targets projection %+v %+v %v", view.Targets, view.Payloads, view.Flags)
	}

	rec = getJSON(t, env.mux, "/api/hub/sets/yt-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var one HubSet
	decodeInto(t, rec, &one)
	if one.Applied == nil || one.Match != nil || one.Display.Bucket != hubwire.BucketNone {
		t.Errorf("unexpected single set view %+v", one)
	}
	expectCode(t, getJSON(t, env.mux, "/api/hub/sets/missing"), http.StatusNotFound, "not_found")
	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/missing/apply", map[string]interface{}{}), http.StatusNotFound, "not_found")
}

func TestHubApplyFailsWhenThePayloadIsUnreachable(t *testing.T) {
	env := newHubEnv(t)
	shared := hubStrategySet("Needs payload", "example.com")
	shared.Faking.SNIType = config.FakePayloadCapture
	shared.Faking.PayloadFile = "captures/tls_www_google_com.bin"
	cs, _ := hubtest.CatalogueSet(t, "p-1", 1, &shared, func(string) ([]byte, error) { return config.FakeSNI1, nil })
	env.publish(t, cs)

	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/p-1/apply", map[string]interface{}{}), http.StatusBadGateway, "payload_missing")

	env.hub.SetDown(true)
	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/p-1/apply", map[string]interface{}{}), http.StatusBadGateway, "hub_unreachable")
	if len(env.api.getCfg().Sets) != 0 {
		t.Errorf("no set may be created when the payload cannot be fetched")
	}
}

func TestHubVoteRefusesModifiedSets(t *testing.T) {
	env := newHubEnv(t)
	local := hubStrategySet("Applied", "example.com")
	local.Id = "local-1"
	fp, err := hubwire.LiveFingerprint(&local, nil)
	if err != nil {
		t.Fatal(err)
	}
	local.Hub = &config.HubOrigin{ID: "yt-1", Version: 3, Hash: fp, AppliedAt: "2026-09-12T10:00:00Z"}
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{&local} })

	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/other/vote", HubVoteRequest{Kind: "works"}), http.StatusConflict, "not_applied")
	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/yt-1/vote", HubVoteRequest{Kind: "meh"}), http.StatusBadRequest, "bad_request")

	rec := postJSON(t, env.mux, "/api/hub/sets/yt-1/vote", HubVoteRequest{Kind: "works", Domain: "Example.com"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	var voted HubVoteResponse
	decodeInto(t, rec, &voted)
	if !voted.Sent || voted.Queued {
		t.Errorf("the vote must be delivered while the hub answers: %+v", voted)
	}
	records := env.hub.Records()
	if len(records) != 1 || records[0].Kind != hubwire.RecordVote {
		t.Fatalf("hub must have received one vote, got %+v", records)
	}
	var body hubwire.VoteBody
	if err := json.Unmarshal(records[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.SetID != "yt-1" || body.Version != 3 || body.FP != fp || body.Kind != hubwire.VoteWorks || body.Domain != "example.com" || body.B4Version != Version {
		t.Errorf("unexpected vote body %+v", body)
	}

	env.api.getCfg().Sets[0].Faking.TTL = 3
	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/yt-1/vote", HubVoteRequest{Kind: "broken"}), http.StatusConflict, "modified")

	env.api.getCfg().Sets[0].Faking.TTL = 7
	env.hub.SetDown(true)
	rec = postJSON(t, env.mux, "/api/hub/sets/yt-1/vote", HubVoteRequest{Kind: "broken"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	decodeInto(t, rec, &voted)
	if voted.Sent || !voted.Queued || env.svc.OutboxCount() != 1 {
		t.Errorf("a vote must be queued while the hub is down: %+v outbox=%d", voted, env.svc.OutboxCount())
	}

	env.hub.SetDown(false)
	env.hub.SetAnswer(func(*hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: http.StatusForbidden, Body: map[string]string{"code": "banned", "error": "key banned"}}
	})
	rec = postJSON(t, env.mux, "/api/hub/sets/yt-1/vote", HubVoteRequest{Kind: "works"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a hub refusal must pass through, got %d (%s)", rec.Code, rec.Body.String())
	}
	var remote HubRemoteError
	decodeInto(t, rec, &remote)
	if remote.Code != "banned" || remote.Message != "key banned" {
		t.Errorf("unexpected remote error %+v", remote)
	}
}

func TestHubShareRoundTrip(t *testing.T) {
	env := newHubEnv(t)
	local := hubStrategySet("My strategy", "example.com")
	local.Id = "local-1"
	local.Routing.Enabled = true
	local.Routing.Mode = config.RoutingModeProxy
	local.Routing.Upstream.Host = "10.0.0.5"
	local.Routing.Upstream.Port = 1080
	local.Routing.Upstream.Password = "secret"
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{&local} })

	expectCode(t, postJSON(t, env.mux, "/api/hub/share", HubShareRequest{SetID: "nope"}), http.StatusNotFound, "not_found")
	expectCode(t, postJSON(t, env.mux, "/api/hub/share", HubShareRequest{}), http.StatusBadRequest, "bad_request")

	rec := postJSON(t, env.mux, "/api/hub/share", HubShareRequest{SetID: "local-1", Description: "works on my ISP"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp HubShareResponse
	decodeInto(t, rec, &resp)
	if resp.HubID == "" || resp.Version != 1 || resp.Status != hubwire.SetStatusPending {
		t.Fatalf("unexpected share reply %+v", resp)
	}
	records := env.hub.Records()
	if len(records) != 1 || records[0].Kind != hubwire.RecordShare {
		t.Fatalf("hub must have received one share, got %+v", records)
	}
	if _, err := hubwire.VerifyRecord(records[0]); err != nil {
		t.Fatalf("share record must verify: %v", err)
	}
	var body hubwire.ShareBody
	if err := json.Unmarshal(records[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Envelope.Title != "My strategy" || body.Envelope.Description != "works on my ISP" || body.B4Version != Version {
		t.Errorf("unexpected share body %+v", body)
	}
	if _, ok := body.Envelope.Set["routing"]; ok || strings.Contains(string(records[0].Body), "secret") {
		t.Errorf("private routing must not leave the box: %s", records[0].Body)
	}
	stamped := env.localSet("local-1")
	if stamped == nil || stamped.Hub == nil || stamped.Hub.ID != resp.HubID || stamped.Hub.Version != 1 || stamped.Hub.Hash != body.Envelope.Fingerprint {
		t.Fatalf("shared set must be stamped with its hub id: %+v", stamped.Hub)
	}
	if hubStateOf(env.api.getCfg(), stamped) != HubStateUnmodified {
		t.Errorf("a set just shared reads as unmodified")
	}

	env.hub.SetAnswer(func(*hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: http.StatusConflict, Body: map[string]interface{}{"code": "duplicate_strategy", "error": "already listed", "set_id": "dup-1", "version": 4}}
	})
	rec = postJSON(t, env.mux, "/api/hub/share", HubShareRequest{SetID: "local-1"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	var remote HubRemoteError
	decodeInto(t, rec, &remote)
	if remote.Code != "duplicate_strategy" || remote.HubID != "dup-1" || remote.Version != 4 {
		t.Errorf("duplicate answer must carry the hub id: %+v", remote)
	}

	env.hub.SetDown(true)
	expectCode(t, postJSON(t, env.mux, "/api/hub/share", HubShareRequest{SetID: "local-1"}), http.StatusBadGateway, "hub_unreachable")
	if env.svc.OutboxCount() != 0 {
		t.Errorf("a share is never queued")
	}
}

func TestHubTestNeedsAPlainDomain(t *testing.T) {
	env := newHubEnv(t)
	set := hubStrategySet("Regex only", "regexp:^.*\\.example\\.com$")
	cs, _ := hubtest.CatalogueSet(t, "rx-1", 1, &set, nil)
	env.publish(t, cs)

	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/rx-1/test", HubTestRequest{}), http.StatusBadRequest, "no_domain")
	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/rx-1/test", HubTestRequest{Domain: "10.0.0.1"}), http.StatusBadRequest, "bad_request")
	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/none/test", HubTestRequest{}), http.StatusNotFound, "not_found")
}

func TestHubIdentityEndpoints(t *testing.T) {
	env := newHubEnv(t)
	rec := getJSON(t, env.mux, "/api/hub/identity")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var id HubIdentityResponse
	decodeInto(t, rec, &id)
	if id.KeyID == "" || id.CreatedAt == "" {
		t.Fatalf("identity must be created on demand: %+v", id)
	}

	rec = postJSON(t, env.mux, "/api/hub/identity/recovery", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var code HubRecoveryCodeResponse
	decodeInto(t, rec, &code)
	if code.Code == "" {
		t.Fatal("recovery code must be returned")
	}

	expectCode(t, postJSON(t, env.mux, "/api/hub/identity/restore", HubRestoreRequest{Code: "garbage"}), http.StatusBadRequest, "bad_recovery_code")

	rec = postJSON(t, env.mux, "/api/hub/identity/restore", HubRestoreRequest{Code: strings.ToLower(code.Code)})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var restored HubRestoreResponse
	decodeInto(t, rec, &restored)
	if restored.KeyID != id.KeyID {
		t.Errorf("restoring the same code must give the same key: %s vs %s", restored.KeyID, id.KeyID)
	}
}

func TestCommunityPresetsKeepDistinctSetsWithTheSameStrategy(t *testing.T) {
	env := newHubEnv(t)
	forA := hubStrategySet("A", "a.example")
	forA.Faking.TTL = 12
	csA, _ := hubtest.CatalogueSet(t, "same-a", 1, &forA, nil)
	forB := hubStrategySet("B", "b.example")
	forB.Faking.TTL = 12
	csB, _ := hubtest.CatalogueSet(t, "same-b", 1, &forB, nil)
	if csA.FP != csB.FP {
		t.Fatalf("the fixture must publish one strategy under two sets")
	}
	env.publish(t, csA, csB)

	presets := env.api.communityPresets([]string{"a.example", "b.example"}, false)
	if len(presets) != 2 {
		t.Fatalf("both publications must be queued, got %d", len(presets))
	}
	for _, p := range presets {
		if p.Config.Hub == nil || len(p.Domains) != 1 {
			t.Fatalf("preset %s must carry its own provenance and one domain, got %+v %v", p.Description, p.Config.Hub, p.Domains)
		}
		want := map[string]string{"A": "a.example", "B": "b.example"}[p.Description]
		if p.Domains[0] != want {
			t.Errorf("%s must be tested for %s only, got %v", p.Description, want, p.Domains)
		}
	}
}

func TestCommunityPresetsCoverEveryRequestedDomain(t *testing.T) {
	env := newHubEnv(t)
	var sets []hubwire.CatalogueSet
	for i, title := range []string{"A1", "A2", "A3", "A4"} {
		set := hubStrategySet(title, "a.example")
		set.Faking.TTL = uint8(10 + i)
		cs, _ := hubtest.CatalogueSet(t, "a-"+title, 1, &set, nil)
		sets = append(sets, cs)
	}
	forB := hubStrategySet("B1", "b.example")
	forB.Faking.TTL = 20
	csB, _ := hubtest.CatalogueSet(t, "b-1", 1, &forB, nil)
	shared := hubStrategySet("AB", "a.example", "b.example")
	shared.Faking.TTL = 30
	csAB, _ := hubtest.CatalogueSet(t, "ab-1", 1, &shared, nil)
	env.publish(t, append(sets, csB, csAB)...)

	presets := env.api.communityPresets([]string{"https://a.example/watch?v=1", "b.example:443"}, false)
	var titles []string
	perTitle := map[string]int{}
	for _, p := range presets {
		titles = append(titles, p.Description)
		perTitle[p.Description]++
		if p.Family != discovery.FamilyCommunity || p.Phase != discovery.PhaseCached {
			t.Errorf("preset %s must be a cached community preset, got %s/%s", p.Description, p.Family, p.Phase)
		}
	}
	if perTitle["B1"] != 1 {
		t.Fatalf("the second domain's own match must be queued, got %v", titles)
	}
	if perTitle["AB"] != 1 {
		t.Fatalf("a set matching both domains must be queued exactly once, got %v", titles)
	}
	if len(presets) > 2*communityCandidateLimit {
		t.Fatalf("at most %d presets per domain, got %v", communityCandidateLimit, titles)
	}
	if presets[0].Description == "B1" || presets[1].Description != "B1" && presets[1].Description != "AB" {
		t.Errorf("each domain's best match must come before any domain's second match, got %v", titles)
	}
	for _, p := range presets {
		switch p.Description {
		case "B1":
			if len(p.Domains) != 1 || p.Domains[0] != "b.example" {
				t.Errorf("B1 must be scoped to b.example, got %v", p.Domains)
			}
		case "AB":
			if len(p.Domains) != 2 {
				t.Errorf("a set matching both domains must be scoped to both, got %v", p.Domains)
			}
		default:
			if len(p.Domains) != 1 || p.Domains[0] != "a.example" {
				t.Errorf("%s must be scoped to a.example, got %v", p.Description, p.Domains)
			}
		}
	}

	if got := env.api.communityPresets([]string{"a.example", "b.example"}, true); got != nil {
		t.Errorf("skip must return no community presets, got %d", len(got))
	}
}

func TestMatchAddressesToSetsNamesTheSetCoveringTheHubAddress(t *testing.T) {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "set-cloud", "cloud", true
	set.Targets.IpsToMatch = []string{"20.0.0.0/8"}
	matcher := sni.NewSuffixSet([]*config.SetConfig{&set})

	got := matchAddressesToSets(matcher, []string{"20.1.2.3", "8.8.8.8", "20.1.2.3", "not-an-address"})

	if len(got) != 1 || got[0].SetId != "set-cloud" || got[0].Domain != "20.1.2.3" || got[0].Via != "ip" || !got[0].Enabled {
		t.Fatalf("matches = %+v, want the cloud set once for 20.1.2.3: a set that targets the hub by address applies its strategy to b4's own connection too", got)
	}
	if matchAddressesToSets(nil, []string{"20.1.2.3"}) != nil {
		t.Fatal("without a running engine there is no matcher and nothing to report")
	}
}
