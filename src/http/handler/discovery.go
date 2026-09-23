package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"github.com/google/uuid"
	"golang.org/x/net/publicsuffix"
)

func (api *API) RegisterDiscoveryApi() {
	api.mux.HandleFunc("/api/discovery/start", api.handleStartDiscovery)
	api.mux.HandleFunc("/api/discovery/status/{id}", api.handleCheckStatus)
	api.mux.HandleFunc("/api/discovery/cancel/{id}", api.handleCancelCheck)
	api.mux.HandleFunc("/api/discovery/finish/{id}", api.handleFinishCheck)
	api.mux.HandleFunc("/api/discovery/add", api.handleAddPresetAsSet)
	api.mux.HandleFunc("/api/discovery/replace", api.handleReplaceStrategy)
	api.mux.HandleFunc("/api/discovery/similar", api.handleFindSimilarSets)
	api.mux.HandleFunc("/api/discovery/cache/clear", api.handleClearDiscoveryCache)
	api.mux.HandleFunc("/api/discovery/current", api.handleGetCurrentDiscovery)
	api.mux.HandleFunc("/api/discovery/history", api.handleDiscoveryHistory)
	api.mux.HandleFunc("/api/discovery/history/clear", api.handleClearDiscoveryHistory)
	api.mux.HandleFunc("/api/discovery/history/applied", api.handleMarkHistoryApplied)
	api.mux.HandleFunc("/api/discovery/history/{domain}", api.handleDeleteHistoryDomain)
	api.mux.HandleFunc("/api/discovery/log", api.handleDiscoveryLog)
}

