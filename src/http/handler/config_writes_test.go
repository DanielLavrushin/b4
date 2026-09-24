package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/watchdog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func storeConcurrently(t *testing.T, api *API, mutate func(*config.Config)) {
	t.Helper()
	next := api.getCfg().Clone()
	mutate(next)
	if err := next.Validate(); err != nil {
		t.Fatalf("concurrent save: %v", err)
	}
	api.cfgPtr.Store(next)
}

func serveWhileLocked(t *testing.T, mux *http.ServeMux, method, path, body string, whileLocked func()) *httptest.ResponseRecorder {
	t.Helper()
	unlock := config.LockWrites()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- serve(mux, method, path, body) }()
	time.Sleep(50 * time.Millisecond)
	whileLocked()
	unlock()
	select {
	case rec := <-done:
		return rec
	case <-time.After(10 * time.Second):
		t.Fatal("the request never finished")
		return nil
	}
}

func setBody(t *testing.T, set *config.SetConfig, revision string, mutate func(map[string]any)) string {
	t.Helper()
	raw := map[string]any{}
	data, _ := json.Marshal(set)
	_ = json.Unmarshal(data, &raw)
	mutate(raw)
	if revision != "" {
		raw["revision"] = revision
	}
	body, _ := json.Marshal(raw)
	return string(body)
}

func TestUpdateSetRevisionCheckRunsUnderTheWriteLock(t *testing.T) {
	api, mux := watchdogAPI(t, urlSet("yt", "https://www.youtube.com/"), urlSet("tr", "https://rutracker.org/"))
	yt := api.getCfg().GetSetById("yt")
	body := setBody(t, yt, watchdog.SetRevision(yt), func(raw map[string]any) {
		raw["fragmentation"].(map[string]any)["strategy"] = "oob"
	})

	rec := serveWhileLocked(t, mux, http.MethodPut, "/api/sets/yt", body, func() {
		storeConcurrently(t, api, func(c *config.Config) { c.GetSetById("yt").TCP.Seg2Delay = 42 })
	})

	expectCode(t, rec, http.StatusConflict, "set_changed")
	got := api.getCfg().GetSetById("yt")
	if got.TCP.Seg2Delay != 42 || got.Fragmentation.Strategy == "oob" {
		t.Errorf("a stale write must not land over a save made while it waited, got %d / %s", got.TCP.Seg2Delay, got.Fragmentation.Strategy)
	}
}

