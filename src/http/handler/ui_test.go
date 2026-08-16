package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func newUITestAPI(t *testing.T) (*API, *http.ServeMux, string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "b4.json")
	cfg := config.NewConfig()
	cfg.ConfigPath = cfgPath
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterUIApi()
	return api, mux, cfgPath
}

func TestDashboardLayoutRoundTrip(t *testing.T) {
	api, mux, cfgPath := newUITestAPI(t)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ui/dashboard", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d", rec.Code)
	}
	var empty config.DashboardLayout
	if err := json.NewDecoder(rec.Body).Decode(&empty); err != nil {
		t.Fatalf("GET: decode: %v", err)
	}
	if len(empty.Order) != 0 || len(empty.Hidden) != 0 || len(empty.Spans) != 0 {
		t.Fatalf("GET: expected an empty layout, got %#v", empty)
	}

	body := `{"order":["mtproto","runtime"],"hidden":["blackhole"],"spans":{"runtime":99}}`
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/ui/dashboard", strings.NewReader(body))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	stored := api.getCfg().UI.Dashboard
	if len(stored.Order) != 2 || stored.Order[0] != "mtproto" {
		t.Fatalf("PUT: order not stored: %#v", stored.Order)
	}
	if stored.Spans["runtime"] != 12 {
		t.Fatalf("PUT: span not clamped to 12: %#v", stored.Spans)
	}

	saved, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if !strings.Contains(string(saved), "mtproto") {
		t.Fatalf("layout missing from saved config: %s", saved)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ui/dashboard", nil))
	var reloaded config.DashboardLayout
	if err := json.NewDecoder(rec.Body).Decode(&reloaded); err != nil {
		t.Fatalf("GET after PUT: decode: %v", err)
	}
	if len(reloaded.Hidden) != 1 || reloaded.Hidden[0] != "blackhole" {
		t.Fatalf("GET after PUT: hidden not returned: %#v", reloaded.Hidden)
	}
}

func TestDashboardLayoutRejectsBadBody(t *testing.T) {
	_, mux, _ := newUITestAPI(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/ui/dashboard", strings.NewReader("not json"))
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected a non-200 for a malformed body, got %d", rec.Code)
	}
}

func TestDashboardLayoutMethodNotAllowed(t *testing.T) {
	_, mux, _ := newUITestAPI(t)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/ui/dashboard", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}
