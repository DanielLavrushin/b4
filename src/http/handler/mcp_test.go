package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/log"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func mcpTestCfg() *config.Config {
	cfg := config.NewConfig()
	cfg.System.WebServer.MCP.Enabled = true
	cfg.System.WebServer.Username = "admin"
	cfg.System.WebServer.Password = "hashed-secret"
	cfg.System.Socks5.Password = "socks-pw"
	cfg.System.Socks5.Username = "socks-user"
	cfg.System.API.IPInfoToken = "ipinfo-token"
	cfg.System.MTProto.Secrets = []config.MTProtoSecret{
		{ID: "s1", Name: "daniel", Secret: "deadbeef", Enabled: true},
		{ID: "s2", Name: "max", Secret: "cafebabe", Enabled: false},
	}
	cfg.Sets = []*config.SetConfig{
		{
			Id:      "set-1",
			Name:    "video",
			Enabled: true,
			Targets: config.TargetsConfig{SNIDomains: []string{"youtube.com"}},
		},
		{
			Id:      "set-2",
			Name:    "disabled-set",
			Enabled: false,
			Targets: config.TargetsConfig{SNIDomains: []string{"example.org"}},
		},
	}
	cfg.Sets[0].Fragmentation.Strategy = "combo"
	return &cfg
}

func newMCPTestServer(t *testing.T, cfg *config.Config) *httptest.Server {
	t.Helper()
	api := &API{cfgPtr: testCfgPtr(cfg), geodataManager: geodat.NewGeodataManager("", "")}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterMCPApi()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func connectMCP(t *testing.T, srv *httptest.Server) (*mcp.ClientSession, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: srv.URL + mcpEndpoint}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, ctx
}

