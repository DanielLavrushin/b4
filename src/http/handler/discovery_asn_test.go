package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/geodat"
)

func TestAddPresetAsSetDropsASNs(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	api := &API{cfgPtr: testCfgPtr(&cfg), geodataManager: geodat.NewGeodataManager("", "")}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterDiscoveryApi()

	body := `{"name":"Telegram","targets":{"sni_domains":["t.me"],"asns":["62041"]}}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/discovery/add", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	sets := api.getCfg().Sets
	if len(sets) != 1 {
		t.Fatalf("expected one set, got %d", len(sets))
	}
	got := sets[0].Targets
	if got.ASNs == nil || len(got.ASNs) != 0 {
		t.Errorf("a discovered set is scoped to its domains and never carries ASNs, got %#v", got.ASNs)
	}
	if len(got.IpsToMatch) != 0 {
		t.Errorf("no ASN prefix may reach the match list of a discovered set: %v", got.IpsToMatch)
	}
	if !reflect.DeepEqual(got.SNIDomains, []string{"t.me"}) {
		t.Errorf("the domains must be kept: %v", got.SNIDomains)
	}
}

func TestReplaceStrategyKeepsTheTargetSetsASNs(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	api, mux := replaceFixture(t, func(set *config.SetConfig) {
		set.Targets.ASNs = []string{"62041"}
		set.Targets.IpsToMatch = append([]string{}, telegramPrefixes...)
	})
	strategy := config.NewSetConfig()
	strategy.Faking.Strategy = "pastseq"
	strategy.Targets.ASNs = []string{"15169"}
	body, err := json.Marshal(DiscoveryReplaceRequest{SetId: "set-a", Set: strategy, Domains: []string{"meduza.io"}})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/discovery/replace", strings.NewReader(string(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: %d (%s)", rec.Code, rec.Body.String())
	}
	target, _ := replaceSets(api)
	if target.Faking.Strategy != "pastseq" {
		t.Errorf("the strategy must be replaced, got %q", target.Faking.Strategy)
	}
	if !reflect.DeepEqual(target.Targets.ASNs, []string{"62041"}) {
		t.Errorf("replacing the strategy must keep the set's own ASNs, got %v", target.Targets.ASNs)
	}
	if !reflect.DeepEqual(target.Targets.IpsToMatch, telegramPrefixes) {
		t.Errorf("the set must keep matching its ASN's prefixes, got %v", target.Targets.IpsToMatch)
	}
}

func TestMCPDiscoveryApplyDropsASNs(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	set := config.NewSetConfig()
	set.Name = "found"
	set.Targets.SNIDomains = []string{"t.me"}
	set.Targets.ASNs = []string{"62041"}
	suite := &discovery.CheckSuite{
		Id:     "asn-run",
		Status: discovery.CheckStatusComplete,
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"t.me": {Domain: "t.me", BestPreset: "combo", BestSuccess: true},
		},
		StrategyGroups: []discovery.StrategyGroup{
			{WinnerPreset: "combo", Family: "combo", Domains: []string{"t.me"}, Set: &set},
		},
	}
	discovery.RegisterSuite(suite)
	mcpRememberSuite(suite.Id)
	t.Cleanup(func() { mcpRememberSuite("") })

	applied := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "apply", "domain": "t.me"}))
	if !applied.Changed || applied.Applied == nil {
		t.Fatalf("apply must create the set: %+v", applied)
	}
	created := findSetIn(api.getCfg(), applied.Applied.Id)
	if created == nil {
		t.Fatalf("the applied set is not in the config")
	}
	if len(created.Targets.ASNs) != 0 || len(created.Targets.IpsToMatch) != 0 {
		t.Errorf("a set built from a discovery run never carries ASNs: asns=%v ips=%v", created.Targets.ASNs, created.Targets.IpsToMatch)
	}
	if !reflect.DeepEqual(set.Targets.ASNs, []string{"62041"}) {
		t.Errorf("applying must not touch the run's result, got %v", set.Targets.ASNs)
	}
}
