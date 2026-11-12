package http

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	serverErrorWindow     = time.Minute
	serverErrorExcerptLen = 200
	serverErrorPathLen    = 200
)

var urlInText = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://(?:\\[^"]|[^\s"\\])+`)

type serverErrorRoute struct {
	method  string
	pattern string
	status  int
}

type serverErrorSeen struct {
	last       time.Time
	suppressed int
}

var serverErrors struct {
	sync.Mutex
	routes map[serverErrorRoute]serverErrorSeen
}

type statusRecorder struct {
	http.ResponseWriter
	status   int
	body     []byte
	clipped  bool
	hijacked bool
}

func (rec *statusRecorder) WriteHeader(code int) {
	if rec.status == 0 && code >= http.StatusOK {
		rec.status = code
	}
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *statusRecorder) Write(p []byte) (int, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	if rec.status >= http.StatusInternalServerError {
		room := serverErrorExcerptLen + utf8.UTFMax - len(rec.body)
		if len(p) > room {
			rec.clipped = true
		}
		rec.body = append(rec.body, p[:min(len(p), room)]...)
	}
	return rec.ResponseWriter.Write(p)
}

func (rec *statusRecorder) Flush() {
	_ = rec.FlushError()
}

func (rec *statusRecorder) FlushError() error {
	err := http.NewResponseController(rec.ResponseWriter).Flush()
	if err == nil && rec.status == 0 {
		rec.status = http.StatusOK
	}
	return err
}

func (rec *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(rec.ResponseWriter).Hijack()
	if err == nil {
		rec.hijacked = true
	}
	return conn, rw, err
}

func (rec *statusRecorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}

func (rec *statusRecorder) excerpt() string {
	flat := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(string(rec.body), ""))
	text := urlsCutToHost(strings.Join(strings.Fields(flat), " "))
	if !rec.clipped && len(text) <= serverErrorExcerptLen {
		return text
	}
	return clipText(text, serverErrorExcerptLen)
}

func urlsCutToHost(text string) string {
	return urlInText.ReplaceAllStringFunc(text, func(found string) string {
		scheme, rest, _ := strings.Cut(found, "://")
		authority, tail := rest, ""
		if i := strings.IndexAny(rest, "/?#"); i >= 0 {
			authority, tail = rest[:i], rest[i:]
		}
		if i := strings.LastIndexByte(authority, '@'); i >= 0 {
			authority = config.RedactedMarker + authority[i:]
		}
		if tail != "" && tail != "/" {
			tail = "/" + config.RedactedMarker
		}
		return scheme + "://" + authority + tail
	})
}

func clipText(s string, limit int) string {
	if len(s) > limit {
		for limit > 0 && !utf8.RuneStart(s[limit]) {
			limit--
		}
		s = s[:limit]
	}
	return s + "..."
}

func logServerErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.hijacked || rec.status < http.StatusInternalServerError {
			return
		}
		logServerError(r, rec)
	})
}

func logServerError(r *http.Request, rec *statusRecorder) {
	method := loggedMethod(r.Method)
	suppressed, due := serverErrorDue(serverErrorRoute{method: method, pattern: r.Pattern, status: rec.status}, time.Now())
	if !due {
		return
	}

	path := r.URL.EscapedPath()
	route := ""
	if r.Pattern != "" && r.Pattern != path && r.Pattern != r.Method+" "+path {
		route = fmt.Sprintf(" (route %s)", r.Pattern)
	}
	if len(path) > serverErrorPathLen {
		path = clipText(path, serverErrorPathLen)
	}
	line := fmt.Sprintf("Web server: %s %s answered %d%s", method, path, rec.status, route)
	if excerpt := rec.excerpt(); excerpt != "" {
		line += ": " + excerpt
	}
	if suppressed > 0 {
		line += fmt.Sprintf(" (%d more on this route since the previous line)", suppressed)
	}
	log.Errorf("%s", line)
}

func loggedMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	}
	return "OTHER"
}

func serverErrorDue(route serverErrorRoute, now time.Time) (int, bool) {
	serverErrors.Lock()
	defer serverErrors.Unlock()
	seen, known := serverErrors.routes[route]
	if known && now.Sub(seen.last) < serverErrorWindow {
		seen.suppressed++
		serverErrors.routes[route] = seen
		return 0, false
	}
	if serverErrors.routes == nil {
		serverErrors.routes = make(map[serverErrorRoute]serverErrorSeen)
	}
	serverErrors.routes[route] = serverErrorSeen{last: now}
	return seen.suppressed, true
}
