package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/daniellavrushin/b4/detector"
	"github.com/daniellavrushin/b4/log"
)

func (api *API) RegisterDetectorApi() {
	api.mux.HandleFunc("/api/detector/start", api.handleStartDetector)
	api.mux.HandleFunc("/api/detector/status/{id}", api.handleDetectorStatus)
	api.mux.HandleFunc("/api/detector/cancel/{id}", api.handleCancelDetector)
	api.mux.HandleFunc("/api/detector/history", api.handleDetectorHistory)
	api.mux.HandleFunc("/api/detector/history/clear", api.handleClearDetectorHistory)
	api.mux.HandleFunc("/api/detector/history/{id}", api.handleDeleteDetectorHistoryEntry)
}

// @Summary Start detection suite
// @Tags Detector
// @Accept json
// @Produce json
// @Param body body DetectorRequest true "Detector request"
// @Success 202 {object} DetectorResponse
// @Security BearerAuth
// @Router /detector/start [post]
func (api *API) handleStartDetector(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req DetectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Errorf("Failed to decode detector request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Tests) == 0 {
		http.Error(w, "At least one test type is required", http.StatusBadRequest)
		return
	}

	// Validate test types
	var tests []detector.TestType
	for _, t := range req.Tests {
		switch t {
		case "dns":
			tests = append(tests, detector.TestDNS)
		case "dns-availability":
			tests = append(tests, detector.TestDNSAvail)
		case "domains":
			tests = append(tests, detector.TestDomains)
		case "tcp":
			tests = append(tests, detector.TestTCP)
		case "sni":
			tests = append(tests, detector.TestSNI)
		case "telegram":
			tests = append(tests, detector.TestTelegram)
		default:
			http.Error(w, fmt.Sprintf("Unknown test type: %s", t), http.StatusBadRequest)
			return
		}
	}

	testOrder := map[detector.TestType]int{
		detector.TestDNS:      0,
		detector.TestDNSAvail: 1,
		detector.TestDomains:  2,
		detector.TestTCP:      3,
		detector.TestSNI:      4,
		detector.TestTelegram: 5,
	}
	sort.Slice(tests, func(i, j int) bool {
		return testOrder[tests[i]] < testOrder[tests[j]]
	})

	cfg := api.getCfg()
	suite := detector.NewDetectorSuite(tests, cfg.DiscoveryFlowMark())

	go func() {
		suite.Run(cfg)
		log.Infof("Detector suite %s complete", suite.Id)
	}()

	response := DetectorResponse{
		Id:             suite.Id,
		Tests:          req.Tests,
		EstimatedTests: suite.TotalChecks,
		Message:        fmt.Sprintf("Detection started with %d test(s)", len(tests)),
	}

	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(response)
}

// @Summary Get detector status
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
	if id == "" {
		http.Error(w, "Suite ID required", http.StatusBadRequest)
		return
	}

	suite, ok := detector.GetSuite(id)
	if !ok {
		http.Error(w, "Suite not found", http.StatusNotFound)
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(suite)
}

// @Summary Cancel detector suite
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
	if id == "" {
		http.Error(w, "Suite ID required", http.StatusBadRequest)
		return
	}

	if err := detector.CancelSuite(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	log.Infof("Canceled detector suite %s", id)

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Detector suite canceled",
	})
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

	history := detector.GetHistory(api.getCfg().ConfigPath)
	setJsonHeader(w)
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

	history := detector.LoadDetectorHistory(api.getCfg().ConfigPath)
	history.Clear()
	if err := history.Save(api.getCfg().ConfigPath); err != nil {
		log.Errorf("Failed to clear detector history: %v", err)
		http.Error(w, "Failed to clear detector history", http.StatusInternalServerError)
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Detector history cleared",
	})
}

// @Summary Delete detector history entry
// @Tags Detector
// @Produce json
// @Param id path string true "Entry ID"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /detector/history/{id} [delete]
func (api *API) handleDeleteDetectorHistoryEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Entry ID required", http.StatusBadRequest)
		return
	}

	history := detector.LoadDetectorHistory(api.getCfg().ConfigPath)
	history.RemoveEntry(id)
	if err := history.Save(api.getCfg().ConfigPath); err != nil {
		log.Errorf("Failed to save detector history: %v", err)
		http.Error(w, "Failed to save detector history", http.StatusInternalServerError)
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Removed history entry %s", id),
	})
}
