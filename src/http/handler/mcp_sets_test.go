package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callManageSet(t *testing.T, session *mcp.ClientSession, ctx context.Context, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_manage_set", Arguments: args})
	if err != nil {
		t.Fatalf("call b4_manage_set %v: %v", args, err)
	}
	return res
}

func decodeManageSet(t *testing.T, res *mcp.CallToolResult) mcpManageSetOut {
	t.Helper()
	if res.IsError {
		t.Fatalf("b4_manage_set returned an error: %+v", res.Content)
	}
	var out mcpManageSetOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func setNames(cfg *config.Config) []string {
	out := make([]string, 0, len(cfg.Sets))
	for _, s := range cfg.Sets {
		out = append(out, s.Name)
	}
	return out
}

func TestMCPManageSetCreateLandsLastNotFirst(t *testing.T) {
	cfg := writableCfg(t)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "create", "name": "new-set",
	}))
	if !out.Changed || out.Set == nil {
		t.Fatalf("create failed: %+v", out)
	}
	if out.Set.Position != 3 || out.TotalSets != 3 {
		t.Fatalf("a new set must land last, got position %d of %d", out.Set.Position, out.TotalSets)
	}
	if got := setNames(api.getCfg()); got[2] != "new-set" {
		t.Fatalf("order = %v", got)
	}
	if !strings.Contains(out.Note, "matches nothing") {
		t.Errorf("a set with no targets matches nothing and the note should say so: %q", out.Note)
	}

	first := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "create", "name": "top", "position": "first",
	}))
	if first.Set.Position != 1 {
		t.Fatalf("position=first should put it at 1, got %d", first.Set.Position)
	}
	if got := setNames(api.getCfg()); got[0] != "top" {
		t.Fatalf("order = %v", got)
	}
}

func TestMCPManageSetCreateUsesRealDefaults(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "create", "name": "fresh",
	}))

	var created *config.SetConfig
	for _, s := range api.getCfg().Sets {
		if s.Name == "fresh" {
			created = s
		}
	}
	if created == nil {
		t.Fatal("set was not created")
	}
	if !created.Enabled {
		t.Error("a set created on request should be enabled")
	}
	if created.Fragmentation.Strategy != config.DefaultSetConfig.Fragmentation.Strategy {
		t.Errorf("strategy = %q, want the default %q",
			created.Fragmentation.Strategy, config.DefaultSetConfig.Fragmentation.Strategy)
	}
	if created.TCP.ConnBytesLimit != config.DefaultSetConfig.TCP.ConnBytesLimit {
		t.Errorf("conn_bytes_limit = %d, want the default %d",
			created.TCP.ConnBytesLimit, config.DefaultSetConfig.TCP.ConnBytesLimit)
	}
}

func TestMCPManageSetMove(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "move", "set": "video", "position": "after:disabled-set",
	}))
	if out.Set.Position != 2 {
		t.Fatalf("after:disabled-set should be position 2, got %d", out.Set.Position)
	}
	if got := setNames(api.getCfg()); got[0] != "disabled-set" || got[1] != "video" {
		t.Fatalf("order = %v", got)
	}

	back := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "move", "set": "video", "position": "before:disabled-set",
	}))
	if back.Set.Position != 1 {
		t.Fatalf("before:disabled-set should be position 1, got %d", back.Set.Position)
	}

	for name, args := range map[string]map[string]any{
		"no position":    {"action": "move", "set": "video"},
		"unknown anchor": {"action": "move", "set": "video", "position": "after:nope"},
		"bad spec":       {"action": "move", "set": "video", "position": "middle"},
		"unknown set":    {"action": "move", "set": "nope", "position": "first"},
	} {
		if res := callManageSet(t, session, ctx, args); !res.IsError {
			t.Errorf("%s should be refused", name)
		}
	}
}

func TestMCPManageSetDuplicateDoesNotCarrySecrets(t *testing.T) {
	cfg := mcpSecretsCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.Sets[0].DNS.Pins = map[string][]string{"example.com": {"1.2.3.4"}}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "duplicate", "set": "video", "name": "video copy",
	}))
	if !out.Changed {
		t.Fatalf("duplicate failed: %+v", out)
	}

	var copySet *config.SetConfig
	for _, s := range api.getCfg().Sets {
		if s.Name == "video copy" {
			copySet = s
		}
	}
	if copySet == nil {
		t.Fatal("the copy was not created")
	}
	if copySet.Routing.Upstream.Username != "" || copySet.Routing.Upstream.Password != "" {
		t.Errorf("the copy carried upstream credentials: %+v", copySet.Routing.Upstream)
	}
	if strings.Contains(copySet.Routing.Upstream.Password, redactedMarker) {
		t.Error("the redaction marker must not be stored as a credential")
	}
	if len(copySet.DNS.Pins) != 0 {
		t.Errorf("the copy carried DNS pins: %v", copySet.DNS.Pins)
	}
	if orig := api.getCfg().Sets[0]; orig.Routing.Upstream.Password != "upstream-pw" {
		t.Errorf("the original lost its credentials: %q", orig.Routing.Upstream.Password)
	}
}

