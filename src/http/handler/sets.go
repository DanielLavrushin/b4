package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"github.com/google/uuid"
)

func (api *API) RegisterSetsApi() {
	api.mux.HandleFunc("/api/sets", api.handleSets)
	api.mux.HandleFunc("/api/sets/targeted-domains", api.handleTargetedDomains)
	api.mux.HandleFunc("/api/sets/check-domain", api.handleCheckDomain)
	api.mux.HandleFunc("/api/sets/{id}", api.handleSetById)
	api.mux.HandleFunc("/api/sets/reorder", api.handleReorderSets)
	api.mux.HandleFunc("/api/sets/{id}/add-domain", api.handleSetDomains)
	api.mux.HandleFunc("/api/sets/batch-delete", api.handleBatchDeleteSets)
	api.mux.HandleFunc("/api/sets/batch-set-enabled", api.handleBatchSetEnabled)
}

// @Summary List all targeted domains from enabled sets
// @Tags Sets
// @Produce json
// @Success 200 {array} string
// @Security BearerAuth
// @Router /sets/targeted-domains [get]
func (api *API) handleTargetedDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	domains := make(map[string]bool)
	for _, set := range api.getCfg().Sets {
		if !set.Enabled {
			continue
		}
		for _, d := range set.Targets.DomainsToMatch {
			domains[d] = true
		}
	}

	result := make([]string, 0, len(domains))
	for d := range domains {
		result = append(result, d)
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(result)
}

type SetDomainMatch struct {
	Domain   string `json:"domain"`
	SetName  string `json:"set_name"`
	SetId    string `json:"set_id"`
	Via      string `json:"via"`
	Relation string `json:"relation"`
	Entry    string `json:"entry"`
	Enabled  bool   `json:"enabled"`
}

type DomainReassignment struct {
	Domain  string `json:"domain"`
	SetName string `json:"set_name"`
	SetId   string `json:"set_id"`
}

func (api *API) releaseDomainsFromOtherSets(sets []*config.SetConfig, keepSetId string, domains []string) []DomainReassignment {
	claimed := make(map[string]bool, len(domains))
	for _, domain := range domains {
		if canonical := sni.CanonicalDomainEntry(domain); canonical != "" {
			claimed[canonical] = true
		}
	}
	if len(claimed) == 0 {
		return nil
	}

	var moved []DomainReassignment
	for _, set := range sets {
		if set == nil || set.Id == keepSetId || !set.Enabled {
			continue
		}

		kept := make([]string, 0, len(set.Targets.SNIDomains))
		var removed []string
		for _, entry := range set.Targets.SNIDomains {
			canonical := sni.CanonicalDomainEntry(entry)
			if canonical != "" && claimed[canonical] {
				removed = append(removed, canonical)
				continue
			}
			kept = append(kept, entry)
		}
		if len(removed) == 0 {
			continue
		}

		set.Targets.SNIDomains = kept
		api.loadTargetsForSetCached(set)

		for _, domain := range removed {
			moved = append(moved, DomainReassignment{Domain: domain, SetName: set.Name, SetId: set.Id})
		}
		log.Infof("Set '%s': released %v to the set being applied (a domain listed in several enabled sets resolves by config order)", set.Name, removed)

		if len(set.Targets.DomainsToMatch) == 0 && len(set.Targets.IpsToMatch) == 0 {
			log.Warnf("Set '%s' no longer targets any domain or IP", set.Name)
		}
	}

	return moved
}

const maxCheckDomains = 32

// parseCheckDomains normalises and de-duplicates the requested domains, capped
// at maxCheckDomains. The second return value reports whether the cap dropped
// anything, so callers can say so instead of answering for a shorter list than
// they were given.
func parseCheckDomains(raw string) ([]string, bool) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '|' || r == ';' || unicode.IsSpace(r)
	})

	seen := make(map[string]bool, len(fields))
	domains := make([]string, 0, len(fields))
	truncated := false
	for _, field := range fields {
		domain := sni.NormalizeDomain(field)
		if domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		if len(domains) >= maxCheckDomains {
			truncated = true
			break
		}
		domains = append(domains, domain)
	}

	return domains, truncated
}

// @Summary Check which sets match a domain
// @Tags Sets
// @Produce json
// @Param domain query string true "Domain to check, several separated by comma or whitespace"
// @Param exclude query string false "Set ID to exclude"
// @Success 200 {array} SetDomainMatch
// @Security BearerAuth
// @Router /sets/check-domain [get]
func (api *API) handleCheckDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	domains, _ := parseCheckDomains(r.URL.Query().Get("domain"))
	excludeId := r.URL.Query().Get("exclude")

	if len(domains) == 0 {
		writeJsonError(w, http.StatusBadRequest, "domain parameter required")
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(api.matchDomainsToSets(domains, excludeId))
}

