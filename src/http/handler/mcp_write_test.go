package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/ai"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
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

func TestMCPWriteToolsAreNotOfferedWhenWritesDisabled(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.MTProto.Enabled = true
	if cfg.System.WebServer.MCP.AllowWrites {
		t.Fatal("writes must default to disabled")
	}
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	got := toolNames(t, session, ctx)
	for _, name := range []string{
		"b4_set_config_value", "b4_revert_last_change",
		"b4_edit_set_targets", "b4_manage_set",
	} {
		if got[name] {
			t.Errorf("%s must not be offered while configuration writes are disabled", name)
		}
	}
	if !got["b4_list_writable_paths"] {
		t.Error("b4_list_writable_paths reads and never writes, so a read-only session still needs it to say what would have to change")
	}

	if instr := session.InitializeResult().Instructions; !strings.Contains(instr, "Allow configuration changes") {
		t.Errorf("instructions must name the setting that unlocks writing: %q", instr)
	}
	if cfg.System.MTProto.Enabled != true {
		t.Error("config must be untouched")
	}
}

func TestMCPWriteToolIsAnnotatedDestructive(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
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
			"b4_edit_set_targets",
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
	srv := newMCPTestServer(t, writableCfg(t))
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
		"system.mtproto.expose",
		"system.mtproto.web_proxy.expose",
		"system.socks5.expose",
		"system.web_server.expose",
		"system.tables.skip_setup",
		"system.ai.api_key_ref",
		"system.logging.directory",
		"system.web_server.mcp.allow_writes",
		"system.web_server.mcp.token",
		"queue.mode",
		"queue.tun.device_name",
		"queue.mark",
		"system.tables.engine",
		"system.tables.dscp.enabled",
		"system.tables.dscp.value",
		"sets[video].routing.upstream.password",
		"sets[video].routing.upstream.username",
		"sets[video].routing.fwmark",
		"sets[video].routing.table",
		"sets[video].dscp.enabled",
		"sets[video].dscp.value",
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

func TestMCPWriteRefusesSetDSCPWithTheReason(t *testing.T) {
	cfg := writableCfg(t)
	cfg.Sets[0].DSCP = config.SetDSCPConfig{Enabled: true, Value: 31}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	for _, path := range []string{"sets[video].dscp.enabled", "sets[set-1].dscp.value", "sets[video].dscp"} {
		res := callSetValue(t, session, ctx, path, "7")
		if !res.IsError {
			t.Fatalf("%s must not be writable", path)
		}
		if text := mcpErrorText(res); !strings.Contains(text, "set's DSCP value is not writable") || !strings.Contains(text, "next router") {
			t.Errorf("the refusal of %s must say why: %q", path, text)
		}
	}
	if got := api.getCfg().Sets[0].DSCP; got != (config.SetDSCPConfig{Enabled: true, Value: 31}) {
		t.Errorf("a refused write changed the set's DSCP value: %+v", got)
	}
	for _, info := range mcpWritablePaths(api.getCfg(), api.getCfg().Sets[0]) {
		if strings.Contains(info.Path, "dscp") {
			t.Errorf("b4_list_writable_paths offers %s", info.Path)
		}
	}
	for _, path := range []string{"sets[video].dscp.enabled", "sets[video].dscp.value"} {
		if out := callTopic(t, session, ctx, map[string]any{"path": path}); !out.Found {
			t.Errorf("a refused setting must still be explained: no topic for %s (%s)", path, out.Note)
		}
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
		{"sets[].discovery.urls", "video"},
		{"sets[].discovery.watchdog", "video"},
		{"sets[].dscp.enabled", "video"},
		{"sets[].dscp.value", "video"},
		{"system.socks5.username", ""},
		{"system.socks5.password", ""},
		{"system.socks5.allowed_sources", ""},
		{"system.mtproto.secrets", ""},
		{"system.mtproto.expose", ""},
		{"system.mtproto.web_proxy.expose", ""},
		{"system.mtproto.cfproxy_url", ""},
		{"system.mtproto.dc_fallback_url", ""},
		{"system.socks5.expose", ""},
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
		"system.tables.dscp.value",
		"system.ai.endpoint",
		"system.checker.dns_server",
		"queue.mode",
		"queue.threads",
		"sets_extra.enabled",
		"sets[].dscp",
		"sets[].dscp.enabled",
		"sets[].dscp.value",
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
	if text := mcpErrorText(res); !strings.Contains(text, "at least one secret or a fake SNI") {
		t.Fatalf("refusal must come from the precondition, not from a port probe: %q", text)
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
	cfg.System.MTProto.BindAddress = "127.0.0.1"
	cfg.System.MTProto.Port = freeTCPPort(t)

	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callSetValue(t, session, ctx, "system.mtproto.enabled", "true")
	if res.IsError {
		t.Fatalf("a fake SNI satisfies the precondition: %+v", res.Content)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
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
		{"sets[video].fragmentation.strategy_pool", "tcp, oob", "tcp,oob"}, // []string
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
	if got := api.getCfg().Sets[0].Fragmentation.StrategyPool; len(got) != 2 || got[0] != "tcp" || got[1] != "oob" {
		t.Errorf("live strategy pool = %v, want [tcp oob]", got)
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

func TestMCPRevertWithdrawnWhenWritesDisabledMidSession(t *testing.T) {
	mcpResetHistory()
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	if !toolNames(t, session, ctx)["b4_revert_last_change"] {
		t.Fatal("precondition: undo is offered while writes are on")
	}

	revoked := api.getCfg().Clone()
	revoked.System.WebServer.MCP.AllowWrites = false
	api.cfgPtr.Store(revoked)

	if toolNames(t, session, ctx)["b4_revert_last_change"] {
		t.Error("undo must be withdrawn as soon as writes are disabled")
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
	srv := newMCPTestServer(t, writableCfg(t))
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
		"sets[].enabled", "sets[].tcp.seg2delay", "sets[].targets.domain_only", "sets[].targets.ip_priority",
		"sets[].fragmentation.strategy", "system.mtproto.port", "system.socks5.udp_timeout",
		"system.mtproto.bridge.enabled",
	} {
		if _, ok := byPath[want]; !ok {
			t.Errorf("%q should be listed as writable", want)
		}
	}
	for _, unwanted := range []string{
		"sets[].id", "sets[].routing.upstream.password", "sets[].routing.fwmark",
		"system.socks5.password", "system.web_server.port", "queue.mode",
		"sets[].targets.sni_domains", "sets[].targets.ip",
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
	if got := byPath["sets[].fragmentation.strategy_pool"]; got.Type != "list" {
		t.Errorf("strategy_pool type = %q, want list", got.Type)
	}
}

func TestMCPWritesIPPriorityAndRevertsIt(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].targets.ip_priority", "true"))
	if !out.Changed || out.Previous != "false" || out.Current != "true" {
		t.Fatalf("diff = %+v, want false -> true", out)
	}
	if set := api.getCfg().GetSetById("set-1"); set == nil || !set.Targets.IPPriority {
		t.Fatal("the write was reported but targets.ip_priority is not on in the saved configuration")
	}

	rev := decodeRevert(t, session, ctx)
	if !rev.Reverted || rev.RestoredTo != "false" {
		t.Errorf("revert = %+v, want the switch restored to false", rev)
	}
	if set := api.getCfg().GetSetById("set-1"); set == nil || set.Targets.IPPriority {
		t.Error("b4_revert_last_change must turn targets.ip_priority off again")
	}
}

func TestMCPWriteSaysWhenTheTLSFilterLeavesIPPriorityWithoutEffect(t *testing.T) {
	const inert = `targets.ip_priority has no effect on set "video" while its targets.tls is set.`
	cases := []struct {
		name, path, value string
		tls               string
		priority, says    bool
	}{
		{name: "the switch written on a set with a TLS filter", path: "sets[video].targets.ip_priority", value: "true", tls: "1.2", says: true},
		{name: "a TLS filter written on a set with the switch on", path: "sets[video].targets.tls", value: "1.3", priority: true, says: true},
		{name: "the switch written on a set without a TLS filter", path: "sets[video].targets.ip_priority", value: "true"},
		{name: "another write to a set whose switch already has no effect", path: "sets[video].tcp.seg2delay", value: "10", tls: "1.2", priority: true},
		{name: "the TLS filter cleared on a set with the switch on", path: "sets[video].targets.tls", value: "", tls: "1.2", priority: true},
		{name: "the switch turned off on a set with a TLS filter", path: "sets[video].targets.ip_priority", value: "false", tls: "1.2", priority: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := writableCfg(t)
			cfg.Sets[0].Targets.IPs = []string{"203.0.113.8/29"}
			cfg.Sets[0].Targets.TLSVersion = tc.tls
			cfg.Sets[0].Targets.IPPriority = tc.priority
			srv, api := newMCPTestServerAPI(t, cfg)
			session, ctx := connectMCP(t, srv)

			out := decodeSetValue(t, callSetValue(t, session, ctx, tc.path, tc.value))
			if !out.Changed {
				t.Fatalf("write did not take: %+v", out)
			}
			set := api.getCfg().GetSetById("set-1")
			if tc.says && (!set.Targets.IPPriority || set.IPPriorityApplies()) {
				t.Fatalf("precondition: the switch is stored on without effect, got ip_priority=%v tls=%q", set.Targets.IPPriority, set.Targets.TLSVersion)
			}
			if got := strings.Contains(out.Note, inert); got != tc.says {
				t.Errorf("note says the switch has no effect = %v, want %v: %q", got, tc.says, out.Note)
			}
		})
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
	srv := newMCPTestServer(t, writableCfg(t))
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

func TestMCPWriteRefusesTargetLists(t *testing.T) {
	cfg := writableCfg(t)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	for _, path := range []string{
		"sets[video].targets.sni_domains",
		"sets[video].targets.ip",
		"sets[video].targets.geosite_categories",
		"sets[video].targets.geoip_categories",
		"sets[video].targets.source_devices",
	} {
		res := callSetValue(t, session, ctx, path, "something")
		if !res.IsError {
			t.Errorf("%s must be refused here: this tool replaces a list wholesale and skips validation and the cross-set handover", path)
			continue
		}
		if !strings.Contains(mcpErrorText(res), "b4_edit_set_targets") {
			t.Errorf("the refusal for %s must name the tool that does the job: %q", path, mcpErrorText(res))
		}
	}
	if got := api.getCfg().Sets[0].Targets.SNIDomains; len(got) != 1 || got[0] != "youtube.com" {
		t.Errorf("targets must be untouched: %v", got)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_list_writable_paths",
		Arguments: map[string]any{"prefix": "sets[].targets"},
	})
	if err != nil {
		t.Fatalf("list writable paths: %v", err)
	}
	var out mcpListPathsOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, p := range out.Paths {
		if mcpPathIsTargetList(p.Path) {
			t.Errorf("%s must not be advertised as writable here", p.Path)
		}
	}
	if len(out.Paths) == 0 {
		t.Error("the scalar targets settings should still be listed")
	}
}

func TestMCPWriteReportsValueRejectedByValidate(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callSetValue(t, session, ctx, "sets[video].escalate.to", "disabled-set")
	if res.IsError {
		t.Fatalf("write failed: %+v", res.Content)
	}
	out := decodeSetValue(t, res)
	if out.Changed {
		t.Errorf("write did not survive Validate and must not report a change: %+v", out)
	}
	if out.Current != "" || out.Current != out.Previous {
		t.Errorf("current must be the value read back after saving, got %+v", out)
	}
	if !strings.Contains(out.Note, "not applied") {
		t.Errorf("note must say the value was not kept: %q", out.Note)
	}
	if got := api.getCfg().Sets[0].Escalate.To; got != "" {
		t.Fatalf("escalate.to should have been cleared, got %q", got)
	}
	if len(mcpHistory) != 0 {
		t.Errorf("a write that changed nothing must not spend an undo slot, history = %d", len(mcpHistory))
	}
}

func TestMCPWriteAcceptedValueIsReadBack(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].escalate.to", "set-2"))
	if !out.Changed || out.Current != "set-2" {
		t.Fatalf("a valid set id must be kept: %+v", out)
	}
	if got := api.getCfg().Sets[0].Escalate.To; got != "set-2" {
		t.Fatalf("escalate.to = %q, want set-2", got)
	}
	if len(mcpHistory) != 1 {
		t.Errorf("a real change must be undoable, history = %d", len(mcpHistory))
	}
}

func TestMCPValidateCandidateRefusesOpenSocks5(t *testing.T) {
	base := mcpTestCfg()
	base.System.Socks5.Enabled = false
	base.System.Socks5.Username = ""
	base.System.Socks5.Password = ""

	open := base.Clone()
	open.System.Socks5.Enabled = true
	if err := mcpValidateCandidate(base, open); err == nil {
		t.Fatal("enabling SOCKS5 with no password and no source ACL publishes an open proxy and must be refused")
	}

	withACL := base.Clone()
	withACL.System.Socks5.Enabled = true
	withACL.System.Socks5.AllowedSources = []string{"192.168.1.0/24"}
	if err := mcpValidateCandidate(base, withACL); err != nil {
		t.Errorf("a source ACL is enough to enable it: %v", err)
	}

	withCreds := base.Clone()
	withCreds.System.Socks5.Enabled = true
	withCreds.System.Socks5.Username = "u"
	withCreds.System.Socks5.Password = "p"
	if err := mcpValidateCandidate(base, withCreds); err != nil {
		t.Errorf("credentials are enough to enable it: %v", err)
	}

	alreadyOpen := open.Clone()
	unrelated := alreadyOpen.Clone()
	unrelated.System.Logging.Level = 3
	if err := mcpValidateCandidate(alreadyOpen, unrelated); err != nil {
		t.Errorf("a configuration that is already open must not block unrelated writes: %v", err)
	}
}

func TestMCPWriteRefusesUpstreamEndpoint(t *testing.T) {
	cfg := mcpSecretsCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	for _, path := range []string{
		"sets[video].routing.upstream.host",
		"sets[video].routing.upstream.port",
	} {
		if res := callSetValue(t, session, ctx, path, "10.0.0.10"); !res.IsError {
			t.Errorf("%s must be refused: repointing the endpoint makes b4 offer the stored proxy credentials to it", path)
		}
	}
	if got := api.getCfg().Sets[0].Routing.Upstream.Host; got != "10.0.0.9" {
		t.Fatalf("upstream host was changed to %q", got)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_list_writable_paths",
		Arguments: map[string]any{"prefix": "sets[].routing.upstream"},
	})
	if err != nil {
		t.Fatalf("list writable paths: %v", err)
	}
	var out mcpListPathsOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, p := range out.Paths {
		if strings.HasSuffix(p.Path, ".upstream.host") || strings.HasSuffix(p.Path, ".upstream.port") {
			t.Errorf("%s must not be advertised as writable", p.Path)
		}
	}
}

func TestMCPWriteRenameReadsBackByID(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].name", "clips"))
	if !out.Changed || out.Previous != "video" || out.Current != "clips" {
		t.Fatalf("renaming a set must be reported accurately: %+v", out)
	}
	if got := api.getCfg().Sets[0].Name; got != "clips" {
		t.Fatalf("name = %q, want clips", got)
	}
}

func TestMCPWriteRefusesOpeningAnExposedPort(t *testing.T) {
	cfg := writableCfg(t)
	cfg.System.MTProto.Enabled = false
	cfg.System.MTProto.Expose = true
	cfg.System.MTProto.FakeSNI = "www.google.com"

	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callSetValue(t, session, ctx, "system.mtproto.enabled", "true")
	if !res.IsError {
		t.Fatal("enabling MTProto while its Expose to internet switch is on opens a port to the internet and must be refused over MCP")
	}
	if text := mcpErrorText(res); !strings.Contains(text, "system.mtproto.expose") {
		t.Fatalf("the refusal must name the switch responsible: %q", text)
	}
	if cfg.System.MTProto.Enabled {
		t.Error("config must be untouched when the write is refused")
	}
}

func TestMCPWriteRefusesMovingOrWideningAnExposedPort(t *testing.T) {
	for _, tc := range []struct{ path, value string }{
		{"system.socks5.port", "10800"},
		{"system.socks5.bind_address", "0.0.0.0"},
	} {
		cfg := writableCfg(t)
		cfg.System.Socks5.Enabled = true
		cfg.System.Socks5.Expose = true
		cfg.System.Socks5.BindAddress = "192.168.1.1"

		srv := newMCPTestServer(t, cfg)
		session, ctx := connectMCP(t, srv)

		res := callSetValue(t, session, ctx, tc.path, tc.value)
		if !res.IsError {
			t.Fatalf("%s=%s opens a new port or address to the internet and must be refused over MCP", tc.path, tc.value)
		}
		if text := mcpErrorText(res); !strings.Contains(text, "system.socks5.expose") {
			t.Fatalf("%s: the refusal must name the switch responsible: %q", tc.path, text)
		}
	}
}

func mcpVoiceCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg := writableCfg(t)
	voice := config.NewSetConfig()
	voice.Id, voice.Name, voice.Enabled = "set-voice", "voice", true
	voice.UDP.DPortFilter = "50000-50100"
	voice.UDP.FilterL7 = []string{config.UDPL7Discord}
	cfg.Sets = append(cfg.Sets, &voice)
	return cfg
}

func TestMCPWriteAcceptsOnlyTheUDPProtocolsB4Matches(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, mcpVoiceCfg(t))
	session, ctx := connectMCP(t, srv)

	for _, value := range []string{"discrod", "discord,wireguard"} {
		res := callSetValue(t, session, ctx, "sets[voice].udp.filter_l7", value)
		if !res.IsError {
			t.Fatalf("%q names a protocol b4 does not match and must be refused, not saved to match nothing", value)
		}
		if text := mcpErrorText(res); !strings.Contains(text, "discord, stun") {
			t.Errorf("the refusal of %q must list the protocols b4 matches: %q", value, text)
		}
	}
	if got := api.getCfg().Sets[2].UDP.FilterL7; !reflect.DeepEqual(got, []string{config.UDPL7Discord}) {
		t.Fatalf("a refused write changed the protocols: %v", got)
	}

	out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[voice].udp.filter_l7", " STUN, Discord "))
	if !out.Changed || out.Current != "stun,discord" {
		t.Fatalf("known protocols in any case must be stored in their canonical form: %+v", out)
	}
	if got := api.getCfg().Sets[2].UDP.FilterL7; !reflect.DeepEqual(got, []string{config.UDPL7STUN, config.UDPL7Discord}) {
		t.Fatalf("live protocols = %v", got)
	}
}

func TestMCPListWritablePathsOffersTheUDPProtocols(t *testing.T) {
	srv := newMCPTestServer(t, mcpVoiceCfg(t))
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_list_writable_paths",
		Arguments: map[string]any{"prefix": "sets[].udp.filter_l7", "set": "voice"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out mcpListPathsOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Paths) != 1 {
		t.Fatalf("expected sets[].udp.filter_l7 alone, got %+v", out.Paths)
	}
	if p := out.Paths[0]; p.Type != "list" || p.Current != config.UDPL7Discord || !reflect.DeepEqual(p.Options, config.UDPL7Classes) {
		t.Errorf("the protocol list must offer the protocols b4 matches: %+v", p)
	}
}

func TestMCPWriteRejectionNamesTheFieldTheConfigRefuses(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, mcpVoiceCfg(t))
	session, ctx := connectMCP(t, srv)

	for _, c := range []struct{ path, value, field, reason string }{
		{"sets[voice].tcp.dport_filter", "443", "sets[2].tcp.dport_filter", "takes no TCP port filter"},
		{"sets[video].udp.filter_l7", "discord", "sets[0].udp.dport_filter", "must list the UDP ports"},
	} {
		res := callSetValue(t, session, ctx, c.path, c.value)
		if !res.IsError {
			t.Fatalf("%s=%s must be refused by the config validation", c.path, c.value)
		}
		if text := mcpErrorText(res); !strings.Contains(text, c.field+": ") || !strings.Contains(text, c.reason) {
			t.Errorf("the refusal of %s must name %s and the reason: %q", c.path, c.field, text)
		}
	}
	live := api.getCfg()
	if live.Sets[2].TCP.DPortFilter != "" || len(live.Sets[0].UDP.FilterL7) != 0 {
		t.Fatalf("refused writes changed the config: tcp=%q filter_l7=%v", live.Sets[2].TCP.DPortFilter, live.Sets[0].UDP.FilterL7)
	}
}