func TestMCPManageSetEnableDisable(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "set_enabled", "set": "video,disabled-set", "enabled": "false",
	}))
	if !out.Changed {
		t.Fatalf("batch disable failed: %+v", out)
	}
	for _, s := range api.getCfg().Sets {
		if s.Enabled {
			t.Errorf("%s should be disabled", s.Name)
		}
	}

	again := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "set_enabled", "set": "video", "enabled": "false",
	}))
	if again.Changed {
		t.Errorf("a no-op should not report a change: %+v", again)
	}

	if res := callManageSet(t, session, ctx, map[string]any{
		"action": "set_enabled", "set": "video", "enabled": "maybe",
	}); !res.IsError {
		t.Error("a non-boolean must be refused")
	}
}

func TestMCPManageSetDeleteNeedsConfirmation(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	res := callManageSet(t, session, ctx, map[string]any{"action": "delete", "set": "video"})
	if !res.IsError {
		t.Fatal("delete without confirm_name must be refused")
	}
	if !strings.Contains(mcpErrorText(res), `confirm_name="video"`) {
		t.Errorf("the refusal should spell out the corrected call: %q", mcpErrorText(res))
	}

	if res := callManageSet(t, session, ctx, map[string]any{
		"action": "delete", "set": "video", "confirm_name": "Video",
	}); !res.IsError {
		t.Error("confirm_name must match exactly, not case-insensitively")
	}
	if len(api.getCfg().Sets) != 2 {
		t.Fatal("nothing should have been deleted yet")
	}

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "delete", "set": "video", "confirm_name": "video",
	}))
	if !out.Changed || out.TotalSets != 1 {
		t.Fatalf("delete failed: %+v", out)
	}
	if got := setNames(api.getCfg()); len(got) != 1 || got[0] != "disabled-set" {
		t.Fatalf("remaining sets = %v", got)
	}
}

func TestMCPManageSetRefusesToDeleteAnEscalationTarget(t *testing.T) {
	cfg := writableCfg(t)
	cfg.Sets[1].Escalate.To = cfg.Sets[0].Id
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callManageSet(t, session, ctx, map[string]any{
		"action": "delete", "set": "video", "confirm_name": "video",
	})
	if !res.IsError {
		t.Fatal("deleting a set another set escalates to must be refused: sanitizeEscalation clears the dangling link silently")
	}
	if !strings.Contains(mcpErrorText(res), "disabled-set") {
		t.Errorf("the refusal should name the set that points at it: %q", mcpErrorText(res))
	}
	if len(api.getCfg().Sets) != 2 {
		t.Error("nothing should have been deleted")
	}
}

func TestMCPManageSetRefusesDeleteAndResetOnProtectedSets(t *testing.T) {
	cfg := mcpSecretsCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	for _, action := range []string{"delete", "reset"} {
		res := callManageSet(t, session, ctx, map[string]any{
			"action": action, "set": "video", "confirm_name": "video",
		})
		if !res.IsError {
			t.Errorf("%s must be refused on a set holding upstream credentials MCP cannot restore", action)
		}
		if !strings.Contains(mcpErrorText(res), "credentials") {
			t.Errorf("%s refusal should say what it is protecting: %q", action, mcpErrorText(res))
		}
	}
	if got := api.getCfg().Sets[0].Routing.Upstream.Password; got != "upstream-pw" {
		t.Errorf("credentials were destroyed: %q", got)
	}
}

func TestMCPManageSetResetKeepsTargets(t *testing.T) {
	cfg := writableCfg(t)
	cfg.Sets[0].TCP.Seg2Delay = 999
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "reset", "set": "video", "confirm_name": "video",
	}))
	if !out.Changed {
		t.Fatalf("reset failed: %+v", out)
	}
	live := api.getCfg().Sets[0]
	if live.TCP.Seg2Delay == 999 {
		t.Error("reset did not restore the defaults")
	}
	if len(live.Targets.SNIDomains) != 1 || live.Targets.SNIDomains[0] != "youtube.com" {
		t.Errorf("reset must keep what the set matches: %v", live.Targets.SNIDomains)
	}
	if len(live.Targets.DomainsToMatch) != 1 {
		t.Errorf("the match list must be re-expanded after a reset: %v", live.Targets.DomainsToMatch)
	}
	if !strings.Contains(out.Note, "Targets and the enabled switch were kept") {
		t.Errorf("note should say targets survived: %q", out.Note)
	}
}

func TestMCPManageSetRevertRestoresEverything(t *testing.T) {
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "delete", "set": "video", "confirm_name": "video",
	}))
	if len(api.getCfg().Sets) != 1 {
		t.Fatal("precondition: the delete should have landed")
	}

	if rev := decodeRevert(t, session, ctx); !rev.Reverted {
		t.Fatalf("a deleted set must be recoverable: %+v", rev)
	}
	live := api.getCfg()
	if len(live.Sets) != 2 {
		t.Fatalf("sets after revert = %v", setNames(live))
	}
	if live.Sets[0].Name != "video" {
		t.Errorf("the set came back in the wrong position: %v", setNames(live))
	}
	if len(live.Sets[0].Targets.DomainsToMatch) != 1 {
		t.Errorf("the restored set's match list must be re-expanded: %v", live.Sets[0].Targets.DomainsToMatch)
	}
}

