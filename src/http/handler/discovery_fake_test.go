package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

const appliedCapture = "captures/tls_applied_example.bin"

var appliedCaptureBytes = []byte("the capture this result was tested with")

func writeAppliedCapture(t *testing.T, configPath string) {
	t.Helper()
	path := filepath.Join(filepath.Dir(configPath), appliedCapture)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, appliedCaptureBytes, 0o644); err != nil {
		t.Fatal(err)
	}
}

func namesAppliedCapture(set *config.SetConfig) {
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = appliedCapture
}

func sendsNoFake(set *config.SetConfig) {
	set.Faking.SNI = false
	set.TCP.SynFake = false
	set.Fragmentation.Combo.DecoyEnabled = false
}

type fakeApplyCase struct {
	name     string
	shape    func(set *config.SetConfig)
	withFile bool
	applied  bool
}

func fakeApplyCases() []fakeApplyCase {
	return []fakeApplyCase{
		{"a fake SNI whose capture is gone", func(set *config.SetConfig) {
			sendsNoFake(set)
			namesAppliedCapture(set)
			set.Faking.SNI = true
		}, false, false},
		{"a fake SYN whose capture is gone", func(set *config.SetConfig) {
			sendsNoFake(set)
			namesAppliedCapture(set)
			set.TCP.SynFake, set.TCP.SynFakeLen = true, 64
		}, false, false},
		{"a fake SYN of length 0 with a gone capture", func(set *config.SetConfig) {
			sendsNoFake(set)
			namesAppliedCapture(set)
			set.TCP.SynFake = true
		}, false, true},
		{"a combo decoy whose capture is gone", func(set *config.SetConfig) {
			sendsNoFake(set)
			namesAppliedCapture(set)
			set.Fragmentation.Combo.DecoyEnabled = true
		}, false, false},
		{"a fake SNI whose capture is there", func(set *config.SetConfig) {
			sendsNoFake(set)
			namesAppliedCapture(set)
			set.Faking.SNI = true
		}, true, true},
		{"a gone capture named by a strategy that sends no fake", func(set *config.SetConfig) {
			sendsNoFake(set)
			namesAppliedCapture(set)
		}, false, true},
		{"a built-in fake with a stale file name", func(set *config.SetConfig) {
			sendsNoFake(set)
			set.Faking.SNI = true
			set.Faking.SNIType = config.FakePayloadDefault1
			set.Faking.PayloadFile = appliedCapture
		}, false, true},
	}
}

func expectFakePayloadRefusal(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var refusal APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &refusal); err != nil {
		t.Fatalf("want an API error, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusConflict || refusal.Code != "fake_payload_missing" || len(refusal.Fields) != 1 {
		t.Fatalf("want a 409 fake_payload_missing refusal, got %d (%s)", rec.Code, rec.Body.String())
	}
	field := refusal.Fields[0]
	if field.Code != "fake_payload_missing" || field.Path != "faking.payload_file" || field.Params["file"] != appliedCapture {
		t.Errorf("the refusal must name the capture so the UI can show it, got %+v", field)
	}
}