func TestUpdateSetKeepsAConcurrentSaveToAnotherSet(t *testing.T) {
	api, mux := watchdogAPI(t, urlSet("yt", "https://www.youtube.com/"), urlSet("tr", "https://rutracker.org/"))
	body := setBody(t, api.getCfg().GetSetById("yt"), "", func(raw map[string]any) {
		raw["fragmentation"].(map[string]any)["strategy"] = "oob"
	})

	rec := serveWhileLocked(t, mux, http.MethodPut, "/api/sets/yt", body, func() {
		storeConcurrently(t, api, func(c *config.Config) { c.GetSetById("tr").TCP.Seg2Delay = 42 })
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("the edit lands: %d (%s)", rec.Code, rec.Body.String())
	}
	cfg := api.getCfg()
	if cfg.GetSetById("yt").Fragmentation.Strategy != "oob" {
		t.Error("the edited set is written")
	}
	if cfg.GetSetById("tr").TCP.Seg2Delay != 42 {
		t.Error("a save to another set made while the edit waited is kept")
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["revision"] != watchdog.SetRevision(cfg.GetSetById("yt")) {
		t.Errorf("the response carries the stored revision: %v", out["revision"])
	}
}

func TestUpdateConfigRevisionCheckRunsUnderTheWriteLock(t *testing.T) {
	api, mux := watchdogAPI(t, urlSet("yt", "https://www.youtube.com/"))
	raw := map[string]any{}
	data, _ := json.Marshal(api.getCfg())
	_ = json.Unmarshal(data, &raw)
	raw["revision"] = watchdog.ConfigRevision(api.getCfg())
	body, _ := json.Marshal(raw)

	rec := serveWhileLocked(t, mux, http.MethodPut, "/api/config", string(body), func() {
		storeConcurrently(t, api, func(c *config.Config) { c.Sets[0].TCP.Seg2Delay = 42 })
	})

	expectCode(t, rec, http.StatusConflict, "config_changed")
	if got := api.getCfg().Sets[0].TCP.Seg2Delay; got != 42 {
		t.Errorf("the save made while the write waited is kept, got %d", got)
	}
}

func TestWatchdogSetEnableRefusesIPOnlySets(t *testing.T) {
	ipOnly := urlSet("ips", "https://www.youtube.com/")
	ipOnly.Targets.SNIDomains = nil
	ipOnly.Targets.DomainsToMatch = nil
	ipOnly.Targets.IPs = []string{"203.0.113.7"}
	api, mux := watchdogAPI(t, ipOnly)

	rec := serve(mux, http.MethodPut, "/api/watchdog/sets/ips", `{"enabled":true}`)
	expectCode(t, rec, http.StatusBadRequest, "ip_only")
	if api.getCfg().GetSetById("ips").Discovery.Watchdog {
		t.Error("nothing is saved")
	}
	if !strings.Contains(rec.Body.String(), "host name") {
		t.Errorf("the refusal says why: %s", rec.Body.String())
	}
}

func TestWatchdogFlagSurvivesDisablingTheSet(t *testing.T) {
	watched := urlSet("yt", "https://www.youtube.com/")
	watched.Discovery.Watchdog = true
	api, mux := watchdogAPI(t, watched)

	if rec := serve(mux, http.MethodPost, "/api/sets/batch-set-enabled", `{"ids":["yt"],"enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("disable: %d (%s)", rec.Code, rec.Body.String())
	}
	set := api.getCfg().GetSetById("yt")
	if !set.Discovery.Watchdog || set.WatchdogActive() {
		t.Fatalf("a disabled set keeps the flag and is not watched: %+v", set.Discovery)
	}
	expectCode(t, serve(mux, http.MethodPost, "/api/watchdog/sets/yt/check", ""), http.StatusBadRequest, "not_watched")

	if rec := serve(mux, http.MethodPost, "/api/sets/batch-set-enabled", `{"ids":["yt"],"enabled":true}`); rec.Code != http.StatusOK {
		t.Fatalf("enable: %d", rec.Code)
	}
	if !api.getCfg().GetSetById("yt").WatchdogActive() {
		t.Error("enabling the set again watches it again")
	}
}

func TestWatchdogSetCheckReportsItsOutcome(t *testing.T) {
	watched := urlSet("yt", "https://www.youtube.com/")
	watched.Discovery.Watchdog = true
	api, mux := watchdogAPI(t, watched)
	withWatchdog(t, api)

	decode := func(rec *httptest.ResponseRecorder) WatchdogActionResponse {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("check: %d (%s)", rec.Code, rec.Body.String())
		}
		var out WatchdogActionResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}

	storeConcurrently(t, api, func(c *config.Config) { c.System.Checker.Watchdog.Enabled = true })
	if out := decode(serve(mux, http.MethodPost, "/api/watchdog/sets/yt/check", "")); out.Outcome != watchdog.ForceCheckScheduled {
		t.Errorf("with the master switch on the check is scheduled: %+v", out)
	}

	storeConcurrently(t, api, func(c *config.Config) { c.System.Checker.Watchdog.Enabled = false })
	out := decode(serve(mux, http.MethodPost, "/api/watchdog/sets/yt/check", ""))
	if out.Outcome != watchdog.ForceCheckMasterOff || !strings.Contains(out.Message, "master switch is off") {
		t.Errorf("with the master switch off the answer says nothing runs: %+v", out)
	}
}

func TestWatchdogStatusNamesTheSetThatChecksALegacyEntry(t *testing.T) {
	watched := urlSet("yt", "https://www.youtube.com/")
	watched.Discovery.Watchdog = true
	api, mux := watchdogAPI(t, watched)
	storeConcurrently(t, api, func(c *config.Config) {
		c.System.Checker.Watchdog.Domains = []string{"www.youtube.com", "rutracker.org"}
	})
	withWatchdog(t, api)

	rec := serve(mux, http.MethodGet, "/api/watchdog/status", "")
	var state struct {
		Domains []map[string]any `json:"domains"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	rows := map[string]map[string]any{}
	for _, d := range state.Domains {
		rows[d["domain"].(string)] = d
	}
	if rows["www.youtube.com"]["watched_by_set_id"] != "yt" || rows["www.youtube.com"]["watched_by_set_name"] != "yt" {
		t.Errorf("an entry a watched set probes names that set: %v", rows["www.youtube.com"])
	}
	if _, ok := rows["rutracker.org"]["watched_by_set_id"]; ok {
		t.Errorf("an entry checked on its own carries no watched_by fields: %v", rows["rutracker.org"])
	}
}

func TestMCPSetWatchdogCheckNotes(t *testing.T) {
	for _, c := range []struct {
		writes, master bool
		want           string
	}{
		{false, true, "a give-up and the heal failures stay"},
		{true, true, "its heal failures and any give-up"},
		{false, false, "nothing will happen until the watchdog master switch is turned on"},
	} {
		cfg := mcpWatchdogCfg(c.writes, true)
		cfg.Sets[0].Discovery.Watchdog = true
		cfg.System.Checker.Watchdog.Enabled = c.master
		mcpResetHistory()
		srv, api := newMCPTestServerAPI(t, cfg)
		session, ctx := connectMCP(t, srv)
		withWatchdog(t, api)

		out := decodeWatchdog(t, callSetWatchdog(t, session, ctx, map[string]any{"action": "check"}))
		if !strings.Contains(out.Note, c.want) {
			t.Errorf("writes=%v master=%v: note %q lacks %q", c.writes, c.master, out.Note, c.want)
		}
	}
	mcpResetHistory()

	if note := mcpForceCheckNote("yt", watchdog.ForceCheckHealing, false); !strings.HasPrefix(note, "nothing will happen now") {
		t.Errorf("a set being healed: %q", note)
	}
	if note := mcpForceCheckNote("yt", watchdog.ForceCheckNotWatched, true); !strings.HasPrefix(note, "nothing will happen") {
		t.Errorf("a set the watchdog does not track: %q", note)
	}
}

func TestMCPSetWatchdogStatusExplainsABlockedFlag(t *testing.T) {
	cfg := mcpWatchdogCfg(false, false)
	cfg.Sets[0].Discovery.Watchdog = true
	cfg.Sets[0].Enabled = false
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)
	withWatchdog(t, api)

	out := decodeWatchdog(t, callSetWatchdog(t, session, ctx, map[string]any{"action": "status"}))
	if !strings.Contains(out.Note, "switched on but is not checked") || !strings.Contains(out.Note, "set_disabled") {
		t.Errorf("status says why a flagged set is not checked: %q", out.Note)
	}
}

func TestMCPSetWatchdogEnableRefusesIPOnly(t *testing.T) {
	cfg := mcpWatchdogCfg(true, true)
	cfg.Sets[0].Targets.SNIDomains = nil
	cfg.Sets[0].Targets.IPs = []string{"203.0.113.7"}
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callSetWatchdog(t, session, ctx, map[string]any{"action": "enable"})
	if !res.IsError || !strings.Contains(mcpErrorText(res), "ip_only") {
		t.Fatalf("an IP-only set cannot be watched: %s", mcpErrorText(res))
	}
	if api.getCfg().GetSetById("set-1").Discovery.Watchdog {
		t.Error("nothing is saved")
	}
}

func TestMCPLegacyCheckSaysWhenASetChecksTheEntry(t *testing.T) {
	cfg := mcpWatchdogCfg(false, true)
	cfg.Sets[0].Discovery.Watchdog = true
	cfg.System.Checker.Watchdog.Enabled = true
	cfg.System.Checker.Watchdog.Domains = []string{"www.youtube.com"}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)
	withWatchdog(t, api)

	out := decodeWatchdog(t, callWatchdog(t, session, ctx, map[string]any{"action": "check", "domain": "www.youtube.com"}))
	if !strings.HasPrefix(out.Note, "nothing will happen") || !strings.Contains(out.Note, `"video"`) {
		t.Errorf("the note says the set checks the entry: %q", out.Note)
	}
	if len(out.Domains) != 1 || out.Domains[0].WatchedBySet != "video" {
		t.Errorf("the row names the set: %+v", out.Domains)
	}
}

func callRevert(t *testing.T, session *mcp.ClientSession) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "b4_revert_last_change"})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestMCPRevertRefusesWhenTheConfigChangedSince(t *testing.T) {
	cfg := writableCfg(t)
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].tcp.seg2delay", "10"))
	if err := api.saveAndPushConfig(func() *config.Config {
		next := api.getCfg().Clone()
		next.Sets[0].Faking.TTL = 9
		return next
	}()); err != nil {
		t.Fatal(err)
	}

	res := callRevert(t, session)
	if !res.IsError || !strings.Contains(mcpErrorText(res), "changed after that change") {
		t.Fatalf("an undo over a newer change is refused: %s", mcpErrorText(res))
	}
	live := api.getCfg().Sets[0]
	if live.TCP.Seg2Delay != 10 || live.Faking.TTL != 9 {
		t.Errorf("nothing changed, got seg2delay %d ttl %d", live.TCP.Seg2Delay, live.Faking.TTL)
	}
	if len(mcpHistory) != 1 {
		t.Errorf("the change stays on the undo list, got %d", len(mcpHistory))
	}
}

