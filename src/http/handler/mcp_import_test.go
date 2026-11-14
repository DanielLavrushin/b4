package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/convert"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	importZapretLine  = "--filter-tcp=443 --hostlist-domains=youtube.com --dpi-desync=fake,multisplit --dpi-desync-repeats=6 --dpi-desync-fooling=md5sig"
	importKeeneticCfg = "../../convert/testdata/nfqws-keenetic.conf"
)

func callImport(t *testing.T, session *mcp.ClientSession, ctx context.Context, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: mcpImportTool, Arguments: args})
	if err != nil {
		t.Fatalf("call %s %v: %v", mcpImportTool, args, err)
	}
	return res
}

func decodeImport(t *testing.T, res *mcp.CallToolResult) mcpImportOut {
	t.Helper()
	if res.IsError {
		t.Fatalf("%s returned an error: %s", mcpImportTool, mcpErrorText(res))
	}
	var out mcpImportOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func importSample(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func comparableSets(t *testing.T, cfg *config.Config) string {
	t.Helper()
	index := map[string]string{}
	for i, s := range cfg.Sets {
		index[s.Id] = "set-at-" + string(rune('a'+i))
	}
	out := make([]map[string]any, 0, len(cfg.Sets))
	for _, s := range cfg.Sets {
		m, err := config.SetToMap(s)
		if err != nil {
			t.Fatalf("set to map: %v", err)
		}
		m["id"] = index[s.Id]
		if esc, ok := m["escalate"].(map[string]any); ok {
			if to, _ := esc["to"].(string); to != "" {
				esc["to"] = index[to]
			}
		}
		out = append(out, m)
	}
	raw, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMCPImportStrategyNeedsWrites(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)
	if toolNames(t, session, ctx)[mcpImportTool] {
		t.Errorf("%s is listed with 'Allow configuration changes' off", mcpImportTool)
	}

	srv = newMCPTestServer(t, writableCfg(t))
	session, ctx = connectMCP(t, srv)
	if !toolNames(t, session, ctx)[mcpImportTool] {
		t.Errorf("%s is not listed with 'Allow configuration changes' on", mcpImportTool)
	}
}

func TestMCPImportStrategyPreviewWritesNothing(t *testing.T) {
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)
	before := api.getCfg()
	revision := mcpRevision(before)

	for _, args := range []map[string]any{
		{"text": importZapretLine},
		{"text": importZapretLine, "apply": "false"},
		{"text": importSample(t, importKeeneticCfg)},
	} {
		out := decodeImport(t, callImport(t, session, ctx, args))
		if out.Applied {
			t.Errorf("a call without apply='true' reports applied: %+v", args)
		}
		if !strings.Contains(out.Note, "nothing was written") {
			t.Errorf("the preview note must say nothing was written: %q", out.Note)
		}
	}

	if api.getCfg() != before || mcpRevision(api.getCfg()) != revision {
		t.Fatal("a preview replaced the configuration")
	}
	mcpWriteMu.Lock()
	recorded := len(mcpHistory)
	mcpWriteMu.Unlock()
	if recorded != 0 {
		t.Fatalf("a preview recorded %d change(s) for undo", recorded)
	}
}

func TestMCPImportStrategyPreviewDescribesTheResult(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	out := decodeImport(t, callImport(t, session, ctx, map[string]any{"text": importZapretLine}))
	if out.Tool != "zapret" || out.ToolLabel == "" || out.Version == "" {
		t.Errorf("tool and version are not reported: %+v", out)
	}
	if !out.CanApply || out.Blocked != "" {
		t.Errorf("a line with a strategy must be importable: can_apply=%v blocked=%q", out.CanApply, out.Blocked)
	}
	if out.Fidelity.Options == 0 || out.Fidelity.Score <= 0 || out.Fidelity.Score > 100 {
		t.Errorf("fidelity = %+v", out.Fidelity)
	}
	if len(out.Sets) != 1 {
		t.Fatalf("sets = %+v, want one", out.Sets)
	}
	set := out.Sets[0]
	if set.Position != 1 || set.Name == "" || set.Id != "" || !set.Enabled || set.Role != "entry" ||
		set.DomainCount != 1 || len(set.Domains) != 1 || set.Domains[0] != "youtube.com" || set.Strategy == "" {
		t.Errorf("set row = %+v", set)
	}
	if len(out.WouldMoveFrom) != 1 || out.WouldMoveFrom[0].Set != "video" || out.WouldMoveFrom[0].Count != 1 ||
		out.WouldMoveFrom[0].Domains[0] != "youtube.com" {
		t.Errorf("the preview must name the set that holds youtube.com now: %+v", out.WouldMoveFrom)
	}
	if out.TotalSets != 2 {
		t.Errorf("total_sets = %d, want the 2 existing sets", out.TotalSets)
	}
}

func TestMCPImportStrategyGivesReasonsAndWarningsInEnglish(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	out := decodeImport(t, callImport(t, session, ctx, map[string]any{"text": importSample(t, importKeeneticCfg)}))
	if len(out.NotConverted) == 0 || len(out.Approximated) == 0 || len(out.Warnings) == 0 {
		t.Fatalf("the nfqws-keenetic sample has unsupported and approximated options and warnings: %+v", out)
	}
	code := regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	for _, row := range append(append([]mcpImportOption{}, out.NotConverted...), out.Approximated...) {
		if code.MatchString(row.Why) || !strings.Contains(row.Why, " ") || strings.Contains(row.Why, "{{") {
			t.Errorf("row %+v carries a code or an unfilled text instead of an English reason", row)
		}
		if len(row.Sets) == 0 {
			t.Errorf("row %+v names no set", row)
		}
	}
	for _, w := range out.Warnings {
		if code.MatchString(w) || strings.Contains(w, "{{") {
			t.Errorf("warning %q is a code or an unfilled text", w)
		}
	}

	var excluded *mcpImportOption
	for i, row := range out.NotConverted {
		if row.Status == string(convert.StatusUnsupported) && strings.Contains(row.Why, "no exclusion list") {
			excluded = &out.NotConverted[i]
		}
	}
	if excluded == nil {
		t.Fatalf("the exclude lists must be reported as not converted: %+v", out.NotConverted)
	}
	if len(excluded.Options) != 2 || len(excluded.Sets) < 2 {
		t.Errorf("the two exclude options share one row that names every set using them: %+v", excluded)
	}

	if len(out.UnresolvedFiles) != 2 {
		t.Fatalf("unresolved_files = %+v, want the host list and the address list once each", out.UnresolvedFiles)
	}
	for _, f := range out.UnresolvedFiles {
		if f.Path == "" || f.Kind == "" || len(f.Sets) != 2 {
			t.Errorf("unresolved file %+v must carry its path, kind and both sets", f)
		}
	}
	if !strings.Contains(out.Note, "b4 cannot read the files") || !strings.Contains(out.Note, "b4_edit_set_targets") {
		t.Errorf("the note must say b4 cannot read the list files and how to add their contents: %q", out.Note)
	}
	if !strings.Contains(out.Note, "DISABLED") {
		t.Errorf("the note must say some sets would be created disabled: %q", out.Note)
	}
}

func TestMCPImportStrategyPreviewStaysSmall(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	text := importSample(t, importKeeneticCfg)
	res := callImport(t, session, ctx, map[string]any{"text": text})
	decodeImport(t, res)
	raw := mustStructured(t, res)

	full, err := convert.Analyze(text, convert.Options{})
	if err != nil {
		t.Fatal(err)
	}
	fullJSON, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}

	const limit = 8 << 10
	if len(raw) > limit {
		t.Errorf("the preview of %s is %d bytes, over %d: it goes into a model's context whole", importKeeneticCfg, len(raw), limit)
	}
	if strings.Contains(string(raw), `"fragmentation"`) || strings.Contains(string(raw), `"faking":{`) {
		t.Errorf("the preview carries a whole set configuration")
	}
	t.Logf("preview %d bytes, the REST analyze result %d bytes", len(raw), len(fullJSON))
}

