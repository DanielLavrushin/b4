package handler

import (
	"net/http"
	"net/netip"
	"sort"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/detector"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/utils"
)

const maxDiscoverySuggestions = 10

const (
	suggestSourceStored   = "stored"
	suggestSourceHistory  = "history"
	suggestSourceDetector = "detector"
	suggestSourceDomain   = "domain"
	suggestSourceService  = "service"
)

// @Summary Suggest URLs to probe when running discovery for a set
// @Description Up to 10 URLs, one per host, in this order: the set's stored discovery URLs, earlier runs for the set, detector sites the set matches, the set's own domains, and known services of its geosite categories. owner_set_id names the set that handles the host today.
// @Tags Discovery
// @Produce json
// @Param set_id query string true "Set ID"
// @Success 200 {object} DiscoverySuggestResponse
// @Failure 400 {object} APIError
// @Failure 404 {object} APIError
// @Security BearerAuth
// @Router /discovery/suggest [get]
func (api *API) handleDiscoverySuggest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	setID := strings.TrimSpace(r.URL.Query().Get("set_id"))
	if setID == "" {
		writeAPIError(w, ErrBadRequest("set_id is required"))
		return
	}
	cfg := api.getCfg()
	set := cfg.GetSetById(setID)
	if set == nil {
		writeAPIError(w, ErrNotFound("Set not found"))
		return
	}

	sendResponse(w, DiscoverySuggestResponse{SetId: set.Id, URLs: api.discoverySuggestions(cfg, set)})
}

type suggestionList struct {
	items []DiscoverySuggestion
	hosts map[string]bool
}

func (l *suggestionList) full() bool {
	return len(l.items) >= maxDiscoverySuggestions
}

func (l *suggestionList) add(raw, source string) {
	if l.full() {
		return
	}
	canonical, host, err := utils.NormalizeProbeURL(raw)
	if err != nil || l.hosts[host] {
		return
	}
	l.hosts[host] = true
	l.items = append(l.items, DiscoverySuggestion{URL: canonical, Host: host, Source: source})
}

func (api *API) discoverySuggestions(cfg *config.Config, set *config.SetConfig) []DiscoverySuggestion {
	list := &suggestionList{items: []DiscoverySuggestion{}, hosts: map[string]bool{}}

	for _, u := range set.Discovery.URLs {
		list.add(u, suggestSourceStored)
	}

	if !list.full() {
		for _, u := range historyURLsForSet(cfg.ConfigPath, set.Id) {
			list.add(u, suggestSourceHistory)
		}
	}

	if !list.full() {
		single := *set
		single.Enabled = true
		matcher := sni.NewSuffixSet([]*config.SetConfig{&single})
		for _, site := range detector.Lists().Sites {
			raw := site
			if !strings.Contains(raw, "://") {
				raw = "https://" + raw
			}
			_, host, err := utils.NormalizeProbeURL(raw)
			if err != nil || setMatchedBy(matcher, host) == nil {
				continue
			}
			list.add(raw, suggestSourceDetector)
		}
	}

	for _, entry := range set.Targets.SNIDomains {
		value, isRegex := sni.ParseDomainEntry(entry)
		if isRegex || !looksLikeHostName(value) {
			continue
		}
		list.add("https://"+value+"/", suggestSourceDomain)
	}

	for _, host := range discovery.KnownServiceHosts(set.Targets.GeoSiteCategories) {
		list.add("https://"+host+"/", suggestSourceService)
	}

	matcher := api.engineMatcher()
	for i := range list.items {
		if owner := setMatchedBy(matcher, list.items[i].Host); owner != nil {
			list.items[i].OwnerSetId = owner.Id
			list.items[i].OwnerSetName = owner.Name
		}
	}
	return list.items
}

func historyURLsForSet(configPath, setID string) []string {
	hist := discovery.GetHistory(configPath)
	if hist == nil || setID == "" {
		return nil
	}
	entries := make([]discovery.HistoryEntry, 0, len(hist.Entries))
	for _, e := range hist.Entries {
		if historyEntryBelongsTo(e, setID) {
			entries = append(entries, e)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].EndTime.After(entries[j].EndTime)
	})
	urls := make([]string, 0, len(entries))
	for _, e := range entries {
		switch {
		case e.Url != "":
			urls = append(urls, e.Url)
		case e.Domain != "":
			urls = append(urls, "https://"+e.Domain+"/")
		}
	}
	return urls
}

func historyEntryBelongsTo(e discovery.HistoryEntry, setID string) bool {
	if e.SetId == setID {
		return true
	}
	for _, mark := range e.Applied {
		if mark.SetId == setID {
			return true
		}
	}
	return false
}

func looksLikeHostName(value string) bool {
	if !strings.Contains(value, ".") || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}