func TestMCPWriteMayCloseAnExposedPort(t *testing.T) {
	cfg := writableCfg(t)
	cfg.System.Socks5.Enabled = true
	cfg.System.Socks5.Expose = true

	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	if res := callSetValue(t, session, ctx, "system.socks5.enabled", "false"); res.IsError {
		t.Fatalf("turning an exposed listener off closes a port and must stay allowed: %+v", res.Content)
	}
}

func TestMCPWriteEnablingASetLoadsItsTargets(t *testing.T) {
	cfg := writableCfg(t)
	cfg.LoadTargets()
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	if matched, _ := sni.NewSuffixSet(api.getCfg().Sets).MatchSNI("example.org"); matched {
		t.Fatal("precondition: a set disabled at startup matches nothing")
	}
	out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[disabled-set].enabled", "true"))
	if !out.Changed {
		t.Fatalf("write did not take: %+v", out)
	}
	matched, set := sni.NewSuffixSet(api.getCfg().Sets).MatchSNI("example.org")
	if !matched || set == nil || set.Id != "set-2" {
		t.Fatalf("a set enabled over MCP must match its own domain at once, not after a restart: matched=%v", matched)
	}
}

func TestMCPSaveRechecksTheWriteSwitchesOnTheConfigItReplaces(t *testing.T) {
	for _, c := range []struct {
		name string
		off  func(*config.MCPConfig)
	}{
		{"allow_writes off", func(m *config.MCPConfig) { m.AllowWrites = false }},
		{"mcp off", func(m *config.MCPConfig) { m.Enabled = false }},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, api := newMCPTestServerAPI(t, writableCfg(t))
			off := api.getCfg().Clone()
			c.off(&off.System.WebServer.MCP)
			if err := api.saveAndPushConfig(off); err != nil {
				t.Fatal(err)
			}

			oldCfg := api.getCfg()
			newCfg := oldCfg.Clone()
			newCfg.Sets[0].TCP.Seg2Delay = 42
			if err := api.mcpSave(oldCfg, newCfg); !errors.Is(err, errMCPWritesOff) {
				t.Fatalf("a write that reaches the save after the switch went off must be refused, got %v", err)
			}
			if got := api.getCfg().Sets[0].TCP.Seg2Delay; got != 0 {
				t.Errorf("the refused write landed: seg2delay = %d", got)
			}
		})
	}
}

