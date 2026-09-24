package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/utils"
	"github.com/daniellavrushin/b4/watchdog"
)

type WatchdogDomainRequest struct {
	Domain string `json:"domain" example:"example.com"`
}

type WatchdogActionResponse struct {
	Success bool   `json:"success" example:"true"`
	Message string `json:"message" example:"added example.com to watchdog"`
	Outcome string `json:"outcome,omitempty" example:"scheduled"`
}

type WatchdogSetRequest struct {
	Enabled bool `json:"enabled" example:"true"`
}

type WatchdogMoveRequest struct {
	SetId string `json:"set_id" example:"3f2a9c1e-7b1d-4e8a-9c2f-5d6e7f8a9b0c"`
}

func (api *API) RegisterWatchdogApi() {
	api.mux.HandleFunc("/api/watchdog/status", api.handleWatchdogStatus)
	api.mux.HandleFunc("/api/watchdog/check", api.handleWatchdogForceCheck)
	api.mux.HandleFunc("/api/watchdog/domains", api.handleWatchdogDomains)
	api.mux.HandleFunc("/api/watchdog/domains/{domain}", api.handleWatchdogDeleteDomain)
	api.mux.HandleFunc("/api/watchdog/domains/{domain}/move", api.handleWatchdogMoveDomain)
	api.mux.HandleFunc("/api/watchdog/sets/{id}", api.handleWatchdogSet)
	api.mux.HandleFunc("/api/watchdog/sets/{id}/check", api.handleWatchdogSetCheck)
	api.mux.HandleFunc("/api/watchdog/enable", api.handleWatchdogEnable)
	api.mux.HandleFunc("/api/watchdog/disable", api.handleWatchdogDisable)
}

func watchdogBlockedError(set *config.SetConfig, blocker string) *APIError {
	return &APIError{
		Status:  http.StatusBadRequest,
		Code:    blocker,
		Message: fmt.Sprintf("the watchdog cannot watch set %q: %s", set.Name, config.WatchdogBlockerText(blocker)),
	}
}

// @Summary Get watchdog status
// @Description Returns the master switch, the status of each domain on the global list (last check, failures, cooldown, matched set, the set the live engine uses for it from the router's view, and watched_by_set_id/watched_by_set_name when the entry is not checked on its own because a set with an active watchdog probes or handles that host), and the status of every set whose own watchdog is active, with a per-URL breakdown.
// @Tags Watchdog
// @Produce json
// @Success 200 {object} watchdog.WatchdogState
// @Failure 405 {string} string "Method not allowed"
// @Security BearerAuth
// @Router /watchdog/status [get]
func (api *API) handleWatchdogStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if globalWatchdog == nil {
		setJsonHeader(w)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled": false,
			"domains": []interface{}{},
			"sets":    []interface{}{},
		})
		return
	}

	state := globalWatchdog.GetState()
	setJsonHeader(w)
	json.NewEncoder(w).Encode(state)
}

// @Summary Force an immediate watchdog check
// @Description Schedules an out-of-band check for a domain that is already present in the watchdog list. The domain may be passed as a bare host or a full URL; both forms are matched against the stored list.
// @Tags Watchdog
// @Accept json
// @Produce json
// @Param body body WatchdogDomainRequest true "Domain to force-check"
// @Success 200 {object} WatchdogActionResponse
// @Failure 400 {string} string "domain is required"
// @Failure 404 {string} string "domain not in watchdog list"
// @Failure 405 {string} string "Method not allowed"
// @Failure 503 {string} string "watchdog is not running"
// @Security BearerAuth
// @Router /watchdog/check [post]
func (api *API) handleWatchdogForceCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if globalWatchdog == nil {
		http.Error(w, "watchdog is not running", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "domain is required", http.StatusBadRequest)
		return
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if domain == "" {
		http.Error(w, "domain is required", http.StatusBadRequest)
		return
	}

	cfg := api.getCfg()
	found := false
	for _, d := range cfg.System.Checker.Watchdog.Domains {
		if d == domain || watchdog.ExtractDomain(d) == domain {
			found = true
			globalWatchdog.ForceCheck(d)
			break
		}
	}
	if !found {
		http.Error(w, "domain not in watchdog list", http.StatusNotFound)
		return
	}
	log.Infof("[WATCHDOG] forced check requested for %s", domain)

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "check scheduled for " + domain,
	})
}

