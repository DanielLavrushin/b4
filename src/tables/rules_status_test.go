package tables

import (
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
)

func restoredEventCount(t *testing.T) int {
	t.Helper()
	total := 0
	for _, ev := range metrics.GetMetricsCollector().Hello().Events.Items {
		if ev.Code != metrics.EventRulesRestored {
			continue
		}
		n, err := strconv.Atoi(ev.Args["count"])
		if err != nil {
			t.Fatalf("rules_restored carries a numeric count, got %+v", ev)
		}
		total += n
	}
	return total
}

func TestRulesStatusFollowsTheMonitorLifecycle(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.Tables.MonitorInterval = 7
	var ptr atomic.Pointer[config.Config]
	ptr.Store(&cfg)
	m := &Monitor{
		cfgPtr:       &ptr,
		stop:         make(chan struct{}),
		interval:     7 * time.Second,
		kick:         make(chan struct{}, 1),
		startDelay:   time.Hour,
		ifaceState:   make(map[string]ifaceSnapshot),
		egressIPHere: make(map[string]bool),
	}
	m.tickFn = func(bool) bool { return false }

	m.Start()
	on := RulesMonitorStatus()
	m.Stop()
	off := RulesMonitorStatus()

	if !on.Monitored || on.Interval != 7*time.Second {
		t.Fatalf("a running monitor reports itself with its interval: %+v", on)
	}
	if off.Monitored {
		t.Fatalf("a stopped monitor is not monitoring: %+v", off)
	}
}

func TestRulesStatusCountsChecksAndRestoresAndRaisesTheEvent(t *testing.T) {
	cfg := newTUNTestConfig()
	cfg.System.Tables.Masquerade.Enabled = true
	stubTUNFirewall(t, map[string]string{
		"-t nat -S POSTROUTING": "-P POSTROUTING ACCEPT\n",
		"-t nat -S B4_MASQ":     "-N B4_MASQ\n",
	})
	masqApplied.Store(cfg)
	t.Cleanup(func() { masqApplied.Store(nil) })

	before := RulesMonitorStatus()
	events := restoredEventCount(t)
	start := time.Now()

	newTUNTestMonitor(cfg).tick(false)

	after := RulesMonitorStatus()
	if after.Restores != before.Restores+1 {
		t.Fatalf("one restore counted: before %d, after %d", before.Restores, after.Restores)
	}
	if after.LastCheck.Before(start) || after.LastRestore.Before(start) {
		t.Fatalf("the check and the restore are dated by this tick: %+v", after)
	}
	if after.LastCheck == after.LastCheck.Round(0) || after.LastRestore == after.LastRestore.Round(0) {
		t.Fatal("the times keep their monotonic reading so the dashboard can relabel them after a clock step")
	}
	if got := restoredEventCount(t); got != events+1 {
		t.Fatalf("the restore raises one rules_restored event: %d before, %d after", events, got)
	}
}

func TestTUNCaptureRestoresCountAsRuleRestores(t *testing.T) {
	before := RulesMonitorStatus()
	diagBefore, _ := RulesRestores()
	events := restoredEventCount(t)
	start := time.Now()

	NoteCaptureRestore()

	after := RulesMonitorStatus()
	if after.Restores != before.Restores+1 || after.LastRestore.Before(start) {
		t.Fatalf("a TUN capture restore is a rule restore: %+v -> %+v", before, after)
	}
	if diagAfter, _ := RulesRestores(); diagAfter != diagBefore {
		t.Fatalf("System Info keeps TUN capture restores apart from the firewall ones: %d -> %d", diagBefore, diagAfter)
	}
	if got := restoredEventCount(t); got != events+1 {
		t.Fatalf("the capture restore raises one rules_restored event: %d before, %d after", events, got)
	}
}
