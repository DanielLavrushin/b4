package tables

import (
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestSyncWithoutRoutingSetsLeavesTheBaseAlone(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	bases := 0
	routeEngine = &mockRouteBackend{ensureBaseFn: func() error {
		bases++
		return errTestClear
	}}
	routingOff := familyTestSet()
	routingOff.Routing.Enabled = false
	setOff := familyTestSet()
	setOff.Id, setOff.Name = "setoff", "setoff"
	setOff.Enabled = false
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{routingOff, setOff}

	RoutingSyncConfig(cfg)
	if bases != 0 {
		t.Fatalf("the sync built the routing base %d times while no enabled set has routing turned on, so a kernel that rejects it logged an error and retried forever", bases)
	}
	if routingSyncRetryConfig() != nil {
		t.Fatalf("a sync with nothing to install was queued for retry")
	}
	if routingSyncedConfig() != cfg {
		t.Fatalf("a sync with nothing to install was not recorded as completed, so the monitor keeps leaving routing alone")
	}
	if st := RoutingStatus(); st.Error != "" || !st.NextRetry.IsZero() || !st.FailingSince.IsZero() {
		t.Fatalf("a sync with nothing to install reported a failure: %+v", st)
	}
}

func TestSyncStillRemovesAnInstalledSetWhenRoutingIsTurnedOff(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	routeEngine = &mockRouteBackend{}
	set := familyTestSet()
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}
	RoutingSyncConfig(cfg)
	if RoutingStatus().Installed != 1 {
		t.Fatalf("the set was not installed by the first sync")
	}

	off := *set
	off.Routing.Enabled = false
	next := familyTestConfig(true, false)
	next.Sets = []*config.SetConfig{&off}
	RoutingSyncConfig(next)
	if n := RoutingStatus().Installed; n != 0 {
		t.Fatalf("turning routing off left %d sets installed, the sync skipped the removal", n)
	}
}

func TestRoutingStatusReportsAFailedBaseUntilARetrySucceeds(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	var fail atomic.Bool
	fail.Store(true)
	routeEngine = &mockRouteBackend{ensureBaseFn: func() error {
		if fail.Load() {
			return errTestClear
		}
		return nil
	}}
	set := familyTestSet()
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}

	before := time.Now()
	RoutingSyncConfig(cfg)
	st := RoutingStatus()
	if st.Error != errTestClear.Error() || st.Backend != "mock" || st.Installed != 0 {
		t.Fatalf("a failed base was not reported with its error and backend: %+v", st)
	}
	if st.FailingSince.Before(before) || st.LastAttempt.Before(before) {
		t.Fatalf("the failure carries no time of the attempt: %+v", st)
	}
	if !st.NextRetry.After(before) {
		t.Fatalf("a queued retry carries no time for the next attempt: %+v", st)
	}

	since := st.FailingSince
	waitUntil(t, "a retry of its own to fail again", func() bool {
		return RoutingStatus().LastAttempt.After(since)
	})
	if got := RoutingStatus(); !got.FailingSince.Equal(since) || got.Error != errTestClear.Error() {
		t.Fatalf("a retry that failed the same way restarted the failure or lost its error: %+v", got)
	}

	fail.Store(false)
	waitUntil(t, "the retry to succeed", func() bool {
		return routingSyncRetryConfig() == nil
	})
	st = RoutingStatus()
	if st.Error != "" || !st.FailingSince.IsZero() || !st.NextRetry.IsZero() || st.Installed != 1 {
		t.Fatalf("the status still reports a failure after the retry installed the set: %+v", st)
	}
}

func TestRoutingStatusNamesTheSetsThatFailedToInstall(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	routeEngine = &mockRouteBackend{ensureChainFn: func(string, bool) error { return errTestClear }}
	set := familyTestSet()
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}

	RoutingSyncConfig(cfg)
	st := RoutingStatus()
	if st.Error != "" {
		t.Fatalf("a failure of one set was reported as a failure of the whole sync: %q", st.Error)
	}
	if len(st.SetErrors) != 1 || st.SetErrors[0].ID != set.Id || st.SetErrors[0].Set != set.Name || !strings.Contains(st.SetErrors[0].Error, errTestClear.Error()) {
		t.Fatalf("the set that failed to install is not named with its error: %+v", st.SetErrors)
	}
	if st.NextRetry.IsZero() {
		t.Fatalf("a set that failed to install carries no time for the next attempt")
	}
}

