package handler

import (
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
