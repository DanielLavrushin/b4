package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
)

func (api *API) RegisterGeositeApi() {
	api.mux.HandleFunc("/api/geosite", api.handleGeoSite)
	api.mux.HandleFunc("/api/geosite/category", api.previewGeoCategory)
	api.mux.HandleFunc("/api/geosite/domain", api.addGeositeDomain)
}

func (a *API) handleGeoSite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getGeositeTags(w)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// @Summary Add domain to a set via geosite
// @Tags Geosite
// @Accept json
// @Produce json
// @Param body body AddDomainRequest true "Domain to add"
// @Success 200 {object} AddDomainResponse
// @Security BearerAuth
// @Router /geosite/domain [put]
func (a *API) addGeositeDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req AddDomainRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		log.Errorf("Failed to decode add domain request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	req.Domain = normalizeDomain(req.Domain)

	if req.SetId == "" {
		req.SetId = config.CreateSetSentinel
	}

	newCfg := a.getCfg().Clone()
	set := newCfg.GetSetById(req.SetId)

	if set == nil && req.SetId == config.CreateSetSentinel {
		newSet := config.DefaultSetConfig
		set = &newSet
		set.Id = uuid.New().String()

		if req.SetName != "" {
			set.Name = req.SetName
		} else {
			set.Name = "Set " + fmt.Sprintf("%d", len(newCfg.Sets)+1)
		}

		newCfg.Sets = append([]*config.SetConfig{set}, newCfg.Sets...)
	}

	if set == nil {
		http.Error(w, fmt.Sprintf("Set with ID '%s' not found", req.SetId), http.StatusBadRequest)
		return
	}

	if req.Domain == "" {
		http.Error(w, "Domain cannot be empty", http.StatusBadRequest)
		return
	}

	err := set.Targets.AppendSNI(req.Domain)
	if err != nil {
		log.Errorf("Failed to add domain '%s' to set '%s': %v", req.Domain, set.Id, err)
		http.Error(w, "Failed to add domain: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Infof("Added domain '%s' to set '%s' domains list", req.Domain, set.Id)

	err = a.saveAndPushConfig(newCfg)

	if err != nil {
		log.Errorf("Failed to apply domain changes after adding domain: %v", err)
		http.Error(w, "Failed to apply domain changes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := AddDomainResponse{
		Success:       true,
		Message:       fmt.Sprintf("Successfully added domain '%s'", req.Domain),
		Domain:        req.Domain,
		TotalDomains:  len(set.Targets.DomainsToMatch),
		ManualDomains: set.Targets.SNIDomains,
	}

	setJsonHeader(w)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// @Summary List geosite categories
// @Tags Geosite
// @Produce json
// @Success 200 {object} GeositeResponse
// @Security BearerAuth
// @Router /geosite [get]
func (a *API) getGeositeTags(w http.ResponseWriter) {
	setJsonHeader(w)
	enc := json.NewEncoder(w)

	if !a.geodataManager.IsGeositeConfigured() {
		log.Tracef("Geosite path is not configured")
		_ = enc.Encode(GeositeResponse{Tags: []string{}})
		return
	}

	tags, err := a.geodataManager.ListCategories(a.geodataManager.GetGeositePath())
	if err != nil {
		http.Error(w, "Failed to load geosite tags: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := GeositeResponse{
		Tags: tags,
	}

	_ = enc.Encode(response)
}

// @Summary Preview geosite category domains
// @Tags Geosite
// @Produce json
// @Param tag query string true "Category tag name"
// @Success 200 {object} object
// @Security BearerAuth
// @Router /geosite/category [get]
func (a *API) previewGeoCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	category := r.URL.Query().Get("tag")
	if category == "" {
		http.Error(w, "Tag category parameter required", http.StatusBadRequest)
		return
	}

	if !a.geodataManager.IsGeositeConfigured() {
		http.Error(w, "Geosite path not configured", http.StatusBadRequest)
		return
	}

	domains, err := a.geodataManager.LoadGeositeCategory(category)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load category: %v", err), http.StatusInternalServerError)
		return
	}

	previewLimit := 100
	preview := domains
	if len(domains) > previewLimit {
		preview = domains[:previewLimit]
	}

	response := map[string]interface{}{
		"category":      category,
		"total_domains": len(domains),
		"preview_count": len(preview),
		"preview":       preview,
	}

	setJsonHeader(w)
	enc := json.NewEncoder(w)
	_ = enc.Encode(response)
}

func normalizeDomain(domain string) string {
	domain = strings.TrimSpace(domain)              // Remove surrounding whitespace
	domain = strings.ToLower(domain)                // Normalize case
	domain = strings.TrimPrefix(domain, "http://")  // Remove scheme
	domain = strings.TrimPrefix(domain, "https://") // Remove scheme
	domain = strings.SplitN(domain, "/", 2)[0]      // Remove path
	domain = strings.SplitN(domain, ":", 2)[0]      // Remove port
	return domain
}