func TestRoutingStatusKeepsEverySetThatFailedWhenNamesRepeat(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	routeEngine = &mockRouteBackend{ensureChainFn: func(string, bool) error { return errTestClear }}
	first := familyTestSet()
	second := familyTestSet()
	second.Id = "famtest2"
	second.Routing.FWMark, second.Routing.Table = 0x7e11, 233
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{first, second}

	RoutingSyncConfig(cfg)
	st := RoutingStatus()
	if len(st.SetErrors) != 2 {
		t.Fatalf("two failed sets that share the name %q were reported as %d: %+v", first.Name, len(st.SetErrors), st.SetErrors)
	}
	if st.SetErrors[0].ID == st.SetErrors[1].ID {
		t.Fatalf("the failed sets carry the same id, so the list in System Info cannot tell them apart: %+v", st.SetErrors)
	}
}

func TestRoutingStatusClearsASetThatADNSAnswerInstalled(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	routeMu.Lock()
	routeSyncRetryBase = time.Hour
	routeMu.Unlock()
	var fail atomic.Bool
	fail.Store(true)
	routeEngine = &mockRouteBackend{ensureChainFn: func(string, bool) error {
		if fail.Load() {
			return errTestClear
		}
		return nil
	}}
	first := familyTestSet()
	second := familyTestSet()
	second.Id, second.Name = "famtest2", "famtest2"
	second.Routing.FWMark, second.Routing.Table = 0x7e11, 233
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{first, second}

	RoutingSyncConfig(cfg)
	st := RoutingStatus()
	if len(st.SetErrors) != 2 || st.FailingSince.IsZero() {
		t.Fatalf("both sets were expected to fail their first install: %+v", st)
	}
	since := st.FailingSince

	fail.Store(false)
	RoutingHandleDNS(cfg, first, []net.IP{net.ParseIP("198.51.100.40")})
	st = RoutingStatus()
	if st.Installed != 1 {
		t.Fatalf("the DNS answer did not install the set: %+v", st)
	}
	if len(st.SetErrors) != 1 || st.SetErrors[0].ID != second.Id {
		t.Fatalf("a set a DNS answer installed is still reported as failing, or the other set's failure was dropped: %+v", st.SetErrors)
	}
	if !st.FailingSince.Equal(since) {
		t.Fatalf("the set that still fails lost the time its failure began: %+v", st)
	}

	RoutingHandleDNS(cfg, second, []net.IP{net.ParseIP("198.51.100.41")})
	st = RoutingStatus()
	if st.Installed != 2 || len(st.SetErrors) != 0 || st.Error != "" || !st.FailingSince.IsZero() {
		t.Fatalf("with every set installed by DNS answers before the retry, the status still reports a failure: %+v", st)
	}
}

func TestRoutingWithoutAFirewallToolIsReported(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	stubBinaryPresence(t, map[string]bool{
		"nft": false, backendIPTables: false, backendIPTablesLegacy: false, "ipset": false,
	})
	set := familyTestSet()
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}

	RoutingSyncConfig(cfg)
	st := RoutingStatus()
	if st.Error == "" || st.Backend != "" {
		t.Fatalf("a routing set with no firewall tool to install it was not reported: %+v", st)
	}
	if !st.NextRetry.IsZero() || routingSyncRetryConfig() != nil {
		t.Fatalf("a missing tool, which only a restart picks up, was queued for retry")
	}
}

func TestRoutingNamesTheToolThatSentItToNftables(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	stubBinaryPresence(t, map[string]bool{
		"nft": true, backendIPTables: true, "ipset": false,
	})
	cfg := familyTestConfig(true, false)
	cfg.System.Tables.Engine = backendIPTables

	routeMu.Lock()
	be := getRouteBackend(cfg)
	routeMu.Unlock()
	if _, ok := be.(*routeNftBackend); !ok {
		t.Fatalf("iptables without ipset did not fall back to nftables for routing, got %T", be)
	}
	if st := RoutingStatus(); st.Backend != backendNFTables || st.MissingTool != "ipset" {
		t.Fatalf("the fallback to nftables does not name the missing ipset: %+v", st)
	}
}
