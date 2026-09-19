package tables

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func newKickTestMonitor(t *testing.T, ticks *atomic.Int32) *Monitor {
	t.Helper()
	cfg := config.NewConfig()
	cfg.System.Tables.MonitorInterval = 3600
	var ptr atomic.Pointer[config.Config]
	ptr.Store(&cfg)
	m := &Monitor{
		cfgPtr:       &ptr,
		stop:         make(chan struct{}),
		interval:     time.Hour,
		kick:         make(chan struct{}, 1),
		kickSettle:   30 * time.Millisecond,
		startDelay:   0,
		ifaceState:   make(map[string]ifaceSnapshot),
		egressIPHere: make(map[string]bool),
	}
	m.tickFn = func() bool {
		ticks.Add(1)
		return false
	}
	m.started = true
	m.wg.Add(1)
	go m.monitorLoop()
	t.Cleanup(m.Stop)
	return m
}

func waitTicks(ticks *atomic.Int32, want int32, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if ticks.Load() >= want {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return ticks.Load() >= want
}

func TestMonitorKickRunsOneCheckForABurst(t *testing.T) {
	var ticks atomic.Int32
	m := newKickTestMonitor(t, &ticks)

	for i := 0; i < 6; i++ {
		m.Kick()
		time.Sleep(5 * time.Millisecond)
	}
	if !waitTicks(&ticks, 1, time.Second) {
		t.Fatalf("kick never reached the monitor loop")
	}
	time.Sleep(4 * m.kickSettle)
	if got := ticks.Load(); got != 1 {
		t.Fatalf("a burst of kicks ran %d checks, want 1", got)
	}
}

func TestMonitorKickWaitsForTheBurstToSettle(t *testing.T) {
	var ticks atomic.Int32
	m := newKickTestMonitor(t, &ticks)

	m.Kick()
	time.Sleep(m.kickSettle / 2)
	m.Kick()
	time.Sleep(m.kickSettle / 2)
	m.Kick()
	if got := ticks.Load(); got != 0 {
		t.Fatalf("check ran %d times while kicks were still arriving, want 0", got)
	}
	if !waitTicks(&ticks, 1, time.Second) {
		t.Fatalf("check never ran after the burst settled")
	}
}

func TestMonitorKickBeforeStartDoesNotBlock(t *testing.T) {
	cfg := config.NewConfig()
	var ptr atomic.Pointer[config.Config]
	ptr.Store(&cfg)
	m := NewMonitor(&ptr)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			m.Kick()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Kick blocked on a monitor that was never started")
	}
}

func TestMonitorStopInterruptsSettle(t *testing.T) {
	var ticks atomic.Int32
	m := newKickTestMonitor(t, &ticks)
	m.kickSettle = time.Hour
	m.Kick()
	time.Sleep(20 * time.Millisecond)
	stopped := make(chan struct{})
	go func() {
		m.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatalf("Stop hung while a kick was settling")
	}
	m.started = false
}
