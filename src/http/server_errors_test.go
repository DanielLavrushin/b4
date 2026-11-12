package http

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/ai"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/handler"
	"github.com/daniellavrushin/b4/log"
	"github.com/gorilla/websocket"
)

type serverErrorLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *serverErrorLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *serverErrorLog) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var lines []string
	for _, line := range strings.Split(l.b.String(), "\n") {
		if strings.Contains(line, " answered ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func resetServerErrors() {
	serverErrors.Lock()
	serverErrors.routes = nil
	serverErrors.Unlock()
}

func captureServerErrors(t *testing.T) *serverErrorLog {
	t.Helper()
	buf := &serverErrorLog{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	log.StartCapture(buf)
	resetServerErrors()
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
		resetServerErrors()
	})
	return buf
}

func ageServerErrors(by time.Duration) {
	serverErrors.Lock()
	for route, seen := range serverErrors.routes {
		seen.last = seen.last.Add(-by)
		serverErrors.routes[route] = seen
	}
	serverErrors.Unlock()
}

func serveRecorded(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func serverErrorMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"failed to generate token"}`+"\n")
	})
	mux.HandleFunc("/api/text", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "No workers available", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("/api/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "item "+r.PathValue("id")+" is broken", http.StatusBadGateway)
	})
	mux.HandleFunc("/api/empty", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("/api/long", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "first line\nsecond\tline\x1b[31m ")
		_, _ = io.WriteString(w, strings.Repeat("я", 400))
	})
	mux.HandleFunc("/api/ok", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "fine")
	})
	mux.HandleFunc("/api/late", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "already answered")
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("/api/refused", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	})
	return mux
}

func TestServerChainLogsAServerErrorItsHandlerDoesNotLog(t *testing.T) {
	buf := captureServerErrors(t)
	base, _ := startChain(t, "")

	resp, err := http.Get(base + "/api/system/cache")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("without workers the cache endpoint answers 503, got %d", resp.StatusCode)
	}

	lines := buf.lines()
	if len(lines) != 1 {
		t.Fatalf("the 503 must be logged once, got %q", lines)
	}
	for _, want := range []string{"[ERROR]", "Web server: GET /api/system/cache answered 503", "No workers available"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the line must contain %q: %s", want, lines[0])
		}
	}
}

func TestLogServerErrorsNamesTheRequestTheStatusAndTheBody(t *testing.T) {
	buf := captureServerErrors(t)
	h := logServerErrors(serverErrorMux())

	if rec := serveRecorded(t, h, http.MethodPost, "/api/json?token=s3cret"); rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "failed to generate token") {
		t.Fatalf("the answer must pass through unchanged, got %d %q", rec.Code, rec.Body.String())
	}
	lines := buf.lines()
	if len(lines) != 1 {
		t.Fatalf("a 500 must be logged once, got %q", lines)
	}
	if !strings.HasSuffix(lines[0], `[ERROR] Web server: POST /api/json answered 500: {"error":"failed to generate token"}`) {
		t.Errorf("unexpected line: %s", lines[0])
	}
	if strings.Contains(lines[0], "s3cret") || strings.Contains(lines[0], "token=") {
		t.Errorf("the query string must never be logged: %s", lines[0])
	}

	serveRecorded(t, h, http.MethodGet, "/api/empty")
	lines = buf.lines()
	if len(lines) != 2 || !strings.HasSuffix(lines[1], "Web server: GET /api/empty answered 500") {
		t.Fatalf("an answer without a body is logged without one, got %q", lines)
	}
}

func TestLogServerErrorsIgnoresAnswersBelow500(t *testing.T) {
	buf := captureServerErrors(t)
	h := logServerErrors(serverErrorMux())

	for target, want := range map[string]int{
		"/api/ok":      http.StatusOK,
		"/api/late":    http.StatusOK,
		"/api/refused": http.StatusForbidden,
		"/api/missing": http.StatusNotFound,
	} {
		if rec := serveRecorded(t, h, http.MethodGet, target); rec.Code != want {
			t.Fatalf("%s: got %d, want %d", target, rec.Code, want)
		}
	}
	if lines := buf.lines(); len(lines) != 0 {
		t.Fatalf("only a 5xx is logged, got %q", lines)
	}
}

func TestLogServerErrorsKeepsTheLineShortAndOnOneLine(t *testing.T) {
	buf := captureServerErrors(t)
	h := logServerErrors(serverErrorMux())

	serveRecorded(t, h, http.MethodGet, "/api/long")
	lines := buf.lines()
	if len(lines) != 1 {
		t.Fatalf("a multi-line body must stay on one log line, got %q", lines)
	}
	if !strings.Contains(lines[0], "answered 500: first line second line [31m я") {
		t.Errorf("whitespace and control characters must be flattened: %s", lines[0])
	}
	if !strings.HasSuffix(lines[0], "я...") {
		t.Errorf("a long body must be cut on a character boundary and marked: %s", lines[0])
	}
	if excerpt := lines[0][strings.Index(lines[0], "answered 500: ")+len("answered 500: "):]; len(excerpt) > serverErrorExcerptLen+len("...") {
		t.Errorf("the excerpt is %d bytes, over the %d byte limit", len(excerpt), serverErrorExcerptLen)
	}

	serveRecorded(t, h, http.MethodGet, "/api/items/"+strings.Repeat("a", 2*serverErrorPathLen)+"%0Ainjected")
	lines = buf.lines()
	if len(lines) != 2 {
		t.Fatalf("a path with an encoded newline must stay on one log line, got %q", lines)
	}
	if strings.Contains(lines[1], "injected") || !strings.Contains(lines[1], "a... answered 502 (route /api/items/{id})") {
		t.Errorf("a long path must be cut: %s", lines[1])
	}
}

func TestServerChainKeepsAnEndpointCredentialOutOfTheLog(t *testing.T) {
	buf := captureServerErrors(t)
	previous := handler.GetAIManager()
	handler.SetAIManager(ai.NewManager(config.AIConfig{}, filepath.Join(t.TempDir(), "config.json")))
	t.Cleanup(func() { handler.SetAIManager(previous) })
	base, _ := startChain(t, "")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	ln.Close()
	endpoint := "http://" + closed + "/SECRETPATH/v1?first=1&api_key=SECRETQUERY"

	resp, err := http.Get(base + "/api/ai/models?provider=" + ai.ProviderOpenAICompatible + "&endpoint=" + url.QueryEscape(endpoint))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "SECRETQUERY") || !strings.Contains(string(body), "SECRETPATH") {
		t.Fatalf("the provider error must name the endpoint in a 502, got %d %s", resp.StatusCode, body)
	}

	lines := buf.lines()
	if len(lines) != 1 {
		t.Fatalf("the 502 must be logged once, got %q", lines)
	}
	for _, secret := range []string{"SECRETPATH", "SECRETQUERY", "api_key", "first=1", "/v1"} {
		if strings.Contains(lines[0], secret) {
			t.Errorf("the endpoint's path and query must stay out of the log, found %q: %s", secret, lines[0])
		}
	}
	if want := `\"http://` + closed + `/[redacted]\"`; !strings.Contains(lines[0], "Web server: GET /api/ai/models answered 502") || !strings.Contains(lines[0], want) {
		t.Errorf("the line must still name the endpoint's host as %s: %s", want, lines[0])
	}
}

