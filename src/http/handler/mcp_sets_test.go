package handler

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
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

func TestMCPManageSetResetKeepsTheLocalDSCP(t *testing.T) {
	cfg := writableCfg(t)
	cfg.Sets[0].Routing = config.RoutingConfig{Enabled: true, Mode: config.RoutingModeProxy, Upstream: config.UpstreamProxyConfig{Host: "10.0.0.9", Port: 1080}}
	cfg.Sets[0].DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
	cfg.Sets[1].DSCP = config.SetDSCPConfig{Enabled: true, Value: 31}
	cfg.Sets[1].Targets.DomainsToMatch = []string{"example.org"}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "reset", "set": "video", "confirm_name": "video",
	}))
	reset := api.getCfg().Sets[0]
	if reset.Routing.Enabled || reset.DSCP != (config.SetDSCPConfig{Enabled: false, Value: 25}) {
		t.Errorf("the reset drops the proxy routing that kept the DSCP value from applying, so the value must stay switched off: routing %+v, dscp %+v", reset.Routing, reset.DSCP)
	}
	if !strings.Contains(out.Note, "DSCP value was kept but switched off") {
		t.Errorf("switching the DSCP value off must be reported: %q", out.Note)
	}

	out = decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "reset", "set": "disabled-set", "confirm_name": "disabled-set",
	}))
	if got := api.getCfg().Sets[1].DSCP; got != (config.SetDSCPConfig{Enabled: true, Value: 31}) {
		t.Errorf("a reset must keep the set's own DSCP value, which MCP cannot write back: %+v", got)
	}
	if strings.Contains(out.Note, "DSCP") {
		t.Errorf("nothing about DSCP changed, so the note must not mention it: %q", out.Note)
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

func TestMCPManageSetDuplicateSwitchesOffARefusedDSCP(t *testing.T) {
	cfg := mcpSecretsCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.Sets[0].DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
	cfg.Sets[0].Targets.DomainsToMatch = []string{"youtube.com"}
	cfg.Sets[1].DSCP = config.SetDSCPConfig{Enabled: true, Value: 31}
	cfg.Sets[1].Targets.DomainsToMatch = []string{"example.org"}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	byName := func(name string) *config.SetConfig {
		for _, s := range api.getCfg().Sets {
			if s.Name == name {
				return s
			}
		}
		t.Fatalf("no set named %q", name)
		return nil
	}

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "duplicate", "set": "video", "name": "video copy",
	}))
	if got := byName("video copy").DSCP; got != (config.SetDSCPConfig{Enabled: false, Value: 25}) {
		t.Errorf("a copy without the proxy routing that kept the value from applying must carry it switched off, got %+v", got)
	}
	if !strings.Contains(out.Note, "DSCP value was copied switched off") {
		t.Errorf("switching the copy's DSCP value off must be reported: %q", out.Note)
	}
	if got := byName("video").DSCP; got != (config.SetDSCPConfig{Enabled: true, Value: 25}) {
		t.Errorf("the original's DSCP value changed: %+v", got)
	}

	out = decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "duplicate", "set": "disabled-set", "name": "plain copy",
	}))
	if got := byName("plain copy").DSCP; got != (config.SetDSCPConfig{Enabled: true, Value: 31}) {
		t.Errorf("a DSCP value that applied to the original must be copied as it is, got %+v", got)
	}
	if strings.Contains(out.Note, "DSCP") {
		t.Errorf("nothing about DSCP changed, so the note must not mention it: %q", out.Note)
	}
}

func TestMCPManageSetDuplicateStartsWithIPPriorityOff(t *testing.T) {
	cfg := writableCfg(t)
	cfg.Sets[0].Targets.IPs = []string{"203.0.113.8/29"}
	cfg.Sets[0].Targets.IPPriority = true
	cfg.Sets[1].Targets.IPs = []string{"198.51.100.0/24"}
	srv, api := newMCPTestServerAPI(t, cfg)
	t.Cleanup(mcpResetHistory)
	session, ctx := connectMCP(t, srv)

	byName := func(name string) *config.SetConfig {
		for _, s := range api.getCfg().Sets {
			if s.Name == name {
				return s
			}
		}
		t.Fatalf("no set named %q", name)
		return nil
	}

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "duplicate", "set": "video", "name": "video copy", "position": "first",
	}))
	if byName("video copy").Targets.IPPriority {
		t.Error("a copy must start with ip_priority off: placed first it would keep the original's addresses from every set below it that matches them by name")
	}
	if !byName("video").Targets.IPPriority {
		t.Error("the original lost its ip_priority")
	}
	if !strings.Contains(out.Note, "targets.ip_priority was copied switched off") {
		t.Errorf("switching the copy's ip_priority off must be reported: %q", out.Note)
	}

	out = decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "duplicate", "set": "disabled-set", "name": "plain copy",
	}))
	if byName("plain copy").Targets.IPPriority {
		t.Error("a copy of a set without ip_priority came out with it on")
	}
	if strings.Contains(out.Note, "ip_priority") {
		t.Errorf("the source had ip_priority off, so the note must not mention it: %q", out.Note)
	}
}

