package handler

import (
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/hubwire"
)

func groupedPresetIDs(presets []discovery.ConfigPreset) []string {
	out := []string{}
	for _, p := range presets {
		out = append(out, p.Config.Hub.ID)
	}
	slices.Sort(out)
	return out
}

func hubPreset(id string) discovery.ConfigPreset {
	set := hubStrategySet(id, "one.example")
	set.Hub = &config.HubOrigin{ID: id, Version: 1}
	return discovery.ConfigPreset{Name: "cached-1-combo", Config: set}
}

func publishOrphanedMembers(t *testing.T, env *hubEnv) {
	t.Helper()
	brought := hubCatalogueMember(t, "brought", 1, "alice", "one.example")
	brought.Status = hubwire.SetStatusGrouped
	alone := hubCatalogueMemberWith(t, "alone", 1, "bob", func(set *config.SetConfig) { set.Faking.TTL = 9 }, "one.example")
	orphan := hubCatalogueMemberWith(t, "orphan", 1, "carol", func(set *config.SetConfig) { set.Faking.TTL = 5 }, "one.example")
	orphan.Status = hubwire.SetStatusGrouped
	stranded := hubCatalogueMemberWith(t, "stranded", 1, "dave", func(set *config.SetConfig) { set.Faking.TTL = 4 }, "one.example")
	stranded.Status = hubwire.SetStatusGrouped
	hidden := hubCatalogueMemberWith(t, "hidden", 1, "dave", func(set *config.SetConfig) { set.Faking.TTL = 3 }, "other.example")
	hidden.Status = hubwire.SetStatusHidden
	for _, cs := range []*hubwire.CatalogueSet{&orphan, &stranded} {
		cs.UpdatedAt = "2099-01-01T00:00:00Z"
	}
	groups := []hubwire.CatalogueGroup{
		{ID: "g", Version: 1, Title: "pair", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "brought"}, {SetID: "alone"}}},
		{ID: "g-hidden", Version: 1, Title: "half", Author: "dave", Members: []hubwire.CatalogueGroupMember{{SetID: "stranded"}, {SetID: "hidden"}}},
	}
	env.publishCatalogue(t, 1, []hubwire.CatalogueSet{brought, alone, orphan, stranded, hidden}, groups)
}

func TestHubApplyNeverTakesAGroupedSetAlone(t *testing.T) {
	env := newHubEnv(t)
	publishOrphanedMembers(t, env)
	other := hubGroupLocalSet("mine", "mine", "", "mine.example")
	copyOf := hubGroupLocalSet("copy", "brought", "pair", "one.example")
	copyOf.Hub = &config.HubOrigin{ID: "brought", Version: 1}
	env.update(func(cfg *config.Config) { cfg.Sets = append(cfg.Sets, other, copyOf) })
	before := len(env.api.getCfg().Sets)

	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/brought/apply", map[string]any{}), http.StatusConflict, "hub_grouped")
	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/brought/apply", map[string]any{"replace": "mine"}), http.StatusConflict, "hub_grouped")
	if got := len(env.api.getCfg().Sets); got != before || env.localSet("mine").Hub != nil {
		t.Fatalf("a refused apply writes nothing: %d sets, mine stamped %+v", got, env.localSet("mine").Hub)
	}

	expectCode(t, postJSON(t, env.mux, "/api/hub/sets/brought/apply", map[string]any{"replace": "copy"}), http.StatusConflict, "hub_grouped")
	if got := env.localSet("copy"); got == nil || got.Hub == nil || *got.Hub != (config.HubOrigin{ID: "brought", Version: 1}) {
		t.Fatalf("a refused update rewrites the copy: %+v", got)
	}
	if rec := postJSON(t, env.mux, "/api/hub/sets/alone/apply", map[string]any{}); rec.Code != http.StatusAccepted {
		t.Fatalf("an active set still applies on its own: %d %s", rec.Code, rec.Body.String())
	}
}

func TestInteractiveDiscoveryQueuesAGroupedMemberOnlyWithAGroupThatResolves(t *testing.T) {
	env := newHubEnv(t)
	publishOrphanedMembers(t, env)

	if got := groupedPresetIDs(env.api.communityPresets([]string{"one.example"}, false, true)); !slices.Equal(got, []string{"alone", "brought"}) {
		t.Errorf("a grouped set whose group does not resolve here can be applied neither alone nor with a group, so it must not take a slot; got %v", got)
	}
}

