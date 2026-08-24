package geodat

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/urlesistiana/v2dat/v2data"
	"google.golang.org/protobuf/proto"
)

func writeGeoSite(t *testing.T, entries ...*v2data.GeoSite) string {
	t.Helper()

	b, err := proto.Marshal(&v2data.GeoSiteList{Entry: entries})
	if err != nil {
		t.Fatalf("marshal geosite: %v", err)
	}

	path := filepath.Join(t.TempDir(), "geosite.dat")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write geosite: %v", err)
	}
	return path
}

func writeGeoIP(t *testing.T, entries ...*v2data.GeoIP) string {
	t.Helper()

	b, err := proto.Marshal(&v2data.GeoIPList{Entry: entries})
	if err != nil {
		t.Fatalf("marshal geoip: %v", err)
	}

	path := filepath.Join(t.TempDir(), "geoip.dat")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write geoip: %v", err)
	}
	return path
}

func dom(kind v2data.Domain_Type, value string) *v2data.Domain {
	return &v2data.Domain{Type: kind, Value: value}
}

func sampleGeoSite(t *testing.T) string {
	return writeGeoSite(t,
		&v2data.GeoSite{
			CountryCode: "GOOGLE",
			Domain: []*v2data.Domain{
				dom(v2data.Domain_Domain, "google.com"),
				dom(v2data.Domain_Full, "www.google.com"),
				dom(v2data.Domain_Regex, `^ad[0-9]+\.google\.com$`),
				dom(v2data.Domain_Plain, "gstatic"),
			},
		},
		&v2data.GeoSite{
			CountryCode: "NETFLIX",
			Domain: []*v2data.Domain{
				dom(v2data.Domain_Domain, "netflix.com"),
				dom(v2data.Domain_Domain, "nflxvideo.net"),
			},
		},
		&v2data.GeoSite{
			CountryCode: "EMPTY",
		},
	)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListCategoriesLowercasesAndSorts(t *testing.T) {
	path := sampleGeoSite(t)
	gm := NewGeodataManager(path, "")

	tags, err := gm.ListCategories(path)
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}

	want := []string{"empty", "google", "netflix"}
	if !equalStrings(tags, want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
}

func TestLoadDomainsFromCategories(t *testing.T) {
	path := sampleGeoSite(t)

	got, err := LoadDomainsFromCategories(path, []string{"google"})
	if err != nil {
		t.Fatalf("LoadDomainsFromCategories: %v", err)
	}

	want := []string{
		"google.com",
		"www.google.com",
		`regexp:^ad[0-9]+\.google\.com$`,
		"gstatic",
	}
	if !equalStrings(got, want) {
		t.Fatalf("domains = %v, want %v", got, want)
	}
}

func TestLoadDomainsEmitsFileOrderAndStripsAttrFilter(t *testing.T) {
	path := sampleGeoSite(t)

	got, err := LoadDomainsFromCategories(path, []string{"NETFLIX", "google@ads"})
	if err != nil {
		t.Fatalf("LoadDomainsFromCategories: %v", err)
	}

	if len(got) != 6 {
		t.Fatalf("expected 6 domains across both categories, got %d (%v)", len(got), got)
	}
	if got[0] != "google.com" {
		t.Fatalf("expected file order (google first), got %v", got)
	}
}

func TestLoadDomainsUnknownCategory(t *testing.T) {
	path := sampleGeoSite(t)

	got, err := LoadDomainsFromCategories(path, []string{"does-not-exist"})
	if err != nil {
		t.Fatalf("LoadDomainsFromCategories: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no domains, got %v", got)
	}
}

func TestLoadDomainsMissingFile(t *testing.T) {
	got, err := LoadDomainsFromCategories(filepath.Join(t.TempDir(), "nope.dat"), []string{"google"})
	if err != nil {
		t.Fatalf("expected missing file to be tolerated, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no domains, got %v", got)
	}
}

func TestEmptyDomainValueIsDropped(t *testing.T) {
	path := writeGeoSite(t, &v2data.GeoSite{
		CountryCode: "BROKEN",
		Domain: []*v2data.Domain{
			dom(v2data.Domain_Regex, ""),
			dom(v2data.Domain_Domain, ""),
			dom(v2data.Domain_Domain, "ok.example"),
		},
	})

	got, err := LoadDomainsFromCategories(path, []string{"broken"})
	if err != nil {
		t.Fatalf("LoadDomainsFromCategories: %v", err)
	}
	if !equalStrings(got, []string{"ok.example"}) {
		t.Fatalf("domains = %v, want [ok.example]", got)
	}
}

func TestDomainAttributesAreSkipped(t *testing.T) {
	path := writeGeoSite(t, &v2data.GeoSite{
		CountryCode: "ATTRS",
		Domain: []*v2data.Domain{
			{
				Type:  v2data.Domain_Domain,
				Value: "attr.example",
				Attribute: []*v2data.Domain_Attribute{
					{Key: "ads", TypedValue: &v2data.Domain_Attribute_BoolValue{BoolValue: true}},
					{Key: "weight", TypedValue: &v2data.Domain_Attribute_IntValue{IntValue: 42}},
				},
			},
			dom(v2data.Domain_Domain, "plain.example"),
		},
	})

	got, err := LoadDomainsFromCategories(path, []string{"attrs"})
	if err != nil {
		t.Fatalf("LoadDomainsFromCategories: %v", err)
	}
	if !equalStrings(got, []string{"attr.example", "plain.example"}) {
		t.Fatalf("domains = %v", got)
	}
}

func TestLoadIpsFromCategories(t *testing.T) {
	path := writeGeoIP(t,
		&v2data.GeoIP{
			CountryCode: "RU",
			Cidr: []*v2data.CIDR{
				{Ip: []byte{5, 45, 192, 0}, Prefix: 18},
				{Ip: []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, Prefix: 32},
			},
		},
		&v2data.GeoIP{
			CountryCode: "US",
			Cidr:        []*v2data.CIDR{{Ip: []byte{8, 8, 8, 0}, Prefix: 24}},
		},
	)

	got, err := LoadIpsFromCategories(path, []string{"ru"})
	if err != nil {
		t.Fatalf("LoadIpsFromCategories: %v", err)
	}

	want := []string{"5.45.192.0/18", "2001:db8::/32"}
	if !equalStrings(got, want) {
		t.Fatalf("ips = %v, want %v", got, want)
	}
}

func TestInvalidCidrIsSkipped(t *testing.T) {
	path := writeGeoIP(t, &v2data.GeoIP{
		CountryCode: "BAD",
		Cidr: []*v2data.CIDR{
			{Ip: []byte{1, 2, 3}, Prefix: 24},
			{Ip: []byte{1, 2, 3, 4}, Prefix: 99},
			{Ip: []byte{9, 9, 9, 0}, Prefix: 24},
		},
	})

	got, err := LoadIpsFromCategories(path, []string{"bad"})
	if err != nil {
		t.Fatalf("LoadIpsFromCategories: %v", err)
	}
	if !equalStrings(got, []string{"9.9.9.0/24"}) {
		t.Fatalf("ips = %v, want [9.9.9.0/24]", got)
	}
}

func TestCountsMatchLoad(t *testing.T) {
	path := sampleGeoSite(t)

	counts, err := CountDomainsInCategories(path, []string{"google", "NETFLIX", "missing"})
	if err != nil {
		t.Fatalf("CountDomainsInCategories: %v", err)
	}

	if counts["google"] != 4 {
		t.Errorf("google count = %d, want 4", counts["google"])
	}
	if counts["NETFLIX"] != 2 {
		t.Errorf("NETFLIX count = %d, want 2", counts["NETFLIX"])
	}
	if counts["missing"] != 0 {
		t.Errorf("missing count = %d, want 0", counts["missing"])
	}
}

func TestPreviewLimitsWithoutLosingTotal(t *testing.T) {
	path := sampleGeoSite(t)

	preview, total, err := PreviewDomainsInCategory(context.Background(), path, "google", 2)
	if err != nil {
		t.Fatalf("PreviewDomainsInCategory: %v", err)
	}
	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
	if !equalStrings(preview, []string{"google.com", "www.google.com"}) {
		t.Errorf("preview = %v", preview)
	}
}

func TestPreviewDoesNotPopulateCache(t *testing.T) {
	path := sampleGeoSite(t)
	gm := NewGeodataManager(path, "")

	if _, _, err := gm.PreviewGeositeCategory(context.Background(), "google", 2); err != nil {
		t.Fatalf("PreviewGeositeCategory: %v", err)
	}

	gm.mu.RLock()
	cached := len(gm.categoryDomains)
	gm.mu.RUnlock()

	if cached != 0 {
		t.Fatalf("preview populated the domain cache with %d categories", cached)
	}
}

func TestRetainCategoriesEvictsUnselected(t *testing.T) {
	path := sampleGeoSite(t)
	gm := NewGeodataManager(path, "")

	if _, err := gm.LoadGeositeCategory("google"); err != nil {
		t.Fatalf("LoadGeositeCategory: %v", err)
	}
	if _, err := gm.LoadGeositeCategory("netflix"); err != nil {
		t.Fatalf("LoadGeositeCategory: %v", err)
	}

	gm.RetainCategories([]string{"netflix"}, nil)

	gm.mu.RLock()
	_, hasGoogle := gm.categoryDomains["google"]
	_, hasNetflix := gm.categoryDomains["netflix"]
	_, hasGoogleCount := gm.categoryDomainsCounts["google"]
	gm.mu.RUnlock()

	if hasGoogle || hasGoogleCount {
		t.Error("google should have been evicted")
	}
	if !hasNetflix {
		t.Error("netflix should have been retained")
	}
}

func TestCachedCountsAvoidRescan(t *testing.T) {
	path := sampleGeoSite(t)
	gm := NewGeodataManager(path, "")

	if _, err := gm.LoadGeositeCategory("google"); err != nil {
		t.Fatalf("LoadGeositeCategory: %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	counts, err := gm.GetGeositeCategoryCounts([]string{"google"})
	if err != nil {
		t.Fatalf("GetGeositeCategoryCounts: %v", err)
	}
	if counts["google"] != 4 {
		t.Fatalf("google count = %d, want 4 from cache", counts["google"])
	}
}

func TestTruncatedFileIsRejected(t *testing.T) {
	path := sampleGeoSite(t)

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	truncated := filepath.Join(t.TempDir(), "truncated.dat")
	if err := os.WriteFile(truncated, b[:len(b)/2], 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	gm := NewGeodataManager(truncated, "")
	if _, err := gm.ListCategories(truncated); err == nil {
		t.Fatal("expected error on truncated file")
	}
}

func TestOversizedEntryLengthIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bogus.dat")
	if err := os.WriteFile(path, []byte{0x0A, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F, 0x00}, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	gm := NewGeodataManager(path, "")
	if _, err := gm.ListCategories(path); err == nil {
		t.Fatal("expected error for entry length larger than the file")
	}
}

func TestGarbageFileIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.dat")
	if err := os.WriteFile(path, []byte("this is not a v2ray dat file"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	gm := NewGeodataManager(path, "")
	if _, err := gm.ListCategories(path); err == nil {
		t.Fatal("expected error for garbage file")
	}
}

func TestScanStopsAfterLastWantedCategory(t *testing.T) {
	b, err := proto.Marshal(&v2data.GeoSiteList{Entry: []*v2data.GeoSite{
		{CountryCode: "FIRST", Domain: []*v2data.Domain{dom(v2data.Domain_Domain, "first.example")}},
	}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	path := filepath.Join(t.TempDir(), "trailing.dat")
	if err := os.WriteFile(path, append(b, 0xFF, 0xFF, 0xFF), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	seen := 0
	err = streamGeoSite(path, []string{"first"}, func(_ string, _ uint64, _ string) error {
		seen++
		return nil
	})
	if err != nil {
		t.Fatalf("scan should have stopped before the trailing garbage, got %v", err)
	}
	if seen != 1 {
		t.Fatalf("emitted %d domains, want 1", seen)
	}

	if err := streamGeoSite(path, []string{"absent"}, func(string, uint64, string) error { return nil }); err == nil {
		t.Fatal("expected the trailing garbage to be reached and rejected when no category matches early")
	}
}
