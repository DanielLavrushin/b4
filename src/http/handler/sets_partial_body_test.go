package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func flattenJSONPaths(prefix string, v any, out map[string]any) {
	m, ok := v.(map[string]any)
	if !ok {
		out[prefix] = v
		return
	}
	for k, val := range m {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		flattenJSONPaths(p, val, out)
	}
}

func setFieldDiffs(t *testing.T, want, got any) []string {
	t.Helper()
	flat := func(v any) map[string]any {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		out := map[string]any{}
		flattenJSONPaths("", m, out)
		return out
	}
	w, g := flat(want), flat(got)
	var diffs []string
	for k, wv := range w {
		if gv, ok := g[k]; !ok || !reflect.DeepEqual(wv, gv) {
			diffs = append(diffs, fmt.Sprintf("%s = %v, want %v", k, gv, wv))
		}
	}
	for k, gv := range g {
		if _, ok := w[k]; !ok {
			diffs = append(diffs, fmt.Sprintf("%s = %v, want it absent", k, gv))
		}
	}
	sort.Strings(diffs)
	return diffs
}

func storedSet(t *testing.T, api *API, id string) *config.SetConfig {
	t.Helper()
	set := api.getCfg().GetSetById(id)
	if set == nil {
		t.Fatalf("set %q is not stored", id)
	}
	return set
}

func copySet(t *testing.T, set *config.SetConfig) *config.SetConfig {
	t.Helper()
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := config.NewSetConfig()
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &out
}

func TestAPartialSetPostStoresTheSetDefaults(t *testing.T) {
	cases := []struct {
		name, body  string
		wantEnabled bool
	}{
		{"enabled in the body", `{"name":"partial","enabled":true,"targets":{"sni_domains":["partial.example.org"]}}`, true},
		{"disabled in the body", `{"name":"partial","enabled":false,"targets":{"sni_domains":["partial.example.org"]}}`, false},
		{"enabled left out", `{"name":"partial","targets":{"sni_domains":["partial.example.org"]}}`, config.DefaultSetConfig.Enabled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, mux := asnAPI(t)
			rec := serve(mux, http.MethodPost, "/api/sets", tc.body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("POST /api/sets = %d %s", rec.Code, rec.Body.String())
			}
			sets := api.getCfg().Sets
			if len(sets) != 1 {
				t.Fatalf("stored %d sets, want 1", len(sets))
			}
			got := sets[0]

			want := config.NewSetConfig()
			want.Id, want.Name, want.Enabled = got.Id, "partial", tc.wantEnabled
			want.Targets.SNIDomains = []string{"partial.example.org"}
			for _, d := range setFieldDiffs(t, &want, got) {
				t.Errorf("after a partial POST: %s", d)
			}
		})
	}
}

func TestAPartialSetPutIsRefusedAndLeavesTheSetAlone(t *testing.T) {
	cases := []struct {
		name, body string
		missing    []string
	}{
		{"only enabled", `{"enabled":false}`, []string{"name", "targets"}},
		{"no name", `{"enabled":true,"targets":{"sni_domains":["yt.example.org"]}}`, []string{"name"}},
		{"null name", `{"name":null,"enabled":true,"targets":{"sni_domains":["yt.example.org"]}}`, []string{"name"}},
		{"no enabled", `{"name":"YouTube","targets":{"sni_domains":["yt.example.org"]}}`, []string{"enabled"}},
		{"no targets", `{"name":"YouTube","enabled":true,"tcp":{"desync":{"mode":"rst"}}}`, []string{"targets"}},
		{"null targets", `{"name":"YouTube","enabled":true,"targets":null}`, []string{"targets"}},
		{"empty object", `{}`, []string{"name", "enabled", "targets"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := namedSet("yt", "YouTube", false, "youtube.com")
			original.TCP.Desync.Mode = "ack"
			api, mux := asnAPI(t, original)
			before := copySet(t, storedSet(t, api, "yt"))

			rec := serve(mux, http.MethodPut, "/api/sets/yt", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("partial PUT %s = %d %s, want 400", tc.body, rec.Code, rec.Body.String())
			}
			var apiErr APIError
			if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			var paths []string
			for _, f := range apiErr.Fields {
				if f.Code != "required" {
					t.Errorf("field %s has code %q, want required", f.Path, f.Code)
				}
				paths = append(paths, f.Path)
			}
			if apiErr.Code != "validation_failed" || !slices.Equal(paths, tc.missing) {
				t.Errorf("error = %s %v, want validation_failed %v", apiErr.Code, paths, tc.missing)
			}
			for _, d := range setFieldDiffs(t, before, storedSet(t, api, "yt")) {
				t.Errorf("the refused PUT changed the set: %s", d)
			}
		})
	}
}

