package discovery

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestHistoryKeepsOnlyTheWinningPresetsSet(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	results := map[string]*DomainPresetResult{}
	for i := 0; i < 60; i++ {
		s := config.NewSetConfig()
		results[fmt.Sprintf("preset-%d", i)] = &DomainPresetResult{
			PresetName: fmt.Sprintf("preset-%d", i), Set: &s,
		}
	}
	winner := config.NewSetConfig()
	winner.Name = "winner"
	results["best"] = &DomainPresetResult{PresetName: "best", Set: &winner}

	suite := &CheckSuite{
		Id: "run-1", Status: CheckStatusComplete, EndTime: time.Now(),
		DomainDiscoveryResults: map[string]*DomainDiscoveryResult{
			"meduza.io": {Domain: "meduza.io", BestPreset: "best", BestSuccess: true, Results: results},
		},
		StrategyGroups: []StrategyGroup{
			{WinnerPreset: "best", Domains: []string{"meduza.io"}, Set: &winner},
		},
	}
	SaveToHistory(suite, cfgPath)

	if suite.DomainDiscoveryResults["meduza.io"].Results["preset-3"].Set == nil {
		t.Error("writing history must not strip the live suite the UI is still reading")
	}

	hist := LoadDiscoveryHistory(cfgPath)
	if len(hist.Entries) != 1 {
		t.Fatalf("entries = %d", len(hist.Entries))
	}
	e := hist.Entries[0]
	if len(e.Results) != 61 {
		t.Errorf("every preset must survive so the UI's count is right, got %d", len(e.Results))
	}
	kept := 0
	for _, r := range e.Results {
		if r.Set != nil {
			kept++
		}
	}
	if kept != 1 {
		t.Errorf("only the winner's set is worth keeping on a router's flash, got %d sets", kept)
	}
	if e.Results["best"].Set == nil {
		t.Error("the winner's set is the one the web interface applies from")
	}
	if e.ApplicableSet() == nil || e.ApplicableSet().Name != "winner" {
		t.Errorf("the entry must expose the set a caller can install: %+v", e.ApplicableSet())
	}
	if e.SuiteId != "run-1" {
		t.Errorf("the run id must be recorded so a caller can address it later, got %q", e.SuiteId)
	}
}