func TestMCPManageSetDuplicateOfATLSFilteredSetTurnsIPPriorityOffSilently(t *testing.T) {
	cfg := writableCfg(t)
	cfg.Sets[0].Targets.IPs = []string{"203.0.113.8/29"}
	cfg.Sets[0].Targets.IPPriority = true
	cfg.Sets[0].Targets.TLSVersion = "1.2"
	srv, api := newMCPTestServerAPI(t, cfg)
	t.Cleanup(mcpResetHistory)
	session, ctx := connectMCP(t, srv)

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "duplicate", "set": "video", "name": "video copy", "position": "first",
	}))
	for _, s := range api.getCfg().Sets {
		if s.Name == "video copy" && s.Targets.IPPriority {
			t.Error("a copy must start with ip_priority off")
		}
	}
	if strings.Contains(out.Note, "ip_priority") {
		t.Errorf("ip_priority has no effect on a set with a TLS version filter, so the copy loses nothing and the note must not mention it: %q", out.Note)
	}
}

func TestMCPManageSetDuplicatePlacedAboveAnIPPrioritySetSaysTheSourceStopsKeepingItsAddresses(t *testing.T) {
	const (
		lost   = "no longer keeps them"
		device = "aa:bb:cc:dd:ee:01"
	)
	ips := []string{"203.0.113.8/29"}
	nameSetAbove := func(cfg *config.Config) {
		gw := &config.SetConfig{Id: "set-gw", Name: "gw", Enabled: true, Targets: config.TargetsConfig{SNIDomains: []string{"googlevideo.com"}}}
		cfg.Sets[1].Enabled = false
		cfg.Sets = append([]*config.SetConfig{gw}, cfg.Sets...)
	}
	cases := []struct {
		name, position string
		ips            []string
		disabled       bool
		layout         func(cfg *config.Config)
		client         string
		neverHeld      bool
		wins, says     bool
	}{
		{name: "first", position: "first", ips: ips, wins: true, says: true},
		{name: "right before the source", position: "before:video", ips: ips, wins: true, says: true},
		{name: "last, the default", ips: ips},
		{name: "right after the source", position: "after:video", ips: ips},
		{name: "first, from a source without addresses", position: "first", neverHeld: true},
		{name: "first, from a source whose only range is /0", position: "first", ips: []string{"0.0.0.0/0"}, neverHeld: true, wins: true},
		{name: "first, from a source with a /0 range and a narrower one", position: "first", ips: []string{"0.0.0.0/0", "203.0.113.8/29"}, wins: true, says: true},
		{name: "first, from a disabled source", position: "first", ips: ips, disabled: true},
		{name: "first, from a source below the only name set", position: "first", ips: ips, layout: nameSetAbove, wins: true},
		{name: "first, from a source limited to a device below a name set for every device", position: "first", ips: ips, layout: func(cfg *config.Config) {
			nameSetAbove(cfg)
			cfg.Sets[1].Targets.SourceDevices = []string{device}
		}, client: device, wins: true, says: true},
		{name: "first, from a source above a name set limited to a device", position: "first", ips: ips, layout: func(cfg *config.Config) {
			cfg.Sets[1].Targets.SourceDevices = []string{device}
		}, wins: true},
		{name: "right before the source, below a set without the switch that owns the same range", position: "before:video", ips: ips, layout: func(cfg *config.Config) {
			upper := &config.SetConfig{Id: "set-upper", Name: "upper", Enabled: true, Targets: config.TargetsConfig{IPs: ips, IpsToMatch: ips}}
			cfg.Sets = append([]*config.SetConfig{upper}, cfg.Sets...)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := writableCfg(t)
			cfg.Sets[0].Enabled = !tc.disabled
			cfg.Sets[0].Targets.IPs = tc.ips
			cfg.Sets[0].Targets.IpsToMatch = tc.ips
			cfg.Sets[0].Targets.IPPriority = true
			cfg.Sets[1].Enabled = true
			if tc.layout != nil {
				tc.layout(cfg)
			}
			srv, api := newMCPTestServerAPI(t, cfg)
			t.Cleanup(mcpResetHistory)
			session, ctx := connectMCP(t, srv)

			args := map[string]any{"action": "duplicate", "set": "video", "name": "video copy"}
			if tc.position != "" {
				args["position"] = tc.position
			}
			out := decodeManageSet(t, callManageSet(t, session, ctx, args))
			if got := strings.Contains(out.Note, "targets.ip_priority was copied switched off"); got == tc.neverHeld {
				t.Errorf("note says the copy's ip_priority was switched off = %v, want %v: a source that lists no range narrower than /0 never held, so its copy loses nothing: %q", got, !tc.neverHeld, out.Note)
			}
			if got := strings.Contains(out.Note, lost); got != tc.says {
				t.Errorf("note says the source stops keeping its addresses = %v, want %v: %q", got, tc.says, out.Note)
			}
			if strings.Contains(out.Note, "the copy's switch is turned on") {
				t.Errorf("the copy's switch would hold these addresses against every set between it and the source, so the note must not offer it as a remedy: %q", out.Note)
			}

			saved := api.getCfg().Clone()
			for _, s := range saved.Sets {
				s.Targets.IpsToMatch = s.Targets.IPs
			}
			_, winner, _ := sni.NewSuffixSet(saved.Sets).MatchIPWithSourceDetail(net.ParseIP("203.0.113.9"), tc.client)
			if copyWins := winner != nil && winner.Name == "video copy"; copyWins != tc.wins {
				t.Errorf("the copy wins the source's address = %v, want %v", copyWins, tc.wins)
			}
		})
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

func TestMCPManageSetNoOpMoveAndResetWriteNothing(t *testing.T) {
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	t.Cleanup(mcpResetHistory)
	session, ctx := connectMCP(t, srv)

	if first := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "reset", "set": "video", "confirm_name": "video",
	})); !first.Changed {
		t.Fatalf("precondition: the first reset changes the set: %+v", first)
	}
	saved, history := api.getCfg(), len(mcpHistory)

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"move the first set to first", map[string]any{"action": "move", "set": "video", "position": "first"}, "already at position 1 of 2"},
		{"move the last set to last", map[string]any{"action": "move", "set": "disabled-set", "position": "last"}, "already at position 2 of 2"},
		{"move a set before the one it precedes", map[string]any{"action": "move", "set": "video", "position": "before:disabled-set"}, "already at position 1 of 2"},
		{"move a set after the one it follows", map[string]any{"action": "move", "set": "disabled-set", "position": "after:video"}, "already at position 2 of 2"},
		{"reset a set already at its defaults", map[string]any{"action": "reset", "set": "video", "confirm_name": "video"}, "already at its default"},
	} {
		out := decodeManageSet(t, callManageSet(t, session, ctx, tc.args))
		if out.Changed {
			t.Errorf("%s changes nothing, so it must report changed=false: %+v", tc.name, out)
		}
		if !strings.Contains(out.Note, tc.want) || !strings.Contains(out.Note, "nothing was written") {
			t.Errorf("%s: the note should say %q and that nothing was written: %q", tc.name, tc.want, out.Note)
		}
		if out.TotalSets != 2 || len(out.Order) != 2 || out.Set == nil {
			t.Errorf("%s: the result should still report the set and the order: %+v", tc.name, out)
		}
	}

	if api.getCfg() != saved {
		t.Error("a no-op must not save the configuration")
	}
	if len(mcpHistory) != history {
		t.Errorf("a no-op must not take an undo slot: history went from %d to %d", history, len(mcpHistory))
	}
	if got := setNames(api.getCfg()); got[0] != "video" || got[1] != "disabled-set" {
		t.Errorf("order = %v", got)
	}
}

