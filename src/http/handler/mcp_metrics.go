package handler

import (
	"math"
	"time"

	"github.com/daniellavrushin/b4/metrics"
)

func mcpWallTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func mcpPercent(part, whole uint64) float64 {
	if whole == 0 {
		return 0
	}
	return math.Round(float64(part)/float64(whole)*10000) / 100
}

func mcpMetricsSnapshot(mc *metrics.MetricsCollector) mcpMetricsOut {
	inSets, notInSet := mc.LastMinute()
	totals := mc.Totals()
	proc := mc.Process()
	return mcpMetricsOut{
		LastMinuteInSets:   inSets,
		LastMinuteNotInSet: notInSet,
		LastMinuteMatched:  mcpPercent(inSets, inSets+notInSet),
		Connections:        totals.Conns,
		CountersSince:      mcpWallTime(mc.StatsSince()),
		RSTDropped:         totals.RSTDropped,
		BlockedDNS:         totals.BlockedDNS,
		BlockedConnections: totals.BlockedConns,
		CPUPercent:         proc.CPUPercent,
		RSSBytes:           proc.RSSBytes,
		RSSPercentOfRAM:    mcpPercent(proc.RSSBytes, proc.MemTotalBytes),
		MemTotalBytes:      proc.MemTotalBytes,
		Uptime:             mc.UptimeString(),
		UptimeS:            mc.UptimeSeconds(),
	}
}
