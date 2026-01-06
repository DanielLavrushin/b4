package handler

import (
	"encoding/json"
	"net/http"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
)

func (api *API) RegisterSetsApi() {
	api.mux.HandleFunc("/api/sets", api.handleSets)
	api.mux.HandleFunc("/api/sets/{id}", api.handleSetById)
	api.mux.HandleFunc("/api/sets/reorder", api.handleReorderSets)
	api.mux.HandleFunc("/api/sets/{id}/add-domain", api.handleSetDomains)
}

func (api *API) handleSetDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	oldConfig := api.cfg.Clone()

	setId := r.PathValue("id")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Find set and add domain
	for _, set := range api.cfg.Sets {
		if set.Id == setId {
			set.Targets.SNIDomains = append(set.Targets.SNIDomains, req.Domain)
			set.Targets.DomainsToMatch = append(set.Targets.DomainsToMatch, req.Domain)

			if err := api.saveAndPushConfig(api.cfg); err != nil {
				http.Error(w, "Failed to save", http.StatusInternalServerError)
				return
			}

			if api.PerformSoftRestart(api.cfg, oldConfig) {
				log.Infof("Soft restart completed successfully")
			}

			setJsonHeader(w)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
			return
		}
	}

	http.Error(w, "Set not found", http.StatusNotFound)
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
		http.Error(w, "Set ID required", http.StatusBadRequest)
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

func (api *API) listSets(w http.ResponseWriter) {
	setJsonHeader(w)
	json.NewEncoder(w).Encode(api.cfg.Sets)
}

func (api *API) getSet(w http.ResponseWriter, id string) {
	set := api.cfg.GetSetById(id)
	if set == nil {
		http.Error(w, "Set not found", http.StatusNotFound)
		return
	}
	setJsonHeader(w)
	json.NewEncoder(w).Encode(set)
}

func (api *API) createSet(w http.ResponseWriter, r *http.Request) {
	var set config.SetConfig
	if err := json.NewDecoder(r.Body).Decode(&set); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	oldConfig := api.cfg.Clone()

	set.Id = uuid.New().String()
	api.initializeSetDefaults(&set)

	api.cfg.Sets = append([]*config.SetConfig{&set}, api.cfg.Sets...)

	api.loadTargetsForSetCached(&set)

	if err := api.saveAndPushConfig(api.cfg); err != nil {
		log.Errorf("Failed to save config after creating set: %v", err)
		http.Error(w, "Failed to save", http.StatusInternalServerError)
		return
	}

	if api.PerformSoftRestart(api.cfg, oldConfig) {
		log.Infof("Soft restart completed successfully")
	}

	log.Infof("Created set '%s' (id: %s)", set.Name, set.Id)
	setJsonHeader(w)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(set)
}

func (api *API) updateSet(w http.ResponseWriter, r *http.Request, id string) {
	var updated config.SetConfig
	if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	oldConfig := api.cfg.Clone()

	found := false
	for i, set := range api.cfg.Sets {
		if set.Id == id {
			updated.Id = id // preserve ID
			api.cfg.Sets[i] = &updated
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "Set not found", http.StatusNotFound)
		return
	}

	api.loadTargetsForSetCached(&updated)

	if err := api.saveAndPushConfig(api.cfg); err != nil {
		log.Errorf("Failed to save config after updating set: %v", err)
		http.Error(w, "Failed to save", http.StatusInternalServerError)
		return
	}

	if api.PerformSoftRestart(api.cfg, oldConfig) {
		log.Infof("Soft restart completed successfully")
	}

	log.Infof("Updated set '%s' (id: %s)", updated.Name, id)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(updated)
}

func (api *API) deleteSet(w http.ResponseWriter, id string) {
	if id == config.MAIN_SET_ID {
		http.Error(w, "Cannot delete main set", http.StatusForbidden)
		return
	}

	oldConfig := api.cfg

	found := false
	filtered := make([]*config.SetConfig, 0, len(api.cfg.Sets))
	for _, set := range api.cfg.Sets {
		if set.Id == id {
			found = true
			continue
		}
		filtered = append(filtered, set)
	}

	if !found {
		http.Error(w, "Set not found", http.StatusNotFound)
		return
	}

	api.cfg.Sets = filtered

	if err := api.saveAndPushConfig(api.cfg); err != nil {
		log.Errorf("Failed to save config after deleting set: %v", err)
		http.Error(w, "Failed to save", http.StatusInternalServerError)
		return
	}

	if api.PerformSoftRestart(api.cfg, oldConfig) {
		log.Infof("Soft restart completed successfully")
	}

	log.Infof("Deleted set (id: %s)", id)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (api *API) handleReorderSets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	oldConfig := api.cfg.Clone()

	var req struct {
		SetIds []string `json:"set_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Build new order
	setMap := make(map[string]*config.SetConfig)
	for _, set := range api.cfg.Sets {
		setMap[set.Id] = set
	}

	reordered := make([]*config.SetConfig, 0, len(req.SetIds))
	for _, id := range req.SetIds {
		if set, ok := setMap[id]; ok {
			reordered = append(reordered, set)
		}
	}

	if len(reordered) != len(api.cfg.Sets) {
		http.Error(w, "Invalid set IDs", http.StatusBadRequest)
		return
	}

	api.cfg.Sets = reordered

	if err := api.saveAndPushConfig(api.cfg); err != nil {
		http.Error(w, "Failed to save", http.StatusInternalServerError)
		return
	}

	if api.PerformSoftRestart(api.cfg, oldConfig) {
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
	if set.TCP.Win.Values == nil {
		set.TCP.Win.Values = []int{0, 1460, 8192, 65535}
	}
	if set.Faking.SNIMutation.FakeSNIs == nil {
		set.Faking.SNIMutation.FakeSNIs = []string{}
	}
}

func (api *API) loadTargetsForSetCached(set *config.SetConfig) {
	domains := []string{}
	ips := []string{}

	for _, cat := range set.Targets.GeoSiteCategories {
		if cached, err := api.geodataManager.LoadGeositeCategory(cat); err == nil {
			domains = append(domains, cached...)
		}
	}
	domains = append(domains, set.Targets.SNIDomains...)
	set.Targets.DomainsToMatch = domains

	for _, cat := range set.Targets.GeoIpCategories {
		if cached, err := api.geodataManager.LoadGeoipCategory(cat); err == nil {
			ips = append(ips, cached...)
		}
	}
	ips = append(ips, set.Targets.IPs...)
	set.Targets.IpsToMatch = ips
}