func waitForQueuedMCPWrite(t *testing.T, file string) {
	t.Helper()
	buf := make([]byte, 1<<20)
	deadline := time.Now().Add(10 * time.Second)
	for {
		n := runtime.Stack(buf, true)
		if n == len(buf) {
			buf = make([]byte, 2*len(buf))
			continue
		}
		for _, g := range strings.Split(string(buf[:n]), "\n\n") {
			if strings.Contains(g, "sync.(*Mutex).Lock") && strings.Contains(g, file) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no tool call queued on the MCP write lock in %s", file)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMCPQueuedWriteIsRefusedOnceWritesAreSwitchedOff(t *testing.T) {
	tools := []struct {
		name, file string
		args       map[string]any
	}{
		{"b4_set_config_value", "/http/handler/mcp_write.go:", map[string]any{"path": "sets[video].tcp.seg2delay", "value": "42"}},
		{"b4_edit_set_targets", "/http/handler/mcp_targets.go:", map[string]any{"set": "video", "kind": "sni_domains", "add": "queued.example"}},
	}
	switches := []struct {
		name string
		off  func(*config.MCPConfig)
	}{
		{"allow_writes off", func(m *config.MCPConfig) { m.AllowWrites = false }},
		{"mcp off", func(m *config.MCPConfig) { m.Enabled = false }},
	}
	for _, tool := range tools {
		for _, sw := range switches {
			t.Run(tool.name+" "+sw.name, func(t *testing.T) {
				srv, api := newMCPTestServerAPI(t, writableCfg(t))
				session, ctx := connectMCP(t, srv)

				mcpWriteMu.Lock()
				held := true
				defer func() {
					if held {
						mcpWriteMu.Unlock()
					}
				}()
				type outcome struct {
					res *mcp.CallToolResult
					err error
				}
				done := make(chan outcome, 1)
				go func() {
					res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool.name, Arguments: tool.args})
					done <- outcome{res, err}
				}()
				waitForQueuedMCPWrite(t, tool.file)

				off := api.getCfg().Clone()
				sw.off(&off.System.WebServer.MCP)
				if err := api.saveAndPushConfig(off); err != nil {
					t.Fatal(err)
				}
				held = false
				mcpWriteMu.Unlock()

				got := <-done
				if got.err != nil {
					t.Fatal(got.err)
				}
				if !got.res.IsError || !strings.Contains(mcpErrorText(got.res), "Allow configuration changes") {
					t.Fatalf("the user switched writes off while the call waited for the write lock, so it must not write: %s", mcpErrorText(got.res))
				}
				live := api.getCfg().Sets[0]
				if live.TCP.Seg2Delay != 0 || !reflect.DeepEqual(live.Targets.SNIDomains, []string{"youtube.com"}) {
					t.Errorf("the refused call changed the set: seg2delay=%d sni_domains=%v", live.TCP.Seg2Delay, live.Targets.SNIDomains)
				}
			})
		}
	}
}

