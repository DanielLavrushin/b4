package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
	"golang.org/x/net/publicsuffix"
)

func (api *API) RegisterDiscoveryApi() {
	api.mux.HandleFunc("/api/discovery/start", api.handleStartDiscovery)
	api.mux.HandleFunc("/api/discovery/status/{id}", api.handleCheckStatus)
	api.mux.HandleFunc("/api/discovery/cancel/{id}", api.handleCancelCheck)
	api.mux.HandleFunc("/api/discovery/add", api.handleAddPresetAsSet)
	api.mux.HandleFunc("/api/discovery/similar", api.handleFindSimilarSets)
	api.mux.HandleFunc("/api/discovery/cache/clear", api.handleClearDiscoveryCache)
	api.mux.HandleFunc("/api/discovery/current", api.handleGetCurrentDiscovery)
	api.mux.HandleFunc("/api/discovery/history", api.handleDiscoveryHistory)
	api.mux.HandleFunc("/api/discovery/history/clear", api.handleClearDiscoveryHistory)
	api.mux.HandleFunc("/api/discovery/history/{domain}", api.handleDeleteHistoryDomain)
}

// @Summary Get discovery status
// @Tags Discovery
// @Produce json
// @Param id path string true "Suite ID"
// @Success 200 {object} object
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /discovery/status/{id} [get]
func (api *API) handleCheckStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	testID := r.PathValue("id")
	if testID == "" {
		http.Error(w, "Check ID required", http.StatusBadRequest)
		return
	}

	suite, ok := discovery.GetCheckSuite(testID)
	if !ok {
		http.Error(w, "Check suite not found", http.StatusNotFound)
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(suite)
}

// @Summary Cancel discovery
// @Tags Discovery
// @Produce json
// @Param id path string true "Suite ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /discovery/cancel/{id} [delete]
func (api *API) handleCancelCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	testID := r.PathValue("id")
	if testID == "" {
		http.Error(w, "Check ID required", http.StatusBadRequest)
		return
	}

	if err := discovery.CancelCheckSuite(testID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	log.Infof("Canceled test suite %s", testID)

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Check suite canceled",
	})
}

// @Summary Start domain discovery
// @Tags Discovery
// @Accept json
// @Produce json
// @Param body body DiscoveryRequest true "Discovery request"
// @Success 202 {object} DiscoveryResponse
// @Security BearerAuth
// @Router /discovery/start [post]
func (api *API) handleStartDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req DiscoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Errorf("Failed to decode discovery request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Normalize input: support both single and multi URL
	var urls []string
	if len(req.CheckURLs) > 0 {
		for _, u := range req.CheckURLs {
			u = strings.TrimSpace(u)
			if u != "" {
				urls = append(urls, u)
			}
		}
	} else if req.CheckURL != "" {
		urls = []string{req.CheckURL}
	}

	if len(urls) == 0 {
		http.Error(w, "check_url or check_urls is required", http.StatusBadRequest)
		return
	}

	// Use ValidationTries from request, or default to 1 if not provided
	validationTries := req.ValidationTries
	if validationTries < 1 {
		validationTries = 1
	}

	if api.discoveryRT == nil {
		http.Error(w, "discovery runtime is not configured", http.StatusInternalServerError)
		return
	}

	suite, err := api.discoveryRT.StartSuite(api.getCfg(), urls, discovery.StartSuiteOptions{
		SkipDNS:         req.SkipDNS,
		SkipCache:       req.SkipCache,
		PayloadFiles:    req.PayloadFiles,
		ValidationTries: validationTries,
		TLSVersion:      req.TLSVersion,
		IPVersion:       req.IPVersion,
	})
	if err != nil {
		if errors.Is(err, discovery.ErrDiscoveryAlreadyRunning) {
			http.Error(w, err.Error(), http.StatusConflict)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	phase1Count := len(discovery.GetPhase1Presets())

	var domainNames []string
	for _, di := range suite.Domains {
		domainNames = append(domainNames, di.Domain)
	}

	response := DiscoveryResponse{
		Id:             suite.Id,
		Domain:         suite.Domain,
		Domains:        domainNames,
		CheckURL:       suite.CheckURL,
		EstimatedTests: (phase1Count + 15) * len(suite.Domains),
		Message:        fmt.Sprintf("Discovery started for %d domains", len(urls)),
	}

	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(response)
}

// @Summary Add discovery preset as a new set
// @Tags Discovery
// @Accept json
// @Produce json
// @Param body body config.SetConfig true "Set configuration"
// @Success 202 {object} map[string]interface{}
// @Security BearerAuth
// @Router /discovery/add [post]
func (api *API) handleAddPresetAsSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var set = config.NewSetConfig()

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&set); err != nil {
		log.Errorf("Failed to decode config update: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	set.Id = uuid.New().String()

	if len(set.Targets.SNIDomains) == 0 {
		log.Errorf("At least one SNI domain is required")
		http.Error(w, "At least one SNI domain is required", http.StatusBadRequest)
		return
	}
	if set.Name == "" {
		set.Name = set.Targets.SNIDomains[0]
	}

	if len(set.Targets.SNIDomains) > 0 {
		baseName := extractDomainName(set.Targets.SNIDomains[0])
		if baseName != "" && api.geodataManager.IsGeositeConfigured() {
			// Check if category already exists in the set
			alreadyHasCategory := false
			for _, cat := range set.Targets.GeoSiteCategories {
				if cat == baseName {
					alreadyHasCategory = true
					break
				}
			}

			// Only add if not already present
			if !alreadyHasCategory {
				tags, err := api.geodataManager.ListCategories(api.geodataManager.GetGeositePath())
				if err == nil {
					for _, tag := range tags {
						if tag == baseName {
							set.Targets.GeoSiteCategories = append(set.Targets.GeoSiteCategories, baseName)
							log.Infof("Auto-added geosite category '%s' for domain %s", baseName, set.Targets.SNIDomains[0])
							break
						}
					}
				}
			}
		}
	}

	set.Targets.IPs = nil

	api.loadTargetsForSetCached(&set)
	config.ApplySetDefaults(&set)

	api.getCfg().Sets = append([]*config.SetConfig{&set}, api.getCfg().Sets...)

	// Save configuration
	if err := api.saveAndPushConfig(api.getCfg()); err != nil {
		log.Errorf("Failed to save config: %v", err)
		http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
		return
	}

	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Added '%s' configuration", set.Name),
	})
}

