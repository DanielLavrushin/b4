package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/warmtuple"
)

func TestWarmTupleConfigurationAPIAndSparseExport(t *testing.T) {
	cfg := config.NewConfig()
	set := config.NewSetConfig()
	set.Id = "warm"
	set.Targets.SNIDomains = []string{"googlevideo.com"}
	set.TCP.WarmTuple.Enabled = true
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1, Cutoff: "client_hello"}
	cfg.Sets = []*config.SetConfig{&set}
	cfg.System.Socks5.WarmTuple = config.Socks5WarmTupleConfig{WarmTimeoutMs: 20000, ReconnectDelayMs: 1000, CompactHello: true}
	api := configDownloadAPI(&cfg)
	full := downloadConfig(t, api, "/api/config")
	if full.Code != http.StatusOK || !strings.Contains(full.Body.String(), `"warm_tuple":{"enabled":true}`) {
		t.Fatal(full.Code, full.Body.String())
	}
	exported := downloadConfig(t, api, "/api/config/download")
	if exported.Code != http.StatusOK {
		t.Fatal(exported.Code, exported.Body.String())
	}
	loaded := config.NewConfig()
	if err := json.Unmarshal(exported.Body.Bytes(), &loaded); err != nil {
		t.Fatal(err)
	}
	if !loaded.Sets[0].TCP.WarmTuple.Enabled || loaded.System.Socks5.WarmTuple.WarmTimeoutMs != 20000 || loaded.System.Socks5.WarmTuple.ReconnectDelayMs != 1000 {
		t.Fatal("API export lost warm settings")
	}
}

type warmStatusRefresher struct{}

func (warmStatusRefresher) UpdateConfig(*config.Config) {}
func (warmStatusRefresher) WarmTupleStatus() warmtuple.Status {
	return warmtuple.Status{Active: 2, Pending: 1}
}

func TestWarmTupleStatusAPI(t *testing.T) {
	previous := globalSocks5Server
	globalSocks5Server = warmStatusRefresher{}
	t.Cleanup(func() { globalSocks5Server = previous })
	cfg := config.NewConfig()
	api := configDownloadAPI(&cfg)
	rec := downloadConfig(t, api, "/api/socks5/warm-tuple")
	var got warmtuple.Status
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Active != 2 || got.Pending != 1 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/socks5/warm-tuple", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatal("status endpoint accepted a write")
	}
}
