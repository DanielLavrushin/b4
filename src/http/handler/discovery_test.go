package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/geodat"
)

func TestAddPresetAsSetDropsGeoCategoriesWithoutDatabases(t *testing.T) {
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")

	api := &API{
		cfgPtr:         testCfgPtr(&cfg),
		geodataManager: geodat.NewGeodataManager("", ""),
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterDiscoveryApi()

	body := `{"name":"YouTube","targets":{"sni_domains":["youtube.com"],` +
		`"geosite_categories":["youtube"],"geoip_categories":["google"]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/discovery/add", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	sets := api.getCfg().Sets
	if len(sets) != 1 {
		t.Fatalf("expected 1 set, got %d", len(sets))
	}
	var reply struct {
		Id   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if reply.Id == "" || reply.Id != sets[0].Id {
		t.Errorf("the reply must name the set it created so the caller can open it, got %q want %q", reply.Id, sets[0].Id)
	}
	if reply.Name != "YouTube" {
		t.Errorf("reply name = %q", reply.Name)
	}
	if len(sets[0].Targets.GeoSiteCategories) != 0 {
		t.Errorf("geosite categories kept without a geosite database: %v", sets[0].Targets.GeoSiteCategories)
	}
	if len(sets[0].Targets.GeoIpCategories) != 0 {
		t.Errorf("geoip categories kept without a geoip database: %v", sets[0].Targets.GeoIpCategories)
	}
	if len(sets[0].Targets.SNIDomains) != 1 || sets[0].Targets.SNIDomains[0] != "youtube.com" {
		t.Errorf("unexpected SNI domains: %v", sets[0].Targets.SNIDomains)
	}
}

func TestSimilarSetsNeedTheWholeStrategyToMatch(t *testing.T) {
	base := config.NewSetConfig()
	base.Fragmentation.Strategy = "tcp"
	base.Faking.SNI = true
	base.Faking.Strategy = "pastseq"
	base.Faking.TTL = 6

	same := base
	same.Name = "other"
	same.Targets.SNIDomains = []string{"meduza.io"}
	same.TCP.DPortFilter = "443"
	same.TCP.IPBlockDetect.Enabled = true
	if !setsHaveSimilarConfig(&base, &same) {
		t.Fatal("port filters and address tracking are not part of the strategy")
	}

	desynced := base
	desynced.TCP.Desync.Mode = "full"
	desynced.TCP.Desync.TTL = 6
	if setsHaveSimilarConfig(&base, &desynced) {
		t.Fatal("a desync set is a different strategy, offering it would apply the wrong one")
	}

	split := base
	split.Fragmentation.SNIPosition = 3
	if setsHaveSimilarConfig(&base, &split) {
		t.Fatal("the split position is part of the strategy")
	}

	tls13 := base
	tls13.Targets.TLSVersion = "1.3"
	if setsHaveSimilarConfig(&base, &tls13) {
		t.Fatal("a set limited to one TLS version would not handle what the run found on the other")
	}

	proxied := base
	proxied.Routing.Enabled = true
	proxied.Routing.Mode = "proxy"
	if setsHaveSimilarConfig(&base, &proxied) {
		t.Fatal("a routed set sends the domain elsewhere instead of applying the strategy")
	}

	redirected := base
	redirected.DNS = config.DNSConfig{Enabled: true, DoHURL: "https://1.1.1.1/dns-query"}
	if setsHaveSimilarConfig(&base, &redirected) {
		t.Fatal("a DNS redirect changes what the added domain resolves to")
	}
}

func TestHistoryOffersTheAlternativesAndRemembersWhatWasTried(t *testing.T) {
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")

	api := &API{
		cfgPtr:         testCfgPtr(&cfg),
		geodataManager: geodat.NewGeodataManager("", ""),
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterDiscoveryApi()

	winner := config.NewSetConfig()
	winner.Name = "meduza"
	winner.Targets.SNIDomains = []string{"meduza.io"}
	runnerUp := config.NewSetConfig()
	runnerUp.Name = "runner-up"

	discovery.SaveToHistory(&discovery.CheckSuite{
		Id:     "run-1",
		Status: discovery.CheckStatusComplete,
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"meduza.io": {
				Domain: "meduza.io", BestPreset: "best", BestSuccess: true,
				Results: map[string]*discovery.DomainPresetResult{
					"best":         {PresetName: "best", Set: &winner, Status: discovery.CheckStatusComplete, Speed: 100},
					"combo-random": {PresetName: "combo-random", Set: &runnerUp, Status: discovery.CheckStatusComplete, Speed: 50},
				},
			},
		},
		StrategyGroups: []discovery.StrategyGroup{
			{WinnerPreset: "best", Domains: []string{"meduza.io"}, Set: &winner},
		},
	}, cfg.ConfigPath)

	read := func() []HistoryEntryView {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/discovery/history", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("history: %d (%s)", rec.Code, rec.Body.String())
		}
		var entries []HistoryEntryView
		if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
			t.Fatalf("decode history: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("entries = %d", len(entries))
		}
		return entries
	}

	entry := read()[0]
	if entry.Results["combo-random"].Set == nil {
		t.Error("a strategy that also worked must stay applicable from history")
	}
	if entry.SizeBytes <= 0 {
		t.Error("the history table shows what a site costs, so the size must be reported")
	}
	if len(entry.Applied) != 0 {
		t.Errorf("nothing was installed yet, got %v", entry.Applied)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/discovery/history/applied",
		strings.NewReader(`{"domains":["meduza.io"],"preset":"combo-random","set_id":"set-1"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("mark applied: %d (%s)", rec.Code, rec.Body.String())
	}

	entry = read()[0]
	if entry.Applied["combo-random"].SetId != "set-1" {
		t.Errorf("the strategy the user installed must come back marked, got %v", entry.Applied)
	}
	if _, tried := entry.Applied["best"]; tried {
		t.Error("only the strategy that was installed carries a mark")
	}
}

func replaceFixture(t *testing.T, mutate func(target *config.SetConfig)) (*API, *http.ServeMux) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")

	target := config.NewSetConfig()
	target.Id, target.Name, target.Enabled = "set-a", "meduza", true
	target.Targets.SNIDomains = []string{"meduza.io"}
	target.TCP.DPortFilter = "443,8443"
	target.TCP.RSTProtection.Enabled = true
	target.Faking.Strategy = "timestamp"
	target.DNS.Pins = map[string][]string{"meduza.io": {"1.1.1.1"}, "other.io": {"2.2.2.2"}}
	if mutate != nil {
		mutate(&target)
	}

	other := config.NewSetConfig()
	other.Id, other.Name, other.Enabled = "set-b", "old", true
	other.Targets.SNIDomains = []string{"cdn.meduza.io", "keep.io"}

	cfg.Sets = []*config.SetConfig{&target, &other}
	api := &API{cfgPtr: testCfgPtr(&cfg), geodataManager: geodat.NewGeodataManager("", "")}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterDiscoveryApi()
	return api, mux
}

