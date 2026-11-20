package tables

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestACarrierRunRecordsItsPathAndStopsWhenTheDSCPRulesAreRewrittenWithoutAnyDiscoveryRule(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	dscpIptNewHost(t, backendIPTables, backendIP6Tables)
	cfg := dscpIptTestConfig(7, true, []string{"wan0"}, dscpPlanTestSet("a", 25, "10.1.2.0/24"))
	cfg.System.Tables.Engine = backendIPTables
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}

	recorded := run
	var calls []string
	run = func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return recorded(args...)
	}
	lost := make(chan error, 4)
	path, release, err := WatchCarrierDiscoveryDSCP(cfg, func(err error) { lost <- err })
	run = recorded
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if got := dscpPathText(path); got != "IPv4 7, IPv6 7, on [wan0]" {
		t.Errorf("a carrier run's probes pass the global stamp, so its path is that stamp's, got %s", got)
	}
	if len(calls) != 0 || activeSteering != nil {
		t.Errorf("a carrier run installs no Discovery rule and no steering, got calls %q, steering %v", calls, activeSteering)
	}
	path.Interfaces[0] = "changed"
	if s := discoveryRun.Load(); s == nil || dscpPathText(s.path) != "IPv4 7, IPv6 7, on [wan0]" {
		t.Error("the run must be registered, and the path it returns must be a copy of the registered one")
	}

	if err := applyDSCPFor(dscpIptTestConfig(9, true, []string{"wan0"}, dscpPlanTestSet("a", 25, "10.1.2.0/24")), backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	select {
	case err := <-lost:
		if !errors.Is(err, errDiscoveryDSCPRewritten) {
			t.Errorf("got %v, want errDiscoveryDSCPRewritten", err)
		}
	case <-time.After(time.Second):
		t.Error("a rewrite of the DSCP rules the probes pass must stop a carrier run")
	}
	if !DiscoveryRunInvalidated() {
		t.Error("a carrier run whose DSCP rules were rewritten must count as invalid")
	}

	release()
	if DiscoveryRunInvalidated() || discoveryRun.Load() != nil {
		t.Error("the release frees the slot")
	}
}

func TestACarrierRunWaitsForPendingDSCPRulesAndRegistersNothing(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	cfg := dscpIptTestConfig(7, true, nil)
	dscpApplied.Store(&dscpState{cfg: cfg, backend: backendIPTables, pending: true})

	if _, _, err := WatchCarrierDiscoveryDSCP(cfg, nil); !errors.Is(err, ErrDiscoveryDSCPPending) {
		t.Errorf("while b4 is still applying its DSCP rules the probes' path is unknown, so a carrier run must wait, got %v", err)
	}
	if discoveryRun.Load() != nil {
		t.Error("a refused carrier run must leave nothing registered")
	}
}

func TestACarrierRunWithoutDSCPRecordsAnEmptyPathAndAStaleReleaseKeepsTheNextRun(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	plain := config.NewConfig()

	path, first, err := WatchCarrierDiscoveryDSCP(&plain, nil)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if path == nil || dscpPathText(path) != "IPv4 -, IPv6 -, on []" {
		t.Errorf("with no DSCP rules the run records a path without values, so turning DSCP on later is caught, got %s", dscpPathText(path))
	}
	discoveryDSCPRewritten(&plain)
	if DiscoveryRunInvalidated() {
		t.Error("with no DSCP rules before or after, nothing changes the probes' path")
	}

	first()
	_, second, err := WatchCarrierDiscoveryDSCP(&plain, nil)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	first()
	if discoveryRun.Load() == nil {
		t.Error("the release of an earlier run must not free the slot of the next one")
	}
	second()
}
