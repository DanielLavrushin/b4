package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/detector"
	"github.com/daniellavrushin/b4/log"
)

const detectorMaxSites = 200

var detectorListsOnce sync.Once

func (api *API) RegisterDetectorApi() {
	api.mux.HandleFunc("/api/detector/start", api.handleStartDetector)
	api.mux.HandleFunc("/api/detector/current", api.handleDetectorCurrent)
	api.mux.HandleFunc("/api/detector/status/{id}", api.handleDetectorStatus)
	api.mux.HandleFunc("/api/detector/cancel/{id}", api.handleCancelDetector)
	api.mux.HandleFunc("/api/detector/history", api.handleDetectorHistory)
	api.mux.HandleFunc("/api/detector/history/clear", api.handleClearDetectorHistory)
	api.mux.HandleFunc("/api/detector/history/{id}", api.handleDetectorHistoryEntry)
	api.mux.HandleFunc("/api/detector/lists", api.handleDetectorLists)
	api.mux.HandleFunc("/api/detector/lists/update", api.handleDetectorListsUpdate)
	api.mux.HandleFunc("/api/detector/lists/reset", api.handleDetectorListsReset)
}

func (api *API) detectorLists() detector.TargetLists {
	detectorListsOnce.Do(func() {
		detector.LoadListOverride(api.getCfg().ConfigPath)
	})
	return detector.Lists()
}

func (api *API) detectorSetLookup() detector.SetLookup {
	cfg := api.getCfg()
	return func(domain string) *detector.SetMatch {
		matches := api.matchDomainsToSets([]string{domain}, "")
		if len(matches) == 0 {
			return nil
		}
		best := matches[0]
		for _, m := range matches {
			if m.Enabled && !best.Enabled {
				best = m
			}
		}
		out := &detector.SetMatch{Id: best.SetId, Name: best.SetName, Enabled: best.Enabled}
		for _, set := range cfg.Sets {
			if set.Id == best.SetId {
				out.DNS = set.DNS
				break
			}
		}
		return out
	}
}

// @Summary Start a detector run
// @Tags Detector
// @Accept json
// @Produce json
// @Param body body DetectorRequest true "Detector request"
// @Success 202 {object} DetectorResponse
// @Failure 400 {string} string
// @Failure 409 {string} string
// @Security BearerAuth
// @Router /detector/start [post]
func (api *API) handleStartDetector(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req DetectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	lists := api.detectorLists()
	for _, sc := range req.Scopes {
		switch sc {
		case detector.ScopeSites, detector.ScopeDNS, detector.ScopeHosting, detector.ScopeTelegram:
		default:
			http.Error(w, fmt.Sprintf("Unknown scope: %s", sc), http.StatusBadRequest)
			return
		}
	}
	if len(req.Scopes) == 0 {
		http.Error(w, "At least one scope is required", http.StatusBadRequest)
		return
	}
	hasSites := false
	for _, sc := range req.Scopes {
		if sc == detector.ScopeSites {
			hasSites = true
		}
	}
	if hasSites && len(req.Sites) == 0 {
		req.Sites = lists.Sites
	}
	if len(req.Sites) > detectorMaxSites {
		http.Error(w, fmt.Sprintf("At most %d sites per run", detectorMaxSites), http.StatusBadRequest)
		return
	}
	cfg := api.getCfg()
	suite, err := detector.NewSuite(detector.Options{
		Sites:     req.Sites,
		Scopes:    req.Scopes,
		IPVersion: req.IPVersion,
		Parallel:  req.Parallel,
		FetchMode: req.FetchMode,
		SkipTLS12: req.SkipTLS12,
		SNISearch: req.SNISearch,
	}, cfg.MainInjectedMark(), api.detectorSetLookup())
	if err != nil {
		http.Error(w, "A detector run is already in progress", http.StatusConflict)
		return
	}

	total := suite.Progress.Total
	go suite.Run(cfg.ConfigPath)

	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(DetectorResponse{
		Id:      suite.Id,
		Total:   total,
		Message: "Detector run started",
	})
}

// @Summary Get the running detector suite, if any
// @Tags Detector
// @Produce json
// @Success 200 {object} object
// @Security BearerAuth
// @Router /detector/current [get]
func (api *API) handleDetectorCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	setJsonHeader(w)
	suite := detector.RunningSuite()
	if suite == nil {
		w.Write([]byte("null"))
		return
	}
	json.NewEncoder(w).Encode(suite)
}

