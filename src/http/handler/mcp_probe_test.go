package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/netprobe"
	"github.com/daniellavrushin/b4/watchdog"
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
		"a space":       {"domain": "a b.com"},
		"an at sign":    {"domain": "user@example.com"},
		"a fragment":    {"domain": "example.com#top"},
		"not ASCII":     {"domain": "пример.рф"},
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

func TestMCPProbeVerdictNoteCountsAnAnswerAsReached(t *testing.T) {
	failed := &mcpProbeResult{Verdict: "TLS_DPI"}
	loads := &mcpProbeResult{OK: true, answered: true}
	offSite := &mcpProbeResult{Verdict: string(watchdog.VerdictRedirect), answered: true, redirect: "x.com"}
	looped := &mcpProbeResult{Verdict: string(watchdog.VerdictRedirect), answered: true, redirect: "www.twitter.com"}
	short := &mcpProbeResult{Verdict: string(netprobe.DomainError), answered: true, status: 202}

	cases := []struct {
		name          string
		through, base *mcpProbeResult
		wants, nots   []string
	}{
		{"redirect both ways", offSite, offSite,
			[]string{"answers either way", "redirect to x.com, a different site", "test x.com with b4_test_domain_now"},
			[]string{"fails both ways", "blocked"}},
		{"redirect through b4, fails bypassed", offSite, failed,
			[]string{"censored", "bypass is working", "fails without b4 (TLS_DPI)", "redirect to x.com", "test x.com"},
			[]string{"fails both ways"}},
		{"fails through b4, redirect bypassed", failed, offSite,
			[]string{"FAILS through b4 (TLS_DPI)", "b4 setting is breaking it", "redirect to x.com"},
			[]string{"fails both ways", "test x.com"}},
		{"loads through b4, redirect bypassed", loads, offSite,
			[]string{"answers either way", "through b4 it loads", "with b4 bypassed it answers only with a redirect to x.com"},
			[]string{"test x.com", "fails both ways"}},
		{"redirect through b4, loads bypassed", offSite, loads,
			[]string{"answers either way", "with b4 bypassed it loads", "test x.com"},
			[]string{"fails both ways"}},
		{"redirects within the site", looped, looped,
			[]string{"answers either way", "redirects within the site", "cannot tell"},
			[]string{"test www.twitter.com", "different site", "fails both ways"}},
		{"short reply both ways", short, short,
			[]string{"answers either way", "HTTP 202, 0 bytes", "cannot tell"},
			[]string{"fails both ways", "address itself", "b4_find_bypass_strategy"}},
		{"short reply through b4, fails bypassed", short, failed,
			[]string{"censored", "bypass is working", "HTTP 202"},
			[]string{"fails both ways"}},
		{"fails both ways", failed, failed,
			[]string{"fails both ways", "not something a packet strategy fixes", "b4_find_bypass_strategy"},
			nil},
	}
	for _, tc := range cases {
		got := mcpProbeVerdictNote("twitter.com", tc.through, tc.base)
		for _, want := range tc.wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s: note %q should mention %q", tc.name, got, want)
			}
		}
		for _, not := range tc.nots {
			if strings.Contains(got, not) {
				t.Errorf("%s: note %q must not say %q", tc.name, got, not)
			}
		}
		if strings.Contains(got, ". ") {
			t.Errorf("%s: notes for several domains are joined with '. ', so one note must not contain it: %q", tc.name, got)
		}
	}

	if got := mcpProbeVerdictNote("twitter.com", offSite, nil); !strings.Contains(got, "fetched through b4") || !strings.Contains(got, "test x.com") {
		t.Errorf("a single through-b4 fetch that redirects away must say where to look next: %q", got)
	}
	if got, want := mcpProbeVerdictNote("twitter.com", nil, failed), "twitter.com fails with b4 bypassed (TLS_DPI)"; got != want {
		t.Errorf("single-mode failure: got %q, want %q", got, want)
	}
}

