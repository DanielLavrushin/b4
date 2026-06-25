package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/log"
)

func newTraceTestAPI(t *testing.T) (*API, *http.ServeMux) {
	t.Helper()

	cfg := config.NewConfig()
	cfg.ConfigPath = "/etc/b4/b4.json"
	api := &API{
		cfgPtr:                 testCfgPtr(&cfg),
		overrideServiceManager: func() string { return "standalone" },
		geodataManager:         geodat.NewGeodataManager("", ""),
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterLogTraceApi()

	t.Cleanup(func() { resetTraceState() })
	resetTraceState()
	return api, mux
}

func resetTraceState() {
	traceMu.Lock()
	defer traceMu.Unlock()

	if activeTrace != nil {
		if activeTrace.timer != nil {
			activeTrace.timer.Stop()
		}
		log.StopCapture(activeTrace.writer)
		_ = activeTrace.file.Close()
		_ = os.Remove(activeTrace.path)
		activeTrace = nil
	}
	if lastTracePath != "" {
		_ = os.Remove(lastTracePath)
	}
	lastTracePath = ""
	lastTraceName = ""
}

func doTrace(t *testing.T, mux *http.ServeMux, method, path, body string) (*httptest.ResponseRecorder, TraceStatusResponse) {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	var req *http.Request
	if reader != nil {
		req = httptest.NewRequest(method, path, reader)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp TraceStatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec, resp
}

func TestTraceStatus_Idle(t *testing.T) {
	_, mux := newTraceTestAPI(t)

	rec, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if resp.Active {
		t.Error("expected active=false when idle")
	}
	if resp.DownloadReady {
		t.Error("expected downloadReady=false when idle")
	}
	if resp.MaxSeconds != int(traceMaxDuration.Seconds()) {
		t.Errorf("expected maxSeconds=%d, got %d", int(traceMaxDuration.Seconds()), resp.MaxSeconds)
	}
}

func TestTraceStartStopLifecycle(t *testing.T) {
	_, mux := newTraceTestAPI(t)

	rec, resp := doTrace(t, mux, http.MethodPost, "/api/logs/trace/start", `{"note":"hello"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: expected 200, got %d", rec.Code)
	}
	if !resp.Active {
		t.Error("start: expected active=true")
	}
	if resp.Note != "hello" {
		t.Errorf("start: expected note=hello, got %q", resp.Note)
	}

	rec, _ = doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d", rec.Code)
	}

	rec, resp = doTrace(t, mux, http.MethodPost, "/api/logs/trace/stop", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stop: expected 200, got %d", rec.Code)
	}
	if resp.Active {
		t.Error("stop: expected active=false")
	}
	if !resp.DownloadReady {
		t.Error("stop: expected downloadReady=true")
	}
	if resp.DownloadName == "" {
		t.Error("stop: expected a download name")
	}
}

func TestTraceStart_ConflictWhenActive(t *testing.T) {
	_, mux := newTraceTestAPI(t)

	rec, _ := doTrace(t, mux, http.MethodPost, "/api/logs/trace/start", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("first start: expected 200, got %d", rec.Code)
	}

	rec, _ = doTrace(t, mux, http.MethodPost, "/api/logs/trace/start", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("second start: expected 409, got %d", rec.Code)
	}
}

func TestTraceStop_WhenIdle(t *testing.T) {
	_, mux := newTraceTestAPI(t)

	rec, _ := doTrace(t, mux, http.MethodPost, "/api/logs/trace/stop", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when no session running, got %d", rec.Code)
	}
}

func TestTraceDownload_NoFile(t *testing.T) {
	_, mux := newTraceTestAPI(t)

	rec, _ := doTrace(t, mux, http.MethodGet, "/api/logs/trace/download", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when no trace file, got %d", rec.Code)
	}
}

func TestTraceDownload_ServesFinishedTrace(t *testing.T) {
	_, mux := newTraceTestAPI(t)

	doTrace(t, mux, http.MethodPost, "/api/logs/trace/start", `{"note":"download-me"}`)
	doTrace(t, mux, http.MethodPost, "/api/logs/trace/stop", "")

	req := httptest.NewRequest(http.MethodGet, "/api/logs/trace/download", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("unexpected content-type: %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("expected attachment disposition, got %q", cd)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "b4 trace session") {
		t.Error("trace file should contain the session header")
	}
	if !strings.Contains(body, "note: download-me") {
		t.Error("trace file should contain the note")
	}
	if !strings.Contains(body, "b4 trace ended") {
		t.Error("trace file should contain the footer written on stop")
	}
}

func TestTraceAutoStop_MaxDuration(t *testing.T) {
	_, mux := newTraceTestAPI(t)

	orig := traceMaxDuration
	traceMaxDuration = 20 * time.Millisecond
	defer func() { traceMaxDuration = orig }()

	rec, resp := doTrace(t, mux, http.MethodPost, "/api/logs/trace/start", "")
	if rec.Code != http.StatusOK || !resp.Active {
		t.Fatalf("start: expected 200 active, got %d active=%v", rec.Code, resp.Active)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, resp = doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", "")
		if !resp.Active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("trace did not auto-stop within deadline")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if !resp.DownloadReady {
		t.Error("expected downloadReady=true after auto-stop")
	}
}

func TestTrace_MethodNotAllowed(t *testing.T) {
	_, mux := newTraceTestAPI(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/logs/trace/start"},
		{http.MethodGet, "/api/logs/trace/stop"},
		{http.MethodPost, "/api/logs/trace/status"},
		{http.MethodPost, "/api/logs/trace/download"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: expected 405, got %d", tc.method, tc.path, rec.Code)
		}
	}
}