// @Summary Get the log of the running or last discovery run
// @Tags Discovery
// @Produce plain
// @Param download query bool false "Send as an attachment"
// @Success 200 {string} string
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /discovery/log [get]
func (api *API) handleDiscoveryLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var body []byte
	if api.discoveryRT != nil && api.discoveryRT.IsActive() {
		body = []byte(strings.Join(log.GetDiscoveryHub().Snapshot(), "\n"))
	} else {
		saved, err := discovery.LoadLastRunLog(api.getCfg().ConfigPath)
		if err != nil {
			if live := log.GetDiscoveryHub().Snapshot(); len(live) > 0 {
				saved = []byte(strings.Join(live, "\n"))
			} else {
				http.Error(w, "no discovery run has been logged yet", http.StatusNotFound)
				return
			}
		}
		body = saved
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.URL.Query().Get("download") != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="b4-discovery.log"`)
	}
	w.Write(body)
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

	api.writeSuite(w, suite)
}

func (api *API) writeSuite(w http.ResponseWriter, suite *discovery.CheckSuite) {
	data, err := json.Marshal(suite)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if api.discoveryRT != nil && api.discoveryRT.IsActive() && len(data) > 1 && data[len(data)-1] == '}' {
		data = append(data[:len(data)-1], []byte(`,"runtime_active":true}`)...)
	}
	setJsonHeader(w)
	w.Write(data)
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

// @Summary Stop the search and confirm what was found
// @Tags Discovery
// @Produce json
// @Param id path string true "Suite ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /discovery/finish/{id} [post]
func (api *API) handleFinishCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	testID := r.PathValue("id")
	if testID == "" {
		http.Error(w, "Check ID required", http.StatusBadRequest)
		return
	}

	if err := discovery.FinishCheckSuite(testID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	log.Infof("Finishing test suite %s early, confirming found strategies", testID)

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Check suite finishing",
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
		Source:          discovery.SourceWeb,
		HubPresets:      func() []discovery.ConfigPreset { return api.communityPresets(urls, req.SkipCommunity) },
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

	set.Targets.GeoIpCategories, set.Targets.GeoSiteCategories = cdnCategoriesFor(set.Targets.SNIDomains)

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

	if len(set.Targets.GeoSiteCategories) > 0 && !api.geodataManager.IsGeositeConfigured() {
		log.Warnf("Set '%s': dropping geosite categories %v, no geosite database is installed", set.Name, set.Targets.GeoSiteCategories)
		set.Targets.GeoSiteCategories = []string{}
	}
	if len(set.Targets.GeoIpCategories) > 0 && !api.geodataManager.IsGeoipConfigured() {
		log.Warnf("Set '%s': dropping geoip categories %v, no geoip database is installed", set.Name, set.Targets.GeoIpCategories)
		set.Targets.GeoIpCategories = []string{}
	}

	set.Targets.IPs = nil

	api.loadTargetsForSetCached(&set)
	config.ApplySetDefaults(&set)

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()

	moved := api.releaseDomainsFromOtherSets(newCfg.Sets, set.Id, set.Targets.SNIDomains)

	newCfg.Sets = append([]*config.SetConfig{&set}, newCfg.Sets...)

	// Save configuration
	if err := api.saveAndPushConfig(newCfg); err != nil {
		log.Errorf("Failed to save config: %v", err)
		writeAPIError(w, err)
		return
	}

	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}

	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Added '%s' configuration", set.Name),
		"moved":   moved,
		"id":      set.Id,
		"name":    set.Name,
	})
}

// @Summary Replace the strategy of an existing set with a discovered one
// @Tags Discovery
// @Accept json
// @Produce json
// @Param body body DiscoveryReplaceRequest true "Target set, discovered strategy, domains and pins"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /discovery/replace [post]
func (api *API) handleReplaceStrategy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	req := DiscoveryReplaceRequest{Set: config.NewSetConfig()}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	domains := make([]string, 0, len(req.Domains))
	for _, raw := range req.Domains {
		if d := strings.TrimSpace(raw); d != "" && !domainInList(domains, d) {
			domains = append(domains, d)
		}
	}
	if req.SetId == "" || len(domains) == 0 {
		http.Error(w, "set_id and domains are required", http.StatusBadRequest)
		return
	}
	config.ApplySetDefaults(&req.Set)

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()

	var target *config.SetConfig
	for _, set := range newCfg.Sets {
		if set != nil && set.Id == req.SetId {
			target = set
			break
		}
	}
	if target == nil {
		writeAPIError(w, ErrNotFound("Set not found"))
		return
	}

	replaceStrategy(target, &req.Set)
	replacePins(target, domains, req.Pins)
	addSNIDomains(target, domains)
	moved := api.releaseDomainsFromOtherSets(newCfg.Sets, target.Id, domains)

	if err := api.saveAndPushConfig(newCfg); err != nil {
		log.Errorf("Failed to save config: %v", err)
		writeAPIError(w, err)
		return
	}
	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}
	log.Infof("Replaced the strategy of set '%s' with a discovered one for %s", target.Name, strings.Join(domains, ", "))

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"moved":   moved,
		"id":      target.Id,
		"name":    target.Name,
	})
}

func replaceStrategy(dst, strategy *config.SetConfig) {
	tcp := strategy.TCP
	tcp.DPortFilter = dst.TCP.DPortFilter
	tcp.RSTProtection = dst.TCP.RSTProtection
	if !strategy.TCP.IPBlockDetect.Enabled {
		tcp.IPBlockDetect = dst.TCP.IPBlockDetect
	}
	udp := strategy.UDP
	udp.DPortFilter = dst.UDP.DPortFilter

	pins := dst.DNS.Pins
	dst.TCP = tcp
	dst.UDP = udp
	dst.Fragmentation = strategy.Fragmentation
	dst.Faking = strategy.Faking
	dst.DNS = strategy.DNS
	dst.DNS.Pins = pins
	dst.Targets.TLSVersion = strategy.Targets.TLSVersion
	dst.Targets.IPVersion = strategy.Targets.IPVersion
}

func replacePins(set *config.SetConfig, domains []string, pins map[string][]string) {
	applied := make(map[string]bool, len(domains))
	for _, domain := range domains {
		if normalized := config.NormalizePinDomain(domain); normalized != "" {
			applied[normalized] = true
		}
	}
	for pin := range set.DNS.Pins {
		if applied[config.NormalizePinDomain(pin)] {
			delete(set.DNS.Pins, pin)
		}
	}
	mergePins(set, pins)
}

func cdnCategoriesFor(domains []string) (geoip, geosite []string) {
	for _, domain := range domains {
		ip, site := discovery.GetCDNCategories(domain)
		geoip = appendMissing(geoip, ip...)
		geosite = appendMissing(geosite, site...)
	}
	return geoip, geosite
}

func appendMissing(list []string, items ...string) []string {
	for _, item := range items {
		found := false
		for _, existing := range list {
			if existing == item {
				found = true
				break
			}
		}
		if !found {
			list = append(list, item)
		}
	}
	return list
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

func (api *API) setCoveringDomainWith(domain string, strategy *config.SetConfig) *config.SetConfig {
	if strategy == nil || domain == "" {
		return nil
	}
	candidate := *strategy
	api.initializeSetDefaults(&candidate)

	for _, set := range api.getCfg().Sets {
		if set == nil || !set.Enabled || !setsHaveSimilarConfig(set, &candidate) {
			continue
		}
		if len(set.Targets.SourceDevices) > 0 && !set.Targets.SourceDevicesExclude {
			continue
		}
		if setCoversDomain(set, domain) {
			return set
		}
	}
	return nil
}

func setCoversDomain(set *config.SetConfig, domain string) bool {
	for _, entries := range [][]string{set.Targets.SNIDomains, set.Targets.DomainsToMatch} {
		for _, entry := range entries {
			switch relation, _ := sni.MatchDomainEntry(entry, domain); relation {
			case sni.RelationExact, sni.RelationCovered, sni.RelationRegexp:
				return true
			}
		}
	}
	return false
}

func setsHaveSimilarConfig(a, b *config.SetConfig) bool {
	return reflect.DeepEqual(strategyShape(a), strategyShape(b))
}

func strategyShape(set *config.SetConfig) config.SetConfig {
	shape := config.SetConfig{
		TCP:           set.TCP,
		UDP:           set.UDP,
		Fragmentation: set.Fragmentation,
		Faking:        set.Faking,
		DNS: config.DNSConfig{
			Enabled:       set.DNS.Enabled,
			TargetDNS:     set.DNS.TargetDNS,
			DoHURL:        set.DNS.DoHURL,
			FragmentQuery: set.DNS.FragmentQuery,
		},
		Routing: config.RoutingConfig{
			Enabled: set.Routing.Enabled,
			Mode:    set.Routing.Mode,
		},
	}
	shape.Targets.TLSVersion = set.Targets.TLSVersion
	shape.Targets.IPVersion = set.Targets.IPVersion
	shape.TCP.DPortFilter = ""
	shape.TCP.IPBlockDetect = config.IPBlockDetectConfig{}
	shape.TCP.RSTProtection = config.RSTProtectionConfig{}
	shape.UDP.DPortFilter = ""
	config.ApplySetDefaults(&shape)
	shape.Faking.PayloadData = nil
	shape.UDP.FakePayloadData = nil
	return shape
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
		if api.discoveryRT != nil && api.discoveryRT.IsActive() {
			w.Write([]byte(`{"runtime_active":true}`))
			return
		}
		w.Write([]byte("null"))
		return
	}

	api.writeSuite(w, suite)
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
	views := make([]HistoryEntryView, 0, len(history.Entries))
	for _, entry := range history.Entries {
		views = append(views, HistoryEntryView{HistoryEntry: entry, SizeBytes: entry.StorageBytes()})
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(views)
}

// @Summary Mark a discovered strategy as applied
// @Tags Discovery
// @Accept json
// @Produce json
// @Param body body HistoryAppliedRequest true "Domains and the preset that was installed"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /discovery/history/applied [post]
func (api *API) handleMarkHistoryApplied(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req HistoryAppliedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Preset == "" || len(req.Domains) == 0 {
		http.Error(w, "Domains and preset required", http.StatusBadRequest)
		return
	}

	if err := discovery.MarkAppliedInHistory(api.getCfg().ConfigPath, req.Domains, req.Preset, req.SetId); err != nil {
		log.Errorf("Failed to save discovery history: %v", err)
		http.Error(w, "Failed to save discovery history", http.StatusInternalServerError)
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Marked %s as applied", req.Preset),
	})
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

	err := discovery.UpdateHistory(api.getCfg().ConfigPath, func(history *discovery.DiscoveryHistory) bool {
		history.Clear()
		return true
	})
	if err != nil {
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

	err := discovery.UpdateHistory(api.getCfg().ConfigPath, func(history *discovery.DiscoveryHistory) bool {
		history.RemoveDomain(domain)
		return true
	})
	if err != nil {
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
