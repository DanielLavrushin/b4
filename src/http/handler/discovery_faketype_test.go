package handler

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

func randomFakeStrategy() config.SetConfig {
	strategy := discoveredStrategy()
	strategy.Faking.SNIType = config.FakePayloadRandom
	return strategy
}

func TestAddAsNewSetKeepsTheFakeTypeItWasSent(t *testing.T) {
	api, mux := pageHostsFixture(t)

	random := randomFakeStrategy()
	random.Name = "random"
	random.Targets.SNIDomains = []string{"random.example"}
	if rec := postDiscovery(t, mux, "/api/discovery/add", DiscoveredSetRequest{Set: mustJSON(t, random)}); rec.Code != http.StatusAccepted {
		t.Fatalf("add: %d (%s)", rec.Code, rec.Body.String())
	}
	if got := setNamed(api, "random").Faking.SNIType; got != config.FakePayloadRandom {
		t.Errorf("the strategy was tested with the Random fake, so the set made from it must send that, got type %d", got)
	}

	body := `{"name":"unnamed fake","targets":{"sni_domains":["unnamed.example"]}}`
	if rec := postDiscovery(t, mux, "/api/discovery/add", DiscoveredSetRequest{Set: []byte(body)}); rec.Code != http.StatusAccepted {
		t.Fatalf("add: %d (%s)", rec.Code, rec.Body.String())
	}
	if got := setNamed(api, "unnamed fake").Faking.SNIType; got != config.DefaultSetConfig.Faking.SNIType {
		t.Errorf("a body that names no fake must get the default, got type %d", got)
	}
}

func TestReplaceKeepsTheFakeTypeItWasSent(t *testing.T) {
	for _, strategyOnly := range []bool{true, false} {
		api, mux := replaceFixture(t, nil)
		target, _ := replaceSets(api)
		rec := replaceWith(t, mux, DiscoveryReplaceRequest{
			SetId:        target.Id,
			Domains:      []string{"www.youtube.com"},
			Set:          randomFakeStrategy(),
			StrategyOnly: strategyOnly,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("replace: %d (%s)", rec.Code, rec.Body.String())
		}
		after, _ := replaceSets(api)
		if got := after.Faking.SNIType; got != config.FakePayloadRandom {
			t.Errorf("strategy_only=%v: the strategy was tested with the Random fake, so the set must send that, got type %d", strategyOnly, got)
		}
	}
}

func TestARandomFakeIsNotTheSameStrategyAsTheDefaultFake(t *testing.T) {
	random := config.NewSetConfig()
	random.Faking.SNIType = config.FakePayloadRandom
	sameRandom := config.NewSetConfig()
	sameRandom.Faking.SNIType = config.FakePayloadRandom
	fallback := config.NewSetConfig()

	if setsHaveSimilarConfig(&random, &fallback) {
		t.Error("a site tested with one fake must not be offered a set that sends another")
	}
	if !setsHaveSimilarConfig(&random, &sameRandom) {
		t.Error("two sets with the Random fake carry the same strategy")
	}
}

func TestMCPDiscoveryApplyKeepsARandomFake(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	news := config.NewSetConfig()
	news.Id, news.Name, news.Enabled = "news", "news", true
	news.Targets.SNIDomains = []string{"svoboda.org"}
	cfg.Sets = append(cfg.Sets, &news)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)
	t.Cleanup(func() { mcpRememberSuite("") })

	found := randomFakeStrategy()
	found.Name = "found"
	found.Targets.SNIDomains = []string{"rutracker.org"}
	run := &discovery.CheckSuite{
		Id: "random-fake-run", Status: discovery.CheckStatusComplete,
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"rutracker.org": {Domain: "rutracker.org", BestPreset: "fake-payloadRand", BestSuccess: true, Confirmed: 3},
		},
		StrategyGroups: []discovery.StrategyGroup{{WinnerPreset: "fake-payloadRand", Domains: []string{"rutracker.org"}, Set: &found}},
	}
	discovery.RegisterSuite(run)
	applied := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "apply", "domain": "rutracker.org", "id": run.Id}))
	if !applied.Changed || applied.Applied == nil {
		t.Fatalf("apply must create a set: %+v", applied)
	}
	if got := api.getCfg().GetSetById(applied.Applied.Id).Faking.SNIType; got != config.FakePayloadRandom {
		t.Errorf("a new set must send the fake the strategy was tested with, got type %d", got)
	}

	winner := randomFakeStrategy()
	setRun := &discovery.CheckSuite{
		Id: "random-fake-set-run", Status: discovery.CheckStatusComplete, SetId: "news",
		SetVerdict: &discovery.SetVerdict{
			Status: discovery.SetVerdictCovered, WinnerPreset: "fake-payloadRand", Covered: []string{"svoboda.org"}, Set: &winner,
		},
	}
	discovery.RegisterSuite(setRun)
	written := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "apply", "set": "news", "id": setRun.Id}))
	if !written.Changed {
		t.Fatalf("apply must write the verdict's strategy: %+v", written)
	}
	if got := api.getCfg().GetSetById("news").Faking.SNIType; got != config.FakePayloadRandom {
		t.Errorf("the set must send the fake the strategy was tested with, got type %d", got)
	}
}