func TestDiscoveryRunsFilterCachedGroupedStrategies(t *testing.T) {
	env := newHubEnv(t)
	publishOrphanedMembers(t, env)

	var got discovery.StartSuiteOptions
	prev := discoveryStartSuite
	t.Cleanup(func() { discoveryStartSuite = prev })
	discoveryStartSuite = func(_ *discovery.Runtime, _ *config.Config, _ []string, opts discovery.StartSuiteOptions) (*discovery.DiscoverySuite, error) {
		got = opts
		return nil, discovery.ErrDiscoveryAlreadyRunning
	}

	env.api.discoveryRT = discovery.NewRuntime()
	env.api.RegisterDiscoveryApi()
	postJSON(t, env.mux, "/api/discovery/start", map[string]any{"check_url": "one.example"})
	if got.KeepPreset == nil {
		t.Fatal("an interactive run must filter the cached and community strategies it tests")
	}
	anonymous := hubPreset("x")
	anonymous.Config.Hub = nil
	for id, keep := range map[string]bool{"brought": true, "alone": true, "orphan": false, "stranded": false} {
		if got.KeepPreset(hubPreset(id)) != keep {
			t.Errorf("interactive run: a cached strategy of %s kept %v, want %v", id, !keep, keep)
		}
	}
	if !got.KeepPreset(anonymous) {
		t.Error("interactive run: a strategy without a hub origin must be tested")
	}

	env.update(func(cfg *config.Config) { cfg.System.Hub.Enabled = false })
	got = discovery.StartSuiteOptions{}
	postJSON(t, env.mux, "/api/discovery/start", map[string]any{"check_url": "one.example"})
	if got.KeepPreset == nil || got.KeepPreset(hubPreset("brought")) {
		t.Error("with Community off the group cannot be applied, so an interactive run must not test a cached strategy offered only with it")
	}
	if !got.KeepPreset(hubPreset("alone")) || !got.KeepPreset(anonymous) {
		t.Error("with Community off an interactive run still tests standalone and anonymous cached strategies")
	}
	env.update(func(cfg *config.Config) { cfg.System.Hub.Enabled = true })

	cfg := probeCfg(t)
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	api.discoveryRT = discovery.NewRuntime()
	session, ctx := connectMCP(t, srv)
	got = discovery.StartSuiteOptions{}
	callDiscovery(t, session, ctx, map[string]any{"action": "start", "domains": "one.example"})
	if got.KeepPreset == nil {
		t.Fatal("an MCP run must filter the cached and community strategies it tests")
	}
	for id, keep := range map[string]bool{"brought": false, "alone": true, "orphan": false, "stranded": false} {
		if got.KeepPreset(hubPreset(id)) != keep {
			t.Errorf("MCP run: a cached strategy of %s kept %v, want %v; MCP cannot apply a group", id, !keep, keep)
		}
	}
	if !got.KeepPreset(anonymous) {
		t.Error("MCP run: a strategy without a hub origin must be tested")
	}
}

func TestCommunityPresetsTestTheActiveTwinOfAGroupedSet(t *testing.T) {
	env := newHubEnv(t)
	brought := hubCatalogueMember(t, "brought", 1, "alice", "one.example")
	brought.Status = hubwire.SetStatusGrouped
	brought.UpdatedAt = "2099-01-05T00:00:00Z"
	twin := hubCatalogueMember(t, "twin", 1, "bob", "one.example")
	twin.UpdatedAt = "2099-01-01T00:00:00Z"
	sets := []hubwire.CatalogueSet{brought, twin}
	for i, ttl := range []int{3, 4, 5} {
		other := hubCatalogueMemberWith(t, "other"+string(rune('a'+i)), 1, "carol", func(set *config.SetConfig) { set.Faking.TTL = uint8(ttl) }, "one.example")
		other.UpdatedAt = "2099-01-0" + string(rune('4'-i)) + "T00:00:00Z"
		sets = append(sets, other)
	}
	if brought.FP != twin.FP {
		t.Fatalf("the twin must carry the grouped set's strategy: %s, %s", brought.FP, twin.FP)
	}
	group := hubwire.CatalogueGroup{ID: "g", Version: 1, Title: "pair", Author: "alice", Members: []hubwire.CatalogueGroupMember{{SetID: "brought"}, {SetID: "othera"}}}
	env.publishCatalogue(t, 1, sets, []hubwire.CatalogueGroup{group})

	got := groupedPresetIDs(env.api.communityPresets([]string{"one.example"}, false, true))
	if !slices.Contains(got, "twin") || slices.Contains(got, "brought") {
		t.Errorf("the strategy both sets carry must be tested as the active set, which can be applied on its own, at the grouped set's rank: got %v", got)
	}
}