func TestMCPImportStrategyCapsLongAnswers(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	profiles := make([]string, 0, mcpImportRowsShown+5)
	for i := 0; i < mcpImportRowsShown+5; i++ {
		profiles = append(profiles, "--filter-tcp=443 --hostlist=/lists/"+string(rune('a'+i%26))+strings.Repeat("x", i/26)+".txt --dpi-desync=fake")
	}
	res := callImport(t, session, ctx, map[string]any{"text": strings.Join(profiles, " --new ")})
	out := decodeImport(t, res)

	if len(out.Sets) != mcpImportRowsShown || len(out.UnresolvedFiles) != mcpImportRowsShown {
		t.Fatalf("an answer lists at most %d rows of a kind, got %d sets and %d files", mcpImportRowsShown, len(out.Sets), len(out.UnresolvedFiles))
	}
	for _, want := range []string{"Left out of this answer for size", "5 more set row(s)", "5 more unresolved file(s)"} {
		if !strings.Contains(out.Note, want) {
			t.Errorf("the note must say what was left out (%q): %q", want, out.Note)
		}
	}
	if !strings.Contains(out.Note, "Would be created DISABLED: 45 of the 45 set(s)") {
		t.Errorf("the counts in the note must cover every set, not only the rows shown: %q", out.Note)
	}
}

