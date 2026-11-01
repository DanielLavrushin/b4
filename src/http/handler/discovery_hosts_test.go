package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
)

func pageHostsFixture(t *testing.T) (*API, *http.ServeMux) {
	t.Helper()
	api, mux := replaceFixture(t, nil)
	dir := t.TempDir()
	cfg := api.getCfg()
	cfg.System.Geo.GeoSitePath = filepath.Join(dir, "geosite.dat")
	cfg.System.Geo.GeoIpPath = filepath.Join(dir, "geoip.dat")
	api.geodataManager = geodat.NewGeodataManager(cfg.System.Geo.GeoSitePath, cfg.System.Geo.GeoIpPath)
	return api, mux
}

func postDiscovery(t *testing.T, mux *http.ServeMux, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(mustJSON(t, body)))))
	return rec
}

func setNamed(api *API, name string) *config.SetConfig {
	for _, s := range api.getCfg().Sets {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func movedOf(t *testing.T, rec *httptest.ResponseRecorder) []DomainReassignment {
	t.Helper()
	var reply struct {
		Moved []DomainReassignment `json:"moved"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("decode reply: %v (%s)", err, rec.Body.String())
	}
	return reply.Moved
}

func refusalCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var refusal APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &refusal); err != nil {
		t.Fatalf("decode refusal: %v (%s)", err, rec.Body.String())
	}
	return refusal.Code
}

func TestAddAsNewSetAddsPageHostsByExactName(t *testing.T) {
	api, mux := pageHostsFixture(t)
	set := discoveredStrategy()
	set.Name = "svoboda"
	set.Targets.SNIDomains = []string{"svoboda.org"}
	rec := postDiscovery(t, mux, "/api/discovery/add", DiscoveredSetRequest{
		Set:   mustJSON(t, set),
		Hosts: []string{"www.youtube.com", " Keep.io. ", "keep.io", ""},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("add: %d (%s)", rec.Code, rec.Body.String())
	}

	created := setNamed(api, "svoboda")
	if created == nil {
		t.Fatal("the set was not created")
	}
	if want := []string{"svoboda.org", "www.youtube.com", "keep.io"}; !reflect.DeepEqual(created.Targets.SNIDomains, want) {
		t.Errorf("hosts go in after the run domains, normalised and once each: got %v want %v", created.Targets.SNIDomains, want)
	}
	if !slices.Contains(created.Targets.DomainsToMatch, "www.youtube.com") {
		t.Errorf("the set must match the hosts it lists, got %v", created.Targets.DomainsToMatch)
	}
	if len(created.Targets.GeoSiteCategories) != 0 || len(created.Targets.GeoIpCategories) != 0 {
		t.Errorf("a page host must not pull its service into the set: geosite %v geoip %v", created.Targets.GeoSiteCategories, created.Targets.GeoIpCategories)
	}
	_, other := replaceSets(api)
	if domainInList(other.Targets.SNIDomains, "keep.io") || !domainInList(other.Targets.SNIDomains, "cdn.meduza.io") {
		t.Errorf("only the exact entry of a ticked host moves out of another set, got %v", other.Targets.SNIDomains)
	}
	if moved := movedOf(t, rec); len(moved) != 1 || moved[0].Domain != "keep.io" || moved[0].SetId != "set-b" {
		t.Errorf("the reply must say which host moved, got %+v", moved)
	}

	run := discoveredStrategy()
	run.Name = "youtube"
	run.Targets.SNIDomains = []string{"www.youtube.com"}
	if rec := postDiscovery(t, mux, "/api/discovery/add", DiscoveredSetRequest{Set: mustJSON(t, run)}); rec.Code != http.StatusAccepted {
		t.Fatalf("add youtube: %d (%s)", rec.Code, rec.Body.String())
	}
	if got := setNamed(api, "youtube"); got == nil || !slices.Contains(got.Targets.GeoSiteCategories, "youtube") {
		t.Fatalf("the fixture must keep the categories a run domain brings, or the check above proves nothing; got %+v", got)
	}
}

func TestAddAsNewSetRefusesHostsThatAreNotHostNames(t *testing.T) {
	tooMany := make([]string, 0, maxExtraHosts+1)
	for i := range maxExtraHosts + 1 {
		tooMany = append(tooMany, fmt.Sprintf("h%d.example.net", i))
	}
	for _, tc := range []struct {
		name  string
		hosts []string
		code  string
	}{
		{"an address", []string{"cdn.example.net", "10.0.0.1"}, "bad_host"},
		{"a public address", []string{"1.2.3.4"}, "bad_host"},
		{"a name without a dot", []string{"localhost"}, "bad_host"},
		{"a link", []string{"https://cdn.example.net/app.js"}, "bad_host"},
		{"a wildcard", []string{"*.example.net"}, "bad_host"},
		{"a regexp entry", []string{"regexp:.*\\.example\\.net"}, "bad_host"},
		{"a Unicode name", []string{"пример.рф"}, "bad_host"},
		{"more hosts than one add takes", tooMany, "too_many_hosts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, mux := pageHostsFixture(t)
			before := len(api.getCfg().Sets)
			set := discoveredStrategy()
			set.Targets.SNIDomains = []string{"svoboda.org"}
			rec := postDiscovery(t, mux, "/api/discovery/add", DiscoveredSetRequest{Set: mustJSON(t, set), Hosts: tc.hosts})
			if rec.Code != http.StatusBadRequest || refusalCode(t, rec) != tc.code {
				t.Errorf("want a 400 %s, got %d (%s)", tc.code, rec.Code, rec.Body.String())
			}
			if len(api.getCfg().Sets) != before {
				t.Error("a refused add must not create the set")
			}
		})
	}

	t.Run("the punycode form and the full count are taken", func(t *testing.T) {
		api, mux := pageHostsFixture(t)
		set := discoveredStrategy()
		set.Name = "svoboda"
		set.Targets.SNIDomains = []string{"svoboda.org"}
		hosts := append([]string{"xn--e1afmkfd.xn--p1ai", "s_1.cdn.example.net"}, tooMany[:maxExtraHosts-2]...)
		rec := postDiscovery(t, mux, "/api/discovery/add", DiscoveredSetRequest{Set: mustJSON(t, set), Hosts: hosts})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("add: %d (%s)", rec.Code, rec.Body.String())
		}
		if got := setNamed(api, "svoboda"); got == nil || len(got.Targets.SNIDomains) != maxExtraHosts+1 {
			t.Errorf("every host the scanner can give must be accepted, got %+v", got)
		}
	})
}

func TestPageHostsNeverStandInForTheRunDomains(t *testing.T) {
	api, mux := pageHostsFixture(t)
	before := len(api.getCfg().Sets)
	set := discoveredStrategy()
	set.Targets.SNIDomains = nil
	rec := postDiscovery(t, mux, "/api/discovery/add", DiscoveredSetRequest{Set: mustJSON(t, set), Hosts: []string{"cdn.example.net"}})
	if rec.Code != http.StatusBadRequest || len(api.getCfg().Sets) != before {
		t.Errorf("a set needs a run domain, hosts alone make none: %d (%s)", rec.Code, rec.Body.String())
	}

	set.Targets.SNIDomains = []string{"svoboda.org"}
	rec = postDiscovery(t, mux, "/api/discovery/add", DiscoveredSetRequest{Set: mustJSON(t, set), Hosts: []string{"cdn.example.net"}})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("add: %d (%s)", rec.Code, rec.Body.String())
	}
	if created := setNamed(api, "svoboda.org"); created == nil {
		t.Errorf("an unnamed set takes the name of its first run domain, not of a host; sets: %d", len(api.getCfg().Sets))
	}
}

func TestReplaceAddsPageHostsWhateverKeepTargetsSays(t *testing.T) {
	for _, tc := range []struct {
		name        string
		keepTargets bool
		domains     []string
		wantDomains []string
		wantMoved   []string
	}{
		{"targets kept", true, nil, []string{"meduza.io", "keep.io", "static.example.net"}, []string{"keep.io"}},
		{"domains added", false, []string{"cdn.meduza.io"}, []string{"meduza.io", "cdn.meduza.io", "keep.io", "static.example.net"}, []string{"cdn.meduza.io", "keep.io"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, mux := replaceFixture(t, nil)
			rec := replaceWith(t, mux, DiscoveryReplaceRequest{
				SetId:        "set-a",
				Set:          discoveredStrategy(),
				Domains:      tc.domains,
				StrategyOnly: true,
				KeepTargets:  tc.keepTargets,
				Hosts:        []string{"Keep.io", "static.example.net."},
			})
			if rec.Code != http.StatusOK {
				t.Fatalf("replace: %d (%s)", rec.Code, rec.Body.String())
			}
			got, other := replaceSets(api)
			if !reflect.DeepEqual(got.Targets.SNIDomains, tc.wantDomains) {
				t.Errorf("got %v want %v", got.Targets.SNIDomains, tc.wantDomains)
			}
			if !slices.Contains(got.Targets.DomainsToMatch, "static.example.net") {
				t.Errorf("the set must match the hosts it lists, got %v", got.Targets.DomainsToMatch)
			}
			if domainInList(other.Targets.SNIDomains, "keep.io") {
				t.Errorf("the exact entry of a ticked host moves into the set, other=%v", other.Targets.SNIDomains)
			}
			if pins := got.DNS.Pins; len(pins["meduza.io"]) != 1 || pins["meduza.io"][0] != "1.1.1.1" || len(pins["other.io"]) != 1 {
				t.Errorf("hosts carry no pins and leave the set's pins alone, got %v", pins)
			}
			var moved []string
			for _, m := range movedOf(t, rec) {
				moved = append(moved, m.Domain)
			}
			if !reflect.DeepEqual(moved, tc.wantMoved) {
				t.Errorf("moved %v want %v", moved, tc.wantMoved)
			}
		})
	}
}

func TestReplaceRefusesABadHostAndChangesNothing(t *testing.T) {
	api, mux := replaceFixture(t, nil)
	rec := replaceWith(t, mux, DiscoveryReplaceRequest{
		SetId:        "set-a",
		Set:          discoveredStrategy(),
		StrategyOnly: true,
		KeepTargets:  true,
		Hosts:        []string{"keep.io", "192.168.1.1"},
	})
	if rec.Code != http.StatusBadRequest || refusalCode(t, rec) != "bad_host" {
		t.Fatalf("want a 400 bad_host, got %d (%s)", rec.Code, rec.Body.String())
	}
	got, other := replaceSets(api)
	if got.Faking.Strategy != "timestamp" || domainInList(got.Targets.SNIDomains, "keep.io") || !domainInList(other.Targets.SNIDomains, "keep.io") {
		t.Errorf("a refused replace must leave both sets as they were, target faking=%q sites=%v other=%v", got.Faking.Strategy, got.Targets.SNIDomains, other.Targets.SNIDomains)
	}
}

func TestSimilarSetsIgnoreTheHosts(t *testing.T) {
	stubDiscoveryDSCPStamp(t, stampsNoSetValue)
	_, mux := replaceFixture(t, nil)
	rec := postDiscovery(t, mux, "/api/discovery/similar", DiscoveredSetRequest{
		Set:   mustJSON(t, config.NewSetConfig()),
		Hosts: []string{"not a host", "10.0.0.1"},
	})
	if rec.Code != http.StatusOK {
		t.Errorf("similar answers for the strategy alone, got %d (%s)", rec.Code, rec.Body.String())
	}
}
