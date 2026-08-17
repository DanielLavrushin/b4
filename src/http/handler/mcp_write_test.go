package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callSetValue(t *testing.T, session *mcp.ClientSession, ctx context.Context, path, value string) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_set_config_value",
		Arguments: map[string]any{"path": path, "value": value},
	})
	if err != nil {
		t.Fatalf("call %s=%s: %v", path, value, err)
	}
	return res
}

func decodeSetValue(t *testing.T, res *mcp.CallToolResult) mcpSetValueOut {
	t.Helper()
	var out mcpSetValueOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestMCPWriteDisabledByDefault(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.MTProto.Enabled = true
	if cfg.System.WebServer.MCP.AllowWrites {
		t.Fatal("writes must default to disabled")
	}
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callSetValue(t, session, ctx, "system.mtproto.enabled", "false")
	if !res.IsError {
		t.Fatal("write must be refused when allow_writes is false")
	}
	if !cfg.System.MTProto.Enabled {
		t.Fatal("config must be untouched when writes are disabled")
	}
}

func TestMCPWriteToolIsAnnotatedDestructive(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "b4_set_config_value" {
			continue
		}
		a := tool.Annotations
		if a == nil || a.ReadOnlyHint {
			t.Fatal("write tool must not be annotated read-only")
		}
		if a.DestructiveHint == nil || !*a.DestructiveHint {
			t.Fatal("write tool must carry destructiveHint so hosts prompt for approval")
		}
		for _, want := range []string{"system.mtproto.enabled", "sets[].fragmentation.strategy", "allow_writes"} {
			if !strings.Contains(tool.Description, want) {
				t.Errorf("description should mention %q", want)
			}
		}
		return
	}
	t.Fatal("b4_set_config_value not registered")
}

func TestMCPWriteSystemToggle(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.System.MTProto.Enabled = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callSetValue(t, session, ctx, "system.mtproto.enabled", "false")
	if res.IsError {
		t.Fatalf("write failed: %+v", res.Content)
	}
	out := decodeSetValue(t, res)
	if out.Previous != "true" || out.Current != "false" || !out.Changed {
		t.Fatalf("diff = %+v", out)
	}

	again := decodeSetValue(t, callSetValue(t, session, ctx, "system.mtproto.enabled", "false"))
	if again.Changed {
		t.Error("second identical write should report Changed=false")
	}
	if again.Note == "" {
		t.Error("no-op write should explain itself")
	}
}

func TestMCPWritePerSetPaths(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].enabled", "false"))
	if out.Previous != "true" || out.Current != "false" {
		t.Fatalf("diff = %+v", out)
	}

	strat := decodeSetValue(t, callSetValue(t, session, ctx, "sets[set-1].fragmentation.strategy", "EXTSPLIT"))
	if strat.Current != "extsplit" {
		t.Fatalf("enum should normalise case, got %q", strat.Current)
	}

	if res := callSetValue(t, session, ctx, "sets[nope].enabled", "false"); !res.IsError {
		t.Error("unknown set must be rejected")
	}
	if res := callSetValue(t, session, ctx, "sets[video].fragmentation.strategy", "banana"); !res.IsError {
		t.Error("value outside the enum must be rejected")
	}
}

func TestMCPWriteRejectsNonAllowlistedPaths(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	forbidden := []string{
		"system.web_server.password",
		"system.web_server.username",
		"system.web_server.port",
		"system.socks5.password",
		"system.mtproto.secrets",
		"system.tables.skip_setup",
		"system.ai.api_key_ref",
		"system.logging.directory",
		"system.web_server.mcp.allow_writes",
		"",
		"sets[video].targets.sni_domains",
	}
	for _, path := range forbidden {
		res := callSetValue(t, session, ctx, path, "true")
		if !res.IsError {
			t.Errorf("path %q must not be writable", path)
		}
	}

	if cfg.System.WebServer.Password != "hashed-secret" || cfg.System.Socks5.Password != "socks-pw" {
		t.Fatal("credentials were mutated by a rejected write")
	}
}

