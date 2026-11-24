package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/watchdog"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func traceConsole(t *testing.T, level log.Level) *lockedBuffer {
	t.Helper()
	console := &lockedBuffer{}
	log.Init(console, level, true)
	t.Cleanup(func() { log.Init(os.Stderr, log.LevelError, true) })
	return console
}

func downloadedTrace(t *testing.T, mux *http.ServeMux) string {
	t.Helper()
	rec := serve(mux, http.MethodGet, "/api/logs/trace/download", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("download: %d %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func startTrace(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	if rec := serve(mux, http.MethodPost, "/api/logs/trace/start", ""); rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
}

func stopTrace(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	if rec := serve(mux, http.MethodPost, "/api/logs/trace/stop", ""); rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
}

func wantLines(t *testing.T, where, text string, present []string, absent []string) {
	t.Helper()
	for _, want := range present {
		if !strings.Contains(text, want) {
			t.Errorf("%s misses %q", where, want)
		}
	}
	for _, not := range absent {
		if strings.Contains(text, not) {
			t.Errorf("%s must not hold %q", where, not)
		}
	}
}

func configBody(t *testing.T, api *API, level log.Level) string {
	t.Helper()
	raw := map[string]any{}
	data, _ := json.Marshal(api.getCfg())
	_ = json.Unmarshal(data, &raw)
	raw["revision"] = watchdog.ConfigRevision(api.getCfg())
	raw["system"].(map[string]any)["logging"].(map[string]any)["level"] = int(level)
	body, _ := json.Marshal(raw)
	return string(body)
}

func traceConfigAPI(t *testing.T) (*API, *http.ServeMux) {
	t.Helper()
	api, mux := newTraceTestAPI(t)
	cfg := api.getCfg().Clone()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Sets = []*config.SetConfig{urlSet("yt", "https://www.youtube.com/")}
	cfg.System.Logging.Directory = ""
	api.cfgPtr.Store(cfg)
	api.RegisterConfigApi()
	return api, mux
}

func TestATraceHoldsDebugLinesWhileTheLogStaysAtInfo(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	console := traceConsole(t, log.LevelInfo)

	startTrace(t, mux)
	log.Infof("info line")
	log.Tracef("trace line")
	log.Debugf("debug line")
	stopTrace(t, mux)
	log.Debugf("after the trace")

	trace := downloadedTrace(t, mux)
	wantLines(t, "the trace", trace, []string{"info line", "trace line", "debug line", "\nlevel: debug\n", "\nlog level setting: info\n"}, []string{"after the trace"})
	wantLines(t, "the console", console.String(), []string{"info line"}, []string{"trace line", "debug line"})
}

func TestATraceHoldsWarningsWhileTheLogIsSilent(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	console := traceConsole(t, -1)

	startTrace(t, mux)
	log.Warnf("warn line")
	log.Debugf("debug line")
	stopTrace(t, mux)

	wantLines(t, "the trace", downloadedTrace(t, mux), []string{"warn line", "debug line", "\nlog level setting: silent\n"}, nil)
	wantLines(t, "the console", console.String(), nil, []string{"warn line", "debug line"})
}

func TestASaveDuringATraceKeepsRecordingDebug(t *testing.T) {
	api, mux := traceConfigAPI(t)
	console := traceConsole(t, log.LevelInfo)

	startTrace(t, mux)
	if rec := serve(mux, http.MethodPut, "/api/config", configBody(t, api, log.LevelInfo)); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	log.Debugf("after a plain save")
	if rec := serve(mux, http.MethodPut, "/api/config", configBody(t, api, log.LevelError)); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	log.Debugf("after the level went down")
	log.Infof("info after the level went down")
	stopTrace(t, mux)

	trace := downloadedTrace(t, mux)
	wantLines(t, "the trace", trace, []string{"after a plain save", "after the level went down", "info after the level went down"}, []string{"Log level changed to info"})
	if got := strings.Count(trace, "Log level changed"); got != 1 {
		t.Errorf("the log reports %d level changes, the user made one", got)
	}
	wantLines(t, "the console", console.String(), nil, []string{"after the level went down", "info after the level went down"})
	if got := log.Level(log.CurLevel.Load()); got != log.LevelError {
		t.Errorf("level after the trace = %v, want the saved one", got)
	}
}

func TestARefusedSaveDuringATraceKeepsTheLogLevel(t *testing.T) {
	api, mux := traceConfigAPI(t)
	console := traceConsole(t, log.LevelInfo)

	startTrace(t, mux)
	rec := serveWhileLocked(t, mux, http.MethodPut, "/api/config", configBody(t, api, log.LevelTrace), func() {
		storeConcurrently(t, api, func(c *config.Config) { c.Sets[0].TCP.Seg2Delay = 42 })
	})
	expectCode(t, rec, http.StatusConflict, "config_changed")
	log.Tracef("trace line")
	stopTrace(t, mux)

	wantLines(t, "the trace", downloadedTrace(t, mux), []string{"trace line"}, nil)
	wantLines(t, "the console", console.String(), nil, []string{"trace line"})
	if got := log.Level(log.CurLevel.Load()); got != log.LevelInfo {
		t.Errorf("level after the trace = %v, want the one in the config", got)
	}
}

func TestAnMCPWriteDuringATraceKeepsRecordingDebug(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	resetTraceState()
	t.Cleanup(resetTraceState)
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	api.overrideServiceManager = func() string { return "standalone" }
	api.RegisterLogTraceApi()
	console := traceConsole(t, log.LevelInfo)
	session, ctx := connectMCP(t, srv)

	startTrace(t, api.mux)
	if res := callSetValue(t, session, ctx, "system.logging.level", "error"); res.IsError {
		t.Fatalf("write refused: %+v", res.Content)
	}
	log.Debugf("after the write")
	log.Infof("info after the write")
	stopTrace(t, api.mux)

	wantLines(t, "the trace", downloadedTrace(t, api.mux), []string{"after the write", "info after the write"}, nil)
	wantLines(t, "the console", console.String(), nil, []string{"after the write", "info after the write"})
	if got := log.Level(log.CurLevel.Load()); got != log.LevelError {
		t.Errorf("level after the trace = %v, want the written one", got)
	}
}
