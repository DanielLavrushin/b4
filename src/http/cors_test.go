package http

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/handler"
	"github.com/daniellavrushin/b4/log"
)

func TestCors(t *testing.T) {
	h := cors(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("sets CORS headers when Origin present", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
			t.Error("expected Access-Control-Allow-Origin to match Origin")
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Error("expected Access-Control-Allow-Credentials to be true")
		}
		if rec.Header().Get("Access-Control-Allow-Methods") == "" {
			t.Error("expected Access-Control-Allow-Methods to be set")
		}
		if rec.Header().Get("Access-Control-Allow-Headers") == "" {
			t.Error("expected Access-Control-Allow-Headers to be set")
		}
		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
	})

	t.Run("no CORS headers without Origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("expected no CORS headers without Origin")
		}
	})

	t.Run("OPTIONS returns 204 No Content", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("expected status 204, got %d", rec.Code)
		}
	})

	t.Run("OPTIONS without Origin still returns 204", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("expected status 204, got %d", rec.Code)
		}
	})
}

func TestStartServer_DisabledWithPort0(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.WebServer.Port = 0

	cfgPtr := &atomic.Pointer[config.Config]{}
	cfgPtr.Store(&cfg)
	srv, _, err := StartServer(cfgPtr, nil)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if srv != nil {
		t.Error("expected nil server when port is 0")
	}
}

func corsCfg(username string) *atomic.Pointer[config.Config] {
	cfg := config.NewConfig()
	cfg.System.WebServer.Username = username
	cfg.System.WebServer.Password = username
	ptr := &atomic.Pointer[config.Config]{}
	ptr.Store(&cfg)
	return ptr
}