// @Summary Add a domain to the watchdog list
// @Description Adds a domain to the list of monitored targets. Duplicates (including different URL forms that resolve to the same host) are rejected. The configuration is persisted and pushed to running services.
// @Tags Watchdog
// @Accept json
// @Produce json
// @Param body body WatchdogDomainRequest true "Domain to add"
// @Success 200 {object} WatchdogActionResponse
// @Failure 400 {string} string "domain is required"
// @Failure 405 {string} string "Method not allowed"
// @Failure 409 {string} string "domain already in watchdog list"
// @Failure 500 {string} string "failed to save configuration"
// @Security BearerAuth
// @Router /watchdog/domains [post]
func (api *API) handleWatchdogDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domain == "" {
		http.Error(w, "domain is required", http.StatusBadRequest)
		return
	}

	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	normalizedDomain := watchdog.ExtractDomain(domain)

	_, _, err := api.editConfig(func(next *config.Config) error {
		wd := &next.System.Checker.Watchdog
		for _, d := range wd.Domains {
			if d == domain || watchdog.ExtractDomain(d) == normalizedDomain {
				return errWatchdogDomainListed
			}
		}
		wd.Domains = append(wd.Domains, domain)
		return nil
	})
	switch {
	case errors.Is(err, errWatchdogDomainListed):
		http.Error(w, "domain already in watchdog list", http.StatusConflict)
		return
	case err != nil:
		log.Errorf("Failed to save watchdog config: %v", err)
		http.Error(w, "failed to save configuration", http.StatusInternalServerError)
		return
	}

	log.Infof("[WATCHDOG] added domain %s to watch list", domain)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "added " + domain + " to watchdog",
	})
}

// @Summary Remove a domain from the watchdog list
// @Description Removes a domain from the monitored list. The path parameter may be either the exact stored value or the bare host extracted from a stored URL.
// @Tags Watchdog
// @Produce json
// @Param domain path string true "Domain or host to remove"
// @Success 200 {object} WatchdogActionResponse
// @Failure 400 {string} string "domain is required"
// @Failure 404 {string} string "domain not found in watchdog list"
// @Failure 405 {string} string "Method not allowed"
// @Failure 500 {string} string "failed to save configuration"
// @Security BearerAuth
// @Router /watchdog/domains/{domain} [delete]
func (api *API) handleWatchdogDeleteDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	domain := strings.ToLower(strings.TrimSpace(r.PathValue("domain")))
	if domain == "" {
		http.Error(w, "domain is required", http.StatusBadRequest)
		return
	}

	_, _, err := api.editConfig(func(next *config.Config) error {
		found := false
		var filtered []string
		for _, d := range next.System.Checker.Watchdog.Domains {
			if d == domain || watchdog.ExtractDomain(d) == domain {
				found = true
				continue
			}
			filtered = append(filtered, d)
		}
		if !found {
			return errWatchdogDomainMissing
		}
		next.System.Checker.Watchdog.Domains = filtered
		return nil
	})
	switch {
	case errors.Is(err, errWatchdogDomainMissing):
		http.Error(w, "domain not found in watchdog list", http.StatusNotFound)
		return
	case err != nil:
		log.Errorf("Failed to save watchdog config: %v", err)
		http.Error(w, "failed to save configuration", http.StatusInternalServerError)
		return
	}

	log.Infof("[WATCHDOG] removed domain %s from watch list", domain)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "removed " + domain + " from watchdog",
	})
}

// @Summary Enable the watchdog
// @Description Turns the watchdog on and persists the change in the configuration. Monitoring of configured domains resumes on the next tick.
// @Tags Watchdog
// @Produce json
// @Success 200 {object} WatchdogActionResponse
// @Failure 405 {string} string "Method not allowed"
// @Failure 500 {string} string "failed to save configuration"
// @Security BearerAuth
// @Router /watchdog/enable [post]
func (api *API) handleWatchdogEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if err := api.setWatchdogMaster(true); err != nil {
		log.Errorf("Failed to save watchdog config: %v", err)
		http.Error(w, "failed to save configuration", http.StatusInternalServerError)
		return
	}

	log.Infof("[WATCHDOG] enabled")
	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "watchdog enabled",
	})
}

// @Summary Disable the watchdog
// @Description Turns the watchdog off and persists the change in the configuration. No further domain checks are performed until it is re-enabled.
// @Tags Watchdog
// @Produce json
// @Success 200 {object} WatchdogActionResponse
// @Failure 405 {string} string "Method not allowed"
// @Failure 500 {string} string "failed to save configuration"
// @Security BearerAuth
// @Router /watchdog/disable [post]
func (api *API) handleWatchdogDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if err := api.setWatchdogMaster(false); err != nil {
		log.Errorf("Failed to save watchdog config: %v", err)
		http.Error(w, "failed to save configuration", http.StatusInternalServerError)
		return
	}

	log.Infof("[WATCHDOG] disabled")
	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "watchdog disabled",
	})
}