func TestMCPImportStrategyMatchesConvertApply(t *testing.T) {
	t.Cleanup(mcpResetHistory)
	for _, text := range []string{
		importZapretLine,
		importSample(t, importKeeneticCfg),
		"-H:youtube.com -s1+s -At -d0+sm",
	} {
		srv, viaTool := newMCPTestServerAPI(t, writableCfg(t))
		session, ctx := connectMCP(t, srv)
		out := decodeImport(t, callImport(t, session, ctx, map[string]any{"text": text, "apply": "true"}))
		if !out.Applied {
			t.Fatalf("the tool did not apply %q: %+v", text, out)
		}

		_, viaREST := newMCPTestServerAPI(t, writableCfg(t))
		body, err := json.Marshal(map[string]any{"text": text})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		viaREST.handleConvertApply(rec, httptest.NewRequest(http.MethodPost, "/api/convert/apply", strings.NewReader(string(body))))
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST /api/convert/apply: %d %s", rec.Code, rec.Body.String())
		}

		got, want := comparableSets(t, viaTool.getCfg()), comparableSets(t, viaREST.getCfg())
		if got != want {
			t.Errorf("the tool and POST /api/convert/apply saved different sets for %q\ntool:\n%s\nREST:\n%s", text, got, want)
		}
		if len(viaTool.getCfg().Sets) != len(out.Sets)+2 {
			t.Errorf("the answer lists %d set(s), the configuration grew by %d", len(out.Sets), len(viaTool.getCfg().Sets)-2)
		}
	}
}

func TestMCPImportStrategyApplyCreatesTheSetsFirstAndTakesTheirDomains(t *testing.T) {
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	out := decodeImport(t, callImport(t, session, ctx, map[string]any{"text": importZapretLine, "apply": "true"}))
	live := api.getCfg()
	if !out.Applied || out.TotalSets != 3 || len(live.Sets) != 3 {
		t.Fatalf("apply must add one set to the two existing: %+v, live %v", out, setNames(live))
	}
	created := live.Sets[0]
	if len(out.Sets) != 1 || out.Sets[0].Id != created.Id || out.Sets[0].Name != created.Name || out.Sets[0].Position != 1 {
		t.Errorf("the answer must describe the set saved first in the list: row %+v, live %q (%s)", out.Sets, created.Name, created.Id)
	}
	if !created.Enabled || len(created.Targets.SNIDomains) != 1 || created.Targets.SNIDomains[0] != "youtube.com" {
		t.Errorf("the created set = enabled %v, domains %v", created.Enabled, created.Targets.SNIDomains)
	}
	if len(created.Targets.DomainsToMatch) != 1 {
		t.Errorf("the created set's match list was not expanded: %v", created.Targets.DomainsToMatch)
	}
	if held := live.Sets[1].Targets.SNIDomains; live.Sets[1].Name != "video" || len(held) != 0 {
		t.Errorf("youtube.com must leave the enabled set that held it: %q holds %v", live.Sets[1].Name, held)
	}
	if len(out.MovedFrom) != 1 || out.MovedFrom[0].Set != "video" || out.MovedFrom[0].Domains[0] != "youtube.com" {
		t.Errorf("moved_from = %+v", out.MovedFrom)
	}
	for _, want := range []string{"applied live", "b4_revert_last_change", "another copy", "moved_from"} {
		if !strings.Contains(out.Note, want) {
			t.Errorf("the apply note lacks %q: %q", want, out.Note)
		}
	}
}

