package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callWatchdog(t *testing.T, session *mcp.ClientSession, ctx context.Context, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_watchdog", Arguments: args})
	if err != nil {
		t.Fatalf("call b4_watchdog %v: %v", args, err)
	}
	return res
}

func decodeWatchdog(t *testing.T, res *mcp.CallToolResult) mcpWatchdogOut {
	t.Helper()
	if res.IsError {
		t.Fatalf("b4_watchdog returned an error: %+v", res.Content)
	}
	var out mcpWatchdogOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestMCPProbeRequiresItsOwnGate(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	if cfg.System.WebServer.MCP.AllowActiveProbes {
		t.Fatal("active probes must default to disabled")
	}
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	// allow_writes is on here: probing is a separate permission, because
	// emitting traffic from the router is a different act from writing config.
	if toolNames(t, session, ctx)["b4_test_domain_now"] {
		t.Fatal("probing must not be offered until 'Allow active probes' is on")
	}
	if instr := session.InitializeResult().Instructions; !strings.Contains(instr, "Allow active probes") {
		t.Errorf("instructions must name the setting that unlocks probing: %q", instr)
	}
}

func TestMCPProbeValidatesInputBeforeEmittingTraffic(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowActiveProbes = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	for name, args := range map[string]map[string]any{
		"no domain":     {"domain": "  "},
		"too many":      {"domain": "a.com,b.com,c.com,d.com"},
		"unknown mode":  {"domain": "a.com", "mode": "sideways"},
		"private host":  {"domain": "192.168.1.1"},
		"router itself": {"domain": "http://127.0.0.1/admin"},
	} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "b4_test_domain_now", Arguments: args,
		})
		if err != nil {
			t.Fatalf("%s: call: %v", name, err)
		}
		if !res.IsError {
			t.Errorf("%s should be refused", name)
		}
	}
}

func TestMCPProbeIsAnnotatedOpenWorld(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowActiveProbes = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "b4_test_domain_now" {
			continue
		}
		a := tool.Annotations
		if a == nil || a.ReadOnlyHint {
			t.Error("a tool that emits traffic is not read-only")
		}
		if a == nil || a.OpenWorldHint == nil || !*a.OpenWorldHint {
			t.Error("b4_test_domain_now reaches the internet and must say so")
		}
		return
	}
	t.Fatal("b4_test_domain_now not registered")
}

func TestMCPProbeVerdictNote(t *testing.T) {
	ok := &mcpProbeResult{OK: true}
	bad := &mcpProbeResult{OK: false, Verdict: "TLS_DPI"}

	cases := []struct {
		name             string
		through, base    *mcpProbeResult
		wants, wantsNots []string
	}{
		{
			name: "bypass working", through: ok, base: bad,
			wants: []string{"censored", "bypass is working"},
		},
		{
			name: "nothing blocked", through: ok, base: ok,
			wants: []string{"either way", "no set is needed"},
		},
		{
			name: "b4 is the problem", through: bad, base: ok,
			wants: []string{"FAILS through b4", "b4 setting is breaking it"},
		},
		{
			name: "beyond a packet strategy", through: bad, base: bad,
			wants: []string{"fails both ways", "not something a packet strategy fixes"},
		},
	}
	for _, tc := range cases {
		got := mcpProbeVerdictNote("example.com", tc.through, tc.base)
		for _, want := range tc.wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s: note %q should mention %q", tc.name, got, want)
			}
		}
	}

	if got := mcpProbeVerdictNote("example.com", nil, nil); got != "" {
		t.Errorf("no results should produce no note, got %q", got)
	}
}

func TestMCPWatchdogStatusWithoutRunningWatchdog(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.Checker.Watchdog.Domains = []string{"rutracker.org"}
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeWatchdog(t, callWatchdog(t, session, ctx, map[string]any{"action": "status"}))
	if len(out.Domains) != 0 {
		t.Errorf("no verdicts exist when the watchdog is not running: %+v", out.Domains)
	}
	if !strings.Contains(out.Note, "not running") {
		t.Errorf("the note must explain the empty result: %q", out.Note)
	}
	if !strings.Contains(out.Note, "1 domain") {
		t.Errorf("the configured domains should still be reported: %q", out.Note)
	}
}

