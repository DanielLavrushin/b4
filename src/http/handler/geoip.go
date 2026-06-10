package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
)

type AddGeoIpRequest struct {
	Cidr    []string `json:"cidr"`
	SetId   string   `json:"set_id,omitempty"`
	SetName string   `json:"set_name,omitempty"`
}

type AddIpResponse struct {
	Success     bool     `json:"success"`
	Message     string   `json:"message"`
	TotalCidrs  int      `json:"total_cidrs"`
	ManualCidrs []string `json:"manual_cidrs,omitempty"`
}

func (api *API) RegisterGeoipApi() {
	api.mux.HandleFunc("/api/geoip", api.handleGeoIp)
}

func (a *API) handleGeoIp(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getGeoIpTags(w)
	case http.MethodPut:
		a.AddGeoIpTag(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// @Summary Add IP/CIDR blocks to a set
// @Tags GeoIP
// @Accept json
// @Produce json
// @Param body body AddGeoIpRequest true "CIDR blocks to add"
// @Success 200 {object} AddIpResponse
// @Security BearerAuth
// @Router /geoip [put]
func (a *API) AddGeoIpTag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req AddGeoIpRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		log.Errorf("Failed to decode add domain request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

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

	if len(req.Cidr) == 0 {
		http.Error(w, "CIDR cannot be empty", http.StatusBadRequest)
		return
	}

	err := set.Targets.AppendIP(req.Cidr)
	if err != nil {
		log.Errorf("Failed to add CIDR to geoip set: %v", err)
		http.Error(w, "Failed to add CIDR", http.StatusInternalServerError)
		return
	}

	log.Infof("Added CIDR '%s' to set '%s' domains list", req.Cidr, set.Id)
	err = a.saveAndPushConfig(newCfg)

	if err != nil {
		log.Errorf("Failed to apply domain changes after adding domain: %v", err)
		http.Error(w, "Failed to apply domain changes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := AddIpResponse{
		Success:     true,
		Message:     fmt.Sprintf("Successfully added CIDR '%s'", req.Cidr),
		TotalCidrs:  len(set.Targets.IPs),
		ManualCidrs: set.Targets.IPs,
	}

	setJsonHeader(w)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// @Summary List geoip categories
// @Tags GeoIP
// @Produce json
// @Success 200 {object} GeoipResponse
// @Security BearerAuth
// @Router /geoip [get]
func (a *API) getGeoIpTags(w http.ResponseWriter) {

	setJsonHeader(w)
	enc := json.NewEncoder(w)

	if !a.geodataManager.IsGeoipConfigured() {
		log.Tracef("Geoip path is not configured")
		_ = enc.Encode(GeoipResponse{Tags: []string{}})
		return
	}

	tags, err := a.geodataManager.ListCategories(a.geodataManager.GetGeoipPath())
	if err != nil {
		http.Error(w, "Failed to load geoip tags: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := GeoipResponse{
		Tags: tags,
	}

	_ = enc.Encode(response)
}