func TestMCPToolsListAndCall(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range tools.Tools {
		byName[tool.Name] = tool
	}
	for _, want := range []string{"b4_status", "b4_get_config", "b4_check_domain", "b4_list_sets", "b4_metrics", "b4_diagnostics"} {
		if byName[want] == nil {
			t.Fatalf("tool %q missing; got %v", want, tools.Tools)
		}
		if byName[want].Description == "" {
			t.Errorf("tool %q has no description", want)
		}
	}
	if a := byName["b4_status"].Annotations; a == nil || !a.ReadOnlyHint {
		t.Error("b4_status should be annotated read-only")
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_status"})
	if err != nil {
		t.Fatalf("call b4_status: %v", err)
	}
	if res.IsError {
		t.Fatalf("b4_status returned error: %+v", res.Content)
	}
	var status mcpStatusOut
	if err := json.Unmarshal(mustStructured(t, res), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.SetsTotal != 2 || status.SetsEnabled != 1 {
		t.Fatalf("sets total/enabled = %d/%d, want 2/1", status.SetsTotal, status.SetsEnabled)
	}
}

func TestMCPCheckDomain(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_check_domain",
		Arguments: map[string]any{"domain": "youtube.com"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}
	var out mcpCheckDomainOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Covered {
		t.Fatalf("youtube.com should be covered, got %+v", out)
	}
	if len(out.Matches) == 0 || out.Matches[0].SetName != "video" {
		t.Fatalf("matches = %+v", out.Matches)
	}
}

func TestMCPGetConfigRedactsSecrets(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_get_config"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out mcpRawOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, leak := range []string{
		"socks-pw", "socks-user", "ipinfo-token", "hashed-secret", "deadbeef", "cafebabe",
	} {
		if strings.Contains(out.JSON, leak) {
			t.Fatalf("config output leaked %q", leak)
		}
	}
	if !strings.Contains(out.JSON, redactedMarker) {
		t.Error("expected redaction markers in output")
	}

	var full struct {
		System struct {
			WebServer struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"web_server"`
			MTProto struct {
				Secrets []config.MTProtoSecret `json:"secrets"`
			} `json:"mtproto"`
		} `json:"system"`
	}
	if err := json.Unmarshal([]byte(out.JSON), &full); err != nil {
		t.Fatalf("decode redacted config: %v", err)
	}
	if full.System.WebServer.Username != "" || full.System.WebServer.Password != "" {
		t.Errorf("web server credentials survived redaction: %+v", full.System.WebServer)
	}
	if len(full.System.MTProto.Secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(full.System.MTProto.Secrets))
	}
	for _, s := range full.System.MTProto.Secrets {
		if s.Secret != redactedMarker {
			t.Errorf("secret %q value not redacted: %q", s.ID, s.Secret)
		}
		if s.Name != redactedMarker {
			t.Errorf("secret %q name not redacted: %q", s.ID, s.Name)
		}
		if s.ID == "" {
			t.Error("secret id should survive redaction so the model can still reason about it")
		}
	}
}

func TestMCPGetConfigSection(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_get_config",
		Arguments: map[string]any{"section": "system.socks5"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out mcpRawOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var section map[string]any
	if err := json.Unmarshal([]byte(out.JSON), &section); err != nil {
		t.Fatalf("section not an object: %v (%s)", err, out.JSON)
	}
	if _, ok := section["port"]; !ok {
		t.Fatalf("expected socks5 section, got %s", out.JSON)
	}

	bad, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_get_config",
		Arguments: map[string]any{"section": "system.nope"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !bad.IsError {
		t.Error("unknown section should be a tool error")
	}
}

func TestMCPTopicResources(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	list, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(list.Resources) == 0 {
		t.Fatal("expected topic resources")
	}

	uri := list.Resources[0].URI
	if !strings.HasPrefix(uri, mcpTopicScheme) {
		t.Fatalf("unexpected resource uri %q", uri)
	}
	read, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("read resource %s: %v", uri, err)
	}
	if len(read.Contents) == 0 || read.Contents[0].Text == "" {
		t.Fatalf("resource %s returned no text", uri)
	}
}

func TestMCPDiagnosePrompt(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	if len(prompts.Prompts) != 1 || prompts.Prompts[0].Name != "diagnose_domain" {
		t.Fatalf("prompts = %+v", prompts.Prompts)
	}

	got, err := session.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      "diagnose_domain",
		Arguments: map[string]string{"domain": "youtube.com"},
	})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("messages = %+v", got.Messages)
	}
	text, ok := got.Messages[0].Content.(*mcp.TextContent)
	if !ok || !strings.Contains(text.Text, "youtube.com") {
		t.Fatalf("prompt content = %+v", got.Messages[0].Content)
	}
}

func TestMCPDiagnosticsTool(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_diagnostics"})
	if err != nil {
		t.Fatalf("call b4_diagnostics: %v", err)
	}
	if res.IsError {
		t.Fatalf("b4_diagnostics returned error: %+v", res.Content)
	}
	var out mcpRawOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var diag map[string]any
	if err := json.Unmarshal([]byte(out.JSON), &diag); err != nil {
		t.Fatalf("diagnostics not an object: %v", err)
	}
	if len(diag) == 0 {
		t.Fatal("diagnostics payload is empty")
	}
}

func TestMCPToolPanicDoesNotCrashServer(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	addTool(srv, &mcp.Tool{Name: "boom", Description: "panics"},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpEmpty) (*mcp.CallToolResult, mcpEmpty, error) {
			var p *config.Config
			_ = p.System
			return nil, mcpEmpty{}, nil
		})

	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "boom"})
	if err != nil {
		t.Fatalf("panicking tool should return a tool error, not a transport failure: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for a panicking tool")
	}

	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatalf("server must survive a tool panic: %v", err)
	}
}

func TestMCPGetSet(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	for _, key := range []string{"set-1", "video", "VIDEO"} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "b4_get_set",
			Arguments: map[string]any{"set": key},
		})
		if err != nil {
			t.Fatalf("call with %q: %v", key, err)
		}
		if res.IsError {
			t.Fatalf("lookup by %q failed: %+v", key, res.Content)
		}
		var out mcpRawOut
		if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var set map[string]any
		if err := json.Unmarshal([]byte(out.JSON), &set); err != nil {
			t.Fatalf("set not an object: %v", err)
		}
		if set["name"] != "video" {
			t.Fatalf("got set %v for key %q", set["name"], key)
		}
	}

	missing, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_get_set",
		Arguments: map[string]any{"set": "does-not-exist"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !missing.IsError {
		t.Error("unknown set should be a tool error")
	}
}

func TestMCPRecentConnections(t *testing.T) {
	hub := log.GetConnectionHub()
	hub.Broadcast("tcp,video,youtube.com,10.0.0.2:5111,,142.250.1.1:443,,TLS1.3")
	hub.Broadcast("tcp,,example.org,10.0.0.2:5112,,93.184.1.1:443,,TLS1.2")

	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_recent_connections"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out mcpRecentConnOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Connections) < 2 {
		t.Fatalf("expected buffered connections, got %+v", out.Connections)
	}

	filtered, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_recent_connections",
		Arguments: map[string]any{"contains": "YOUTUBE"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var fout mcpRecentConnOut
	if err := json.Unmarshal(mustStructured(t, filtered), &fout); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(fout.Connections) == 0 {
		t.Fatal("case-insensitive filter returned nothing")
	}
	for _, l := range fout.Connections {
		if !strings.Contains(strings.ToLower(l), "youtube") {
			t.Fatalf("filter leaked non-matching line: %q", l)
		}
	}
}

func TestMCPLogsTail(t *testing.T) {
	dir := t.TempDir()
	cfg := mcpTestCfg()
	cfg.System.Logging.Directory = dir

	var sb strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&sb, "line-%03d ordinary message\n", i)
	}
	sb.WriteString("line-300 nftables rule failed\n")
	if err := os.WriteFile(cfg.System.Logging.ErrorFilePath(), []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_logs_tail",
		Arguments: map[string]any{"limit": 10},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out mcpLogsOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Lines) != 10 {
		t.Fatalf("want 10 lines, got %d", len(out.Lines))
	}
	if !strings.Contains(out.Lines[len(out.Lines)-1], "line-300") {
		t.Fatalf("newest line should be last, got %q", out.Lines[len(out.Lines)-1])
	}

	filtered, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_logs_tail",
		Arguments: map[string]any{"contains": "nftables"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var fout mcpLogsOut
	if err := json.Unmarshal(mustStructured(t, filtered), &fout); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(fout.Lines) != 1 || !strings.Contains(fout.Lines[0], "nftables") {
		t.Fatalf("filter = %+v", fout.Lines)
	}
}

func TestMCPLogsTailDisabledAndMissing(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.Logging.Directory = ""
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_logs_tail"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("disabled logging should not be a tool error: %+v", res.Content)
	}
	var out mcpLogsOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Note == "" {
		t.Error("expected an explanatory note when file logging is off")
	}

	cfg2 := mcpTestCfg()
	cfg2.System.Logging.Directory = t.TempDir()
	srv2 := newMCPTestServer(t, cfg2)
	session2, ctx2 := connectMCP(t, srv2)

	res2, err := session2.CallTool(ctx2, &mcp.CallToolParams{Name: "b4_logs_tail"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res2.IsError {
		t.Fatalf("absent log file should not be a tool error: %+v", res2.Content)
	}
}

func TestMCPTailLinesReadsOnlyFileTail(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/big.log"

	var sb strings.Builder
	for i := 0; sb.Len() < mcpTailReadCap*2; i++ {
		fmt.Fprintf(&sb, "line-%06d %s\n", i, strings.Repeat("x", 200))
	}
	sb.WriteString("FINAL-LINE\n")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := tailLines(path, 5, "")
	if err != nil {
		t.Fatalf("tailLines: %v", err)
	}
	if len(lines) != 5 {
		t.Fatalf("want 5 lines, got %d", len(lines))
	}
	if lines[len(lines)-1] != "FINAL-LINE" {
		t.Fatalf("last line = %q", lines[len(lines)-1])
	}
	for _, l := range lines {
		if strings.Contains(l, "line-000000") {
			t.Fatal("should not have read from the start of a large file")
		}
	}
}

func TestMCPGateDisabled(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.Enabled = false
	srv := newMCPTestServer(t, cfg)

	resp := mcpRawPost(t, srv.URL+mcpEndpoint, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestMCPServesWithoutWebAuth(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.Username = ""
	cfg.System.WebServer.Password = ""
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools without web auth: %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("expected tools to be served when web auth is disabled")
	}
}

func TestMCPOriginCheckAppliesWithoutWebAuth(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.Username = ""
	cfg.System.WebServer.Password = ""
	srv := newMCPTestServer(t, cfg)

	if resp := mcpRawPost(t, srv.URL+mcpEndpoint, "http://evil.example"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("origin check must still apply without web auth: status = %d, want 403", resp.StatusCode)
	}
}

func TestMCPGateRejectsForeignOrigin(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())

	if resp := mcpRawPost(t, srv.URL+mcpEndpoint, "http://evil.example"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin status = %d, want 403", resp.StatusCode)
	}
	if resp := mcpRawPost(t, srv.URL+mcpEndpoint, "http://"+strings.TrimPrefix(srv.URL, "http://")); resp.StatusCode == http.StatusForbidden {
		t.Fatal("same-host origin must not be rejected")
	}
}

func TestMCPOriginAllowedRules(t *testing.T) {
	cases := []struct {
		origin, host string
		allowed      []string
		want         bool
	}{
		{"http://192.168.1.1:7000", "192.168.1.1:7000", nil, true},
		{"http://192.168.1.1", "192.168.1.1:7000", nil, true},
		{"http://evil.example", "192.168.1.1:7000", nil, false},
		{"http://evil.example", "192.168.1.1:7000", []string{"http://evil.example"}, true},
		{"http://anything", "192.168.1.1:7000", []string{"*"}, true},
		{"not a url", "192.168.1.1:7000", nil, false},
	}
	for _, tc := range cases {
		if got := mcpOriginAllowed(tc.origin, tc.host, tc.allowed); got != tc.want {
			t.Errorf("mcpOriginAllowed(%q, %q, %v) = %v, want %v", tc.origin, tc.host, tc.allowed, got, tc.want)
		}
	}
}

func mcpRawPost(t *testing.T, url, origin string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func mustStructured(t *testing.T, res *mcp.CallToolResult) []byte {
	t.Helper()
	if res.StructuredContent == nil {
		t.Fatalf("no structured content: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	return raw
}