func TestMCPImportStrategyIsUndoneByRevert(t *testing.T) {
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)
	outline := func() string {
		var sb strings.Builder
		for _, s := range api.getCfg().Sets {
			sb.WriteString(s.Id + " " + s.Name + " enabled=" + map[bool]string{true: "yes", false: "no"}[s.Enabled] +
				" domains=" + strings.Join(s.Targets.SNIDomains, ",") + "\n")
		}
		return sb.String()
	}
	before := outline()

	out := decodeImport(t, callImport(t, session, ctx, map[string]any{"text": importZapretLine, "apply": "true"}))
	if len(api.getCfg().Sets) != 3 || len(out.MovedFrom) != 1 {
		t.Fatalf("precondition: the import should have added a set and taken youtube.com: %+v", out)
	}

	if rev := decodeRevert(t, session, ctx); !rev.Reverted {
		t.Fatalf("an import must be undone by b4_revert_last_change: %+v", rev)
	}
	if after := outline(); after != before {
		t.Errorf("the undo must remove the imported sets and give the taken domains back\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if match := api.getCfg().Sets[0].Targets.DomainsToMatch; len(match) != 1 || match[0] != "youtube.com" {
		t.Errorf("the set that got youtube.com back must match it again: %v", match)
	}
}

func TestMCPImportStrategyRepeatedApplyAddsAnotherCopy(t *testing.T) {
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	first := decodeImport(t, callImport(t, session, ctx, map[string]any{"text": importZapretLine, "apply": "true"}))
	second := decodeImport(t, callImport(t, session, ctx, map[string]any{"text": importZapretLine, "apply": "true"}))
	if len(api.getCfg().Sets) != 4 {
		t.Fatalf("two applies must leave 4 sets: %v", setNames(api.getCfg()))
	}
	if second.Sets[0].Name != first.Sets[0].Name+" 2" {
		t.Errorf("the second copy must get its own name: %q after %q", second.Sets[0].Name, first.Sets[0].Name)
	}
	if len(second.MovedFrom) != 1 || second.MovedFrom[0].Set != first.Sets[0].Name {
		t.Errorf("the second copy takes youtube.com from the first: %+v", second.MovedFrom)
	}

	preview := decodeImport(t, callImport(t, session, ctx, map[string]any{"text": importZapretLine}))
	if preview.Sets[0].Name != first.Sets[0].Name+" 3" {
		t.Errorf("the preview must show the name an apply would use: %q", preview.Sets[0].Name)
	}

	refused := decodeImport(t, callImport(t, session, ctx, map[string]any{
		"text": "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=multisplit --dpi-desync-split-seqovl=99999",
	}))
	if !strings.Contains(refused.Blocked, strconv.Quote(first.Sets[0].Name+" 3")) {
		t.Errorf("a refusal must name the set as an apply would name it: %q", refused.Blocked)
	}
}