func TestMCPRevertWalksBackAcrossToolsAndStopsAtAForeignChange(t *testing.T) {
	cfg := mcpWatchdogCfg(true, true)
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].tcp.seg2delay", "10"))
	if res := callEditTargets(t, session, ctx, map[string]any{"set": "video", "kind": "sni_domains", "add": "m.youtube.com"}); res.IsError {
		t.Fatalf("targets: %s", mcpErrorText(res))
	}
	decodeWatchdog(t, callSetWatchdog(t, session, ctx, map[string]any{"action": "add", "url": "https://m.youtube.com/"}))
	decodeWatchdog(t, callWatchdog(t, session, ctx, map[string]any{"action": "add", "domain": "rutracker.org"}))

	for i := 0; i < 4; i++ {
		if rev := decodeRevert(t, session, ctx); !rev.Reverted {
			t.Fatalf("undo %d: %+v", i+1, rev)
		}
	}
	live := api.getCfg()
	set := live.GetSetById("set-1")
	if set.TCP.Seg2Delay != 0 || len(set.Targets.SNIDomains) != 1 || len(set.Discovery.URLs) != 1 || len(live.System.Checker.Watchdog.Domains) != 0 {
		t.Errorf("four undos restore the start: seg2delay %d sni %v urls %v domains %v",
			set.TCP.Seg2Delay, set.Targets.SNIDomains, set.Discovery.URLs, live.System.Checker.Watchdog.Domains)
	}

	decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].tcp.seg2delay", "10"))
	if err := api.saveAndPushConfig(func() *config.Config {
		next := api.getCfg().Clone()
		next.Sets[0].Faking.TTL = 9
		return next
	}()); err != nil {
		t.Fatal(err)
	}
	decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].tcp.seg2delay", "20"))

	if rev := decodeRevert(t, session, ctx); !rev.Reverted || rev.RestoredTo != "10" {
		t.Fatalf("the last change undoes: %+v", rev)
	}
	if got := api.getCfg().Sets[0].Faking.TTL; got != 9 {
		t.Errorf("the foreign change made before it is kept, got ttl %d", got)
	}
	res := callRevert(t, session)
	if !res.IsError || !strings.Contains(mcpErrorText(res), "changed after that change") {
		t.Fatalf("walking back past a foreign change is refused: %s", mcpErrorText(res))
	}
	if live := api.getCfg().Sets[0]; live.TCP.Seg2Delay != 10 || live.Faking.TTL != 9 {
		t.Errorf("the refused undo changed nothing: %d / %d", live.TCP.Seg2Delay, live.Faking.TTL)
	}
}

