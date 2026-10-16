package tables

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestTheRulesReportTheBackendTheyWereAppliedWith(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendIPTablesLegacy, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			var got []string
			prev := BackendAppliedFunc
			BackendAppliedFunc = func(b string) { got = append(got, b) }
			t.Cleanup(func() { BackendAppliedFunc = prev })

			cfg := goldenConfig(backend, config.DSCPConfig{})
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			if len(got) != 1 || got[0] != backend {
				t.Fatalf("reported %q, want [%s]: the packet engine decides which marks to check for another route by the backend in use", got, backend)
			}
		})
	}
}

func TestSkippedRulesReportNoBackend(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	called := false
	prev := BackendAppliedFunc
	BackendAppliedFunc = func(string) { called = true }
	t.Cleanup(func() { BackendAppliedFunc = prev })

	cfg := goldenConfig(backendIPTables, config.DSCPConfig{})
	cfg.System.Tables.SkipSetup = true
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	if called {
		t.Fatal("with firewall setup skipped no backend was applied, so none may be reported")
	}
}
