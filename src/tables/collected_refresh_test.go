package tables

import (
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