func TestMCPWatchdogStatusNeedsNoWriteGate(t *testing.T) {
	cfg := mcpTestCfg()
	if cfg.System.WebServer.MCP.AllowWrites {
		t.Fatal("precondition: writes are off")
	}
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	if res := callWatchdog(t, session, ctx, map[string]any{"action": "status"}); res.IsError {
		t.Errorf("reading the watchdog state changes nothing and must not need the write gate: %+v", res.Content)
	}
}

func TestMCPWatchdogEditsRequireWriteGate(t *testing.T) {
	cfg := mcpTestCfg()
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	for _, args := range []map[string]any{
		{"action": "enable"},
		{"action": "add", "domain": "rutracker.org"},
	} {
		if res := callWatchdog(t, session, ctx, args); !res.IsError {
			t.Errorf("%v must be refused while writes are disabled", args)
		}
	}
	if api.getCfg().System.Checker.Watchdog.Enabled {
		t.Error("config must be untouched")
	}
}

func TestMCPWatchdogAddRemoveAndToggle(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	add := decodeWatchdog(t, callWatchdog(t, session, ctx, map[string]any{
		"action": "add", "domain": "https://rutracker.org/forum/",
	}))
	if !add.Changed {
		t.Fatalf("add should change the config: %+v", add)
	}
	// A URL is reduced to its host, so the stored list stays comparable.
	if got := api.getCfg().System.Checker.Watchdog.Domains; len(got) != 1 || got[0] != "rutracker.org" {
		t.Fatalf("stored domains = %v", got)
	}

	again := decodeWatchdog(t, callWatchdog(t, session, ctx, map[string]any{
		"action": "add", "domain": "rutracker.org",
	}))
	if again.Changed || !strings.Contains(again.Note, "already watched") {
		t.Errorf("a duplicate add should be a no-op: %+v", again)
	}

	on := decodeWatchdog(t, callWatchdog(t, session, ctx, map[string]any{"action": "enable"}))
	if !on.Enabled || !on.Changed {
		t.Fatalf("enable failed: %+v", on)
	}
	if !strings.Contains(on.Note, "rewrite") {
		t.Errorf("enabling grants b4 permission to rewrite sets and must say so: %q", on.Note)
	}

	rm := decodeWatchdog(t, callWatchdog(t, session, ctx, map[string]any{
		"action": "remove", "domain": "rutracker.org",
	}))
	if !rm.Changed {
		t.Fatalf("remove failed: %+v", rm)
	}
	if got := api.getCfg().System.Checker.Watchdog.Domains; len(got) != 0 {
		t.Errorf("domains after removal = %v", got)
	}

	missing := decodeWatchdog(t, callWatchdog(t, session, ctx, map[string]any{
		"action": "remove", "domain": "nothere.test",
	}))
	if missing.Changed || !strings.Contains(missing.Note, "not watched") {
		t.Errorf("removing an absent domain should be a no-op: %+v", missing)
	}

	if len(mcpHistory) == 0 {
		t.Error("watchdog edits must be undoable")
	}
}

func TestMCPWatchdogRevertRestoresList(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	decodeWatchdog(t, callWatchdog(t, session, ctx, map[string]any{
		"action": "add", "domain": "rutracker.org",
	}))
	if len(api.getCfg().System.Checker.Watchdog.Domains) != 1 {
		t.Fatal("precondition: the add should have landed")
	}

	if rev := decodeRevert(t, session, ctx); !rev.Reverted {
		t.Fatalf("a watchdog edit must be undoable: %+v", rev)
	}
	if got := api.getCfg().System.Checker.Watchdog.Domains; len(got) != 0 {
		t.Errorf("domains after revert = %v", got)
	}
}

func TestMCPWatchdogRejectsBadInput(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	for name, args := range map[string]map[string]any{
		"no action":      {},
		"unknown action": {"action": "obliterate"},
		"add no domain":  {"action": "add"},
		"check no wd":    {"action": "check", "domain": "a.com"},
	} {
		if res := callWatchdog(t, session, ctx, args); !res.IsError {
			t.Errorf("%s should be refused", name)
		}
	}
}
