package handler

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/http/apispec"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() {
	mcpAPILoad = func() (*apispec.Spec, error) {
		raw, err := os.ReadFile("../../../docs/static/swagger.json")
		if err != nil {
			return nil, err
		}
		return apispec.Parse(raw)
	}
}

func callAPIReference(t *testing.T, session *mcp.ClientSession, ctx context.Context, args map[string]any) (*mcp.CallToolResult, mcpAPIOut) {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_api_reference", Arguments: args})
	if err != nil {
		t.Fatalf("call b4_api_reference: %v", err)
	}
	var out mcpAPIOut
	if !res.IsError {
		if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
			t.Fatalf("decode b4_api_reference: %v", err)
		}
	}
	return res, out
}

func TestMCPAPIReferenceIsServedWithoutPermissions(t *testing.T) {
	session, ctx := connectMCP(t, newMCPTestServer(t, mcpTestCfg()))
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "b4_api_reference" {
			continue
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Error("b4_api_reference should be annotated read-only")
		}
		if tool.OutputSchema != nil {
			t.Error("b4_api_reference should publish no output schema: it would cost tools/list bytes on every connection")
		}
		return
	}
	t.Fatal("b4_api_reference is not served while writes and probes are off")
}

func TestMCPAPIReferenceListsEveryOperationByTag(t *testing.T) {
	spec, err := mcpAPILoad()
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	session, ctx := connectMCP(t, newMCPTestServer(t, mcpTestCfg()))
	res, out := callAPIReference(t, session, ctx, nil)
	if res.IsError {
		t.Fatalf("index refused: %s", mcpErrorText(res))
	}

	if want := len(spec.Operations()); out.Count != want {
		t.Errorf("count = %d, want %d", out.Count, want)
	}
	lines := 0
	for _, group := range out.Operations {
		lines += len(group)
	}
	if lines != out.Count {
		t.Errorf("%d lines for %d operations", lines, out.Count)
	}

	found := false
	for _, line := range out.Operations["Sets"] {
		if strings.HasPrefix(line, "GET /api/sets - ") {
			found = true
			if !strings.Contains(line, "(prefer b4_list_sets)") {
				t.Errorf("the line for GET /api/sets names no MCP tool: %q", line)
			}
		}
	}
	if !found {
		t.Errorf("no GET /api/sets line under Sets: %v", out.Operations["Sets"])
	}

	if out.APIVersion != spec.Version() || out.B4Version != Version {
		t.Errorf("versions = %q and %q", out.APIVersion, out.B4Version)
	}
	for _, want := range []string{"Bearer", "POST /api/auth/login", "ask the user", "beside /api/mcp", "switched off " + mcpSwitchWhere} {
		if !strings.Contains(out.Note, want) {
			t.Errorf("note lacks %q: %s", want, out.Note)
		}
	}
}

func TestMCPAPIReferenceSaysWhenTheWebLoginIsOff(t *testing.T) {
	cfg := mcpTestCfg()
	cfg.System.WebServer.Username = ""
	cfg.System.WebServer.Password = ""
	session, ctx := connectMCP(t, newMCPTestServer(t, cfg))
	_, out := callAPIReference(t, session, ctx, nil)
	if !strings.Contains(out.Note, "web login is off, so REST calls need no token") || strings.Contains(out.Note, "Bearer") {
		t.Errorf("note should say no token is needed: %s", out.Note)
	}
}

func TestMCPAPIReferenceDescribesOneOperation(t *testing.T) {
	session, ctx := connectMCP(t, newMCPTestServer(t, mcpTestCfg()))
	res, out := callAPIReference(t, session, ctx, map[string]any{"operation": "put /api/sets/set-1"})
	if res.IsError {
		t.Fatalf("operation refused: %s", mcpErrorText(res))
	}
	if out.Operation == nil || out.Operation.Method != "PUT" || out.Operation.Path != "/api/sets/{id}" {
		t.Fatalf("operation = %+v", out.Operation)
	}
	if out.PreferTool != "b4_set_config_value, one setting per call" {
		t.Errorf("prefer_tool = %q", out.PreferTool)
	}
	if _, ok := out.Schemas["handler.SetWithRevision"]; !ok {
		t.Errorf("the body type is not expanded: %v", out.Schemas)
	}
	if size := len(mustStructured(t, res)); size > 3*mcpAPISchemaBudget {
		t.Errorf("one operation took %d bytes", size)
	}
	for _, want := range []string{"b4_get_topic", "b4_set_config_value is not offered right now", mcpAPIWritesSwitch, mcpSwitchWhere, mcpSwitchReconnect} {
		if !strings.Contains(out.Note, want) {
			t.Errorf("note lacks %q: %s", want, out.Note)
		}
	}

	writes := mcpTestCfg()
	writes.System.WebServer.MCP.AllowWrites = true
	session, ctx = connectMCP(t, newMCPTestServer(t, writes))
	_, out = callAPIReference(t, session, ctx, map[string]any{"operation": "PUT /api/sets/{id}"})
	if strings.Contains(out.Note, "not offered right now") {
		t.Errorf("with writes on the note should not call b4_set_config_value switched off: %s", out.Note)
	}
}

