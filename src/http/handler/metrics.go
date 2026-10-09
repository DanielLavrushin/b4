package handler

import (
	"encoding/json"
	"net/http"

	"github.com/daniellavrushin/b4/metrics"
)

type MetricsCollector = metrics.MetricsCollector

func GetMetricsCollector() *metrics.MetricsCollector {
	return metrics.GetMetricsCollector()
}

type MetricsResetResponse struct {
	Success    bool  `json:"success"`
	StatsSince int64 `json:"stats_since"`
}

type EscalationsClearResponse struct {
	Success bool `json:"success"`
	Cleared int  `json:"cleared"`
}

type MetricsSummary struct {
	Connections           uint64  `json:"connections"`
	ConnectionsInSets     uint64  `json:"connections_in_sets"`
	ConnectionsLastMinute uint64  `json:"connections_last_minute"`
	RSTDropped            uint64  `json:"rst_dropped"`
	BlockedTotal          uint64  `json:"blocked_total"`
	Uptime                string  `json:"uptime"`
	UptimeS               int64   `json:"uptime_s"`
	CPUPercent            float64 `json:"cpu_percent"`
	RSSBytes              uint64  `json:"rss_bytes"`
	MemTotalBytes         uint64  `json:"mem_total_bytes"`
}

func (api *API) RegisterMetricsApi() {
	api.mux.HandleFunc("/api/metrics", api.getMetrics)
	api.mux.HandleFunc("/api/metrics/summary", api.getMetricsSummary)
	api.mux.HandleFunc("/api/metrics/reset", api.resetMetrics)
	api.mux.HandleFunc("/api/escalations/clear", api.clearEscalations)
}

// @Summary Get the dashboard metrics
// @Description Returns the same hello frame that /api/ws/metrics sends on connect: engine state, uptime, per-minute and per-ten-minute connection buckets, per-set activity, totals since stats_since, blocked lists, the most connected domains, escalations, events and attention items. Connections are counted once per flow, and so are domains. All times are wall-clock milliseconds.
// @Tags Metrics
// @Produce json
// @Success 200 {object} metrics.Frame
// @Failure 405 {string} string "Method not allowed"
// @Security BearerAuth
// @Router /metrics [get]
func (a *API) getMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	setJsonHeader(w)
	_ = json.NewEncoder(w).Encode(metrics.GetMetricsCollector().Hello())
}

// @Summary Reset the metrics counters
// @Description Zeroes the totals, the RST drop and escalation counts, the blocked lists and the domain counts, and moves stats_since to now. Activity history, uptime, events and live escalations are kept.
// @Tags Metrics
// @Produce json
// @Success 200 {object} MetricsResetResponse
// @Failure 405 {string} string "Method not allowed"
// @Security BearerAuth
// @Router /metrics/reset [post]
func (a *API) resetMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	mc := metrics.GetMetricsCollector()
	mc.ResetCounters()

	setJsonHeader(w)
	_ = json.NewEncoder(w).Encode(MetricsResetResponse{Success: true, StatsSince: mc.StatsSince()})
}

// @Summary Clear the escalations
// @Description Returns every escalated host to its original set and forgets the RST and DNS failure history that leads to escalation. cleared is the number of escalations that were active.
// @Tags Metrics
// @Produce json
// @Success 200 {object} EscalationsClearResponse
// @Failure 405 {string} string "Method not allowed"
// @Security BearerAuth
// @Router /escalations/clear [post]
func (a *API) clearEscalations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cleared := 0
	if globalPool != nil {
		cleared = len(globalPool.GetEscalations())
		globalPool.ClearEscalations()
	}
	metrics.GetMetricsCollector().UpdateEscalations([]metrics.EscalationEntry{})

	setJsonHeader(w)
	_ = json.NewEncoder(w).Encode(EscalationsClearResponse{Success: true, Cleared: cleared})
}

// @Summary Get a metrics summary
// @Description Connections are counted once per flow since stats_since; connections_last_minute is the last closed minute. uptime_s is the process uptime. cpu_percent is the share of all cores over the last 10 s.
// @Tags Metrics
// @Produce json
// @Success 200 {object} MetricsSummary
// @Failure 405 {string} string "Method not allowed"
// @Security BearerAuth
// @Router /metrics/summary [get]
func (a *API) getMetricsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	mc := metrics.GetMetricsCollector()
	totals := mc.Totals()
	inSets, notInSet := mc.LastMinute()
	proc := mc.Process()

	setJsonHeader(w)
	_ = json.NewEncoder(w).Encode(MetricsSummary{
		Connections:           totals.Conns,
		ConnectionsInSets:     totals.ConnsInSets,
		ConnectionsLastMinute: inSets + notInSet,
		RSTDropped:            totals.RSTDropped,
		BlockedTotal:          totals.BlockedDNS + totals.BlockedConns,
		Uptime:                mc.UptimeString(),
		UptimeS:               mc.UptimeSeconds(),
		CPUPercent:            proc.CPUPercent,
		RSSBytes:              proc.RSSBytes,
		MemTotalBytes:         proc.MemTotalBytes,
	})
}