var (
	errWatchdogDomainListed  = errors.New("domain already in watchdog list")
	errWatchdogDomainMissing = errors.New("domain not found in watchdog list")
)

func (api *API) setWatchdogMaster(enabled bool) error {
	_, _, err := api.editConfig(func(next *config.Config) error {
		next.System.Checker.Watchdog.Enabled = enabled
		return nil
	})
	return err
}

func (api *API) saveWatchdogChange(edit func(next *config.Config) error) error {
	oldCfg, newCfg, err := api.editConfig(edit)
	if err != nil {
		return err
	}
	api.PerformSoftRestart(newCfg, oldCfg)
	return nil
}

// @Summary Turn one set's watchdog on or off
// @Description Turns the per-set watchdog of one set on or off. A watched set is checked on the global watchdog schedule through its own discovery URLs, and when they keep failing b4 runs a discovery for that set and writes a confirmed strategy into it. Turning it on needs an enabled, direct (not routed) set with at least one discovery URL, that is not limited to listed devices and that lists at least one domain or geosite category. Turning it on also clears a previous give-up. A set that is later disabled, loses its last URL or its last domain keeps the flag but is not watched until that is undone; routing or a device include list turns the flag off.
// @Tags Watchdog
// @Accept json
// @Produce json
// @Param id path string true "Set ID"
// @Param body body WatchdogSetRequest true "Desired state"
// @Success 200 {object} WatchdogActionResponse
// @Failure 400 {object} APIError "codes: no_urls, routed_set, device_scoped, set_disabled, ip_only, invalid_json"
// @Failure 404 {object} APIError "code: not_found"
// @Failure 405 {string} string "Method not allowed"
// @Security BearerAuth
// @Router /watchdog/sets/{id} [put]
func (api *API) handleWatchdogSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req WatchdogSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}

	id := r.PathValue("id")
	state := map[bool]string{true: "on", false: "off"}[req.Enabled]
	var name string
	err := api.saveWatchdogChange(func(next *config.Config) error {
		set := next.GetSetById(id)
		if set == nil {
			return ErrNotFound("Set not found")
		}
		name = set.Name
		if req.Enabled {
			if blocker := set.WatchdogBlocker(); blocker != "" {
				return watchdogBlockedError(set, blocker)
			}
		}
		if set.Discovery.Watchdog == req.Enabled {
			return errConfigUnchanged
		}
		set.Discovery.Watchdog = req.Enabled
		return nil
	})
	switch {
	case errors.Is(err, errConfigUnchanged):
	case err != nil:
		if !isRefusal(err) {
			log.Errorf("Failed to save watchdog config: %v", err)
		}
		writeAPIError(w, err)
		return
	default:
		log.Infof("[WATCHDOG] set %q: watchdog turned %s", name, state)
	}
	resp := WatchdogActionResponse{
		Success: true,
		Message: fmt.Sprintf("watchdog for set %s is %s", name, state),
	}
	if req.Enabled && globalWatchdog != nil {
		resp.Outcome = globalWatchdog.ForceCheckSet(id, true)
		if resp.Outcome == watchdog.ForceCheckMasterOff {
			resp.Message += "; the watchdog master switch is off, so nothing is checked until it is turned on"
		}
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(resp)
}