func TestDiscoveryApplyPathsRefuseACaptureThatIsGone(t *testing.T) {
	strategyFrom := func(c fakeApplyCase) config.SetConfig {
		strategy := config.NewSetConfig()
		strategy.Faking.Strategy = "pastseq"
		c.shape(&strategy)
		return strategy
	}
	post := func(mux *http.ServeMux, path string, body []byte) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)))
		return rec
	}

	paths := []struct {
		name    string
		mutate  func(c fakeApplyCase) func(*config.SetConfig)
		apply   func(t *testing.T, mux *http.ServeMux, target *config.SetConfig, c fakeApplyCase) *httptest.ResponseRecorder
		written func(api *API, before int, target *config.SetConfig) bool
		success int
	}{
		{
			name:   "add as a new set",
			mutate: func(fakeApplyCase) func(*config.SetConfig) { return nil },
			apply: func(t *testing.T, mux *http.ServeMux, _ *config.SetConfig, c fakeApplyCase) *httptest.ResponseRecorder {
				strategy := strategyFrom(c)
				strategy.Targets.SNIDomains = []string{"youtube.com"}
				return post(mux, "/api/discovery/add", mustJSON(t, DiscoveredSetRequest{Set: mustJSON(t, strategy)}))
			},
			written: func(api *API, before int, _ *config.SetConfig) bool {
				return len(api.getCfg().Sets) == before+1
			},
			success: http.StatusAccepted,
		},
		{
			name:   "replace the strategy of a set",
			mutate: func(fakeApplyCase) func(*config.SetConfig) { return nil },
			apply: func(t *testing.T, mux *http.ServeMux, _ *config.SetConfig, c fakeApplyCase) *httptest.ResponseRecorder {
				return post(mux, "/api/discovery/replace", mustJSON(t, DiscoveryReplaceRequest{SetId: "set-a", Set: strategyFrom(c), Domains: []string{"meduza.io"}}))
			},
			written: func(api *API, _ int, _ *config.SetConfig) bool {
				target, _ := replaceSets(api)
				return target.Faking.Strategy == "pastseq"
			},
			success: http.StatusOK,
		},
		{
			name:   "write a set run's verdict into its set",
			mutate: func(fakeApplyCase) func(*config.SetConfig) { return nil },
			apply: func(t *testing.T, mux *http.ServeMux, _ *config.SetConfig, c fakeApplyCase) *httptest.ResponseRecorder {
				return post(mux, "/api/discovery/replace", mustJSON(t, DiscoveryReplaceRequest{SetId: "set-a", Set: strategyFrom(c), StrategyOnly: true, KeepTargets: true}))
			},
			written: func(api *API, _ int, _ *config.SetConfig) bool {
				target, _ := replaceSets(api)
				return target.Faking.Strategy == "pastseq"
			},
			success: http.StatusOK,
		},
		{
			name:   "add the domains to a set with the same strategy",
			mutate: func(c fakeApplyCase) func(*config.SetConfig) { return c.shape },
			apply: func(t *testing.T, mux *http.ServeMux, target *config.SetConfig, _ fakeApplyCase) *httptest.ResponseRecorder {
				return post(mux, "/api/sets/set-a/add-domain", mustJSON(t, map[string]any{"domains": []string{"new.example"}, "tested": target}))
			},
			written: func(api *API, _ int, _ *config.SetConfig) bool {
				target, _ := replaceSets(api)
				return domainInList(target.Targets.SNIDomains, "new.example")
			},
			success: http.StatusOK,
		},
	}

	for _, path := range paths {
		for _, c := range fakeApplyCases() {
			t.Run(path.name+": "+c.name, func(t *testing.T) {
				api, mux := replaceFixture(t, path.mutate(c))
				api.RegisterSetsApi()
				if c.withFile {
					writeAppliedCapture(t, api.getCfg().ConfigPath)
				}
				target, _ := replaceSets(api)
				cfgBefore := api.getCfg()
				before := len(cfgBefore.Sets)

				rec := path.apply(t, mux, target, c)

				if written := path.written(api, before, target); written != c.applied {
					t.Fatalf("want written %t, got %t: %d (%s)", c.applied, written, rec.Code, rec.Body.String())
				}
				if c.applied {
					if rec.Code != path.success {
						t.Errorf("want %d, got %d (%s)", path.success, rec.Code, rec.Body.String())
					}
					return
				}
				expectFakePayloadRefusal(t, rec)
				if api.getCfg() != cfgBefore {
					t.Error("a refused apply must leave the configuration as it was")
				}
			})
		}
	}
}

func TestAddDomainFromElsewhereIsNotCheckedForTheCapture(t *testing.T) {
	api, mux := replaceFixture(t, func(set *config.SetConfig) {
		set.Faking.SNI = true
		namesAppliedCapture(set)
	})
	api.RegisterSetsApi()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/sets/set-a/add-domain", strings.NewReader(`{"domains":["new.example"]}`)))
	target, _ := replaceSets(api)
	if rec.Code != http.StatusOK || !domainInList(target.Targets.SNIDomains, "new.example") {
		t.Errorf("adding a domain by hand tested nothing, so a missing capture must not stop it, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func mcpCaptureRunCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	return cfg
}

func capturedResult(shape func(set *config.SetConfig)) *config.SetConfig {
	set := config.NewSetConfig()
	set.Name = "found"
	set.Targets.SNIDomains = []string{"rutracker.org"}
	shape(&set)
	return &set
}

func TestMCPDiscoveryApplyRefusesAResultWhoseCaptureIsGone(t *testing.T) {
	saved := func(t *testing.T, cfg *config.Config, set *config.SetConfig) map[string]any {
		t.Helper()
		discovery.SaveToHistory(&discovery.CheckSuite{
			Id: "history-capture", Status: discovery.CheckStatusComplete, EndTime: time.Now(),
			DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
				"rutracker.org": {Domain: "rutracker.org", Url: "https://rutracker.org/", BestPreset: "combo", BestSuccess: true, Confirmed: 3},
			},
			StrategyGroups: []discovery.StrategyGroup{{WinnerPreset: "combo", Domains: []string{"rutracker.org"}, Set: set}},
		}, cfg.ConfigPath)
		mcpRememberSuite("")
		return map[string]any{"action": "apply", "domain": "rutracker.org"}
	}
	live := func(t *testing.T, _ *config.Config, set *config.SetConfig) map[string]any {
		t.Helper()
		suite := &discovery.CheckSuite{
			Id: "live-capture-" + strings.ReplaceAll(t.Name(), "/", "-"), Status: discovery.CheckStatusComplete,
			DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
				"rutracker.org": {Domain: "rutracker.org", BestPreset: "combo", BestSuccess: true, Confirmed: 3},
			},
			StrategyGroups: []discovery.StrategyGroup{{WinnerPreset: "combo", Domains: []string{"rutracker.org"}, Set: set}},
		}
		discovery.RegisterSuite(suite)
		return map[string]any{"action": "apply", "domain": "rutracker.org", "id": suite.Id}
	}

	for name, source := range map[string]func(*testing.T, *config.Config, *config.SetConfig) map[string]any{
		"a saved result":  saved,
		"a run in memory": live,
	} {
		for _, c := range fakeApplyCases() {
			t.Run(name+": "+c.name, func(t *testing.T) {
				cfg := mcpCaptureRunCfg(t)
				if c.withFile {
					writeAppliedCapture(t, cfg.ConfigPath)
				}
				srv, api := newMCPTestServerAPI(t, cfg)
				session, ctx := connectMCP(t, srv)
				t.Cleanup(func() { mcpRememberSuite("") })
				before := len(api.getCfg().Sets)

				res := callDiscovery(t, session, ctx, source(t, cfg, capturedResult(c.shape)))

				if !c.applied {
					text := mcpErrorText(res)
					if !res.IsError || !strings.Contains(text, "nothing written") || !strings.Contains(text, appliedCapture) {
						t.Errorf("want a refusal naming the capture, got %q", text)
					}
					if got := len(api.getCfg().Sets); got != before {
						t.Errorf("the refused apply created a set: %d sets, want %d", got, before)
					}
					return
				}
				if res.IsError {
					t.Fatalf("apply: %s", mcpErrorText(res))
				}
				out := decodeDiscovery(t, res)
				created := api.getCfg().GetSetById(out.Applied.Id)
				if created == nil {
					t.Fatalf("the applied set is not in the config: %+v", out)
				}
				if c.withFile && !bytes.Equal(created.Faking.PayloadData, appliedCaptureBytes) {
					t.Errorf("the set must send the capture it was tested with, got %d bytes", len(created.Faking.PayloadData))
				}
			})
		}
	}
}