func TestLogServerErrorsRemovesCredentialsFromURLsInTheBody(t *testing.T) {
	buf := captureServerErrors(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/quoted", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": `Get "https://api.example.com/v1/models?first=1&api_key=SECRETQUERY&more=<x>": dial tcp: no route to host`})
	})
	mux.HandleFunc("/api/bare", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failed to fetch https://user:SECRETPASS@mirror.example.com/geo.dat#access_token=SECRETFRAG then socks5://proxy:SECRETPROXY@10.0.0.1:1080 timed out", http.StatusBadGateway)
	})
	mux.HandleFunc("/api/plain", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Could not reach https://api.github.com/repos/SECRETPATH/releases or http://[::1]:7000/ or https://b4.example.com: no route", http.StatusBadGateway)
	})
	h := logServerErrors(mux)

	serveRecorded(t, h, http.MethodGet, "/api/quoted")
	serveRecorded(t, h, http.MethodGet, "/api/bare")
	serveRecorded(t, h, http.MethodGet, "/api/plain")
	lines := buf.lines()
	if len(lines) != 3 {
		t.Fatalf("three routes log three lines, got %q", lines)
	}
	for _, line := range lines {
		for _, secret := range []string{"SECRETQUERY", "SECRETPASS", "SECRETFRAG", "SECRETPROXY", "SECRETPATH", "api_key", "more=", "access_token", "geo.dat"} {
			if strings.Contains(line, secret) {
				t.Errorf("%q must stay out of the log: %s", secret, line)
			}
		}
	}
	if want := `{"error":"Get \"https://api.example.com/[redacted]\": dial tcp: no route to host"}`; !strings.HasSuffix(lines[0], want) {
		t.Errorf("a path and a query written with JSON escapes must be removed whole, want %s: %s", want, lines[0])
	}
	if want := "failed to fetch https://[redacted]@mirror.example.com/[redacted] then socks5://[redacted]@10.0.0.1:1080 timed out"; !strings.HasSuffix(lines[1], want) {
		t.Errorf("user info, a path and a fragment must be removed, want %s: %s", want, lines[1])
	}
	if want := "Could not reach https://api.github.com/[redacted] or http://[::1]:7000/ or https://b4.example.com: no route"; !strings.HasSuffix(lines[2], want) {
		t.Errorf("a URL keeps its scheme and host and a bare root path, want %s: %s", want, lines[2])
	}
}