func TestCorsRefusesCrossSiteWritesWithoutCredentials(t *testing.T) {
	served := 0
	guarded := cors(corsCfg(""), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served++
		w.WriteHeader(http.StatusOK)
	}))
	send := func(method, path, origin, fetchSite string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://192.168.1.1:7000"+path, nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if fetchSite != "" {
			req.Header.Set("Sec-Fetch-Site", fetchSite)
		}
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		return rec
	}

	rec := send(http.MethodPost, "/api/hub/identity/restore", "http://evil.example", "cross-site")
	if rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" || served != 0 {
		t.Fatalf("a cross-site write must be refused without a CORS grant, got %d %q served=%d", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"), served)
	}
	rec = send(http.MethodGet, "/api/config", "http://evil.example", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("a cross-site read passes to the handler but gets no CORS grant, got %d %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
	rec = send(http.MethodOptions, "/api/config", "http://evil.example", "cross-site")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("a cross-site preflight must not be granted, got %d %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
	rec = send(http.MethodPost, "/api/config", "http://192.168.1.1:7000", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "http://192.168.1.1:7000" {
		t.Fatalf("the router's own origin must be served, got %d", rec.Code)
	}
	rec = send(http.MethodPost, "/api/config", "http://192.168.1.1:5173", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("another port on the same host is another origin, got %d", rec.Code)
	}
	rec = send(http.MethodPost, "/api/config", "https://192.168.1.1:7000", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("another scheme on the same host is another origin, got %d", rec.Code)
	}
	rec = send(http.MethodPost, "/api/config", "http://addon.router.example", "same-site")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a sibling site is not trusted, got %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7000/api/config", nil)
	req.Host = "b4.example.com"
	req.Header.Set("Origin", "https://b4.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("behind a proxy that forwards Host and the scheme an old browser is still recognised, got %d", rec.Code)
	}
	rec = send(http.MethodPost, "/api/config", "https://b4.example.com", "same-origin")
	if rec.Code != http.StatusOK {
		t.Fatalf("a browser-attested same-origin request behind a proxy must be served, got %d", rec.Code)
	}
	rec = send(http.MethodPost, "/api/config", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("a request without Origin is not a browser cross-site request, got %d", rec.Code)
	}

	mcpCfg := corsCfg("")
	mcpCfg.Load().System.WebServer.MCP.Token = "mcp-secret"
	mcp := cors(mcpCfg, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	for _, tc := range []struct {
		method, token string
		code          int
		granted       bool
	}{
		{http.MethodOptions, "", http.StatusNoContent, true},
		{http.MethodPost, "mcp-secret", http.StatusOK, true},
		{http.MethodPost, "wrong", http.StatusForbidden, false},
		{http.MethodPost, "", http.StatusForbidden, false},
	} {
		req := httptest.NewRequest(tc.method, "http://192.168.1.1:7000"+handler.MCPEndpoint, nil)
		req.Header.Set("Origin", "http://tool.example")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		rec = httptest.NewRecorder()
		mcp.ServeHTTP(rec, req)
		if rec.Code != tc.code || (rec.Header().Get("Access-Control-Allow-Origin") != "") != tc.granted {
			t.Fatalf("mcp %s token=%q: got %d granted=%v, want %d granted=%v", tc.method, tc.token, rec.Code, rec.Header().Get("Access-Control-Allow-Origin") != "", tc.code, tc.granted)
		}
	}

	for _, auth := range []string{"bearer mcp-secret", "BEARER mcp-secret", "Bearer\tmcp-secret"} {
		req := httptest.NewRequest(http.MethodPost, "http://192.168.1.1:7000"+handler.MCPEndpoint, nil)
		req.Header.Set("Origin", "http://tool.example")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Authorization", auth)
		rec = httptest.NewRecorder()
		mcp.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") == "" {
			t.Fatalf("mcp with Authorization %q: got %d granted=%v, want 200 granted, as the MCP gate accepts it", auth, rec.Code, rec.Header().Get("Access-Control-Allow-Origin") != "")
		}
	}

	authed := cors(corsCfg("admin"), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	req = httptest.NewRequest(http.MethodPost, "http://192.168.1.1:7000/api/config", nil)
	req.Header.Set("Origin", "http://tool.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec = httptest.NewRecorder()
	authed.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "http://tool.example" {
		t.Fatalf("with credentials configured the bearer token is the guard and cross-origin clients keep their grant, got %d %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCorsRefusesCrossSiteWebSocketsWithoutCredentials(t *testing.T) {
	served := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served++
		w.WriteHeader(http.StatusOK)
	})
	upgrade := func(guarded http.Handler, host, origin, fetchSite, connection string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/api/ws/connections", nil)
		req.Header.Set("Connection", connection)
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if fetchSite != "" {
			req.Header.Set("Sec-Fetch-Site", fetchSite)
		}
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		return rec
	}

	open := cors(corsCfg(""), next)
	for _, tc := range []struct {
		name, host, origin, fetchSite, connection string
		refused                                   bool
	}{
		{"a foreign page", "192.168.1.1:7000", "http://evil.example", "", "Upgrade", true},
		{"a foreign page over https", "192.168.1.1:7000", "https://evil.example", "cross-site", "Upgrade", true},
		{"a sandboxed frame", "192.168.1.1:7000", "null", "", "Upgrade", true},
		{"another port on the router", "192.168.1.1:7000", "http://192.168.1.1:5173", "", "Upgrade", true},
		{"a sibling site", "b4.router.example:7000", "http://addon.router.example:7000", "same-site", "Upgrade", true},
		{"a foreign page with a comma-list Connection header", "192.168.1.1:7000", "http://evil.example", "", "keep-alive, Upgrade", true},
		{"the interface itself", "192.168.1.1:7000", "http://192.168.1.1:7000", "", "Upgrade", false},
		{"an upgrade attested same-origin by Sec-Fetch-Site, which Firefox sends and Chrome does not", "127.0.0.1:17070", "http://localhost:5173", "same-origin", "Upgrade", false},
		{"an unattested upgrade whose Origin differs from Host, as Chrome sends through a proxy", "127.0.0.1:17070", "http://localhost:5173", "", "Upgrade", true},
		{"a client that is not a browser", "192.168.1.1:7000", "", "", "Upgrade", false},
	} {
		served = 0
		rec := upgrade(open, tc.host, tc.origin, tc.fetchSite, tc.connection)
		if refused := rec.Code == http.StatusForbidden && served == 0; refused != tc.refused {
			t.Errorf("%s: got %d served=%d, want refused=%v", tc.name, rec.Code, served, tc.refused)
		}
		if tc.refused && rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: a refused upgrade must not carry a CORS grant", tc.name)
		}
	}

	served = 0
	rec := upgrade(cors(corsCfg("admin"), next), "192.168.1.1:7000", "http://tool.example", "cross-site", "Upgrade")
	if rec.Code != http.StatusOK || served != 1 {
		t.Fatalf("with credentials configured the token check guards the stream, so the upgrade must reach it, got %d served=%d", rec.Code, served)
	}
}

func corsRequest(t *testing.T, listener, host, method, origin, fetchSite string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+"/api/ws/logs", nil)
	req.Host = host
	local, err := net.ResolveTCPAddr("tcp", listener)
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, local))
	if method == http.MethodGet {
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	}
	req.Header.Set("Origin", origin)
	if fetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", fetchSite)
	}
	return req
}