func (api *API) matchDomainsToSets(domains []string, excludeId string) []SetDomainMatch {
	byDomain := make(map[string][]SetDomainMatch, len(domains))
	exactRank := sni.RelationExact.Priority()

	for _, set := range api.getCfg().Sets {
		if set.Id == excludeId {
			continue
		}

		best := make(map[string]SetDomainMatch, len(domains))
		ranks := make(map[string]int, len(domains))

		consider := func(entry, via string) {
			for _, domain := range domains {
				if ranks[domain] == exactRank {
					continue
				}
				relation, matched := sni.MatchDomainEntry(entry, domain)
				rank := relation.Priority()
				if rank <= ranks[domain] {
					continue
				}
				ranks[domain] = rank
				best[domain] = SetDomainMatch{
					Domain:   domain,
					SetName:  set.Name,
					SetId:    set.Id,
					Via:      via,
					Relation: string(relation),
					Entry:    matched,
					Enabled:  set.Enabled,
				}
			}
		}

		manual := make(map[string]bool, len(set.Targets.SNIDomains))
		for _, entry := range set.Targets.SNIDomains {
			if canonical := sni.CanonicalDomainEntry(entry); canonical != "" {
				manual[canonical] = true
			}
			consider(entry, "manual")
		}

		for _, entry := range set.Targets.DomainsToMatch {
			canonical := sni.CanonicalDomainEntry(entry)
			if canonical == "" || manual[canonical] {
				continue
			}
			consider(entry, "geosite")
		}

		for _, domain := range domains {
			if ranks[domain] > 0 {
				byDomain[domain] = append(byDomain[domain], best[domain])
			}
		}
	}

	matches := make([]SetDomainMatch, 0, len(domains))
	for _, domain := range domains {
		matches = append(matches, byDomain[domain]...)
	}
	return matches
}