func TestMCPMessagesOutsideTheGatesSayWhereTheSwitchIs(t *testing.T) {
	for name, text := range map[string]string{
		"a write that would start probes": (&mcpProbesError{what: "start the watchdog of set \"video\""}).Error(),
		"a write to the MCP settings":     mcpDeniedPathHint("system.web_server.mcp.allow_writes"),
		"a check that keeps a give-up":    mcpForceCheckNote("video", watchdog.ForceCheckScheduled, false),
	} {
		if !strings.Contains(text, mcpSwitchWhere) {
			t.Errorf("%s: the message must say the switch is in b4's own web interface: %q", name, text)
		}
	}
}

func TestMCPProbeNoteForResultsThatAreNotAFetchVerdict(t *testing.T) {
	refused := &mcpProbeResult{Error: "\"a b.com\" is not a bare hostname: pass a domain such as example.com"}
	sinkholed := &mcpProbeResult{Verdict: string(netprobe.DomainDNSFake), Error: "every address youtube.com resolves to is private or local"}
	bareAddress := &mcpProbeResult{Verdict: string(watchdog.VerdictRedirect), answered: true, redirect: "203.0.113.5"}
	noHost := &mcpProbeResult{Verdict: string(watchdog.VerdictRedirect), answered: true}

	for _, c := range []struct {
		name          string
		through, base *mcpProbeResult
		wants, nots   []string
	}{
		{"input refused before any fetch", refused, refused,
			[]string{"was not fetched", "not a bare hostname"},
			[]string{"fails both ways", "b4_find_bypass_strategy", "blocked"}},
		{"sinkholed both ways", sinkholed, sinkholed,
			[]string{"forged DNS answer", "DNS_FAKE", "b4_find_bypass_strategy checks DNS"},
			[]string{"address itself may be blocked", "site may be down"}},
		{"redirect to a bare address", bareAddress, bareAddress,
			[]string{"bare address 203.0.113.5", "cannot tell"},
			[]string{"test 203.0.113.5", "different site"}},
		{"redirect that names no host", noHost, noHost,
			[]string{"names no host", "cannot tell"},
			[]string{"test ", "redirect to ,"}},
	} {
		got := mcpProbeVerdictNote("youtube.com", c.through, c.base)
		for _, want := range c.wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s: note %q should mention %q", c.name, got, want)
			}
		}
		for _, not := range c.nots {
			if strings.Contains(got, not) {
				t.Errorf("%s: note %q must not say %q", c.name, got, not)
			}
		}
	}
	if got := mcpProbeVerdictNote("a b.com", refused, nil); !strings.Contains(got, "was not fetched") {
		t.Errorf("a single refused fetch is not a failure verdict either: %q", got)
	}
}