func TestMCPImportStrategyRefusals(t *testing.T) {
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)
	before := api.getCfg()

	for _, c := range []struct {
		name string
		args map[string]any
		want []string
	}{
		{"no text", map[string]any{"text": "   "}, []string{"text is required"}},
		{"nothing to parse", map[string]any{"text": "hello there"}, []string{"No recognizable options"}},
		{"unknown tool", map[string]any{"text": "-s1", "tool": "goodbyedpi"}, []string{"unknown tool", "byedpi", "zapret"}},
		{"unknown version", map[string]any{"text": "-s1 -d3+s", "tool": "byedpi", "version": "9.9"}, []string{"is not a byedpi version", "0.13", "0.17"}},
		{"bad apply", map[string]any{"text": importZapretLine, "apply": "maybe"}, []string{"apply must be true or false"}},
		{"bad domain", map[string]any{"text": importZapretLine, "domains": "https://youtube.com/watch"}, []string{"is not a domain"}},
		{"pattern in domains", map[string]any{"text": importZapretLine, "domains": "regexp:.*\\.example\\.com"}, []string{"is a pattern", "b4_edit_set_targets"}},
		{"zapret2", map[string]any{"text": "--lua-desync=fake:blob=quic_initial:repeats=11 --new --lua-desync=multidisorder:pos=1,midsld"},
			[]string{"zapret2", "Lua", "Supported", "byedpi"}},
		{"nothing recognized", map[string]any{"text": "--qnum=200 --daemon", "tool": "zapret", "apply": "true"},
			[]string{"None of the options that do the bypass were recognized"}},
		{"out of range", map[string]any{
			"text":  "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=multisplit --dpi-desync-split-seqovl=99999",
			"apply": "true",
		}, []string{"fragmentation.seq_overlap_length=99999", "--dpi-desync-split-seqovl=99999", "0 to 1460", "over MCP"}},
		{"unusable domain", map[string]any{
			"text":  "--filter-tcp=443 --hostlist-domains=localhost --dpi-desync=fake",
			"apply": "true",
		}, []string{"has no dot"}},
	} {
		res := callImport(t, session, ctx, c.args)
		if !res.IsError {
			t.Errorf("%s: must be refused, got %s", c.name, mustStructured(t, res))
			continue
		}
		for _, want := range c.want {
			if !strings.Contains(mcpErrorText(res), want) {
				t.Errorf("%s: the refusal lacks %q: %s", c.name, want, mcpErrorText(res))
			}
		}
	}
	if api.getCfg() != before {
		t.Fatal("a refused import changed the configuration")
	}
}

func TestMCPImportStrategyPreviewSaysWhyApplyWouldBeRefused(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	for _, c := range []struct{ text, want string }{
		{"--qnum=200 --daemon", "None of the options that do the bypass were recognized"},
		{"--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=multisplit --dpi-desync-split-seqovl=99999", "fragmentation.seq_overlap_length=99999"},
	} {
		out := decodeImport(t, callImport(t, session, ctx, map[string]any{"text": c.text, "tool": "zapret"}))
		if out.CanApply || !strings.Contains(out.Blocked, c.want) {
			t.Errorf("%q: can_apply=%v blocked=%q, want a refusal naming %q", c.text, out.CanApply, out.Blocked, c.want)
		}
		if !strings.Contains(out.Note, "apply would be refused") {
			t.Errorf("%q: the note must say apply would be refused: %q", c.text, out.Note)
		}
	}
}

