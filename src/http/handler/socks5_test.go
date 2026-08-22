package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func socks5TestAPI(t *testing.T) (*API, *config.Config) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.System.Socks5.Enabled = true
	cfg.System.Socks5.BindAddress = "127.0.0.1"
	cfg.System.Socks5.AllowedSources = []string{"192.168.1.0/24"}
	return &API{cfgPtr: testCfgPtr(&cfg)}, &cfg
}

func TestSaveRejectsIncompleteSocks5Credentials(t *testing.T) {
	api, cfg := socks5TestAPI(t)

	next := cfg.Clone()
	next.System.Socks5.Username = "user"
	next.System.Socks5.Password = ""

	err := api.saveAndPushConfig(next)
	if err == nil {
		t.Fatal("a half-filled credential pair must be refused")
	}
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if len(ae.Fields) != 1 || ae.Fields[0].Code != "socks5_incomplete_credentials" {
		t.Fatalf("expected socks5_incomplete_credentials, got %+v", ae.Fields)
	}
	if api.getCfg().System.Socks5.Username != "" {
		t.Error("a refused save must not be applied")
	}
}

func TestSaveAcceptsBothCredentialsEmptyOrBothSet(t *testing.T) {
	api, cfg := socks5TestAPI(t)

	both := cfg.Clone()
	both.System.Socks5.Username = "user"
	both.System.Socks5.Password = "pass"
	if err := api.saveAndPushConfig(both); err != nil {
		t.Fatalf("a complete credential pair must be accepted: %v", err)
	}

	neither := api.getCfg().Clone()
	neither.System.Socks5.Username = ""
	neither.System.Socks5.Password = ""
	if err := api.saveAndPushConfig(neither); err != nil {
		t.Fatalf("an empty credential pair must be accepted: %v", err)
	}
}

func TestSaveRejectsCatchAllAllowedSource(t *testing.T) {
	api, cfg := socks5TestAPI(t)

	next := cfg.Clone()
	next.System.Socks5.AllowedSources = []string{"0.0.0.0/0"}

	err := api.saveAndPushConfig(next)
	if err == nil {
		t.Fatal("an entry that allows every source must be refused")
	}
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if findAPIField(ae, "system.socks5.allowed_sources", "socks5_source_all") == nil {
		t.Fatalf("expected socks5_source_all, got %+v", ae.Fields)
	}
}

func TestUpdateSocks5ConfigKeepsOmittedFields(t *testing.T) {
	api, _ := socks5TestAPI(t)

	body := []byte(`{"enabled":true,"port":1080,"bind_address":"127.0.0.1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/socks5/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.handleSocks5Config(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := api.getCfg().System.Socks5.AllowedSources
	if len(got) != 1 || got[0] != "192.168.1.0/24" {
		t.Fatalf("omitting allowed_sources must not clear it, got %v", got)
	}
}

func TestUpdateSocks5ConfigReplacesSuppliedFields(t *testing.T) {
	api, _ := socks5TestAPI(t)

	body := []byte(`{"enabled":true,"port":1080,"bind_address":"127.0.0.1","allowed_sources":["10.0.0.0/8"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/socks5/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.handleSocks5Config(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := api.getCfg().System.Socks5.AllowedSources
	if len(got) != 1 || got[0] != "10.0.0.0/8" {
		t.Fatalf("a supplied allowed_sources must replace the stored one, got %v", got)
	}
}

func TestGetSocks5ConfigCarriesAllowedSources(t *testing.T) {
	api, _ := socks5TestAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/socks5/config", nil)
	rec := httptest.NewRecorder()
	api.handleSocks5Config(rec, req)

	var out struct {
		Config config.Socks5Config `json:"config"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Config.AllowedSources) != 1 || out.Config.AllowedSources[0] != "192.168.1.0/24" {
		t.Fatalf("allowed_sources missing from the API response: %v", out.Config.AllowedSources)
	}
}

func findAPIField(ae *APIError, path, code string) *FieldError {
	for i := range ae.Fields {
		if ae.Fields[i].Path == path && ae.Fields[i].Code == code {
			return &ae.Fields[i]
		}
	}
	return nil
}