func TestMCPAPIReferenceRefusesBadInput(t *testing.T) {
	session, ctx := connectMCP(t, newMCPTestServer(t, mcpTestCfg()))
	for _, c := range []struct {
		args  map[string]any
		wants []string
	}{
		{map[string]any{"operation": "GET /api/sets/a/b/c"}, []string{"no operation at /api/sets/a/b/c", "under /api/sets", "query=<words>"}},
		{map[string]any{"operation": "/api/sets/{id}"}, []string{"give the method too"}},
		{map[string]any{"schema": "no.SuchType"}, []string{"no schema named"}},
		{map[string]any{"operation": "GET /api/sets", "query": "sets"}, []string{"at most one of operation, query or schema"}},
	} {
		res, _ := callAPIReference(t, session, ctx, c.args)
		if !res.IsError {
			t.Errorf("%v was accepted", c.args)
			continue
		}
		text := mcpErrorText(res)
		for _, want := range c.wants {
			if !strings.Contains(text, want) {
				t.Errorf("%v: error %q lacks %q", c.args, text, want)
			}
		}
	}
}

func TestMCPAPIReferenceSearchesAndExpandsSchemas(t *testing.T) {
	session, ctx := connectMCP(t, newMCPTestServer(t, mcpTestCfg()))

	_, out := callAPIReference(t, session, ctx, map[string]any{"query": "mtproto"})
	if out.Count == 0 || len(out.Operations["MTProto"]) == 0 {
		t.Errorf("query mtproto found %d operations: %v", out.Count, out.Operations)
	}

	_, out = callAPIReference(t, session, ctx, map[string]any{"query": "no-such-word-anywhere"})
	if out.Count != 0 || !strings.Contains(out.Note, "no operation matches") {
		t.Errorf("an empty search should say so: %+v", out)
	}

	_, out = callAPIReference(t, session, ctx, map[string]any{"schema": "config.SetConfig"})
	if _, ok := out.Schemas["config.SetConfig"].(map[string]any); !ok {
		t.Errorf("config.SetConfig not expanded into fields: %v", out.Schemas["config.SetConfig"])
	}
	if !strings.Contains(out.Note, "b4_get_topic") {
		t.Errorf("a set's fields should point at b4_get_topic: %s", out.Note)
	}
}

func TestMCPAPIPreferredToolsExistInTheSpecAndTheServer(t *testing.T) {
	spec, err := mcpAPILoad()
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	session, ctx := connectMCP(t, newMCPTestServer(t, mcpTestCfg()))
	served := toolNames(t, session, ctx)

	full := mcpTestCfg()
	full.System.WebServer.MCP.AllowWrites = true
	full.System.WebServer.MCP.AllowActiveProbes = true
	fullSession, fullCtx := connectMCP(t, newMCPTestServer(t, full))
	servedFull := toolNames(t, fullSession, fullCtx)

	for ref, hint := range mcpAPIPreferredTools {
		op, err := spec.Find(ref)
		if err != nil {
			t.Errorf("%s is not in the built-in spec: %v", ref, err)
		} else if got := op.Method + " " + op.Path; got != ref {
			t.Errorf("%s resolves to %s: key it by the spec's own path", ref, got)
		}
		if !servedFull[hint.tool] {
			t.Errorf("%s points at %s, which b4 does not serve", ref, hint.tool)
		}
		if gated := mcpAPIToolNeeds(hint.tool) != ""; served[hint.tool] == gated {
			t.Errorf("%s: served without permissions = %v, but mcpAPIToolNeeds says gated = %v", hint.tool, served[hint.tool], gated)
		}
	}
}

func TestMCPAPIVersionNoteFlagsAMismatch(t *testing.T) {
	if note := mcpAPIVersionNote("1.88.0", "1.88.2"); !strings.Contains(note, "built for b4 1.88.0 and this b4 is 1.88.2") {
		t.Errorf("mismatch note = %q", note)
	}
	for _, running := range []string{"1.88.0", "dev", "1.0.0", ""} {
		if note := mcpAPIVersionNote("1.88.0", running); note != "" {
			t.Errorf("running %q: unexpected note %q", running, note)
		}
	}
}
