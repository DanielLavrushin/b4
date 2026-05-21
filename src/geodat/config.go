package geodat

import (
	"path/filepath"

	"github.com/daniellavrushin/b4/log"
)

type GeoDatConfig struct {
	GeoSitePath string              `json:"sitedat_path"`
	GeoIpPath   string              `json:"ipdat_path"`
	GeoSiteURL  string              `json:"sitedat_url"`
	GeoIpURL    string              `json:"ipdat_url"`
	AutoUpdate  GeoAutoUpdateConfig `json:"auto_update"`
}

type GeoAutoUpdateConfig struct {
	OnStartup bool   `json:"on_startup,omitempty"`
	Interval  string `json:"interval,omitempty"`
	LastRun   string `json:"last_run,omitempty"`
}

func (c *GeoDatConfig) SanitizePaths(defaultDir string) {
	c.GeoSitePath = sanitizeGeoPath("sitedat_path", c.GeoSitePath, defaultDir)
	c.GeoIpPath = sanitizeGeoPath("ipdat_path", c.GeoIpPath, defaultDir)
}

func sanitizeGeoPath(field, p, defaultDir string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	base := filepath.Base(p)
	if base == "" || base == "." || base == "/" {
		log.Warnf("[GEODAT] dropping unusable %s=%q (not an absolute path)", field, p)
		return ""
	}
	if !filepath.IsAbs(defaultDir) {
		log.Warnf("[GEODAT] dropping %s=%q (no absolute base directory to heal from)", field, p)
		return ""
	}
	healed := filepath.Join(defaultDir, base)
	log.Warnf("[GEODAT] rewriting non-absolute %s=%q to %q", field, p, healed)
	return healed
}