// @Summary Add domain to a set
// @Tags Sets
// @Accept json
// @Produce json
// @Param id path string true "Set ID"
// @Param body body object true "Domain object"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /sets/{id}/add-domain [post]
func (api *API) handleSetDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()

	setId := r.PathValue("id")

	var req struct {
		Domain  string   `json:"domain"`
		Domains []string `json:"domains"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}

	domains := appendMissing(nil, req.Domains...)
	if d := strings.TrimSpace(req.Domain); d != "" {
		domains = appendMissing(domains, d)
	}
	if len(domains) == 0 {
		writeAPIError(w, ErrBadRequest("domain is required"))
		return
	}

	for _, set := range newCfg.Sets {
		if set.Id == setId {
			for _, domain := range domains {
				if domainInList(set.Targets.SNIDomains, domain) {
					continue
				}
				set.Targets.SNIDomains = append(set.Targets.SNIDomains, domain)
				set.Targets.DomainsToMatch = append(set.Targets.DomainsToMatch, domain)
			}

			moved := api.releaseDomainsFromOtherSets(newCfg.Sets, setId, domains)

			if err := api.saveAndPushConfig(newCfg); err != nil {
				writeAPIError(w, err)
				return
			}

			if api.PerformSoftRestart(newCfg, oldCfg) {
				log.Infof("Soft restart completed successfully")
			}

			setJsonHeader(w)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "moved": moved})
			return
		}
	}

	writeAPIError(w, ErrNotFound("Set not found"))
}

func domainInList(list []string, domain string) bool {
	for _, existing := range list {
		if strings.EqualFold(strings.TrimSpace(existing), domain) {
			return true
		}
	}
	return false
}

// GET /api/sets - list all, POST /api/sets - create new
func (api *API) handleSets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		api.listSets(w)
	case http.MethodPost:
		api.createSet(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// GET/PUT/DELETE /api/sets/{id}
func (api *API) handleSetById(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeAPIError(w, ErrBadRequest("Set ID required"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		api.getSet(w, id)
	case http.MethodPut:
		api.updateSet(w, r, id)
	case http.MethodDelete:
		api.deleteSet(w, id)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// @Summary List all sets
// @Tags Sets
// @Produce json
// @Success 200 {array} config.SetConfig
// @Security BearerAuth
// @Router /sets [get]
func (api *API) listSets(w http.ResponseWriter) {
	setJsonHeader(w)
	sets := api.getCfg().Sets
	if sets == nil {
		sets = []*config.SetConfig{}
	}
	json.NewEncoder(w).Encode(sets)
}

// @Summary Get a set by ID
// @Tags Sets
// @Produce json
// @Param id path string true "Set ID"
// @Success 200 {object} config.SetConfig
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /sets/{id} [get]
func (api *API) getSet(w http.ResponseWriter, id string) {
	set := api.getCfg().GetSetById(id)
	if set == nil {
		writeAPIError(w, ErrNotFound("Set not found"))
		return
	}
	setJsonHeader(w)
	json.NewEncoder(w).Encode(set)
}

// @Summary Create a new set
// @Tags Sets
// @Accept json
// @Produce json
// @Param set body config.SetConfig true "Set configuration"
// @Success 201 {object} config.SetConfig
// @Security BearerAuth
// @Router /sets [post]
func (api *API) createSet(w http.ResponseWriter, r *http.Request) {
	var set config.SetConfig
	if err := json.NewDecoder(r.Body).Decode(&set); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()

	set.Id = uuid.New().String()
	log.Tracef("createSet: routing before defaults: enabled=%v, egress=%s, ttl=%d", set.Routing.Enabled, set.Routing.EgressInterface, set.Routing.IPTTLSeconds)
	api.initializeSetDefaults(&set)
	log.Tracef("createSet: routing after defaults: enabled=%v, egress=%s, ttl=%d", set.Routing.Enabled, set.Routing.EgressInterface, set.Routing.IPTTLSeconds)

	newCfg.Sets = append([]*config.SetConfig{&set}, newCfg.Sets...)

	api.loadTargetsForSetCached(&set)

	if err := api.saveAndPushConfig(newCfg); err != nil {
		log.Errorf("Failed to save config after creating set: %v", err)
		writeAPIError(w, err)
		return
	}

	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}

	log.Tracef("Created set '%s' (id: %s)", set.Name, set.Id)
	setJsonHeader(w)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(set)
}

// @Summary Update a set
// @Tags Sets
// @Accept json
// @Produce json
// @Param id path string true "Set ID"
// @Param set body config.SetConfig true "Updated set configuration"
// @Success 200 {object} config.SetConfig
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /sets/{id} [put]
func (api *API) updateSet(w http.ResponseWriter, r *http.Request, id string) {
	var updated config.SetConfig
	if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}

	log.Tracef("updateSet: routing received: enabled=%v, egress=%s, ttl=%d", updated.Routing.Enabled, updated.Routing.EgressInterface, updated.Routing.IPTTLSeconds)

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()

	found := false
	for i, set := range newCfg.Sets {
		if set.Id == id {
			updated.Id = id // preserve ID
			newCfg.Sets[i] = &updated
			found = true
			break
		}
	}

	if !found {
		writeAPIError(w, ErrNotFound("Set not found"))
		return
	}

	api.loadTargetsForSetCached(&updated)

	if err := api.saveAndPushConfig(newCfg); err != nil {
		log.Errorf("Failed to save config after updating set: %v", err)
		writeAPIError(w, err)
		return
	}

	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}

	log.Infof("Updated set '%s' (id: %s)", updated.Name, id)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(updated)
}

// @Summary Delete a set
// @Tags Sets
// @Produce json
// @Param id path string true "Set ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /sets/{id} [delete]
func (api *API) deleteSet(w http.ResponseWriter, id string) {
	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()

	found := false
	filtered := make([]*config.SetConfig, 0, len(newCfg.Sets))
	for _, set := range newCfg.Sets {
		if set.Id == id {
			found = true
			continue
		}
		filtered = append(filtered, set)
	}

	if !found {
		writeAPIError(w, ErrNotFound("Set not found"))
		return
	}

	newCfg.Sets = filtered

	if err := api.saveAndPushConfig(newCfg); err != nil {
		log.Errorf("Failed to save config after deleting set: %v", err)
		writeAPIError(w, err)
		return
	}

	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}

	log.Infof("Deleted set (id: %s)", id)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// @Summary Reorder sets
// @Tags Sets
// @Accept json
// @Produce json
// @Param body body object true "Ordered set IDs"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /sets/reorder [post]
func (api *API) handleReorderSets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()

	var req struct {
		SetIds []string `json:"set_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}

	// Build new order
	setMap := make(map[string]*config.SetConfig)
	for _, set := range newCfg.Sets {
		setMap[set.Id] = set
	}

	reordered := make([]*config.SetConfig, 0, len(req.SetIds))
	for _, id := range req.SetIds {
		if set, ok := setMap[id]; ok {
			reordered = append(reordered, set)
		}
	}

	if len(reordered) != len(newCfg.Sets) {
		writeAPIError(w, ErrBadRequest("Invalid set IDs"))
		return
	}

	newCfg.Sets = reordered

	if err := api.saveAndPushConfig(newCfg); err != nil {
		writeAPIError(w, err)
		return
	}

	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (api *API) initializeSetDefaults(set *config.SetConfig) {
	if set.Targets.IPs == nil {
		set.Targets.IPs = []string{}
	}
	if set.Targets.SNIDomains == nil {
		set.Targets.SNIDomains = []string{}
	}
	if set.Targets.GeoSiteCategories == nil {
		set.Targets.GeoSiteCategories = []string{}
	}
	if set.Targets.GeoIpCategories == nil {
		set.Targets.GeoIpCategories = []string{}
	}
	if set.Targets.SourceDevices == nil {
		set.Targets.SourceDevices = []string{}
	}
	if set.TCP.Win.Values == nil {
		set.TCP.Win.Values = []int{0, 1460, 8192, 65535}
	}
	if set.Faking.SNIMutation.FakeSNIs == nil {
		set.Faking.SNIMutation.FakeSNIs = []string{}
	}
	if set.Routing.SourceInterfaces == nil {
		set.Routing.SourceInterfaces = []string{}
	}
	if set.Routing.IPTTLSeconds <= 0 {
		set.Routing.IPTTLSeconds = config.DefaultSetConfig.Routing.IPTTLSeconds
	}
}

func (api *API) retainGeoCaches(sets []*config.SetConfig) {
	if api.geodataManager == nil {
		return
	}

	geosite := []string{}
	geoip := []string{}
	for _, set := range sets {
		geosite = append(geosite, set.Targets.GeoSiteCategories...)
		geoip = append(geoip, set.Targets.GeoIpCategories...)
	}
	api.geodataManager.RetainCategories(geosite, geoip)
}

type targetExpansion struct {
	Domains      int
	IPs          int
	EmptyGeoSite []string
	EmptyGeoIP   []string
}

func (api *API) loadTargetsForSetCached(set *config.SetConfig) targetExpansion {
	domains := []string{}
	ips := []string{}
	var report targetExpansion

	for _, cat := range set.Targets.GeoSiteCategories {
		cached, err := api.geodataManager.LoadGeositeCategory(cat)
		if err != nil || len(cached) == 0 {
			report.EmptyGeoSite = append(report.EmptyGeoSite, cat)
			continue
		}
		domains = append(domains, cached...)
	}
	domains = append(domains, set.Targets.SNIDomains...)
	set.Targets.DomainsToMatch = domains

	for _, cat := range set.Targets.GeoIpCategories {
		cached, err := api.geodataManager.LoadGeoipCategory(cat)
		if err != nil || len(cached) == 0 {
			report.EmptyGeoIP = append(report.EmptyGeoIP, cat)
			continue
		}
		ips = append(ips, cached...)
	}
	ips = append(ips, set.Targets.IPs...)
	set.Targets.IpsToMatch = ips

	report.Domains = len(domains)
	report.IPs = len(ips)
	return report
}

// @Summary Batch delete sets
// @Tags Sets
// @Accept json
// @Produce json
// @Param body body object true "Set IDs to delete"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /sets/batch-delete [post]
func (api *API) handleBatchDeleteSets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Ids []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}

	if len(req.Ids) == 0 {
		writeAPIError(w, ErrBadRequest("No set IDs provided"))
		return
	}

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()

	toDelete := make(map[string]bool, len(req.Ids))
	for _, id := range req.Ids {
		toDelete[id] = true
	}

	filtered := make([]*config.SetConfig, 0, len(newCfg.Sets))
	for _, set := range newCfg.Sets {
		if !toDelete[set.Id] {
			filtered = append(filtered, set)
		}
	}

	deleted := len(newCfg.Sets) - len(filtered)
	if deleted == 0 {
		writeAPIError(w, ErrNotFound("No matching sets found"))
		return
	}

	newCfg.Sets = filtered

	if err := api.saveAndPushConfig(newCfg); err != nil {
		log.Errorf("Failed to save config after batch deleting sets: %v", err)
		writeAPIError(w, err)
		return
	}

	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}

	log.Infof("Batch deleted %d sets", deleted)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "deleted": deleted})
}