func TestMCPRevertGuardsTheMasterSwitchAndTheGlobalList(t *testing.T) {
	current := mcpTestCfg()
	snapshot := current.Clone()
	snapshot.System.Checker.Watchdog.Enabled = true
	if what := mcpRevertStartsProbes(snapshot, current); !strings.Contains(what, "master switch") {
		t.Errorf("turning the master switch on is caught: %q", what)
	}
	snapshot.System.Checker.Watchdog.Enabled = false
	snapshot.System.Checker.Watchdog.Domains = []string{"https://rutracker.org/forum"}
	if what := mcpRevertStartsProbes(snapshot, current); !strings.Contains(what, "global list") {
		t.Errorf("adding a global entry is caught: %q", what)
	}
	current.System.Checker.Watchdog.Domains = []string{"rutracker.org"}
	if what := mcpRevertStartsProbes(snapshot, current); what != "" {
		t.Errorf("an entry whose host is already listed is not new: %q", what)
	}

	for _, action := range []string{"disable", "remove"} {
		cfg := mcpWatchdogCfg(true, false)
		cfg.System.Checker.Watchdog.Enabled = true
		cfg.System.Checker.Watchdog.Domains = []string{"rutracker.org"}
		mcpResetHistory()
		srv, api := newMCPTestServerAPI(t, cfg)
		session, ctx := connectMCP(t, srv)

		decodeWatchdog(t, callWatchdog(t, session, ctx, map[string]any{"action": action, "domain": "rutracker.org"}))
		before := watchdog.ConfigRevision(api.getCfg())
		res := callRevert(t, session)
		if !res.IsError || !strings.Contains(mcpErrorText(res), "Allow active probes") {
			t.Errorf("undoing %s would start probes and needs the gate: %s", action, mcpErrorText(res))
		}
		if watchdog.ConfigRevision(api.getCfg()) != before {
			t.Errorf("the refused undo of %s changed nothing", action)
		}
	}
	mcpResetHistory()
}

