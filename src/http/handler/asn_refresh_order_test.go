package handler

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestSetWritersAskForAnASNRefreshOnceTheSetIsSaved(t *testing.T) {
	cases := []struct {
		name  string
		write func(t *testing.T, api *API, mux *http.ServeMux) int
	}{
		{"create a set", func(t *testing.T, api *API, mux *http.ServeMux) int {
			body, _ := json.Marshal(asnSet("", "13335"))
			return serve(mux, http.MethodPost, "/api/sets", string(body)).Code
		}},
		{"update a set", func(t *testing.T, api *API, mux *http.ServeMux) int {
			body, _ := json.Marshal(asnSet("s1", "13335"))
			return serve(mux, http.MethodPut, "/api/sets/s1", string(body)).Code
		}},
		{"update the config", func(t *testing.T, api *API, mux *http.ServeMux) int {
			cfg := api.getCfg().Clone()
			cfg.Sets[0].Targets.ASNs = []string{"13335"}
			body, _ := json.Marshal(cfg)
			return serve(mux, http.MethodPut, "/api/config", string(body)).Code
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useAsnStore(t)
			api, mux := asnAPI(t, asnSet("s1"))
			refreshedAfterSave := watchASNRefreshes(t, func() bool { return configReferencesASN(api.getCfg(), "13335") })

			if code := tc.write(t, api, mux); code != http.StatusOK && code != http.StatusCreated {
				t.Fatalf("the write failed with %d", code)
			}
			if !configReferencesASN(api.getCfg(), "13335") {
				t.Fatalf("the ASN was not saved: %+v", api.getCfg().Sets)
			}
			if !refreshedAfterSave() {
				t.Error("the refresher must be asked to resolve the unknown ASN after the set is saved, or it reads the old config and misses it until the next hourly pass")
			}
		})
	}
}

func TestSavingASetWithResolvedASNsDoesNotAskForARefresh(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	_, mux := asnAPI(t, asnSet("s1"))

	body, _ := json.Marshal(asnSet("s1", "62041"))
	if code := serve(mux, http.MethodPut, "/api/sets/s1", string(body)).Code; code != http.StatusOK {
		t.Fatalf("the write failed with %d", code)
	}
	if drainASNRefresh() {
		t.Error("an ASN whose prefixes are known needs no refresh")
	}
}

func TestAConfigWriteCommitsTheASNPrefixesKnownAtCommitTime(t *testing.T) {
	cases := []struct {
		name    string
		resolve func(t *testing.T, api *API, s *config.AsnStore)
	}{
		{"the refresher reloads before the write takes the lock", func(t *testing.T, api *API, s *config.AsnStore) {
			putTelegram(t, s)
			api.ReloadASNTargets([]string{"62041"})
		}},
		{"the store changes before the reload runs", func(t *testing.T, api *API, s *config.AsnStore) {
			putTelegram(t, s)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := useAsnStore(t)
			base := asnSet("s1")
			base.Targets.IPs = []string{"198.51.100.0/24"}
			api, _ := asnAPI(t, base)
			countRefreshes(t)

			updated := asnSet("s1", "62041")
			updated.Targets.IPs = []string{"198.51.100.0/24"}
			api.loadTargetsForSetCached(updated)
			if slices.Contains(updated.Targets.IpsToMatch, telegramPrefixes[0]) {
				t.Fatal("the ASN must be unresolved when the write expands the set")
			}

			tc.resolve(t, api, s)

			err := api.updateAndPushConfig(func(current *config.Config) (*config.Config, error) {
				next := current.Clone()
				for i, set := range next.Sets {
					if set.Id == "s1" {
						next.Sets[i] = updated
					}
				}
				return next, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			got := api.getCfg().GetSetById("s1").Targets.IpsToMatch
			for _, p := range append([]string{"198.51.100.0/24"}, telegramPrefixes...) {
				if !slices.Contains(got, p) {
					t.Fatalf("the committed set lost %s: %v", p, got)
				}
			}
		})
	}
}
