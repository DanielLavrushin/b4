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
			"meduza.io": {Domain: "meduza.io", BestPreset: "best", BestSuccess: true, Results: results, Outcome: OutcomeFound, Unconfirmed: true},
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
	if e.Outcome != OutcomeFound || !e.Unconfirmed {
		t.Errorf("the verdict and its confirmation state travel with the entry, got %q unconfirmed=%v", e.Outcome, e.Unconfirmed)
	}
}

func TestCancelSuiteWithoutAChannelDoesNotPanic(t *testing.T) {
	suite := &CheckSuite{Id: "hand-built", Status: CheckStatusRunning}
	RegisterSuite(suite)
	t.Cleanup(func() { suite.Status = CheckStatusComplete })

	if err := CancelCheckSuite(suite.Id); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if suite.Status != CheckStatusCanceled {
		t.Errorf("a suite built without NewCheckSuite must still cancel, got %q", suite.Status)
	}
}

func TestEffectiveOutcomeDerivesLegacyEntries(t *testing.T) {
	cases := []struct {
		name  string
		entry HistoryEntry
		want  Outcome
	}{
		{"baseline works", HistoryEntry{BaselineWorks: true, BestSuccess: true, BestPreset: presetNoBypass}, OutcomeWorksWithoutBypass},
		{"strategy found", HistoryEntry{BestSuccess: true, BestPreset: "combo-random"}, OutcomeFound},
		{"address blocked", HistoryEntry{DNSResult: &DNSDiscoveryResult{TransportBlocked: true}}, OutcomeAddressBlocked},
		{"nothing worked", HistoryEntry{}, OutcomeNotFound},
		{"recorded wins", HistoryEntry{Outcome: OutcomeFound}, OutcomeFound},
	}
	for _, tc := range cases {
		if got := tc.entry.EffectiveOutcome(); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
