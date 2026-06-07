// src/http/handler/metrics.go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/daniellavrushin/b4/metrics"
)

// Re-export for backward compatibility
type MetricsCollector = metrics.MetricsCollector
type WorkerHealth = metrics.WorkerHealth

func GetMetricsCollector() *metrics.MetricsCollector {
	return metrics.GetMetricsCollector()
}

// RegisterMetricsApi registers the metrics API endpoints
func (api *API) RegisterMetricsApi() {
	api.mux.HandleFunc("/api/metrics", api.getMetrics)
	api.mux.HandleFunc("/api/metrics/summary", api.getMetricsSummary)
	api.mux.HandleFunc("/api/metrics/reset", api.resetMetrics)
}

// @Summary Get full metrics snapshot
// @Tags Metrics
// @Produce json
// @Success 200 {object} object
// @Security BearerAuth
// @Router /metrics [get]
func (a *API) getMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	metricsData := metrics.GetMetricsCollector().GetSnapshot()

	setJsonHeader(w)
	enc := json.NewEncoder(w)
	_ = enc.Encode(metricsData)
}

// @Summary Reset metrics statistics
// @Tags Metrics
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /metrics/reset [post]
func (a *API) resetMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	metrics.GetMetricsCollector().ResetStats()
	if globalPool != nil {
		globalPool.ClearEscalations()
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Statistics reset successfully",
	})
}

// @Summary Get metrics summary
// @Tags Metrics
// @Produce json
// @Success 200 {object} object
// @Security BearerAuth
// @Router /metrics/summary [get]
func (a *API) getMetricsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	m := metrics.GetMetricsCollector().GetSnapshot()

	summary := map[string]interface{}{
		"total_connections": m.TotalConnections,
		"active_flows":      m.ActiveFlows,
		"current_cps":       m.CurrentCPS,
		"current_pps":       m.CurrentPPS,
		"rst_dropped":       m.RSTDropped,
		"blocked_total":     m.BlockedTotal,
		"uptime":            m.Uptime,
		"memory_percent":    m.MemoryUsage.Percent,
	}

	setJsonHeader(w)
	enc := json.NewEncoder(w)
	_ = enc.Encode(summary)
}