// @Summary Batch enable/disable sets
// @Tags Sets
// @Accept json
// @Produce json
// @Param body body object true "Set IDs and enabled flag"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /sets/batch-set-enabled [post]
func (api *API) handleBatchSetEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Ids     []string `json:"ids"`
		Enabled bool     `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}

	if len(req.Ids) == 0 {
		writeAPIError(w, ErrBadRequest("No set IDs provided"))
		return
	}

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()

	target := make(map[string]bool, len(req.Ids))
	for _, id := range req.Ids {
		target[id] = true
	}

	matched := 0
	updated := 0
	for _, set := range newCfg.Sets {
		if target[set.Id] {
			matched++
			if set.Enabled != req.Enabled {
				set.Enabled = req.Enabled
				updated++
			}
		}
	}

	if matched == 0 {
		writeAPIError(w, ErrNotFound("No matching sets found"))
		return
	}

	if updated == 0 {
		setJsonHeader(w)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "updated": 0})
		return
	}

	if err := api.saveAndPushConfig(newCfg); err != nil {
		log.Errorf("Failed to save config after batch toggling sets: %v", err)
		writeAPIError(w, err)
		return
	}

	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}

	log.Infof("Batch set enabled=%v for %d sets", req.Enabled, updated)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "updated": updated})
}