func TestMCPParseWritePath(t *testing.T) {
	cases := []struct {
		in      string
		key     string
		setRef  string
		wantErr bool
	}{
		{in: "system.mtproto.enabled", key: "system.mtproto.enabled"},
		{in: "sets[video].enabled", key: "sets[].enabled", setRef: "video"},
		{in: " sets[ set-1 ].fragmentation.strategy ", key: "sets[].fragmentation.strategy", setRef: "set-1"},
		{in: "sets[].enabled", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tc := range cases {
		key, ref, err := parseMCPWritePath(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parse(%q) should fail", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parse(%q): %v", tc.in, err)
			continue
		}
		if key != tc.key || ref != tc.setRef {
			t.Errorf("parse(%q) = (%q,%q), want (%q,%q)", tc.in, key, ref, tc.key, tc.setRef)
		}
	}
}

func TestMCPWriteAllowlistHasNoCredentialPaths(t *testing.T) {
	banned := []string{"password", "secret", "username", "token", "key", "skip_setup", "tls"}
	for _, path := range mcpWritablePathList() {
		lower := strings.ToLower(path)
		for _, b := range banned {
			if strings.Contains(lower, b) {
				t.Errorf("allowlist entry %q looks credential-adjacent (matched %q)", path, b)
			}
		}
	}
}

func TestMCPWriteRefreshesFirewallWhenSetPortsChange(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	// A set whose ports only b4 knows about: enabling it changes the port list
	// the firewall has to intercept.
	cfg.Sets[1].TCP.DPortFilter = "8443"

	refreshed := 0
	old := tablesRefreshFunc
	tablesRefreshFunc = func() error { refreshed++; return nil }
	t.Cleanup(func() { tablesRefreshFunc = old })

	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[disabled-set].enabled", "true"))
	if !out.Changed {
		t.Fatalf("write did not take: %+v", out)
	}
	if refreshed != 1 {
		t.Fatalf("firewall refreshed %d times, want 1: enabling a set with its own ports must reach the firewall", refreshed)
	}
	if !strings.Contains(out.Note, "firewall") {
		t.Errorf("note should say the firewall was refreshed, got %q", out.Note)
	}
}

func TestMCPWriteSkipsFirewallRefreshWhenNothingRelevantChanged(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true

	refreshed := 0
	old := tablesRefreshFunc
	tablesRefreshFunc = func() error { refreshed++; return nil }
	t.Cleanup(func() { tablesRefreshFunc = old })

	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].fragmentation.strategy", "tcp"))
	if !out.Changed {
		t.Fatalf("write did not take: %+v", out)
	}
	if refreshed != 0 {
		t.Errorf("firewall refreshed %d times, want 0: a strategy change does not alter the rules", refreshed)
	}
}

func TestMCPWriteRejectsEnablingMTProtoWithoutSecretOrFakeSNI(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.System.MTProto.Enabled = false
	cfg.System.MTProto.Secrets = nil
	cfg.System.MTProto.FakeSNI = ""

	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callSetValue(t, session, ctx, "system.mtproto.enabled", "true")
	if !res.IsError {
		t.Fatal("enabling MTProto with no secret and no fake SNI must be refused, as the REST API does")
	}
	if cfg.System.MTProto.Enabled {
		t.Error("config must be untouched when the precondition fails")
	}
}

func TestMCPWriteAllowsEnablingMTProtoWithFakeSNI(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.System.MTProto.Enabled = false
	cfg.System.MTProto.Secrets = nil
	cfg.System.MTProto.FakeSNI = "www.google.com"

	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callSetValue(t, session, ctx, "system.mtproto.enabled", "true")
	if res.IsError {
		t.Fatalf("a fake SNI satisfies the precondition: %+v", res.Content)
	}
}
