package handler

import (
	"net/http"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const stampedHub = `"hub":{"id":"s1","version":2,"hash":"h","group_id":"g-video","group_version":3}`

func TestCreatedSetsDropTheHubGroupStamp(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		status           int
	}{
		{"create", "/api/sets", `{"name":"made","enabled":true,"targets":{"sni_domains":["made.example.org"]},` + stampedHub + `}`, http.StatusCreated},
		{"import", "/api/sets/import", `{"group":"video","sets":[{"name":"made","enabled":true,"targets":{"sni_domains":["made.example.org"]},` + stampedHub + `}]}`, http.StatusCreated},
		{"discovery add", "/api/discovery/add", `{"name":"made","targets":{"sni_domains":["made.example.org"]},` + stampedHub + `}`, http.StatusAccepted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, mux := asnAPI(t, groupedSet("other", ""))
			api.RegisterDiscoveryApi()
			if rec := serve(mux, http.MethodPost, tc.path, tc.body); rec.Code != tc.status {
				t.Fatalf("%s = %d %s", tc.path, rec.Code, rec.Body.String())
			}
			set := setNamed(api, "made")
			if set == nil || set.Hub == nil {
				t.Fatalf("stored set %+v, want it with its hub origin", set)
			}
			if set.Hub.ID != "s1" || set.Hub.Version != 2 || set.Hub.Hash != "h" {
				t.Errorf("hub = %+v, want the hub id, version and hash kept", set.Hub)
			}
			if set.Hub.GroupID != "" || set.Hub.GroupVersion != 0 {
				t.Errorf("hub = %+v, want no group stamp from the body", set.Hub)
			}
		})
	}
}

func TestSavingASetKeepsOnlyTheHubGroupStampItIsStoredWith(t *testing.T) {
	stamped := func(id, hubID, groupID string, groupVersion int) *config.SetConfig {
		set := groupedSet(id, "video")
		set.Hub = &config.HubOrigin{ID: hubID, Version: 1, Hash: "h", GroupID: groupID, GroupVersion: groupVersion}
		return set
	}
	put := func(t *testing.T, mux *http.ServeMux, id, hub string) {
		t.Helper()
		body := `{"name":"` + id + `","enabled":true,"targets":{"sni_domains":["` + id + `.example.org"]}` + hub + `}`
		if rec := serve(mux, http.MethodPut, "/api/sets/"+id, body); rec.Code != http.StatusOK {
			t.Fatalf("PUT %s = %d %s", id, rec.Code, rec.Body.String())
		}
	}
	hubOf := func(api *API, id string) config.HubOrigin {
		set := api.getCfg().GetSetById(id)
		if set == nil || set.Hub == nil {
			return config.HubOrigin{}
		}
		return *set.Hub
	}

	for _, tc := range []struct {
		name   string
		stored *config.SetConfig
		hub    string
		want   config.HubOrigin
	}{
		{"a stamp the stored set lacks", stamped("a", "s1", "", 0), `,"hub":{"id":"s1","version":1,"hash":"h","group_id":"g-video","group_version":3}`, config.HubOrigin{ID: "s1", Version: 1, Hash: "h"}},
		{"the same hub id with a forged stamp", stamped("a", "s1", "g-video", 3), `,"hub":{"id":"s1","version":1,"hash":"h","group_id":"g-other","group_version":9}`, config.HubOrigin{ID: "s1", Version: 1, Hash: "h", GroupID: "g-video", GroupVersion: 3}},
		{"the same hub id without a stamp", stamped("a", "s1", "g-video", 3), `,"hub":{"id":"s1","version":1,"hash":"h"}`, config.HubOrigin{ID: "s1", Version: 1, Hash: "h", GroupID: "g-video", GroupVersion: 3}},
		{"another hub id", stamped("a", "s1", "g-video", 3), `,"hub":{"id":"s2","version":1,"hash":"h","group_id":"g-video","group_version":3}`, config.HubOrigin{ID: "s2", Version: 1, Hash: "h"}},
		{"no hub id", stamped("a", "s1", "g-video", 3), `,"hub":{"hash":"h","group_id":"g-video","group_version":3}`, config.HubOrigin{Hash: "h"}},
		{"no hub origin", stamped("a", "s1", "g-video", 3), ``, config.HubOrigin{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, mux := asnAPI(t, tc.stored)
			put(t, mux, "a", tc.hub)
			if got := hubOf(api, "a"); got != tc.want {
				t.Errorf("hub = %+v, want %+v", got, tc.want)
			}
		})
	}
}
