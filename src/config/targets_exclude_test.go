package config

import (
	"reflect"
	"testing"
)

func TestGetTargetsForSetDropsExcludedCategoryEntries(t *testing.T) {
	cfg := NewConfig()
	cfg.System.Geo.GeoSitePath = "/nonexistent/geosite.dat"
	cfg.System.Geo.GeoIpPath = "/nonexistent/geoip.dat"
	set := NewSetConfig()
	set.Targets.GeoSiteCategories = []string{"youtube", "google"}
	set.Targets.GeoIpCategories = []string{"google"}
	set.Targets.SNIDomains = []string{"youtubei.googleapis.com"}
	set.Targets.GeoSiteExclude = map[string][]string{"youtube": {"YouTubei.googleapis.com", "regexp:^yt[0-9]\\.ggpht\\.com$"}}
	set.Targets.GeoIpExclude = map[string][]string{"google": {"8.8.8.0/24"}}

	geosite := map[string][]string{
		"youtube": {"youtube.com", "youtubei.googleapis.com", "regexp:^yt[0-9]\\.ggpht\\.com$", "googleapis.com.ua"},
		"google":  {"youtubei.googleapis.com", "google.com"},
	}
	geoip := map[string][]string{"google": {"8.8.8.0/24", "8.8.4.0/24"}}

	domains, ips, err := cfg.GetTargetsForSetWithCache(&set, geosite, geoip)
	if err != nil {
		t.Fatal(err)
	}
	wantDomains := []string{"youtube.com", "googleapis.com.ua", "youtubei.googleapis.com", "google.com", "youtubei.googleapis.com"}
	if !reflect.DeepEqual(domains, wantDomains) {
		t.Fatalf("domains = %v, want %v", domains, wantDomains)
	}
	if want := []string{"8.8.4.0/24"}; !reflect.DeepEqual(ips, want) {
		t.Fatalf("ips = %v, want %v", ips, want)
	}
	if got := geosite["youtube"]; len(got) != 4 {
		t.Fatalf("the cached category must stay intact, got %v", got)
	}
}

func TestPruneCategoryExcludes(t *testing.T) {
	got := pruneCategoryExcludes(map[string][]string{
		"youtube": {" YouTubei.googleapis.com ", "youtubei.googleapis.com", ""},
		"removed": {"example.com"},
		"empty":   {},
	}, []string{"youtube", "empty"})
	want := map[string][]string{"youtube": {"youtubei.googleapis.com"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := pruneCategoryExcludes(map[string][]string{"removed": {"a.com"}}, nil); got != nil {
		t.Fatalf("no kept category must give nil so the field is omitted, got %v", got)
	}
}