func TestLogServerErrorsLimitsEachRouteToOneLinePerWindow(t *testing.T) {
	buf := captureServerErrors(t)
	h := logServerErrors(serverErrorMux())

	serveRecorded(t, h, http.MethodGet, "/api/items/1")
	serveRecorded(t, h, http.MethodGet, "/api/items/2")
	serveRecorded(t, h, http.MethodGet, "/api/items/3")
	lines := buf.lines()
	if len(lines) != 1 {
		t.Fatalf("one route is logged once per window whatever its path values, got %q", lines)
	}
	if !strings.HasSuffix(lines[0], "Web server: GET /api/items/1 answered 502 (route /api/items/{id}): item 1 is broken") {
		t.Errorf("unexpected line: %s", lines[0])
	}

	serveRecorded(t, h, http.MethodPost, "/api/items/1")
	serveRecorded(t, h, http.MethodGet, "/api/text")
	if lines := buf.lines(); len(lines) != 3 {
		t.Fatalf("another method and another route have their own limit, got %q", lines)
	}

	ageServerErrors(serverErrorWindow + time.Second)
	serveRecorded(t, h, http.MethodGet, "/api/items/4")
	lines = buf.lines()
	if len(lines) != 4 || !strings.HasSuffix(lines[3], "item 4 is broken (2 more on this route since the previous line)") {
		t.Fatalf("the next window logs again with the count it held back, got %q", lines)
	}

	serveRecorded(t, h, "BREW", "/api/text")
	serveRecorded(t, h, "STEEP", "/api/text")
	lines = buf.lines()
	if len(lines) != 5 || !strings.Contains(lines[4], "Web server: OTHER /api/text answered 503") {
		t.Fatalf("unknown methods share one limit and are not echoed, got %q", lines)
	}
}

func serveWrapped(t *testing.T, handle http.HandlerFunc) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/probe", handle)
	srv := httptest.NewServer(logServerErrors(mux))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestLogServerErrorsKeepsFlushWorking(t *testing.T) {
	buf := captureServerErrors(t)
	release := make(chan struct{})
	problems := make(chan string, 4)
	base := serveWrapped(t, func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			problems <- "the writer must still be an http.Flusher"
			return
		}
		_, _ = io.WriteString(w, "one\n")
		flusher.Flush()
		<-release
		_, _ = io.WriteString(w, "two\n")
		rc := http.NewResponseController(w)
		if err := rc.Flush(); err != nil {
			problems <- "ResponseController.Flush: " + err.Error()
		}
		if err := rc.SetWriteDeadline(time.Now().Add(time.Minute)); err != nil {
			problems <- "ResponseController.SetWriteDeadline must reach the connection: " + err.Error()
		}
		<-release
	})

	resp, err := http.Get(base + "/api/probe")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := bufio.NewReader(resp.Body)
	for _, want := range []string{"one\n", "two\n"} {
		got, err := body.ReadString('\n')
		if err != nil || got != want {
			t.Fatalf("a flushed chunk must reach the client while the handler is still running, got %q %v", got, err)
		}
		release <- struct{}{}
	}
	if _, err := body.ReadString('\n'); err != io.EOF {
		t.Fatalf("the stream must end cleanly, got %v", err)
	}
	close(problems)
	for problem := range problems {
		t.Error(problem)
	}
	if lines := buf.lines(); len(lines) != 0 {
		t.Fatalf("a streamed 200 logs nothing, got %q", lines)
	}
}

