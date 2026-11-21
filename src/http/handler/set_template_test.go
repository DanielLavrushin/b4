package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const routingOnlyTemplate = `{"faking":{"sni":false},"fragmentation":{"strategy":"none"}}`

func templateString(t *testing.T, m map[string]interface{}) string {
	t.Helper()
	if m == nil {
		return "null"
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func withSetTemplate(t *testing.T, api *API, template string) {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(template), &m); err != nil {
		t.Fatal(err)
	}
	next := api.getCfg().Clone()
	next.UI.NewSetTemplate = m
	api.cfgPtr.Store(next)
}

func fieldError(t *testing.T, body []byte) (path, code string) {
	t.Helper()
	var apiErr APIError
	if err := json.Unmarshal(body, &apiErr); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, body)
	}
	if len(apiErr.Fields) != 1 {
		t.Fatalf("want one field error, got %s", body)
	}
	return apiErr.Fields[0].Path, apiErr.Fields[0].Code
}

func TestNewSetTemplateEndpointStoresOnlyTheChangedFields(t *testing.T) {
	api, mux, cfgPath := newUITestAPI(t)

	rec := serve(mux, http.MethodGet, "/api/ui/new-set-template", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"new_set_template":null}` {
		t.Fatalf("GET before any template = %d %s", rec.Code, rec.Body.String())
	}

	export := `{"b4_version":"dev","name":"Сет 2","fragmentation":{"strategy":"none"},"faking":{"sni":false},"enabled":true,"targets":{"sni_domains":["example.com"]}}`
	rec = serve(mux, http.MethodPut, "/api/ui/new-set-template", export)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"new_set_template":`+routingOnlyTemplate+`}` {
		t.Errorf("PUT answered %s", got)
	}
	if got := templateString(t, api.getCfg().UI.NewSetTemplate); got != routingOnlyTemplate {
		t.Errorf("stored template = %s, want %s", got, routingOnlyTemplate)
	}
	saved, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if !strings.Contains(string(saved), "new_set_template") || strings.Contains(string(saved), "example.com") {
		t.Errorf("saved config holds the wrong template:\n%s", saved)
	}

	rec = serve(mux, http.MethodPut, "/api/ui/new-set-template", `{}`)
	if rec.Code != http.StatusOK || api.getCfg().UI.NewSetTemplate != nil {
		t.Errorf("an empty object did not clear the template: %d %s", rec.Code, templateString(t, api.getCfg().UI.NewSetTemplate))
	}
}

func TestNewSetTemplateEndpointRefusesWhatIsNotASet(t *testing.T) {
	api, mux, _ := newUITestAPI(t)
	withSetTemplate(t, api, routingOnlyTemplate)

	rec := serve(mux, http.MethodPut, "/api/ui/new-set-template", `{"routing":{"mode":"nonsense"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid template = %d %s, want 400", rec.Code, rec.Body.String())
	}
	if path, code := fieldError(t, rec.Body.Bytes()); path != "ui.new_set_template.routing.mode" || code != "invalid_routing_mode" {
		t.Errorf("field error = %s %s", path, code)
	}

	rec = serve(mux, http.MethodPut, "/api/ui/new-set-template", `{"fragmentation":`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("broken JSON = %d, want 400", rec.Code)
	}
	if got := templateString(t, api.getCfg().UI.NewSetTemplate); got != routingOnlyTemplate {
		t.Errorf("a refused PUT changed the template to %s", got)
	}

	rec = serve(mux, http.MethodPost, "/api/ui/new-set-template", routingOnlyTemplate)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405", rec.Code)
	}
}

func TestUpdateConfigNormalizesTheNewSetTemplate(t *testing.T) {
	api, mux := asnAPI(t, namedSet("yt", "YouTube", true, "youtube.com"))
	putTemplate := func(template string) *httptest.ResponseRecorder {
		raw := map[string]any{}
		data, _ := json.Marshal(api.getCfg())
		_ = json.Unmarshal(data, &raw)
		ui, _ := raw["ui"].(map[string]any)
		if ui == nil {
			ui = map[string]any{}
			raw["ui"] = ui
		}
		var m any
		if err := json.Unmarshal([]byte(template), &m); err != nil {
			t.Fatal(err)
		}
		ui["new_set_template"] = m
		body, _ := json.Marshal(raw)
		return serve(mux, http.MethodPut, "/api/config", string(body))
	}

	rec := putTemplate(`{"name":"Proxy","enabled":true,"fragmentation":{"strategy":"none","reverse_order":true},"faking":{"sni":false},"targets":{"sni_domains":["example.com"]}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config = %d %s", rec.Code, rec.Body.String())
	}
	if got := templateString(t, api.getCfg().UI.NewSetTemplate); got != routingOnlyTemplate {
		t.Errorf("stored template = %s, want %s", got, routingOnlyTemplate)
	}

	rec = putTemplate(`{"routing":{"mode":"nonsense"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid template = %d %s, want 400", rec.Code, rec.Body.String())
	}
	if path, code := fieldError(t, rec.Body.Bytes()); path != "ui.new_set_template.routing.mode" || code != "invalid_routing_mode" {
		t.Errorf("field error = %s %s", path, code)
	}
	if got := templateString(t, api.getCfg().UI.NewSetTemplate); got != routingOnlyTemplate {
		t.Errorf("a refused save changed the template to %s", got)
	}
}

func TestCreatingASetThroughTheAPIIgnoresTheNewSetTemplate(t *testing.T) {
	api, mux := asnAPI(t)
	withSetTemplate(t, api, routingOnlyTemplate)

	rec := serve(mux, http.MethodPost, "/api/sets", `{"name":"partial","targets":{"sni_domains":["partial.example.org"]}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/sets = %d %s", rec.Code, rec.Body.String())
	}
	sets := api.getCfg().Sets
	if len(sets) != 1 {
		t.Fatalf("stored %d sets, want 1", len(sets))
	}
	want := config.NewSetConfig()
	got := sets[0]
	if got.Fragmentation.Strategy != want.Fragmentation.Strategy || got.Faking.SNI != want.Faking.SNI {
		t.Errorf("a partial POST took strategy %q and fake %v; the API keeps b4's set defaults %q and %v, only the Sets page reads the template",
			got.Fragmentation.Strategy, got.Faking.SNI, want.Fragmentation.Strategy, want.Faking.SNI)
	}
	if !reflect.DeepEqual(api.getCfg().UI.NewSetTemplate["fragmentation"], map[string]interface{}{"strategy": "none"}) {
		t.Errorf("creating a set changed the template: %s", templateString(t, api.getCfg().UI.NewSetTemplate))
	}
}
