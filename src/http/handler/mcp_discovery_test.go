package handler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	if !strings.Contains(out.Note, "saved history") {
		t.Errorf("the note must say the rows come from saved history: %q", out.Note)
	}
	if strings.Contains(out.Note, "30 seconds") {
		t.Errorf("history has no expiry, so the note must not claim one: %q", out.Note)
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
		t.Fatal("with nothing in memory and nothing saved, apply must be refused")
	}
	msg := mcpErrorText(res)
	if !strings.Contains(msg, "nothing saved") {
		t.Errorf("the refusal must say the domain has no saved result either: %q", msg)
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
	if applied.Applied.Position != 1 {
		t.Errorf("a discovered set must land first so it is not shadowed by a set already matching the domain, got %d of %d",
			applied.Applied.Position, len(api.getCfg().Sets))
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

func TestMCPDiscoveryStatusDropsStalePhaseWhenFinished(t *testing.T) {
	cfg := probeCfg(t)
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	suite := &discovery.CheckSuite{
		Id:           "phase-run",
		Status:       discovery.CheckStatusComplete,
		CurrentPhase: discovery.DiscoveryPhase("strategy_detection"),
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"youtube.com": {Domain: "youtube.com", BestPreset: "no-bypass", BestSuccess: true, BaselineWorks: true},
		},
	}
	discovery.RegisterSuite(suite)

	out := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status", "id": "phase-run"}))
	if out.Phase != "" {
		t.Errorf("a finished run must not report the phase it stopped in: %q alongside status %q reads as still working", out.Phase, out.Status)
	}
	if len(out.Domains) != 1 || !strings.Contains(out.Domains[0].Verdict, "do not create a set") {
		t.Fatalf("baseline verdict missing: %+v", out.Domains)
	}
}

func TestMCPDiscoveryApplyCoversAGroupOnce(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	set := config.NewSetConfig()
	set.Name = "found"
	set.Targets.SNIDomains = []string{"rutracker.org", "nnmclub.to"}
	suite := &discovery.CheckSuite{
		Id:     "group-run",
		Status: discovery.CheckStatusComplete,
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"rutracker.org": {Domain: "rutracker.org", BestPreset: "combo", BestSuccess: true},
			"nnmclub.to":    {Domain: "nnmclub.to", BestPreset: "combo", BestSuccess: true},
		},
		StrategyGroups: []discovery.StrategyGroup{
			{WinnerPreset: "combo", Family: "combo", Domains: []string{"nnmclub.to", "rutracker.org"}, Set: &set},
		},
	}
	discovery.RegisterSuite(suite)
	mcpRememberSuite(suite.Id)
	t.Cleanup(func() { mcpRememberSuite("") })

	before := len(api.getCfg().Sets)
	first := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{
		"action": "apply", "domain": "rutracker.org",
	}))
	if !first.Changed || first.Applied == nil {
		t.Fatalf("the first apply should create the set: %+v", first)
	}
	if !strings.Contains(first.Note, "nnmclub.to") {
		t.Errorf("one strategy won for both domains, so the note must say the set already covers them: %q", first.Note)
	}

	second := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{
		"action": "apply", "domain": "nnmclub.to",
	}))
	if second.Changed {
		t.Errorf("the second domain of the same group is already covered; applying again only duplicates the set: %+v", second)
	}
	if got := len(api.getCfg().Sets); got != before+1 {
		t.Fatalf("applying one group twice must not create two sets, got %d sets from %d", got, before)
	}

	live := api.getCfg().Sets
	var created *config.SetConfig
	for _, s := range live {
		if s.Id == first.Applied.Id {
			created = s
		}
	}
	if created == nil {
		t.Fatal("the created set disappeared")
	}
	if len(created.Targets.SNIDomains) != 2 {
		t.Errorf("the second apply must not strip the first set's domains, got %v", created.Targets.SNIDomains)
	}
}

func TestMCPDiscoveryStartRefusesPrivateHosts(t *testing.T) {
	cfg := probeCfg(t)
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	for _, domains := range []string{"192.168.1.50", "169.254.169.254", "rutracker.org,127.0.0.1"} {
		res := callDiscovery(t, session, ctx, map[string]any{"action": "start", "domains": domains})
		if !res.IsError {
			t.Errorf("%q aims hundreds of fetches at the network b4 runs on and must be refused", domains)
		}
	}
}

