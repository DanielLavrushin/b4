package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const (
	traceLevel        = log.LevelDebug
	traceRequestLimit = 64 << 10
)

var traceMaxDuration = 15 * time.Minute

type traceSession struct {
	startedAt time.Time
	note      string
	writer    *traceWriter
	timer     *time.Timer
}

var (
	traceMu     sync.Mutex
	activeTrace *traceSession
	lastTrace   *finishedTrace
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
	EndReason     string `json:"endReason,omitempty"`
	MaxSeconds    int    `json:"maxSeconds"`
}

func (api *API) RegisterLogTraceApi() {
	api.mux.HandleFunc("/api/logs/trace/start", api.handleTraceStart)
	api.mux.HandleFunc("/api/logs/trace/stop", api.handleTraceStop)
	api.mux.HandleFunc("/api/logs/trace/status", api.handleTraceStatus)
	api.mux.HandleFunc("/api/logs/trace/download", api.handleTraceDownload)
}

func RestoreTrace() {
	traceMu.Lock()
	defer traceMu.Unlock()
	if activeTrace == nil && lastTrace == nil {
		lastTrace = leftoverTrace()
	}
}

func FinishActiveTrace() {
	if !traceMu.TryLock() {
		return
	}
	defer traceMu.Unlock()
	finishTraceLocked(traceEndShutdown)
}

func traceStatusResponse() TraceStatusResponse {
	resp := TraceStatusResponse{
		Level:      traceLevel.String(),
		MaxSeconds: int(traceMaxDuration.Seconds()),
	}
	if lastTrace.size() > 0 {
		resp.DownloadReady = true
		resp.DownloadName = lastTrace.name
		resp.EndReason = string(lastTrace.end)
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
// @Description Records every log line, down to debug, to a file until stopped or the max duration elapses; the log level setting and the other log outputs stay as they are. The file is prefixed with build info and a system diagnostics snapshot, cut when it would take more than a quarter of the trace. A trace that reaches its size limit (8 MB, less when free memory or disk space is short) keeps its first and its newest lines. Only one session may run at a time.
// @Tags Logs
// @Accept json
// @Produce json
// @Param body body TraceStartRequest false "Optional note describing the session"
// @Success 200 {object} TraceStatusResponse
// @Failure 409 {object} map[string]string "A trace session is already running"
// @Failure 507 {object} map[string]string "Not enough free memory or disk space for a trace"
// @Security BearerAuth
// @Router /logs/trace/start [post]
func (api *API) handleTraceStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req TraceStartRequest
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, traceRequestLimit)).Decode(&req)
	}

	traceMu.Lock()
	defer traceMu.Unlock()

	if activeTrace != nil {
		writeJsonError(w, http.StatusConflict, "A trace session is already running")
		return
	}

	limit := traceSizeLimit(os.TempDir(), lastTrace.size())
	if limit < traceMinBytes {
		writeJsonError(w, http.StatusInsufficientStorage, "Not enough free memory or disk space for a trace in "+os.TempDir())
		return
	}
	diag := api.buildDiagnostics()
	sets := collectTraceSets(api.getCfg())
	startedAt := time.Now()
	header := traceHeader(startedAt, req.Note, diag, sets, limit/4)

	dir, err := newTraceDir(startedAt)
	if errors.Is(err, syscall.ENOSPC) && lastTrace != nil {
		lastTrace.remove()
		lastTrace = nil
		dir, err = newTraceDir(startedAt)
	}
	if err != nil {
		writeTraceStartError(w, err)
		return
	}
	lastTrace.remove()
	lastTrace = nil
	writer, err := newTraceWriter(dir, header, limit)
	if err != nil {
		writeTraceStartError(w, err)
		return
	}

	session := &traceSession{startedAt: startedAt, note: req.Note, writer: writer}
	writer.failed = func(err error) {
		if finishTrace(session, traceEndWriteFailed) {
			log.Warnf("Log trace session stopped, the trace could not be written: %v", err)
		}
	}
	longest := traceMaxDuration
	session.timer = time.AfterFunc(longest, func() {
		if finishTrace(session, traceEndTimeLimit) {
			log.Infof("Log trace session stopped after %s", longest)
		}
	})
	activeTrace = session
	log.StartRecording(writer, traceLevel)

	log.Infof("Log trace session started")
	sendResponse(w, traceStatusResponse())
}

func writeTraceStartError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, syscall.ENOSPC) {
		status = http.StatusInsufficientStorage
	}
	writeJsonError(w, status, "Failed to create trace file: "+err.Error())
}

func traceHeader(startedAt time.Time, note string, diag Diagnostics, sets []traceSet, most int64) []byte {
	var top, bottom bytes.Buffer
	fmt.Fprintf(&top, "=== b4 trace session ===\n")
	fmt.Fprintf(&top, "version: %s (%s)\n", Version, Commit)
	fmt.Fprintf(&top, "started: %s\n", startedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&top, "level: %s\n", traceLevel)
	fmt.Fprintf(&top, "log level setting: %s\n", log.ConfiguredLevel())
	if note != "" {
		fmt.Fprintf(&top, "note: %s\n", note)
	}
	fmt.Fprintf(&top, "--- system diagnostics ---\n")
	if data, err := json.MarshalIndent(sets, "", "  "); err == nil {
		fmt.Fprintf(&bottom, "\n--- enabled sets (%d) ---\n%s", len(sets), data)
	}
	fmt.Fprintf(&bottom, "\n=========================\n")

	data, err := json.MarshalIndent(diag, "", "  ")
	if err != nil {
		data = []byte(err.Error())
	}
	data = cutTrace(data, max(0, most-int64(top.Len()+bottom.Len())))
	return cutTrace(slices.Concat(top.Bytes(), data, bottom.Bytes()), most)
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

	finishTraceLocked(traceEndUser)
	log.Infof("Log trace session stopped")
	sendResponse(w, traceStatusResponse())
}

func finishTrace(session *traceSession, end traceEnd) bool {
	traceMu.Lock()
	defer traceMu.Unlock()
	if activeTrace != session {
		return false
	}
	finishTraceLocked(end)
	return true
}

func finishTraceLocked(end traceEnd) {
	s := activeTrace
	if s == nil {
		return
	}
	s.timer.Stop()
	log.StopRecording(s.writer)
	lastTrace = s.writer.finish(s.startedAt, time.Now(), end)
	activeTrace = nil
}

// @Summary Get log trace session status
// @Description Reports whether a trace is active, its captured line count and start time, whether a finished trace is available to download and, in endReason, how it ended: user, time_limit, write_failed, shutdown or interrupted.
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

	name, parts, err := openLastTrace()
	if err != nil {
		writeJsonError(w, http.StatusNotFound, "No trace file available")
		return
	}
	defer closeTrace(parts)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	for _, part := range parts {
		if _, err := io.Copy(w, part); err != nil {
			return
		}
	}
}

func openLastTrace() (string, []io.ReadCloser, error) {
	traceMu.Lock()
	defer traceMu.Unlock()
	if lastTrace == nil {
		return "", nil, os.ErrNotExist
	}
	parts, err := lastTrace.dir.open()
	return lastTrace.name, parts, err
}
