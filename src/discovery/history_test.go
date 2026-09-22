package discovery

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func TestHistoryKeepsTheWinnerAndTheFastestAlternatives(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	results := map[string]*DomainPresetResult{}
	for i := 0; i < 60; i++ {
		s := config.NewSetConfig()
		name := fmt.Sprintf("preset-%d", i)
		results[name] = &DomainPresetResult{
			PresetName: name, Set: &s, Status: CheckStatusComplete, Speed: float64(i),
		}
	}
	dead := config.NewSetConfig()
	results["broken"] = &DomainPresetResult{
		PresetName: "broken", Set: &dead, Status: CheckStatusFailed, Speed: 9999,
	}
	winner := config.NewSetConfig()
	winner.Name = "winner"
	results["best"] = &DomainPresetResult{
		PresetName: "best", Set: &winner, Status: CheckStatusComplete, Speed: 500,
	}

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
	if len(e.Results) != 62 {
		t.Errorf("every preset must survive so the UI's count is right, got %d", len(e.Results))
	}
	kept := []string{}
	for name, r := range e.Results {
		if r.Set != nil {
			kept = append(kept, name)
		}
	}
	if len(kept) != maxAlternateSets+1 {
		t.Errorf("a router's flash caps what a domain may keep, got %d sets: %v", len(kept), kept)
	}
	if e.Results["best"].Set == nil {
		t.Error("the winner's set is the one the web interface applies from")
	}
	if e.Results["broken"].Set != nil {
		t.Error("a strategy that failed is not worth a set")
	}
	for i := 60 - maxAlternateSets; i < 60; i++ {
		if e.Results[fmt.Sprintf("preset-%d", i)].Set == nil {
			t.Errorf("preset-%d was among the fastest and must stay applicable", i)
		}
	}
	if e.Results["preset-0"].Set != nil {
		t.Error("the slowest alternatives are the ones to drop")
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
	if e.StorageBytes() == 0 {
		t.Error("the UI shows what an entry costs, so it must be measurable")
	}
}

func TestHistoryRemembersWhichStrategiesWereTried(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	build := func(id string) *CheckSuite {
		winner := config.NewSetConfig()
		winner.Name = "winner"
		return &CheckSuite{
			Id: id, Status: CheckStatusComplete, EndTime: time.Now(),
			DomainDiscoveryResults: map[string]*DomainDiscoveryResult{
				"meduza.io": {Domain: "meduza.io", BestPreset: "best", BestSuccess: true, Outcome: OutcomeFound,
					Results: map[string]*DomainPresetResult{
						"best": {PresetName: "best", Set: &winner, Status: CheckStatusComplete},
					}},
			},
			StrategyGroups: []StrategyGroup{
				{WinnerPreset: "best", Domains: []string{"meduza.io"}, Set: &winner},
			},
		}
	}

	SaveToHistory(build("run-1"), cfgPath)
	if err := MarkAppliedInHistory(cfgPath, []string{"Meduza.IO"}, "combo-random", "set-7"); err != nil {
		t.Fatalf("mark: %v", err)
	}

	hist := LoadDiscoveryHistory(cfgPath)
	if got := hist.AppliedSetFor("meduza.io", "combo-random"); got != "set-7" {
		t.Errorf("a strategy the user installed must be recognisable later, got %q", got)
	}
	if mark := hist.Entries[0].Applied["combo-random"]; mark.At.IsZero() {
		t.Error("when it was tried is what makes the mark readable")
	}

	SaveToHistory(build("run-2"), cfgPath)
	hist = LoadDiscoveryHistory(cfgPath)
	if got := hist.AppliedSetFor("meduza.io", "combo-random"); got != "set-7" {
		t.Errorf("re-running a site must not forget what was already tried, got %q", got)
	}
	if hist.AppliedSetFor("meduza.io", "never-tried") != "" {
		t.Error("an untried strategy must stay unmarked")
	}
}

func TestHistoryKeepsTheOrderTheSitesWereTyped(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	suite := &CheckSuite{
		Id: "run-2", Status: CheckStatusComplete, EndTime: time.Now(),
		Domains: []DomainInput{{Domain: "zona.media"}, {Domain: "meduza.io"}, {Domain: "facebook.com"}},
		DomainDiscoveryResults: map[string]*DomainDiscoveryResult{
			"facebook.com": {Domain: "facebook.com"},
			"meduza.io":    {Domain: "meduza.io"},
			"zona.media":   {Domain: "zona.media"},
		},
	}
	SaveToHistory(suite, cfgPath)

	hist := LoadDiscoveryHistory(cfgPath)
	got := map[string]int{}
	for _, e := range hist.Entries {
		got[e.Domain] = e.Order
	}
	if got["zona.media"] != 1 || got["meduza.io"] != 2 || got["facebook.com"] != 3 {
		t.Fatalf("entries of one run share an end time, so the typed order must be recorded: %v", got)
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

func TestLastRunLogIsSavedNextToTheConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	hub := log.GetDiscoveryHub()
	hub.Reset()
	hub.Broadcast("Starting discovery for 1 domains")
	hub.Broadcast("  ✓ [example.com] Best: combo-pastseq")

	SaveLastRunLog(cfgPath)

	saved, err := LoadLastRunLog(cfgPath)
	if err != nil {
		t.Fatalf("the log must survive the ring being reset by the next run: %v", err)
	}
	if !strings.Contains(string(saved), "combo-pastseq") || !strings.HasPrefix(string(saved), "Starting discovery") {
		t.Fatalf("saved log = %q", saved)
	}
	if _, err := LoadLastRunLog(""); err == nil {
		t.Fatal("without a config path there is nowhere to read from")
	}
}
