package geodat

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/urlesistiana/v2dat/v2data"
)

func TestScanGeositeEntriesWalksEveryCategory(t *testing.T) {
	gm := NewGeodataManager(sampleGeoSite(t), "")

	byTag := map[string][]string{}
	if err := gm.ScanGeositeEntries(func(tag, entry string) error {
		byTag[tag] = append(byTag[tag], entry)
		return nil
	}); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(byTag["google"]) != 4 {
		t.Errorf("google should yield 4 entries, got %v", byTag["google"])
	}
	if len(byTag["netflix"]) != 2 {
		t.Errorf("netflix should yield 2 entries, got %v", byTag["netflix"])
	}
	if _, ok := byTag["empty"]; ok {
		t.Error("a category with no domains should emit nothing")
	}

	found := false
	for _, e := range byTag["google"] {
		if e == `regexp:^ad[0-9]+\.google\.com$` {
			found = true
		}
	}
	if !found {
		t.Errorf("regex entry not rendered with its prefix: %v", byTag["google"])
	}
}

func TestScanGeositeEntriesStopsOnSentinel(t *testing.T) {
	gm := NewGeodataManager(sampleGeoSite(t), "")

	seen := 0
	err := gm.ScanGeositeEntries(func(string, string) error {
		seen++
		return ErrStopScan
	})
	if err != nil {
		t.Fatalf("stopping early must not be an error, got %v", err)
	}
	if seen != 1 {
		t.Errorf("scan continued past the sentinel: %d entries", seen)
	}
}

func TestScanGeositeEntriesWithoutDatabase(t *testing.T) {
	gm := NewGeodataManager("", "")
	if err := gm.ScanGeositeEntries(func(string, string) error { return nil }); err == nil {
		t.Error("scanning with no database configured must report an error, not silently succeed")
	}
}

func TestScanGeoipPrefixes(t *testing.T) {
	path := writeGeoIP(t,
		&v2data.GeoIP{
			CountryCode: "CLOUDFLARE",
			Cidr: []*v2data.CIDR{
				{Ip: []byte{104, 16, 0, 0}, Prefix: 13},
			},
		},
		&v2data.GeoIP{
			CountryCode: "RU",
			Cidr: []*v2data.CIDR{
				{Ip: []byte{95, 173, 128, 0}, Prefix: 18},
			},
		},
	)
	gm := NewGeodataManager("", path)

	hit := map[string]netip.Prefix{}
	if err := gm.ScanGeoipPrefixes(func(tag string, p netip.Prefix) error {
		hit[tag] = p
		return nil
	}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got := hit["cloudflare"].String(); got != "104.16.0.0/13" {
		t.Errorf("cloudflare prefix = %q", got)
	}
	if !hit["cloudflare"].Contains(netip.MustParseAddr("104.22.45.1")) {
		t.Error("the emitted prefix should contain an address inside it")
	}
	if len(hit) != 2 {
		t.Errorf("expected 2 categories, got %d", len(hit))
	}
}

func TestPreviewGeoipCategory(t *testing.T) {
	path := writeGeoIP(t, &v2data.GeoIP{
		CountryCode: "RU",
		Cidr: []*v2data.CIDR{
			{Ip: []byte{95, 173, 128, 0}, Prefix: 18},
			{Ip: []byte{5, 8, 0, 0}, Prefix: 19},
			{Ip: []byte{31, 7, 224, 0}, Prefix: 21},
		},
	})
	gm := NewGeodataManager("", path)

	preview, total, err := gm.PreviewGeoipCategory(context.Background(), "ru", 2)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want the real count 3 even when the preview is capped", total)
	}
	if len(preview) != 2 {
		t.Errorf("preview should honour the limit, got %d", len(preview))
	}

	empty, total, err := gm.PreviewGeoipCategory(context.Background(), "no-such-category", 10)
	if err != nil {
		t.Fatalf("unknown category should not error: %v", err)
	}
	if total != 0 || len(empty) != 0 {
		t.Errorf("unknown category should be empty, got %d/%v", total, empty)
	}
}

func TestPreviewGeoipCategoryWithoutDatabase(t *testing.T) {
	gm := NewGeodataManager("", "")
	if _, _, err := gm.PreviewGeoipCategory(context.Background(), "ru", 10); err == nil {
		t.Error("preview with no database must report an error")
	}
	if !errors.Is(ErrStopScan, errStopScan) {
		t.Error("the exported sentinel must be the one scanEntries recognises")
	}
}
