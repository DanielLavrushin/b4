package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/handler"
)

func TestMCPWorksBehindAReverseProxyOnALoopbackListener(t *testing.T) {
	session, err := issueToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	t.Cleanup(func() { revokeToken(session) })

	cases := []struct {
		name       string
		mcpToken   string
		login      bool
		auth       string
		wantStatus int
		wantBody   string
	}{
		{"MCP token", "mcp-token-value", false, "Bearer mcp-token-value", http.StatusOK, `"tools"`},
		{"MCP token with a web login", "mcp-token-value", true, "Bearer mcp-token-value", http.StatusOK, `"tools"`},
		{"web login session", "", true, "Bearer " + session, http.StatusOK, `"tools"`},
		{"web login, no session", "", true, "", http.StatusUnauthorized, "unauthorized"},
		{"MCP token, none presented", "mcp-token-value", false, "", http.StatusUnauthorized, "invalid or missing MCP token"},
		{"no authentication at all", "", false, "", http.StatusForbidden, "system.web_server.mcp.token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.System.WebServer.MCP.Enabled = true
			cfg.System.WebServer.MCP.Token = tc.mcpToken
			if tc.login {
				cfg.System.WebServer.Username = "admin"
				cfg.System.WebServer.Password = "hashed"
			}
			cfgPtr := &atomic.Pointer[config.Config]{}
			cfgPtr.Store(&cfg)

			mux := http.NewServeMux()
			registerAPIEndpoints(mux, cfgPtr)
			srv := httptest.NewServer(telegramWebProxyVhost(cors(cfgPtr, authMiddleware(cfgPtr, mux))))
			t.Cleanup(srv.Close)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+handler.MCPEndpoint,
				strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Host = "b4.example.com"
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
			req.Header.Set("Mcp-Method", "tools/list")
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.wantStatus || !strings.Contains(string(raw), tc.wantBody) {
				t.Fatalf("status %d, body %.200q; want %d with %q", resp.StatusCode, raw, tc.wantStatus, tc.wantBody)
			}
		})
	}
}
