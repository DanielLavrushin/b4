package discovery

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestInstalledGeoCategories(t *testing.T) {
	ds := &DiscoverySuite{cfg: &config.Config{}}

	geoip, geosite := ds.installedGeoCategories([]string{"google"}, []string{"youtube"})
	if len(geoip) != 0 {
		t.Errorf("geoip categories kept without a geoip database: %v", geoip)
	}
	if len(geosite) != 0 {
		t.Errorf("geosite categories kept without a geosite database: %v", geosite)
	}

	ds.cfg.System.Geo.GeoSitePath = "/etc/b4/geosite.dat"
	geoip, geosite = ds.installedGeoCategories([]string{"google"}, []string{"youtube"})
	if len(geoip) != 0 {
		t.Errorf("geoip categories kept without a geoip database: %v", geoip)
	}
	if len(geosite) != 1 || geosite[0] != "youtube" {
		t.Errorf("geosite categories dropped with a geosite database: %v", geosite)
	}

	ds.cfg.System.Geo.GeoIpPath = "/etc/b4/geoip.dat"
	geoip, geosite = ds.installedGeoCategories([]string{"google"}, []string{"youtube"})
	if len(geoip) != 1 || geoip[0] != "google" {
		t.Errorf("geoip categories dropped with a geoip database: %v", geoip)
	}
	if len(geosite) != 1 || geosite[0] != "youtube" {
		t.Errorf("geosite categories dropped with a geosite database: %v", geosite)
	}
}

func TestInstalledGeoCategoriesWithoutConfig(t *testing.T) {
	ds := &DiscoverySuite{}

	geoip, geosite := ds.installedGeoCategories([]string{"google"}, []string{"youtube"})
	if len(geoip) != 0 || len(geosite) != 0 {
		t.Errorf("categories kept without a config: %v %v", geoip, geosite)
	}
}