func TestConfigWritersDoNotDeadlock(t *testing.T) {
	cfg := writableCfg(t)
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	api.RegisterSetsApi()
	session, ctx := connectMCP(t, srv)

	body := setBody(t, api.getCfg().GetSetById("set-2"), "", func(raw map[string]any) {
		raw["tcp"].(map[string]any)["seg2delay"] = 5
	})
	startTTL := api.getCfg().Sets[0].Faking.TTL
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func(i int) {
			defer wg.Done()
			_, _ = session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "b4_set_config_value",
				Arguments: map[string]any{"path": "sets[video].tcp.seg2delay", "value": strconv.Itoa(i + 1)},
			})
		}(i)
		go func() {
			defer wg.Done()
			serve(api.mux, http.MethodPut, "/api/sets/set-2", body)
		}()
		go func() {
			defer wg.Done()
			_ = api.updateAndPushConfig(func(current *config.Config) (*config.Config, error) {
				next := current.Clone()
				next.Sets[0].Faking.TTL++
				return next, nil
			})
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("concurrent config writers deadlocked")
	}
	if got := api.getCfg().Sets[0].Faking.TTL; got != startTTL+10 {
		t.Errorf("every update under the lock lands on the current config, got ttl %d from %d", got, startTTL)
	}
}

func concurrentAdoptAPI(t *testing.T) (*API, *http.ServeMux) {
	t.Helper()
	tr := urlSet("tr", "https://rutracker.org/")
	tr.Targets.SNIDomains = []string{"rutracker.org"}
	tr.Targets.DomainsToMatch = []string{"rutracker.org"}
	api, mux := watchdogAPI(t, urlSet("yt", "https://www.youtube.com/"), tr)
	api.RegisterDiscoveryApi()
	api.RegisterGeodatApi()
	storeConcurrently(t, api, func(c *config.Config) {
		c.System.Checker.Watchdog.Domains = []string{"example.net", "rutracker.org"}
	})
	return api, mux
}