// @Summary Force a check of one watched set
// @Description Schedules an immediate check of every discovery URL of a set whose watchdog is on, and clears its cooldown, its failure count and a previous give-up so a failing set can be healed again. "outcome" is scheduled, master_off (the master switch is off, nothing runs until it is on) or healing (a heal is running, nothing is scheduled).
// @Tags Watchdog
// @Produce json
// @Param id path string true "Set ID"
// @Success 200 {object} WatchdogActionResponse
// @Failure 400 {object} APIError "code: not_watched"
// @Failure 404 {object} APIError "code: not_found"
// @Failure 405 {string} string "Method not allowed"
// @Failure 503 {string} string "watchdog is not running"
// @Security BearerAuth
// @Router /watchdog/sets/{id}/check [post]
func (api *API) handleWatchdogSetCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")
	set := api.getCfg().GetSetById(id)
	if set == nil {
		writeAPIError(w, ErrNotFound("Set not found"))
		return
	}
	if !set.WatchdogActive() {
		writeAPIError(w, &APIError{
			Status:  http.StatusBadRequest,
			Code:    "not_watched",
			Message: fmt.Sprintf("set %q is not watched: turn its watchdog on first", set.Name),
		})
		return
	}
	if globalWatchdog == nil {
		http.Error(w, "watchdog is not running", http.StatusServiceUnavailable)
		return
	}

	outcome := globalWatchdog.ForceCheckSet(id, true)
	resp := WatchdogActionResponse{Success: true, Outcome: outcome}
	switch outcome {
	case watchdog.ForceCheckNotWatched:
		writeAPIError(w, &APIError{
			Status:  http.StatusBadRequest,
			Code:    "not_watched",
			Message: fmt.Sprintf("set %q is not watched: turn its watchdog on first", set.Name),
		})
		return
	case watchdog.ForceCheckHealing:
		resp.Message = fmt.Sprintf("set %s is being healed right now, so no check was scheduled; the heal verifies it when it ends", set.Name)
	case watchdog.ForceCheckMasterOff:
		resp.Message = fmt.Sprintf("the watchdog master switch is off, so set %s is not checked until it is turned on", set.Name)
	default:
		resp.Message = "check scheduled for set " + set.Name
	}
	log.Infof("[WATCHDOG] forced check requested for set %q (%s)", set.Name, outcome)

	setJsonHeader(w)
	json.NewEncoder(w).Encode(resp)
}

// @Summary Move a global watchdog entry to a set
// @Description Moves a domain from the global watchdog list to a set: its URL is added to the set's discovery URLs (unless the set already probes that host), the set's own watchdog is turned on, and the entry is removed from the global list.
// @Tags Watchdog
// @Accept json
// @Produce json
// @Param domain path string true "Domain or host on the global list"
// @Param body body WatchdogMoveRequest true "Target set"
// @Success 200 {object} WatchdogActionResponse
// @Failure 400 {object} APIError "codes: too_many_urls, invalid_url, no_urls, routed_set, device_scoped, set_disabled, ip_only, invalid_json"
// @Failure 404 {object} APIError "code: not_found"
// @Failure 405 {string} string "Method not allowed"
// @Security BearerAuth
// @Router /watchdog/domains/{domain}/move [post]
func (api *API) handleWatchdogMoveDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req WatchdogMoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}

	domain := strings.ToLower(strings.TrimSpace(r.PathValue("domain")))
	setId := strings.TrimSpace(req.SetId)
	var entry, name string
	err := api.saveWatchdogChange(func(next *config.Config) error {
		wd := &next.System.Checker.Watchdog
		idx := -1
		for i, d := range wd.Domains {
			if d == domain || watchdog.ExtractDomain(d) == domain {
				idx = i
				break
			}
		}
		if domain == "" || idx < 0 {
			return ErrNotFound("domain not found in watchdog list")
		}
		set := next.GetSetById(setId)
		if set == nil {
			return ErrNotFound("Set not found")
		}
		entry, name = wd.Domains[idx], set.Name

		canonical, host, err := utils.NormalizeProbeURL(entry)
		if err != nil {
			return &APIError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_url",
				Message: fmt.Sprintf("%q cannot be a discovery URL: %v", entry, err),
			}
		}
		known := false
		for _, existing := range set.Discovery.URLs {
			if _, existingHost, err := utils.NormalizeProbeURL(existing); err == nil && existingHost == host {
				known = true
				break
			}
		}
		if !known {
			if len(set.Discovery.URLs) >= utils.MaxProbeURLs {
				return &APIError{
					Status:  http.StatusBadRequest,
					Code:    "too_many_urls",
					Message: fmt.Sprintf("set %q already has %d discovery URLs, the most it can keep", set.Name, utils.MaxProbeURLs),
				}
			}
			set.Discovery.URLs = append(set.Discovery.URLs, canonical)
		}
		if blocker := set.WatchdogBlocker(); blocker != "" {
			return watchdogBlockedError(set, blocker)
		}
		set.Discovery.Watchdog = true
		wd.Domains = append(wd.Domains[:idx], wd.Domains[idx+1:]...)
		return nil
	})
	if err != nil {
		if !isRefusal(err) {
			log.Errorf("Failed to save watchdog config: %v", err)
		}
		writeAPIError(w, err)
		return
	}
	if globalWatchdog != nil {
		globalWatchdog.ForceCheckSet(setId, true)
	}

	log.Infof("[WATCHDOG] moved %s from the global list to set %q", entry, name)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(WatchdogActionResponse{
		Success: true,
		Message: fmt.Sprintf("moved %s to set %s", entry, name),
	})
}
