package metrics

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type MetricsCollector struct {
	mu            sync.RWMutex
	engineFailure *EngineFailure

	initOnce   sync.Once
	nowFn      func() (int64, int64)
	sampler    procSampler
	flowCap    int
	clkTck     uint64
	cores      int
	off        atomic.Int64
	tickMono   atomic.Int64
	rstDropped atomic.Uint64
	escTotal   atomic.Uint64
	listener   atomic.Pointer[func()]
	fl         flowState
	bl         blockedState
	ev         eventState
	esc        escState
	st         liveState
	ts         tickState
}

type EscalationEntry struct {
	Host      string    `json:"host"`
	ToSet     string    `json:"to_set"`
	Hops      int       `json:"hops"`
	SetAt     time.Time `json:"set_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type MTProtoStats struct {
	Enabled           bool                `json:"enabled"`
	Port              int                 `json:"port"`
	Networks          int                 `json:"networks"`
	ActiveConnections int64               `json:"active_connections"`
	TotalConnections  int64               `json:"total_connections"`
	BytesUp           int64               `json:"bytes_up"`
	BytesDown         int64               `json:"bytes_down"`
	Secrets           []MTProtoSecretStat `json:"secrets"`
}

type EngineFailure struct {
	Mode        string `json:"mode"`
	Error       string `json:"error"`
	RetryAt     int64  `json:"retry_at,omitempty"`
	RetriesLeft int    `json:"retries_left"`
}

type MTProtoSecretStat struct {
	Name         string   `json:"name"`
	Active       int64    `json:"active"`
	Total        int64    `json:"total"`
	BytesUp      int64    `json:"bytes_up"`
	BytesDown    int64    `json:"bytes_down"`
	Networks     int      `json:"networks"`
	NetworkAddrs []string `json:"network_addrs,omitempty"`
}

var mtprotoStatsProvider atomic.Pointer[func() *MTProtoStats]

func SetMTProtoStatsProvider(fn func() *MTProtoStats) {
	if fn == nil {
		mtprotoStatsProvider.Store(nil)
		return
	}
	mtprotoStatsProvider.Store(&fn)
}

var (
	metricsCollector *MetricsCollector
	metricsOnce      sync.Once
)

func GetMetricsCollector() *MetricsCollector {
	metricsOnce.Do(func() {
		metricsCollector = &MetricsCollector{}
		metricsCollector.initOnce.Do(metricsCollector.init)
		go metricsCollector.updateLoop()
	})
	return metricsCollector
}

func (m *MetricsCollector) updateLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.tick()
	}
}

func (m *MetricsCollector) RecordRSTDrop() {
	m.rstDropped.Add(1)
}

func (m *MetricsCollector) RecordEscalation() {
	m.escTotal.Add(1)
}

func (m *MetricsCollector) UpdateEscalations(entries []EscalationEntry) {
	m.storeEscalations(entries)
}

func (m *MetricsCollector) SetEngineFailure(f *EngineFailure) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.engineFailure = f
}

func (m *MetricsCollector) GetEngineFailure() *EngineFailure {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.engineFailure == nil {
		return nil
	}
	f := *m.engineFailure
	return &f
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
