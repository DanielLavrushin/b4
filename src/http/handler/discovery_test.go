package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
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
