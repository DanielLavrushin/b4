package http

import (
	"net/http"
	"net/http/httptest"
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
