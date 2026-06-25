package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const traceMaxDuration = 15 * time.Minute

type traceWriter struct {
	f     *os.File
	lines atomic.Int64
}

var traceNewline = []byte{'\n'}

func (t *traceWriter) Write(p []byte) (int, error) {
	t.lines.Add(int64(bytes.Count(p, traceNewline)))
	return t.f.Write(p)
}

type traceSession struct {
	file         *os.File
	path         string
	downloadName string
	startedAt    time.Time
	note         string
	writer       *traceWriter
	timer        *time.Timer
}

var (
	traceMu       sync.Mutex
	activeTrace   *traceSession
	lastTracePath string
	lastTraceName string
)

func (api *API) RegisterLogTraceApi() {
	api.mux.HandleFunc("/api/logs/trace/start", api.handleTraceStart)
	api.mux.HandleFunc("/api/logs/trace/stop", api.handleTraceStop)
	api.mux.HandleFunc("/api/logs/trace/status", api.handleTraceStatus)
	api.mux.HandleFunc("/api/logs/trace/download", api.handleTraceDownload)
}

func currentLevelName() string {
	return log.Level(log.CurLevel.Load()).String()
}

func traceStatusResponse() map[string]any {
	resp := map[string]any{
		"active":        false,
		"level":         currentLevelName(),
		"lines":         int64(0),
		"downloadReady": lastTracePath != "",
		"maxSeconds":    int(traceMaxDuration.Seconds()),
	}
	if activeTrace != nil {
		resp["active"] = true
		resp["startedAt"] = activeTrace.startedAt.UTC().Format(time.RFC3339)
		resp["note"] = activeTrace.note
		resp["lines"] = activeTrace.writer.lines.Load()
	}
	if lastTraceName != "" {
		resp["downloadName"] = lastTraceName
	}
	return resp
}

func (api *API) handleTraceStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Note string `json:"note"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	traceMu.Lock()
	defer traceMu.Unlock()

	if activeTrace != nil {
		writeJsonError(w, http.StatusConflict, "A trace session is already running")
		return
	}

	if lastTracePath != "" {
		_ = os.Remove(lastTracePath)
		lastTracePath = ""
		lastTraceName = ""
	}

	f, err := os.CreateTemp("", "b4-trace-*.log")
	if err != nil {
		writeJsonError(w, http.StatusInternalServerError, "Failed to create trace file: "+err.Error())
		return
	}

	startedAt := time.Now()
	fmt.Fprintf(f, "=== b4 trace session ===\n")
	fmt.Fprintf(f, "version: %s (%s)\n", Version, Commit)
	fmt.Fprintf(f, "started: %s\n", startedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(f, "level: %s\n", currentLevelName())
	if req.Note != "" {
		fmt.Fprintf(f, "note: %s\n", req.Note)
	}
	fmt.Fprintf(f, "=========================\n")

	tw := &traceWriter{f: f}
	log.StartCapture(tw)

	session := &traceSession{
		file:         f,
		path:         f.Name(),
		downloadName: fmt.Sprintf("b4-trace-%s.log", startedAt.Format("20060102-150405")),
		startedAt:    startedAt,
		note:         req.Note,
		writer:       tw,
	}
	session.timer = time.AfterFunc(traceMaxDuration, func() {
		traceMu.Lock()
		defer traceMu.Unlock()
		if activeTrace == session {
			finishTraceLocked("auto-stopped (max duration reached)")
		}
	})
	activeTrace = session

	log.Infof("Log trace session started")
	sendResponse(w, traceStatusResponse())
}

func (api *API) handleTraceStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	traceMu.Lock()
	defer traceMu.Unlock()

	if activeTrace == nil {
		writeJsonError(w, http.StatusBadRequest, "No trace session is running")
		return
	}

	finishTraceLocked("stopped by user")
	log.Infof("Log trace session stopped")
	sendResponse(w, traceStatusResponse())
}

func finishTraceLocked(reason string) {
	s := activeTrace
	if s == nil {
		return
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	log.StopCapture(s.writer)

	endedAt := time.Now()
	dur := endedAt.Sub(s.startedAt).Round(time.Millisecond)
	fmt.Fprintf(s.file, "=== b4 trace ended: %s  duration: %s  lines: %d  reason: %s ===\n",
		endedAt.UTC().Format(time.RFC3339), dur, s.writer.lines.Load(), reason)
	_ = s.file.Sync()
	_ = s.file.Close()

	lastTracePath = s.path
	lastTraceName = s.downloadName
	activeTrace = nil
}

func (api *API) handleTraceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	traceMu.Lock()
	defer traceMu.Unlock()
	sendResponse(w, traceStatusResponse())
}

func (api *API) handleTraceDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	traceMu.Lock()
	path := lastTracePath
	name := lastTraceName
	traceMu.Unlock()

	if path == "" {
		writeJsonError(w, http.StatusNotFound, "No trace file available")
		return
	}

	f, err := os.Open(path)
	if err != nil {
		writeJsonError(w, http.StatusNotFound, "Trace file no longer available")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	_, _ = io.Copy(w, f)
}