func groupedWinnerResults(set *config.SetConfig) map[string]*discovery.DomainDiscoveryResult {
	return map[string]*discovery.DomainDiscoveryResult{
		"one.example": {Domain: "one.example", BestPreset: "community-brought-v1", BestSuccess: true, Confirmed: 3, Results: map[string]*discovery.DomainPresetResult{
			"community-brought-v1": {PresetName: "community-brought-v1", Status: discovery.CheckStatusComplete, Confirmed: 3, ConfirmTries: 3, Set: set},
		}},
	}
}

func TestMCPStatusDoesNotOfferAGroupedWinnerForApply(t *testing.T) {
	env := newHubEnv(t)
	publishGroupedMember(t, env)

	cfg := probeCfg(t)
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	news := config.NewSetConfig()
	news.Id, news.Name, news.Enabled = "news", "news", true
	news.Targets.SNIDomains = []string{"one.example"}
	cfg.Sets = append(cfg.Sets, &news)
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)
	t.Cleanup(func() { mcpRememberSuite("") })

	member := hubStrategySet("brought", "one.example")
	member.Hub = &config.HubOrigin{ID: "brought", Version: 1}
	results := groupedWinnerResults(&member)
	groups := []discovery.StrategyGroup{{WinnerPreset: "community-brought-v1", Domains: []string{"one.example"}, Set: &member}}
	discovery.RegisterSuite(&discovery.CheckSuite{Id: "web-run", Status: discovery.CheckStatusComplete, DomainDiscoveryResults: results, StrategyGroups: groups})
	discovery.SaveToHistory(&discovery.CheckSuite{Id: "web-run-saved", Status: discovery.CheckStatusComplete, EndTime: time.Now(), DomainDiscoveryResults: results, StrategyGroups: groups}, cfg.ConfigPath)
	discovery.RegisterSuite(&discovery.CheckSuite{
		Id: "web-set-run", Status: discovery.CheckStatusComplete, SetId: "news",
		SetVerdict: &discovery.SetVerdict{Status: discovery.SetVerdictCovered, WinnerPreset: "community-brought-v1", Covered: []string{"one.example"}, Set: &member},
	})

	run := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status", "id": "web-run"}))
	if !strings.Contains(run.Note, "0 of 1 domain(s) have a strategy worth applying") {
		t.Errorf("a grouped winner cannot be applied with action=apply, so it must not be counted: %q", run.Note)
	}
	if len(run.Domains) != 1 || !strings.Contains(run.Domains[0].Verdict, "applied only with its community group, in b4's web interface") {
		t.Errorf("the row must say the strategy is applied only with its group in the web interface: %+v", run.Domains)
	}

	mcpRememberSuite("")
	saved := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status", "id": "web-run-saved"}))
	if saved.Source != "history" || strings.Contains(saved.Note, "you can install with action=apply") {
		t.Errorf("the saved history must not offer a grouped winner for apply: %q", saved.Note)
	}
	if len(saved.Domains) != 1 || !strings.Contains(saved.Domains[0].Verdict, "applied only with its community group, in b4's web interface") {
		t.Errorf("the history row must say the strategy is applied only with its group in the web interface: %+v", saved.Domains)
	}

	verdict := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status", "id": "web-set-run"}))
	if verdict.SetVerdict == nil || strings.Contains(verdict.SetVerdict.Meaning, "action=apply with set writes it") || !strings.Contains(verdict.SetVerdict.Meaning, "applied only with its community group") {
		t.Errorf("a covered verdict with a grouped winner must not promise apply: %+v", verdict.SetVerdict)
	}
}

