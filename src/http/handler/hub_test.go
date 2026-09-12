package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
)

func hubAPI(t *testing.T) (*http.ServeMux, *config.Config) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterHubApi()
	return mux, &cfg
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
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
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
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
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
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	var ae APIError
	_ = json.Unmarshal(rec.Body.Bytes(), &ae)
	if ae.Code != "unsupported_format" {
		t.Errorf("unexpected error code %q", ae.Code)
	}
}
