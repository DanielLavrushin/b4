package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestCollectedRefreshAddsNoQueueRulesToAnUnappliedFirewall(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			run = refuseDeletes(run)
			rulesMu.Lock()
			rulesAppliedCfg = nil
			rulesMu.Unlock()
			cfg := goldenConfig(backend, config.DSCPConfig{})
			cfg.Queue.Mode = "tun"
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
			h.take()
			if err := RefreshCollectedRules(cfg); err != nil {
				t.Fatal(err)
			}
			if lines := h.take(); len(lines) != 0 {
				t.Fatalf("a collected refresh with no firewall applied ran %d commands, first %q", len(lines), lines[0])
			}
		})
	}
}

func TestCollectedRefreshAfterTheFinalClearAddsNothing(t *testing.T) {
	oldAdd, oldClear, oldRun := addRulesFn, clearRulesFn, run
	rulesMu.Lock()
	oldApplied, oldClosed := rulesAppliedCfg, dscpSyncClosed.Load()
	rulesMu.Unlock()
	t.Cleanup(func() {
		addRulesFn, clearRulesFn, run = oldAdd, oldClear, oldRun
		rulesMu.Lock()
		rulesAppliedCfg = oldApplied
		dscpSyncClosed.Store(oldClosed)
		rulesMu.Unlock()
	})
	var calls []string
	run = refuseDeletes(func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})
	clearRulesFn = func(*config.Config) error {
		calls = append(calls, "clear")
		return nil
	}
	addRulesFn = func(*config.Config) error {
		calls = append(calls, "add")
		return nil
	}
	applied := config.NewConfig()
	rulesMu.Lock()
	rulesAppliedCfg = &applied
	rulesMu.Unlock()
	if err := ClearAppliedRules(&applied); err != nil {
		t.Fatal(err)
	}
	calls = nil
	next := config.NewConfig()
	next.Queue.TCPConnBytesLimit = applied.Queue.TCPConnBytesLimit + 2
	if err := RefreshCollectedRules(&next); err != nil || len(calls) != 0 {
		t.Fatalf("a collected refresh after the final clear ran %q, err=%v", calls, err)
	}
}