func TestMCPApplyAnswersCoveredForAGroupAlreadyApplied(t *testing.T) {
	env := newHubEnv(t)
	publishGroupedMember(t, env)
	if rec := postJSON(t, env.mux, "/api/hub/groups/g/apply", map[string]interface{}{}); rec.Code != http.StatusAccepted {
		t.Fatalf("apply the group: %d (%s)", rec.Code, rec.Body.String())
	}
	var applied *config.SetConfig
	for _, set := range env.api.getCfg().Sets {
		if set.Hub != nil && set.Hub.ID == "brought" {
			copied := *set
			applied = &copied
		}
	}
	if applied == nil {
		t.Fatal("the group must apply its grouped member")
	}

	cfg := env.api.getCfg().Clone()
	cfg.System.WebServer.MCP = probeCfg(t).System.WebServer.MCP
	cfg.System.WebServer.MCP.AllowWrites = true
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)
	t.Cleanup(func() { mcpRememberSuite("") })

	copied := *applied
	winner := &copied
	winner.Id, winner.Name = "", ""
	groups := []discovery.StrategyGroup{{WinnerPreset: "community-brought-v1", Domains: []string{"one.example"}, Set: winner}}
	discovery.RegisterSuite(&discovery.CheckSuite{Id: "web-run-covered", Status: discovery.CheckStatusComplete, DomainDiscoveryResults: groupedWinnerResults(winner), StrategyGroups: groups})

	before := len(api.getCfg().Sets)
	res := callDiscovery(t, session, ctx, map[string]any{"action": "apply", "domain": "one.example", "id": "web-run-covered"})
	if res.IsError {
		t.Fatalf("a domain the applied group already covers with this strategy must get the covered answer, got %q", mcpErrorText(res))
	}
	if out := decodeDiscovery(t, res); !strings.Contains(out.Note, "already covered") || out.Applied == nil || out.Applied.Id != applied.Id {
		t.Errorf("the answer must name the group's set that covers the domain: %+v", out)
	}
	if len(api.getCfg().Sets) != before {
		t.Errorf("nothing may be written: %d sets from %d", len(api.getCfg().Sets), before)
	}
}

func TestWebDiscoveryApplyRefusesAGroupedSet(t *testing.T) {
	env := newHubEnv(t)
	publishGroupedMember(t, env)
	env.api.RegisterDiscoveryApi()
	env.api.RegisterSetsApi()
	target := hubStrategySet("target", "one.example")
	target.Id, target.Enabled = "set-a", true
	target.Faking.TTL = 2
	env.update(func(cfg *config.Config) { cfg.Sets = append(cfg.Sets, &target) })

	strategy := func(hubID string) config.SetConfig {
		set := hubStrategySet(hubID, "one.example")
		set.Faking.TTL = 11
		set.Hub = &config.HubOrigin{ID: hubID, Version: 1}
		return set
	}
	writes := []struct {
		name string
		path string
		body func(hubID string) any
	}{
		{"add as a new set", "/api/discovery/add", func(id string) any { return DiscoveredSetRequest{Set: mustJSON(t, strategy(id))} }},
		{"replace the strategy of a set", "/api/discovery/replace", func(id string) any {
			return DiscoveryReplaceRequest{SetId: "set-a", Set: strategy(id), Domains: []string{"one.example"}}
		}},
		{"write a set run's verdict into its set", "/api/discovery/replace", func(id string) any {
			return DiscoveryReplaceRequest{SetId: "set-a", Set: strategy(id), StrategyOnly: true, KeepTargets: true}
		}},
	}
	for _, enabled := range []bool{true, false} {
		env.update(func(cfg *config.Config) { cfg.System.Hub.Enabled = enabled })
		for _, w := range writes {
			before := env.api.getCfg().Clone()
			rec := postJSON(t, env.mux, w.path, w.body("brought"))
			expectCode(t, rec, http.StatusConflict, "hub_grouped")
			if !strings.Contains(rec.Body.String(), `"path":"hub.id"`) {
				t.Errorf("%s (community on: %v): the refusal must carry a field the web interface translates: %s", w.name, enabled, rec.Body.String())
			}
			after := env.api.getCfg()
			if len(after.Sets) != len(before.Sets) || after.GetSetById("set-a").Faking.TTL != 2 {
				t.Errorf("%s (community on: %v): nothing may be written", w.name, enabled)
			}
		}
	}

	env.update(func(cfg *config.Config) { cfg.System.Hub.Enabled = true })
	for _, w := range writes {
		if rec := postJSON(t, env.mux, w.path, w.body("alone")); rec.Code != http.StatusOK && rec.Code != http.StatusAccepted {
			t.Errorf("%s: a set the hub lists on its own must still be written, got %d (%s)", w.name, rec.Code, rec.Body.String())
		}
	}

	existing := *env.api.getCfg().GetSetById("set-a")
	existing.Hub = &config.HubOrigin{ID: "brought", Version: 1}
	if rec := postJSON(t, env.mux, "/api/sets/set-a/add-domain", map[string]any{"domains": []string{"new.example"}, "tested": existing}); rec.Code != http.StatusOK {
		t.Errorf("adding domains to a set with the same strategy installs no strategy and stays allowed, got %d (%s)", rec.Code, rec.Body.String())
	}
}