func TestMCPManageSetRejectsBadInput(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	for name, args := range map[string]map[string]any{
		"no action":      {},
		"unknown action": {"action": "explode"},
		"duplicate none": {"action": "duplicate", "set": "nope"},
		"enable no set":  {"action": "set_enabled", "enabled": "true"},
		"reset unknown":  {"action": "reset", "set": "nope", "confirm_name": "nope"},
	} {
		if res := callManageSet(t, session, ctx, args); !res.IsError {
			t.Errorf("%s should be refused", name)
		}
	}
}

func TestMCPManageSetRefusesCollidingName(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	res := callManageSet(t, session, ctx, map[string]any{"action": "create", "name": "video"})
	if !res.IsError {
		t.Fatal("a second set named 'video' makes every later reference ambiguous and must be refused")
	}
	if len(api.getCfg().Sets) != 2 {
		t.Fatalf("nothing should have been created: %v", setNames(api.getCfg()))
	}

	dup := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "duplicate", "set": "video",
	}))
	if dup.Set.Name != "video copy" {
		t.Fatalf("first duplicate should be %q, got %q", "video copy", dup.Set.Name)
	}
	again := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "duplicate", "set": "video",
	}))
	if again.Set.Name == dup.Set.Name {
		t.Errorf("a second duplicate must not reuse the name %q", again.Set.Name)
	}
}

func TestMCPManageSetRefusesAmbiguousDelete(t *testing.T) {
	cfg := writableCfg(t)
	twin := config.NewSetConfig()
	twin.Id = "set-3"
	twin.Name = "video"
	twin.Enabled = true
	cfg.Sets = append(cfg.Sets, &twin)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	for _, action := range []string{"delete", "reset"} {
		res := callManageSet(t, session, ctx, map[string]any{
			"action": action, "set": "video", "confirm_name": "video",
		})
		if !res.IsError {
			t.Fatalf("%s by an ambiguous name would hit the wrong set and must be refused", action)
		}
		if !strings.Contains(mcpErrorText(res), "set-1") || !strings.Contains(mcpErrorText(res), "set-3") {
			t.Errorf("the refusal should list the colliding ids: %q", mcpErrorText(res))
		}
	}
	if len(api.getCfg().Sets) != 3 {
		t.Fatal("nothing should have been deleted")
	}

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "delete", "set": "set-3", "confirm_name": "video",
	}))
	if !out.Changed || out.TotalSets != 2 {
		t.Fatalf("addressing by id must still work: %+v", out)
	}
	if got := api.getCfg().Sets[0]; got.Id != "set-1" {
		t.Errorf("the wrong set was deleted, survivor is %s", got.Id)
	}
}

func TestMCPManageSetResetKeepsTheEnabledSwitch(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "reset", "set": "disabled-set", "confirm_name": "disabled-set",
	}))
	if out.Set.Enabled {
		t.Fatal("reset must not re-enable a set the operator deliberately disabled")
	}
	if api.getCfg().Sets[1].Enabled {
		t.Fatal("the live set was re-enabled and immediately started claiming its domains again")
	}
	if !strings.Contains(out.Note, "enabled switch") {
		t.Errorf("the note should say the switch survived: %q", out.Note)
	}
}

func TestMCPManageSetDuplicateDropsRoutingAndSaysSo(t *testing.T) {
	cfg := mcpSecretsCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "duplicate", "set": "video", "name": "copy",
	}))

	var copySet *config.SetConfig
	for _, s := range api.getCfg().Sets {
		if s.Name == "copy" {
			copySet = s
		}
	}
	if copySet == nil {
		t.Fatal("the copy was not created")
	}
	if copySet.Routing.Enabled || copySet.Routing.Upstream.Host != "" {
		t.Errorf("a copy that routes to an authenticating proxy without credentials would misroute: %+v", copySet.Routing)
	}
	if !strings.Contains(out.Note, "Routing was NOT copied") {
		t.Errorf("dropping routing must be reported: %q", out.Note)
	}
	if !strings.Contains(out.Note, "whichever sits earlier wins") {
		t.Errorf("a duplicate carries its source's targets and the note must not claim it matches nothing: %q", out.Note)
	}
}

func TestMCPManageSetRefusesACommaInAName(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	before := len(api.getCfg().Sets)
	res := callManageSet(t, session, ctx, map[string]any{"action": "create", "name": "video, audio"})
	if !res.IsError {
		t.Fatal("set_enabled splits its argument on commas, so such a set could never be addressed again")
	}
	if got := len(api.getCfg().Sets); got != before {
		t.Errorf("a refused create must not add a set, got %d from %d", got, before)
	}
}