func TestLogServerErrorsLetsAWebSocketUpgradeThrough(t *testing.T) {
	buf := captureServerErrors(t)
	upgrader := websocket.Upgrader{}
	base := serveWrapped(t, func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		kind, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.WriteMessage(kind, append([]byte("echo "), msg...))
	})

	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/api/probe", nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("the upgrade must pass through the wrapper, got %d %v", status, err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, msg, err := conn.ReadMessage(); err != nil || string(msg) != "echo ping" {
		t.Fatalf("the upgraded connection must carry frames both ways, got %q %v", msg, err)
	}
	if lines := buf.lines(); len(lines) != 0 {
		t.Fatalf("an upgraded connection logs nothing, got %q", lines)
	}
}

func TestLogServerErrorsSkipsAHijackedConnection(t *testing.T) {
	buf := captureServerErrors(t)
	done := make(chan error, 1)
	base := serveWrapped(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			done <- fmt.Errorf("the writer must still be an http.Hijacker")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err == nil {
			err = conn.Close()
		}
		done <- err
	})

	if resp, err := http.Get(base + "/api/probe"); err == nil {
		resp.Body.Close()
	}
	if err := <-done; err != nil {
		t.Fatalf("hijack through the wrapper: %v", err)
	}
	if lines := buf.lines(); len(lines) != 0 {
		t.Fatalf("a hijacked connection is the handler's own business, got %q", lines)
	}
}

type notHijackable struct{ http.ResponseWriter }

func TestStatusRecorderReportsAMissingHijacker(t *testing.T) {
	rec := &statusRecorder{ResponseWriter: notHijackable{httptest.NewRecorder()}}
	if _, _, err := rec.Hijack(); err == nil || rec.hijacked {
		t.Fatalf("a writer that cannot hijack must return an error and stay unhijacked, got %v %v", err, rec.hijacked)
	}
}

func TestErrLogFilterKeepsPanicsAndDropsTLSNoise(t *testing.T) {
	var out strings.Builder
	filter := errLogFilter{w: &out}

	dropped := []string{
		"2026/11/12 10:00:00 http: TLS handshake error from 192.168.1.5:51234: EOF\n",
		"2026/11/12 10:00:00 http: TLS handshake error from 192.168.1.5:51234: remote error: tls: unknown certificate\n",
		"2026/11/12 10:00:00 http: URL query contains semicolon, which is no longer a supported separator\n",
	}
	for _, line := range dropped {
		if n, err := filter.Write([]byte(line)); err != nil || n != len(line) {
			t.Fatalf("a dropped line still reports a full write, got %d %v", n, err)
		}
	}
	if out.Len() != 0 {
		t.Fatalf("handshake noise must stay out of the error log, got %q", out.String())
	}

	kept := []string{
		"2026/11/12 10:00:00 http: panic serving 192.168.1.5:51234: tls: failed to parse certificate\ngoroutine 42 [running]:\n",
		"2026/11/12 10:00:00 http2: panic serving 192.168.1.5:51234: TLS handshake error in a handler\ngoroutine 43 [running]:\n",
	}
	for _, line := range kept {
		_, _ = filter.Write([]byte(line))
	}
	if out.String() != strings.Join(kept, "") {
		t.Fatalf("a handler panic must reach the error log whatever its text, got %q", out.String())
	}
}