func TestMCPImportStrategySendsAnUnresolvedPayloadFileToPayloads(t *testing.T) {
	srv := newMCPTestServer(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	const (
		listRoute    = "b4_edit_set_targets"
		payloadRoute = "Settings -> Payloads"
	)
	for _, c := range []struct {
		name, text      string
		kinds           []string
		lists, payloads bool
	}{
		{"a zapret fake file",
			"--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake --dpi-desync-fake-tls=/opt/zapret/files/fake/tls_clienthello.bin",
			[]string{"payload"}, false, true},
		{"a byedpi fake file", "-H:example.com -f-1 --fake-data=/tmp/fake.bin", []string{"payload"}, false, true},
		{"a host list and a fake file",
			"--filter-tcp=443 --hostlist=/opt/zapret/hosts.txt --dpi-desync=fake --dpi-desync-fake-tls=/opt/zapret/files/fake/tls_clienthello.bin",
			[]string{"hostlist", "payload"}, true, true},
		{"list files only", importSample(t, importKeeneticCfg), []string{"ipset", "hostlist"}, true, false},
	} {
		out := decodeImport(t, callImport(t, session, ctx, map[string]any{"text": c.text}))
		var kinds []string
		for _, f := range out.UnresolvedFiles {
			kinds = append(kinds, f.Kind)
		}
		if strings.Join(kinds, ",") != strings.Join(c.kinds, ",") {
			t.Errorf("%s: unresolved kinds = %v, want %v", c.name, kinds, c.kinds)
			continue
		}
		if !strings.Contains(out.Note, "b4 cannot read the files in unresolved_files") {
			t.Errorf("%s: the note must say b4 cannot read the files: %q", c.name, out.Note)
		}
		if got := strings.Contains(out.Note, "hostlist and ipset files") && strings.Contains(out.Note, listRoute); got != c.lists {
			t.Errorf("%s: the note sends list files to %s = %v, want %v: %q", c.name, listRoute, got, c.lists, out.Note)
		}
		if got := strings.Contains(out.Note, payloadRoute); got != c.payloads {
			t.Errorf("%s: the note sends a payload file to %s = %v, want %v: %q", c.name, payloadRoute, got, c.payloads, out.Note)
		}
		if !c.lists && strings.Contains(out.Note, listRoute) {
			t.Errorf("%s: %s takes targets and cannot install a payload file, so the note must not name it: %q", c.name, listRoute, out.Note)
		}
	}
}

func TestMCPImportStrategyRefusesAWildcardHost(t *testing.T) {
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)
	before := api.getCfg()

	text := "--filter-tcp=443 --hostlist-domains=*.youtube.com --dpi-desync=fake"
	wants := []string{`"*.youtube.com"`, "would match nothing", "Write it as youtube.com"}

	preview := decodeImport(t, callImport(t, session, ctx, map[string]any{"text": text}))
	if preview.CanApply {
		t.Errorf("b4 matches *.youtube.com as written, so a set listing it matches nothing: the preview must not offer to apply it")
	}
	for _, want := range wants {
		if !strings.Contains(preview.Blocked, want) {
			t.Errorf("blocked lacks %q: %q", want, preview.Blocked)
		}
	}

	res := callImport(t, session, ctx, map[string]any{"text": text, "apply": "true"})
	if !res.IsError {
		t.Fatalf("an apply with a wildcard host must be refused, got %s", mustStructured(t, res))
	}
	for _, want := range wants {
		if !strings.Contains(mcpErrorText(res), want) {
			t.Errorf("the refusal lacks %q: %s", want, mcpErrorText(res))
		}
	}
	if api.getCfg() != before {
		t.Fatal("the refused import changed the configuration")
	}

	plain := decodeImport(t, callImport(t, session, ctx, map[string]any{
		"text": "--filter-tcp=443 --hostlist-domains=YouTube.com. --dpi-desync=fake",
	}))
	if !plain.CanApply || len(plain.WouldMoveFrom) != 1 {
		t.Errorf("a host that only differs in case or a trailing dot is matched as the same domain and must import: can_apply=%v blocked=%q would_move_from=%+v",
			plain.CanApply, plain.Blocked, plain.WouldMoveFrom)
	}
}

func TestMCPImportStrategyKeepsToTheSetLimit(t *testing.T) {
	t.Cleanup(mcpResetHistory)
	cfg := writableCfg(t)
	for len(cfg.Sets) < mcpMaxSets {
		set := config.NewSetConfig()
		set.Id = "filler-" + string(rune('a'+len(cfg.Sets)%26)) + "-" + strings.Repeat("x", len(cfg.Sets)/26)
		set.Name = set.Id
		cfg.Sets = append(cfg.Sets, &set)
	}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callImport(t, session, ctx, map[string]any{"text": importZapretLine, "apply": "true"})
	if !res.IsError || !strings.Contains(mcpErrorText(res), "will not go past 200 sets") {
		t.Fatalf("an import past the set limit must be refused: %s", mcpErrorText(res))
	}
	if len(api.getCfg().Sets) != mcpMaxSets {
		t.Fatalf("the refused import left %d sets", len(api.getCfg().Sets))
	}
}

