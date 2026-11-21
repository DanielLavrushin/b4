package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func captureTestMux(t *testing.T) (*http.ServeMux, *API) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = hubTestConfigPath(t)
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterCaptureApi()
	return mux, api
}

func serveRecovered(mux *http.ServeMux, req *http.Request) (rec *httptest.ResponseRecorder, panicked string) {
	rec = httptest.NewRecorder()
	defer func() {
		if r := recover(); r != nil {
			panicked = fmt.Sprint(r)
		}
	}()
	mux.ServeHTTP(rec, req)
	return rec, ""
}

func TestDownloadAnswers404ForAnyFileItCannotStat(t *testing.T) {
	mux, _ := captureTestMux(t)
	for _, name := range []string{strings.Repeat("a", 300) + ".bin", "never-saved.download.example.bin"} {
		rec, panicked := serveRecovered(mux, httptest.NewRequest(http.MethodGet, "/api/capture/download?file="+name, nil))
		if panicked != "" {
			t.Errorf("GET /api/capture/download?file=<%d-byte name> panicked: %s", len(name), panicked)
			continue
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /api/capture/download?file=<%d-byte name> answered %d %q, want 404", len(name), rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
}