func TestMCPProbeResultOfNamesTheSinkholedHop(t *testing.T) {
	hop := mcpProbeResultOf("youtube.com", probeModeBaseline, watchdog.CheckResult{},
		&watchdog.ErrPrivateDestination{Addr: "10.0.0.7", Host: "www.youtube.com"})
	if hop.Verdict != string(netprobe.DomainDNSFake) ||
		!strings.Contains(hop.Error, "youtube.com redirects to www.youtube.com") ||
		!strings.Contains(hop.Error, "every address www.youtube.com resolves to") {
		t.Errorf("a sinkhole met after a redirect must name the name that is forged: %+v", hop)
	}

	own := mcpProbeResultOf("youtube.com", probeModeBaseline, watchdog.CheckResult{},
		&watchdog.ErrPrivateDestination{Addr: "10.0.0.7", Host: "YouTube.com"})
	if strings.Contains(own.Error, "redirects to") || !strings.Contains(own.Error, "every address youtube.com resolves to") {
		t.Errorf("the probed name itself, in any case, is reported as before: %+v", own)
	}

	redirect := mcpProbeResultOf("twitter.com", probeModeThroughB4, watchdog.CheckResult{
		Verdict: watchdog.VerdictRedirect, RedirectTo: "x.com", Answered: true, Error: "answered with a redirect to x.com",
	}, nil)
	if !mcpProbeAnswered(&redirect) || redirect.redirect != "x.com" || !mcpProbeOffSite("twitter.com", &redirect) {
		t.Errorf("the redirect target must reach the note: %+v", redirect)
	}
	short := mcpProbeResultOf("amazon.com", probeModeThroughB4, watchdog.CheckResult{
		Verdict: netprobe.DomainError, Answered: true, StatusCode: 202, Error: "insufficient data: 0 bytes (HTTP 202)",
	}, nil)
	if note := mcpProbeVerdictNote("amazon.com", &short, &short); short.status != 202 || !strings.Contains(note, "HTTP 202") {
		t.Errorf("the status of a short reply must reach the note: %+v, %q", short, note)
	}
	raw, err := json.Marshal(redirect)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]any
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for key := range keys {
		switch key {
		case "domain", "mode", "ok", "verdict", "error", "kb_per_sec", "bytes_read":
		default:
			t.Errorf("the result's JSON grew a key %q; the tool-list byte budget counts the output schema", key)
		}
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
	cfg.System.WebServer.MCP.AllowActiveProbes = true
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
	cfg.System.WebServer.MCP.AllowActiveProbes = true
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

func TestMCPWatchdogTrafficActionsRequireTheProbeGate(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.System.Checker.Watchdog.Domains = []string{"rutracker.org"}
	if cfg.System.WebServer.MCP.AllowActiveProbes {
		t.Fatal("precondition: active probes are off")
	}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	for name, args := range map[string]map[string]any{
		"add makes the router fetch a new host on a timer": {"action": "add", "domain": "example.com"},
		"enable starts fetching every watched host":        {"action": "enable"},
		"check fetches one host immediately":               {"action": "check", "domain": "rutracker.org"},
	} {
		res := callWatchdog(t, session, ctx, args)
		if !res.IsError {
			t.Errorf("%s, so it must need 'Allow active probes' and not the write gate alone", name)
			continue
		}
		if !strings.Contains(mcpErrorText(res), "Allow active probes") {
			t.Errorf("%s: the refusal must name the setting that unlocks it: %q", name, mcpErrorText(res))
		}
	}

	live := api.getCfg().System.Checker.Watchdog
	if live.Enabled || len(live.Domains) != 1 {
		t.Errorf("a refused action must not touch the config: %+v", live)
	}
}

func TestMCPWatchdogQuietActionsNeedOnlyTheWriteGate(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.System.Checker.Watchdog.Enabled = true
	cfg.System.Checker.Watchdog.Domains = []string{"rutracker.org"}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	if res := callWatchdog(t, session, ctx, map[string]any{"action": "disable"}); res.IsError {
		t.Errorf("disabling the watchdog stops traffic and must not need the probe gate: %s", mcpErrorText(res))
	}
	if res := callWatchdog(t, session, ctx, map[string]any{"action": "remove", "domain": "rutracker.org"}); res.IsError {
		t.Errorf("removing a domain stops traffic and must not need the probe gate: %s", mcpErrorText(res))
	}
	live := api.getCfg().System.Checker.Watchdog
	if live.Enabled || len(live.Domains) != 0 {
		t.Errorf("both actions should have applied: %+v", live)
	}
}

func TestMCPWatchdogAddRefusesPrivateHostsAndNormalisesCase(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.System.WebServer.MCP.AllowActiveProbes = true
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	for _, host := range []string{"192.168.1.50", "127.0.0.1", "https://10.0.0.1/status"} {
		if res := callWatchdog(t, session, ctx, map[string]any{"action": "add", "domain": host}); !res.IsError {
			t.Errorf("%q would put the router on a timer against its own network and must be refused", host)
		}
	}

	decodeWatchdog(t, callWatchdog(t, session, ctx, map[string]any{"action": "add", "domain": "RuTracker.ORG"}))
	got := api.getCfg().System.Checker.Watchdog.Domains
	if len(got) != 1 || got[0] != "rutracker.org" {
		t.Fatalf("the stored entry must be lower case, or the web UI can never delete it: %v", got)
	}
}

func TestMCPWatchdogCheckRefusesAnUnwatchedDomain(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowActiveProbes = true
	cfg.System.Checker.Watchdog.Domains = []string{"rutracker.org"}
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callWatchdog(t, session, ctx, map[string]any{"action": "check", "domain": "youtube.com"})
	if !res.IsError {
		t.Fatal("check only re-tests a watched domain; anything else schedules nothing and must say so rather than report success")
	}
	if !strings.Contains(mcpErrorText(res), "not watched") {
		t.Errorf("the refusal should say why: %q", mcpErrorText(res))
	}
}
