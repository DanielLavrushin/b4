package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/handler"
)

func mcpAuthCfg(t *testing.T, mcpToken string) *atomic.Pointer[config.Config] {
	t.Helper()
	cfg := config.NewConfig()
	cfg.System.WebServer.Username = "admin"
	cfg.System.WebServer.Password = "hashed"
	cfg.System.WebServer.MCP.Enabled = true
	cfg.System.WebServer.MCP.Token = mcpToken

	p := &atomic.Pointer[config.Config]{}
	p.Store(&cfg)
	return p
}

func TestAuthMiddlewareAcceptsMCPToken(t *testing.T) {
	cfgPtr := mcpAuthCfg(t, "mcp-token-value")

	reached := false
	h := authMiddleware(cfgPtr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	call := func(path, auth string) int {
		reached = false
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := call(handler.MCPEndpoint, "Bearer mcp-token-value"); got != http.StatusOK || !reached {
		t.Errorf("MCP token on the MCP endpoint should pass: status=%d reached=%v", got, reached)
	}

	if got := call(handler.MCPEndpoint, "Bearer wrong"); got != http.StatusUnauthorized {
		t.Errorf("wrong MCP token = %d, want 401", got)
	}

	if got := call(handler.MCPEndpoint, ""); got != http.StatusUnauthorized {
		t.Errorf("no credential = %d, want 401", got)
	}

	if got := call("/api/config", "Bearer mcp-token-value"); got != http.StatusUnauthorized {
		t.Errorf("MCP token must NOT unlock other API routes, got %d", got)
	}

	for _, auth := range []string{"bearer mcp-token-value", "BEARER mcp-token-value", "Bearer\tmcp-token-value", "Bearer  mcp-token-value"} {
		if got := call(handler.MCPEndpoint, auth); got != http.StatusOK || !reached {
			t.Errorf("Authorization %q must pass like the MCP gate itself lets it: status=%d reached=%v", auth, got, reached)
		}
	}
	if got := call(handler.MCPEndpoint, "Basic mcp-token-value"); got != http.StatusUnauthorized {
		t.Errorf("another auth scheme = %d, want 401", got)
	}
}

func TestAuthMiddlewareRefusesAWrongMCPTokenLikeTheMCPGate(t *testing.T) {
	cfgPtr := mcpAuthCfg(t, "mcp-token-value")
	h := authMiddleware(cfgPtr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a refused MCP request reached the handler: %q", r.Header.Get("Authorization"))
	}))

	for _, auth := range []string{"Bearer wrong", "", "Basic mcp-token-value", "Bearer"} {
		req := httptest.NewRequest(http.MethodPost, handler.MCPEndpoint, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != `Bearer realm="b4 MCP"` {
			t.Errorf("Authorization %q: status %d, WWW-Authenticate %q; with a web login set, the refusal must still be the MCP gate's, with its Bearer challenge and logged reason",
				auth, rec.Code, rec.Header().Get("WWW-Authenticate"))
		}
	}
}

func TestAuthMiddlewareLeavesASwitchedOffMCPToTheGate(t *testing.T) {
	cfgPtr := mcpAuthCfg(t, "mcp-token-value")
	off := cfgPtr.Load().Clone()
	off.System.WebServer.MCP.Enabled = false
	cfgPtr.Store(off)

	reached := false
	h := authMiddleware(cfgPtr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNotFound)
	}))
	token, err := issueToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	t.Cleanup(func() { revokeToken(token) })

	req := httptest.NewRequest(http.MethodPost, handler.MCPEndpoint, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !reached || rec.Code != http.StatusNotFound {
		t.Errorf("with the MCP server switched off, a logged-in caller must reach the gate, which says it is switched off: status %d reached %v", rec.Code, reached)
	}
}

func TestExtractBearerTokenReadsAnySchemeSpellingAndKeepsTheWebSocketFallback(t *testing.T) {
	for _, c := range []struct {
		path, auth, upgrade, want string
	}{
		{"/api/config", "Bearer abc", "", "abc"},
		{"/api/config", "bearer abc", "", "abc"},
		{"/api/config", "BEARER abc", "", "abc"},
		{"/api/config", "Bearer\tabc", "", "abc"},
		{"/api/config", "Basic abc", "", ""},
		{"/api/config?token=abc", "", "", ""},
		{"/api/ws/logs?token=abc", "", "", "abc"},
		{"/api/socket?token=abc", "", "websocket", "abc"},
		{"/api/ws/logs?token=abc", "bearer xyz", "", "xyz"},
		{"/api/ws/logs?token=abc", "Bearer", "", "abc"},
		{"/api/ws/logs?token=abc", "Basic xyz", "", "abc"},
		{"/api/config?token=abc", "Bearer", "", ""},
	} {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		if c.auth != "" {
			req.Header.Set("Authorization", c.auth)
		}
		if c.upgrade != "" {
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", c.upgrade)
		}
		if got := extractBearerToken(req); got != c.want {
			t.Errorf("%s with Authorization %q: token = %q, want %q", c.path, c.auth, got, c.want)
		}
	}
}

func TestAuthMiddlewareAcceptsASessionTokenWithAnySchemeSpelling(t *testing.T) {
	cfgPtr := mcpAuthCfg(t, "")
	h := authMiddleware(cfgPtr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	token, err := issueToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	t.Cleanup(func() { revokeToken(token) })

	for _, auth := range []string{"Bearer " + token, "bearer " + token, "BEARER " + token, "Bearer\t" + token} {
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		req.Header.Set("Authorization", auth)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("a valid session token sent as %q = %d, want 200", strings.Replace(auth, token, "<token>", 1), rec.Code)
		}
	}
}

func TestAuthMiddlewareWithoutMCPTokenStillRequiresSession(t *testing.T) {
	cfgPtr := mcpAuthCfg(t, "")

	h := authMiddleware(cfgPtr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, handler.MCPEndpoint, nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("with no MCP token configured the session check must apply, got %d", rec.Code)
	}
}
