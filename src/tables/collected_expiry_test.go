package tables

import (
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func expiringCollectedConsumer(backend string) *config.Config {
	cfg := collectedGuardTestConfig(config.RoutingModeProxy, true)
	cfg.System.Tables.Engine = backend
	cfg.Sets[0].MSSClamp.Enabled, cfg.Sets[0].MSSClamp.Size = true, 88
	return cfg
}

func TestAnExpiredCollectedAddressLeavesTheCaptureAndGuardSets(t *testing.T) {
	pool := []string{"198.51.100.1", "198.51.100.2"}
	left := []string{"198.51.100.2"}
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run("capture set on "+backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			captureCollectedKeep(t)
			run = refuseDeletes(run)
			cfg := expiringCollectedConsumer(backend)
			if err := AddRules(cfg.WithCollectedIPs(pool)); err != nil {
				t.Fatal(err)
			}
			if got := captureCollectedEntries("b4_mss_0_v4c"); !slices.Equal(got, pool) {
				t.Fatalf("setup: the build filled b4_mss_0_v4c with %q, want the pool %q", got, pool)
			}
			h.take()
			if err := SyncCollectedCaptureSets(cfg.WithCollectedIPs(left)); err != nil {
				t.Fatal(err)
			}
			lines := h.take()
			if !slices.ContainsFunc(lines, func(line string) bool {
				return strings.Contains(line, "del") && strings.Contains(line, "b4_mss_0_v4c") && strings.Contains(line, "198.51.100.1")
			}) {
				t.Errorf("the view without the expired address never deleted it from b4_mss_0_v4c, so the clamp keeps covering it:\n%s", strings.Join(lines, "\n"))
			}
			if got := captureCollectedEntries("b4_mss_0_v4c"); !slices.Equal(got, left) {
				t.Errorf("b4_mss_0_v4c is recorded with %q after the expiry, want %q", got, left)
			}
		})
	}
	t.Run("guard set", func(t *testing.T) {
		staticResetGlobals(t)
		run = refuseDeletes(run)
		cfg := expiringCollectedConsumer(backendNFTables)
		var added, deleted []staticCall
		be := staticSyncBackend(&added, &deleted)
		collectedGuardPrepare(t, be, cfg.WithCollectedIPs(pool))
		added, deleted = nil, nil
		st := collectedGuardPrepare(t, be, cfg.WithCollectedIPs(left))
		guard := st.setV4 + "c"
		if len(deleted) != 1 || deleted[0].set != guard || !slices.Equal(deleted[0].ips, []string{"198.51.100.1"}) {
			t.Errorf("the view without the expired address deleted %+v, want 198.51.100.1 from %s; a stale guard entry keeps new connections to it marked for the proxy", deleted, guard)
		}
		if got := collectedGuardLastAdd(added, guard); !slices.Equal(got, left) {
			t.Errorf("the last add to %s was %v, want the remaining pool %v", guard, got, left)
		}
	})
}