func TestCorsAcceptsTheUIBehindAProxyThatDropsThePort(t *testing.T) {
	served := 0
	open := cors(corsCfg(""), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served++
		w.WriteHeader(http.StatusOK)
	}))
	for _, tc := range []struct {
		name, listener, host, method, origin, fetchSite string
		refused                                         bool
	}{
		{"the interface through nginx with Host $host", "127.0.0.1:7000", "b4.example.com", http.MethodGet, "https://b4.example.com:9743", "", false},
		{"a settings save through the same proxy over plain http", "127.0.0.1:7000", "b4.example.com", http.MethodPost, "http://b4.example.com:8080", "", false},
		{"the host name in another case", "127.0.0.1:7000", "b4.example.com", http.MethodGet, "https://B4.Example.COM:9743", "", false},
		{"an IPv6 host without a port", "[::1]:7000", "[fd00::1]", http.MethodGet, "http://[fd00::1]:9743", "", false},
		{"a foreign page through the same proxy", "127.0.0.1:7000", "b4.example.com", http.MethodGet, "https://evil.example", "", true},
		{"a foreign write through the same proxy", "127.0.0.1:7000", "b4.example.com", http.MethodPost, "https://evil.example", "", true},
		{"a page the browser attests is only same-site", "127.0.0.1:7000", "b4.example.com", http.MethodGet, "https://b4.example.com:8443", "same-site", true},
		{"a Host that keeps its port is compared with the port", "127.0.0.1:7000", "127.0.0.1:7000", http.MethodGet, "http://127.0.0.1:5173", "", true},
		{"another port of the router while b4 listens on port 80 itself", "192.168.1.1:80", "192.168.1.1", http.MethodGet, "http://192.168.1.1:5173", "", true},
		{"another port of the host while b4 listens on port 443 itself", "203.0.113.5:443", "b4.example.com", http.MethodGet, "https://b4.example.com:8443", "", true},
		{"the interface while b4 listens on port 80 itself", "192.168.1.1:80", "192.168.1.1", http.MethodGet, "http://192.168.1.1", "", false},
	} {
		served = 0
		rec := httptest.NewRecorder()
		open.ServeHTTP(rec, corsRequest(t, tc.listener, tc.host, tc.method, tc.origin, tc.fetchSite))
		if refused := rec.Code == http.StatusForbidden && served == 0; refused != tc.refused {
			t.Errorf("%s: got %d served=%d, want refused=%v", tc.name, rec.Code, served, tc.refused)
		}
	}
}

type corsLogBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (c *corsLogBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.Write(p)
}

func (c *corsLogBuffer) refusals() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var lines []string
	for _, line := range strings.Split(c.b.String(), "\n") {
		if strings.Contains(line, "Web server: refused") {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestCorsLogsRefusalsWithTheFixAtMostOncePerWindow(t *testing.T) {
	buf := &corsLogBuffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	log.StartCapture(buf)
	resetRefusals := func() {
		crossSiteRefusals.Lock()
		crossSiteRefusals.last = time.Time{}
		crossSiteRefusals.suppressed = 0
		crossSiteRefusals.Unlock()
	}
	resetRefusals()
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
		resetRefusals()
	})

	open := cors(corsCfg(""), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	refuse := func() {
		t.Helper()
		rec := httptest.NewRecorder()
		open.ServeHTTP(rec, corsRequest(t, "127.0.0.1:7000", "127.0.0.1:7000", http.MethodGet, "https://b4.example.com:9743", ""))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("the upgrade must be refused, got %d", rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	open.ServeHTTP(rec, corsRequest(t, "127.0.0.1:7000", "127.0.0.1:7000", http.MethodGet, "http://127.0.0.1:7000", ""))
	if rec.Code != http.StatusOK || len(buf.refusals()) != 0 {
		t.Fatalf("a served upgrade logs nothing, got %d and %q", rec.Code, buf.refusals())
	}

	refuse()
	lines := buf.refusals()
	if len(lines) != 1 {
		t.Fatalf("the first refusal must be logged once, got %q", lines)
	}
	for _, want := range []string{"[WARN]", "WebSocket", `"/api/ws/logs"`, `"https://b4.example.com:9743"`, `"http://127.0.0.1:7000"`, "Settings > System > Web Server", "Host header with its port", "X-Forwarded-Proto"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the refusal line must contain %q: %s", want, lines[0])
		}
	}

	refuse()
	refuse()
	if lines := buf.refusals(); len(lines) != 1 {
		t.Fatalf("refusals inside the window must not be logged, got %q", lines)
	}

	crossSiteRefusals.Lock()
	crossSiteRefusals.last = crossSiteRefusals.last.Add(-crossSiteRefusalWindow - time.Second)
	crossSiteRefusals.Unlock()
	refuse()
	lines = buf.refusals()
	if len(lines) != 2 || !strings.Contains(lines[1], "(2 more refused since the previous line)") {
		t.Fatalf("the next window logs again with the count it held back, got %q", lines)
	}
}