func TestRESTWritesKeepAConcurrentAdopt(t *testing.T) {
	cases := []struct {
		name, method, path, body string
		status                   int
		check                    func(*config.Config) bool
	}{
		{"create set", http.MethodPost, "/api/sets", `{"name":"new","targets":{"sni_domains":["example.org"]}}`, http.StatusCreated,
			func(c *config.Config) bool { return len(c.Sets) == 3 && c.Sets[0].Name == "new" }},
		{"delete set", http.MethodDelete, "/api/sets/tr", "", http.StatusOK,
			func(c *config.Config) bool { return c.GetSetById("tr") == nil }},
		{"reorder", http.MethodPost, "/api/sets/reorder", `{"set_ids":["tr","yt"]}`, http.StatusOK,
			func(c *config.Config) bool { return c.Sets[0].Id == "tr" }},
		{"add domain", http.MethodPost, "/api/sets/yt/add-domain", `{"domain":"m.youtube.com"}`, http.StatusOK,
			func(c *config.Config) bool {
				return slices.Contains(c.GetSetById("yt").Targets.SNIDomains, "m.youtube.com")
			}},
		{"batch delete", http.MethodPost, "/api/sets/batch-delete", `{"ids":["tr"]}`, http.StatusOK,
			func(c *config.Config) bool { return c.GetSetById("tr") == nil }},
		{"batch enable", http.MethodPost, "/api/sets/batch-set-enabled", `{"ids":["tr"],"enabled":false}`, http.StatusOK,
			func(c *config.Config) bool { return !c.GetSetById("tr").Enabled }},
		{"set watchdog", http.MethodPut, "/api/watchdog/sets/yt", `{"enabled":true}`, http.StatusOK,
			func(c *config.Config) bool { return c.GetSetById("yt").Discovery.Watchdog }},
		{"add global entry", http.MethodPost, "/api/watchdog/domains", `{"domain":"example.com"}`, http.StatusOK,
			func(c *config.Config) bool { return slices.Contains(c.System.Checker.Watchdog.Domains, "example.com") }},
		{"remove global entry", http.MethodDelete, "/api/watchdog/domains/example.net", "", http.StatusOK,
			func(c *config.Config) bool { return !slices.Contains(c.System.Checker.Watchdog.Domains, "example.net") }},
		{"master on", http.MethodPost, "/api/watchdog/enable", "", http.StatusOK,
			func(c *config.Config) bool { return c.System.Checker.Watchdog.Enabled }},
		{"move entry", http.MethodPost, "/api/watchdog/domains/rutracker.org/move", `{"set_id":"tr"}`, http.StatusOK,
			func(c *config.Config) bool {
				return c.GetSetById("tr").Discovery.Watchdog && !slices.Contains(c.System.Checker.Watchdog.Domains, "rutracker.org")
			}},
		{"discovery add", http.MethodPost, "/api/discovery/add", `{"targets":{"sni_domains":["example.org"]}}`, http.StatusAccepted,
			func(c *config.Config) bool { return len(c.Sets) == 3 && c.Sets[0].Name == "example.org" }},
		{"discovery replace", http.MethodPost, "/api/discovery/replace", `{"set_id":"tr","domains":["rutracker.org"],"set":{"fragmentation":{"strategy":"disorder"}}}`, http.StatusOK,
			func(c *config.Config) bool { return c.GetSetById("tr").Fragmentation.Strategy == "disorder" }},
		{"geodat remove", http.MethodPost, "/api/geodat/remove", `{"type":"both"}`, http.StatusOK,
			func(c *config.Config) bool { return c.System.Geo.GeoSitePath == "" && c.System.Geo.GeoIpPath == "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api, mux := concurrentAdoptAPI(t)
			rec := serveWhileLocked(t, mux, c.method, c.path, c.body, func() {
				storeConcurrently(t, api, func(cfg *config.Config) {
					cfg.GetSetById("yt").Fragmentation.Strategy = "oob"
					cfg.System.Geo.AutoUpdate.LastRun = "2026-09-24T12:00:00Z"
				})
			})
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d (%s)", rec.Code, c.status, rec.Body.String())
			}
			live := api.getCfg()
			if !c.check(live) {
				t.Error("the request's own change is written")
			}
			if yt := live.GetSetById("yt"); yt != nil && yt.Fragmentation.Strategy != "oob" {
				t.Errorf("an adopt that landed while the request waited is kept, got %q", yt.Fragmentation.Strategy)
			}
			if live.System.Geo.AutoUpdate.LastRun != "2026-09-24T12:00:00Z" {
				t.Error("an unrelated save that landed while the request waited is kept")
			}
		})
	}
}