func TestASetPutWithoutStrategyFieldsStoresTheSetDefaults(t *testing.T) {
	original := namedSet("yt", "YouTube", true, "youtube.com")
	original.TCP.Desync.Mode = "rst"
	original.TCP.Win.Mode = "zero"
	original.Fragmentation.Strategy = "oob"
	original.Faking.SNI = false
	original.Faking.TTL = 3
	api, mux := asnAPI(t, original)

	rec := serve(mux, http.MethodPut, "/api/sets/yt", `{"name":"Renamed","enabled":false,"targets":{"sni_domains":["renamed.example.org"]}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/sets/yt = %d %s", rec.Code, rec.Body.String())
	}

	want := config.NewSetConfig()
	want.Id, want.Name, want.Enabled = "yt", "Renamed", false
	want.Targets.SNIDomains = []string{"renamed.example.org"}
	for _, d := range setFieldDiffs(t, &want, storedSet(t, api, "yt")) {
		t.Errorf("after a PUT without strategy fields: %s", d)
	}
}

func TestAFullSetPutKeepsTheSetAndDropsTheKeysTheUILeavesOut(t *testing.T) {
	original := namedSet("yt", "YouTube", true)
	original.TCP.Desync.Mode = "rst"
	original.UDP.FilterL7 = []string{"stun"}
	original.UDP.DPortFilter = "3478"
	original.DNS.Pins = map[string][]string{"youtube.com": {"192.0.2.10"}}
	original.Discovery.Watchdog = false
	api, mux := asnAPI(t, original)

	var body map[string]any
	if err := json.Unmarshal(serve(mux, http.MethodGet, "/api/sets/yt", "").Body.Bytes(), &body); err != nil {
		t.Fatalf("GET /api/sets/yt: %v", err)
	}
	before := copySet(t, storedSet(t, api, "yt"))

	raw, _ := json.Marshal(body)
	if rec := serve(mux, http.MethodPut, "/api/sets/yt", string(raw)); rec.Code != http.StatusOK {
		t.Fatalf("full PUT = %d %s", rec.Code, rec.Body.String())
	}
	for _, d := range setFieldDiffs(t, before, storedSet(t, api, "yt")) {
		t.Errorf("a full PUT of the stored set changed it: %s", d)
	}

	delete(body["udp"].(map[string]any), "filter_l7")
	delete(body["dns"].(map[string]any), "pins")
	raw, _ = json.Marshal(body)
	if rec := serve(mux, http.MethodPut, "/api/sets/yt", string(raw)); rec.Code != http.StatusOK {
		t.Fatalf("PUT without filter_l7 and pins = %d %s", rec.Code, rec.Body.String())
	}
	got := storedSet(t, api, "yt")
	if len(got.UDP.FilterL7) != 0 || len(got.DNS.Pins) != 0 {
		t.Errorf("a PUT that leaves filter_l7 and pins out kept them: filter_l7=%v pins=%v", got.UDP.FilterL7, got.DNS.Pins)
	}
	if got.TCP.Desync.Mode != "rst" || got.Name != "YouTube" {
		t.Errorf("a full PUT lost the set's own values: desync=%q name=%q", got.TCP.Desync.Mode, got.Name)
	}
}

func TestASetInsideAConfigPutTakesTheSetDefaults(t *testing.T) {
	kept := namedSet("kept", "Kept", true, "kept.example.org")
	kept.TCP.Desync.Mode = "rst"
	api, mux := asnAPI(t, kept)

	var body map[string]any
	if err := json.Unmarshal(serve(mux, http.MethodGet, "/api/config", "").Body.Bytes(), &body); err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}
	sets := body["sets"].([]any)
	body["sets"] = append(sets, map[string]any{
		"id":      "partial",
		"name":    "Partial",
		"enabled": true,
		"targets": map[string]any{"sni_domains": []string{"partial.example.org"}},
	})
	raw, _ := json.Marshal(body)
	if rec := serve(mux, http.MethodPut, "/api/config", string(raw)); rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config = %d %s", rec.Code, rec.Body.String())
	}

	want := config.NewSetConfig()
	want.Id, want.Name, want.Enabled = "partial", "Partial", true
	want.Targets.SNIDomains = []string{"partial.example.org"}
	for _, d := range setFieldDiffs(t, &want, storedSet(t, api, "partial")) {
		t.Errorf("a partial set inside PUT /api/config: %s", d)
	}
	if got := storedSet(t, api, "kept").TCP.Desync.Mode; got != "rst" {
		t.Errorf("the full set inside PUT /api/config lost its desync mode: %q", got)
	}
}

func TestASetPutKeepsANamelessSetSaveable(t *testing.T) {
	legacy := namedSet("legacy", "", true, "legacy.example.org")
	api, mux := asnAPI(t, legacy)

	var body map[string]any
	if err := json.Unmarshal(serve(mux, http.MethodGet, "/api/sets/legacy", "").Body.Bytes(), &body); err != nil {
		t.Fatalf("GET /api/sets/legacy: %v", err)
	}
	body["discovery"].(map[string]any)["urls"] = []string{"https://legacy.example.org/"}
	raw, _ := json.Marshal(body)
	if rec := serve(mux, http.MethodPut, "/api/sets/legacy", string(raw)); rec.Code != http.StatusOK {
		t.Fatalf("PUT of a nameless set = %d %s", rec.Code, rec.Body.String())
	}
	got := storedSet(t, api, "legacy")
	if got.Name != "" || !slices.Equal(got.Discovery.URLs, []string{"https://legacy.example.org/"}) {
		t.Errorf("nameless set stored as name=%q urls=%v", got.Name, got.Discovery.URLs)
	}
}