func TestMCPWriteAcceptsEverySNIMutationModeThePacketPathImplements(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	implemented := []string{"off", "duplicate", "grease", "padding", "reorder", "full", "advanced"}
	for _, mode := range implemented {
		res := callSetValue(t, session, ctx, "sets[video].faking.sni_mutation.mode", strings.ToUpper(mode))
		if res.IsError {
			t.Errorf("%s is implemented by nfq/mutation.go and must be writable: %s", mode, mcpErrorText(res))
			continue
		}
		if got := api.getCfg().Sets[0].Faking.SNIMutation.Mode; got != mode {
			t.Errorf("mode %s stored as %q", mode, got)
		}
	}
	for _, mode := range []string{"random", "fakeext", "fakesni"} {
		if res := callSetValue(t, session, ctx, "sets[video].faking.sni_mutation.mode", mode); !res.IsError {
			t.Errorf("%s leaves the ClientHello unchanged in the packet path and must be refused", mode)
		}
	}

	topic := ai.TopicFacts("faking.sni_mutation.mode")
	for _, mode := range implemented {
		if !strings.Contains(topic, "'"+mode+"'") {
			t.Errorf("the faking.sni_mutation.mode topic never describes the mode '%s'", mode)
		}
	}
}

func TestMCPWriteOptionFieldsTakeOnlyTheirOptionsInCanonicalCase(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	cases := []struct{ path, value, want, bad string }{
		{"sets[video].faking.strategy", "PastSeq", "pastseq", "past_seq"},
		{"sets[video].udp.faking_strategy", "CheckSum", "checksum", "bogus"},
		{"sets[video].fragmentation.combo.shuffle_mode", "Middle", "middle", "bogus"},
		{"sets[video].fragmentation.disorder.shuffle_mode", "REVERSE", "reverse", "bogus"},
		{"sets[video].fragmentation.disorder.shuffle_mode", "Middle", "middle", "edges"},
		{"sets[video].routing.block_action", "Drop", "drop", "reject-tcp-reset"},
		{"sets[video].fragmentation.strategy_pool", "TCP, Oob", "tcp,oob", "tcp,bogus"},
		{"sets[video].faking.tls_mod", "RND, DupSid", "rnd,dupsid", "bogus"},
	}
	for _, c := range cases {
		res := callSetValue(t, session, ctx, c.path, c.value)
		if res.IsError {
			t.Errorf("%s=%s refused: %s", c.path, c.value, mcpErrorText(res))
		} else if got := decodeSetValue(t, res).Current; got != c.want {
			t.Errorf("%s=%s stored as %q, want the canonical %q", c.path, c.value, got, c.want)
		}
		if res := callSetValue(t, session, ctx, c.path, c.bad); !res.IsError {
			t.Errorf("%s=%s is not a value b4 acts on and must be refused", c.path, c.bad)
		}
	}

	cfg := mcpTestCfg()
	listed := map[string]mcpPathInfo{}
	for _, p := range mcpWritablePaths(cfg, cfg.Sets[0]) {
		listed[p.Path] = p
	}
	for _, c := range cases {
		canonical, _, err := parseMCPWritePath(c.path)
		if err != nil {
			t.Fatal(err)
		}
		if len(listed[canonical].Options) == 0 {
			t.Errorf("b4_list_writable_paths offers no options for %s", canonical)
		}
	}
}

