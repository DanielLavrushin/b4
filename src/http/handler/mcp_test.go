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

	"github.com/daniellavrushin/b4/ai"
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
	srv, _ := newMCPTestServerAPI(t, cfg)
	return srv
}

// newMCPTestServerAPI also hands back the API, so a test can read the
// configuration as it stands after a write rather than the value it passed in:
// saving stores a new pointer instead of mutating the original.
func newMCPTestServerAPI(t *testing.T, cfg *config.Config) (*httptest.Server, *API) {
	t.Helper()
	api := &API{
		cfgPtr:         testCfgPtr(cfg),
		geodataManager: geodat.NewGeodataManager(cfg.System.Geo.GeoSitePath, cfg.System.Geo.GeoIpPath),
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterMCPApi()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, api
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
	raw := mustStructured(t, res)
	for _, leak := range []string{
		"socks-pw", "socks-user", "ipinfo-token", "hashed-secret", "deadbeef", "cafebabe",
	} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("config output leaked %q", leak)
		}
	}
	if !strings.Contains(string(raw), redactedMarker) {
		t.Error("expected redaction markers in output")
	}

	var full struct {
		Config struct {
			System struct {
				WebServer struct {
					Username string `json:"username"`
					Password string `json:"password"`
				} `json:"web_server"`
				MTProto struct {
					Secrets []config.MTProtoSecret `json:"secrets"`
				} `json:"mtproto"`
			} `json:"system"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &full); err != nil {
		t.Fatalf("decode redacted config: %v", err)
	}
	if full.Config.System.WebServer.Username != "" || full.Config.System.WebServer.Password != "" {
		t.Errorf("web server credentials survived redaction: %+v", full.Config.System.WebServer)
	}
	if len(full.Config.System.MTProto.Secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(full.Config.System.MTProto.Secrets))
	}
	for _, s := range full.Config.System.MTProto.Secrets {
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
	var out struct {
		Section string         `json:"section"`
		Config  map[string]any `json:"config"`
	}
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Section != "system.socks5" {
		t.Errorf("result should echo the requested section, got %q", out.Section)
	}
	if _, ok := out.Config["port"]; !ok {
		t.Fatalf("expected socks5 section, got %v", out.Config)
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
	var out struct {
		Diagnostics map[string]any `json:"diagnostics"`
	}
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Diagnostics) == 0 {
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
		var out struct {
			Set map[string]any `json:"set"`
		}
		if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out.Set["name"] != "video" {
			t.Fatalf("got set %v for key %q", out.Set["name"], key)
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

func TestMCPLogsTailAlwaysExplainsEmptyResult(t *testing.T) {
	newSession := func(t *testing.T, mutate func(*config.Config)) (*mcp.ClientSession, context.Context) {
		t.Helper()
		cfg := mcpTestCfg()
		mutate(cfg)
		return connectMCP(t, newMCPTestServer(t, cfg))
	}

	call := func(t *testing.T, session *mcp.ClientSession, ctx context.Context, args map[string]any) mcpLogsOut {
		t.Helper()
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_logs_tail", Arguments: args})
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected tool error: %+v", res.Content)
		}
		var out mcpLogsOut
		if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out.Note == "" {
			t.Fatal("note must never be empty — an unexplained empty result is what confused the model")
		}
		if out.Lines == nil {
			t.Error("lines must always be present, even when empty")
		}
		return out
	}

	t.Run("logging disabled", func(t *testing.T) {
		s, ctx := newSession(t, func(c *config.Config) { c.System.Logging.Directory = "" })
		out := call(t, s, ctx, nil)
		if !strings.Contains(out.Note, "disabled") {
			t.Errorf("note = %q", out.Note)
		}
	})

	t.Run("file missing", func(t *testing.T) {
		s, ctx := newSession(t, func(c *config.Config) { c.System.Logging.Directory = t.TempDir() })
		out := call(t, s, ctx, nil)
		if !strings.Contains(out.Note, "does not exist") {
			t.Errorf("note = %q", out.Note)
		}
	})

	t.Run("file empty", func(t *testing.T) {
		dir := t.TempDir()
		s, ctx := newSession(t, func(c *config.Config) { c.System.Logging.Directory = dir })
		if err := os.WriteFile(dir+"/errors.log", nil, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		out := call(t, s, ctx, nil)
		if !strings.Contains(out.Note, "empty") {
			t.Errorf("note = %q", out.Note)
		}
	})

	t.Run("filter matches nothing points at connections tool", func(t *testing.T) {
		dir := t.TempDir()
		s, ctx := newSession(t, func(c *config.Config) { c.System.Logging.Directory = dir })
		if err := os.WriteFile(dir+"/errors.log", []byte("some unrelated error\nanother line\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		out := call(t, s, ctx, map[string]any{"contains": "zona.media"})
		if out.Matched != 0 {
			t.Fatalf("matched = %d, want 0", out.Matched)
		}
		if out.Scanned != 2 {
			t.Errorf("scanned = %d, want 2", out.Scanned)
		}
		if !strings.Contains(out.Note, "b4_recent_connections") {
			t.Errorf("an unmatched domain filter should redirect to the connections tool, note = %q", out.Note)
		}
	})
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

	res, err := tailLines(path, 5, "")
	if err != nil {
		t.Fatalf("tailLines: %v", err)
	}
	if !res.Exists {
		t.Fatal("Exists should be true for a present file")
	}
	if len(res.Lines) != 5 {
		t.Fatalf("want 5 lines, got %d", len(res.Lines))
	}
	if res.Scanned <= 5 {
		t.Fatalf("Scanned should count every line read, got %d", res.Scanned)
	}
	if res.Lines[len(res.Lines)-1] != "FINAL-LINE" {
		t.Fatalf("last line = %q", res.Lines[len(res.Lines)-1])
	}
	for _, l := range res.Lines {
		if strings.Contains(l, "line-000000") {
			t.Fatal("should not have read from the start of a large file")
		}
	}
}

func TestMCPTokenEnforcement(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.Token = "tok-secret"
	cfg.System.WebServer.Username = ""
	cfg.System.WebServer.Password = ""
	srv := newMCPTestServer(t, cfg)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	post := func(auth string) int {
		req, err := http.NewRequest(http.MethodPost, srv.URL+mcpEndpoint, strings.NewReader(body))
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if got := post(""); got != http.StatusUnauthorized {
		t.Errorf("missing token = %d, want 401", got)
	}
	if got := post("Bearer wrong"); got != http.StatusUnauthorized {
		t.Errorf("wrong token = %d, want 401", got)
	}
	if got := post("Bearer tok-secret"); got != http.StatusOK {
		t.Errorf("correct token = %d, want 200", got)
	}
}

func TestMCPTokenClientSession(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.Token = "tok-secret"
	srv := newMCPTestServer(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   srv.URL + mcpEndpoint,
		HTTPClient: &http.Client{Transport: bearerTransport{token: "tok-secret"}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect with MCP token: %v", err)
	}
	defer session.Close()

	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatalf("list tools with MCP token: %v", err)
	}
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(clone)
}

func TestMCPTokenAcceptsIsStrict(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.Token = "abc"

	if MCPTokenAccepts(cfg, "abc") != true {
		t.Error("exact token should match")
	}
	for _, bad := range []string{"", "ab", "abcd", "ABC", " abc"} {
		if MCPTokenAccepts(cfg, bad) {
			t.Errorf("token %q must not be accepted", bad)
		}
	}

	cfg.System.WebServer.MCP.Token = ""
	if MCPTokenAccepts(cfg, "anything") {
		t.Error("no configured token must accept nothing")
	}
	if MCPTokenAccepts(cfg, "") {
		t.Error("empty presented token must never be accepted")
	}
}

func TestMCPGenerateTokenIsRandomAndUsable(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		tok, err := GenerateMCPToken()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if len(tok) != 64 {
			t.Fatalf("token length = %d, want 64 hex chars", len(tok))
		}
		if seen[tok] {
			t.Fatal("generated a duplicate token")
		}
		seen[tok] = true
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
		{"http://192.168.1.1:9999", "192.168.1.1:7000", nil, false},
		{"http://192.168.1.1:9999", "192.168.1.1:9999", nil, true},
		{"http://192.168.1.1:9999", "192.168.1.1:7000", []string{"http://192.168.1.1:9999"}, true},
		{"http://localhost:7000", "localhost:7000", nil, true},
		{"http://[::1]:7000", "[::1]:7000", nil, true},
		{"https://[2001:db8::1]", "[2001:db8::1]:443", nil, true},
		{"http://evil.example", "192.168.1.1:7000", nil, false},

		// DNS rebinding: the attacker owns the name, so it appears in both the
		// page's Origin and the request's Host and any comparison of the two
		// succeeds. A hostname is therefore never accepted on a Host match
		// alone, only through allowed_origins.
		{"http://evil.example", "evil.example:7000", nil, false},
		{"http://evil.example:7000", "evil.example:7000", nil, false},
		{"http://b4.lan:7000", "b4.lan:7000", nil, false},
		{"http://b4.lan:7000", "b4.lan:7000", []string{"http://b4.lan:7000"}, true},
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

func mcpSecretsCfg() *config.Config {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.Token = ""
	cfg.Sets[0].Routing.Enabled = true
	cfg.Sets[0].Routing.Mode = "proxy"
	cfg.Sets[0].Routing.Upstream = config.UpstreamProxyConfig{
		Host: "10.0.0.9", Port: 1080,
		Username: "upstream-user", Password: "upstream-pw",
	}
	return cfg
}

func TestMCPGetConfigRedactsSetUpstreamCredentials(t *testing.T) {
	srv := newMCPTestServer(t, mcpSecretsCfg())
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_get_config"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	raw := string(mustStructured(t, res))
	for _, leak := range []string{"upstream-user", "upstream-pw"} {
		if strings.Contains(raw, leak) {
			t.Errorf("config output leaked set upstream credential %q", leak)
		}
	}
	if !strings.Contains(raw, "10.0.0.9") {
		t.Error("the upstream host should survive: it is not a secret and the model needs it")
	}
}

func TestMCPGetSetRedactsUpstreamCredentials(t *testing.T) {
	cfg := mcpSecretsCfg()
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_get_set",
		Arguments: map[string]any{"set": "video"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	raw := string(mustStructured(t, res))
	for _, leak := range []string{"upstream-user", "upstream-pw"} {
		if strings.Contains(raw, leak) {
			t.Errorf("b4_get_set leaked upstream credential %q", leak)
		}
	}

	// Redacting the copy must not disturb the live config.
	if got := cfg.Sets[0].Routing.Upstream.Password; got != "upstream-pw" {
		t.Errorf("live config was mutated by redaction: password = %q", got)
	}
}

func TestMCPGetConfigRedactsMCPToken(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.Token = "mcp-token-value"
	raw, err := json.Marshal(redactConfigForMCP(cfg))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "mcp-token-value") {
		t.Error("the MCP bearer token must not be returned to the model")
	}
	if cfg.System.WebServer.MCP.Token != "mcp-token-value" {
		t.Error("live config was mutated by redaction")
	}
}

func TestMCPListSetsCountsDomainsOnce(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.Sets[0].Targets.SNIDomains = []string{"youtube.com", "ytimg.com"}
	// The expanded list already contains the manual entries.
	cfg.Sets[0].Targets.DomainsToMatch = []string{"geo-a.com", "geo-b.com", "youtube.com", "ytimg.com"}

	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_list_sets"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out mcpSetsOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Sets[0].Domains != 4 {
		t.Errorf("domain_count = %d, want 4", out.Sets[0].Domains)
	}
	if out.Sets[0].ManualDomains != 2 {
		t.Errorf("manual_domain_count = %d, want 2", out.Sets[0].ManualDomains)
	}

	// A set whose targets have not been expanded yet still reports its manual entries.
	if out.Sets[1].Domains != 1 {
		t.Errorf("unexpanded set domain_count = %d, want 1", out.Sets[1].Domains)
	}
}

func TestMCPClampLimit(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, mcpDefaultLines},
		{-5, mcpDefaultLines},
		{50, 50},
		{mcpMaxLines, mcpMaxLines},
		{mcpMaxLines + 1, mcpMaxLines},
		{100000, mcpMaxLines},
	}
	for _, tc := range cases {
		if got := mcpClampLimit(tc.in); got != tc.want {
			t.Errorf("mcpClampLimit(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestMCPRecentConnectionsClampsOversizedLimit(t *testing.T) {
	hub := log.GetConnectionHub()
	for i := 0; i < mcpMaxLines+50; i++ {
		hub.Broadcast(fmt.Sprintf("conn line %d", i))
	}

	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_recent_connections",
		Arguments: map[string]any{"limit": mcpMaxLines + 1},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out mcpRecentConnOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Connections) != mcpMaxLines {
		t.Errorf("over-max limit returned %d lines, want the maximum %d", len(out.Connections), mcpMaxLines)
	}
}

func TestMCPCheckDomainReportsPerDomainCoverage(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_check_domain",
		Arguments: map[string]any{"domain": "youtube.com, nowhere.example"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out mcpCheckDomainOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Covered {
		t.Error("covered must be false when one of the domains is not covered")
	}
	if !out.CoveredByDomain["youtube.com"] {
		t.Error("youtube.com is in an enabled set and should read as covered")
	}
	if out.CoveredByDomain["nowhere.example"] {
		t.Error("nowhere.example is in no set and should not read as covered")
	}
	if !strings.Contains(out.Note, "nowhere.example") {
		t.Errorf("note should name the uncovered domain, got %q", out.Note)
	}
}

func TestMCPCheckDomainReportsTruncation(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	many := make([]string, 0, maxCheckDomains+3)
	for i := 0; i < maxCheckDomains+3; i++ {
		many = append(many, fmt.Sprintf("d%d.example", i))
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_check_domain",
		Arguments: map[string]any{"domain": strings.Join(many, ",")},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out mcpCheckDomainOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Truncated {
		t.Fatal("dropping domains past the cap must be reported")
	}
	if len(out.Checked) != maxCheckDomains {
		t.Errorf("checked %d domains, want %d", len(out.Checked), maxCheckDomains)
	}
	if !strings.Contains(out.Note, "call again") {
		t.Errorf("note should tell the model to call again, got %q", out.Note)
	}
}

func TestMCPStatusReportsCaptureEngineNotFirewallBackend(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.Tables.Engine = ""
	cfg.Queue.Mode = ""

	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_status"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out mcpStatusOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Engine != "nfqueue" {
		t.Errorf("engine = %q, want the resolved capture engine %q", out.Engine, "nfqueue")
	}
	if out.FirewallBackend != "auto" {
		t.Errorf("firewall_backend = %q, want %q when it is left to auto-detection", out.FirewallBackend, "auto")
	}

	cfg2 := mcpTestCfg()
	cfg2.Queue.Mode = "tun"
	cfg2.System.Tables.Engine = "nftables"
	srv2 := newMCPTestServer(t, cfg2)
	session2, ctx2 := connectMCP(t, srv2)

	res2, err := session2.CallTool(ctx2, &mcp.CallToolParams{Name: "b4_status"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out2 mcpStatusOut
	if err := json.Unmarshal(mustStructured(t, res2), &out2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out2.Engine != "tun" || out2.FirewallBackend != "nftables" {
		t.Errorf("got engine=%q backend=%q, want tun/nftables", out2.Engine, out2.FirewallBackend)
	}
}

// connections_seen is documented as a running total, so it must come from the
// counter that only ever grows. ActiveFlows happens to grow today only because
// CloseConnection has no callers; sourcing a documented total from it would
// turn into a silent lie the moment anyone wires that up.
func TestMCPConnectionsSeenIsNotAnInFlightGauge(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	readSeen := func() int64 {
		t.Helper()
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_status"})
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		var out mcpStatusOut
		if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out.ConnectionsSeen
	}

	m := GetMetricsCollector()
	m.RecordConnection("TCP", "example.com", "10.0.0.2", "1.2.3.4:443", true, "", "video", "1.3")
	before := readSeen()

	// Simulate the close accounting being wired up later.
	m.CloseConnection()

	if after := readSeen(); after < before {
		t.Errorf("connections_seen fell from %d to %d when a connection closed; it is a running total, not a gauge", before, after)
	}
}

func TestMCPMetricsReportsNoConcurrencyFigure(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_metrics"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var summary map[string]any
	if err := json.Unmarshal(mustStructured(t, res), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if _, ok := summary["connections_seen"]; !ok {
		t.Error("connections_seen should be reported")
	}
	// b4 cannot count connections open right now, so it must not appear to.
	for _, unwanted := range []string{"active_flows", "total_connections"} {
		if _, ok := summary[unwanted]; ok {
			t.Errorf("%q must not be reported: it duplicates connections_seen or implies a concurrency figure b4 does not have", unwanted)
		}
	}
}

func decodeTopic(t *testing.T, res *mcp.CallToolResult) mcpTopicOut {
	t.Helper()
	if res.IsError {
		t.Fatalf("b4_get_topic returned an error: %+v", res.Content)
	}
	var out mcpTopicOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode topic: %v", err)
	}
	return out
}

func callTopic(t *testing.T, session *mcp.ClientSession, ctx context.Context, args map[string]any) mcpTopicOut {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_get_topic", Arguments: args})
	if err != nil {
		t.Fatalf("call b4_get_topic %v: %v", args, err)
	}
	return decodeTopic(t, res)
}

func TestMCPGetTopicLookup(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	out := callTopic(t, session, ctx, map[string]any{"topic": "faking.tcp_md5"})
	if !out.Found || len(out.Topics) != 1 {
		t.Fatalf("exact lookup failed: %+v", out)
	}
	if out.Topics[0].Facts != ai.TopicFacts("faking.tcp_md5") {
		t.Error("a direct lookup must return the full body, not an excerpt")
	}
	if out.Documented != len(ai.TopicKeys()) {
		t.Errorf("documented_total = %d, want %d", out.Documented, len(ai.TopicKeys()))
	}
}

func TestMCPGetTopicAcceptsWritePath(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	for _, path := range []string{
		"sets[video].faking.tcp_md5",
		"sets[].faking.tcp_md5",
		"faking.tcp_md5",
		"FAKING.TCP_MD5",
	} {
		out := callTopic(t, session, ctx, map[string]any{"path": path})
		if !out.Found || len(out.Topics) != 1 || out.Topics[0].Topic != "faking.tcp_md5" {
			t.Errorf("path %q should resolve to faking.tcp_md5, got %+v", path, out)
		}
	}
}

func TestMCPGetTopicMissIsInstructive(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	out := callTopic(t, session, ctx, map[string]any{"path": "sets[video].tcp.win.values"})
	if out.Found {
		t.Fatal("tcp.win.values has no topic and must not report found")
	}
	// The miss is exactly where a model invents a unit, so it has to carry the instruction.
	for _, want := range []string{"Do NOT infer", "unsure"} {
		if !strings.Contains(out.Note, want) {
			t.Errorf("miss note must warn against guessing, got %q", out.Note)
		}
	}
	if len(out.Related) == 0 {
		t.Error("a miss should offer the documented settings nearby")
	}
	for _, r := range out.Related {
		if !strings.HasPrefix(r, "tcp.win.") {
			t.Errorf("related topic %q is not from the same section", r)
		}
	}
}

func TestMCPGetTopicListAndSearch(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	list := callTopic(t, session, ctx, map[string]any{})
	if len(list.Topics) != len(ai.TopicKeys()) {
		t.Fatalf("listing returned %d keys, want %d", len(list.Topics), len(ai.TopicKeys()))
	}
	for _, e := range list.Topics {
		if e.Facts != "" {
			t.Fatal("the listing must return keys only: bodies would blow up the result")
		}
	}

	hits := callTopic(t, session, ctx, map[string]any{"query": "ttl"})
	if len(hits.Topics) == 0 {
		t.Fatal("search for 'ttl' found nothing")
	}
	if len(hits.Topics) > mcpMaxTopicHits {
		t.Errorf("search returned %d hits, cap is %d", len(hits.Topics), mcpMaxTopicHits)
	}
	for _, e := range hits.Topics {
		if len(e.Facts) > mcpTopicExcerptLen+4 {
			t.Errorf("search body for %q was not shortened: %d chars", e.Topic, len(e.Facts))
		}
	}

	none := callTopic(t, session, ctx, map[string]any{"query": "zzzz-no-such-thing"})
	if none.Found || len(none.Topics) != 0 {
		t.Errorf("empty search should report nothing found: %+v", none)
	}
}

func TestMCPInstructionsPointAtTheTopicTool(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	got := session.InitializeResult().Instructions
	if !strings.Contains(got, "b4_get_topic") {
		t.Errorf("instructions must name the tool, since most clients hide resources: %q", got)
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "b4_get_topic" {
			continue
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Error("b4_get_topic must be annotated read-only")
		}
		return
	}
	t.Fatal("b4_get_topic not registered")
}

func TestWritablePathsGrounding(t *testing.T) {
	cfg := mcpTestCfg()
	documented := map[string]bool{}
	for _, k := range ai.TopicKeys() {
		documented[k] = true
	}

	grounded := 0
	missingByFamily := map[string][]string{}
	paths := mcpWritablePaths(cfg, cfg.Sets[0])
	for _, p := range paths {
		key := strings.TrimPrefix(p.Path, mcpSetPathPrefix+".")
		if documented[key] {
			grounded++
			continue
		}
		family := strings.SplitN(key, ".", 2)[0]
		missingByFamily[family] = append(missingByFamily[family], key)
	}

	// A ratchet, not a target: b4_get_topic tells the model to say it is unsure
	// when a setting has no entry, so partial coverage is safe. Losing coverage
	// silently is not.
	const groundedFloor = 84
	if grounded < groundedFloor {
		t.Errorf("grounding regressed: %d of %d writable settings documented, floor is %d; still missing %v",
			grounded, len(paths), groundedFloor, missingByFamily)
	}

	if corpus := len(ai.TopicKeys()); corpus < 96 {
		t.Errorf("the topics corpus shrank to %d entries; it should only ever grow", corpus)
	}

	// These families were backfilled completely. A new writable field in one of
	// them needs an entry in the same change.
	for _, family := range []string{"routing", "escalate", "dns", "targets", "udp", "mss_clamp"} {
		if missing := missingByFamily[family]; len(missing) > 0 {
			t.Errorf("%s.* is fully documented and must stay that way; %v has no b4://topics entry", family, missing)
		}
	}
}

func TestToolsListStaysWithinContextBudget(t *testing.T) {
	// Two budgets, because they protect different things. The default surface is
	// what a constrained client actually pays for on every request; the widest
	// one is a runaway guard for an operator who has opted into everything.
	for _, c := range []struct {
		name           string
		writes, probes bool
		budget         int
	}{
		{"default", false, false, 16000},
		{"writes and probes", true, true, 30000},
	} {
		cfg := mcpTestCfg()
		cfg.System.WebServer.MCP.AllowWrites = c.writes
		cfg.System.WebServer.MCP.AllowActiveProbes = c.probes
		srv := newMCPTestServer(t, cfg)
		session, ctx := connectMCP(t, srv)

		tools, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatalf("%s: list tools: %v", c.name, err)
		}

		total := 0
		widest, widestName := 0, ""
		for _, tool := range tools.Tools {
			raw, err := json.Marshal(tool)
			if err != nil {
				t.Fatalf("marshal %s: %v", tool.Name, err)
			}
			total += len(raw)
			if len(raw) > widest {
				widest, widestName = len(raw), tool.Name
			}
			if tool.Description == "" {
				t.Errorf("tool %q has no description", tool.Name)
			}
		}

		if total > c.budget {
			t.Errorf("%s surface is %d bytes across %d tools, over the %d budget (widest: %s at %d). "+
				"Trim a description, or register a tool family only when its subsystem is configured.",
				c.name, total, len(tools.Tools), c.budget, widestName, widest)
		}
		t.Logf("%s: %d tools, %d bytes, %d%% of budget", c.name, len(tools.Tools), total, total*100/c.budget)
	}
}

func toolNames(t *testing.T, session *mcp.ClientSession, ctx context.Context) map[string]bool {
	t.Helper()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	out := map[string]bool{}
	for _, tool := range tools.Tools {
		out[tool.Name] = true
	}
	return out
}

func TestToolsAreRegisteredByPermission(t *testing.T) {
	writeTools := []string{"b4_set_config_value", "b4_revert_last_change", "b4_edit_set_targets"}
	probeTools := []string{"b4_test_domain_now"}
	always := []string{"b4_status", "b4_get_topic", "b4_geo_lookup", "b4_watchdog"}

	cfg := mcpTestCfg()
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	got := toolNames(t, session, ctx)
	for _, name := range always {
		if !got[name] {
			t.Errorf("%s should always be served", name)
		}
	}
	for _, name := range append(append([]string{}, writeTools...), probeTools...) {
		if got[name] {
			t.Errorf("%s must not be advertised while its permission is off: a tool the model cannot use is pure context cost", name)
		}
	}

	full := mcpTestCfg()
	full.System.WebServer.MCP.AllowWrites = true
	full.System.WebServer.MCP.AllowActiveProbes = true
	fullSrv := newMCPTestServer(t, full)
	fullSession, fullCtx := connectMCP(t, fullSrv)

	got = toolNames(t, fullSession, fullCtx)
	for _, name := range append(append([]string{}, writeTools...), probeTools...) {
		if !got[name] {
			t.Errorf("%s should be served once its permission is on", name)
		}
	}
}

func TestToolRegistrationFollowsConfigWithoutRestart(t *testing.T) {
	cfg := mcpTestCfg()
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	if toolNames(t, session, ctx)["b4_edit_set_targets"] {
		t.Fatal("precondition: writes are off")
	}

	// b4's whole design is to apply changes live; the served tool list has to
	// follow the permission flags without restarting the daemon.
	next := api.getCfg().Clone()
	next.System.WebServer.MCP.AllowWrites = true
	api.cfgPtr.Store(next)

	if !toolNames(t, session, ctx)["b4_edit_set_targets"] {
		t.Error("turning writes on must expose the write tools without a restart")
	}

	back := api.getCfg().Clone()
	back.System.WebServer.MCP.AllowWrites = false
	api.cfgPtr.Store(back)

	if toolNames(t, session, ctx)["b4_edit_set_targets"] {
		t.Error("turning writes off again must withdraw them")
	}
}

func TestInstructionsDescribeTheServedSurface(t *testing.T) {
	cfg := mcpTestCfg()
	srv := newMCPTestServer(t, cfg)
	session, _ := connectMCP(t, srv)
	if got := session.InitializeResult().Instructions; !strings.Contains(got, "read-only") {
		t.Errorf("a read-only server should say so, so the model does not offer to change things: %q", got)
	}

	full := mcpTestCfg()
	full.System.WebServer.MCP.AllowWrites = true
	full.System.WebServer.MCP.AllowActiveProbes = true
	fullSrv := newMCPTestServer(t, full)
	fullSession, _ := connectMCP(t, fullSrv)
	if got := fullSession.InitializeResult().Instructions; strings.Contains(got, "read-only") {
		t.Errorf("a writable server must not claim to be read-only: %q", got)
	}
}

// mcpFindBoolSchema reports a path where a NAMED property or item schema is the
// boolean form. additionalProperties:false is idiomatic and accepted, but a
// boolean where a client expects a property schema is not: LM Studio rejects the
// whole tool list over one, and jsonschema-go emits exactly that for interface{}.
func mcpFindBoolSchema(node any, path string) string {
	switch v := node.(type) {
	case bool:
		return path
	case map[string]any:
		for _, key := range []string{"properties", "$defs", "definitions", "patternProperties"} {
			sub, ok := v[key].(map[string]any)
			if !ok {
				continue
			}
			for name, child := range sub {
				if found := mcpFindBoolSchema(child, path+"."+key+"."+name); found != "" {
					return found
				}
			}
		}
		for _, key := range []string{"items", "not", "if", "then", "else"} {
			if child, ok := v[key]; ok {
				if found := mcpFindBoolSchema(child, path+"."+key); found != "" {
					return found
				}
			}
		}
		for _, key := range []string{"anyOf", "allOf", "oneOf", "prefixItems"} {
			list, ok := v[key].([]any)
			if !ok {
				continue
			}
			for i, child := range list {
				if found := mcpFindBoolSchema(child, fmt.Sprintf("%s.%s[%d]", path, key, i)); found != "" {
					return found
				}
			}
		}
	}
	return ""
}

func TestToolSchemasAreObjectsNotBooleans(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.System.WebServer.MCP.AllowActiveProbes = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	for _, tool := range tools.Tools {
		for label, schema := range map[string]any{
			"inputSchema":  tool.InputSchema,
			"outputSchema": tool.OutputSchema,
		} {
			if schema == nil {
				continue
			}
			raw, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("%s %s: marshal: %v", tool.Name, label, err)
			}
			var decoded any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("%s %s: decode: %v", tool.Name, label, err)
			}
			if at := mcpFindBoolSchema(decoded, label); at != "" {
				t.Errorf("%s publishes a boolean sub-schema at %s. An `any`-typed field produces one, "+
					"and a strict client rejects the entire tool list over it — give the field a concrete type, "+
					"or drop the schema by typing the handler's Out as `any`.", tool.Name, at)
			}
		}
	}
}

func TestInstructionsNameTheGatedCapabilities(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	if toolNames(t, session, ctx)["b4_find_bypass_strategy"] {
		t.Fatal("precondition: probes are off, so discovery is not served")
	}

	instr := session.InitializeResult().Instructions
	for _, want := range []string{"discovery", "Allow active probes", "do not conclude the feature does not exist"} {
		if !strings.Contains(instr, want) {
			t.Errorf("instructions must mention %q so an unavailable capability is reported as gated, not absent: %q", want, instr)
		}
	}
}

func TestGetTopicFindsDiscovery(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	hits := callTopic(t, session, ctx, map[string]any{"query": "discovery"})
	if !hits.Found {
		t.Fatal("a search for 'discovery' must find something: it is a real b4 feature")
	}
	exact := callTopic(t, session, ctx, map[string]any{"topic": "discovery.pipeline"})
	if !exact.Found {
		t.Fatal("discovery.pipeline should be documented")
	}
	for _, want := range []string{"baseline_works", "transport_blocked", "b4_find_bypass_strategy"} {
		if !strings.Contains(exact.Topics[0].Facts, want) {
			t.Errorf("the discovery topic should cover %q", want)
		}
	}
}
