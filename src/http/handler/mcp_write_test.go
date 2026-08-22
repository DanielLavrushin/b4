package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func decodeRevert(t *testing.T, session *mcp.ClientSession, ctx context.Context) mcpRevertOut {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_revert_last_change"})
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if res.IsError {
		t.Fatalf("revert returned an error: %+v", res.Content)
	}
	var out mcpRevertOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode revert: %v", err)
	}
	return out
}

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
		// The surface is too large to enumerate in the description, so it must
		// instead point at the discovery tool, name the gate and offer the way back.
		for _, want := range []string{
			"b4_list_writable_paths",
			"b4_revert_last_change",
			"Allow configuration changes",
			"sets[<id or name>]",
		} {
			if !strings.Contains(tool.Description, want) {
				t.Errorf("description should mention %q", want)
			}
		}
		return
	}
	t.Fatal("b4_set_config_value not registered")
}

func TestMCPRevertToolIsRegisteredAndDestructive(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "b4_revert_last_change" {
			continue
		}
		a := tool.Annotations
		if a == nil || a.DestructiveHint == nil || !*a.DestructiveHint {
			t.Error("undo changes the running configuration and must carry destructiveHint")
		}
		return
	}
	t.Fatal("b4_revert_last_change not registered")
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
		"system.web_server.mcp.token",
		"queue.mode",
		"queue.tun.device_name",
		"queue.mark",
		"system.tables.engine",
		"sets[video].routing.upstream.password",
		"sets[video].routing.upstream.username",
		"sets[video].routing.fwmark",
		"sets[video].routing.table",
		"",
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

func TestMCPWritableSurfaceExposesNoCredentials(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.Sets[0].Routing.Upstream.Username = "u"
	cfg.Sets[0].Routing.Upstream.Password = "p"

	banned := []string{"password", "secret", "username", "token", "api_key"}
	for _, info := range mcpWritablePaths(cfg, cfg.Sets[0]) {
		lower := strings.ToLower(info.Path)
		for _, b := range banned {
			if strings.Contains(lower, b) {
				t.Errorf("writable path %q looks credential-adjacent (matched %q); it needs an mcp:\"deny\" tag", info.Path, b)
			}
		}
	}
}

// The deny tags are the only thing standing between the reflection writer and
// the fields it must never touch, so assert each one is actually in place.
func TestMCPDenyTagsArePresent(t *testing.T) {
	cfg := mcpTestCfg()
	denied := []struct{ canonical, setRef string }{
		{"sets[].id", "video"},
		{"sets[].routing.upstream.username", "video"},
		{"sets[].routing.upstream.password", "video"},
		{"sets[].routing.fwmark", "video"},
		{"sets[].routing.table", "video"},
		{"system.socks5.username", ""},
		{"system.socks5.password", ""},
		{"system.socks5.allowed_sources", ""},
		{"system.mtproto.secrets", ""},
		// Inside a writable root, but the directory is a filesystem location:
		// pointing it elsewhere silently stops file logging, which is also what
		// b4_logs_tail reads.
		{"system.logging.directory", ""},
	}
	for _, d := range denied {
		if _, err := mcpResolvePath(cfg, d.canonical, d.setRef); err == nil {
			t.Errorf("%s resolved but must be refused: the mcp:\"deny\" tag is missing", d.canonical)
		}
	}
}