func TestMCPWriteRefusesNumbersOutsideTheUIRange(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	for _, c := range []struct{ path, value, bounds string }{
		{"sets[video].faking.sni_seq_length", "1000000000", "1 to 20"},
		{"sets[video].faking.sni_seq_length", "0", "1 to 20"},
		{"sets[video].udp.fake_seq_length", "1000000", "1 to 20"},
		{"sets[video].udp.fake_len", "-100", "32 to 1500"},
		{"sets[video].tcp.seg2delay", "3600000", "0 to 1000"},
		{"sets[video].tcp.seg2delay_max", "9223372036854775807", "0 to 1000"},
		{"sets[video].tcp.desync.count", "-5", "1 to 20"},
		{"sets[video].faking.sni_mutation.grease_count", "-1", "1 to 10"},
		{"sets[video].fragmentation.combo.first_delay_ms_max", "5", "10 to 500, or 0"},
		{"sets[video].fragmentation.seq_overlap_length", "9223372036854775807", "0 to 1460"},
		{"sets[video].fragmentation.seq_overlap_length", "100000000", "0 to 1460"},
	} {
		res := callSetValue(t, session, ctx, c.path, c.value)
		if !res.IsError {
			t.Errorf("%s=%s is outside the accepted range and must be refused", c.path, c.value)
			continue
		}
		if text := mcpErrorText(res); !strings.Contains(text, c.bounds) {
			t.Errorf("the refusal of %s=%s must name the range %q: %s", c.path, c.value, c.bounds, text)
		}
	}
	live := api.getCfg().Sets[0]
	if live.Faking.SNISeqLength != 0 || live.UDP.FakeSeqLength != 0 || live.TCP.Seg2Delay != 0 || live.TCP.Seg2DelayMax != 0 {
		t.Fatalf("a refused write changed the set: sni_seq_length=%d fake_seq_length=%d seg2delay=%d-%d",
			live.Faking.SNISeqLength, live.UDP.FakeSeqLength, live.TCP.Seg2Delay, live.TCP.Seg2DelayMax)
	}

	for _, c := range []struct{ path, value string }{
		{"sets[video].faking.sni_seq_length", "20"},
		{"sets[video].tcp.seg2delay", "1000"},
		{"sets[video].fragmentation.combo.first_delay_ms_max", "200"},
		{"sets[video].fragmentation.combo.first_delay_ms_max", "0"},
		{"sets[video].fragmentation.seq_overlap_length", "681"},
	} {
		res := callSetValue(t, session, ctx, c.path, c.value)
		if res.IsError {
			t.Errorf("%s=%s is inside the range and must be kept: %s", c.path, c.value, mcpErrorText(res))
			continue
		}
		if got := decodeSetValue(t, res).Current; got != c.value {
			t.Errorf("%s=%s read back as %q", c.path, c.value, got)
		}
	}
}

