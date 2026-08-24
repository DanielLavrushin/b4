package handler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func probeCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowActiveProbes = true
	return cfg
}

func callDiscovery(t *testing.T, session *mcp.ClientSession, ctx context.Context, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_find_bypass_strategy", Arguments: args})
	if err != nil {
		t.Fatalf("call b4_find_bypass_strategy %v: %v", args, err)
	}
	return res
}

func decodeDiscovery(t *testing.T, res *mcp.CallToolResult) mcpDiscoveryOut {
	t.Helper()
	if res.IsError {
		t.Fatalf("b4_find_bypass_strategy returned an error: %+v", res.Content)
	}
	var out mcpDiscoveryOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestMCPDiscoveryNeedsProbePermission(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	if toolNames(t, session, ctx)["b4_find_bypass_strategy"] {
		t.Fatal("a discovery run is the heaviest probe b4 has and must not be offered until active probes are permitted")
	}
}

func TestMCPDiscoveryStartValidatesInput(t *testing.T) {
	srv := newMCPTestServer(t, probeCfg(t))
	session, ctx := connectMCP(t, srv)

	for name, args := range map[string]map[string]any{
		"no domains":     {"action": "start", "domains": "  "},
		"too many":       {"action": "start", "domains": "a.com,b.com,c.com,d.com,e.com,f.com"},
		"no action":      {},
		"unknown action": {"action": "obliterate"},
	} {
		if res := callDiscovery(t, session, ctx, args); !res.IsError {
			t.Errorf("%s should be refused", name)
		}
	}
}

func TestMCPDiscoveryStartNeedsARuntime(t *testing.T) {
	srv := newMCPTestServer(t, probeCfg(t))
	session, ctx := connectMCP(t, srv)

	res := callDiscovery(t, session, ctx, map[string]any{"action": "start", "domains": "example.com"})
	if !res.IsError {
		t.Fatal("starting without a discovery runtime must be refused, not silently do nothing")
	}
	if !strings.Contains(mcpErrorText(res), "runtime") {
		t.Errorf("the refusal should say what is missing: %q", mcpErrorText(res))
	}
}

func TestMCPDiscoveryStatusFallsBackToHistory(t *testing.T) {
	dir := t.TempDir()
	cfg := probeCfg(t)
	cfg.ConfigPath = filepath.Join(dir, "config.json")

	history := `{"entries":[
	  {"domain":"rutracker.org","best_preset":"combo-duplicate","best_family":"combo","best_speed":204800,"best_success":true,"status":"complete","confirmed":3},
	  {"domain":"youtube.com","best_preset":"no-bypass","best_success":true,"baseline_works":true,"status":"complete"},
	  {"domain":"blocked.example","best_success":false,"status":"complete"}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "discovery_history.json"), []byte(history), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status", "id": "long-gone"}))
	if out.Source != "history" {
		t.Fatalf("a suite that has left memory must fall back to history, got source %q", out.Source)
	}
	if len(out.Domains) != 3 {
		t.Fatalf("expected 3 history rows, got %+v", out.Domains)
	}

	byDomain := map[string]mcpDiscoveryDomain{}
	for _, d := range out.Domains {
		byDomain[d.Domain] = d
	}

	if got := byDomain["youtube.com"]; !got.BaselineWorks || got.Found {
		t.Errorf("youtube.com works without b4 and must not read as a strategy to apply: %+v", got)
	}
	if !strings.Contains(byDomain["youtube.com"].Verdict, "do not create a set") {
		t.Errorf("verdict should say so plainly: %q", byDomain["youtube.com"].Verdict)
	}
	if got := byDomain["rutracker.org"]; !got.Found || got.Family != "combo" {
		t.Errorf("rutracker.org has a usable strategy: %+v", got)
	}
	if got := byDomain["blocked.example"]; got.Found {
		t.Errorf("a run that found nothing must not read as found: %+v", got)
	}

	if !strings.Contains(out.Note, "30 seconds") {
		t.Errorf("the note must explain why history cannot be applied: %q", out.Note)
	}
}

func TestMCPDiscoveryStatusWithNothingAtAll(t *testing.T) {
	cfg := probeCfg(t)
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status"}))
	if len(out.Domains) != 0 {
		t.Errorf("nothing has been discovered: %+v", out.Domains)
	}
	if !strings.Contains(out.Note, "nothing has been discovered") {
		t.Errorf("the empty case should explain itself: %q", out.Note)
	}
}

func TestMCPDiscoveryCancelWithNoRun(t *testing.T) {
	srv := newMCPTestServer(t, probeCfg(t))
	session, ctx := connectMCP(t, srv)

	res := callDiscovery(t, session, ctx, map[string]any{"action": "cancel"})
	if !res.IsError {
		t.Fatal("cancelling with no run in progress should say so, not report success")
	}
}

func TestMCPDiscoveryApplyNeedsWritePermission(t *testing.T) {
	cfg := probeCfg(t)
	if cfg.System.WebServer.MCP.AllowWrites {
		t.Fatal("precondition: writes are off")
	}
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callDiscovery(t, session, ctx, map[string]any{"action": "apply", "domain": "rutracker.org"})
	if !res.IsError {
		t.Fatal("apply creates a set and must need the write permission, not just the probe one")
	}
	if !strings.Contains(mcpErrorText(res), "Allow configuration changes") {
		t.Errorf("the refusal should name the setting: %q", mcpErrorText(res))
	}
}

func TestMCPDiscoveryApplyWithoutALiveRun(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callDiscovery(t, session, ctx, map[string]any{
		"action": "apply", "domain": "rutracker.org", "id": "not-in-memory",
	})
	if !res.IsError {
		t.Fatal("applying a run that has left memory must be refused")
	}
	msg := mcpErrorText(res)
	if !strings.Contains(msg, "not saved to history") {
		t.Errorf("the refusal must explain why, so the model re-runs instead of retrying: %q", msg)
	}
	if len(api.getCfg().Sets) != 2 {
		t.Error("no set should have been created")
	}
}

func TestMCPDiscoveryVerdicts(t *testing.T) {
	cases := []struct {
		name string
		row  mcpDiscoveryDomain
		want string
	}{
		{"baseline works", mcpDiscoveryDomain{BaselineWorks: true}, "do not create a set"},
		{"transport blocked", mcpDiscoveryDomain{Blocked: true}, "no packet strategy can help"},
		{"found", mcpDiscoveryDomain{Found: true, BestPreset: "combo"}, "working strategy was found"},
		{"nothing worked", mcpDiscoveryDomain{}, "no strategy tried made it work"},
	}
	for _, tc := range cases {
		if got := mcpDiscoveryVerdict(tc.row); !strings.Contains(got, tc.want) {
			t.Errorf("%s: verdict %q should mention %q", tc.name, got, tc.want)
		}
	}

	both := mcpDiscoveryDomain{BaselineWorks: true, Found: true, Blocked: true}
	if !strings.Contains(mcpDiscoveryVerdict(both), "do not create a set") {
		t.Error("works-without-b4 must win over every other verdict")
	}
}

func TestMCPDiscoveryIsAnnotatedOpenWorld(t *testing.T) {
	srv := newMCPTestServer(t, probeCfg(t))
	session, ctx := connectMCP(t, srv)

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "b4_find_bypass_strategy" {
			continue
		}
		a := tool.Annotations
		if a == nil || a.ReadOnlyHint {
			t.Error("a discovery run is not read-only")
		}
		if a == nil || a.OpenWorldHint == nil || !*a.OpenWorldHint {
			t.Error("a discovery run reaches the internet and must say so")
		}
		if !strings.Contains(tool.Description, "do not poll in a loop") {
			t.Error("the description must tell the model not to busy-poll a job that takes minutes")
		}
		return
	}
	t.Fatal("b4_find_bypass_strategy not registered")
}

func TestMCPDiscoveryResolvesAFinishedRun(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	set := config.NewSetConfig()
	set.Name = "found"
	set.Targets.SNIDomains = []string{"rutracker.org"}
	suite := &discovery.CheckSuite{
		Id:     "finished-run",
		Status: discovery.CheckStatusComplete,
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"rutracker.org": {Domain: "rutracker.org", BestPreset: "combo", BestSuccess: true},
		},
		StrategyGroups: []discovery.StrategyGroup{
			{WinnerPreset: "combo", Family: "combo", Domains: []string{"rutracker.org"}, Set: &set},
		},
	}
	discovery.RegisterSuite(suite)
	mcpRememberSuite(suite.Id)
	t.Cleanup(func() { mcpRememberSuite("") })

	out := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status"}))
	if out.Source != "run" || out.Id != "finished-run" {
		t.Fatalf("status with no id must find the finished run, got %+v", out)
	}

	applied := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{
		"action": "apply", "domain": "rutracker.org",
	}))
	if !applied.Changed || applied.Applied == nil {
		t.Fatalf("apply with no id must reach the finished run: %+v", applied)
	}
	if applied.Applied.Position != len(api.getCfg().Sets) {
		t.Errorf("a discovered set should land last, at %d of %d", applied.Applied.Position, len(api.getCfg().Sets))
	}
	if !strings.Contains(applied.Note, "b4_test_domain_now") {
		t.Errorf("the note should send the model to verify it: %q", applied.Note)
	}
}

func TestMCPDiscoveryApplyRefusesWorksWithoutB4(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	suite := &discovery.CheckSuite{
		Id:     "baseline-run",
		Status: discovery.CheckStatusComplete,
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"youtube.com": {Domain: "youtube.com", BestPreset: "no-bypass", BestSuccess: true, BaselineWorks: true},
		},
	}
	discovery.RegisterSuite(suite)

	res := callDiscovery(t, session, ctx, map[string]any{
		"action": "apply", "domain": "youtube.com", "id": "baseline-run",
	})
	if !res.IsError {
		t.Fatal("a domain that works without b4 must not get a set")
	}
	if !strings.Contains(mcpErrorText(res), "works without b4") {
		t.Errorf("the refusal should say why: %q", mcpErrorText(res))
	}
	if len(api.getCfg().Sets) != 2 {
		t.Error("no set should have been created")
	}
}