func TestRESTWritesRefuseAgainstTheCurrentConfig(t *testing.T) {
	api, mux := concurrentAdoptAPI(t)

	rec := serveWhileLocked(t, mux, http.MethodPost, "/api/sets/tr/add-domain", `{"domain":"rutracker.net"}`, func() {
		storeConcurrently(t, api, func(c *config.Config) {
			c.Sets = slices.DeleteFunc(c.Sets, func(s *config.SetConfig) bool { return s.Id == "tr" })
		})
	})
	expectCode(t, rec, http.StatusNotFound, "not_found")

	rec = serveWhileLocked(t, mux, http.MethodPost, "/api/watchdog/domains", `{"domain":"example.com"}`, func() {
		storeConcurrently(t, api, func(c *config.Config) {
			c.System.Checker.Watchdog.Domains = append(c.System.Checker.Watchdog.Domains, "https://example.com/")
		})
	})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "domain already in watchdog list") {
		t.Errorf("an entry added while the request waited makes it a duplicate: %d %s", rec.Code, rec.Body.String())
	}

	if rec := serve(mux, http.MethodDelete, "/api/watchdog/domains/nope.example", ""); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "domain not found in watchdog list") {
		t.Errorf("removing an unknown entry: %d %s", rec.Code, rec.Body.String())
	}
	expectCode(t, serve(mux, http.MethodPut, "/api/watchdog/sets/nope", `{"enabled":true}`), http.StatusNotFound, "not_found")
	expectCode(t, serve(mux, http.MethodPost, "/api/discovery/replace", `{"set_id":"nope","domains":["a.example"]}`), http.StatusNotFound, "not_found")

	before := watchdog.ConfigRevision(api.getCfg())
	rec = serve(mux, http.MethodPost, "/api/sets/batch-set-enabled", `{"ids":["yt"],"enabled":true}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"updated":0`) {
		t.Errorf("a toggle that changes nothing answers updated 0: %d %s", rec.Code, rec.Body.String())
	}
	rec = serve(mux, http.MethodPut, "/api/watchdog/sets/yt", `{"enabled":false}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "watchdog for set yt is off") {
		t.Errorf("a watchdog switch that changes nothing still answers: %d %s", rec.Code, rec.Body.String())
	}
	if watchdog.ConfigRevision(api.getCfg()) != before {
		t.Error("a request that changes nothing writes nothing")
	}
}

func setWatchedButOff(cfg *config.Config) {
	cfg.Sets[0].Discovery.Watchdog = true
	cfg.Sets[0].Enabled = false
}

func setWatchedIPOnly(cfg *config.Config) {
	cfg.Sets[0].Discovery.Watchdog = true
	cfg.Sets[0].Targets.SNIDomains = nil
	cfg.Sets[0].Targets.IPs = []string{"203.0.113.7"}
}

func TestMCPForwardWritesNeedTheProbeGateToStartProbing(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*config.Config)
		call    func(*testing.T, *mcp.ClientSession, context.Context) *mcp.CallToolResult
	}{
		{"set_config_value enables a watched set", setWatchedButOff,
			func(t *testing.T, s *mcp.ClientSession, ctx context.Context) *mcp.CallToolResult {
				return callSetValue(t, s, ctx, "sets[video].enabled", "true")
			}},
		{"manage_set enables a watched set", setWatchedButOff,
			func(t *testing.T, s *mcp.ClientSession, ctx context.Context) *mcp.CallToolResult {
				return callManageSet(t, s, ctx, map[string]any{"action": "set_enabled", "set": "video", "enabled": "true"})
			}},
		{"edit_set_targets gives a watched IP-only set its first domain", setWatchedIPOnly,
			func(t *testing.T, s *mcp.ClientSession, ctx context.Context) *mcp.CallToolResult {
				return callEditTargets(t, s, ctx, map[string]any{"set": "video", "kind": "sni_domains", "add": "youtube.com"})
			}},
	}
	for _, c := range cases {
		for _, probes := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s probes=%v", c.name, probes), func(t *testing.T) {
				cfg := mcpWatchdogCfg(true, probes)
				c.prepare(cfg)
				mcpResetHistory()
				t.Cleanup(mcpResetHistory)
				srv, api := newMCPTestServerAPI(t, cfg)
				session, ctx := connectMCP(t, srv)
				before := watchdog.ConfigRevision(api.getCfg())

				res := c.call(t, session, ctx)
				live := api.getCfg().GetSetById("set-1")
				if probes {
					if res.IsError || !live.WatchdogActive() {
						t.Fatalf("with the probe gate on the write lands: %s", mcpErrorText(res))
					}
					return
				}
				if !res.IsError || !strings.Contains(mcpErrorText(res), "allow_active_probes") || !strings.Contains(mcpErrorText(res), "video") {
					t.Fatalf("a write that starts the set's watchdog needs allow_active_probes: %s", mcpErrorText(res))
				}
				if watchdog.ConfigRevision(api.getCfg()) != before || live.WatchdogActive() {
					t.Error("the refused write changed nothing")
				}
				if len(mcpHistory) != 0 {
					t.Errorf("a refused write is not on the undo list, got %d", len(mcpHistory))
				}
			})
		}
	}
}

func TestMCPStartsProbesForwardOnlyCountsWatchedSets(t *testing.T) {
	current := mcpTestCfg()
	next := current.Clone()
	next.Sets[0].Discovery.URLs = []string{"https://www.youtube.com/"}
	if what := mcpStartsProbes(next, current, false); what != "" {
		t.Errorf("a URL on a set nobody watches starts nothing: %q", what)
	}
	if what := mcpRevertStartsProbes(next, current); !strings.Contains(what, "discovery URL") {
		t.Errorf("the undo guard still counts every new URL: %q", what)
	}

	current.Sets[0].Discovery.URLs = []string{"https://www.youtube.com/"}
	current.Sets[0].Discovery.Watchdog = true
	next = current.Clone()
	next.Sets[0].Discovery.URLs = append(next.Sets[0].Discovery.URLs, "https://m.youtube.com/")
	if what := mcpStartsProbes(next, current, false); !strings.Contains(what, "m.youtube.com") {
		t.Errorf("a new URL on a watched set is caught: %q", what)
	}

	next = current.Clone()
	next.Sets[0].Enabled = false
	if what := mcpStartsProbes(next, current, false); what != "" {
		t.Errorf("disabling a watched set starts nothing: %q", what)
	}
	next = current.Clone()
	next.System.Checker.Watchdog.Enabled = true
	if what := mcpStartsProbes(next, current, false); !strings.Contains(what, "master switch") {
		t.Errorf("turning the master switch on is caught: %q", what)
	}
}
