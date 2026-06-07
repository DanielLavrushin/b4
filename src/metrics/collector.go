package metrics

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

type MetricsCollector struct {
	TopDomains          map[string]uint64 `json:"top_domains"`
	ProtocolDist        map[string]uint64 `json:"protocol_dist"`
	GeoDist             map[string]uint64 `json:"geo_dist"`
	BlockedDomains      map[string]uint64 `json:"blocked_domains"`
	BlockedDevices      map[string]uint64 `json:"blocked_devices"`
	TotalConnections    uint64            `json:"total_connections"`
	ActiveFlows         uint64            `json:"active_flows"`
	PacketsProcessed    uint64            `json:"packets_processed"`
	BytesProcessed      uint64            `json:"bytes_processed"`
	TCPConnections      uint64            `json:"tcp_connections"`
	UDPConnections      uint64            `json:"udp_connections"`
	TargetedConnections uint64            `json:"targeted_connections"`
	RSTDropped          uint64            `json:"rst_dropped"`
	BlockedTotal        uint64            `json:"blocked_total"`
	CurrentCPS          float64           `json:"current_cps"`
	CurrentPPS          float64           `json:"current_pps"`
	CPUUsage            float64           `json:"cpu_usage"`

	ConnectionRate    []TimeSeriesPoint            `json:"connection_rate"`
	PacketRate        []TimeSeriesPoint            `json:"packet_rate"`
	StartTime         time.Time                    `json:"start_time"`
	Uptime            string                       `json:"uptime"`
	MemoryUsage       MemoryStats                  `json:"memory_usage"`
	WorkerStatus      []WorkerHealth               `json:"worker_status"`
	NFQueueStatus     string                       `json:"nfqueue_status"`
	TablesStatus      string                       `json:"tables_status"`
	RecentConnections []ConnectionLog              `json:"recent_connections"`
	RecentEvents      []SystemEvent                `json:"recent_events"`
	DeviceDomains     map[string]map[string]uint64 `json:"device_domains"`
	DomainTLS         map[string]string            `json:"domain_tls"`
	Escalations       []EscalationEntry            `json:"escalations"`
	TotalEscalations  uint64                       `json:"total_escalations"`

	lastUpdate      time.Time    `json:"-"`
	mu              sync.RWMutex `json:"-"`
	lastConnCount   uint64       `json:"-"`
	lastPacketCount uint64       `json:"-"`
}

type TimeSeriesPoint struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

type MemoryStats struct {
	Allocated      uint64  `json:"allocated"`
	TotalAllocated uint64  `json:"total_allocated"`
	System         uint64  `json:"system"`
	Percent        float64 `json:"percent"`
	HeapAlloc      uint64  `json:"heap_alloc"`
	HeapInuse      uint64  `json:"heap_inuse"`
	NumGC          uint32  `json:"num_gc"`
}

type WorkerHealth struct {
	Processed uint64 `json:"processed"`
	ID        int    `json:"id"`
	Status    string `json:"status"`
}

type ConnectionLog struct {
	Timestamp   time.Time `json:"timestamp"`
	Protocol    string    `json:"protocol"`
	Domain      string    `json:"domain"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	IsTarget    bool      `json:"is_target"`
	SourceMAC   string    `json:"source_mac,omitempty"`
	HostSet     string    `json:"host_set,omitempty"`
}

type SystemEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

type EscalationEntry struct {
	Host      string    `json:"host"`
	ToSet     string    `json:"to_set"`
	Hops      int       `json:"hops"`
	SetAt     time.Time `json:"set_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

var (
	metricsCollector *MetricsCollector
	metricsOnce      sync.Once
)

func GetMetricsCollector() *MetricsCollector {
	metricsOnce.Do(func() {
		metricsCollector = &MetricsCollector{
			StartTime:         time.Now(),
			TopDomains:        make(map[string]uint64),
			ProtocolDist:      make(map[string]uint64),
			GeoDist:           make(map[string]uint64),
			BlockedDomains:    make(map[string]uint64),
			BlockedDevices:    make(map[string]uint64),
			ConnectionRate:    make([]TimeSeriesPoint, 0, 60),
			PacketRate:        make([]TimeSeriesPoint, 0, 60),
			RecentConnections: make([]ConnectionLog, 0, 10),
			RecentEvents:      make([]SystemEvent, 0, 20),
			WorkerStatus:      make([]WorkerHealth, 0),
			DeviceDomains:     make(map[string]map[string]uint64),
			DomainTLS:         make(map[string]string),
			Escalations:       make([]EscalationEntry, 0),
			NFQueueStatus:     "active",
			TablesStatus:      "active",
			lastUpdate:        time.Now(),
		}

		go metricsCollector.updateLoop()
	})
	return metricsCollector
}

func (m *MetricsCollector) updateLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.updateRates()
		m.updateSystemStats()
	}
}

