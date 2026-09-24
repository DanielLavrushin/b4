package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/watchdog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func watchdogAPI(t *testing.T, sets ...*config.SetConfig) (*API, *http.ServeMux) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Sets = sets
	api := &API{cfgPtr: testCfgPtr(&cfg), geodataManager: geodat.NewGeodataManager("", "")}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterWatchdogApi()
	api.RegisterSetsApi()
	api.RegisterConfigApi()
	return api, mux
}

func withWatchdog(t *testing.T, api *API) *watchdog.Watchdog {
	t.Helper()
	previous := globalWatchdog
	wd := watchdog.New(api.cfgPtr, nil, func(mutate func(*config.Config) (*config.Config, error)) error {
		unlock := config.LockWrites()
		defer unlock()
		next, err := mutate(api.cfgPtr.Load())
		if err != nil {
			return err
		}
		api.cfgPtr.Store(next)
		return nil
	})
	globalWatchdog = wd
	t.Cleanup(func() { globalWatchdog = previous })
	return wd
}

func serve(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func urlSet(id string, urls ...string) *config.SetConfig {
	set := namedSet(id, id, true, "youtube.com")
	set.Discovery.URLs = urls
	return set
}

func TestWatchdogSetEnableValidation(t *testing.T) {
	ok := urlSet("ok", "https://www.youtube.com/")
	empty := urlSet("empty")
	routed := urlSet("routed", "https://www.youtube.com/")
	routed.Routing.Enabled = true
	routed.Routing.EgressInterface = "wg0"
	devices := urlSet("devices", "https://www.youtube.com/")
	devices.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
	disabled := urlSet("disabled", "https://www.youtube.com/")
	disabled.Enabled = false
	api, mux := watchdogAPI(t, ok, empty, routed, devices, disabled)

	on := `{"enabled":true}`
	expectCode(t, serve(mux, http.MethodPut, "/api/watchdog/sets/nope", on), http.StatusNotFound, "not_found")
	expectCode(t, serve(mux, http.MethodPut, "/api/watchdog/sets/empty", on), http.StatusBadRequest, "no_urls")
	expectCode(t, serve(mux, http.MethodPut, "/api/watchdog/sets/routed", on), http.StatusBadRequest, "routed_set")
	expectCode(t, serve(mux, http.MethodPut, "/api/watchdog/sets/devices", on), http.StatusBadRequest, "device_scoped")
	expectCode(t, serve(mux, http.MethodPut, "/api/watchdog/sets/disabled", on), http.StatusBadRequest, "set_disabled")
	expectCode(t, serve(mux, http.MethodPut, "/api/watchdog/sets/ok", `{`), http.StatusBadRequest, "invalid_json")

	if rec := serve(mux, http.MethodPut, "/api/watchdog/sets/ok", on); rec.Code != http.StatusOK {
		t.Fatalf("a watchable set can be turned on, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !api.getCfg().GetSetById("ok").Discovery.Watchdog {
		t.Fatal("the flag must be saved")
	}
	if rec := serve(mux, http.MethodPut, "/api/watchdog/sets/empty", `{"enabled":false}`); rec.Code != http.StatusOK {
		t.Errorf("turning off is always allowed, got %d", rec.Code)
	}
	if rec := serve(mux, http.MethodPut, "/api/watchdog/sets/ok", `{"enabled":false}`); rec.Code != http.StatusOK || api.getCfg().GetSetById("ok").Discovery.Watchdog {
		t.Errorf("turning off saves the flag, got %d", rec.Code)
	}
}

func TestWatchdogStatusListsSets(t *testing.T) {
	watched := urlSet("yt", "https://www.youtube.com/")
	watched.Discovery.Watchdog = true
	api, mux := watchdogAPI(t, watched)

	previous := globalWatchdog
	globalWatchdog = nil
	rec := serve(mux, http.MethodGet, "/api/watchdog/status", "")
	globalWatchdog = previous
	if !strings.Contains(rec.Body.String(), `"sets":[]`) {
		t.Errorf("without a running watchdog the sets list is still present: %s", rec.Body.String())
	}

	withWatchdog(t, api)
	rec = serve(mux, http.MethodGet, "/api/watchdog/status", "")
	var state struct {
		Enabled bool                      `json:"enabled"`
		Domains []json.RawMessage         `json:"domains"`
		Sets    []watchdog.SetWatchStatus `json:"sets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Sets) != 1 || state.Sets[0].SetId != "yt" || state.Sets[0].Status != watchdog.SetStatusQueued || len(state.Sets[0].URLs) != 1 {
		t.Fatalf("a watched set is reported: %+v", state.Sets)
	}
}

func TestWatchdogSetCheck(t *testing.T) {
	watched := urlSet("yt", "https://www.youtube.com/")
	watched.Discovery.Watchdog = true
	unwatched := urlSet("tr", "https://rutracker.org/")
	api, mux := watchdogAPI(t, watched, unwatched)

	expectCode(t, serve(mux, http.MethodPost, "/api/watchdog/sets/nope/check", ""), http.StatusNotFound, "not_found")
	expectCode(t, serve(mux, http.MethodPost, "/api/watchdog/sets/tr/check", ""), http.StatusBadRequest, "not_watched")

	previous := globalWatchdog
	globalWatchdog = nil
	if rec := serve(mux, http.MethodPost, "/api/watchdog/sets/yt/check", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no watchdog, no check: %d", rec.Code)
	}
	globalWatchdog = previous

	withWatchdog(t, api)
	if rec := serve(mux, http.MethodPost, "/api/watchdog/sets/yt/check", ""); rec.Code != http.StatusOK {
		t.Errorf("a watched set can be checked: %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestWatchdogMoveDomainToSet(t *testing.T) {
	yt := urlSet("yt", "https://www.youtube.com/")
	full := urlSet("full", "https://a.example/", "https://b.example/", "https://c.example/", "https://d.example/", "https://e.example/")
	routed := urlSet("routed", "https://www.youtube.com/")
	routed.Routing.Enabled = true
	routed.Routing.EgressInterface = "wg0"
	api, mux := watchdogAPI(t, yt, full, routed)
	next := api.getCfg().Clone()
	next.System.Checker.Watchdog.Domains = []string{"rutracker.org", "https://www.youtube.com/watch", "f.example"}
	api.cfgPtr.Store(next)

	expectCode(t, serve(mux, http.MethodPost, "/api/watchdog/domains/nothere.example/move", `{"set_id":"yt"}`), http.StatusNotFound, "not_found")
	expectCode(t, serve(mux, http.MethodPost, "/api/watchdog/domains/rutracker.org/move", `{"set_id":"nope"}`), http.StatusNotFound, "not_found")
	expectCode(t, serve(mux, http.MethodPost, "/api/watchdog/domains/f.example/move", `{"set_id":"full"}`), http.StatusBadRequest, "too_many_urls")
	expectCode(t, serve(mux, http.MethodPost, "/api/watchdog/domains/rutracker.org/move", `{"set_id":"routed"}`), http.StatusBadRequest, "routed_set")

	if rec := serve(mux, http.MethodPost, "/api/watchdog/domains/rutracker.org/move", `{"set_id":"yt"}`); rec.Code != http.StatusOK {
		t.Fatalf("move failed: %d (%s)", rec.Code, rec.Body.String())
	}
	cfg := api.getCfg()
	set := cfg.GetSetById("yt")
	if !set.Discovery.Watchdog || len(set.Discovery.URLs) != 2 || set.Discovery.URLs[1] != "https://rutracker.org/" {
		t.Errorf("the entry's URL is added and the set is watched: %+v", set.Discovery)
	}
	if got := cfg.System.Checker.Watchdog.Domains; len(got) != 2 || got[0] != "https://www.youtube.com/watch" {
		t.Errorf("the entry leaves the global list: %v", got)
	}

	if rec := serve(mux, http.MethodPost, "/api/watchdog/domains/www.youtube.com/move", `{"set_id":"yt"}`); rec.Code != http.StatusOK {
		t.Fatalf("a host the set already probes moves without a new URL: %d (%s)", rec.Code, rec.Body.String())
	}
	if got := api.getCfg().GetSetById("yt").Discovery.URLs; len(got) != 2 {
		t.Errorf("no duplicate URL for a known host: %v", got)
	}

	if rec := serve(mux, http.MethodPost, "/api/watchdog/domains/f.example/move", `{"set_id":"full"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("still too many: %d", rec.Code)
	}
}

func TestConfigRevisionRefusesStaleWrites(t *testing.T) {
	api, mux := watchdogAPI(t, urlSet("yt", "https://www.youtube.com/"))

	rec := serve(mux, http.MethodGet, "/api/config", "")
	var got struct {
		Revision string           `json:"revision"`
		Sets     []map[string]any `json:"sets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Revision) != 16 || got.Revision != watchdog.ConfigRevision(api.getCfg()) {
		t.Fatalf("GET /api/config carries a 16-character revision of the stored config: %q", got.Revision)
	}
	if len(got.Sets) != 1 || got.Sets[0]["revision"] != watchdog.SetRevision(api.getCfg().Sets[0]) || got.Sets[0]["stats"] == nil {
		t.Fatalf("every set carries its revision next to the existing fields: %v", got.Sets)
	}

	body, err := json.Marshal(api.getCfg())
	if err != nil {
		t.Fatal(err)
	}
	put := func(revision string) *httptest.ResponseRecorder {
		var raw map[string]any
		_ = json.Unmarshal(body, &raw)
		if revision != "" {
			raw["revision"] = revision
		}
		data, _ := json.Marshal(raw)
		return serve(mux, http.MethodPut, "/api/config", string(data))
	}

	expectCode(t, put("0123456789abcdef"), http.StatusConflict, "config_changed")

	rec = put(got.Revision)
	if rec.Code != http.StatusOK {
		t.Fatalf("the current revision is accepted, got %d (%s)", rec.Code, rec.Body.String())
	}
	var saved struct {
		Revision string `json:"revision"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	if saved.Revision != watchdog.ConfigRevision(api.getCfg()) {
		t.Errorf("the response carries the new revision: %q", saved.Revision)
	}

	if rec := put(""); rec.Code != http.StatusOK {
		t.Errorf("a body without a revision is accepted as before, got %d", rec.Code)
	}
}

func TestSetRevisionRefusesStaleWrites(t *testing.T) {
	api, mux := watchdogAPI(t, urlSet("yt", "https://www.youtube.com/"))

	rec := serve(mux, http.MethodGet, "/api/sets", "")
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	revision, _ := list[0]["revision"].(string)
	if len(revision) != 16 || list[0]["id"] != "yt" {
		t.Fatalf("GET /api/sets items carry their revision: %v", list[0])
	}

	edit := func(rev string, strategy string) *httptest.ResponseRecorder {
		raw := map[string]any{}
		data, _ := json.Marshal(api.getCfg().GetSetById("yt"))
		_ = json.Unmarshal(data, &raw)
		raw["fragmentation"].(map[string]any)["strategy"] = strategy
		if rev != "" {
			raw["revision"] = rev
		}
		body, _ := json.Marshal(raw)
		return serve(mux, http.MethodPut, "/api/sets/yt", string(body))
	}

	rec = edit(revision, "oob")
	if rec.Code != http.StatusOK {
		t.Fatalf("the current revision is accepted: %d (%s)", rec.Code, rec.Body.String())
	}
	var updated map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &updated)
	newRevision, _ := updated["revision"].(string)
	if newRevision == "" || newRevision == revision || newRevision != watchdog.SetRevision(api.getCfg().GetSetById("yt")) {
		t.Fatalf("PUT returns the set with its new revision: %q", newRevision)
	}

	expectCode(t, edit(revision, "disorder"), http.StatusConflict, "set_changed")
	if got := api.getCfg().GetSetById("yt").Fragmentation.Strategy; got != "oob" {
		t.Errorf("a stale write must not land, got %q", got)
	}

	if rec := edit("", "disorder"); rec.Code != http.StatusOK {
		t.Errorf("a body without a revision is accepted as before, got %d", rec.Code)
	}
}

func mcpWatchdogCfg(writes, probes bool) *config.Config {
	cfg := mcpTestCfg()
	cfg.System.WebServer.MCP.AllowWrites = writes
	cfg.System.WebServer.MCP.AllowActiveProbes = probes
	cfg.Sets[0].Discovery.URLs = []string{"https://www.youtube.com/"}
	return cfg
}

func callSetWatchdog(t *testing.T, session *mcp.ClientSession, ctx context.Context, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	args["set"] = "video"
	return callWatchdog(t, session, ctx, args)
}

func TestMCPSetWatchdogGates(t *testing.T) {
	cases := []struct {
		action         string
		writes, probes bool
		allowed        bool
	}{
		{"status", false, false, true},
		{"enable", true, false, false},
		{"enable", false, true, false},
		{"enable", true, true, true},
		{"disable", true, false, true},
		{"disable", false, true, false},
		{"add", true, false, false},
		{"add", true, true, true},
		{"remove", true, false, true},
		{"remove", false, false, false},
		{"check", false, false, false},
	}
	for _, c := range cases {
		cfg := mcpWatchdogCfg(c.writes, c.probes)
		if c.action == "disable" {
			cfg.Sets[0].Discovery.Watchdog = true
		}
		mcpResetHistory()
		srv, _ := newMCPTestServerAPI(t, cfg)
		session, ctx := connectMCP(t, srv)
		args := map[string]any{"action": c.action}
		if c.action == "add" {
			args["url"] = "https://m.youtube.com/"
		}
		if c.action == "remove" {
			args["url"] = "https://www.youtube.com/"
		}
		res := callSetWatchdog(t, session, ctx, args)
		if res.IsError == c.allowed {
			t.Errorf("%s writes=%v probes=%v: allowed=%v, got error=%v (%s)", c.action, c.writes, c.probes, c.allowed, res.IsError, mcpErrorText(res))
		}
	}
	mcpResetHistory()
}

func TestMCPSetWatchdogEditsTheSet(t *testing.T) {
	cfg := mcpWatchdogCfg(true, true)
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)
	withWatchdog(t, api)

	out := decodeWatchdog(t, callSetWatchdog(t, session, ctx, map[string]any{"action": "enable"}))
	if !out.Changed || !api.getCfg().GetSetById("set-1").Discovery.Watchdog {
		t.Fatalf("enable turns the set's watchdog on: %+v", out)
	}
	if out.Set == nil || out.Set.SetId != "set-1" {
		t.Errorf("the result reports the set's watch status: %+v", out.Set)
	}

	for _, bad := range []string{"http://192.168.1.1/", "127.0.0.1", "ftp://example.com/"} {
		if res := callSetWatchdog(t, session, ctx, map[string]any{"action": "add", "url": bad}); !res.IsError {
			t.Errorf("%q must be refused", bad)
		}
	}

	added := decodeWatchdog(t, callSetWatchdog(t, session, ctx, map[string]any{"action": "add", "url": "M.YouTube.com"}))
	urls := api.getCfg().GetSetById("set-1").Discovery.URLs
	if !added.Changed || len(urls) != 2 || urls[1] != "https://m.youtube.com/" {
		t.Fatalf("add stores the normalised URL: %v", urls)
	}
	again := decodeWatchdog(t, callSetWatchdog(t, session, ctx, map[string]any{"action": "add", "url": "https://m.youtube.com/feed"}))
	if again.Changed {
		t.Error("a second URL for a host the set already probes is a no-op")
	}

	for _, host := range []string{"a.example", "b.example", "c.example"} {
		callSetWatchdog(t, session, ctx, map[string]any{"action": "add", "url": host})
	}
	if res := callSetWatchdog(t, session, ctx, map[string]any{"action": "add", "url": "f.example"}); !res.IsError {
		t.Error("a sixth URL must be refused")
	}

	decodeWatchdog(t, callSetWatchdog(t, session, ctx, map[string]any{"action": "remove", "url": "www.youtube.com"}))
	if got := api.getCfg().GetSetById("set-1").Discovery.URLs; len(got) != 4 || got[0] != "https://m.youtube.com/" {
		t.Errorf("remove matches the host: %v", got)
	}

	status := decodeWatchdog(t, callSetWatchdog(t, session, ctx, map[string]any{"action": "status"}))
	if status.Set == nil || len(status.Set.URLs) != 4 {
		t.Errorf("status reports the set: %+v", status.Set)
	}
	if res := callSetWatchdog(t, session, ctx, map[string]any{"action": "check"}); res.IsError {
		t.Errorf("check of a watched set: %s", mcpErrorText(res))
	}

	decodeRevert(t, session, ctx)
	if got := api.getCfg().GetSetById("set-1").Discovery.URLs; len(got) != 5 {
		t.Errorf("set watchdog edits are undoable: %v", got)
	}
}

func TestMCPRevertRefusesToStartProbesWithoutTheGate(t *testing.T) {
	cfg := mcpWatchdogCfg(true, false)
	cfg.Sets[0].Discovery.Watchdog = true
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	decodeWatchdog(t, callSetWatchdog(t, session, ctx, map[string]any{"action": "disable"}))
	if api.getCfg().GetSetById("set-1").Discovery.Watchdog {
		t.Fatal("precondition: disabled")
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_revert_last_change"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(mcpErrorText(res), "Allow active probes") {
		t.Fatalf("undoing a disable turns the watchdog back on and needs the probe gate: %s", mcpErrorText(res))
	}
	if api.getCfg().GetSetById("set-1").Discovery.Watchdog {
		t.Error("the refused revert changed nothing")
	}
	if len(mcpHistory) != 1 {
		t.Errorf("the refused change stays on the undo list, got %d", len(mcpHistory))
	}

	decodeWatchdog(t, callSetWatchdog(t, session, ctx, map[string]any{"action": "remove", "url": "https://www.youtube.com/"}))
	if res, _ := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_revert_last_change"}); !res.IsError {
		t.Error("undoing a URL removal adds a discovery URL and needs the probe gate")
	}
}

func TestMCPRevertStartsProbes(t *testing.T) {
	current := mcpTestCfg()
	snapshot := current.Clone()
	if what := mcpRevertStartsProbes(snapshot, current); what != "" {
		t.Fatalf("identical configs: %q", what)
	}
	snapshot.Sets[0].Discovery.URLs = []string{"https://www.youtube.com/"}
	if what := mcpRevertStartsProbes(snapshot, current); !strings.Contains(what, "discovery URL") {
		t.Errorf("an added URL is caught: %q", what)
	}
	current.Sets[0].Discovery.URLs = []string{"https://www.youtube.com/"}
	current.Sets[0].Discovery.Watchdog = true
	if what := mcpRevertStartsProbes(snapshot, current); what != "" {
		t.Errorf("turning a watchdog off is always allowed: %q", what)
	}
	snapshot.Sets[0].Discovery.Watchdog = true
	current.Sets[0].Discovery.Watchdog = false
	if what := mcpRevertStartsProbes(snapshot, current); !strings.Contains(what, "watchdog") {
		t.Errorf("turning a watchdog on is caught: %q", what)
	}
}

func TestMCPDuplicateClearsTheWatchdog(t *testing.T) {
	cfg := mcpWatchdogCfg(true, false)
	cfg.Sets[0].Discovery.Watchdog = true
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_manage_set", Arguments: map[string]any{
		"action": "duplicate", "set": "video", "name": "video copy",
	}})
	if err != nil || res.IsError {
		t.Fatalf("duplicate: %v %s", err, mcpErrorText(res))
	}
	var copied *config.SetConfig
	for _, s := range api.getCfg().Sets {
		if s.Name == "video copy" {
			copied = s
		}
	}
	if copied == nil {
		t.Fatal("no copy")
	}
	if copied.Discovery.Watchdog {
		t.Error("a copy must not start probing on its own")
	}
	if len(copied.Discovery.URLs) != 1 {
		t.Errorf("the URLs are copied: %v", copied.Discovery.URLs)
	}
}
