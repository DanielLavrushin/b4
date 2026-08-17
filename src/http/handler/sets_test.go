package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func checkDomainAPI(t *testing.T, sets []*config.SetConfig) *http.ServeMux {
	t.Helper()

	cfg := config.NewConfig()
	cfg.Sets = sets

	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSetsApi()

	return mux
}

func checkDomain(t *testing.T, mux *http.ServeMux, query string) []SetDomainMatch {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/sets/check-domain?"+query, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var matches []SetDomainMatch
	if err := json.Unmarshal(rec.Body.Bytes(), &matches); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if matches == nil {
		t.Fatal("expected an array, got null")
	}

	return matches
}

func TestHandleCheckDomain(t *testing.T) {
	manual := &config.SetConfig{Id: "manual-set", Name: "Manual", Enabled: true}
	manual.Targets.SNIDomains = []string{"example.com"}
	manual.Targets.DomainsToMatch = []string{"example.com"}

	geo := &config.SetConfig{Id: "geo-set", Name: "Geo", Enabled: true}
	geo.Targets.DomainsToMatch = []string{"cdn.example.org", `regexp:^video-[0-9]+\.example\.net$`}

	disabled := &config.SetConfig{Id: "disabled-set", Name: "Disabled", Enabled: false}
	disabled.Targets.SNIDomains = []string{"blocked.test"}
	disabled.Targets.DomainsToMatch = []string{"blocked.test"}

	mux := checkDomainAPI(t, []*config.SetConfig{manual, geo, disabled})

	t.Run("exact match", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=Example.com")
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].SetId != "manual-set" || matches[0].Relation != "exact" || matches[0].Via != "manual" {
			t.Errorf("unexpected match: %+v", matches[0])
		}
		if !matches[0].Enabled {
			t.Error("expected enabled set")
		}
	})

	t.Run("subdomain reported as covered", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=www.example.com")
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].Relation != "covered" || matches[0].Entry != "example.com" {
			t.Errorf("unexpected match: %+v", matches[0])
		}
	})

	t.Run("parent domain reported as covers", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=example.org")
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].SetId != "geo-set" || matches[0].Relation != "covers" || matches[0].Via != "geosite" {
			t.Errorf("unexpected match: %+v", matches[0])
		}
	})

	t.Run("regexp entry", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=video-12.example.net")
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].Relation != "regexp" || matches[0].Entry != `regexp:^video-[0-9]+\.example\.net$` {
			t.Errorf("unexpected match: %+v", matches[0])
		}
	})

	t.Run("disabled set is reported as disabled", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=blocked.test")
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].Enabled {
			t.Error("expected disabled set")
		}
	})

	t.Run("exclude skips a set", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=www.example.com&exclude=manual-set")
		if len(matches) != 0 {
			t.Fatalf("expected no matches, got %+v", matches)
		}
	})

	t.Run("no match returns empty array", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=unrelated.test")
		if len(matches) != 0 {
			t.Fatalf("expected no matches, got %+v", matches)
		}
	})

	t.Run("several domains in one request", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=www.example.com%2Cunrelated.test+cdn.example.org")
		if len(matches) != 2 {
			t.Fatalf("expected 2 matches, got %+v", matches)
		}
		if matches[0].Domain != "www.example.com" || matches[0].Relation != "covered" {
			t.Errorf("unexpected first match: %+v", matches[0])
		}
		if matches[1].Domain != "cdn.example.org" || matches[1].Relation != "exact" {
			t.Errorf("unexpected second match: %+v", matches[1])
		}
	})

	t.Run("missing domain is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sets/check-domain?domain=+", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("POST not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/sets/check-domain", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rec.Code)
		}
	})
}

func TestParseCheckDomains(t *testing.T) {
	got, truncated := parseCheckDomains(" A.com, b.com | c.com;\td.com. a.com ")
	want := []string{"a.com", "b.com", "c.com", "d.com"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if truncated {
		t.Error("four domains must not report truncation")
	}

	if blank, _ := parseCheckDomains("   "); len(blank) != 0 {
		t.Error("expected no domains for blank input")
	}
}

func TestParseCheckDomainsReportsTruncation(t *testing.T) {
	raw := make([]string, 0, maxCheckDomains+5)
	for i := 0; i < maxCheckDomains+5; i++ {
		raw = append(raw, fmt.Sprintf("d%d.example", i))
	}

	got, truncated := parseCheckDomains(strings.Join(raw, ","))
	if len(got) != maxCheckDomains {
		t.Fatalf("got %d domains, want %d", len(got), maxCheckDomains)
	}
	if !truncated {
		t.Error("dropping domains past the cap must be reported")
	}

	exact, truncated := parseCheckDomains(strings.Join(raw[:maxCheckDomains], ","))
	if len(exact) != maxCheckDomains {
		t.Fatalf("got %d domains, want %d", len(exact), maxCheckDomains)
	}
	if truncated {
		t.Error("a list that exactly fills the cap must not report truncation")
	}
}