func (m *MetricsCollector) updateRates() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	duration := now.Sub(m.lastUpdate).Seconds()
	if duration <= 0 {
		return
	}

	connDiff := m.TotalConnections - m.lastConnCount
	packetDiff := m.PacketsProcessed - m.lastPacketCount

	m.CurrentCPS = float64(connDiff) / duration
	m.CurrentPPS = float64(packetDiff) / duration

	nowMs := now.UnixMilli()

	m.ConnectionRate = append(m.ConnectionRate, TimeSeriesPoint{
		Timestamp: nowMs,
		Value:     m.CurrentCPS,
	})
	if len(m.ConnectionRate) > 60 {
		m.ConnectionRate = m.ConnectionRate[len(m.ConnectionRate)-60:]
	}

	m.PacketRate = append(m.PacketRate, TimeSeriesPoint{
		Timestamp: nowMs,
		Value:     m.CurrentPPS,
	})
	if len(m.PacketRate) > 60 {
		m.PacketRate = m.PacketRate[len(m.PacketRate)-60:]
	}

	m.lastUpdate = now
	m.lastConnCount = m.TotalConnections
	m.lastPacketCount = m.PacketsProcessed

	m.Uptime = formatDuration(now.Sub(m.StartTime))
}

func (m *MetricsCollector) updateSystemStats() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.MemoryUsage = MemoryStats{
		Allocated:      memStats.Alloc,
		TotalAllocated: memStats.TotalAlloc,
		System:         memStats.Sys,
		NumGC:          memStats.NumGC,
		HeapAlloc:      memStats.HeapAlloc,
		HeapInuse:      memStats.HeapInuse,
		Percent:        float64(memStats.Alloc) / float64(memStats.Sys) * 100,
	}

	m.CPUUsage = float64(runtime.NumGoroutine())
}

func (m *MetricsCollector) RecordConnection(protocol, domain, source, destination string, isTarget bool, sourceMac, hostSet, tlsVersion string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalConnections++
	m.ActiveFlows++

	switch protocol {
	case "TCP":
		m.TCPConnections++
		m.ProtocolDist["TCP"]++
	case "UDP":
		m.UDPConnections++
		m.ProtocolDist["UDP"]++
	}

	if isTarget {
		m.TargetedConnections++
	}

	if domain != "" {
		m.TopDomains[domain]++
		if len(m.TopDomains) > 20 {
			m.pruneTopDomains()
		}
		if tlsVersion != "" {
			if existing, ok := m.DomainTLS[domain]; !ok {
				m.DomainTLS[domain] = tlsVersion
			} else if existing != tlsVersion && !strings.Contains(existing, tlsVersion) {
				m.DomainTLS[domain] = existing + "," + tlsVersion
			}
		}
	}

	// Track device-to-domain mapping
	if sourceMac != "" && domain != "" {
		if m.DeviceDomains[sourceMac] == nil {
			if len(m.DeviceDomains) >= 50 {
				// Prune least-active device (smallest total count)
				var minMac string
				var minTotal uint64 = ^uint64(0)
				for mac, domains := range m.DeviceDomains {
					var total uint64
					for _, c := range domains {
						total += c
					}
					if total < minTotal {
						minTotal = total
						minMac = mac
					}
				}
				delete(m.DeviceDomains, minMac)
			}
			m.DeviceDomains[sourceMac] = make(map[string]uint64)
		}
		m.DeviceDomains[sourceMac][domain]++
		// Prune domains per device
		if len(m.DeviceDomains[sourceMac]) > 100 {
			var minDomain string
			var minCount uint64 = ^uint64(0)
			for d, c := range m.DeviceDomains[sourceMac] {
				if c < minCount {
					minCount = c
					minDomain = d
				}
			}
			delete(m.DeviceDomains[sourceMac], minDomain)
		}
	}

	conn := ConnectionLog{
		Timestamp:   time.Now(),
		Protocol:    protocol,
		Domain:      domain,
		Source:      source,
		Destination: destination,
		IsTarget:    isTarget,
		SourceMAC:   sourceMac,
		HostSet:     hostSet,
	}

	m.RecentConnections = append([]ConnectionLog{conn}, m.RecentConnections...)
	if len(m.RecentConnections) > 10 {
		m.RecentConnections = m.RecentConnections[:10]
	}
}

