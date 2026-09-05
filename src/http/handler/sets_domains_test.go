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

func TestAddDomainsToSetInOneRequest(t *testing.T) {
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")

	target := config.NewSetConfig()
	target.Id = "target"
	target.Name = "Streaming"
	target.Enabled = true
	target.Targets.SNIDomains = []string{"youtube.com"}

	other := config.NewSetConfig()
	other.Id = "other"
	other.Name = "Discord"
	other.Enabled = true
	other.Targets.SNIDomains = []string{"discord.com", "discordapp.com"}

	cfg.Sets = []*config.SetConfig{&target, &other}

	api := &API{
		cfgPtr:         testCfgPtr(&cfg),
		geodataManager: geodat.NewGeodataManager("", ""),
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSetsApi()

	body := `{"domains":["discord.com","Youtube.com","twitch.tv"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/sets/target/add-domain", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var reply struct {
		Moved []DomainReassignment `json:"moved"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("decode reply: %v", err)
	}

	var got, rest *config.SetConfig
	for _, s := range api.getCfg().Sets {
		switch s.Id {
		case "target":
			got = s
		case "other":
			rest = s
		}
	}
	if got == nil || rest == nil {
		t.Fatal("both sets must survive the request")
	}

	want := []string{"youtube.com", "discord.com", "twitch.tv"}
	if strings.Join(got.Targets.SNIDomains, ",") != strings.Join(want, ",") {
		t.Errorf("one request adds every domain once, got %v want %v", got.Targets.SNIDomains, want)
	}
	if strings.Join(rest.Targets.SNIDomains, ",") != "discordapp.com" {
		t.Errorf("a domain belongs to one enabled set, so discord.com must leave the other set, got %v", rest.Targets.SNIDomains)
	}
	if len(reply.Moved) != 1 || reply.Moved[0].Domain != "discord.com" || reply.Moved[0].SetName != "Discord" {
		t.Errorf("the reply must report every reassignment, got %+v", reply.Moved)
	}
}

func TestAddDomainToSetRejectsAnEmptyRequest(t *testing.T) {
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	set := config.NewSetConfig()
	set.Id = "target"
	set.Enabled = true
	cfg.Sets = []*config.SetConfig{&set}

	api := &API{
		cfgPtr:         testCfgPtr(&cfg),
		geodataManager: geodat.NewGeodataManager("", ""),
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSetsApi()

	req := httptest.NewRequest(http.MethodPost, "/api/sets/target/add-domain", strings.NewReader(`{"domain":"  "}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an empty domain must be refused, got %d (%s)", rec.Code, rec.Body.String())
	}
}