// @Summary Find sets with similar configuration
// @Tags Discovery
// @Accept json
// @Produce json
// @Param body body config.SetConfig true "Set to compare"
// @Success 200 {array} object
// @Security BearerAuth
// @Router /discovery/similar [post]
func (api *API) handleFindSimilarSets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var incoming config.SetConfig
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	type SimilarSet struct {
		Id      string   `json:"id"`
		Name    string   `json:"name"`
		Domains []string `json:"domains"`
	}

	var similar []SimilarSet

	for _, set := range api.getCfg().Sets {
		if !set.Enabled {
			continue
		}
		if setsHaveSimilarConfig(set, &incoming) {
			similar = append(similar, SimilarSet{
				Id:      set.Id,
				Name:    set.Name,
				Domains: set.Targets.SNIDomains,
			})
		}
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(similar)
}

func setsHaveSimilarConfig(a, b *config.SetConfig) bool {
	return a.Fragmentation.Strategy == b.Fragmentation.Strategy &&
		a.Fragmentation.ReverseOrder == b.Fragmentation.ReverseOrder &&
		a.Fragmentation.MiddleSNI == b.Fragmentation.MiddleSNI &&
		a.Faking.Strategy == b.Faking.Strategy &&
		a.Faking.TTL == b.Faking.TTL &&
		a.Faking.SNI == b.Faking.SNI &&
		a.TCP.DropSACK == b.TCP.DropSACK
}

func extractDomainName(domain string) string {
	domain = strings.TrimPrefix(domain, "www.")

	registered, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		parts := strings.Split(domain, ".")
		if len(parts) > 0 {
			return strings.ToLower(parts[0])
		}
		return ""
	}

	parts := strings.Split(registered, ".")
	if len(parts) > 0 {
		return strings.ToLower(parts[0])
	}
	return ""
}

// @Summary Get current running discovery
// @Tags Discovery
// @Produce json
// @Success 200 {object} object
// @Security BearerAuth
// @Router /discovery/current [get]
func (api *API) handleGetCurrentDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	suite, ok := discovery.GetCurrentSuite()
	if !ok {
		setJsonHeader(w)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("null"))
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(suite)
}

// @Summary Get discovery history
// @Tags Discovery
// @Produce json
// @Success 200 {array} object
// @Security BearerAuth
// @Router /discovery/history [get]
func (api *API) handleDiscoveryHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	history := discovery.GetHistory(api.getCfg().ConfigPath)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(history.Entries)
}

// @Summary Clear discovery history
// @Tags Discovery
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /discovery/history/clear [post]
func (api *API) handleClearDiscoveryHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	history := discovery.LoadDiscoveryHistory(api.getCfg().ConfigPath)
	history.Clear()
	if err := history.Save(api.getCfg().ConfigPath); err != nil {
		log.Errorf("Failed to clear discovery history: %v", err)
		http.Error(w, "Failed to clear discovery history", http.StatusInternalServerError)
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Discovery history cleared",
	})
}

// @Summary Delete discovery history entry
// @Tags Discovery
// @Produce json
// @Param domain path string true "Domain name"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /discovery/history/{domain} [delete]
func (api *API) handleDeleteHistoryDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	domain := r.PathValue("domain")
	if domain == "" {
		http.Error(w, "Domain required", http.StatusBadRequest)
		return
	}

	history := discovery.LoadDiscoveryHistory(api.getCfg().ConfigPath)
	history.RemoveDomain(domain)
	if err := history.Save(api.getCfg().ConfigPath); err != nil {
		log.Errorf("Failed to save discovery history: %v", err)
		http.Error(w, "Failed to save discovery history", http.StatusInternalServerError)
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Removed history for %s", domain),
	})
}

// @Summary Clear discovery cache
// @Tags Discovery
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /discovery/cache/clear [post]
func (api *API) handleClearDiscoveryCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cache := discovery.LoadDiscoveryCache(api.getCfg().ConfigPath)
	cache.Entries = nil
	if err := cache.Save(api.getCfg().ConfigPath); err != nil {
		log.Errorf("Failed to clear discovery cache: %v", err)
		http.Error(w, "Failed to clear discovery cache", http.StatusInternalServerError)
		return
	}

	log.Infof("Discovery cache cleared")

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Discovery cache cleared",
	})
}
