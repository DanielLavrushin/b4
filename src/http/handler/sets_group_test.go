package handler

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hub/hubtest"
)

func setOrder(api *API) []string {
	var out []string
	for _, set := range api.getCfg().Sets {
		out = append(out, set.Id+":"+set.Group)
	}
	return out
}

func groupedSet(id, group string) *config.SetConfig {
	set := namedSet(id, id, true, id+".example.org")
	set.Group = group
	return set
}

func TestBatchSetGroupLabelsAndGathersTheSets(t *testing.T) {
	cases := []struct {
		name string
		sets []*config.SetConfig
		body string
		want []string
	}{
		{
			name: "a new group lands at its first listed set",
			sets: []*config.SetConfig{groupedSet("a", ""), groupedSet("b", ""), groupedSet("c", ""), groupedSet("d", "")},
			body: `{"ids":["d","b"],"group":"  YouTube  "}`,
			want: []string{"a:", "b:YouTube", "d:YouTube", "c:"},
		},
		{
			name: "a set joining a group moves to the group, not the group to it",
			sets: []*config.SetConfig{groupedSet("x", ""), groupedSet("a", ""), groupedSet("g1", "yt"), groupedSet("g2", "yt")},
			body: `{"ids":["x"],"group":"yt"}`,
			want: []string{"a:", "x:yt", "g1:yt", "g2:yt"},
		},
		{
			name: "an empty group takes sets out and leaves them in place",
			sets: []*config.SetConfig{groupedSet("g1", "yt"), groupedSet("g2", "yt"), groupedSet("a", "")},
			body: `{"ids":["g1"],"group":""}`,
			want: []string{"g1:", "g2:yt", "a:"},
		},
		{
			name: "renaming onto another group merges into it",
			sets: []*config.SetConfig{groupedSet("o1", "old"), groupedSet("a", ""), groupedSet("n1", "new")},
			body: `{"ids":["o1"],"group":"new"}`,
			want: []string{"a:", "o1:new", "n1:new"},
		},
		{
			name: "a set taken out of the middle of a group moves to just after it",
			sets: []*config.SetConfig{groupedSet("g1", "yt"), groupedSet("g2", "yt"), groupedSet("g3", "yt"), groupedSet("a", "")},
			body: `{"ids":["g2"],"group":""}`,
			want: []string{"g1:yt", "g3:yt", "g2:", "a:"},
		},
		{
			name: "a new group made from the middle of another leaves that one together",
			sets: []*config.SetConfig{groupedSet("h1", "old"), groupedSet("h2", "old"), groupedSet("h3", "old"), groupedSet("x", "")},
			body: `{"ids":["h2","x"],"group":"new"}`,
			want: []string{"h1:old", "h3:old", "h2:new", "x:new"},
		},
		{
			name: "a name that differs only by case joins the group and takes its spelling",
			sets: []*config.SetConfig{groupedSet("x", ""), groupedSet("g1", "YouTube"), groupedSet("g2", "YouTube")},
			body: `{"ids":["x"],"group":"youtube"}`,
			want: []string{"x:YouTube", "g1:YouTube", "g2:YouTube"},
		},
		{
			name: "an exact name wins over one that differs by case",
			sets: []*config.SetConfig{groupedSet("a", "yt"), groupedSet("b", "YT"), groupedSet("x", "")},
			body: `{"ids":["x"],"group":"YT"}`,
			want: []string{"a:yt", "b:YT", "x:YT"},
		},
		{
			name: "a group keeps a new case of its own name",
			sets: []*config.SetConfig{groupedSet("g1", "yt"), groupedSet("g2", "yt"), groupedSet("a", "")},
			body: `{"ids":["g1","g2"],"group":"YT"}`,
			want: []string{"g1:YT", "g2:YT", "a:"},
		},
		{
			name: "a split group is pulled back together",
			sets: []*config.SetConfig{groupedSet("g1", "yt"), groupedSet("a", ""), groupedSet("g2", "yt")},
			body: `{"ids":["g1","g2"],"group":"yt"}`,
			want: []string{"g1:yt", "g2:yt", "a:"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, mux := asnAPI(t, tc.sets...)
			rec := serve(mux, http.MethodPost, "/api/sets/batch-set-group", tc.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("batch-set-group = %d %s", rec.Code, rec.Body.String())
			}
			if got := setOrder(api); !slices.Equal(got, tc.want) {
				t.Errorf("sets = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReorderSetsSavesGroupsWithTheOrder(t *testing.T) {
	api, mux := asnAPI(t, groupedSet("a", ""), groupedSet("g1", "yt"), groupedSet("g2", "yt"), groupedSet("b", ""))
	rec := serve(mux, http.MethodPost, "/api/sets/reorder", `{"set_ids":["g1","a","g2","b"],"groups":{"a":"  yt ","b":"","g2":"yt"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder = %d %s", rec.Code, rec.Body.String())
	}
	if got, want := setOrder(api), []string{"g1:yt", "a:yt", "g2:yt", "b:"}; !slices.Equal(got, want) {
		t.Errorf("sets = %v, want %v", got, want)
	}

	rec = serve(mux, http.MethodPost, "/api/sets/reorder", `{"set_ids":["a","g1","g2","b"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder without groups = %d %s", rec.Code, rec.Body.String())
	}
	if got, want := setOrder(api), []string{"a:yt", "g1:yt", "g2:yt", "b:"}; !slices.Equal(got, want) {
		t.Errorf("sets = %v, want the groups kept: %v", got, want)
	}

	rec = serve(mux, http.MethodPost, "/api/sets/reorder", `{"set_ids":["b","a","g1","g2"],"groups":{"nope":"yt"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown id in groups = %d, want 400", rec.Code)
	}
	if got, want := setOrder(api), []string{"a:yt", "g1:yt", "g2:yt", "b:"}; !slices.Equal(got, want) {
		t.Errorf("sets = %v, want them untouched after a refusal: %v", got, want)
	}
}

func TestBatchSetGroupRefusesUnknownSets(t *testing.T) {
	api, mux := asnAPI(t, groupedSet("a", ""))
	if rec := serve(mux, http.MethodPost, "/api/sets/batch-set-group", `{"ids":["nope"],"group":"yt"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id = %d, want 404", rec.Code)
	}
	if rec := serve(mux, http.MethodPost, "/api/sets/batch-set-group", `{"ids":[],"group":"yt"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("no ids = %d, want 400", rec.Code)
	}
	if got := setOrder(api); !slices.Equal(got, []string{"a:"}) {
		t.Errorf("sets = %v, want them untouched", got)
	}
}

func TestImportSetsCreatesTheGroupAtTheTopInOrder(t *testing.T) {
	api, mux := asnAPI(t, groupedSet("old", "youtube"), namedSet("plain", "youtube-video", true, "plain.example.org"))
	body := `{"b4_version":"1.93.0","group":"youtube","sets":[
		{"b4_version":"1.93.0","name":"youtube-video","tcp":{"wssize":{"enabled":true,"window":1,"strip_scale":true}},"targets":{"sni_domains":["googlevideo.com"]},"enabled":true},
		{"name":"youtube","enabled":false,"targets":{"geosite_categories":[]},"faking":{"strategy":"timestamp"}}
	]}`
	rec := serve(mux, http.MethodPost, "/api/sets/import", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import = %d %s", rec.Code, rec.Body.String())
	}
	var res ImportSetsResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Group != "youtube 2" {
		t.Errorf("group = %q, want a new group beside the existing one", res.Group)
	}

	sets := api.getCfg().Sets
	if len(sets) != 4 {
		t.Fatalf("stored %d sets, want 4", len(sets))
	}
	first, second := sets[0], sets[1]
	if first.Name != "youtube-video 2" || second.Name != "youtube" {
		t.Errorf("names = %q, %q, want the document order with a taken name numbered", first.Name, second.Name)
	}
	if first.Group != "youtube 2" || second.Group != "youtube 2" {
		t.Errorf("groups = %q, %q", first.Group, second.Group)
	}
	if !first.TCP.WSSize.Enabled || first.TCP.WSSize.Window != 1 || !first.TCP.WSSize.StripScale {
		t.Errorf("wssize = %+v, want the imported values", first.TCP.WSSize)
	}
	if second.Enabled {
		t.Error("the second set is enabled, want it disabled as imported")
	}
	if first.Faking.Strategy != config.DefaultSetConfig.Faking.Strategy {
		t.Errorf("faking.strategy = %q, want the default for a field left out", first.Faking.Strategy)
	}
	if first.Id == "" || first.Id == second.Id {
		t.Errorf("ids = %q, %q, want fresh distinct ids", first.Id, second.Id)
	}
	if sets[2].Id != "old" || sets[3].Id != "plain" {
		t.Errorf("existing sets moved: %v", setOrder(api))
	}
}

func TestImportSetsRefusesBadDocumentsWithoutWriting(t *testing.T) {
	for name, body := range map[string]string{
		"no sets":     `{"group":"yt","sets":[]}`,
		"not a set":   `{"group":"yt","sets":[{"name":"ok"},"text"]}`,
		"not json":    `{"group":`,
		"wrong shape": `{"group":"yt","sets":{"name":"x"}}`,
		"no name":     `{"sets":[{"name":"ok"},{"targets":{"sni_domains":["a.example.org"]}}]}`,
		"blank name":  `{"sets":[{"name":"  ","tcp":{}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			api, mux := asnAPI(t, groupedSet("a", ""))
			rec := serve(mux, http.MethodPost, "/api/sets/import", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("import = %d %s, want 400", rec.Code, rec.Body.String())
			}
			if got := setOrder(api); !slices.Equal(got, []string{"a:"}) {
				t.Errorf("sets = %v, want them untouched", got)
			}
		})
	}
}

func TestHubApplyReplaceKeepsTheSetsGroup(t *testing.T) {
	env := newHubEnv(t)
	shared := hubStrategySet("Shared video", "youtube.com")
	cs, _ := hubtest.CatalogueSet(t, "yt-9", 1, &shared, nil)
	env.publish(t, cs)

	local := hubStrategySet("Video here", "youtube.com")
	local.Id = "local-1"
	local.Group = "YouTube"
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{&local} })

	rec := postJSON(t, env.mux, "/api/hub/sets/yt-9/apply", map[string]interface{}{"replace": "local-1"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := env.localSet("local-1").Group; got != "YouTube" {
		t.Errorf("group = %q after a hub replace, want the set's own group", got)
	}
}
