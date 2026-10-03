package tables

import (
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/metrics"
)

type RulesStatus struct {
	Monitored   bool
	Interval    time.Duration
	LastCheck   time.Time
	Restores    int64
	LastRestore time.Time
}

var (
	monitorRunning      atomic.Bool
	monitorInterval     atomic.Int64
	monitorLastCheck    atomic.Pointer[time.Time]
	captureRestoreCount atomic.Int64
	captureLastRestore  atomic.Pointer[time.Time]
)

func RulesMonitorStatus() RulesStatus {
	st := RulesStatus{
		Monitored: monitorRunning.Load(),
		Interval:  time.Duration(monitorInterval.Load()),
	}
	if t := monitorLastCheck.Load(); t != nil {
		st.LastCheck = *t
	}
	n, last := RulesRestores()
	st.Restores = n + captureRestoreCount.Load()
	st.LastRestore = last
	if t := captureLastRestore.Load(); t != nil && t.After(st.LastRestore) {
		st.LastRestore = *t
	}
	return st
}

func NoteCaptureRestore() {
	now := time.Now()
	captureLastRestore.Store(&now)
	captureRestoreCount.Add(1)
	metrics.GetMetricsCollector().NoteRulesRestored()
}

func noteRulesChecked() {
	now := time.Now()
	monitorLastCheck.Store(&now)
}

func noteMonitorRunning(interval time.Duration) {
	monitorInterval.Store(int64(interval))
	monitorRunning.Store(true)
}

func noteMonitorStopped() {
	monitorRunning.Store(false)
}
