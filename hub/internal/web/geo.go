package web

import (
	"net/http"
	"strings"

	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	geositeMissingWarning = "geosite_categories_missing"
	geoipMissingWarning   = "geoip_categories_missing"
)

type GeoCategoriesView struct {
	GeoSite []string `json:"geosite"`
	GeoIP   []string `json:"geoip"`
}

type geoCategoryLists struct {
	site    []string
	ip      []string
	hasSite bool
	hasIP   bool
}

func (s *Server) geoIndex() *geodat.GeodataManager {
	if s.Geo == nil {
		return nil
	}
	s.geoMu.Lock()
	defer s.geoMu.Unlock()
	if s.geodata == nil {
		s.geodata = geodat.NewGeodataManager(s.Geo.GeoSitePath(), s.Geo.GeoIPPath())
	}
	return s.geodata
}

func (s *Server) geoCategoryLists() geoCategoryLists {
	var out geoCategoryLists
	gm := s.geoIndex()
	if gm == nil {
		return out
	}
	if tags, err := gm.ListCategories(s.Geo.GeoSitePath()); err == nil {
		out.site, out.hasSite = tags, true
	}
	if tags, err := gm.ListCategories(s.Geo.GeoIPPath()); err == nil {
		out.ip, out.hasIP = tags, true
	}
	return out
}

func (s *Server) geoCategories(w http.ResponseWriter, r *http.Request) {
	lists := s.geoCategoryLists()
	writeJSON(w, http.StatusOK, GeoCategoriesView{GeoSite: orEmpty(lists.site), GeoIP: orEmpty(lists.ip)})
}

func siteCategory(name string) string {
	tag, _, _ := strings.Cut(name, "@")
	return strings.ToLower(strings.TrimSpace(tag))
}

func ipCategory(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func missingCategories(known, wanted []string, key func(string) string) []string {
	have := make(map[string]bool, len(known))
	for _, tag := range known {
		have[strings.ToLower(tag)] = true
	}
	seen := make(map[string]bool, len(wanted))
	var missing []string
	for _, name := range wanted {
		if strings.TrimSpace(name) == "" || have[key(name)] || seen[name] {
			continue
		}
		seen[name] = true
		missing = append(missing, name)
	}
	return missing
}

func (s *Server) categoryWarnings(projection map[string]interface{}) []hubwire.Warning {
	site := store.TargetList(projection, "geosite_categories")
	ip := store.TargetList(projection, "geoip_categories")
	if len(site) == 0 && len(ip) == 0 {
		return nil
	}
	lists := s.geoCategoryLists()
	var out []hubwire.Warning
	if missing := missingCategories(lists.site, site, siteCategory); lists.hasSite && len(missing) > 0 {
		out = append(out, hubwire.Warning{Code: geositeMissingWarning, Params: map[string]interface{}{"categories": missing}})
	}
	if missing := missingCategories(lists.ip, ip, ipCategory); lists.hasIP && len(missing) > 0 {
		out = append(out, hubwire.Warning{Code: geoipMissingWarning, Params: map[string]interface{}{"categories": missing}})
	}
	return out
}