func TestMCPImportStrategySharedDomainsAndVersion(t *testing.T) {
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, writableCfg(t))
	session, ctx := connectMCP(t, srv)

	preview := decodeImport(t, callImport(t, session, ctx, map[string]any{
		"text": "-s1 -d3+s", "tool": "byedpi", "version": "0.13", "domains": "Rutracker.org, *.kinozal.tv",
	}))
	if preview.Tool != "byedpi" || preview.Version != "0.13" || preview.VersionGuessed {
		t.Errorf("the given tool and version must be used as given: %+v", preview)
	}
	if len(preview.Versions) < 2 {
		t.Errorf("a tool with several versions must list them, so a wrong guess can be corrected: %v", preview.Versions)
	}
	if len(preview.Sets) != 1 || preview.Sets[0].DomainCount != 2 || !preview.Sets[0].Enabled {
		t.Fatalf("the shared domains go to the first set without a host list: %+v", preview.Sets)
	}

	decodeImport(t, callImport(t, session, ctx, map[string]any{
		"text": "-s1 -d3+s", "tool": "byedpi", "version": "0.13", "domains": "Rutracker.org, *.kinozal.tv", "apply": "true",
	}))
	got := strings.Join(api.getCfg().Sets[0].Targets.SNIDomains, ",")
	if got != "rutracker.org,kinozal.tv" {
		t.Errorf("stored domains = %q, want the canonical forms b4_edit_set_targets stores", got)
	}
}

func TestMCPImportStrategySavesFakePayloadsAndSaysTheyOutliveAnUndo(t *testing.T) {
	t.Cleanup(mcpResetHistory)
	cfg := writableCfg(t)
	cfg.ConfigPath = hubTestConfigPath(t)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeImport(t, callImport(t, session, ctx, map[string]any{
		"text": binaryFakeLine, "domains": "example.com", "apply": "true",
	}))
	file := api.getCfg().Sets[0].Faking.PayloadFile
	if !strings.HasPrefix(file, "captures/") {
		t.Fatalf("the imported set must name a payload file under captures/, got %q", file)
	}
	t.Cleanup(func() { os.Remove(filepath.Join(filepath.Dir(cfg.ConfigPath), file)) })
	if !strings.Contains(out.Note, "1 fake payload(s)") || !strings.Contains(out.Note, "leaves those files") {
		t.Errorf("the note must say a payload file was saved and that an undo leaves it: %q", out.Note)
	}
}

func TestMCPImportStrategySamplesPassTheWriteGuards(t *testing.T) {
	api := &API{cfgPtr: testCfgPtr(writableCfg(t))}
	samples, err := filepath.Glob("../../convert/testdata/*")
	if err != nil || len(samples) == 0 {
		t.Fatalf("no converter samples: %v", err)
	}
	for _, path := range samples {
		res, err := convert.Analyze(importSample(t, path), convert.Options{})
		if err != nil || !res.Applicable {
			continue
		}
		if refusal := api.mcpImportRefusal(api.getCfg(), res); refusal != nil {
			t.Errorf("%s converts in the web interface but the tool refuses it: %v", path, refusal)
		}
	}
}

func TestMCPAPIReferencePrefersTheImportToolForConvertOperations(t *testing.T) {
	for _, op := range []string{"POST /api/convert/analyze", "POST /api/convert/apply"} {
		hint, ok := mcpAPIPreferredTools[op]
		if !ok || hint.tool != mcpImportTool {
			t.Errorf("%s must point at %s, got %+v", op, mcpImportTool, hint)
		}
	}
	if mcpAPIToolNeeds(mcpImportTool) != mcpAPIWritesSwitch {
		t.Errorf("%s is served only with 'Allow configuration changes' on, and b4_api_reference must say so", mcpImportTool)
	}
}
