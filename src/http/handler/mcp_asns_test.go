package handler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPEditTargetsAddsAndRemovesASNs(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)
	refreshedAfterSave := watchASNRefreshes(t, func() bool { return configReferencesASN(api.getCfg(), "44907") })

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "asns", "add": "AS62041, 44907",
	}))
	if !out.Changed || !reflect.DeepEqual(out.Added, []string{"62041", "44907"}) {
		t.Fatalf("expected both ASNs added in canonical form: %+v", out)
	}
	if len(out.Rewritten) != 1 || !strings.Contains(out.Rewritten[0], "AS62041 -> 62041") {
		t.Errorf("dropping the AS prefix must be reported: %+v", out.Rewritten)
	}
	live := api.getCfg().Sets[0].Targets
	if !reflect.DeepEqual(live.ASNs, []string{"62041", "44907"}) {
		t.Fatalf("ASNs not saved: %v", live.ASNs)
	}
	if !reflect.DeepEqual(live.IpsToMatch, telegramPrefixes) {
		t.Errorf("the resolved ASN must expand into the match list: %v", live.IpsToMatch)
	}
	if out.Expansion == nil || out.Expansion.IPs != len(telegramPrefixes) || !reflect.DeepEqual(out.Expansion.UnresolvedASNs, []string{"44907"}) {
		t.Errorf("the expansion must count the prefixes and name the unresolved ASN: %+v", out.Expansion)
	}
	if !strings.Contains(out.Note, "AS44907 has no known prefixes yet") {
		t.Errorf("the note must say the ASN matches nothing yet: %q", out.Note)
	}
	if !strings.Contains(out.Note, "4 of them prefixes announced by its ASNs") {
		t.Errorf("the note must say how many addresses the ASNs contribute: %q", out.Note)
	}
	if !refreshedAfterSave() {
		t.Error("the refresher must be asked to resolve the new ASN once the set is saved")
	}
	if len(mcpHistory) != 1 {
		t.Fatalf("the edit must be undoable, history = %d", len(mcpHistory))
	}

	again := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "asns", "add": "as44907",
	}))
	if again.Changed || !reflect.DeepEqual(again.AlreadySet, []string{"44907"}) {
		t.Errorf("an ASN already listed under another spelling is already present: %+v", again)
	}

	back := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "asns", "remove": "AS62041",
	}))
	if !reflect.DeepEqual(back.Removed, []string{"62041"}) {
		t.Errorf("removal must match the canonical entry: %+v", back)
	}
	live = api.getCfg().Sets[0].Targets
	if !reflect.DeepEqual(live.ASNs, []string{"44907"}) || len(live.IpsToMatch) != 0 {
		t.Errorf("after removal only the unresolved ASN remains and nothing is matched: %v %v", live.ASNs, live.IpsToMatch)
	}
}

func TestMCPEditTargetsRefusesInvalidASNs(t *testing.T) {
	useAsnStore(t)
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	for _, bad := range []string{"64512", "AS0", "23456", "4200000001", "google", "AS15169x"} {
		res := callEditTargets(t, session, ctx, map[string]any{
			"set": "video", "kind": "asns", "add": "15169, " + bad,
		})
		if !res.IsError {
			t.Errorf("%q must be refused", bad)
			continue
		}
		if !strings.Contains(mcpErrorText(res), "AS number") {
			t.Errorf("the refusal for %q must say what is expected: %q", bad, mcpErrorText(res))
		}
	}
	if got := api.getCfg().Sets[0].Targets.ASNs; len(got) != 0 {
		t.Errorf("a refused call must write nothing: %v", got)
	}
}

func TestMCPSetValueCannotBypassTheASNList(t *testing.T) {
	useAsnStore(t)
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callSetValue(t, session, ctx, "sets[video].targets.asns", "64512,15169")
	if !res.IsError {
		t.Fatal("targets.asns must be refused by b4_set_config_value")
	}
	if !strings.Contains(mcpErrorText(res), "b4_edit_set_targets") {
		t.Errorf("the refusal must name the tool that edits the list: %q", mcpErrorText(res))
	}
	if got := api.getCfg().Sets[0].Targets.ASNs; len(got) != 0 {
		t.Errorf("targets.asns must be untouched: %v", got)
	}

	list, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_list_writable_paths",
		Arguments: map[string]any{"prefix": "sets[].targets"},
	})
	if err != nil {
		t.Fatalf("list writable paths: %v", err)
	}
	var paths mcpListPathsOut
	if err := json.Unmarshal(mustStructured(t, list), &paths); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, p := range paths.Paths {
		if p.Path == mcpSetPathPrefix+".targets.asns" {
			t.Errorf("%s must not be advertised as writable", p.Path)
		}
	}
	if !mcpPathIsTargetList(mcpSetPathPrefix + ".targets.asns") {
		t.Error("targets.asns must be classified as a target list")
	}
}

func TestMCPEditTargetsToolDescribesASNs(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "b4_edit_set_targets" {
			continue
		}
		raw, _ := json.Marshal(tool)
		if !strings.Contains(tool.Description, "AS number") || !strings.Contains(string(raw), "asns") {
			t.Errorf("the tool must tell the model about the asns kind: %s", raw)
		}
		return
	}
	t.Fatal("b4_edit_set_targets is not offered")
}

func TestMCPExpansionNoteNamesUnresolvedASNs(t *testing.T) {
	note := mcpExpansionNote(&mcpTargetExpansion{Domains: 1, IPs: 7, ASNPrefixes: 3, UnresolvedASNs: []string{"15169", "13335"}})
	for _, want := range []string{"7 addresses (3 of them prefixes announced by its ASNs)", "AS15169, AS13335 have no known prefixes yet"} {
		if !strings.Contains(note, want) {
			t.Errorf("note %q lacks %q", note, want)
		}
	}
	plain := mcpExpansionNote(&mcpTargetExpansion{Domains: 2, IPs: 1})
	if plain != "the set now matches 2 domains and 1 addresses." {
		t.Errorf("a set without ASNs keeps the old note, got %q", plain)
	}
}