func (m *MetricsCollector) RecordRSTDrop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RSTDropped++
}

func (m *MetricsCollector) RecordBlock(target, sourceMac string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.BlockedTotal++

	if target != "" {
		m.BlockedDomains[target]++
		pruneCounterMap(m.BlockedDomains, 20)
	}

	if sourceMac != "" {
		m.BlockedDevices[sourceMac]++
		pruneCounterMap(m.BlockedDevices, 50)
	}
}

func pruneCounterMap(m map[string]uint64, keep int) {
	for len(m) > keep {
		var minKey string
		var minVal uint64 = ^uint64(0)
		for k, v := range m {
			if v < minVal {
				minVal = v
				minKey = k
			}
		}
		delete(m, minKey)
	}
}

func (m *MetricsCollector) RecordEscalation() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalEscalations++
}

func (m *MetricsCollector) UpdateEscalations(entries []EscalationEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Escalations = entries
}

func (m *MetricsCollector) RecordPacket(bytes uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.PacketsProcessed++
	m.BytesProcessed += bytes
}

func (m *MetricsCollector) RecordEvent(level, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	event := SystemEvent{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	}

	m.RecentEvents = append([]SystemEvent{event}, m.RecentEvents...)
	if len(m.RecentEvents) > 20 {
		m.RecentEvents = m.RecentEvents[:20]
	}
}

func (m *MetricsCollector) UpdateWorkerStatus(workers []WorkerHealth) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WorkerStatus = workers
}

// UpdateSingleWorker updates a single worker's status without overwriting the entire array
func (m *MetricsCollector) UpdateSingleWorker(workerID int, status string, processed uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Ensure WorkerStatus has enough capacity
	for len(m.WorkerStatus) <= workerID {
		m.WorkerStatus = append(m.WorkerStatus, WorkerHealth{})
	}

	m.WorkerStatus[workerID] = WorkerHealth{
		ID:        workerID,
		Status:    status,
		Processed: processed,
	}
}

func (m *MetricsCollector) ResetStats() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalConnections = 0
	m.ActiveFlows = 0
	m.PacketsProcessed = 0
	m.BytesProcessed = 0
	m.TCPConnections = 0
	m.UDPConnections = 0
	m.TargetedConnections = 0
	m.RSTDropped = 0
	m.BlockedTotal = 0
	m.TotalEscalations = 0
	m.CurrentCPS = 0
	m.CurrentPPS = 0

	m.TopDomains = make(map[string]uint64)
	m.ProtocolDist = make(map[string]uint64)
	m.GeoDist = make(map[string]uint64)
	m.BlockedDomains = make(map[string]uint64)
	m.BlockedDevices = make(map[string]uint64)
	m.DeviceDomains = make(map[string]map[string]uint64)
	m.DomainTLS = make(map[string]string)
	m.Escalations = make([]EscalationEntry, 0)

	m.ConnectionRate = make([]TimeSeriesPoint, 0, 60)
	m.PacketRate = make([]TimeSeriesPoint, 0, 60)
	m.RecentConnections = make([]ConnectionLog, 0, 10)
	m.RecentEvents = make([]SystemEvent, 0, 20)

	now := time.Now()
	m.StartTime = now
	m.lastUpdate = now
	m.lastConnCount = 0
	m.lastPacketCount = 0
	m.Uptime = "0s"
}

func (m *MetricsCollector) CloseConnection() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ActiveFlows > 0 {
		m.ActiveFlows--
	}
}

