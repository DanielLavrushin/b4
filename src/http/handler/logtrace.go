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

var traceMaxDuration = 15 * time.Minute

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

type TraceStartRequest struct {
	Note string `json:"note"`
}

type TraceStatusResponse struct {
	Active        bool   `json:"active"`
	StartedAt     string `json:"startedAt,omitempty"`
	Note          string `json:"note,omitempty"`
	Lines         int64  `json:"lines"`
	Level         string `json:"level"`
	DownloadReady bool   `json:"downloadReady"`
	DownloadName  string `json:"downloadName,omitempty"`
	MaxSeconds    int    `json:"maxSeconds"`
}

func (api *API) RegisterLogTraceApi() {
	api.mux.HandleFunc("/api/logs/trace/start", api.handleTraceStart)
	api.mux.HandleFunc("/api/logs/trace/stop", api.handleTraceStop)
	api.mux.HandleFunc("/api/logs/trace/status", api.handleTraceStatus)
	api.mux.HandleFunc("/api/logs/trace/download", api.handleTraceDownload)
}

func currentLevelName() string {
	return log.Level(log.CurLevel.Load()).String()
}

func traceStatusResponse() TraceStatusResponse {
	resp := TraceStatusResponse{
		Level:         currentLevelName(),
		DownloadReady: lastTracePath != "",
		DownloadName:  lastTraceName,
		MaxSeconds:    int(traceMaxDuration.Seconds()),
	}
	if activeTrace != nil {
		resp.Active = true
		resp.StartedAt = activeTrace.startedAt.UTC().Format(time.RFC3339)
		resp.Note = activeTrace.note
		resp.Lines = activeTrace.writer.lines.Load()
	}
	return resp
}

// @Summary Start a log trace session
// @Description Captures all log output to a file until stopped or the max duration elapses. The file is prefixed with build info and a full system diagnostics snapshot. Only one session may run at a time.
// @Tags Logs
// @Accept json
// @Produce json
// @Param body body TraceStartRequest false "Optional note describing the session"
// @Success 200 {object} TraceStatusResponse
// @Failure 409 {object} map[string]string "A trace session is already running"
// @Security BearerAuth
// @Router /logs/trace/start [post]
func (api *API) handleTraceStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req TraceStartRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	traceMu.Lock()
	defer traceMu.Unlock()

	if activeTrace != nil {
		writeJsonError(w, http.StatusConflict, "A trace session is already running")
		return
	}

	diag := api.buildDiagnostics()

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
	if data, err := json.MarshalIndent(diag, "", "  "); err == nil {
		fmt.Fprintf(f, "--- system diagnostics ---\n")
		_, _ = f.Write(data)
		fmt.Fprintf(f, "\n")
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

// @Summary Stop the active log trace session
// @Description Finalizes the current trace file (writes footer, flushes, closes) and makes it available for download.
// @Tags Logs
// @Produce json
// @Success 200 {object} TraceStatusResponse
// @Failure 400 {object} map[string]string "No trace session is running"
// @Security BearerAuth
// @Router /logs/trace/stop [post]
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

// @Summary Get log trace session status
// @Description Reports whether a trace is active, its captured line count and start time, and whether a finished trace is available to download.
// @Tags Logs
// @Produce json
// @Success 200 {object} TraceStatusResponse
// @Security BearerAuth
// @Router /logs/trace/status [get]
func (api *API) handleTraceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	traceMu.Lock()
	resp := traceStatusResponse()
	traceMu.Unlock()
	sendResponse(w, resp)
}

// @Summary Download the last log trace file
// @Description Streams the most recently finished trace file as a plain-text attachment.
// @Tags Logs
// @Produce plain
// @Success 200 {file} binary
// @Failure 404 {object} map[string]string "No trace file available"
// @Security BearerAuth
// @Router /logs/trace/download [get]
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