func replaceRequest(t *testing.T, mux *http.ServeMux) *httptest.ResponseRecorder {
	t.Helper()
	strategy := config.NewSetConfig()
	strategy.Faking.Strategy = "pastseq"
	strategy.TCP.ConnBytesLimit = 7
	body, err := json.Marshal(DiscoveryReplaceRequest{
		SetId:   "set-a",
		Set:     strategy,
		Domains: []string{"meduza.io", "cdn.meduza.io"},
		Pins:    map[string][]string{"meduza.io": {"9.9.9.9"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/discovery/replace", strings.NewReader(string(body))))
	return rec
}

func replaceSets(api *API) (target, other *config.SetConfig) {
	for _, s := range api.getCfg().Sets {
		switch s.Id {
		case "set-a":
			target = s
		case "set-b":
			other = s
		}
	}
	return target, other
}

func TestReplaceStrategySwapsOnlyTheStrategyAndMovesTheGroupInOneSave(t *testing.T) {
	api, mux := replaceFixture(t, nil)
	rec := replaceRequest(t, mux)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: %d (%s)", rec.Code, rec.Body.String())
	}

	got, left := replaceSets(api)
	if got.Faking.Strategy != "pastseq" || got.TCP.ConnBytesLimit != 7 {
		t.Errorf("the discovered strategy must replace the old one, got faking=%q conn_bytes=%d", got.Faking.Strategy, got.TCP.ConnBytesLimit)
	}
	if got.Name != "meduza" || got.TCP.DPortFilter != "443,8443" || !got.TCP.RSTProtection.Enabled {
		t.Errorf("the set's own settings are not part of the strategy and must survive: name=%q dport=%q rst=%v", got.Name, got.TCP.DPortFilter, got.TCP.RSTProtection.Enabled)
	}
	if pins := got.DNS.Pins["meduza.io"]; len(pins) != 1 || pins[0] != "9.9.9.9" {
		t.Errorf("an applied site's pins come from the new strategy alone, got %v", got.DNS.Pins["meduza.io"])
	}
	if len(got.DNS.Pins["other.io"]) != 1 {
		t.Errorf("pins of sites the replace did not touch must stay, got %v", got.DNS.Pins)
	}
	if !domainInList(got.Targets.SNIDomains, "cdn.meduza.io") || domainInList(left.Targets.SNIDomains, "cdn.meduza.io") {
		t.Errorf("a grouped site must move into the set in the same save, target=%v other=%v", got.Targets.SNIDomains, left.Targets.SNIDomains)
	}
	if !domainInList(left.Targets.SNIDomains, "keep.io") {
		t.Error("the other set keeps what the replace did not claim")
	}
	if !strings.Contains(rec.Body.String(), "cdn.meduza.io") {
		t.Errorf("the reply must say what moved, got %s", rec.Body.String())
	}
}

func TestReplaceStrategyChangesNothingWhenTheSaveIsRejected(t *testing.T) {
	api, mux := replaceFixture(t, func(target *config.SetConfig) {
		target.MSSClamp.Enabled, target.MSSClamp.Size = true, 1300
	})
	if rec := replaceRequest(t, mux); rec.Code == http.StatusOK {
		t.Fatalf("a configuration the validator refuses must not be saved: %s", rec.Body.String())
	}

	got, left := replaceSets(api)
	if got.Faking.Strategy != "timestamp" || domainInList(got.Targets.SNIDomains, "cdn.meduza.io") {
		t.Errorf("a refused replace must leave the target as it was, got faking=%q sites=%v", got.Faking.Strategy, got.Targets.SNIDomains)
	}
	if !domainInList(left.Targets.SNIDomains, "cdn.meduza.io") {
		t.Error("a refused replace must not move sites out of other sets")
	}
	if pins := got.DNS.Pins["meduza.io"]; len(pins) != 1 || pins[0] != "1.1.1.1" {
		t.Errorf("a refused replace must leave the live pins alone, got %v", got.DNS.Pins)
	}
}
