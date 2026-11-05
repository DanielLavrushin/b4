package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

func fakePreview(entries []string) categoryPreviewFunc {
	return func(_ context.Context, _ string, limit int) ([]string, int, error) {
		return entries[:min(limit, len(entries))], len(entries), nil
	}
}

func previewRequest(t *testing.T, preview categoryPreviewFunc, query string) CategoryPreviewResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	servePreview(rec, httptest.NewRequest(http.MethodGet, "/api/geosite/category?"+query, nil), preview)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp CategoryPreviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestServePreviewSearchesTheWholeCategory(t *testing.T) {
	var entries []string
	for i := range 300 {
		entries = append(entries, fmt.Sprintf("host%d.example.com", i))
	}
	entries = append(entries, "youtubei.googleapis.com", "YouTube.googleapis.com")
	preview := fakePreview(entries)

	got := previewRequest(t, preview, "tag=youtube")
	if got.PreviewCount != defaultPreviewLimit || got.TotalDomains != len(entries) || got.Matched != len(entries) {
		t.Fatalf("default request: %+v", got)
	}

	got = previewRequest(t, preview, "tag=youtube&q=GoogleAPIs")
	if want := []string{"youtubei.googleapis.com", "YouTube.googleapis.com"}; !reflect.DeepEqual(got.Preview, want) || got.Matched != 2 {
		t.Fatalf("search past the default limit: %+v", got)
	}

	got = previewRequest(t, preview, "tag=youtube&q=host1&limit=3")
	if got.PreviewCount != 3 || got.Matched != 111 {
		t.Fatalf("limited search: %+v", got)
	}

	got = previewRequest(t, preview, "tag=youtube&q=nothing")
	if got.Preview == nil || got.Matched != 0 {
		t.Fatalf("empty search must return an empty array: %+v", got)
	}

	got = previewRequest(t, preview, "tag=youtube&limit=100000")
	if got.PreviewCount != len(entries) {
		t.Fatalf("limit is capped at %d, not below the category size: %+v", maxPreviewLimit, got)
	}
}

func TestHandleSetOverlaps(t *testing.T) {
	apple := &config.SetConfig{Id: "apple", Name: "APPLE", Enabled: true}
	apple.Targets.GeoSiteCategories = []string{"apple"}
	apple.Targets.DomainsToMatch = []string{"apple.com", "icloud.com"}
	apple.Targets.SNIDomains = []string{"apple.com"}
	dpi := &config.SetConfig{Id: "dpi", Name: "DPI_SNI", Enabled: true}
	dpi.Targets.GeoSiteCategories = []string{"youtube", "apple"}
	dpi.Targets.DomainsToMatch = []string{"youtube.com", "apple.com", "icloud.com"}

	mux := checkDomainAPI(t, []*config.SetConfig{apple, dpi})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sets/overlaps", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got []sni.SetOverlap
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SetIds != [2]string{"apple", "dpi"} || got[0].Domains != 2 ||
		!reflect.DeepEqual(got[0].SharedGeosite, []string{"apple"}) || got[0].Examples[0].WinnerId != "apple" ||
		got[0].Examples[0].Entries[0].Source != "manual" || got[0].Examples[0].Reason != sni.OverlapReasonOrder {
		t.Fatalf("got %+v", got)
	}
}