func TestMCPRangesNameIntegerSettingsAndAdmitTheirDefaults(t *testing.T) {
	cfg := config.NewConfig()
	set := config.NewSetConfig()
	set.Id, set.Name = "set-1", "video"
	cfg.Sets = []*config.SetConfig{&set}

	for path, r := range mcpRanges {
		if !mcpPathAllowed(path) {
			t.Errorf("%s is outside the writable roots", path)
			continue
		}
		field, err := mcpResolvePath(&cfg, path, "video")
		if err != nil {
			t.Errorf("%s names no setting: %v", path, err)
			continue
		}
		switch field.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		default:
			t.Errorf("%s is a %s, but the range is checked only for signed integers", path, field.Kind())
			continue
		}
		if r.Min > r.Max {
			t.Errorf("%s has an empty range %d to %d", path, r.Min, r.Max)
		}
		def := field.Int()
		if err := mcpCheckRange(path, strconv.FormatInt(def, 10), def); err != nil {
			t.Errorf("the default %d of %s must stay writable: %v", def, path, err)
		}
	}
}

func TestMCPWritablePathsListTheRangeOfEachRangedSetting(t *testing.T) {
	cfg := mcpTestCfg()
	listed := map[string]mcpPathInfo{}
	for _, p := range mcpWritablePaths(cfg, cfg.Sets[0]) {
		listed[p.Path] = p
	}
	for path, r := range mcpRanges {
		p, ok := listed[path]
		if !ok {
			t.Errorf("%s is range-checked but b4_list_writable_paths does not list it", path)
			continue
		}
		if len(p.Options) != 1 || p.Options[0] != r.String() {
			t.Errorf("b4_list_writable_paths must give the accepted range of %s (%s) before a write is refused, got %v", path, r, p.Options)
		}
	}
	if got := listed[mcpSetPathPrefix+".udp.fake_len"].Options; len(got) != 1 || got[0] != "32 to 1500" {
		t.Errorf("udp.fake_len options = %v, want [32 to 1500]", got)
	}
}

