package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func configDownloadAPI(cfg *config.Config) *API {
	api := &API{cfgPtr: testCfgPtr(cfg)}
	api.mux = http.NewServeMux()
	api.RegisterConfigApi()
	return api
}

func downloadConfig(t *testing.T, api *API, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func secretsDownloadCfg() *config.Config {
	cfg := mcpTestCfg()
	cfg.System.WebServer.Password = "$2a$12$hashhashhashhashhashhu1234567890abcdefghijklmnopqrstuv"
	cfg.System.WebServer.MCP.Token = "mcp-token-value"
	cfg.System.MTProto.CFWorkerDomain = "personal.workers.dev"
	cfg.System.Geo.GeoSiteURL = "https://example.com/geosite.dat?token=geo-token"
	cfg.System.Update.Mirrors = []string{"https://mirror.personal.workers.dev"}
	cfg.Sets[0].Routing.Upstream.Host = "10.0.0.9"
	cfg.Sets[0].Routing.Upstream.Username = "upstream-user"
	cfg.Sets[0].Routing.Upstream.Password = "upstream-pw"
	cfg.Sets[0].DNS.DoHURL = "https://dns.nextdns.io/abc123"
	return cfg
}

func TestConfigDownloadAsIsIsTheConfigFile(t *testing.T) {
	cfg := secretsDownloadCfg()
	rec := downloadConfig(t, configDownloadAPI(cfg), "/api/config/download")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	disposition := rec.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, `filename="b4-config-`) || strings.Contains(disposition, "safe") {
		t.Errorf("Content-Disposition = %q", disposition)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("a file with secrets must not be cached, Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	want, err := cfg.FileBytes()
	if err != nil {
		t.Fatalf("FileBytes: %v", err)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Error("the as-is download differs from the bytes b4 writes to its config file")
	}
	if !strings.Contains(rec.Body.String(), "socks-pw") {
		t.Error("the as-is download must keep the secrets")
	}
}

func TestConfigDownloadSafeMasksSecrets(t *testing.T) {
	cfg := secretsDownloadCfg()
	rec := downloadConfig(t, configDownloadAPI(cfg), "/api/config/download?safe=true")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), `filename="b4-config-safe-`) {
		t.Errorf("Content-Disposition = %q", rec.Header().Get("Content-Disposition"))
	}
	body := rec.Body.String()
	for _, secret := range []string{
		"admin", "$2a$12$", "mcp-token-value", "socks-user", "socks-pw", "ipinfo-token",
		"daniel", "deadbeef", "cafebabe", "personal.workers.dev", "geo-token",
		"upstream-user", "upstream-pw", "abc123",
	} {
		if strings.Contains(body, secret) {
			t.Errorf("the safe download still contains %q", secret)
		}
	}
	for _, kept := range []string{"10.0.0.9", "youtube.com", "https://dns.nextdns.io/[redacted]", "https://example.com/geosite.dat"} {
		if !strings.Contains(body, kept) {
			t.Errorf("the safe download dropped %q, which is needed for debugging", kept)
		}
	}

	var parsed config.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("the safe download is not valid config JSON: %v", err)
	}
	if parsed.System.WebServer.Password != "" || !parsed.System.WebServer.PasswordSet {
		t.Error("the safe download must leave the web password empty and flag that one is set")
	}
	if parsed.System.WebServer.MCP.Token != redactedMarker {
		t.Errorf("the safe download must mark the MCP token, got %q", parsed.System.WebServer.MCP.Token)
	}
	if len(parsed.RedactedValuePaths()) == 0 {
		t.Error("the safe download must carry placeholders that b4 refuses to load")
	}

	if cfg.System.Socks5.Password != "socks-pw" || cfg.Sets[0].Routing.Upstream.Password != "upstream-pw" || cfg.System.WebServer.MCP.Token != "mcp-token-value" {
		t.Error("the safe download changed the live config")
	}
}

func TestConfigDownloadRejectsOtherMethods(t *testing.T) {
	api := configDownloadAPI(secretsDownloadCfg())
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/config/download", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rec.Code)
	}
}

func TestConfigDownloadSafeParameter(t *testing.T) {
	api := configDownloadAPI(secretsDownloadCfg())
	cases := []struct {
		query  string
		status int
		safe   bool
	}{
		{"", http.StatusOK, false},
		{"?safe=false", http.StatusOK, false},
		{"?safe=true", http.StatusOK, true},
		{"?safe=1", http.StatusOK, true},
		{"?safe", http.StatusBadRequest, false},
		{"?safe=yes", http.StatusBadRequest, false},
		{"?safe=false&safe=true", http.StatusBadRequest, false},
		{"?Safe=true", http.StatusBadRequest, false},
		{"?safe=true;", http.StatusBadRequest, false},
		{"?safe%3Dtrue", http.StatusBadRequest, false},
		{"?other=1", http.StatusBadRequest, false},
		{"?safe=true&other=1", http.StatusBadRequest, false},
	}
	for _, tc := range cases {
		rec := downloadConfig(t, api, "/api/config/download"+tc.query)
		if rec.Code != tc.status {
			t.Errorf("%q: status = %d, want %d", tc.query, rec.Code, tc.status)
			continue
		}
		if tc.status != http.StatusOK {
			if strings.Contains(rec.Body.String(), "socks-pw") {
				t.Errorf("%q: a rejected request returned secrets", tc.query)
			}
			continue
		}
		if got := strings.Contains(rec.Header().Get("Content-Disposition"), "b4-config-safe-"); got != tc.safe {
			t.Errorf("%q: safe copy = %v, want %v", tc.query, got, tc.safe)
		}
	}
}

func TestMCPGetConfigMasksOnlyCredentials(t *testing.T) {
	raw, err := json.Marshal(redactConfigForMCP(secretsDownloadCfg()))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(raw)
	for _, secret := range []string{"socks-pw", "upstream-pw", "mcp-token-value", "deadbeef"} {
		if strings.Contains(out, secret) {
			t.Errorf("b4_get_config still returns %q", secret)
		}
	}
	for _, kept := range []string{"personal.workers.dev", "https://dns.nextdns.io/abc123"} {
		if !strings.Contains(out, kept) {
			t.Errorf("b4_get_config hides %q, which MCP can also read and write through b4_list_writable_paths", kept)
		}
	}
	if !strings.Contains(out, `"token":"`+redactedMarker+`"`) {
		t.Error("b4_get_config should still show that an MCP token is set")
	}
}

func TestPushConfigRejectsRedactedPlaceholders(t *testing.T) {
	cfg := secretsDownloadCfg()
	api := configDownloadAPI(cfg)
	incoming := cfg.Clone()
	incoming.System.Socks5.Password = redactedMarker
	err := api.pushConfigLocked(incoming)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "validation_failed" {
		t.Fatalf("pushConfigLocked = %v, want a validation error", err)
	}
	found := false
	for _, f := range apiErr.Fields {
		if f.Path == "system.socks5.password" && f.Code == "redacted_placeholder" {
			found = true
		}
	}
	if !found {
		t.Errorf("fields = %+v, want system.socks5.password / redacted_placeholder", apiErr.Fields)
	}
}