func (m *MetricsCollector) GetSnapshot() *MetricsCollector {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := &MetricsCollector{
		TotalConnections:    m.TotalConnections,
		ActiveFlows:         m.ActiveFlows,
		PacketsProcessed:    m.PacketsProcessed,
		BytesProcessed:      m.BytesProcessed,
		TCPConnections:      m.TCPConnections,
		UDPConnections:      m.UDPConnections,
		TargetedConnections: m.TargetedConnections,
		RSTDropped:          m.RSTDropped,
		BlockedTotal:        m.BlockedTotal,
		TotalEscalations:    m.TotalEscalations,
		StartTime:           m.StartTime,
		Uptime:              m.Uptime,
		CPUUsage:            m.CPUUsage,
		MemoryUsage:         m.MemoryUsage,
		NFQueueStatus:       m.NFQueueStatus,
		TablesStatus:        m.TablesStatus,
		CurrentCPS:          m.CurrentCPS,
		CurrentPPS:          m.CurrentPPS,
	}

	if len(m.ConnectionRate) > 0 {
		snapshot.ConnectionRate = make([]TimeSeriesPoint, len(m.ConnectionRate))
		copy(snapshot.ConnectionRate, m.ConnectionRate)
	} else {
		snapshot.ConnectionRate = make([]TimeSeriesPoint, 0)
	}

	if len(m.PacketRate) > 0 {
		snapshot.PacketRate = make([]TimeSeriesPoint, len(m.PacketRate))
		copy(snapshot.PacketRate, m.PacketRate)
	} else {
		snapshot.PacketRate = make([]TimeSeriesPoint, 0)
	}

	snapshot.TopDomains = make(map[string]uint64)
	for k, v := range m.TopDomains {
		snapshot.TopDomains[k] = v
	}

	snapshot.ProtocolDist = make(map[string]uint64)
	for k, v := range m.ProtocolDist {
		snapshot.ProtocolDist[k] = v
	}

	snapshot.GeoDist = make(map[string]uint64)
	for k, v := range m.GeoDist {
		snapshot.GeoDist[k] = v
	}

	snapshot.BlockedDomains = make(map[string]uint64, len(m.BlockedDomains))
	for k, v := range m.BlockedDomains {
		snapshot.BlockedDomains[k] = v
	}

	snapshot.BlockedDevices = make(map[string]uint64, len(m.BlockedDevices))
	for k, v := range m.BlockedDevices {
		snapshot.BlockedDevices[k] = v
	}

	if len(m.WorkerStatus) > 0 {
		snapshot.WorkerStatus = make([]WorkerHealth, len(m.WorkerStatus))
		copy(snapshot.WorkerStatus, m.WorkerStatus)
	} else {
		snapshot.WorkerStatus = make([]WorkerHealth, 0)
	}

	if len(m.RecentConnections) > 0 {
		snapshot.RecentConnections = make([]ConnectionLog, len(m.RecentConnections))
		copy(snapshot.RecentConnections, m.RecentConnections)
	} else {
		snapshot.RecentConnections = make([]ConnectionLog, 0)
	}

	if len(m.RecentEvents) > 0 {
		snapshot.RecentEvents = make([]SystemEvent, len(m.RecentEvents))
		copy(snapshot.RecentEvents, m.RecentEvents)
	} else {
		snapshot.RecentEvents = make([]SystemEvent, 0)
	}

	snapshot.DomainTLS = make(map[string]string, len(m.DomainTLS))
	for k, v := range m.DomainTLS {
		snapshot.DomainTLS[k] = v
	}

	snapshot.DeviceDomains = make(map[string]map[string]uint64, len(m.DeviceDomains))
	for mac, domains := range m.DeviceDomains {
		snapshot.DeviceDomains[mac] = make(map[string]uint64, len(domains))
		for d, c := range domains {
			snapshot.DeviceDomains[mac][d] = c
		}
	}

	if len(m.Escalations) > 0 {
		snapshot.Escalations = make([]EscalationEntry, len(m.Escalations))
		copy(snapshot.Escalations, m.Escalations)
	} else {
		snapshot.Escalations = make([]EscalationEntry, 0)
	}

	snapshot.ConnectionRate = smoothTimeSeriesData(m.ConnectionRate, 3)
	snapshot.PacketRate = smoothTimeSeriesData(m.PacketRate, 3)
	return snapshot
}

func (m *MetricsCollector) pruneTopDomains() {
	var minCount uint64 = ^uint64(0)
	var minDomain string

	if len(m.TopDomains) <= 10 {
		return
	}

	for domain, count := range m.TopDomains {
		if count < minCount {
			minCount = count
			minDomain = domain
		}
	}

	delete(m.TopDomains, minDomain)
	delete(m.DomainTLS, minDomain)
}

func smoothTimeSeriesData(data []TimeSeriesPoint, windowSize int) []TimeSeriesPoint {
	if len(data) <= windowSize {
		return data
	}

	smoothed := make([]TimeSeriesPoint, len(data))

	for i := range data {
		sum := 0.0
		count := 0

		for j := max(0, i-windowSize/2); j <= min(len(data)-1, i+windowSize/2); j++ {
			sum += data[j].Value
			count++
		}

		smoothed[i] = TimeSeriesPoint{
			Timestamp: data[i].Timestamp,
			Value:     sum / float64(count),
		}
	}

	return smoothed
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm %ds", days, hours, minutes, seconds)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