func TestMCPManageSetReportsTheSavedSetsNotAConcurrentWritersOnes(t *testing.T) {
	cfg := writableCfg(t)
	cfg.Sets[1].Enabled = true
	cfg.Sets[1].TCP.DPortFilter = "8443"
	srv, api := newMCPTestServerAPI(t, cfg)
	t.Cleanup(mcpResetHistory)
	session, ctx := connectMCP(t, srv)

	refreshes := 0
	prev := tablesRefreshFunc
	tablesRefreshFunc = func() error {
		refreshes++
		next := api.getCfg().Clone()
		extra := config.NewSetConfig()
		extra.Id, extra.Name = "set-9", "added elsewhere"
		next.Sets = append(next.Sets, &extra)
		return api.saveAndPushConfig(next)
	}
	t.Cleanup(func() { tablesRefreshFunc = prev })

	out := decodeManageSet(t, callManageSet(t, session, ctx, map[string]any{
		"action": "delete", "set": "disabled-set", "confirm_name": "disabled-set",
	}))
	if refreshes != 1 {
		t.Fatalf("precondition: deleting the set that owns TCP port 8443 refreshes the firewall once, got %d", refreshes)
	}
	if !out.Changed || out.TotalSets != 1 || len(out.Order) != 1 || !strings.Contains(out.Note, "1 set(s) configured") {
		t.Errorf("b4 saved one set, so that is what the result must report: %+v", out)
	}
	if len(mcpHistory) != 1 || mcpHistory[0].Current != "1 sets" {
		currents := make([]string, 0, len(mcpHistory))
		for _, c := range mcpHistory {
			currents = append(currents, c.Current)
		}
		t.Errorf("the undo list must hold one entry describing the saved configuration, got %q", currents)
	}
}
