package handler

import (
	"encoding/json"
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
}

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

	if req.CheckURL == "" {
		http.Error(w, "Check URL is required", http.StatusBadRequest)
		return
	}

	suite := discovery.NewDiscoverySuite(req.CheckURL, globalPool, req.SkipDNS, req.PayloadFiles)

	phase1Count := len(discovery.GetPhase1Presets())

	go func() {
		suite.RunDiscovery()
		log.Infof("Discovery complete for %s", suite.Domain)
	}()

	response := DiscoveryResponse{
		Id:             suite.Id,
		Domain:         suite.Domain,
		CheckURL:       suite.CheckURL,
		EstimatedTests: phase1Count + 15,
		Message:        fmt.Sprintf("Discovery started for %s", suite.Domain),
	}

	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(response)
}

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

	api.loadTargetsForSetCached(&set)
	config.ApplySetDefaults(&set)

	api.cfg.Sets = append([]*config.SetConfig{&set}, api.cfg.Sets...)

	if api.cfg.MainSet == nil {
		api.cfg.MainSet = &set
	}

	// Save configuration
	if err := api.saveAndPushConfig(api.cfg); err != nil {
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

	for _, set := range api.cfg.Sets {
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
