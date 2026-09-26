package handler

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/geodat"
)

func TestSaveGeoConfigRefreshesTheFirewallWhenIpsetsChange(t *testing.T) {
	useAsnStore(t)
	dup := asnSet("dup")
	dup.TCP.Duplicate.Enabled = true
	dup.TCP.Duplicate.Count = 2
	dup.Targets.GeoIpCategories = []string{"telegram"}
	dup.Targets.IpsToMatch = []string{"91.108.4.0/22"}
	api, _ := asnAPI(t, dup)
	dir := t.TempDir()
	api.getCfg().System.Geo.GeoIpPath = filepath.Join(dir, "old.dat")
	refreshed := countRefreshes(t)

	if err := api.saveGeoConfig(func(geo *geodat.GeoDatConfig) { geo.GeoIpPath = filepath.Join(dir, "empty.dat") }); err != nil {
		t.Fatal(err)
	}
	if got := api.getCfg().GetSetById("dup").Targets.IpsToMatch; len(got) != 0 {
		t.Fatalf("the addresses of the old file are gone: %v", got)
	}
	if *refreshed != 1 {
		t.Fatalf("the duplicate ipset must drop them too: %d refreshes", *refreshed)
	}
}

func TestSaveGeoConfigExpandsASNsOfEverySet(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	api, _ := asnAPI(t, asnSet("tg", "62041"))
	if err := api.saveGeoConfig(func(geo *geodat.GeoDatConfig) {}); err != nil {
		t.Fatal(err)
	}
	if got := api.getCfg().GetSetById("tg").Targets.IpsToMatch; !slices.Equal(got, telegramPrefixes) {
		t.Fatalf("a geodat reload keeps ASN prefixes: %v", got)
	}
}