func TestMCPWritableRootsAreFailClosed(t *testing.T) {
	outside := []string{
		"system.web_server.port",
		"system.web_server.mcp.enabled",
		"system.tables.skip_setup",
		"system.ai.endpoint",
		"system.checker.reference_domain",
		"queue.mode",
		"queue.threads",
		"sets_extra.enabled",
	}
	for _, path := range outside {
		if mcpPathAllowed(path) {
			t.Errorf("%q is outside the writable roots but was allowed", path)
		}
	}
	inside := []string{
		"sets[].enabled",
		"sets[].tcp.seg2delay",
		"sets[].targets.sni_domains",
		"system.mtproto.port",
		"system.socks5.udp_timeout",
		"system.logging.level",
	}
	for _, path := range inside {
		if !mcpPathAllowed(path) {
			t.Errorf("%q should be writable", path)
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

func writableCfg(t *testing.T) *config.Config {
	t.Helper()
	mcpResetHistory()
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	return cfg
}

func TestMCPWriteCoercesEveryScalarKind(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	cases := []struct{ path, value, want string }{
		{"sets[video].tcp.seg2delay", "80", "80"},                          // int
		{"sets[video].faking.ttl", "7", "7"},                               // uint8
		{"sets[video].faking.seq_offset", "-3", "-3"},                      // int32
		{"sets[video].faking.timestamp_decrease", "1200", "1200"},          // uint32
		{"sets[video].tcp.drop_sack", "true", "true"},                      // bool
		{"sets[video].tcp.dport_filter", "80,443,8443", "80,443,8443"},     // string
		{"sets[video].targets.sni_domains", "a.com, b.com", "a.com,b.com"}, // []string
		{"system.mtproto.port", "1443", "1443"},
		{"system.socks5.udp_timeout", "45", "45"},
	}
	for _, tc := range cases {
		res := callSetValue(t, session, ctx, tc.path, tc.value)
		if res.IsError {
			t.Errorf("%s=%s rejected: %+v", tc.path, tc.value, res.Content)
			continue
		}
		if got := decodeSetValue(t, res).Current; got != tc.want {
			t.Errorf("%s = %q, want %q", tc.path, got, tc.want)
		}
	}

	// Read through the API: saving stores a new config pointer rather than
	// mutating the one the test handed in.
	if got := api.getCfg().Sets[0].Targets.SNIDomains; len(got) != 2 || got[0] != "a.com" || got[1] != "b.com" {
		t.Errorf("live set domains = %v, want [a.com b.com]", got)
	}
}

func TestMCPWriteRejectsMalformedValues(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	cases := []struct{ path, value string }{
		{"sets[video].tcp.seg2delay", "soon"},  // not a number
		{"sets[video].faking.ttl", "999"},      // overflows uint8
		{"sets[video].faking.ttl", "-1"},       // negative into unsigned
		{"sets[video].tcp.drop_sack", "maybe"}, // not a boolean
		{"sets[video].tcp.win.mode", "banana"}, // outside the enum
		{"sets[video].nope", "1"},              // no such setting
		{"sets[video].tcp", "1"},               // section, not a value
		{"sets[missing].enabled", "false"},     // no such set
	}
	for _, tc := range cases {
		if res := callSetValue(t, session, ctx, tc.path, tc.value); !res.IsError {
			t.Errorf("%s=%q should have been rejected", tc.path, tc.value)
		}
	}
}

func TestMCPWriteNormalisesEnumCase(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].tcp.win.mode", "OSCILLATE"))
	if out.Current != "oscillate" {
		t.Errorf("enum should normalise case, got %q", out.Current)
	}
}

func TestMCPRevertRestoresPreviousValue(t *testing.T) {
	cfg := writableCfg(t)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	before := cfg.Sets[0].Fragmentation.Strategy
	if res := callSetValue(t, session, ctx, "sets[video].fragmentation.strategy", "disorder"); res.IsError {
		t.Fatalf("write failed: %+v", res.Content)
	}
	if got := api.getCfg().Sets[0].Fragmentation.Strategy; got != "disorder" {
		t.Fatalf("write did not land: %q", got)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_revert_last_change"})
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	var out mcpRevertOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Reverted || out.RestoredTo != before {
		t.Fatalf("revert = %+v, want restored_to %q", out, before)
	}
	if got := api.getCfg().Sets[0].Fragmentation.Strategy; got != before {
		t.Errorf("config still holds %q after undo, want %q", got, before)
	}
}

func TestMCPRevertWalksBackOneChangeAtATime(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	callSetValue(t, session, ctx, "sets[video].tcp.seg2delay", "10")
	callSetValue(t, session, ctx, "sets[video].tcp.seg2delay", "20")

	first := decodeRevert(t, session, ctx)
	if first.RestoredTo != "10" {
		t.Errorf("first undo restored %q, want 10", first.RestoredTo)
	}
	second := decodeRevert(t, session, ctx)
	if second.RestoredTo == "10" {
		t.Error("second undo should walk further back, not repeat the first")
	}
	if second.Remaining != 0 {
		t.Errorf("remaining_changes = %d, want 0", second.Remaining)
	}

	empty := decodeRevert(t, session, ctx)
	if empty.Reverted {
		t.Error("undo with nothing left must report reverted=false, not fail")
	}
	if empty.Note == "" {
		t.Error("an empty undo should explain itself")
	}
}

func TestMCPRevertRefusedWhenWritesDisabled(t *testing.T) {
	mcpResetHistory()
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_revert_last_change"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("undo must be refused while configuration writes are disabled")
	}
}

func TestMCPPreconditionOnlyBlocksWritesThatBreakIt(t *testing.T) {
	cfg := writableCfg(t)
	// A configuration that already violates the MTProto precondition, which is
	// reachable by editing the file by hand.
	cfg.System.MTProto.Enabled = true
	cfg.System.MTProto.Secrets = nil
	cfg.System.MTProto.FakeSNI = ""

	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	if res := callSetValue(t, session, ctx, "sets[video].tcp.seg2delay", "25"); res.IsError {
		t.Errorf("an unrelated write must not be blocked by a pre-existing violation: %+v", res.Content)
	}
}

func TestMCPListWritablePaths(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_list_writable_paths"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out mcpListPathsOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	byPath := map[string]mcpPathInfo{}
	for _, p := range out.Paths {
		byPath[p.Path] = p
	}
	for _, want := range []string{
		"sets[].enabled", "sets[].tcp.seg2delay", "sets[].targets.sni_domains",
		"sets[].fragmentation.strategy", "system.mtproto.port", "system.socks5.udp_timeout",
	} {
		if _, ok := byPath[want]; !ok {
			t.Errorf("%q should be listed as writable", want)
		}
	}
	for _, unwanted := range []string{
		"sets[].id", "sets[].routing.upstream.password", "sets[].routing.fwmark",
		"system.socks5.password", "system.web_server.port", "queue.mode",
	} {
		if _, ok := byPath[unwanted]; ok {
			t.Errorf("%q must not be listed as writable", unwanted)
		}
	}

	if got := byPath["sets[].fragmentation.strategy"]; len(got.Options) == 0 {
		t.Error("an enum path should list its accepted values")
	}
	if got := byPath["sets[].tcp.seg2delay"]; got.Type != "number" {
		t.Errorf("seg2delay type = %q, want number", got.Type)
	}
	if got := byPath["sets[].targets.sni_domains"]; got.Type != "list" {
		t.Errorf("sni_domains type = %q, want list", got.Type)
	}
}

func TestMCPWriteLogLevelByName(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	out := decodeSetValue(t, callSetValue(t, session, ctx, "system.logging.level", "debug"))
	if out.Current != "debug" {
		t.Errorf("current = %q, want the name back rather than the stored number", out.Current)
	}
	if got := api.getCfg().System.Logging.Level; got != log.LevelDebug {
		t.Errorf("stored level = %v, want %v", got, log.LevelDebug)
	}
	// The level is applied to the running process, not just saved.
	if got := log.Level(log.CurLevel.Load()); got != log.LevelDebug {
		t.Errorf("running log level = %v, want %v: applyRuntimeChanges was not called", got, log.LevelDebug)
	}

	if res := callSetValue(t, session, ctx, "system.logging.level", "verbose"); !res.IsError {
		t.Error("a name outside the four levels must be rejected, not parsed as a number")
	}

	// Undo restores the level on the running process too.
	rev := decodeRevert(t, session, ctx)
	if rev.RestoredTo != "info" {
		t.Errorf("undo restored %q, want info", rev.RestoredTo)
	}
	if got := log.Level(log.CurLevel.Load()); got != log.LevelInfo {
		t.Errorf("running log level after undo = %v, want %v", got, log.LevelInfo)
	}
}

func TestMCPListWritablePathsShowsLogLevelNames(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_list_writable_paths",
		Arguments: map[string]any{"prefix": "system.logging"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out mcpListPathsOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, p := range out.Paths {
		if p.Path == "system.logging.directory" {
			t.Error("the log directory must not be listed as writable")
		}
		if p.Path != "system.logging.level" {
			continue
		}
		if strings.Join(p.Options, ",") != "error,info,trace,debug" {
			t.Errorf("options = %v, want the four level names in verbosity order", p.Options)
		}
		return
	}
	t.Fatal("system.logging.level should be listed as writable")
}