func TestMCPDiscoveryApplyDoesNotCallAGoneCaptureCovered(t *testing.T) {
	cfg := mcpCaptureRunCfg(t)
	existing := capturedResult(func(set *config.SetConfig) {
		set.Faking.SNI = true
		namesAppliedCapture(set)
	})
	existing.Id, existing.Name, existing.Enabled = "same-strategy", "same strategy", true
	cfg.Sets = append(cfg.Sets, existing)
	srv, _ := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)
	t.Cleanup(func() { mcpRememberSuite("") })

	found := capturedResult(func(set *config.SetConfig) {
		set.Faking.SNI = true
		namesAppliedCapture(set)
	})
	suite := &discovery.CheckSuite{
		Id: "covered-capture", Status: discovery.CheckStatusComplete,
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"rutracker.org": {Domain: "rutracker.org", BestPreset: "combo", BestSuccess: true, Confirmed: 3},
		},
		StrategyGroups: []discovery.StrategyGroup{{WinnerPreset: "combo", Domains: []string{"rutracker.org"}, Set: found}},
	}
	discovery.RegisterSuite(suite)

	res := callDiscovery(t, session, ctx, map[string]any{"action": "apply", "domain": "rutracker.org", "id": suite.Id})
	if !res.IsError || !strings.Contains(mcpErrorText(res), appliedCapture) {
		t.Errorf("that set sends the built-in fake now, so it does not carry what the run tested: %s", mcpErrorText(res))
	}
}

func TestMCPDiscoveryApplyForASetRefusesACaptureThatIsGone(t *testing.T) {
	inMemory := func(cfg *config.Config, suite *discovery.CheckSuite) {
		discovery.RegisterSuite(suite)
		mcpRememberSuite(suite.Id)
	}
	savedOnly := func(cfg *config.Config, suite *discovery.CheckSuite) {
		suite.EndTime = time.Now()
		discovery.SaveToHistory(suite, cfg.ConfigPath)
		mcpRememberSuite("")
	}
	for name, source := range map[string]func(*config.Config, *discovery.CheckSuite){
		"a run in memory": inMemory,
		"a saved run":     savedOnly,
	} {
		for _, c := range fakeApplyCases() {
			t.Run(name+": "+c.name, func(t *testing.T) {
				cfg := mcpCaptureRunCfg(t)
				if c.withFile {
					writeAppliedCapture(t, cfg.ConfigPath)
				}
				srv, api := newMCPTestServerAPI(t, cfg)
				session, ctx := connectMCP(t, srv)
				mcpResetHistory()
				t.Cleanup(func() { mcpRememberSuite("") })

				verdict := coveredVerdict()
				c.shape(verdict.Set)
				source(cfg, &discovery.CheckSuite{
					Id: "set-run-capture-" + strings.ReplaceAll(t.Name(), "/", "-"), Status: discovery.CheckStatusComplete, SetId: "set-1",
					StoppedCovered: true, SetVerdict: verdict,
				})

				res := callDiscovery(t, session, ctx, map[string]any{"action": "apply", "set": "video"})
				live := api.getCfg().GetSetById("set-1")
				adopted := live.Fragmentation.Strategy == "tls"
				if adopted != c.applied {
					t.Fatalf("want the strategy written %t, got %t: %s", c.applied, adopted, mcpErrorText(res))
				}
				if !c.applied && (!res.IsError || !strings.Contains(mcpErrorText(res), "nothing written") || !strings.Contains(mcpErrorText(res), appliedCapture)) {
					t.Errorf("want a refusal naming the capture, got %q", mcpErrorText(res))
				}
			})
		}
	}
}