// @Summary Get detector run status
// @Tags Detector
// @Produce json
// @Param id path string true "Suite ID"
// @Success 200 {object} object
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /detector/status/{id} [get]
func (api *API) handleDetectorStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if suite, ok := detector.GetSuite(id); ok {
		setJsonHeader(w)
		json.NewEncoder(w).Encode(suite)
		return
	}
	if entry := detector.LoadHistory(api.getCfg().ConfigPath).Get(id); entry != nil {
		setJsonHeader(w)
		json.NewEncoder(w).Encode(entry)
		return
	}
	http.Error(w, "Suite not found", http.StatusNotFound)
}

// @Summary Cancel a detector run
// @Tags Detector
// @Produce json
// @Param id path string true "Suite ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /detector/cancel/{id} [delete]
func (api *API) handleCancelDetector(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if !detector.CancelSuite(id) {
		http.Error(w, "Suite not found", http.StatusNotFound)
		return
	}
	log.Infof("Canceled detector run %s", id)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// @Summary Get detector history
// @Tags Detector
// @Produce json
// @Success 200 {array} object
// @Security BearerAuth
// @Router /detector/history [get]
func (api *API) handleDetectorHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	history := detector.LoadHistory(api.getCfg().ConfigPath)
	setJsonHeader(w)
	if history.Entries == nil {
		history.Entries = []*detector.Suite{}
	}
	json.NewEncoder(w).Encode(history.Entries)
}

// @Summary Clear detector history
// @Tags Detector
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /detector/history/clear [post]
func (api *API) handleClearDetectorHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	history := detector.LoadHistory(api.getCfg().ConfigPath)
	history.Clear()
	if err := history.Save(api.getCfg().ConfigPath); err != nil {
		http.Error(w, "Failed to clear detector history", http.StatusInternalServerError)
		return
	}
	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// @Summary Get or delete a detector history entry
// @Tags Detector
// @Produce json
// @Param id path string true "Entry ID"
// @Success 200 {object} object
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /detector/history/{id} [delete]
func (api *API) handleDetectorHistoryEntry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	history := detector.LoadHistory(api.getCfg().ConfigPath)
	switch r.Method {
	case http.MethodGet:
		entry := history.Get(id)
		if entry == nil {
			http.Error(w, "Entry not found", http.StatusNotFound)
			return
		}
		setJsonHeader(w)
		json.NewEncoder(w).Encode(entry)
	case http.MethodDelete:
		history.Remove(id)
		if err := history.Save(api.getCfg().ConfigPath); err != nil {
			http.Error(w, "Failed to save detector history", http.StatusInternalServerError)
			return
		}
		setJsonHeader(w)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func detectorListsResponse(lists detector.TargetLists) DetectorListsResponse {
	return DetectorListsResponse{
		ListsDate:    lists.ListsDate,
		ListsSource:  lists.ListsSource,
		EmbeddedDate: detector.EmbeddedLists().ListsDate,
		Custom:       lists.ListsDate != detector.EmbeddedLists().ListsDate,
		Sites:        lists.Sites,
		SiteCount:    len(lists.Sites),
		DNSServers:   len(lists.DNSServers),
		TCPTargets:   len(lists.TCPTargets),
		WhitelistSNI: len(lists.WhitelistSNI),
	}
}

// @Summary Get detector target lists
// @Tags Detector
// @Produce json
// @Success 200 {object} DetectorListsResponse
// @Security BearerAuth
// @Router /detector/lists [get]
func (api *API) handleDetectorLists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	setJsonHeader(w)
	json.NewEncoder(w).Encode(detectorListsResponse(api.detectorLists()))
}

// @Summary Update detector target lists from the upstream dpi-detector project
// @Tags Detector
// @Produce json
// @Success 200 {object} DetectorListsResponse
// @Failure 502 {string} string
// @Security BearerAuth
// @Router /detector/lists/update [post]
func (api *API) handleDetectorListsUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	api.detectorLists()
	cfg := api.getCfg()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	lists, err := detector.UpdateListsFromUpstream(ctx, cfg.MainInjectedMark(), cfg.ConfigPath)
	if err != nil {
		log.Errorf("Detector list update failed: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	log.Infof("Detector target lists updated: %d sites, %d targets, %d resolvers", len(lists.Sites), len(lists.TCPTargets), len(lists.DNSServers))
	setJsonHeader(w)
	json.NewEncoder(w).Encode(detectorListsResponse(lists))
}

// @Summary Reset detector target lists to the embedded copy
// @Tags Detector
// @Produce json
// @Success 200 {object} DetectorListsResponse
// @Security BearerAuth
// @Router /detector/lists/reset [post]
func (api *API) handleDetectorListsReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	api.detectorLists()
	detector.ResetListOverride(api.getCfg().ConfigPath)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(detectorListsResponse(detector.Lists()))
}