func TestMCPDiscoveryApplyRefusesAnUnfinishedRun(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	set := config.NewSetConfig()
	set.Name = "partial"
	set.Targets.SNIDomains = []string{"rutracker.org"}
	suite := &discovery.CheckSuite{
		Id:     "cancelled-run",
		Status: discovery.CheckStatusCanceled,
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

	before := len(api.getCfg().Sets)
	res := callDiscovery(t, session, ctx, map[string]any{"action": "apply", "domain": "rutracker.org"})
	if !res.IsError {
		t.Fatal("a cancelled run never confirmed its winner, so applying it would write an untested set")
	}
	if got := len(api.getCfg().Sets); got != before {
		t.Errorf("a refused apply must not add a set, got %d from %d", got, before)
	}
}

func TestMCPDiscoveryAppliesAfterTheRunLeavesMemory(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	set := config.NewSetConfig()
	set.Name = "meduza"
	set.Targets.SNIDomains = []string{"meduza.io"}
	suite := &discovery.CheckSuite{
		Id:      "009ec414-504e-4cdf-ba01-3b835c660dc7",
		Status:  discovery.CheckStatusComplete,
		EndTime: time.Now(),
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"meduza.io": {
				Domain: "meduza.io", BestPreset: "cached-11-fake_sni",
				BestSuccess: true, BestSpeed: 1320266, Confirmed: 3,
				Results: map[string]*discovery.DomainPresetResult{
					"cached-11-fake_sni": {PresetName: "cached-11-fake_sni", Family: "desync", Set: &set},
				},
			},
		},
		StrategyGroups: []discovery.StrategyGroup{
			{WinnerPreset: "cached-11-fake_sni", Family: "desync", Domains: []string{"meduza.io"}, Set: &set},
		},
	}
	discovery.SaveToHistory(suite, cfg.ConfigPath)

	mcpRememberSuite(suite.Id)
	t.Cleanup(func() { mcpRememberSuite("") })

	status := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{
		"action": "status", "id": suite.Id,
	}))
	if status.Source != "history" {
		t.Fatalf("precondition: the suite is not registered, so status must read history: %+v", status)
	}
	if len(status.Domains) != 1 || status.Domains[0].Domain != "meduza.io" {
		t.Fatalf("the suite_id should narrow history to that run: %+v", status.Domains)
	}
	if !status.Domains[0].Found {
		t.Fatalf("the saved run found a strategy: %+v", status.Domains[0])
	}

	before := len(api.getCfg().Sets)
	applied := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{
		"action": "apply", "domain": "meduza.io", "id": suite.Id,
	}))
	if !applied.Changed || applied.Applied == nil {
		t.Fatalf("a saved run holds the set it built, so apply must work after the run leaves memory: %+v", applied)
	}
	if applied.Source != "history" {
		t.Errorf("the result should say where the strategy came from, got %q", applied.Source)
	}
	if got := len(api.getCfg().Sets); got != before+1 {
		t.Fatalf("expected one new set, got %d from %d", got, before)
	}

	var created *config.SetConfig
	for _, s := range api.getCfg().Sets {
		if s.Id == applied.Applied.Id {
			created = s
		}
	}
	if created == nil || len(created.Targets.SNIDomains) != 1 || created.Targets.SNIDomains[0] != "meduza.io" {
		t.Errorf("the applied set must target the domain it was discovered for: %+v", created)
	}
}

func TestMCPDiscoveryAppliesFromHistoryByDomainAlone(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	set := config.NewSetConfig()
	set.Name = "meduza"
	set.Targets.SNIDomains = []string{"meduza.io"}
	discovery.SaveToHistory(&discovery.CheckSuite{
		Id: "older-run", Status: discovery.CheckStatusComplete, EndTime: time.Now(),
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"meduza.io": {Domain: "meduza.io", BestPreset: "combo", BestSuccess: true},
		},
		StrategyGroups: []discovery.StrategyGroup{
			{WinnerPreset: "combo", Domains: []string{"meduza.io"}, Set: &set},
		},
	}, cfg.ConfigPath)

	mcpRememberSuite("")
	t.Cleanup(func() { mcpRememberSuite("") })

	before := len(api.getCfg().Sets)
	applied := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{
		"action": "apply", "domain": "meduza.io",
	}))
	if !applied.Changed {
		t.Fatalf("the domain alone must be enough to apply a saved result: %+v", applied)
	}
	if got := len(api.getCfg().Sets); got != before+1 {
		t.Errorf("expected one new set, got %d from %d", got, before)
	}
}