func TestMCPWriteSetNameFollowsTheManageSetRules(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	for _, c := range []struct{ path, value, reason string }{
		{"sets[set-2].name", "VIDEO", "already exists"},
		{"sets[video].name", "a,b", "comma"},
		{"sets[video].name", "   ", "empty"},
	} {
		res := callSetValue(t, session, ctx, c.path, c.value)
		if !res.IsError || !strings.Contains(mcpErrorText(res), c.reason) {
			t.Errorf("%s=%q must be refused (%s): %s", c.path, c.value, c.reason, mcpErrorText(res))
		}
	}
	if live := api.getCfg(); live.Sets[0].Name != "video" || live.Sets[1].Name != "disabled-set" {
		t.Fatalf("refused renames changed the names: %q, %q", live.Sets[0].Name, live.Sets[1].Name)
	}

	if out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].name", "VIDEO")); !out.Changed || out.Current != "VIDEO" {
		t.Errorf("renaming a set to a case variant of its own name is allowed: %+v", out)
	}
	if out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[set-2].name", "  clips  ")); !out.Changed || out.Current != "clips" {
		t.Errorf("a new name is stored trimmed: %+v", out)
	}
}

func TestMCPWriteReportsTheSavedValueNotALaterWritersOne(t *testing.T) {
	for _, c := range []struct{ name, other string }{
		{"another value", "7443"},
		{"the old value", "8443"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := writableCfg(t)
			cfg.Sets[0].TCP.DPortFilter = "8443"
			srv, api := newMCPTestServerAPI(t, cfg)
			session, ctx := connectMCP(t, srv)

			old := tablesRefreshFunc
			tablesRefreshFunc = func() error {
				next := api.getCfg().Clone()
				next.Sets[0].TCP.DPortFilter = c.other
				return api.saveAndPushConfig(next)
			}
			t.Cleanup(func() { tablesRefreshFunc = old })

			out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].tcp.dport_filter", "9443"))
			if !out.Changed || out.Current != "9443" || strings.Contains(out.Note, "normalised") || strings.Contains(out.Note, "not applied") {
				t.Fatalf("b4 saved 9443, and another writer's later value must not be reported as b4's: %+v", out)
			}
			if len(mcpHistory) != 1 {
				t.Errorf("the saved change must be undoable, history = %d", len(mcpHistory))
			}
		})
	}
}

func TestMCPWriteReportsWhatTheSaveChangedElsewhere(t *testing.T) {
	t.Run("escalation cleared on another set", func(t *testing.T) {
		cfg := writableCfg(t)
		target := config.NewSetConfig()
		target.Id, target.Name, target.Enabled = "set-tgt", "tgt", true
		target.UDP.DPortFilter = "50000-50100"
		cfg.Sets = append(cfg.Sets, &target)
		cfg.Sets[0].Escalate.To = "set-tgt"
		srv, api := newMCPTestServerAPI(t, cfg)
		session, ctx := connectMCP(t, srv)

		out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[tgt].udp.filter_l7", "discord"))
		if !out.Changed {
			t.Fatalf("write did not take: %+v", out)
		}
		if got := api.getCfg().Sets[0].Escalate.To; got != "" {
			t.Fatalf("precondition: saving clears an escalation to a set that selects UDP protocols, got %q", got)
		}
		for _, want := range []string{`escalate.to on set "video"`, `"tgt" now selects UDP protocols`} {
			if !strings.Contains(out.Note, want) {
				t.Errorf("the note must say the save also cleared that escalation (%s): %q", want, out.Note)
			}
		}
	})

	t.Run("watchdog switched off", func(t *testing.T) {
		cfg := writableCfg(t)
		cfg.Sets[0].Discovery.URLs = []string{"https://www.youtube.com/"}
		cfg.Sets[0].Discovery.Watchdog = true
		srv, api := newMCPTestServerAPI(t, cfg)
		session, ctx := connectMCP(t, srv)

		out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].routing.enabled", "true"))
		if !out.Changed {
			t.Fatalf("write did not take: %+v", out)
		}
		if api.getCfg().Sets[0].Discovery.Watchdog {
			t.Fatal("precondition: saving switches the watchdog of a routed set off")
		}
		if !strings.Contains(out.Note, `watchdog of set "video"`) || !strings.Contains(out.Note, "routes its traffic") {
			t.Errorf("the note must say the save also switched the watchdog off, and why: %q", out.Note)
		}
	})

	t.Run("nothing else changed", func(t *testing.T) {
		srv := newMCPTestServer(t, writableCfg(t))
		session, ctx := connectMCP(t, srv)

		out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].tcp.seg2delay", "10"))
		if strings.Contains(out.Note, "Saving also") {
			t.Errorf("a write without side effects must not report one: %q", out.Note)
		}
	})
}

