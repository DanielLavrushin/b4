package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestTheCaptureProbeRouteIsGone(t *testing.T) {
	cfg := config.NewConfig()
	cfg.ConfigPath = hubTestConfigPath(t)
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterCaptureApi()

	for _, body := range []string{`{"domain":"probe.removed.example","protocol":"tls"}`, `{"domain":"probe.removed.example"}`} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/capture/probe", strings.NewReader(body)))
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST /api/capture/probe %s answered %d %q, want 404: nothing captures a live payload any more", body, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
}