func TestMCPWriteWarnsWhenAPortFilterMakesASetMatchEveryConnection(t *testing.T) {
	cfg := writableCfg(t)
	bare := config.NewSetConfig()
	bare.Id, bare.Name, bare.Enabled = "set-bare", "bare", true
	cfg.Sets = append(cfg.Sets, &bare)
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeSetValue(t, callSetValue(t, session, ctx, "sets[bare].tcp.dport_filter", "443"))
	if !out.Changed {
		t.Fatalf("write did not take: %+v", out)
	}
	if !strings.Contains(out.Note, "EVERY connection on TCP 443") {
		t.Errorf("a port filter on a set without destination targets makes it act on every connection on that port, and the note must say so: %q", out.Note)
	}

	out = decodeSetValue(t, callSetValue(t, session, ctx, "sets[bare].tcp.seg2delay", "10"))
	if strings.Contains(out.Note, "EVERY connection") {
		t.Errorf("a write that does not change what the set matches must not repeat the warning: %q", out.Note)
	}

	out = decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].tcp.dport_filter", "443"))
	if strings.Contains(out.Note, "EVERY connection") {
		t.Errorf("a set with destination targets is not widened by a port filter: %q", out.Note)
	}
}

func TestMCPWriteRefusesWideningAnUnauthenticatedLoopbackSocks5(t *testing.T) {
	loopback := func(t *testing.T) *config.Config {
		cfg := writableCfg(t)
		s5 := &cfg.System.Socks5
		s5.Enabled, s5.BindAddress, s5.Port = true, "127.0.0.1", freeTCPPort(t)
		s5.Username, s5.Password, s5.AllowedSources = "", "", nil
		return cfg
	}

	for _, bind := range []string{"0.0.0.0", "192.168.1.1", ""} {
		srv, api := newMCPTestServerAPI(t, loopback(t))
		session, ctx := connectMCP(t, srv)

		res := callSetValue(t, session, ctx, "system.socks5.bind_address", bind)
		if !res.IsError || !strings.Contains(mcpErrorText(res), "without a password") {
			t.Errorf("moving an unauthenticated SOCKS5 from loopback to %q lets other machines use it and must be refused: %s", bind, mcpErrorText(res))
		}
		if got := api.getCfg().System.Socks5.BindAddress; got != "127.0.0.1" {
			t.Errorf("the refused write moved the server to %q", got)
		}
	}

	for _, bind := range []string{"127.0.0.1", "::1"} {
		before := loopback(t)
		before.System.Socks5.Enabled = false
		before.System.Socks5.BindAddress = bind
		after := before.Clone()
		after.System.Socks5.Enabled = true
		if err := mcpValidateCandidate(before, after); err != nil {
			t.Errorf("an unauthenticated SOCKS5 bound to %s reaches no other machine, so enabling it is allowed: %v", bind, err)
		}
	}
}

func TestMCPWriteRefusesTheTelegramListSources(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	for _, path := range []string{"system.mtproto.cfproxy_url", "system.mtproto.dc_fallback_url"} {
		if res := callSetValue(t, session, ctx, path, "http://192.168.1.50:8080/list"); !res.IsError {
			t.Errorf("%s makes the router fetch any URL on a timer and must be refused over MCP", path)
		}
	}
	mt := api.getCfg().System.MTProto
	if mt.CFProxyURL != config.TGCFProxyURL || mt.DCFallbackURL != config.TGDCFallbackURL {
		t.Fatalf("a refused write changed the list sources: %q, %q", mt.CFProxyURL, mt.DCFallbackURL)
	}
	for _, info := range mcpWritablePaths(api.getCfg(), api.getCfg().Sets[0]) {
		if info.Path == "system.mtproto.cfproxy_url" || info.Path == "system.mtproto.dc_fallback_url" {
			t.Errorf("b4_list_writable_paths offers %s", info.Path)
		}
	}
}

func TestMCPWriteSaysWhenASettingWaitsForARestart(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	for _, c := range []struct{ path, value string }{
		{"system.logging.instaflush", "false"},
		{"system.logging.syslog", "true"},
	} {
		out := decodeSetValue(t, callSetValue(t, session, ctx, c.path, c.value))
		if !out.Changed || !strings.Contains(out.Note, "after b4 restarts") || strings.Contains(out.Note, "applied live") {
			t.Errorf("b4 reads %s only at startup, so the note must not call the change live: %+v", c.path, out)
		}
	}
	if out := decodeSetValue(t, callSetValue(t, session, ctx, "system.socks5.udp_timeout", "45")); !strings.Contains(out.Note, "applied live") {
		t.Errorf("a setting applied live must still say so: %q", out.Note)
	}

	if rev := decodeRevert(t, session, ctx); !strings.Contains(rev.Note, "applied live") {
		t.Errorf("undoing a live setting must say it applied live: %q", rev.Note)
	}
	if rev := decodeRevert(t, session, ctx); !strings.Contains(rev.Note, "after b4 restarts") || strings.Contains(rev.Note, "applied live") {
		t.Errorf("undoing %s restores a setting b4 reads only at startup, so the note must not call it live: %q", rev.Path, rev.Note)
	}
}
